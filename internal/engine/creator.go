package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// CreateParams describes a torrent to create from files on disk.
type CreateParams struct {
	// SourcePath is the file or folder, inside an allowed root.
	SourcePath string `json:"sourcePath"`
	// PieceSize in bytes; 0 picks one from the total size.
	PieceSize int64      `json:"pieceSize,omitempty" minimum:"0"`
	Private   bool       `json:"private,omitempty"`
	Trackers  [][]string `json:"trackers,omitempty" doc:"Tiers of announce URLs."`
	WebSeeds  []string   `json:"webSeeds,omitempty"`
	Comment   string     `json:"comment,omitempty"`
	Source    string     `json:"source,omitempty" doc:"The info dictionary's source field, which some private trackers require."`
	// StartSeeding adds the new torrent, seeding from SourcePath.
	StartSeeding bool `json:"startSeeding,omitempty"`
}

// CreateTask is a torrent being created, or done.
type CreateTask struct {
	ID         string       `json:"id"`
	Params     CreateParams `json:"params"`
	Status     string       `json:"status" enum:"queued,running,finished,failed"`
	Progress   float64      `json:"progress" doc:"0 to 1."`
	Error      string       `json:"error,omitempty"`
	Hash       string       `json:"hash,omitempty" doc:"The new torrent's infohash, once finished."`
	AddedAt    time.Time    `json:"addedAt"`
	StartedAt  *time.Time   `json:"startedAt,omitempty"`
	FinishedAt *time.Time   `json:"finishedAt,omitempty"`
}

type createTask struct {
	CreateTask
	done    atomic.Int64 // bytes hashed
	total   int64
	torrent []byte
	cancel  context.CancelFunc
}

// creator runs one creation at a time, like qBittorrent.
type creator struct {
	mu    sync.Mutex
	tasks []*createTask
	wake  chan struct{}
}

const maxCreateTasks = 50

// CreateTorrent queues a torrent to be created and returns its task ID.
func (e *Engine) CreateTorrent(p CreateParams) (string, error) {
	src, err := e.confine(p.SourcePath)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if p.PieceSize < 0 || (p.PieceSize != 0 && (p.PieceSize < 16<<10 || p.PieceSize&(p.PieceSize-1) != 0)) {
		return "", fmt.Errorf("%w: piece size must be a power of two of at least 16 KiB", ErrInvalid)
	}
	for _, tier := range p.Trackers {
		for _, u := range tier {
			if err := validAnnounceURL(u); err != nil {
				return "", err
			}
		}
	}
	p.SourcePath = src
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	t := &createTask{CreateTask: CreateTask{ID: hex.EncodeToString(id), Params: p, Status: "queued", AddedAt: time.Now()}}
	c := &e.creator
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.tasks) >= maxCreateTasks {
		// Drop the oldest finished task to make room.
		i := slices.IndexFunc(c.tasks, func(x *createTask) bool { return x.Status == "finished" || x.Status == "failed" })
		if i < 0 {
			return "", fmt.Errorf("%w: too many torrents being created", ErrInvalid)
		}
		c.tasks = slices.Delete(c.tasks, i, i+1)
	}
	c.tasks = append(c.tasks, t)
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return t.ID, nil
}

// CreateTasks returns every creation task, oldest first.
func (e *Engine) CreateTasks() []CreateTask {
	c := &e.creator
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]CreateTask, 0, len(c.tasks))
	for _, t := range c.tasks {
		ct := t.CreateTask
		if t.Status == "running" && t.total > 0 {
			ct.Progress = float64(t.done.Load()) / float64(t.total)
		}
		out = append(out, ct)
	}
	return out
}

// CreatedTorrent returns a finished task's .torrent file.
func (e *Engine) CreatedTorrent(id string) ([]byte, error) {
	c := &e.creator
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.tasks {
		if t.ID != id {
			continue
		}
		if t.Status != "finished" {
			return nil, fmt.Errorf("%w: task is %s", ErrInvalid, t.Status)
		}
		return t.torrent, nil
	}
	return nil, fmt.Errorf("%w: task %s", ErrNotFound, id)
}

// DeleteCreateTask cancels a task if it's running and forgets it.
func (e *Engine) DeleteCreateTask(id string) error {
	c := &e.creator
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, t := range c.tasks {
		if t.ID == id {
			if t.cancel != nil {
				t.cancel()
			}
			c.tasks = slices.Delete(c.tasks, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("%w: task %s", ErrNotFound, id)
}

// runCreator works through queued tasks until the engine closes.
func (e *Engine) runCreator() {
	c := &e.creator
	for {
		c.mu.Lock()
		var next *createTask
		for _, t := range c.tasks {
			if t.Status == "queued" {
				next = t
				break
			}
		}
		var ctx context.Context
		if next != nil {
			now := time.Now()
			next.Status, next.StartedAt = "running", &now
			ctx, next.cancel = context.WithCancel(e.ctx)
		}
		c.mu.Unlock()
		if next == nil {
			select {
			case <-e.ctx.Done():
				return
			case <-c.wake:
				continue
			}
		}
		b, hash, err := buildTorrent(ctx, next.Params, &next.done, &next.total)
		next.cancel()
		c.mu.Lock()
		now := time.Now()
		next.FinishedAt = &now
		if err != nil {
			next.Status, next.Error = "failed", err.Error()
		} else {
			next.Status, next.Progress, next.torrent, next.Hash = "finished", 1, b, hash
		}
		p := next.Params
		c.mu.Unlock()
		if err != nil {
			e.log.Warn("creating a torrent failed", "source", p.SourcePath, "error", err)
			continue
		}
		e.log.Info("torrent created", "source", p.SourcePath, "hash", hash)
		if p.StartSeeding {
			if _, err := e.Add(e.ctxOrBackground(), AddParams{
				Metainfo: b, SavePath: filepath.Dir(p.SourcePath), SkipChecking: true,
			}); err != nil && !errors.Is(err, ErrExists) {
				e.log.Error("adding a created torrent", "hash", hash, "error", err)
			}
		}
	}
}

// buildTorrent hashes the files under p.SourcePath into a .torrent file.
// done counts the bytes hashed so far, out of total.
func buildTorrent(ctx context.Context, p CreateParams, done *atomic.Int64, total *int64) ([]byte, string, error) {
	info := metainfo.Info{PieceLength: p.PieceSize, Source: p.Source}
	if p.Private {
		private := true
		info.Private = &private
	}
	root := p.SourcePath
	info.Name = filepath.Base(root)
	st, err := os.Stat(root)
	if err != nil {
		return nil, "", err
	}
	if st.IsDir() {
		err = filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			fi, err := os.Stat(path) // follows symlinks: the bytes hashed are the target's
			if err != nil {
				return err
			}
			if fi.IsDir() || !fi.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			info.Files = append(info.Files, metainfo.FileInfo{
				Path: strings.Split(filepath.ToSlash(rel), "/"), Length: fi.Size(),
			})
			return nil
		})
		if err != nil {
			return nil, "", err
		}
		if len(info.Files) == 0 {
			return nil, "", fmt.Errorf("%w: no files in %s", ErrInvalid, root)
		}
		slices.SortFunc(info.Files, func(a, b metainfo.FileInfo) int {
			return strings.Compare(strings.Join(a.Path, "/"), strings.Join(b.Path, "/"))
		})
	} else {
		info.Length = st.Size()
	}
	*total = info.TotalLength()
	if info.PieceLength == 0 {
		info.PieceLength = metainfo.ChoosePieceLength(*total)
	}
	err = info.GeneratePieces(func(fi metainfo.FileInfo) (io.ReadCloser, error) {
		path := root
		if st.IsDir() {
			path = filepath.Join(root, filepath.FromSlash(strings.Join(fi.Path, "/")))
		}
		f, err := os.Open(path) //nolint:gosec // inside an allowed root, checked by the caller
		if err != nil {
			return nil, err
		}
		return &countingReader{ctx: ctx, r: f, n: done}, nil
	})
	if err != nil {
		return nil, "", err
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		return nil, "", err
	}
	mi := metainfo.MetaInfo{
		InfoBytes: infoBytes, Comment: p.Comment, CreatedBy: "piecemeal", CreationDate: time.Now().Unix(),
		UrlList: p.WebSeeds,
	}
	if len(p.Trackers) > 0 {
		mi.AnnounceList = p.Trackers
		if len(p.Trackers[0]) > 0 {
			mi.Announce = p.Trackers[0][0]
		}
	}
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mi.HashInfoBytes().HexString(), nil
}

// countingReader counts bytes read and stops when ctx ends.
type countingReader struct {
	ctx context.Context
	r   io.ReadCloser
	n   *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func (c *countingReader) Close() error { return c.r.Close() }
