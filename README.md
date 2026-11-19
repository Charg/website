# website

My personal site [syn.sh](https://syn.sh).

## Layout

- `index.html`, `tools/`, `humans.txt`, `llms.txt`, `.well-known/`: served as-is.
- `posts/*.md`: blog posts, built into `blog/` by `build.js`.
- `server/`: the Go server. It embeds the built site, terminates TLS itself
  (behind a PROXY-protocol TCP passthrough), and serves `/api/connection`,
  which reports the client's TLS ClientHello (JA3, JA4) and opening HTTP/2
  frames for the fingerprint tool.

## Develop

```sh
npm ci
npm run build                 # blog, CSP, and server/site/
cd server
go test ./...
go run . -self-signed -proxy-protocol=false -addr 127.0.0.1:8443
```

Then open <https://localhost:8443> and accept the certificate warning.

## Image

`Dockerfile` builds the site, tests and builds the server, and ships the
binary alone on distroless (non-root, port 8443, `/healthz` on 8081). It
expects the certificate at `/tls/tls.crt` and `/tls/tls.key`.
