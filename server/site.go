package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// cspFile is written into the site directory by cmd/build. Dotfiles are never
// served, so it stays private to the server.
const cspFile = ".csp"

type file struct {
	data []byte
	etag string
}

// site serves a fixed set of files, loaded into memory at startup.
type site struct {
	files map[string]file
	dirs  map[string]bool
	csp   string
}

func loadSite(fsys fs.FS) (*site, error) {
	s := &site{files: map[string]file{}, dirs: map[string]bool{}}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if name == cspFile {
			b, err := fs.ReadFile(fsys, name)
			s.csp = strings.TrimSpace(string(b))
			return err
		}
		if hidden(name) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			s.dirs[name] = true
			return nil
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		s.files[name] = file{data: b, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.csp == "" {
		return nil, errors.New("site: missing " + cspFile + " (run go generate)")
	}
	if _, ok := s.files["index.html"]; !ok {
		return nil, errors.New("site: missing index.html (run go generate)")
	}
	return s, nil
}

// hidden reports whether any path segment is a dotfile, except a leading
// .well-known.
func hidden(name string) bool {
	for i, seg := range strings.Split(name, "/") {
		if strings.HasPrefix(seg, ".") && !(i == 0 && seg == ".well-known") {
			return true
		}
	}
	return false
}

// routes is the full handler: security headers, GET/HEAD only, the
// connection API and the static files.
func routes(s *site) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=15552000")
		h.Set("Content-Security-Policy", s.csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/api/connection" {
			serveConnection(w, r)
			return
		}
		s.ServeHTTP(w, r)
	})
}

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	p := r.URL.Path
	if !strings.HasPrefix(p, "/") {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean(p), "/")
	if hidden(name) {
		http.NotFound(w, r)
		return
	}

	if name == "" || s.dirs[name] {
		if _, ok := s.files[path.Join(name, "index.html")]; !ok {
			http.NotFound(w, r)
			return
		}
		if name != "" && !strings.HasSuffix(p, "/") {
			loc := "/" + name + "/"
			if r.URL.RawQuery != "" {
				loc += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, loc, http.StatusMovedPermanently)
			return
		}
		name = path.Join(name, "index.html")
	}

	f, ok := s.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.Set("ETag", f.etag)
	h.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(f.data))
}
