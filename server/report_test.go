package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// hostileHello is a ClientHello that fills the recorded handshake with as
// many list entries as fit, using long names where it can.
func hostileHello(t *testing.T) []byte {
	t.Helper()
	var b cryptobyte.Builder
	b.AddUint8(handshakeClientHel)
	b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint16(0x0303)
		b.AddBytes(make([]byte, 32))
		b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) {})
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			for range 2000 {
				b.AddUint16(0xcca9) // TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256
			}
		})
		b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint8(0) })
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			ext(b, extGroups, func(b *cryptobyte.Builder) {
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
					for range 600 {
						b.AddUint16(0x11ec)
					}
				})
			})
			ext(b, extSigAlgs, func(b *cryptobyte.Builder) {
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
					for range 600 {
						b.AddUint16(0x0403)
					}
				})
			})
			ext(b, extKeyShare, func(b *cryptobyte.Builder) {
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
					for range 300 {
						b.AddUint16(0x11ec)
						b.AddUint16(0)
					}
				})
			})
			ext(b, extALPN, func(b *cryptobyte.Builder) {
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
					for range 4 {
						b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(bytes.Repeat([]byte{1}, 255)) })
					}
					for range 600 {
						b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) {})
					}
				})
			})
			// Empty ALPN lists: the longest extension name, 4+2 bytes each.
			for range 450 {
				ext(b, extALPN, func(b *cryptobyte.Builder) { b.AddUint16(0) })
			}
		})
	})
	rec := records(b.BytesOrPanic(), 16384)
	if len(rec) > maxHelloBytes {
		t.Fatalf("hostile hello is %d bytes, more than is recorded", len(rec))
	}
	return rec
}

// hostileStream is a full recording of HTTP/2 frames: a large SETTINGS
// frame, PRIORITY frames, then a HEADERS block of many escaped fields.
func hostileStream(t *testing.T) []byte {
	t.Helper()
	var hb bytes.Buffer
	enc := hpack.NewEncoder(&hb)
	enc.WriteField(hpack.HeaderField{Name: ":method", Value: "GET"})
	for hb.Len() < 12<<10 {
		enc.WriteField(hpack.HeaderField{Name: "x", Value: strings.Repeat(`"`, 100), Sensitive: true})
	}
	block := hb.Bytes()

	var out bytes.Buffer
	out.WriteString(h2Preface)
	fr := http2.NewFramer(&out, nil)
	settings := make([]http2.Setting, 400)
	for i := range settings {
		settings[i] = http2.Setting{ID: http2.SettingMaxHeaderListSize, Val: 4294967295}
	}
	fr.WriteSettings(settings...)
	fr.WriteWindowUpdate(0, 2147483647)
	for out.Len()+len(block)+64 < maxStreamBytes {
		fr.WritePriority(2147483647, http2.PriorityParam{StreamDep: 2147483647, Exclusive: true, Weight: 255})
	}
	fr.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: block, EndHeaders: true, EndStream: true})
	if out.Len() > maxStreamBytes {
		t.Fatalf("hostile stream is %d bytes, more than is recorded", out.Len())
	}
	return out.Bytes()
}

func h2State() tls.ConnectionState {
	return tls.ConnectionState{Version: tls.VersionTLS13, CipherSuite: tls.TLS_AES_128_GCM_SHA256, NegotiatedProtocol: "h2", ServerName: "localhost"}
}

func TestReportBoundedForHostileInput(t *testing.T) {
	ci := &connInfo{hello: hostileHello(t), state: h2State(), stream: &streamRecorder{buf: hostileStream(t)}}
	b := ci.reportJSON("[2001:db8::1]:443")
	t.Logf("hostile report: %d bytes", len(b))
	if len(b) > maxReportBytes {
		t.Fatalf("report is %d bytes, limit %d", len(b), maxReportBytes)
	}
	var rep report
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if !rep.Truncated {
		t.Error("truncated not set")
	}
	if rep.TLS.JA4 == "" || rep.TLS.JA3Hash == "" || rep.HTTP.H2 == nil || rep.HTTP.H2.AkamaiHash == "" {
		t.Errorf("fingerprints missing: %+v %+v", rep.TLS, rep.HTTP.H2)
	}
}

// Each capped section on its own stays informative and within its caps.
func TestReportCapsLists(t *testing.T) {
	body, err := helloFromRecords(hostileHello(t))
	if err != nil {
		t.Fatal(err)
	}
	h, err := parseClientHello(body)
	if err != nil {
		t.Fatal(err)
	}
	if !h.Truncated || len(h.CipherSuites) != maxHelloItems || len(h.Extensions) > maxHelloItems || len(h.ALPN) > maxHelloItems {
		t.Errorf("parse caps: truncated=%v suites=%d exts=%d alpn=%d", h.Truncated, len(h.CipherSuites), len(h.Extensions), len(h.ALPN))
	}
	if fp, _ := ja4(h); !strings.HasPrefix(fp, "t12i9999") {
		t.Errorf("ja4 = %s", fp)
	}
	out, cut := describeHello(h)
	if !cut || len(out.CipherSuites) != maxListOut || len(out.Extensions) != maxListOut || len(out.ALPN) != maxListOut {
		t.Errorf("display caps: cut=%v suites=%d exts=%d alpn=%d", cut, len(out.CipherSuites), len(out.Extensions), len(out.ALPN))
	}

	o, err := parseH2Opening(hostileStream(t))
	if err != nil {
		t.Fatal(err)
	}
	if !o.Truncated || len(o.Priorities) != maxListOut || len(o.Settings) != maxListOut || len(o.Headers) > maxListOut {
		t.Errorf("h2 caps: truncated=%v prio=%d settings=%d headers=%d", o.Truncated, len(o.Priorities), len(o.Settings), len(o.Headers))
	}
	if !strings.HasSuffix(o.Akamai, "|m") {
		t.Errorf("akamai = %.80s...", o.Akamai)
	}

	var req strings.Builder
	req.WriteString("GET / HTTP/1.1\r\n")
	for range 500 {
		req.WriteString("X: y\r\n")
	}
	req.WriteString("\r\n")
	_, headers, cut, err := parseH1Headers([]byte(req.String()))
	if err != nil || !cut || len(headers) != maxListOut {
		t.Errorf("h1 caps: err=%v cut=%v headers=%d", err, cut, len(headers))
	}
}

func TestReportBuiltOncePerConnection(t *testing.T) {
	ci := &connInfo{hello: records(sampleHello(), 16384), state: h2State(), stream: &streamRecorder{buf: hostileStream(t)}}
	first := ci.reportJSON("203.0.113.7:1")
	if ci.hello != nil || ci.stream.buf != nil {
		t.Error("recordings kept after the report was built")
	}
	second := ci.reportJSON("198.51.100.1:2")
	if &first[0] != &second[0] {
		t.Error("report rebuilt on the second call")
	}
	if allocs := testing.AllocsPerRun(100, func() { ci.reportJSON("203.0.113.7:1") }); allocs != 0 {
		t.Errorf("cached report allocates %v times", allocs)
	}
}

func TestStreamRecorderStops(t *testing.T) {
	r := &streamRecorder{}
	r.record([]byte("first"))
	r.stop()
	r.record([]byte("second"))
	if got := string(r.take()); got != "first" {
		t.Errorf("recorded %q", got)
	}
	if r.buf != nil {
		t.Error("buffer kept after take")
	}
}

func TestHelloRecorderHandsOver(t *testing.T) {
	r := &helloRecorder{buf: []byte("hello")}
	if got := string(r.stop()); got != "hello" {
		t.Errorf("stop returned %q", got)
	}
	if r.buf != nil {
		t.Error("recorder kept the hello")
	}
}
