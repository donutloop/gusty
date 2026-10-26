# The element a slot is WRITTEN with answers what it is: the third road that writes a container slot asks the ELEMENT, and the container is told

## Status

Accepted — 2026-10-25. Closes **roadmap Gap R.441**:

```
xs = [1]
xs[0] = 2.5
print(xs)          #  [2.5]      reference
                   #  [1]        this backend, at the exit code of success
xs.append(2.5)
print(xs)          #  [1, 2.5]   — the append road answered all along
```

Measured with the row's own parent as the "before" build, by `git stash`-ing the arm and rebuilding:
`2.5 → [1]`, `None → [0]`, `True → [1]`, `[2] → [1]`, `{"a": 1} → [2]`, `(1, 2) → [2]`, `{1,2} → [1]`,
`xs[0] = 2.5; xs[0] = 3.5 → [2]`, the loop over a promoted list → the payload, `xs[0] = 2.5; print(xs[0]) → 1`,
and `ys = [2]; xs[0] = ys → [0]` — **ten wrong answers at exit 0**, every one of them a container printing a
slot through a kind the container still claimed and the store had left behind.

One commit, one feature: one arm in `pkg/lang/codegen_index.go` (`assignIndex`'s plain-store road), the two
test files, one conformance program, and one row filed from what the same measurement surfaced (**Gap R.442**).

## Context

Three roads write a container slot, and they were not written on the same day:

| road | asks the ELEMENT its kind? | since |
|---|---|---|
| `xs.append(v)` | yes — `elemKindTag`, then `promoteMixed` when the tag describes itself | **ADR 0233** |
| `d[k] = v` | yes — twice, once for the key and once for the value, and again after the put | **ADR 0232** |
| `xs[i] = v` | **no** — it asked `heapElemKind`, i.e. *is this word text or a number* | this row |

What the item store got right, and did not need fixing, is visible in its own IR:

```llvm
; xs = [1] / xs[0] = 2.5 — parent build, the store half
  %t2 = fadd double 0.0, 2.5e+00
  %t3 = call i32 @rt_float_new(double %t2)          ; the float is boxed
  ...
  call void @rt_put_elem(i32 %_xs.ld1, i32 %t8, i32 %t3)   ; the handle goes in
  call void @rt_tag_elem(i32 %_xs.ld1, i32 %t8, i32 1)     ; and the slot is labelled TagFloat
```

Every word the object needed was written: the payload AND its tag, **ADR 0189**'s rule kept at this site.
The one word missing is the container's. A compiled container carries a census bit —
`rt_mark_estr(handle, 8)`, *"my slots are the truth, ask them"* — and it is the printer's decision: with the
bit set the emitter emits `rt_print_list_mixed`, which walks `(payload, tag)` per slot; without it, it emits
`rt_print_list`, which reads every slot as the one kind the compiler recorded for the container. The item
store reached for that bit only on the **contradiction** branch — text against number, the census `recordElemKind`
keeps — so a list that had only ever held integers stayed on the single-kind printer while a float box's
handle sat in slot 0 with a correct float tag beside it. `[1]` is that handle printed as a number.

```llvm
; the same program, parent build: the container never claimed its slots
  call void @rt_print_list(i32 %h2, i32 0)      ; one kind for every slot → the handle, printed
```

This is **ADR 0232**'s finding restated one road over: *"writing one slot is not the same statement as what
the whole list holds."* That row fixed the dict's entry store, and the same commit left this road's
`replaceElemKind` overwriting the container's claim from one slot's evidence — which is the other half of the
same bug, and this row closes the half that answers wrong at exit 0.

## Decision

**Ask the value, in the one place the other two stores already ask it.** In `assignIndex`'s plain-store road,
where the str/int census finds no contradiction, the element is asked its own tag and the container is
promoted when that tag cannot speak through a bare word:

```go
g.replaceElemKind(nm.Value, "list", sIsStr)
if t, ok := g.elemKindTag(val); ok && slotTagSelfDescribing(t) {
    if g.promoteMixed(b, h, nm.Value, "list", val) {
        g.floatFmtUsed = g.floatFmtUsed || t == int32(TagFloat)
    }
}
```

Four things are load-bearing in those four lines:

1. **The predicate is the neighbours' predicate.** `elemKindTag` is the function `append` asks (**ADR 0233**)
   and the dict store asks (**ADR 0232**); `slotTagSelfDescribing` is **ADR 0232**'s move widened by
   **ADR 0238** to the tags a compiled word cannot render at all — `TagFloat` (a `@float_box` handle),
   `TagNone` (nothing), `TagBool` (a bare 0/1), `TagList`/`TagDict`/`TagSet`/`TagInstance` (heap handles).
   `TagInt` is not on the list because its payload *is* the value, and `TagStr` is not because the interned
   index is what the str/int census already asks about. A fifth list of kinds would be a fifth chance to
   drift.
2. **It sits in the agreement branch, not before the census.** The contradiction branch already promoted and
   already had its own answer (`xs = [1]` / `xs[0] = "s"` printed `['s']` before this row); the arm runs where
   the census had nothing to say, which is exactly where the wrong answers were.
3. **`floatFmtUsed` travels with the float promotion**, the way it does on the dict's road — a container that
   can now print a double has to leave the module carrying `rt_fmt_double`.
4. **The arm is narrower than the door, by design.** `promoteMixed` will mark a container for any value
   `elemKindTag` can name; the row asks it to only when the payload cannot speak its own kind. **ADR 0249**'s
   rule is an implication — *whatever the gate sends down the tagged road, the door must be able to build* —
   and the unit file asserts it as a property over hand-built elements, not over the programs that happen to
   be convenient.

The row is paired with the census it must not disturb: **ADR 0239**'s static-array fold is why a numeric store
must still be free. `xs = [1]` / `xs[0] = 3` emits no `rt_mark_estr`, keeps `rt_print_list`, and the IR case
denies it both the bit and the mixed printer.

## Evidence

* **34 answered rows** in `pkg/lang/item_store_kind_test.go::itemStoreFamily` — every kind, every spelling
  (literal, binding, body's answer, container under a name), every reader that walks the object back
  (`str`, `repr`, `in`, `len`, `==`, `is`, a slice, `pop`, `reverse`, a `for`), run-time indices from a `for`
  and a `while`, and the same object under a second name — each judged on the compiled leg **and** the record
  leg.
* **13 control rows** the promotion must not touch, **4 bounds rows** (`xs[5] = 2.5` still raises the
  reference's `IndexError: list assignment index out of range`), and **14 gate/door property rows**.
* **4 IR rows**: the fraction, void and verdict stores assert `rt_mark_estr …, i32 8`, the tag word
  (`i32 1`/`3`/`2`) and `rt_print_list_mixed`, and deny the single-kind printer; the numeric control asserts
  the plain printer and denies the bit.
* **42 CLI rows** against a live `python3` (`integration/item_store_kind_test.go`), every expectation read in
  the case, plus a bounds table that compares the exception class and sentence rather than a traceback
  (whose file name is a temp path and whose source line the reference quotes and this backend does not).
* `integration/programs/item_stores_say_themselves.gy`, **43 lines**, registered as an asserted `match` row:
  **253 → 254** programs, matrix **239 → 240**, pass **210 → 211**, `match` **196 → 197**, fail 0, drift 0.
* `tools/recmerge` wrote **69 entries** from the reference, cross-checking the compiled leg
  (**9325 → 9394**), and refused four whose compiled leg disagrees with the reference — the stored `lambda`
  (**Gap R.150/R.151**), `sum(xs)` (**Gap R.71/R.107**), `sorted` over a promoted list (**Gap R.438**) and the
  stored `range` (**Gap R.293**) — which are pinned as today's bytes instead of acquiring answers the
  recorder cannot certify.
* A sweep of all **9394** record sources through **both** builds — this row's parent (the arm `git stash`-ed,
  rebuilt) and this row — reports **9352 unchanged, 40 improvements, 0 regressions**; the only other two
  differing sources are `llc` rejections whose text carries the per-run temp directory, identical apart from
  that path. Every one of the 40 is this family.
* No ledger row moved: none of the 40 is on `testdata/interpreter-golden-drift.json` or its `integration`
  twin, which is what the ratchet would have failed if it had been.

## What the promotion exposes (filed, not fixed)

A promotion is not free, and the row says what it costs. Three programs that used to answer now refuse, and
the measurement that makes that honest is that **their old answers were the stale payload speaking**:

```
xs = [1, 2] / xs[0] = 2.5 / print(0 < xs[0] < 3)   reference True   parent True   now REFUSED
xs = [1, 2] / xs[0] = 5.5 / print(0 < xs[0] < 3)   reference False  parent True   — the parent was never green
xs = [3, 1] / xs[0] = 2.5 / print(sorted(xs))      reference [1, 2.5]  parent [2.5, 1]  now REFUSED
xs = [1] / xs[0] = 2.5 / print(xs[0] + 1) / for i in [0]: xs[i] = None    reference 3.5, [None]   parent 2, [0]   now REFUSED
```

A chained comparison lowers each operand through an ordinary read and needs one static kind (**Gap R.82**'s
one-word position); the numeric courtesy **ADR 0249** grants a slot read works from the literal the container
was built from, and once a store with a run-time index owns the slots there is no literal left to consult.
Filed as **Gap R.442**, pinned with today's bytes beside the reference's in BOTH suites, and pinned *with* the
parent's bytes in its dossier so nobody reads the refusal as a lost answer. Refusals the same sweep re-named
are pinned beside it with their owners: `sum`/`min` over a container the compiler does not hold
(**Gap R.71**/**Gap R.107**), `count`/`index`/`remove`/`insert`/`join` over a mutated list (**Gap R.174**,
**Gap I.2**), the store through a subscript rather than a name (**Gap R.381**), the store from inside a body
over a module container (**Gap R.35**), a stored lambda (**Gap R.150**/**Gap R.151**/**ADR 0283**), and the
stored `range`, which prints its loop word (**Gap R.293**) — unchanged on the parent, because there was never
a tag there to ask.

## Consequences

* The three writers of a container slot now ask one predicate, so `append`, `d[k] = v` and `xs[i] = v` cannot
  each keep their own list of "kinds that need a tag".
* A promoted container is promoted **for the object, not the name**: `markListMixed` walks
  `containerSharers` (**ADR 0412**), which is why `xs = [1, 2]` / `ys = xs` / `ys[0] = 2.5` / `print(xs)`
  prints `[2.5, 2]` and is now covered by this row's family rather than only **Gap R.415**'s.
* The record gained 69 sources, all cross-checked, so the answers in this row's tables are not certified by
  the backend that earned them.
* `docs/language.md` says, of item assignment, that the container stops claiming a kind — the sentence a
  reader needs before they try the program.

## Alternatives rejected

* **Widening `heapElemKind` to answer a third question** (text / number / *needs a tag*). `heapElemKind`
  answers *which word did I intern*, and two callers already depend on that answer being one bit. The tag
  question has one home, `elemKindTag`, and this row joins the third road to it.
* **Promoting every list that is ever stored into.** It removes the class of bug by removing the fast road,
  and it costs `sum`/`min`/`max` their fold (**ADR 0239**) and every numeric loop its static array — the same
  trade this repo refused for container literals in **ADR 0421**.
* **Leaving the tag write and skipping the census bit**, i.e. trusting the runtime printer to ask per slot
  anyway. The printer's road is chosen at compile time from the bit; a tag nobody reads is exactly how this
  row's wrong answer was produced, one word earlier.
* **Refusing the store instead** — `mixedKindErr("list")` for any value whose kind the container does not
  claim. Honest but a worse witness: the reference answers all ten shapes, `append` answers all ten, and the
  arm costs four lines.
* **Fixing the chained comparison in the same commit.** It is `Gap R.82`'s one-word position, whose Definition
  of done is the tagged value word travelling through every position (**ADR 0331**), and four programs move
  from a wrong answer to a refusal when the promotion arrives — which is the hole being *shown*, and belongs
  in a row of its own (**Gap R.442**) with its own measurement, not smuggled into this one.
