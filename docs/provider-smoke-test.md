# Opt-in OpenAI provider smoke test

Relay's automated suites use the deterministic mock provider so CI never consumes paid API traffic. An operator can explicitly run one live Responses API smoke test without storing a key in Git or workflow JSON.

1. Put one project-scoped key in the ignored `OPENAI_API_KEY.txt` file.
2. Start PostgreSQL and NATS, apply migrations, and start the API and control plane normally.
3. Start exactly one worker with the live provider:

   ~~~powershell
   $env:RELAY_PROVIDER = "openai"
   $env:OPENAI_API_KEY_FILE = "OPENAI_API_KEY.txt"
   $env:RELAY_DEFAULT_MODEL = "gpt-5-mini"
   go run ./cmd/worker
   ~~~

4. Submit [`examples/research-brief.json`](../examples/research-brief.json) through the console or API with a small prompt.
5. Verify that the run terminates, the node attempt has structured output, and `/api/v1/runs/{runId}/usage` contains provider/model/token data. Cost remains `unknown` unless a versioned pricing file was deliberately configured.

Stop other workers before the test so the deterministic worker cannot claim the live attempt. Delete the local key file or rotate the key after the smoke test. Provider bodies and credentials must never be copied into issues, logs, traces, or benchmark results.
