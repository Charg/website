package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"os"
	"sync/atomic"
	"time"
)

// certLoader serves a key pair from disk and picks up renewals. Kubernetes
// swaps both files of a mounted secret at once, so a mismatched read is
// rejected and retried on the next tick.
type certLoader struct {
	certPath, keyPath string
	cert              atomic.Pointer[tls.Certificate]
	certPEM, keyPEM   []byte
}

func (l *certLoader) load() (changed bool, err error) {
	c, err := os.ReadFile(l.certPath)
	if err != nil {
		return false, err
	}
	k, err := os.ReadFile(l.keyPath)
	if err != nil {
		return false, err
	}
	if bytes.Equal(c, l.certPEM) && bytes.Equal(k, l.keyPEM) {
		return false, nil
	}
	pair, err := tls.X509KeyPair(c, k)
	if err != nil {
		return false, err
	}
	l.cert.Store(&pair)
	l.certPEM, l.keyPEM = c, k
	return true, nil
}

func (l *certLoader) watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			changed, err := l.load()
			if err != nil {
				slog.Warn("certificate reload failed", "err", err)
			} else if changed {
				slog.Info("certificate reloaded")
			}
		}
	}
}

func (l *certLoader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c := l.cert.Load()
	if c == nil {
		return nil, errors.New("no certificate loaded")
	}
	return c, nil
}

// selfSigned returns a short-lived certificate for localhost, for local runs.
func selfSigned() (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
