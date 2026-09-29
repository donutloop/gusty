# ADR 0212 — a built-in trap is a typed raise, in every backend

Status: accepted (roadmap Gap R.18; first instance of the rule; R.19 is next)

## Context

```gusty
print(1 / 0)      # compiled: `inf`, exit 0        · interpreter: `division by zero`, exit 3 · CPython: ZeroDivisionError
print(7 % 0)      # compiled: `-1724971560`, exit 0 · interpreter: `division by zero`, exit 3 · CPython: ZeroDivisionError
```

Two defects in one line, and the interesting thing about them is that they are not the same
defect on the two sides:

- **The compiled backend never trapped.** It emitted `sdiv`/`srem`/`fdiv`/`frem` and continued.
  On x86 an integer divide by zero is a SIGFPE; on AArch64 it is *not* a fault at all — the
  instruction returns junk, so `print(7 % 0)` printed a different garbage number on each run and
  exited 0, and the float paths returned ±inf. Nobody had noticed because the output merely looked
  silly rather than wrong.
- **The interpreter trapped, but not in a way the language could see.** The six sites built
  `&EvalError{Msg: "division by zero"}` — a message with no `ExnType`. Exception matching is on the
  *class*, so `except ZeroDivisionError:` matched nothing and the handler never ran. The traceback
  printed a bare sentence where an exception should have been named, and the wording was wrong for
  three of the six operations (`7.0 // 0` said "float division by zero"; CPython says "float floor
  division by zero").

The general form: **a trap that reports a message instead of raising a typed exception is not a
trap in this language** — because handling is by class, and because a report without a class is a
string the user cannot program against.

## Decision

**One rule, applied to the arithmetic traps here and to the remaining ones by name in the
roadmap: every runtime error the language can produce is raised as a typed exception, on both
backends, with the reference implementation's wording.**

- Interpreter: one funnel, `zeroDivisionErr(kind)`, replacing six hand-built `EvalError` literals.
  Its wording table is CPython's (`division by zero`, `float division by zero`,
  `integer division or modulo by zero`, `float floor division by zero`, `integer modulo by zero`,
  `float modulo`) — shared by `/`, `//` and `%`, which is why `floorMessage(floor)` exists: the
  `/` and `//` branches were written as one and had drifted into one wording.
- Codegen: `guardNonZeroInt` / `guardNonZeroFloat` emit the test in front of every `sdiv`/`srem` and
  `fdiv`/`frem`, and the trap goes through `raiseTo` — the same entry point as an explicit `raise` —
  so `@exn_code` is set, an enclosing `except ZeroDivisionError:` unwinds to its handler, and a
  program that handles the error has run to completion (exit 0, per ADR 0211).
- The float test is `fcmp oeq double %r, 0.0`, which is what Python means: `1.0 / -0.0` raises, a
  NaN divisor does not.
- The *wording* is chosen from the source operands, not the instruction. Int `/` is lowered to a
  `fdiv` (PEP 238 true division), so the naive message would have been "float division by zero"
  for `7 / 0`; naming the instruction would describe our codegen instead of the program. The
  residual deviation is honest and documented: a parameter that our float-parameter lowering made
  a double (Gap R.3c / L11.6) makes its division say "float division by zero" where CPython says
  "division by zero". The class is exact; the wording follows our IR, and the fix belongs to the
  typing work, not to the string.
- Constant divisors are not special-cased: LLVM folds the test, the raise block survives, and a
  literal `7 % 0` traps at run time exactly as `x % 0` does. The constant folder in
  `imports.go` was deliberately left alone — folding a literal division is a compile-time event,
  and a program's arithmetic trap is not.

## Why the message is in the contract

Wording is usually a UI detail, and `docs/operations.md` says branch on `code`, not on `msg`. This
is the exception (pun intended) that earns its keep: an exception report is the *only* surface an
uncaught error has, and if ours reads `division by zero` where the reference reads `float floor
division by zero`, then a user searching a codebase for what a traceback meant gets nothing, and a
test that compares our output to CPython's needs an exception list. So for traps, wording parity is
tested (six cases, three engines).

## Codegen / IR implications

A guarded divide emits its own basic-block pair, the same shape as the bounds checks added by
ADR 0210:

```
  %z = icmp eq i32 %r, 0
  br i1 %z, label %.div.bad, label %.div.ok
.div.bad:
  ; setExn: exn_flag=1, exn_code=7, exn_msg="ZeroDivisionError: integer modulo by zero"
  br label %handler   (or %func.raiseexit when there is no handler)
.div.ok:
  %q = srem i32 %l, %r
```

`setExn` already sets `g.raiseUsed`, so main's `raiseexit` epilogue (ADR 0211's exit-status work)
travels with it, and ADR 0209's reference-derived emission carries the raise runtime blocks. The
unit test asserts the property at the module level: if a module contains `srem`, it must contain
`ZeroDivisionError`.

## Alternatives rejected

- **Call `exit(1)` (or a runtime `rt_die`) from a guard.** Rejected: no handler would ever run, and
  the exit status would lie for programs that catch the error.
- **Install an LLVM `resume`/personality-based unwinding table.** Rejected as vastly more machinery
  than the language needs; the existing handler-stack lowering already does what `try`/`except`
  requires, and this rule keeps every trap on that one path.
- **Rely on the CPU's trap for integer division and translate SIGFPE.** Rejected: AArch64 does not
  fault (that is how the garbage number escaped), and signal handling in a JIT'd shared object is a
  portability sinkhole.
- **Fold literal divisions into a compile-time refusal.** Rejected: `print(1 / 0)` is a valid program
  whose answer is a runtime `ZeroDivisionError`; refusing it is a compiler deciding the program is
  meaningless, and it would report class 1 for what is class 3.
- **Fix the three defects found while testing this one (multi-arm `except` dispatch, `return` inside
  `try`, mixed float/string returns emitting invalid IR).** Rejected as bundling — each is a separate
  defect in the exception lowering, each is recorded with its own measurement as roadmap Gap R.20,
  R.21 and R.22, and R.20/R.21 are registered as probes so they cannot quietly disappear.
