package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/sebdanielsson/piecemeal/internal/engine"
	"github.com/sebdanielsson/piecemeal/internal/fetch"
)

// fetchMetadata, saveMetadata and parseMetadata (WebAPI 2.11.9) let an add
// dialog show a torrent's files before adding it. Magnets are fetched by
// the engine; torrent files behind http(s) URLs are downloaded here and
// kept in metaCache until added.

const (
	maxURLMeta     = 64
	urlMetaTTL     = time.Hour
	urlMetaTimeout = 2 * time.Minute
)

// urlMeta is a torrent file being downloaded for fetchMetadata.
type urlMeta struct {
	done   bool
	mi     []byte // the torrent file
	magnet string // or the magnet the URL redirected to
	hash   string
	err    error
	since  time.Time
}

type metaCache struct {
	mu    sync.Mutex
	byURL map[string]*urlMeta
}

func (c *metaCache) get(u string) (urlMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.byURL[u]; ok {
		return *e, true
	}
	return urlMeta{}, false
}

// byHash finds a downloaded torrent file by its info hash.
func (c *metaCache) byHash(hash string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.byURL {
		if e.done && e.hash == hash && e.mi != nil {
			return e.mi
		}
	}
	return nil
}

func (c *metaCache) remove(u string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byURL, u)
}

// start begins downloading u unless it already is; it reports false when
// too many downloads are cached.
func (c *metaCache) start(u string, get func(context.Context, string) (fetch.Result, error)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byURL == nil {
		c.byURL = map[string]*urlMeta{}
	}
	now := time.Now()
	for k, e := range c.byURL {
		if now.Sub(e.since) > urlMetaTTL {
			delete(c.byURL, k)
		}
	}
	if _, ok := c.byURL[u]; ok {
		return true
	}
	if len(c.byURL) >= maxURLMeta {
		return false
	}
	e := &urlMeta{since: now}
	c.byURL[u] = e
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), urlMetaTimeout)
		defer cancel()
		res, err := get(ctx, u)
		var mi []byte
		var magnet, hash string
		if err == nil {
			magnet, mi, err = engine.ParseSource(res.Body)
			if res.Magnet != "" {
				magnet, mi, err = res.Magnet, nil, nil
			}
		}
		if err == nil && mi != nil {
			if m, lerr := metainfo.Load(bytes.NewReader(mi)); lerr == nil {
				hash = m.HashInfoBytes().HexString()
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		e.done, e.mi, e.magnet, e.hash, e.err = true, mi, magnet, hash, err
	}()
	return true
}

var (
	hexHash    = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	base32Hash = regexp.MustCompile(`^[2-7A-Za-z]{32}$`)
)

// sourceMagnet turns a magnet or a bare v1 info hash into a magnet URI.
func sourceMagnet(src string) (string, bool) {
	switch {
	case strings.HasPrefix(strings.ToLower(src), "magnet:"):
		return src, true
	case hexHash.MatchString(src), base32Hash.MatchString(src):
		return "magnet:?xt=urn:btih:" + src, true
	}
	return "", false
}

func isWebURL(src string) bool {
	l := strings.ToLower(src)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

func (s *Server) fetchMetadata(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "source") {
		return
	}
	src := strings.TrimSpace(r.FormValue("source"))
	if magnet, ok := sourceMagnet(src); ok {
		s.fetchMagnetMetadata(w, src, magnet, true)
		return
	}
	if !isWebURL(src) {
		http.Error(w, fmt.Sprintf("Unable to parse '%s'", src), http.StatusBadRequest)
		return
	}
	e, ok := s.meta.get(src)
	if !ok {
		if !s.meta.start(src, s.fetcher.Get) {
			http.Error(w, "Too many metadata fetches", http.StatusConflict)
			return
		}
		e = urlMeta{}
	}
	switch {
	case !e.done:
		writeStatusJSON(w, http.StatusAccepted, map[string]any{})
	case e.err != nil:
		// Gone, so polling again tries again.
		s.meta.remove(src)
		http.Error(w, fmt.Sprintf("Unable to download '%s'", src), http.StatusConflict)
	case e.magnet != "":
		s.fetchMagnetMetadata(w, src, e.magnet, false)
	default:
		s.writeMetadata(w, e.mi)
	}
}

// fetchMagnetMetadata answers fetchMetadata for a magnet: 202 while the
// engine fetches it, then the metadata. withHash puts the hash in the
// 202 body, as qBittorrent does for magnets but not for URLs.
func (s *Server) fetchMagnetMetadata(w http.ResponseWriter, src, magnet string, withHash bool) {
	hash, mi, err := s.engine.FetchMetadata(magnet)
	switch {
	case errors.Is(err, engine.ErrInvalid):
		http.Error(w, fmt.Sprintf("Unable to parse '%s'", src), http.StatusBadRequest)
	case errors.Is(err, engine.ErrBusy):
		http.Error(w, "Too many metadata fetches", http.StatusConflict)
	case err != nil:
		s.engineError(w, err)
	case mi == nil && withHash:
		writeStatusJSON(w, http.StatusAccepted, map[string]any{"hash": hash, "infohash_v1": hash, "infohash_v2": ""})
	case mi == nil:
		writeStatusJSON(w, http.StatusAccepted, map[string]any{})
	default:
		s.writeMetadata(w, mi)
	}
}

func (s *Server) writeMetadata(w http.ResponseWriter, mi []byte) {
	obj, err := metadataObject(mi)
	if err != nil {
		s.engineError(w, err)
		return
	}
	writeJSON(w, obj)
}

func (s *Server) saveMetadata(w http.ResponseWriter, r *http.Request) {
	if !required(w, r, "source") {
		return
	}
	src := strings.TrimSpace(r.FormValue("source"))
	var mi []byte
	if magnet, ok := sourceMagnet(src); ok {
		hash := magnetHash(magnet)
		if mi = s.engine.FetchedMetadata(hash); mi == nil {
			mi = s.meta.byHash(hash)
		}
	} else {
		e, ok := s.meta.get(src)
		if !ok {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		mi = e.mi
		if e.magnet != "" {
			mi = s.engine.FetchedMetadata(magnetHash(e.magnet))
		}
	}
	if mi == nil {
		http.Error(w, "Metadata is not yet available", http.StatusConflict)
		return
	}
	m, err := metainfo.Load(bytes.NewReader(mi))
	if err != nil {
		s.engineError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.torrent"`, m.HashInfoBytes().HexString()))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(mi) //nolint:gosec // a parsed torrent file, sent as an attachment that browsers don't sniff
}

// magnetHash is a magnet's info hash in hex, or "".
func magnetHash(magnet string) string {
	m, err := metainfo.ParseMagnetUri(magnet)
	if err != nil {
		return ""
	}
	return m.InfoHash.HexString()
}

func (s *Server) parseMetadata(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	if r.MultipartForm == nil {
		writeJSON(w, out)
		return
	}
	// Any field name per file; fields in name order, files in upload order.
	fields := make([]string, 0, len(r.MultipartForm.File))
	for f := range r.MultipartForm.File {
		fields = append(fields, f)
	}
	slices.Sort(fields)
	for _, field := range fields {
		for _, fh := range r.MultipartForm.File[field] {
			obj, err := parseUpload(fh.Open)
			if err != nil {
				http.Error(w, fmt.Sprintf("'%s' is not a valid torrent file.", fh.Filename), http.StatusUnsupportedMediaType)
				return
			}
			out = append(out, obj)
		}
	}
	writeJSON(w, out)
}

func parseUpload[F io.ReadCloser](open func() (F, error)) (map[string]any, error) {
	f, err := open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil {
		return nil, err
	}
	return metadataObject(b)
}

// metadataObject is a torrent file as qBittorrent's metadata endpoints
// describe it.
func metadataObject(b []byte) (map[string]any, error) {
	mi, err := metainfo.Load(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}
	hash := mi.HashInfoBytes().HexString()
	files := []map[string]any{}
	if len(info.Files) == 0 {
		files = append(files, map[string]any{"length": info.Length, "path": info.BestName()})
	} else {
		for _, f := range info.Files {
			path := append([]string{info.BestName()}, f.BestPath()...)
			files = append(files, map[string]any{"length": f.Length, "path": strings.Join(path, "/")})
		}
	}
	trackers := []map[string]any{}
	for tier, urls := range mi.UpvertedAnnounceList() {
		for _, u := range urls {
			trackers = append(trackers, map[string]any{"tier": tier, "url": u})
		}
	}
	return map[string]any{
		"comment": mi.Comment, "created_by": mi.CreatedBy, "creation_date": mi.CreationDate, "hash": hash,
		"infohash_v1": hash, "infohash_v2": "",
		"info": map[string]any{
			"files": files, "length": info.TotalLength(), "name": info.BestName(), "piece_length": info.PieceLength,
			"pieces_num": info.NumPieces(), "private": info.Private != nil && *info.Private,
		},
		"trackers": trackers, "webseeds": stringsOrEmpty(mi.UrlList),
	}, nil
}

func stringsOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// writeStatusJSON is writeJSON with a status other than 200.
func writeStatusJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
