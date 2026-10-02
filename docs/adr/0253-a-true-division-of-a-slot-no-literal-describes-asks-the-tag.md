# ADR 0253 — the true division of a slot no literal describes asks the tag

Date: 2026-10-02 · Status: Accepted · Roadmap: L11.1 (the numeric use of a slot the object describes), Gap R.96 (closed), Gap R.82, Gap R.88 (a new shape of the same emission, filed as Gap R.98), Gap R.99–R.101 (filed), ADR 0252, ADR 0251, ADR 0250, ADR 0249, ADR 0246, ADR 0238, ADR 0214, ADR 0205, ADR 0187, ADR 0166

## Context

ADR 0252 ended with a sentence that was also a boundary: an ordering can be lowered against a slot whose kind only the object knows because **its verdict is a bool whatever the operands turn out to be**, and `xs[0] + 1` still refuses because it must know whether it answers `4` or `4.5` before the module exists. ADR 0252 filed the exception as Gap R.96, because one arithmetic operator does not have that problem:

```
xs = []
xs.append(3)
print(xs[0] / 4)    # CPython 0.75 · --interp 0.75 · --aot printed 0.0, exit 0
```

`0.0` was not a wrong arithmetic result. It was ADR 0249's empty operand — the float arm asking `floatValue` for a register the tag never described, being handed the empty string, and the caller substituting the literal `0.0` so the instruction would at least verify. The version that reaches `llc` is exit 2, which ADR 0166 already assigns to us; the version that substitutes is worse, because the program prints a number-shaped answer, exits 0, and looks like it worked. The neighbours on the same slot (`+ 1`, `* 2`, `// 1`, `% 1`, `** 2`) either answered correctly for an int slot or refused honestly for a float one, so the defect was one operator's arm, not the door's.

Two more shapes came out of the sweep that wrote the arm, both of the same "a double met an `i32`" family, both pre-existing and both measured here because the new arm makes the emission live:

```
t = 0.0
t += 1.5
print(t)            # --aot: llc rejects "multiple definition of local value named '_t'" — exit 2

u = 0
u += 1.5
print(u)            # --aot: panic: assignment to entry in nil map — the compiler crashed
```

## Decision

**True division is admissible where the object reports a side, because the operator settles the result's kind.** `taggedNumberOperands` and `taggedNumberUseApplies` gain one clause: a slot read that no literal describes (`slotReadFromObject` — the same gate the print, the equality, the length, the ordering and the subscript use, ADR 0246/0250/0251/0252) is admitted for `/` only. Every other operator still refuses, and the gate is written so the door cannot drift: a `fromTag` side for anything but `/` fails the gate, because its raise sentence names the other operand's type and that type is only knowable per pair.

The arm is `taggedDoubleFromObject`. The (payload, tag) pair comes from ADR 0251's `taggedSlotPair`, and the tag chooses:

- **float** → `rt_float_of`, the payload is a handle on an `@float_box` (ADR 0238);
- **int, bool** → `sitofp`, the payload *is* the number (and a bool is the int CPython says it is, until L11.2 gives bool its own tag);
- **str, NoneType, list, dict, set** → one `raiseTo` each, with CPython's sentence for *this* operator and *this* kind — `unsupported operand type(s) for /: 'list' and 'int'`. The set is closed because the writers are ADR 0187's, which is what lets the last arm be the unconditional `else` and keeps the merge's only predecessors the two arms that produce a double (ADR 0205, and no `reachable` statement for a block nobody can enter, ADR 0214).

**The zero trap lives inside the arms.** `fdiv` does not trap — LLVM returns ±inf, which is how the backend once printed `inf` for `print(1 / 0)` — so the `fcmp oeq … 0.0` guard is ours to emit, and CPython's wording for it is a fact about *both* operand kinds: `3 / 0` is `division by zero`, `1.5 / 0` is `float division by zero`. With one operand's kind living only in the object, a guard placed after the merge could only guess, and a guessed sentence is one the program's own `except ZeroDivisionError:` would not match. So `guardNonZeroFloat`/`branchRaise` now *return the block the continuation runs in*, and each arm guards the operand it knows to be the divisor. (An operand the lift cannot reach for free — a call — is refused rather than divided by an unguarded `fdiv`.)

**An operand that still cannot be lifted is refused, never substituted.** `floatBinOp` records the failure *and* calls `noteUnlowered` — sticky as well as returned — and returns the empty string, so no caller can accidentally write the instruction. The `0.0` substitution is gone from the file.

**A double may only be asked for by a caller that can hold one.** `floatValue` increments a `doubleDomain` counter around everything it lowers; `value()` refuses a tagged numeric BinOp when the counter is zero. Printing, a comparison, an `if`/`while` head, a float binding and a float-boxed container element are in the double domain; a call argument, `str()`'s argument, an augmented assignment onto an `int` variable and a dict slot written by key are not, and receiving the door's double there is the module `llc` rejects. Those refusals name the missing word — the `(payload, tag)` pair the tagged value word will carry — and are exit 1, never exit 2. Two related emission bugs fell to the same sweep: an augmented assignment lowered its float result **twice** (once into a dead `add i32`, once into the `fadd`), which was harmless until a register could arrive as a double, and `t = 0.0` / `t += 1.5` asked for a second `%_t` alloca, which `llc` rejected as a duplicate. Both are fixed by asking for the value once, in the domain that stores it, and by consulting the `allocd` table the plain assignment already keeps.

## Consequences

- Gap R.96 closes: `xs.append(3)` then `print(xs[0] / 4)` answers `0.75` on both engines, and so do the float slot, the bool slot, the negative slot, the divisor side, the dict slot by key, the slot one level below, three levels down, an index the program computes, a loop-built container, a binding, a condition, a `while` head, an f-string, a builtin's argument and a container element. `integration/programs/slot_division.gy` joined the conformance corpus and is `match` on all three legs.
- The traps are **raised**: the five non-numeric kinds each name themselves, and both `ZeroDivisionError` wordings are reachable and catchable. A compiled program that dies on these programs exits 3, like the interpreter and the oracle.
- Pinned by `pkg/lang/slot_division_test.go` (28 parity rows × both engines, 16 traps with CPython's exact sentence, catchability, the refusal family, the IR row that fails if the module stops branching on the tag or starts inventing an operand) and `integration/slot_division_test.go` (the same claim through the shipped CLI against `python3`, with exit 2 failing the file and every refusal required to name its missing half). `assertNoForbiddenIR` gained `fdiv/fadd/fmul/fsub double ,` so the empty-operand float instructions are blacklisted module-wide, not just in this file.
- `branchRaise` returning its continuation block is a small API change with a wide future: any hand-written `phi` after a guard must name the block the guard *ends in*, not the block it was written in (ADR 0138's lesson, now enforced by the signature rather than by memory).
- Three shapes measured beside this change and **filed rather than absorbed**: **Gap R.98** — the door's double reaching a context that stores an `i32` word is refused, and answering it (a call argument, `str()`, a dict slot, `+=` onto an `int` variable) is the tagged value word's; **Gap R.99** — a float element of a comprehension over a container the program built is appended through the static path and answers `[2]` for `[3.0]`; **Gap R.100** — when such an element *raises*, the guard's blocks land between the loop header and the increment the induction `phi` names as its back edge and `llc` rejects the module (exit 2), ADR 0224's lesson one door over; and **Gap R.101** — `xs[0] / ys[0]` over two built containers refuses, because the sentence names two types and the pair table is what Gap R.97 also owes.
- One re-measurement came out of the store-once rule and belongs to another row: `programs/probe_float_param_rebind`
  (`def addf(x): x = x + 1.5; return x`) had been **exit 2** since ADR 0196 — `llc` rejected `%p0` defined with type
  `double` but expected `i32` — and asking for a float value once, in the domain that stores it, made the module
  verify. The leg now *prints the argument*: `print(addf(1.0))` answers `1` with exit 0 where the oracle answers
  `2.5`. That is the worst class, so the debt row was re-pinned per leg with the wrong answer written into it
  (interpreter `2.5 / 3.0`, compiled `1 / 3.0`) rather than left claiming a failure that no longer happens. The
  answer is the return word's, not this ADR's: roadmap **Gap R.3c** under **L11.6**, and the ADR that pays it
  promotes the program out of the debt ledger.
- Agentic path: no new flag, no new exit class, no schema change. What changed for a script is that twelve programs it had to route around now answer with exit 0, six more trap with a stable class, and every remaining refusal is one stable sentence naming the half that is missing — all still reportable through `--json` with the exit-code contract unchanged.

## Alternatives rejected

- **Keep the `0.0` substitution and make the callers check the record.** One call site that forgets the check is a wrong answer with exit 0, which is the exact bug this ADR exists to close; the sticky `noteUnlowered` plus an empty return makes the forgetting unrepresentable instead of discouraged.
- **Lift the slot to float unconditionally and let the raise be the `else` for everything.** Cheapest IR, and it answers `4.5` for a slot holding the int `3` — a wrong *value*, not a near miss. The tag has to pick the arm because the payload's meaning is the tag's, not the operator's.
- **Guard the divisor after the merge with one sentence.** `print(1 / xs[0])` over an int slot would then print `float division by zero`, and a program matching `except ZeroDivisionError:` with a message — or a person reading it — gets a sentence CPython never writes for that pair. Guessing a trap's wording is guessing the program's behaviour.
- **Take the two-`fromTag`-side division too, with a chain of chains.** `xs[0] / ys[0]` is a real program and CPython answers it; but the raise table is per *pair*, the common pair (float against float, int against int) would carry 16 blocks of tests for one instruction, and the same table is owed for the ordering by Gap R.97. Doing it once, for both operators, from one pair table is the right shape; doing it here would be two features in one commit. Filed as Gap R.101.
- **Fold the comprehension's element through the tagged door inside this change.** `[v / 2 for v in xs]` fails for a reason the division does not cause — the comprehension appends elements statically, and its induction `phi` names a fixed back edge. Fixing the append is Gap R.99's change; fixing the phi is Gap R.100's; both are pinned by a table that fails when they land.
- **Let `+=` onto an `int` variable re-decide the variable's kind.** `t = 0; t += 1.5` is a program CPython answers, and today it is refused. Re-deciding a variable's kind mid-function is the `(payload, tag)` pair — the tagged value word — and a silent re-type here would give the slot two words with one name, which is what `llc` was already rejecting.
