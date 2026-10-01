# ADR 0236 — a numeric rule lives in one IEEE operation, not in two implementations that agree

Status: accepted. Closes roadmap **Gap R.50** (`round` ties away from zero where CPython ties to even)
and is the first landed piece of **L11.6** (numeric truth in the compiled backend), which is the last
open item of **Phase 2 / P2.15** (on-disk stdlib modules). Cites: ADR 0161 (one question, one table —
the rule this applies to arithmetic instead of names), ADR 0186 (the CPython oracle leg, which is the
only instrument that could see this), ADR 0233 (a construct is implemented, refused, or absent — never
answer wrong), ADR 0211 (exit-code classes, relevant to the `ndigits` half left open).

## What had been measured

```
print(round(2.5))    CPython 2   --interp 3   --aot 3
print(round(0.5))    CPython 0   --interp 1   --aot 1
print(round(-2.5))   CPython -2  --interp -3  --aot -3
```

The gap's own wording: *"Both backends agree with each other and disagree with Python, which is
exactly the class of defect the two-backend parity matrix can never see."* Nothing to add to that —
this ADR is mostly about why it survived, and what makes the next one harder to survive.

## Why it survived

Not one bug: **four places all saying the same wrong thing.**

1. The evaluator: `int64(math.Round(o.fval))` — Go's `math.Round` is round-half-away-from-zero.
2. The compiled runtime: `call double @llvm.round.f64` — LLVM's `llvm.round` is the same operation.
3. The compiled constant fold: `int64(math.Round(fv))` — so a literal tied one way and a variable the
   same wrong way, and the two paths never had to agree to look consistent.
4. The tests: four of them, two with the rule in prose — `// round(float variable) must round
   half-away in the AOT binary` and `// round(float) rounds half-away-from-zero in both interpreter and
   AOT` — asserting `3`, `-3`, and an IR pin requiring the string `i32 3`.

Point 4 is the lesson. A test that states the rule is not a check of the rule, it is a second
authority for it, and when the authority is wrong the whole build goes green around the wrongness. The
only thing that ever disagreed was CPython, and CPython was not in the room for those four tests: two
of them never invoked it, and neither did parity, which compares the two implementations to each other
by construction.

## The decision

**Name the operation, not the behaviour.** `round` is IEEE `roundTiesToEven`. Both backends now ask
for that operation by name from the facility that provides exactly it:

- evaluator: `math.RoundToEven`
- compiled runtime: `call double @llvm.roundeven.f64`
- compiled fold: `math.RoundToEven`

Each is a standard implementation of one named IEEE operation rather than a hand-written rule, so the
only way they drift is if the host's and the target's IEEE libraries disagree — which is a different
conversation, and a hardware one. "Half away from zero" does not appear anywhere in the code now, in
code or in comment.

**The value expectations come from the oracle, not from us.** `integration/programs/round_ties.gy` is
in the corpus with **no ledger row**, which under this repo's convention means "must print what
CPython prints", checked on both gusty legs. It covers literal ties (the fold path) and variable ties
(the intrinsic path) separately, precisely because those are two paths and fixing one would leave the
other wrong. Matrix: row 104, `oracle: match` on both legs — the ratchet that makes the old answer
fail CI.

**The inverted pins are the deliverable too.** The four assertions were rewritten to CPython's values,
and each keeps a note saying what it used to assert and why that is the interesting part. Two of them
sit in a test (`TestExecFloatFloorModAbsEdgeCases`) that already carried a note about a *previous*
expectation that had described the emitted `frem` instead of the language. That test file is where
wrong-answer pins accumulate, so it is where the habit of writing them down matters most.

## Alternatives rejected

- **Leave the rule but make both backends call one shared Go helper**: rejected — codegen emits
  textual IR; a "shared helper" on that side is still an intrinsic choice, so the sharing has to be
  the *naming of the operation*, not a call site.
- **Implement ties-to-even as `(floor(x+0.5))` with an even correction**: rejected. It is the kind of
  expression that is right on the four cases someone thinks of and wrong on `nextafter`, and it
  re-implements in four lines of IR what `llvm.roundeven.f64` is defined by the IEEE to do.
- **Add a rounding-mode flag to `round`**: rejected — gusty is Python-like by contract; the rule is the
  language's, not the user's.
- **Fix `round(x, ndigits)` in the same commit**: rejected under one-commit-per-feature; it is a third
  answer (below) in a different dimension (typing and arity, not the tie rule), and it has its own row.

## Newly measured while writing the test, deliberately owed — Gap R.69

| program | `--interp` | `--aot` | CPython |
| --- | --- | --- | --- |
| `round(3.5, 0)` | `4` | exit 1 `round expects one argument` | `4.0` |
| `round(2.345, 2)` | `2` | exit 1 (same refusal) | `2.35` |

Three answers again, and the compiled one is the loudest-but-wrong kind: exit **1** is the contract's
"your program has a compile error" class (ADR 0211), issued for a program CPython runs — the exact
shape L11.8 and Gap R.38 are about (a refusal claiming something false about the program). The
interpreter is quieter and no better: it ignores `ndigits` entirely and returns an integer.

## Codegen/IR notes

- `llvm.roundeven.f64` needs no explicit `declare` in textual IR (intrinsics are recognised by name),
  as was already true of `llvm.round.f64` in the same emitter — verified by `opt -passes=verify` in
  the existing IR tests, which is what would have caught a wrong intrinsic *name*.
- The constant path leaves no call at all. That asymmetry is why the fold gets its own assertion: a
  test that only looks for the right call would pass on a module whose fold was still wrong.
