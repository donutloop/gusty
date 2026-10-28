# ADR 0439 — a comprehension is a container at the doors that DECIDE too: the tag, the store and the census ask its brace one question

## Status

Accepted, 2026-10-28. Closes **roadmap Gap R.487** and **Gap R.488** (both filed 2026-10-28 by **ADR 0437**,
one commit before **ADR 0438** paid the sibling door), and files four rows this cycle measured: **Gap R.490**
(the same instruction shape at the loop head, the condition and `bool()`), **Gap R.491** (a comprehension whose
iterable is a container of containers), **Gap R.492** (a set literal accepting a comprehension as a member),
**Gap R.493** (a builtin's name as an expression statement, found by the recorder).

Read after **ADR 0188** (a compile-time container global is never an operand), **ADR 0192** (the runtime
comprehension builder, and the `containerOperand`/`containerHandleOf` arms this ADR finally reaches), **ADR
0232** (the slots the builder wrote are the truth about the object), **ADR 0249** (a gate and a door must not ask
different questions), **ADR 0438** (the same three roads, one question, for the container held in a name), **ADR
0166** (exit 2 is a compiler bug), **ADR 0239** (the static array the folds read), **ADR 0361** (the tuple is the
container the language hashes), **Gap R.232**/**Gap R.249** (a refusal sentence is an interface).

## Context

The two programs **ADR 0437** filed are one construct wearing two clothes:

```gusty
print([[{"a": 1} for x in range(1)], 1])   #  reference [[{'a': 1}], 1]   this backend [1, 1]      exit 0
def f():
    return 1
print([[f() for x in range(1)], 2])        #  reference [[1], 2]          this backend [1, 2]      exit 0
print([[[x for x in [1]]]])                #  reference [[[1]]]           this backend llc rejects  exit 2
```

A slot of a compiled container is a pair — one word, one tag that says what the word is — and every road that
writes one asks three questions: what word goes in the slot, what tag goes beside it, and whether the OBJECT may
be read through those tags. The three roads that ask them are `heapElemKind`, `elemTagFor` → `elemKindTag`, and
`literalNeedsTags`/`literalNeedsHeap`. **ADR 0438** had made all three ask one question about an element that is a
NAME. A comprehension was still asked a spelling, and a comprehension has none:

| element the builder cannot see | what the three roads asked | answer for `[<comprehension>, 1]` |
|---|---|---|
| a comprehension the folder cannot fold | is it one of the three literal spellings, or a name? **no** → the ordinary value road | a heap handle from `runtimeCompList`, tagged `TagInt` by `elemKindTag`'s tail, in an object whose header claimed one element kind → the single-kind printer printed the allocator's slot: `[1, 1]` |
| a comprehension the folder FOLDS | the same, and the value road's answer is a global NAME | `call void @rt_set_elem(i32 %t3, i32 0, i32 @.lst1)` — `llc`: *"global variable reference must have pointer type"*, **exit 2**, ADR 0166's own-bug code |

The embarrassing part is where the fix already lived. `containerOperand` and `containerHandleOf` have had a
`*Comp` arm since **ADR 0192**, and the former's comment says out loud that a folded comprehension *"is
materialised into the heap here, because handing `@.lst1` to a runtime helper is the invalid-IR shape ADR 0188
removed for literals"*. The doors that DECIDE which road an element takes — `nestedContainerElem` for the store,
`elemKindTag` for the label, the two censuses for the header — never grew it. That is **ADR 0249**'s defect class
in its purest form: a build door that knows a construct and a decide door that does not, with the wrong answer (a
number at exit 0) or the compiler's own bug (a rejected module at exit 2) sitting in the gap.

## Decision

**1 — One predicate, asked by every road that writes the slot.** `pkg/lang/heapargs.go` gained
`compSlotIsContainer`, which reads the comprehension's own brace (`Comp.Kind`) and the taggability of its
contents, and is asked three ways:

```go
func (g *irGen) compSlotIsContainer(e Expr) (int32, bool) {   // TagList / TagDict / TagSet, or "no"
	c, isComp := e.(*Comp)
	if !isComp || g == nil { return 0, false }
	if !g.compContentsTaggable(c) { return 0, false }
	switch c.Kind {
	case CompList: return int32(TagList), true
	case CompSet:  return int32(TagSet), true
	case CompDict: return int32(TagDict), true
	}
	return 0, false          // a generator expression is not a container (Gap R.195)
}
```

- `elemKindTag`'s `switch` gained `case *Comp`, returning that tag and **breaking to the old arms** when the
  predicate declines — so no shape that answers today starts refusing;
- `nestedContainerElem` returns it, which is what routes the store through `containerOperand` (the door that
  materialises the fold instead of naming it);
- `literalNeedsTags`' `slotsItself` and `literalNeedsHeap`'s `slotsContainer` return it, so the object says its
  slots are the truth (bit 8) and the printer chosen is the one that reads the pair;
- `taggableNestedElem` asks `compContentsTaggable`, which puts the comprehension's element/key/value expressions
  into the literal of the same shape and asks the question the literals ask. Decline means the existing
  `nestedContainerElemErr` / `nestedContainerKeyErr` sentences, not a new one.

**2 — A comprehension that builds an unhashable object cannot be a dict key.** `dictKeyIsContainer` and
`dictKeyHasNoHash` grew the `*Comp` arm (list/dict/set kinds), so `{[x for x in [1]]: 1}` is refused by the
hashing rule exactly as `{[1]: 1}` has been, and `tupleElementsHashable` refuses to hash a tuple that CARRIES one
(**ADR 0361**'s walk, extended to the brace). The set-MEMBER side is **Gap R.492**, filed not fixed: the member
door owes a raise for every unhashable member and today prints them (**Gap R.485**), and making this cycle ship
the comprehension spelling's raise while the literal spelling still printed would have given one rule two answers.

**3 — A refusal names the construct, not the Go type.** `exprSnippet` and `exprSummary` had no `*Comp` case, so the
key door said `a compiled container cannot hold *lang.Comp as a key` and the repr door said `repr of  is not
implemented`. Both now say "a list comprehension" / "a dict comprehension" / "a set comprehension" / "a generator
expression" — **Gap R.232**'s and **Gap R.249**'s rule, applied to a node nobody had asked.

**4 — Pinned rows that stop being wrong MOVE.** The three rows (two legs × three programs) left
`container_element_and_member_test.go` and its CLI twin and answer in `comprehension_element_test.go` (unit:
compiled leg + record leg via `HasGoldenAnswer`/`runGoldenStdout`; CLI: against a live CPython with the exit code
asserted, plus ten programs with exit **2** forbidden). `tools/recmerge` wrote 27 entries from the reference with
the compiled leg cross-checked (**9905 → 9932**: the family's 26 sources, plus the corpus program's own text, which the
GC corpus asks about by its own spelling — a `.gy` added to the corpus costs a record, because
`integration/gc_stress_test.go` fails a corpus program the artifact has no key for), and
`integration/programs/comprehension_container_element.gy` joined the conformance corpus (259 programs, `match`
line-for-line).

**5 — What this ADR does not claim, written into the suite as it was written here.** The loop head, the condition
and `bool()` still hand `llc` a global (**Gap R.490**); a comprehension over a container of containers still
raises `IndexError` where the reference prints (**Gap R.491**); a set literal still accepts an unhashable member
(**Gap R.492**, both spellings); and a builtin's name as a statement still violates the frame-slot audit (**Gap
R.493**). Each is pinned with today's bytes and exit code at both legs.

## Consequences

- The change is six arms in `pkg/lang/heapargs.go` and no new runtime function, tag, or ABI. The IR difference for
  an answered element is `rt_tag_elem(_, _, 5|6|7)` beside `rt_set_elem(_, _, %handle)`, `rt_mark_estr(_, 8)`, and
  the mixed printer; nothing else in the module moves.
- Objects that claim per-element tags are more common, which is the honest direction and the expensive one —
  `rt_print_list_mixed` walks two words per slot. The IR rows keep the two sides pinned: the comprehension element
  MUST carry the census bit and the mixed printer; `print([1, 2, 3])` MUST still take `rt_print_list` with no bit,
  which is **ADR 0239**'s `sum`/`min`/`max` fold.
- The census arm is a question about the element, so a comprehension whose contents no slot can describe declines
  rather than over-claims — the same `taggableNestedElem` the literals ask, so the two cannot drift.
- `docs/language.md` now says a comprehension written as an element is the container its brace spells; the two
  filed sentences that described the slot numbers and the `llc` rejection were deleted, not edited.
- The record grows to 9932 sources and the corpus to 259 programs; `AGENTS.md`, `docs/operations.md`'s artifact
  table and `roadmap.md`'s Snapshot were moved by the guards that recompute them (**Gap R.204**), which is the only
  reason four files stayed in step.

## Alternatives rejected

- **Add the `*Comp` arms to `isContainerExpr`/`staticContainerKind` as well, in this commit.** Rejected on
  measurement, not on taste: the arms were written and the three residual programs re-measured, and all three
  still died in `llc` at the same instructions (the loop's `icmp slt i32 %_ctr1.ld1, @.lst2` moves line, the
  truth test's `icmp ne i32 @.lst1, 0` does not). A deciding door that says "list" without a handle to hand is how
  a `for` gets a compile-time global for a repeat count; the door owes the handle first, and that is **Gap
  R.490**'s work. Shipping the arm alone would have converted an honest crash into a wrong number.
- **Give `heapElemKind` a `*Comp` arm of its own, bypassing `nestedContainerElem`.** Rejected: `nestedContainerElem`
  IS the store's deciding question, and a second door onto the same fact is how the tag and the word disagreed in
  the first place. One predicate, three askers.
- **Tag every comprehension element `TagList`.** Rejected: the brace says otherwise — `[{x for x in [1]}]` and
  `[{1: x for x in [1]}]` are a set and a dict, the printer and the comparison route on the tag, and a dict
  labelled list prints its entries as if they were members (**ADR 0232**'s rule that a tag is never invented).
- **Refuse the untaggable comprehension element instead of declining to answer its kind.** Rejected: `elemKindTag`
  is asked by roads (the print door, `staticSlotTag`, the membership needle) for which "no answer" is not a
  refusal but a disappearance. The arm answers when it can and lets the pre-existing arms answer when it cannot.
- **Land the set-member raise with this cycle.** Rejected: **Gap R.485** owns the member door's hashability rule
  and still prints `{[1], [2]}` for the literal spelling. One raise for three spellings is one row; shipping the
  comprehension spelling's raise alone would have made the door answer two ways depending on how the member was
  written.
- **Materialise the folded global lazily at print time.** Rejected: **ADR 0438** rejected the print-time fix for
  the same reason — the object, not the call, decides how its slots are read, and `len`, `in`, `==`, a `for` and a
  nested subscript all read the same slot. This cycle's IR rows assert the answer is in the object.

## Note on this cycle's method

Three measurements did the work, and none of them was a new test. (1) The two builds — `272c969` and this one —
run side by side over 29 programs: every change was a wrong number or an exit 2 becoming CPython's bytes, and the
two shapes that stayed wrong stayed wrong *identically*, which is what let them be filed rather than debugged.
(2) The rejected alternative above was rejected because the arms were actually built and measured, not because
they looked wrong; the comment in `compSlotIsContainer` and **Gap R.490**'s row both carry that measurement, so
the next cycle cannot re-litigate it from the diff. (3) `tools/recmerge` refused a source and pointed at **Gap
R.493** — a builtin name as a statement dies in the frame-slot audit — which is the recorder's cross-check finding
a bug in a road this cycle never touched; it is filed as its own row rather than folded in, and the recorder run
that found it is named in the row so it can be re-found.
