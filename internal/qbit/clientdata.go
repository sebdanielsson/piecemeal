package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/sebdanielsson/piecemeal/internal/fetch"
	"github.com/sebdanielsson/piecemeal/internal/store"
)

// Namespaces in the store's kv table.
const (
	nsClientData = "qbit.clientdata"
	nsCookies    = "qbit.cookies"
)

// clientDataLimit caps the clientdata store, far above what qBittorrent's
// own web UI keeps there.
const clientDataLimit = 1 << 20

var errTooLarge = errors.New("client data would exceed 1 MiB")

// loadCookies hands the stored cookie jar to the fetcher.
func (s *Server) loadCookies(ctx context.Context) error {
	rows, err := s.store.ListKV(ctx, nsCookies)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var cs []fetch.Cookie
		if err := json.Unmarshal([]byte(row.Value), &cs); err != nil {
			return err
		}
		s.fetcher.SetCookies(cs)
	}
	return nil
}

// cookies returns the download cookies: tracker logins, so admins only.
func (s *Server) cookies(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	writeJSON(w, s.fetcher.Cookies())
}

// setCookies replaces the cookies sent when downloading torrent files.
func (s *Server) setCookies(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	raw := bytes.TrimSpace([]byte(r.FormValue("cookies")))
	var cs []fetch.Cookie
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &cs) != nil {
		http.Error(w, "cookies must be a JSON array of cookies", http.StatusBadRequest)
		return
	}
	cs = slices.DeleteFunc(cs, func(c fetch.Cookie) bool { return c.Name == "" })
	b, err := json.Marshal(cs)
	if err != nil {
		s.engineError(w, err)
		return
	}
	if err := s.store.PutKV(r.Context(), store.PutKVParams{Namespace: nsCookies, Key: "jar", Value: string(b)}); err != nil {
		s.engineError(w, err)
		return
	}
	s.fetcher.SetCookies(cs)
	done(w)
}

// clientDataLoad returns stored values: those named in keys, or all.
func (s *Server) clientDataLoad(w http.ResponseWriter, r *http.Request) {
	var keys []string
	if v := r.FormValue("keys"); v != "" {
		if err := json.Unmarshal([]byte(v), &keys); err != nil {
			http.Error(w, "keys must be a JSON array of strings", http.StatusBadRequest)
			return
		}
	}
	rows, err := s.store.ListKV(r.Context(), nsClientData)
	if err != nil {
		s.engineError(w, err)
		return
	}
	out := map[string]json.RawMessage{}
	for _, row := range rows {
		if keys == nil || slices.Contains(keys, row.Key) {
			out[row.Key] = json.RawMessage(row.Value)
		}
	}
	writeJSON(w, out)
}

// clientDataStore merges data into the store: each key replaces its value,
// and null deletes it.
func (s *Server) clientDataStore(w http.ResponseWriter, r *http.Request) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.FormValue("data")), &data); err != nil || data == nil {
		http.Error(w, "data must be a JSON object", http.StatusBadRequest)
		return
	}
	err := s.store.Tx(r.Context(), func(q *store.Queries) error {
		rows, err := q.ListKV(r.Context(), nsClientData)
		if err != nil {
			return err
		}
		size := 0
		for _, row := range rows {
			if _, replaced := data[row.Key]; !replaced {
				size += len(row.Key) + len(row.Value)
			}
		}
		for k, v := range data {
			if string(v) == "null" {
				if err := q.DeleteKV(r.Context(), store.DeleteKVParams{Namespace: nsClientData, Key: k}); err != nil {
					return err
				}
				continue
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, v); err != nil {
				return err
			}
			if size += len(k) + compact.Len(); size > clientDataLimit {
				return errTooLarge
			}
			if err := q.PutKV(r.Context(), store.PutKVParams{Namespace: nsClientData, Key: k, Value: compact.String()}); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errTooLarge) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		s.engineError(w, err)
		return
	}
	noContent(w)
}
