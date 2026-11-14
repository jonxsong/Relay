# Relay deterministic load harness

The harness creates a one-node mock-provider workflow, then submits the requested number of runs at concurrency 1, 5, 10, and 25. It polls durable terminal state, reads attempt counts, and samples Prometheus process RSS and CPU.

~~~bash
go run ./cmd/relay-bench -base-url http://127.0.0.1:8080/api/v1 -runs 20
~~~

Results are machine- and configuration-specific. Keep the mock provider enabled when comparing scheduler and persistence behavior. See results-local.md for the latest recorded local run.
