# ADR 0324: a haystack two slots deep, and a needle read out of a slot, are the same door

**Status:** Accepted
**Date:** 2026-10-13
**Implements:** roadmap `L11.1`, `Gap R.210`, `Gap R.211` — and files `Gap R.212`
**Extends:** ADR 0323 (the membership and loop arms), ADR 0251 (the recursive slot read), ADR 0187 (a payload travels with its tag), ADR 0311 (an unhashable write raises from its tag)
**Precedent:** ADR 0187's rule that a payload is meaningless without its tag, applied to a value on the *other* side of the door

## The decision

`builtSlotPair` is recursive, and the **needle** of a membership test is read the same way the haystack is.

```go
// pkg/lang/heapargs.go
func (g *irGen) builtSlotPair(b *strings.Builder, ix *Index) (payload, tag string, ok bool) {
    ...
    // The base is not a plain variable the compiler described. Before ADR 0251's recursive read
    // this was where the arm stopped; the pair it computes is an operand as much as a value.
    return g.runtimeSlotPairDeep(b, ix)
}
```

and, in `membershipOfBuiltSlotHaystack`:

```go
if _, isIndex := n.Left.(*Index); isIndex {
    needlePayload, needleTag, ok = g.builtSlotPair(b, n.Left.(*Index))
} else {
    needlePayload, needleTag, ok = g.taggedOperand(n.Left)
}
if !ok {
    return "", false, nil
}
g.guardHashableNeedle(b, needleTag, n.Span, true)
```

The hashability guard is keyed by the **needle's tag**, once per container kind, each arm leaving through the
program's own `except TypeError:`. A set haystack keeps the reference's own exception: `{1} in {1, 2}` is answered
`False`, not raised, because a set needle is looked up as the frozenset it converts to.

## The measurement that made the shape necessary

`Gap R.95` (ADR 0323) added four arms keyed by the runtime tag of a slot read, and closed with two rows in a
refusal table: a haystack two slots deep, and a loop over one. The shape was written into the row as "these two
would have to dispatch a dispatch". Asking them anyway, with the reference open beside the probe:

| program | reference | the build before this commit |
|---|---|---|
| `xs.append([[7, 8]])` / `print(7 in xs[0][0])` | `True` | **exit 1** — `index cannot reach into xs's slots` |
| `xs.append([[7, 8]])` / `for v in xs[0][0]: print(v)` | `7` then `8` | **exit 1** — `loop: cannot iterate over this value` |
| `d["a"] = {"k": [1, 2]}` / `for v in d["a"]["k"]: print(v)` | `1` then `2` | **exit 1** |
| `xs.append([[[[1, 2]]]])` / `for v in xs[0][0][0][0]` | `1` then `2` | **exit 1** |
| `xs.append([[1, 2], 5])` / `print(1 in xs[0][1])` | **raises** `argument of type 'int' is not iterable` | exit 1 — refused, not raised |
| `xs.append([{"k": 1}])` / `d["z"] = {"k": 1}` / `print(d["z"] in xs[0])` | `True` | **`False` at exit 0** |
| `xs.append({1, 2})` / `print([1] in xs[0])` | **raises** `unhashable type: 'list'` | **`False` at exit 0** |

The first five are one defect, and it is not the one the row named. The last two are a different defect, and they
were not in any row: they are answers the previous commit invented. Every one of them has a witness on both legs
before this commit is green.

## Why the haystack was never depth-limited

`runtimeSlotPairDeep` is four levels of the same argument, and it was written for `print`:

1. `containerHandleOf(base)` — the container the *program* built, not the compiler;
2. `slotReadUnderTag` — the outer tag picks the helper, and the helper reads the slot;
3. if the slot read comes back as a container itself, go to 2 with the new tag;
4. the pair at the end of the chain is `(payload, tag)`.

`Gap R.210` was written as if the membership arms would need a *second* dispatch, one below the dispatch ADR 0251
already runs. They do not. The loop arm asks `@rt_iter_len(h, t)` and `@rt_iter_elem(h, t, i)`; membership asks
`@rt_contains_tagged(h, t, p, ntag)`. Neither question has a depth parameter — both ask *what is this object*, and
the answer was sitting in a function three lines away, already computing exactly the pair. The fix is:

```go
pair, ok := g.builtSlotPair(b, ix.Base.(*Index))
if !ok { return "", false, nil }
```

The row's own sentence — "the *read* below the same slot is answered, because it dispatches on the outer tag and
then on the inner one, and these two would have to dispatch a dispatch" — was the wrong diagnosis. The arms
already dispatched on a tag; they had simply never been handed the tag at the end of a long chain. A refusal that
describes the mechanism accurately can still point at the wrong thing.

## Why the needle had to be read the same way

`taggedOperand` answers "what kind does the **compiler's notebook** say this expression has". For a literal and a
name that is enough. For `d["z"]`, the notebook records *which container the variable points at* — never what the
slot at the end of the chain contains, because by then the slot has a tag beside its payload and no entry in the
notebook. The consequence is not cosmetic:

```gy
xs = []
xs.append([{"k": 1}])
d = {}
d["z"] = {"k": 1}
print(d["z"] in xs[0])   # reference: True; the door before this commit: False
```

Two dicts with the same entries are equal. The comparison that answered `False` was comparing the two slots'
*payloads* — heap addresses — under a tag it had inferred from the wrong question. The same bug, one class worse,
answered `[1] in xs[0]` over a dict or set slot with `False`:

* a dict is looked up by its **key**, and a list cannot be a key;
* a set is hashed by its **member**, and a list cannot be hashed.

Reference: `TypeError: unhashable type: 'list'`. The door before this commit answered "not present", which is a
different answer at a different exit code. `ADR 0311` already raises from the *tag* on the write side for exactly
this reason; reading a dict or testing a set asks the same question of the same tag, and got the opposite verdict.

The guard is therefore keyed by the needle's tag, per arm:

```llvm
  ; dict arm — %needle.tag is 8 (list), 10 (dict) or 11 (set)
  %uh.d = icmp eq i32 %needle.tag, 8
  br i1 %uh.d, label %raise.unhashable, label %next.d
```

**Keyed by the tag, not by the proven kind**, because a set asked of a set is *answered* — `{1} in {1, 2}` is
`False`, since a set needle converts to a frozenset conceptually and the frozenset is not a member. A proven-kind
guard, or the reference's Python-level `__hash__` absence, would raise on that row. Keyed by the tag is the same
rule as ADR 0311's, and keeps the reference's one exception.

## The residue: a needle no road can prove

With the needle now asked for its pair, `taggedOperand` remains the fallback for a needle that is *neither* a slot
read nor a literal, and one shape has no tag at all:

```gy
def pick(v):
    if v:
        return v
    return "a"

xs = []
xs.append([7, 8])
print(1 if pick(0) in xs[0] else 0)   # reference: 0; compiled: exit 1, "more than one kind"
```

The haystack's tag picks the arm, and the arm compares the needle's payload **under a tag**. A call that returns a
number on one path and a text on another has no tag to carry, so guessing means whichever verdict happened to
match the number in the slot. The refusal is the honest answer, and it is pinned as one: exit 1, the sentence
checked, unit and CLI. This is `Gap R.212` — `Gap R.83`'s unprovable operand, seen from a different door. The half
it is missing is already built elsewhere (ADR 0273's callee returns the pair), which is what makes the row a
future parity row and not a permanent refusal.

## Witness

| claim | witness |
|---|---|
| a haystack two slots deep answers, on both legs | `pkg/lang/runtime_slot_read_test.go::TestRunTimeBuiltSlotMembershipAndLoopAnswerOnBothLegs`, 10 depth-two/depth-three/depth-four rows |
| a haystack two slots deep *raises* rather than refuses | same file's `…TrapLikeCPython`, 5 rows: the class, the message and the `except` that catches it |
| the CLI agrees, one case at a time through `gustyc` | `integration/runtime_slot_read_test.go::TestRunTimeBuiltNestedSlotReadsMatchCPython` (10 deep rows) and `…TrapsAreRaisedNotRefused` (5) |
| the pair travels, and the refusal is still an absence of a pair, not a verdict | `…RefusalsNameTheMissingHalf`, unit and CLI |
| the needle is read the way the haystack is read | 5 container-needle parity rows in both tables (`d["z"] in xs[0]` → `True`, `[1, 3]` → `False`, a set read out of a slot, a needle two slots deep, a dict read two levels up) |
| an unhashable needle raises | 8 trap rows (list/dict/set needle against a dict slot, list/dict needle against a set slot), class and message, caught with `except TypeError:` |
| one program, both legs, every answer above | `integration/programs/runtime_slot_haystack_deep.gy` — 17 lines, `oracle: match`, asserted, in `conformance-matrix.json` |
| what still refuses still refuses in words | the unchanged static-door rows in `TestRunTimeBuiltSlotMembershipAndLoopAsksTheObjectInTheModule` and the new `a needle whose kind cannot be proven` row, unit and CLI |
| the record leg | 18 sources added to `testdata/interpreter-golden.json` through `tools/recmerge`, which refuses an entry the compiled leg will not agree to |
| the ledger | unchanged at **337 + 21**, both adjudicated over 20 shards, all on the ledger — no row paid here without its witness, and none silently disappeared |
| the whole suite | `testshards`: `./pkg/... ./cmd/... ./tools/...` and `./integration/...` — **PASS**, 77 shards |

## Alternatives rejected

* **Teach the arms to dispatch a dispatch.** The row's plan was to grow depth into the membership and loop arms,
  which would mean either a second copy of ADR 0251's nested switch or an arms API that carries a chain. The
  measurement that killed it was `git grep runtimeSlotPairDeep`: the recursive read was already four levels deep,
  returned exactly the pair the arms wanted, and had one caller — the read. The bug was that two callers never
  asked for the value, not that a helper was too shallow.
* **Keep the two deep refusals as "unsupported".** The previous commit shipped them. They are exit-1 refusals for
  programs the reference runs at exit 0, and the row filed them precisely to stop that being the end state. A
  refusal is not "unsupported forever"; it is a debt with a sentence attached.
* **Compare payloads without tags and rely on `rt_eq`'s structural path.** That would have made
  `d["z"] in xs[0]` answer `True`. It would also have made a set asked of a dict, or a list asked of a set, answer
  `False` where the reference raises `unhashable type`. The unhashable verdict is the point of `TypeError`, not an
  extra detail: an author who writes `if [1] in xs[0]:` expects to be told the question is unaskable, not to be
  silently routed to the `else` arm. `ADR 0311`'s write-side guard already established that rule for the *other*
  side of the same object.
* **Ask the set arm to raise on a set needle.** Tempting because `TypeError: unhashable type: 'set'` is the
  sentence a Python programmer has seen; it is also wrong, and the reference is unambiguous: `{1} in {1, 2}`
  evaluates to `False`, because the set is converted to a frozenset conceptually before hashing.
* **Guard hashability before entering the container arm.** Rejected against the reference:
  `1 in [([1],)]` is `True` (the list is a *list* element, and lists hash their members unguarded), while
  `[1] in {([1],)}` raises. The verdict depends on what kind of container is at the end of the haystack chain,
  which is a fact the arm knows and the pre-arm does not.
* **Fix only the haystack (`R.210`) and file the needle (`R.211`) for next cycle.** The needle was discovered by
  testing the haystack, against the record leg and the reference leg. Two container-needle programs answered
  wrongly *at exit 0* one commit after the door that made them reachable. A commit that opens a door and leaves
  two programs printing wrong answers at success is a commit that shipped two regressions from "unsupported".
