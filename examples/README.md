# Relay examples

Start Relay with <code>docker compose up --build</code>, then create the parallel research workflow:

~~~bash
curl --fail-with-body \
  -H "Content-Type: application/json" \
  --data @examples/research-brief.json \
  http://localhost:8080/api/v1/workflows/
~~~

Copy the returned workflow <code>id</code> and submit a run:

~~~bash
curl --fail-with-body \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: research-brief-001" \
  --data '{"input":{"market_prompt":"Summarize the market","technical_prompt":"Summarize the architecture"}}' \
  http://localhost:8080/api/v1/workflows/WORKFLOW_ID/runs
~~~

Development mode needs no bearer header. In production add an Authorization bearer header. The deterministic mock provider is intentionally suitable for local tests and CI.
