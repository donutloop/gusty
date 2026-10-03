# ADR 0263 — `round(x, ndigits)` rounds the decimal value, and both backends borrow the conversion

Date: 2026-10-03 · Status: Accepted · Roadmap: closes Gap R.69 (L11.6's numeric-truth row); files Gaps
R.129, R.130, R.131 and R.132 ·
Related: ADR 0236 (`round(x)` names the IEEE ties-to-even operation instead of implementing a rule),
ADR 0254 (a function's return word is read off what the body does), ADR 0262 (`ternaryKind`, and the
`scanBareReturns`/`namedNumericLeaves` mechanism this rides on), ADR 0257 (a verdict is a number),
ADR 0166 (a refusal is a divergence, never a pass; exit 2 is the compiler's bug), ADR 0186 (the
three-leg oracle), ADR 0211 + L11.8 (exit 1 is for a program the reference rejects)

## Context

Two lines, three answers, and one of them the wrong exit code:

```gusty
print(round(2.345, 2))   # CPython 2.35 · --interp 2 · --aot exit 1
print(round(3.5, 0))     # CPython 4.0  · --interp 4 · --aot exit 1
```

The evaluator had one line for `round` — `int64(math.RoundToEven(o.fval))` — which never looked at a
second argument, so a digit count was ignored and the answer was the one the *one-argument* form gives.
The compiler had `if len(c.Args) != 1 { return "round expects one argument" }`, which spends exit 1 —
"your program has a compile error" — on a program CPython runs in one line, which is exactly what L11.8
and Gap R.38 exist to stop. And `round()` with no argument was `n.Args[0]` in the evaluator: a Go panic,
`index out of range [0] with length 0`, exit 2, the contract's "the compiler is broken" code, issued for
a typo.

ADR 0236 fixed the one-argument form by refusing to write a rounding rule down: both backends ask for
the named IEEE operation (`math.RoundToEven` / `@llvm.roundeven.f64`). The obvious move for the
digit-count form was to do the same thing one step out — scale by 10ⁿ, ask `roundeven`, unscale — and
the roadmap row said it. **It was measured, and it is wrong** — on 1,077 of the 375,224 fractional pairs
and 74,838 of the 156,048 negative-digit pairs swept below. And it is wrong in the worst available way:
it prints CPython's answer for the row's own definition-of-done example, `round(2.345, 2)` → `2.35`, and
disagrees elsewhere, quietly.

The mechanism is not that ties are subtle. It is that **scaling manufactures ties the value never had**:

| program | the double's exact value | ×100 | CPython | scale · `roundeven` · unscale |
|---|---|---|---|---|
| `round(0.005, 2)` | 0.00500000000000000010408… | `0.5` | `0.01` | `0.0` |
| `round(0.025, 2)` | 0.02500000000000000138777… | `2.5` | `0.03` | `0.02` |
| `round(0.075, 2)` | 0.07499999999999999722444… | `7.5` | `0.07` | `0.08` |
| `round(2.675, 2)` | 2.67499999999999982236… | `267.5` | `2.67` | `2.68` |
| `round(-0.005, 2)` | −0.00500000000000000010408… | `-0.5` | `-0.01` | `-0.0` |
| `round(2.345, 2)` | 2.34500000000000019539… | 234.50000000000003 | `2.35` | `2.35` — right by luck |

Each of the first five is a value whose exact decimal is *above or below* the tie, whose scaled product
is *exactly* on it, and to which the nearest-even rule then hands back the wrong neighbour. The last row
is why the algorithm could have shipped: the roadmap's chosen example happens to be a double that sits
above its tie, so the scaled product lands past it and even-rounding picks the answer CPython picks for
an unrelated reason. The rule is wrong because the thing being rounded is not the value the question is
about.

What CPython rounds is the **exact decimal value of the double**: to `ndigits` places, ties to even, and
it comes back to the nearest double. That is a correctly-rounded double→decimal conversion (David Gay's
dtoa, with the tie broken by comparing against the true value), which is a different operation from
`roundeven` and one that neither backend owns.

## Decision

**One rule, in one function, and both halves of it are conversions somebody else got right.**

`roundToDigits(v float64, n int)` in `pkg/lang/round_digits.go` is the whole rule, and the evaluator and
the compiler's constant fold both reach it. Its non-obvious step is a correctly-rounded
double→decimal→double round trip:

```
interpreter   strconv.FormatFloat(v, 'f', n, 64) → strconv.ParseFloat     (Go's dtoa)
compiled      snprintf("%.*f", n, v)             → strtod                 (the C library's dtoa)
```

Both libraries are correctly rounded, so each backend agrees with CPython — not with each other by
construction, which is the arrangement ADR 0236 warns about. The identity was checked rather than
assumed: **531,272 `(value, ndigits)` pairs** swept against CPython and compared bit for bit — every
`k/1000` and `k/100` in the tested range, 22,500 uniform randoms, 500 raw bit patterns, `ndigits` from
−2 to 10 (375,224 pairs, **zero** differences for either spelling of the conversion), then 156,048 more
with `ndigits` from −400 to 400. The surviving 143 differences are all between |x| = 4.117e18 and 1e300,
all exactly one ULP, are caused by the scale step rather than by the conversion, and are recorded as
their own roadmap row instead of being hidden behind a passing test. For contrast, the roadmap row's own
algorithm — the scale/`roundeven`/unscale rule — differs from CPython on 1,077 of the first sweep and
74,838 of the second (among them every pair at a digit count where the scale overflows to +Inf and the
rule answers NaN).

The four pieces under the round trip:

- **Negative digit counts** divide by 10⁻ⁿ, apply the same conversion with no fractional digits, and
  multiply back. `math.RoundToEven(v/scale)*scale` was tried first — the row's rule again, one scale
  earlier — and differs from CPython on 74,838 of those 156,048 cases where this shape differs on 143:
  the nearest-even rule asked of a binary value that is not the one the question is about, plus a scale
  that overflows to +Inf and returns NaN at the extreme digit counts.
- **`pow10` is the slow way on purpose**: ten multiplied by itself k times, in Go and as a `phi` loop in
  the emitted runtime, instruction for instruction. If one backend called libm `pow` and the other
  squared its way there, the last bits of the *scale* would differ per backend, and a digit-count round
  would disagree across engines for a reason no error message mentions.
- **The clamps are arithmetic facts, not moods.** Every binary double is exactly a decimal with at most
  324 digits after the point (the smallest subnormal is 4.94e-324), so `n ≥ 324` is the identity — and
  the text stays inside the runtime's buffer. And 10³⁰⁹ is past the largest finite double, so at
  `n ≤ −309` the nearest multiple of that scale to any finite value is zero *with the sign kept*, which
  is what CPython answers there too. The sign is taken from the value's own bits, because an `fcmp`
  cannot tell −0.0 from 0.0.
- **NaN and the infinities have no decimal point to move** and are handed back (CPython agrees; they
  also happen to survive the text round trip).

**The answer is the kind the value arrived as.** `round(5, 2)` is the int `5`, `round(True, 2)` is `1`
(ADR 0257: a verdict *is* the number it is made of), `round(5.0, 2)` is the float `5.0`, and
`round(3.5, 0)` is the float `4.0` — not `4`. The digit count is evaluated even when the value is an
integer, because `round(5, 1.5)` is a `TypeError` in the reference and a free pass in a backend that
short-circuits.

**A digit count that is not an integer is a raise, not a refusal.** `round(2.345, 1.5)` is CPython's
`TypeError: 'float' object cannot be interpreted as an integer`, so both engines raise that sentence
through the same catchable door an out-of-range index uses, and `try: … except TypeError:` takes the
branch on both. Refusing to compile it would be exit 1 for a program the reference merely stops on.
Truncating `1.5` to `1` instead would be Gap R.69 wearing a different hat — answering a question nobody
asked — so the compiled constant fold checks the digit count *before* it folds anything.

**The arity sentence is written once** (`roundArityMessage`) and read by both backends: the evaluator
raises it, codegen refuses with it. `round()` is no longer a Go panic in either engine: exit 3 with the
sentence interpreted, exit 1 with the sentence compiled, exit 2 nowhere.

**The fold hands back the constant, not an instruction that recomputes it.** The first version emitted
`%t = fadd double 0.0, <const>` to get a name for the value; that addition is where `round(-0.5, 0)`
lost the sign of its zero and printed `0.0` where CPython prints `-0.0`. A constant is a value; it
travels as itself. The same idiom is sitting in thirteen other places in `codegen.go`, which is why
`print(-0.0)` still prints `0.0` on the compiled leg — that is Gap R.132, measured against the pre-cycle
binary so nobody mistakes it for this cycle's work, and the reason this cycle's probe is written as a
boundary (two lines lose the sign, two keep it) rather than as a list of complaints.

**A function returning `round(v, 2)` returns a double when its body made `v` a double.** `round` joined
`kindPreservingNumericBuiltins` (`abs`, `float`) so ADR 0254/0262's `scanBareReturns` promotes the
return word — with a condition codegen and the scan both apply: only the two-argument form, and only its
**first** argument, because the digit count is an integer whoever wrote it. Walking it would promote a
function because a name in its digit count happens to hold a double. The other half — a parameter whose
kind the module cannot see — is Gap R.129, filed with each engine's number rather than refused, because
retiring `def f(x): return round(x, 2)` would trade a working int-call-site program for an error message.

## Consequences

- `round`'s builtin table in `docs/language.md` stops saying the digit count is unimplemented; the
  paragraph says what the rule is and where each half comes from.
- `pkg/lang/semantic.go` answers a two-argument `round` of a float as `TFlt()`, so the checker's
  annotation and the codegen's word agree.
- `integration/programs/round_ndigits.gy` is the row's own definition of done — `2.35` and `4.0` on all
  three engines — and entered the corpus as a standalone row: `oracle: match`, parity yes. The matrix is
  127 rows, 98 parity-asserted, 0 parity failures, 0 oracle drift.
- `@rt_round_digits` is `internal`, is emitted only when the module calls it, and is called exactly once
  per rounding the compiler cannot do itself; a program that never rounds does not carry it. A unit row
  asserts all three, because a runtime block that is emitted unconditionally is a runtime block nobody
  audits.
- Four rows leave with fresh IDs, each pinned per leg and none of them asserted: Gap R.129 (a digit-count
  round of a value whose kind the module cannot see takes the i32 road), Gap R.130 (the loop variable of
  `for v in [2.345]` has no kind to follow at all — the wall underneath R.129), Gap R.131 (`int()`,
  `float()`, `ord()`, `chr()` still panic on zero arguments, and `int()`/`float()` with no argument are
  programs CPython answers with `0`/`0.0` while the compiler refuses them), and Gap R.132 (a negative zero the
  **compiler** wrote has no sign: `print(-0.0)` prints `0.0` compiled while `print(0.0 * -1)` prints `-0.0`,
  because the emitter materialises a folded constant with `fadd double 0.0, <const>` and IEEE's −0.0 + +0.0 is
  +0.0 — thirteen such sites, the same habit this ADR's fold was written with and no longer uses).

## Agentic rationale

An agent consuming this feature needs three things, and all three are machine-readable:

- **The behaviour, as a verdict.** `gustyc --oracle "$(cat integration/programs/round_ndigits.gy)"`
  returns 0 and the JSON says `match`, on both backends; the same artifact records the toolchain whose
  correctness the claim presupposes (ADR 0186). No prose parsing.
- **The refusal, as a sentence with the missing half in it.** Every refusal here names what is missing —
  the arity, or the digit count's kind — with a stable exit class: 1 for a program CPython also rejects
  (bad arity), 3 for a program CPython runs and stops on at run time (the `TypeError`). Exit 2 fails the
  test suite, including the trap and refusal tables, which is where a partial implementation reaches for
  it.
- **The debt, as data.** The four filed rows are `oracleDebt` ledger entries with the expected stdout (or
  `Missing`) per leg, so a cycle that fixes one makes the harness fail until the row is deleted. The
  sweep that justified the algorithm is reproducible arithmetic, not a claim: its numbers are in
  `docs/roadmap-details.md#gap-r-69` beside the code that implements the rule.

## Alternatives rejected

- **Scale, `roundeven`, unscale** — what the roadmap row proposed, and what the row's definition of
  done cannot distinguish from the shipped rule: `round(2.345, 2)` comes out `2.35` either way, because
  that particular double sits above its tie. Rejected by the sweep: 1,077 differences on the fractional
  pairs, 74,838 on the negative ones, and the mechanism above for why a binary rounding cannot answer a
  question about a decimal point at any level of cleverness about ties. ADR 0236's lesson is that two
  backends implementing one wrong rule looks like agreement; this is that lesson with a green DoD program
  stapled to it.
- **`%.*e` and parsing the exponent** for negative digit counts — one conversion for both branches, but
  it puts string surgery (splitting the mantissa at `e`, re-scaling the exponent) in emitted IR where the
  scale-and-round form puts a multiplication loop. Rejected for legibility of the IR, kept in reserve
  against Gap R.69's own scale step if the far-magnitude class ever matters beyond being recorded.
- **A hand-written decimal round in both backends** (digits in a buffer, schoolbook carry, ties by
  inspection) — the only fully self-contained option, and 200 lines of the most audited code in the tree
  or the least. Borrowing the correctly-rounded conversion each platform already has is what ADR 0236
  does with `roundeven`, one level up.
- **Compiling a `TypeError` digit count to exit 1** — a refusal is a divergence, never a pass (ADR 0166),
  and it would also make `except TypeError` impossible on the compiled leg while working on the other
  two engines.
- **Refusing `round(x, 2)` of a parameter the module cannot see** — honest, and rejected: the line prints
  the right *number* on the interpreter today and a truncated one compiled, so retiring the program
  trades one divergence for the loss of working code. Filed as Gap R.129 with both numbers pinned.
- **`math.RoundToEven(v/scale)*scale` for negative digit counts** — tried, swept, and 74,838 of 156,048
  cases from the reference where the shipped shape takes 143.
- **A `fadd 0.0` to name the folded constant** — loses the sign of a zero the reference keeps.
