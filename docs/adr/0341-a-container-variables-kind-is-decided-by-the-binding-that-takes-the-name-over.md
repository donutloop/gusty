# A container variable's kind is decided by the binding that takes the name over

Status: accepted

Record leg: `pkg/lang/testdata/interpreter-golden.json`, read by `pkg/lang/golden.go`
(`evalGolden`, `runGoldenStdout`, `goldenRepr`) — the retired engine's record, as it stood
for the sources this row answers. Reference leg: CPython (`/usr/bin/python3`), through
`gustyc --oracle` and `integration/conformance_test.go`.

Decision owner: roadmap Gap Q.1 — "A container variable rebound to another kind keeps the
old kind" — whose plan cell is "settle a variable's kind from the object's tag instead of the
`listVars`/`runtimeDicts`/`runtimeSets` maps", and whose owner line is L11.1, the object model.

Amends [ADR 0172 — `None` is a singleton value](0172-none-is-a-singleton-value.md) and
[ADR 0270 — a rebinding retires every status the new value contradicts](0270-a-rebinding-retires-every-status-the-new-value-contradicts.md):
both decide a name's status at the binding; neither reached the container kinds, which were
being decided by six different binding roads that did not agree with each other.

Extends [ADR 0232 — a container's elements answer what the container was built from](0232-a-container-word-means-its-payload-and-its-tag.md),
[ADR 0241 — a container bound once and never mutated is folded to its literal value](0241-a-slot-is-read-back-by-the-tag-the-builder-wrote-remembered-at.md),
[ADR 0333 — a container handed to the runtime carries the pair that names its slots](0333-a-container-returned-from-a-call-is-a-value.md),
[ADR 0335 — an element read asks the container what it holds](0335-a-slice-hands-back-a-copy-of-the-bases-slots-tags-included-and-the-copy-is-a-list-the-records-know.md),
[ADR 0187 — traps are LLVM calls that unwind](0187-tagged-slots-are-written-as-pairs.md),
[ADR 0166 — the exit code contract](0166-unsupported-lowering-is-a-diagnostic.md) and
[ADR 0273 — a gate is asked, never run](0273-a-slot-read-handed-to-a-function-crosses-the-call-as-a-pair.md); related
[ADR 0302 — one execution backend](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md).

**What this is:** a container variable's kind is the kind of the object its **latest** binding
gave it, and exactly one door — `settleContainerKind` in `pkg/lang/rebind_kind.go` — writes
that claim. Every road that binds a name to a container (a literal, a comprehension, a call's
answer, a slice, a parameter) goes through the door, and every reader (`print`, `len`,
subscript, membership, the mutators, the ordering builtins, `for`, `in`, a trap) reads the one
claim the door left standing.

## Context

A name's container status was kept in fourteen name-keyed tables: three kind claims
(`listVars`, `runtimeDicts`, `runtimeSets`), three "my slots describe themselves" notebooks (`mixedLists`,
`mixedDicts`, `mixedSets`) and six element-kind notebooks (`listElemStr`/`Int`,
`setElemStr`/`Int`, `dictKeyStr`/`Int` and `dictValStr`/`Int`). Six binding roads wrote those
fourteen entries, and no two wrote them the same way:

- the **list-literal road cleared nothing at all** — it only set `listVars[name]`;
- the set-literal road cleared the list and dict claims but not the set's own element notebook;
- the dict-literal road cleared the list and set claims and all eight element notebooks;
- the comprehension road cleared the three claims plus `mixedLists`;
- `rebindsContainer`'s scalar arm cleared the three claims (twice, in copy-pasted pairs);
- the container-answer road (`recordContainerAnswer`, `recordContainerSlice`, `bindTaggedVar`)
  set claims without clearing a rival's.

`pkg/lang/rebind.go` — ADR 0270's door — retires tags, interned texts, bools, `None`, classes,
functions, callables and exceptions on every rebinding, and does **not** touch containers: it
defers to "whatever binding path handles containers decides" for the three claims. So the
container kind had no door, and the six roads that did have it disagreed.

The measured result, from CPython's own programs (all before this ADR, all at the exit code
that means the program ran):

| program | the reference | the compiled backend, before |
|---|---|---|
| `d = {1: 10}` / `d = [3, 4]` / `print(d)` | `[3, 4]` | `{3: 4, 0: 0}` at exit 0 |
| the same, `print(d[0])` | `3` | `0` at exit 0 |
| the same, `print(d[1])` | `4` | `KeyError: 1` at exit 3 |
| the same, `print(d[-1])` | `4` | `KeyError: -1` at exit 3 |
| the same, `d.append(9)` then `print(d)` | `[3, 4, 9]` | `{3: 4, 9: 0, 0: 0}` at exit 0 |
| `d = {1: 2}` / `d = [1, 2, 3]` / `d.sort()` / `print(d)` | `[1, 2, 3]` | `{1: 2, 3: 0, 0: 0}` at exit 0 |
| `d = {1: "x"}` / `d = [2, 3]` / `print(d)` | `[2, 3]` | **exit 2**, `llc` rejecting the module |
| `xs = [1, 2]` / `xs = {1: 10}` / `print(xs[1])` | `10` | `IndexError: list index out of range` at exit 3 |
| `xs = [1, 2]` / `xs = {"a": 1}` / `print(xs["a"])` | `1` | `KeyError: 'a'` at exit 3 |
| `xs = [1, 2]` / `xs = {1, 2}` / `print(1 in xs)` | `True` | `False` at exit 0 |
| `s = {1, 2}` / `s = [5, 6]` / `print(s)` | `[5, 6]` | `{5, 6}` at exit 0 |
| `kv = [1, 2]` / `kv = made_of([[1, 10]])` / `print(kv[1])` | `10` | `IndexError: list index out of range` at exit 3 |
| `v = [1, 2]` / `v = {1: 2}` / `v = 7` / `print(v)` | `7` | `list index out of range`, then `7` |
| `xs = ["a"]` / `xs = [1, 2]` / `print(xs)` | `[1, 2]` | a refusal: "a compiled list holds either strings or numbers, not both" |

The mechanism is the one the table's own shape shows. `x = {1: 10}` registers `runtimeDicts[x]`
and the dict's slot pairs; `x = [3, 4]` then registers `listVars[x]` and clears **nothing**.
The two claims sit on the same name, and each reader takes whichever it consults first: the
binder keeps writing into the dict slots the first binding built, the printer reads the list
slots, and the freed object — `emitFreeOld` had already freed the dict, because the list road
believed it was a list rebinding a list — answers reads after it was handed back. The `exit 2`
row is the same disagreement between the kind a read asked for and the word type the write
prepared; exit 2 is what ADR 0166 reserves for a compiler bug, which is exactly what it was.

The last row shows the same stale notebook read as a **refusal**: `recordElemKind` refuses when
the opposite element notebook is set, so a `[1, 2]` bound over an old `["a"]` was refused for a
kind no live object had.

## Decision

**One door decides a container variable's kind, and it decides it from the binding happening
right now.** `settleContainerKind(name, kind)` (in `pkg/lang/rebind_kind.go`, where
`kind` is `"list"`, `"dict"`, `"set"` or `""` for "not a container") does two things and
nothing else:

- it **clears every claim that disagrees** with the binding — the kind claim, the element-kind notebooks
  and the "my slots describe themselves" notebook of every kind the binding is *not*, plus all three
  "my slots describe themselves" bits unconditionally: eleven of the fourteen when a list takes over a
  name that held a dict, and all fourteen when the binding takes container-hood away; and
- it **sets the claim of the kind it is**, so the new object's readers find their road.

The door is deliberately narrower than a wipe, in two directions:

1. **It clears only what the binding contradicts.** A same-kind claim is left standing, so a
   name that stays a list keeps its element notebook — the literal-fold record of ADR 0241
   (`containerLits`, keyed "bound exactly once, never mutated") is dropped only when the kind
   changes, and the fold's own answers survive a rebinding to the same kind. The variant
   `settleContainerKindFromLiteral` additionally retires the bound kind's own element
   notebook, because a literal binding rebuilds every slot and inherits nothing; that is the
   variant that turns `xs = ["a"]` / `xs = [1, 2]` from a refusal into the answer `[1, 2]`.
   The mixed bits are cleared for the kind being bound too, which looks like a violation of the rule and is
   its application: every road that builds an object writes `mixedLists`/`mixedDicts`/`mixedSets` only in the
   branch where the slots carry tags, and none writes `false`. Leaving the bit standing would have a name that
   had been a mixed list keep walking the per-slot-tag readers over an object whose tags nobody wrote — the
   right answer for the wrong reason, which is the state ADR 0233 forbids. The gate table in the Witness
   section seeds all fourteen claims and prints the survivors, and it is what caught this.

2. **It decides nothing else.** A scalar binding (`kind == ""`) retires the container claims
   and leaves the tag, interned-text, bool, `None`, class and function statuses to ADR 0270's
   `forgetTaggedBinding`, which already runs on those roads. A container binding never touches
   them. One subject per door, so the two doors cannot undo each other.

The door is called from **every** binding road, after that road's `emitFreeOld` gate has asked
what it is freeing (the free is about the object being replaced; the claim is about the object
taking over — reading them in that order keeps the two questions apart): the list-literal,
set-literal, dict-literal and comprehension bindings, the scalar and same-kind arms of
`rebindsContainer`, the folded-static rewrite, the `sorted`/`reversed`/`list` call bindings,
`recordContainerAnswer` and `recordContainerSlice` in `container_return.go`, and `bindTaggedVar`
in `heapargs.go` for parameters.

The door is a gate in ADR 0273's sense: it writes name claims and emits nothing, advances no
counter, and asks nothing of the backend — no kind is decided by trial emission.

**What is not claimed.** The door decides a kind where the compiler **sees** the binding. A name
whose kind would have to be decided at run time — a container flowing through a heap argument
into a function that indexes it (Gap R.142), a container that reaches the ordering builtins as a
variable (Gap R.107, refused by ADR 0333's rule) — is still refused, and still owes L11.1's real
fix, the run-time object that carries its own kind. This ADR is the compile-time half of Gap
Q.1's plan cell; the run-time half stays open under L11.1's owner line.

## Consequences

- The fourteen tables stay, because they are the compiler's read-only view of a name, and rewriting
  every reader to ask an object would be the run-time fix, not this one. What changes is that
  they have **exactly one writer** for the question "what kind is this name".
- Six binding roads lose their hand-rolled clears. The list-literal road, which had none, gains
  the door; the copy-pasted duplicate clears in `rebindsContainer`'s scalar arm collapse into it.
- `print` of a rebound container, its `len`, its indexing, its membership, its mutators, its
  sorting, and a `for` over it now agree with each other — because they read one claim. Before,
  four of them were wrong at exit 0 and two raised the wrong `KeyError`.
- A container rebound over a container frees the old object and never reads it afterwards,
  because the stale claim that made the read possible is what the door removes. The residual
  aliasing use (`b = a` then `b = [...]` then `print(a[1])`) is a *different* object's lifetime —
  filed as Gap R.257 with the reference's answer, not pinned as an answer.
- The `exit 2` row is gone: a word asked for one kind is never handed to a reader of another, so
  the module `llc` rejected no longer exists. ADR 0166's code for a compiler bug is no longer
  produced by this program.
- Rows that were refusals for a stale notebook are answers now (`xs = ["a"]` / `xs = [1, 2]`).
  Rows that were refusals because the *object* cannot answer stay refusals, with the ADR that
  owns them named in the sentence.

## Witness

The tests ask the door, the readers and the roads, in the two languages the repo already speaks.

- `pkg/lang/rebind_kind_test.go` —
  `TestAReboundContainerAnswersWhatTheLatestBindingGaveIt` (52 rows, both legs in one table: the
  record leg through `HasGoldenAnswer` — a missing entry fails the row, so coverage cannot be deleted by
  deleting a record — and the compiled leg through `compiledOutOrRefusal` (the module `Compile` writes) and
  `runForParity` (the JIT that runs it); one table because the legs must not be allowed to drift apart), and
  the same family through the CLI against CPython in `integration/rebind_kind_test.go`;
  `TestTheContainerKindTrapNamesTheContainerTheLastBindingMade` (three traps by class and message
  through `jitTrap` — `IndexError: list index out of range` for an out-of-range position,
  `TypeError: list indices must be integers or slices, not str` for a text position in a list, so
  the *message proves* the latest binding's kind is the one that answered);
  `TestTheContainerKindTrapIsCatchable` (the trap is an exception value, so `except` still sees it);
  `TestTheRebindingDoorClearsWhatTheBindingContradicts`,
  `TestTheRebindingDoorLeavesWhatTheBindingAgreesWith` and
  `TestTheLiteralDoorClearsItsOwnKindsNotebookToo` (the gate asked as a map — every name is seeded with
  all fourteen claims and the survivors are the answer, including the restraint rows: a binding that stays
  a list keeps `listElemStr`, and a scalar binding leaves the tag/interned/bool statuses to ADR 0270);
  `TestTheRebindingDoorClearsNothingForAnotherName` (the door is name-scoped — `mixedLists` is the
  one notebook shared across scopes, so this is asked rather than assumed);
  `TestTheLiteralDoorClearsItsOwnKindsNotebookToo`;
  `TestAReboundContainerIsReadByTheSameRoadsAsAContainerBoundOnce` — the **road-identity witness**:
  for seven pairs, the program that rebinds a name and the program that binds the same name once
  must emit the *same multiset* of reader calls (`@rt_int_list_get`, `@rt_get`, `@rt_in`,
  `@rt_list_len`, `@rt_dyn_len`, `@rt_append`, `@rt_list_sort`, `@gy_print`, `@rt_dict_repr`,
  `@rt_set_repr`, `@rt_list_join`) inside `@main`/`@gy_*`;
  `TestTheAliasedContainerIsNotThisRowsDebt` and `TestWhatTheRebindingRowDoesNotOwn` (owed rows,
  refused and named).
- `integration/rebind_kind_test.go` — `TestAReboundContainerAtTheCLIMatchesCPython` (45 answered
  rows through the CLI against CPython), `TestAReboundContainerRaisesTheWordsTheLastBindingEarns` (three
  traps, class and message, at exit 3 and never exit 0), `TestAReboundContainersTrapsStayCatchable` (three
  `except` arms that still reach the raise), `TestWhatAReboundContainerStillOwesAtTheCLI` (the refusals the
  row does not own, pinned at exit 1 with the reference's answer beside them and the owning Gap row in the
  name), `TestASingleBindingContainerIsNotMovedByTheDoor` (ten negative controls: the door must not move a
  program that rebinds nothing), and `TestTheAliasedContainerIsADebtRowNotAnAnswer` (Gap R.257, pinned as
  debt).
- `integration/programs/rebound_container_kind.gy` with
  `integration/testdata/rebound_container_kind.expected` — one 68-line program, 26 lines printed,
  whose expectation is what CPython printed for it; matrix row `programs/rebound_container_kind`
  is a `match`.
- The record leg: `tools/recmerge -sources` added 212 entries for these programs, each of them the
  answer the reference gives — the compiled leg never writes an answer into the record — taking
  `pkg/lang/testdata/interpreter-golden.json` from 6665 to 6877 sources.
- The sweep the row is ranked on: **19 of the 58 probe shapes change answer** against the commit before
  (`/tmp`-fresh `git worktree` build of HEAD, so the comparison is the binary and not the tree), 15 of them
  from a fabricated value at exit 0 to CPython's value, one (`mixeddict->list print`) from **exit 2** to
  exit 0, and **none of the other 203 conformance programs changes answer** (the 204th is this row's own, which the commit before refused as Gap J.6's mixed-key sentence), which is the no-regression half of
  the same sweep.

## Alternatives rejected

- **Clear all fourteen entries on every container binding.** Rejected: it deletes the element
  notebook a same-kind rebinding still needs, which turns four working literal reads back into
  refusals, and it drops ADR 0241's fold record for programs that never changed kind. The door
  clears what the binding *contradicts* — that is the whole decision, and ADR 0270's wording
  ("every status the new value contradicts", not "every status") is the precedent.
- **Fix the readers instead — make `print` ask the tag first.** Rejected: the readers were never
  the disagreement. Six writers with fourteen tables is the disagreement; a reader that guesses
  among three claims is what a correct compiler must not have to do.
- **Delete the fourteen tables and ask the object (L11.1's plan cell literally).** Deferred, not
  rejected — it is the run-time half, and it needs the value word carrying its own kind, which is
  the whole of L11.1. Refusing to do it here is not refusing to do it ever: the refusals it would
  retire stay refusals in this commit, are named, and are Gap R.142 and Gap R.107's rows.
- **Let the list-literal road clear just the dict and set claims.** Rejected as an
  un-principled patch of the worst row: the bug was not that one road clearing too little, it was
  that roads cleared differently.
- **Decide the kind by emitting both readers and picking the one that verifies.** Rejected under
  ADR 0273: a gate is asked, never run.
- **Fold the door into `forgetTaggedBinding`.** Rejected: ADR 0270's door runs on every assignment
  and would then run the container rules onto non-container assignments, where it would read a
  name's stale container claim as a contradiction; and one subject per door is what keeps
  `TestTheRebindingDoorLeavesWhatTheBindingAgreesWith` askable.
