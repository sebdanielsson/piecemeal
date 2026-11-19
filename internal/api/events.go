package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2/sse"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

// SnapshotEvent is the first event on a stream: the full state the
// following events change.
type SnapshotEvent struct {
	Torrents   []engine.Torrent      `json:"torrents"`
	Transfer   engine.TransferStatus `json:"transfer"`
	Categories []engine.Category     `json:"categories"`
	Tags       []string              `json:"tags"`
}

// TorrentsEvent carries torrents that were added or changed, and the hashes
// of removed ones, since the previous event.
type TorrentsEvent struct {
	Updated []engine.Torrent `json:"updated"`
	Removed []string         `json:"removed"`
}

// TransferEvent is the new transfer status.
type TransferEvent engine.TransferStatus

// CategoriesEvent is the new list of categories.
type CategoriesEvent struct {
	Items []engine.Category `json:"items"`
}

// TagsEvent is the new list of tags.
type TagsEvent struct {
	Items []string `json:"items"`
}

// SettingsEvent is the new settings, as GET /settings returns them.
type SettingsEvent SettingsResponse

func (a *API) registerEvents() {
	op := a.operation(http.MethodGet, "/events", "streamEvents", "Stream live changes", "Events", accessUser)
	op.Description = "Server-sent events. The first event is a snapshot of the torrents, transfer status, " +
		"categories and tags; the rest are changes to it. When the server drops a slow client the stream " +
		"ends, and reconnecting starts again from a snapshot."
	sse.Register(a.huma, op, map[string]any{
		"snapshot":   SnapshotEvent{},
		"torrents":   TorrentsEvent{},
		"transfer":   TransferEvent{},
		"categories": CategoriesEvent{},
		"tags":       TagsEvent{},
		"settings":   SettingsEvent{},
	}, func(ctx context.Context, _ *struct{}, send sse.Sender) {
		a.stream(ctx, send)
	})
}

func (a *API) stream(ctx context.Context, send sse.Sender) {
	// Subscribe before the snapshot so nothing between the two is lost.
	sub := a.Engine.Events().Subscribe(4096)
	defer sub.Close()
	if err := send.Data(SnapshotEvent{
		Torrents: a.Engine.List(), Transfer: a.Engine.Transfer(), Categories: a.Engine.Categories(), Tags: a.Engine.Tags(),
	}); err != nil {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	// Coalesce bursts (every torrent changes on every tick) into one event.
	flush := time.NewTicker(250 * time.Millisecond)
	defer flush.Stop()
	pending := map[string]*engine.Torrent{}
	removed := map[string]struct{}{}
	var transfer *engine.TransferStatus
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.done:
			return
		case <-heartbeat.C:
			if err := send.Comment("ping"); err != nil {
				return
			}
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			switch ev.Type {
			case engine.EventTorrent:
				pending[ev.Torrent.Hash] = ev.Torrent
				delete(removed, ev.Torrent.Hash)
			case engine.EventTorrentRemoved:
				delete(pending, ev.Hash)
				removed[ev.Hash] = struct{}{}
			case engine.EventTransfer:
				transfer = ev.Transfer
			case engine.EventCategories:
				if send.Data(CategoriesEvent{Items: a.Engine.Categories()}) != nil {
					return
				}
			case engine.EventTags:
				if send.Data(TagsEvent{Items: a.Engine.Tags()}) != nil {
					return
				}
			case engine.EventSettings:
				if send.Data(SettingsEvent(a.settingsResponse(a.Settings.Get()))) != nil {
					return
				}
			}
		case <-flush.C:
			if len(pending) > 0 || len(removed) > 0 {
				ev := TorrentsEvent{Updated: make([]engine.Torrent, 0, len(pending)), Removed: make([]string, 0, len(removed))}
				for _, st := range pending {
					ev.Updated = append(ev.Updated, *st)
				}
				for h := range removed {
					ev.Removed = append(ev.Removed, h)
				}
				clear(pending)
				clear(removed)
				if send.Data(ev) != nil {
					return
				}
			}
			if transfer != nil {
				if send.Data(TransferEvent(*transfer)) != nil {
					return
				}
				transfer = nil
			}
		}
	}
}
