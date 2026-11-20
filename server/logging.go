package main

import (
	"log/slog"
	"net"
	"net/http"
	"time"
)

// maxLogValue caps client-supplied strings in the request log.
const maxLogValue = 512

// logRequests writes one log line per request after it is served.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		log.LogAttrs(r.Context(), slog.LevelInfo, "request",
			slog.String("method", r.Method),
			slog.String("host", truncate(r.Host)),
			slog.String("path", truncate(r.URL.Path)),
			slog.String("proto", r.Proto),
			slog.Int("status", sw.status),
			slog.Int64("bytes", sw.bytes),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("remote", remoteIP(r.RemoteAddr)),
			slog.String("user_agent", truncate(r.UserAgent())),
			slog.String("referer", truncate(r.Referer())),
		)
	})
}

// statusWriter records the status code and body size of a response.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 { // 1xx responses are not final
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func remoteIP(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func truncate(s string) string {
	if len(s) > maxLogValue {
		return s[:maxLogValue]
	}
	return s
}
