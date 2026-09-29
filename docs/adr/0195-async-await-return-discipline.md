# ADR 0195: The await/return discipline is a shared front-end check, and effect signatures are its machine path

**Status:** Accepted
**Date:** Phase 7, L7.6
**Decided by:** Phase 7 — "Correctness as a product"
**Related:** ADR 0167 (L5.6 `async def`/`await`/`await`-at-top-level), ADR 0176 (L7.1 deferred
coroutines + `async for`/`async with`), ADR 0149 (L5.3 match exhaustiveness — the precedent
this copies), ADR 0154 (match exhaustiveness as a warning), ADR 0172/0173 (diagnostic identity,
exit-code contract), ADR 0146/0127 (`--schema`, `--lang`: publish a fact before an agent has to
guess it)

## Context

`async def` in Gusty is a *deferred* function: calling it builds a coroutine and runs
nothing; the body runs when the coroutine is awaited, once. Neither backend can enforce
that. Measured before this round, on

```gusty
async def f(x):
    return x * 2
v = f(2)          # nothing has run
print(v)          # what does this print?
```

| Path | Behaviour |
|------|-----------|
| `--interp`, `--file` (AST interpreter) | prints `<coro>` — the repr of a value whose body never ran |
| `--aot` / `--jit` (LLVM) | prints `4` — codegen lowers `f(2)` as a *call*, so `v` holds the result |
| CPython | runs, prints `2`, and warns `RuntimeWarning: coroutine 'f' was never awaited` |

So the same program is three different programs, and two of the answers are plausible. An
agent writing async Gusty gets a number from the compiled backend and a `<coro>` from the
interpreter with no signal that either is a bug. A double await was worse: the interpreter
re-ran the body and printed `4 4`, CPython raised `RuntimeError: cannot reuse
coroutine object`.

The reference implementation's discipline is not enforceable at runtime either — CPython can
only warn *after* the fact, from a `__del__`. The language has to decide in the front end.

## Decision

**(a) The discipline is checked in the shared front end, in the one place both backends
already agree.** `pkg/lang/effects.go` runs inside `Analyze`, so the interpreter leg, the
AOT leg, `--check`, `--verify`, the benchmark harness and the LSP see the same verdicts:

| Code | Level | Rule |
|------|-------|------|
| `async.coro.never_awaited` | error | a coroutine is created and nothing ever awaits it, so its body never runs |
| `async.coro.awaited_twice` | error | one coroutine object is awaited twice on a path (CPython: `RuntimeError`) |
| `async.generator.unsupported` | error | an `async def` whose body yields — no backend lowers async generators |
| `async.await.outside_coroutine` | warning | `await` inside a plain `def` (CPython: `SyntaxError`) |
| `async.async_stmt.outside_coroutine` | warning | `async for`/`async with` inside a plain `def` |
| `async.await.not_coroutine` | warning | `await` on a value that provably is not a coroutine (CPython: `TypeError`) |
| `async.missing_return` | warning | an `async def` path runs off the end while other paths, or its `-> T`, promise a value |

Errors are refusals (exit 1, "a bad program is not a compiler bug"); warnings ride the
machine path without moving the exit code, matching ADR 0154's precedent for match
exhaustiveness. The distinction is drawn on one question — *can this program mean what it
appears to mean?* A dropped coroutine's body never runs and a double await runs it twice:
the program has no meaning, so refuse. An `await` in a plain `def` evaluates exactly what
it names and a missing `return` is the same `None` a sync function gives: suspicious, but
meaningful, so warn.

**(b) The proof is a small, flow-sensitive abstract interpretation — not a syntactic scan.**
Each function body is walked with a symbolic value state, and each local is `coro`,
`awaited`, `not_coro` or `unknown` (top). Merges widen, branches are joined, loops are
walked once and their tails folded back. A `try` body and its `except`/`finally` arms are
walked in sequence rather than from a restored state, which is the direction that keeps both
verdicts honest: a coroutine created in the body is still pending when the arms and the
end-of-body sweep look at it, and an `await` already performed in the body is still on record
when an arm awaits the same object twice. The three error
rules are decided *at the point where the promise breaks* — the binding that stores it, the
rebinding that overwrites it, the operation that consumes it as a mere value, the second
`await` — because that is the last line the author could still have fixed.

**(c) Effect signatures are published, per function, as data.**

```go
type EffectSummary struct {
    Function string; Async bool; Line int
    Effects  []string   // "await" | "yield" | "raise"
    Awaits, Yields, Raises, CoroutineCalls int
    ReturnsValue, ReturnsBare, FallsThrough, Terminates bool
}
```

`gustyc --effects <src>` prints them, `gusty effects <file>...` does the same for files,
`--json` emits `{schema_version, language_version, generated_by, source, functions[]}`, and
`gustyc --schema` declares `definitions.effectSummary`. This is not decoration: the rules in
(a) are *decided from* these facts, so publishing them makes an agent's reasoning
reconstructible — `falls_through && (returns_value || annotated) && async` *is*
`async.missing_return`, and `coroutine_calls > 0` with no await *is* `async.coro.never_awaited`.
A tool that disagrees with a verdict can see why. Counting is deliberately a *signature*, not
a profile: `for i in range(10): await f(i)` counts one await site, because the question the
table answers is "what does this function do", not "how often".

**(d) The rules are drawn where our semantics and the reference implementation's diverge,
and named in the message.** `await` at module scope stays legal (ADR 0167); `async for`
stays legal with a list of coroutines; awaiting a plain value is legal. Those are documented
divergences, not errors — but every message states the Python behaviour next to ours, so the
author learns the discipline rather than just our dialect.

## Alternatives rejected

- **Runtime warning, like CPython's `__del__`.** Requires a finaliser, fires after the fact,
  needs allocation liveness the collector deliberately does not track, and tells you about a
  coroutine that was dropped at the end of a run.
- **Refuse `await` outside an `async def` as an error.** That is what CPython does, and it
  would reject the handoff helper (`def run(c): return await c`) that our evaluation model
  supports and the corpus uses; also `await` at module scope is documented behaviour (ADR
  0167), so the language would contradict itself.
- **Desugar `async def` to a state machine / real suspension now.** That fixes the eager-AOT
  gap for good, but it is L11.1's oracle work and L7.3's event loop; deferring it here is the
  only way to get the *checks* — which are what agents actually need to write honest async —
  in without a rewrite. The gap is named in `roadmap.md` as L7.6a, and pinned as
  `programs/probe_async_eager`, rather than papered over.
- **Implement the async surface in codegen instead of refusing it.** `async for` over a
  lowered coroutine needs a suspension the LLVM path does not have: `await` inside the loop
  body would run the coroutine but the loop could not. Refusing is honest, matches the
  ADR-0167 precedent for `yield` inside `async for`, and keeps "the compiler refuses" ahead
  of "the compiler lies".
- **Count effects per execution (a profile).** Then `for i in range(10): await f(i)` would
  report 10 awaits and a loop with no iterations 0 — a signature must describe what the body
  *does*, and the rules are all "at least one site" questions.
- **Run the async rules only in `--check`.** Then the interpreter leg and the AOT leg would
  keep disagreeing, which is the whole defect this round exists to close.

## Consequences

- Programs that "worked" are now refused: `v = f(2)` with no await is a compile error on
  every path that consults the checker, and the interpreter refuses through the conformance
  leg (`InterpreterRun` runs the checker before it runs the program). `--eval`/`--file
  --interp` keep running without the checker, as documented, and the gap is stated in
  `docs/operations.md` rather than hidden.
- The corpus gains `programs/async_effects.gy`, a program whose legality is *arguable* — a
  coroutine handed to a function, a list of coroutines awaited by `async for`, a bare `await`
  whose result is discarded — pinned to one answer on both backends. It is the first async
  program in the corpus: L5.6/L7.1 shipped async with no program in the matrix and no section
  in `docs/language.md`.
- Four separate defects were isolated while pinning behaviour and are recorded in
  `roadmap.md` (Gap R) rather than fixed here, because only one of them is async-shaped: a
  compiled loop whose body reassigns the variable in its own condition spins forever
  (interpreter `3`, compiled binary never returns); `await` followed by a `while True` that
  returns a string emits a call to an undefined `@rt_str_intern2` — `llc` rejects the module
  and `--verify-llvm` catches it, which is the L8.2 gate doing its job; a
  `for a, _ in pairs:` destructure has no tuple lowering at all; and a user function named
  after a libc symbol (`def sync()`) answers `0` in the compiled binary where the
  interpreter answers `7`, because the call linked against libc's `sync()` — a silent wrong
  answer from the ABI, not a refusal.
