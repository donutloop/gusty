# ADR 0286 — a rendering the body bound to a name is a string return, and the digits road is asked what it was handed

Date: 2026-07-05
Status: Accepted
Roadmap: closes **Gap R.170**; files **Gap R.171**.
Depends on: ADR 0281 (`return str(v)` makes a function string-returning — this is the same verdict one
statement earlier), ADR 0258 (`str()`/`repr()` are one renderer, and "a missing rendering must not become
the number underneath the value"), ADR 0285 (the same two-hop question asked of the number road), ADR 0174
(a text argument is refused at the call site), ADR 0166 (exit 2 is the compiler's bug), Gap R.6 (a program
that takes a builtin's name means its own function).

## The measure

`python3` 3.12.3 is the oracle. The body renders a value and returns it — once directly, once through a
name:

| program | CPython | `--interp` | `--aot` before | `--aot` after |
| --- | --- | --- | --- | --- |
| `return str(42)` / `print(g())` (ADR 0281) | `42` | `42` | `42` | `42` |
| `s = str(v)` / `return s` / `print(f(3))` | `3` | `3` | **`0` at exit 0** | `3` |
| same with `f("hi")` | `hi` | `hi` | **`0`** | `hi` |
| `r = repr(v)` / `return r` / `print(f(3))` | `3` | `3` | **`0`** | `3` |
| `s = "x" + str(v)` / `return s` | `x3` | `x3` | **`2`** | `x3` |
| `a = str(v)` / `b = a` / `return b` | `7` | `7` | **`0`** | `7` |
| `s = str(v)` / `return s` / `print(f(3).upper())` | `3` | `3` | **refused** | `3` |
| `def str(x): return x + 7` (Gap R.6) | `49` | `49` | `49` | `49` |

Five wrong numbers at exit 0 became the reference's answer. A 164-file sweep against the pre-cycle binary
moved **nothing** except the new probe, and ADR 0281's own row keeps answering `42` — the direct form was
already right and had to stay right.

## The diagnosis

Two defects, one visible and one underneath it.

**The visible one is ADR 0281's scope.** `strReturningFuncs` walks a body's `return` expressions and asks
"is this a text?". `return str(42)` says yes; `return s` names a local, the question is asked of the wrong
syntax, the function stays number-returning, and `print(f(3))` hands `printf` an index into `@str_tab` with
`%d`. The rendering door had already run — one statement earlier — and its result was thrown away by the
verdict that follows. This is exactly the shape ADR 0285 found on the number side: the value's kind is
written by an **assignment**, and a scan that reads only the returned expression cannot see it.

**The underneath one is why the wrong number was possible at all.** `str()` of an expression the compiler
could not fold fell to `rt_str_of_int`, guarded only by "not a text and not a float". `rt_str_of_int`
writes the **decimal digits of the word it is handed**. That is right for an int and a fabrication for a
handle — which is why `str([1, 2])` and `str(None)` used to answer `0` (ADR 0258's own words), and why the
same class sat one hop from every parameter.

## The decision

**Ask the name.** `isStrExprIn`'s `*Name` case, having failed the string-parameter test, now consults the
body's own assignment table — `bodyBindings(fd)`, `scanRebinds` over `fd.Body`, memoised per `*FuncDef` —
and asks the same question of each value that wrote the name. Recursion is bounded by the `out[name]`
fixed point that already guards the outer loop. Two consequences fall out correctly: `s = "x" + str(v)` is
a text because the `*BinOp` arm already reads `isConcat`, and `b = a` is a text because `a`'s binding is.

The closure had to be declared before its body (`var isStrExprIn func(...)`; a `:=`-assigned closure cannot
reach its own name in its initializer) — the small mechanical cost of asking a question of itself.

**Ask the digits road what it was handed.** `renderPair`'s digits arm and the `str()` fold road both now
require `strArgIsNumberish(e)`: literals, arithmetic over numbers, a call whose callee's every `return` is
a number, and a name whose **latest** binding is a number. Containers, texts, `None`, and index slots
answer `false`.

**Read the latest binding, not "was it ever".** Narrowing the digits road is the kind of change that
silently converts answers into refusals, and it did: `x = "abc"` / `x = 5` / `print(str(x))` — CPython's
`5` — began refusing, because my first version asked whether the name had *ever* been bound to a
non-number. The renderer's own fold arm reads what the name holds **now**, so the gate reads the last
binding too. `TestTheDigitsRoadFollowsTheLatestBinding` is the row that would have caught it; it is kept as
a permanent test, not a debugging note.

## What narrowing deliberately does *not* touch

A **parameter** keeps the digits road (`return true` in the `*Name` arm). My first cut returned `false`
for every parameter, on the reasoning that a parameter's kind is unknown — and it broke four programs that
had always worked, including `x = str(v); print(x)` printing `3`. The reasoning was wrong because the guard
already exists one position earlier: **ADR 0174 refuses a text argument at the call site** ("strings are not
supported as function arguments in the AOT backend yet"), so by the time `str(v)` is lowered, `v` cannot
hold a text, a container, or anything else a word cannot carry. Deleting the road did not close a hole; it
took away an answer. The ladder rule is that a working answer must not become a refusal, and here it is the
reason the rule is written down.

## Gap R.38 applied to my own instrument: `builtinShadowed`

The call arm first read `if !isName || g.builtinShadowed(nm.Value) { return false }` and every
`str(g())` refused. `builtinShadowed` answers *"did the program take this builtin's name?"* — and answers
**yes** for an ordinary callee, which is precisely what a callee is. It is the right predicate for Gap R.6
(`def str(x)` means the program's own function) and the wrong one for "is this a program-defined callee",
which is `g.fds[name]`. The comment sits in the code, not only here, because the two questions differ by
one word in English and the wrong one reads plausible.

## Filed, not absorbed: Gap R.171

`def f(v): return str(v)` / `print(f(None))` prints `0` on the compiled leg — and so does the plain
`print(v)` body, **byte-identically on the pre-cycle binary**. `NoneLit` lowers to the bare word `0`, so a
parameter cannot distinguish the void from the integer zero and no rendering question can be asked of it.
That is L11.1's tagged value word arriving by call rather than by assignment; it is its own row, not a
clause of this one, and it is why the digits road's gate is written as a question rather than as an
assumption.

## Alternatives rejected

* **Answer all shapes the sweep surfaced.** The `str(None)`-as-parameter row cannot be answered without a
  tag beside the value, and inventing one is how this family produced five plausible digits.
* **Refuse a parameter at the digits road.** Shipped once, caught by the suite, reverted: see "What
  narrowing deliberately does not touch".
* **Gate on "was this name ever a number".** Rejected after it refused CPython's `5`; it asks a different
  question than the one the renderer answers.
* **Add a fourth binding table.** Rejected: `scanRebinds` is what ADR 0274's return convention and ADR
  0285's number half already read, and the fold arm already reads the latest binding — a new record would
  be a second answer to a question the front end has already settled.
* **Fix it in `print`'s dispatch instead.** Rejected: `print` asks `callReturnsStr`, which reads this
  table; the table was the missing half, and fixing a consumer would leave every other reader of the verdict
  with the old answer (the choke-point lesson of Gap R.150).

## Agentic rationale

The five fixed rows are the class an agent reads as success — exit 0, a plausible digit. After this
change, exit 0 means the reference's answer including through a bound name; exit 1 means the renderer
cannot name the operand and says which one; exit 3 remains a program that raised. `--json` is unchanged.
The `strReturningFuncs` table is the single answer both engines' callers consult, so an agent that asks
"does this function give me text?" once gets the same verdict the printer used.

## Tests

* `pkg/lang/bound_str_return_test.go` — `TestARenderingBoundToANameIsAStringReturn` (12 rows × both
  engines, each pinned against `python3`, including ADR 0281's direct form and Gap R.6's override as
  non-regressions), `TestTheDigitsRoadFollowsTheLatestBinding` (the regression I introduced and reverted),
  `TestTheRenderingRefusalIsNotAWrongNumber` and `TestTheDigitsRoadStillRefusesWhatItCannotSee` (a handle's
  digits may never be printed).
* `integration/bound_str_return_test.go` — the CLI table over both engines with a live `python3`
  cross-check per row and an explicit exit-2 ban, plus the refusal rows.
* `integration/programs/probe_a_rendering_bound_to_a_name.gy` — five lines, three legs, one answer,
  registered in `conformanceStandalone()`. Matrix 154 → 155 rows, 117 → 118 parity, 102 → 103 `match`,
  0 fail, 0 drift.
