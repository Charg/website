package main

// http2.Server.ServeConn has no net/http equivalent for a conn we have already
// terminated TLS on, so the deprecated x/net server stays for now.
//lint:file-ignore SA1019 x/net/http2 server needed for ServeConn

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

const handshakeTimeout = 10 * time.Second

// server does the TLS handshake itself and then hands each connection to
// Go's HTTP/2 or HTTP/1.1 server, based on the negotiated protocol.
type server struct {
	tlsConfig *tls.Config
	handler   http.Handler
	h1        *http.Server
	h2        *http2.Server
	h1conns   *connListener
	perIP     *ipLimiter // nil: no per-client limit
}

func newServer(tlsConfig *tls.Config, handler http.Handler) (*server, error) {
	handler = stopRecording(handler)
	h1 := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if hc, ok := c.(*h1Conn); ok {
				return withConnInfo(ctx, hc.info)
			}
			return ctx
		},
	}
	h2 := &http2.Server{
		MaxConcurrentStreams: 100,
		IdleTimeout:          120 * time.Second,
	}
	// Ties h2 connections to h1.Shutdown, which sends them GOAWAY.
	if err := http2.ConfigureServer(h1, h2); err != nil {
		return nil, err
	}
	return &server{
		tlsConfig: tlsConfig,
		handler:   handler,
		h1:        h1,
		h2:        h2,
		h1conns:   newConnListener(),
	}, nil
}

// serve accepts connections from ln until it is closed.
func (s *server) serve(ln net.Listener) error {
	go s.h1.Serve(s.h1conns)
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go s.handle(c)
	}
}

func (s *server) handle(c net.Conn) {
	// With PROXY protocol, RemoteAddr waits for the header.
	release, ok := s.perIP.acquire(c.RemoteAddr())
	if !ok {
		c.Close()
		return
	}
	c = &limitedConn{Conn: c, release: release}

	raw := &helloRecorder{Conn: c}
	tc := tls.Server(raw, s.tlsConfig)
	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	err := tc.HandshakeContext(ctx)
	cancel()
	if err != nil {
		tc.Close()
		return
	}
	stream := &streamRecorder{Conn: tc}
	ci := &connInfo{hello: raw.stop(), state: tc.ConnectionState(), stream: stream}

	if ci.state.NegotiatedProtocol == http2.NextProtoTLS {
		// No BaseConfig: with it, x/net serves the conn on a copy of h1 that
		// Shutdown never reaches.
		s.h2.ServeConn(stream, &http2.ServeConnOpts{
			Context: withConnInfo(context.Background(), ci),
			Handler: s.handler,
		})
		stream.Close()
		return
	}
	if !s.h1conns.push(&h1Conn{streamRecorder: stream, info: ci}) {
		tc.Close()
	}
}

// h1Conn carries connInfo to http.Server's ConnContext.
type h1Conn struct {
	*streamRecorder
	info *connInfo
}

func (s *server) shutdown(ctx context.Context) error {
	return s.h1.Shutdown(ctx)
}

// connListener feeds already-accepted connections to http.Server.Serve.
type connListener struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
}

func newConnListener() *connListener {
	return &connListener{ch: make(chan net.Conn), done: make(chan struct{})}
}

func (l *connListener) push(c net.Conn) bool {
	select {
	case l.ch <- c:
		return true
	case <-l.done:
		return false
	}
}

func (l *connListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *connListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *connListener) Addr() net.Addr { return pipeAddr{} }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }
