# A name that held the void is still the void: the tag a BINDING leaves behind, and the key that arrives with no kind at all

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.286** (`d = {None: 3.5}` / `k = None` / `print(d[k])` died
`KeyError: key not found` at exit 3 where the reference prints `3.5`; measured unchanged across ADR 0353's
probe, so the row sat filed for eight days while every neighbour got fixed). Files nothing new: the two
sentences it leaves un-named belong to **Gap R.189** (a KeyError naming a key the compiler cannot see), and
the one shape the sweep surfaced beside it — `[k]` printing `[0]` for a name holding the void — is the list
road's own, byte-identical on the pre-row build (`7cbffb5`) and on this row's parent (`2b49a2a`), which **Gap R.146**'s family already owns.

One commit, one feature: two arms in `pkg/lang/heapargs.go` (`elemKindTag`'s arms for a `*Name` the void or
a verdict record claims, and `constKeyView`), one arm in `pkg/lang/codegen_index.go` (`dictKeyNameTag`, the
key-tag question the read road had never been able to ask), the KeyError message beside it, and the two test
files.

## Context

```
d = {None: 3.5}
k = None
print(d[k])          #  3.5  reference   →  KeyError: key not found, exit 3
```

The read itself was fine. **ADR 0419** had already routed a dict read through the tagged doors —
`rt_dict_has_tagged` / `rt_dict_get_tagged` / `rt_dict_value_tag` — and a dict built by that pass stores each
entry as a `(payload, tag)` pair, so the void's entry is `(0, TagNone)` and the integer 0's is `(0, TagInt)`:
two keys, exactly as the reference keeps them. What the read handed the object was the other half of that
pair — and for a key that arrived through a binding, it handed over only the payload.

The IR says it in one line:

```llvm
store i32 0, i32* %_k                                        ; k = None  — the value, and nothing else
%t11 = call i32 @rt_dict_has_tagged(i32 %t10, i32 %_k.ld1, i32 0)   ; ← 0 is TagInt, not TagNone
```

Two defects stood behind that line, and the second is the reason the first was invisible.

**The read road already had the arm, and it was unreachable.** `pkg/lang/codegen_index.go` chose the key's
tag with

```go
if _, isLit := n.Idx.(*IntLit); !isLit { … }
else if nm, ok := n.Idx.(*Name); ok && g.taggedVars[nm.Value] { /* load _<name>_tag */ }
else { keyTag = TagInt }
```

The second branch could only run for an index that was **both** an `*IntLit` and a `*Name`. Every name key
fell to the first branch, and the first branch asked `elemKindTag` — which brings us to the real door.

**`elemKindTag` answers "which kind is this value", and its foot is a blanket `TagInt`.** The door has arms
for the container a name holds, the text a name holds (`strVals`), the float a name holds, the instance a
name holds, the loop variable the unroll tagged — and it has literal arms for `*NoneLit` → `TagNone` and
`*BoolLit` → `TagBool`. What it never had was the NAME whose binding wrote one of those singletons. So
`k = None` was, to every door that asks this one question — the mixed-dict pair, the membership test, the
tagged read, a container slot — *an integer whose value happens to be 0*.

That is L11.1's debt appearing in a place the row had not named: not "the value word cannot carry a kind"
but "the binding never wrote the kind down next to the value". ADR 0267 had already solved this shape for an
arithmetic answer (payload *and* tag into the pair slots); the singletons had been left with print's record
(`noneVars`, `boolVars`) and no tag beside them.

## Decision

**Answer the two singletons where every other kind is answered: in `elemKindTag`, from the ADR 0172
records.**

```go
if nm, isName := e.(*Name); isName && g != nil {
    if _, perItem := g.loopElemTag[nm.Value]; !perItem && !g.nameIsContainerRecorded(nm) {
        if g.noneVars[nm.Value]  { return TagNone, true }
        if g.boolVars[nm.Value]  { return TagBool, true }
    }
}
```

Three parts of that are load-bearing, and each is a boundary this row measured rather than assumed:

* **`noneVars` / `boolVars` are the same records print, truthiness, `str` and equality already live by**
  (ADR 0172's latest-binding rule). The key door may not know less about a name at the same point than the
  printer does; that divergence is exactly the family ADR 0384 and ADR 0419 keep closing.
* **A loop variable the unroll gave a per-item tag stands aside.** An unrolled `for k in [None, 1]` is *this
  item*, not the kind some earlier binding handed the name — Gap R.358's rule, restated at the new arm
  rather than bent around it.
* **A name the container records claim stands aside**, because its payload is a handle and its kind comes
  from the object; the arm must not turn a list variable into a void wearing a label.

**Give the read road the key-tag question it was written to ask.** `dictKeyNameTag` asks, in order: the
pair road's own `_<name>_tag` word (and the alloca's presence is asked too — a load of a register this
function never allocated is exit 2, ADR 0166's class, not an answer); then `elemKindTag`, the one door,
which now answers the void and the verdict. The dead `else`-of-an-`*IntLit` branch is gone rather than
patched, because a branch that cannot run is not a road.

**Let the static key door read an entry the program addresses by name.** `constKeyView` retires a key that
is a `*Name` to the singleton its binding wrote, and `constKeysMatch` asks both sides through it. This is
what makes `d = {None: "a"}` / `k = None` / `print(d[k] + "b")` answer `ab`: the label road asks the door for
the entry, and the door can now find it. Only the VOID is retired, because only the void is unambiguous —
`boolVars` records that a name holds a verdict and not *which* verdict, and a key the compiler cannot quote
is left as a name the door cannot read, which is the refusal, not a guess.

**Name the key in the traceback when the record can quote it.** `keyNotFoundMessage` now reads the same
records: a void key raises `KeyError: None`, a text key raises `KeyError: 'zz'` (from `strVals`, the record
the printer asks). A number key through a name stays the backend's generic sentence and is pinned that way,
because naming it needs a record no one owns — which is **Gap R.189**'s sentence, not this row's.

## Consequences

* The program the row is named for prints `3.5` on the compiled leg, the record leg and the CLI leg, as do
  nine more shapes of the same operand (concatenation, repeat, the method road, the loop over the entry,
  `len` of a list entry, the void and the number as two keys addressed by name, the number key through a
  name, and a verdict reading both the verdict key and the number key it is).
* **13** sources went to the record through `tools/recmerge` (**9267 → 9280**), each recorded from the
  reference with the compiled leg cross-checked. It REFUSED two — `k = 0` over a void-keyed dict and
  `k = True` over one — because the compiled raise is `KeyError: key not found` where the reference raises
  `KeyError: 0` / `KeyError: True`; those two are pinned as traps whose CLASS matches, with the named row in
  the dossier. A recorder that will not file a wrong answer is the reason the record is worth having.
* Four pins are **deleted, not edited**, because the rows they filed are paid: two filed-not-fixed rows in
  `pkg/lang` (`none_key_label_roads_test.go`, `dict_none_bool_key_test.go`), one CLI owed row and one CLI
  filed row in `integration`. Each had a line that failed the day the answer arrived, which is the only
  reason closing the row was safe.
* The blanket `TagInt` at `elemKindTag`'s foot is unchanged, and it is still the largest silent-wrong
  surface in the tag story: any name whose kind no arm recognises is called an integer by every door that
  asks. This row narrowed it by two kinds and left the rest to **L11.1** — the destination, not this rung.
* `[k]` for a name holding the void still prints `[0]`, byte-identical on the pre-row build (`7cbffb5`) and on this row's parent (`2b49a2a`): the list
  literal's recorded element kind wins over the element's own, which is the one-word-position family
  (**Gap R.146**) and is untouched here.

## Alternatives rejected

* **Bind the singletons the way ADR 0267 binds an arithmetic answer** — have `k = None` lower through the
  pair road (`_<k>` + `_<k>_tag`, `taggedVars` set). Rejected: it changes what `print`, truthiness,
  `str` and equality see (all of which consult `taggedVars` as a fact about the name), for an answer the
  records already hold. The row is about a missing LABEL, and the label belongs in the door that asks for
  labels.
* **Retire a `boolVars` name to a `*BoolLit` in `constKeyView`** by adding a value record beside the status.
  Rejected for this commit: it is a new fact in the generator's state to serve one message, and the truth is
  that the record does not know which verdict it is. Pinning the generic message and naming the row that
  owns the sentence costs nothing and cannot rot.
* **Compare untagged keys payload-only, as the untagged `rt_dict_has` road does.** Rejected by ADR 0419 and
  re-rejected here: the void's payload IS 0, the verdict's IS a number, and a lookup that compares payloads
  answers the wrong entry at the exit code of success.
* **Load `_<name>_tag` whenever `taggedVars[name]` is set.** Rejected: the alloca is emitted by the road that
  binds the pair, and the read road can be reached from a function that never ran it — which turns a wrong
  answer into a module `llc` rejects, ADR 0166's forbidden class. The arm asks `allocd` too.
