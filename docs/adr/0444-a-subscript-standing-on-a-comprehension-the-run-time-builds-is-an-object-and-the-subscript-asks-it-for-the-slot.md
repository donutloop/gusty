# ADR 0444 — a subscript standing on a comprehension the run time BUILDS is an object, and the subscript asks it for the slot

- **Status**: Accepted
- **Date**: 2026-10-28
- **Cycle**: the one after **ADR 0443** (which closed **Gap R.490** + **Gap R.500**, filed **Gap R.502**, and left **Gap R.501** standing)
- **Roadmap**: **Gap R.501** closed; **Gap R.491** closed by the same permission at the neighbouring base; **Gap R.502** remains filed; **Gap R.495**, **Gap R.217**, **Gap R.146** and **Gap R.131** stay on their own rows.
- **Builds measured**: parent `15aa854` (**ADR 0443**) and this commit, every byte from a build made with the gate's `-tags=llvm20`.

## Context

Three sibling doors have now been opened one at a time for the same object:

- **ADR 0442** — `len([g(x) for x in [1, 2, 3]])` stopped loading a count out of the fold's empty global and asked `rt_heap_len` on the object the builder returned.
- **ADR 0443** — the loop, the condition and `bool()` stopped comparing a counter with `@.lstN` and walked/measured the object.
- **this ADR** — the subscript.

Before it stood:

```
$ gustyc --eval 'def g(v):
    return v * 2
print([g(x) for x in [1, 2, 3]][1])'
Traceback ...
IndexError: list index out of range                                             → exit 3
$ python3 (same)                                                             reference: 4
```

The program is the same shape **ADR 0442** closed two doors ago. The base is a `*Comp` whose builder emitted
`rt_alloc`/`rt_append_tagged` into `%h1`; the fold wrote no global and recorded no `compLen`. The subscript road
asked the fold's table anyway, got a zero-length count, and bounds-checked `[1]` against nothing. The pair door,
the one that can carry a payload plus a tag to `print`, had arms for a literal, a name, a call and a slice — and no
arm for a comprehension. The fold's twin `[x * 2 for x in [1, 2, 3]][1]` was already answered by the global load and
**ADR 0192**'s rule that a folded comprehension IS the literal it folds to, so opening the pair door without a
permission test would have moved the fold onto a runtime allocation.

## Decision

**A built comprehension is read as the object it built; the fold keeps its global.**

1. **`pkg/lang/codegen_comp.go` — `compTakesTheRuntimeRoad` is the gate.** It is a side-effect-free mirror of the
   first lines of `comp`: a clause list asks whether the nest folds; a literal iterable asks whether it needs the
   tagged loop; a non-static iterable asks the same container questions; an element/key/value/filter the folder cannot
   see belongs to the runtime builder. The pair road asks this before it emits anything, and the real `rtComps`
   record still decides once a builder has filled it. This keeps the fold's module free of `rt_get_elem` and free of
   the allocation that materialising a global would require.

2. **`pkg/lang/codegen_index.go` — the value road.** In `valueIndex`'s `*Comp` arm, `g.rtComps[obj]` sends the read
   to `compContainerKind` and `containerSlotRead`. A built list is measured with `rt_list_len` and read with
   `rt_get_elem`/`rt_tag_of`; a built dict uses the keyed door and raises `KeyError`; a built set raises the
   reference's `TypeError: 'set' object is not subscriptable`; the folded global road is unchanged below it.

3. **`pkg/lang/heapargs.go` — the pair road.** `runtimeSlotPairDeep` grew a bottom-of-chain arm for a `*Comp` base,
   gated by the same predicate, and `runtimeContainerSide` admits only built comprehensions. `subscriptPairOf` asks
   that door for a `*Comp` base after the ordinary literal/slot doors have had their turn. A set is declined here:
   a set slot may ask the membership extension, but a set comprehension standing under a subscript has no position
   to read and must reach the subscript door's raise.

4. **`pkg/lang/codegen_gc.go` — no new rooting.** **ADR 0443** already roots the object for the duration of its own
   build. The subscript runs after the build root is cleared; a later out-of-range read or keyed lookup is an
   exception, not a new allocation window.

## Agentic rationale

The machine interface is now three assertions, not one guess:

- a built comprehension subscript's `main` carries `call i32 @rt_list_len(i32 %h`, `rt_get_elem(i32 %h`, and
  `rt_tag_of(i32 %h`;
- the fold's control carries `load i32, i32* getelementptr({i32, [3 x i32]}, … @.lst` and **must not** carry
  `call i32 @rt_get_elem(i32`;
- the record and CLI rows cover answers, traps and catchability. Exit **2** is forbidden on every answered row and
  on every trap row, because this family began with module rejections and the only acceptable failure left is a
  program's own exception.

## Codegen / IR implications

* `pkg/lang/codegen_comp.go`: new `compTakesTheRuntimeRoad`, called by the pair gate and by the pair door. It calls
  only the same folding questions `comp` calls (`compLiteralAsksForTheTaggedLoop`, `compItems`, `comprehensionFolds`,
  `setCompFolds`, `dictCompFolds`, `nestedCompFold`).
* `pkg/lang/codegen_index.go`: built `*Comp` subscript now goes through `containerSlotRead`; built `CompSet` emits a
  constant trap for `'set' object is not subscriptable`.
* `pkg/lang/heapargs.go`: `runtimeContainerSide` and `runtimeSlotPairDeep` gained the built-comprehension arm; the
  fold is rejected before `containerHandleOf` can materialise it.
* `pkg/lang/subscript_pair.go`: the printer/f-string field asks the pair door for a built comprehension base after
  the existing five subscript pair arms.
* No new runtime helper. The module uses `rt_alloc`, `rt_append_tagged`, `rt_list_len`, `rt_dict_len`, `rt_get_elem`,
  `rt_tag_of`, and the same exception-entry shape already used by container variables.

## What this commit does NOT reach — filed, with today's bytes pinned

* **Gap R.502** — an element whose kind a call hands back still arrives without its tag. A built list of strings,
  floats, `None` or containers may print the payload where the reference prints the value; a built dict comprehension's
  mixed keys are the same row. That is the builder's self-describing-slot question, not the subscript door's position.
* **Gap R.495** — a subscript of a read out of a comprehension is the same hand-off at the folded base. This commit
  kept the fold on its global road rather than opening that one too.
* **Gap R.217** — a set variable's subscript still refuses where the reference raises; the built comprehension's set
  brace is answered because the program already gave it a brace and the door can see it.
* **Gap R.146** / **Gap R.131** — a pair meeting a one-word position and `bool` of a container variable/literal keep
  their own refusals.

## Alternatives rejected

* **Open the pair door for every comprehension.** It answered the built rows but made the fold allocate and use the
  runtime read. The fold's rule is **ADR 0192**'s, and the module assertion failed for exactly that reason.
* **Call `containerHandleOf` before deciding built vs folded.** It works after lowering but emits IR while deciding.
  A pair road that emits and then declines is the same defect class **ADR 0430** learned from: the module keeps
  instructions for a road that never owned them.
* **Let `containerSlotRead` answer a set as membership.** That would confuse two different programs: a slot whose tag
  says `set` is allowed to answer the documented membership question, while a comprehension with `{ }` standing under
  `[i]` has written a position into a language that has none. The reference raises, and now the compiler raises.
* **Land **Gap R.502** together.** Rejected: the element tag is a builder question, and the parent already printed the
  same wrong tagless bytes for mixed elements. One commit per feature (**AGENTS.md**).

## Evidence

* `pkg/lang/built_comp_subscript_test.go` — 14 answer rows on the compiled leg and the record leg; 5 trap rows with
  the compiled trap and `goldenRunError`; catchable rows; module rows for the built object's `rt_list_len`/`rt_get_elem`/
  `rt_tag_of` and the fold's global-load negative control.
* `integration/built_comp_subscript_test.go` — the same family through the CLI beside live `python3`, plus the record
  trap check, plus a module-shape row with the fold forbidden from taking `rt_get_elem`.
* `integration/programs/built_comp_subscript.gy` — the corpus row, `match`, line-for-line against CPython; it carries
  the answered numeric slots, negative index, arithmetic, comparison, nested list/dict reads, and catchable raises.
* `tools/recmerge` wrote **23 entries (10086 → 10109)**, none refused — 11 first pass, 4 trap rows, 6 rows needed
  exactly the Go string spacing the tests use, the **Gap R.491** control `print([[x for x in [[1]]][0], 2])`, and the
  corpus program's own source text, which the GC-corpus case asks as one whole program.
