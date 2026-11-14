FROM golang:1.27.4-alpine AS go-build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relay-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relay-controlplane ./cmd/controlplane \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relay-worker ./cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goose github.com/pressly/goose/v3/cmd/goose

FROM node:26-alpine AS web-dependencies
WORKDIR /web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

FROM node:26-alpine AS web-build
WORKDIR /web
RUN corepack enable
COPY --from=web-dependencies /web/node_modules ./node_modules
COPY web/ ./
RUN pnpm build

FROM nats:2.11-alpine AS nats

FROM node:26-alpine AS runtime
ENV NODE_ENV=production \
    HOSTNAME=0.0.0.0 \
    RELAY_API_URL=http://127.0.0.1:8080/api/v1 \
    NATS_URL=nats://127.0.0.1:4222
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata tini
COPY --from=go-build /out/ /app/
COPY --from=nats /usr/local/bin/nats-server /app/nats-server
COPY migrations/ /app/migrations/
COPY deploy/render-entrypoint.sh /app/render-entrypoint.sh
COPY --from=web-build /web/.next/standalone /app/web/
COPY --from=web-build /web/.next/static /app/web/.next/static
COPY --from=web-build /web/public /app/web/public
RUN addgroup -S relay && adduser -S -G relay -h /app relay \
    && chmod 0555 /app/render-entrypoint.sh \
    && mkdir -p /tmp/relay-nats \
    && chown -R relay:relay /app /tmp/relay-nats
USER relay
EXPOSE 10000
ENTRYPOINT ["/sbin/tini", "--", "/app/render-entrypoint.sh"]
