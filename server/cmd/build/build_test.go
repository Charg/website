package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func page(body string) string {
	return "<html><body>" + headerMarker + body + footerMarker + "</body></html>"
}

func testSrc() fstest.MapFS {
	f := func(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }
	return fstest.MapFS{
		"index.html":               f(page("<ul>\n{{posts}}\n</ul><script>alert(1)</script>")),
		"humans.txt":               f("Last update: {{updated}}\n"),
		"llms.txt":                 f("## Posts\n\n{{posts}}\n"),
		"templates/post.html":      f(page("<title>{{title}}</title><time>{{date}}</time>{{content}}")),
		"templates/blog.html":      f(page("<ul>\n{{posts}}\n</ul>")),
		"templates/header.html":    f(`<nav><a href="/blog/"{{current:blog}}>blog</a><a href="/"{{current:home}}>home</a></nav>`),
		"templates/footer.html":    f("<footer>footer</footer>"),
		"tools/index.html":         f(page("tools")),
		"tools/x.html":             f(page(`<script src="/assets/x.js"></script><script>alert(1)</script>`)),
		"assets/site.css":          f("body{}"),
		".well-known/security.txt": f("Contact: x"),
		"posts/old.md":             f("---\ntitle: Old [one]\ndate: 2026-01-01\n---\n\n![shot](old/shot.png)\n"),
		"posts/old/shot.png":       f("png"),
		"posts/new.md":             f("---\r\ntitle: A & <b>\r\ndate: 2026-02-01\r\n---\r\n\nHi <span>raw</span>\n"),
		"posts/wip.md":             f("---\ntitle: WIP\ndate: 2026-03-01\ndraft: true\n---\n\nsoon\n"),
	}
}

func TestBuild(t *testing.T) {
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stale.html"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := build(testSrc(), out, time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if res != (result{posts: 2, hashes: 1}) {
		t.Errorf("result = %+v", res)
	}

	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	contains := func(name string, wants ...string) {
		t.Helper()
		got := read(name)
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in:\n%s", name, w, got)
			}
		}
	}
	missing := func(name string) {
		t.Helper()
		if _, err := os.Stat(filepath.Join(out, name)); err == nil {
			t.Errorf("%s: should not exist", name)
		}
	}

	contains("index.html",
		`<li><a href="/blog/new.html">A &amp; &lt;b&gt;</a> <time datetime="2026-02-01">2026-02-01</time></li>`+"\n"+
			`          <li><a href="/blog/old.html">Old [one]</a>`,
		`<a href="/"`+` aria-current="page">home`,
		`<a href="/blog/">blog`,
		"<footer>footer</footer>")
	contains("blog/index.html", `        <li><a href="/blog/new.html">`, `<a href="/blog/" aria-current="page">blog`)
	contains("blog/new.html", "<title>A &amp; &lt;b&gt;</title>", "<time>2026-02-01</time>", "<span>raw</span>",
		`<a href="/blog/" aria-current="true">blog`)
	contains("blog/old.html", `<img src="old/shot.png" alt="shot" loading="lazy">`)
	contains("blog/old/shot.png", "png")
	contains("tools/x.html", `<a href="/blog/">blog`, `<a href="/">home`)
	contains("tools/index.html", "tools")
	contains("assets/site.css", "body{}")
	contains(".well-known/security.txt", "Contact")
	contains("humans.txt", "Last update: 2026/11/19")
	contains("llms.txt", "- [A & <b>](/blog/new.html): 2026-02-01\n- [Old \\[one\\]](/blog/old.html): 2026-01-01")
	// sha256("alert(1)"), computed independently with openssl.
	contains(".csp", "script-src 'self' 'sha256-bhHHL3z2vDgxUt0W3dWQOrprscmda2Y5pLsLg4GF+pI=';", "frame-ancestors 'none'")

	missing("blog/wip.html")
	missing("stale.html")
	missing("posts")
	if _, err := os.Stat(filepath.Join(out, ".gitkeep")); err != nil {
		t.Error(".gitkeep was removed")
	}
	for _, name := range []string{"index.html", "blog/new.html", "tools/x.html"} {
		if s := read(name); strings.Contains(s, "<!-- include:") || strings.Contains(s, "{{") {
			t.Errorf("%s: unfilled placeholder:\n%s", name, s)
		}
	}
}

func TestBuildNoPosts(t *testing.T) {
	src := testSrc()
	for name := range src {
		if strings.HasPrefix(name, "posts/") {
			delete(src, name)
		}
	}
	out := t.TempDir()
	if _, err := build(src, out, time.Now()); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"index.html": "<li>Nothing yet.</li>", "llms.txt": "Nothing yet."} {
		b, _ := os.ReadFile(filepath.Join(out, name))
		if !strings.Contains(string(b), want) {
			t.Errorf("%s: missing %q", name, want)
		}
	}
}

func TestBuildErrors(t *testing.T) {
	for name, tc := range map[string]struct{ file, data, want string }{
		"no front matter": {"posts/a.md", "# hi\n", "posts/a.md: missing front matter"},
		"no title":        {"posts/a.md", "---\ndate: 2026-01-01\n---\nx", "posts/a.md: missing title"},
		"bad date":        {"posts/a.md", "---\ntitle: A\ndate: 1/2/2026\n---\nx", "posts/a.md: date must be YYYY-MM-DD"},
		"index slug":      {"posts/index.md", "---\ntitle: A\ndate: 2026-01-01\n---\nx", "posts/index.md: clashes"},
		"no header":       {"tools/y.html", "<p>" + footerMarker, "tools/y.html: missing " + headerMarker},
		"no posts slot":   {"index.html", page(""), "index.html: missing {{posts}}"},
	} {
		t.Run(name, func(t *testing.T) {
			src := testSrc()
			src[tc.file] = &fstest.MapFile{Data: []byte(tc.data)}
			_, err := build(src, t.TempDir(), time.Now())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
