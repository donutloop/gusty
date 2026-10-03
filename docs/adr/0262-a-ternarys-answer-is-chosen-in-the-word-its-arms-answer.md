# ADR 0262 — a ternary's answer is chosen in the word its arms answer

Date: 2026-10-03 · Status: Accepted · Roadmap: closes Gap R.102; files Gaps R.127 and R.128 ·
Related: ADR 0254 (a function's return word is read off what the body does), ADR 0174 (the parameter
convention), ADR 0261 (`constantTestArm`: a ternary whose test is a value the source wrote), ADR 0259
(`boolSlotExpr`: ask an element as an expression, not as a literal), ADR 0239 (a fold returns the chosen
candidate's own type), ADR 0166 (a refusal is a divergence, never a pass; exit 2 is the compiler's bug),
ADR 0186 (the three-leg oracle)

## Context

Roadmap Gap R.102 named one missing instruction. `def f(x): x = x + 1.5` / `return x if x > 2 else 0.0`
answers `2.5` in CPython and the interpreter; the compiled leg had been refusing it in words since ADR 0254,
and before that answered a truncated `1` with exit 0. The refusal said the backend had “no number-typed
`select` to choose two doubles with”.

It has had one the whole time. `select i1 %c, double %a, double %b` is legal LLVM, and this emitter already
writes that instruction in four other places (the `min`/`max` folds and the remainder's fixups). What was
missing was not an instruction. It was a predicate willing to say that a ternary's arms are doubles.

`isFloat` — the question the whole compiled backend asks to decide whether an expression is a double — had no
`CondExpr` arm. So a ternary answered “not a float” whatever it held, and `value()`, the i32 lowering, was
handed both arms. Its float case is:

```go
case *FloatLit:
        return fmt.Sprintf("%d", int64(n.Value)), nil
```

which is a perfectly reasonable answer for a context that stores an `i32`, and a silent truncation for every
context that was never told. Measured on the compiled leg, all exit 0, all against CPython:

| program | CPython | compiled, before |
|---|---|---|
| `print(1 if 0 else 2.5)` | `2.5` | `2` |
| `def f(x): return 1.5 if x > 2 else 2.5` → `f(1)` | `2.5` | `2` |
| `def f(x): return 1 if x > 2 else 0.0` → `f(1)` | `0.0` | `0` |
| `print([1.5 if c > 0 else 2.5])` | `[2.5]` | `[2]` |
| `def f(x): x = x + 1.5` / `return x if x > 2 else 0.0` | `2.5` | refused in words |

One `false` from one predicate, five programs. That is the shape of the bug: not the missing `select`, but the
number of places that re-derived the answer from the *shape of the line* instead of asking it once.

## Decision

**One question, in one file, read by every door that needs it.** `pkg/lang/ternary_number.go` owns
`ternaryKind(n *CondExpr)`, and it answers four ways:

| arms | test | the answer's word |
|---|---|---|
| either kind | a value the source wrote (`BoolLit`, `IntLit`, `FloatLit`, `NoneLit`, `StrLit`, an empty/non-empty container, `not` of one) | the kind of the arm that **runs**; the other arm is not emitted |
| both doubles | a run-time fact | `double` — `select i1 %c, double %a, double %b` |
| neither a double | a run-time fact | `i32` — the select this lowering has always emitted, byte for byte |
| exactly one double | a run-time fact | **refused in words**: neither word prints both arms correctly |

Four call sites read it, and none of them re-derives the answer:

* `isFloat` (`codegen.go`) — so the renderer, the arithmetic and the return-word gate all agree with the
  lowering about what the expression is. This is the arm the gap was blind to.
* `floatValue`'s `*CondExpr` arm → `ternaryDouble`: the select. An arm that is not already a double converts,
  because the caller asked for a double and `sitofp` is what the reference's own promotion means there — inside
  `(1 if c else 2.5) * 2` the `1` *is* `1.0`.
* `value`'s `*CondExpr` arm → `ternaryI32`: the i32 select, plus the two refusals.
* `returnedTernary` + the gate at the function header, which refuses a returned ternary whose arms disagree and
  quotes the program's own arms in the message.

### The constant test is answered, not guessed

`print(1 if 1 else 2.5)` and `print(1 if 0 else 2.5)` have different answers in CPython: `1` and `2.5`. A rule
that looked at both arms would have to pick one kind and render the other arm wrongly, and it would be doing
that for a program whose test the source spelled out. So when the test is a value the source wrote, the ternary
takes the kind of the arm that runs and the dead arm is **not emitted**. ADR 0261's `constantTestArm` is the
same helper, asked by the renderer there and by the answer's word here — one list of what “a value the source
wrote” means, two questions about it. Not emitting the dead arm also matches the reference: CPython does not
evaluate the arm it does not take, so `print(2.5 if 1 else boom())` where `boom` divides by zero prints `2.5`.

### The arms that disagree are refused

`1 if c else 0.0` with a run-time `c` has no compile-time answer: a `double` word renders the integer arm `1.0`
where CPython writes `1`, and an `i32` word truncates the double arm — which is the answer in the table above,
the one this ADR is fixing. Both are number-shaped wrong answers, and ADR 0166 calls those the compiler's bug.
The refusal names the missing thing (a value that carries its own kind, roadmap L11.1) and the rewrite that
compiles (`else 0` → `else 0.0`, or take the branch with `if`/`else`). Refusing here retires no program that
answers correctly today: the shape it refuses is the shape that was truncating.

Considered and rejected: *always choose `double`*. `(1 if c else 2.5) * 2` would then answer `2.0` where CPython
answers `2` — the same bug with a decimal point on it, and silently. The pinned rows in
`TestATernaryWhoseArmsDisagreeOnAWordIsRefused` are the reason the refusal is the honest half: CPython's answer
there genuinely depends on which arm runs.

### Two more doors had the same blind spot

Both were found by the table, not by the row, and both were invisible to the two-engine matrix because the
interpreter answered correctly.

* **The return-word gate stopped at a wrapper.** `scanBareReturns` looked through a `*Call`, a negation and a
  ternary's arms, but read only a bare `*Name` on the side of a product — so `return (x if x > 2 else 0.0) * 2`
  named no parameter, was never promoted, and the body ended in an i32 `ret` under a program whose answer is a
  double. It now walks `namedNumericLeaves`, the same walker ADR 0254 added for `return abs(-x)`.
* **The container asked a literal where it should have asked an expression.** `literalNeedsTags` tested
  `isFloatLitExpr`, so a float-valued ternary in a list left the container unmarked: the builder stored the
  `@float_box` handle and tagged it correctly, and the printer — following the container-wide kind — read the
  handle as the number it is, printing `[1]`. `floatSlotExpr` is the sibling of ADR 0259's `boolSlotExpr` for
  exactly this reason, and asks `isFloat` of the element.

## Consequences

* Five answers move to CPython's, on the compiled leg, and the interpreter is unchanged: the four in the table
  above, plus `print([1.5 if c > 0 else 2.5])`.
* The refusal ADR 0254 wrote for Gap R.102 is gone for the shape it was written for. Its two paid test rows
  (`pkg/lang/float_rebound_return_test.go`, `integration/float_rebound_return_test.go`) are **deleted, not
  repinned** — a contract row left behind expecting a refusal passes forever while asserting nothing. The
  method form stays refused: a method is emitted with an `i32` return word whatever its body computes, which is
  a different gate with a different reason.
* `scanBareReturns` widening is checked against the row that keeps it honest: `return x > 2` of a rebound
  parameter still answers with the bool word and is not promoted (`TestTheFloatReturnPromotionIsOnlyForValues
  ThatAreTheDouble`), because that arm asks `isArithmeticOp` first.
* Two divergences this cycle measured are filed rather than quietly left out: **Gap R.127** (text arms print the
  interned index) and **Gap R.128** (container arms put `@.lstN` in an operand position and the assembler exits
  2). Both are the same question asked of a non-numeric arm; both are pinned per leg, with the interpreter's
  correct answer beside the compiled leg's wrong or rejected one.
* The tripwire is `TestTheTernaryRuleIsAskedOnceAndNotTwice`: a `select i1 … i32 …, i32 …` for a program whose
  arms are doubles fails it, and so does an i32 pair that grew a double select it was not for.
* `pkg/lang/ternary_number.go` is where the next arm kind lands. The two refusals it holds are the places a
  tagged value word (L11.1) will delete code.
