# 276. A number handed to a function keeps the kind its argument had

Date: 2026-10-05
Status: Accepted
Roadmap: L11.6 (numeric truth in the compiled backend), L11.1 (the tagged value word)
Closes: Gap P.1's remaining half (an untyped parameter that receives a double), Gap R.154, Gap R.157, Gap R.158
Files: `pkg/lang/paircall.go`, `pkg/lang/heapargs.go`, `pkg/lang/codegen.go`, `pkg/lang/float_arg_test.go`, `integration/float_argument_test.go`, `integration/conformance_cases.go`, `integration/programs/probe_float_numeric.gy`

## The decision

The pair-across-call door ADR 0273 opened is widened from *"the ordinary road refuses this argument"* to
**"the ordinary road refuses this argument, or answers it with a double it cannot carry"**. A parameter whose
argument can end on a double arrives as two `i32`s — payload and tag — and the callee's arithmetic lifts it
through the one door every other tagged value uses. Three further rules come with it:

- **the parameters the pair does not carry are named from their call sites** (`intParams`): a parameter every
  call site hands a provable integer — a literal, or a name whose every binding is one, its default included —
  is a number the body may read as a plain word, which is what lets the shared arithmetic door name a kind for
  *both* operands of `w * h`;
- **a default is an argument** the caller did not have to write, so a parameter only a default describes is
  opened by the same question;
- **a body emitted under a convention that owns its return word owns its parameter words with it**: where
  ADR 0274's float return or ADR 0174's string index runs, the scan marks nothing at all, and the function
  keeps the answer or refusal it had.

And one door in the other direction: `return g(x)` of a callee whose own answer is a pair hands the pair on
beside the caller's `ret`, and where the caller's convention is a `double`, the pair is lifted by
`@rt_lift_num` — the same helper ADR 0267's bindings and ADR 0268's number positions lift through.

## Context — what the gate never asked

ADR 0273's scan marked a parameter pair-carrying when the ordinary numeric road would have **refused** the
argument: an index of a container the program built at run time. It asked nothing about an argument that
carries a *double*, so the case that silently truncated never reached the door:

```
def twice(v):
    return v * 2

print(twice(2.5))   # CPython 5.0 · --interp 5.0 · --aot 4, exit 0
```

`twice(2.5)` printed `4` at exit 0. That is Gap P.1's remaining half, filed in 2026-08-03 and re-measured
after ADR 0274, which paid the `x /= 2` half of the same row and left these two lines in the ledger: the probe
`integration/programs/probe_float_numeric.gy` carried `dbl(0.1)` pinned at `0` and `bump(1.5)` pinned at `2`,
against CPython's `0.2` and `2.5`. A float-state variable handed to a call (Gap R.158) and an accumulator whose
state changes twice (Gap R.157) were honest refusals; a pair-carrying parameter beside an ordinary one (Gap
R.154) was an honest refusal too.

The reason one `double` parameter cannot express `twice` is CPython's own rule: the answer's kind follows the
argument. `twice(2)` is `4`, `twice(2.5)` is `5.0`. A single calling convention has to pick, and both picks are
wrong half the time. The pair is the only shape that carries the question across.

## The scan, asked with one set of names

`pairCallSpecs` answers four questions per function, all of them pure functions of the AST, none of them
dependent on which `define` the module emits first:

| question | answered by | what it decides |
|---|---|---|
| would the ordinary road refuse this argument? | `exprNeedsWord` | the parameter takes the pair |
| could it answer a double instead? | `exprCarriesDouble` | the parameter takes the pair |
| are the arguments this position ever receives integers? | `argsAreInts` / `knownIntParams` | the body may read it as one word |
| can the body read every pair parameter back? | `pairGate.expr` / `pairGate.cond` / `pairBodyAnswers` | the marking survives at all |

The last is settled with the answer direction in **rounds**, because `return g(x)` is only served once `g`'s
own answer is decided, and `g`'s answer is only decided once `g`'s body is served. Both questions only ever
*remove* — a parameter, a callee — so the marking shrinks monotonically and a bounded number of rounds reaches
a fixpoint. A name's own bindings are walked with a `seen` guard: `x = x + 1.5` reads the name it writes, and
without the guard the scan recursed until the stack gave out — a compiler crash on a two-line program, which
is the class ADR 0166 reserves exit 2 for.

## Condition and value are different positions

`pairGate.expr` (a value position) serves `+ - * and or` and **no comparison**. `pairGate.cond` (the `if`/`while`
head, a comprehension's iterable) also serves `< <= > >= == != and or`, because a condition asks an operand's
*truth* and the truth doors ADR 0269 and ADR 0275 already answer a `(payload, tag)`.

That distinction is a measured one, not a stylistic one. Serving a comparison in a value position refused
`def cmpf(v): return v > 1.5` — a program the compiled leg answered `True`/`False` for — because the comparison
road has one word for its operand and no door for a pair. Refusing to answer is only ever the right direction
when the program did not already answer; where it did, the parameter stays on the road it had. The value
position is filed as Gap R.161's comparison half.

## The roads the scan leaves alone

`pairReturnRoadOwns` asks, of each `return`, the same leaves ADR 0274's promotion reads
(`namedNumericLeaves`), and closes the whole function if any of them is bound to a double (ADR 0274's
`define double`), or to anything that is not a number at all — `s = str(v)` / `return s` being ADR 0174's
`@str_tab` index.

This rule paid for itself the day it was written. Without it `def fmt(v): s = str(v); return s` printed `0`
where it had printed an honest refusal: the pair road handed the callee a float box handle and the body handed
back the interned index, which is Gap P.1's wrong-number class arriving through a door this commit opened. A
function whose answer is a `double` word has the same exemption, and the pin is textual:
`define double @gy_f(double %p0)` must keep exactly that spelling, with no tag word beside it, because the
double already carries the value.

## What the compiled module looks like

```
define i32 @gy_twice(i32 %p0, i32 %q0) {   ; payload, tag
  …bindTaggedVar(v, %p0, %q0)…
  %t = call i32 @rt_num_arith(i32 %v, i32 %tag, i32 %two, i32 0, i32 1)   ; the one tagged door
  store i32 %tagout, i32* @gy_twice.anst
  ret i32 %payload
}
@gy_twice.anst = internal global i32 0
```

An integer-only function in the same module keeps `define i32 @gy_plus1(i32 %p0)`: the arity is the scan's
decision, and a function nobody hands a double gets no door at all — which is also what keeps the
`function_calls` and `fibonacci` benchmarks byte-identical.

## Alternatives rejected

- **A `double` parameter for a function called with a float.** Cannot express `twice`: the answer's kind
  follows the argument, and one convention must pick int or float. This is what the row had been since 2026-08-03.
- **Widening `arithOperandPair`'s `*Name` fallback to answer tag `0` for any parameter** (Gap R.154's first
  candidate). Cheap, and it silently assumes a kind for a value the compiler cannot see. `intParams` gets the
  same answer from evidence: the call sites are the proof, and where they are not readable the parameter stays
  closed.
- **Deciding the exemption at emit time** (`g.floatFuncs[name]` when the call is lowered). Rejected for the
  reason ADR 0273 gave for the whole scan: the `define` and every `call` must agree, and a function defined
  after its first call site makes that a question about emission order.
- **Serving comparisons everywhere and teaching the comparison road to lift.** Correct where it applies, and it
  reopens a shape that already answered. Deferred to Gap R.161 rather than shipped half.

## Measured, before and after

| program | CPython | before, `--aot` | after, both engines |
|---|---|---|---|
| `print(twice(2.5))` | `5.0` | `4` (exit 0) | `5.0` |
| `print(twice(2))` | `4` | `4` | `4` |
| `print(twice(True))` | `2` | `2` | `2` |
| `def show(w): return w` / `show(2.5)` | `2.5` | `2` (exit 0) | `2.5` |
| `print(area(2.5, 2))` | `5.0` | `4` | `5.0` |
| `def bump(v): return v + 1` / `bump(1.5)` | `2.5` | `2` | `2.5` |
| `def scale(v, k=1.5): return v * k` / `scale(3)` (default only) | `4.5` | `3` (exit 0) | `4.5` |
| `ys = [1, 2.5]` / `twice(ys[1])` | `5.0` | `4` | `5.0` |
| `x = 8` / `x = 2.5` / `twice(x)` | `5.0` | exit 1 (Gap R.158) | `5.0` |
| `t = 0` / `t += i / 2` in a loop | `6.5` | exit 1 (Gap R.157) | `6.5` |
| `def shift(a, b=100): return a + b` | `108`, `10` | exit 1 (Gap R.154) | `108`, `10` |
| `f(1, xs[0] / 2)` | `3.0` | exit 1 | `3.0` |
| `def g(y): return y * 2` under `return g(x)` | `5.0` | `2` (exit 0) | `5.0` |
| `print(5.0 // 2)` through a parameter | `2.0` | `2` (exit 0) | `2` — **owed**, Gap R.162 |
| `def outer(x): return twice(x)` / `outer(2.5)` | `5.0` | `4` (exit 0) | `4` — **owed**, Gap R.161 |
| `def fmt(v): return str(v)` | `42`-style text | exit 1 | exit 1 (unchanged) |

`integration/programs/probe_float_numeric.gy` printed `-4 / 0.5 / 4.0 / 0 / 2` compiled against CPython's
`-4 / 0.5 / 4.0 / 0.2 / 2.5`; it prints the reference's six lines on both legs now, so its debt row is deleted
and the program is promoted to `conformanceStandalone()` — the corpus, not this file, is what keeps it honest.

## What stays owed

- **Gap R.161** — a double forwarded through *another function's* parameter: the scan reads a name's bindings
  and an enclosing parameter has none, so `def outer(x): return twice(x)` truncates at exit 0. Needs the call
  graph, not another predicate.
- **Gap R.162** — `//` and `%` in a body over a pair-carrying parameter answer the integer domain:
  `floorit(5.0)` is `2` where CPython answers `2.0`.
- **Gap R.163** — `return str(42)` from a function prints the interned index (`0`), pre-existing and unrelated
  to floats, found by this cycle's whole-corpus sweep.
- **Gap R.156** (a float-state variable returned from a function) and **Gap R.159/R.160** stay honest refusals,
  pinned as such.
