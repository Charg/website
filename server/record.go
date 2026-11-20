package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"sync"
)

const (
	maxHelloBytes  = 16 << 10 // raw bytes kept from the TLS handshake
	maxStreamBytes = 32 << 10 // decrypted bytes kept from the start of the connection
)

// helloRecorder sits under TLS and keeps the bytes read during the handshake.
// Only the handshake goroutine reads it before stop, so it needs no lock.
type helloRecorder struct {
	net.Conn
	buf     []byte
	stopped bool
}

func (r *helloRecorder) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if !r.stopped && n > 0 {
		keep := min(n, maxHelloBytes-len(r.buf))
		r.buf = append(r.buf, p[:keep]...)
	}
	return n, err
}

// stop ends recording and hands over what was kept.
func (r *helloRecorder) stop() []byte {
	b := r.buf
	r.buf, r.stopped = nil, true
	return b
}

// streamRecorder sits over TLS and keeps the first decrypted bytes the client
// sent: the HTTP/2 preface and opening frames, or the first HTTP/1.1 request.
type streamRecorder struct {
	*tls.Conn
	mu      sync.Mutex
	buf     []byte
	stopped bool
}

func (r *streamRecorder) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 {
		r.record(p[:n])
	}
	return n, err
}

func (r *streamRecorder) record(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if keep := min(len(p), maxStreamBytes-len(r.buf)); keep > 0 && !r.stopped {
		r.buf = append(r.buf, p[:keep]...)
	}
}

// stop ends recording. By the time any handler runs, the first request has
// been read in full.
func (r *streamRecorder) stop() {
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
}

// take stops recording and hands over the recorded bytes.
func (r *streamRecorder) take() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.buf
	r.buf, r.stopped = nil, true
	return b
}

// connInfo is what a handler can learn about the connection it runs on.
type connInfo struct {
	hello  []byte // raw TLS records up to the end of the handshake
	state  tls.ConnectionState
	stream *streamRecorder

	once   sync.Once
	report []byte
}

// reportJSON returns the connection report, built on the first call. The
// recordings are released once it is built.
func (ci *connInfo) reportJSON(remoteAddr string) []byte {
	ci.once.Do(func() {
		ci.report = encodeReport(buildReport(ci, remoteAddr, ci.stream.take()))
		ci.hello = nil
	})
	return ci.report
}

// stopRecording ends stream recording when a connection's first request is
// dispatched, so later traffic is not kept.
func stopRecording(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ci := connInfoFrom(r.Context()); ci != nil {
			ci.stream.stop()
		}
		next.ServeHTTP(w, r)
	})
}

type connInfoKey struct{}

func withConnInfo(ctx context.Context, ci *connInfo) context.Context {
	return context.WithValue(ctx, connInfoKey{}, ci)
}

func connInfoFrom(ctx context.Context) *connInfo {
	ci, _ := ctx.Value(connInfoKey{}).(*connInfo)
	return ci
}
