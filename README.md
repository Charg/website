# website

My personal site [syn.sh](https://syn.sh).

## Layout

- `index.html`, `tools/`, `humans.txt`, `llms.txt`, `.well-known/`: served as-is,
  except that the build fills in the `<!-- include:header -->` and
  `<!-- include:footer -->` markers from `templates/`, the build date into
  `humans.txt`, and the post list into `llms.txt`.
- `assets/`: `site.css`, `theme.js`, and `myspace.js`, shared by every page, and
  `trippy.js`, which `theme.js` loads only when the trippy theme is picked.
- `posts/*.md`: blog posts, built into `blog/` along with the `blog/index.html`
  list from `templates/blog.html`. The build also fills in the post list in
  `index.html` (`{{posts}}`) and the Content-Security-Policy.
- `server/`: the Go server. It embeds the built site, terminates TLS itself
  (behind a PROXY-protocol TCP passthrough), and serves `/api/connection`,
  which reports the client's TLS ClientHello (JA3, JA4) and opening HTTP/2
  frames for the fingerprint tool. `server/cmd/build` is the site build; it
  writes everything into `server/site/`, never back into the source files.

## Develop

```sh
cd server
go generate                   # blog, CSP, and server/site/
go test ./...
go tool govulncheck ./...
go run . -self-signed -proxy-protocol=false -addr 127.0.0.1:8443
```

Then open <https://localhost:8443> and accept the certificate warning.

## Image

`Dockerfile` builds the site, tests and builds the server, and ships the
binary alone on distroless (non-root, port 8443, `/healthz` on 8081). It
expects the certificate at `/tls/tls.crt` and `/tls/tls.key`.
