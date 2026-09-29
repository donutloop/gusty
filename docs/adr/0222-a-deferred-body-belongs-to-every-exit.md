# A deferred body belongs to every exit

## Status

Accepted (cycle 170, Gap R.23) — both backends. The method-shaped hole it uncovered is recorded as
Gap R.41, and was verified as pre-existing rather than caused here.

## Context

```gusty
def f() -> int:
    try:
        return 1
    finally:
        print("fin")     # gusty (both backends): prints nothing, then 1 · CPython: fin, then 1
```

Eleven shapes were measured against CPython before anything changed. Nine were wrong, and **both
backends were wrong in the same way**: `finally` ran on fall-through and after a handled exception,
and not at all when the block was left by `return`, `break`, `continue`, or by an exception that no
arm matched. `finally` is the construct people reach for precisely because a transfer happens — a
file closed, a lock released, a progress line printed — and the language silently skipped it on
those paths. Parity could not see it, because the two backends agreed; only CPython could, which is
why the ledger's third leg exists (ADR 0186).

The same statement had a second wrong half, found while reading the interpreter's `case *TryStmt`.
This interpreter moves `return`/`break`/`continue` as Go `error` values (`returnSignal`,
`loopSignal`), the same channel it uses for raised exceptions, and the arm-matching code asked "did
something come out?" rather than "did an *exception* come out?". So a bare arm caught control flow:

```gusty
def f() -> int:
    try:
        return 1
    except:
        print("caught a return")   # gusty printed this and returned 3; CPython returns 1
    return 3
```

## Decision

**A `finally` body is attached to every exit from its `try`, and an arm sees only exceptions.**

*Interpreter.* `case *TryStmt` now runs the arms (when the in-flight thing is an exception) and then
runs the deferred body exactly once, before releasing whatever transfer is pending. A `return` or
`raise` inside the `finally` returns *its own* signal unchanged, which is Python's
last-transfer-wins and needed no extra code — the pending value is simply dropped. Arm errors
(`raise_from_arm`: an arm that itself returns) also run the deferred body first.
`catchesException(err)` is the new predicate arms match through: `*EvalError` is an exception,
`*returnSignal`/`*loopSignal` are not, and anything unknown propagates uncaught — the safe direction.

*Compiled.* `irGen` carries a `deferred [][]Stmt` stack of the `finally` bodies belonging to the
`try` statements being lowered. `tryStmt` pushes its own body for the duration of its body and arms,
and pops it before emitting its straight-line `finally:` block (which emits the body itself, once).
Transfers consult the stack:

| leaving the `try` by | what runs |
|----------------------|-----------|
| `return` | the value is emitted first, then `runDeferred` (every pending body, innermost first), then `gcCloseFrame`/`ret` |
| `break` / `continue` | `clearExn` if in an arm (ADR 0218), then `runDeferred`, then the branch |
| an exception no arm matched | `runDeferredInnermost` — this statement's own body only |
| a `raise` inside an arm | `runDeferredInnermost`, then the branch outward |

Two decisions inside that table are the whole difference between correct and double-running:

1. **An escaping exception runs only the innermost body.** The outer ones are run by *their own*
   `try` statements when the hand-off reaches their handler blocks. Running the whole stack at the
   raise site printed `outer fin` once per path — once on the way out and again on the outer
   statement's straight-line path. A `return`/`break`/`continue` does cascade, because nothing else
   is ever going to run those bodies.
2. **An escape is judged by where control went, not by opcode.** `blockEndsInTerminator` (ADR 0218)
   calls any `br` a terminator, but an `if` or a loop ends its block with an ordinary forward `br`;
   treating that as an escape would silently stop the walk before the outer bodies. So
   `escapedTerminator` looks at the text a statement emitted and counts only `ret`,
   `unreachable`, and a branch headed for the raise-exit.

The value-before-finally ordering is Python's, and it is observable: with `n = 1`, a body of
`return n` and a `finally` of `n = 99; print("fin", n)`, both backends now print `fin 99` then `1`.

## Agentic rationale

Nine silent wrong answers on the same construct, identical across backends, is the class of defect an
agent cannot discover by testing its own program against the other backend. `programs/deferred_bodies.gy`
is now a standalone parity program — 24 comparisons against CPython's exact output, run under both
`--interp` and `--aot` and by `python3` on the same file — and the 20-shape integration table plus the
10 unit tests carry the rest. The matrix moved to 92 cases, 66 parity rows, 47 oracle matches.

The exit-code assertions were written from the documented contract rather than from observation, and
that mattered: an uncaught exception is class **3** on both backends (ADR 0211), and a test written by
watching CPython would have demanded 1 and been wrong about our own interface.

## Codegen / IR implications

Deferred bodies are *inlined at each transfer site*, which duplicates their code per path — the same
trade `tryStmt` already made between the handler and straight-line blocks. Two things fall out of it
that are recorded rather than papered over:

- A `finally` body emitted inside a `ret` path runs before `gcCloseFrame`, so handles it allocates are
  still rooted when it runs, and its own returns close the frame once (the `runDeferredLevel`
  truncation stops a body re-entering itself).
- `g.funcRaiseExit` is not set for **methods**, so any method containing a `try` emits
  `br label %` and `llc` rejects the module — exit 2, a toolchain rejection for a program whose answer
  is `m fin\n3`. Verified against the binary from before this cycle, which fails identically: it is
  Gap R.41, pinned at the time by a debt row for this shape and a test asserting the malformed
  emission. Gap R.41 landed in the next cycle (ADR 0223): that probe was promoted to the parity program
  `programs/method_try.gy`, its ledger row was deleted, and the pinned test was replaced by
  `TestMethodWithTryCompiles` — exactly the life cycle a pinned wrong answer is supposed to have.

The honest long-term shape is LLVM's own: `invoke` plus a cleanup landingpad per `try`, where the
runtime walks the pending cleanups instead of the compiler duplicating them per exit edge (already
noted for `@exn_flag` in ADR 0218). That is a redesign of exception lowering, not of this rule.

## Alternatives rejected

- **Leave it: "the interpreter and compiler agree".** Rejected — that is the parity fallacy this loop
  keeps hitting; agreement between two implementations is not evidence.
- **Run the whole deferred stack at every raise site.** Rejected by measurement: double execution, and
  `outer fin` printed twice on the propagating shape, where CPython prints it once.
- **Reuse `blockEndsInTerminator` for the walk.** Rejected: it would have stopped the walk at the
  first `finally` whose last statement is an `if` or a loop — a wrong answer that looks like a passing
  test, because the simple measured shapes do not contain one.
- **Emit `try`/`finally` as LLVM `invoke`/cleanuppad now.** Rejected for this cycle: it is the right
  end state, but it is the whole exception-lowering redesign, and the language needed the deferred
  bodies to run *today*, in both paths, with tests.
- **Fix the method case in this commit.** Rejected under one-commit-per-feature: it is a separate
  pre-existing defect with its own measurement (Gap R.41), and bundling it would have made the
  finally-rule change impossible to review.
