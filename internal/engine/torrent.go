package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
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
	checking bool
	dirty    bool
	rates    rates
	timesAt  time.Time
	timeAcc  time.Duration
	meta     metaFields
}

// metaFields are read from the torrent file once, for the status.
type metaFields struct {
	comment, createdBy, tracker string
	createdAt                   int64
}

func (m *managed) setMetainfo(b []byte) {
	m.rec.Metainfo = b
	mi, err := metainfo.Load(bytes.NewReader(b))
	if err != nil {
		return
	}
	m.meta = metaFields{comment: mi.Comment, createdBy: mi.CreatedBy, createdAt: mi.CreationDate}
	if al := mi.UpvertedAnnounceList(); len(al) > 0 && len(al[0]) > 0 {
		m.meta.tracker = al[0][0]
	}
	if m.info == nil {
		if info, err := mi.UnmarshalInfo(); err == nil {
			m.info = &info
		}
	}
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
	tags := slices.Clone(p.Tags)
	slices.Sort(tags)
	tags = slices.Compact(tags)

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
		},
	}
	if mib != nil {
		m.setMetainfo(mib)
	} else if spec != nil && len(spec.Trackers) > 0 && len(spec.Trackers[0]) > 0 {
		m.meta.tracker = spec.Trackers[0][0]
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
	if !stopped {
		if err := e.attachLocked(m, attachOpts{spec: spec, skipChecking: p.SkipChecking}); err != nil {
			m.setError(err)
		}
	}
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
	e.savePaths.Store(m.hash, m.rec.SavePath)

	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return err
	}
	m.t = t
	m.seenDown, m.seenUp = 0, 0
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

// applyPiecePrioritiesLocked raises the first and last piece of every wanted
// file when firstLastPiece is on.
func (e *Engine) applyPiecePrioritiesLocked(m *managed) {
	if m.t == nil || m.info == nil || !m.rec.FirstLastPiece {
		return
	}
	for _, f := range m.t.Files() {
		if f.Priority() == types.PiecePriorityNone || f.Length() == 0 {
			continue
		}
		for _, i := range []int{f.BeginPieceIndex(), f.EndPieceIndex() - 1} {
			p := m.t.Piece(i)
			if p.State().Priority < types.PiecePriorityHigh {
				p.SetPriority(types.PiecePriorityHigh)
			}
		}
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
	for _, m := range targets {
		e.refreshLocked(m)
	}
	return errors.Join(errs...)
}

// Start resumes stopped torrents.
func (e *Engine) Start(ctx context.Context, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.Stopped = false
		if m.t != nil {
			return nil
		}
		return e.attachLocked(m)
	})
}

// Stop stops torrents: they leave the swarm, and keep their data and record.
func (e *Engine) Stop(ctx context.Context, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.Stopped = true
		m.rec.Forced = false
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
			if err != nil && m.t == t {
				e.log.Warn("recheck", "hash", m.rec.InfoHash, "error", err)
			}
			e.refreshLocked(m)
		}()
		return nil
	})
}

// Reannounce asks for more peers now. anacrolix announces on its own
// schedule; adding the known trackers again triggers an announce to them.
func (e *Engine) Reannounce(ctx context.Context, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		if m.t == nil {
			return nil
		}
		mi := m.t.Metainfo()
		m.t.AddTrackers(mi.UpvertedAnnounceList())
		return nil
	})
}

// SetSequential turns sequential download on or off.
func (e *Engine) SetSequential(ctx context.Context, on bool, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.Sequential = on
		return nil
	})
}

// SetFirstLastPiece turns first and last piece priority on or off.
func (e *Engine) SetFirstLastPiece(ctx context.Context, on bool, hashes ...string) error {
	return e.each(ctx, hashes, func(m *managed) error {
		m.rec.FirstLastPiece = on
		if m.t != nil && m.info != nil {
			if on {
				e.applyPiecePrioritiesLocked(m)
			} else {
				e.onInfoLocked(m)
			}
		}
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
		e.detachLocked(m)
		delete(e.torrents, m.hash)
		r := removal{hash: m.hash}
		if deleteData && m.info != nil {
			r.root = m.rec.SavePath
			r.paths = contentFiles(m.rec.SavePath, m.info)
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
		e.savePaths.Delete(r.hash)
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

// contentFiles lists the paths a torrent's files are stored at, including
// the .part names anacrolix uses for incomplete files.
func contentFiles(savePath string, info *metainfo.Info) []string {
	var paths []string
	for _, f := range info.UpvertedFiles() {
		parts := []string{savePath}
		if info.BestName() != metainfo.NoName {
			parts = append(parts, info.BestName())
		}
		p := filepath.Join(append(parts, f.BestPath()...)...)
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
		} else if spec, err := torrent.TorrentSpecFromMagnetUri(r.Magnet); err == nil && len(spec.Trackers) > 0 && len(spec.Trackers[0]) > 0 {
			m.meta.tracker = spec.Trackers[0][0]
		}
		e.savePaths.Store(ih, r.SavePath)
		e.torrents[ih] = m
		if m.info != nil {
			m.completed = e.completedFromStore(m)
		}
		if !r.Stopped {
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
