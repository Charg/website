package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

const h2Preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

type h2Setting struct {
	ID    uint16 `json:"id"`
	Name  string `json:"name"`
	Value uint32 `json:"value"`
}

type h2Priority struct {
	Stream    uint32 `json:"stream"`
	Exclusive bool   `json:"exclusive"`
	DependsOn uint32 `json:"depends_on"`
	Weight    int    `json:"weight"` // 1-256, as the Akamai format writes it
}

// h2Opening is what a client sends before and with its first request.
type h2Opening struct {
	Settings     []h2Setting  `json:"settings"`
	WindowUpdate uint32       `json:"window_update"`
	Priorities   []h2Priority `json:"priority_frames"`
	HeaderPrio   *h2Priority  `json:"headers_priority,omitempty"`
	Headers      [][2]string  `json:"-"`
	Akamai       string       `json:"akamai"`
	AkamaiHash   string       `json:"akamai_hash"`
}

var errNoHeaders = errors.New("no HEADERS frame in the recorded bytes")

// parseH2Opening reads the client's frames up to the end of its first HEADERS
// block and builds the Akamai HTTP/2 fingerprint:
// SETTINGS|WINDOW_UPDATE|PRIORITY|pseudo-header order.
// https://www.blackhat.com/docs/eu-17/materials/eu-17-Shuster-Passive-Fingerprinting-Of-HTTP2-Clients-wp.pdf
func parseH2Opening(b []byte) (*h2Opening, error) {
	if !bytes.HasPrefix(b, []byte(h2Preface)) {
		return nil, errors.New("missing HTTP/2 preface")
	}
	fr := http2.NewFramer(nil, bytes.NewReader(b[len(h2Preface):]))
	fr.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	fr.MaxHeaderListSize = maxStreamBytes

	o := &h2Opening{}
	for {
		f, err := fr.ReadFrame()
		if err != nil {
			return nil, errNoHeaders
		}
		switch f := f.(type) {
		case *http2.SettingsFrame:
			if f.IsAck() || o.Settings != nil {
				continue
			}
			o.Settings = []h2Setting{}
			f.ForeachSetting(func(s http2.Setting) error {
				o.Settings = append(o.Settings, h2Setting{uint16(s.ID), settingName(s.ID), s.Val})
				return nil
			})
		case *http2.WindowUpdateFrame:
			if f.StreamID == 0 && o.WindowUpdate == 0 {
				o.WindowUpdate = f.Increment
			}
		case *http2.PriorityFrame:
			o.Priorities = append(o.Priorities, h2Priority{
				f.StreamID, f.Exclusive, f.StreamDep, int(f.Weight) + 1,
			})
		case *http2.MetaHeadersFrame:
			if f.HasPriority() {
				o.HeaderPrio = &h2Priority{
					f.StreamID, f.Priority.Exclusive, f.Priority.StreamDep, int(f.Priority.Weight) + 1,
				}
			}
			for _, hf := range f.Fields {
				o.Headers = append(o.Headers, [2]string{hf.Name, hf.Value})
			}
			o.Akamai = o.akamai()
			sum := md5.Sum([]byte(o.Akamai))
			o.AkamaiHash = hex.EncodeToString(sum[:])
			return o, nil
		}
	}
}

func (o *h2Opening) akamai() string {
	settings := make([]string, len(o.Settings))
	for i, s := range o.Settings {
		settings[i] = fmt.Sprintf("%d:%d", s.ID, s.Value)
	}
	window := "00"
	if o.WindowUpdate != 0 {
		window = fmt.Sprint(o.WindowUpdate)
	}
	prio := "0"
	if len(o.Priorities) > 0 {
		parts := make([]string, len(o.Priorities))
		for i, p := range o.Priorities {
			ex := 0
			if p.Exclusive {
				ex = 1
			}
			parts[i] = fmt.Sprintf("%d:%d:%d:%d", p.Stream, ex, p.DependsOn, p.Weight)
		}
		prio = strings.Join(parts, ",")
	}
	var pseudo []string
	for _, h := range o.Headers {
		if strings.HasPrefix(h[0], ":") && len(h[0]) > 1 {
			pseudo = append(pseudo, h[0][1:2])
		}
	}
	return strings.Join([]string{strings.Join(settings, ";"), window, prio, strings.Join(pseudo, ",")}, "|")
}

func settingName(id http2.SettingID) string {
	if id == 9 {
		return "NO_RFC7540_PRIORITIES" // RFC 9218; x/net doesn't name it
	}
	if id == 8 {
		return "ENABLE_CONNECT_PROTOCOL"
	}
	return id.String()
}
