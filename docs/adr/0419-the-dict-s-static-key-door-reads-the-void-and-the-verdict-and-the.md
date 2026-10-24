# The dict's static key door reads the void and the verdict — and the roads that LABEL an operand ask the object first

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.285** (`d = {None: "a"}` / `print(d[None] + "b")` refused at
exit 1 with a sentence that is *false of the program*, and `print(d[None] * 2)` printed `0` at **exit 0**,
while the `1`-keyed twin answered both), and closes **Gap R.231** beside it as the side-effect its own
Definition of Done described (an integer key now carries `TagInt` to the tagged dict doors, so a number
subscripting a text-keyed dict misses the way the reference misses). It FILES two rows that the same
measurement uncovered and this commit deliberately does not fix — **Gap R.433** (the `min`/`max` fold's
fractional answer is a bare double where the position wants a value word; the slot's half of it is why
`foldableCandidate` declines a fractional `Index` at all) and **Gap R.434** (a tuple in a dict slot is
repeated and concatenated as a list, because the sequence door asks a *kind* where the pair door handed it a
*tag*) — and leaves the neighbours the measurement found already named: **Gap R.235** for the slice of an
undescribed base, **Gap R.429** for sorting keys of more than one kind, **Gap R.286** for a name whose last
binding was the void, **Gap R.247** for a key collection handed to `sorted`/`list`, **Gap R.146** for a pair
in a one-word index position, **Gap R.82** for arithmetic on a mixed container's slot. One commit, one
feature: the key door in `pkg/lang/heapargs.go` (`constKeysMatch`), the pair door beside it
(`slotPairWhereStaticLabelCannotWalk`), the loop arm in `pkg/lang/codegen_for.go`, the text-label road in
`pkg/lang/codegen_class.go`, the concatenation gate in `pkg/lang/codegen_binop.go`, the integer key's tag in
`pkg/lang/codegen_index.go`, the fold's decline in `pkg/lang/codegen_float.go`, and the four test files.

## Context

```
d = {None: "a"}
print(d[None] + "b")      #  "ab"  reference   →  refused, exit 1: "…CPython raises TypeError for it"
print(d[None] * 2)        #  "aa"  reference   →  0, at the exit code of success
d = {1: "a"}
print(d[1] + "b")         #  "ab"  — and THIS one answered, all along
```

One construct, two answers, decided by which **kind** the key was written with. `d[None]` and `d[1]` are
the same read through the same door; the difference is that the door which answers *what kind is this
entry* — `constKeysMatch`, which walks the dict's own literal — has arms for an integer, a text and a
float, and none for the void or a verdict. Every road that labels an operand before choosing a word for
it (the concatenation, the repeat, the method road, the print road, the length road) therefore got
"not a text" for one spelling of a key and "a text" for another. The refusal was worse than a wrong
answer because it was a *claim* — that CPython raises `TypeError` where CPython answers `"ab"` (**Gap
R.116**'s rule: a refusal has to be true of the program it refuses).

**The arm was not simply missing, and that is the whole reason this is one row.** ADR 0353 built it,
measured it, reverted it, and left the measurement in the comment above the door:

| program | reference | with the door open and the roads unchanged |
|---|---|---|
| `d = {None: "a"}` / `print("a" in d[None])` | `True` | `False` |
| `d = {None: 5}` / `print(1 in d[None])` | raises `argument of type 'int' is not iterable` | `False` |
| `d = {None: "ab"}` / `for x in d[None]: print(x)` | `a`, `b` | prints nothing |
| `d = {None: 5}` / `for x in d[None]: print(x)` | raises `'int' object is not iterable` | prints `0 1 2` |

The two callers that broke are not readers of the entry — they are readers of the entry's **kind**, which
they use to decide what question to ask at all: walk its slots, its keys, its members, or test a
substring. Neither can walk a text, a number, a verdict or the void, and each reacted to a label it could
not honour by doing the nothing-shaped thing: reading the payload as a heap handle and answering at exit
0. That is the failure class this contract ranks worst — a wrong value at the exit code that means the
program ran.

## Decision

**1. Widen the two label roads FIRST, then open the door.** The membership test and the loop now ask the
object for the slot's `(payload, tag)` pair through `slotPairWhereStaticLabelCannotWalk`, which is
`builtSlotPair` with the static decline taken out — and which stands aside unless the static label is a
kind a loop cannot step over and a membership test cannot walk (a container label keeps the sound static
road, so ADR 0241's promise and its pinned rows are untouched). Asking the object means asking the tag the
*writer* left beside the payload, which is the same widening ADR 0323 gave a slot the compiler could not
describe at all — applied one step wider, to a slot the compiler *can* describe and the road still cannot
walk. The four rows above are now controls, not regressions, and the order is what makes them so: open the
door first and they come back.

**2. The void is a key of its own kind, and it is not the number 0.** `constKeysMatch` answers a `None`
key against a `None` index and nothing else, and it does so **before** any number arm can reach it,
because `foldConstInt` reads a `NoneLit` as the integer `0`. The old comment called that "harmless only
because every road that reaches it still asks the *object*"; luck is not a gate. It is refused at the door
now, in both directions, and the crossing is pinned twice — once as a decision of the door asked **as a
function** over a table of key/index pairs, once as a program raising `KeyError: None`. A verdict key keeps
the reference's own identity rule (`{1: "a"}[True]`, `{True: "a"}[1]`, `{0: "a"}[False]` are one lookup),
with the kind still asked before the number so an interned text can never collide with the integer it is
indexed by (**ADR 0232**).

**3. One question per operand, one answer — and two predicates, not one.** `exprIsString` answers *what
kind a value is*, and the literal that still describes the object's slots may answer it for an entry that
has no kind recorded on the name. `printsAsInternedStr` answers something stronger — that the **word** is
an `@str_tab` index a road may print or concatenate directly — and for a container whose slots describe
themselves that claim needs the tag beside the payload. Measured, and the measurement is the design: the
first version widened both. It took `print(["a", "b"][0])`, `xs = [1, "a", None]` / `print(xs[1])` and
`xs = [["a", "b"]]` / `print(xs[0][0])` from ADR 0331's pair door to a one-word road that **refused them**
— programs the reference prints, taken from green to exit 1. Both arms are in the file, the widened one
only where it is sound, and the reason the narrow one is not widened is written where the next reader
will trip over it.

**4. The concatenation asks the print road's question.** With the label road widened,
`{None: "a", 1: "b"}` / `print(d[None] + d[1])` emitted `%t = add i32 %g2, %g3` and printed the sum of two
interned indices as a text — `b`. It had printed `1` before this row, so both answers were wrong and only
one was new. The gate in front of the text-concatenation arm (`isStrOperand`) now also asks
`exprIsString`, the question the print road asks of the same operand, so an operand cannot be a text to
one door and a number to its neighbour. `rt_str_cat` answers, in both orders.

**5. The refusals and the traps stay refusals and traps.** `print(len(d[None]))` over `5` raises
`TypeError: object of type 'int' has no len()`; membership names the *type* and a loop names the *object*
(two sentences, one door writing the other's wording is ADR 0271's complaint); a missing key names the key
(`KeyError: None`) and is reachable by the program's own `except`. The exit-code contract holds: 0 for an
answer, 3 for the program's failure, 1 only for a shape this backend declines, 2 never.

## Consequences

* The two programs the row is named for answer `ab` and `aa` on the compiled leg, the record leg and the
  CLI leg, as do twenty more shapes of the same operand (the method road, the length road, the
  interpolation field, `str`/`repr`, the equality door, the condition door, a container element, a call
  argument, a character and a slice of the entry).
* Sixteen shapes that were **wrong answers at exit 0** are the reference's answers now, and two of them
  never had a `None` anywhere in the source: `xs = [5]` / `print(1 in xs[0])` answered `False` and
  `xs = ["ab"]` / `for x in xs[0]: print(x)` printed nothing. A defect found while fixing another defect
  is still a defect, and it is closed here rather than filed, because the door that caused it is the door
  this row opens.
* **95** sources went to the record through `tools/recmerge` (**9172 → 9267**), each recorded from the
  reference with the compiled leg cross-checked; `recmerge` refused four sources rather than let an
  answer be self-certified, and those are the pinned owed rows below. Twelve more that a first pass wrote were
  WITHDRAWN by hand: they were `print(f"{d[None]}")` and `print(f"{d[1]!r}")`, interpolated slot reads, and
  `TestTheRecordNeverInterpolatedASlotReadSoTheFieldIsWitnessedByItsPrintTwinAlone` keeps the record leg
  permanently silent about that shape. The f-string field is witnessed by its `print` twin in the same table
  and by the live-`python3` CLI row — a minted record is worth less than the property that makes records
  trustworthy.
* Two defects the change made *visible* are filed rather than fixed, because fixing them is a second row's
  work: **Gap R.433** and **Gap R.434**, each pinned with today's bytes beside the reference's in both suites
  (`TestWhatTheVoidKeyLabelRoadStillCannotReachIsFiledNotFixed`,
  `TestTheVoidKeyRoadsAnswerWrongArePinnedBesideTheReference`), so closing either row fails its pin.
* The two pins Gap R.285 kept in `integration/dict_none_bool_key_test.go` are **deleted, not edited** —
  the answers moved to `integration/none_key_label_roads_test.go` with CPython's bytes as the expectation.
* The key door is asked **as a function** over 21 key/index pairs, in both directions. A door tested only
  through programs that already reach it proves nothing about the keys it turns away — and the `{0: "a"}`
  read by `None` crossing is exactly such a key, because the object road that saves it today could be the
  road a later refactor removes.
* `integration/programs/void_key_label_roads.gy` joins the corpus as an asserted matrix row: one program,
  line for line against CPython, whose expectations are what the reference printed.
* Nothing here is a new representation: no new runtime helper, no new boxed value, no new tag. The pair
  door, the tagged loop helpers (`@rt_iter_len` / `@rt_iter_elem` / `@rt_iter_tag`), the tagged membership
  doors (`@rt_contains_tagged` and its dict/set siblings) and the text doors (`@rt_str_cat`,
  `@rt_str_repeat`, `@rt_str_case`) already existed; this row routes to them.

## Alternatives rejected

* **Open the door and leave the two roads alone.** Rejected on the measurement above — four programs move
  from the reference's answer to a plausible-looking one at exit 0. Rejected specifically because the
  four include a loop that prints nothing and a membership test that answers `False`, which are the two
  shapes a program is most likely to sail past.
* **Answer the void key as the integer 0 (what `foldConstInt` already says).** Rejected: `{0: "a"}` read
  by `None` becomes an entry the program never wrote. It happens to be caught today by the object's
  tag-aware lookup, and a gate that is only correct because some other road exists is not a gate.
* **Widen `printsAsInternedStr` as well.** Rejected on measurement (four pinned programs went from green
  to exit 1). The two predicates are not synonyms and the fix is to keep them answering their own
  questions, not to make the wider claim portable between them.
* **Refuse the operator shapes instead of answering them.** Rejected: the reference answers all of them,
  the record answers them, and ADR 0273's rule is that a gate must not cost a program that compiles today
  — refusing `d[None] + "b"` when `d[1] + "b"` answers is not honesty, it is an accident about the key's
  spelling.
* **Give the class of problem a new runtime door (a tagged dict read that carries the kind itself).**
  Rejected as unnecessary: the tagged read already exists (`rt_dict_get_tagged` + `rt_dict_value_tag`,
  ADR 0327/ADR 0353). What was missing was routing, and a second door would be a second answer.
