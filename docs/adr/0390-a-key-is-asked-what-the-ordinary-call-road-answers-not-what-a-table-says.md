# A `key=` is asked what the ordinary call road answers, not what a table of names says

**Roadmap:** Gap R.255 (the builtin-key clause, closed here; the call-expression key and the
not-a-callable-builtin keys stay open on that row) ·
Gap R.248 (the element's own tagged value word, which the keyed door turns out to need too) ·
Gap R.363 (filed here — `round` of a value with no `__round__` refuses instead of raising) ·
L11.1 (the tagged value word in the positions that read one word) ·
L11.7 (a function value, and the keys that are not callable builtins at all) ·
ADR 0340 (the key door that calls through the ordinary call road) ·
ADR 0345 (the defunctionalisation pass that gives a lambda key a name) ·
ADR 0233 (one construct, one answer — a builtin's answer comes from the road, not from a sentence) ·
ADR 0273 (a gate decides what the program states; it does not invent a missing half) ·
ADR 0166 (a module `llc` rejects is exit 2, the code reserved for a compiler bug) ·
ADR 0214 / ADR 0228 (a raise the reference makes is emitted as a raise, not as a refusal)

## Status

Accepted. Landed 2026-10-21.

## Context

`sorted(xs, key=f)` is the one place in this language where the program hands the compiler a **function to
call later, on values the compiler cannot see**. ADR 0340 settled how the call is made — the door synthesises
`f(element)` and lowers it through `g.value`, so arity, parameter pairs, closures, float returns and every raise
inside the key are the ordinary call's business. ADR 0345 gave a `key=lambda …` a declaration of its own so that
road could be reached at all.

One piece had never been reconciled with ADR 0340's own rule: which builtins may be keys at all. `builtinKey`
kept a **table of names** — `len`/`abs`/`ord`/`int` may, `str`/`repr` may as text, `float`/`round`/`sum`/`min`/
`max`/`sorted`/`reversed` may not — and the refusal for the third group read:

> the key answers something this backend has no comparator for from one word — either a float or a container

That sentence is not true of `round`. `round(x)` **with the one argument a key is called with** answers an
integer: `round(2.6)` prints `3`, `round(1.5)` and `round(2.5)` both print `2`, and `round(2.6) + 1` prints `4`,
all matching CPython's one-argument `round`. The two-argument form answers a float — and the two-argument form
is not the call a key ever makes. So the door was refusing a callable the ordinary road answers, on the strength
of a claim about a different call.

`bool` and `chr` sat in the same refusal for the same reason: the road answers `bool(x)` as the verdict the
payload already is and `chr(x)` as a text, and the comparator orders both of those words today.

Measuring the clause turned up the half that was actually missing. The keyed door keeps **one word for the
element it picks**, and at HEAD nobody had asked a float element for its tag:

```python
def f(x):
    return 1

print(sorted([2.5, 1.5], key=f))     # CPython: [2.5, 1.5] — backend: [0.0, 0.0], at exit 0
print(sorted([2.5, 1.5], key=abs))   # CPython: [1.5, 2.5] — backend: llc rejects the module, exit 2
print(sorted([3.5, 1.5, 2.5], key=str))  # CPython: [1.5, 2.5, 3.5] — backend: [0.0, 0.0, 0.0]
```

Three wrong answers and a toolchain failure, all silent, all for a program whose only unusual feature is that
its elements are floats.

## Decision

**1. The door asks the road; the table keeps only the answer-word claim.** `builtinKey` no longer decides which
builtins are callable — the ordinary road is the only authority on that, and a builtin it cannot call (`hash`,
`bytes`, `print`) already refuses with its own sentence. What the table keeps is the claim the door genuinely
needs and cannot get from anywhere else: **which word the one-argument answer arrives in**, because the
comparator `@rt_elem_gt` orders numbers and interned text and nothing else.

- number word: `len`, `abs`, `ord`, `int`, and now **`bool`** (the verdict the payload already is) and
  **`round`** (the whole number the reference's one-argument `round` answers);
- text word: `str`, `repr`, and now **`chr`**;
- refused as before, with the same sentence and the same owner: `float`, `sum`, `min`, `max`, `sorted`,
  `reversed` — a double or a container, both of which need the tag beside the payload (**Gap R.248**);
- everything else falls through to the road's own refusal, which is where `key=hash` and `key=bytes` live:
  those are not callable builtins in this backend at all, and a key-specific sentence would have hidden that.

**2. The element is asked for its tag, and the door refuses when it has none to give.** `keyedOrderError` now
refuses, for a callable key, over any element whose kind this pass reads as a float:

> ordering by `key=round` is refused over 3.0: the door returns the element it picks in one word, and a float
> element's kind travels in a tag beside its payload — the answer is the payload or a module `llc` rejects, and
> the tagged value word is what decides it (roadmap L11.1, Gap R.248)

This converts two wrong answers at exit 0 and one exit-2 toolchain failure into a refusal at exit 1 that names
the missing half and its row. It is not the fix — **Gap R.248** owns the element's tagged value word, and when
that lands the row deletes this arm. It is the difference between a door that declines and a door that lies.

**3. What the road answers for a bad operand is the road's sentence, not the door's.**
`sorted(["bbb", "a"], key=round)` now reaches `round("bbb")` on the ordinary call road and hears
`round: cannot round a string` — the same sentence the plain `round("a")` produces. The reference instead
**raises** `TypeError: type str doesn't define __round__ method`, catchable by the program's own
`except TypeError:`. That difference is a raise-vs-refusal defect of the builtin itself, and this row made it
visible from a second direction, so it is filed as **Gap R.363** rather than papered over with a key-specific
sentence.

## Consequences

- `print(sorted([3, 1, 2], key=round))` → `[1, 2, 3]`, `print(max([3, 1, 2], key=round))` → `3`,
  `print(min([3, 1, 2], key=round))` → `1`, `print(sorted([3, 1, 2], key=bool))` → `[3, 1, 2]` (every element
  truthy, so the order is the stable one — CPython's too), `print(sorted([0, 3, 0, 1], key=bool))` →
  `[0, 0, 3, 1]`, `print(sorted([3, 1, 2], key=chr))` → `[1, 2, 3]`, and the same with `reverse=True` →
  `[3, 2, 1]`.
- The refusals that stay, stay with their own words: `key=sum`/`key=min`/`key=max`/`key=sorted`/`key=float` name
  the missing comparator (Gap R.248), `key=print` and `key=f(1)` name L11.7, `key=hash` names the road that has
  no call to make.
- The keyed-float-element shapes move from exit 0 / exit 2 to exit 1 with the missing half named — a behaviour
  change that is deliberate and is asserted in `keyedRefused`.
- `key=str`, `key=abs`, `key=len`, `key=repr`, `key=int`, `key=ord` are untouched; the unkeyed sort, the
  documented positional repeat-flag reading, and every unkeyed answer are the negative-control table this row
  must not move.
- The door's doc comment now says which claim the table carries, so the next person does not read it as a
  capability list.

## Agentic rationale

The machine path is unchanged in shape — same flags, same refusal class at exit 1, same trap class at exit 3 —
and three programs moved from wrong-at-exit-0 to CPython's answer, which is the only kind of change an agent
consuming the compiler can act on without re-deriving it from `python3`. Two things keep the claim checkable:

- every answered row above is asserted against CPython on the compiled leg and on both CLI paths, with exit 2
  forbidden, in `pkg/lang/call_key_test.go` (`keyedAnswered`) and `integration/call_key_test.go`;
- every refusal row names what is missing **and which row owns it** (`keyedRefused`'s `owns` field), including
  the three the row newly refuses. A refusal whose sentence an agent cannot act on is how a gap stops being
  tracked, which is what `integration/compiled_or_refuses_test.go::refusesHonestly` exists to fail.

`--capabilities` and the registry are unchanged: none of these shapes has a code, because none of them is a new
refusal class — two are new answers, three are existing refusals reached from a new direction.

## Alternatives rejected

- **Keep the name table as the capability list and add three names to it.** That is what this commit does,
  superficially — but the table's *meaning* is what changed, and the difference shows up the next time a builtin
  gains an answer: a table that says "these are callable" is a second opinion about a call, and second opinions
  in this family ship wrong answers. The table now states only what no other component can: which word the
  answer arrives in.
- **Answer the keyed float-element shapes by widening the comparator here.** Rejected: that is Gap R.248's work
  (`@rt_sort`'s float comparator and the tagged copy that carries the elements), done in the one door that has
  the least claim to it. A refusal that names R.248 costs nothing when R.248 lands — the arm is deleted — where
  a local comparator would be a second sort to keep correct.
- **Leave the float-element answers as they were.** `[0.0, 0.0]` at exit 0 is the class this tracker calls the
  worst, and `key=abs` spending ADR 0166's reserved compiler-bug code on a program the reference runs is not a
  status quo to preserve by silence. The alternative to a refusal here was not an answer; it was a wrong number.
- **Make `sorted(["bbb","a"], key=round)` raise the reference's `TypeError` in this commit.** It is the right
  end state (ADR 0214/ADR 0228) and it is `round`'s own defect, not the key door's: the plain `round("a")`
  refuses identically. Fixing it means one `TypeError` sentence per kind on the road the signless call already
  uses — a feature of its own, so it is **Gap R.363** with that definition of done, and this row only records
  that the door now hears the road's answer rather than inventing its own.
- **Also pay `key=hash`.** `hash` is not a callable builtin in this backend at all (`print(hash(3))` refuses),
  and CPython's `hash` of a **text** is hash-randomised per process, so a compiled answer for it is not one
  number this suite could pin. Refusing it with the road's own sentence keeps the disagreement visible instead of
  answering with a number nobody can reproduce.

## Codegen / IR implications

- No new runtime helper and no new IR block: the paid keys call through `keyCallReg` → `g.value`, the same road
  that already lowers `abs(elem)` and `str(elem)` anywhere else in a program.
- The three newly-refused shapes emit **no** sort at all — the refusal happens in the scan, before the door
  emits, so no module reaches `llc` with the bad store the `key=abs` shape used to produce (the failure was
  `llc-20: … use of undefined value`/type mismatch on a float element carried in an `i32` word).
- Every module this row produces passes `llc -filetype=null`; the exit-2 count for the `sorted` family goes
  **down** by one shape (`key=abs` over floats), which is the only direction ADR 0166 permits.
