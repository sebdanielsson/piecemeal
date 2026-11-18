package api

import (
	"context"
	"mime"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type setTrackersInput struct {
	Hash string `path:"hash" pattern:"^[0-9a-f]{40}$" doc:"Infohash, lowercase hex."`
	Body struct {
		Tiers [][]string `json:"tiers" doc:"Announce URLs by tier, in order. Empty tiers are dropped."`
	}
}

type webSeedsBody struct {
	URLs []string `json:"urls" doc:"BEP 19 web seed URLs."`
}

type webSeedsOutput struct{ Body webSeedsBody }

type setWebSeedsInput struct {
	Hash string `path:"hash" pattern:"^[0-9a-f]{40}$" doc:"Infohash, lowercase hex."`
	Body webSeedsBody
}

type renamePathInput struct {
	Hash string `path:"hash" pattern:"^[0-9a-f]{40}$" doc:"Infohash, lowercase hex."`
	Body struct {
		OldPath string `json:"oldPath" minLength:"1" doc:"A file's or folder's path as listTorrentFiles reports it."`
		NewPath string `json:"newPath" minLength:"1"`
	}
}

type torrentFileOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

func (a *API) registerEditing() {
	const tag = "Torrents"
	huma.Register(a.huma, a.operation(http.MethodPut, "/torrents/{hash}/trackers", "setTorrentTrackers", "Replace a torrent's trackers", tag, accessUser),
		func(ctx context.Context, in *setTrackersInput) (*trackersOutput, error) {
			if err := a.Engine.SetTrackers(ctx, in.Hash, in.Body.Tiers); err != nil {
				return nil, apiError(err)
			}
			trackers, err := a.Engine.Trackers(in.Hash)
			if err != nil {
				return nil, apiError(err)
			}
			out := &trackersOutput{}
			out.Body.Items = trackers
			return out, nil
		})

	huma.Register(a.huma, a.operation(http.MethodGet, "/torrents/{hash}/web-seeds", "listTorrentWebSeeds", "List a torrent's web seeds", tag, accessUser),
		func(_ context.Context, in *hashInput) (*webSeedsOutput, error) {
			urls, err := a.Engine.WebSeeds(in.Hash)
			if err != nil {
				return nil, apiError(err)
			}
			return &webSeedsOutput{Body: webSeedsBody{URLs: urls}}, nil
		})

	huma.Register(a.huma, a.operation(http.MethodPut, "/torrents/{hash}/web-seeds", "setTorrentWebSeeds", "Replace a torrent's web seeds", tag, accessUser),
		func(ctx context.Context, in *setWebSeedsInput) (*webSeedsOutput, error) {
			if err := a.Engine.SetWebSeeds(ctx, in.Hash, in.Body.URLs); err != nil {
				return nil, apiError(err)
			}
			urls, _ := a.Engine.WebSeeds(in.Hash)
			return &webSeedsOutput{Body: webSeedsBody{URLs: urls}}, nil
		})

	op := a.operation(http.MethodPost, "/torrents/{hash}/files/rename", "renameTorrentPath", "Rename a file or folder", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	op.Description = "Renames the file or folder on disk too. Paths are relative to the save path with / separators."
	huma.Register(a.huma, op, func(_ context.Context, in *renamePathInput) (*struct{}, error) {
		return nil, apiError(a.Engine.RenamePath(in.Hash, in.Body.OldPath, in.Body.NewPath))
	})

	op = a.operation(http.MethodGet, "/torrents/{hash}/torrent-file", "getTorrentFile", "Download a torrent's .torrent file", tag, accessUser)
	op.Description = "With the torrent's current trackers and web seeds. 422 until the metadata is known."
	op.Responses = map[string]*huma.Response{"200": {
		Description: "The .torrent file.",
		Content:     map[string]*huma.MediaType{"application/x-bittorrent": {Schema: &huma.Schema{Type: "string", Format: "binary"}}},
	}}
	huma.Register(a.huma, op, func(_ context.Context, in *hashInput) (*torrentFileOutput, error) {
		t, ok := a.Engine.Get(in.Hash)
		if !ok {
			return nil, huma.Error404NotFound("no such torrent")
		}
		b, err := a.Engine.TorrentFile(in.Hash)
		if err != nil {
			return nil, apiError(err)
		}
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": t.Name + ".torrent"})
		if disposition == "" {
			disposition = mime.FormatMediaType("attachment", map[string]string{"filename": t.Hash + ".torrent"})
		}
		return &torrentFileOutput{ContentType: "application/x-bittorrent", ContentDisposition: disposition, Body: b}, nil
	})
}
