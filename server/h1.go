package main

import (
	"bytes"
	"errors"
)

// parseH1Headers returns the request line and the headers of the first
// HTTP/1.x request, in the order and casing the client sent them. At most
// maxListOut headers are returned; cut reports whether there were more.
func parseH1Headers(b []byte) (line string, headers [][2]string, cut bool, err error) {
	end := bytes.Index(b, []byte("\r\n\r\n"))
	if end < 0 {
		return "", nil, false, errors.New("incomplete HTTP/1 request")
	}
	head := b[:end]
	first, rest, _ := bytes.Cut(head, []byte("\r\n"))
	for len(rest) > 0 {
		var l []byte
		l, rest, _ = bytes.Cut(rest, []byte("\r\n"))
		k, v, ok := bytes.Cut(l, []byte(":"))
		if !ok {
			continue
		}
		if len(headers) == maxListOut {
			cut = true
			break
		}
		headers = append(headers, [2]string{string(k), string(bytes.TrimSpace(v))})
	}
	return string(first), headers, cut, nil
}
