package engine

import (
	"cmp"
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent/types"
)

// State is a torrent's state. Stopped, queued and checking torrents are
// told apart by Torrent.Complete into their downloading and seeding variants.
type State string

// Torrent states.
const (
	StateMetadata           State = "metadata"
	StateChecking           State = "checking"
	StateDownloading        State = "downloading"
	StateStalledDownloading State = "stalledDownloading"
	StateSeeding            State = "seeding"
	StateStalledSeeding     State = "stalledSeeding"
	StateStopped            State = "stopped"
	StateQueued             State = "queued"
	StateMoving             State = "moving"
	StateError              State = "error"
)

// stallGrace is how long after the last payload a torrent still counts as
// downloading or seeding rather than stalled.
const stallGrace = 30 * time.Second

// Torrent is a torrent as the API reports it.
type Torrent struct {
	Hash        string   `json:"hash" doc:"SHA-1 infohash, hex."`
	Name        string   `json:"name"`
	State       State    `json:"state" enum:"metadata,checking,downloading,stalledDownloading,seeding,stalledSeeding,stopped,queued,moving,error"`
	Complete    bool     `json:"complete" doc:"Every wanted file is downloaded."`
	HasMetadata bool     `json:"hasMetadata"`
	Progress    float64  `json:"progress" doc:"Fraction of the wanted bytes downloaded, 0 to 1."`
	Size        int64    `json:"size" doc:"Bytes in the wanted files."`
	TotalSize   int64    `json:"totalSize" doc:"Bytes in all files."`
	Completed   int64    `json:"completed" doc:"Wanted bytes downloaded and verified."`
	Downloaded  int64    `json:"downloaded" doc:"Payload bytes downloaded, all time."`
	Uploaded    int64    `json:"uploaded" doc:"Payload bytes uploaded, all time."`
	SessionDown int64    `json:"sessionDownloaded" doc:"Payload bytes downloaded since piecemeal started."`
	SessionUp   int64    `json:"sessionUploaded" doc:"Payload bytes uploaded since piecemeal started."`
	MultiFile   bool     `json:"multiFile" doc:"The torrent's files are in a root folder, even if there is only one."`
	Trackers    int      `json:"trackers" doc:"Number of trackers."`
	DownRate    int64    `json:"downloadRate" doc:"Bytes per second."`
	UpRate      int64    `json:"uploadRate" doc:"Bytes per second."`
	ETA         int64    `json:"eta" doc:"Seconds until complete, -1 when unknown."`
	Ratio       float64  `json:"ratio"`
	Seeds       int      `json:"seeds" doc:"Connected seeds."`
	Peers       int      `json:"peers" doc:"Connected peers that are not seeds."`
	SwarmPeers  int      `json:"swarmPeers" doc:"Peers known in the swarm."`
	SavePath    string   `json:"savePath"`
	ContentPath string   `json:"contentPath" doc:"The torrent's file, or its top folder, or the save path when its files share no folder."`
	RootPath    string   `json:"rootPath" doc:"The torrent's top folder, empty when it has none."`
	Layout      string   `json:"contentLayout" enum:"Original,Subfolder,NoSubfolder"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	AddedAt     int64    `json:"addedAt" doc:"Unix time."`
	CompletedAt int64    `json:"completedAt" doc:"Unix time, 0 if never completed."`
	LastActive  int64    `json:"lastActivityAt" doc:"Unix time of the last payload transfer, 0 if none."`
	SeedingTime int64    `json:"seedingTime" doc:"Seconds spent seeding, all time."`
	ActiveTime  int64    `json:"activeTime" doc:"Seconds spent running, all time."`
	Stopped     bool     `json:"stopped"`
	Forced      bool     `json:"forced"`
	Sequential  bool     `json:"sequential"`
	FirstLast   bool     `json:"firstLastPiece"`
	Private     bool     `json:"private"`
	PieceSize   int64    `json:"pieceSize"`
	Pieces      int      `json:"pieces"`
	PiecesHave  int      `json:"piecesHave"`
	Magnet      string   `json:"magnet"`
	Comment     string   `json:"comment"`
	CreatedBy   string   `json:"createdBy"`
	CreatedAt   int64    `json:"createdAt" doc:"Unix time the torrent was created, 0 if unknown."`
	Tracker     string   `json:"tracker" doc:"The first tracker's URL."`
	Error       string   `json:"error,omitempty"`

	RatioLimit               float64 `json:"ratioLimit" doc:"-2 follows the global setting, -1 is unlimited."`
	SeedingTimeLimit         int64   `json:"seedingTimeLimit" doc:"Minutes. -2 follows the global setting, -1 is unlimited."`
	InactiveSeedingTimeLimit int64   `json:"inactiveSeedingTimeLimit" doc:"Minutes. -2 follows the global setting, -1 is unlimited."`
	QueuePosition            int64   `json:"queuePosition"`
}

// TransferStatus is the client-wide transfer state.
type TransferStatus struct {
	DownRate          int64  `json:"downloadRate" doc:"Bytes per second."`
	UpRate            int64  `json:"uploadRate" doc:"Bytes per second."`
	SessionDownloaded int64  `json:"sessionDownloaded" doc:"Payload bytes downloaded since piecemeal started."`
	SessionUploaded   int64  `json:"sessionUploaded" doc:"Payload bytes uploaded since piecemeal started."`
	DownloadLimit     int64  `json:"downloadLimit" doc:"The limit in effect, bytes per second, 0 for unlimited."`
	UploadLimit       int64  `json:"uploadLimit" doc:"The limit in effect, bytes per second, 0 for unlimited."`
	AltSpeedEnabled   bool   `json:"altSpeedEnabled"`
	Port              int    `json:"port" doc:"The port peers connect to."`
	DHTNodes          int    `json:"dhtNodes"`
	Peers             int    `json:"peers" doc:"Connected peers across all torrents."`
	FreeSpace         int64  `json:"freeSpace" doc:"Free bytes on the default save path's filesystem, -1 if unknown."`
	Torrents          int    `json:"torrents"`
	Connection        string `json:"connection" enum:"connected,firewalled,disconnected"`
}

type snapshot struct {
	list     []*Torrent
	byHash   map[string]*Torrent
	transfer TransferStatus
}

// List returns every torrent's status, oldest first.
func (e *Engine) List() []Torrent {
	s := e.snap.Load()
	out := make([]Torrent, len(s.list))
	for i, st := range s.list {
		out[i] = *st
	}
	return out
}

// Get returns one torrent's status.
func (e *Engine) Get(hash string) (Torrent, bool) {
	st, ok := e.snap.Load().byHash[hash]
	if !ok {
		return Torrent{}, false
	}
	return *st, true
}

// Transfer returns the client-wide transfer state.
func (e *Engine) Transfer() TransferStatus {
	return e.snap.Load().transfer
}

// rates keeps a torrent's smoothed transfer rates between ticks.
type rates struct {
	at         time.Time
	down, up   int64 // totals at the previous tick
	downR, upR float64
}

// tick refreshes every torrent's counters and status and publishes the
// changes.
func (e *Engine) tick(now time.Time) {
	e.mu.Lock()
	e.collectCountersLocked()
	prev := e.snap.Load()
	next := &snapshot{byHash: make(map[string]*Torrent, len(e.torrents))}
	var down, up float64
	for _, m := range e.torrents {
		e.updateRates(m, now)
		e.updateTimes(m, now)
		if m.rec.Sequential && m.t != nil {
			e.applyPiecePrioritiesLocked(m)
		}
		st := e.statusLocked(m)
		next.list = append(next.list, st)
		next.byHash[st.Hash] = st
		down += m.rates.downR
		up += m.rates.upR
	}
	next.transfer = e.transferLocked(down, up, len(e.torrents))
	slices.SortFunc(next.list, func(a, b *Torrent) int {
		return cmp.Or(cmp.Compare(a.AddedAt, b.AddedAt), cmp.Compare(a.Hash, b.Hash))
	})
	// Stored under the lock, so a refreshLocked can't be overwritten by an
	// older snapshot.
	e.snap.Store(next)
	e.mu.Unlock()

	for _, st := range next.list {
		if old, ok := prev.byHash[st.Hash]; !ok || !st.equal(old) {
			e.bus.publish(Event{Type: EventTorrent, Torrent: st})
		}
	}
	// Remove publishes its own event, unless a tick dropped the torrent
	// from the snapshot first.
	for h := range prev.byHash {
		if _, ok := next.byHash[h]; !ok {
			e.bus.publish(Event{Type: EventTorrentRemoved, Hash: h})
		}
	}
	if next.transfer != prev.transfer {
		t := next.transfer
		e.bus.publish(Event{Type: EventTransfer, Transfer: &t})
	}
}

func (e *Engine) updateRates(m *managed, now time.Time) {
	r := &m.rates
	if !r.at.IsZero() {
		dt := now.Sub(r.at).Seconds()
		if dt > 0 {
			dr := float64(m.rec.Downloaded-r.down) / dt
			ur := float64(m.rec.Uploaded-r.up) / dt
			// Exponential smoothing over a few ticks, so rates don't jump
			// with every chunk.
			const a = 0.5
			r.downR = a*max(dr, 0) + (1-a)*r.downR
			r.upR = a*max(ur, 0) + (1-a)*r.upR
			if r.downR < 1 {
				r.downR = 0
			}
			if r.upR < 1 {
				r.upR = 0
			}
			if m.rec.Downloaded > r.down || m.rec.Uploaded > r.up {
				m.rec.LastActivityAt = now.Unix()
				dd, du := max(m.rec.Downloaded-r.down, 0), max(m.rec.Uploaded-r.up, 0)
				m.sessionDown += dd
				m.sessionUp += du
				if dd > 0 {
					m.lastDownAt = now
				}
				if du > 0 {
					m.lastUpAt = now
				}
				e.session.mu.Lock()
				e.session.downloaded += dd
				e.session.uploaded += du
				e.session.mu.Unlock()
			}
		}
	}
	if m.t == nil {
		r.downR, r.upR = 0, 0
	}
	r.at, r.down, r.up = now, m.rec.Downloaded, m.rec.Uploaded
}

// updateTimes accumulates active and seeding time, and records completion.
func (e *Engine) updateTimes(m *managed, now time.Time) {
	last := m.timesAt
	m.timesAt = now
	if m.t == nil || m.info == nil {
		m.timeAcc = 0
		return
	}
	complete := m.complete()
	if complete && m.rec.CompletedAt == 0 {
		m.rec.CompletedAt = now.Unix()
		m.dirty = true
		e.log.Info("torrent completed", "hash", m.rec.InfoHash, "name", m.rec.Name)
	}
	if last.IsZero() {
		return
	}
	m.timeAcc += now.Sub(last)
	secs := int64(m.timeAcc / time.Second)
	if secs <= 0 {
		return
	}
	m.timeAcc -= time.Duration(secs) * time.Second
	m.rec.ActiveSeconds += secs
	if complete {
		m.rec.SeedingSeconds += secs
	}
	m.dirty = true
}

// wanted returns the size of the wanted files and how much of them is done.
func (m *managed) wanted() (size, done int64) {
	if m.info == nil {
		return 0, 0
	}
	if m.t == nil {
		// Stopped: per-file detail isn't loaded, use the whole torrent.
		total := m.info.TotalLength()
		if m.filePrio == nil {
			return total, m.completed
		}
		for i, f := range m.info.UpvertedFiles() {
			if i < len(m.filePrio) && m.filePrio[i] == PrioritySkip {
				continue
			}
			size += f.Length
		}
		return size, min(m.completed, size)
	}
	if m.filePrio == nil {
		return m.t.Length(), m.t.BytesCompleted()
	}
	for _, f := range m.t.Files() {
		if f.Priority() == types.PiecePriorityNone {
			continue
		}
		size += f.Length()
		done += f.BytesCompleted()
	}
	return size, done
}

// complete reports whether every wanted piece is downloaded and has
// passed its hash check. anacrolix's byte counts include chunks written
// but not yet verified, so they alone would call a torrent complete while
// its last pieces are still being checked, and a move or an import then
// could take unfinished files.
func (m *managed) complete() bool {
	size, done := m.wanted()
	if m.info == nil || done < size {
		return false
	}
	if m.t == nil || m.t.Info() == nil {
		// Stopped: the counts come from the piece completion store, which
		// only holds verified pieces.
		return true
	}
	if m.filePrio == nil {
		return m.t.Complete().Bool()
	}
	for _, f := range m.t.Files() {
		if f.Priority() == types.PiecePriorityNone {
			continue
		}
		for i := f.BeginPieceIndex(); i < f.EndPieceIndex(); i++ {
			if !m.t.PieceState(i).Complete {
				return false
			}
		}
	}
	return true
}

func (e *Engine) statusLocked(m *managed) *Torrent {
	r := m.rec
	st := &Torrent{
		Hash: r.InfoHash, Name: r.Name, HasMetadata: m.info != nil, Downloaded: r.Downloaded,
		Uploaded: r.Uploaded, SessionDown: m.sessionDown, SessionUp: m.sessionUp,
		DownRate: int64(m.rates.downR), UpRate: int64(m.rates.upR), ETA: -1, Trackers: m.meta.trackers,
		SavePath: r.SavePath, Category: r.Category, Tags: m.tags, AddedAt: r.AddedAt,
		CompletedAt: r.CompletedAt, LastActive: r.LastActivityAt, SeedingTime: r.SeedingSeconds,
		ActiveTime: r.ActiveSeconds, Stopped: r.Stopped, Forced: r.Forced, Sequential: r.Sequential,
		FirstLast: r.FirstLastPiece, Magnet: r.Magnet, Error: r.Error, RatioLimit: r.RatioLimit,
		SeedingTimeLimit: r.SeedingTimeLimit, InactiveSeedingTimeLimit: r.InactiveSeedingTimeLimit,
		QueuePosition: r.QueuePosition,
		Layout:        cmp.Or(r.ContentLayout, LayoutOriginal),
	}
	if st.Tags == nil {
		st.Tags = []string{}
	}
	if m.info != nil {
		st.TotalSize = m.info.TotalLength()
		st.MultiFile = m.info.IsDir()
		st.PieceSize = m.info.PieceLength
		st.Pieces = m.info.NumPieces()
		st.Private = m.info.Private != nil && *m.info.Private
		st.ContentPath, st.RootPath = contentAndRoot(r.SavePath, m.pathsOf().relativePaths(m.info))
		st.Size, st.Completed = m.wanted()
		st.Complete = m.complete()
		if !st.Complete && st.Size > 0 && st.Completed >= st.Size {
			// Every chunk is written but the last pieces are still being
			// checked: not 100% yet.
			st.Completed = st.Size - 1
		}
		if st.Size > 0 {
			st.Progress = float64(st.Completed) / float64(st.Size)
		} else {
			st.Progress = 1
		}
		if m.t != nil {
			st.PiecesHave = m.t.Stats().PiecesComplete
		} else if st.PieceSize > 0 {
			st.PiecesHave = int(m.completed / st.PieceSize)
		}
	} else {
		st.ContentPath = r.SavePath
	}
	st.Comment, st.CreatedBy, st.CreatedAt, st.Tracker = m.meta.comment, m.meta.createdBy, m.meta.createdAt, m.meta.tracker
	st.Ratio = ratio(r.Uploaded, r.Downloaded, st.Completed)

	if m.t != nil {
		ts := m.t.Stats()
		st.Seeds = ts.ConnectedSeeders
		st.Peers = max(ts.ActivePeers-ts.ConnectedSeeders, 0)
		st.SwarmPeers = ts.TotalPeers
	}
	if !st.Complete && st.DownRate > 0 {
		st.ETA = (st.Size - st.Completed) / st.DownRate
	} else if st.Complete {
		st.ETA = 0
	}

	switch {
	case m.moving:
		st.State = StateMoving
	case r.Error != "":
		st.State = StateError
	case m.queued:
		st.State = StateQueued
	case m.t == nil:
		st.State = StateStopped
	case m.checking:
		st.State = StateChecking
	case m.info == nil:
		st.State = StateMetadata
	// A torrent counts as transferring for a while after the last payload,
	// so a pause between pieces doesn't flip it to stalled: the *arrs show
	// a stalled download as a warning.
	case st.Complete && (st.UpRate > 0 || time.Since(m.lastUpAt) < stallGrace):
		st.State = StateSeeding
	case st.Complete:
		st.State = StateStalledSeeding
	case st.DownRate > 0 || time.Since(m.lastDownAt) < stallGrace:
		st.State = StateDownloading
	case hashing(m):
		st.State = StateChecking
	default:
		st.State = StateStalledDownloading
	}
	return st
}

// hashing reports whether any of a running torrent's pieces are being
// verified, as during the initial check of existing data.
func hashing(m *managed) bool {
	for _, run := range m.t.PieceStateRuns() {
		if run.Hashing || run.QueuedForHash {
			return true
		}
	}
	return false
}

// ratio is uploaded over downloaded. A torrent seeded from data that was
// already on disk has downloaded little or nothing, so below 1% of the
// completed size the completed size is the denominator instead.
func ratio(uploaded, downloaded, completed int64) float64 {
	d := downloaded
	if float64(d) < float64(completed)*0.01 {
		d = completed
	}
	if d <= 0 {
		return 0
	}
	return math.Round(float64(uploaded)/float64(d)*10000) / 10000
}

func (e *Engine) transferLocked(down, up float64, n int) TransferStatus {
	s := e.settings.Get()
	e.session.mu.Lock()
	sd, su := e.session.downloaded, e.session.uploaded
	e.session.mu.Unlock()
	cs := e.client.Stats()
	t := TransferStatus{
		DownRate: int64(down), UpRate: int64(up), SessionDownloaded: sd, SessionUploaded: su,
		DownloadLimit: limitOf(e.down), UploadLimit: limitOf(e.up), AltSpeedEnabled: s.AltSpeedEnabled,
		Port: e.port, Peers: cs.ActivePeers, FreeSpace: freeSpace(s.DefaultSavePath), Torrents: n,
		Connection: "connected",
	}
	for _, d := range e.client.DhtServers() {
		if st, ok := d.Stats().(dht.ServerStats); ok {
			t.DHTNodes += st.GoodNodes
		}
	}
	if len(e.client.ListenAddrs()) == 0 {
		t.Connection = "disconnected"
	}
	return t
}

// refreshLocked recomputes one torrent's status right after a change, so
// the API returns it updated without waiting for the next tick.
func (e *Engine) refreshLocked(ms ...*managed) {
	if len(ms) == 0 {
		return
	}
	fresh := make(map[string]*Torrent, len(ms))
	for _, m := range ms {
		fresh[m.rec.InfoHash] = e.statusLocked(m)
	}
	prev := e.snap.Load()
	next := &snapshot{byHash: make(map[string]*Torrent, len(prev.byHash)+len(ms)), transfer: prev.transfer}
	for _, s := range prev.list {
		if st, ok := fresh[s.Hash]; ok {
			s = st
			delete(fresh, s.Hash)
		}
		next.list = append(next.list, s)
		next.byHash[s.Hash] = s
	}
	for _, st := range fresh { // new torrents
		next.list = append(next.list, st)
		next.byHash[st.Hash] = st
		next.transfer.Torrents++
	}
	e.snap.Store(next)
	for _, m := range ms {
		e.bus.publish(Event{Type: EventTorrent, Torrent: next.byHash[m.rec.InfoHash]})
	}
}

// refreshTransferLocked recomputes the transfer status after a change to
// limits, keeping the last tick's rates.
func (e *Engine) refreshTransferLocked() {
	prev := e.snap.Load()
	next := *prev
	next.transfer = e.transferLocked(float64(prev.transfer.DownRate), float64(prev.transfer.UpRate), len(prev.list))
	e.snap.Store(&next)
}

func (e *Engine) removeFromSnapshot(hash string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.snap.Load()
	if _, ok := prev.byHash[hash]; !ok {
		return
	}
	next := &snapshot{byHash: make(map[string]*Torrent, len(prev.byHash)), transfer: prev.transfer}
	for _, s := range prev.list {
		if s.Hash != hash {
			next.list = append(next.list, s)
			next.byHash[s.Hash] = s
		}
	}
	next.transfer.Torrents--
	e.snap.Store(next)
	e.bus.publish(Event{Type: EventTorrentRemoved, Hash: hash})
}

func (s *Torrent) equal(o *Torrent) bool {
	return reflect.DeepEqual(s, o)
}

// enforceSeedGoals stops or removes complete torrents that reached a seeding
// goal.
func (e *Engine) enforceSeedGoals() {
	s := e.settings.Get()
	now := time.Now().Unix()
	var reached []string
	e.mu.Lock()
	for _, m := range e.torrents {
		if m.t == nil || m.info == nil || !m.complete() {
			continue
		}
		ratioLimit := m.rec.RatioLimit
		if ratioLimit == -2 {
			ratioLimit = s.RatioLimit
		}
		seedLimit := m.rec.SeedingTimeLimit
		if seedLimit == -2 {
			seedLimit = s.SeedingTimeLimit
		}
		inactiveLimit := m.rec.InactiveSeedingTimeLimit
		if inactiveLimit == -2 {
			inactiveLimit = s.InactiveSeedingTimeLimit
		}
		_, done := m.wanted()
		r := ratio(m.rec.Uploaded, m.rec.Downloaded, done)
		lastActive := max(m.rec.LastActivityAt, m.rec.CompletedAt)
		switch {
		case ratioLimit >= 0 && r >= ratioLimit:
		case seedLimit >= 0 && m.rec.SeedingSeconds >= seedLimit*60:
		case inactiveLimit >= 0 && lastActive > 0 && now-lastActive >= inactiveLimit*60:
		default:
			continue
		}
		reached = append(reached, m.rec.InfoHash)
	}
	e.mu.Unlock()
	if len(reached) == 0 {
		return
	}
	ctx := e.ctxOrBackground()
	var err error
	switch s.SeedGoalAction {
	case "remove":
		err = e.Remove(ctx, false, reached...)
	case "removeWithData":
		err = e.Remove(ctx, true, reached...)
	default:
		err = e.Stop(ctx, reached...)
	}
	e.log.Info("seeding goal reached", "torrents", reached, "action", s.SeedGoalAction, "error", err)
}
