# Deploying Relay

This guide describes a single-host deployment suitable for evaluation. It is not a substitute for a platform-specific security review.

## Public evaluation deployment

[`render.yaml`](../render.yaml) defines a no-secret-in-source Render Blueprint for `relay.jonzhang.dev`. It creates a private Render PostgreSQL database and one public web service from [`deploy/render.Dockerfile`](../deploy/render.Dockerfile). Render generates separate 256-bit Relay and browser-session secrets, terminates TLS, and exposes only the Next.js console.

The evaluation image co-locates the API, control plane, one worker, and an ephemeral JetStream process so it can run on a single free web instance. PostgreSQL remains authoritative, so loss of the local JetStream store does not lose workflow state; the outbox republishes unfinished work. This topology is intentionally not horizontally scalable. Use the multi-service Compose topology below—or split the same binaries into private services—for sustained production workloads.

Create the Blueprint from the repository, wait for the `relay-console` health check to pass, and use the custom-domain verification values Render displays. Keep the generated `RELAY_API_KEY` private: it unlocks execution, while the public URL remains safe to visit without it.

## Prerequisites

- A Linux host with Docker Engine and Compose v2
- DNS A/AAAA records for the chosen hostname
- Ports 80 and 443 reachable by a maintained TLS reverse proxy
- Persistent, encrypted storage for PostgreSQL

## Secrets

Create a deployment-only .env that is never committed:

~~~dotenv
POSTGRES_PASSWORD=replace-with-a-random-database-password
RELAY_ENV=production
RELAY_API_KEY=replace-with-at-least-32-random-characters
RELAY_SESSION_SECRET=replace-with-a-different-32-character-random-secret
RELAY_PROVIDER=mock
# Optional: an OpenTelemetry Collector OTLP/HTTP endpoint.
OTEL_EXPORTER_OTLP_ENDPOINT=http://collector:4318
~~~

For live model execution, set the provider to OpenAI and mount the OpenAI key file. Prefer file-mounted platform secrets over environment variables.

## Start and verify

~~~bash
docker compose pull
docker compose up --build -d
docker compose ps
curl --fail http://127.0.0.1:8080/api/v1/health/ready
curl --fail http://127.0.0.1:3000/
~~~

Only the console should be internet-facing. Bind the API, NATS, and PostgreSQL ports to loopback or remove their host mappings on an external host.

## TLS reverse proxy

An example Caddy site for relay.jonzhang.dev:

~~~caddyfile
relay.jonzhang.dev {
    encode zstd gzip
    reverse_proxy 127.0.0.1:3000
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains"
        X-Content-Type-Options "nosniff"
        Referrer-Policy "no-referrer"
        -Server
    }
}
~~~

The Next.js route handler proxies API and SSE traffic to the private API service. The console exchanges the operator's Relay key for an eight-hour, same-site, HttpOnly cookie encrypted with `RELAY_SESSION_SECRET`; neither key is baked into the browser bundle or container image. Use distinct random values for `RELAY_API_KEY` and `RELAY_SESSION_SECRET`.

## Operations

- Back up PostgreSQL before upgrades and test a restore.
- Apply Goose migrations before starting new service binaries.
- Scale workers horizontally; do not scale the database by copying its volume.
- Drain workers with SIGTERM; leases and recovery handle interrupted attempts.
- Monitor readiness, error rate, expired leases, retry count, queue delay, and database saturation.
- Point `OTEL_EXPORTER_OTLP_ENDPOINT` at an OpenTelemetry Collector to export API, control-plane, and worker spans over OTLP/HTTP. Prometheus metrics remain available at `/metrics`.
- Rotate Relay, OpenAI, and MCP keys independently.

See [the threat model](threat-model.md), [architecture](architecture.md), and [OpenAPI contract](openapi.yaml).
