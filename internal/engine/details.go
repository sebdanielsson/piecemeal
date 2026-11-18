package engine

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// File is one file of a torrent.
type File struct {
	Index    int      `json:"index"`
	Path     string   `json:"path" doc:"Path relative to the save path, with / separators, after the content layout and any renames."`
	Size     int64    `json:"size"`
	Progress float64  `json:"progress" doc:"0 to 1. Stopped torrents report 0 for files they haven't finished."`
	Priority Priority `json:"priority" enum:"0,1,6,7" doc:"0 skip, 1 normal, 6 high, 7 highest."`
	// The range of pieces the file spans, end exclusive.
	PieceStart int `json:"pieceStart"`
	PieceEnd   int `json:"pieceEnd"`
}

// Files returns a torrent's files. A torrent without metadata has none.
func (e *Engine) Files(hash string) ([]File, error) {
	e.mu.Lock()
	m, err := e.get(hash)
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	info, t, prio, h := m.info, m.t, slices.Clone(m.filePrio), m.hash
	var rel []string
	if info != nil {
		rel = m.pathsOf().relativePaths(info)
	}
	e.mu.Unlock()
	if info == nil {
		return []File{}, nil
	}
	if t != nil && t.Info() != nil {
		files := t.Files()
		out := make([]File, len(files))
		for i, f := range files {
			p := PriorityNormal
			if prio != nil && i < len(prio) {
				p = prio[i]
			}
			var progress float64 = 1
			if f.Length() > 0 {
				progress = float64(f.BytesCompleted()) / float64(f.Length())
			}
			out[i] = File{
				Index: i, Path: rel[i], Size: f.Length(), Progress: progress,
				Priority: p, PieceStart: f.BeginPieceIndex(), PieceEnd: f.EndPieceIndex(),
			}
		}
		return out, nil
	}
	// Stopped: work out each file's progress from the completion store.
	files := info.UpvertedFiles()
	out := make([]File, len(files))
	var offset int64
	for i, fi := range files {
		p := PriorityNormal
		if prio != nil && i < len(prio) {
			p = prio[i]
		}
		start, end := pieceRange(info, offset, fi.Length)
		f := File{Index: i, Path: rel[i], Size: fi.Length, Priority: p, PieceStart: start, PieceEnd: end}
		if fi.Length == 0 {
			f.Progress = 1
		} else if e.piecesComplete(h, start, end) {
			f.Progress = 1
		}
		out[i] = f
		offset += fi.Length
	}
	return out, nil
}

func (e *Engine) piecesComplete(h metainfo.Hash, start, end int) bool {
	for i := start; i < end; i++ {
		c, err := e.pc.Get(metainfo.PieceKey{InfoHash: h, Index: i})
		if err != nil || !c.Ok || !c.Complete {
			return false
		}
	}
	return true
}

func pieceRange(info *metainfo.Info, offset, length int64) (start, end int) {
	if info.PieceLength == 0 {
		return 0, 0
	}
	start = int(offset / info.PieceLength)
	if length == 0 {
		return start, start
	}
	end = int((offset+length-1)/info.PieceLength) + 1
	return start, end
}

// Peer is a connected peer.
type Peer struct {
	Address    string  `json:"address"`
	Client     string  `json:"client"`
	Network    string  `json:"network" doc:"tcp, utp, webrtc or webseed."`
	Source     string  `json:"source" doc:"How the peer was found."`
	Encryption string  `json:"encryption" enum:"none,header,rc4" doc:"header: obfuscated handshake only; rc4: the whole connection."`
	Progress   float64 `json:"progress" doc:"How much of the torrent the peer has, 0 to 1."`
	DownRate   int64   `json:"downloadRate" doc:"Bytes per second from the peer."`
	UpRate     int64   `json:"uploadRate" doc:"Bytes per second to the peer."`
	Downloaded int64   `json:"downloaded" doc:"Payload bytes from the peer this session."`
	Uploaded   int64   `json:"uploaded" doc:"Payload bytes to the peer this session."`
}

// Peers returns a running torrent's connected peers.
func (e *Engine) Peers(hash string) ([]Peer, error) {
	e.mu.Lock()
	m, err := e.get(hash)
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	t := m.t
	e.mu.Unlock()
	if t == nil {
		return []Peer{}, nil
	}
	conns := t.PeerConns()
	n := 0
	if t.Info() != nil {
		n = t.NumPieces()
	}
	out := make([]Peer, 0, len(conns))
	for _, pc := range conns {
		st := pc.Stats()
		name, _ := pc.PeerClientName.Load().(string)
		p := Peer{
			Address:    pc.RemoteAddr.String(),
			Client:     name,
			Network:    pc.Network,
			Source:     sourceName(pc.Discovery),
			Encryption: encryption(pc),
			DownRate:   int64(st.DownloadRate),
			UpRate:     int64(st.LastWriteUploadRate),
			Downloaded: st.BytesReadUsefulData.Int64(),
			Uploaded:   st.BytesWrittenData.Int64(),
		}
		if n > 0 {
			p.Progress = min(float64(st.RemotePieceCount)/float64(n), 1)
		}
		out = append(out, p)
	}
	return out, nil
}

// encryption reads a connection's encryption from its description:
// anacrolix exports it only there, as the E (RC4) and e (header only)
// connection flags. Both are set at the handshake, before the connection
// is listed.
func encryption(pc *torrent.PeerConn) string {
	s := pc.String()
	i := strings.Index(s, "flags=")
	if i < 0 {
		return "none"
	}
	flags, _, _ := strings.Cut(s[i+len("flags="):], " ")
	for _, f := range strings.Split(flags, ",") {
		switch f {
		case "E":
			return "rc4"
		case "e":
			return "header"
		}
	}
	return "none"
}

func sourceName(s torrent.PeerSource) string {
	switch s {
	case torrent.PeerSourceTracker:
		return "tracker"
	case torrent.PeerSourceIncoming:
		return "incoming"
	case torrent.PeerSourceDhtGetPeers, torrent.PeerSourceDhtAnnouncePeer:
		return "dht"
	case torrent.PeerSourcePex:
		return "pex"
	case torrent.PeerSourceDirect:
		return "direct"
	default:
		return string(s)
	}
}

// Tracker is one of a torrent's trackers.
type Tracker struct {
	URL  string `json:"url"`
	Tier int    `json:"tier"`
}

// Trackers returns a torrent's trackers, by tier.
func (e *Engine) Trackers(hash string) ([]Tracker, error) {
	e.mu.Lock()
	m, err := e.get(hash)
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	al := m.currentTrackers()
	e.mu.Unlock()
	out := []Tracker{}
	for tier, urls := range al {
		for _, u := range urls {
			out = append(out, Tracker{URL: u, Tier: tier})
		}
	}
	return out, nil
}

// Pieces returns a torrent's piece states as a string with one byte per
// piece: 0 missing, 1 downloading, 2 complete, 3 checking.
func (e *Engine) Pieces(hash string) (string, error) {
	e.mu.Lock()
	m, err := e.get(hash)
	if err != nil {
		e.mu.Unlock()
		return "", err
	}
	t, info, h := m.t, m.info, m.hash
	e.mu.Unlock()
	if info == nil {
		return "", nil
	}
	b := make([]byte, info.NumPieces())
	if t == nil {
		for i := range b {
			b[i] = '0'
			if e.piecesComplete(h, i, i+1) {
				b[i] = '2'
			}
		}
		return string(b), nil
	}
	i := 0
	for _, run := range t.PieceStateRuns() {
		c := byte('0')
		switch {
		case run.Hashing || run.QueuedForHash:
			c = '3'
		case run.Complete:
			c = '2'
		case run.Partial:
			c = '1'
		}
		for range run.Length {
			if i < len(b) {
				b[i] = c
			}
			i++
		}
	}
	return string(b), nil
}

// SavePathOf returns where a torrent's content is.
func (e *Engine) SavePathOf(hash string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m, err := e.get(hash)
	if err != nil {
		return "", err
	}
	if m.info == nil {
		return m.rec.SavePath, nil
	}
	return filepath.Join(m.rec.SavePath, m.info.BestName()), nil
}
