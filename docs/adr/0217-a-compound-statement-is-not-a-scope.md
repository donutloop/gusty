# A compound statement is not a scope

## Status

Accepted (cycle 165, Gap R.24). Supersedes the child-scope handling of `try`, its handlers and
`finally`, of `while`, and of `match` arms in the semantic analyser.

## Context

The analyser created a child scope for a try body, for each handler, for a `while` body and else,
and for each `match` arm, then threw those scopes away. `finally` was not analysed at all. The
consequence was measured, not guessed:

```gusty
def f() -> int:
    try:
        a = 7
    except:
        a = 0
    return a
```

`--check` said `error at 5:12: undefined name "a"`, `--aot` exited 1 with `jit: 1 error(s) in
source`, while `--interp` printed `7` and CPython printed `7`. Same for a name first assigned in a
`while` body, in a handler, in a `finally` clause, and in both arms of a `match` — five shapes that
are ordinary Python, all refused by the front end. Two other compound statements already did the
right thing for the right reason, with comments saying so: `if` analyses its branches in the
enclosing scope ("assignments there flow outward (like Python)"), and `for` shares the enclosing
scope because "runtime uses shared vars". Four sites had simply never been brought to that rule.

Two things made this worth more care than "just delete the scopes":

- **Visibility and definiteness are different questions.** Some of the refusals were protecting a
  real signal: `def f(x): match x: case 1: pass; case y: pass; return y` genuinely may raise
  `NameError`. If the fix only made names visible, that case would go silent.
- **`finally` had never been walked**, so nothing inside it was checked. That is a whole statement
  of the program the checker was blind to — including a call to a function that does not exist.

## Decision

**A compound statement binds its names in the enclosing function or module scope.** Python has one
flat scope per `def` and one per module; a block inside one does not create a scope. The analyser
now analyses `try` bodies, every handler, the `finally` clause, `while` bodies and `else`, and each
`match` arm in the enclosing scope, joining them in the same statement as `if` and `for` already
did. Pattern captures are treated the way assignments are — visible from the binding point on, and
locals of the enclosing function.

**Definiteness is tracked separately, and is the intersection over paths.** `definiteOnEveryPath`
gives a name "definite after this statement" only if every path reaching that point assigned it, or
it was definite before:

- a `try` counts the no-exception path and each handler as separate paths, each starting from the
  state before the `try` — the body may raise at any point; a name assigned only in one handler is
  visible but not certain;
- whatever a `finally` assigns is certain, because it always runs;
- a `while` body may run zero times, so names first assigned inside it are visible but not certain;
- `match` arms are intersected too, so a capture in one arm is visible but not certain, while a
  binding present in every arm is certain.

For the constant-pattern narrowing, the subject's `Literal[v]` type became a **temporary shadow** —
snapshot the scope's names before the arm, apply the narrowing and the captures, analyse the arm,
then restore any name the arm did not itself decide. Narrowing must not outlive the arm, and the
arm's own assignments must.

The resulting diagnostic for a partially-bound local is the existing `possibly unbound: "y" is not
definitely assigned on all paths` **warning** rather than the old `undefined name` **error**, because
that is the accurate statement: the name exists and other paths bind it; this path may not have.

This is a real trade, said plainly rather than argued away. `--check` exits 0 on warnings and 1 only
on errors, so the partial-capture program that used to fail a build now passes it with a diagnostic.
What takes over from the build-time stop is the runtime: the interpreter raises `NameError`, matching
CPython, and that is asserted in `TestUnboundAfterPartialMatchTrapsLikeCPythonInInterpreter`. The
compiled leg does **not** yet trap there — it loads the untouched slot and prints its contents, which
is roadmap Gap R.36 and the reason R.36 is the immediate next cycle rather than a backlog item: while
it stands, the softened diagnostic is unbacked on one of the two paths.

## Agentic rationale

A front-end refusal is the most expensive wrong answer an agent can get: it stops the run before
any value exists, and its shape (`undefined name`) points at a mistake the program does not
contain. This class of refusal was triggered by programs that both backends otherwise handled —
the codegen half already collected assignments across nested blocks, so the whole discrepancy lived
in the analyser, which is why the fix is one file and the compiled leg started working on its own.
`compound_scoping.gy` is in the ledger as a `OracleMatch` case: if a future change reintroduces a
scope where Python has none, the compiled or interpreted leg will stop matching `-1 1 42 123 high 3`
and the matrix reports it.

## Codegen / IR implications

None for the programs concerned: they lowered correctly all along, and the refusal came from
upstream. `finally` clauses are now analysed, so a typo inside one is a check-time error instead of
a runtime surprise — strictly more diagnostics, never fewer. The `match` IR is unchanged; only the
types the analyser holds for the subject between arms.

## Alternatives rejected

- **Keep the child scopes and widen a lookup fallback** (look in discarded sibling scopes when a
  name is missing). Rejected: it keeps the wrong model and makes the answer order-dependent — the
  second binding of a name in a later arm would be found or not depending on traversal.
- **Report a partially-bound name as an error**, as the old code did. Rejected: it is the same
  over-strictness in a different costume, and it refuses programs that run — Python raises at
  runtime only when the unbound path is actually taken, which
  `TestUnboundAfterPartialMatchTrapsOnBothBackends` pins on both engines.
- **Analyse `finally` in a child scope so its assignments stay hidden.** Rejected: `finally` runs on
  every path, so it is the most definite block in the statement.
- **Leave `while` in a child scope because loops "shouldn't leak".** Rejected: `for` already shares
  its scope, the runtime has one vars map, and a counter assigned in a loop body is readable after
  it in every Python program ever written.
