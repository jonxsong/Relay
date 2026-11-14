# Relay threat model

## Scope and assets

Relay protects workflow definitions, run inputs and outputs, provider credentials, MCP credentials, usage records, and the authority to execute external tools. PostgreSQL is the authoritative state store; JetStream messages are untrusted delivery hints. The public console and API boundary, provider responses, workflow JSON, MCP responses, and worker messages are all treated as untrusted input.

## Trust boundaries

1. Browser to the Next.js console over TLS.
2. Console proxy to the Relay API on a private network.
3. API, control plane, and workers to PostgreSQL and NATS.
4. Workers to OpenAI and explicitly configured MCP servers.
5. Operators to secret files, environment variables, deployment logs, and backups.

## Primary threats and controls

| Threat | Current control | Residual risk |
| --- | --- | --- |
| Unauthorized execution | Production bearer key, constant-time comparison, owner-scoped queries | One configured production owner; key rotation is operational |
| Cross-tenant access | Owner checks on workflow and run reads/writes | Multi-user key lifecycle is not implemented |
| Duplicate messages | Transactional claims, stable deduplication IDs, fencing tokens | External side effects still require idempotency |
| Stale worker completion | Monotonic fencing token checked in the completion transaction | A provider call may already have completed remotely |
| SSRF through MCP | HTTPS rules, hostname and resolved-address checks, redirect denial, connection allowlists | DNS may change after validation; network egress policy remains recommended |
| Secret disclosure | Secret references, file/env loading, redacted errors, no credentials in workflow JSON | Process environment and host administrators remain trusted |
| Malicious LLM/tool output | JSON schema validation, local revalidation, bounded outputs, no arbitrary shell | Semantic prompt injection can still influence allowed tools |
| Resource exhaustion | Per-principal request rate limit, payload/output bounds, workflow node/depth limits, concurrency and retry bounds | The limiter is process-local; multi-instance deployments need a shared edge limit |
| Event/data tampering | Database constraints, append-only event API, owner-scoped replay | A database administrator can alter records |
| Browser credential theft | Key exchanged once for an encrypted, HttpOnly, SameSite=Strict session cookie | A key entered into a compromised browser can still be stolen at entry time |

## Security invariants

- NATS never decides workflow truth.
- A node completion without the current fencing token is rejected.
- A remote MCP destination and tool must both be allowlisted.
- Side-effecting tools require explicit approval.
- Unknown model pricing remains unknown.
- API errors do not include provider bodies, credentials, or database errors.
- Relay does not claim exactly-once execution or exactly-once external effects.

## Production requirements

- Terminate TLS at a maintained reverse proxy.
- Keep API, PostgreSQL, and NATS on private networks; expose only the console.
- Generate a Relay API key with a cryptographically secure random source and rotate it after suspected exposure.
- Mount provider and Relay keys as secret files where the platform supports it.
- Restrict worker egress to required provider and allowlisted MCP destinations.
- Encrypt database volumes and backups and test restoration.
- Send logs to access-controlled storage with retention limits.
- Run dependency and container scanning in the deployment pipeline.

## Known limitations

Relay currently uses one configured production owner and bearer key. The browser session is encrypted and time-bounded, but per-user roles, audited key creation/revocation, distributed rate limiting, and managed secret-store integrations remain future work. The public evaluation topology co-locates NATS and the Relay processes on one instance and is not horizontally scalable. The console never embeds the backend key in a public image or repository.
