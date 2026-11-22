// Command server serves the website over TLS, behind a TCP passthrough that
// sends the PROXY protocol.
package main

import (
	"context"
	"crypto/tls"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pires/go-proxyproto"
	"golang.org/x/net/netutil"
)

//go:generate go run ./cmd/build -root ..

//go:embed all:site
var embedded embed.FS

type config struct {
	addr, healthAddr  string
	httpAddr          string
	ipHost, ipPage    string
	certPath, keyPath string
	devCert           bool
	proxyProto        bool
	trustedProxies    string
	maxConns          int
	maxConnsPerIP     int
}

func main() {
	var c config
	flag.StringVar(&c.addr, "addr", ":8443", "TLS listen address")
	flag.StringVar(&c.healthAddr, "health-addr", ":8081", "plain HTTP address for /healthz")
	flag.StringVar(&c.httpAddr, "http-addr", "", "plain HTTP address serving only the IP host, behind a proxy that sets X-Real-Ip (empty: off)")
	flag.StringVar(&c.ipHost, "ip-host", "ip.syn.sh", "host name that answers only with the client's IP (empty: off)")
	flag.StringVar(&c.ipPage, "ip-page", "https://syn.sh/ip", "where the IP host sends browsers")
	flag.StringVar(&c.certPath, "cert", "/tls/tls.crt", "certificate file")
	flag.StringVar(&c.keyPath, "key", "/tls/tls.key", "private key file")
	flag.BoolVar(&c.devCert, "self-signed", false, "use a throwaway localhost certificate instead of -cert/-key")
	flag.BoolVar(&c.proxyProto, "proxy-protocol", true, "require a PROXY protocol header on every connection")
	flag.StringVar(&c.trustedProxies, "trusted-proxies", "", "comma-separated IPs/CIDRs allowed to send PROXY headers; others are refused (empty: any peer)")
	flag.IntVar(&c.maxConns, "max-conns", 1024, "maximum open connections")
	flag.IntVar(&c.maxConnsPerIP, "max-conns-per-ip", 32, "maximum open connections per client IP (IPv6: per /64); 0 disables")
	var level slog.Level
	flag.TextVar(&level, "log-level", slog.LevelInfo, "minimum log level: DEBUG, INFO, WARN or ERROR")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	if err := run(c); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func run(c config) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	root, err := fs.Sub(embedded, "site")
	if err != nil {
		return err
	}
	st, err := loadSite(root)
	if err != nil {
		return err
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		// TLS 1.2 only: forward-secret AEAD suites. TLS 1.3 suites aren't configurable.
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		},
	}
	if c.devCert {
		cert, err := selfSigned()
		if err != nil {
			return err
		}
		tlsConfig.Certificates = []tls.Certificate{*cert}
	} else {
		certs := &certLoader{certPath: c.certPath, keyPath: c.keyPath}
		if _, err := certs.load(); err != nil {
			return err
		}
		go certs.watch(ctx, time.Minute)
		tlsConfig.GetCertificate = certs.get
	}

	ih := ipHost{name: strings.ToLower(c.ipHost), pageURL: c.ipPage}
	trusted, err := parsePrefixes(c.trustedProxies)
	if err != nil {
		return fmt.Errorf("-trusted-proxies: %w", err)
	}

	srv, err := newServer(tlsConfig, logRequests(slog.Default(), routes(st, ih)))
	if err != nil {
		return err
	}
	srv.perIP = newIPLimiter(c.maxConnsPerIP)

	ln, err := net.Listen("tcp", c.addr)
	if err != nil {
		return err
	}
	ln = netutil.LimitListener(ln, c.maxConns)
	if c.proxyProto {
		pl, err := newProxyListener(ln, c.trustedProxies)
		if err != nil {
			ln.Close()
			return err
		}
		if pl.ConnPolicy == nil {
			slog.Warn("no -trusted-proxies: any peer can set the client address with a PROXY header")
		}
		ln = pl
	}

	health := newHealthServer(c.healthAddr)
	go func() {
		if err := health.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("health server", "err", err)
			stop()
		}
	}()

	var plain *http.Server
	if c.httpAddr != "" {
		plain = newPlainServer(c.httpAddr, realIP(trusted, logRequests(slog.Default(), plainRoutes(ih))))
		go func() {
			if err := plain.ListenAndServe(); err != http.ErrServerClosed {
				slog.Error("plain HTTP server", "err", err)
				stop()
			}
		}()
	}

	slog.Info("listening", "addr", ln.Addr().String(), "proxy_protocol", c.proxyProto, "files", len(st.files))
	go srv.serve(ln)

	<-ctx.Done()
	slog.Info("shutting down")
	ln.Close()
	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	health.Shutdown(sctx)
	if plain != nil {
		plain.Shutdown(sctx)
	}
	return srv.shutdown(sctx)
}

// newProxyListener reads PROXY headers on ln. If trusted (comma-separated IPs
// and CIDRs) is set, peers in it must send a header and all others are
// refused; if empty, a header is required from any peer.
func newProxyListener(ln net.Listener, trusted string) (*proxyproto.Listener, error) {
	pl := &proxyproto.Listener{Listener: ln, ReadHeaderTimeout: 5 * time.Second}
	var ranges []string
	for _, r := range strings.Split(trusted, ",") {
		if r = strings.TrimSpace(r); r != "" {
			ranges = append(ranges, r)
		}
	}
	if len(ranges) == 0 {
		if strings.TrimSpace(trusted) != "" {
			return nil, fmt.Errorf("-trusted-proxies %q lists no addresses", trusted)
		}
		return pl, nil
	}
	policy, err := proxyproto.TrustProxyHeaderFromRanges(ranges)
	if err != nil {
		return nil, err
	}
	pl.ConnPolicy = policy
	return pl, nil
}

// newHealthServer serves /healthz on addr.
func newHealthServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    4 << 10,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte("ok\n"))
		}),
	}
}
