package engine

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Imported is a torrent read from another client's state, for Import.
type Imported struct {
	// Source is the file it was read from, for messages.
	Source   string
	Metainfo []byte
	SavePath string
	// IncompletePath is the torrent's incomplete folder, "" for none, and
	// InIncompletePath is set when its data is there rather than in
	// SavePath.
	IncompletePath   string
	InIncompletePath bool
	// AutoManaged is automatic torrent management.
	AutoManaged bool
	// Name replaces the torrent's own name; "" keeps it.
	Name     string
	Category string
	Tags     []string
	Stopped  bool
	// Times and totals from the other client.
	AddedAt, CompletedAt          time.Time
	Uploaded, Downloaded          int64
	SeedingSeconds, ActiveSeconds int64
	RatioLimit                    float64 // -2 follows the global setting, -1 is unlimited
	SeedingTimeLimit              int64   // minutes, same values
	InactiveSeedingTimeLimit      int64   // minutes, same values
	Sequential, FirstLastPiece    bool
	// Paths are each file's path relative to SavePath, with / separators,
	// after the other client's renames; "" or missing keeps the torrent's.
	Paths []string
	// Skip marks files that aren't downloaded.
	Skip []bool
	// Trackers and WebSeeds replace the torrent file's when not nil.
	Trackers [][]string
	WebSeeds []string
	// Complete is set when the other client had all wanted data.
	Complete bool
}

// ImportOptions control Import.
type ImportOptions struct {
	// AllowedRoots confine save paths, as in the engine's configuration.
	AllowedRoots []string
	// Trust marks the pieces of complete torrents as verified instead of
	// letting piecemeal check them at its next start.
	Trust bool
	// DryRun checks everything but writes nothing.
	DryRun bool
}

// ImportResult is what happened to one imported torrent.
type ImportResult struct {
	Source, Hash, Name string
	// Skipped says why it wasn't imported, for one already in piecemeal.
	Skipped string
	Err     error
}

// Import writes torrents from another client into piecemeal's database,
// for the engine to load at its next start. Their data is checked then,
// unless Trust is set. piecemeal must not be running; the caller holds the
// data directory lock.
func Import(ctx context.Context, st *store.Store, dataDir string, items []Imported, o ImportOptions) []ImportResult {
	have := map[string]bool{}
	if rows, err := st.ListTorrents(ctx); err == nil {
		for _, r := range rows {
			have[r.InfoHash] = true
		}
	}
	var pc storage.PieceCompletion
	if o.Trust && !o.DryRun {
		var err error
		if pc, err = storage.NewBoltPieceCompletion(dataDir); err != nil {
			return []ImportResult{{Err: fmt.Errorf("piece completion database: %w", err)}}
		}
		defer func() { _ = pc.Close() }()
	}
	e := &Engine{cfg: Config{AllowedRoots: o.AllowedRoots}}
	var out []ImportResult
	for _, it := range items {
		res := ImportResult{Source: it.Source}
		rec, tags, info, err := e.importRecord(it)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		res.Hash, res.Name = rec.InfoHash, rec.Name
		if have[rec.InfoHash] {
			res.Skipped = "already in piecemeal"
			out = append(out, res)
			continue
		}
		have[rec.InfoHash] = true
		if !o.DryRun {
			res.Err = st.Tx(ctx, func(q *store.Queries) error {
				if rec.Category != "" {
					if err := q.CreateCategoryIfMissing(ctx, rec.Category); err != nil {
						return err
					}
				}
				for _, tag := range tags {
					if err := q.CreateTag(ctx, tag); err != nil {
						return err
					}
				}
				if err := q.UpsertTorrent(ctx, rec); err != nil {
					return err
				}
				for _, tag := range tags {
					if err := q.AddTorrentTag(ctx, store.AddTorrentTagParams{InfoHash: rec.InfoHash, Tag: tag}); err != nil {
						return err
					}
				}
				return nil
			})
			if res.Err == nil && pc != nil && it.Complete {
				res.Err = trustPieces(pc, rec.InfoHash, info, it.Skip)
			}
		}
		out = append(out, res)
	}
	return out
}

// importRecord turns an imported torrent into a database record.
func (e *Engine) importRecord(it Imported) (store.UpsertTorrentParams, []string, *metainfo.Info, error) {
	var rec store.UpsertTorrentParams
	mi, err := metainfo.Load(bytes.NewReader(it.Metainfo))
	if err != nil {
		return rec, nil, nil, fmt.Errorf("%w: torrent file: %w", ErrInvalid, err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return rec, nil, nil, fmt.Errorf("%w: torrent info: %w", ErrInvalid, err)
	}
	savePath, err := e.confine(filepath.Clean(it.SavePath))
	if err != nil {
		return rec, nil, nil, fmt.Errorf("save path %s: %w (map it into the allowed roots with --map)", it.SavePath, err)
	}
	var incomplete string
	if it.IncompletePath != "" {
		if incomplete, err = e.confine(filepath.Clean(it.IncompletePath)); err != nil {
			return rec, nil, nil, fmt.Errorf("incomplete folder %s: %w (map it into the allowed roots with --map)", it.IncompletePath, err)
		}
	}
	for _, tag := range it.Tags {
		if err := validName("tag", tag); err != nil {
			return rec, nil, nil, err
		}
	}
	if it.Category != "" {
		if err := validName("category", it.Category); err != nil {
			return rec, nil, nil, err
		}
	}
	files := info.UpvertedFiles()
	renames := map[string]string{}
	for i, f := range files {
		if i >= len(it.Paths) || it.Paths[i] == "" {
			continue
		}
		orig := originalPath(&info, f)
		p, err := cleanRelative(it.Paths[i])
		if err != nil {
			return rec, nil, nil, fmt.Errorf("file %d: %w", i, err)
		}
		if p != orig {
			renames[orig] = p
		}
	}
	var prio []Priority
	if slices.Contains(it.Skip, true) {
		prio = make([]Priority, len(files))
		for i := range prio {
			prio[i] = PriorityNormal
			if i < len(it.Skip) && it.Skip[i] {
				prio[i] = PrioritySkip
			}
		}
	}
	tags := slices.Clone(it.Tags)
	slices.Sort(tags)
	tags = slices.Compact(tags)
	unix := func(t time.Time) int64 {
		if t.IsZero() {
			return 0
		}
		return t.Unix()
	}
	m := &managed{renames: renames, filePrio: prio, trackers: it.Trackers, webSeeds: it.WebSeeds}
	if len(renames) == 0 {
		m.renames = nil
	}
	ih := mi.HashInfoBytes()
	m.rec = store.Torrent{
		InfoHash: ih.HexString(), Name: cmp.Or(it.Name, info.BestName()), Metainfo: it.Metainfo, SavePath: savePath,
		Category: it.Category, AddedAt: cmp.Or(unix(it.AddedAt), time.Now().Unix()), CompletedAt: unix(it.CompletedAt),
		Stopped: it.Stopped, Sequential: it.Sequential, FirstLastPiece: it.FirstLastPiece,
		Uploaded: it.Uploaded, Downloaded: it.Downloaded, SeedingSeconds: it.SeedingSeconds,
		ActiveSeconds: it.ActiveSeconds, RatioLimit: it.RatioLimit, SeedingTimeLimit: it.SeedingTimeLimit,
		InactiveSeedingTimeLimit: it.InactiveSeedingTimeLimit, ContentLayout: LayoutOriginal,
		IncompletePath: incomplete, InIncompletePath: incomplete != "" && it.InIncompletePath,
		AutoManaged: it.AutoManaged,
	}
	return m.params(), tags, &info, nil
}

// trustPieces marks the pieces wholly within wanted files complete; the
// rest are checked at the next start.
func trustPieces(pc storage.PieceCompletion, hash string, info *metainfo.Info, skip []bool) error {
	var ih metainfo.Hash
	if err := ih.FromHexString(hash); err != nil {
		return err
	}
	files := info.UpvertedFiles()
	var errs []error
	for i := range info.NumPieces() {
		p := info.Piece(i)
		start, end := p.Offset(), p.Offset()+p.Length()
		ok := true
		var offset int64
		for fi, f := range files {
			fStart, fEnd := offset, offset+f.Length
			offset = fEnd
			if fi < len(skip) && skip[fi] && fStart < end && fEnd > start {
				ok = false
				break
			}
		}
		if ok {
			errs = append(errs, pc.Set(metainfo.PieceKey{InfoHash: ih, Index: i}, g.Some(true)))
		}
	}
	return errors.Join(errs...)
}
