package qbit

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// The expectations below are qBittorrent 5.3.0's, observed through its
// WebAPI.

func (e *env) addMagnet(hash string, form url.Values) {
	e.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	form.Set("urls", "magnet:?xt=urn:btih:"+hash)
	form.Set("stopped", "true")
	expect(e.t, "add "+hash, e.post("torrents/add", form), http.StatusOK)
}

// paths is a torrent's save path, download path and automatic management,
// checked against info and properties.
func (e *env) paths(hash string) (save, download string, auto bool) {
	e.t.Helper()
	t := e.info(hash)
	var props map[string]any
	e.get("torrents/properties?hash="+hash).json(e.t, &props)
	if props["save_path"] != t.SavePath || props["download_path"] != t.DownloadPath {
		e.t.Fatalf("properties %v differ from info %+v", props, t)
	}
	return t.SavePath, t.DownloadPath, t.AutoTMM
}

func (e *env) wantPaths(what, hash, save, download string, auto bool) {
	e.t.Helper()
	s, d, a := e.paths(hash)
	if s != save || d != download || a != auto {
		e.t.Fatalf("%s: save %q, download %q, auto %v; want %q, %q, %v", what, s, d, a, save, download, auto)
	}
}

func (e *env) setPrefs(prefs string) {
	e.t.Helper()
	expect(e.t, "setPreferences "+prefs, e.post("app/setPreferences", url.Values{"json": {prefs}}), http.StatusOK)
}

func (e *env) categoryDownloadPath(name string) any {
	e.t.Helper()
	var cats map[string]map[string]any
	e.get("torrents/categories").json(e.t, &cats)
	c, ok := cats[name]
	if !ok {
		e.t.Fatalf("no category %s: %v", name, cats)
	}
	return c["download_path"]
}

func TestTempPathPreferences(t *testing.T) {
	e := newEnv(t)
	e.login()
	var prefs map[string]any
	e.get("app/preferences").json(t, &prefs)
	if prefs["temp_path_enabled"] != false || prefs["temp_path"] != filepath.Join(e.save, "temp") ||
		prefs["auto_tmm_enabled"] != false || prefs["torrent_changed_tmm_enabled"] != true ||
		prefs["save_path_changed_tmm_enabled"] != false || prefs["category_changed_tmm_enabled"] != false {
		t.Fatalf("defaults: %v", prefs)
	}
	// A relative or empty temp_path is inside the temp folder in the save
	// path, the new one when it changes in the same request.
	for _, tc := range []struct{ json, want string }{
		{`{"temp_path":"rel"}`, filepath.Join(e.save, "temp", "rel")},
		{`{"temp_path":""}`, filepath.Join(e.save, "temp")},
		{`{"temp_path":"` + e.save + `/abs"}`, filepath.Join(e.save, "abs")},
		{`{"save_path":"` + e.save + `/sv","temp_path":"x"}`, filepath.Join(e.save, "sv", "temp", "x")},
	} {
		e.setPrefs(tc.json)
		e.get("app/preferences").json(t, &prefs)
		if prefs["temp_path"] != tc.want {
			t.Errorf("%s: temp_path %v, want %s", tc.json, prefs["temp_path"], tc.want)
		}
	}
	e.setPrefs(`{"temp_path_enabled":true,"auto_tmm_enabled":true,"torrent_changed_tmm_enabled":false,` +
		`"save_path_changed_tmm_enabled":true,"category_changed_tmm_enabled":true}`)
	e.get("app/preferences").json(t, &prefs)
	if prefs["temp_path_enabled"] != true || prefs["auto_tmm_enabled"] != true || prefs["torrent_changed_tmm_enabled"] != false ||
		prefs["save_path_changed_tmm_enabled"] != true || prefs["category_changed_tmm_enabled"] != true {
		t.Fatalf("after setting them: %v", prefs)
	}
	r := e.post("app/setPreferences", url.Values{"json": {`{"temp_path":"/etc"}`}})
	if r.status != http.StatusBadRequest {
		t.Fatalf("a temp path outside the roots: %d %s", r.status, r.body)
	}
}

func TestCategoryDownloadPath(t *testing.T) {
	e := newEnv(t)
	e.login()
	create := func(name string, form url.Values) {
		t.Helper()
		form.Set("category", name)
		expect(t, "createCategory "+name, e.post("torrents/createCategory", form), http.StatusOK)
	}
	create("global", url.Values{})
	create("off", url.Values{"downloadPathEnabled": {"false"}})
	create("own", url.Values{"downloadPathEnabled": {"true"}, "downloadPath": {e.save + "/inc"}})
	create("named", url.Values{"downloadPathEnabled": {"true"}})
	create("ignored", url.Values{"downloadPath": {e.save + "/x"}})
	create("relative", url.Values{"savePath": {"rel/sp"}, "downloadPathEnabled": {"true"}, "downloadPath": {"rel/dl"}})
	for name, want := range map[string]any{
		"global": nil, "off": false, "own": e.save + "/inc", "named": "", "ignored": nil, "relative": "rel/dl",
	} {
		if got := e.categoryDownloadPath(name); got != want {
			t.Errorf("%s: download_path %#v, want %#v", name, got, want)
		}
	}
	var cats map[string]map[string]any
	e.get("torrents/categories").json(t, &cats)
	if cats["relative"]["savePath"] != "rel/sp" {
		t.Fatalf("a relative save path is kept as it is: %v", cats["relative"])
	}

	// editCategory: without downloadPathEnabled the download path stays,
	// even with a downloadPath; empty follows the global setting again.
	edit := func(form url.Values) {
		t.Helper()
		form.Set("category", "own")
		expect(t, "editCategory", e.post("torrents/editCategory", form), http.StatusOK)
	}
	edit(url.Values{"savePath": {e.save + "/own"}})
	edit(url.Values{"downloadPath": {e.save + "/other"}})
	if got := e.categoryDownloadPath("own"); got != e.save+"/inc" {
		t.Fatalf("after edits without downloadPathEnabled: %#v", got)
	}
	edit(url.Values{"downloadPathEnabled": {""}, "downloadPath": {e.save + "/other"}})
	if got := e.categoryDownloadPath("own"); got != nil {
		t.Fatalf("after downloadPathEnabled empty: %#v", got)
	}
	edit(url.Values{"downloadPathEnabled": {"false"}})
	if got := e.categoryDownloadPath("own"); got != false {
		t.Fatalf("after downloadPathEnabled false: %#v", got)
	}
	r := e.post("torrents/createCategory", url.Values{"category": {"bad"}, "downloadPathEnabled": {"true"}, "downloadPath": {"/etc"}})
	if r.status != http.StatusConflict {
		t.Fatalf("a download path outside the roots: %d %s", r.status, r.body)
	}
	// sync/maindata reports the same.
	var main struct {
		Categories map[string]map[string]any `json:"categories"`
	}
	e.get("sync/maindata?rid=0").json(t, &main)
	if main.Categories["named"]["download_path"] != "" || main.Categories["off"]["download_path"] != false {
		t.Fatalf("maindata categories: %v", main.Categories)
	}
}

func TestAddPaths(t *testing.T) {
	e := newEnv(t)
	e.login()
	s, temp := e.save, filepath.Join(e.save, "temp")
	expect(t, "createCategory", e.post("torrents/createCategory", url.Values{"category": {"c2"}, "savePath": {s + "/c2path"}}), http.StatusOK)
	expect(t, "createCategory", e.post("torrents/createCategory", url.Values{"category": {"c3"}, "savePath": {"relative/c3"}}), http.StatusOK)
	expect(t, "createCategory", e.post("torrents/createCategory", url.Values{
		"category": {"c4"}, "savePath": {s + "/c4save"}, "downloadPathEnabled": {"true"}, "downloadPath": {s + "/inc4"},
	}), http.StatusOK)
	expect(t, "createCategory", e.post("torrents/createCategory", url.Values{"category": {"c5"}, "downloadPathEnabled": {"false"}}), http.StatusOK)
	expect(t, "createCategory", e.post("torrents/createCategory", url.Values{"category": {"c7"}, "downloadPathEnabled": {"true"}}), http.StatusOK)

	hash := func(i int) string {
		return strings.Repeat(string(rune('a'+i%6)), 2) + strings.Repeat("0", 36) + string(rune('a'+i/6)) + "1"
	}
	n := 0
	add := func(form url.Values, save, download string, auto bool) {
		t.Helper()
		h := hash(n)
		n++
		e.addMagnet(h, form)
		e.wantPaths("add "+form.Encode(), h, save, download, auto)
	}

	// Temp path off.
	add(nil, s, "", false)
	add(url.Values{"category": {"c2"}, "autoTMM": {"true"}}, s+"/c2path", "", true)
	add(url.Values{"category": {"c1"}, "autoTMM": {"true"}}, s+"/c1", "", true)
	add(url.Values{"category": {"c3"}, "autoTMM": {"true"}}, s+"/relative/c3", "", true)
	add(url.Values{"category": {"parent/child"}, "autoTMM": {"true"}}, s+"/parent/child", "", true)
	add(url.Values{"category": {"c2"}, "autoTMM": {"true"}, "savepath": {s + "/x8"}}, s+"/c2path", "", true)
	add(url.Values{"savepath": {"rel9"}}, s+"/rel9", "", false)
	add(url.Values{"category": {"c7"}, "autoTMM": {"true"}}, s+"/c7", temp+"/c7", true)
	add(url.Values{"useDownloadPath": {"true"}}, s, temp, false)
	add(url.Values{"downloadPath": {s + "/dp"}}, s, s+"/dp", false)

	// auto_tmm_enabled: a save path in the request means manual.
	e.setPrefs(`{"auto_tmm_enabled":true}`)
	add(url.Values{"category": {"c2"}}, s+"/c2path", "", true)
	add(url.Values{"category": {"c2"}, "savepath": {s + "/manual"}}, s+"/manual", "", false)
	e.setPrefs(`{"auto_tmm_enabled":false,"temp_path_enabled":true}`)

	// Temp path on.
	add(nil, s, temp, false)
	add(url.Values{"category": {"c4"}, "autoTMM": {"true"}}, s+"/c4save", s+"/inc4", true)
	add(url.Values{"category": {"c5"}, "autoTMM": {"true"}}, s+"/c5", "", true)
	add(url.Values{"category": {"c2"}, "autoTMM": {"true"}}, s+"/c2path", temp+"/c2", true)
	add(url.Values{"useDownloadPath": {"false"}}, s, "", false)
	add(url.Values{"useDownloadPath": {"false"}, "downloadPath": {s + "/dp"}}, s, "", false)
	add(url.Values{"useDownloadPath": {"true"}, "downloadPath": {s + "/dp20"}}, s, s+"/dp20", false)
	add(url.Values{"useDownloadPath": {"true"}, "downloadPath": {"rel22"}}, s, temp+"/rel22", false)
	add(url.Values{"category": {"c4"}, "autoTMM": {"true"}, "useDownloadPath": {"true"}, "downloadPath": {s + "/dp21"}}, s+"/c4save", s+"/inc4", true)
	add(url.Values{"autoTMM": {"true"}}, s, temp, true)

	// maindata has the fields too.
	var main struct {
		Torrents map[string]map[string]any `json:"torrents"`
	}
	e.get("sync/maindata?rid=0").json(t, &main)
	if tor := main.Torrents[hash(n-1)]; tor["auto_tmm"] != true || tor["download_path"] != temp || tor["save_path"] != s {
		t.Fatalf("maindata: %v", tor)
	}
	// Turning the temp path on gave the automatically managed torrents one,
	// whatever save_path_changed_tmm_enabled says.
	e.wantPaths("auto c2 after the temp path was turned on", hash(1), s+"/c2path", temp+"/c2", true)
}

func TestPathEndpoints(t *testing.T) {
	e := newEnv(t)
	e.login()
	s, temp := e.save, filepath.Join(e.save, "temp")
	e.setPrefs(`{"temp_path_enabled":true}`)
	for _, c := range []url.Values{
		{"category": {"c2"}, "savePath": {s + "/c2path"}},
		{"category": {"c4"}, "savePath": {s + "/c4save"}, "downloadPathEnabled": {"true"}, "downloadPath": {s + "/inc4"}},
	} {
		expect(t, "createCategory", e.post("torrents/createCategory", c), http.StatusOK)
	}
	const manual, auto = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	e.addMagnet(manual, url.Values{"category": {"c2"}})
	e.addMagnet(auto, url.Values{"category": {"c2"}, "autoTMM": {"true"}})
	// Unlike qBittorrent, piecemeal gives a manually managed torrent its
	// category's save path, as it always has.
	e.wantPaths("manual", manual, s+"/c2path", temp, false)
	e.wantPaths("auto", auto, s+"/c2path", temp+"/c2", true)

	// setAutoManagement needs both parameters; an enable that isn't a
	// boolean changes nothing.
	expect(t, "setAutoManagement without enable", e.post("torrents/setAutoManagement", url.Values{"hashes": {manual}}), http.StatusBadRequest)
	expect(t, "setAutoManagement without hashes", e.post("torrents/setAutoManagement", url.Values{"enable": {"true"}}), http.StatusBadRequest)
	expect(t, "setAutoManagement yes", e.post("torrents/setAutoManagement", url.Values{"hashes": {auto}, "enable": {"yes"}}), http.StatusOK)
	e.wantPaths("after enable=yes", auto, s+"/c2path", temp+"/c2", true)
	expect(t, "setAutoManagement unknown", e.post("torrents/setAutoManagement", url.Values{"hashes": {"ffffffffffffffffffffffffffffffffffffffff"}, "enable": {"true"}}), http.StatusOK)

	// setSavePath and setDownloadPath leave automatically managed torrents
	// alone.
	expect(t, "setSavePath auto", e.post("torrents/setSavePath", url.Values{"id": {auto}, "path": {s + "/sp"}}), http.StatusOK)
	expect(t, "setDownloadPath auto", e.post("torrents/setDownloadPath", url.Values{"id": {auto}, "path": {s + "/dl"}}), http.StatusOK)
	e.wantPaths("auto after setSavePath and setDownloadPath", auto, s+"/c2path", temp+"/c2", true)

	// Their parameters.
	for _, tc := range []struct {
		path   string
		form   url.Values
		status int
		body   string
	}{
		{"torrents/setSavePath", url.Values{"path": {s}}, http.StatusBadRequest, "Missing required parameters: id"},
		{"torrents/setSavePath", url.Values{"id": {manual}}, http.StatusBadRequest, "Missing required parameters: path"},
		{"torrents/setSavePath", url.Values{"id": {manual}, "path": {""}}, http.StatusBadRequest, "Save path cannot be empty"},
		{"torrents/setSavePath", url.Values{"id": {manual}, "path": {"rel"}}, http.StatusConflict, "Cannot create target directory"},
		{"torrents/setSavePath", url.Values{"id": {manual}, "path": {"/etc"}}, http.StatusForbidden, "Cannot write to directory"},
		{"torrents/setDownloadPath", url.Values{"id": {manual}}, http.StatusBadRequest, "Missing required parameters: path"},
		{"torrents/setDownloadPath", url.Values{"id": {manual}, "path": {"rel"}}, http.StatusConflict, "Cannot create target directory"},
		{"torrents/setLocation", url.Values{"hashes": {manual}, "location": {""}}, http.StatusBadRequest, "Save path cannot be empty"},
		{"torrents/setLocation", url.Values{"hashes": {manual}, "location": {"rel"}}, http.StatusConflict, "Cannot make save path"},
	} {
		r := e.post(tc.path, tc.form)
		if r.status != tc.status || strings.TrimSpace(string(r.body)) != tc.body {
			t.Errorf("%s %s: %d %q, want %d %q", tc.path, tc.form.Encode(), r.status, r.body, tc.status, tc.body)
		}
	}

	// On a manually managed torrent they work; an empty download path is
	// the save path.
	expect(t, "setSavePath", e.post("torrents/setSavePath", url.Values{"id": {manual}, "path": {s + "/m6"}}), http.StatusOK)
	expect(t, "setDownloadPath", e.post("torrents/setDownloadPath", url.Values{"id": {manual}, "path": {s + "/dp6"}}), http.StatusOK)
	e.wantPaths("manual after both", manual, s+"/m6", s+"/dp6", false)
	expect(t, "setDownloadPath empty", e.post("torrents/setDownloadPath", url.Values{"id": {manual}, "path": {""}}), http.StatusOK)
	e.wantPaths("manual after an empty download path", manual, s+"/m6", s+"/m6", false)

	// Turning automatic management on takes the category's paths; off
	// keeps them.
	expect(t, "setAutoManagement on", e.post("torrents/setAutoManagement", url.Values{"hashes": {manual}, "enable": {"true"}}), http.StatusOK)
	e.wantPaths("manual turned auto", manual, s+"/c2path", temp+"/c2", true)
	expect(t, "setAutoManagement off", e.post("torrents/setAutoManagement", url.Values{"hashes": {manual}, "enable": {"false"}}), http.StatusOK)
	e.wantPaths("turned manual again", manual, s+"/c2path", temp+"/c2", false)

	// setCategory moves an automatically managed torrent, or with
	// torrent_changed_tmm_enabled off switches it to manual.
	expect(t, "setCategory c4", e.post("torrents/setCategory", url.Values{"hashes": {auto}, "category": {"c4"}}), http.StatusOK)
	e.wantPaths("auto in c4", auto, s+"/c4save", s+"/inc4", true)
	expect(t, "setCategory none", e.post("torrents/setCategory", url.Values{"hashes": {auto}, "category": {""}}), http.StatusOK)
	e.wantPaths("auto without a category", auto, s, temp, true)
	e.setPrefs(`{"torrent_changed_tmm_enabled":false}`)
	expect(t, "setCategory c2", e.post("torrents/setCategory", url.Values{"hashes": {auto}, "category": {"c2"}}), http.StatusOK)
	e.wantPaths("auto after setCategory without relocation", auto, s, temp, false)

	// A category's path change: with category_changed_tmm_enabled off its
	// automatically managed torrents turn manual; on, they follow.
	expect(t, "setAutoManagement on", e.post("torrents/setAutoManagement", url.Values{"hashes": {auto}, "enable": {"true"}}), http.StatusOK)
	expect(t, "editCategory", e.post("torrents/editCategory", url.Values{"category": {"c2"}, "savePath": {s + "/c2new"}}), http.StatusOK)
	e.wantPaths("after editCategory without relocation", auto, s+"/c2path", temp+"/c2", false)
	e.setPrefs(`{"category_changed_tmm_enabled":true}`)
	expect(t, "setAutoManagement on", e.post("torrents/setAutoManagement", url.Values{"hashes": {auto}, "enable": {"true"}}), http.StatusOK)
	expect(t, "editCategory", e.post("torrents/editCategory", url.Values{
		"category": {"c2"}, "savePath": {s + "/c2newer"}, "downloadPathEnabled": {"false"},
	}), http.StatusOK)
	e.wantPaths("after editCategory with relocation", auto, s+"/c2newer", "", true)

	// The default save path: torrents that follow it move with
	// save_path_changed_tmm_enabled, else turn manual.
	expect(t, "setCategory none", e.post("torrents/setCategory", url.Values{"hashes": {auto}, "category": {""}}), http.StatusOK)
	e.wantPaths("auto turned manual by setCategory", auto, s+"/c2newer", "", false)
	expect(t, "setAutoManagement on", e.post("torrents/setAutoManagement", url.Values{"hashes": {auto}, "enable": {"true"}}), http.StatusOK)
	e.wantPaths("auto again", auto, s, temp, true)
	e.setPrefs(`{"save_path_changed_tmm_enabled":true,"save_path":"` + s + `/new"}`)
	e.wantPaths("after the default save path changed", auto, s+"/new", temp, true)

	// setLocation turns it off and keeps the download path.
	expect(t, "setLocation", e.post("torrents/setLocation", url.Values{"hashes": {auto}, "location": {s + "/loc"}}), http.StatusOK)
	e.wantPaths("after setLocation", auto, s+"/loc", temp, false)

	// Removing a category: its automatically managed torrents lose it and
	// follow, as with setCategory.
	e.setPrefs(`{"torrent_changed_tmm_enabled":true}`)
	expect(t, "setCategory c4", e.post("torrents/setCategory", url.Values{"hashes": {manual}, "category": {"c4"}}), http.StatusOK)
	expect(t, "setAutoManagement on", e.post("torrents/setAutoManagement", url.Values{"hashes": {manual}, "enable": {"true"}}), http.StatusOK)
	e.wantPaths("in c4", manual, s+"/c4save", s+"/inc4", true)
	expect(t, "removeCategories", e.post("torrents/removeCategories", url.Values{"categories": {"c4"}}), http.StatusOK)
	e.wantPaths("after removing c4", manual, s+"/new", temp, true)
}
