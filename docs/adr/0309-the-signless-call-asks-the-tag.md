# The signless call asks the tag — `abs` of a value whose kind only the run time can describe

Status: accepted. Roadmap: `L11.1` (the tagged value word), `Gap R.146` (the positions that keep **one word**
for a whole value — `abs(n)` is paid here; `min(n, 3)`, a dict entry and a set member still owe it),
`Gap R.191` (the unrelated-container refusal this cycle's probe had to route around). Continues the pair arc:
ADR 0265 (the arithmetic door), ADR 0271 (`abs`'s raise wording), ADR 0303 (the one printer), ADR 0304 (the
pair-bound name's arithmetic), ADR 0305 (the double domain), ADR 0306 (the container element), ADR 0307 (the
f-string field), ADR 0308 (the witness vocabulary that retired engine now lives behind).

## Context

`xs = []` / `xs.append(7)` / `n = xs[0]` / `print(abs(n))` is CPython's `7`, and the compiled backend refused
it. The same name already answered `print(n)` (ADR 0303's one tag-reading printer), `print(n - 1)` and
`print(-n)` (ADR 0304's per-operator door), `print(n / 4)` (ADR 0305), `print([n])` (ADR 0306) and
`print(f"{n}")` (ADR 0307). `abs` was the last of the four examples that began this whole arc, and it was
still a refusal because of one structural difference: `+`, `-` and `//` are **operators**, and ADR 0265's
door was written for operators. `abs` is a **call**.

A pair-bound name is two words — the payload the slot held, and the tag that says what it means. Every
position that has been "paid" in this arc has been a position taught to read both. The positions still in
`Gap R.146` are the ones that read **one**: `abs`'s numeric road went through the built-in's ordinary
argument lowering, which asks for a single number. Reading a single number out of a pair is not a partial
answer, it is a different value: for a float slot the payload is a box handle, for a text slot an interned
`@str_tab` index, for a container a heap address. Taking the magnitude of one and printing it is a plausible
number at the exit code of success — ADR 0302's own ledger calls that the worst class of defect, and the
refusal counter (`compiled refusals this run`) never counts it because nothing was refused.

## Decision

**`abs` becomes the arithmetic door's own op — the signless call, beside the unary minus.** In
`taggedArithPair` (`pkg/lang/heapargs.go`) a `*Call` whose function is the name `abs` with exactly one
argument now maps to the door with operand code `6`, passing the operand twice because the door is
binary-shaped and the second word is read only by the operators that ask for a divisor. In the emitted
helper (`rt_num_arith`, `pkg/lang/codegen.go`) op `6` selects the magnitude of the **lifted** value —
`select` on `fcmp olt %af, 0.0` between the negation and the value — which is the honest test for `-0.0`
(`abs(-0.0)` is `0.0`) and keeps the answer in the same word the operands arrived in, so the answer carries
its own kind back out: int or bool in, int out; float in, float out.

**The door takes the operand whatever the slot holds.** Same rule as unary minus, and for the same reason:
CPython answers a number or raises, so a tag that says `str`/`NoneType`/`list`/`dict`/`set` is not a reason
to refuse at compile time — it is a reason to raise at run time with the reference's own sentence, catchable
by the `except` the program wrote. `arithWouldRefuse` therefore recurses through `abs`'s argument rather than
consulting `pairRetDone`.

**`abs`'s sentence is its own, chosen by which CALL asked, not by which op the helper implements.**
`bad operand type for abs(): 'str'`, never `bad operand type for unary -: 'str'`. ADR 0271 established that a
trap says what the program wrote; sharing one format string between the two signless ops is precisely how
that rule broke before, and `rt_num_bad` now picks between `@rt.num.absfmt` and `@rt.num.negfmt` on the op
code it was handed. `TestTheSignlessCallRaisesWhatTheReferenceRaises` pins both sentences and the
catchability of the raise, through the CLI against CPython as well (`integration/pair_abs_test.go`).

**The constant road is untouched.** `abs(7)`, `abs(-8)`, `abs(-2.5)`, `abs(True)` and `abs(x)` for a plain
`x = -7` keep their compile-time answer; `TestTheSignlessCallOfAKnownValueIsStillFolded` fails if the door
starts routing a literal's magnitude through the run time. A door that swallows the constant path is both a
lost instruction and a module that grows a helper call it does not need.

**A pair-shaped sibling makes the door keep its hand off.** Found by the compiler breaking itself, not by a
program: with `abs` admitted, `round(abs(n) / 2)` had the float door lift its own sibling — `abs(n)` — which
asked the door for its two words, which asked for the double of the sibling, and so on until the goroutine's
stack gave out. A compiler crash is exit 2, which ADR 0166 reserves for a module `llc` rejected; a stack
exhaustion on a program the reference answers is the same class of bug wearing a different exit code. So
`arithOperandPair` first asks `arithWouldRefuse` of the operand, and a pair-shaped call inside the door
returns "not mine" — the position refuses in words, naming the missing half, which is what
`TestTheSignlessCallStillRefusesThePositionsThatTakeOneWord` pins (`abs(n) + 1`, `abs(n) * 2`, `abs(n) % 3`,
`round(abs(n) / 2)`, `abs(-n)`, `min(abs(n), 3)`, `sum([abs(n)])`). Termination comes from the door being
asked of a call's **argument**, never of the call.

**What stays in the row.** `min(n, 3)`, `max(n, 3)`, a dict entry, a set member, a builtin-folded static
array, and an f-string used as a VALUE are still `Gap R.146`'s. This ADR pays the signless call and nothing
else; `pair_abs_test.go`'s refusal table is what keeps the rest visible rather than inherited.

## Agentic rationale

The IR is the assertion, not the output. `TestTheSignlessCallRunsThroughTheArithmeticDoor` fails if the
module stops calling `@rt_num_arith` or stops emitting `@llvm.fabs.f64` — i.e. if `abs` grows a second road of
its own, which is how a "fixed" feature silently re-acquires the one-word bug — and if the answer is printed
without asking the tag. The probe program `programs/probe_the_signless_call_answers_for_a_pair_bound_name.gy`
is registered `match`, so an agent running the matrix sees `7 / 7 / 8 / 2.5 / 2.5 / 1 / 0 / 14 / 14 / 9 / 5`
against CPython rather than reading this ADR to learn what the backend does.

## Alternatives rejected

- **Lift the payload and take its magnitude (`rt_lift_num` + `fabs`).** Rejected: that is the one-word
  reading this ADR exists to refuse — it converts a text slot's interned index and a float slot's box handle
  into magnitudes, and prints them at exit 0.
- **Give `abs` its own runtime helper.** Rejected: a second road is where the tag-reading invariant goes to
  die; the operator door already branches per kind and already writes CPython's sentences. The IR test names
  this rejection explicitly.
- **Compile-time refusal for every non-numeric slot (`abs` of a text slot says "unsupported").** Rejected:
  the reference raises, the program can catch it, and a refusal that a program cannot `except` is a compiler
  opinion, not a language answer (ADR 0166, ADR 0228).
- **Let the float door lift a pair-shaped sibling "just for this shape".** Rejected: it recursed until the
  stack gave out (a compiler crash is exit 2 by ADR 0166's reading), and the "just this shape" carve-out is
  the same as deleting the refusal that makes `Gap R.146`'s remaining field measurable.
