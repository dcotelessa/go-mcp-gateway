# Spec: Sensitivity Guard — Deterministic Layer

Covers: `internal/sensitivity` scanner, path deny list, workspace policy, and enforcement at dispatch.

## Requirements

### Requirement: Findings never contain values
A `Finding` SHALL carry `Category`, `Rule`, and `Line` only. The matched text MUST NOT appear in findings, logs, span attributes, events, or API responses.

### Requirement: Known formats
The scanner SHALL flag, with category `credential`, `private_key`, `token`, or `connection_string`:
- API keys with known prefixes (e.g. `sk-or-v1-`, `sk-ant-`, `sk-proj-`)
- Cloud access key IDs (`AKIA…`, `ASIA…`)
- VCS tokens (`ghp_`, `gho_`, `ghs_`, `github_pat_`)
- Chat platform tokens (`xox[baprs]-`)
- PEM private key headers (`-----BEGIN … PRIVATE KEY-----`)
- JWTs (three base64url segments, the first two beginning `eyJ`)
- URIs with embedded credentials (`scheme://user:password@host`)

### Requirement: Keyword-anchored entropy
The scanner SHALL flag, with category `generic_secret`, a value assigned (`=` or `:`) to a name containing `password`, `passwd`, `secret`, `token`, `api_key`, `apikey`, `access_key`, `private_key`, or `client_secret`, when the value is at least 12 characters with Shannon entropy of at least 3.5 bits per character. High-entropy strings with no such name MUST NOT be flagged by this rule.

### Requirement: Placeholder exclusion
The scanner MUST NOT flag values that are empty, a variable reference (`${…}`, `$VAR`), a command substitution (`$(…)`), an environment lookup (`os.Getenv(…)`, `process.env.…`), an angle-bracket placeholder (`<…>`), or a masked value made of a single repeated character.

### Requirement: Path deny list
Graft results whose file path matches the deny list SHALL be removed before prompt assembly. Default patterns: `.env`, `.env.*`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `id_rsa*`, `id_ed25519*`, `*credentials*`, `.netrc`, `.pgpass`, `.npmrc`, `.git-credentials`, `secrets/**`, `.password-store/**`. The list SHALL be overridable in config.

### Requirement: Workspace policy
A task whose worktree lies under a path in `sensitivity.local_only` SHALL be treated as local-only regardless of scan results.

### Requirement: Scan point
The scanner SHALL run on the fully assembled prompt — system prompt, task, and filtered graft context — at the single dispatch point used by both REST and MCP, immediately before any model call.

### Requirement: Local-only enforcement
A local-only task (by policy or by finding, with action `route_local`) SHALL:
- be dispatched only to local tiers, overriding a remote selection, with reasoning tag `sensitivity_local_only`
- never escalate to a remote tier in the retry ladder; the ladder ends at the highest local tier
- never fall back to a remote tier in any cascade
- fail closed with reason `sensitive_no_local_tier` if no local tier can be loaded

### Requirement: Block action
When configured action is `block`, a flagged task SHALL return a structured error with reason `sensitive_content` and make no model call.

### Requirement: Scan endpoint
`POST /scan {text}` SHALL return `{sensitive, findings: [{category, rule, line}]}` and make no model call.

### Requirement: Telemetry
The gateway SHALL expose `gateway.sensitivity.flagged` (Int64Counter) with attributes `layer` (`1`), `source` (`scan` | `path` | `policy`), `category`, and `action`.

## Scenarios

### SENS-1 — Known key prefix flagged
GIVEN text containing an `sk-or-v1-` key
WHEN scanned
THEN the result is sensitive with one `credential` finding on the correct line

### SENS-2 — Private key header flagged
GIVEN text containing a PEM private key header
WHEN scanned
THEN a `private_key` finding is returned

### SENS-3 — Credentialed connection string flagged
GIVEN `postgres://app:hunter2secret@db.internal:5432/prod`
WHEN scanned
THEN a `connection_string` finding is returned

### SENS-4 — JWT flagged
GIVEN a three-segment JWT
WHEN scanned
THEN a `token` finding is returned

### SENS-5 — Keyword-anchored entropy flagged
GIVEN `client_secret = "q8Zr2mP9vXk4Lt7wN3bY"`
WHEN scanned
THEN a `generic_secret` finding is returned

### SENS-6 — Unanchored entropy not flagged
GIVEN a `go.sum` line and a 40-character commit hash
WHEN scanned
THEN the result is not sensitive

### SENS-7 — Placeholders not flagged
GIVEN `api_key: ""`, `export OPENROUTER_API_KEY=$(pass show ai/openrouter)`, `password: ${DB_PASSWORD}`, and `key := os.Getenv("API_KEY")`
WHEN scanned
THEN the result is not sensitive

### SENS-8 — Values never leak
GIVEN text containing a known key
WHEN the result is marshaled to JSON
THEN the key's characters do not appear in the output

### SENS-9 — Denied path removed from graft context
GIVEN graft hits including `.env` and `internal/rest/handlers.go`
WHEN the prompt is assembled
THEN only the `handlers.go` content appears

### SENS-10 — Workspace policy forces local
GIVEN a worktree under a `local_only` path and clean content routed to `remote_deepseek`
WHEN dispatched
THEN the task runs on a local tier with tag `sensitivity_local_only`

### SENS-11 — Finding overrides remote routing
GIVEN flagged content routed to a remote tier
WHEN dispatched with action `route_local`
THEN the task runs on a local tier and no remote call is made

### SENS-12 — Ladder cannot escalate to remote
GIVEN a local-only task whose output fails to parse on every attempt
WHEN the retry ladder runs
THEN every model call targets a local tier and zero remote calls are made

### SENS-13 — Fail closed without a local tier
GIVEN a local-only task and no loadable local tier
WHEN dispatched
THEN the error reason is `sensitive_no_local_tier` and zero remote calls are made

### SENS-14 — Block action
GIVEN flagged content with action `block`
WHEN dispatched
THEN the error reason is `sensitive_content` and no model call is made

### SENS-15 — Scan endpoint
GIVEN `POST /scan` with text containing a known key
WHEN handled
THEN the response is sensitive, lists category and line, and contains no key characters

### SENS-16 — Telemetry without content
GIVEN a flagged dispatch
WHEN counters and spans are inspected
THEN `gateway.sensitivity.flagged` increased with source, category, and action, and no span attribute or event contains prompt text
