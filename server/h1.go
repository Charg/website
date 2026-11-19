package main

import (
	"bytes"
	"errors"
)

// parseH1Headers returns the request line and the headers of the first
// HTTP/1.x request, in the order and casing the client sent them.
func parseH1Headers(b []byte) (string, [][2]string, error) {
	end := bytes.Index(b, []byte("\r\n\r\n"))
	if end < 0 {
		return "", nil, errors.New("incomplete HTTP/1 request")
	}
	lines := bytes.Split(b[:end], []byte("\r\n"))
	var headers [][2]string
	for _, l := range lines[1:] {
		k, v, ok := bytes.Cut(l, []byte(":"))
		if !ok {
			continue
		}
		headers = append(headers, [2]string{string(k), string(bytes.TrimSpace(v))})
	}
	return string(lines[0]), headers, nil
}
