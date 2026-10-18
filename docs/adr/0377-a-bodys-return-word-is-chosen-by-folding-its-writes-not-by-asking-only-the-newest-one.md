# A body's return word is chosen by folding its writes, not by asking only the newest one

**Roadmap:** Gap R.341 (closed here) · ADR 0373 (the last-write rule this keeps) · ADR 0375 (whose writes
the table holds; the cycle that measured this row) · ADR 0273 (a scan and an emission must hold one
opinion) · ADR 0166 (a wrong value at exit 0 is this compiler's bug) · ADR 0254 (what a function returns is
what its body does)

## Status

Accepted. Landed 2026-10-19.

## Context

`returnedFloatRebindings` decides, before a single instruction is written, which names of a body
**end up holding doubles** — the names whose bare `return` therefore needs the `define double` convention.
It asked that question of one expression per name: the **last** write. ADR 0373 chose that deliberately,
against the earlier "was it ever a float" scan, because `u = 2.5` / `u = 3` / `return u` must answer `3`
and not `3.0`.

Asking only the newest write is right when the newest write **states** a kind. It cannot answer when the
newest write **reads the name itself**:

```
def loopAcc():
    t = 0.0
    for i in [1, 2, 3]:
        t = t + i      # the last write of t — and isFloat(t + i) asks about t, whose kind is what we are deciding
    return t
```

`isFloat(t + i)` consults the body's float table for `t` and finds nothing (the scan runs before
emission, so the table has not been filled), and finds an `int` loop variable on the other side. The name
was therefore not among the float bindings, the body was emitted `define i32`, and the return read a
`double` slot through an `i32` word:

```
%_t = alloca double
store double %t10, double* %_t
%_t.ld4 = load i32, i32* %_t        # the double, read as its low word
ret i32 %_t.ld4                      # print(loopAcc()) → 0
```

`0` at the exit code of success, where the reference prints `6.0`. The identical loop written at module
level answers `6.0` — `@main` has no return word to choose, so nothing asks the question wrongly. That
control is what localised the defect to the scan rather than to the arithmetic, and it is why ADR 0375's
cycle filed this as its own row (Gap R.341) instead of absorbing it: the float *table* was correct, the
*pre-pass that reads it* was not.

## Decision

**The scan folds the body's writes instead of asking only the newest one.** For each name the return reads,
walk its bindings in source order, asking `isFloat` of each write against the table the earlier writes
already produced — including, for a write that reads its own name, the kind the writes before it gave that
name. The last write still decides everything it can decide on its own:

| body | fold | return word |
|---|---|---|
| `t = 0.0` … `t = t + i` … `return t` | `true`, then `t + i` asked with `t` a double → `true` | `define double` → `6.0` |
| `u = 2.5` / `u = 3` / `return u` | `true`, then `3` → `false` | `define i32` → `3` (ADR 0373 kept) |
| `t = 0` … `t = t + i` … `return t` | `false`, then `t + i` with `t` an int → `false` | `define i32` → `6` |
| `t = 0.0` … `t = t + i` … `t = 3` / `return t` | `true`, `true`, then `3` → `false` | `define i32` → `3` |

The rule is inherited-kind, in other words, which is what the running value actually is: an accumulator
takes the kind of its seed and the kinds it adds, and ADR 0254's principle — what a function returns is
determined by what its body does — is satisfied by walking the body rather than by reading its last line.

The fixed-point rounds and the `known` snapshot are kept exactly as they were: the fold replaces *which
write is asked*, not the determinism rule (ADR 0273) that says a round must not see another round's
half-written answer or depend on map iteration order.

## Alternatives rejected

- **Seed the scan from "any write was ever a float".** One line, and it reintroduces Gap R.338: `u = 2.5`
  / `u = 3` / `return u` would be promoted and print `3.0`. The distinction that matters is not *whether* a
  name was ever a double but whether its newest spelling **states** a kind or **inherits** one, which is
  exactly what the fold encodes and a set-membership scan cannot.
- **Special-case `t = t + <loop var>` in the scan.** It names one operator, one operand position and one
  construct. Accumulators appear as `+=`, as `t = i + t`, as `t = t * f` and inside `while`; each would be
  its own special case, and the fold covers all of them because it asks the same predicate the emission
  asks.
- **Decide the return word during emission, when the assignment road has already recorded the writes.**
  The `define` header, the parameter words and every **call site emitted before the body** are chosen from
  this scan (ADR 0273's reason for the pre-pass at all). A later decision would give the caller and the
  callee different conventions — the failure mode ADR 0260 and Gap R.259 exist to keep out.
- **Refuse the program instead.** The shape has a well-defined answer that the backend is perfectly
  capable of emitting once it chooses the right word; refusing would replace an exit-0 wrong value with an
  exit-1 wrong *silence* about a program CPython runs, and the row's own control (module level) answers
  `6.0`.

## Consequences

- `programs`-level corpus: the accumulator pair is now an **answer** in `floatFramePrograms` — the body
  form and the module-level control side by side, plus the two rules that must not move (an int
  accumulator stays an int; a float accumulator finally rebound to an int answers the int). The filed
  rows that pinned the wrong `0` were deleted, in both packages, as their own text required.
- The `for`-driven half of Gap R.341 is `DONE`; its sibling **Gap R.342** (a nested `def` storing through
  its enclosing frame's alloca, `llc` rejecting the module at exit 2) is untouched and still pinned, in
  `pkg/lang` and in `integration`.
- Any future change to what a name "ends up as" must keep all four fold rows green; the last of them is the
  ADR 0373 guard inside this ADR's own table, so the two rules cannot be silently traded for one another.
- The record is **7932** sources (three of these four sources had never been asked before and were
  recorded from the reference with the compiled leg cross-checked).
