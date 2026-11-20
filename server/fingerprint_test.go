package main

import (
	"bytes"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

func hexList(t *testing.T, s string) []uint16 {
	t.Helper()
	var out []uint16
	for _, p := range strings.Split(s, ",") {
		b, err := hex.DecodeString(p)
		if err != nil || len(b) != 2 {
			t.Fatalf("bad hex %q", p)
		}
		out = append(out, uint16(b[0])<<8|uint16(b[1]))
	}
	return out
}

// The worked example from the JA4 technical details.
func TestJA4SpecExample(t *testing.T) {
	ciphers := hexList(t, "002f,0035,009c,009d,1301,1302,1303,c013,c014,c02b,c02c,c02f,c030,cca8,cca9")
	exts := hexList(t, "0005,000a,000b,000d,0012,0015,0017,001b,0023,002b,002d,0033,4469,ff01")
	rev := slices.Clone(ciphers)
	slices.Reverse(rev)
	h := &clientHello{
		Version: 0x0303,
		// GREASE and reordering must not change the result.
		CipherSuites: append([]uint16{0x1a1a}, rev...),
		Extensions:   append([]uint16{0x2a2a, extServerName, extALPN}, exts...),
		SigAlgs:      hexList(t, "0403,0804,0401,0503,0805,0501,0806,0601"),
		Versions:     []uint16{0x3a3a, 0x0304, 0x0303},
		ALPN:         []string{"h2", "http/1.1"},
	}
	fp, raw := ja4(h)
	if want := "t13d1516h2_8daaf6152771_e5627efa2ab1"; fp != want {
		t.Errorf("ja4 = %s, want %s", fp, want)
	}
	wantRaw := "t13d1516h2_002f,0035,009c,009d,1301,1302,1303,c013,c014,c02b,c02c,c02f,c030,cca8,cca9_0005,000a,000b,000d,0012,0015,0017,001b,0023,002b,002d,0033,4469,ff01_0403,0804,0401,0503,0805,0501,0806,0601"
	if raw != wantRaw {
		t.Errorf("ja4_r = %s, want %s", raw, wantRaw)
	}
}

func TestJA4Edges(t *testing.T) {
	h := &clientHello{Version: 0x0303, CipherSuites: []uint16{0x002f}}
	if fp, _ := ja4(h); fp != "t12i010000_ba72b8082249_000000000000" {
		// b = sha256("002f")[:12]
		t.Errorf("no extensions: %s", fp)
	}
	if got := ja4ALPN([]string{"\x01x\xff"}); got != "0f" {
		t.Errorf("non-alnum ALPN = %q, want 0f", got)
	}
	if got := ja4ALPN([]string{"http/1.1"}); got != "h1" {
		t.Errorf("http/1.1 ALPN = %q", got)
	}
}

func TestJA3(t *testing.T) {
	h := &clientHello{
		Version:      0x0303,
		CipherSuites: []uint16{0x0a0a, 4865, 4866},
		Extensions:   []uint16{0xfafa, 0, 23, 65281},
		Groups:       []uint16{0x2a2a, 29, 23},
		PointFormats: []uint8{0},
	}
	s, sum := ja3(h)
	if want := "771,4865-4866,0-23-65281,29-23,0"; s != want {
		t.Errorf("ja3 = %s, want %s", s, want)
	}
	if len(sum) != 32 {
		t.Errorf("ja3 hash = %s", sum)
	}
}

func TestIsGREASE(t *testing.T) {
	for _, v := range []uint16{0x0a0a, 0x1a1a, 0xfafa} {
		if !isGREASE(v) {
			t.Errorf("%04x should be GREASE", v)
		}
	}
	for _, v := range []uint16{0x0a1a, 0x1301, 0x000a} {
		if isGREASE(v) {
			t.Errorf("%04x should not be GREASE", v)
		}
	}
}

// buildHello encodes a ClientHello handshake message.
func buildHello(exts func(b *cryptobyte.Builder)) []byte {
	var b cryptobyte.Builder
	b.AddUint8(handshakeClientHel)
	b.AddUint24LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddUint16(0x0303)
		b.AddBytes(make([]byte, 32))
		b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(make([]byte, 32)) })
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			for _, c := range []uint16{0x2a2a, 0x1301, 0x1302, 0xc02b} {
				b.AddUint16(c)
			}
		})
		b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint8(0) })
		b.AddUint16LengthPrefixed(exts)
	})
	return b.BytesOrPanic()
}

func ext(b *cryptobyte.Builder, typ uint16, body func(b *cryptobyte.Builder)) {
	b.AddUint16(typ)
	b.AddUint16LengthPrefixed(body)
}

func sampleHello() []byte {
	return buildHello(func(b *cryptobyte.Builder) {
		ext(b, 0x3a3a, func(b *cryptobyte.Builder) {})
		ext(b, extServerName, func(b *cryptobyte.Builder) {
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddUint8(0)
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes([]byte("syn.sh")) })
			})
		})
		ext(b, extGroups, func(b *cryptobyte.Builder) {
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint16(0x11ec); b.AddUint16(29) })
		})
		ext(b, extPointFormats, func(b *cryptobyte.Builder) {
			b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint8(0) })
		})
		ext(b, extSigAlgs, func(b *cryptobyte.Builder) {
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint16(0x0403); b.AddUint16(0x0804) })
		})
		ext(b, extALPN, func(b *cryptobyte.Builder) {
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				for _, p := range []string{"h2", "http/1.1"} {
					b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes([]byte(p)) })
				}
			})
		})
		ext(b, extSupportedVers, func(b *cryptobyte.Builder) {
			b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint16(0x0304); b.AddUint16(0x0303) })
		})
		ext(b, extKeyShare, func(b *cryptobyte.Builder) {
			b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
				b.AddUint16(29)
				b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(make([]byte, 32)) })
			})
		})
		ext(b, extPSKModes, func(b *cryptobyte.Builder) {
			b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint8(1) })
		})
		ext(b, extCompressCert, func(b *cryptobyte.Builder) {
			b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint16(2) })
		})
	})
}

// records splits a handshake message into TLS records of at most n bytes.
func records(msg []byte, n int) []byte {
	var out []byte
	for len(msg) > 0 {
		k := min(n, len(msg))
		out = append(out, recordHandshake, 3, 1, byte(k>>8), byte(k))
		out = append(out, msg[:k]...)
		msg = msg[k:]
	}
	return out
}

func TestParseClientHello(t *testing.T) {
	msg := sampleHello()
	for _, size := range []int{16384, 100, 7} {
		body, err := helloFromRecords(records(msg, size))
		if err != nil {
			t.Fatalf("records of %d: %v", size, err)
		}
		h, err := parseClientHello(body)
		if err != nil {
			t.Fatal(err)
		}
		if h.ServerName != "syn.sh" ||
			!slices.Equal(h.CipherSuites, []uint16{0x2a2a, 0x1301, 0x1302, 0xc02b}) ||
			!slices.Equal(h.Extensions, []uint16{0x3a3a, 0, 10, 11, 13, 16, 43, 51, 45, 27}) ||
			!slices.Equal(h.Groups, []uint16{0x11ec, 29}) ||
			!slices.Equal(h.SigAlgs, []uint16{0x0403, 0x0804}) ||
			!slices.Equal(h.ALPN, []string{"h2", "http/1.1"}) ||
			!slices.Equal(h.Versions, []uint16{0x0304, 0x0303}) ||
			!slices.Equal(h.KeyShares, []uint16{29}) ||
			!slices.Equal(h.PSKModes, []uint8{1}) ||
			!slices.Equal(h.CertCompAlgs, []uint16{2}) ||
			!slices.Equal(h.PointFormats, []uint8{0}) {
			t.Errorf("parsed %+v", h)
		}
		if fp, _ := ja4(h); !strings.HasPrefix(fp, "t13d0309h2_") {
			t.Errorf("ja4 = %s", fp)
		}
	}
}

func TestParseClientHelloRejectsBadInput(t *testing.T) {
	msg := sampleHello()
	cases := map[string][]byte{
		"empty":         nil,
		"truncated":     records(msg[:len(msg)-5], 16384),
		"not handshake": append([]byte{23, 3, 3, 0, 1}, 0),
		"server hello":  records(append([]byte{2}, msg[1:]...), 16384),
	}
	for name, in := range cases {
		if body, err := helloFromRecords(in); err == nil {
			if _, err := parseClientHello(body); err == nil {
				t.Errorf("%s: no error", name)
			}
		}
	}
	// Trailing bytes inside an extension.
	bad := buildHello(func(b *cryptobyte.Builder) {
		ext(b, extSupportedVers, func(b *cryptobyte.Builder) {
			b.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) { b.AddUint16(0x0304) })
			b.AddUint8(9)
		})
	})
	if _, err := parseClientHello(bad[4:]); err == nil {
		t.Error("trailing extension bytes: no error")
	}
}

func h2Bytes(t *testing.T, write func(fr *http2.Framer, enc *hpack.Encoder, hb *bytes.Buffer)) []byte {
	t.Helper()
	var out, hb bytes.Buffer
	out.WriteString(h2Preface)
	fr := http2.NewFramer(&out, nil)
	write(fr, hpack.NewEncoder(&hb), &hb)
	return out.Bytes()
}

func TestParseH2Opening(t *testing.T) {
	b := h2Bytes(t, func(fr *http2.Framer, enc *hpack.Encoder, hb *bytes.Buffer) {
		fr.WriteSettings(
			http2.Setting{ID: http2.SettingHeaderTableSize, Val: 65536},
			http2.Setting{ID: http2.SettingEnablePush, Val: 0},
			http2.Setting{ID: http2.SettingInitialWindowSize, Val: 6291456},
			http2.Setting{ID: http2.SettingMaxHeaderListSize, Val: 262144},
		)
		fr.WriteWindowUpdate(0, 15663105)
		fr.WritePriority(3, http2.PriorityParam{StreamDep: 0, Weight: 200})
		for _, f := range [][2]string{{":method", "GET"}, {":authority", "syn.sh"}, {":scheme", "https"}, {":path", "/"}, {"user-agent", "test"}} {
			enc.WriteField(hpack.HeaderField{Name: f[0], Value: f[1]})
		}
		// Split across HEADERS + CONTINUATION.
		block := hb.Bytes()
		fr.WriteHeaders(http2.HeadersFrameParam{
			StreamID: 1, BlockFragment: block[:5], EndStream: true,
			Priority: http2.PriorityParam{StreamDep: 0, Exclusive: true, Weight: 255},
		})
		fr.WriteContinuation(1, true, block[5:])
	})
	o, err := parseH2Opening(b)
	if err != nil {
		t.Fatal(err)
	}
	if want := "1:65536;2:0;4:6291456;6:262144|15663105|3:0:0:201|m,a,s,p"; o.Akamai != want {
		t.Errorf("akamai = %s, want %s", o.Akamai, want)
	}
	if o.HeaderPrio == nil || o.HeaderPrio.Weight != 256 || !o.HeaderPrio.Exclusive {
		t.Errorf("headers priority = %+v", o.HeaderPrio)
	}
	if len(o.Headers) != 5 || o.Headers[4] != [2]string{"user-agent", "test"} {
		t.Errorf("headers = %v", o.Headers)
	}
}

func TestParseH2OpeningNoHeaders(t *testing.T) {
	b := h2Bytes(t, func(fr *http2.Framer, _ *hpack.Encoder, _ *bytes.Buffer) {
		fr.WriteSettings()
	})
	if _, err := parseH2Opening(b); err == nil {
		t.Error("expected an error")
	}
	if _, err := parseH2Opening([]byte("GET / HTTP/1.1\r\n\r\n")); err == nil {
		t.Error("expected an error without a preface")
	}
}

func TestParseH1Headers(t *testing.T) {
	line, h, _, err := parseH1Headers([]byte("GET / HTTP/1.1\r\nHost: syn.sh\r\nUser-Agent: curl/8\r\nAccept: */*\r\n\r\nbody"))
	if err != nil {
		t.Fatal(err)
	}
	if line != "GET / HTTP/1.1" || len(h) != 3 || h[0] != [2]string{"Host", "syn.sh"} || h[1][0] != "User-Agent" {
		t.Errorf("got %q %v", line, h)
	}
	if _, _, _, err := parseH1Headers([]byte("GET / HTTP/1.1\r\nHost: x")); err == nil {
		t.Error("incomplete request: no error")
	}
}

func FuzzParseClientHello(f *testing.F) {
	msg := sampleHello()
	f.Add(records(msg, 16384))
	f.Add(records(msg, 7))
	f.Fuzz(func(t *testing.T, b []byte) {
		body, err := helloFromRecords(b)
		if err != nil {
			return
		}
		h, err := parseClientHello(body)
		if err != nil {
			return
		}
		ja3(h)
		ja4(h)
		describeHello(h)
	})
}

func FuzzParseH2Opening(f *testing.F) {
	f.Add([]byte(h2Preface))
	f.Fuzz(func(t *testing.T, b []byte) {
		parseH2Opening(append([]byte(h2Preface), b...))
	})
}

func FuzzParseH1Headers(f *testing.F) {
	f.Add([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		parseH1Headers(b)
	})
}
