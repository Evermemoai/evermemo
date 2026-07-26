# Security Policy

## Supported versions

The latest minor release receives security fixes.

## Reporting a vulnerability

Please report vulnerabilities privately via
[GitHub Security Advisories](https://github.com/Evermemoai/evermemo/security/advisories/new).
Do **not** open a public issue.

You can expect an acknowledgement within 72 hours and a fix or mitigation
plan within 14 days for confirmed issues.

## Deployment hardening checklist

- Run the hub behind TLS (`serve --cert/--key`) or a TLS-terminating proxy.
- Use per-agent keys (`EVERMEMO_AGENT_KEYS` or `EVERMEMO_KEYS_FILE`) —
  never share one key across agents.
- Set namespace ACLs (`EVERMEMO_ACL`) so agents only reach what they need.
- Enable rate limiting (`EVERMEMO_RATE`).
- Back up with `evermemo backup` (WAL-safe) and store snapshots securely —
  the database contains everything your agents remember.
