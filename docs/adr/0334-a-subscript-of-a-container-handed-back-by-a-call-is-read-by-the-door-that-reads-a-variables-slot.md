# ADR 0334: a subscript of a container handed back by a call is read by the door that reads a variable's slot

Status: accepted (2026-10-14)
Amends: [ADR 0326](0326-a-constant-that-is-not-there-is-the-programs-index-error.md) (the fold's bounds trap, which named this row as the door it could not reach), [ADR 0251](0251-a-slot-read-out-of-a-container-the-program-built-asks-the-object.md) (the same misattribution, for a container the program built), [ADR 0333](0333-a-container-returned-from-a-call-is-a-value.md) (the answer this door finally reads)
Extends: [ADR 0331](0331-a-list-literal-is-read-by-the-door-a-list-elements-are-read-by.md)/[ADR 0327](0327-a-dict-literal-is-read-by-the-door-a-dict-variable-uses.md) (a literal read by the door a variable uses), [ADR 0332](0332-a-literals-element-that-is-itself-a-read-is-stored-as-the-pair-that-read-answers.md) (the gate and the door asked one question), [ADR 0187](0187-payload-and-tag-travel-together.md) (the pair the read returns), [ADR 0232](0232-mixed-dicts-and-sets-answer-by-the-slots-they-carry.md) (the per-slot tag the read consults)
Supersedes: nothing
Related: roadmap L11.1, Gap R.214 (closes), Gap R.67 (paid by ADR 0333 — the dependency this row waited on), Gap R.217 / R.227 / R.228 / R.229 / R.33 / R.82 (the doors beside this one, filed there), Gap R.230 / R.231 / R.232 (newly measured here, filed here)
Owner: `pkg/lang/container_subscript_test.go`, `pkg/lang/heapargs.go`, `pkg/lang/codegen.go`

## Context

ADR 0333 made a returned container an object. It did not make the object readable **at the position the
program names**, and the difference was visible in three doors that had to agree:

```
def build():
    return [1, 2]

print([1, 2][7])         # IndexError: list index out of range   — the fold's door, since ADR 0326
ys = build()
print(ys[7])             # IndexError: list index out of range   — the variable's door, since ADR 0326
print(build()[7])        # exit 1: codegen: index requires an inline list/dict/set literal
print(build()[0])        # the same refusal, where the reference answers 1
```

Same question — read position 7 out of this list — and two doors that raised the program's catchable
`IndexError` and one that stopped the build with a compiler sentence. `docs/language.md` had carried that
third line as a refusal for a year; ADR 0326 named it in its own consequences as the row this one would pay.

The cause was the same one ADR 0251 recorded for a container the program *built*: the subscript road chooses
which of its doors to ask from the **spelling of the base**. A `*ListLit` goes to the fold, a `*Name` to the
variable's read, an `*Index` to the object below it — and a `*Call` to none of them, so it fell to the
generic refusal in `slotReadRefusal`. That was sound reasoning while a call had nothing to hand back: before
ADR 0333 `build()` returned `ret i32 @.lst1`, and there was no object to read. After ADR 0333 the answer is
a heap handle under a rooted name carrying exactly the notebooks a container variable has. The base's
spelling had stopped being the relevant fact and was still the one being asked.

Two behaviours, one question, is the defect class this tracker keeps pricing: `print(build()[0])` refusing
while `ys = build()` / `print(ys[0])` answered `1` is not a missing feature but a door in the wrong place —
and the row ranks above a plain refusal because the fold-shaped spelling (`[1, 2][7]`) proved the language
already had the trap, the message and the exit class.

## Decision

**The answer is lowered to the name it is recorded under, and the roads that have always read `ys[i]` read
it — with the ordering of those roads part of the decision.**

- **At the print door**, before any of its subscript arms is asked: `containerAnswerOperand` lowers
  `build()` to the rooted `__cansN` that `recordContainerAnswer` writes its notebooks for, and the subscript
  node is copied with its base set to that name. The copy matters — the AST is shared by every lowering of
  that source position, and rewriting a node in place would make the next reader of the same program see a
  different program (and leak a `store` into whichever road ran second).
- **In the value road** (`case *Index`, after the bad-index gate), the same rewrite and a re-dispatch, which
  is what carries the non-printing consumers — a binding, a further subscript, an arithmetic operand the
  proof already covers.
- **In the pair doors** (`runtimeContainerSide`, `runtimeSlotPairDeep`) a `*Call` side is admitted when the
  scan claims the callee and the call's arguments are carriable, and read through `containerHandleOf` — so
  the membership haystack, the loop's element, a literal's element and the tagged read beside them see the
  same object.
- **The notebooks are cleared when the object says so itself.** `recordContainerAnswer` asks the build site's
  own predicate whether the answer's slots carry per-slot tags, and a tagged container has **no** single kind:
  the notebooks (`listElemStr`/`listElemInt`, `dictKeyStr`/`dictKeyInt`/`dictValStr`/`dictValInt`,
  `setElemStr`/`setElemInt`) are cleared for a tagged answer, exactly as `heapDictFrom`/`heapSetFrom` clear them
  for the variable they build into. The list half of that was missing, and it was this row's own blocker:
  `def words(): return ["hi", "ho"]` / `print(words()[1])` refused with Gap R.82's "needs a single static kind"
  because a notebook said *text* over an object whose slots speak for themselves, so the reader took the
  untagged word road — which has no tag to print — instead of the mixed one that does. The whole-container
  printer was never wrong (`print(words())` printed `['hi', 'ho']`), because `containerAnswerPrinter` asks
  `mixedLists` **before** the notebooks; only the slot read consulted them. The clearing is one judgement with
  the printer, not a second opinion (ADR 0232's rule about claims over bytes).

The gate is asked before any of that, and it is the same gate that decides every other subscript: a call the
scan cannot name, or one whose arguments would put a double into a slot, is left exactly where it was, with
the refusal sentence it always had (ADR 0249's rule, and ADR 0333's carve-out, respected in both directions
by `TestAPositionThatCannotReadACallResultSubscriptRefusesThroughTheCLI` and
`TestThePairRoadKeepsWhatTheCallResultSubscriptLeavesIt`).

### The slice is deliberately not widened

`print(b()[0:2])` still refuses, and that is the decision rather than the oversight. A subscript is two
AST nodes and the second one's road is broken for **every** base, not just for an answer:

```
ys = [1.5, "a", [7]]
print(ys[0:2])            # CPython [1.5, 'a']      this backend [1, 0]
zs = ys[0:2]
print(zs)                 # CPython [1, 2]          this backend 1
```

The slice road copies a list's words without their tags, and binding a slice hands back one word for an
object. Both answer at exit 0 where the reference raises nothing but a different string — the class the queue
ranks above every refusal. Rewriting a slice into the answer's name would have converted one honest refusal
at exit 1 into a **new** wrong number at exit 0, which is moving a defect down the ranking, not up it. The
slice of an answer is therefore filed with the doors the answer has not reached (Gap R.229) and the two
measurements above are filed as their own row (Gap R.230), where the fix belongs: give the slice road tags,
and the subscript road then admits the shape for free.

### The ordering is not an implementation detail

`print(f()[2])` where `f` returns `{1: "one", 2: "two"}` was the row's own trap. Answered through the
tagged door it printed `1` — the value's `@str_tab` index — because a dict built from a **literal** carries
no per-slot tags at all, and `rt_dict_get_tagged` on an untagged object returns tag 0, which means "int" to
the printer. The bound form had always printed `two`, because the *kind roads* (the notebooks
`dictValStr`, `dictKeyStr`, `listElemStr`, `setElemStr`) run first in the print door and answer from what
the writer recorded. So the rewrite happens **before** them, not after: an answer must meet the same roads a
variable meets, in the same order, or "the same read of the same object" is a slogan.

The mirror-image bug was the temptation to fix that by claiming `mixedDicts` for every dict answer. It made
`print(f()[2])` right and broke `programs/subscript_assign.gy` — `for k in m:` / `t = t + m[k]` over a dict
the body **filled in** (`d[i] = i * 10`), whose slots carry no tags because the store road wrote them
untagged. Claiming a tag the writer did not write sends the reader to an empty tag array, and the pair road
then refuses with Gap R.146's words. `containerAnswerDictCarriesTags` therefore asks `heapDictFrom`'s own
question (`taggableMixedDict || literalTakesPairSlots`) of each **literal** arm and answers *no* for an arm
it cannot see — the table reports what was written, never what would be convenient (ADR 0232, read as a
claim about bytes rather than an opinion).

## What the row leaves standing, filed rather than bundled

Each carries the reference's answer beside this backend's, in
`TestWhatTheCallResultSubscriptStillCannotReachIsFiledNotFixed` and its CLI twin, and fails the day the door
grows:

| Program | Reference | This backend | Owner |
|---|---|---|---|
| `def s(): return {1, 2}` / `print(s()[0])` | `TypeError: 'set' object is not subscriptable` | refuses, on the **same sentence** the set in a variable gets | Gap R.217 (a set has no slots; the raise is that door's) |
| `def n(): return None` / `print(n()[0])` | `TypeError: 'NoneType' object is not subscriptable` | refuses | **Gap R.228** (a `None` handed back has no return kind) |
| `def n(): return 5` / `print(n()[0])` | `TypeError: 'int' object is not subscriptable` | refuses | Gap R.227 (the same shape at a call) |
| `b()[0] = 9` | `1` | refuses | **Gap R.229** (the item-assignment road takes a variable on the left) |
| `print(b()[0:2])` | `[1, 2]` | refuses with ADR 0333's one-word sentence | **Gap R.229 / R.230** (the slice door has no road for an answer, and no tags for anybody) |
| `len(b()[0:2])` | `2` | refuses | Gap R.33 (sequence operations) |
| `print(b()[0] + b()[1])` on text slots | `xy` | refuses | Gap R.82 (no road joins a text out of two slots) |
| `print(mk()[0] + mk()[1])` on float slots | `4.0` | refuses with the pair road's sentence | Gap R.82 (kept: this row did not widen arithmetic over slots) |
| `print(mk()[0]["k"] + 1)` | `2` | refuses: `index must be a constant` | Gap R.82 (the numeric use of a slot) |
| `ys = [1.5, "a", [7]]` / `print(ys[0:2])` | `[1.5, 'a']` | `[1, 0]` at exit 0 | **Gap R.230** (the slice copies words, not pairs) |
| `ys = [1,2,3]` / `zs = ys[0:2]` / `print(zs)` | `[1, 2]` | `1` at exit 0 | **Gap R.230** (binding a slice answers the handle) |
| `d = {"a": 1, "b": 2}` / `print(d[0], d[1])` | `KeyError: 0` | `1 2` at exit 0 | **Gap R.231** (a number is a key; the read treats it as a position) |
| `print(words()[0] in words())` | `True` | refuses, printing the needle's Go representation | **Gap R.232** (an unprovable needle, and a sentence that names no construct) |

Nothing was relaxed to clear a row: every refusal above is asserted to stay a refusal at the CLI, with
`codegen:` present (it says what is missing) and exit 2 forbidden, and the rows that answer today do so
against CPython's line, not this file's memory.

## IR / codegen consequences

- `build()[i]` emits the callee call, a rooted `alloca`/`store` for the handle, and then **the same
  `rt_get_elem`/`rt_tag_of`/`rt_dict_get_tagged` sequence `ys = build(); ys[i]` emits** — asserted by
  `TestTheCallResultReadIsTheBoundFormsRead`, which runs both and compares stdout *and* the raise, so the
  parity holds for traps as well as for answers.
- The bounds check is the one ADR 0330 positioned, so the sentence is the reference's (`list index out of
  range`, not the backend's older `index out of range`) and the trap is catchable — asserted by class
  **and** message against CPython, then caught by the program's own `except`.
- The fold's shapes allocate nothing: `print([1, 2][0])`, `print({1: 2}[1])`, `print([1, 2][0] + 1)` emit no
  `rt_alloc`, `rt_get_elem`, `rt_dict_get_tagged` or `rt_list_from` in `@main`.
- No module this row emits puts a global in a value position: `TestNoCallResultSubscriptModuleReturnsAGlobalToAValuePosition`
  compiles each answered program and greps the module for `ret i32 @.` (ADR 0188's rule, inherited from
  ADR 0333 — the shape that made `llc` reject the module and the CLI spend exit 2).
- The rewrite copies the subscript node rather than mutating it, so a call site lowered twice lowers the same
  source position twice, each with its own rooted name.

## Alternatives rejected

- **Teach the fold about a call base** (evaluate the callee, find its one literal return). Rejected for the
  same reason ADR 0327 and ADR 0331 refused to widen the fold: the fold has one word and no place for a tag.
  A call has as many returns as its body has roads; the object the answer names is a run-time fact.
- **A second reader for an answer's slots**, private to this row. Rejected: ADR 0241's single-read rule. Two
  readers of one slot diverge the first time a kind is added to the tag table, and the divergence is a wrong
  answer rather than an error.
- **Claiming per-slot tags on every container answer** so the tagged door always works. Rejected with a
  failing program: `programs/subscript_assign.gy`. A tag is a claim about bytes some writer wrote; this
  table reports them, and for a run-built container the writer is the store road.
- **Refusing `build()[i]` and filing the whole row.** Rejected: two-thirds of the shape already worked
  (the variable door, the fold door), the answer exists since ADR 0333, and a refusal whose neighbours raise
  is the asymmetry this contract treats as the defect, not as the honest position.
- **Rewriting the AST node in place.** Rejected: the same position is lowered by more than one road (print,
  then the value road on a re-dispatch), and an in-place rewrite makes the second road see a program the
  first one edited.
- **Clearing the element notebooks always**, rather than only when the object carries tags. Rejected: the
  notebooks are the only thing that tells `for x in build()` that its element is text and not the interned
  index the slot holds — clearing them unconditionally printed digits over an untagged object, which is how
  ADR 0333's loop rows came to depend on them.
- **Opening the slice door with the same rewrite.** Rejected above, on a measurement: the road it lands on
  answers `[1, 0]` and `1` at exit 0, so the "fix" would have shipped two more silently-wrong programs to
  close one honest refusal.

## Verification

- `pkg/lang/container_subscript_test.go`:
  - `TestASubscriptOnACallResultAnswersTheElement` — the answered table, every `want` read off CPython, the
    all-text answer included: `print(words()[1])`, `print(words())` and `for x in words(): print(x)` sit beside
    each other as the guard on the notebook clearing (the printer asks `mixedLists`, the loop and the slot read
    ask the slots — if the clearing ever runs on an untagged answer, those two rows print interned indices);
  - `TestASubscriptPastTheEndOfACallResultRaisesWhatTheReferenceRaises` — class **and** sentence, exit 2 forbidden;
  - `TestATrapInsideACallResultSubscriptIsCatchable` — the same raise taken by the program's `except`;
  - `TestTheCallResultReadIsTheBoundFormsRead` — the parity, run through one runner (`runForParity`) that can
    speak about an answer and a raise in the same breath;
  - `TestTheFoldStillOwnsTheLiteralRead` — the negative control over `@main`;
  - `TestEveryCallResultSubscriptModuleVerifies` — `llc` on every module the row emits;
  - `TestWhatTheCallResultSubscriptStillCannotReachIsFiledNotFixed` — the ledger table above;
  - `TestTheCallResultSubscriptRowsAreRecorded` — the record leg, so the door cannot be removed without
    removing the evidence.
- `integration/container_subscript_test.go`: `TestACallResultSubscriptAnswersThroughTheCLI` (the same source
  through `python3` and through `gustyc --eval`, expectations derived at run time),
  `TestACallResultSubscriptRaisesThroughTheCLI` (exit class, empty stdout, no `codegen:` in stderr),
  `TestAPositionThatCannotReadACallResultSubscriptRefusesThroughTheCLI` (the neighbours keep their own
  sentences — and the set row is asserted **not** to repeat the closed door's),
  `TestThePairRoadKeepsWhatTheCallResultSubscriptLeavesIt` (the float slot prints, the numeric uses keep the
  pair road's sentence), `TestTheFoldRoadStillCarriesNoDoorThroughACallResult` (`@main` allocates nothing for a
  read the fold answers), `TestNoCallResultSubscriptModuleReturnsAGlobalToAValuePosition`,
  `TestTheCallResultSubscriptProgramMatchesTheReferenceThroughTheCLI` and
  `TestTheCallResultSubscriptProgramIsAnAssertedMatrixRow`.
- `pkg/lang/constant_trap_test.go`: `TestTheCallResultStillRefuses` is **gone** — its own comment named the
  day it would become a raise row, and this is that day. The row moved to
  `TestASubscriptPastTheEndOfACallResultRaisesWhatTheReferenceRaises`; it did not disappear.
- `integration/programs/container_subscript.gy` + `integration/testdata/container_subscript.expected`,
  matrix row `programs/container_subscript`, oracle `match`: three kinds, a mixed list, a nested answer, a
  run-built list, a dict filled by the body, a loop-filled list, a forwarding call, a variable index, a
  `for j in range(3)` over positions, arithmetic and `str`/`repr` over a slot, equality against a literal, an
  all-text answer's slot, an int-keyed answer's slot, a list of dicts read through two subscripts, and both
  raises caught by their own class.
- `go test -tags=llvm20 -count=1 ./...` and `make testshards` green; every emitted module passes
  `opt -passes=verify` / `llc`.
