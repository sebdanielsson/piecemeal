package api

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// Filters accepted by the torrent list.
const (
	FilterAll         = "all"
	FilterDownloading = "downloading"
	FilterSeeding     = "seeding"
	FilterCompleted   = "completed"
	FilterStopped     = "stopped"
	FilterActive      = "active"
	FilterInactive    = "inactive"
	FilterStalled     = "stalled"
	FilterChecking    = "checking"
	FilterErrored     = "errored"
)

type listTorrentsInput struct {
	Filter        string   `query:"filter" enum:"all,downloading,seeding,completed,stopped,active,inactive,stalled,checking,errored" default:"all"`
	Category      string   `query:"category" doc:"Only torrents in this category."`
	Uncategorized bool     `query:"uncategorized" doc:"Only torrents without a category."`
	Tag           string   `query:"tag" doc:"Only torrents with this tag."`
	Untagged      bool     `query:"untagged" doc:"Only torrents without tags."`
	Search        string   `query:"search" doc:"Only torrents whose name contains this, ignoring case."`
	Hashes        []string `query:"hashes,explode" doc:"Only these torrents."`
	Sort          string   `query:"sort" enum:"name,addedAt,completedAt,size,progress,state,downloadRate,uploadRate,eta,ratio,seeds,peers,category,uploaded,downloaded" default:"addedAt"`
	Reverse       bool     `query:"reverse"`
	Limit         int      `query:"limit" minimum:"0" maximum:"10000" doc:"Page size; 0 returns everything."`
	Cursor        string   `query:"cursor" doc:"nextCursor from the previous page."`
}

// TorrentList is a page of torrents.
type TorrentList struct {
	Items      []engine.Torrent `json:"items"`
	Total      int              `json:"total" doc:"Torrents matching the filters, across all pages."`
	NextCursor string           `json:"nextCursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page."`
}

type listTorrentsOutput struct{ Body TorrentList }

type hashInput struct {
	Hash string `path:"hash" pattern:"^[0-9a-f]{40}$" doc:"Infohash, lowercase hex."`
}

type torrentOutput struct{ Body engine.Torrent }

// AddTorrentRequest adds a torrent from exactly one of magnet, url and
// torrentFile.
type AddTorrentRequest struct {
	Magnet                   string   `json:"magnet,omitempty" doc:"A magnet URI."`
	URL                      string   `json:"url,omitempty" doc:"An http(s) URL of a .torrent file, or one that redirects to a magnet URI."`
	TorrentFile              []byte   `json:"torrentFile,omitempty" doc:"The .torrent file, base64."`
	SavePath                 string   `json:"savePath,omitempty"`
	Category                 string   `json:"category,omitempty"`
	Tags                     []string `json:"tags,omitempty"`
	Stopped                  *bool    `json:"stopped,omitempty" doc:"Add without starting; defaults to the addStopped setting."`
	Sequential               bool     `json:"sequential,omitempty"`
	FirstLastPiece           bool     `json:"firstLastPiece,omitempty"`
	Name                     string   `json:"name,omitempty" doc:"Display name instead of the torrent's own."`
	RatioLimit               *float64 `json:"ratioLimit,omitempty"`
	SeedingTimeLimit         *int64   `json:"seedingTimeLimit,omitempty" doc:"Minutes."`
	InactiveSeedingTimeLimit *int64   `json:"inactiveSeedingTimeLimit,omitempty" doc:"Minutes."`
	DownloadLimit            int64    `json:"downloadLimit,omitempty" minimum:"0" deprecated:"true" doc:"Ignored: piecemeal has no per-torrent speed limits. Use the global limits."`
	UploadLimit              int64    `json:"uploadLimit,omitempty" minimum:"0" deprecated:"true" doc:"Ignored: piecemeal has no per-torrent speed limits. Use the global limits."`
	SkipChecking             bool     `json:"skipChecking,omitempty" doc:"Trust data already on disk instead of verifying it."`
	ContentLayout            string   `json:"contentLayout,omitempty" enum:"Original,Subfolder,NoSubfolder" doc:"Original keeps the torrent's own layout, Subfolder puts a single file in a folder, NoSubfolder drops the top folder."`
}

type addTorrentInput struct{ Body AddTorrentRequest }

type addTorrentOutput struct {
	Status int
	Body   engine.Torrent
}

// UpdateTorrentRequest changes a torrent. Absent fields are left as they
// are.
type UpdateTorrentRequest struct {
	Name                     *string   `json:"name,omitempty"`
	Comment                  *string   `json:"comment,omitempty" doc:"Replaces the comment shown for the torrent; the torrent file is unchanged."`
	SavePath                 *string   `json:"savePath,omitempty" doc:"Moves the data there in the background; the state is moving until it's done."`
	Category                 *string   `json:"category,omitempty" doc:"Empty removes the category."`
	Tags                     *[]string `json:"tags,omitempty" doc:"Replaces the torrent's tags."`
	Stopped                  *bool     `json:"stopped,omitempty"`
	Forced                   *bool     `json:"forced,omitempty"`
	Sequential               *bool     `json:"sequential,omitempty"`
	FirstLastPiece           *bool     `json:"firstLastPiece,omitempty"`
	RatioLimit               *float64  `json:"ratioLimit,omitempty" doc:"-2 follows the global setting, -1 is unlimited."`
	SeedingTimeLimit         *int64    `json:"seedingTimeLimit,omitempty" doc:"Minutes. -2 follows the global setting, -1 is unlimited."`
	InactiveSeedingTimeLimit *int64    `json:"inactiveSeedingTimeLimit,omitempty" doc:"Minutes. -2 follows the global setting, -1 is unlimited."`
	DownloadLimit            *int64    `json:"downloadLimit,omitempty" minimum:"0" deprecated:"true" doc:"Ignored: piecemeal has no per-torrent speed limits. Use the global limits."`
	UploadLimit              *int64    `json:"uploadLimit,omitempty" minimum:"0" deprecated:"true" doc:"Ignored: piecemeal has no per-torrent speed limits. Use the global limits."`
}

type updateTorrentInput struct {
	Hash string `path:"hash" pattern:"^[0-9a-f]{40}$" doc:"Infohash, lowercase hex."`
	Body UpdateTorrentRequest
}

type deleteTorrentInput struct {
	Hash       string `path:"hash" pattern:"^[0-9a-f]{40}$" doc:"Infohash, lowercase hex."`
	DeleteData bool   `query:"deleteData" doc:"Also delete the downloaded files."`
}

// BulkAction applies an action to several torrents.
type BulkAction struct {
	Hashes     []string `json:"hashes" minItems:"1" doc:"Infohashes, or [\"all\"]."`
	Action     string   `json:"action" enum:"start,stop,forceStart,recheck,reannounce,remove,setCategory,addTags,removeTags,move,queueTop,queueBottom,queueUp,queueDown"`
	DeleteData bool     `json:"deleteData,omitempty" doc:"With remove: also delete the files."`
	SavePath   string   `json:"savePath,omitempty" doc:"With move: where to move the data."`
	Category   string   `json:"category,omitempty" doc:"With setCategory; empty removes the category."`
	Tags       []string `json:"tags,omitempty" doc:"With addTags and removeTags; removeTags without tags removes all."`
}

type bulkInput struct{ Body BulkAction }

type filesOutput struct {
	Body struct {
		Items []engine.File `json:"items"`
	}
}

// SetFilePriority sets the priority of some of a torrent's files.
type SetFilePriority struct {
	Indices  []int           `json:"indices" minItems:"1"`
	Priority engine.Priority `json:"priority" enum:"0,1,6,7" doc:"0 skip, 1 normal, 6 high, 7 highest."`
}

type setFilesInput struct {
	Hash string `path:"hash" pattern:"^[0-9a-f]{40}$" doc:"Infohash, lowercase hex."`
	Body SetFilePriority
}

type peersOutput struct {
	Body struct {
		Items []engine.Peer `json:"items"`
	}
}

type addPeersInput struct {
	Hash string `path:"hash" pattern:"^[0-9a-f]{40}$" doc:"Infohash, lowercase hex."`
	Body struct {
		Addresses []string `json:"addresses" minItems:"1" doc:"host:port, IP addresses only."`
	}
}

type trackersOutput struct {
	Body struct {
		Items []engine.Tracker `json:"items"`
	}
}

type piecesOutput struct {
	Body struct {
		States string `json:"states" doc:"One character per piece: 0 missing, 1 downloading, 2 complete, 3 checking."`
	}
}

func (a *API) registerTorrents() {
	const tag = "Torrents"
	defer a.registerEditing()
	huma.Register(a.huma, a.operation(http.MethodGet, "/torrents", "listTorrents", "List torrents", tag, accessUser),
		func(_ context.Context, in *listTorrentsInput) (*listTorrentsOutput, error) {
			return a.listTorrents(in)
		})

	op := a.operation(http.MethodPost, "/torrents", "addTorrent", "Add a torrent", tag, accessUser)
	op.DefaultStatus = http.StatusCreated
	op.Errors = append(op.Errors, http.StatusConflict, http.StatusUnprocessableEntity)
	// Room for a 16 MiB torrent file in base64, the size adding by URL
	// allows.
	op.MaxBodyBytes = 24 << 20
	huma.Register(a.huma, op, func(ctx context.Context, in *addTorrentInput) (*addTorrentOutput, error) {
		return a.addTorrent(ctx, in.Body)
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/torrents/{hash}", "getTorrent", "Get a torrent", tag, accessUser),
		func(_ context.Context, in *hashInput) (*torrentOutput, error) {
			st, ok := a.Engine.Get(in.Hash)
			if !ok {
				return nil, huma.Error404NotFound("no such torrent")
			}
			return &torrentOutput{Body: st}, nil
		})

	huma.Register(a.huma, a.operation(http.MethodPatch, "/torrents/{hash}", "updateTorrent", "Change a torrent", tag, accessUser),
		func(ctx context.Context, in *updateTorrentInput) (*torrentOutput, error) {
			if err := a.updateTorrent(ctx, in.Hash, in.Body); err != nil {
				return nil, apiError(err)
			}
			st, _ := a.Engine.Get(in.Hash)
			return &torrentOutput{Body: st}, nil
		})

	op = a.operation(http.MethodDelete, "/torrents/{hash}", "deleteTorrent", "Remove a torrent", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *deleteTorrentInput) (*struct{}, error) {
		return nil, apiError(a.Engine.Remove(ctx, in.DeleteData, in.Hash))
	})

	for _, act := range []struct {
		path, id, summary string
		fn                func(context.Context, ...string) error
	}{
		{"start", "startTorrent", "Start a torrent", a.Engine.Start},
		{"stop", "stopTorrent", "Stop a torrent", a.Engine.Stop},
		{"recheck", "recheckTorrent", "Verify a torrent's data", a.Engine.Recheck},
		{"reannounce", "reannounceTorrent", "Announce to a torrent's trackers now", a.Engine.Reannounce},
	} {
		op := a.operation(http.MethodPost, "/torrents/{hash}/"+act.path, act.id, act.summary, tag, accessUser)
		huma.Register(a.huma, op, func(ctx context.Context, in *hashInput) (*torrentOutput, error) {
			if err := act.fn(ctx, in.Hash); err != nil {
				return nil, apiError(err)
			}
			st, _ := a.Engine.Get(in.Hash)
			return &torrentOutput{Body: st}, nil
		})
	}

	op = a.operation(http.MethodPost, "/torrents/actions", "bulkTorrentAction", "Act on several torrents", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *bulkInput) (*struct{}, error) {
		return nil, apiError(a.bulk(ctx, in.Body))
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/torrents/{hash}/files", "listTorrentFiles", "List a torrent's files", tag, accessUser),
		func(_ context.Context, in *hashInput) (*filesOutput, error) {
			files, err := a.Engine.Files(in.Hash)
			if err != nil {
				return nil, apiError(err)
			}
			out := &filesOutput{}
			out.Body.Items = files
			return out, nil
		})

	op = a.operation(http.MethodPatch, "/torrents/{hash}/files", "setTorrentFilePriority", "Set file priorities", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *setFilesInput) (*struct{}, error) {
		return nil, apiError(a.Engine.SetFilePriority(ctx, in.Hash, in.Body.Indices, in.Body.Priority))
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/torrents/{hash}/peers", "listTorrentPeers", "List a torrent's connected peers", tag, accessUser),
		func(_ context.Context, in *hashInput) (*peersOutput, error) {
			peers, err := a.Engine.Peers(in.Hash)
			if err != nil {
				return nil, apiError(err)
			}
			out := &peersOutput{}
			out.Body.Items = peers
			return out, nil
		})

	op = a.operation(http.MethodPost, "/torrents/{hash}/peers", "addTorrentPeers", "Connect a torrent to peers", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *addPeersInput) (*struct{}, error) {
		return nil, apiError(a.Engine.AddPeers(ctx, in.Body.Addresses, in.Hash))
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/torrents/{hash}/trackers", "listTorrentTrackers", "List a torrent's trackers", tag, accessUser),
		func(_ context.Context, in *hashInput) (*trackersOutput, error) {
			trackers, err := a.Engine.Trackers(in.Hash)
			if err != nil {
				return nil, apiError(err)
			}
			out := &trackersOutput{}
			out.Body.Items = trackers
			return out, nil
		})

	huma.Register(a.huma, a.operation(http.MethodGet, "/torrents/{hash}/pieces", "getTorrentPieces", "Get a torrent's piece states", tag, accessUser),
		func(_ context.Context, in *hashInput) (*piecesOutput, error) {
			states, err := a.Engine.Pieces(in.Hash)
			if err != nil {
				return nil, apiError(err)
			}
			out := &piecesOutput{}
			out.Body.States = states
			return out, nil
		})
}

func (a *API) listTorrents(in *listTorrentsInput) (*listTorrentsOutput, error) {
	all := a.Engine.List()
	search := strings.ToLower(in.Search)
	items := all[:0:0]
	for _, t := range all {
		if MatchesFilter(t, in.Filter) &&
			(in.Category == "" || t.Category == in.Category) &&
			(!in.Uncategorized || t.Category == "") &&
			(in.Tag == "" || slices.Contains(t.Tags, in.Tag)) &&
			(!in.Untagged || len(t.Tags) == 0) &&
			(search == "" || strings.Contains(strings.ToLower(t.Name), search)) &&
			(len(in.Hashes) == 0 || slices.Contains(in.Hashes, t.Hash)) {
			items = append(items, t)
		}
	}
	SortTorrents(items, in.Sort, in.Reverse)

	offset := 0
	if in.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err == nil {
			offset, err = strconv.Atoi(string(b))
		}
		if err != nil || offset < 0 {
			return nil, huma.Error422UnprocessableEntity("bad cursor")
		}
	}
	out := &listTorrentsOutput{Body: TorrentList{Total: len(items)}}
	offset = min(offset, len(items))
	end := len(items)
	if in.Limit > 0 && offset+in.Limit < end {
		end = offset + in.Limit
		out.Body.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	out.Body.Items = items[offset:end]
	return out, nil
}

// MatchesFilter reports whether a torrent matches a list filter.
func MatchesFilter(t engine.Torrent, filter string) bool {
	switch filter {
	case FilterDownloading:
		return !t.Complete && !t.Stopped && t.State != engine.StateError
	case FilterSeeding:
		return t.State == engine.StateSeeding || t.State == engine.StateStalledSeeding
	case FilterCompleted:
		return t.Complete
	case FilterStopped:
		return t.Stopped
	case FilterActive:
		return t.DownRate > 0 || t.UpRate > 0
	case FilterInactive:
		return t.DownRate == 0 && t.UpRate == 0
	case FilterStalled:
		return t.State == engine.StateStalledDownloading || t.State == engine.StateStalledSeeding
	case FilterChecking:
		return t.State == engine.StateChecking || t.State == engine.StateQueuedForChecking
	case FilterErrored:
		return t.State == engine.StateError
	default:
		return true
	}
}

// SortTorrents sorts by a field name, ties broken by hash.
func SortTorrents(items []engine.Torrent, field string, reverse bool) {
	key := func(a, b engine.Torrent) int {
		switch field {
		case "name":
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "completedAt":
			return cmp.Compare(a.CompletedAt, b.CompletedAt)
		case "size":
			return cmp.Compare(a.Size, b.Size)
		case "progress":
			return cmp.Compare(a.Progress, b.Progress)
		case "state":
			return strings.Compare(string(a.State), string(b.State))
		case "downloadRate":
			return cmp.Compare(a.DownRate, b.DownRate)
		case "uploadRate":
			return cmp.Compare(a.UpRate, b.UpRate)
		case "eta":
			return cmp.Compare(uint64(a.ETA), uint64(b.ETA)) //nolint:gosec // -1 (unknown) sorts last
		case "ratio":
			return cmp.Compare(a.Ratio, b.Ratio)
		case "seeds":
			return cmp.Compare(a.Seeds, b.Seeds)
		case "peers":
			return cmp.Compare(a.Peers, b.Peers)
		case "category":
			return strings.Compare(a.Category, b.Category)
		case "uploaded":
			return cmp.Compare(a.Uploaded, b.Uploaded)
		case "downloaded":
			return cmp.Compare(a.Downloaded, b.Downloaded)
		default:
			return cmp.Compare(a.AddedAt, b.AddedAt)
		}
	}
	slices.SortStableFunc(items, func(a, b engine.Torrent) int {
		c := key(a, b)
		if reverse {
			c = -c
		}
		return cmp.Or(c, strings.Compare(a.Hash, b.Hash))
	})
}

func (a *API) addTorrent(ctx context.Context, req AddTorrentRequest) (*addTorrentOutput, error) {
	sources := 0
	for _, set := range []bool{req.Magnet != "", req.URL != "", len(req.TorrentFile) > 0} {
		if set {
			sources++
		}
	}
	if sources != 1 {
		return nil, huma.Error422UnprocessableEntity("give exactly one of magnet, url and torrentFile")
	}
	p := engine.AddParams{
		Magnet: req.Magnet, Metainfo: req.TorrentFile, SavePath: req.SavePath, Category: req.Category,
		Tags: req.Tags, Stopped: req.Stopped, Sequential: req.Sequential, FirstLastPiece: req.FirstLastPiece,
		Name: req.Name, RatioLimit: req.RatioLimit, SeedingTimeLimit: req.SeedingTimeLimit,
		InactiveSeedingTimeLimit: req.InactiveSeedingTimeLimit, SkipChecking: req.SkipChecking,
		ContentLayout: req.ContentLayout,
	}
	if req.URL != "" {
		res, err := a.Fetcher.Get(ctx, req.URL)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("fetching the URL: %v", err))
		}
		if res.Magnet != "" {
			p.Magnet = res.Magnet
		} else {
			p.Magnet, p.Metainfo, err = engine.ParseSource(res.Body)
			if err != nil {
				return nil, apiError(err)
			}
		}
	}
	hash, err := a.Engine.Add(ctx, p)
	if errors.Is(err, engine.ErrExists) {
		return nil, huma.Error409Conflict("torrent already added: " + hash)
	}
	if err != nil {
		return nil, apiError(err)
	}
	st, _ := a.Engine.Get(hash)
	return &addTorrentOutput{Status: http.StatusCreated, Body: st}, nil
}

func (a *API) updateTorrent(ctx context.Context, hash string, u UpdateTorrentRequest) error {
	if _, ok := a.Engine.Get(hash); !ok {
		return fmt.Errorf("%w: torrent %s", engine.ErrNotFound, hash)
	}
	e := a.Engine
	var errs []error
	if u.Name != nil {
		errs = append(errs, e.Rename(ctx, hash, *u.Name))
	}
	if u.Category != nil {
		errs = append(errs, e.SetCategory(ctx, *u.Category, hash))
	}
	if u.Tags != nil {
		st, _ := e.Get(hash)
		var remove []string
		for _, t := range st.Tags {
			if !slices.Contains(*u.Tags, t) {
				remove = append(remove, t)
			}
		}
		if len(remove) > 0 {
			errs = append(errs, e.RemoveTags(ctx, remove, hash))
		}
		if len(*u.Tags) > 0 {
			errs = append(errs, e.AddTags(ctx, *u.Tags, hash))
		}
	}
	if u.Sequential != nil {
		errs = append(errs, e.SetSequential(ctx, *u.Sequential, hash))
	}
	if u.FirstLastPiece != nil {
		errs = append(errs, e.SetFirstLastPiece(ctx, *u.FirstLastPiece, hash))
	}
	if u.RatioLimit != nil || u.SeedingTimeLimit != nil || u.InactiveSeedingTimeLimit != nil {
		st, _ := e.Get(hash)
		l := engine.ShareLimits{
			RatioLimit: st.RatioLimit, SeedingTimeLimit: st.SeedingTimeLimit, InactiveSeedingTimeLimit: st.InactiveSeedingTimeLimit,
		}
		if u.RatioLimit != nil {
			l.RatioLimit = *u.RatioLimit
		}
		if u.SeedingTimeLimit != nil {
			l.SeedingTimeLimit = *u.SeedingTimeLimit
		}
		if u.InactiveSeedingTimeLimit != nil {
			l.InactiveSeedingTimeLimit = *u.InactiveSeedingTimeLimit
		}
		errs = append(errs, e.SetShareLimits(ctx, l, hash))
	}
	if u.Comment != nil {
		errs = append(errs, e.SetComment(ctx, *u.Comment, hash))
	}
	if u.Forced != nil {
		errs = append(errs, e.SetForced(ctx, *u.Forced, hash))
	}
	if u.SavePath != nil {
		if st, _ := e.Get(hash); st.SavePath != *u.SavePath {
			errs = append(errs, e.Move(ctx, hash, *u.SavePath))
		}
	}
	if u.Stopped != nil {
		if *u.Stopped {
			errs = append(errs, e.Stop(ctx, hash))
		} else {
			errs = append(errs, e.Start(ctx, hash))
		}
	}
	return errors.Join(errs...)
}

func (a *API) bulk(ctx context.Context, b BulkAction) error {
	e := a.Engine
	h := b.Hashes
	switch b.Action {
	case "start":
		return e.Start(ctx, h...)
	case "stop":
		return e.Stop(ctx, h...)
	case "forceStart":
		return e.SetForced(ctx, true, h...)
	case "recheck":
		return e.Recheck(ctx, h...)
	case "reannounce":
		return e.Reannounce(ctx, h...)
	case "remove":
		return e.Remove(ctx, b.DeleteData, h...)
	case "setCategory":
		return e.SetCategory(ctx, b.Category, h...)
	case "addTags":
		return e.AddTags(ctx, b.Tags, h...)
	case "removeTags":
		return e.RemoveTags(ctx, b.Tags, h...)
	case "move":
		if len(h) == 1 && h[0] == "all" {
			h = nil
			for _, t := range e.List() {
				h = append(h, t.Hash)
			}
		}
		var errs []error
		for _, hash := range h {
			errs = append(errs, e.Move(ctx, hash, b.SavePath))
		}
		return errors.Join(errs...)
	case "queueTop", "queueBottom", "queueUp", "queueDown":
		if !a.Settings.Get().QueueingEnabled {
			return fmt.Errorf("%w: queueing is off", engine.ErrInvalid)
		}
		return e.QueueMove(ctx, strings.ToLower(strings.TrimPrefix(b.Action, "queue")), h...)
	default:
		return fmt.Errorf("%w: unknown action %q", engine.ErrInvalid, b.Action)
	}
}
