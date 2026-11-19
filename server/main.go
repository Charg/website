// Command server serves the website over TLS, behind a TCP passthrough that
// sends the PROXY protocol.
package main

import (
	"context"
	"crypto/tls"
	"embed"
	"flag"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pires/go-proxyproto"
	"golang.org/x/net/netutil"
)

//go:generate go run ./cmd/build -root ..

//go:embed all:site
var embedded embed.FS

func main() {
	addr := flag.String("addr", ":8443", "TLS listen address")
	healthAddr := flag.String("health-addr", ":8081", "plain HTTP address for /healthz")
	certPath := flag.String("cert", "/tls/tls.crt", "certificate file")
	keyPath := flag.String("key", "/tls/tls.key", "private key file")
	devCert := flag.Bool("self-signed", false, "use a throwaway localhost certificate instead of -cert/-key")
	proxyProto := flag.Bool("proxy-protocol", true, "require a PROXY protocol header on every connection")
	maxConns := flag.Int("max-conns", 1024, "maximum open connections")
	flag.Parse()

	if err := run(*addr, *healthAddr, *certPath, *keyPath, *devCert, *proxyProto, *maxConns); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func run(addr, healthAddr, certPath, keyPath string, devCert, proxyProto bool, maxConns int) error {
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
	if devCert {
		c, err := selfSigned()
		if err != nil {
			return err
		}
		tlsConfig.Certificates = []tls.Certificate{*c}
	} else {
		certs := &certLoader{certPath: certPath, keyPath: keyPath}
		if _, err := certs.load(); err != nil {
			return err
		}
		go certs.watch(ctx, time.Minute)
		tlsConfig.GetCertificate = certs.get
	}

	srv, err := newServer(tlsConfig, routes(st))
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	ln = netutil.LimitListener(ln, maxConns)
	if proxyProto {
		ln = &proxyproto.Listener{Listener: ln, ReadHeaderTimeout: 5 * time.Second}
	}

	health := &http.Server{
		Addr:              healthAddr,
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte("ok\n"))
		}),
	}
	go func() {
		if err := health.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("health server", "err", err)
			stop()
		}
	}()

	slog.Info("listening", "addr", ln.Addr().String(), "proxy_protocol", proxyProto, "files", len(st.files))
	go srv.serve(ln)

	<-ctx.Done()
	slog.Info("shutting down")
	ln.Close()
	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	health.Shutdown(sctx)
	return srv.shutdown(sctx)
}
