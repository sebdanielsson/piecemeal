package engine

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/tracker"
	"github.com/anacrolix/torrent/types"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

// managed is one torrent: its persisted record and, while it runs, the
// anacrolix torrent. All fields are guarded by Engine.mu.
type managed struct {
	hash metainfo.Hash
	rec  store.Torrent
	tags []string
	info *metainfo.Info // nil until the metadata is known
	t    *torrent.Torrent

	// The anacrolix counters already folded into rec, for t. They restart
	// from zero with every new *torrent.Torrent.
	seenDown, seenUp int64
	// Bytes completed when last attached, for stopped torrents.
	completed int64
	// filePrio holds the per-file priorities, nil for all normal.
	filePrio []Priority
	// renames maps original file paths to renamed ones; see torrentPaths.
	renames map[string]string
	// trackers and webSeeds replace the torrent file's when set.
	trackers [][]string
	webSeeds []string
	moving   bool
	// queued: runnable but held back by the queue limits.
	queued    bool
	slow      bool
	slowSince time.Time
	// boosted holds the piece priorities applyPiecePrioritiesLocked set.
	boosted map[int]types.PiecePriority
	// seqFrom is where the search for the next missing piece starts, for
	// sequential download.
	seqFrom  int
	checking bool
	dirty    bool
	rates    rates
	timesAt  time.Time
	timeAcc  time.Duration
	meta     metaFields
	// Payload this session, and when it last moved, for stall detection.
	sessionDown, sessionUp int64
	lastDownAt, lastUpAt   time.Time
}

// metaFields are read from the torrent file once, for the status.
type metaFields struct {
	comment, createdBy, tracker string
	createdAt                   int64
	trackers                    int
}

func (m *managed) setMagnetTrackers(al [][]string) {
	if len(al) > 0 && len(al[0]) > 0 {
		m.meta.tracker = al[0][0]
	}
	for _, tier := range al {
		m.meta.trackers += len(tier)
	}
}

func (m *managed) setMetainfo(b []byte) {
	m.rec.Metainfo = b
	mi, err := metainfo.Load(bytes.NewReader(b))
	if err != nil {
		return
	}
	m.meta = metaFields{comment: mi.Comment, createdBy: mi.CreatedBy, createdAt: mi.CreationDate}
	al := mi.UpvertedAnnounceList()
	if len(al) > 0 && len(al[0]) > 0 {
		m.meta.tracker = al[0][0]
	}
	for _, tier := range al {
		m.meta.trackers += len(tier)
	}
	if m.info == nil {
		if info, err := mi.UnmarshalInfo(); err == nil {
			m.info = &info
		}
	}
}

// jsonOrEmpty encodes v, or returns "" for a nil or empty value.
func jsonOrEmpty[T any](v T) string {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Len() == 0 {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// loadOverrides decodes the record's JSON columns into the managed fields.
func (m *managed) loadOverrides() error {
	var errs []error
	if m.rec.Trackers != "" {
		errs = append(errs, json.Unmarshal([]byte(m.rec.Trackers), &m.trackers))
	}
	if m.rec.WebSeeds != "" {
		errs = append(errs, json.Unmarshal([]byte(m.rec.WebSeeds), &m.webSeeds))
	}
	if m.rec.FilePaths != "" {
		errs = append(errs, json.Unmarshal([]byte(m.rec.FilePaths), &m.renames))
	}
	return errors.Join(errs...)
}

func (m *managed) addCounters(down, up int64) {
	if d := down - m.seenDown; d > 0 {
		m.rec.Downloaded += d
		m.dirty = true
	}
	if u := up - m.seenUp; u > 0 {
		m.rec.Uploaded += u
		m.dirty = true
	}
	m.seenDown, m.seenUp = down, up
}

func (m *managed) setError(err error) {
	m.rec.Error = err.Error()
	m.dirty = true
}

func (m *managed) params() store.UpsertTorrentParams {
	r := m.rec
	prio := ""
	if m.filePrio != nil {
		b, _ := json.Marshal(m.filePrio)
		prio = string(b)
	}
	return store.UpsertTorrentParams{
		InfoHash: r.InfoHash, Name: r.Name, Magnet: r.Magnet, Metainfo: r.Metainfo, SavePath: r.SavePath,
		Category: r.Category, AddedAt: r.AddedAt, CompletedAt: r.CompletedAt, Stopped: r.Stopped,
		Forced: r.Forced, Sequential: r.Sequential, FirstLastPiece: r.FirstLastPiece,
		QueuePosition: r.QueuePosition, Uploaded: r.Uploaded, Downloaded: r.Downloaded,
		SeedingSeconds: r.SeedingSeconds, ActiveSeconds: r.ActiveSeconds, LastActivityAt: r.LastActivityAt,
		RatioLimit: r.RatioLimit, SeedingTimeLimit: r.SeedingTimeLimit,
		InactiveSeedingTimeLimit: r.InactiveSeedingTimeLimit, DownloadLimit: r.DownloadLimit,
		UploadLimit: r.UploadLimit, FilePriorities: prio, Error: r.Error,
		Trackers: jsonOrEmpty(m.trackers), WebSeeds: jsonOrEmpty(m.webSeeds), FilePaths: jsonOrEmpty(m.renames),
		ContentLayout: cmp.Or(r.ContentLayout, LayoutOriginal),
	}
}

// Priority is a file download priority, with qBittorrent's values.
type Priority int

// File priorities.
const (
	PrioritySkip    Priority = 0
	PriorityNormal  Priority = 1
	PriorityHigh    Priority = 6
	PriorityHighest Priority = 7
)

func (p Priority) piecePriority() types.PiecePriority {
	switch {
	case p <= PrioritySkip:
		return types.PiecePriorityNone
	case p >= PriorityHighest:
		return types.PiecePriorityNext
	case p >= PriorityHigh:
		return types.PiecePriorityHigh
	default:
		return types.PiecePriorityNormal
	}
}

// AddParams describes a torrent to add. Exactly one of Magnet and Metainfo
// is set.
type AddParams struct {
	Magnet   string
	Metainfo []byte
	// SavePath overrides the category's and the default save path.
	SavePath string
	Category string
	Tags     []string
	// Stopped adds the torrent without starting it; nil follows the
	// addStopped setting.
	Stopped        *bool
	Sequential     bool
	FirstLastPiece bool
	// Name replaces the torrent's display name.
	Name string
	// Seed goals and limits; nil leaves the defaults.
	RatioLimit               *float64
	SeedingTimeLimit         *int64
	InactiveSeedingTimeLimit *int64
	DownloadLimit            int64
	UploadLimit              int64
	// SkipChecking trusts existing data instead of hashing it.
	SkipChecking bool
	// ContentLayout is Original (the default), Subfolder or NoSubfolder.
	ContentLayout string
}

// ParseSource turns the body of an add request, a magnet URI or a .torrent
// file, into AddParams' Magnet or Metainfo.
func ParseSource(b []byte) (magnet string, mi []byte, err error) {
	if s := strings.TrimSpace(string(b)); strings.HasPrefix(s, "magnet:") {
		return s, nil, nil
	}
	if _, err := metainfo.Load(bytes.NewReader(b)); err != nil {
		return "", nil, fmt.Errorf("%w: neither a magnet URI nor a torrent file", ErrInvalid)
	}
	return "", b, nil
}

// Add adds a torrent and returns its infohash. Adding a torrent that is
// already present returns ErrExists with its hash.
func (e *Engine) Add(ctx context.Context, p AddParams) (string, error) {
	if e.closed.Load() {
		return "", ErrClosed
	}
	var (
		ih     metainfo.Hash
		name   string
		magnet string
		mib    []byte
		info   *metainfo.Info
		spec   *torrent.TorrentSpec
	)
	switch {
	case p.Magnet != "" && p.Metainfo == nil:
		var err error
		spec, err = torrent.TorrentSpecFromMagnetUri(p.Magnet)
		if err != nil {
			return "", fmt.Errorf("%w: magnet URI: %w", ErrInvalid, err)
		}
		ih, name, magnet = spec.InfoHash, spec.DisplayName, p.Magnet
	case p.Metainfo != nil && p.Magnet == "":
		mi, err := metainfo.Load(bytes.NewReader(p.Metainfo))
		if err != nil {
			return "", fmt.Errorf("%w: torrent file: %w", ErrInvalid, err)
		}
		i, err := mi.UnmarshalInfo()
		if err != nil {
			return "", fmt.Errorf("%w: torrent info: %w", ErrInvalid, err)
		}
		info = &i
		ih, name, mib = mi.HashInfoBytes(), i.BestName(), p.Metainfo
	default:
		return "", fmt.Errorf("%w: give either a magnet URI or a torrent file", ErrInvalid)
	}
	if p.Name != "" {
		name = p.Name
	}
	if name == "" {
		name = ih.HexString()
	}

	s := e.settings.Get()
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.torrents[ih]; ok {
		return ih.HexString(), ErrExists
	}
	if p.Category != "" {
		if _, ok := e.cats[p.Category]; !ok {
			return "", fmt.Errorf("%w: category %q does not exist", ErrInvalid, p.Category)
		}
	}
	savePath := p.SavePath
	if savePath == "" && p.Category != "" {
		savePath = e.cats[p.Category].SavePath
	}
	if savePath == "" {
		savePath = s.DefaultSavePath
	}
	savePath, err := e.confine(savePath)
	if err != nil {
		return "", err
	}
	for _, tag := range p.Tags {
		if err := validName("tag", tag); err != nil {
			return "", err
		}
	}
	tags := slices.Clone(p.Tags)
	slices.Sort(tags)
	tags = slices.Compact(tags)

	if !validLayout(p.ContentLayout) {
		return "", fmt.Errorf("%w: content layout %q", ErrInvalid, p.ContentLayout)
	}
	stopped := s.AddStopped
	if p.Stopped != nil {
		stopped = *p.Stopped
	}
	now := time.Now().Unix()
	m := &managed{
		hash: ih,
		info: info,
		tags: tags,
		rec: store.Torrent{
			InfoHash: ih.HexString(), Name: name, Magnet: magnet, SavePath: savePath,
			Category: p.Category, AddedAt: now, Stopped: stopped, Sequential: p.Sequential,
			FirstLastPiece: p.FirstLastPiece, RatioLimit: -2, SeedingTimeLimit: -2,
			InactiveSeedingTimeLimit: -2, DownloadLimit: p.DownloadLimit, UploadLimit: p.UploadLimit,
			ContentLayout: cmp.Or(p.ContentLayout, LayoutOriginal),
		},
	}
	if mib != nil {
		m.setMetainfo(mib)
	} else if spec != nil {
		m.setMagnetTrackers(spec.Trackers)
	}
	if p.RatioLimit != nil {
		m.rec.RatioLimit = *p.RatioLimit
	}
	if p.SeedingTimeLimit != nil {
		m.rec.SeedingTimeLimit = *p.SeedingTimeLimit
	}
	if p.InactiveSeedingTimeLimit != nil {
		m.rec.InactiveSeedingTimeLimit = *p.InactiveSeedingTimeLimit
	}

	err = e.store.Tx(ctx, func(q *store.Queries) error {
		for _, tag := range tags {
			if err := q.CreateTag(ctx, tag); err != nil {
				return err
			}
		}
		if err := q.UpsertTorrent(ctx, m.params()); err != nil {
			return err
		}
		for _, tag := range tags {
			if err := q.AddTorrentTag(ctx, store.AddTorrentTagParams{InfoHash: m.rec.InfoHash, Tag: tag}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("save torrent: %w", err)
	}
	newTags := false
	for _, tag := range tags {
		if _, ok := e.tags[tag]; !ok {
			e.tags[tag] = struct{}{}
			newTags = true
		}
	}
	e.torrents[ih] = m
	if p.SkipChecking && m.info != nil {
		e.markCompleteLocked(m)
	}
	e.normalizeQueueLocked()
	if !stopped && !s.QueueingEnabled {
		if err := e.attachLocked(m, attachOpts{spec: spec, skipChecking: p.SkipChecking}); err != nil {
			m.setError(err)
		}
	}
	e.scheduleLocked(time.Now())
	e.refreshLocked(m)
	e.log.Info("torrent added", "hash", m.rec.InfoHash, "name", name, "savePath", savePath, "stopped", stopped)
	if newTags {
		e.bus.publish(Event{Type: EventTags})
	}
	return ih.HexString(), nil
}

type attachOpts struct {
	spec         *torrent.TorrentSpec
	skipChecking bool
}

// attachLocked adds a stopped or newly loaded torrent to the client.
func (e *Engine) attachLocked(m *managed, opts ...attachOpts) error {
	if m.moving {
		return fmt.Errorf("%w: the torrent is being moved", ErrInvalid)
	}
	var o attachOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	spec := o.spec
	if m.rec.Metainfo != nil {
		mi, err := metainfo.Load(bytes.NewReader(m.rec.Metainfo))
		if err != nil {
			return fmt.Errorf("stored torrent file: %w", err)
		}
		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
		if err != nil {
			return fmt.Errorf("stored torrent file: %w", err)
		}
	} else if spec == nil {
		var err error
		spec, err = torrent.TorrentSpecFromMagnetUri(m.rec.Magnet)
		if err != nil {
			return fmt.Errorf("stored magnet URI: %w", err)
		}
	}
	spec.DisplayName = m.rec.Name
	spec.Storage = e.storage
	spec.DisableInitialPieceCheck = o.skipChecking
	if m.trackers != nil {
		spec.Trackers = m.trackers
	}
	if m.webSeeds != nil {
		spec.Webseeds = m.webSeeds
	}
	e.recordPathsLocked(m)

	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return err
	}
	m.t = t
	m.seenDown, m.seenUp = 0, 0
	m.boosted, m.seqFrom = nil, 0
	m.rec.Error = ""
	t.SetMaxEstablishedConns(e.settings.Get().MaxConnectionsPerTorrent)
	if t.Info() != nil {
		e.onInfoLocked(m)
	} else {
		go e.awaitInfo(m, t)
	}
	return nil
}

// awaitInfo waits for a magnet's metadata, then stores it so a restart
// doesn't have to fetch it again, and starts the download.
func (e *Engine) awaitInfo(m *managed, t *torrent.Torrent) {
	select {
	case <-t.GotInfo():
	case <-t.Closed():
		return
	}
	var buf bytes.Buffer
	mi := t.Metainfo()
	// anacrolix fills these in for metainfo it built itself; they describe
	// nothing about the torrent.
	mi.Comment, mi.CreatedBy, mi.CreationDate = "", "", 0
	if err := mi.Write(&buf); err != nil {
		e.log.Error("encoding metainfo", "hash", m.hash.HexString(), "error", err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if m.t != t {
		return
	}
	m.setMetainfo(buf.Bytes())
	m.dirty = true
	e.onInfoLocked(m)
	if err := e.store.UpsertTorrent(e.ctxOrBackground(), m.params()); err != nil {
		e.log.Error("saving metainfo", "hash", m.hash.HexString(), "error", err)
	}
	e.log.Info("metadata received", "hash", m.rec.InfoHash, "name", t.Name())
}

func (e *Engine) ctxOrBackground() context.Context {
	if e.ctx != nil {
		return e.ctx
	}
	return context.Background()
}

// onInfoLocked runs once a running torrent's info is known: it applies file
// priorities and starts downloading.
func (e *Engine) onInfoLocked(m *managed) {
	t := m.t
	info := t.Info()
	m.info = info
	// Until the metadata arrives a magnet's torrent is named by its dn
	// parameter or hash. Then the real name takes over, unless it was
	// renamed.
	placeholder := m.rec.Name == "" || m.rec.Name == m.rec.InfoHash
	if spec, err := torrent.TorrentSpecFromMagnetUri(m.rec.Magnet); err == nil && m.rec.Name == spec.DisplayName {
		placeholder = true
	}
	if placeholder && m.rec.Name != info.BestName() {
		m.rec.Name = info.BestName()
		t.SetDisplayName(m.rec.Name)
		m.dirty = true
	}
	files := t.Files()
	if m.filePrio != nil && len(m.filePrio) != len(files) {
		m.filePrio = nil
	}
	for i, f := range files {
		p := PriorityNormal
		if m.filePrio != nil {
			p = m.filePrio[i]
		}
		f.SetPriority(p.piecePriority())
	}
	e.applyPiecePrioritiesLocked(m)
}

// applyPiecePrioritiesLocked sets the piece-level priorities piecemeal
// manages on top of file priorities: the first and last piece of each
// wanted file with firstLastPiece, and a window ahead of the first missing
// piece with sequential. anacrolix uses the higher of a piece's own
// priority and its files', so pieces that no longer need a boost are reset
// to none.
func (e *Engine) applyPiecePrioritiesLocked(m *managed) {
	if m.t == nil || m.info == nil || m.t.Info() == nil {
		return
	}
	want := map[int]types.PiecePriority{}
	raise := func(i int, p types.PiecePriority) {
		if p > want[i] {
			want[i] = p
		}
	}
	files := m.t.Files()
	if m.rec.FirstLastPiece {
		for i, f := range files {
			if f.Length() == 0 || !m.fileWanted(i) {
				continue
			}
			raise(f.BeginPieceIndex(), types.PiecePriorityHigh)
			raise(f.EndPieceIndex()-1, types.PiecePriorityHigh)
		}
	}
	if m.rec.Sequential {
		e.sequentialWindowLocked(m, raise)
	}
	for i := range m.boosted {
		if _, ok := want[i]; !ok {
			m.t.Piece(i).SetPriority(types.PiecePriorityNone)
		}
	}
	for i, p := range want {
		if m.boosted[i] != p {
			m.t.Piece(i).SetPriority(p)
		}
	}
	m.boosted = want
}

// fileWanted reports whether file i isn't skipped.
func (m *managed) fileWanted(i int) bool {
	return m.filePrio == nil || i >= len(m.filePrio) || m.filePrio[i] != PrioritySkip
}

// pieceWanted reports whether piece i belongs to a file that isn't skipped.
func (m *managed) pieceWanted(i int) bool {
	if m.filePrio == nil {
		return true
	}
	var offset int64
	start, end := int64(i)*m.info.PieceLength, int64(i+1)*m.info.PieceLength
	for fi, f := range m.info.UpvertedFiles() {
		if offset < end && offset+f.Length > start && m.fileWanted(fi) {
			return true
		}
		offset += f.Length
	}
	return false
}

// sequentialWindowLocked raises the next missing wanted pieces in order:
// the first one most, then a window after it, so pieces arrive roughly
// front to back.
func (e *Engine) sequentialWindowLocked(m *managed, raise func(int, types.PiecePriority)) {
	n := m.info.NumPieces()
	window := int(min(max((32<<20)/max(m.info.PieceLength, 1), 4), 64))
	i := m.seqFrom
	for i < n && (m.t.PieceState(i).Complete || !m.pieceWanted(i)) {
		i++
	}
	m.seqFrom = i
	for k := 0; i < n && k < window; i++ {
		if m.t.PieceState(i).Complete || !m.pieceWanted(i) {
			continue
		}
		if k == 0 {
			raise(i, types.PiecePriorityNext)
		} else {
			raise(i, types.PiecePriorityReadahead)
		}
		k++
	}
}

// detachLocked drops a torrent from the client, keeping its record.
func (e *Engine) detachLocked(m *managed) {
	if m.t == nil {
		return
	}
	st := m.t.Stats()
	m.addCounters(st.BytesReadUsefulData.Int64(), st.BytesWrittenData.Int64())
	if m.info != nil {
		m.completed = m.t.BytesCompleted()
	}
	t := m.t
	m.t = nil
	t.Drop()
}

func (e *Engine) get(hash string) (*managed, error) {
	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		return nil, fmt.Errorf("%w: torrent %q", ErrNotFound, hash)
	}
	m, ok := e.torrents[ih]
	if !ok {
		return nil, fmt.Errorf("%w: torrent %s", ErrNotFound, hash)
	}
	return m, nil
}

// each runs fn on each torrent in hashes under the engine lock, saves the
// changed records and refreshes their status. A hash of "all" means every
// torrent.
func (e *Engine) each(ctx context.Context, hashes []string, fn func(m *managed) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var targets []*managed
	if len(hashes) == 1 && hashes[0] == "all" {
		for _, m := range e.torrents {
			targets = append(targets, m)
		}
	} else {
		for _, h := range hashes {
			m, err := e.get(h)
			if err != nil {
				return err
			}
			targets = append(targets, m)
		}
	}
	var errs []error
	for _, m := range targets {
		if err := fn(m); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.rec.InfoHash, err))
		}
	}
	err := e.store.Tx(ctx, func(q *store.Queries) error {
		for _, m := range targets {
			if err := q.UpsertTorrent(ctx, m.params()); err != nil {
				return err
			}
			m.dirty = false
		}
		return nil
	})
	if err != nil {
		errs = append(errs, fmt.Errorf("save torrents: %w", err))
	}
	e.refreshLocked(targets...)
	return errors.Join(errs...)
}

// Start resumes stopped torrents.
func (e *Engine) Start(ctx context.Context, hashes ...string) error {
	queueing := e.settings.Get().QueueingEnabled
	err := e.each(ctx, hashes, func(m *managed) error {
		m.rec.Stopped = false
		if m.t != nil || queueing {
			return nil
		}
		return e.attachLocked(m)
	})
	if queueing {
		e.mu.Lock()
		e.scheduleLocked(time.Now())
		e.refreshLocked(e.allLocked()...)
		e.mu.Unlock()
	}
	return err
}

func (e *Engine) allLocked() []*managed {
	out := make([]*managed, 0, len(e.torrents))
	for _, m := range e.torrents {
		out = append(out, m)
	}
	return out
}

// Stop stops torrents: they leave the swarm, and keep their data and record.
func (e *Engine) Stop(ctx context.Context, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.Stopped = true
		m.rec.Forced = false
		m.queued = false
		e.detachLocked(m)
		return nil
	})
}

// SetForced sets force start: forced torrents ignore queueing limits.
func (e *Engine) SetForced(ctx context.Context, forced bool, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.Forced = forced
		if forced && m.t == nil {
			m.rec.Stopped = false
			return e.attachLocked(m)
		}
		return nil
	})
}

// Recheck verifies a torrent's data against its piece hashes.
func (e *Engine) Recheck(ctx context.Context, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		if m.t == nil || m.info == nil {
			return nil
		}
		t := m.t
		m.checking = true
		go func() {
			err := t.VerifyDataContext(e.ctxOrBackground())
			e.mu.Lock()
			defer e.mu.Unlock()
			m.checking = false
			if e.torrents[m.hash] != m {
				return // removed meanwhile
			}
			if err != nil && m.t == t {
				e.log.Warn("recheck", "hash", m.rec.InfoHash, "error", err)
			}
			m.seqFrom = 0
			e.refreshLocked(m)
		}()
		return nil
	})
}

// Reannounce asks the trackers and the DHT for peers now, in the
// background. anacrolix keeps announcing on its own schedule; it has no way
// to bring that forward, so these are extra announces whose peers are
// handed to the torrent.
func (e *Engine) Reannounce(ctx context.Context, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		if m.t == nil {
			return nil
		}
		t := m.t
		mi := t.Metainfo()
		var trackers []string
		for _, tier := range mi.UpvertedAnnounceList() {
			trackers = append(trackers, tier...)
		}
		left := int64(-1)
		if m.info != nil {
			size, done := m.wanted()
			left = max(size-done, 0)
		}
		req := tracker.AnnounceRequest{
			InfoHash: m.hash, PeerId: e.client.PeerID(), Port: uint16(e.port), //nolint:gosec // ports fit in uint16
			Left: left, Uploaded: m.rec.Uploaded, Downloaded: m.rec.Downloaded, NumWant: 200,
		}
		dhts := e.client.DhtServers()
		e.wg.Go(func() { e.announce(t, trackers, req, dhts) })
		return nil
	})
}

func (e *Engine) announce(t *torrent.Torrent, trackers []string, req tracker.AnnounceRequest, dhts []torrent.DhtServer) {
	ctx, cancel := context.WithTimeout(e.ctxOrBackground(), 30*time.Second)
	defer cancel()
	for _, s := range dhts {
		if _, stop, err := t.AnnounceToDht(s); err == nil {
			context.AfterFunc(ctx, stop)
		}
	}
	for _, u := range trackers {
		if !strings.HasPrefix(u, "http") && !strings.HasPrefix(u, "udp") {
			continue
		}
		res, err := tracker.Announce{TrackerUrl: u, Request: req, Context: ctx, UserAgent: "piecemeal"}.Do()
		if err != nil {
			e.log.Debug("reannounce", "tracker", u, "error", err)
			continue
		}
		peers := make([]torrent.PeerInfo, 0, len(res.Peers))
		for _, p := range res.Peers {
			if ap, ok := p.ToNetipAddrPort(); ok {
				peers = append(peers, torrent.PeerInfo{Addr: net.TCPAddrFromAddrPort(ap), Source: torrent.PeerSourceTracker})
			}
		}
		t.AddPeers(peers)
	}
	<-ctx.Done()
}

// SetSequential turns sequential download on or off.
func (e *Engine) SetSequential(ctx context.Context, on bool, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.Sequential = on
		m.seqFrom = 0
		e.applyPiecePrioritiesLocked(m)
		return nil
	})
}

// SetFirstLastPiece turns first and last piece priority on or off.
func (e *Engine) SetFirstLastPiece(ctx context.Context, on bool, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.FirstLastPiece = on
		e.applyPiecePrioritiesLocked(m)
		return nil
	})
}

// ShareLimits are per-torrent seed goals. -2 follows the global setting,
// -1 is unlimited. Times are in minutes.
type ShareLimits struct {
	RatioLimit               float64
	SeedingTimeLimit         int64
	InactiveSeedingTimeLimit int64
}

// SetShareLimits sets per-torrent seed goals.
func (e *Engine) SetShareLimits(ctx context.Context, l ShareLimits, hashes ...string) error {
	if (l.RatioLimit < 0 && l.RatioLimit != -1 && l.RatioLimit != -2) ||
		l.SeedingTimeLimit < -2 || l.InactiveSeedingTimeLimit < -2 {
		return fmt.Errorf("%w: share limits must be -2, -1 or at least 0", ErrInvalid)
	}
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.RatioLimit = l.RatioLimit
		m.rec.SeedingTimeLimit = l.SeedingTimeLimit
		m.rec.InactiveSeedingTimeLimit = l.InactiveSeedingTimeLimit
		return nil
	})
}

// SetLimits sets per-torrent rate limits in bytes per second, 0 for none.
// They are stored and reported; anacrolix does not enforce per-torrent
// limits yet.
func (e *Engine) SetLimits(ctx context.Context, down, up int64, hashes ...string) error {
	if down < 0 || up < 0 {
		return fmt.Errorf("%w: limits must not be negative", ErrInvalid)
	}
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.DownloadLimit, m.rec.UploadLimit = down, up
		return nil
	})
}

// Rename changes a torrent's display name.
func (e *Engine) Rename(ctx context.Context, hash, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalid)
	}
	return e.each(ctx, []string{hash}, func(m *managed) error {
		m.rec.Name = name
		if m.t != nil {
			m.t.SetDisplayName(name)
		}
		return nil
	})
}

// SetFilePriority sets the priority of the files at indices.
func (e *Engine) SetFilePriority(ctx context.Context, hash string, indices []int, p Priority) error {
	if p != PrioritySkip && p != PriorityNormal && p != PriorityHigh && p != PriorityHighest {
		return fmt.Errorf("%w: priority %d", ErrInvalid, p)
	}
	return e.each(ctx, []string{hash}, func(m *managed) error {
		if m.info == nil {
			return fmt.Errorf("%w: metadata not received yet", ErrInvalid)
		}
		n := len(m.info.UpvertedFiles())
		for _, i := range indices {
			if i < 0 || i >= n {
				return fmt.Errorf("%w: file index %d", ErrInvalid, i)
			}
		}
		if m.filePrio == nil {
			m.filePrio = make([]Priority, n)
			for i := range m.filePrio {
				m.filePrio[i] = PriorityNormal
			}
		}
		for _, i := range indices {
			m.filePrio[i] = p
		}
		if m.t != nil {
			files := m.t.Files()
			for _, i := range indices {
				files[i].SetPriority(p.piecePriority())
			}
			e.applyPiecePrioritiesLocked(m)
		}
		return nil
	})
}

// Remove removes torrents, and their downloaded data with deleteData.
func (e *Engine) Remove(ctx context.Context, deleteData bool, hashes ...string) error {
	e.mu.Lock()
	var targets []*managed
	if len(hashes) == 1 && hashes[0] == "all" {
		for _, m := range e.torrents {
			targets = append(targets, m)
		}
	} else {
		for _, h := range hashes {
			m, err := e.get(h)
			if err != nil {
				e.mu.Unlock()
				return err
			}
			targets = append(targets, m)
		}
	}
	type removal struct {
		hash  metainfo.Hash
		paths []string
		root  string
		n     int
	}
	var removals []removal
	for _, m := range targets {
		if m.moving {
			e.mu.Unlock()
			return fmt.Errorf("%w: %s is being moved", ErrInvalid, m.rec.InfoHash)
		}
	}
	for _, m := range targets {
		e.detachLocked(m)
		delete(e.torrents, m.hash)
		r := removal{hash: m.hash}
		if deleteData && m.info != nil {
			r.root = m.rec.SavePath
			r.paths = contentFiles(m.rec.SavePath, m.pathsOf().relativePaths(m.info))
			r.n = m.info.NumPieces()
		}
		removals = append(removals, r)
	}
	e.mu.Unlock()

	var errs []error
	err := e.store.Tx(ctx, func(q *store.Queries) error {
		for _, r := range removals {
			if err := q.DeleteTorrent(ctx, r.hash.HexString()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		errs = append(errs, fmt.Errorf("delete torrents: %w", err))
	}
	for _, r := range removals {
		e.paths.Delete(r.hash)
		if r.root != "" {
			if err := removeContent(r.root, r.paths); err != nil {
				errs = append(errs, err)
			}
			// Forget completion so a re-add verifies instead of trusting
			// pieces that are gone.
			for i := range r.n {
				_ = e.pc.Set(metainfo.PieceKey{InfoHash: r.hash, Index: i}, g.None[bool]())
			}
		}
		e.removeFromSnapshot(r.hash.HexString())
		e.log.Info("torrent removed", "hash", r.hash.HexString(), "deleteData", deleteData)
	}
	return errors.Join(errs...)
}

// contentFiles lists the absolute paths of a torrent's files, from their
// paths relative to the save path, including the .part names anacrolix
// uses for incomplete files when part files are on.
func contentFiles(savePath string, rel []string) []string {
	var paths []string
	for _, r := range rel {
		p := filepath.Join(savePath, filepath.FromSlash(r))
		paths = append(paths, p, p+".part")
	}
	return paths
}

// removeContent deletes paths, all inside root, then the directories they
// leave empty up to root.
func removeContent(root string, paths []string) error {
	var errs []error
	dirs := map[string]struct{}{}
	for _, p := range paths {
		if !within(root, p) {
			errs = append(errs, fmt.Errorf("%w: %s", ErrPathForbidden, p))
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		for d := filepath.Dir(p); d != root && within(root, d); d = filepath.Dir(d) {
			dirs[d] = struct{}{}
		}
	}
	// Deepest first, so parents are empty by the time they're tried.
	sorted := make([]string, 0, len(dirs))
	for d := range dirs {
		sorted = append(sorted, d)
	}
	slices.SortFunc(sorted, func(a, b string) int { return len(b) - len(a) })
	for _, d := range sorted {
		if empty, _ := isEmptyDir(d); empty {
			_ = os.Remove(d)
		}
	}
	return errors.Join(errs...)
}

func isEmptyDir(d string) (bool, error) {
	f, err := os.Open(d) //nolint:gosec // d is a directory of a torrent's content, inside its save path
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

// loadTorrents re-adds every stored torrent at startup.
func (e *Engine) loadTorrents(ctx context.Context) error {
	rows, err := e.store.ListTorrents(ctx)
	if err != nil {
		return fmt.Errorf("load torrents: %w", err)
	}
	tagRows, err := e.store.ListTorrentTags(ctx)
	if err != nil {
		return fmt.Errorf("load torrent tags: %w", err)
	}
	tagsByHash := map[string][]string{}
	for _, r := range tagRows {
		tagsByHash[r.InfoHash] = append(tagsByHash[r.InfoHash], r.Tag)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range rows {
		var ih metainfo.Hash
		if err := ih.FromHexString(r.InfoHash); err != nil {
			e.log.Error("skipping stored torrent with a bad infohash", "hash", r.InfoHash)
			continue
		}
		m := &managed{hash: ih, rec: r, tags: tagsByHash[r.InfoHash]}
		if r.FilePriorities != "" {
			if err := json.Unmarshal([]byte(r.FilePriorities), &m.filePrio); err != nil {
				e.log.Warn("ignoring stored file priorities", "hash", r.InfoHash, "error", err)
			}
		}
		if r.Metainfo != nil {
			m.setMetainfo(r.Metainfo)
		} else if spec, err := torrent.TorrentSpecFromMagnetUri(r.Magnet); err == nil {
			m.setMagnetTrackers(spec.Trackers)
		}
		if err := m.loadOverrides(); err != nil {
			e.log.Warn("ignoring a stored tracker, web seed or file path override", "hash", r.InfoHash, "error", err)
		}
		if m.trackers != nil {
			m.meta.tracker, m.meta.trackers = "", 0
			m.setMagnetTrackers(m.trackers)
		}
		e.recordPathsLocked(m)
		e.torrents[ih] = m
		if m.info != nil {
			m.completed = e.completedFromStore(m)
		}
		if !r.Stopped && !e.settings.Get().QueueingEnabled {
			if err := e.attachLocked(m); err != nil {
				e.log.Error("starting torrent", "hash", r.InfoHash, "error", err)
				m.setError(err)
			}
		}
	}
	e.log.Info("torrents loaded", "count", len(rows))
	return nil
}

// completedFromStore sums the pieces the completion database marks
// complete, for a torrent that isn't running.
func (e *Engine) completedFromStore(m *managed) int64 {
	var n int64
	for i := range m.info.NumPieces() {
		c, err := e.pc.Get(metainfo.PieceKey{InfoHash: m.hash, Index: i})
		if err == nil && c.Ok && c.Complete {
			n += m.info.Piece(i).Length()
		}
	}
	return n
}

// AddPeers gives running torrents peer addresses (host:port) to connect to.
func (e *Engine) AddPeers(ctx context.Context, addrs []string, hashes ...string) error {
	peers := make([]torrent.PeerInfo, 0, len(addrs))
	for _, a := range addrs {
		addr, err := netip.ParseAddrPort(a)
		if err != nil {
			return fmt.Errorf("%w: peer %q: %w", ErrInvalid, a, err)
		}
		peers = append(peers, torrent.PeerInfo{Addr: net.TCPAddrFromAddrPort(addr), Trusted: true})
	}
	return e.each(ctx, hashes, func(m *managed) error {
		if m.t != nil {
			m.t.AddPeers(peers)
		}
		return nil
	})
}

// saveLocked writes a torrent's record now.
func (e *Engine) saveLocked(m *managed) {
	if err := e.store.UpsertTorrent(e.ctxOrBackground(), m.params()); err != nil {
		e.log.Error("saving torrent", "hash", m.rec.InfoHash, "error", err)
		return
	}
	m.dirty = false
}

// markCompleteLocked records every piece as complete without checking the
// data: skip checking (qBittorrent's seed mode) trusts what's on disk, and
// the torrent creator has just hashed it. anacrolix's own option only skips
// the check, leaving the pieces unknown and so missing.
func (e *Engine) markCompleteLocked(m *managed) {
	for i := range m.info.NumPieces() {
		if err := e.pc.Set(metainfo.PieceKey{InfoHash: m.hash, Index: i}, g.Some(true)); err != nil {
			e.log.Warn("marking pieces complete", "hash", m.rec.InfoHash, "error", err)
			return
		}
	}
}
