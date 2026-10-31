# ADR 0478 — a set asked of a set is looked up as the frozenset it hashes as, and it is the HAYSTACK that decides which needles it refuses to hash

**Status:** Accepted
**Date:** 2026-11-03
**Decides:** roadmap **Gap R.586** (`print({1} in {1, 2})` died `TypeError: unhashable type: 'set'` at **exit 3** where the reference prints `False`; filed 2026-11-02 while closing **Gap R.582**/**Gap R.583** under **ADR 0476**, and pinned in the residue table of `pkg/lang/membership_needle_pair_test.go` from the day it was filed — the same program behaving identically at the parent commit is what made it a row rather than a regression).
**Files:** no new file. The change is one parameter on one gate, one table, and one promise reader:
`unhashableNeedleKinds` (the kinds a hashing haystack refuses, now read by three doors), `unhashableKeyGate(needle, haystack, line)` (the gate asked of the PAIR), and `programPromisesASet` in `pkg/lang/membership_kind.go` (the binder walk's answer to "does the PROGRAM settle this name's set-hood").
**Keeps open:** **Gap R.587** (a non-text needle asked of a TEXT haystack: three spellings reach `llc` as a global in an `i32` argument — **exit 2** — two answer `True` at **exit 0**, three die of SIGSEGV), **Gap R.588** (a haystack whose set-hood is a run-time fact — bound inside an arm, bound by a loop target — keeps today's raise where the reference answers), **Gap R.589** (a parameter used as a membership needle is kinded by one call site, so the second call with another kind answers wrong at **exit 0**), **L11.1** (the tagged value word all of this is a workaround for), **ADR 0425**'s **Gap R.422** (the removal door's residue).
**Related:** ADR 0328 (the unhashable rule and the three kinds, which this ADR does not weaken one notch), ADR 0425 (the removal door measured the identical asymmetry for `discard`/`remove` and was allowed it; the membership door was not), ADR 0476 (the guard keyed by KIND rather than by "is this a container", which is what keeps `(1, {2}) in {1, 2}` raising), ADR 0232 (one question asked once — the refused-kind table now lives in one function that the compile-time gate and both run-time guards read), ADR 0452 / ADR 0453 (the fold may not kind what it cannot see; a name-keyed notebook is a summary, not a promise), ADR 0273 (a gate that opens may not cost a program that answers today), ADR 0214 (a raise stays the program's: catchable, exit 3, reported in the program's words), ADR 0166 (**exit 2** is the compiler's own bug; a wrong value at **exit 0** is worse than a raise), ADR 0302 (both witness legs read inside every case).

## The rule

**A haystack decides which needles it refuses to hash.** "May this value be hashed?" is one question and it has
two answers, because it is asked by two objects:

```
print({1} in {1, 2})       # False        — a SET converts the needle to the frozenset it hashes as
print({1} in {1: 2})       # TypeError: unhashable type: 'set'   — a DICT asks the key's own hash
print([1] in {1, 2})       # TypeError: unhashable type: 'list'  — the conversion is for a set needle only
print((1, {2}) in {1, 2})  # TypeError: unhashable type: 'set'   — a tuple is hashed ELEMENT BY ELEMENT
print({1} in [{1}])        # True         — a list walks; nothing is hashed and nothing is refused
```

CPython's `set_contains`, `set_discard` and `set_remove` build a frozenset out of a set or frozenset needle and
look THAT up; the needle is never asked for its own hash, so no `TypeError` can come of it. This language has no
`frozenset` to find — a set cannot hold one, and `{…}` and `set()` are the only spellings of the empty one — so
the conversion finds no member and the answer is **absence**, which is the reference's `False`. The answer is not
invented here: it is what the door beside it already returns when the scan matches nothing, which is why no new
road was written and no verdict folded in.

The rule's second half is the one this cycle nearly got wrong:

**The exemption is taken from the PROGRAM, never from a notebook.** A kind read out of the fourteen name-keyed
tables the compiler keeps is a SUMMARY of the assignments a pass happened to see, and one statement makes it a
lie:

```
s = {"a": 1}
if no():
    s = {1, 2}
print({1} in s)   # the run is standing in the other arm: the haystack IS the dict, and the reference raises
```

Skipping the raise there is a wrong answer at the exit code of success — the trade **ADR 0452** and **ADR 0453**
refuse in as many words. So the exemption fires only where the source settles BOTH sides: the spelled literal at
the question, a name whose EVERY binder this frame spelled as a set literal behind the read, or a name the source
never binds at all (a compiler-minted slot such as a materialised container answer, whose kind only the minting
road wrote). Everything else keeps the raise it has always kept, and is pinned in the residue tables with the
reference's answer beside it (**Gap R.588**).

## Context

The sentence was CPython's own and it was in the right file — the run-time guard `guardHashableNeedle` had been
carrying the set arm off since **ADR 0425** measured the same asymmetry for the two removal verbs:

```go
// pkg/lang/membership_literal.go, unchanged since ADR 0425
case *SetLit:
    // A SET haystack keeps the reference's own exception: `{1} in {1, 2}` answers, because a set
    // needle is looked up as the frozenset it converts to.
    g.guardHashableNeedle(b, needleTag, n.Span(), false)
case *DictLit:
    g.guardHashableNeedle(b, needleTag, n.Span(), true)
```

…for a needle whose kind arrives as a tag REGISTER. A needle whose kind the source spells never reaches that
guard, because the compile-time gate in front of it had already raised — and its own comment said so: *"A tag the
compiler spelled as a literal needs no guard: the static door (`unhashableKeyGate`) has refused that program
since Gap R.217."* One rule, two doors, and the two doors disagreed. That is **ADR 0232**'s defect class with a
wrong exit class attached: not a refusal that understates the compiler, a raise that overstates the language.

Measured before the change, all at exit 3 with one sentence (`tools/../run probes`, 22 shapes, CPython beside
each): the spelled pair, the named pair, `not in`, a condition, a `while`, `and`, a ternary, a parameter, a call's
container answer on either side, and `{0} in {0}` — whose payload is a heap handle and whose only member is the
number 0, which is the collision the tag exists to prevent (**ADR 0232**).

## Decision

1. **One table.** `unhashableNeedleKinds(setHaystack bool)` holds the refused kinds — `{list, dict, set}` for a
   key, `{list, dict}` for a set's member — and is read by the compile-time gate, by the store-side guard
   (`guardHashableTag`) and by the run-time guard (`guardHashableNeedle`). Before this, all three spelled the
   kinds inline in their own loops, and the set arm existed in two of them.
2. **The gate is asked of the pair.** `unhashableKeyGate(b, keyExpr, haystack, sp)` — `nil` haystack at the two
   subscript/store doors (a key is hashed by its own type, no conversion, nothing exempt), the right operand at
   the membership door. The exemption is `setHaystackConvertsTheNeedle(needle, haystack, line)`, one call, and it
   is asked BEFORE the message is built so the sentence cannot be minted for an exempt pair.
3. **The promise is the binder walk's, not the notebooks'.** `programPromisesASet` reuses `spelledScalarFrame`,
   which is the artifact **ADR 0453** built for exactly this argument: pass 1 counts every binder road in the
   frame (including the ones that hide in arms, loop targets, `with … as`, comprehension targets and walrus),
   pass 2 spells the candidates, and the prune retires a name whose binder count and spelled count differ. The
   container table widens that discipline by exactly one measure — several top-level bindings are allowed when
   they all spell the SAME family, because `s = {1}` / `s = {3}` is still a set whatever the run does — and a name
   that spelled two families is dead, not "the last one wins".
4. **A minted name is the strongest case, not the weakest.** `__cansN`, the rooted slot a container answer is
   materialised under (`container_return.go`), has no binder the source wrote; the only road that can store into
   it is the line that allocated it, so what that road recorded IS a promise. The reading is gated on the minting
   road's own `allocd` record, so a source-visible name can never reach it.
5. **The exemption does not replace the lookup.** The answered module still calls `rt_contains_tagged(hay, payload,
   tag)` — the members answer, with the kind beside every payload. `TestTheHashSetNeedleIsAskedOfTheMembersAndNotOfTheGate`
   asserts the call is there, and that the unhashable sentence is gone from the PROGRAM's own bodies (the runtime
   block carries the sentence whether or not the program can raise it, so the census is `programBodies`, the one
   every other IR row in the package uses).

## What was refused the exemption, and why

* a **tuple carrying a set** — CPython hashes a tuple by hashing its elements, so `(1, {2}) in {1, 2}` names 'set'.
  The exemption is keyed by the needle's OWN kind, which is **ADR 0476**'s rule read at this door.
* a **dict haystack** — `dict.__contains__` asks the key's own hash; `{1} in {1: 2}` must raise, and raising is
  what the negative half of the IR row asserts.
* a **list haystack** — nothing is hashed there; `{1} in [{1}]` is True in the reference, and an exemption that
  reached it would trade a True for a False.
* a **name the binder walk cannot settle** (**Gap R.588**) — including the loop-target case where every value the
  loop hands the name IS a set: the pass cannot see that, and a door that guesses here is the door **Gap R.582**
  and **Gap R.583** were filed against.
* the **removal door's** looser `memberIsABareSet` reading, deliberately NOT unified with the new one: the two
  consequences are not the same size. Getting the removal door wrong loses a verb call at exit 3; getting this one
  wrong prints `False` at exit 0 for a program the reference stops. The comments in both files say so, so neither
  is quietly widened into the other.

## Consequences

* Twenty-two answered shapes, ten raise shapes and four catchable shapes move to the record leg and the reference
  leg in the same commit (`pkg/lang/membership_set_needle_test.go`, `integration/membership_set_needle_test.go`,
  `integration/programs/membership_set_needle.gy` + `.expected` read from `python3`, 49 record entries via
  `tools/recmerge`). The oracle matrix carries the program at `match`, `fail 0`, `oracle_drift 0`.
* The residue table in `membership_needle_pair_test.go` that pinned this row is DELETED rather than edited: a row
  whose compiled answer has become the reference's has nothing left to pin.
* The sweep that produced the fix measured three families this row does not own and left them as rows rather than
  absorbing them: **Gap R.587** (a non-text needle asked of a text — the exit-2/SIGSEGV/wrong-True family),
  **Gap R.588** (the run-time-fact haystack), **Gap R.589** (a parameter kinded by one call site). Each is pinned
  with today's answer and the reference's beside it, so the next cycle inherits evidence rather than folklore.
* No refusal changed wording, so the capability registry, `--json` schema and the exit-code contract are untouched.
