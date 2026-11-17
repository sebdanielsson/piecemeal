package natpmp

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	natpmp "github.com/jackpal/go-nat-pmp"
)

type fakeGateway struct {
	mu    sync.Mutex
	port  uint16
	fail  bool
	calls int
}

func (g *fakeGateway) AddPortMapping(_ string, _, _, lifetime int) (*natpmp.AddPortMappingResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.fail {
		return nil, errors.New("gateway unreachable")
	}
	return &natpmp.AddPortMappingResult{MappedExternalPort: g.port, PortMappingLifetimeInSeconds: uint32(lifetime)}, nil
}

func (g *fakeGateway) set(port uint16, fail bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.port, g.fail = port, fail
}

func TestMapperFollowsPortChanges(t *testing.T) {
	gw := &fakeGateway{port: 40000}
	m := NewWithClient(gw, "10.2.0.1", slog.New(slog.DiscardHandler))
	m.Lifetime = 2 // renew every second
	m.RetryInterval = 10 * time.Millisecond
	ports := make(chan int, 10)
	m.OnPort = func(p int) { ports <- p }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	if p := <-ports; p != 40000 {
		t.Fatalf("first port %d", p)
	}
	if !m.Ready() {
		t.Fatal("not ready after mapping")
	}
	gw.set(0, true)
	time.Sleep(1200 * time.Millisecond)
	if m.Status().Error == "" {
		t.Fatal("no error reported while the gateway fails")
	}
	gw.set(41000, false)
	select {
	case p := <-ports:
		if p != 41000 {
			t.Fatalf("new port %d", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("port change not reported")
	}
	if st := m.Status(); st.Port != 41000 || st.Error != "" {
		t.Fatalf("status %+v", st)
	}
}
