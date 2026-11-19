# Build the site, then the Go server that embeds and serves it.
# This stage runs on the build machine; Go cross-compiles the binary.

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS server
ARG TARGETOS TARGETARCH
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY . /src
RUN CGO_ENABLED=0 go generate . && CGO_ENABLED=0 go test ./...
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/server .

# Non-root, no shell. TLS on 8443 (behind a PROXY-protocol passthrough), /healthz on 8081.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=server /out/server /server
EXPOSE 8443 8081
ENTRYPOINT ["/server"]
