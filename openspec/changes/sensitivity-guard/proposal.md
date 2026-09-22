# Proposal: Sensitivity Guard (deterministic layer)

## Problem

Graft enrichment injects real file contents into prompts, and those prompts
go to OpenRouter, Z.ai, and Opus. Nothing checks them. A `.env` file, a
connection string in a config, or a private key in a fixture can leave the
machine today.

Disclosure is the one failure in the pipeline that cannot be undone. A bad
route costs money; a bad retry costs time; a leaked credential has to be
rotated and a leaked client record cannot be recalled.

This change is split out of the v0.5 decision layer because it cannot wait
for model testing and needs no shadow period: it replaces "no protection"
with "deterministic protection," which is strictly better from day one.

## What Will Change

- ADDED: `internal/sensitivity` package — a deterministic `Scanner`
  - Known secret formats: API key prefixes, cloud access keys, VCS tokens,
    private key headers, JWTs, credentialed connection strings
  - Keyword-anchored entropy: a high-entropy value is flagged only when
    assigned to a secret-like name, so hashes, checksums, and lockfiles
    do not trigger it
  - Placeholder exclusion: empty values, variable references, and command
    substitutions are never flagged
  - Findings report category, rule, and line — never the matched value
- ADDED: Path deny list applied to graft results before prompt assembly
- ADDED: Per-workspace `local_only` policy in config — absolute, no detector
- ADDED: Enforcement at the single dispatch point shared by REST and MCP
  - A flagged or local-only task is restricted to local tiers
  - The v0.4 retry ladder is capped so it cannot escalate to a remote tier
  - If no local tier is available, the task fails closed — never falls
    through to a remote tier
  - Configurable action: `route_local` (default) or `block`
- ADDED: `POST /scan` — lets pi or Mastra screen planning inputs before
  sending them to a cloud planner. Returns findings without values.
- ADDED: Telemetry that never records prompt or graft content

## What Won't Change

- Routing for tasks with no findings and no local-only policy
- REST and MCP contracts, except the additive `/scan` endpoint and an
  additive `sensitivity_local_only` reasoning tag
- The v0.4 parser, writer, and diff

## Key Risks

- (HIGH) False negatives. A deterministic ruleset misses secrets in
  unfamiliar formats and cannot see personal information in prose. This
  layer reduces risk; it does not eliminate it. The v0.5 decision layer adds
  a local model for the vague cases. Workspace `local_only` is the only
  guarantee.
- (MEDIUM) False positives route tasks to weaker local models. Mitigated by
  keyword anchoring and placeholder exclusion, and measured by the flagged
  counter.
- (MEDIUM) A small in-house ruleset will lag maintained rule sets such as
  gitleaks'. Chosen deliberately to avoid a heavy dependency; revisit if
  telemetry shows misses.
- (LOW) Deny-listing `.env.*` also hides `.env.example`. Acceptable: fail
  closed, with a config override.

## Sequencing

- Scanner, deny list, and policy config: after v0.4 Group 2. Independent of
  the rest of v0.4.
- Ladder cap and dispatch enforcement: after v0.4 Group 4.
- Handler wiring and `/scan`: after v0.4 Group 8.

## Open Questions

1. (MEDIUM) Default local tier for rerouted tasks — the smallest capable
   local model, or whichever is currently resident to avoid a swap?
2. (LOW) Import gitleaks' detection package later if the in-house ruleset
   proves too narrow?
