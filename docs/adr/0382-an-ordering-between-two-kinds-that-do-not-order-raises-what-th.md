# An ordering between two kinds that do not order raises what the reference raises, asked of the operands before either is lowered

**Roadmap:** Gap R.85 (closed here — `<`, `<=`, `>`, `>=` between a number and the void, or between a text and a
number, answered a verdict at exit 0) · Gap R.202 (closed here — the same raise naming `'int'` for an operand that
is a verdict, the void or a text) · Gap R.354 (newly measured and filed: one half of the pair is a fact the front end
never wrote down, so the door stays out) · Gap R.131 (comparison chains — still open, and this ADR's remaining hole) ·
Gap R.201 / Gap R.86 (two containers of the same kind: still the tagged order door's, deliberately untouched here) ·
ADR 0302 (one backend, so a rule the comparison road does not have has no second engine to catch it) · ADR 0273 (a
door that cannot answer says so; a wrong value at exit 0 is the defect this repo files rather than ships) · ADR 0271
(a raise says what the program wrote, which is why a sentence naming the wrong kind is itself a wrong answer) ·
ADR 0215 (the reference's wording is the wording) · ADR 0293 (the arithmetic door this one copies: the sentence is a
program-level raise, not a refusal) · ADR 0266 (negation's door, the same shape one operator earlier) · ADR 0365 (the
sequence-operation road this door sits behind, and the rule that a question is asked before the operands are lowered) ·
ADR 0341 (a container's kind outranks a stale scalar record — the reason the container claims are asked first) ·
ADR 0334 (what an interned text's word is, which is why a text cannot be compared after lowering) · ADR 0368 (a body
whose every road answers the void, read here from the AST) · ADR 0166 (exit 2 is a compiler bug, so every arm of this
door is checked through `llc`) · ADR 0302 / ADR 0317 (the record leg and the anti-caching rule that judges it)

## Status

Accepted. Landed 2026-10-20.

## Context

Nine lines of source a person can write in one breath. Before this cycle the compiled leg answered six of them at
exit 0, and raised over three of them naming a type the program never mentioned.

```python
print(1 < None)      # the reference: TypeError … 'int' and 'NoneType'      here: False   at exit 0
print(None < 1)      # the reference: TypeError … 'NoneType' and 'int'      here: True    at exit 0
print(None <= None)  # the reference: TypeError … 'NoneType' and 'NoneType' here: True    at exit 0
print(1.5 < "a")     # the reference: TypeError … 'float' and 'str'         here: False   at exit 0
print("a" < 1.5)     # the reference: TypeError … 'str' and 'float'         here: True    at exit 0
print(True < None)   # the reference: TypeError … 'bool' and 'NoneType'     here: False   at exit 0
print(True < [1])    # the reference: … 'bool' and 'list'                   here: … 'int' and 'list'
print(None < [1])    # the reference: … 'NoneType' and 'list'               here: … 'int' and 'list'
print("a" < [1])     # the reference: … 'str' and 'list'                    here: … 'int' and 'list'
```

The reason is that a comparison never sees the kinds. By the time the ordering road runs, `None` has lowered to the
word `0`, a text has lowered to an index into `@str_tab`, and a verdict has lowered to `0`/`1` — so `1 < None` is an
`icmp slt i32 1, 0`, which is a true thing to say about two integers and a meaningless thing to say about the program
that produced them. The record leg had the right answer on file for all thirty-seven of these sources all along
(`testdata/interpreter-golden.json`); the compiled leg was diverging from it, and the divergence was not on the drift
ledger because a compiled answer against a recorded trap is not the shape that ledger held.

There was already a door of exactly this shape in the codebase — ADR 0293's arithmetic door, which stops `1 + "a"`
before the addition runs. Comparisons simply had no counterpart.

## Decision

**Ask the pair, once, before either operand is lowered.** `pkg/lang/order_kind.go` holds the whole rule, in three
pieces, each of which is a question the AST can answer.

1. **`orderFamilyOf(kind)`** puts a kind name in one of five families: `int`/`float`/`bool` are numbers, `str` is
   text, `NoneType` is the void, `list`/`dict`/`set`/`tuple` are containers, and anything the table does not name is
   unknown.
2. **`orderPairHasNoOrdering(left, right)`** is the language question, and it is a fact about the pair, not about the
   operator: the void orders against nothing, including itself; two kinds from different families order against nothing;
   two kinds from the same family may order and the door says nothing; an unknown kind is not an unorderable kind, so
   the door says nothing. Two containers say nothing even when the kinds differ — that pair is the container-order
   door's beside it, which already raises the reference's sentence for a mixed pair (`[1] < {1}`) and files the
   same-kind pairs it cannot yet build (Gap R.201, Gap R.86).
3. **`orderKindOf(expr)`** names an operand's kind only where the AST states it: the literals; a `+`/`-` over a number
   and a container built by `[]`/`{}`/`()`, with the tuple asked of `isTupleExpr` so a tuple reads `'tuple'` (L11.3,
   ADR 0361); arithmetic over two numbers, text concatenation and text repeat; a name whose latest binding the front end
   recorded (ADR 0172's tables — the same ones the printers read); and the builtins whose answer is a fact about the
   callee (`len`, `ord`, `int`, `float`, `str`, `repr`, `chr`) or a fact about one argument (`abs` over a number).

When the pair has no ordering, the door raises — `raiseTo` with `TypeError` and CPython's own sentence,
`'<' not supported between instances of 'int' and 'NoneType'`, the two type names in source order because the
reference names the operand whose `__lt__` was tried first — then opens a fresh continuation block and returns a value
word the caller never reads, the shape `badIndexGate` and `badSliceBoundGate` already use. The old `TypeError: unorderable
types: <not-implemented>` and the 256-char truncated `unable to compare …` fallback are deleted rather than improved:
one question now decides the case, and a raise the reference does not have is as much a wrong answer as a wrong verdict.

The door is hooked on **two** roads, because a comparison is lowered on two roads: `codegen_binop.go`'s value road
(bare `print(1 < None)`) and `codegen_truth.go`'s condition road (`if 1 < x:`, `while`, the ternary, `and`/`or`). A
programme whose ordering is the condition of an `if` is the same program to its author and gets the same raise.

**A claim, never a guess.** Where one side's kind is a run-time fact — a parameter, a slot the module tagged, a call
the scan cannot describe — the door stays out and the ordinary road runs, exactly as it did. That is what leaves the
last hole (below), and it is the deliberate trade: this door may raise only where the program states the pair.

## The hole this leaves, filed rather than papered over

`x = None` / `y = 1` / `print(x < y)` still answers `True` at exit 0. The front end records a name whose latest binding
was the void, a verdict, a float, a text or a container — and records nothing for a name holding an int, because an int
is what a slot holds when nothing was recorded. So one half of the pair is a fact the compiler never wrote down, this
door stays out, and the two words get compared. `Gap R.354` files it with the compiled answer and the reference's
raise pinned side by side, and `TestWhatTheOrderingDoorStillCannotNameIsFiledNotFixed` asserts each row's exact stdout,
that it is not a refusal, and that no module reaches the compiler-bug exit code. The same table holds the `while None <
i:` shape, the parameter shape (`def g(v): return v < None`) and a call whose branches answer two kinds.

## Consequences

- **37 sources that answered at exit 0 now raise the reference's sentence**, all of them traps the record already
  carried, so the record leg and the compiled leg agree on every one (the drift ledger lost the two rows these sources
  held, and `docs/operations.md`'s counts came down with them: 325 → 323 for `pkg/lang`, 20 → 19 for the CLI suite).
- **The condition road is a first-class lowering road.** A rule asked only on the value road is a rule half the
  language has. `codegen_truth.go` carries the same call, and the tests assert both shapes.
- **The three Gap R.202 rows moved out of the filed table** in `integration/pair_container_order_test.go` and are
  asserted raising the reference's own sentence. A filed row whose defect is paid is pruned, not re-asserted with the
  new value — the table describes what is owed.
- **A wrong sentence is a wrong answer.** The naming bug was in the message builder, one level below the defect that
  caused it: names come from `orderKindOf` now, so the raise describes the operands and never borrows the number-table
  default that reads every operand as an int.
- The int-against-void shapes (`print(1 < None)`, `print(1.5 >= None)`, `print((3 + 4) < None)`, `print(-1 < None)`)
  are constant-folded **by the door**, not by the folder: a fold is allowed to compute a number, never to decide that
  two kinds order.

## Machine path

`--emit-llvm` shows the door from the outside, and the IR leg asserts both halves of it: the raising module carries the
sentence as a global (`private unnamed_addr constant [56 x i8] c"'<' not supported …"`) and does **not** carry the
`icmp slt i32 1, 0` / `fcmp olt double 1.000000e+00, ptrtoint …` the lowering would have produced — a raise that still
computes the comparison it refused has not replaced anything — while a program that never asks the question
(`print(1 < 2)`) carries neither the sentence nor a `gusty_raise`. Exit codes are unchanged: the raise is a program
behaviour (exit 3), and the 15 IR-shape arms forbid exit 0 and exit 2 explicitly.

## Alternatives rejected

- **Let the float/tagged comparison roads each gain a cross-kind arm.** Three roads would each need the same table,
  and the pair would be judged after `None` had become `0` and a text had become its index — the state of affairs that
  caused the defect. One door, asked first, cannot drift from three.
- **Answer `False` for an unorderable pair, as the backend did.** A verdict where the reference raises is the worst
  class in the ladder (ADR 0273), and it was the shipped behaviour being repaired.
- **Keep the `unorderable types:` message.** It is not a sentence the reference has ever written, so every such raise
  was teaching a language that is not this one (ADR 0215).
- **Guess the kind of a name the tables do not describe.** Naming an unrecorded int `int` would pay Gap R.354's rows
  and invent traps for programs whose name holds something else by the time the comparison runs. The door stays out
  instead, the rows stay filed, and the fix is a naming table the front end owns — not a guess at the use site.
- **Route the raise through the `unimplemented()` refusal path.** A refusal is the compiler's opinion about a
  construct; the construct is implemented and the program is wrong, so it is a raise with a source position (ADR 0293's
  distinction, kept).
