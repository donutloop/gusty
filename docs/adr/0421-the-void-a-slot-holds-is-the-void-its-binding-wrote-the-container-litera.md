# The void a slot holds is the void its binding wrote: the container literal asks the ELEMENT, not the spelling

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.435** (`k = None` / `print([k])` wrote **`[0]`** at **exit 0**
where the reference writes `[None]`, while `print([None])` wrote `[None]` and `ys = [None, 1, "a"]` /
`print(ys)` wrote `[None, 1, 'a']` all along). Filed by **ADR 0420**, which two commits ago gave the READ
door exactly this arm and left the literal beside it; byte-identical on ADR 0420's parent (`2b49a2a`) and on
the build before that (`7cbffb5`), which is the evidence that the commit that found it did not write it.

One commit, one feature: one gate arm in `pkg/lang/heapargs.go` (`noneSlotExpr`, the void arm of
`literalNeedsTags`, asked in the element position and the dict-key position), one door arm beside it
(`elemKindTag`'s arm for the void a CALL answers), the two test files, one conformance program, and four rows
filed from what the same measurement surfaced (**Gap R.437**, **Gap R.438**, **Gap R.439**, **Gap R.440**).

## Context

```
k = None
print([k])            #  [None]     reference
                      #  [0]        this backend, at the exit code of success
print([None])         #  [None]     — the spelled singleton was answered all along
ys = [None, 1, "a"]
print(ys)             #  [None, 1, 'a']  — and so was the mixed road
```

Two lines of the same program, two answers, and the difference is whether the void was WRITTEN or NAMED.
That is the signature of a gate that reads the source text next to a road that reads the object.

A container literal is built one of two ways. If every slot can live in an `i32`, the emitter folds the whole
literal into a module-level static array, `@.lstN = private global {i32, [N x i32]} { i32 N, [N x i32] [...] }`
— cheap, and what `sum`, `min` and `max` reduce from (**ADR 0239**). If a slot cannot live in a bare word, the
literal is built on the heap with a per-slot TAG beside each payload, and the printer walks the pair
(`rt_print_list_mixed`). The gate that decides is `literalNeedsTags`: *can every element be read back without a
tag of its own?*

Its arms ask a question of the element, and two of them had long since stopped asking the spelling. The float
arm is `floatSlotExpr`, which is `isFloatLitExpr(e) || g.isFloat(e)`; the verdict arm is `boolSlotExpr`, which
is `*BoolLit || g.printsAsBool(e)` — **ADR 0259** added exactly that `|| g.printsAsBool(e)` because
`print([verdict(), 0])` was being built by the static path and printing `[1, 0]`. The void arm was the bare
literal test, `isNoneLitExpr`, and it had never been widened.

So a slot holding the void *through a binding* was not a void to the gate. The literal went to the static
array, whose element reader folds each element to an integer — and `foldConstInt` reads a void-bound name as
the payload the void happens to carry, which is `0`. The array was correct about the word it stored and silent
about the kind, and the printer had one word to read.

```llvm
; k = None / print([k]) — before: one word, no tag, no kind
  %_k = alloca i32
  store i32 0, i32* %_k
  %t1 = call i32 @rt_alloc(i32 1)          ; actually: no heap object at all — @.lst1, an i32 array
  ...
  call void @rt_print_list(i32 %t1, i32 0)  ; the single-kind printer, reading the payload
```

The gate was not simply missing an arm, and this is the second time that mattered. **ADR 0353** built the
matching arm for the dict's static key door and reverted it, because the roads that CALL that door were
LABEL roads — they decide which question to ask an operand — and an operand labelled from a half-informed
record answers wrong at exit 0 rather than refusing (**ADR 0419** is the story of paying that debt properly,
gate and door in one row). Here the failure appeared in the other direction: the first cut of this arm
widened the gate past its door. `elemsTaggable` — the answer to *can every slot carry its own tag?* — did not
know the void a CALL answers, so

```
def f():
    pass
print([f()])
```

went from a wrong answer (`[0]`, exit 0) to a refusal (exit 1, `a compiled list holds either strings or
numbers, not both`). A refusal is a worse witness than a measured wrong answer, because it hides the shape.
**ADR 0249**'s rule is the one this row had to satisfy: a gate and a door that test different things ship a
refusal nobody measured.

## Decision

**Ask the element, and ask it once.** Three arms, all of them the same question the neighbouring arms
already ask:

1. **`noneSlotExpr`** is the void arm of `literalNeedsTags`, and it is the sibling of `floatSlotExpr` and
   `boolSlotExpr`: the spelled `NoneLit`, or whatever `g.isNoneExpr(e)` answers — the ADR 0172 predicate that
   print, `str`, truthiness and equality already live by (the literal, a name whose latest binding wrote the
   void, and a body that never returns a value). The gate and the print road now ask ONE predicate, so the
   label and the value cannot disagree about whether an element is the void.
2. **The two exemptions `elemKindTag` carries, restated in the gate** — **ADR 0249** again, in the direction
   that matters. A name the unroll gave a per-item tag is THIS item, not the void some earlier binding left
   behind, so `k = None` / `for k in [1, 2]: print([k])` is `[1]`, `[2]` and not a refusal; and a name the
   container records claim holds a handle whose kind the object carries. Both exemptions are read from the
   same maps the door reads (`loopElemTag`, `nameIsContainerRecorded`), so a future widening moves both doors
   at once or neither.
3. **`elemKindTag` grew the arm the gate needed it to have**: the void a CALL answers is `TagNone`. Without
   it, the gate sends `[f()]` to the tagged road and the door declines to build it.

The payload is not trusted and is not printed: a `TagNone` slot's payload is forced to `0` by the builder, the
object says its slots are the truth (`rt_mark_estr …, i32 8`), and the printer is the one that walks the pair.
The emitted IR for the row's own program is now

```llvm
  %t1 = call i32 @rt_alloc(i32 1)
  call void @rt_tag_elem(i32 %t1, i32 0, i32 3)     ; TagNone — the kind, beside the slot
  call void @rt_set_elem(i32 %t1, i32 0, i32 0)     ; the payload, forced, and never printed
  call void @rt_mark_estr(i32 %t1, i32 8)            ; the object says: ask my slots
  call void @rt_print_list_mixed(i32 %t1, i32 0)
```

with the static array absent, which the IR case asserts in both directions — and for the numeric control, the
static road still answers with every slot tagged `TagInt` and the plain printer, so `sum`, `min` and `max`
kept the fold **ADR 0239** gave them.

**What this row is not.** It is not **Gap R.286**'s second half: that row's read door (`elemKindTag`) and this
row's literal gate are different doors asked at different times, and a row that fixed both would have one
evidence cell claiming two measurements. It is not the fold's debt either — `print(sum([None]))` still writes
`0` where the reference raises, and it is filed as **Gap R.438** rather than fixed here, because the fold's
road is `ADR 0239`'s and its own Definition of done names the runtime reduction it needs. And it is not the
frame's: `def g(v): return [v]` / `print(g(None))` still writes `[0]`, because the arm asks the FRAME's records
and a body's fresh map never received the module's — that is **Gap R.437**, filed with its sibling shapes.

## Consequences

* The literal and the print of the same element can no longer disagree, because they read one predicate.
  Every wrong answer of the `[k]` family that the record corpus holds is answered: 23 list/set/tuple/dict
  shapes and 13 call shapes in the unit file, all judged on the compiled leg AND the record leg.
* **`tools/recmerge` wrote 45 entries** from the reference, cross-checking the compiled leg (**9280 → 9325**),
  and one source was withdrawn from the batch rather than recorded — `bool([k])`, which the compiled leg
  refuses and **Gap R.131** owns. It is pinned as a refusal in both suites instead of acquiring an answer the
  recorder cannot certify.
* **One ledger row went paid, not edited**: `print([{"a": 1}.get("z")])` — the dict miss in a slot — was on
  `testdata/interpreter-golden-drift.json` as `stdout differs from the record: expected [None], actual [0]`,
  and now agrees. The ratchet fails a divergence that silently disappears, so the row left in the same commit
  that paid it (**318 → 317**).
* The gate is now the fourth container door to read the ADR 0172 singleton records — after `elemKindTag`
  (**ADR 0420**), the print road, and the equality door. `isNoneLitExpr` survives only as the spelled-literal
  half of `noneSlotExpr`; nothing else asks it, which the grep in the ADR's own review checked.
* Four rows are filed from the same sweep, each pinned with today's bytes beside the reference's in BOTH
  suites and each byte-identical on this row's parent: **Gap R.437** (the void reaching a slot through a
  frame's front door — a parameter, or the module's name read from inside a body), **Gap R.438** (the fold's
  void element: `sum([None])` is `0`, `sorted([1, None])` produces an order the reference refuses to
  compute), **Gap R.439** (a container the program NAMED, beside a number, prints its handle), **Gap R.440**
  (a loop item's own kind, put into a literal, prints its payload).
* A sweep of all **9325** record sources through a `git worktree` build of this row's parent reports **20
  changed sources: 18 improvements and 0 regressions**. Every improvement is this family — a void reaching a
  slot through a binding or a call and printing the object's answer instead of its payload — and the two
  remainders are `llc` rejections whose text carries the per-run temp directory, identical apart from that
  path. That is the honest shape of this change: one gate arm, one road fixed, nothing else moved.

## Alternatives rejected

* **Widening the gate without widening the door.** Measured, and rejected on the measurement: `[f()]` becomes
  a refusal. The pair is the unit of this change, and the gate/door agreement case in the unit file asserts it
  as a property over whole literals rather than over the programs that happen to be convenient.
* **Answering the tag question from the fold (`foldConstInt`) instead of the record.** The fold reads a void
  as `0`; that is the defect, not the oracle. Any arm built on it re-derives the payload-as-value confusion
  this row exists to remove.
* **Making every container literal a heap object.** It removes the class of bug by removing the fast road, and
  it costs `sum`/`min`/`max` their fold (**ADR 0239**) and every numeric loop its static array. The gate is
  the design; the fix is an arm in it.
* **Binding the void through the pair road** (`%_k` plus `%_k_tag`) so the literal could ask the binding for a
  tag — the alternative **Gap R.286** rejected for the same reason: it changes a representation for a value
  that the object-level answer already describes, and it would leave every other door still asking a record.
* **Fixing the fold and the frame in this commit.** Both are one-arm-looking changes to doors whose Definition
  of done is wider (**ADR 0239**'s fold, **ADR 0172**'s per-frame records), and both were measured unchanged
  on the parent — which is what makes filing them honest rather than convenient.
* **Recording `bool([k])` from the reference to make a CLI row pass.** The recorder refuses exactly that, and
  for the right reason: an entry the compiled leg will not agree to is debt, not coverage. The row that owns
  it (**Gap R.131**) is named in the pin instead.
