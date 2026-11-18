package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/sebdanielsson/piecemeal/internal/rss"
)

// RSSTree is the RSS folders and feeds.
type RSSTree struct {
	Folders []string       `json:"folders" doc:"Folder paths; names are separated by backslashes."`
	Feeds   []rss.FeedInfo `json:"feeds" doc:"Without articles."`
}

type rssTreeOutput struct{ Body RSSTree }

type rssFeedOutput struct {
	Status int
	Body   rss.FeedInfo
}

type rssFolderInput struct {
	Body struct {
		Path string `json:"path" minLength:"1"`
	}
}

type addRSSFeedInput struct {
	Body struct {
		URL             string `json:"url" minLength:"1"`
		Path            string `json:"path,omitempty" doc:"Folder and name. Empty names the feed after its title once fetched."`
		RefreshInterval int    `json:"refreshInterval,omitempty" minimum:"0" doc:"Seconds; 0 follows the rssRefreshInterval setting."`
	}
}

type rssFeedInput struct {
	UID string `path:"uid"`
}

type updateRSSFeedInput struct {
	UID  string `path:"uid"`
	Body struct {
		Path            *string `json:"path,omitempty" doc:"Moves or renames the feed."`
		URL             *string `json:"url,omitempty"`
		RefreshInterval *int    `json:"refreshInterval,omitempty" minimum:"0"`
	}
}

type rssPathInput struct {
	Path string `query:"path" doc:"A feed or folder; empty for every feed."`
}

type markReadInput struct {
	Body struct {
		Path      string `json:"path" doc:"A feed, a folder, or empty for every feed."`
		ArticleID string `json:"articleId,omitempty" doc:"One article; empty marks them all."`
	}
}

type rssRulesOutput struct {
	Body struct {
		Items map[string]rss.Rule `json:"items"`
	}
}

type putRSSRuleInput struct {
	Name string `path:"name" maxLength:"200"`
	Body rss.Rule
}

type renameRSSRuleInput struct {
	Name string `path:"name" maxLength:"200"`
	Body struct {
		NewName string `json:"newName" minLength:"1" maxLength:"200"`
	}
}

type rssMatchesOutput struct {
	Body struct {
		Items []rss.Match `json:"items"`
	}
}

func rssError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, rss.ErrNotFound), errors.Is(err, rss.ErrNoFeed):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, rss.ErrExists), errors.Is(err, rss.ErrURLExists):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, new(*rss.PathError)):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return apiError(err)
	}
}

func (a *API) registerRSS() {
	const tag = "RSS"
	huma.Register(a.huma, a.operation(http.MethodGet, "/rss", "getRSS", "List RSS folders and feeds", tag, accessUser),
		func(context.Context, *struct{}) (*rssTreeOutput, error) {
			out := &rssTreeOutput{Body: RSSTree{Folders: a.RSS.Folders(), Feeds: a.RSS.Feeds(false)}}
			if out.Body.Folders == nil {
				out.Body.Folders = []string{}
			}
			if out.Body.Feeds == nil {
				out.Body.Feeds = []rss.FeedInfo{}
			}
			return out, nil
		})

	op := a.operation(http.MethodPost, "/rss/folders", "addRSSFolder", "Add an RSS folder", tag, accessAdmin)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *rssFolderInput) (*struct{}, error) {
		return nil, rssError(a.RSS.AddFolder(ctx, in.Body.Path))
	})

	op = a.operation(http.MethodPost, "/rss/feeds", "addRSSFeed", "Add an RSS feed", tag, accessAdmin)
	op.DefaultStatus = http.StatusCreated
	huma.Register(a.huma, op, func(ctx context.Context, in *addRSSFeedInput) (*rssFeedOutput, error) {
		f, err := a.RSS.AddFeed(ctx, in.Body.URL, in.Body.Path, in.Body.RefreshInterval)
		if err != nil {
			return nil, rssError(err)
		}
		return &rssFeedOutput{Status: http.StatusCreated, Body: f}, nil
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/rss/feeds/{uid}", "getRSSFeed", "Get an RSS feed with its articles", tag, accessUser),
		func(_ context.Context, in *rssFeedInput) (*rssFeedOutput, error) {
			f, ok := a.RSS.Feed(in.UID)
			if !ok {
				return nil, huma.Error404NotFound("no such feed")
			}
			return &rssFeedOutput{Status: http.StatusOK, Body: f}, nil
		})

	huma.Register(a.huma, a.operation(http.MethodPatch, "/rss/feeds/{uid}", "updateRSSFeed", "Change an RSS feed", tag, accessAdmin),
		func(ctx context.Context, in *updateRSSFeedInput) (*rssFeedOutput, error) {
			path, ok := a.RSS.FeedPath(in.UID)
			if !ok {
				return nil, huma.Error404NotFound("no such feed")
			}
			if in.Body.URL != nil {
				if err := a.RSS.SetFeedURL(ctx, path, *in.Body.URL); err != nil {
					return nil, rssError(err)
				}
			}
			if in.Body.RefreshInterval != nil {
				if err := a.RSS.SetFeedRefreshInterval(ctx, path, *in.Body.RefreshInterval); err != nil {
					return nil, rssError(err)
				}
			}
			if in.Body.Path != nil {
				if err := a.RSS.MoveItem(ctx, path, *in.Body.Path); err != nil {
					return nil, rssError(err)
				}
			}
			f, _ := a.RSS.Feed(in.UID)
			return &rssFeedOutput{Status: http.StatusOK, Body: f}, nil
		})

	op = a.operation(http.MethodDelete, "/rss/items", "removeRSSItem", "Remove an RSS feed, or a folder with everything in it", tag, accessAdmin)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *rssPathInput) (*struct{}, error) {
		return nil, rssError(a.RSS.RemoveItem(ctx, in.Path))
	})

	op = a.operation(http.MethodPost, "/rss/refresh", "refreshRSS", "Fetch RSS feeds now", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(_ context.Context, in *rssPathInput) (*struct{}, error) {
		a.RSS.Refresh(in.Path)
		return nil, nil
	})

	op = a.operation(http.MethodPost, "/rss/read", "markRSSRead", "Mark RSS articles read", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *markReadInput) (*struct{}, error) {
		if !a.RSS.MarkAsRead(ctx, in.Body.Path, in.Body.ArticleID) {
			return nil, huma.Error404NotFound("no such feed or folder")
		}
		return nil, nil
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/rss/rules", "listRSSRules", "List RSS download rules", tag, accessUser),
		func(context.Context, *struct{}) (*rssRulesOutput, error) {
			out := &rssRulesOutput{}
			out.Body.Items = a.RSS.Rules()
			return out, nil
		})

	op = a.operation(http.MethodPut, "/rss/rules/{name}", "putRSSRule", "Create or replace an RSS download rule", tag, accessAdmin)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *putRSSRuleInput) (*struct{}, error) {
		a.RSS.SetRule(ctx, in.Name, in.Body)
		return nil, nil
	})

	op = a.operation(http.MethodDelete, "/rss/rules/{name}", "removeRSSRule", "Remove an RSS download rule", tag, accessAdmin)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *nameInput) (*struct{}, error) {
		a.RSS.RemoveRule(ctx, in.Name)
		return nil, nil
	})

	op = a.operation(http.MethodPost, "/rss/rules/{name}/rename", "renameRSSRule", "Rename an RSS download rule", tag, accessAdmin)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *renameRSSRuleInput) (*struct{}, error) {
		if _, ok := a.RSS.Rules()[in.Name]; !ok {
			return nil, huma.Error404NotFound("no such rule")
		}
		if _, ok := a.RSS.Rules()[in.Body.NewName]; ok {
			return nil, huma.Error409Conflict("a rule with that name exists")
		}
		a.RSS.RenameRule(ctx, in.Name, in.Body.NewName)
		return nil, nil
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/rss/rules/{name}/matches", "listRSSRuleMatches", "List the articles an RSS rule accepts", tag, accessUser),
		func(_ context.Context, in *nameInput) (*rssMatchesOutput, error) {
			out := &rssMatchesOutput{}
			out.Body.Items = a.RSS.Matches(in.Name)
			return out, nil
		})
}
