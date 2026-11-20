package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// static is copied to the output as-is, apart from the pages in tools/.
var static = []string{".well-known", "assets", "tools"}

const (
	headerMarker = "<!-- include:header -->"
	footerMarker = "<!-- include:footer -->"
)

type post struct {
	slug, title, date string
	draft             bool
	html              string
}

type result struct{ posts, scripts, styles int }

func build(src fs.FS, out string, now time.Time) (result, error) {
	posts, err := loadPosts(src)
	if err != nil {
		return result{}, err
	}

	// Pages get the shared header and footer, and their inline scripts go in the CSP.
	pages := map[string]string{}

	tmpl, err := readString(src, "templates/post.html")
	if err != nil {
		return result{}, err
	}
	for _, p := range posts {
		page := strings.ReplaceAll(tmpl, "{{title}}", html.EscapeString(p.title))
		page = strings.ReplaceAll(page, "{{date}}", p.date)
		page = strings.Replace(page, "{{content}}", p.html, 1)
		pages["blog/"+p.slug+".html"] = page
	}

	if pages["index.html"], err = readFill(src, "index.html", "{{posts}}", postList(posts, "          ")); err != nil {
		return result{}, err
	}
	if pages["blog/index.html"], err = readFill(src, "templates/blog.html", "{{posts}}", postList(posts, "        ")); err != nil {
		return result{}, err
	}

	tools, err := fs.Glob(src, "tools/*.html")
	if err != nil {
		return result{}, err
	}
	for _, name := range tools {
		if pages[name], err = readString(src, name); err != nil {
			return result{}, err
		}
	}

	header, err := readString(src, "templates/header.html")
	if err != nil {
		return result{}, err
	}
	footer, err := readString(src, "templates/footer.html")
	if err != nil {
		return result{}, err
	}

	// Hashed from the final page, since CSP hashes are byte-exact.
	var scripts, styles []string
	for _, name := range pageOrder(pages) {
		page, err := include(name, pages[name], header, footer)
		if err != nil {
			return result{}, err
		}
		if attr := styleAttr(page); attr != "" {
			return result{}, fmt.Errorf("%s: style attribute is blocked by the CSP: %s", name, attr)
		}
		pages[name] = page
		scripts = addNew(scripts, scriptHashes(page))
		styles = addNew(styles, styleHashes(page))
	}

	texts := map[string]string{".csp": csp(scripts, styles)}
	if texts["humans.txt"], err = readFill(src, "humans.txt", "{{updated}}", now.UTC().Format("2006/01/02")); err != nil {
		return result{}, err
	}
	if texts["llms.txt"], err = readFill(src, "llms.txt", "{{posts}}", llmsList(posts)); err != nil {
		return result{}, err
	}

	if err := reset(out); err != nil {
		return result{}, err
	}
	copies := map[string]string{}
	for _, dir := range static {
		copies[dir] = dir
	}
	for _, p := range posts {
		if fi, err := fs.Stat(src, "posts/"+p.slug); err == nil && fi.IsDir() {
			copies["posts/"+p.slug] = "blog/" + p.slug
		}
	}
	for from, to := range copies {
		sub, err := fs.Sub(src, from)
		if err != nil {
			return result{}, err
		}
		if err := os.CopyFS(filepath.Join(out, filepath.FromSlash(to)), sub); err != nil {
			return result{}, fmt.Errorf("copy %s: %w", from, err)
		}
	}
	for _, files := range []map[string]string{pages, texts} {
		for name, s := range files {
			if err := write(filepath.Join(out, filepath.FromSlash(name)), s); err != nil {
				return result{}, err
			}
		}
	}
	return result{posts: len(posts), scripts: len(scripts), styles: len(styles)}, nil
}

func loadPosts(src fs.FS) ([]post, error) {
	files, err := fs.Glob(src, "posts/*.md")
	if err != nil {
		return nil, err
	}
	var posts []post
	for _, name := range files {
		raw, err := readString(src, name)
		if err != nil {
			return nil, err
		}
		p, err := parsePost(strings.TrimSuffix(strings.TrimPrefix(name, "posts/"), ".md"), raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if !p.draft {
			posts = append(posts, p)
		}
	}
	// Newest first; ties keep file name order.
	slices.SortStableFunc(posts, func(a, b post) int { return strings.Compare(b.date, a.date) })
	return posts, nil
}

var (
	frontMatter = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n(.*)$`)
	dateFormat  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func parsePost(slug, raw string) (post, error) {
	m := frontMatter.FindStringSubmatch(raw)
	if m == nil {
		return post{}, fmt.Errorf("missing front matter")
	}
	meta := map[string]string{}
	for line := range strings.Lines(m[1]) {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) != "" {
			meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	p := post{slug: slug, title: meta["title"], date: meta["date"], draft: meta["draft"] == "true"}
	if p.title == "" {
		return post{}, fmt.Errorf("missing title")
	}
	if !dateFormat.MatchString(p.date) {
		return post{}, fmt.Errorf("date must be YYYY-MM-DD")
	}
	if slug == "index" {
		return post{}, fmt.Errorf("clashes with the blog index page")
	}
	var b strings.Builder
	if err := markdown.Convert([]byte(m[2]), &b); err != nil {
		return post{}, err
	}
	p.html = b.String()
	return p, nil
}

var markdown = goldmark.New(
	// GFM, with table alignment as align attributes: the CSP blocks style attributes.
	goldmark.WithExtensions(
		extension.Linkify,
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
		extension.Strikethrough,
		extension.TaskList,
	),
	goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(lazyImages{}, 100))),
	goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
)

// lazyImages lazy-loads images so long posts don't fetch every screenshot up front.
type lazyImages struct{}

func (lazyImages) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if img, ok := n.(*ast.Image); ok && entering {
			img.SetAttributeString("loading", []byte("lazy"))
		}
		return ast.WalkContinue, nil
	})
}

func postList(posts []post, indent string) string {
	if len(posts) == 0 {
		return indent + "<li>Nothing yet.</li>"
	}
	lines := make([]string, len(posts))
	for i, p := range posts {
		lines[i] = fmt.Sprintf(`%s<li><a href="/blog/%s.html">%s</a> <time datetime="%s">%s</time></li>`,
			indent, p.slug, html.EscapeString(p.title), p.date, p.date)
	}
	return strings.Join(lines, "\n")
}

var mdBrackets = strings.NewReplacer("[", `\[`, "]", `\]`)

func llmsList(posts []post) string {
	if len(posts) == 0 {
		return "Nothing yet."
	}
	lines := make([]string, len(posts))
	for i, p := range posts {
		lines[i] = fmt.Sprintf("- [%s](/blog/%s.html): %s", mdBrackets.Replace(p.title), p.slug, p.date)
	}
	return strings.Join(lines, "\n")
}

// pageOrder lists the home page first, then the rest by path.
func pageOrder(pages map[string]string) []string {
	names := make([]string, 0, len(pages))
	for name := range pages {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		switch {
		case a == b:
			return 0
		case a == "index.html":
			return -1
		case b == "index.html":
			return 1
		}
		return strings.Compare(a, b)
	})
	return names
}

var currentMarker = regexp.MustCompile(`\{\{current:(\w+)\}\}`)

// include fills in the shared header and footer. The nav marks the page's
// section: "page" on the section's own index page, "true" on pages inside it.
func include(name, page, header, footer string) (string, error) {
	section, _, _ := strings.Cut(name, "/")
	if name == "index.html" {
		section = "home"
	}
	current := "true"
	if strings.HasSuffix(name, "index.html") {
		current = "page"
	}
	nav := currentMarker.ReplaceAllStringFunc(header, func(m string) string {
		if currentMarker.FindStringSubmatch(m)[1] == section {
			return ` aria-current="` + current + `"`
		}
		return ""
	})
	for _, marker := range []string{headerMarker, footerMarker} {
		if !strings.Contains(page, marker) {
			return "", fmt.Errorf("%s: missing %s", name, marker)
		}
	}
	page = strings.Replace(page, headerMarker, nav, 1)
	return strings.Replace(page, footerMarker, footer, 1), nil
}

var (
	scriptTag = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)
	styleTag  = regexp.MustCompile(`(?is)<style([^>]*)>(.*?)</style>`)
	srcAttr   = regexp.MustCompile(`\bsrc=`)
	// A start tag with a style attribute; script and style bodies are removed first.
	styleAttrTag = regexp.MustCompile(`(?i)<[a-z][^>]*\sstyle\s*=[^>]*>`)
)

// scriptHashes returns CSP sources for the page's inline scripts.
func scriptHashes(page string) []string {
	var hashes []string
	for _, m := range scriptTag.FindAllStringSubmatch(page, -1) {
		if !srcAttr.MatchString(m[1]) {
			hashes = append(hashes, hash(m[2]))
		}
	}
	return hashes
}

// styleHashes returns CSP sources for the page's <style> blocks.
func styleHashes(page string) []string {
	var hashes []string
	for _, m := range styleTag.FindAllStringSubmatch(page, -1) {
		hashes = append(hashes, hash(m[2]))
	}
	return hashes
}

// styleAttr returns the first tag with a style attribute, which hashes can't allow.
func styleAttr(page string) string {
	page = scriptTag.ReplaceAllString(page, "")
	page = styleTag.ReplaceAllString(page, "")
	return styleAttrTag.FindString(page)
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func addNew(list, items []string) []string {
	for _, s := range items {
		if !slices.Contains(list, s) {
			list = append(list, s)
		}
	}
	return list
}

func csp(scripts, styles []string) string {
	return strings.Join([]string{
		"default-src 'self'",
		strings.Join(append([]string{"script-src 'self'"}, scripts...), " "),
		strings.Join(append([]string{"style-src 'self'"}, styles...), " "),
		"img-src 'self' data:",
		"object-src 'none'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
		"form-action 'self'",
	}, "; ") + "\n"
}

// reset empties out, keeping the .gitkeep that holds its place in git.
func reset(out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == ".gitkeep" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(out, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func readString(src fs.FS, name string) (string, error) {
	b, err := fs.ReadFile(src, name)
	return string(b), err
}

func readFill(src fs.FS, name, placeholder, value string) (string, error) {
	s, err := readString(src, name)
	if err != nil {
		return "", err
	}
	if !strings.Contains(s, placeholder) {
		return "", fmt.Errorf("%s: missing %s", name, placeholder)
	}
	return strings.Replace(s, placeholder, value, 1), nil
}

func write(name, s string) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, []byte(s), 0o644)
}
