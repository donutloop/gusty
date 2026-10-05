# ADR 0282 — a text has no double to widen from: printf-style `%` refuses instead of answering `0.0`

Date: 2026-10-06
Status: Accepted
Roadmap: L11.2 (text and rendering), closing **Gap R.165**.
Depends on: ADR 0278 (`%` over a pair, and the four divide-by-zero sentences), ADR 0265/0249 (the
tagged numeric door; an unlift-able operand is refused, never substituted), ADR 0166 (exit 2 is the
compiler's bug), ADR 0258 (one renderer), Gap R.82 (the text-left `%` refusal this completes).

## The measure

`python3` 3.12.3 is the oracle; `--interp` and `--aot` are the two engines. All values typed at the CLI.

| program | CPython | `--interp` | `--aot` before | `--aot` after |
| --- | --- | --- | --- | --- |
| `print("%.2f" % 3.5)` | `3.50` | TypeError | **`0.0` at exit 0** | exit 1, names the format string |
| `print("%.1f" % 3.14159)` | `3.1` | TypeError | **`0.0` at exit 0** | exit 1 |
| `print("%f" % 3.5)` | `3.000000` | TypeError | **`0.0` at exit 0** | exit 1 |
| `s = "%.2f" % 3.5` / `print(s)` | `3.50` | TypeError | **`0.0`** | exit 1 |
| `def f(v): return "%.2f" % v` / `print(f(3.5))` | `3.50` | TypeError | refusal | exit 1 |
| `print("%d items" % 3)`, `"%s!" % "hi"`, `"%x" % 255` | formatted | TypeError | exit 1 (Gap R.82) | exit 1, same words |
| `print(7 % 3)`, `-7 % 3` | `1`, `2` | same | same | same |
| `print(7.5 % 2)`, `-7.5 % 2`, `7.5 % 2.5` | `1.5`, `0.5`, `0.0` | same | same | same |
| `def f(v): return v % 2` → `f(7.5)`, `f(7)` | `1.5`, `1` | same | same | same |
| `1.0 == "a"`, `"a" == 1.0` | `False` | `False` | `0` | `0` |

The first five rows are the defect. Note what they share with the last six: **the same operator**. A
fix that banned `%` would turn the last six into failures, which is why half of each new test file is
the numeric control table rather than the refusal table.

## The diagnosis

`"%.2f" % 3.5` reached the **double** road, because the right operand is a float literal. The emitted
module says exactly what happened:

```llvm
  %t2 = call i32 @rt_str_intern2(i8* @.str1, i8* @.str2)   ; the format string is interned
  %t1 = sitofp i32 %t2 to double                            ; …and its @str_tab index widened
  %t3 = fadd double 0.0, 3.5e+00
  %t6 = frem double %t1, %t3                                ; the "remainder" of an index
```

`floatValue` ends in a fall-through that widens whatever `value()` produced, and for a text the value
is its **index** in `@str_tab`. Index `0` widened, `frem`'d against 3.5, and printed `0.0` — at exit
0, with a traceback-worthy program looking like a successful run.

The sibling shapes were already refused. `"%d items" % 3`, `"%s!" % "hi"` and `"%x" % 255` never
reach the double road: no operand is a float, so the i32 road sees `isStringExpr(l)` and answers Gap
R.82's sentence. A `%f`-style conversion in the literal was the one spelling that changed the road —
and the pair door added in ADR 0278 already guards `%` for exactly this reason (`op == 5` sits in the
guarded list with `+` and `*`), so the hole was in the *float* road, one road away from the guard.

## The decision

**A text has no double to widen from.** At `floatValue`'s fall-through, a text operand is refused the
way every other unlift-able operand is: record the cause, return `""`, and let the caller refuse at
exit 1 (the record-then-refuse convention ADR 0249 established for `fdiv double , %t1`).

Two scoping rules keep it from becoming a ban:

* **A comparison is not a numeric use.** `==`, `!=`, `<`, `in`, `is` … answer a verdict for a pair of
  unlike kinds — CPython answers `1.0 == "a"` with `False`, and this backend answers `0` — and the
  equality road lowers **both** operands through the same lift. The first draft refused there too and
  three pinned rows in `integration/container_element_test.go` failed (`float_eq_str`,
  `str_eq_float`, …), a working answer turned into a refusal. The guard asks the operator
  (`comparisonOpForTextGuard`) before it asks the kind.
* **The record carries its own cause.** `floatUnlowerable` used to hold a fragment that
  `floatOperandRefusal` / `floatLoweringRefusal` wrapped in the *slot* sentence — "a slot whose kind
  only the object knows … needs the run-time tag". For a literal format string that diagnosis is
  simply false: no slot, no object, no tag, and a reader would go hunting for a container that is not
  in the program. A record may now be a complete sentence (marked `floatLowerCompleteMarker`), which
  the two callers quote instead of re-diagnosing.

The refusal the user sees:

```
codegen: a string is a text, which has no double to widen from: printf-style formatting — the
reference's `"%.2f" % 3.5` answers `3.50`, and this backend builds neither that nor a number from a
format string (roadmap L11.2, Gap R.165)
```

## Alternatives rejected

* **Answer the formatting** (`"%.2f" % 3.5` → `3.50`). Rejected for this cycle: it is a real feature —
  conversion types, precision, width, mapping and tuple right-operands — and building it half-way
  would produce numbers from format strings in the shapes it missed, which is the class of bug this
  ADR removes. It stays open as the *positive* half of Gap R.165's row; what had to die today was the
  `0.0` at exit 0. The natural home is ADR 0258's renderer (the format string selects a form, the
  right operand is rendered, the result is interned), and the acceptance row is already written:
  `print("%.2f" % 3.5)` prints `3.50` on three legs.
* **Fold it at the checker** so only literal format strings with literal arguments are answered.
  Rejected: it answers the probe and leaves every computed format string in the same `0.0` hole,
  moving the divergence rather than removing it.
* **Ban `%` when the left operand is a text** (no operator test). Rejected by measurement: the three
  equality rows above, which answer today.
* **Widen a text to `NaN` or `0.0` and let the raise come later.** Rejected: ADR 0249/0166 — a
  substitute operand is how `fdiv double , %t1` reached `llc`, and a substituted digit is how
  `print(xs[0] / 2)` printed `0.0` with exit 0.
* **Reuse the numeric door** (`@rt_num_arith` op 5), which already raises CPython's
  `unsupported operand type(s) for %: 'str' and 'float'` for a slot-held text. Rejected as the wrong
  *class* here: that raise is right when the kind is only known at run time; here the compiler can see
  the text, and a compile-time refusal names the missing feature rather than pretending the program
  might have run.

## Agentic rationale

An agent reading exit codes learns, before this change, that exit 0 means "the number CPython prints"
— and `0.0` is the most believable number in the language. After it, the three states are distinct
again: **exit 0** prints the reference's answer, **exit 1** names the missing feature with the
reference's own answer quoted in the message (`answers \`3.50\``), and **exit 3** is a program that
raised. The refusal also names itself as *printf-style formatting*, which is a searchable feature
name, and the machine path is unchanged (`--json` reports the same `"error"` with the same string), so
an agent can tell "not implemented" from "wrong" without reading the IR.

## Codegen / IR notes

* `floatValue`'s fall-through (`sitofp i32 …, to double`) is now unreachable for a text; the module
  that emitted `sitofp i32 %t2 to double` + `frem double` for a format string is emitted nowhere.
* `floatOperandRefusal` / `floatLoweringRefusal` gained a pass-through for a marked record; unmarked
  records keep the slot sentence verbatim, so every existing refusal's wording is byte-identical
  (checked by the whole-corpus sweep: wording included, not just numbers).

## Tests

* `pkg/lang/percent_format_test.go` — `TestTextLeftPercentRefusesRatherThanAnsweringANumber`
  (twelve refusal rows: `%f`, `%e`, `%.1f`, `%.2f`, `%%` alone, several conversions, inside a `return`,
  bound to a name), `TestRemainderStillAnswersIsTheOtherHalfOfGapR165` (twelve numeric/control rows ×
  both engines against pinned CPython values), and `TestThePercentRefusalNamesItsOwnCause`, which
  fails if the message stops naming printf, the reference's `3.50`, and **fails if it starts claiming
  the slot story** ("run-time tag", "only the object knows").
* `integration/percent_format_test.go` — the same families through the CLI, exit classes pinned (1 for
  refusals, 2 forbidden), each control row checked against `python3` so the pinned expectation itself
  cannot drift.
* `integration/programs/probe_remainder_and_percent.gy` — thirteen lines, three legs, one answer,
  registered in `conformanceStandalone()`. The matrix went 150 → 151 rows, 114 → 115 parity, 99 → 100
  oracle `match`, 0 fail, 0 drift, and the 168-file sweep against the pre-cycle binary moved **only**
  the `%` probe: `0.0` → the honest refusal.
