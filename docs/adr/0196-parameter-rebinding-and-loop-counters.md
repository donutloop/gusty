# ADR 0196: A parameter is a local variable, and a range loop owns its counter

**Status:** Accepted
**Date:** Phase 7 — "Correctness as a product" (found while closing Gap R)
**Decided by:** the compiled backend's variable model
**Related:** ADR 0181 (precise roots: the loop-body alloca that changed address each
iteration), ADR 0166 (a shape must not leave the toolchain as an LLVM rejection), ADR
0176/0167 (async, whose probe `probe_async_eager` is what made a parameter look at all),
ADR 0195 (the await discipline round that went looking), roadmap Gap R.3 / R.3b / R.3c,
L11.6 (floats are half-implemented)

## Context

Roadmap Gap R.3 was filed as "a compiled loop whose condition variable the body
reassigns never ends". Reproduced, it was one symptom of something much wider. This is
the whole compiled backend's behaviour for a parameter that is assigned:

```gusty
def bump(n):
    n = n + 1
    return n
print(bump(0))
```

The interpreter answers `1`, CPython answers `1`, and the compiled binary answers **`0`**.
No error, no warning, no exit code — a plausible number, which is the worst thing a
compiler can produce. Measured the same way across the surface:

| shape | interpreter / CPython | compiled (before) |
|-------|----------------------|-------------------|
| `def bump(n): n = n + 1; return n` | `1` | `0` |
| `def twice(n): n = n*2; n = n+1; return n` | `7` | `3` |
| `def acc(n): while n > 0: total += n; n = n - 1` | `10` | **never returns** |
| `def count(n): for n in range(3): print(n); return n` | `0 1 2` then `2` | `9 9 9 9` then `9` |
| a method `def bumped(self, n): n = n + 1; return n` | `2` | `1` |
| a nested `def inner(m): m = m * 3; return m` | `6` | `2` |
| `def show(xs): xs = [9, 9]; print(xs)` | `[9, 9]` | `[9, 9]` ✓ |
| `def greet(s): s = "world"; return s` | `world` | `world` ✓ |

**Parameters were read-only in the compiled backend**, except where a kind map happened
to route the read elsewhere: floats, strings and containers worked because their reads
consult `floatVars` / `strVals` / `listVars`, and plain values — the common case — read
the incoming argument register. Functions, methods and nested defs were all affected.
Every Python-shaped program that decrements the number it was given, clamps an argument,
or reuses a parameter as a loop variable was wrong or non-terminating.

The IR said it plainly:

```
define i32 @C_bump(i32 %self, i32 %p1, i32 %p2) {
  %_n = alloca i32                       ; the assignment's slot
  %t5 = add i32 %p1, 1
  store i32 %t5, i32* %_n                ; stored…
  ret i32 %p1                            ; …and ignored
}
```

Looking into *why* the loop hung exposed a second, independent defect in the same
lowering. `for i in range(3)` drove its iteration **through the user's loop variable**,
so after the loop `i` answered the bound, and an assignment to the loop variable inside
the body moved the iteration:

| shape | CPython / interpreter | compiled (before) |
|-------|----------------------|-------------------|
| `for i in range(3): print(i)` then `print(i)` | `0 1 2` then `2` | `0 1 2` then **`3`** |
| `for i in range(3): i = i * 100; print(i)` then `print(i)` | `0 100 200` then `200` | `0 100 101` |

Neither was caught by three years of corpus, because the corpus never had a program that
assigned to a parameter or read a loop variable after its loop.

## Decision

**(a) A parameter that the body rebinds is a local variable with an initialiser.** At the
top of the entry block, the incoming register is copied into a named slot and the slot
becomes authoritative for the whole body:

```
%_n = alloca i32
store i32 %p0, i32* %_n        ; the entry copy — this is the fix
```

This is the copy any SSA-form compiler performs for a variable assigned after its
definition, and the *entry* placement is the part with a reason attached: a read
textually before the first assignment (`if cond(): n = 5` … `print(n)`) must still read
initialized storage. Allocating lazily at the first assignment — which is what the old
code effectively did — leaves that path reading a slot that no store has ever touched.
Reads are gated on `paramSlot`, not on a fresh `alloca` having appeared, so the slot's
existence never depends on which statement codegen happened to reach first.

Parameters the body never assigns keep reading the argument register: the fix costs
nothing for the overwhelming majority of parameters (`TestUnreboundParamKeepsItsRegister`
pins that).

**(b) The scan is generous, because the two errors are not symmetric.** A parameter is
given a slot if *anything* in the body's own scope binds its name: `x = …`, `x += …`,
a `for` whose variable is the parameter, a `with … as` target, a `case y:` capture, a
destructuring target, a comprehension's loop variable. A copy-in that turns out to be
unnecessary costs one `alloca` and one `store`; a missed one is a wrong answer or a
hang. The scan does **not** descend into a nested `def`/`lambda` body — those own their
names — and each nested function gets its own copy from its own prologue, which is why
methods and nested defs are fixed by the same three lines.

**(c) Only scalars are copied in, and the float question is asked of the store's own
predicate.** `reboundParams(fd, g.isFloat)` takes the *same* `(*irGen).isFloat` the
assignment path will consult before choosing `store double` vs `store i32`. If the two
questions could disagree, the entry would allocate an `i32` slot that a later
`store double` writes through — an invalid module. A parameter rebound to a float, a
string or a container keeps its existing (already correct) kind-map path; nothing here
changes it.

**(d) A `for … in range(...)` loop owns its counter.** The counter is a private
`%_ctrN` slot; the loop variable is bound from it at the top of the body — the point at
which Python binds it — and nowhere else. Three behaviours fall out of that one change,
all of them Python's and none of them previously obtainable: the variable answers the
**last value bound** after the loop, not the bound; writing to the loop variable in the
body does not move the iteration; and a loop whose condition also reads a rebound
parameter terminates. The container-loop lowering already had this shape (an `idxVar`
apart from the loop variable); the range path had not.

**(e) A float-returning function's parameters are registered as allocated.** That
prologue already copied every parameter into a `double` slot, but did not record it, so
the first assignment to a parameter emitted a second `alloca` of the same name and `llc`
refused the module with "multiple definition of local value". Registering the slot makes
those programs compile.

## What this does not fix, and where it is pinned

`def addf(x): x = x + 1.5; return x` still answers `1` interpreted `2.5`. Making it work
means deciding that the *argument* type is a float, and today a function's calling
convention is read off the **shape of its return expression** — `return x` says nothing,
so the parameters arrive as `i32` and `1.0` is truncated on the way in. I tried the
obvious fix (treat a parameter rebound to a float as a float variable) and it produced a
different module that `llc` rejected for the same reason one layer down: the type is
decided twice, in two places, from different evidence. That is the tagged value word —
roadmap **L11.6** — not a patch here. It is pinned as `programs/probe_float_param_rebind`
(interpreter `2.5 / 3.0`, compiled leg refused), so L11.6 pays it into parity and the
ledger complains about the stale row instead of nobody noticing.

## Alternatives rejected

- **Copy the parameter in lazily, at its first assignment.** That is what the old code
  did by accident. A path that reads the name before any assignment on that path loads
  an uninitialized slot — a bug with no signature, in a language with no undefined-value
  trap.
- **Refuse functions whose parameters are rebound** (the narrow, honest refusal this
  project sometimes chooses). It would reject the most ordinary Python-shaped code in
  existence — every accumulator, every clamp, every loop that reuses a parameter name —
  and it would be a refusal the interpreter does not enforce, which is the divergence
  ADR 0195 exists to eliminate. Refuse when there is no correct lowering; here there is
  one, and it is four instructions.
- **Copy in every parameter unconditionally.** Correct, but it costs an `alloca`, a
  `store` and a GC root per parameter in every function, including the `def add(a, b)`
  majority that never assigns. The scan exists to pay only where it is needed.
- **Keep the range loop's counter in the loop variable and patch the two symptoms.**
  Sharing the storage *is* the bug: it makes the variable observable as the bound, and
  makes the body's writes part of the iteration. Any patch on top of sharing would have
  to re-simulate Python's binding rule at every read, which is the separate counter
  wearing a disguise.
- **Fix the float case in the same round.** It reaches into the calling convention
  (L11.6), and its half-fix turned a wrong answer into an `llc` rejection. A wrong answer
  that is already recorded and a rejection I would have introduced are not a trade.

## Consequences

- `programs/param_rebind.gy` joins the corpus as a *parity and oracle-match* row: 19
  lines of the shapes above, and the interpreter, the compiled backend and CPython print
  it identically. `integration/params_test.go` adds the same class shape-by-shape with a
  **timeout on the compiled leg** — a hanging program produces no output and no error,
  so an unbounded leg is a test suite that stops reporting rather than failing.
- The `%_paramN` slots (the GC roots for incoming handles) stay; the named slot is added
  beside them, not instead of them. Rooting an `i32` that holds a raw int costs the
  collector one `skipped`, which `--gc-stats` already reports.
- Generated code for range loops grows by one `alloca`, one `store` per iteration, and
  moves the loop-variable store into the body. `--bench-suite` gates the difference.
- Two IR-structure tests now hold the line where a wrong answer used to live:
  `TestReboundParamGetsAnEntrySlot` (one alloca, copy precedes the body's arithmetic) and
  `TestRangeLoopHasItsOwnCounter` (the counter is a `_ctr` slot; the user's variable is
  bound from the counter, and nothing increments it). Both were verified to fail against
  the previous codegen — a test that cannot fail is decoration.
