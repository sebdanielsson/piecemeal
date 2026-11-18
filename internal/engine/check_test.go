package engine

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/sebdanielsson/piecemeal/internal/engine/enginetest"
)

// slowHashing makes every piece take delay to hash, so checks last long
// enough to watch. anacrolix lets a storage hash its own pieces
// (storage.SelfHashing), which is where the delay goes.
func slowHashing(delay time.Duration) func(*harness) {
	return func(h *harness) {
		h.wrapStorage = func(ci storage.ClientImplCloser) storage.ClientImplCloser {
			return slowStorage{ci, delay}
		}
	}
}

type slowStorage struct {
	storage.ClientImplCloser
	delay time.Duration
}

func (s slowStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, ih metainfo.Hash) (storage.TorrentImpl, error) {
	ti, err := s.ClientImplCloser.OpenTorrent(ctx, info, ih)
	if err != nil {
		return ti, err
	}
	if piece := ti.Piece; piece != nil {
		ti.Piece = func(p metainfo.Piece) storage.PieceImpl { return slowPiece{piece(p), p.Length(), s.delay} }
	}
	if piece := ti.PieceWithHash; piece != nil {
		ti.PieceWithHash = func(p metainfo.Piece, h g.Option[[]byte]) storage.PieceImpl {
			return slowPiece{piece(p, h), p.Length(), s.delay}
		}
	}
	return ti, nil
}

type slowPiece struct {
	storage.PieceImpl
	length int64
	delay  time.Duration
}

func (p slowPiece) SelfHash() (metainfo.Hash, error) {
	time.Sleep(p.delay)
	h := sha1.New()
	_, err := io.Copy(h, io.NewSectionReader(p.PieceImpl, 0, p.length))
	var sum metainfo.Hash
	copy(sum[:], h.Sum(nil))
	return sum, err
}

func withSetting(name string, v any) func(*harness) {
	return func(h *harness) {
		if _, err := h.settings.Set(context.Background(), map[string]any{name: v}); err != nil {
			h.t.Fatal(err)
		}
	}
}

// checkEvent is a check starting or ending, from the engine's log.
type checkEvent struct {
	start bool
	hash  string
	all   bool
}

// checkLog collects the engine's "checking torrent" and "torrent checked"
// log records, in the order the engine wrote them (under its lock).
type checkLog struct {
	mu     sync.Mutex
	events []checkEvent
}

func withCheckLog(l *checkLog) func(*harness) {
	return func(h *harness) { h.log = slog.New(l) }
}

func (l *checkLog) Enabled(_ context.Context, level slog.Level) bool { return level >= slog.LevelInfo }
func (l *checkLog) WithAttrs([]slog.Attr) slog.Handler               { return l }
func (l *checkLog) WithGroup(string) slog.Handler                    { return l }

func (l *checkLog) Handle(_ context.Context, r slog.Record) error {
	var ev checkEvent
	switch r.Message {
	case "checking torrent":
		ev.start = true
	case "torrent checked":
	default:
		return nil
	}
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "hash":
			ev.hash = a.Value.String()
		case "allPieces":
			ev.all = a.Value.Bool()
		}
		return true
	})
	l.mu.Lock()
	l.events = append(l.events, ev)
	l.mu.Unlock()
	return nil
}

// take returns the events so far and forgets them.
func (l *checkLog) take() []checkEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	ev := l.events
	l.events = nil
	return ev
}

// replay checks a sequence of check events: no more than limit checks at
// once, and every check that starts ends. It returns the hashes in the
// order their checks started, and the most checks at once.
func replay(t *testing.T, events []checkEvent, limit int) (started []string, most int) {
	t.Helper()
	active := map[string]bool{}
	for _, ev := range events {
		if !ev.start {
			if !active[ev.hash] {
				t.Errorf("%s: check ended without starting", ev.hash)
			}
			delete(active, ev.hash)
			continue
		}
		if active[ev.hash] {
			t.Errorf("%s: check started twice", ev.hash)
		}
		active[ev.hash] = true
		started = append(started, ev.hash)
		most = max(most, len(active))
		if len(active) > limit {
			t.Errorf("%d torrents checked at once, limit %d", len(active), limit)
		}
	}
	if len(active) > 0 {
		t.Errorf("checks still running: %v", active)
	}
	return started, most
}

// diskTorrent is a torrent whose data is already in the save path, and a
// seeder with all of it.
type diskTorrent struct {
	*enginetest.Seeder
}

// newDiskTorrents makes n single-file torrents of size bytes in 32 KiB
// pieces, with their data in the save path. The data of those in corrupt
// has a wrong first byte, so their first piece fails its check and has to
// come from the seeder.
func (h *harness) newDiskTorrents(n, size int, corrupt ...int) []diskTorrent {
	h.t.Helper()
	out := make([]diskTorrent, n)
	for i := range out {
		s := enginetest.NewSeeder(h.t, fmt.Sprintf("t%d", i), map[string]int{"data.bin": size})
		for p, b := range s.Content {
			b = bytes.Clone(b)
			if slices.Contains(corrupt, i) {
				b[0] ^= 0xff
			}
			full := filepath.Join(h.saveDir, s.Name, p)
			if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
				h.t.Fatal(err)
			}
			if err := os.WriteFile(full, b, 0o600); err != nil {
				h.t.Fatal(err)
			}
		}
		out[i] = diskTorrent{s}
	}
	return out
}

// checkContents checks every torrent's data matches its seeder's.
func (h *harness) checkContents(ts []diskTorrent) {
	h.t.Helper()
	for _, dt := range ts {
		for p, want := range dt.Content {
			got, err := os.ReadFile(filepath.Join(h.saveDir, dt.Name, p))
			if err != nil {
				h.t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				h.t.Fatalf("%s/%s: content differs from the seeder's", dt.Name, p)
			}
		}
	}
}

func (h *harness) states() map[State]int {
	out := map[State]int{}
	for _, st := range h.engine.List() {
		out[st.State]++
	}
	return out
}

func (h *harness) waitAllComplete(ts []diskTorrent) {
	h.t.Helper()
	for _, dt := range ts {
		h.waitFor(dt.Hash, dt.Name+" complete", func(st Torrent) bool {
			return st.Complete && st.State != StateChecking && st.State != StateQueuedForChecking
		})
	}
}

// checkWatch samples the engine every millisecond while a test runs and
// records what breaks the check queue's rules:
//
//   - more torrents checked at once than the limit;
//   - a torrent waiting for its check in the client;
//   - a torrent in the client with pieces of unknown completion, outside a
//     check;
//   - payload moving for a torrent whose check isn't done.
//
// It gives each torrent being checked its seeder as a peer, so a torrent
// allowed to download during its check would.
type checkWatch struct {
	stop, done chan struct{}

	mu         sync.Mutex
	problems   []string
	mostActive int
	waiting    bool
}

func (h *harness) watchChecks(ts []diskTorrent) *checkWatch {
	peers := map[string]torrent.PeerInfo{}
	for _, dt := range ts {
		ap := netip.MustParseAddrPort(dt.Addr)
		peers[dt.Hash] = torrent.PeerInfo{Addr: net.TCPAddrFromAddrPort(ap), Trusted: true}
	}
	w := &checkWatch{stop: make(chan struct{}), done: make(chan struct{})}
	given := map[*torrent.Torrent]bool{}
	go func() {
		defer close(w.done)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-tick.C:
			}
			w.sample(h.engine, peers, given)
		}
	}()
	h.t.Cleanup(w.finish)
	return w
}

func (w *checkWatch) sample(e *Engine, peers map[string]torrent.PeerInfo, given map[*torrent.Torrent]bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	limit := e.settings.Get().MaxActiveCheckingTorrents
	var problems []string
	active, waiting := 0, false
	for _, m := range e.torrents {
		name := m.rec.Name
		switch m.check {
		case checkActive:
			active++
		case checkPending:
			waiting = waiting || m.waitingForCheck()
			if m.t != nil {
				problems = append(problems, name+" waits for its check in the client")
			}
		}
		if m.t == nil || m.info == nil {
			continue
		}
		if m.check != checkNone {
			st := m.t.Stats()
			if down, up := st.BytesReadUsefulData.Int64(), st.BytesWrittenData.Int64(); down > 0 || up > 0 {
				problems = append(problems, fmt.Sprintf("%s transferred %d down and %d up during its check", name, down, up))
			}
			if p, ok := peers[m.rec.InfoHash]; ok && !given[m.t] {
				m.t.AddPeers([]torrent.PeerInfo{p})
				given[m.t] = true
			}
			continue
		}
		for _, run := range m.t.PieceStateRuns() {
			if !run.Ok {
				problems = append(problems, name+" is in the client with pieces of unknown completion, outside a check")
				break
			}
		}
	}
	if active > limit {
		problems = append(problems, fmt.Sprintf("%d torrents checked at once, limit %d", active, limit))
	}
	w.mu.Lock()
	w.problems = append(w.problems, problems...)
	w.mostActive = max(w.mostActive, active)
	w.waiting = w.waiting || waiting
	w.mu.Unlock()
}

// finish stops the sampling; it's safe to call more than once.
func (w *checkWatch) finish() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
}

// verify stops the sampling and fails the test on any problem seen. It
// also wants to have seen torrents waiting, and as many checked at once as
// most.
func (w *checkWatch) verify(t *testing.T, most int) {
	t.Helper()
	w.finish()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range slices.Compact(w.problems) {
		t.Error(p)
	}
	if !w.waiting {
		t.Error("never saw a torrent waiting for its check")
	}
	if w.mostActive != most {
		t.Errorf("at most %d torrents were checked at once, want %d", w.mostActive, most)
	}
}

// TestCheckQueueOnAdd adds torrents whose data is already on disk: they're
// checked one at a time, the others wait outside the client, nothing moves
// before a torrent's data is checked, and then the bad pieces come from
// the seeders.
func TestCheckQueueOnAdd(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, slowHashing(2*time.Millisecond), withCheckLog(&log))
	ts := h.newDiskTorrents(6, 2<<20, 0, 2, 5)
	w := h.watchChecks(ts)

	var hashes []string
	for _, dt := range ts {
		hash, err := h.engine.Add(ctx, AddParams{Metainfo: dt.Metainfo})
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	if n := h.states()[StateQueuedForChecking]; n < 4 {
		t.Fatalf("%d torrents queued for checking right after adding 6, want at least 4: %v", n, h.states())
	}
	h.waitAllComplete(ts)
	w.verify(t, 1)
	h.checkContents(ts)

	started, _ := replay(t, log.take(), 1)
	if !slices.Equal(started, hashes) {
		t.Fatalf("checks started in order %v, want the order added %v", started, hashes)
	}
}

// TestCheckQueueAfterLostCompletion loses the piece completion database:
// at the next start every torrent's data is checked, two at a time, and a
// recheck of them all goes through the same queue.
func TestCheckQueueAfterLostCompletion(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, slowHashing(2*time.Millisecond), withCheckLog(&log), withSetting("maxActiveCheckingTorrents", 2))
	ts := h.newDiskTorrents(5, 2<<20, 1, 3)
	// Trusted, so the corrupt pieces count as complete for now.
	for _, dt := range ts {
		if _, err := h.engine.Add(ctx, AddParams{Metainfo: dt.Metainfo, SkipChecking: true}); err != nil {
			t.Fatal(err)
		}
	}
	h.waitAllComplete(ts)
	if ev := log.take(); len(ev) > 0 {
		t.Fatalf("trusted torrents were checked: %v", ev)
	}

	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.dataDir, ".torrent.bolt.db")); err != nil {
		t.Fatal(err)
	}
	h.start()
	w := h.watchChecks(ts)
	if st := h.states(); st[StateQueuedForChecking] < 2 || st[StateChecking] > 2 {
		t.Fatalf("states right after the start: %v, want 2 checking and the rest queued for checking", st)
	}
	h.waitAllComplete(ts)
	w.verify(t, 2)
	h.checkContents(ts)
	if started, most := replay(t, log.take(), 2); len(started) != 5 || most != 2 {
		t.Fatalf("checks after the start: %v, at most %d at once", started, most)
	}

	// A recheck: until its data is checked again, a torrent has nothing.
	w = h.watchChecks(ts)
	if err := h.engine.Recheck(ctx, "all"); err != nil {
		t.Fatal(err)
	}
	for _, st := range h.engine.List() {
		if st.State != StateChecking && st.State != StateQueuedForChecking || st.Complete ||
			(st.State == StateQueuedForChecking && st.Progress != 0) {
			t.Fatalf("right after the recheck: %+v", st)
		}
	}
	h.waitAllComplete(ts)
	w.verify(t, 2)
	events := log.take()
	if started, _ := replay(t, events, 2); len(started) != 5 {
		t.Fatalf("rechecks: %v", started)
	}
	for _, ev := range events {
		if ev.start && !ev.all {
			t.Fatalf("a recheck checked only some pieces: %+v", ev)
		}
	}
}

// TestRecheckStopped checks a stopped torrent, which stays stopped, and a
// stop cancels a recheck.
func TestRecheckStopped(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, slowHashing(time.Millisecond), withCheckLog(&log))
	ts := h.newDiskTorrents(1, 2<<20)
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: ts[0].Metainfo, SkipChecking: true, Stopped: new(true)})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Recheck(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(hash); st.State != StateChecking {
		t.Fatalf("a stopped torrent after a recheck: %+v", st)
	}
	h.waitFor(hash, "checked and stopped", func(st Torrent) bool {
		return st.State == StateStopped && st.Complete && st.Progress == 1
	})
	if started, _ := replay(t, log.take(), 1); len(started) != 1 {
		t.Fatalf("checks: %v", started)
	}

	if err := h.engine.Recheck(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Stop(ctx, hash); err != nil {
		t.Fatal(err)
	}
	st, _ := h.engine.Get(hash)
	if st.State != StateStopped || !st.Complete {
		t.Fatalf("after stopping a recheck: %+v", st)
	}
	h.engine.mu.Lock()
	m, _ := h.engine.get(hash)
	check := m.check
	h.engine.mu.Unlock()
	if check != checkNone {
		t.Fatalf("a stopped recheck is still queued: %v", check)
	}
}

// TestCheckQueueImport imports a torrent with --trust-data and one
// without: only the second is checked when piecemeal starts.
func TestCheckQueueImport(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, withCheckLog(&log))
	ts := h.newDiskTorrents(2, 1<<20)
	if err := h.engine.Close(); err != nil {
		t.Fatal(err)
	}
	for i, trust := range []bool{true, false} {
		res := Import(ctx, h.store, h.dataDir, []Imported{{
			Source: ts[i].Name, Metainfo: ts[i].Metainfo, SavePath: h.saveDir, Complete: true,
			RatioLimit: -2, SeedingTimeLimit: -2, InactiveSeedingTimeLimit: -2,
		}}, ImportOptions{AllowedRoots: []string{h.saveDir}, Trust: trust})
		if res[0].Err != nil {
			t.Fatal(res[0].Err)
		}
	}
	h.start()
	h.waitAllComplete(ts)
	if started, _ := replay(t, log.take(), 1); !slices.Equal(started, []string{ts[1].Hash}) {
		t.Fatalf("checked %v, want only the untrusted %s", started, ts[1].Hash)
	}
}

// TestCheckQueueMagnet adds a magnet whose data is on disk while another
// torrent is being checked: once its metadata arrives it waits its turn.
func TestCheckQueueMagnet(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, slowHashing(5*time.Millisecond), withCheckLog(&log))
	ts := h.newDiskTorrents(2, 4<<20, 1)
	w := h.watchChecks(ts)
	first, err := h.engine.Add(ctx, AddParams{Metainfo: ts[0].Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	// Without a display name: it's named by its hash until the metadata
	// arrives.
	uri := fmt.Sprintf("magnet:?xt=urn:btih:%s&x.pe=%s", ts[1].Hash, url.QueryEscape(ts[1].Addr))
	magnet, err := h.engine.Add(ctx, AddParams{Magnet: uri})
	if err != nil {
		t.Fatal(err)
	}
	h.waitFor(magnet, "the magnet queued for checking", func(st Torrent) bool {
		return st.HasMetadata && st.State == StateQueuedForChecking && st.Name == ts[1].Name
	})
	h.waitAllComplete(ts)
	w.verify(t, 1)
	h.checkContents(ts)
	if started, _ := replay(t, log.take(), 1); !slices.Equal(started, []string{first, magnet}) {
		t.Fatalf("checks: %v", started)
	}
}

// TestCheckLimitChange raises the limit while torrents wait: more start
// at once.
func TestCheckLimitChange(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, slowHashing(5*time.Millisecond), withCheckLog(&log))
	ts := h.newDiskTorrents(4, 2<<20)
	for _, dt := range ts {
		if _, err := h.engine.Add(ctx, AddParams{Metainfo: dt.Metainfo}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.settings.Set(ctx, map[string]any{"maxActiveCheckingTorrents": 3}); err != nil {
		t.Fatal(err)
	}
	if n := h.states()[StateChecking]; n != 3 {
		t.Fatalf("%d torrents checking after raising the limit to 3: %v", n, h.states())
	}
	h.waitAllComplete(ts)
	if started, most := replay(t, log.take(), 3); len(started) != 4 || most != 3 {
		t.Fatalf("checks: %v, at most %d at once", started, most)
	}
}

// TestCheckThenQueue has queueing on: torrents are checked whatever the
// queue's limits, and join the queue once checked.
func TestCheckThenQueue(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, slowHashing(time.Millisecond), withCheckLog(&log),
		withSetting("queueingEnabled", true), withSetting("maxActiveUploads", 1))
	ts := h.newDiskTorrents(3, 1<<20)
	w := h.watchChecks(ts)
	for _, dt := range ts {
		if _, err := h.engine.Add(ctx, AddParams{Metainfo: dt.Metainfo}); err != nil {
			t.Fatal(err)
		}
	}
	waitStates := func(want map[State]int) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !maps.Equal(h.states(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("states %v, want %v", h.states(), want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitStates(map[State]int{StateStalledSeeding: 1, StateQueued: 2})
	w.verify(t, 1)
	if started, _ := replay(t, log.take(), 1); len(started) != 3 {
		t.Fatalf("checks: %v", started)
	}
	for _, st := range h.engine.List() {
		if !st.Complete {
			t.Fatalf("not complete after its check: %+v", st)
		}
	}
}

// TestCheckSurvivesClientRestart changes a setting that recreates the
// anacrolix client while a torrent is checked: the check starts over and
// finishes.
func TestCheckSurvivesClientRestart(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, slowHashing(5*time.Millisecond), withCheckLog(&log))
	ts := h.newDiskTorrents(1, 4<<20, 0)
	w := h.watchChecks(ts)
	hash, err := h.engine.Add(ctx, AddParams{Metainfo: ts[0].Metainfo})
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(hash); st.State != StateChecking {
		t.Fatalf("after adding: %+v", st)
	}
	if _, err := h.settings.Set(ctx, map[string]any{"pex": false}); err != nil {
		t.Fatal(err)
	}
	h.waitAllComplete(ts)
	w.finish()
	h.checkContents(ts)
	var starts, ends int
	for _, ev := range log.take() {
		if ev.start {
			starts++
		} else {
			ends++
		}
	}
	if starts != 2 || ends != 1 {
		t.Fatalf("%d checks started and %d ended, want 2 and 1", starts, ends)
	}
}

// TestStopFreesCheckSlot stops a torrent being checked: the next one starts,
// and the stopped one is checked once started again.
func TestStopFreesCheckSlot(t *testing.T) {
	ctx := context.Background()
	var log checkLog
	h := newHarness(t, slowHashing(5*time.Millisecond), withCheckLog(&log))
	ts := h.newDiskTorrents(2, 4<<20)
	var hashes []string
	for _, dt := range ts {
		hash, err := h.engine.Add(ctx, AddParams{Metainfo: dt.Metainfo})
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	if err := h.engine.Stop(ctx, hashes[0]); err != nil {
		t.Fatal(err)
	}
	first, _ := h.engine.Get(hashes[0])
	second, _ := h.engine.Get(hashes[1])
	if first.State != StateStopped || second.State != StateChecking {
		t.Fatalf("after stopping the first: %s and %s", first.State, second.State)
	}
	if err := h.engine.Start(ctx, hashes[0]); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.engine.Get(hashes[0]); st.State != StateQueuedForChecking {
		t.Fatalf("started again while the other is checked: %s", st.State)
	}
	h.waitAllComplete(ts)
	ev := log.take()
	if len(ev) != 5 || ev[0].hash != hashes[0] || ev[1].hash != hashes[1] || ev[4].hash != hashes[0] {
		t.Fatalf("check events: %+v", ev)
	}
}
