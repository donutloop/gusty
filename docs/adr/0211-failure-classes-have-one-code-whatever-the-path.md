# ADR 0211 — a failure class has one code, whichever path produced it

Status: accepted (roadmap Gap R.17; the exit-code half of L11.8)

## Context

```gusty
xs = [1, 2, 3]
print(xs[-4])
```

Run this program and all three ways of asking report three different things. The linked binary
exits **1** after printing `IndexError: index out of range`. `gustyc --interp` exits **3** —
the runtime-error class the exit-code table promises. `gustyc --aot` prints the same traceback
and exits **0**.

The last one is the bug this ADR is about, and it was not a wrong constant: the in-process JIT
links the program, dlopen's it, calls the generated `main`, and

```go
out, outErr = captureFD1(func() { C.jit_call(fn) })
```

discarded the value `main` returned. The C helper already returned it (`static int
jit_call(void* fn) { return ((int (*)(void)) fn)(); }`) — the answer existed one frame down and
was thrown away on the way past. So `--aot` had no way to know the program died, the CLI printed
`"exit": 0` in its JSON, and the oracle's compiled leg recorded a trap as an *answer* (output
differed from CPython's traceback, so the report called it a divergence rather than a leg that
did not complete — ADR 0166's rule in the wrong column).

While measuring, the same shape turned up a second time in the same branch: every failure of the
compiled path — source error, codegen refusal, and `llc` refusing the module *we* emitted — came
back as an untyped error and was reported as exit 1, "your program does not compile". The table
says an LLVM rejection is exit **2**, a compiler-bug class; `--build` got that right and `--aot`
never did. One event, two codes, depending on which flag you used.

## Decision

**The status is data, and the classification is a type.**

- `JITResult` carries `Code int` — the generated `main`'s return value — documented as the only
  place the compiled backend records that a program trapped. `dlopenRun` returns it, `JIT`
  stores it.
- The CLI maps `Code != 0` to `exitRuntime` (3) on the run path, and its `--json` payload's
  `exit` field is that same number — never a separately-computed one — plus the target's fd 2 as
  `"stderr"` so a machine caller gets the diagnostic without scraping a terminal.
- Toolchain failures become a type: `lang.ToolchainRejectionError{Tool, Stage, Err, Output}`,
  produced by `toolchainFailure(stage, tool, err, output)` at the `llc`/`cc` sites. Callers use
  `errors.As`, never message matching. Its `Error()` keeps the old `"jit: llc: …"` text, because
  docs and scripts already quote it.
- A tool that could not be **started** (uninstalled, not on `PATH`) is *not* a rejection:
  `toolchainFailure` checks `exec.ErrNotFound`/`os.ErrNotExist`/`os.ErrPermission` and says
  `could not be run`, leaving it class 1. Accusing the compiler of emitting an invalid module
  because someone forgot to install LLVM would send a reader looking for a bug that isn't there.
- The oracle's compiled leg now reports a trap as a **failed leg** (`compiled program trapped
  (exit 1): IndexError: …`) — a leg that did not complete is never recorded as an answer.
- The REPL forwards the target's stderr and names the status, instead of showing a blank line
  for a program that raised.

## What this says about the loop

The bug class is by now a familiar one in this codebase: **the information existed and the
mechanism dropped it.** ADR 0209 was a runtime block that travelled by flag instead of by
reference; ADR 0198 was symbol names prefixed at some sites and not others; ADR 0202 was a
diagnostic said twice. Here the status was computed correctly by the generated program, returned
correctly by the C helper, and dropped in the Go frame that had no use for it yet. The general
test to run: for every quantity the toolchain already computes (exit status, verification result,
diagnostics, timings), ask *who consumes it* — and if the answer is "nobody, we re-derive it", that
is the bug.

It also sharpens what "machine-readable" means in this project's contract. The JSON had an
`"exit"` field for the compiled run that was always `0` — a field shaped like the truth and
worse than no field, because an agent that trusts a documented schema has no reason to doubt it.
A payload field must be *derived from the artifact it describes*, and a test must be able to fail
if it is hard-coded.

## Codegen / IR implications

None for the emitted IR itself: `main.raiseexit` already returned 1 after `rt_die` (the linked
binary has been correct all along — which is exactly why the defect survived: every test that
linked and ran a binary saw the right status, and only the in-process path dropped it). The IR
section of this ADR is therefore a warning: **a contract verified only on one execution path is
unverified.** `integration/` runs binaries, `cmd/gustyc` runs the CLI, and only `--aot` ran
in-process; the new tests cover the CLI contract for all run paths.

## Alternatives rejected

- **Run the compiled program as a subprocess and read its exit status.** Rejected: the whole
  point of the in-process JIT is no process spawn per REPL turn (fast interactive feedback), and
  the status was already available at zero cost.
- **Have the CLI re-run the linked binary to check the status.** Rejected: two executions of a
  program with side effects is a different program, and it doubles the failure modes.
- **Classify toolchain failures by matching `"jit: llc:"` in the message.** Rejected: message text
  is a human surface (the table says branch on `code`, not on `msg`); the typed error is the
  machine surface, and the test for the missing-tool case is only possible because the type is
  there.
- **Report the missing-`llc` case as exit 2 "so the CI log looks alarming enough".** Rejected,
  see above.
- **Fix the two traps that still answer rather than raise (`1 / 0` → `inf`, missing attribute →
  `0`) in this commit.** Rejected as bundling: they are wrong answers, not wrong statuses — the
  program genuinely runs to completion, so exit 0 is *correct* for them. Recorded as roadmap Gap
  R.18 and R.19, with measurements, and they are the next things this loop should take.
