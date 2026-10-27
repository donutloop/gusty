# A loop item's kind is kept once per iteration, so one predicate answers it — the print door and the four gates beside it

## Status

Accepted — 2026-10-27. Closes **roadmap Gap R.440** (filed by **ADR 0421**), and files what the same probe
found and did not fix: **Gap R.445** (the interpolation field of an item), **Gap R.446** (the fold over a
fractional item), **Gap R.447** (the `for` road's `i32` store into the `double` slot the float road declared).

```
for k in [None, 1.5]:
    print(k)      #  None   1.5    reference — and this backend, since Gap R.358
    print([k])    #  [None] [1.5]  reference
                  #  [0]    [1]    this backend, at the exit code of SUCCESS
```

One commit, one feature: one reading of one map (`loopItemTag` / `loopItemIsThis` / `loopItemKindTag` in
`pkg/lang/heapargs.go`), asked by the four gate arms that had been asking the name instead, and two new test
files. No runtime function, no IR shape, no printer moved.

## Context

The unrolled `for` road keeps a per-iteration record of what the item is:

```go
// pkg/lang/codegen_for.go — once per element, before the body is emitted
g.loopElemTag[loopSlot] = g.elemTagFor(el, isStr)
```

That record is why `print(k)` answers `None` and `1.5` for the program above: the print door reads
`loopElemTag` (**Gap R.358**), chooses the mixed printer, and renders the item by its tag. The builders' own
tag question — `elemKindTag`, `elemTagFor` — grew the same arm in the same row.

What did not grow it were the four **gates** that stand in front of the builders, in `literalNeedsTags`, the
predicate that decides whether a container literal is the static `{count, [N x i32]}` array or a heap object
whose slots say what they hold:

| gate arm | what it asked | what an item of a `for` looks like to it |
|---|---|---|
| `floatSlotExpr` | `isFloatLitExpr` then `g.isFloat(e)` | a name; the float record belongs to the name's *older* binding |
| `boolSlotExpr` | `*BoolLit` then `g.printsAsBool(e)` | a name, and a name is not a `BoolLit` |
| `noneSlotExpr` | `isNoneLitExpr`, then `g.isNoneExpr(e)` — with a blanket "is this a loop item, then no" exemption **ADR 0421** had to add | the item's own void was answered `not a void` |
| container arm | `isContainerLiteral(x)` | a spelled literal only; a name holding a list is not a literal |

So one value was answered by two doors, and only one of them had been told what the item is. Twelve shapes
were wrong at exit 0 — `[0]`/`[1]` for `[None]`/`[1.5]`, `[0]`/`[2]` for `[[1]]`/`[[2, 3]]`, `[1]`/`[0]` for
`[True]`/`[False]`, `[0]`/`[2]` for the box handles of `[1.5]`/`[2.5]` — and four more refused in words
(`str([k])`, `f"{[k]}"`, `[k, k]`, `[k, 1, 2]`) while their `print(k)` twin answered. **ADR 0249**'s rule — a
gate and the door behind it must test the same thing — had been satisfied for the void by writing an
*exemption* in one arm rather than by making the two read one record; this row is that debt, plus the three
arms nobody had touched.

## Decision

**One reading of the record, asked by both sides.** Three functions in `pkg/lang/heapargs.go`:

```go
func (g *irGen) loopItemTag(e Expr) (int32, bool)          // the record, and whether it speaks at all
func (g *irGen) loopItemIsThis(e Expr, tag int32) (bool, bool) // answered? and is the item THIS kind?
func (g *irGen) loopItemKindTag(e Expr) (int32, bool)      // the door's half: self-describing kinds only
```

`elemKindTag` and `elemTagFor` — which had carried their own copy of the loop arm since **Gap R.358** — were
rewritten to call `loopItemKindTag`, so the reading exists once. Each gate arm asks `loopItemIsThis` **before**
it asks the name:

```go
if answered, yes := g.loopItemIsThis(e, int32(TagFloat)); answered {
    return yes
}
return g != nil && g.isFloat(e)   // the name's own records, for a name that is not an item
```

**The `answered` half is the whole of the difference between answering and refusing.** A numeric item has a
record (`TagInt`) that is not self-describing, and a helper that reports "no record" for it sends the gate on
to the name's stale records. Measured: `k = None` / `for k in [1, 2]: print([k])` — the exact program
**ADR 0421** pinned as a control — came back as a *mixed-list refusal* (`a compiled list holds either strings
or numbers, not both`) on the first cut, because `noneSlotExpr` read the old void claim. The record therefore
answers "this name **is** an item" whatever the item is, and only the door's half filters to the kinds a
compiled word cannot render. That is **ADR 0421**'s exemption restated as a reading instead of a special case.

**The container arm is separate and narrower.** `loopItemIsContainer` claims `TagList`, `TagDict`, `TagSet`,
`TagInstance` — the kinds whose payload is a heap handle — and joins `slotsItself` beside
`isContainerLiteral`, so `for k in [[1], [2, 3]]:` builds `[[1]]`, `[[2, 3]]` rather than `[0]`, `[2]`.

**The fold is not touched, and had to keep not being touched.** `TagInt` and `TagStr` answer `false` from
every arm, which is what keeps **ADR 0239**'s static array reachable by a numeric loop: `for k in [1, 2]:
print(sum([k]))` prints `1`, `2` and its main region carries no `rt_tag_elem` and no `rt_print_list_mixed`.
The Definition of Done demanded the fold's census in the same commit as the widening, and `sum`, `min`, `max`
each have a row of their own in the control table.

## Evidence

* **Probe**: 46 shapes, a `git worktree` build of the parent (`git worktree add -f /tmp/basewt HEAD` — the
  tree is never stashed, because another process may be building it) beside this build and `python3`:
  **16 newly answering / 0 regressed / 30 already correct**. Twelve of the sixteen were wrong answers at
  exit 0; four were refusals in words.
* **Record sweep**: all **9421** sources of `pkg/lang/testdata/interpreter-golden.json` through both builds,
  answers compared — **9421 unchanged / 0 improved / 0 regressed**; the two lines that differ are `llc`'s
  temp path inside an error string. The sweep is also what caught the mixed-list regression above, which no
  probe had thought to try.
* **Unit** (`pkg/lang/loop_item_literal_test.go`, new): **28** answering rows, each on the compiled leg AND
  the record leg (`compileAndRun` + `HasGoldenAnswer`/`runGoldenStdout`); **12** control rows including the
  fold's census; **3** IR rows (TagNone `3`, TagFloat `1`, TagList `5` written beside the payload, plus the
  mixed printer — and both ABSENT from main for the numeric control, read off `mainRegion` because the
  runtime's own definitions call those functions); **9 × 6** gate-and-door property rows, one per tag the
  unroll can record, asserting the four arms agree with the door and that the gate never sends a literal the
  door declines to build (**ADR 0249**'s implication, the direction that can break a program); **7**
  filed-not-fixed rows.
* **Integration** (`integration/loop_item_literal_test.go`, new): **34** CLI rows against a live `python3`,
  **6** pinned wrong answers with the reference's bytes asked inside the same case, **1** refusal row asserting
  exit 1 with the missing half named and never exit 2 (**ADR 0166**).
* **Record**: `tools/recmerge` wrote **33** entries from the reference with the compiled leg cross-checked
  (**9421 → 9454**) and **REFUSED three** whose compiled leg disagrees with the reference — every one of them
  a shape of **Gap R.447**, pinned instead of recorded.
* **Pins moved, not deleted**: `pkg/lang/void_slot_literal_test.go` and `integration/void_slot_literal_test.go`
  each carried the row's program in a refusal/wrong-answer table; both rows now sit in answering tables, and
  the vacated tables say where they went.

## What this row leaves owed, pinned with today's bytes

| shape | today | reference | owner |
|---|---|---|---|
| `for k in [None, 1.5]:` `print(k == None)` | `False`, `False` | `True`, `False` | **Gap R.416** (the compare door asks the name) |
| `for k in [None, 1.5]:` `print(k in [None, 1.5])` | `True`, `False` | `True`, `True` | **Gap R.416** (its third spelling) |
| `for k in [None, 1.5]:` `print(f"{k}")` | `0`, `0` | `None`, `1.5` | **Gap R.445** (the interpolation field) |
| `for k in [1.5, 2.5]:` `print(sum([k]))` | `0`, `1` | `1.5`, `2.5` | **Gap R.446** (the fold's census) |
| `k = 1.5` / `for k in [1, 2]:` `print(k)` | `1.5000000000000002`, `1.5000000000000004` | `1`, `2` | **Gap R.447** (the slot) |
| `k = 1.5` / `for k in [1, 2]:` `print([k])` | `[1]`, `[0]` | `[1]`, `[2]` | **Gap R.447** |
| `for k in [None, 1.5]:` `print(bool([k]))` | refusal, exit 1 | `True`, `True` | **Gap R.131** |

Every row above is byte-identical on this row's parent except the last of the `k = 1.5` pair, whose bytes
changed while its wrongness did not: the parent printed the stale box handle, this row prints the int payload.
Same defect, different symptom — which is why it is filed rather than smoothed.

## Consequences

* The `for` road's per-item record is now the single source for "what is this item", and a door that reads it
  is `loopItemKindTag`'s caller. A future door adds an arm to that predicate, not a fourth copy of the map read.
* `loopItemIsThis` answers `(answered, yes)` rather than a bool, and the doc comment says why: an arm that
  collapses the two answers becomes the mixed-list refusal the sweep caught.
* **ADR 0421**'s `noneSlotExpr` exemption survives as behaviour, not as code: the case it protected is a
  control row in `TestTheNumericLoopItemKeepsTheFoldsStaticArray`, and in the record (`k = None` /
  `for k in [1, 2]:` `print([k])` → `[1]`, `[2]`).
* The corpus grew by 33 sources, so every answer this row claims is checkable against the record leg without
  running the compiler.

## Alternatives rejected

* **Widening `isFloat`/`isNoneExpr`/`printsAsBool` themselves.** Those predicates are the *name's* records and
  are asked by the print door, truthiness, equality and the f-string road too; making them read the loop record
  would answer for the name everywhere, which is exactly the confusion **ADR 0172**'s latest-binding rule and
  **Gap R.447** are about. The item and the name are two questions.
* **A `taggedVars` entry written by the `for` road** (route every item through the pair road). That would put
  the numeric loop's literal on the tagged road, which is **ADR 0239**'s fold road for `sum`/`min`/`max` — the
  same reason **Gap R.440** was filed apart from **Gap R.435**, and the DoD pins the fold in this commit.
* **Keeping the exemption and adding three more** (`if _, perItem := g.loopElemTag[...]; perItem { return false }`
  in each arm). Four copies of one rule, each able to drift — the drift is how the row got filed. One predicate
  read by gate and door is the version **ADR 0232** and **ADR 0249** already legislate for.
* **Fixing the item's SLOT in the same commit** (**Gap R.447**). It is a different road — the store, not the
  gate — and it needs the float road's slot declaration to agree with the item's kind, which is a change to the
  module's memory layout, not to a predicate. Bundling it would have made this row's 9421-source sweep
  unreadable, since both would have moved.
