# ADR 0264 — `floor` and `ceil` answer a whole number, `sqrt` answers a float, and each raise is the reference's own sentence

Date: 2026-10-03 · Status: Accepted · Roadmap: closes Gap R.51 and Gap P.2 (L11.6's numeric-truth pair,
with Gap R.134 closed alongside); files Gaps R.133, R.135 and R.136 ·
Related: ADR 0236 (`round(x)` names the IEEE ties-to-even operation instead of implementing a rule),
ADR 0263 (`round(x, ndigits)` rounds the decimal value — the same wrong-kind-of-answer family),
ADR 0243 (an element is carried as (payload, widen), which is how a whole number sits beside a float),
ADR 0254 (a function's return word is read off what the body does), ADR 0262 (`ternaryKind`, `isFloat`
and the read-what-the-operand-answers mechanism this rides on), ADR 0228 (a built-in trap is a typed
raise, so `except` can name it), ADR 0166 (a refusal is a divergence, never a pass; exit 2 is the
compiler's own bug), ADR 0211 + L11.8 (exit 1 belongs to a program the reference rejects), ADR 0186
(the three-leg oracle)

## Context

`predeclared.go` has advertised `ceil`, `floor` and `sqrt` since the checker existed, and the checker has
always typed a call to them. Behind that one table were three languages. Measured on the binary at the
commit before this one (`--interp` is the evaluator, `-aot` the compiled leg, both forced explicitly —
`--file` alone defaults to the interpreter, which is how a first pass at this measurement produced a
table of the same engine twice):

| program | CPython (`math.*`) | `--interp` | `-aot` |
|---|---|---|---|
| `print(floor(3.7))` | `3` (an `int`) | `NameError: name 'floor' is not defined`, **exit 3** | `3.0` |
| `print(ceil(-0.5))` | `0` | `NameError`, exit 3 | `-0.0` |
| `print(floor(7))` | `7` | `NameError`, exit 3 | `7.0` |
| `print(sqrt(-1))` | `ValueError: math domain error`, exit 1 of the reference's own kind | `NameError`, exit 3 | `nan`, **exit 0** |
| `print(sqrt(-1.0))` | `ValueError: math domain error` | `NameError`, exit 3 | `nan`, exit 0 |
| `print(floor("a"))` | `TypeError: must be real number, not str` | `NameError`, exit 3 | `0.0`, exit 0 |
| `print(sqrt(9))` | `3.0` | `NameError`, exit 3 | `3.0` ✓ |

Three shapes of wrong, and none of them was visible to the tests. `TestExecFloorCeil` pinned
`2.0\n-3.0\n3.0\n-2.0`, with a comment explaining that floor and ceil "return a float (documented:
'the largest double <=')" and pointing at a roadmap row for the divergence — and the roadmap row
(Gap P.2, in `docs/roadmap-details.md`) said the same thing back: *"docs say 'the largest double <=', so
this is a deliberate-but-questionable choice"*. The docs said it, the compiler did it, the test asserted
it, and the reference was not in the room — a closed loop of three voices agreeing with each other and with
nothing outside. That is the defect this ADR exists to remove, and the reason both legs of a "documented
divergence" have to be checked against CPython before the word "documented" is allowed to mean "settled".

The two halves of the defect are separate:

* **The answer's kind.** `math.floor` and `math.ceil` return an `int`. A backend that answers `3.0` has
  shown the program a number it never wrote — the same mistake ADR 0236 found in `round(x)` and ADR 0263
  found in `round(x, ndigits)`: the question was asked in the wrong word. `sqrt` is the opposite case:
  `math.sqrt(9)` is `3.0`, a float with a fractional part most of the time, and lowering it as anything
  else is wrong.
* **The raise, or its absence.** A text argument, a `None`, a container, a negative under `sqrt`, a NaN
  or an infinity under `floor`: the reference has a raise for every one of these. The compiled leg had a
  *value* for every one of them — `0.0`, `nan` — and exited 0. A value where the reference has a raise is
  worse than a crash, because the program continues and prints.

## Decision

**One rule per name, in one file, read by both backends.** `pkg/lang/math_names.go` holds the arity
sentence, the argument-kind check, the domain check and the answer for all three names, so the evaluator
(`jit.go`'s `evalCall`) and codegen (`g.call`, `floatValue`, `isFloat`) cannot drift apart — which is what
they had done.

**1. `floor` and `ceil` answer a whole number; `sqrt` answers a float.** The checker says so too
(`semantic.go` returns `TInt()` for the pair and `TFlt()` for `sqrt`), which is the third voice that used
to disagree. Concretely: `isFloat("floor(…)")` is now false, and was true; the answer is computed by
`@llvm.floor.f64` / `@llvm.ceil.f64` and carried in the int word, and where the *context* wants a double
(`floor(2.7) + 1.5`) the i32 is lifted back with `sitofp` — the same (payload, widen) step ADR 0243 uses
for an element, and for the same reason: widening is a fact about the word, truncation would be a lie
about the value.

**2. The argument has to be a real number, and the sentence is CPython's.** `must be real number, not str`
— unquoted, exactly as the reference writes it, which is *not* how the neighbouring `TypeError` family in
this language reads (`'str' object cannot be interpreted as an integer`, ADR 0263, quotes the kind). Both
spellings are kept because a program's `except` matches on the text a user reads, and the two sentences
are CPython's own pair. Containers, `None`, text and functions each name themselves (`list`, `dict`,
`set`, `NoneType`, `str`, `function`). It is a **raise** on both engines — exit 3, catchable by
`except TypeError:` — never a compile-time refusal: the reference runs and stops on these programs, so
exit 1 would be ADR 0211's misclassed class.

**3. `sqrt` of a negative raises `ValueError: math domain error`, at run time, on both roads.** This
replaces `nan`. The guard is one `fcmp olt …, 0.0` and the shared raise door, so the domain error is a
branch a program can catch (`except ValueError:` takes it on both engines, verified).

**4. A whole number that does not fit, and a value that has no whole number, are the reference's own two
raises.** `floor(nan)` → `ValueError: cannot convert float NaN to integer`; `floor(inf)` →
`OverflowError: cannot convert float infinity to integer`. That needed `OverflowError` in the canonical
exception table (`exceptions.go`, tag 9) — the class CPython itself uses for `int(inf)`, so a program's
`except OverflowError:` means the same thing on all three engines.

**5. Beyond the compiled int word the guard raises, and the disagreement is filed rather than averaged.**
`fptosi double X to i32` is not a wrong number outside the i32 range, it is *poison*: LLVM is free to hand
back anything, and `-2147483648` is what it usually hands back. So `floorCeilValue` compares first
(`fcmp oge 2147483648.0`, `fcmp olt -2147483649.0`, `fcmp uno` for the NaN) and raises an `OverflowError`
that names the row owning the decision, roadmap L12.12 ("integers are integers, or the language says so").
The evaluator's ints are `int64`, so it answers `floor(3000000000.0)` = `3000000000`, which is CPython's
answer. The two legs therefore differ, and the difference is a ledger row with a pin per leg
(`programs/probe_whole_number_beyond_the_int_word`, Gap R.133) plus a test that pins both sides
(`TestTheWholeNumberBuiltinsBeyondTheCompiledIntWordAreFiledNotSilent`) — not a `--aot` expectation
rewritten to `3000000000` that the compiler cannot deliver, and not a silent wrap added to Gap R.64's
collection.

**6. Arity is one sentence per name**, `floor expects 1 argument, 2 given`, written once and read by both
backends: exit 1 compiled (a call the reference would also reject — CPython's `math.floor(1, 2)` raises
`TypeError: floor expected 1 argument, got 2`), exit 3 interpreted, and exit 2 nowhere. `floor()` was in
the evaluator's `n.Args[0]` family, which is Gap R.131's Go-panic class; it is a reported arity message
now.

**7. Found by running the sweep rather than by reading the row: a folded non-finite constant could not be
emitted at all.** `sqrt(float("inf"))` folds, and `floatConst` wrote the answer as Go's own spelling with
this emitter's exponent suffix glued on — `inf.0e+00` — which LLVM 20's parser has no token for. `llc`
stopped with `expected value token`: **exit 2**, the contract's "the compiler is broken" code, spent on a
program CPython prints. The same spelling was reachable without any of this work, from the simplest
program that binds one of the two values (`x = float("inf")`, measured on the pre-change binary). The fix
is central, in `floatConst`: the two values with no decimal spelling are written as their IEEE bit
patterns (`0x7FF0000000000000`, `0xFFF0000000000000`, `0x7FF8000000000000`), which is what a double
literal is made of anyway and what the parser does accept. `inf.0e+00` and `nan.0e+00` joined the shared
`forbiddenIR` blacklist, so every codegen test in the package now refuses to ship a module containing
them. Gap R.134, closed alongside Gap R.51 by this ADR.

**8. Two more names on the same walk, filed open.** Walking the predeclared table and calling every name
in it — the sweep that found `floor`/`ceil`/`sqrt` dead — found ten more that are dead too
(`map`, `filter`, `isinstance`, `hash`, `id`, `getattr`, `callable`, `pow`, `divmod`, `fabs`), the sharpest
being `print(pow(2, 3))`: CPython says `8`, this toolchain says `NameError` interpreted (exit 3) and
`unsupported call "pow"` compiled (exit 1). Filed as Gap R.136 with a probe and per-leg pins. And `1e18`
does not lex — the exponent marker is not in the number lexer, so both engines stop at
`parse error at 1:8: expected ")"` where CPython prints `1e+18`; filed as Gap R.135. Both were found while
choosing test values for `sqrt`, which is the argument for testing with the numbers the feature is for.

## Consequences

* `print(floor(3.7))` prints `2`-style answers now: `floor(2.7)` → `2`, `ceil(-2.2)` → `-2`,
  `floor(2.7) + 1.5` → `3.5`, `[floor(2.7), ceil(2.2)]` → `[2, 3]`, `f"{floor(2.7)}"` → `2`,
  `sqrt(9)` → `3.0`. Every one of those is asserted on both engines against
  `from math import floor, ceil, sqrt` — the same source bytes, one line apart — in
  `integration/math_names_test.go`, and the corpus files `whole_number_builtins.gy` and
  `non_finite_float_constant.gy` are run against that twin too. The ledger carries both as
  `not_applicable` for the spelling reason only, which is what forces the twin test to exist rather than
  letting the row quietly stop being checked.
* `docs/language.md` never described the *kind* of the answer, only the value ("floors the quotient",
  "the largest double <=" is this repo's own phrase, and it lived in Gap P.2's record and in the test
  comment, not in the language doc). It now says what the answer is: a whole number for `floor`/`ceil`, a
  float for `sqrt`, and which raise each bad argument earns.
* Gap P.2 — "numeric builtins Python types differently" — is closed by this commit. Its three named
  defects are the floored float `%` (fixed by ADR 0216), the `round` tie rule (ADR 0236) and
  `floor`/`ceil` returning a float (this one), and all three were re-measured against CPython on both
  engines before the row was allowed to leave the queue: `-3.5 % 2.0` → `0.5`, `round(2.5)` → `2`,
  `round(3.5)` → `4`, `floor(2.7)` → `2`, `ceil(2.2)` → `3`. L11.6 stays open: `/=` → float, a float
  through an untyped parameter and typed stdlib constants are still owed.
* `TestExecFloorCeil`'s expectations were rewritten, and the old ones are quoted in its comment. A test
  that asserts a divergence is a record of a decision; rewriting it silently loses the record.
* The `int` word is now load-bearing in a new place: `floorCeilValue` is the second site (with L12.12's
  overflow rows) where the 32-bit compiled `int` decides what the language can answer. That is one more
  reason L12.12 is the row the loop should take, and why the guard's message names it instead of
  inventing its own complaint.
* `OverflowError` is catchable. Any earlier program that could not name it can now handle the domain the
  whole-number question can land in.

## Agentic rationale

An agent compiling `floor`/`ceil`/`sqrt` through this toolchain needs three things, and each is now a
machine-readable fact rather than a behaviour to probe:

* **The kind is in the checker.** `--emit-ast`/the type annotations say `floor`/`ceil` → int, `sqrt` →
  float, so a planner does not have to guess an answer's word from an example, and cannot be misled by a
  codegen that prints something else.
* **The failure is classified.** Every raise here is exit 3 with the reference's sentence and is catchable
  by class name; nothing in the feature's surface reaches exit 2 (the CLI test fails the file on exit 2 in
  any row, including the trap and arity tables), and the only exit-1 rows are the ones CPython also
  rejects (arity). That is the exit-code contract (ADR 0006, 0211) being usable as a control signal.
* **The limit is in the ledger, not in a comment.** `--verify`/the conformance matrix carries
  `probe_whole_number_beyond_the_int_word` with one pin per leg, `probe_predeclared_name_not_callable`
  and `probe_float_literal_with_exponent` as `debt` rows with a `ref` each, so an agent can discover that
  `floor(3e9)` is a compiled-engine wall owned by L12.12 without recompiling twice and diffing stdout.
  The `forbiddenIR` rows are the same idea one layer down: a module that would die in `llc` is caught by a
  test in the package, not by a build that fails at link time.

## Alternatives rejected

* **Answer `floor`/`ceil` as a double and let the printer hide it.** `3.0` for `floor(3.7)` is what shipped,
  and it leaks everywhere the answer goes (each line measured on the pre-change compiled leg, with
  CPython beside it): `str(floor(2.7))` → `2.0` not `2`, `f"{floor(2.7)}"` → `2.0`,
  `[floor(2.7), ceil(2.2)]` → `[2.0, 3.0]` not `[2, 3]`, `floor(2.7) * 3` → `6.0` not `6`. The arithmetic
  that looked like evidence for the float answer was luck, exactly as in ADR 0263: `floor(2.7) + 1.5`
  printed `3.5` on the old backend too, because `2.0 + 1.5` is `3.5` for the same reason the value is
  `3.5` when the `2` is an `int`. By the time the printer sees the value, the kind is gone, so none of this
  is fixable downstream.
* **Clamp, or wrap, past the int word.** Wrapping is what `fptosi` does to poison and is Gap R.64's
  existing complaint about this backend — a number that becomes a different number, quietly, at the
  address of a builtin. Clamping to `2147483647` is worse: it is a plausible wrong answer, the kind no
  test notices. Raising the reference's own class, catchably, and filing the row is the only option here
  that tells the truth.
* **Make the interpreter raise `OverflowError` too, to "keep the engines in sync".** The evaluator can
  answer the number, and CPython answers it; making a working program fail to match the reference in order
  to match the other leg is agreement bought with two wrongs. It is filed as a divergence instead, whose
  resolution is L12.12's, not mine.
* **Refuse `sqrt(-1)` at compile time.** The pre-change compiler answered `nan` for it; the alternative
  that looks tempting is a diagnostic, which spends exit 1 on a program the reference *runs and raises
  on*, and makes `try: sqrt(n) except ValueError:` unwritable for a constant. It is a run-time raise.
* **Give `sqrt` its own copy of the argument check, or reuse the `'str' object cannot be interpreted as an
  integer` message.** Two copies of a rule drift — that is the defect being closed. One message is right
  only if it is the message the reference prints, and CPython prints two different sentences for the two
  families, so the file keeps both and says why.
* **Write the non-finite constants as `fdiv double 1.0, 0.0` / `fdiv double 0.0, 0.0`.** Works, and is what
  this file used to recommend for NaN in ADR 0263's runtime; rejected here because it makes the value
  depend on a fold and gives a reader no way to see which special value was meant, while the hex pattern is
  the same 64 bits the assembler would write and needs no fold to be the number.
* **Take `floor`/`ceil`/`sqrt` out of `predeclared.go` instead of implementing them.** Honest, and it is
  the right move for the ten names in Gap R.136; wrong here, because the three are the language's own
  numeric surface (docs/language.md documents them, the checker types them, the compiled backend had a
  lowering for all three) and the row asked for the reference's answer, not for less language.
