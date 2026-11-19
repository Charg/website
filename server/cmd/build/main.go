// Command build renders the site into the directory the server embeds:
// blog posts from posts/*.md, the shared header and footer on every page,
// the post lists, and the Content-Security-Policy.
//
// Each post starts with front matter:
//
//	---
//	title: My post
//	date: 2026-11-12
//	draft: true        (optional; drafts are skipped)
//	---
//
// Images: put them in a folder named after the post, e.g. posts/my-post/shot.png,
// and reference them as ![alt text](my-post/shot.png). The folder is copied to
// blog/my-post/, so the same relative path works in your editor and on the site.
//
// Usage, from server/: go generate (or go run ./cmd/build -root ..)
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	root := flag.String("root", "..", "repository root")
	out := flag.String("out", "server/site", "output directory, relative to -root")
	flag.Parse()

	res, err := build(os.DirFS(*root), filepath.Join(*root, *out), time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	fmt.Printf("Built %d post(s), CSP with %d inline script hash(es).\n", res.posts, res.hashes)
}
