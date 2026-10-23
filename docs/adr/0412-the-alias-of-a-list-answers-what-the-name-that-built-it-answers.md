# The alias of a list answers what the name that built it answers

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.415** (the same list under a second name answers from an empty
notebook). Files one row it measured beside itself and did not buy: **Gap R.419** (an alias of a DICT or a SET is
the same missing record one container over — two wrong answers at exit 0 and two crashes). The three shapes this
row reaches but does not own stay **Gap R.417** (the callee's own printer), **Gap R.146** (the one-word position)
and, one door over, **Gap R.388**/**Gap R.416**/**Gap R.418**. One commit, one feature:
`pkg/lang/container_alias.go` (new), `pkg/lang/codegen.go`, `pkg/lang/codegen_assign.go`, `pkg/lang/rebind_kind.go`,
`pkg/lang/heapargs.go`, `pkg/lang/codegen_for.go`, `pkg/lang/pop_answer.go`, plus the two test files and the pin
this row deletes from `pkg/lang/the_void_question_test.go` and `integration/the_void_question_test.go`.

## Context

**ADR 0411** closed the identity question and, in its own honest table, left the program that made it findable:

```
xs = [None, 1]
ys = xs
print(ys.pop(0))              #  None     — the reference
print(ys.pop(0) is None)      #  True     — the reference
                            #  0        — this backend, at exit 0
                            #  False    — this backend, at exit 0
```

Asked of the ORIGINAL name, both programs answer. `print(xs.pop(0))` prints `None` and `xs.pop(0) is None` says
`True`; the two lines above differ from the two that work by the NAME the read is written against, and by nothing
else. That asymmetry is the whole diagnosis: the defect is not in the list, not in `@rt_pop`, not in the pair door
**ADR 0410** hung and **ADR 0411** widened. It is in the notebook.

`ys = xs` lowers as what it is — the handle `xs` holds is stored into the slot `ys` holds — and the two names then
read **one object**. That object carries its own truth: every slot was written with its tag (**ADR 0189**), and
bit 8 of the container header says the slots may be read as pairs (**ADR 0232**). The *notebook* carries a
different truth, and it is keyed by NAME: `mixedLists` says a list's slots describe themselves, `listElemStr` and
`listElemInt` say which single kind the record claims they hold. The binding that shares a handle writes none of
the three. So every door that asks the notebook before it asks the object — `popAnswerShape`, `mixedIndexRead`,
the loop's tagged arm — asked a record that had never been written, got nothing, and answered from the payload:
the void's `0`, the intern table's index, the float box's handle. `print(ys.pop(0))` printing `0` and
`for v in ys` printing `0` beside `for v in xs`'s `None` are the same event at two doors, and **ADR 0232**'s
collision is the reason the payload is never the discriminator (**ADR 0172**).

The row also had a second, worse face. A mutation written against the alias — `xs = [1, 2]` / `ys = xs` /
`ys.append("a")` — read back through the *name* (`print(xs.pop(2))`) reached the printer with a text slot nobody
had labelled and died on a **segmentation fault**: the door that decides whether an incoming element needs a tag
written with it consults its own name's record, found none for `ys`, and stored an untagged payload into a tagged
container. That is **ADR 0166**'s artifact-at-fault class, produced by the most ordinary statement in the language.

## Decision

**One: the record travels with the handle, in three places, and all three are load-bearing.**
`recordContainerAlias` (new `pkg/lang/container_alias.go`) is the only writer, called from the one place that
lowers an assignment, beside the door that settles the kind — because the kind and the claims are one fact about
one object. It does two things at once:

* it copies the three claims onto the new name, because the doors that read the maps *directly* (the mutation
  doors, which decide whether an incoming element needs a tag written beside it) would otherwise read an empty
  record for the alias and store an untagged payload into a tagged container — the segfault above; and
* it keeps a `containerAlias` link, because a claim frozen at the binding is a claim about the moment of the
  binding, and `xs.append("a")` is an event after it: the doors that ask the record at READ time must ask the
  record of the object (`listElementClaims` asks the name's own record first and follows the link only when the
  name has nothing recorded at all).

**Two: the promotion is told to every name that holds the object.** `markListMixed` walks `containerSharers` —
the link in both directions, cycle-guarded — because a promotion is ONE event on ONE object wherever the source
writes it. `xs.append("a")` contradicts the kind recorded for the list; a notebook that told only `xs` would leave
`ys` claiming integers about a list whose next slot is a text. Copy-at-binding without promote-everywhere freezes a
claim that the next mutation makes a lie; the pair of half-measures is what the row's measurement rules out, and
the three halves therefore live in one file, beside one another.

**Three: the link dies when the object does.** `forgetContainerAlias` is called from `settleContainerKind` and
from `bindTaggedVar`, and it drops the name's own link *and every link that leads through it*. After
`xs = [None, 1]` / `ys = xs` / `xs = [2, None]` the alias still holds the FIRST list, and a link resolving into
`xs`' record of the SECOND one would be a claim about the wrong object. The copied claims — which are about the
object `ys` holds — are what keeps that program answering: `print(ys)` gives `[None, 1]` and `print(ys[0])` gives
`None`.

**Four: the roads gained a lookup, not a road.** `popAnswerShape` (gate) and `popAnswerPair` (door) both ask
`listElementClaims`, one table, so the gate's answer and the door's cannot drift (**ADR 0249**'s gate-and-door
rule, **ADR 0232**'s one question); `mixedIndexRead` and the loop's tagged arm ask `mixedListRead`, which is the
same lookup. The handle is still loaded from the name the read is written against — the link is a lookup into the
record, not a redirection of the read — and the removal still goes through the one
`@rt_pop(i32 %h, i32 %i, i32* %outt)` **ADR 0410** hung. The IR guard asserts that from the module: **one** call
per removal, the tag out through the parameter, `%_ys` as the handle, and the four helper names a parallel road
would have invented (`@rt_pop_aliased`, `@rt_pop_alias`, `@rt_alias_`, `@rt_tag_elem_aliased`) absent.

**Five: the third answer survives.** A list no record describes is still DECLINED, alias included. Following a
link into an empty record is not a licence to guess a kind — a guessed kind is how a payload gets rendered under a
label the object never gave it (**ADR 0166**), which is the bug this family exists to remove, not to add. The gate
test asks all three answers over six notebooks: the mixed list lending its object's tag, a single-kind claim
lent, a chain of two links followed to the end, a name whose own record outranking the link, a cycle, and the
DECLINE — with `popAnswerPair` asked the same table so a gate that lies about the door fails.

**Six: what the row left out, it filed with the bytes it saw.** `recordContainerAlias` returns after the list's
three claims. A dict's and a set's alias is the same missing record one container over, and it does not even
decline: `d = {"a": 1}` / `e = d` / `e["b"] = "x"` / `print(d)` writes `{'a': 1, 'b': 1}` — the text's intern index
under `d`'s still-integer record — and `t.add("a")` / `e[7] = 2` are SIGSEGV. Three shapes, byte-identical on the
parent build, pinned in both files and filed as **Gap R.419**. The callee's printer handed the alias's popped slot
(`w(ys.pop(0))` printing `0`) is **Gap R.417**'s shape reached through the alias — byte-identical to the non-alias
twin, which is the sentence that separates a filed debt from a regression — and a slot of the alias bound to a name
and asked as a condition is refused for the same reason the same program with no alias in it is: **Gap R.146**'s
one-word position.

## Machine path

Nothing new is discoverable that was not discoverable before, which is the point — the row changes an *answer*,
not the interface. What an agent can still ask, and what this row keeps honest:

* `gustyc --eval 'xs = [None, 1]
ys = xs
print(ys.pop(0))'` prints `None` at exit 0, and the same source is on the record leg with those bytes.
* `--emit-llvm` for the alias's program shows the same single `call i32 @rt_pop(i32, i32, i32*)` the name's
  program shows, and no alias-specific helper. `TestTheAliasSharesThePopsRoadAndInventsNoHelper` asserts both, so
  a future second road is a failing test rather than a quiet fork.
* the refusals this row leaves standing keep their capability codes: `lower.unsupported.container-method-over-a-name`
  for `e.pop("a")` over an aliased dict, and the one-word position's sentence for a slot bound and asked as a
  condition, each with `ok:false`, `exit:1`, `phase:"codegen"`, a `suggestion` and `rows` naming the roadmap row
  (**ADR 0006**, **ADR 0346**). The second of those reaches an agent with `"capability":null` today, which is
  **Gap R.408**'s defect measured on a refusal nobody had pointed `--json` at since the road grew; the row pins it
  with today's artifact rather than smoothing it over.
* the traps in the family (a list emptied through one name and popped through the other, a bad index through the
  alias) exit **3** with CPython's class and sentence — never **1** (a refusal would be the compiler declining a
  program the reference runs) and never **2** (a broken module, **ADR 0166**) — and `except IndexError:` catches
  them, which is the difference between a trap the runtime raises and a diagnostic the compiler prints.

## IR implications

* No runtime signature changed, no helper added, no new global, no new bit. The notebook grew one map
  (`containerAlias`) beside the three it already had, and the reads that were consulting the three now consult the
  four.
* The alias's removal emits the identical instruction sequence the name's removal emits, because after the lookup
  they are the same lowering emitted by the same function. That is the claim `TestTheAliasSharesThePopsRoadAndInventsNoHelper`
  checks from the module rather than from the answer.
* `forgetContainerAlias` runs on every name assignment, and `settleContainerKind` already cleared the container
  facts there; the link is cleared in the same place for the same reason — a fact about an object may not outlive
  the binding that made it.
* Because `emitPopRoad` can emit a bounds check whose `IndexError` branch terminates a block, the pair door keeps
  building its answer in a scratch buffer and committing once. Adding a notebook lookup in front of that door did
  not change the discipline; a door that changes its mind halfway leaves instructions after a terminator and `llc`
  reports the compiler's mistake as the program's.

## Alternatives rejected

* **Fix the printer.** `print(ys.pop(0))` was the visible symptom, and the printer already asks the pair correctly
  — under the name. The renderer had the right rule; it was handed an untagged payload because the door that
  decides whether a tag needs *writing* consulted a record the alias did not have. Patching the renderer would
  have bought one road and left `ys[0]`, `for v in ys`, `if ys.pop(0):` and `str(ys.pop(0))` each to be repaired
  one at a time.
* **Copy the claims at the binding and nothing else.** The cheapest diff, and it survives the answered family —
  then loses `xs = []` / `ys = xs` / `xs.append("a")`, because the copy froze "these slots do not describe
  themselves" onto a list whose next element is a text. Measured, not argued: the promotion cases are in the
  answered family precisely because a copy-only fix fails them.
* **Follow the link at read time and nothing else.** The mirror-image failure, and the worse one: the mutation
  doors read the maps directly, so an alias with no record stores an untagged payload into a tagged container, and
  the read-back is a segfault rather than an answer.
* **Promote on the mutated name only.** Leaves `xs` and `ys` disagreeing about one object, which is a wrong answer
  at exit 0 (one event, one object, two names).
* **Share the three maps by aliasing the maps themselves instead of the entries.** One map per object would make
  the notebook's own lifetime wrong: `xs = [2, None]` must settle NEW facts for `xs` while `ys` keeps the old
  ones, and two names that hold different objects cannot read one map. The link plus the copy is what lets the
  notebook have per-object truth with per-name bindings.
* **Give the alias its own pop road (`@rt_pop_aliased`).** A name is not a new kind of container. The IR guard
  forbids the helper names, and the row passes with the one `@rt_pop` **ADR 0410** hung.
* **Answer the dict and the set too, here.** Their doors were not opened by this row: their claims are copied by
  nothing and their wrong answers and crashes predate it byte-for-byte. Filing them as **Gap R.419** with the
  bytes beside them keeps one commit one feature and makes the next cycle's first commit a lookup rather than an
  excavation.
* **Leave `w(ys.pop(0))` unfiled because it looked like this row's.** `pairCallScan` OPENS the parameter's pair for
  a popped argument, so the tag crosses the frame and the body throws it away — and the non-alias twin prints the
  same `0`. Byte-identity with the non-alias twin is the evidence that it is **Gap R.417**'s lesion, and the pin
  says so.

## Measurements (taken, not remembered)

Over the **42** programs the two new files carry as ANSWERED, a build of the parent commit (`8aa3029`, **ADR
0411**) beside a build of this one, each asked of the pinned CPython:

* **26 moved to the reference's answer** — the popped slot through the printer and through `str`, the identity
  question and its negation and its `==` twin, the void/text/double/nested-list/dict kinds, the slot read, the loop
  item and the loop item's identity, a chain of three names, the popped slot as a condition and inside a literal, a
  method's return, the default and negative indices, and all three promotions read back through the other name —
  including the two programs **Gap R.415** was filed with.
* **16 controls held** — the name's own pop, the printer and `len` asked of the alias, an empty alias's falsiness,
  a single-kind list's pop, a scalar alias carrying no list claim, and the same reads written against the name in a
  program that also declares the alias.
* **0 regressions, 0 of the answered family still differing.**
* The owed shapes reproduce **byte-for-byte on the parent build**: the callee's printer (`0`), the dict's value
  written through the alias (`{'a': 1, 'b': 1}` / `{'a': 'x', 'b': 'x'}`), and the two SIGSEGVs. That column is why
  they are rows and not this commit's damage.

`tools/recmerge` wrote **36** entries from the reference for this family (**9025 → 9061**) and refused none of
them: the compiled leg agreed with the reference on every source this row claims to have paid (**ADR 0309**'s rule
that the compiled leg never writes an answer, checked by the recorder rather than by this paragraph). The pin
**Gap R.415** left in `TestWhatTheVoidQuestionStillOwesIsFiledNotFixed` and its CLI twin is **deleted, not
edited** — the program moved to the answered family of this file instead — which is the only way a paid debt is
distinguishable from a rewritten one (**ADR 0411**'s precedent).

`make testparallel` green; every emitted module verifier-clean; exit **2** forbidden in both new files and present
only in the two pinned **Gap R.419** crashes, which name their own row.
