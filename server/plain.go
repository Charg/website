package main

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// plainRoutes serves the IP host over plain HTTP, so that `curl ip.syn.sh`
// works. Wrap it in realIP: it sits behind a reverse proxy.
func plainRoutes(ih ipHost) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if ih.name == "" || hostName(r) != ih.name {
			http.NotFound(w, r)
			return
		}
		ih.ServeHTTP(w, r)
	})
}

// realIP sets r.RemoteAddr from X-Real-Ip when the peer is trusted, so later
// handlers and the request log see the client. Otherwise the header is
// ignored.
func realIP(trusted []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peer, ok := clientIP(r); ok && contains(trusted, peer) {
			if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-Ip"))); err == nil {
				r2 := *r
				r2.RemoteAddr = netip.AddrPortFrom(ip.Unmap().WithZone(""), 0).String()
				r = &r2
			}
		}
		next.ServeHTTP(w, r)
	})
}

func contains(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// parsePrefixes parses comma-separated IPs and CIDRs.
func parsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
			continue
		}
		ip, err := netip.ParseAddr(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP or CIDR", f)
		}
		out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
	}
	return out, nil
}

func newPlainServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
}
