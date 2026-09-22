# Design Principles

These principles guide every design in this repository. They follow John
Ousterhout's *A Philosophy of Software Design*. One document serves three
uses, so they cannot drift apart:

1. **Prevention** — the planning agent's instructions reference this file,
   so designs start from these principles.
2. **Detection** — the v0.5 design-review decision derives its yes/no
   questions from the checks below.
3. **Verification** — AST metrics measure the implemented code against the
   mechanical checks.

Design review is advisory. It informs the human review gate; it never blocks
implementation on its own.

## 1. Prefer deep modules

A module's interface is its cost; its functionality is its benefit. Good
modules have small interfaces over substantial functionality.

**Check:** Is the interface simple relative to what the module does?
**Smell:** A type or package whose methods each do very little, so callers
must stitch many calls together.

## 2. Hide information; avoid leakage

Each design decision should be known to one module. When two modules both
encode the same knowledge — a file format, a resolution rule, an ordering —
a change to it must be made in both places.

**Check:** Must a caller understand this module's internals to use it
correctly?
**Smell:** Two modules that each read the same file, parse the same format,
or resolve the same paths.

## 3. Pull complexity downward

When complexity is unavoidable, the module should absorb it rather than push
it onto callers. Sensible defaults, internal retries, and normalization belong
inside.

**Check:** Does this interface require the caller to supply something the
module could decide itself?

## 4. Define errors out of existence

Every error type is interface surface that every caller must handle. Where
the semantics can be chosen so the error case cannot occur, choose them.
Keep errors that carry information a caller must act on.

**Check:** Could this error be removed by changing the operation's
definition, without losing information callers need?
**Smell:** Errors for states that already match the desired outcome — for
example, failing to delete a file that is already absent.

## 5. Different layer, different abstraction

Adjacent layers should offer different abstractions. A method that only
forwards its arguments to another method adds interface without adding
capability.

**Check (mechanical):** Is this function's body a single call to another
function with the same arguments?

## 6. Somewhat general-purpose interfaces

Design interfaces for the general case the module serves, with
implementations tuned for today's use. Special-purpose parameters added for
one caller are a warning sign.

**Check:** Does any parameter exist only to serve one caller?

## 7. Design it twice

For any non-trivial module, sketch at least one materially different
alternative before committing. Record the alternative and why it lost.

**Check:** Does the design document name an alternative?

## 8. Comments describe what code cannot

Interface comments describe the abstraction — what a caller may rely on.
Implementation comments explain why, not what.

**Check:** Does each exported type and function say what it guarantees?

## Worked examples from this repository

**Deep:** `filewriter.Executor.Run(ctx, in)` — one call hides the retry
ladder, prompt selection, parsing, writing, and diffing.
**Deep:** `filewriter.Writer.Apply` — hides snapshotting, temp staging,
rename-swap, and rollback.

**Amendments made under these principles** (v0.4 design.md § Amendments):
- AM-1 removed the `empty_content` error — an empty file is valid (principle 4).
- AM-2 made delete idempotent, keeping the signal as telemetry (principle 4).
- AM-3 made the parser pure; only the writer knows worktree resolution
  (principle 2).

## Mechanical metrics (post-implementation, report-only)

- Pass-through functions per package (principle 5)
- Exported identifiers vs. implementation size per package — a depth proxy
  (principle 1)
- Parameter count per exported function (principles 1, 3)
- Exported error values and types per package (principle 4)

These are proxies. They surface candidates for review; they do not measure
quality by themselves.
