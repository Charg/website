// Builds the blog: posts/*.md -> blog/<slug>.html, then rewrites the post
// list in index.html between the <!-- posts:start --> and <!-- posts:end --> markers.
//
// Each post starts with front matter:
//   ---
//   title: My post
//   date: 2026-11-12
//   draft: true        (optional; drafts are skipped)
//   ---
//
// Images: put them in a folder named after the post, e.g. posts/my-post/shot.png,
// and reference them as ![alt text](my-post/shot.png). The folder is copied to
// blog/my-post/, so the same relative path works in your editor and on the site.
//
// Usage: npm run build

import { readFileSync, writeFileSync, readdirSync, mkdirSync, rmSync, cpSync, existsSync } from "node:fs";
import { marked } from "marked";

const POSTS_DIR = "posts";
const OUT_DIR = "blog";

const escape = (s) =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

function parse(file) {
  const raw = readFileSync(`${POSTS_DIR}/${file}`, "utf8");
  const match = raw.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
  if (!match) throw new Error(`${file}: missing front matter`);

  const meta = {};
  for (const line of match[1].split(/\r?\n/)) {
    const i = line.indexOf(":");
    if (i > 0) meta[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  }
  if (!meta.title) throw new Error(`${file}: missing title`);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(meta.date ?? "")) throw new Error(`${file}: date must be YYYY-MM-DD`);

  return {
    slug: file.replace(/\.md$/, ""),
    title: meta.title,
    date: meta.date,
    draft: meta.draft === "true",
    // Lazy-load images so long posts don't fetch every screenshot up front.
    html: marked.parse(match[2]).replaceAll("<img ", '<img loading="lazy" '),
  };
}

const posts = readdirSync(POSTS_DIR)
  .filter((f) => f.endsWith(".md"))
  .map(parse)
  .filter((p) => !p.draft)
  .sort((a, b) => b.date.localeCompare(a.date));

// Rebuild blog/ from scratch so deleted or renamed posts don't linger.
rmSync(OUT_DIR, { recursive: true, force: true });
mkdirSync(OUT_DIR);

const template = readFileSync("templates/post.html", "utf8");
for (const post of posts) {
  const page = template
    .replaceAll("{{title}}", escape(post.title))
    .replaceAll("{{date}}", post.date)
    .replace("{{content}}", () => post.html);
  writeFileSync(`${OUT_DIR}/${post.slug}.html`, page);

  // Copy the post's image folder, if it has one.
  if (existsSync(`${POSTS_DIR}/${post.slug}`)) {
    cpSync(`${POSTS_DIR}/${post.slug}`, `${OUT_DIR}/${post.slug}`, { recursive: true });
  }
}

// Rewrite the list on the home page.
const list = posts.length
  ? posts
      .map((p) => `          <li><a href="/blog/${p.slug}.html">${escape(p.title)}</a> <time datetime="${p.date}">${p.date}</time></li>`)
      .join("\n")
  : `          <li>Nothing yet.</li>`;

const index = readFileSync("index.html", "utf8");
const markers = /<!-- posts:start -->[\s\S]*?<!-- posts:end -->/;
if (!markers.test(index)) throw new Error("index.html: missing <!-- posts:start/end --> markers");
writeFileSync(
  "index.html",
  index.replace(markers, () => `<!-- posts:start -->\n${list}\n          <!-- posts:end -->`),
);

console.log(`Built ${posts.length} post(s).`);
