package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
)

type namedID struct {
	ID   uint16 `json:"id"`
	Name string `json:"name"`
}

type tlsReport struct {
	Version     string    `json:"version"`
	Cipher      string    `json:"cipher"`
	ALPN        string    `json:"alpn"`
	ServerName  string    `json:"server_name"`
	JA3         string    `json:"ja3,omitempty"`
	JA3Hash     string    `json:"ja3_hash,omitempty"`
	JA4         string    `json:"ja4,omitempty"`
	JA4Raw      string    `json:"ja4_r,omitempty"`
	ClientHello *helloOut `json:"client_hello,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type helloOut struct {
	Version      string    `json:"version"`
	CipherSuites []namedID `json:"cipher_suites"`
	Extensions   []namedID `json:"extensions"`
	Groups       []namedID `json:"groups"`
	KeyShares    []namedID `json:"key_shares"`
	SigAlgs      []namedID `json:"signature_algorithms"`
	Versions     []namedID `json:"supported_versions"`
	ALPN         []string  `json:"alpn"`
	PointFormats []uint8   `json:"point_formats"`
	PSKModes     []uint8   `json:"psk_key_exchange_modes"`
	CertComp     []namedID `json:"certificate_compression"`
}

type httpReport struct {
	Version   string      `json:"version"`
	FirstLine string      `json:"first_request,omitempty"`
	Headers   [][2]string `json:"headers,omitempty"`
	H2        *h2Opening  `json:"h2,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type report struct {
	IP        string     `json:"ip"`
	TLS       tlsReport  `json:"tls"`
	HTTP      httpReport `json:"http"`
	Truncated bool       `json:"truncated,omitempty"` // some lists or values were cut
}

const (
	maxListOut     = 64       // entries shown per list
	maxReportBytes = 64 << 10 // encoded report size limit
	maxNameBytes   = 255      // longest server name shown
)

// serveConnection answers with what this connection revealed: the client's
// address, its TLS ClientHello and its opening HTTP frames. The report is
// built once per connection and kept only while it is open.
func serveConnection(w http.ResponseWriter, r *http.Request) {
	ci := connInfoFrom(r.Context())
	if ci == nil {
		http.Error(w, "connection details unavailable", http.StatusServiceUnavailable)
		return
	}
	b := ci.reportJSON(r.RemoteAddr)
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	w.Write(b)
}

// encodeReport writes rep as indented JSON. A report over maxReportBytes is
// replaced by its summary.
func encodeReport(rep report) []byte {
	b := indentJSON(rep)
	if len(b) > maxReportBytes {
		b = indentJSON(rep.summary())
	}
	return b
}

func indentJSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return []byte("{}\n")
	}
	return append(b, '\n')
}

// summary drops everything whose size the client controls, keeping the
// fixed-size fingerprints.
func (r report) summary() report {
	r.Truncated = true
	r.TLS.JA3, r.TLS.JA4Raw, r.TLS.ClientHello = "", "", nil
	r.HTTP.FirstLine, r.HTTP.Headers = "", nil
	if o := r.HTTP.H2; o != nil {
		r.HTTP.H2 = &h2Opening{WindowUpdate: o.WindowUpdate, Akamai: o.Akamai, AkamaiHash: o.AkamaiHash}
	}
	return r
}

func buildReport(ci *connInfo, remoteAddr string, stream []byte) report {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		ip = remoteAddr
	}
	rep := report{IP: ip}

	st := ci.state
	rep.TLS = tlsReport{
		Version:    tls.VersionName(st.Version),
		Cipher:     tls.CipherSuiteName(st.CipherSuite),
		ALPN:       st.NegotiatedProtocol,
		ServerName: st.ServerName,
	}
	if len(rep.TLS.ServerName) > maxNameBytes {
		rep.TLS.ServerName, rep.Truncated = rep.TLS.ServerName[:maxNameBytes], true
	}
	if body, err := helloFromRecords(ci.hello); err != nil {
		rep.TLS.Error = err.Error()
	} else if h, err := parseClientHello(body); err != nil {
		rep.TLS.Error = err.Error()
	} else {
		rep.TLS.JA3, rep.TLS.JA3Hash = ja3(h)
		rep.TLS.JA4, rep.TLS.JA4Raw = ja4(h)
		var cut bool
		rep.TLS.ClientHello, cut = describeHello(h)
		rep.Truncated = rep.Truncated || cut
	}

	if st.NegotiatedProtocol == "h2" {
		rep.HTTP.Version = "HTTP/2"
		if o, err := parseH2Opening(stream); err != nil {
			rep.HTTP.Error = err.Error()
		} else {
			rep.HTTP.H2 = o
			rep.HTTP.Headers = o.Headers
			rep.Truncated = rep.Truncated || o.Truncated
		}
	} else {
		rep.HTTP.Version = "HTTP/1.1"
		if line, headers, cut, err := parseH1Headers(stream); err != nil {
			rep.HTTP.Error = err.Error()
		} else {
			rep.HTTP.FirstLine, rep.HTTP.Headers = line, headers
			rep.Truncated = rep.Truncated || cut
		}
	}
	return rep
}

// describeHello names the ClientHello's lists, at most maxListOut entries
// each. cut reports whether any list was shortened, here or when parsed.
func describeHello(h *clientHello) (out *helloOut, cut bool) {
	cut = h.Truncated
	named := func(vs []uint16, name func(uint16) string) []namedID {
		vs = clip(vs, &cut)
		out := make([]namedID, len(vs))
		for i, v := range vs {
			n := name(v)
			if isGREASE(v) {
				n = "GREASE"
			}
			out[i] = namedID{v, n}
		}
		return out
	}
	out = &helloOut{
		Version:      tls.VersionName(h.Version),
		CipherSuites: named(h.CipherSuites, tls.CipherSuiteName),
		Extensions:   named(h.Extensions, extensionName),
		Groups:       named(h.Groups, groupName),
		KeyShares:    named(h.KeyShares, groupName),
		SigAlgs:      named(h.SigAlgs, func(v uint16) string { return tls.SignatureScheme(v).String() }),
		Versions:     named(h.Versions, tls.VersionName),
		ALPN:         clip(h.ALPN, &cut),
		PointFormats: clip(h.PointFormats, &cut),
		PSKModes:     clip(h.PSKModes, &cut),
		CertComp:     named(h.CertCompAlgs, certCompName),
	}
	return out, cut
}

// clip shortens s to maxListOut entries, setting cut if it did.
func clip[T any](s []T, cut *bool) []T {
	if len(s) > maxListOut {
		*cut = true
		return s[:maxListOut]
	}
	return s
}

func groupName(v uint16) string {
	switch v {
	case 0x11ec:
		return "X25519MLKEM768"
	case 0x6399:
		return "X25519Kyber768Draft00"
	case 0x0100:
		return "ffdhe2048"
	case 0x0101:
		return "ffdhe3072"
	}
	return tls.CurveID(v).String()
}

func certCompName(v uint16) string {
	switch v {
	case 1:
		return "zlib"
	case 2:
		return "brotli"
	case 3:
		return "zstd"
	}
	return fmt.Sprintf("0x%04X", v)
}

// IANA TLS ExtensionType values, the ones browsers send.
var extensionNames = map[uint16]string{
	0x0000: "server_name",
	0x0005: "status_request",
	0x000a: "supported_groups",
	0x000b: "ec_point_formats",
	0x000d: "signature_algorithms",
	0x0010: "application_layer_protocol_negotiation",
	0x0012: "signed_certificate_timestamp",
	0x0015: "padding",
	0x0016: "encrypt_then_mac",
	0x0017: "extended_master_secret",
	0x001b: "compress_certificate",
	0x001c: "record_size_limit",
	0x0022: "delegated_credentials",
	0x0023: "session_ticket",
	0x0029: "pre_shared_key",
	0x002a: "early_data",
	0x002b: "supported_versions",
	0x002d: "psk_key_exchange_modes",
	0x0031: "post_handshake_auth",
	0x0033: "key_share",
	0x44cd: "application_settings",
	0x4469: "application_settings_old",
	0xfe0d: "encrypted_client_hello",
	0xff01: "renegotiation_info",
}

func extensionName(v uint16) string {
	if n, ok := extensionNames[v]; ok {
		return n
	}
	return fmt.Sprintf("0x%04X", v)
}
