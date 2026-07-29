# syntax=docker/dockerfile:1

ARG GO_IMAGE=golang:1.25-bookworm
ARG NODE_IMAGE=node:22-bookworm-slim
ARG NGINX_IMAGE=nginx:1.27-alpine
ARG API_RUNTIME_IMAGE=alpine:3.20

FROM ${GO_IMAGE} AS api-builder
WORKDIR /src
COPY go.mod go.sum ./
ARG GOPROXY=https://goproxy.cn|https://proxy.golang.org|direct
ARG GOSUMDB=sum.golang.google.cn
RUN GOPROXY="${GOPROXY}" GOSUMDB="${GOSUMDB}" go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-$(go env GOARCH)}" \
    go build -trimpath -ldflags="-s -w" -o /out/campusos-server ./cmd/server

FROM ${API_RUNTIME_IMAGE} AS api
ARG CAMPUSOS_VERSION=0.13.0
ARG POSTGRES_CLIENT_MAJOR=16
LABEL org.opencontainers.image.title="CampusOS API" \
      org.opencontainers.image.version="${CAMPUSOS_VERSION}" \
      org.opencontainers.image.source="https://github.com/javencpdd/CampusOS"
RUN apk add --no-cache bash ca-certificates curl tini tzdata "postgresql${POSTGRES_CLIENT_MAJOR}-client" \
    && addgroup -S -g 10001 campusos \
    && adduser -S -D -H -u 10001 -G campusos campusos
WORKDIR /app
COPY --from=api-builder --chown=campusos:campusos /out/campusos-server ./bin/campusos-server
COPY --chown=campusos:campusos migrations ./migrations
COPY --chown=campusos:campusos modules ./modules
COPY --chown=campusos:campusos data ./seed-data
COPY --chown=campusos:campusos scripts/migrate.sh scripts/backup.sh scripts/restore.sh ./scripts/
COPY --chown=campusos:campusos deploy/docker/entrypoint.sh ./entrypoint.sh
COPY --chown=campusos:campusos deploy/docker/restore-container.sh ./scripts/docker-restore.sh
RUN chmod 0755 ./bin/campusos-server ./entrypoint.sh ./scripts/*.sh \
    && mkdir -p data .campusos/logs \
    && chown -R campusos:campusos data .campusos
USER 10001:10001
ENV SERVER_HOST=0.0.0.0 \
    SERVER_PORT=8080 \
    MIGRATIONS_DIR=/app/migrations \
    PLUGINS_DIR=/app/data/plugins \
    PLUGIN_DATA_DIR=/app/data/plugin_data \
    MODULE_DATA_DIR=/app/data/module_data \
    RESOURCE_DIR=/app/data/resources \
    CAMPUSOS_LOG_DIR=/app/data/logs
EXPOSE 8080
VOLUME ["/app/data"]
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=10 \
  CMD curl -fsS http://127.0.0.1:8080/api/v1/health >/dev/null || exit 1
ENTRYPOINT ["/sbin/tini", "--", "/app/entrypoint.sh"]
CMD ["/app/bin/campusos-server"]

FROM ${NODE_IMAGE} AS web-builder
RUN corepack enable && corepack prepare pnpm@8.15.9 --activate
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM ${NGINX_IMAGE} AS web
LABEL org.opencontainers.image.title="CampusOS Web"
COPY deploy/docker/nginx-spa.conf /etc/nginx/conf.d/default.conf
COPY --from=web-builder /src/web/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=10 \
  CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1

FROM ${NODE_IMAGE} AS admin-builder
RUN corepack enable && corepack prepare pnpm@8.15.9 --activate
WORKDIR /src/admin
COPY admin/package.json admin/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY admin/ ./
ARG VITE_WEB_URL=
ARG VITE_DOCS_URL=
ARG VITE_GITHUB_URL=https://github.com/javencpdd/CampusOS
ENV VITE_WEB_URL=${VITE_WEB_URL} \
    VITE_DOCS_URL=${VITE_DOCS_URL} \
    VITE_GITHUB_URL=${VITE_GITHUB_URL}
RUN pnpm build

FROM ${NGINX_IMAGE} AS admin
LABEL org.opencontainers.image.title="CampusOS Admin"
COPY deploy/docker/nginx-spa.conf /etc/nginx/conf.d/default.conf
COPY --from=admin-builder /src/admin/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=10 \
  CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1

FROM ${NODE_IMAGE} AS docs-builder
RUN corepack enable && corepack prepare pnpm@8.15.9 --activate
WORKDIR /src/docs-site
COPY docs-site/package.json docs-site/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY docs-site/ ./
ENV CAMPUSOS_DOCS_LAST_UPDATED=false
RUN pnpm build

FROM ${NGINX_IMAGE} AS docs
LABEL org.opencontainers.image.title="CampusOS Docs"
COPY deploy/docker/nginx-docs.conf /etc/nginx/conf.d/default.conf
COPY --from=docs-builder /src/docs-site/.vitepress/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=10 \
  CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1
