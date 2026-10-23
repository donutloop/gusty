# The alias door is a container door: the dict's and the set's record travels with the handle too

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.419** (an alias of a dict or a set is the same missing record one
container over). Files three rows it measured beside itself and did not buy: **Gap R.420** (a rebinding releases a
container another name still holds and the allocator hands that slot to the next object), **Gap R.421** (a set's print
order puts the void last), **Gap R.422** (`discard`/`remove` compare the payload and not the pair). The container
METHOD over a name stays **Gap R.174**'s, and the residue **ADR 0412** left — **Gap R.417**, **Gap R.146**,
**Gap R.416**, **Gap R.418**, **Gap R.388** — is untouched by this commit. One commit, one feature:
`pkg/lang/container_alias.go`, `pkg/lang/heapargs.go`, and the two test files.

## Context

**ADR 0412** gave the shared-handle binding a record: `ys = xs` shares an object, and the claims that say what that
object's slots hold are about the object, so they must be reachable through every name that holds it. It paid the
list and stopped at its own door — `recordContainerAlias` copied `mixedLists`, `listElemStr` and `listElemInt`, and
returned. The dict and the set were named in the row as "the same defect one container over", and the bytes were
pinned in the owed table. They are the reason this commit exists:

```
d = {"a": 1}   e = d   e["b"] = "x"
print(d)   #  {'a': 1, 'b': 'x'}   reference
           #  {'a': 1, 'b': 1}     this backend — the text's intern index, at exit 0
print(e)   #  {'a': 1, 'b': 'x'}   reference
           #  {'a': 'x', 'b': 'x'} this backend — every value now wears the entry just written

s = {1, 2}   t = s   t.add("a")   print(s)    #  SIGSEGV
```

The name the write is written against decides the answer, which is the signature **ADR 0412** localised: the handle
travels through the binding and the notebook claims do not. For a list those claims are three; for a dict they are
six (`runtimeDicts`, `mixedDicts`, and the four position claims `dictKeyStr`/`dictKeyInt`/`dictValStr`/`dictValInt`)
and for a set three (`runtimeSets`, `mixedSets`, `setElemStr`/`setElemInt`). Each of them is read by a door — the
store door to decide whether the incoming value needs a tag written with it (**ADR 0232**'s contradiction check), the
set door to choose between `rt_set_add` and `rt_set_add_tagged`, the printer to choose which table renders a slot.
With an empty record for the alias, the store door concludes that nothing contradicts and stores unlabelled, the
set door picks the untagged arm, and the printer then obeys the stale claim the object had outgrown.

Two things about that list of maps decided the shape of this commit. The first is that **the doors are already
written**: `d[k] = v`, `s.add`, `s.discard`, the dict printer and the membership test all consult the same
name-keyed maps for every program in the corpus. A fix that grew a second path for aliased containers would have
been a second door over one fact — the exact thing **ADR 0249** and **ADR 0232** exist to forbid, and the thing that
had already made ADR 0412's measurement necessary. The second is that the claim that mattered most was not the one
the row was filed for. `contradicts` in the dict store asks `dictValInt[<name>]` and `dictValStr[<name>]`; an alias
with an empty record does not "not know" — it **believes nothing contradicts**, and a wrong belief is what promoted
nothing and printed an intern index. An empty notebook is not ignorance; it is a confident wrong answer.

## Decision

**One: the alias door asks which container the name holds, and copies THAT container's claims.**
`containerKindHeld(name)` reads the same three maps the binding's own `settleContainerKind` writes
(`listVars`, `runtimeDicts`, `runtimeSets`) plus the mixed records, and `recordContainerAlias` copies the claims for
that kind — all six for a dict, all three for a set, the three for a list — and keeps the link, exactly as the list
half did. One door, one writer, called from the one place that lowers an assignment (**ADR 0412**'s shape), because
the claims and the kind are one fact about one object.

**Two: the promotion is told to every name that holds the object, one container over.** `markDictMixed` and
`markSetMixed` join `markListMixed`, all three walking the cycle-guarded `containerSharers`, and the two promotion
roads (`promoteMixed`, `promotePairMixed`) now call them instead of writing the mutated name's maps. This is not
decorative: `d["b"] = "x"` written against the alias promotes the OBJECT, and a notebook that cleared the
single-kind claim on `e` alone would leave `d` rendering every slot through a table the object had outgrown — which
is the row's own wrong answer, reproduced from the other side of the alias.

**Three: no new door, no new helper, no new bit.** The store still lowers to `rt_dict_put_tagged`, the member to
`rt_set_add_tagged`, the print to the same `rt_dict_print_s`; nothing in `codegen_ir_arena_globals.go` changed.
An alias is a NAME, not a kind of container, and the moment a fix needs `@rt_dict_put_aliased` it has grown a second
answer to one question.

**Four: the copy is what makes the doors right, the link is what keeps them honest.** The copy at the binding is
what the mutation doors need to decide about an incoming element (**ADR 0412** measured the segfault that happens
without it, on the list); the link is what the read-time questions follow when a name has nothing of its own
recorded. The two are not alternatives, and the list row's tests are the evidence; this row inherits both rather
than re-deriving them.

**Five: what this row did not open, it filed with the bytes it saw.** Three shapes measured beside the family, each
byte-identical WITHOUT an alias and on the parent build `8aa3029` — the sentence that separates a filed debt from a
regression — and each pinned in both files:

* **Gap R.420**: `d = {"a": 1}` / `e = d` / `d = {"b": 2}` / `print(e)` prints the second dict, because the
  rebinding releases the object (`emitFreeOld` → `rt_free`, a free-list push that knows nothing about the alias's
  root) and the next `rt_alloc` takes the slot back. The list and the set answer the same programs correctly, so
  the row is the release road's, and the general question is the **Gap Q.2**/**Q.1** memory family's.
* **Gap R.421**: a set with a void member prints `{1, None}` where the reference prints `{None, 1}` — an ORDER
  question at the printer, present without an alias.
* **Gap R.422**: `s.discard("a")` over `{1, 2}` removes `2`, because `discard` asks the set's recorded kind instead
  of the argument's tag; `add` asks the argument and does not have this bug (**ADR 0187**, **ADR 0189**).

And two shapes this row deliberately left refused: `for k, v in e.items()` and `t.remove(1)` over the alias refuse
exactly as they do over the source name — a container METHOD over a NAME is **Gap R.174**'s standing. An alias gives
the object another name; it does not give the compiler a method door it never had. The CLI harness grew a `refuses`
arm for those pins, because "still refused" and "still answered wrongly" are different debts and a row that conflates
them hides whichever one moves.

## Machine path

* `gustyc --eval 'd = {"a": 1}
e = d
e["b"] = "x"
print(d)'` prints `{'a': 1, 'b': 'x'}` at exit 0, and the same source is on the record leg with those bytes.
* `--emit-llvm` shows no alias-specific helper: the aliased store emits the same `call void @rt_dict_put_tagged(i32,
  i32, i32, i32, i32)` the non-aliased store emits, and `--emit-llvm` for the set shows `rt_set_add_tagged`. The
  answer moved; the IR shape did not, which is what "the notebook, not the door" has to mean.
* `--json` on the two refusals this row leaves standing still carries `ok:false`, `exit:1`, `phase:"codegen"`, a
  capability code, a `suggestion` and `rows` naming the roadmap row (**ADR 0006**, **ADR 0346**); the harness fails
  them at exit 2 (**ADR 0166**) as loudly as it fails an answer that became a refusal.
* The battery's programs are on the record leg, so the answers this row claims can be audited without this backend
  (`tools/recmerge` wrote them; it refuses an entry the compiled leg will not agree to, **ADR 0309**).

## IR implications

* Nothing was added to the runtime IR and nothing changed signature. `rt_dict_put_tagged`, `rt_set_add_tagged`,
  `rt_dict_print_s` and the tag array are exactly as **ADR 0189**/**ADR 0232** left them.
* The notebook grew per-kind copying in one writer and two new marking functions; `settleContainerKind`'s clearing
  already covered all three kinds, so a name rebound to another object loses the claims it had copied — which is the
  invariant that keeps `s = {1}` / `t = s` / `s = {2, 3}` / `print(t)` answering `{1}`.
* The dict's release order (`load` → `rt_free` → `rt_root_clear` → `rt_alloc`) is unchanged here and is the subject
  of **Gap R.420**; this row pins it from the module only in the sense that its owed row pins the wrong answer that
  order produces.

## Alternatives rejected

* **Copy the claims and skip the promotion walk.** The dict store's contradiction check then promotes only the name
  the write was written against, and the other name keeps a single-kind claim about an object that has outgrown it:
  `{'a': 1, 'b': 1}` again, from the opposite side of the alias. Measured, not argued — it is the first build this
  row made before `markDictMixed` existed.
* **Follow the link only, without copying.** Leaves the doors that read the maps *directly* — the mutation doors —
  with nothing, and the set member arrives untagged: the SIGSEGV the row was filed for.
* **Resolve every claim through a dict/set-specific reader instead of copying.** A per-container reader is what
  **ADR 0412** does for the list's element claims, and it is right there because the pop door asks one question of
  one name. The dict and the set doors ask six questions of a name and a kind (`contradicts` alone asks four);
  threading a resolver through each is six chances to miss one, and the miss is a wrong answer at exit 0. The copy
  makes the maps themselves the single source; the link stays for the questions the copy cannot answer.
* **Fix `discard` and the set print order in this commit.** Both are pre-existing, both reproduce without an alias,
  and each wants a door this row has no business opening (the pair road at the removal door, the printer's member
  order). Filing them with this backend's bytes beside the reference's keeps one commit one feature and makes the
  next cycle's first commit a lookup (**ADR 0309**, and the practice **ADR 0410**–**0412** established).
* **Leave the alias's dict rebind answered "close enough" because the alias is a corner.** It prints the wrong object
  at exit 0 and raises `KeyError` for a key the program wrote. It is a row (**Gap R.420**) with the emitted
  instruction order quoted, not a shrug.
* **Teach `print` to special-case an aliased name.** The renderer was right under the source name and wrong under
  the alias only because it was handed an unlabelled payload; patching it would have bought the printer and left the
  store door writing untagged entries.

## Measurements (taken, not remembered)

Over the **37** programs the dict/set battery carries, a build of the parent commit (`8aa3029`, **ADR 0412**) beside
a build of this one, each asked of the pinned CPython:

* **15 → 31 agreeing.** Every one of the two shapes this row was filed for moved (the dict's value written through
  the alias, the set member added through it), along with the chain of three names, the second int key, the double
  and the void written through the alias, the nested list read out of a dict, membership, the loop over keys and over
  members, the aliased dict handed to a function, and the set rebound under its own alias.
* **None moved away from the reference.** The six programs that still differ were measured on both builds and are
  the three rows above plus the two **Gap R.174** refusals; the battery prints both columns for each, and the row
  cells quote them.
* The alias-family list programs (**ADR 0412**'s 42) were re-run as controls and held.

`tools/recmerge` wrote **23** entries from the reference for this family (the record **9061 → 9084**) and refused
none: on every source this row claims to have paid, the compiled leg agreed with the reference first (**ADR 0309**).
The three pins **Gap R.419** left in `TestWhatTheAliasRowStillOwesIsFiledNotFixed` and its CLI twin are **deleted,
not edited** — the programs moved into the answered family, which is the only spelling that distinguishes a paid debt
from a rewritten one (**ADR 0411**, **ADR 0412**).

`make testparallel` green; every emitted module verifier-clean; exit **2** forbidden in both files and present in no
pin this commit adds.
