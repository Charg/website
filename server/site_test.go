package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testSite(t *testing.T) *site {
	t.Helper()
	s, err := loadSite(fstest.MapFS{
		".csp":                     {Data: []byte("default-src 'self'\n")},
		".gitkeep":                 {},
		".git/config":              {Data: []byte("secret")},
		".well-known/security.txt": {Data: []byte("Contact: x")},
		".well-known/.hidden":      {Data: []byte("no")},
		"index.html":               {Data: []byte("<!doctype html>home")},
		ipPageFile:                 {Data: []byte("<p>{{ip}} v{{version}}</p>")},
		"llms.txt":                 {Data: []byte("hello")},
		"tools/fingerprint.html":   {Data: []byte("<!doctype html>fp")},
		"blog/index.html":          {Data: []byte("<!doctype html>blog")},
		"blog/post.html":           {Data: []byte("<!doctype html>post")},
		"blog/.draft/x.html":       {Data: []byte("no")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSiteRoutes(t *testing.T) {
	s := testSite(t)
	tests := []struct {
		method, path string
		status       int
		location     string
		body         string
	}{
		{"GET", "/", 200, "", "<!doctype html>home"},
		{"HEAD", "/", 200, "", ""},
		{"GET", "/index.html", 200, "", "<!doctype html>home"},
		{"GET", "/llms.txt", 200, "", "hello"},
		{"GET", "/tools/fingerprint.html", 200, "", "<!doctype html>fp"},
		{"GET", "/tools", 404, "", ""}, // directory without index.html
		{"GET", "/tools/", 404, "", ""},
		{"GET", "/blog", 301, "/blog/", ""},
		{"GET", "/blog?a=1", 301, "/blog/?a=1", ""},
		{"GET", "/blog/", 200, "", "<!doctype html>blog"},
		{"GET", "/blog/../llms.txt", 200, "", "hello"},
		{"GET", "/.well-known/security.txt", 200, "", "Contact: x"},
		{"GET", "/.well-known/.hidden", 404, "", ""},
		{"GET", "/.git/config", 404, "", ""},
		{"GET", "/.csp", 404, "", ""},
		{"GET", "/.gitkeep", 404, "", ""},
		{"GET", "/blog/.draft/x.html", 404, "", ""},
		{"GET", "/nope", 404, "", ""},
		{"GET", "//evil.example", 404, "", ""},
		{"POST", "/", 405, "", ""},
		{"PUT", "/llms.txt", 405, "", ""},
		{"OPTIONS", "/", 405, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "https://syn.sh"+tt.path, nil)
			if tt.path == "//evil.example" {
				req.URL.Path = tt.path
			}
			rec := httptest.NewRecorder()
			routes(s, testIPHost).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Header().Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			if tt.body != "" && rec.Body.String() != tt.body {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.body)
			}
			if tt.method == "HEAD" && rec.Body.Len() != 0 {
				t.Errorf("HEAD returned a body")
			}
		})
	}
}

func TestSiteHeaders(t *testing.T) {
	s := testSite(t)
	want := map[string]string{
		"Strict-Transport-Security":    "max-age=15552000",
		"Content-Security-Policy":      "default-src 'self'",
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "strict-origin-when-cross-origin",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	// Every response carries them, errors included.
	for _, p := range []string{"/", "/nope", "/blog", "/.git/config"} {
		for _, m := range []string{"GET", "POST"} {
			rec := httptest.NewRecorder()
			routes(s, testIPHost).ServeHTTP(rec, httptest.NewRequest(m, p, nil))
			for k, v := range want {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s %s: %s = %q, want %q", m, p, k, got, v)
				}
			}
			if rec.Header().Get("Server") != "" {
				t.Errorf("%s %s: Server header set", m, p)
			}
		}
	}
}

func TestSiteContentTypeAndETag(t *testing.T) {
	s := testSite(t)
	rec := httptest.NewRecorder()
	routes(s, testIPHost).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	routes(s, testIPHost).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("conditional GET = %d, want 304", rec.Code)
	}

	rec = httptest.NewRecorder()
	routes(s, testIPHost).ServeHTTP(rec, httptest.NewRequest("GET", "/llms.txt", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("llms.txt Content-Type = %q", ct)
	}
}

func TestLoadSiteRequiresCSP(t *testing.T) {
	_, err := loadSite(fstest.MapFS{"index.html": {Data: []byte("x")}, ipPageFile: {Data: []byte("{{ip}}")}})
	if err == nil {
		t.Fatal("expected an error without .csp")
	}
}
