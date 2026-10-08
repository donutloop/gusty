# 0312. A toolchain call that stops answering is a failure class of its own

Status: accepted. Roadmap: `Gap R.194` (a subprocess call with no budget cannot fail, only stall),
`Phase 3` (tooling), `Phase 10` (targets/portability — the pinned toolchain is a dependency, and a
dependency that hangs is not a dependency that failed). Continues: ADR 0164/0166 (the exit-code contract:
one failure class, one code, reached with `errors.As`), ADR 0211 (a toolchain that is *not installed* is
not a module rejection — the same honesty rule, applied to a toolchain that is installed and silent),
ADR 0231 (`--debug` reads its claim from the artifact), ADR 0302 (one backend, so one place where a
program's behaviour is decided — and one place where a hung `llc` stops it).

## Context

CI's `test-on-ubuntu-with-llvm-20` job died like this:

```
Run go test -tags=llvm20 ./pkg/...
panic: test timed out after 10m0s
running tests:
    TestTextPredicatesPrintAVerdict (7s)
    TestTextPredicatesPrintAVerdict/not_verdict_false (0s)
...
os/exec.(*Cmd).CombinedOutput(...)
github.com/donutloop/gusty/pkg/lang.runIR(...)   gc_function_ir_test.go:77
```

The message is wrong twice over. `not_verdict_false` is a `print(not "1".isdigit())` that answers in
70 ms on any machine; it was simply the case holding the microphone when the alarm went off. And the
`runIR` frame underneath it — an `exec.Command("llvm-as-20", …)` followed by
`exec.Command("lli-20", …)`, both waited on with `CombinedOutput()` and **no deadline anywhere** — is
the only frame in the dump that describes what the process was actually waiting for. Every toolchain
call in the compiler was written that way: `llc` (JIT, build, bench), `cc` (three sites), `opt`,
`llvm-dwarfdump`, and `python3` on the reference leg. Eleven calls, zero budgets.

Two distinct faults live in that arrangement, and it is worth keeping them apart because they got two
ADRs:

* **A call that hangs is unreportable.** The suite would sit inside `lli` until `go test`'s own alarm
  fired, and the alarm names the running test, not the stuck child. Nothing in the process could say
  "`lli-20` has been thinking for nine minutes", because nothing had ever been asked how long the tools
  were allowed to think.
* **A suite that is merely slow is a separate fault**, and was the proximate cause of this particular
  run: measured below, the package needs ~350 s of single-threaded work, which does not fit a
  four-core runner in ten minutes. That one is ADR 0313's problem; this ADR makes the *hang* — the shape
  the log showed — impossible to confuse with anything else, and makes it end.

## Decision

**Every call the compiler makes to an external tool carries a budget, and an expired budget is its own
class.** `pkg/lang/tool_budget.go` owns all of it; there is exactly one `exec.CommandContext` in the
tree (`grep -c 'exec\.Command' pkg/lang/*.go` is a claim this decision is what keeps true), and every
stage reaches the toolchain through `runToolStage`/`toolCall`.

```go
const ToolBudgetDefault   = 5 * time.Minute   // llc, cc, opt, llvm-as, llvm-dwarfdump
const OracleBudgetDefault = 2 * time.Minute   // one CPython reference run
var ToolBudget   = envBudget("GUSTY_TOOL_TIMEOUT", ToolBudgetDefault)
var OracleBudget = envBudget("GUSTY_ORACLE_TIMEOUT", OracleBudgetDefault)
```

Four parts, each with a reason the obvious alternative fails:

1. **`ToolTimeoutError`, not `ToolchainRejectionError`.** A tool that was killed never said no, so
   wrapping it in the rejection type would report a compiler bug (exit 2) for a machine fault.
   `toolchainFailure` now asks for a timeout *before* it builds a rejection, and the exit-code contract
   gains **class 8, "the toolchain never answered"** — a code that is neither "your program is wrong"
   (1) nor "the compiler is wrong" (2). `--aot`, `--build` and `--verify-llvm` all reach it, and the
   `--json` payload's `phase` becomes `"toolchain"`. Classification stays `errors.As`-only; the CLI's
   check order puts the timeout ahead of the rejection because the sentence a killed `llc` arrives in
   usually names the stage that was running.
2. **The budget is asked, not inferred.** `toolCall` keeps the `context.Context`, and `timedOut()`
   reads `ctx.Err()` directly. Both alternatives were tried and both were wrong: a channel closed by a
   goroutine watching `ctx.Done()` loses the race against `Wait` (the CI's own llc call came back as
   plain `context deadline exceeded`, with no tool, no stage and no number attached), and consulting
   the `Cancel` hook misses the case where the budget expires *before* the child is started — os/exec
   returns from `Start` without ever calling `Cancel`, which is what a 1 ns budget does every time.
3. **The kill takes the process group**, and `WaitDelay` bounds the pipes. `cc` is a driver: kill only
   it and the `as`/`ld` it forked survives holding the write end of stdout, so `Wait` blocks on a pipe
   whose writer outlived its parent — the same hang, one layer down. `Setpgid` plus
   `kill(-pid, SIGKILL)`, with the plain `Process.Kill` kept for Windows; the budget, the class and the
   message are the portable half, the group signal is a Unix refinement.
4. **The budgets are tunable and say so.** `GUSTY_TOOL_TIMEOUT`/`GUSTY_ORACLE_TIMEOUT` take a duration;
   a value that does not parse is written to stderr and the pinned default is kept, because an operator
   who set the variable and is still waiting five minutes has to learn it never took effect.

What is deliberately **not** timed: the in-process run. `dlopenRun` executes the user's program in this
process, and `while True:` is a program, not a bug — a budget there would be the CLI killing a program
for being long. Only *tools* are timed, plus the reference leg's `python3`, where the harness is waiting
on someone else's answer and a row whose oracle was killed reads "the oracle gave up" rather than
"the reference answered nothing".

## Agentic rationale

An agent driving this compiler needs three things from a toolchain fault and had none of them: which
tool stopped, how long it was allowed, and whether to look at its own source or at the machine. All
three are now in the type, in the message (`llc: /usr/bin/llc-20 gave up after 1ns and was killed — it
did not refuse the module, it stopped answering (… pinned LLVM 20 on PATH)`), in the exit code (8) and
in the JSON payload (`phase: "toolchain"`, `exit: 8`). Branching on those is supported; scraping stderr
is not required and no longer rewarded.

The budgets are also part of the pinned-toolchain contract in `docs/operations.md`: the LLVM pin names a
binary, and a binary you cannot put a deadline on is a dependency you do not control.

## Consequences

* `pkg/lang/tool_budget_test.go` pins the six properties that matter, driven by shell stubs named for
  the behaviour they fake (`stub-never-answers`, `stub-with-children`, `stub-answers`): the call ends;
  the error names the tool and carries the budget; `errors.Is(err, context.DeadlineExceeded)` survives
  the classification; a tool that leaves a backgrounded child holding stdout still returns at its
  budget; a *missing* tool keeps its exec error and is never a timeout (else the advice would be
  "raise the budget" instead of "install LLVM 20"); and a tool that answers is untouched.
* `cmd/gustyc/toolchain_timeout_test.go` drives the real binary with `GUSTY_TOOL_TIMEOUT=1ns` and
  asserts exit 8 with `phase: "toolchain"`, **with a control** running the same program at the pinned
  budget for exit 0 — the shape that stops an assertion being satisfied by a CLI that reports 8 for
  everything.
* `TestToolchainExitKeepsSilenceApartFromRejection` pins the one-way door of `%w`: a stage that wraps a
  timeout with `%v` loses the class and lands on 1. That row looks like a bug in the table and is a
  regression test for the wrapping discipline (`build.go`'s two toolchain errors are `%w` now).
* CI's budget is derived from these numbers, so the defaults are pinned in prose in
  `docs/operations.md` and asserted in a test (`ToolBudgetDefault != 5m` fails the suite): a change to
  one is a change to that page.

## Alternatives rejected

* **Give the CI step a bigger `go test -timeout`.** This makes the observed job pass and fixes nothing:
  the next hung `lli` buys 30 minutes of nothing, and the report still names the wrong case. It is also
  half of what ADR 0313 does properly.
* **File a timeout as a `ToolchainRejectionError` (exit 2).** One type fewer, and every hung runner
  becomes a compiler-bug report. The distinction between 1/2/8 is the whole content of the exit-code
  contract; collapsing two classes to save a type is how that table became unusable once already (Gap
  J.3).
* **`context.WithTimeout` at each of the eleven call sites.** They would drift within a month — one
  site forgets `cancel`, another forgets `WaitDelay` — and the interesting parts (the group kill, the
  classification, the missing-tool exception) are not the kind of code to duplicate eleven times.
* **Kill only the child os/exec started.** Cheaper, and it leaves `cc`'s assembler holding the output
  file; the test with the backgrounded child exists precisely because that failure looks like success
  until the machine runs out of them.
* **Timeout the user's program too.** `while True:` must be allowed to run; the in-process path stays
  untimed, on purpose, in the comment as well as the code.
* **Per-test timeouts only, in the harness.** The REPL and `--eval` wait on the same `llc`; a suite that
  cannot hang while a user's session can is a suite that was never the problem.

## References

`pkg/lang/tool_budget.go`, `pkg/lang/tool_proc_unix.go`, `pkg/lang/tool_proc_windows.go`,
`pkg/lang/toolchain_error.go`, `pkg/lang/tool_budget_test.go`, `cmd/gustyc/toolchain_timeout_test.go`,
`docs/operations.md` (Exit codes, The three pinned toolchains), ADR 0164, 0166, 0211, and ADR 0313 (the
throughput half of the same CI log).
