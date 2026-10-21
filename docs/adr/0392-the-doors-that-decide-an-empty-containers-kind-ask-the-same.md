# The doors that decide an empty container's kind ask the door the roads that store it ask

**Roadmap:** Gap R.364 (closed here — `len`, the comparison, the sort, the concatenation and the signless
roads for `set()` / `list()` / `dict()`) ·
Gap R.360 (paid by **ADR 0391** — the store side of the same value) ·
Gap R.175 (the container-ordering helper the literal spellings also lack — `set() < set()` stays filed) ·
Gap R.81 (the hashability rule a set member written as a call still escapes) ·
Gap R.171 / Gap R.67 / Gap I.2 (the refusals this row leaves standing, each in its own words) ·
L11.1 (the tagged value word, in the positions that decide before they store) ·
ADR 0391 (the label; this ADR is the same door, one pair further on) ·
ADR 0333 (a container answer needs the table, not a guess — the decide-then-build pair) ·
ADR 0232 (one question, asked once) ·
ADR 0271 / ADR 0266 (`abs` names the operation, the unary minus names its own) ·
ADR 0214 (a raise the reference makes is emitted as a raise, not as a refusal) ·
ADR 0166 (a wrong value printed at exit 0 is the defect this repo files rather than ships)

## Status

Accepted. Landed 2026-10-21.

## Context

ADR 0391 wrote the set's, list's and dict's tag into every slot that stores one of them. Four shapes kept
answering anyway, and the reason is that a container value is asked about twice in this backend, in two
different places:

```python
print(len(set()))      # CPython: 0     — backend: codegen refuses, "len requires an inline list/dict/set literal"
print(list() == [])    # CPython: True  — backend: codegen refuses, "comparing a container with an empty list
                       #                             needs a tagged value"
print(set() == set())  # CPython: True  — backend: False, at exit 0, no diagnostic
print(abs(list()))     # CPython: TypeError: bad operand type for abs(): 'list'
                       #                — backend: 0, at exit 0, no diagnostic
```

The roads that STORE a value ask `elemKindTag`, and ADR 0391 gave that predicate the empty-container arm.
The roads that DECIDE — before anything is stored, before anything is printed, sometimes instead of any of
it — ask a different pair:

```
staticContainerKind(e)   "what would the handle this expression denotes be, if the compiler is willing to say"
containerHandleOf(b, e)  "then build it"
```

and a third, `isContainerExpr`, gates the comparison road. All three knew one shape of call: a callee the
container-answer scan names. `set()` is a builtin spelling, not a scan result, so:

* `len`'s gate declined, because the door behind it asks for an *inline literal*;
* the comparison's gate could not see a container on one side, fell to its "prove the other side is a
  scalar" branch, and refused with ADR 0189's honest-but-wrong-for-this-program sentence;
* `set() == set()` skipped the comparison road entirely and reached the integer compare, where two distinct
  handles are unequal — the answer the tag would have prevented, printed at exit 0;
* `abs`'s kind door named nothing, so the operand went to the numeric road, which folded a handle to `0`.

The control that localises it: **the literal spellings answer all four.** `abs([1])` raises
`bad operand type for abs(): 'list'` word for word, `[] == []` is `True`, `len(xs)` over `xs = set()` is `0`.
The value was never the problem; the three ways of spelling it were, and the ADR 0391 lesson repeated — a
gate grows an arm, and the road beside it does not.

## Decision

**The same door, one pair further.** The arm ADR 0391 put in `elemKindTag` is put in the deciding doors, and
it is the same call: `(*irGen).emptyContainerCall`, the shape plus `builtinShadowed`, so a program that
defines `set` itself is never labelled by a builtin it does not call.

* `staticContainerKind`'s `*Call` arm asks it **before** the container-answer scan and returns `"list"` /
  `"dict"` / `"set"`.
* `containerHandleOf`'s `*Call` arm asks it and builds the literal through `containerOperand` — the pair
  stays a pair (ADR 0333): a door that decides without its build twin is how a refusal becomes a half-emitted
  module, which is exit 2, ADR 0166's forbidden class.
* `isContainerExpr`'s `*Call` arm asks it **before** returning false for a callee the scan does not name —
  the position where the tail of that function, which does know the spelling, was unreachable.
* `negationOperandKind`'s `*Call` arm names `"list"` / `"dict"` / `"set"`, which is the door both `abs`
  (ADR 0271's per-kind sentence) and the unary minus (ADR 0266's) consult, so the raise is emitted where the
  operand is, with the reference's wording, reachable by the program's own `except TypeError:`.

Nothing was added to the runtime. No door was given a new answer — each was given the answer the literal had
always got.

## What moved, measured

Twenty-four programs changed answer, and every one of them moved to CPython's text:

| program | before | after | CPython |
|---|---|---|---|
| `print(len(list()))` / `(set())` / `(dict())` | refusal | `0` | `0` |
| `print(len(list()) + len(set()))` | refusal | `0` | `0` |
| `print(list() == [])`, `([] == list())` | refusal | `True` | `True` |
| `print(dict() == {})`, `print(list() != [])` | refusal | `True` / `False` | same |
| `print(set() == {1})`, `print(list() == [1])` | refusal | `False` | `False` |
| `print(set() == set())`, `print(list() == list())` | `False` at exit 0 | `True` | `True` |
| `print(list() in [[]])` | `False` at exit 0 | `True` | `True` |
| `print(sorted(set()))`, `print(sorted(list()))` | refusal | `[]` | `[]` |
| `print(list() + [1])`, `print([1] + list())` | raise of `unsupported operand` | `[1]` | `[1]` |
| `print(list() * 2)` | `0` at exit 0 | `[]` | `[]` |
| `print(abs(list()))`, `(set())`, `(dict())` | `0` at exit 0 | `TypeError: bad operand type for abs(): '<kind>'`, exit 3 | same |
| `print(-list())`, `-set()`, `-dict()` | `0` at exit 0 | `TypeError: bad operand type for unary -: '<kind>'`, exit 3 | same |

Two refusals were *better* than the answer they replaced, which is the direction ADR 0166 says to be
careful about: `list() + [1]` had been raising `unsupported operand type(s)` — a raise the reference does not
make — and now prints `[1]`.

## The IR, asserted

`pkg/lang/empty_container_decide_test.go` pins the shape, not the story:

```
print(len(set()))     →  %t1 = call i32 @rt_alloc(i32 3) … %t3 = call i32 @rt_heap_len(i32 %t1)
print(set() == set()) →  %t5 = call i32 @rt_container_eq(i32 %t1, i32 %t3)     and NOT icmp eq i32 %t1, %t3
print(abs(list()))    →  @.str1 = … c"TypeError: bad operand type for abs(): 'list'" … rt_die   and no `sub i32`
```

The negative half matters as much: an all-integer literal still takes `len` from its own table, `[1] == [1]`
still compares through the container helper it always used, `sum(xs)` over a container variable still refuses
with the fold door's sentence, and `{1} < {1, 2}` still refuses with Gap R.175's — the empty call did not buy
an ordering helper that does not exist.

## Alternatives rejected

* **A per-builtin special case in `len`, another in `==`, another in `abs`.** That is how the tree got here:
  four doors each holding their own list of container spellings, each list slightly different. Every one of
  the four symptoms was that divergence, and a fifth symptom appears the moment a fifth door is written.
* **Evaluating `set()` to a fresh empty literal at parse time.** Cheap, and it would have hidden the bug
  rather than fixed it: the AST would keep the literal, `--emit-ast` and `--fmt` would stop round-tripping
  what the author wrote, and the doors would still hold four spelling lists for the shapes that did come from
  the source as calls.
* **Refusing all four.** The reference answers all four, the literal spellings answer all four on this
  backend, and ADR 0166's ladder is one-way: an answer may not become a refusal.
* **Fixing `set() < set()` in the same commit.** It answers `True` against the reference's `False` — but so
  does the *literal* pair `{1} < {1, 2}` refuse-to-answer differently, and the missing piece is
  `rt_container_order`, which the literal spellings need too. That is **Gap R.175**'s helper, not this row's
  spelling; the shape is pinned with today's bytes in the filed table.

## Consequences

* `tools/recmerge` accepted **28** new sources (the record to **8334**), the six trap sources among them —
  an entry whose compiled leg disagrees is refused by the recorder, so the table is again the reference's
  answer and not this backend's opinion.
* The four rows ADR 0391 filed as `Gap R.364` — three refusals and two wrong answers — are **deleted** from
  both packages' filed tables, as their own text required; the table keeps only what still does not answer
  (`sum(list())`, whose refusal is the same one `sum(xs)` gives, `set() < set()` under Gap R.175, `{set()}`
  under Gap R.81, `str()` of a loop variable under Gap R.171, and a container-answering call as an element
  under Gap R.67).
* The standing lesson for the next gate that grows an arm: `grep` the question, not the road.
  `isContainerLiteral(` / `emptyContainerLiteral(` / `staticContainerKind(` each answer a list of doors, and
  the doors on more than one list are where the next row is.
