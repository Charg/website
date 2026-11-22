package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var testIPHost = ipHost{name: "ip.syn.sh", pageURL: "https://syn.sh/ip"}

func TestIPRoutes(t *testing.T) {
	s := testSite(t)
	const html, curl = "text/html,application/xhtml+xml,*/*;q=0.8", "*/*"
	tests := []struct {
		host, method, path, accept, remote string
		status                             int
		ctype, body, location              string
	}{
		{"syn.sh", "GET", "/ip", curl, "203.0.113.7:1234", 200, "text/plain; charset=utf-8", "203.0.113.7\n", ""},
		{"syn.sh", "GET", "/ip", "", "203.0.113.7:1234", 200, "text/plain; charset=utf-8", "203.0.113.7\n", ""},
		{"syn.sh", "GET", "/ip", html, "203.0.113.7:1234", 200, "text/html; charset=utf-8", "<p>203.0.113.7 v4</p>", ""},
		{"syn.sh", "GET", "/ip", html, "[::ffff:203.0.113.7]:1234", 200, "text/html; charset=utf-8", "<p>203.0.113.7 v4</p>", ""},
		{"syn.sh", "GET", "/ip", html, "[2001:db8::1]:1234", 200, "text/html; charset=utf-8", "<p>2001:db8::1 v6</p>", ""},
		{"syn.sh", "GET", "/ip.txt", html, "203.0.113.7:1234", 200, "text/plain; charset=utf-8", "203.0.113.7\n", ""},
		{"syn.sh", "GET", "/ip.json", curl, "203.0.113.7:1234", 200, "application/json", "{\n  \"ip\": \"203.0.113.7\",\n  \"version\": 4\n}\n", ""},
		{"syn.sh", "GET", "/ip.yaml", curl, "203.0.113.7:1234", 200, "application/yaml", "ip: \"203.0.113.7\"\nversion: 4\n", ""},
		{"syn.sh", "GET", "/ip.toml", curl, "203.0.113.7:1234", 200, "application/toml", "ip = \"203.0.113.7\"\nversion = 4\n", ""},
		{"syn.sh", "GET", "/ip.xml", curl, "203.0.113.7:1234", 200, "application/xml; charset=utf-8", "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<client>\n  <ip>203.0.113.7</ip>\n  <version>4</version>\n</client>\n", ""},
		{"syn.sh", "GET", "/ip.yaml", curl, "[2001:db8::1]:1234", 200, "application/yaml", "ip: \"2001:db8::1\"\nversion: 6\n", ""},
		{"syn.sh", "HEAD", "/ip.json", curl, "203.0.113.7:1234", 200, "application/json", "", ""},
		{"syn.sh", "GET", "/ip.csv", curl, "203.0.113.7:1234", 404, "", "", ""},
		{"syn.sh", "GET", "/ip/", curl, "203.0.113.7:1234", 404, "", "", ""},
		{"syn.sh", "GET", "/json", curl, "203.0.113.7:1234", 404, "", "", ""},
		{"syn.sh", "POST", "/ip", curl, "203.0.113.7:1234", 405, "", "", ""},

		{"ip.syn.sh", "GET", "/", curl, "203.0.113.7:1234", 200, "text/plain; charset=utf-8", "203.0.113.7\n", ""},
		{"IP.syn.sh:443", "GET", "/", curl, "203.0.113.7:1234", 200, "text/plain; charset=utf-8", "203.0.113.7\n", ""},
		{"ip.syn.sh", "GET", "/", html, "203.0.113.7:1234", 302, "", "", "https://syn.sh/ip"},
		{"ip.syn.sh", "GET", "/ip", html, "203.0.113.7:1234", 302, "", "", "https://syn.sh/ip"},
		{"ip.syn.sh", "GET", "/json", curl, "203.0.113.7:1234", 200, "application/json", "{\n  \"ip\": \"203.0.113.7\",\n  \"version\": 4\n}\n", ""},
		{"ip.syn.sh", "GET", "/ip.toml", html, "203.0.113.7:1234", 200, "application/toml", "ip = \"203.0.113.7\"\nversion = 4\n", ""},
		{"ip.syn.sh", "GET", "/txt", html, "203.0.113.7:1234", 200, "text/plain; charset=utf-8", "203.0.113.7\n", ""},
		{"ip.syn.sh", "GET", "/index.html", curl, "203.0.113.7:1234", 404, "", "", ""},
		{"ip.syn.sh", "GET", "/api/connection", curl, "203.0.113.7:1234", 404, "", "", ""},
		{"ip.syn.sh", "GET", "/assets/site.css", curl, "203.0.113.7:1234", 404, "", "", ""},
		{"ip.syn.sh", "PUT", "/", curl, "203.0.113.7:1234", 405, "", "", ""},
		{"ip.syn.sh", "GET", "/", curl, "pipe", 503, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.host+" "+tt.method+" "+tt.path+" "+tt.remote, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "https://syn.sh"+tt.path, nil)
			req.Host = tt.host
			req.RemoteAddr = tt.remote
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			rec := httptest.NewRecorder()
			routes(s, testIPHost).ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (%q)", rec.Code, tt.status, rec.Body.String())
			}
			if tt.ctype != "" {
				if got := rec.Header().Get("Content-Type"); got != tt.ctype {
					t.Errorf("Content-Type = %q, want %q", got, tt.ctype)
				}
			}
			if tt.status == 200 && rec.Body.String() != tt.body {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.body)
			}
			if got := rec.Header().Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			if tt.status == 200 || tt.status == 302 {
				if got := rec.Header().Get("Cache-Control"); got != "no-store" {
					t.Errorf("Cache-Control = %q, want no-store", got)
				}
			}
		})
	}
}

func TestIPHeaders(t *testing.T) {
	s := testSite(t)
	get := func(path, accept string) http.Header {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		routes(s, testIPHost).ServeHTTP(rec, req)
		return rec.Header()
	}

	// Machine formats can be read from any origin; the page can't.
	h := get("/ip.json", "*/*")
	if h.Get("Access-Control-Allow-Origin") != "*" || h.Get("Cross-Origin-Resource-Policy") != "cross-origin" {
		t.Errorf("ip.json: cross-origin headers = %q, %q", h.Get("Access-Control-Allow-Origin"), h.Get("Cross-Origin-Resource-Policy"))
	}
	if h.Get("Vary") != "" {
		t.Errorf("ip.json: Vary = %q, want none", h.Get("Vary"))
	}
	h = get("/ip", "text/html")
	if h.Get("Access-Control-Allow-Origin") != "" || h.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Errorf("ip page: cross-origin headers = %q, %q", h.Get("Access-Control-Allow-Origin"), h.Get("Cross-Origin-Resource-Policy"))
	}
	for _, accept := range []string{"text/html", "*/*"} {
		if h := get("/ip", accept); h.Get("Vary") != "Accept" {
			t.Errorf("/ip with %s: Vary = %q, want Accept", accept, h.Get("Vary"))
		}
	}
	if h := get("/ip", "*/*"); h.Get("Strict-Transport-Security") == "" || h.Get("Content-Security-Policy") == "" {
		t.Error("/ip: missing security headers")
	}
}

func TestIPHostDisabled(t *testing.T) {
	s := testSite(t)
	req := httptest.NewRequest("GET", "/json", nil)
	req.Host = "ip.syn.sh"
	rec := httptest.NewRecorder()
	routes(s, ipHost{}).ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("status = %d, want 404 with the IP host off", rec.Code)
	}
}

func TestLoadSiteRequiresIPPage(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"missing":        {".csp": {Data: []byte("x")}, "index.html": {Data: []byte("x")}},
		"no placeholder": {".csp": {Data: []byte("x")}, "index.html": {Data: []byte("x")}, ipPageFile: {Data: []byte("x")}},
	} {
		if _, err := loadSite(fsys); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	s := testSite(t)
	rec := httptest.NewRecorder()
	routes(s, testIPHost).ServeHTTP(rec, httptest.NewRequest("GET", "/tools/.ip.html", nil))
	if rec.Code != 404 {
		t.Errorf("template served directly: status = %d", rec.Code)
	}
}

func TestPlainRoutes(t *testing.T) {
	trusted, err := parsePrefixes("10.42.0.0/16, 192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	h := realIP(trusted, plainRoutes(testIPHost))
	tests := []struct {
		name, host, method, path, accept, remote, realIP string
		status                                           int
		body, location                                   string
	}{
		{"trusted proxy", "ip.syn.sh", "GET", "/", "*/*", "10.42.3.4:5555", "203.0.113.7", 200, "203.0.113.7\n", ""},
		{"trusted single IP", "ip.syn.sh", "GET", "/", "*/*", "192.0.2.1:5555", "203.0.113.7", 200, "203.0.113.7\n", ""},
		{"mapped header", "ip.syn.sh", "GET", "/json", "*/*", "10.42.3.4:5555", "::ffff:203.0.113.7", 200, "{\n  \"ip\": \"203.0.113.7\",\n  \"version\": 4\n}\n", ""},
		{"untrusted peer", "ip.syn.sh", "GET", "/", "*/*", "198.51.100.9:5555", "203.0.113.7", 200, "198.51.100.9\n", ""},
		{"garbage header", "ip.syn.sh", "GET", "/", "*/*", "10.42.3.4:5555", "not-an-ip", 200, "10.42.3.4\n", ""},
		{"no header", "ip.syn.sh", "GET", "/", "*/*", "10.42.3.4:5555", "", 200, "10.42.3.4\n", ""},
		{"browser", "ip.syn.sh", "GET", "/", "text/html", "10.42.3.4:5555", "203.0.113.7", 302, "", "https://syn.sh/ip"},
		{"other host", "syn.sh", "GET", "/", "*/*", "10.42.3.4:5555", "203.0.113.7", 404, "", ""},
		{"site path", "ip.syn.sh", "GET", "/index.html", "*/*", "10.42.3.4:5555", "203.0.113.7", 404, "", ""},
		{"post", "ip.syn.sh", "POST", "/", "*/*", "10.42.3.4:5555", "203.0.113.7", 405, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://ip.syn.sh"+tt.path, nil)
			req.Host = tt.host
			req.RemoteAddr = tt.remote
			req.Header.Set("Accept", tt.accept)
			if tt.realIP != "" {
				req.Header.Set("X-Real-Ip", tt.realIP)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if tt.status == 200 && rec.Body.String() != tt.body {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.body)
			}
			if got := rec.Header().Get("Location"); got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			if rec.Header().Get("Strict-Transport-Security") != "" {
				t.Error("HSTS sent over plain HTTP")
			}
		})
	}
}

func TestParsePrefixes(t *testing.T) {
	got, err := parsePrefixes(" 10.42.0.0/16 ,,192.0.2.1, 10.1.2.3/8, 2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	var s []string
	for _, p := range got {
		s = append(s, p.String())
	}
	if want := "10.42.0.0/16 192.0.2.1/32 10.0.0.0/8 2001:db8::/32"; strings.Join(s, " ") != want {
		t.Errorf("got %v, want %s", s, want)
	}
	if _, err := parsePrefixes("10.0.0.0/99"); err == nil {
		t.Error("expected an error for a bad prefix")
	}
	if got, err := parsePrefixes(""); err != nil || len(got) != 0 {
		t.Errorf("empty: %v, %v", got, err)
	}
}
