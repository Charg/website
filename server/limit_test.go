package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/pires/go-proxyproto"
)

// dialAs opens a connection that claims to come from src and completes the
// TLS handshake.
func dialAs(addr string, roots *x509.CertPool, src net.IP, protos ...string) (*tls.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := dialFrom(addr, roots, protos, &net.TCPAddr{IP: src, Port: 40000})(ctx, "", "")
	if err != nil {
		return nil, err
	}
	return c.(*tls.Conn), nil
}

// waitFree waits until the limiter holds no slots.
func waitFree(t *testing.T, l *ipLimiter) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if l.open() == 0 {
			return
		}
	}
	t.Fatalf("%d slots still held", l.open())
}

func TestClientKey(t *testing.T) {
	tests := map[string]string{
		"203.0.113.7:1":                 "203.0.113.7/32",
		"[::ffff:203.0.113.7]:1":        "203.0.113.7/32",
		"[2001:db8:1:2:3:4:5:6]:1":      "2001:db8:1:2::/64",
		"[2001:db8:1:2:ffff::1%eth0]:1": "2001:db8:1:2::/64",
	}
	for in, want := range tests {
		ap := netip.MustParseAddrPort(in)
		if got := clientKey(net.TCPAddrFromAddrPort(ap)); got.String() != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
	a := clientKey(&net.TCPAddr{IP: net.ParseIP("2001:db8::1")})
	b := clientKey(&net.TCPAddr{IP: net.ParseIP("2001:db8::ffff:2")})
	if a != b {
		t.Error("addresses in one /64 got different keys")
	}
}

func TestPerIPLimit(t *testing.T) {
	addr, roots, srv := startServerWith(t, 2, "")
	a, b := net.ParseIP("203.0.113.7"), net.ParseIP("203.0.113.8")

	var held []*tls.Conn
	for range 2 {
		c, err := dialAs(addr, roots, a, "h2")
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	if c, err := dialAs(addr, roots, a, "h2"); err == nil {
		c.Close()
		t.Fatal("third connection from one client was served")
	}
	other, err := dialAs(addr, roots, b, "h2")
	if err != nil {
		t.Fatalf("another client was refused: %v", err)
	}
	other.Close()

	held[0].Close()
	time.Sleep(100 * time.Millisecond)
	c, err := dialAs(addr, roots, a, "h2")
	if err != nil {
		t.Fatalf("slot not released after close: %v", err)
	}
	c.Close()
	held[1].Close()
	waitFree(t, srv.perIP)
}

// Every way a connection ends gives its slot back exactly once.
func TestPerIPLimitReleases(t *testing.T) {
	addr, roots, srv := startServerWith(t, 1, "")
	src := net.ParseIP("203.0.113.7")

	// Failed handshake.
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	proxyproto.HeaderProxyFromAddrs(2, &net.TCPAddr{IP: src, Port: 1}, c.LocalAddr()).WriteTo(c)
	c.Write([]byte("not tls\r\n\r\n"))
	c.Read(make([]byte, 1))
	c.Close()
	waitFree(t, srv.perIP)

	// HTTP/1.1 and HTTP/2 after requests, closed by the client.
	for _, protos := range [][]string{{"http/1.1"}, {"h2"}} {
		for range 3 {
			cl := client(addr, roots, protos, true)
			if _, err := get(t, cl); err != nil {
				t.Fatalf("%v: %v", protos, err)
			}
			cl.CloseIdleConnections()
			waitFree(t, srv.perIP)
		}
	}

	// Shutdown with idle connections open; the server closes them.
	for i, protos := range [][]string{{"http/1.1"}, {"h2"}} {
		fake := &net.TCPAddr{IP: net.IPv4(198, 51, 100, byte(i)), Port: 1}
		cl := client(addr, roots, protos, true)
		cl.Transport.(*http.Transport).DialTLSContext = dialFrom(addr, roots, protos, fake)
		if _, err := get(t, cl); err != nil {
			t.Fatalf("%v: %v", protos, err)
		}
	}
	if srv.perIP.open() != 2 {
		t.Fatalf("open = %d, want 2", srv.perIP.open())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.shutdown(ctx)
	waitFree(t, srv.perIP)
}

func TestPerIPLimitDisabled(t *testing.T) {
	addr, roots, _ := startServerWith(t, 0, "")
	for range 5 {
		c, err := dialAs(addr, roots, net.ParseIP("203.0.113.7"), "h2")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
	}
}

func TestTrustedProxies(t *testing.T) {
	addr, roots, _ := startServerWith(t, 0, " 10.0.0.0/8, 127.0.0.1")
	got, err := get(t, client(addr, roots, []string{"h2"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if want := "HTTP/2.0 " + fakeClient.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := get(t, client(addr, roots, []string{"h2"}, false)); err == nil {
		t.Error("trusted peer without a PROXY header was served")
	}
}

func TestUntrustedProxyRejected(t *testing.T) {
	addr, roots, _ := startServerWith(t, 0, "10.0.0.0/8,::1")
	if _, err := get(t, client(addr, roots, []string{"h2"}, true)); err == nil {
		t.Error("untrusted peer with a PROXY header was served")
	}
	if _, err := get(t, client(addr, roots, []string{"h2"}, false)); err == nil {
		t.Error("untrusted peer without a PROXY header was served")
	}
}

func TestTrustedProxiesInvalid(t *testing.T) {
	for _, s := range []string{"10.0.0.0/33", "not-an-ip", "10.0.0.1,,bogus/8"} {
		if _, err := newProxyListener(nil, s); err == nil {
			t.Errorf("%q: no error", s)
		}
	}
}

func TestHealthServer(t *testing.T) {
	s := newHealthServer(":0")
	if s.ReadHeaderTimeout == 0 || s.ReadTimeout == 0 || s.WriteTimeout == 0 || s.IdleTimeout == 0 {
		t.Errorf("missing timeouts: %+v", s)
	}
	if s.MaxHeaderBytes == 0 || s.MaxHeaderBytes > 8<<10 {
		t.Errorf("MaxHeaderBytes = %d", s.MaxHeaderBytes)
	}
}
