package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// Fetching a magnet's metadata without adding the torrent, for
// qBittorrent's fetchMetadata: the torrent joins the client with storage
// that holds nothing and transfers disallowed, until its info dictionary
// arrives from peers. Adding the torrent for real drops the fetch first.

// metaFetch is one magnet's metadata being fetched, or fetched.
type metaFetch struct {
	t     *torrent.Torrent // nil once done
	mi    []byte           // the torrent file, once known
	since time.Time
}

const (
	maxMetaFetches = 16
	metaFetchTTL   = time.Hour
)

// ErrBusy is returned when too many metadata fetches are running.
var ErrBusy = errors.New("too many metadata fetches running")

// FetchMetadata starts fetching a magnet's metadata, or reports on a fetch
// already started. It returns the info hash, and the torrent file once the
// metadata is known; nil means it's still being fetched. A torrent already
// in piecemeal answers with its own torrent file.
func (e *Engine) FetchMetadata(magnet string) (string, []byte, error) {
	spec, err := magnetSpec(magnet)
	if err != nil {
		return "", nil, err
	}
	ih := spec.InfoHash
	hash := ih.HexString()
	if mi, ok := e.managedTorrentFile(ih); ok {
		return hash, mi, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed.Load() || e.client == nil {
		return hash, nil, ErrClosed
	}
	e.expireMetaFetchesLocked(time.Now())
	if f, ok := e.metaFetches[ih]; ok {
		select {
		case <-closedOrNil(f.t):
			// The client restarted under it: start over.
			delete(e.metaFetches, ih)
		default:
			return hash, f.mi, nil
		}
	}
	if len(e.metaFetches) >= maxMetaFetches {
		return hash, nil, ErrBusy
	}
	spec.Storage = metadataOnly{}
	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return hash, nil, err
	}
	t.DisallowDataDownload()
	t.DisallowDataUpload()
	f := &metaFetch{t: t, since: time.Now()}
	e.metaFetches[ih] = f
	go e.awaitMetadata(ih, f, t)
	return hash, nil, nil
}

// FetchedMetadata returns a fetched torrent file by info hash, without
// starting a fetch.
func (e *Engine) FetchedMetadata(hash string) []byte {
	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		return nil
	}
	if mi, ok := e.managedTorrentFile(ih); ok {
		return mi
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if f, ok := e.metaFetches[ih]; ok {
		return f.mi
	}
	return nil
}

func (e *Engine) awaitMetadata(ih metainfo.Hash, f *metaFetch, t *torrent.Torrent) {
	select {
	case <-t.GotInfo():
	case <-t.Closed():
		return
	}
	mi := t.Metainfo()
	var buf bytes.Buffer
	err := mi.Write(&buf)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.metaFetches[ih] != f || f.t != t {
		return
	}
	if err == nil {
		f.mi = buf.Bytes()
	}
	f.t = nil
	t.Drop()
}

// managedTorrentFile is a torrent's own file when piecemeal has it with
// metadata.
func (e *Engine) managedTorrentFile(ih metainfo.Hash) ([]byte, bool) {
	e.mu.Lock()
	m, ok := e.torrents[ih]
	have := ok && m.rec.Metainfo != nil
	e.mu.Unlock()
	if !have {
		return nil, false
	}
	mi, err := e.TorrentFile(ih.HexString())
	return mi, err == nil
}

// fetchedForMagnet is a magnet's fetched torrent file, if any.
func (e *Engine) fetchedForMagnet(magnet string) []byte {
	spec, err := magnetSpec(magnet)
	if err != nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if f, ok := e.metaFetches[spec.InfoHash]; ok {
		return f.mi
	}
	return nil
}

// takeFetchedLocked removes a magnet's fetch before the torrent is added
// to the client for real, returning its torrent file if it was fetched.
func (e *Engine) takeFetchedLocked(ih metainfo.Hash) []byte {
	f, ok := e.metaFetches[ih]
	if !ok {
		return nil
	}
	delete(e.metaFetches, ih)
	if f.t != nil {
		f.t.Drop()
		f.t = nil
	}
	return f.mi
}

func (e *Engine) expireMetaFetchesLocked(now time.Time) {
	for ih, f := range e.metaFetches {
		if now.Sub(f.since) < metaFetchTTL {
			continue
		}
		if f.t != nil {
			f.t.Drop()
		}
		delete(e.metaFetches, ih)
	}
}

// closedOrNil is t's Closed channel, or nil (never ready) for a finished
// fetch.
func closedOrNil(t *torrent.Torrent) <-chan struct{} {
	if t == nil {
		return nil
	}
	return t.Closed()
}

// metadataOnly is storage that holds nothing: a torrent fetching metadata
// never downloads or uploads data.
type metadataOnly struct{}

func (metadataOnly) OpenTorrent(context.Context, *metainfo.Info, metainfo.Hash) (storage.TorrentImpl, error) {
	return storage.TorrentImpl{
		Piece: func(metainfo.Piece) storage.PieceImpl { return noPiece{} },
		Close: func() error { return nil },
	}, nil
}

type noPiece struct{}

var errMetadataOnly = errors.New("fetching metadata only")

func (noPiece) ReadAt([]byte, int64) (int, error)  { return 0, io.EOF }
func (noPiece) WriteAt([]byte, int64) (int, error) { return 0, errMetadataOnly }
func (noPiece) MarkComplete() error                { return errMetadataOnly }
func (noPiece) MarkNotComplete() error             { return nil }

func (noPiece) Completion() storage.Completion { return storage.Completion{Ok: true} }
