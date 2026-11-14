# Security policy

## Reporting a vulnerability

Please do not open a public issue for a suspected vulnerability. Use GitHub's private vulnerability reporting for this repository. Include the affected commit, a minimal reproduction, impact, and any suggested mitigation.

## Current status

Relay is pre-release and is not yet supported for production workloads. Security-sensitive acceptance gates include ownership authorization, request and output bounds, secret redaction, SSRF protection, explicit side-effect permissions, dependency scanning, and recovery tests.

## Secret handling

Never commit credentials. Workflow definitions contain secret references only. Local `.env` variants and `OPENAI_API_KEY.txt` are ignored by Git. Reports, logs, traces, fixtures, and screenshots must use synthetic values.

