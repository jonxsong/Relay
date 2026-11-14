FROM golang:1.27.4-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relay-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relay-controlplane ./cmd/controlplane \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relay-worker ./cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goose github.com/pressly/goose/v3/cmd/goose

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S relay \
    && adduser -S -G relay -h /app relay
WORKDIR /app
COPY --from=build /out/ /app/
COPY migrations/ /app/migrations/
USER relay
EXPOSE 8080
ENTRYPOINT ["/app/relay-api"]

