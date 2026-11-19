package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ja3 returns the JA3 string and its MD5:
// version,ciphers,extensions,groups,point formats (decimal, GREASE removed).
// https://github.com/salesforce/ja3
func ja3(h *clientHello) (string, string) {
	dec16 := func(vs []uint16) string {
		parts := make([]string, 0, len(vs))
		for _, v := range withoutGREASE(vs) {
			parts = append(parts, strconv.Itoa(int(v)))
		}
		return strings.Join(parts, "-")
	}
	formats := make([]string, 0, len(h.PointFormats))
	for _, f := range h.PointFormats {
		formats = append(formats, strconv.Itoa(int(f)))
	}
	s := strings.Join([]string{
		strconv.Itoa(int(h.Version)),
		dec16(h.CipherSuites),
		dec16(h.Extensions),
		dec16(h.Groups),
		strings.Join(formats, "-"),
	}, ",")
	sum := md5.Sum([]byte(s))
	return s, hex.EncodeToString(sum[:])
}

// ja4 returns the JA4 TLS client fingerprint and its raw (unhashed) form.
// https://github.com/FoxIO-LLC/ja4/blob/main/technical_details/JA4.md
func ja4(h *clientHello) (string, string) {
	ciphers := withoutGREASE(h.CipherSuites)
	exts := withoutGREASE(h.Extensions)

	version := h.Version
	if vs := withoutGREASE(h.Versions); len(vs) > 0 {
		version = slices.Max(vs)
	}
	sni := "i"
	if slices.Contains(exts, extServerName) {
		sni = "d"
	}
	a := fmt.Sprintf("t%s%s%02d%02d%s",
		ja4Version(version), sni, min(len(ciphers), 99), min(len(exts), 99), ja4ALPN(h.ALPN))

	sortedCiphers := hex4s(slices.Sorted(slices.Values(ciphers)))
	var rest []uint16
	for _, e := range exts {
		if e != extServerName && e != extALPN {
			rest = append(rest, e)
		}
	}
	sortedExts := hex4s(slices.Sorted(slices.Values(rest)))
	c := sortedExts
	if sigs := withoutGREASE(h.SigAlgs); len(sigs) > 0 {
		c += "_" + hex4s(sigs)
	}

	b12 := func(s, src string) string {
		if src == "" {
			return "000000000000"
		}
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])[:12]
	}
	fp := a + "_" + b12(sortedCiphers, sortedCiphers) + "_" + b12(c, sortedExts)
	raw := a + "_" + sortedCiphers + "_" + c
	return fp, raw
}

func ja4Version(v uint16) string {
	switch v {
	case 0x0304:
		return "13"
	case 0x0303:
		return "12"
	case 0x0302:
		return "11"
	case 0x0301:
		return "10"
	case 0x0300:
		return "s3"
	case 0x0002:
		return "s2"
	case 0xfeff:
		return "d1"
	case 0xfefd:
		return "d2"
	case 0xfefc:
		return "d3"
	}
	return "00"
}

func ja4ALPN(protos []string) string {
	if len(protos) == 0 || protos[0] == "" {
		return "00"
	}
	p := protos[0]
	first, last := p[0], p[len(p)-1]
	if isAlnum(first) && isAlnum(last) {
		return string([]byte{first, last})
	}
	h := hex.EncodeToString([]byte(p))
	return string([]byte{h[0], h[len(h)-1]})
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func hex4s(vs []uint16) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = fmt.Sprintf("%04x", v)
	}
	return strings.Join(parts, ",")
}
