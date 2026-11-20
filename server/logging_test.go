package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func logOne(t *testing.T, h http.Handler, req *http.Request) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	logRequests(log, h).ServeHTTP(httptest.NewRecorder(), req)
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line %q: %v", buf.String(), err)
	}
	return line
}

func TestLogRequests(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("short and stout"))
	})
	req := httptest.NewRequest("GET", "/pot?q=1", nil)
	req.RemoteAddr = "[2001:db8::1]:4321"
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("Referer", "https://example.com/")

	got := logOne(t, h, req)
	want := map[string]any{
		"msg":        "request",
		"method":     "GET",
		"host":       "example.com",
		"path":       "/pot",
		"proto":      "HTTP/1.1",
		"status":     float64(418),
		"bytes":      float64(15),
		"remote":     "2001:db8::1",
		"user_agent": "test-agent",
		"referer":    "https://example.com/",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if _, ok := got["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms missing: %v", got)
	}
}

func TestLogRequestsDefaults(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"nothing written": func(w http.ResponseWriter, r *http.Request) {},
		"body only":       func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) },
		"informational first": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusEarlyHints)
			w.Write([]byte("x"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := logOne(t, h, httptest.NewRequest("GET", "/", nil))
			if got["status"] != float64(200) {
				t.Errorf("status = %v, want 200", got["status"])
			}
		})
	}
}

func TestLogRequestsTruncates(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", strings.Repeat("a", 4*maxLogValue))
	got := logOne(t, http.NotFoundHandler(), req)
	if ua, _ := got["user_agent"].(string); len(ua) != maxLogValue {
		t.Errorf("user_agent length = %d, want %d", len(ua), maxLogValue)
	}
}

func TestStatusWriterUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := http.NewResponseController(&statusWriter{ResponseWriter: rec}).Flush(); err != nil {
		t.Fatalf("Flush through statusWriter: %v", err)
	}
	if !rec.Flushed {
		t.Error("underlying writer not flushed")
	}
}
