// Package natpmp keeps a TCP and a UDP port mapping open on a NAT-PMP
// gateway, such as a VPN provider's, and reports the forwarded port.
//
// Like ProtonVPN's instructions, it asks for internal port 0: the gateway
// picks the external port and forwards it to the same port locally, so the
// client must listen on whatever port comes back. When the gateway moves the
// mapping (the tunnel reconnected, say), OnPort is called with the new port.
package natpmp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	natpmp "github.com/jackpal/go-nat-pmp"
)

// Mapping is the mapping as last seen.
type Mapping struct {
	Gateway   string    `json:"gateway"`
	Port      int       `json:"port" doc:"The forwarded port, 0 until mapped."`
	Lifetime  int       `json:"lifetime" doc:"Seconds the gateway keeps the mapping."`
	RenewedAt time.Time `json:"renewedAt"`
	Error     string    `json:"error,omitempty"`
}

// Client is the subset of the NAT-PMP client the mapper uses.
type Client interface {
	AddPortMapping(protocol string, internalPort, requestedExternalPort, lifetime int) (*natpmp.AddPortMappingResult, error)
}

// Mapper maintains the mappings.
type Mapper struct {
	client  Client
	gateway string
	log     *slog.Logger
	// OnPort is called with the forwarded port whenever it changes,
	// including the first time.
	OnPort func(port int)
	// Lifetime is the lifetime requested for each mapping, in seconds.
	Lifetime int
	// RetryInterval is how long to wait after a failed attempt.
	RetryInterval time.Duration

	mu     sync.Mutex
	status Mapping
}

// New creates a mapper for the gateway.
func New(gateway net.IP, log *slog.Logger) *Mapper {
	return NewWithClient(natpmp.NewClientWithTimeout(gateway, 10*time.Second), gateway.String(), log)
}

// NewWithClient creates a mapper using client, for tests.
func NewWithClient(client Client, gateway string, log *slog.Logger) *Mapper {
	return &Mapper{
		client: client, gateway: gateway, log: log, Lifetime: 60, RetryInterval: 5 * time.Second,
		status: Mapping{Gateway: gateway},
	}
}

// Status returns the mapping as last seen.
func (m *Mapper) Status() Mapping {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Ready reports whether a port is mapped.
func (m *Mapper) Ready() bool {
	return m.Status().Port != 0
}

func (m *Mapper) mapPort(want int) (port, lifetime int, err error) {
	tcp, err := m.client.AddPortMapping("tcp", 0, want, m.Lifetime)
	if err != nil {
		return 0, 0, fmt.Errorf("tcp mapping: %w", err)
	}
	udp, err := m.client.AddPortMapping("udp", 0, int(tcp.MappedExternalPort), m.Lifetime)
	if err != nil {
		return 0, 0, fmt.Errorf("udp mapping: %w", err)
	}
	if udp.MappedExternalPort != tcp.MappedExternalPort {
		m.log.Warn("NAT-PMP gave UDP a different port than TCP; uTP and DHT won't be reachable inbound",
			"tcp", tcp.MappedExternalPort, "udp", udp.MappedExternalPort)
	}
	return int(tcp.MappedExternalPort), int(tcp.PortMappingLifetimeInSeconds), nil
}

// Run maps a port and renews the mapping until ctx ends. It asks to keep
// the current port on every renewal and follows the gateway when it hands
// out another.
func (m *Mapper) Run(ctx context.Context) {
	port, lifetime := 0, 0
	for {
		var wait time.Duration
		got, l, err := m.mapPort(port)
		m.mu.Lock()
		if err != nil {
			m.status.Error = err.Error()
			m.mu.Unlock()
			m.log.Warn("NAT-PMP mapping failed", "gateway", m.gateway, "error", err)
			wait = m.RetryInterval
		} else {
			changed := got != port
			port, lifetime = got, l
			m.status = Mapping{Gateway: m.gateway, Port: port, Lifetime: lifetime, RenewedAt: time.Now()}
			m.mu.Unlock()
			if changed {
				m.log.Info("NAT-PMP port forwarded", "gateway", m.gateway, "port", port, "lifetime", lifetime)
				if m.OnPort != nil {
					m.OnPort(port)
				}
			}
			// Renew well before expiry.
			wait = max(time.Duration(lifetime)*time.Second/2, time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// ErrNoGateway is returned when NAT-PMP is configured without a gateway.
var ErrNoGateway = errors.New("no NAT-PMP gateway configured")
