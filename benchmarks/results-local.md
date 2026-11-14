# Local deterministic benchmark — 2026-11-14

This is a factual developer-machine result, not a capacity claim. Each level submitted 20 one-node LLM runs using the deterministic mock provider. PostgreSQL 17 and NATS 2.11 ran in Docker Desktop; the API, control plane, and one worker ran as local Go processes.

| Concurrency | Runs | Throughput | p50 | p95 | Completion | Retries | API RSS | API CPU delta |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 20 | 4.12 runs/s | 241ms | 266ms | 100.0% | 0 | 60.1 MiB | 0.219s |
| 5 | 20 | 17.23 runs/s | 240ms | 360ms | 100.0% | 0 | 61.4 MiB | 0.016s |
| 10 | 20 | 23.85 runs/s | 344ms | 415ms | 100.0% | 0 | 62.9 MiB | 0.078s |
| 25 | 20 | 17.81 runs/s | 681ms | 1.095s | 100.0% | 0 | 65.5 MiB | 0.312s |

Environment:

- Windows 11 Pro 10.0.26200
- AMD Ryzen 7 7800X3D, 8 cores / 16 logical processors
- 47.2 GiB visible memory
- Go 1.27.4 windows/amd64
- 20 runs per concurrency level, 80 measured runs total
- Default 250 ms control-plane polling; one worker; no injected failures

The CPU column is the API process delta sampled from Prometheus metrics before and after each level. RSS is the final API resident-set sample. These numbers exclude PostgreSQL, NATS, the control plane, and worker processes. OpenAI network latency and tool execution are intentionally not represented.

Command:

~~~bash
go run ./cmd/relay-bench -base-url http://127.0.0.1:18080/api/v1 -runs 20
~~~
