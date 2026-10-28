# ADR 0440 — a subscript stands on what the FOLD already answered: the door raises the reference's TypeError instead of refusing the build

## Status

Accepted, 2026-10-28. Closes **roadmap Gap R.227** (filed while landing **Gap R.214**'s row, pinned in
`pkg/lang/pair_element_read_test.go` as a refusal), and files two rows this cycle measured and did not fix:
**Gap R.494** (`len` of a literal never evaluates the literal's elements) and **Gap R.495** (a subscript of a
read out of a COMPREHENSION-built container still refuses).

Read after **ADR 0326** (a fold's failure is the program's trap, emitted through `constantTrap`, not a build
that stops), **ADR 0330** (the bounds sentences, one per container), **ADR 0329** (an index that is not a
position is the reference's `TypeError`, not a refusal), **ADR 0334**/**Gap R.214** (the call-result twin of
this refusal, whose pair door exists and this one borrows the shape of), **ADR 0331**/**ADR 0327** (a literal
whose slots the fold cannot read goes to the builder and the tag), **ADR 0188** (a compile-time container
global is never an operand), **ADR 0166** (exit 2 is a compiler bug), **ADR 0232** (one question, one reading).

## Context

Two programs, both refused at exit 1, both answered by the reference with a trap the program can catch:

```gusty
print([[1, 2][0][0], 1])   #  reference TypeError: 'int' object is not subscriptable   this backend: exit 1,
print([[1][0][0], 1])      #  `codegen: index requires an inline list/dict/set literal`
```

The fold is right and was never the bug: `[1, 2][0]` IS the integer 2, and `listLiteralFoldAnswers` is the
question that licenses saying so (every element an integer constant, the position a constant). What the fold
leaves behind is an AST the outer subscript cannot read — its `Obj` is still the inner `Index` node, while the
program has by then written `1[0]`. The subscript door asked its gates (`numericElemUse`, the pair doors) and
each declined, and the tail of the road was a sentence about an inline literal.

That sentence is a dead end for the reader and wrong about the program. The program did write an inline
literal; it also wrote a subscript of what that literal read gave it, and the reference's answer to THAT is a
`TypeError` naming the kind, catchable by the program's own `except TypeError:`. Three sibling accidents
already work this way — a key that is not there (**ADR 0326**'s `KeyError`), a position past the end
(**ADR 0330**'s `IndexError`), an index that is not a position (**ADR 0329**'s `TypeError`) — all emitted by
`constantTrap` so the enclosing `try:`'s landing pad is reached and the verifier never sees an instruction
after a terminator. The fold's `not-subscriptable` case was the one left refusing.

Measured on two builds, both quoted in the roadmap row: on `b276ddf` (this commit's parent) the two programs
exit **1** with that sentence, on this build they exit **3** with the reference's class and message, catchable.

## Decision

**1 — The subscript door walks the fold's answer one level.** `(*irGen).constantSubscriptKind`
(`pkg/lang/constant_trap.go`, beside the sentences it quotes) names the constant a subscript is really
standing on: a literal `int`/`float`/`bool`/`None` answers itself, and an `Index` answers the element the fold
would have produced — `foldedLiteralElemOf`, which asks the *same* `listLiteralFoldAnswers` question
valueIndex asks before folding, and declines a literal it cannot read, a non-constant position, and a position
out of range (the pair door and the bounds check own those, and this road exists only for the accident the
fold has already decided). `valueIndex`'s two tails — the `*Call` base and the `default` base — ask it after
`numericElemUse` declines, and emit:

```go
if kind, isConstant := g.constantSubscriptKind(n.Obj); isConstant {
    g.constantTrap(b, "TypeError", notSubscriptableSentence(kind), n.Span())
    return "0", nil
}
```

**2 — One sentence per kind, written once.** `notSubscriptableSentence(kind)` is now the single spelling of
`'int'/'float'/'bool'/'NoneType' object is not subscriptable`; the run-time tag-dispatch door in
`pkg/lang/heapargs.go`, which built the same string inline, quotes it too. A program that reaches the same
accident through the fold and through the object cannot get two messages, which is **ADR 0326**'s rule moved
one road over.

**3 — A TEXT is not in this list, on purpose.** `"ab"[0][0]` is `'a'[0]` to CPython and answers `'a'`: a text
is a container of one-character texts, so the character road keeps its arm and `constantSubscriptKind` names
no kind for a `*StrLit`. The control rows in the test tables pin both halves — the double text read answering
and the int read raising.

**4 — What this ADR does not claim, written into the suite as it was written here.** A read of a read out of a
comprehension (`print([[x for x in [1]][0][0]])`) still refuses with the old sentence (**Gap R.495**, pinned in
`pkg/lang/pair_element_read_test.go`'s filed table, where the pair-element residue lives); and `len` of a
literal takes the literal's COUNT without lowering its elements, so `print(len([[1, 2][0][0], 1]))` prints
`2` at the exit code that means the program ran where the reference raises, and `len([f()])` never performs
the call inside the literal (**Gap R.494**, pinned with today's bytes at both legs). Neither moved between
this commit and its parent, which is what makes them rows rather than parts of this one.

## Consequences

- The change is two tails of one road plus two helpers and one sentence helper; no new runtime function, tag,
  or ABI, and no new diagnostic code. The emitted difference is a `call @rt_die`(class `'TypeError'`, message
  `'int' object is not subscriptable`) plus the `fold.trap` label `constantTrap` opens, and the ABSENCE of the
  refusal text — both asserted in `TestTheFoldedSubscriptTrapIsEmittedAndTheRefusalIsGone`, which also fails if
  a compile-time container global reaches an operand position (**ADR 0188**, the failure mode that family
  reached by a different road).
- The trap ordering is the reference's: `print([[[1, 2][5]][0]])` raises the INNER `IndexError` — the
  accident the program wrote first — because the inner read is lowered by the door that owns bounds checks.
  A row pins it so a future widening of this road cannot quietly swap the two.
- `pkg/lang/pair_element_read_test.go`'s filed table lost its **Gap R.227** row and gained **Gap R.495**'s: a
  row that stops refusing moves to the answering table (`folded_subscript_trap_test.go`), and the table it
  leaves stays populated with what its door still cannot reach. That is the suite's own instruction ("the row
  fails the day the door grows"), followed literally.
- The record grows to **10005** sources: 72 of the family, written by `tools/recmerge` from the reference with
  the compiled leg cross-checked and none refused, plus the conformance program's own text. The recorder
  REFUSED three sources before they were excluded from the batch — the two `len` shapes and the comprehension
  shape — which is the same evidence that filed **Gap R.494** and **Gap R.495**: the artifact-writing mode is a
  detector, not a scribe (**Gap R.493**'s lesson, one cycle old).
- `docs/language.md` now states the rule where comprehensions and subscripts are described, naming the raise
  and the catchability, and the two sentences that described the refusal were deleted rather than edited.

## Alternatives rejected

- **Refuse with a better sentence.** The refusal's text was the least of the problem: a program cannot catch a
  build, and the reference does not refuse. **ADR 0326** settled this class for keys and positions — a fold
  that notices an impossibility emits the trap and returns the unreachable word — and a fourth sibling
  refusing instead of raising is an inconsistency, not a judgement.
- **Fold the whole chain in the semantic pass so the outer subscript sees an `IntLit`.** Rejected: the folder
  is shared with the checker, and rewriting a node whose subexpression has a *trap* to a plain constant is how
  a program loses its own raise — the trap is emitted code, and a fold that discards the lowering that emitted
  it silently turns a catchable exception into no exception at all. This ADR keeps the AST alone and asks the
  question at the door that must answer it.
- **Answer the outer read from the pair door (`runtimeSlotPair`/`taggedSlotPair`) instead.** Rejected on
  measurement: those doors decline exactly the shapes the fold answers (`runtimeSlotPairDeep` refuses a
  literal the fold can read, by **ADR 0331**'s "the fold's own shape stays the fold's answer" rule), so
  widening them would put two roads in charge of one literal's slots — the failure **ADR 0249** and **ADR
  0232** exist to prevent. Where the pair door owns the read (a list of texts, a slot out of a built
  container) it keeps answering, and the controls pin it.
- **Make the text case raise too.** Rejected: CPython answers `'a'` for `"ab"[0][0]`. Naming `*StrLit` here
  would have shipped a raise where the reference prints, which is the worst thing this backend can do.
- **Land the `len` fix in this commit.** Rejected and filed as **Gap R.494**: the count door and the subscript
  door are two doors, the defect is that the count door never lowers the elements at all (a call inside the
  literal is skipped too), and fixing it means deciding what a literal's elements contribute when their values
  are discarded — a commit of its own, with its own measurements.
- **Silently drop the `print([[x for x in [1]][0][0]])` shape from the sweep.** Rejected: it is **Gap R.227**'s
  shape one builder over, it measures identically on the parent and this build, and the filed table is where a
  refusal that owes a raise lives — visible, with the reference's answer written beside it.

## Note on this cycle's method

The two-build measurement caught the cycle twice before it shipped. The first draft of the fix added the arms
to a stale binary in `/tmp` and reported the family as fixed; rebuilding from the tree showed `print([[1, 2][0]
[0], 1])` still refusing and `print([[x for x in [1]][0][0]])` refusing on BOTH builds — the latter is why
**Gap R.495** exists instead of a false "fixed" claim. The recorder then refused three programs and named them,
which is how **Gap R.494** was found: no test in the repo had ever asked `len` about a literal whose element
raises, because no program had ever written one.
