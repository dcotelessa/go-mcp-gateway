# Spec: Prompt Contract

Covers: the implement system-prompt templates that encode the structured output contract (`internal/filewriter` prompts; overrides via `internal/config`).

## Requirements

### Requirement: Standard template carries the contract
The standard implement prompt SHALL contain: the fenced-block contract (`file:<path>` / `file-delete:<path>`), a minimal valid example, the full-file-replacement requirement (RD-1), and a no-prose constraint ("emit only file blocks"). It SHALL be rendered with the task description and the graft context.

### Requirement: Strict variant
The strict variant SHALL additionally state that output MUST contain ONLY `file:`/`file-delete:` blocks and that the ENTIRE file MUST be reproduced even for a one-line change. It SHALL be used for recovery attempts (RD-2).

### Requirement: Per-tier overrides (SHOULD)
`internal/config` SHOULD support per-tier template overrides (`file_writer.prompt_overrides`); when present for the tier and variant, the override SHALL be used instead of the default template.

### Requirement: Graft context handling
The rendered prompt SHALL include the graft context section when context is available and SHALL contain no empty section or dangling placeholder when it is not.

## Scenarios

### PROMPT-1 — Standard prompt carries the contract
GIVEN a task with a description and graft context at tier `small`
WHEN the standard implement prompt is rendered
THEN it contains the fenced-block contract with a minimal valid example, the full-replacement requirement, the no-prose constraint, the task description, and the graft context section

### PROMPT-2 — Strict variant tightens the contract
GIVEN a strict render for the same task
WHEN compared with the standard render
THEN it additionally states that output must contain only file blocks and that the entire file must be reproduced even for one-line changes

### PROMPT-3 — Per-tier override wins
GIVEN a configured override for tier `large` strict variant
WHEN the strict prompt for tier `large` is rendered
THEN the override template is used instead of the default strict template

### PROMPT-4 — Empty graft context omitted
GIVEN a task with no graft context
WHEN the prompt is rendered
THEN no empty context section or dangling placeholder appears in the output
