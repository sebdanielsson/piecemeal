// Package engine owns the anacrolix/torrent client and everything
// torrent-shaped: adding, removing, starting and stopping torrents, limits,
// seed goals, categories and tags, and the state the rest of piecemeal reads.
//
// Nothing outside this package imports anacrolix. Reads (List, Get, Transfer)
// are served from a snapshot that a background loop refreshes every second,
// so they never wait on the client's lock or on disk I/O.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/mse"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"

	"github.com/sebdanielsson/piecemeal/internal/settings"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Config is what the engine needs from piecemeal's configuration.
type Config struct {
	// DataDir holds anacrolix's piece completion database.
	DataDir string
	// AllowedRoots confines every save path. The default save path and
	// category paths must be inside one of them too.
	AllowedRoots []string
	// Mmap selects anacrolix's memory-mapped file I/O instead of pread and
	// pwrite. See docs/plan.md for why it is off by default.
	Mmap bool
	// DisableIPv6 stops listening and dialling on IPv6, for networks such as
	// an IPv4-only VPN tunnel.
	DisableIPv6 bool
	// ListenHost is the address to bind the peer listeners to; empty is all.
	ListenHost string
	// PortForwarded is true when something outside the engine (NAT-PMP)
	// forwards the peer port, which turns off anacrolix's UPnP and makes
	// the peerPort setting irrelevant.
	PortForwarded bool
	// InitialPort is the forwarded port to start on when PortForwarded.
	InitialPort int
	// TickInterval is how often the snapshot is refreshed. Default 1s.
	TickInterval time.Duration
	// CheckpointInterval is how often counters are written to the store.
	// Default 1m.
	CheckpointInterval time.Duration

	// ClientConfig, when set, adjusts the anacrolix configuration before the
	// client is created. Tests use it to stay off the network.
	ClientConfig func(*torrent.ClientConfig)

	// wrapStorage, when set, wraps the torrent storage; tests slow down
	// hashing with it.
	wrapStorage func(storage.ClientImplCloser) storage.ClientImplCloser
}

// Engine is the torrent engine.
type Engine struct {
	cfg      Config
	log      *slog.Logger
	store    *store.Store
	settings *settings.Manager

	pc      storage.PieceCompletion
	storage storage.ClientImplCloser
	// Where each torrent's files go (infohash to torrentPaths), and which
	// torrent a *metainfo.Info belongs to: the storage's path makers read
	// them.
	paths      sync.Map
	infoOwners sync.Map
	filter     peerFilter

	down, up *rate.Limiter

	// mu guards the client, the torrent map and every managed torrent's
	// fields. Reads served from the snapshot don't take it.
	mu     sync.Mutex
	client *torrent.Client
	// metaFetches are magnets whose metadata qBittorrent's fetchMetadata
	// asked for, without adding them.
	metaFetches map[metainfo.Hash]*metaFetch
	port        int
	torrents    map[metainfo.Hash]*managed
	cats        map[string]Category
	tags        map[string]struct{}
	// checkSeq numbers torrents as they join the check queue.
	checkSeq uint64

	snap atomic.Pointer[snapshot]
	bus  *Bus

	// lastWindow is whether the alternative speed schedule's window was on
	// at the last check, nil before the first.
	lastWindow *bool

	creator creator

	session sessionCounters

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool
}

type sessionCounters struct {
	mu         sync.Mutex
	downloaded int64
	uploaded   int64
}

// Errors returned by engine methods. Callers map them to API responses.
var (
	ErrNotFound      = errors.New("not found")
	ErrExists        = errors.New("already exists")
	ErrInvalid       = errors.New("invalid")
	ErrPathForbidden = errors.New("path is outside the allowed roots")
	ErrClosed        = errors.New("engine closed")
)

// New loads every torrent from the store and starts the client. Close it
// when done.
func New(ctx context.Context, cfg Config, st *store.Store, sm *settings.Manager, log *slog.Logger) (*Engine, error) {
	if cfg.TickInterval == 0 {
		cfg.TickInterval = time.Second
	}
	if cfg.CheckpointInterval == 0 {
		cfg.CheckpointInterval = time.Minute
	}
	if len(cfg.AllowedRoots) == 0 {
		return nil, errors.New("engine: no allowed roots")
	}
	roots := make([]string, 0, len(cfg.AllowedRoots))
	for _, r := range cfg.AllowedRoots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, fmt.Errorf("allowed root %q: %w", r, err)
		}
		roots = append(roots, filepath.Clean(abs))
	}
	cfg.AllowedRoots = roots

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	pc, err := storage.NewBoltPieceCompletion(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("piece completion database: %w", err)
	}

	e := &Engine{
		cfg:         cfg,
		log:         log,
		store:       st,
		settings:    sm,
		pc:          pc,
		torrents:    map[metainfo.Hash]*managed{},
		metaFetches: map[metainfo.Hash]*metaFetch{},
		cats:        map[string]Category{},
		tags:        map[string]struct{}{},
		bus:         newBus(),
		creator:     creator{wake: make(chan struct{}, 1)},
		down:        rate.NewLimiter(rate.Inf, 1<<20),
		up:          rate.NewLimiter(rate.Inf, 1<<20),
	}
	e.filter.log = newPeerLog()
	opts := storage.NewFileClientOpts{
		ClientBaseDir:   cfg.DataDir,
		PieceCompletion: nopClosePieceCompletion{pc},
		DisableMmap:     !cfg.Mmap,
		// Incomplete files keep their final names, as in qBittorrent by
		// default. With part files, anacrolix v1.63.0 closes a file's cached
		// writer on promotion while the piece hasher may still be reading
		// through it (a data race in storage/file-io-classic.go).
		UsePartFiles: g.Some(false),
		Logger:       log.With("component", "storage"),
	}
	e.storagePaths(&opts)
	e.storage = storage.NewFileOpts(opts)
	if cfg.wrapStorage != nil {
		e.storage = cfg.wrapStorage(e.storage)
	}
	e.applyLimits(sm.Get())
	e.snap.Store(&snapshot{byHash: map[string]*Torrent{}})

	if err := e.loadCategoriesAndTags(ctx); err != nil {
		return nil, errors.Join(err, e.closeStorage())
	}

	s := sm.Get()
	if _, err := e.confine(s.DefaultSavePath); err != nil {
		return nil, errors.Join(fmt.Errorf("defaultSavePath: %w", err), e.closeStorage())
	}
	port := s.PeerPort
	if cfg.PortForwarded {
		port = cfg.InitialPort
	}
	e.reloadFilter(s)
	if err := e.startClient(port, s); err != nil {
		return nil, errors.Join(err, e.closeStorage())
	}
	if err := e.loadTorrents(ctx); err != nil {
		return nil, errors.Join(append(e.client.Close(), err, e.closeStorage())...)
	}

	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.mu.Lock()
	e.normalizeQueueLocked()
	e.scheduleLocked(time.Now())
	e.mu.Unlock()
	sm.Subscribe(e.onSettingsChanged)
	sm.AddValidator(func(s settings.Settings) error {
		if _, err := e.confine(s.DefaultSavePath); err != nil {
			return fmt.Errorf("defaultSavePath: %w", err)
		}
		return nil
	})
	e.tick(time.Now())
	e.wg.Go(e.loop)
	e.wg.Go(e.runCreator)
	return e, nil
}

// Close stops the client after writing every torrent's counters.
func (e *Engine) Close() error {
	if !e.closed.CompareAndSwap(false, true) {
		return nil
	}
	e.cancel()
	e.wg.Wait()
	e.tick(time.Now())
	errs := []error{e.checkpoint(context.Background(), true)}
	e.mu.Lock()
	errs = append(errs, e.client.Close()...)
	e.mu.Unlock()
	e.bus.close()
	errs = append(errs, e.closeStorage())
	return errors.Join(errs...)
}

func (e *Engine) closeStorage() error {
	return errors.Join(e.storage.Close(), e.pc.Close())
}

// Events returns the engine's event bus.
func (e *Engine) Events() *Bus { return e.bus }

// Port returns the port the client listens for peers on.
func (e *Engine) Port() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.port
}

// SetPort moves the peer listeners to port, recreating the client. Torrents
// keep running; their peers reconnect on the new port.
func (e *Engine) SetPort(port int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if port == e.port {
		return nil
	}
	return e.restartClientLocked(port, e.settings.Get())
}

func (e *Engine) onSettingsChanged(old, cur settings.Settings) {
	e.applyLimits(cur)
	filterChanged := cur.IPFilterEnabled != old.IPFilterEnabled || cur.IPFilterPath != old.IPFilterPath ||
		cur.BannedIPs != old.BannedIPs
	if filterChanged {
		// Outside the lock: a large filter file takes a while to parse.
		e.reloadFilter(cur)
	}
	e.mu.Lock()
	e.refreshTransferLocked()
	if cur.MaxConnectionsPerTorrent != old.MaxConnectionsPerTorrent {
		for _, m := range e.torrents {
			if m.t != nil {
				m.t.SetMaxEstablishedConns(cur.MaxConnectionsPerTorrent)
			}
		}
	}
	if filterChanged {
		e.dropBlockedPeersLocked()
	}
	if cur.MaxActiveCheckingTorrents != old.MaxActiveCheckingTorrents {
		e.scheduleChecksLocked()
	}
	needsRestart := cur.DHT != old.DHT || cur.PEX != old.PEX || cur.UTP != old.UTP ||
		cur.Encryption != old.Encryption || cur.MaxHalfOpenConnections != old.MaxHalfOpenConnections ||
		(!e.cfg.PortForwarded && cur.PeerPort != old.PeerPort)
	if needsRestart {
		// Keep the port in use unless the port setting changed, also when
		// it is 0 (random).
		port := e.port
		if !e.cfg.PortForwarded && cur.PeerPort != old.PeerPort {
			port = cur.PeerPort
		}
		if err := e.restartClientLocked(port, cur); err != nil {
			e.log.Error("restarting client for new settings", "error", err)
		}
	}
	e.mu.Unlock()
	e.bus.publish(Event{Type: EventSettings})
}

func (e *Engine) applyLimits(s settings.Settings) {
	down, up := s.DownloadLimit, s.UploadLimit
	if s.AltSpeedEnabled {
		down, up = s.AltDownloadLimit, s.AltUploadLimit
	}
	setLimit(e.down, down)
	setLimit(e.up, up)
}

func setLimit(l *rate.Limiter, bytesPerSecond int64) {
	if bytesPerSecond <= 0 {
		l.SetLimit(rate.Inf)
		return
	}
	l.SetLimit(rate.Limit(bytesPerSecond))
}

func limitOf(l *rate.Limiter) int64 {
	if l.Limit() == rate.Inf {
		return 0
	}
	return int64(math.Round(float64(l.Limit())))
}

func (e *Engine) clientConfig(port int, s settings.Settings) *torrent.ClientConfig {
	tc := torrent.NewDefaultClientConfig()
	tc.DataDir = e.cfg.DataDir
	tc.DefaultStorage = e.storage
	tc.ListenPort = port
	tc.Seed = true
	tc.NoDefaultPortForwarding = e.cfg.PortForwarded
	tc.DisableIPv6 = e.cfg.DisableIPv6
	if host := e.cfg.ListenHost; host != "" {
		tc.ListenHost = func(string) string { return host }
		// anacrolix listens on both families; an address of one family
		// can't be bound for the other.
		if ip := net.ParseIP(host); ip != nil {
			if ip.To4() != nil {
				tc.DisableIPv6 = true
			} else {
				tc.DisableIPv4 = true
			}
		}
	}
	tc.UploadRateLimiter = e.up
	tc.DownloadRateLimiter = e.down
	tc.EstablishedConnsPerTorrent = s.MaxConnectionsPerTorrent
	tc.TotalHalfOpenConns = s.MaxHalfOpenConnections
	tc.NoDHT = !s.DHT
	tc.DisablePEX = !s.PEX
	tc.DisableUTP = !s.UTP
	switch s.Encryption {
	case "require":
		tc.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{Preferred: true, RequirePreferred: true}
		tc.CryptoProvides = mse.CryptoMethodRC4
	case "disable":
		tc.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{Preferred: false, RequirePreferred: false}
		tc.CryptoProvides = mse.CryptoMethodPlaintext
	default:
		tc.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{Preferred: true}
	}
	tc.IPBlocklist = &e.filter
	tc.Slogger = e.log.With("component", "anacrolix")
	if e.cfg.ClientConfig != nil {
		e.cfg.ClientConfig(tc)
	}
	return tc
}

// startClient creates the anacrolix client. The caller holds mu or is New.
func (e *Engine) startClient(port int, s settings.Settings) error {
	cl, err := torrent.NewClient(e.clientConfig(port, s))
	if err != nil {
		return fmt.Errorf("torrent client on port %d: %w", port, err)
	}
	e.client = cl
	e.port = cl.LocalPort()
	return nil
}

// startClientRetrying starts a client on port, waiting for the previous
// client to let go of it: anacrolix's Client.Close closes its listeners in
// goroutines (v1.63.0, client.go: go s.Close()), so the port can still be
// bound for a moment after Close returns.
func (e *Engine) startClientRetrying(port int, s settings.Settings) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := e.startClient(port, s)
		if err == nil || port == 0 || !errors.Is(err, syscall.EADDRINUSE) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// restartClientLocked replaces the client, for changes anacrolix only reads
// at startup such as the listen port. Running torrents are re-added; piece
// completion is persisted, so they don't rehash.
func (e *Engine) restartClientLocked(port int, s settings.Settings) error {
	e.log.Info("restarting torrent client", "port", port)
	// Fold the old client's session counters into the records first: the new
	// client's counters start from zero.
	e.collectCountersLocked()
	for _, err := range e.client.Close() {
		e.log.Warn("closing torrent client", "error", err)
	}
	for _, m := range e.torrents {
		m.t = nil
		if m.check == checkActive {
			m.check = checkPending // starts over below, first in line
		}
		e.forgetInfo(m.hash)
	}
	if err := e.startClientRetrying(port, s); err != nil {
		// Leave the engine usable: fall back to any free port.
		e.log.Error("restarting torrent client, falling back to a random port", "error", err)
		if err2 := e.startClient(0, s); err2 != nil {
			return errors.Join(err, err2)
		}
	}
	for _, m := range e.torrents {
		if !m.rec.Stopped {
			if err := e.attachLocked(m); err != nil {
				m.setError(err)
			}
		}
	}
	e.scheduleChecksLocked()
	return nil
}

// collectCountersLocked folds every attached torrent's anacrolix counters
// into its record.
func (e *Engine) collectCountersLocked() {
	for _, m := range e.torrents {
		if m.t != nil {
			st := m.t.Stats()
			m.addCounters(st.BytesReadUsefulData.Int64(), st.BytesWrittenData.Int64())
		}
	}
}

func (e *Engine) loop() {
	tick := time.NewTicker(e.cfg.TickInterval)
	defer tick.Stop()
	checkpoint := time.NewTicker(e.cfg.CheckpointInterval)
	defer checkpoint.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case now := <-tick.C:
			e.mu.Lock()
			e.normalizeQueueLocked()
			e.scheduleLocked(now)
			e.mu.Unlock()
			e.tick(now)
			e.enforceSeedGoals()
			e.applySchedule(now)
		case <-checkpoint.C:
			if err := e.checkpoint(e.ctx, false); err != nil && !errors.Is(err, context.Canceled) {
				e.log.Error("saving torrent counters", "error", err)
			}
		}
	}
}

// checkpoint writes the records of torrents whose counters changed since the
// last checkpoint, or all of them with all set.
func (e *Engine) checkpoint(ctx context.Context, all bool) error {
	// The write happens under the lock too, so a torrent removed meanwhile
	// can't have its row written back after Remove deleted it.
	e.mu.Lock()
	defer e.mu.Unlock()
	var dirty []*managed
	for _, m := range e.torrents {
		if all || m.dirty {
			dirty = append(dirty, m)
		}
	}
	if len(dirty) == 0 {
		return nil
	}
	err := e.store.Tx(ctx, func(q *store.Queries) error {
		for _, m := range dirty {
			if err := q.UpsertTorrent(ctx, m.params()); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		for _, m := range dirty {
			m.dirty = false
		}
	}
	return err
}

type nopClosePieceCompletion struct{ storage.PieceCompletion }

func (nopClosePieceCompletion) Close() error { return nil }

// ListenAddrs returns the addresses the client accepts peers on.
func (e *Engine) ListenAddrs() []net.Addr {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.client.ListenAddrs()
}
