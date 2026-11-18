# Build the blog, then serve the static files with nginx.

FROM node:24-alpine@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5 AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# Runs as a non-root user and listens on 8080.
FROM nginxinc/nginx-unprivileged:1.29-alpine@sha256:0c79d56aee561a1d81c63f00eee5fb5fe29279560cdc55e91425133104c7fbe6
COPY --from=build /src/index.html /src/humans.txt /src/llms.txt /usr/share/nginx/html/
COPY --from=build /src/.well-known /usr/share/nginx/html/.well-known
COPY --from=build /src/blog /usr/share/nginx/html/blog
COPY --from=build /src/tools /usr/share/nginx/html/tools
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/csp.conf /etc/nginx/csp.conf
