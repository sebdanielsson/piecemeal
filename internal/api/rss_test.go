package api

import (
	"net/http"
	"testing"

	"github.com/sebdanielsson/piecemeal/internal/rss"
)

func TestRSSEndpoints(t *testing.T) {
	s := newTestServer(t)
	s.login()
	s.do(http.MethodPost, "/rss/folders", map[string]string{"path": "TV"}, http.StatusNoContent, nil, csrf...)
	s.do(http.MethodPost, "/rss/folders", map[string]string{"path": "TV"}, http.StatusConflict, nil, csrf...)
	var f rss.FeedInfo
	s.do(http.MethodPost, "/rss/feeds", map[string]any{"url": "https://feeds.example/a.xml", "path": `TV\A`}, http.StatusCreated, &f, csrf...)
	s.do(http.MethodPost, "/rss/feeds", map[string]any{"url": "https://feeds.example/b.xml", "path": `Missing\B`}, http.StatusUnprocessableEntity, nil, csrf...)
	var tree RSSTree
	s.do(http.MethodGet, "/rss", nil, http.StatusOK, &tree)
	if len(tree.Folders) != 1 || len(tree.Feeds) != 1 || tree.Feeds[0].UID != f.UID {
		t.Fatalf("tree: %+v", tree)
	}
	interval := 600
	path := "Renamed"
	s.do(http.MethodPatch, "/rss/feeds/"+f.UID, map[string]any{"path": path, "refreshInterval": interval}, http.StatusOK, &f, csrf...)
	if f.Path != "Renamed" || f.RefreshInterval != 600 {
		t.Fatalf("after update: %+v", f)
	}
	s.do(http.MethodGet, "/rss/feeds/nope", nil, http.StatusNotFound, nil)

	rule := rss.NewRule()
	rule.MustContain, rule.AffectedFeeds = "Show", []string{"https://feeds.example/a.xml"}
	s.do(http.MethodPut, "/rss/rules/Show", rule, http.StatusNoContent, nil, csrf...)
	var rules struct{ Items map[string]rss.Rule }
	s.do(http.MethodGet, "/rss/rules", nil, http.StatusOK, &rules)
	if r := rules.Items["Show"]; r.MustContain != "Show" || !r.Enabled || r.Add.RatioLimit != -2 {
		t.Fatalf("rules: %+v", rules)
	}
	s.do(http.MethodPost, "/rss/rules/Show/rename", map[string]string{"newName": "Shows"}, http.StatusNoContent, nil, csrf...)
	s.do(http.MethodPost, "/rss/rules/Nope/rename", map[string]string{"newName": "X"}, http.StatusNotFound, nil, csrf...)
	var matches struct{ Items []rss.Match }
	s.do(http.MethodGet, "/rss/rules/Shows/matches", nil, http.StatusOK, &matches)
	s.do(http.MethodPost, "/rss/read", map[string]string{"path": "Nope"}, http.StatusNotFound, nil, csrf...)
	s.do(http.MethodPost, "/rss/refresh?path=Renamed", nil, http.StatusNoContent, nil, csrf...)
	s.do(http.MethodDelete, "/rss/rules/Shows", nil, http.StatusNoContent, nil, csrf...)
	s.do(http.MethodDelete, "/rss/items?path=TV", nil, http.StatusNoContent, nil, csrf...)
	s.do(http.MethodDelete, "/rss/items?path=TV", nil, http.StatusNotFound, nil, csrf...)
}
