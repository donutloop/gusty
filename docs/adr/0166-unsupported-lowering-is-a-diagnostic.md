# 0166. Unsupported lowering paths are compile diagnostics, never unverifiable IR

Status: Accepted
Date: 2026-09-27
Audience: compiler maintainers, agents consuming the CLI

## Context

The AOT backend emits textual IR, and several language-legal programs reach a point
where the representation cannot express them: a `list[str]` element would need to store
an `i8*` in an `i32` heap slot (Gap I.2), a string argument would need an `i8*` parameter
where the callee declares `i32` (Gap J.5), calling through a `Callable` parameter has no
lowering at all.

Historically these paths did not fail — they emitted IR that LLVM rejects, and somebody
downstream noticed:

- `--emit-llvm` happily returned a module containing `call i32 @total(i32 @.lst1)` or
  `call i32 @greet(i32 @.str1)`.
- `--build` failed inside `llc`, with a temp-file path in the message.
- An agent driving the compiler could not tell "my program is unsupported" from "the
  compiler crashed", and had to parse `llc` stderr to find out which line of *its own
  source* was at fault.

L8.2 (ADR 0164) made verification a pipeline stage, which surfaced these bugs reliably.
The remaining problem is attribution and wording: the verifier's message describes the IR,
not the program.

## Decision

**An unsupported lowering path is a compile diagnostic raised by the stage that would have
emitted the IR. It never emits a module that LLVM would reject.**

Rules applied in `pkg/lang/codegen.go` / `heapargs.go`:

- Detect at the point of lowering, not after emission: `heapElem` (string inside a runtime
  container), `argVal`/`strArgCheck` (string passed to a user function), `call` for
  unknown callables.
- The message names the construct and the participant where the message can:
  `strings are not supported as function arguments in the AOT backend yet (parameter
  "name" of greet); the interpreter supports them`.
- The message always says which backend *does* support it, so the caller has a next action.
- The wording is a contract: fixed substring per case, listed in
  `docs/operations.md` § Codegen capability messages, asserted by tests
  (`TestStringArgumentDiagnosticIsStable` pins the wording against drift).
- Machine path: through `--emit-llvm --json` as `{"ok": false, "phase": "compile",
  "error": "...", "exit": 2}`; through `--build` as a `BuildResult` error.
- `--verify-llvm` (ADR 0164) stays as the safety net: if a path is missed, the verdict says
  `LLVM rejected the module; this is a compiler bug, not a source error` rather than
  pretending the program is at fault.

## Rationale (agentic)

Agents consuming a compiler need three outcomes kept apart: the program is fine, the
program is unsupported (with a named construct and a working alternative), or the compiler
is broken. Emitting bad IR collapses the second into the third. Naming the parameter turns
a dead end into an edit the agent can apply, and a stable substring means it never has to
re-learn the wording.

## Codegen / IR implications

- `argVal(a, idx)` now rejects string-valued arguments (literal or f-string) before
  `g.value`, so no `i32 @.strN` operand can reach a call site
  (`TestStringArgumentIsReportedNotMiscompiled`, `TestNonStringArgumentsStillCompile`).
- Keyword and default arguments are covered too, because the check sits in the shared
  argument-lowering closure.
- Compile failures produce no partial IR for these cases (`res.IR == ""`), so a caller
  cannot mistake the artifact for a usable module.

## Alternatives rejected

- **Let the verifier report it** (status quo before this change) — correct but late and
  impersonal: it reports IR line numbers in a temp file, not the program construct.
- **Silently coerce the string to an `i32` handle** — would produce a program that runs and
  prints garbage, which is worse than a refusal.
- **Emit IR plus a warning comment** — an `llc` run would still fail, and `--emit-llvm`
  output that is known to be invalid is not an artifact.
- **Refuse at semantic-analysis time** — the checker has no representation knowledge
  (whether a parameter is a container is a codegen-level inference), and the interpreter
  *does* support these programs, so a checker error would break the interpreter path.

## Consequences

- Every capability gap is a listed, matchable message, and `docs/operations.md` is the
  catalogue — adding a gap means adding a row and a test asserting the wording.
- The real fixes (Gap I.2's string heap kind, Gap J.5's string parameters) stay open but
  are now behind a message the team promised to remove, which keeps them honest.
- The verifier remains the backstop: any missing check shows up as a compiler-bug report
  rather than a silent bad module.
