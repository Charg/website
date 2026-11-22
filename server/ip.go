package main

import (
	"encoding/xml"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// ipFormats maps a format name, as in /ip.json or ip.syn.sh/json, to its
// media type.
var ipFormats = map[string]string{
	"txt":  "text/plain; charset=utf-8",
	"json": "application/json",
	"yaml": "application/yaml",
	"toml": "application/toml",
	"xml":  "application/xml; charset=utf-8",
}

// ipMainPath parses a path on the main host: /ip or /ip.<format>. ok is
// false for any other path; format is "" for /ip.
func ipMainPath(p string) (format string, ok bool) {
	if p == "/ip" {
		return "", true
	}
	f, found := strings.CutPrefix(p, "/ip.")
	if !found || ipFormats[f] == "" {
		return "", false
	}
	return f, true
}

// ipHostPath parses a path on the IP host: /, /<format>, /ip or /ip.<format>.
func ipHostPath(p string) (format string, ok bool) {
	if p == "/" {
		return "", true
	}
	if f, ok := ipMainPath(p); ok {
		return f, true
	}
	f := strings.TrimPrefix(p, "/")
	if ipFormats[f] == "" {
		return "", false
	}
	return f, true
}

// wantsHTML reports whether the client asked for a web page. curl sends
// */*, so it gets text.
func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// clientIP is the request's remote address, without port or zone, with
// IPv4-mapped IPv6 addresses unmapped.
func clientIP(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap().WithZone(""), true
}

func ipVersion(ip netip.Addr) int {
	if ip.Is4() {
		return 4
	}
	return 6
}

type ipXML struct {
	XMLName xml.Name `xml:"client"`
	IP      string   `xml:"ip"`
	Version int      `xml:"version"`
}

// encodeIP renders ip in format. The address only holds hex digits, dots
// and colons, so the hand-written formats need no escaping.
func encodeIP(ip netip.Addr, format string) []byte {
	s, v := ip.String(), ipVersion(ip)
	switch format {
	case "json":
		return indentJSON(struct {
			IP      string `json:"ip"`
			Version int    `json:"version"`
		}{s, v})
	case "yaml":
		return fmt.Appendf(nil, "ip: %q\nversion: %d\n", s, v)
	case "toml":
		return fmt.Appendf(nil, "ip = %q\nversion = %d\n", s, v)
	case "xml":
		b, err := xml.MarshalIndent(ipXML{IP: s, Version: v}, "", "  ")
		if err != nil {
			return nil
		}
		return append(append([]byte(xml.Header), b...), '\n')
	}
	return []byte(s + "\n")
}

// serveIP answers with the client's address in format. An empty format
// means /ip itself: a page for browsers, text for everything else.
func serveIP(w http.ResponseWriter, r *http.Request, format string, page func(netip.Addr) []byte) {
	h := w.Header()
	ip, ok := clientIP(r)
	if !ok {
		http.Error(w, "client address unavailable", http.StatusServiceUnavailable)
		return
	}
	h.Set("Cache-Control", "no-store")
	var b []byte
	switch {
	case format == "" && wantsHTML(r):
		h.Set("Vary", "Accept")
		h.Set("Content-Type", "text/html; charset=utf-8")
		b = page(ip)
	default:
		if format == "" {
			h.Set("Vary", "Accept")
			format = "txt"
		}
		// Machine formats are for anyone's scripts, from any origin.
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("Content-Type", ipFormats[format])
		b = encodeIP(ip, format)
	}
	h.Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(b)
	}
}

// ipHost serves the IP host, which answers nothing but the address. A
// browser asking for the page is sent to the main site's copy, which has the
// site's styles and scripts.
type ipHost struct {
	name    string // host name, e.g. ip.syn.sh; "" disables the IP host
	pageURL string // the main site's IP page
}

func (ih ipHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	format, ok := ipHostPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if format == "" && wantsHTML(r) {
		w.Header().Set("Vary", "Accept")
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, ih.pageURL, http.StatusFound)
		return
	}
	serveIP(w, r, format, nil)
}

// ipPage fills in the IP page template.
func (s *site) ipPage(ip netip.Addr) []byte {
	return []byte(strings.NewReplacer(
		"{{ip}}", html.EscapeString(ip.String()),
		"{{version}}", strconv.Itoa(ipVersion(ip)),
	).Replace(s.ipTmpl))
}

// hostName is r.Host without a port, lowercased.
func hostName(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}
