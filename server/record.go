package main

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
)

const (
	maxHelloBytes  = 16 << 10 // raw bytes kept from the TLS handshake
	maxStreamBytes = 64 << 10 // decrypted bytes kept from the start of the connection
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

// stop ends recording and returns what was kept.
func (r *helloRecorder) stop() []byte {
	r.stopped = true
	return r.buf
}

// streamRecorder sits over TLS and keeps the first decrypted bytes the client
// sent: the HTTP/2 preface and opening frames, or the first HTTP/1.1 request.
type streamRecorder struct {
	*tls.Conn
	mu  sync.Mutex
	buf []byte
}

func (r *streamRecorder) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 {
		r.mu.Lock()
		if keep := min(n, maxStreamBytes-len(r.buf)); keep > 0 {
			r.buf = append(r.buf, p[:keep]...)
		}
		r.mu.Unlock()
	}
	return n, err
}

func (r *streamRecorder) snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf...)
}

// connInfo is what a handler can learn about the connection it runs on.
type connInfo struct {
	hello  []byte // raw TLS records up to the end of the handshake
	state  tls.ConnectionState
	stream *streamRecorder
}

type connInfoKey struct{}

func withConnInfo(ctx context.Context, ci *connInfo) context.Context {
	return context.WithValue(ctx, connInfoKey{}, ci)
}

func connInfoFrom(ctx context.Context) *connInfo {
	ci, _ := ctx.Value(connInfoKey{}).(*connInfo)
	return ci
}
