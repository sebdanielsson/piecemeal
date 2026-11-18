package qbit

import (
	"errors"
	"net/http"
	"strings"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

func (s *Server) categories(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{}
	for _, c := range s.engine.Categories() {
		out[c.Name] = categoryObject(c)
	}
	writeJSON(w, out)
}

// validCategory follows qBittorrent's rules: / separates subcategories, so
// no empty segments, no leading or trailing /, and no backslash.
func validCategory(name string) bool {
	if name == "" || strings.Contains(name, `\`) || strings.Contains(name, "//") ||
		strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}
	return true
}

func (s *Server) putCategory(w http.ResponseWriter, r *http.Request, create bool) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("category")
	if name == "" {
		http.Error(w, "Missing required parameter: category", http.StatusBadRequest)
		return
	}
	if !validCategory(name) {
		http.Error(w, "Incorrect category name", http.StatusConflict)
		return
	}
	c := engine.Category{Name: name}
	if p := r.FormValue("savePath"); p != "" {
		c.SavePath = s.absolute(p)
	}
	err := s.engine.PutCategory(r.Context(), c, create)
	switch {
	case errors.Is(err, engine.ErrExists):
		http.Error(w, "Category already exists", http.StatusConflict)
	case errors.Is(err, engine.ErrNotFound):
		http.Error(w, "Category does not exist", http.StatusNotFound)
	case errors.Is(err, engine.ErrInvalid), errors.Is(err, engine.ErrPathForbidden):
		http.Error(w, err.Error(), http.StatusConflict)
	case err != nil:
		s.engineError(w, err)
	default:
		done(w)
	}
}

func (s *Server) createCategory(w http.ResponseWriter, r *http.Request) { s.putCategory(w, r, true) }

func (s *Server) editCategory(w http.ResponseWriter, r *http.Request) { s.putCategory(w, r, false) }

func (s *Server) removeCategories(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var names []string
	for _, n := range strings.Split(r.FormValue("categories"), "\n") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	if err := s.engine.DeleteCategories(r.Context(), names...); err != nil {
		s.engineError(w, err)
		return
	}
	done(w)
}

func (s *Server) setCategory(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hs := s.known(hashes(r, "hashes"))
	err := s.engine.SetCategory(r.Context(), r.FormValue("category"), hs...)
	if errors.Is(err, engine.ErrNotFound) {
		http.Error(w, "Incorrect category name", http.StatusConflict)
		return
	}
	if err != nil {
		s.engineError(w, err)
		return
	}
	done(w)
}

func (s *Server) tags(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.engine.Tags())
}

func (s *Server) addTags(w http.ResponseWriter, r *http.Request) {
	s.withHashes(w, r, func(hs []string) error {
		return s.engine.AddTags(r.Context(), list(r.FormValue("tags")), hs...)
	})
}

func (s *Server) removeTags(w http.ResponseWriter, r *http.Request) {
	s.withHashes(w, r, func(hs []string) error {
		return s.engine.RemoveTags(r.Context(), list(r.FormValue("tags")), hs...)
	})
}

func (s *Server) createTags(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.engine.CreateTags(r.Context(), list(r.FormValue("tags"))...); err != nil {
		s.engineError(w, err)
		return
	}
	done(w)
}

func (s *Server) deleteTags(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.engine.DeleteTags(r.Context(), list(r.FormValue("tags"))...); err != nil {
		s.engineError(w, err)
		return
	}
	done(w)
}
