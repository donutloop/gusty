# ADR 0337: a slice step is asked what it holds — zero is the program's ValueError, and a run that has to be killed is the compiler's bug

Status: accepted (2026-10-14)
Amends: [ADR 0336](0336-a-slice-bound-is-asked-what-it-is-a-position-the-absence-of-one-or-the-reference-s-typeerror.md) (the bound door: it asks what a bound *is*, and had nothing to say about what it *holds*), [ADR 0335](0335-a-slice-hands-back-a-copy-of-the-bases-slots-tags-included-and-the-copy-is-a-list-the-records-know.md) (the copy this row refuses to build)
Extends: [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md) (exit 2 is the compiler's own code — a killed run wearing it is the worst thing this toolchain can do to a program), [ADR 0214](0214-a-trap-the-program-cannot-name-is-not-handlable.md) (the trap must be the program's, so its `except ValueError:` can take it), [ADR 0273](0273-a-gate-is-pure-no-trial-emission-no-counters-advanced.md) (the gate touches no counter and emits nothing it will not use), [ADR 0229](0229-a-string-is-an-index-into-a-table-the-runtime-can-add-to.md) (the text road, whose stepped half still cannot answer — filed as Gap R.243)
Supersedes: nothing
Related: roadmap L11.4 (the position/key rule), Gap R.236 (closes), Gap R.237 / R.238 / R.239 / R.240 / R.241 (the family ADR 0336 filed), **Gap R.242 / R.243 (newly measured here, filed here)**
Owner: `pkg/lang/slice_bounds_kind.go`, `pkg/lang/slice_bounds_kind_test.go`, `pkg/lang/codegen.go`

## Context

ADR 0336 closed the slice door's *kind* question and filed the rest of the family. The worst row in that family was
`Gap R.236`, and it was the worst precisely because it could not be asserted: it did not answer, did not refuse and
did not raise.

```
xs = [1, 2]
print(xs[::0])      # nothing at all — the run is killed; exit 2, the code ADR 0166 reserves for OUR bug
print(xs[0:2:0])    # []   at exit 0 — the loop happened to be stopped by its bounds instead of by its stride
print("abcd"[::0])  # `cannot fold string slice` at exit 1 — a refusal where the reference raises
```

CPython answers all three the same way:

```
ValueError: slice step cannot be zero
```

One program, three behaviours, none of them the reference's. And the first is the class this contract ranks worst:
`rt_slice` walks `for (i = low; i != high; i += step)` and a step of zero leaves `i` where it started, forever. The
run is killed by a budget, the CLI reports **exit 2**, and the reader is told the *compiler* failed — for a program
whose only fault is writing `xs[::0]`, which is a two-minute tutorial line and a half-typed `0`.

That is also why the row ranked first in its family rather than last: a wrong answer misleads one program, and a
killed run mis-files the toolchain in the one class ADR 0166 keeps for a compiler bug — where the suite's own
guard (`exit 2 is forbidden on every row`) has nothing to compare against, because the run never came back.

## Decision

**A step is asked what it holds, where the value is.** Zero is the reference's `ValueError`, raised through the same
non-local road every other trap takes, so `except ValueError:` catches it and an uncaught one reports the class and
the message and exits 3.

Three cases, deliberately different in the emitted module (`sliceStepIsKnown` decides between them):

1. **A step the compiler reads as zero** — a literal, a constant expression, a name in the constant notebook —
   raises where the call would have stood: no copy, no `@rt_slice(`, and no counter advanced, because a gate stays
   pure (ADR 0273) and nothing that is about to be raised may also be built.
2. **A step the compiler cannot read** — `xs[::k]`, a parameter, a call's answer — carries a run-time guard
   (`zeroStepGuardAtSlice`: `icmp eq i32 %step, 0` → the raise, else the continuation beside the call). The check
   has to live where the value is, because the loop that never ends is the helper's own; the helper is not touched
   and keeps no exception state, which is what keeps the trap catchable (ADR 0214).
3. **A step the compiler reads as anything else** — `2`, `-1`, `1 - 1` — asks nothing. No compare, no raise site,
   no string in the module. This is the row's negative control: a guard emitted unconditionally would keep every
   other row green while quietly making the ordinary case pay for the pathological one
   (`TestTheZeroStepGuardIsInTheModuleWhereTheValueIs` asserts the absence).

**Where the question is asked.** After every bound has been evaluated, and before the copy is built. That is the
reference's order — a slice object is constructed from low, high and step and *then* refuses a zero stride — and it
is also what makes the guard safe: the bounds' own side effects have run, the raise arrives with the exception the
program would have got, and the copy that must never exist is not emitted first.

**The text road answers what it can.** A text slice whose step is a *constant* zero now raises the same ValueError
(where it used to be refused with `cannot fold string slice`). A step that is a *value* still cannot be answered:
`rt_str_slice` takes a low and a high and **no stride at all**, so `s[::k]` keeps refusing —
[Gap R.243](../roadmap-details.md#gap-r-243). The refusal stays, because inventing a stride by walking the
characters in the compiler is the guess this loop has refused everywhere else; and unlike the list road it cannot
hang, because there is no loop to hang in.

**What the row did not fold in.** The kind gate (ADR 0336) and this value question share a file and a call site but
not a predicate: `0` is a perfectly good integer, and the class, the message and the DoD differ. A step whose *kind*
is wrong (`xs[::"a"]`) is ADR 0336's raise; a step whose *value* is zero is this one's.

## Filed from the same measurement, not fixed here

| shape | CPython | this backend today | row |
| --- | --- | --- | --- |
| `x = 5` / `y = x-1` / `print(y)` | `4` | answers **`5`** at exit 0 — and `print(1-1)` is a `parse error at 1:8: expected ")"` | **Gap R.242** — the lexer emits `-1` as one token whenever a `-` is followed by a digit (`lexer.go`'s `case c == '-' && IsDigit(src[i+1])`), with no "can the previous token end an expression?" context, so a subtraction is parsed as two expressions and the statement form silently drops the arithmetic |
| `s = "abcd"` / `k = 2` / `print(s[::k])` | `ac` | refuses: `cannot fold string slice` | **Gap R.243** — the text road has no stepped helper |
| `ys = ["a"]` / `print(xs[ys[0]:2])`, `xs[g():2]`, `at(1.5)` through a float parameter | the `TypeError` of ADR 0336 | answers `[1, 2]` / `1` at exit 0 | Gap R.237 |
| `def n(): return xs[1:3]` / `print(n())` | `[2, 3]` | answers `1` at exit 0 | Gap R.238 |
| `print([x * 2 for x in xs[0:2]])` | `[2, 4]` | refuses | Gap R.239 |
| `print(len(s[1:3]))` | `2` | refuses | Gap R.240 |
| `except IndexError:` with a user call in the arm | the arm runs, the program continues, exit 0 | traceback, exit 3, the statement after the `try` skipped | Gap R.241 |

`Gap R.242` is the one this cycle found that is not a slice row at all: `print(1-1)`, `print(x-1)` and
`print(xs[2-1])` are parse errors, and in a statement position the same program **answers at exit 0 with the
subtraction missing** (`y = x-1` binds `x`). That is the priority-1 class this queue ranks above everything else, and
it is filed with a row, a measurement and a fix sketch rather than being fixed inside a row about slices.

## IR / codegen consequences

- A known-zero step emits the raise and **no** call: `main` carries `store i32 1, i32* @exn_flag`, the ValueError's
  interned message and a branch to the handler in scope (or the raise-exit), and `call i32 @rt_slice(` is absent —
  asserted, because its presence is the shape that used to hang.
- A value step emits `icmp eq i32 %step, 0` with a raise block and a reopen, immediately before the call. The block
  structure is the same one `normalizeIndexFor` has used for bounds traps since ADR 0210, so nothing new has to be
  verified; both shapes are run through `llc` in the test table.
- `rt_slice`'s signature and body are **unchanged**. A helper that raised would own exception state the codegen
  cannot see and a trap the program could not catch (ADR 0214); a helper that returned empty on a zero step would
  be a wrong answer wearing the shape of a fix.
- The string road is untouched: `rt_str_slice` still takes `(str, lo, hi)`. Adding the stride there is Gap R.243's
  change, with its own fold and its own rows.

## Alternatives rejected

- **Make `rt_slice` treat a zero step as 1.** It removes the hang and prints a list the reference never prints —
  the worst trade in this contract (a wrong answer at exit 0 beats a raise), and it hides the question rather than
  asking it.
- **Check the step inside the helper and `rt_die`.** The run would exit 3 with the right words and no catchability:
  `try: … except ValueError:` is language surface (ADR 0214), and a runtime that can report a class it cannot raise
  is the shape Gap R.21/R.22 kept tripping over.
- **Emit the guard unconditionally, beside every `rt_slice` call.** Simple, and it puts a compare, a raise block and
  an interned string into every stepped slice the language builds — paid by every program for a value almost none of
  them writes. The three-way split costs one `foldConstInt` and keeps the ordinary module ordinary.
- **Fold the value question into `badSliceBoundGate`.** One gate reading kinds and values decides two questions with
  two classes (`TypeError` and `ValueError`), two messages and two DoDs; the call site would then need to know which
  of them fired. They share a file, not a predicate.
- **Fix the zero step and the lexer's `-1` in one commit.** The second is the larger bug and a different component
  (`pkg/lang/lexer.go`, a priority-1 silently-wrong row); bundling it would have made the slice row's evidence — the
  raise tables, the guard, the negative control — unreadable, and would have hidden a *new* defect inside a *closed*
  row. It is filed as Gap R.242 with the measurement in its section, and it is the next row this loop takes.

## Verification

- `pkg/lang/slice_bounds_kind_test.go` — 19 raise rows (class **and** message, exit 2 forbidden by `jitTrap`,
  the `codegen:` prefix forbidden), five catchable rows including the arm-order row that proves a `TypeError` arm
  does not take a `ValueError`, four answered stride rows on both legs and on the record, and
  `TestTheZeroStepGuardIsInTheModuleWhereTheValueIs` (three modules: raise without a call, guard beside the call, and
  the untouched ordinary case) — every module verified by `llc`.
- `integration/slice_bounds_test.go` — 16 CLI raise rows across every position that reads a copy, seven answered
  stride rows against `pythonOutput`, the filed tables, and the Gap R.241 pins.
- `integration/programs/slice_bounds.gy` grows four raising rows and two stride rows; its expectation is what
  `python3` printed for it, and it is `match` on the matrix (roadmap `programs/slice_bounds`).
