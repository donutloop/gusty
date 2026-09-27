# ADR 0172: None is a singleton value, not the integer 0

## Status

Accepted (2026). Implements a gap the parity harness could never see: both backends agreed,
because both were wrong in the same way.

## Decision

`None` is a first-class value with its own dynamic type (`None`) and canonical value tag
(`TagNone`, already reserved in `pkg/lang/value.go`). A function that runs off the end, or a
bare `return`, yields `None`. The CLI stops echoing a program's last value: stdout is program
output only, and snippets (last statement a bare expression) still echo.

## Context

`NoneLit` existed in the AST and `KindNone`/`TNone()` in the type table, but nothing more:

```
$ gustyc --eval 'print(None)'        # printed 0
$ gustyc --eval 'def f():
>     x = 1
>
> print(f())'                        # printed 1 — the value of `x = 1`
$ ... print(f() == None)             # 0 — a procedure never equalled None
$ ... print(0 == None)               # 1 — 0 was None
$ gustyc --file prog.gy              # program output, then a stray `0`
```

The stray line was the more dangerous half: `print` returned the int `0`, and `--eval`/`--file`
printed the last value unconditionally, so *every* program's stdout ended with a line the
program never wrote. Anything piping `gustyc --file prog.gy` — an agent, a test harness, a
shell pipeline — had to know to discard it.

## Options considered for the AOT representation

1. **Reserved immediate** (`None` = a chosen `i32`, e.g. `INT32_MIN`). Cheapest, rejected:
   every `i32` is a legal integer, so `x = -2147483648` would print `None`. A lie that only
   shows up in user programs.
2. **Tag every value** (`{i32 tag, i32 payload}` everywhere). Correct but rewrites the calling
   convention, every arithmetic instruction, and the GC layout for one value. Rejected as
   disproportionate.
3. **Heap singleton, allocated once, never freed** — chosen. The runtime already models
   reference kinds as `%obj {kind, len, data}` slots created by `rt_alloc(kind)`; None becomes
   kind 4, cached in `@none_h` and handed out by `rt_none()`. Equality is handle identity; the
   object is a permanent root so the free list can never reuse it.

The interpreter gets the same shape for free: its heap already allocates reference kinds and
reserves handles above `1 << 20` so they never collide with raw small ints.

## The asymmetry that had to be designed around

Interpreter values are either heap handles or raw `int64`s, so `IsNone` can be asked at run
time. AOT values are untagged `i32`s: **the compiled backend cannot ask whether a value is
None**. So `irGen.isNoneExpr` decides statically from what the source says:

- the `None` literal;
- a variable whose *latest* assignment was None (cleared by any other assignment, so
  `x = None; x = 0` prints `0`);
- a call to a function that cannot produce a value.

That last rule is why `fdReturnsValue` walks the body with reflection instead of enumerating
statement types: a hand-written walk missed a `return` nested inside `match` and printed four
`None`s in the conformance corpus. Any `*ReturnStmt` with a non-nil expression anywhere counts,
and any `yield` counts (a generator evaluates to the list of yields).

## Static decision must not delete effects

Both static rules evaluate their operands before answering:

- `print(emit())` emits the call, *then* `rt_print_none` — otherwise the callee's output
  disappears and the backends disagree on ordering (ADR 0165's interleaving rule).
- `f() == None` emits the call, then folds the comparison. Folding `==` to a constant while
  dropping the call was caught by `TestNoneEqualityIsStaticButNotLazy`.

## Consequences

- `print(None)`, `x = None; print(x)`, `f() == None`, `0 == None`, `if None:` and
  `while x == None:` behave identically in the interpreter, the AOT binary, and (for the
  shapes the language supports) Python.
- The `--eval` echo rule is now: echo iff the last statement is a bare expression *and* the
  value is not None. `--json` reports `{"result": null, "type": "None"}` for a program ending
  in a void call instead of claiming an int `0` result.
- `IsNone` is part of the `Evaluator` API so the CLI and future tools ask instead of guessing.
- Bools still print as `1`/`0` and strings inside containers still print unquoted. Those are
  separate repr decisions, deliberately left out of this change.

## Alternatives rejected

- **Keep `None` = 0 and only fix the CLI echo.** Leaves `f() == None` false, `0 == None` true,
  and `print(None)` printing `0`; the CLI fix would then have to special-case a value that is
  indistinguishable from a legitimate one.
- **Echo `None` like CPython's REPL does for `--eval`.** `--eval`/`--file` run programs, not
  REPL lines; Python's `python prog.py` prints nothing extra and neither should we. Snippets
  keep their echo, which is what the documented `--eval "x = 1 + 2\nx"` contract relies on.
