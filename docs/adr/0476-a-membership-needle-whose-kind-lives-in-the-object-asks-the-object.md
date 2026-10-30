# ADR 0476 — a membership needle whose kind lives in the object asks the object, and the fold may not claim a needle it cannot kind

**Status:** Accepted
**Date:** 2026-11-02
**Decides:** roadmap **Gap R.582** (a needle read out of a dict answers `False` where the reference answers `True`, and a container needle asked of that dict answers `False` where the reference refuses to hash it) and **Gap R.583** (the same needle in the haystack a source spells: `xs = [g()[1], 3]` / `print(xs[0] in xs)` is `False` at the exit code of success). Both were filed 2026-11-01 with a diagnosis — the container's slots are not claimed as self-describing — that the closing measurement disproved; the corrected cause travels with the rows.
**Files:** **Gap R.586** (`print({1} in {1, 2})` raises `unhashable type: 'set'` where the reference converts the needle to the frozenset it hashes as and answers `False`; measured while extending the unhashable guard, and **not caused by it** — the program behaves identically at the parent commit, which is what makes it a row rather than a regression report)
**Keeps open:** **Gap R.212** / **Gap R.83** (a needle whose kind only a run-time branch knows stays a refusal, and refuses in the program's words), **Gap R.232**'s residue beside **Gap R.67**, **L11.1** (the tagged value word every one of these is waiting for)
**Related:** ADR 0474 (the membership needle that is a slot read asks the equality pair door — this ADR moves *when* that door is asked), ADR 0232 (one question asked once; a payload without its tag is how `1 in ["1"]` became true), ADR 0286-style invented tag: **Gap R.286** (a dict KEY's tag answered `0` by the compiler, the same defect one door earlier), ADR 0328 (an unhashable key raises from its tag; this is the read half, at the third door), ADR 0324 (`guardHashableNeedle`, keyed by kind and not by "any container"), ADR 0332 / ADR 0457 / ADR 0459 (a literal's element that is a read writes payload and tag; name what the run time built), ADR 0192 (a fix reaches the position that asked, not the optimisation beside it), ADR 0166 (exit 2 is the compiler's bug; a plausible `False` at exit 0 is worse), ADR 0249 (a refusal names the program), ADR 0302 (the two witness legs)

## The rule

**Where a value's kind is a fact about an object, the object answers the question — and a fold may not answer it
on the object's behalf by defaulting.**

A membership test has two operands and this backend was asking each of them a different number of questions. The
haystack was asked of the object (`rt_contains_tagged`, `rt_dict_has_tagged`: walk the slots, compare payload
*between* the tags). The needle was asked of the compiler, and the compiler's kind question — `elemKindTag` — has
an answer for every expression it can see and one more for every expression it cannot: `TagInt`.

```ll
%t20 = call i32 @rt_dict_get_tagged(i32 %h2, i32 %t10, i32 4)    ; the needle's payload, from the object
%t21 = call i32 @rt_dict_value_tag(i32 %h2, i32 %t10, i32 4)     ; the needle's kind, from the object — available
%t14 = call i32 @rt_dict_has_tagged(i32 %t13, i32 %t20, i32 0)   ; …and the scan was handed 0 anyway
```

That is one question with two answers in one module, and the scan took the compiler's. `d = {"a": "a", "b": 2}` /
`print(d["a"] in d)` printed `False`: a text's `@str_tab` index, looked up among text keys under the tag of an
integer. **ADR 0232**'s collision, in the direction that prints a verdict instead of a refusal.

## Context

```
$ gustyc --eval 'd = {"a": "a", "b": 2}
  print(d["a"] in d)'
False                                       # CPython: True, at the same exit code

$ gustyc --eval 'd = {"a": 1, "b": [2]}
  print(d["b"] in d)'
False                                       # CPython: TypeError: unhashable type: 'list'

$ gustyc --eval 'def g():
      return [7, "z"]
    xs = [g()[1], 3]
    print(xs[0] in xs)'
False                                       # CPython: True
```

Three programs, three rows, one missing register. The wrong answers are the priority-1 class: exit 0, no
diagnostic, and the reference running all three. The unhashable half is the same defect wearing a second face —
`ADR 0328`'s rule (a container cannot be a key or a member; it raises, it is not absent) was implemented, and it
was implemented *on the tag*, which for these needles was a constant the compiler had written.

**ADR 0474** had already opened the right door: a needle that is a slot read asks `comparePair`, the equality
door, which answers payload and tag from the object. What it had not done was ask it *first*. In all three places
that take a needle's pair the order was fold-then-object, and the fold never declines an entry it cannot see into
— it answers `TagInt` — so the door that could answer was reached only by programs the fold had already refused.

## Decision

**Ask the object first, for exactly the needle the object alone can describe; leave the fold's precedence
everywhere else.**

* `mixedMembership` (a haystack the program named) asks `membershipNeedlePair` before `taggedOperand` when
  `membershipNeedIsObjectRead(n.L)` — the same `isTaggedSlotRead` question the equality door asks, so the two
  cannot disagree about which expressions are in question.
* `membershipOfLiteralContainer` (a haystack the source spells) asks it at the same precedence, after the
  haystack is built, so no effect an operand already performed is undone by a later decline.
* `foldNamesNeedle` — the fold's own claim to be able to name the needle with an `icmp eq i32` — now refuses to
  claim a needle the object describes. This is the half that makes the rule a rule: a fold that defaults a kind
  is not "declining", and a door ordered behind a decline never happens.
* The unhashable question is then asked of the tag that finally exists, at the dict and set haystacks, via
  **ADR 0324**'s `guardHashableNeedle` — and only for a tag the compiler did not spell, because the static door
  (`unhashableKeyGate`, **Gap R.217**) has refused a spelled container needle since it was written, and refusing
  it twice leaves two places to keep in step.

No new representation was introduced, and no scan loosened: the tagged scans were already right, which is the
reason a payload-only scan is not the fix (`1 in ["1"]` is the historical proof).

## Alternatives rejected

* **Loosen the payload scan** (compare a needle's word against members ignoring the tag). This is the exact
  change that once made `1 in ["1"]` true; **ADR 0232** exists because of it.
* **Claim the dict literal's slots** (add the `mixedDicts`/`literalNeedsTags` entry the row predicted), or **give
  the literal's builder a self-describing bit** (the `settleContainerKind` claim the sibling row predicted). Both
  were measured to be unnecessary: the builder already wrote `rt_dict_put_tagged` with the value's tag, and the
  pair door already answered. A claim nobody consults is a second notebook to keep in sync.
* **Make `elemKindTag` decline for a dict entry read.** Correct in effect and rejected on scope: `elemKindTag` is
  the container *builder's* kind question too, and a decline there turns today's answers into refusals across
  every container road. **ADR 0192** — reach the position that asked.
* **Refuse these needles** ("the kind is a run-time fact"). A refusal is the wrong answer to a program the
  reference runs, and here it would have been a refusal to answer a question the module already had an instruction
  for.
* **Guard every container needle as unhashable.** Rejected by the reference: `{1} in {1, 2}` is `False`, not a
  raise — a set needle into a set is looked up as the frozenset it hashes as. The guard is keyed by kind for that
  reason, and the residue is **Gap R.586**.

## Consequences

* `print(d["a"] in d)` is `True`, `print(d["b"] in d)` raises `unhashable type: 'list'` catchably at exit class 3,
  and `print(d["b"] in [2.5])`, `print(d["b"] in [[2]])`, `print(d["b"] in {2.5})` and the call-answer haystack all
  answer the reference. Fifteen sources join the record leg; `integration/programs/object_membership_needle.gy`
  is an asserted matrix row, `match` line-for-line against CPython.
* The IR is asserted, not just the value: some `rt_dict_has_tagged`/`rt_contains_tagged` call takes a payload that
  came out of `rt_dict_get_tagged`/`rt_get_elem` **and** a tag that is a register. A constant tag is the compiler
  stating an opinion about a value it has not looked at, and the value rows cannot see it come back.
* The fold's cost is asserted from the other side: `print(1 in [1, 2, 3])` still allocates nothing, and a needle
  the source settles still never opens the object door. Without that control this change is indistinguishable from
  a widening.
* Every door that asks "which (payload, tag) pair does this expression denote" now has one more precedent for
  asking the object *before* a fold that defaults rather than declines — the shape of **Gap R.286** generalised
  from dict keys to any operand a scan consumes.
