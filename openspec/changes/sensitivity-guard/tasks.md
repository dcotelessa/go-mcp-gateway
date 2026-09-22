# Tasks: Sensitivity Guard (deterministic layer)

Conventions match file-writing-v04. Dependencies on v0.4 groups are noted per group.

## Group A — Scanner (after v0.4 Group 2; no other dependency)

- [ ] A.1 (scaffold, impl) `internal/sensitivity/types.go`: `Finding{Category, Rule, Line}`, `Result{Sensitive, Findings}`, category constants. No field can hold matched text.
- [ ] A.2 (single_file, impl) `scanner.go`: known-format rules (case-insensitive where appropriate), keyword-anchored entropy, placeholder exclusion, line numbering.
- [ ] A.3 (single_file, test) `scanner_test.go`: SENS-1..8.

## Group B — Deny list and policy config (after Group A)

- [ ] B.1 (single_file, impl) `internal/config`: `SensitivityConfig{LocalOnly []string, DenyPaths []string, Action string, LocalTier string}` with defaults; action defaults to `route_local`.
- [ ] B.2 (single_file, impl) `internal/sensitivity/paths.go`: `Denied(path) bool` glob matching, including `**`.
- [ ] B.3 (single_file, test) `paths_test.go` and config defaults test.
- [ ] B.4 (single_file, impl) Filter graft hits through `Denied` before prompt assembly.
- [ ] B.5 (single_file, test) SENS-9.

## Group C — Enforcement (after v0.4 Group 4)

- [ ] C.1 (single_file, impl) `internal/sensitivity/policy.go`: `Decide(worktree, prompt) Verdict{LocalOnly, Block, Source, Findings}` combining workspace policy, scan, and action.
- [ ] C.2 (multi_file, impl) Local-only routing override with reasoning tag; `filewriter.Input.LocalOnly` wraps `NextTier` so it never returns a remote tier; fail closed when no local tier loads.
- [ ] C.3 (multi_file, test) SENS-10..14.

## Group D — Wiring, endpoint, telemetry (after v0.4 Group 8)

- [ ] D.1 (multi_file, impl) Call `Decide` at the single dispatch point used by REST `/implement` and MCP `route_complete`.
- [ ] D.2 (single_file, impl) `POST /scan` handler.
- [ ] D.3 (single_file, impl) `gateway.sensitivity.flagged` counter; audit that no span attribute or event carries prompt or graft content.
- [ ] D.4 (multi_file, test) SENS-15, SENS-16.
