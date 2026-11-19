package main

import (
	"errors"

	"golang.org/x/crypto/cryptobyte"
)

// clientHello holds the fields of a TLS ClientHello that fingerprints use.
// Lists keep the order the client sent, GREASE values included.
type clientHello struct {
	Version      uint16
	CipherSuites []uint16
	Extensions   []uint16
	ServerName   string
	Groups       []uint16
	PointFormats []uint8
	SigAlgs      []uint16
	ALPN         []string
	Versions     []uint16
	KeyShares    []uint16
	PSKModes     []uint8
	CertCompAlgs []uint16
}

const (
	extServerName      = 0x0000
	extGroups          = 0x000a
	extPointFormats    = 0x000b
	extSigAlgs         = 0x000d
	extALPN            = 0x0010
	extCompressCert    = 0x001b
	extSupportedVers   = 0x002b
	extPSKModes        = 0x002d
	extKeyShare        = 0x0033
	recordHandshake    = 22
	handshakeClientHel = 1
)

var errMalformed = errors.New("malformed ClientHello")

// helloFromRecords pulls the first ClientHello out of raw TLS records,
// joining it back together if the client split it across records.
func helloFromRecords(b []byte) ([]byte, error) {
	var msg []byte
	s := cryptobyte.String(b)
	for !s.Empty() {
		var typ uint8
		var ver uint16
		var frag cryptobyte.String
		if !s.ReadUint8(&typ) || !s.ReadUint16(&ver) || !s.ReadUint16LengthPrefixed(&frag) {
			return nil, errMalformed
		}
		if typ != recordHandshake {
			return nil, errMalformed
		}
		msg = append(msg, frag...)
		if len(msg) >= 4 {
			if msg[0] != handshakeClientHel {
				return nil, errMalformed
			}
			n := int(msg[1])<<16 | int(msg[2])<<8 | int(msg[3])
			if len(msg) >= 4+n {
				return msg[4 : 4+n], nil
			}
		}
	}
	return nil, errMalformed
}

// parseClientHello parses a ClientHello message body (without the 4-byte
// handshake header).
func parseClientHello(body []byte) (*clientHello, error) {
	h := &clientHello{}
	s := cryptobyte.String(body)
	var sessionID, suites, compression cryptobyte.String
	if !s.ReadUint16(&h.Version) ||
		!s.Skip(32) ||
		!s.ReadUint8LengthPrefixed(&sessionID) ||
		!s.ReadUint16LengthPrefixed(&suites) ||
		!s.ReadUint8LengthPrefixed(&compression) {
		return nil, errMalformed
	}
	var err error
	if h.CipherSuites, err = readUint16s(suites); err != nil {
		return nil, err
	}
	if s.Empty() {
		return h, nil
	}

	var exts cryptobyte.String
	if !s.ReadUint16LengthPrefixed(&exts) || !s.Empty() {
		return nil, errMalformed
	}
	for !exts.Empty() {
		var typ uint16
		var data cryptobyte.String
		if !exts.ReadUint16(&typ) || !exts.ReadUint16LengthPrefixed(&data) {
			return nil, errMalformed
		}
		h.Extensions = append(h.Extensions, typ)
		if err := h.parseExtension(typ, data); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *clientHello) parseExtension(typ uint16, d cryptobyte.String) error {
	var list cryptobyte.String
	var err error
	switch typ {
	case extServerName:
		if !d.ReadUint16LengthPrefixed(&list) {
			return errMalformed
		}
		for !list.Empty() {
			var nameType uint8
			var name cryptobyte.String
			if !list.ReadUint8(&nameType) || !list.ReadUint16LengthPrefixed(&name) {
				return errMalformed
			}
			if nameType == 0 && h.ServerName == "" {
				h.ServerName = string(name)
			}
		}
	case extGroups:
		if !d.ReadUint16LengthPrefixed(&list) {
			return errMalformed
		}
		h.Groups, err = readUint16s(list)
	case extPointFormats:
		if !d.ReadUint8LengthPrefixed(&list) {
			return errMalformed
		}
		h.PointFormats = append([]uint8(nil), list...)
	case extSigAlgs:
		if !d.ReadUint16LengthPrefixed(&list) {
			return errMalformed
		}
		h.SigAlgs, err = readUint16s(list)
	case extALPN:
		if !d.ReadUint16LengthPrefixed(&list) {
			return errMalformed
		}
		for !list.Empty() {
			var proto cryptobyte.String
			if !list.ReadUint8LengthPrefixed(&proto) {
				return errMalformed
			}
			h.ALPN = append(h.ALPN, string(proto))
		}
	case extSupportedVers:
		if !d.ReadUint8LengthPrefixed(&list) {
			return errMalformed
		}
		h.Versions, err = readUint16s(list)
	case extKeyShare:
		if !d.ReadUint16LengthPrefixed(&list) {
			return errMalformed
		}
		for !list.Empty() {
			var group uint16
			var key cryptobyte.String
			if !list.ReadUint16(&group) || !list.ReadUint16LengthPrefixed(&key) {
				return errMalformed
			}
			h.KeyShares = append(h.KeyShares, group)
		}
	case extPSKModes:
		if !d.ReadUint8LengthPrefixed(&list) {
			return errMalformed
		}
		h.PSKModes = append([]uint8(nil), list...)
	case extCompressCert:
		if !d.ReadUint8LengthPrefixed(&list) {
			return errMalformed
		}
		h.CertCompAlgs, err = readUint16s(list)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	if !d.Empty() {
		return errMalformed
	}
	return nil
}

func readUint16s(s cryptobyte.String) ([]uint16, error) {
	if len(s)%2 != 0 {
		return nil, errMalformed
	}
	out := make([]uint16, 0, len(s)/2)
	for !s.Empty() {
		var v uint16
		s.ReadUint16(&v)
		out = append(out, v)
	}
	return out, nil
}

// isGREASE reports whether v is a GREASE value (RFC 8701): 0x?a?a with both
// bytes equal.
func isGREASE(v uint16) bool {
	return v&0x0f0f == 0x0a0a && v>>8 == v&0xff
}

func withoutGREASE(vs []uint16) []uint16 {
	out := make([]uint16, 0, len(vs))
	for _, v := range vs {
		if !isGREASE(v) {
			out = append(out, v)
		}
	}
	return out
}
