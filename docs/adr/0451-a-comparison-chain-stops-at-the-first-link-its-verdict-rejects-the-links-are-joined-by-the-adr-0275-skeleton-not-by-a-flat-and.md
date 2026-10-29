# ADR 0451 — a comparison chain stops at the first link its verdict rejects: the links are joined by the ADR 0275 skeleton, not by a flat `and`

- **Status**: Accepted
- **Roadmap**: **Gap R.520** done — filed one cycle ago by **ADR 0450**, from the sweep that closed **L12.1**.
  The sibling that sweep filed, **Gap R.521** (membership in a value that has no members answers `False`
  instead of raising), is untouched here and stays in the Open queue.
- **Builds measured**: parent `821a0d5` (**ADR 0450**) and this commit, every byte from the gate's
  `-tags=llvm20`; the parent was rebuilt in a `git worktree` and asked the same programs, so the before column
  is a measurement and not a recollection.

## Context

**ADR 0288** gave the chain its node and its operand discipline: `n + 1` operands, each stored ONCE into a
fresh local slot, every link reading the slots back. **ADR 0450** gave the same node its checker arm. What
was left between them was the conjunction, and the conjunction was a machine instruction:

```llvm
  %t6 = icmp slt i32 %x.ld, 3          ; link 0 — lowered
  %t7 = zext i1 %t6 to i32
  %t9 = call i32 @gy_boom()            ; link 1's operand — lowered TOO, unconditionally
  %t11 = icmp slt i32 %t9, %t10
  %t12 = and i32 %t7, %t11             ; the "short-circuit"
```

The comment above it said why that was thought to be safe: by the time the `and` runs, every operand has
already been evaluated into its slot, so there is no side effect left for short-circuiting to skip. That
sentence is true of the OPERANDS and false of the LINKS. A link is not a pure expression:

- an ordering can raise — `None < 3` is CPython's `TypeError: '<' not supported between instances of
  'NoneType' and 'int'`, and the chain that contains it is supposed to never ask it;
- an operand can carry an effect the program can see — a call that prints, a call that divides, a call that
  counts itself into a global.

Measured on the parent, three programs, all of them at an exit code that means something:

| program | CPython | parent `821a0d5` |
|---|---|---|
| `x = None` / `print(1 == x < 3)` | `False` | `TypeError: '<' not supported between instances of 'NoneType' and 'int'`, **exit 3** |
| `x = 5` / `print(x > 9 < 1 // 0)` | `False` | `ZeroDivisionError: integer division or modulo by zero`, **exit 3** |
| `def boom():` / `    print("called")` / `    return 5` / `print(3 < 2 < boom())` | `False` | `called` then `False`, exit 0 |

The third is the dangerous one, and it is the one no verdict assertion can catch: the chain answered the
right verdict at the right exit code and performed a call the reference never made. The counter program makes
that class visible to a test — `print(3 < 2 < probe(9))` / `print(1 < 2 < probe(9))` / `print(calls)` — where
the reference leaves `calls` at `1` and the parent left it at `2`.

Two of the three are the **priority-1 artifact class** this house puts above everything else: a trap where
the reference answers, at exit 3, from a comparison the program never asked. The reference stops at the first
link whose verdict is false; the links after it do not exist as far as the run is concerned.

## Decision

**1. The links are joined by the skeleton ADR 0275 built for `and`/`or`.** `chainValue` lowers the FIRST link
in the block the chain stands in — the reference evaluates the first two operands and asks the first question
before it decides anything — and every later link goes through `logicSkeleton` with `op = "and"`:

```go
acc, err := linkAt(b, 0)                                   // in the chain's own block
for i := 1; i < len(n.Ops); i++ {
    cond := g.asI1(b, acc)
    out, ok, serr := g.logicSkeleton(b, "and", cond, []string{"0"}, []string{"i32"},
        func(bb *strings.Builder) ([]string, bool, error) { return linkAt(bb, i) })
    acc = out[0]
}
```

which emits, per link, one `br i1` on the previous verdict, one block the verdict admits, one block it
rejects, and one `phi` over the two arms. The rejected arm contributes the literal `0`: it lowers no operand
and asks no question.

**2. Operand binding moved INTO the block.** `bindOperand(bb, i)` and `linkAt(bb, i)` take the builder as a
parameter instead of closing over the chain's own, and a `bound[]` memo records that an operand has already
been lowered. The memo is what keeps **ADR 0288**'s "the middle operand runs exactly once" true while the
blocks move around: operand `i + 1` is lowered inside the block its first link lives in, and the next link
reads the register that block produced rather than lowering the expression again. The register is defined in a
block that dominates every block that reads it, because a later link's block is reachable only through the
earlier link's true edge.

**3. A link's verdict travels the merge as `i32` 0/1.** `logicSkeleton` merges with a `phi`, and a `phi` has
one type; the settled arm is the constant `0`, so a link lowered to an `i1` is widened with `asBoolI32` before
it reaches the merge. No arm is ever converted into a shape the program never gave it (**ADR 0166**'s rule
read at the merge instead of at a print).

**4. A raise stays in its own link's block.** A comparison that cannot answer raises through the non-local
road (`@exn_flag` → `main.raiseexit`), and the raise is now emitted inside the block the previous verdict
admits — which is why the first row of the table above prints `False` instead of dying, and why
`except TypeError:` still catches the raise that a chain DOES reach (`x = None` / `print(x == x < 3)` still
ends in the handler). Skipping a raise the reference also skips is not a divergence — it is the answer.

**5. Every link still goes through the ORDINARY comparison road.** `linkAt` builds the link over the
placeholder nodes carrying the already-computed words and hands them to `g.value` as a `*BinOp`, so a chain
gets the int, text, float, container and tagged-slot answers an ordinary comparison gets — never a second set
that can disagree with the first (**ADR 0232**'s one question, asked once).

## Agentic rationale

A chain is one of the constructs an agent writes most often — a range test, a bound check — and the compiled
answer has to be the program the agent described, not a program that happens to print the same verdict. The
third row of the table is the reason this ADR exists rather than a comment: nothing an agent could observe
about `print(3 < 2 < boom())` at exit 0 told it that its own `boom` had run. Now the effect structure is a
property of the emitted module and is tested as one: `TestTheChainConjunctionIsTheShortCircuitSkeletonNotAFlatAnd`
fails if a flat `and i32` grows back over any of seven chain shapes (ints, texts, a double, a container read),
`TestTheChainAnswersTheArmsThePreviousVerdictSettled` reads the merge line and requires the settled arm to be
the literal `0`, requires the operand's call to sit after the `br i1` that decides it, and requires the slot to
be written by exactly one value store. The diagnostics, exit codes and JSON surfaces are unchanged; `--check`
says the same thing it said one commit ago (**ADR 0450**'s rows are unmoved), because this is a lowering change
and not an interface change.

## Codegen / IR implications

`pkg/lang/codegen_arith.go`'s `chainValue` is the only emitter that changed. A two-link chain now carries, in
`@main`:

```llvm
  %t8 = icmp ne i32 %t7, 0
  br i1 %t8, label %logic.rhs2, label %logic.lhs1
logic.rhs2:
  %t9 = call i32 @gy_boom()          ; the operand, ONLY in the block the verdict admits
  %t10 = load i32, i32* @exn_flag    ; the link's raise, in the same block
  …
  store i32 %t9, i32* %_chain3       ; the operand's one store
  %t14 = load i32, i32* %_chain3
  %t15 = icmp slt i32 9, %_chain3.ld4
  %t16 = zext i1 %t15 to i32
  br label %logic.rhsfwd3
logic.lhs1:
  br label %logic.merge4
logic.rhsfwd3:
  br label %logic.merge4
logic.merge4:
  %t17 = phi i32 [ 0, %logic.lhs1 ], [ %t16, %logic.rhsfwd3 ]
```

— the labels `logic.rhs`/`logic.lhs`/`logic.rhsfwd`/`logic.merge` are ADR 0275's, reused rather than renamed,
so the one skeleton that knows how to skip an operand a test rejected stays the only place that knowledge
lives. `chain_operand.go`'s gate, the pair-binding roads and the container-operand refusal are untouched: a
chain whose operand carries more than one word still binds a `_chainN`/`_chainN_tag` pair, and the refusal of
a container operand still fires at the same position. An `and i1` may still appear in a module — it belongs to
a comparison's OWN lowering (a float `!=`, an `is not`), not to the chain's conjunction, which is why the
shape guard pins `and i32` and not every `and`.

## Alternatives rejected

- **Folding a chain to `a < b and b < c` over the original nodes.** That is **ADR 0288**'s bug re-imported by
  the code written to fix it: the middle operand runs twice, and `and` gets handed a link the reference never
  asked about. The node exists to keep the operands once; the fix had to keep that.
- **Eagerly lowering the links and then guarding the raises.** A link's raise is not the only effect a link
  has — the operand's call, its print, its own division all happen before any guard could run. Guarding the
  trap leaves the counter at `2`.
- **A run-time "am I still live" flag consulted inside each link.** That keeps the eager lowering and asks the
  runtime to undo what codegen did; the effects have already happened by the time the flag is read, and it
  puts language semantics in the runtime where a `br` belongs (**ADR 0166**'s division of labour).
- **Short-circuiting only when the later operand is effect-free.** The case that needs it is exactly the case
  with the effect, so the filter would keep the bug and drop the fix; and "effect-free" is a static claim about
  a call whose body the pass has not decided about.
- **A chain-specific branch chain instead of `logicSkeleton`.** Two skeletons that each know how to skip an
  operand a test rejected is two chances to disagree about when — and ADR 0275's door already carries the
  double-eval fix (**Gap R.149**), the print-once rule, and the phi/forwarder block shape `llc` accepts.
- **Deleting ADR 0288's operand slots now that blocks could hold a register.** A register computed in the
  chain's block is usable downstream, but the slots are what the comparison roads ask about — `listVars`,
  `dictVals`, the tagged tables are keyed by NAME, and a synthetic register answers none of them. That was a
  real regression an earlier draft of this code shipped (`0 < xs[1] < 3` refused as `index of a non-literal
  variable`).

## What this row does not pay

**Gap R.521** (`print(1 in 5)` answers `False` at exit 0 where the reference raises `argument of type 'int' is
not iterable`) is a fold, not a conjunction, and stays open. **Gap R.443** (the loop item is still a one-word
chain operand) is an operand gate, not a conjunction, and stays open. The chain's checker arm, its `bool` type
and the `type.compare.uncomparable` warning are **ADR 0450**'s and are unmoved — every row of
`pkg/lang/compare_chain_check_test.go` and `integration/compare_chain_check_test.go` passed unchanged, which is
this commit's "the verdicts did not move" claim, tested rather than asserted.
