# The comparison asked of an unrolled loop item asks the ITEM — one (payload, tag) pair, loaded not rebuilt, asked by the equality door and the membership scan alike

## Status

Accepted — 2026-10-28. Closes **roadmap Gap R.416** (filed 2026-10-24 by **ADR 0411**, measured beside the
alias battery of **Gap R.415**). Turns six wrong verdicts over four questions — `v is None`, `v == None`,
`v != None`, `v in [...]` over a literal list — into the reference's answers, all of them previously printed at
the exit code of success, and leaves nothing owed that is this door's own: what the same probe found and did not
fix is **Gap R.448** (the ordering door never
asks the item), **Gap R.449** (the condition road folds `x == None` from the payload words while the value
road folds it from the source kinds), and **Gap R.450** (an item whose kind is a container, compared with a
container literal, is refused).

```
for v in [None, "a"]:
    print(v)                #  None   a      ← the print door, which asks the item's own record
    print(v is None)        #  False False   ← the compare road; the reference: True,  False
    print(v == None)        #  False False   ← the same fold;    the reference: True,  False
    print(v != None)        #  True  True    ←                     False, True
for v in ["a", None]:
    print(v in ["a"])       #  True  True    ← the void joined the text list;  True, False
for k in [None, 1.5]:
    print(k in [None, 1.5]) #  True  False   ← a float failed to be its own member; True, True
```

One line of one program was right and the four beside it were not, which is the whole shape of the row: the
print door had been taught to read the unroll's per-item record and the compare road had not.

One commit, one feature: one question — *which `(payload, tag)` pair does this item carry?* — asked of the
item by the doors that compare it, in the three places that question was being answered without it:
`taggedOperand` and `isTaggedCompareSide` in `pkg/lang/heapargs.go`, and the literal-haystack membership road
in `pkg/lang/codegen_binop.go`. No new door is written, and no comparison road is widened: the pair comes out
of the door the printer and the truth road already ask.

## Context

An unrolled `for` over a literal list emits one body copy per element (**ADR 0233**, `codegen_for.go`). Before
each copy it stores the element's payload into the loop variable's slot and records the element's KIND in
`loopElemTag` — a compile-time fact about the copy being emitted, not a run-time word. **Gap R.358** taught the
print door to read that record (a float's payload is the handle of a `@float_box`, the void's is nothing at
all, so printing the payload alone showed `1.5000000000000002`-shaped numbers and `0`s), and **ADR 0388**
taught the truth door (`truthAsksTheLoopItem`). **Gap R.440**/**ADR 0424** taught the four container-literal
gates beside them. What nobody had taught is the compare family, which does not consult the unroll's record at
all: it consults `isTaggedCompareSide`, whose notebook is `taggedVars` — written by the pair road, which the
literal unroll never goes through.

So one item was answered twice, and the second answer was a number nobody wrote:

| program | this backend | reference (CPython 3.12.3) |
|---|---|---|
| `for v in [None, "a"]:` / `print(v is None)` | `False`, `False` — exit 0 | `True`, `False` |
| `for v in [None, "a"]:` / `print(v == None)` | `False`, `False` — exit 0 | `True`, `False` |
| `for v in [None, "a"]:` / `print(v != None)` | `True`, `True` — exit 0 | `False`, `True` |
| `for v in [1.5, 2.5]:` / `print(v == 1.5)` | `False`, `False` — exit 0 | `True`, `False` |
| `for v in ["a", None]:` / `print(v in ["a"])` | `True`, `True` — exit 0 | `True`, `False` |
| `for k in [None, 1.5]:` / `print(k in [None, 1.5])` | `True`, `False` — exit 0 | `True`, `True` |

The six symptoms are two collisions, and both are the ones **ADR 0187** and **ADR 0232** exist to name. The
void's payload is `0`, and an integer's slot legally carries `0`; a text's payload is an index into
`@str_tab`, and the first string a program interns lands at index `0`. So the void item equalled the void only
by luck and the text item equalled the void by arithmetic, and the float item — whose payload is a box handle —
failed to equal its own literal. Every one of those printed at exit 0 with a traceback-free run, which is the
class this house treats as the compiler's own defect (**ADR 0166**), not the program's.

The neighbour that answered correctly was the same loop over a NAMED list (`for v in xs`). There the loop binds
the element through the pair road, so the name carries a `_tag` slot and `comparePair` can ask it. The literal
loop had recorded exactly the same fact in a different place, and the compare door had never been pointed at
that place.

## Decision

**Ask the item, through the one predicate that already reads the record.** `loopItemKindTag` (`heapargs.go`,
**Gap R.440**) answers "the kind of this item, when it is one a compiled word cannot render"; it is what the
print door and the truth door ask, and it is now what the three compare roads ask.

1. **`taggedOperand` answers the item as a LOAD plus a constant.** The payload comes from the name's own slot
   (`load i32, i32* %_v`); the tag is the recorded constant. It is deliberately NOT re-derived: the float arm
   below it would call `floatValue`, which for a name the float road never declared reads a `double` slot and
   `sitofp`s the handle; the container arm would call `containerOperand`, which would build a second object
   out of the number the slot holds. Loading is both cheaper and the only sound reading — the slot already
   holds the handle the element was built with (**ADR 0233**'s rule, applied to the reader instead of the
   writer). An IR row asserts this from the module: the register `@rt_payload_eq` is handed must be the one the
   load of `%_v` produced.
2. **`isTaggedCompareSide` admits the item.** That gate is what opens `mixedTaggedCompare`, which already
   answers `==`, `!=`, `is` and `is not` through `comparePair` → `@rt_payload_eq` for a popped slot (**ADR 0410**)
   and for the void (**ADR 0411**). It is asked in BOTH places the question is live — the value road
   (`print`, a binding, an argument) and the condition road (`if`, `while`, a ternary head, a short-circuit
   operand) — which is **ADR 0232**'s requirement that one question have one answer, and the reason the
   verdict rows cover a condition and a `while` head as well as a print.
3. **The literal-haystack membership scan takes the tagged call.** The untagged `rt_contains` compares payloads
   only; the haystack the road materialises is built by `containerOperand`, and every builder in this backend
   writes a kind beside every payload (**ADR 0187**), so `rt_contains_tagged` is sound and the untagged scan is
   not. The arm fires only when the needle is such an item and the haystack really is a heap object;
   otherwise the road it always used stays exactly where it was.

**What is deliberately NOT widened.** `loopItemKindTag` answers "unknown" for `TagInt` and `TagStr` — the two
kinds a compiled word renders on its own — so a numeric item keeps the source-side fold and **ADR 0239**'s
static array, and an interned-text item keeps the text road. That is the Definition of Done's second clause,
asserted from the emitted module in both directions: the void item's equality emits
`call i32 @rt_payload_eq(i32 %…, i32 3, i32 0, i32 3)` and the integer item's emits NO `rt_payload_eq` and NO
`rt_contains_tagged` at all. A table that asserted only presence could not tell a widening from a rewrite that
moved every program onto the new road.

**And what is refused rather than guessed.** When the needle is an item whose kind is a container and the other
side of an `==` is a container LITERAL, `containerEquality` still asks `isProvenScalarExpr` of the item and
refuses in words at exit 1. This row leaves that as **Gap R.450** instead of quietly answering it through a
door that was not built for it — the same judgement **Gap R.418** recorded for container identity.

## Evidence

- `pkg/lang/loop_item_compare_test.go` (NEW): **23** answered programs on the compiled leg AND the record leg
  (`compileAndRun` + `HasGoldenAnswer` + `runGoldenStdout`) — identity, equality, both negations, the
  `0`-vs-void payload collision from both orders, a fractional item that now equals its own literal, a verdict
  item that equals the number Python says it is, membership and negated membership over the very literal the
  loop unrolls from, a container item in its own iterable, four kinds asked four questions each, the named-list
  control **ADR 0411** already paid, and the int/text controls; **6** rows that put `print(v)` and `v == …` on
  ONE line so a divergence between the print door and the compare door shows up as a self-contradicting line
  rather than a missing line; **8** rows that move the item through the positions a compare can stand in — left
  side, right side, an `if` head, a `while` head, a short-circuit's operand, and the answer bound to a name —
  so the door cannot be open in only one of them; **12** door rows asked DIRECTLY over hand-built generator
  states — the seven self-describing kinds admitted by `isTaggedCompareSide`, answered by `taggedOperand` with
  a load of the item's slot and the record's tag, and refused for `TagInt`, `TagStr`, an unbound name, a spelled
  literal and a different name, plus a literal list that answers none of them at all; **6** IR rows in both
  directions; and **5** pins for what the row leaves owed, with today's bytes beside the reference's and the
  owning row named.
- `integration/loop_item_compare_test.go` (NEW): **28** family rows through the CLI with every expectation read
  off the live `python3` in the same case, **8** exit-code rows (no answer spends exit 2, no refusal is silently
  answered, no sentence leaks `*lang.`/`%!s(`/`__cans`), `--verify-llvm-file` on each shape, and **5** owed pins
  beside the reference's bytes.
- `TestTheComparedItemIsTheSameWordThePrinterPrints` reads the emitted module the other way round: it finds the
  register handed to `@rt_payload_eq` and requires it to be the one loaded from `%_v`. That is the difference
  between reading the unroll's record and re-deriving a value from it, and no behavioural case can tell.
- `tools/recmerge` wrote **48** entries from the reference with the compiled leg cross-checked
  (**9526 → 9574**) — none of these shapes may now be recorded from the backend that has to be tested
  (**ADR 0302**). It REFUSED one source: `def f(v): return v == None` called with `None`, whose compiled leg
  prints `False False` where the reference prints `True False`. That is a PARAMETER's kind rather than an item's
  — **Gap R.417**'s standing one row over — so it stays a pinned wrong answer with the row that owns it
  attached, and not a record the next cycle trips over.
- Pins MOVED rather than disappeared: the two `theVoidOwed()` rows this row owns are DELETED and stand in
  `theVoidAnswered()`, with their CLI twins moved into `voidQuestionFamily()` in the integration suite; the two
  `Gap R.416` rows in `pkg/lang/loop_item_literal_test.go` and `integration/loop_item_literal_test.go` leave the
  owed tables for the answered ones. And a pin that was not this row's at all went paid on the way through:
  **ADR 0425**'s owed row `the_loop_item_whose_slot_word_is_wrong` — `s = {1, 2}` / `for k in [1.5]:` /
  `s.discard(k)` / `print(len(s))`, pinned at `1` where the reference prints `2` because the removal was handed
  the number a box handle looks like — now prints `2`, because the removal asks the same `taggedOperand` arm
  this row wrote and the tagged match declines a float against an integer. It moved to `TestRemovingASetMemberAtTheCLI`
  with the reference's bytes. Deleting pins without moving them would have been the failure mode, not the fix.
- **The sweep is the witness for "only the doors I meant to change changed."** All **9526** record sources
  through a `git worktree` build of the parent (`28a0a7c`, **ADR 0425**) and this one, 24 workers, both
  in-process: **0 changed answers**, the only two differing lines being the `llc` temp path inside the same
  unchanged error string. Two consequences: nothing that answered before answers differently now, and the corpus
  had no case for this family at all — which is why the 48 new entries are the coverage, not the 9526.

## Consequences

- The item's pair is now a fact four doors read: print, truth, literal-building, comparison. A future door
  that wants an item's kind has one predicate to ask, and `gap-r-448` is written in that shape so the next
  cycle widens the ordering door instead of inventing a fifth reading of `loopElemTag`.
- An unrolled item is now a tagged compare side, so `isTaggedCompareSide` is no longer equivalent to
  "`taggedVars`". Anything that reads the notebook alone — the chain gate included (**Gap R.443**) — is now
  the odd one out, and is filed as such rather than left to drift.
- A program whose item is an int or an interned text compiles byte-for-byte as before. That is what makes the
  change safe to land under `make testparallel` without a migration: the widening is bounded by what the
  record can name, and the record cannot name a number.
- Three refusals/answers this probe surfaced are ledger rows with their own DoD, not comments (**Gap R.448**,
  **Gap R.449**, **Gap R.450**), each pinned in both suites so closing it fails a test rather than passing one.

## Alternatives rejected

- **Have the `for` road bind a `_tag` slot for the item, like the named-list road does.** Rejected: it makes
  one iteration's tag a run-time word that outlives the iteration — the exact bug **ADR 0172**/**Gap R.142**
  and **Gap R.447** are about (a stale kind read as a live one, and a slot whose declared word and stored word
  disagree under opaque pointers). The record is compile-time because the unroll is compile-time; the fix keeps
  the fact where the fact lives and points the doors at it.
- **Ask `floatValue`/`containerOperand` of the item's name inside the existing arms.** Rejected: it re-derives
  a value from its own address. `floatValue` on a name the float road never declared emits
  `sitofp i32 <box handle>`, which is **Gap R.447**'s `1.5000000000000002` wearing this row's symptom, and
  `containerOperand` mints a second object so two equal containers stop being equal.
- **Fold `v == None` from the source kinds on the compare side too.** Rejected: that is **Gap R.449**'s bug,
  imported deliberately. The fold is sound only where the compiler can state the operand's kind, and the whole
  point of this row is that the item's kind is a recorded fact the doors must read, not a spelling.
- **Widen the ordering door in the same commit** (`v < 2` over a float item is wrong too). Rejected under
  AGENTS's one-feature rule: the ordering road is a separate shape machine (`orderShapeOf`, `emitTaggedOrder`,
  per-kind raise sentences), and answering its rows needs CPython's raise wording per kind pair, which is a
  row of its own — **Gap R.448**, pinned with today's bytes.
- **Answer the container-item-against-container-literal refusal by routing it through `comparePair`.**
  Rejected: `containerEquality`'s scalar-proof is a different claim about a different position, and bending it
  here would answer one shape while making the "name no loop bound" case unprovable — **Gap R.450** owns it
  with the refusal pinned until then.
