# Contributing to Relay

Thank you for helping build a dependable agent runtime.

## Ground rules

1. Keep commits small and describe the invariant being protected.
2. Add or update a test for behavior changes.
3. Never weaken fencing, ownership checks, payload bounds, redaction, or permission gates for convenience.
4. Do not commit credentials, generated local state, benchmark claims without raw results, or fabricated screenshots.
5. Run the checks relevant to your change and report exactly what ran.

## Commit style

Use a concise imperative subject with a conventional prefix, for example `feat(domain): validate workflow graph depth` or `docs: explain lease fencing`.

## Pull requests

Explain the failure mode addressed, the chosen tradeoff, and how it was verified. Changes to core durability semantics should include an ADR or update an existing one.

