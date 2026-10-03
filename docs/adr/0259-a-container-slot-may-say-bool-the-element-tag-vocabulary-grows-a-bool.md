# A container slot may say `bool`: the element tag vocabulary grows a bool

**Status:** Accepted
**Date:** 2026-10-03
**Roadmap:** roadmap L11.1 (tagged value word, remaining item 2), closes Gap R.112 and files Gaps R.116–R.119
**Related:** ADR 0182 (one tag table), ADR 0184 (a tag word per element), ADR 0232 (mixed dicts and
sets, whose comment promised this change), ADR 0257 (a verdict is printed from the expression that
made it), ADR 0258 (the rendering pair writes to a switchable sink), ADR 0233 (a float slot is a box
plus a tag)
**Evidence:** `pkg/lang/bool_element_test.go`, `integration/bool_element_test.go`,
`integration/programs/probe_bool_in_a_container.gy` (promoted to the parity corpus),
`pkg/lang/mixed_list_test.go`, `pkg/lang/mixed_dict_set_test.go`,
`integration/slot_order_object_test.go`, `integration/tagged_numeric_test.go`,
and the four probe programs the residuals below name

## Context

`print([True, 1])` printed `[1, 1]` — on **both** backends, which is exactly why the conformance
matrix was green. CPython prints `[True, 1]`. Gap R.112 recorded it, and ADR 0257 recorded why the
print rule could not fix it: a print site still has an AST to ask ("is this expression a verdict?"),
and a container slot does not.

The representation was not missing the name. `value.go`'s canonical tag table has had `TagBool` since
ADR 0182, and the runtime already knew how to write `True` and `False` (`rt_print_bool`,
`rt_bool_text`, from ADR 0257). What was missing was permission:

```go
case *BoolLit:
    // Both backends store a bool as the number it behaves like today … when L11.1 gives bool
    // its own kind this line returns TagBool and every container follows (ADR 0232).
    return int32(TagInt), true
```

That comment had been a promise for two cycles. Meanwhile three predicates — `taggableMixedList`,
`taggableMixedSet`, `taggableMixedDict` — each kept its own list of "tags that need the tagged build
path", and the float and None arms were transcribed differently in each. That transcription is why
`[True]` was the easy case and `{"k": True}` and `{True, 1}` were still *refused outright*:

```
print({"k": True})  →  codegen: a compiled dict holds either strings or numbers, not both
print({True, 1})    →  set literal elements must be constant integers
```

On the interpreter side there was no per-element anything: an element is an `int64`, `True` is the
`int64` 1, and a list of them is a `[]int64`. The precedent for "a value that has to outlive the
expression that made it" already existed in that heap — a float is a `kind:"float"` box whose payload
the read sites unbox.

## Decision

**1. One question, asked at the site that writes the slot.** `irGen.boolSlotExpr` is ADR 0257's
`IsBoolExpr` reached from an element rather than a print site: the literal, a comparison, a name whose
last binding was a verdict, or a call whose body returns one. The interpreter asks the same predicate
(`Evaluator.IsBoolExpr`) at its own write sites, through `slotVal`. Neither backend keeps a second
list of what counts as a bool.

**2. The compiled element tag grows a bool.** `elemKindTag` answers `TagBool`;
`slotTagSelfDescribing` includes it, so a container holding one has no container-wide kind and is
built through the tagged path; and the three container-shape predicates collapse into
`elemsTaggable`, which asks the tag table instead of restating it. `literalNeedsTags` became a method
so it can ask `boolSlotExpr` too — `print([verdict(), 0])` needed that, not just `[True]`.

**3. The gates on a literal ask both questions.** `value()`'s list/dict/set cases gated on
`literalNeedsHeap` alone — "does a payload not fit an i32?" — and a bool's does fit perfectly, which
is precisely the bug: the word fits and nothing beside it says what it is. The gates now ask
`literalNeedsHeap(n) || g.literalNeedsTags(n)`. This also un-broke a family that had never been
measured: `def half(xs): return xs[0] / 2` / `print(half([1.5]))` had been refused with "a compiled
container cannot hold a float yet" because a float-holding literal in a *value position* fell through
to the static global emitter. It answers 0.75 on both backends now, and the refusal test that pinned
it moved to the parity table.

**4. One rendering, reached three ways.** The bool arm is added to `rt_print_mixed_value`, which
print, the `str()`/`repr()` pair and container elements all call (ADR 0258), and it writes through
`rt_bool_text` into the same switchable sink. No second renderer, and no `printf` for the bool case;
`TestRenderPairIsOneTableNotTwo` still forbids one.

**5. A bool is still a number where the program asks a number.** `rt_payload_eq`'s numeric family
becomes int, float **and** bool, so `True == 1`, `True == 1.0`, `[True] == [1]`, `1 in {True}`,
`{1: 'a'}[True]` and `{True, 1}`-dedups-to-one keep CPython's answers. The interpreter's entry points
for arithmetic, ordering, equality, membership and `sum` unbox the bool box (`unboxBool`) — with one
deliberate exception: the *comparison* operators are not unboxed before the gate, because the
`TypeError` CPython raises for `xs[0] > "a"` names `'bool'`, and both engines now say that word.
The interpreter used to say `'int'`; the compiled backend already said `'bool'`.

**6. The interpreter mirrors the compiled representation, not a shortcut.** A bool stored in a
container slot becomes a boxed `kind:"bool"` object carrying the 0/1 — the same shape a float already
has — rather than a parallel `[]bool` beside `elems`. The GC's reclaim list grows the kind, and
`objKindTag("bool")` returns `TagBool`, so the two heaps read one table (ADR 0182).

**7. A landing may not break a green promise.** Making `[True, 1]` a tagged container silently routed
its numeric reads down the "this context needs a single static kind" refusal, taking
`xs = [True, 1]` / `i = 0` / `print(xs[i] + 1)` from a correct `2` to a refusal. `numericSlotUse` is
the second door for that read: when the literal that built the container is still the whole story and
every slot is int-or-bool, the payload is the number the arithmetic wants, whatever the index is. A
float among the slots closes the door, because there the payload is a box handle and converting it is
ADR 0249's job.

## Measured

Against `python3`, before → after, both backends. Before, every container line was the number.

| program | CPython | `--interp` (before) | `--aot` (before) | both (after) |
|---|---|---|---|---|
| `print([True, 1])` | `[True, 1]` | `[1, 1]` | `[1, 1]` | `[True, 1]` |
| `print({"k": True})` | `{'k': True}` | `{'k': 1}` | *refused: "either strings or numbers, not both"* | `{'k': True}` |
| `print({True, 1})` | `{True}` | `{1}` | *refused: "must be constant integers"* | `{True}` |
| `print({1: True})` / `print({True: 1})` | `{1: True}` / `{True: 1}` | numbers | numbers | CPython's |
| `xs.append(True)` / `print(xs)` | `[True]` | `[1]` | `[1]` | `[True]` |
| `xs = [1,2]` / `xs.append(1 == 1)` | `[1, 2, True]` | `[1, 2, 1]` | `[1, 2, 1]` | `[1, 2, True]` |
| `print(str([True, 1]))`, `print(repr([True, False]))` | as printed | numbers | numbers | CPython's |
| `xs = [True, 1]` / `print(xs[0])` | `True` | `1` | `1` | `True` |
| `for x in [True, 1]: print(x)` | `True`,`1` | `1`,`1` | `1`,`1` | CPython's |
| `[True] == [1]`, `True in [1]`, `1 in {True}`, `d[True]`, `sum([True, 1])`, `True + 1`, `xs[0] * 3`, `sorted([True, 1, False])` | unchanged | ✅ | ✅ | ✅ |
| `xs[i] > "a"` of a bool slot, the `TypeError` | names `'bool'` | named `'int'` | named `'bool'` | both name `'bool'` |
| `print([x for x in [True, 1, 1]])` | `[True, 1, 1]` | `[True, 1, 1]` | **`[1, 1, 1]`** | `[True, 1, 1]` |
| `print([x for x in [True, 1] if x])` | `[True, 1]` | `[True, 1]` | **`[1, 1]`** | `[True, 1]` |
| `print([True if y else False, "a"])`, `print({y: 1})` where `y = 1 == 1` | `[True, 'a']`, `{True: 1}` | numbers | numbers | CPython's |
| `--json --eval 'xs=[True,1]; xs[0]'` | — | `"type": "int"` | — | `"type": "bool"` |
| `def half(xs): return xs[0] / 2` / `print(half([1.5]))` | `0.75` | `0.75` | **refused** | `0.75` |

## What this did not fix, filed rather than absorbed

Four shapes lost the name again in measurement. Three are the *same* root as Gap R.115 — a value whose
kind no expression and no tag names — reached from a different operator; the fourth is a defect the
sweep found beside them, which is not about bools at all and is filed on its own.

* **Gap R.111** (already open): `def show(f): print(f)` / `show(True)` — a parameter is a fresh
  binding and the caller's AST does not travel. `programs/probe_bool_through_a_call.gy` stays in the
  ledger; the CLI exit-code-contract row and the oracle stub check moved onto this program, because a
  contract row pointed at a closed debt asserts nothing.
* **Gap R.116** (new): a comprehension that builds verdicts. The **list** is paid by this ADR — see the
  note below — but `{True for x in [1]}`, `{1: True for x in [1]}` and `{True: 1 for x in [1]}` refuse
  on the compiled leg, because a set or dict comprehension folds into a compile-time global and a
  global has no `@heap_tags` row to write: the old fold printed `{1}`, `{1: 1}` and `{1: 1}`, which is a
  wrong answer wearing the same clothes as the defect this ADR closes, so the fold now declines and
  names the half it is missing. The interpreter answers all four lines. `programs/probe_bool_in_a_comprehension.gy`,
  ledger `debt`, exit 6.
* **Gap R.117** (new): `print(max([True, 0]))` → compiled `1` (the fold returns the candidate's number),
  interpreter and CPython `True`; same for `min([False, 1])`. The third line of that program,
  `ys[0] and ys[1]`, is the row that must **not** move: CPython hands back the operand there, and its
  number, so all three engines print `1`. `programs/probe_bool_chosen_by_an_operator.gy`, ledger `debt`,
  exit 6.
* **Gap R.118** (new, pre-existing and found here): `{1: 2 for x in [1, 2]}` keeps **both** entries in
  the interpreter — `{1: 2, 1: 2}` and `len` 2 — where CPython and the compiled fold replace the value
  and keep the size. Nothing in the program is a verdict; the dict comprehension's fold appends entries
  where a dict needs a lookup. `programs/probe_dict_comprehension_duplicate_key.gy`, ledger `debt`,
  exit 6.
* **Gap R.119** (new, pre-existing and found here): `xs = [1]` / `print(1 if xs[0] > "a" else 0)` —
  CPython and the interpreter raise `TypeError`, the compiled backend folds the ternary's condition and
  prints the true branch. The int spelling predates this cycle; the bool spelling is what found it.
  `programs/probe_slot_order_in_a_ternary.gy`, ledger `not_applicable` (CPython has no opinion to give),
  exit 7.

**A comprehension's element is the item, not the expression.** `print([x for x in [True, 1, 1]])` was
compiled to `[1, 1, 1]` even after the literal rule was fixed, because the element expression is the
loop variable `x`, which says nothing about a kind, and the constant fold re-synthesised every element
as an `IntLit` — the same trap ADR 0244 recorded for `None`, `float` and `str` elements (`foldsToAnInteger`
declines those; `*BoolLit` was still on the "folds to a number" side). The rule now reaches one level
indirect: `compElemCopiesABool` asks the *item* when the element is only a copy of the loop variable,
the fold declines, and `runtimeCompList` writes the slot through `rt_append_tagged` with the tag the
item gave. `print([x for x in [True, 1] if x])` and the promoted program's last line are the rows.

## Alternatives rejected

* **Ask the AST at the read site.** There is no AST at a read: `xs[0]` is a slot in an object that
  outlived the expression that filled it. This is the arrangement ADR 0257 already drew, and pushing
  it further would mean re-deriving provenance at every use.
* **A parallel `[]bool` beside `elems` in the interpreter.** Cheaper to write, and it drifts: `pop`,
  `remove`, `insert`, `sort` and every future mutation have to shift it in step, which is the
  bookkeeping this project has repeatedly had to retrofit (ADR 0187's pairing rule, ADR 0232's tag
  arrays). The float box shows what the alternative costs — nothing, because the payload travels with
  the value.
* **Box every bool, everywhere.** That is L11.1's item (2) at full width: truthiness, `--json`,
  f-strings, `is`, error messages and every arithmetic path change at once. This cycle boxed a bool
  only where it enters a container slot, which is where the name was actually being lost.
* **Let the compiled backend be right on its own.** `print(xs[0])` on a tagged bool slot was going to
  answer `True` in the compiled leg while the interpreter answered `1`. A parity corpus cannot ship
  that, and "file the divergence" is for behaviours neither engine can settle yet, not for one engine
  declining to catch up.
* **Refuse the numeric read of a tagged bool slot.** Cheapest option on the page, and it would have
  turned a passing program (`xs[i] + 1 == 2`) into an exit-1 refusal. The second door is a few lines.
* **Keep three tag lists.** They were three, they had already drifted, and the drift is the reason two
  of the six shapes in this record were refusals rather than wrong answers.

## Consequences

* `print`, `str()`, `repr()`, membership, equality, ordering, `sum`, sorting and `--json` now agree
  with CPython about a bool in a slot; the interpreter's heap gained a fourth boxed kind, which the
  collector knows.
* A literal in a value position is built as the heap object its slots need, so the float/None-holding
  containers that used to be refused in argument position compile.
* The tag vocabulary is asked in one place, so the next kind added to `value.go` cannot be true for a
  list and refused for a dict.
* Docs: `docs/language.md` § Dicts & sets / § Builtin functions (a bool is a value in a container),
  `docs/operations.md` (`--json` element types, the promoted program and the four new probes),
  `README.md`, `_001_session_learnings.md`, and `roadmap.md` closes Gap R.112 and opens R.116–R.119.
