# ADR 0215 — an operator is a question about two runtime kinds

Status: accepted (roadmap Gap R.26; closes it for the interpreter, opens R.27–R.33)

## Context

`print("a" * "b")` printed `1099516870662` and exited 0. So did `print(1 + None)` (`1048578`),
`print([1] + 1)` (`2097157`) and `print("a" < 1)` (`0`). The operator switch in `evalBin` never
consulted an operand's kind: an operand that was not a known container fell into the arithmetic path
holding a heap handle, and Go multiplied it.

I did not fix one shape. I wrote the matrix first — 23 mistyped shapes and 30 *legal* ones, run
through the interpreter, the compiled backend and CPython — and it produced three findings instead
of one:

1. Every mistyped pair answered a number. This was the recorded gap.
2. **Legal programs answered numbers too.** `[1] + [2]` printed `2097157`, `[1] * 3` printed
   `3145734`, `"ab" * 2` printed `2097156`: list concatenation and sequence repeat did not exist, so
   they went down the same handle path. A gate that only refuses would have made these *worse* —
   refusing a program CPython runs is still a divergence — so the rule had to come with the missing
   operations.
3. Ordered comparison compared handles: `"a" < "b"` was a question about allocation order, and
   `[1, 2] < [2]` about nothing.

## Decision

**An operator is applied to a pair of runtime kinds, and a pair with no rule raises `TypeError`.**
The gate (`checkBinOp`) sits in `evalBin` after dunder dispatch — so a class defining `__add__`
still performs the operation — and before every arithmetic path.

- Legal pairs: number × number for `+ - * / // ** %`; `str` × `str` and `list` × `list` for `+`;
  a sequence × `int` (either order) for `*`; same-kind pairs for `<`-class operators.
- Everything else raises, in the reference implementation's words — four message shapes, chosen by
  what Python says for that situation, not by which internal branch noticed:
  `unsupported operand type(s) for …`, `can't multiply sequence by non-int of type …`,
  `can only concatenate X (not "Y") to X`, `'<' not supported between instances of …`.
  All 22 measured shapes now agree with CPython character for character, asserted by running
  `python3` (`integration/operator_operand_test.go`), not by pasting strings.
- `%` on a string is interpolation, which the language does not have (Gap R.31). I reproduce the
  reference's message for the case it also rejects (`"a" % 2` → `not all arguments converted during
  string formatting`) and raise the generic refusal where it would have formatted — that half is
  pinned in a probe rather than pretended to work.
- Sequence operations are implemented, not refused: `"ab" * 2`, `3 * [1]`, `[1] + [2] + [3]`,
  negative/zero counts giving empty, and ordering by value for strings and lists — including the
  nested case nobody tests, `[1] < ["a"]`, which raises rather than guessing.
- `==`, `!=`, `in`, `not in`, `is`, `is not` are **excluded** from the gate: they compare, they do not
  compute. `1 == "a"` is False, not an error. A rule that traps everything mistyped is wrong in the
  other direction, so there is a test for exactly that.

## The bug the gate exposed (and why the fix is a constant)

The gate turned a silently-wrong bench program into an error, and the error led to a representation
flaw. Interpreter values are an untagged `int64`: a heap handle and the program's own integers share
one space, distinguished only by "does this number name a live object". The heap base was `1 << 20`,
so a loop computing `i * i` walked into the object space — at `i = 1024`, `self.x * self.x` is
`1048576`, the accumulator reached `1048580`, which *was* the class's own method object, and
`s + p.norm()` became int-plus-method. Measured: correct up to 1000 iterations, wrong from ~1020.

`heapIDBase` is now `1 << 48`, and `isHandle` is the single predicate answering "is this value an
object?" — the collector's root tracer, the operator gate and every kind test ask it, so they cannot
disagree about what a value is. I first suspected the collector and spent a measurement disproving
that (same failure with collection disabled) before the operand dump showed `x*x = 1048576`; the
lesson is in the learnings file, and the regression test computes the expected sum in Go so a
reintroduced collision cannot hide behind the language's own arithmetic.

Two rules worth keeping:

- **One predicate per ontology question.** `operandKind` used to ask `e.heap[v]` directly while the
  collector asked `isHandle` — two answers to "is this an object?" is how a value becomes an int to
  one subsystem and a method object to another.
- **A base address is a contract, not a tuning knob**, and it is documented as one, including where
  it stops working (real tags are L11.1).

## Also found, by the same measurement

The property-test generator bound a list to a name and then read that name as an operand of `+`
(`v1 = [1,7,6,3,4]` … `v2 = 9 + v1`). Its own test asserts every generated program runs cleanly, so
that promise had been untrue all along — invisible while arithmetic accepted anything. The generator
now records the kind of every binding and reads a name only where that kind is accepted. Recorded
here rather than as a gap because it is fixed in this commit: a corpus that cannot fail is not a
test, and the operator gate was the first thing that could make it fail.

## Alternatives rejected

- **Static refusal in the checker** for mistyped pairs. Rejected: untyped programs are the common
  case, and a catchable runtime event must not become a build error; the compiled backend already
  refuses statically where its type info is sound, and does not where it is not (Gap R.27).
- **Refuse the sequence operations too, since codegen cannot lower them yet.** Rejected: `[1] + [2]`
  runs in the reference, so refusing it in the interpreter would trade a wrong number for an
  unfounded error. Codegen's half is recorded (Gap R.33: refusals for `str * int`, and an
  llc-rejected module for list concatenation — a compiler bug, not a user error).
- **Tag interpreter values now** (low-bit tag or boxed-word scheme). Deferred to L11.1: it is the
  right long-term answer, it touches every heap site in two backends, and moving the base address
  closes the observable defect for the whole practical range at a fraction of the risk. The
  regression test is written so that L11.1 landing keeps it as proof rather than requiring deletion.
- **Keep the internal wording (`"unsupported call"`, `"cannot index this"`).** Rejected for the same
  reason as ADR 0214: a message about the implementation is a message the user cannot act on.
