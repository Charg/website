package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pires/go-proxyproto"
	"golang.org/x/net/netutil"
)

// startServer runs the real accept path (limit + PROXY + TLS + h1/h2 routing)
// with a handler that echoes what it saw.
func startServer(t *testing.T) (addr string, roots *x509.CertPool) {
	t.Helper()
	cert, err := selfSigned()
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	roots = x509.NewCertPool()
	roots.AddCert(leaf)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Proto+" "+r.RemoteAddr)
	})
	srv, err := newServer(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
		Certificates: []tls.Certificate{*cert},
	}, handler)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pl := &proxyproto.Listener{Listener: netutil.LimitListener(ln, 16), ReadHeaderTimeout: time.Second}
	go srv.serve(pl)
	t.Cleanup(func() {
		pl.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.shutdown(ctx)
	})
	return ln.Addr().String(), roots
}

var fakeClient = &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 40000}

func client(addr string, roots *x509.CertPool, protos []string, sendProxy bool) *http.Client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		if sendProxy {
			h := proxyproto.HeaderProxyFromAddrs(2, fakeClient, c.LocalAddr())
			if _, err := h.WriteTo(c); err != nil {
				c.Close()
				return nil, err
			}
		}
		tc := tls.Client(c, &tls.Config{RootCAs: roots, ServerName: "localhost", NextProtos: protos})
		if err := tc.HandshakeContext(ctx); err != nil {
			c.Close()
			return nil, err
		}
		return tc, nil
	}
	tr := &http.Transport{DialTLSContext: dial, ForceAttemptHTTP2: true}
	if len(protos) == 1 && protos[0] == "http/1.1" {
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	return &http.Client{Transport: tr, Timeout: 5 * time.Second}
}

func get(t *testing.T, c *http.Client) (string, error) {
	t.Helper()
	resp, err := c.Get("https://localhost/")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestServerHTTP2(t *testing.T) {
	addr, roots := startServer(t)
	got, err := get(t, client(addr, roots, []string{"h2", "http/1.1"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if want := "HTTP/2.0 " + fakeClient.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestServerHTTP1(t *testing.T) {
	addr, roots := startServer(t)
	got, err := get(t, client(addr, roots, []string{"http/1.1"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if want := "HTTP/1.1 " + fakeClient.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestServerRequiresProxyHeader(t *testing.T) {
	addr, roots := startServer(t)
	if _, err := get(t, client(addr, roots, []string{"h2"}, false)); err == nil {
		t.Fatal("request without a PROXY header succeeded")
	}
}

func TestServerHandshakeTimeout(t *testing.T) {
	addr, _ := startServer(t)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Send nothing: the PROXY header timeout should close the connection.
	c.SetReadDeadline(time.Now().Add(handshakeTimeout))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected the server to close the connection")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("server kept an idle connection open")
	}
}

// The real handler stack, over a real TLS connection, reports the client's
// ClientHello and opening frames.
func TestConnectionAPI(t *testing.T) {
	cert, err := selfSigned()
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	st, err := loadSite(fstest.MapFS{".csp": {Data: []byte("default-src 'self'")}, "index.html": {Data: []byte("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := newServer(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
		Certificates: []tls.Certificate{*cert},
	}, routes(st))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pl := &proxyproto.Listener{Listener: ln, ReadHeaderTimeout: time.Second}
	go srv.serve(pl)
	defer pl.Close()

	for _, protos := range [][]string{{"h2", "http/1.1"}, {"http/1.1"}} {
		c := client(ln.Addr().String(), roots, protos, true)
		resp, err := c.Get("https://localhost/api/connection")
		if err != nil {
			t.Fatal(err)
		}
		var rep report
		err = json.NewDecoder(resp.Body).Decode(&rep)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Error("missing Cache-Control: no-store")
		}
		if rep.IP != "203.0.113.7" {
			t.Errorf("ip = %q", rep.IP)
		}
		if rep.TLS.Error != "" || !strings.HasPrefix(rep.TLS.JA4, "t13d") || rep.TLS.ServerName != "localhost" {
			t.Errorf("tls = %+v", rep.TLS)
		}
		if rep.HTTP.Error != "" || len(rep.HTTP.Headers) == 0 {
			t.Errorf("http = %+v", rep.HTTP)
		}
		if protos[0] == "h2" {
			// Go's client sends pseudo-headers sorted: a,m,p,s.
			if rep.HTTP.Version != "HTTP/2" || rep.HTTP.H2 == nil || !strings.HasSuffix(rep.HTTP.H2.Akamai, "|a,m,p,s") {
				t.Errorf("h2 = %+v %+v", rep.HTTP, rep.HTTP.H2)
			}
		} else if rep.HTTP.Version != "HTTP/1.1" || rep.HTTP.FirstLine != "GET /api/connection HTTP/1.1" {
			t.Errorf("h1 = %+v", rep.HTTP)
		}
	}
}
