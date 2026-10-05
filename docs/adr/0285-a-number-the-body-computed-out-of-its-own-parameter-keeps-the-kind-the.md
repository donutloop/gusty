# ADR 0285 — a number the body computed out of its own parameter keeps the kind the argument arrived with

Date: 2026-10-06
Status: Accepted
Roadmap: closes the one-binding family of **Gap R.169**; files **Gap R.170**.
Depends on: ADR 0280 (a name bound by a *call* is read as a pair — this is the same question for a name
bound by *arithmetic*), ADR 0276/0277 (the argument and forwarding halves), ADR 0265/0249 (the tagged
numeric door, and record-then-refuse), ADR 0215 (trap wording is a contract), ADR 0166 (exit 2 is the
compiler's bug), Gap R.38 (a refusal that describes a program the reader cannot find is its own defect).

## The measure

`python3` 3.12.3 is the oracle. With the body binding its answer to a name first:

| program | CPython | `--interp` | `--aot` before | `--aot` after |
| --- | --- | --- | --- | --- |
| `y = x + 1` / `return y` / `f(0.1)` | `1.1` | `1.1` | **`1` at exit 0** | `1.1` |
| `y = x * 2` / `return y` / `f(0.1)` | `0.2` | `0.2` | **`0`** | `0.2` |
| `y = x - 1` / `return y` / `f(0.1)` | `-0.9` | `-0.9` | **`-1`** | `-0.9` |
| `y = (v - 1) * 2` / `return y` / `f(2.5)` | `3.0` | `3.0` | **`3`** | `3.0` |
| `def f(x=2.5): y = x * 2` / `f()` | `5.0` | `5.0` | **`5`** | `5.0` |
| the same bodies called with an int (`f(2)`, `f(3)`, `f(10)`) | `3`, `6`, `11` | ✓ | ✓ | ✓ |
| `y = d(x)` / `return y` (ADR 0280's row) | `5.0` | `5.0` | `5.0` | `5.0` |

Four shapes fixed that printed a **truncated integer at exit 0**, and no working answer moved: the int
side of every body keeps its exact answer, and a sweep against the pre-cycle binary over 162 corpus
programs showed no number or wording change anywhere else.

Three shapes stay **refused** rather than answered — `y = x` / `return y`, a binding under a condition
whose sibling arm returns an int, and two bindings deep (`y = x + 1` / `z = y * 2` / `return z`). They are
in the ledger as still owed, and each now refuses in words about the program the reader is holding.

## The diagnosis

The scan asks a body "what does your answer need to travel in?". For `return x + 1` the leaves are visible
in the expression, the answer direction opens the pair road, and the body is emitted double-returning:

```llvm
@gy_f.anst = internal global i32 0          ; the answer's tag word exists
```

For `y = x + 1` / `return y` the same question reaches the leaf `y`, a plain local. `exprNumberish` looked
`y` up in the binding table and — in that function's own comment — *"a parameter of an enclosing function is
bound by no assignment and so is not here"*, because a parameter is written by the **caller**. The answer
came back "not a number", `pairReturnRoadOwns` let the ordinary return road claim the body, and the emit
was:

```llvm
define i32 @gy_f(i32 %p0) {                 ; one word in, one word out, no tag
```

…the double the tagged door had computed for `x + 1` was truncated at the `ret`. Four different digits,
one mechanism, all of them looking like answers.

The same predicate was already asked and answered correctly two positions away — ADR 0276 for an argument,
ADR 0277 for a forwarded name, ADR 0280 for a name bound by a **call**. The binding-by-arithmetic case was
the missing sibling.

## The decision

**Name a function's own parameters to the numberish question.** `pairReturnRoadOwns` asks
`s.exprNumberish(v, pairParamSeen(fd))`, where `pairParamSeen` fills `seen` with the function's parameter
names. `seen` is the fixed point's "in progress: the fixed point says nothing worse than *not proven*"
set, so marking a name there cannot invent a number — it can only stop the predicate from answering "this
name has no spelling that writes it", which is the wrong answer for a value the caller supplied.

Scoped deliberately narrow, because this is a program-wide predicate and cycles 0280/0281 both measured
what a global widening costs: the set is built from **the one function being asked**, not a module-wide
pool, so a body whose `x` means something else in another function keeps its own answer.

**Decline, don't guess.** When the returned name's value reaches a parameter through the body's bindings
(`bindsNameToPairArithmeticAnswer`, following `scanRebinds` two hops), `pairReturnRoadOwns` returns `false`
— the ordinary road does not own this return — and the pair road takes the body and either answers it or
refuses it. Never a substitute value, never a `0`: ADR 0249's rule, and the reason this row produced four
wrong digits in the first place.

## The refusals were wrong too, and that is a separate defect

Two sentences were false about the programs they described:

* A name bound straight from a parameter (`y = x` / `return y`) was refused with *"the left operand … holds
  the answer of arithmetic over a slot the program built at run time"* — a story about a container. There
  is no container in that file. Added `taggedOriginParam` (**"a parameter the call site handed a double"**)
  and `taggedOriginParamArith` (**"arithmetic over a parameter the caller supplied"**), chosen from the
  operand the arithmetic actually read, followed through the body's bindings.
* A first draft of the parameter sentence added *"another arm of this function returns a plain integer"* —
  true of `if x > 1: return 100 / return x`, false of `y = x / return y`, which has no other arm at all.
  Removed: a refusal the reader cannot check against their own source is Gap R.38's defect, and this
  project has already been burned once by a refusal that blamed a slot where there was none.

Both are pinned by a test that fails if the banned phrase returns, and a companion test keeps the
*original* slot sentence alive for the shape it was written for, so the new origins cannot swallow the
diagnosis that was already right.

## Alternatives rejected

* **Answer all five shapes.** Rejected on the record: the three still-refused cases need the return to
  carry a tag beside its value — L11.1's tagged value word — and the shapes that remain refuse honestly in
  the meantime. Shipping a guessed word for `y = x` is how this row was written in the first place.
* **Widen `exprNumberish`'s `*Name` answer to "a name is a number unless proven otherwise".** Rejected:
  it inverts the predicate's purpose. It exists to refuse a container handle reaching arithmetic; defaulting
  to "number" re-opens `print(x + 1)` of a list, which is Gap P.1's wrong number one operator further in.
* **Track parameter kinds in the module-wide `paired` fixed point.** Rejected: `s.paired` is keyed by bare
  name, so `f(x)` and `g(x)` would share one verdict — the class of order-dependent bug ADR 0280 pinned with
  its either-order test.
* **Refuse at the checker.** Rejected: `print(f(0.1))` is a program the reference answers, and ADR 0166
  reserves exit 1's "your program has a compile error" for programs that have one.
* **Copy ADR 0280's `pairBoundCallNames` wholesale for arithmetic.** Tried first and it did not fire: that
  set decides the *answer* direction, while the truncation happens earlier, where the *return road* is
  chosen. Both sites now consult the same shape question, which is why the helpers are shared.

## Agentic rationale

Four of this row's five defects were exit 0 with a plausible digit — the class an agent reads as success.
After the change: exit 0 means the reference's answer including through a bound name, exit 1 means a
missing tagged return word and says **which** value it is stuck on and **what that value came from**
("a parameter the call site handed a double", not a container hunt), and exit 3 remains a program that
raised. The `--json` shape is unchanged. The refusal text is now something a harness can pattern-match to
decide "rewrite with `return x + 0.0`" versus "this is fine".

## Codegen / IR notes

The distinction is visible at module level and is what the unit rows assert rather than trusting prose:
an answered body emits `@gy_f.anst = internal global i32 0` and a `define double @gy_f(...)`, the
truncated body emitted `define i32 @gy_f(i32 %p0)` with no tag global. The regression test's failure mode
is a digit, not a diagnostic, so it cannot be satisfied by a better message.

## Tests

* `pkg/lang/bound_pair_answer_test.go` — `TestANumberBoundFromAParameterKeepsItsKind` (10 rows × both
  engines, each pinned against `python3`, including the int side of every body and ADR 0280's call-bound
  row as a non-regression), `TestABoundAnswerStillRefusedIsTheHonestHalf` (the still-owed shape, skipping
  to a "promote it" note the day it compiles), `TestTheBoundAnswerRefusalNamesWhatTheArithmeticWasOver`
  (fails on the banned phrases), `TestASlotTheProgramBuiltStillNamesItself`.
* `integration/bound_pair_answer_test.go` — the CLI table plus the two honest refusals (exit 1, never 0,
  never 2, wording checked).
* `integration/programs/probe_a_number_bound_from_a_parameter.gy` — eight lines, three legs, one answer,
  promoted into `conformanceStandalone()`. Matrix 153 → 154 rows, 116 → 117 parity, 101 → 102 `match`,
  0 fail, 0 drift.
* Filed from the same sweep, not from speculation: **Gap R.170** — `s = str(v); return s` prints `0`
  compiled at exit 0 where ADR 0281 fixed only the direct `return str(v)`. Verified pre-existing on the
  pre-cycle binary, so it is a fresh ID rather than an instance buried in this record.
