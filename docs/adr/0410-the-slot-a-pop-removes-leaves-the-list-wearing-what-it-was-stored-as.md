# The slot a `pop()` removes leaves the list wearing what it was stored as

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.411** (a `list.pop()` moves the payload and leaves the tag).
Files three rows it measured on the way and did not buy: **Gap R.412** (the popped slot compared with `None`),
**Gap R.413** (a popped text in an arithmetic position), **Gap R.414** (a body whose answer IS the popped
slot). One commit, one feature: `pkg/lang/codegen_ir_arena_globals.go`, `pkg/lang/pop_answer.go`,
`pkg/lang/codegen_method.go`, `pkg/lang/heapargs.go`, `pkg/lang/paircall.go`, `pkg/lang/answer_kind.go`,
`pkg/lang/pair_element_read.go`, `pkg/lang/codegen_builtin_print.go`, `pkg/lang/pop_tag_test.go`,
`integration/pop_tag_test.go`.

## Context

Three programs, all of them ordinary, all of them wrong:

```
xs = [1, 2, "a"]          xs = ["a", 1]          xs = ["a", 1]
xs.pop(0)                 xs.pop(0)              print(xs.pop(0))
print(xs)                 if xs[0]:              #  the reference: a
#  reference: [2, 'a']         print("t")         #  this backend, before: 0
#  this backend: [2, 0]     #  reference: prints t
                           #  this backend: exit 139, nothing on stdout
```

The first is a wrong answer at the exit code of success. The second is a segfault. The third is the same wrong
answer with a printer in front of it. They look like three defects and they are one:

A list slot in this backend is **two words** — a payload, and the tag that says what the payload means. A
text's payload is its index into `@str_tab`, a double's is the handle of a `@float_box` object, a container's
is a slot in `@heap`, and a `None`'s is `0`. Nothing in the container says which of those a number is; the tag
beside it does (ADR 0232's rule that the slots are the truth, ADR 0187's rule that a payload is never written
without its tag).

`@rt_pop` is the door that takes an element OUT of a list. It shifted the payloads left, shrank the length, and
returned the removed payload. It never touched `@heap_tags`, and it never reported the removed slot's tag. So
two things went wrong at once, and only one of them was `pop`'s:

* **the tail kept wearing the wrong kind.** Every element after the removed one moved down one slot in the
  payload array and stayed where it was in the tag array, so it was read under the kind of its new neighbour.
  `xs = [1, 2, "a"]` / `xs.pop(0)` slid `"a"`'s intern index into the slot whose tag still said `TagInt`, and
  `print(xs)` answered `[2, 0]`.
* **the answer was a word with no kind.** The removed slot's tag stayed in the array — and the array is not
  cleared on free, so what `print(xs.pop(0))` rendered was whatever kind that slot's last tenant had carried.
  A popped text printed `0`, the position of the first text the program interned; a popped double printed `1`,
  the float box's heap handle; a popped list printed the arena slot the inner list got. The answer depended on
  the program's *history* and not on its source.

The condition road was the same defect wearing a different face: the truth of a payload is "the word is not
zero", and an interned index is nonzero for reasons that have nothing to do with whether a text has characters
— which is why `if xs.pop(0):` took the then-arm for the empty-looking `"a"` and ADR 0409 had to make the same
distinction for a call's text answer.

What made this a segfault rather than only a lie was the read after the shift. `if xs[0]:` on
`xs = ["a", 1]` / `xs.pop(0)` reads payload `1` under tag `TagStr`, indexes `@str_tab` at the entry nobody
wrote, and hands the printer a pointer that is not a string (ADR 0166's artifact-at-fault class, exit 139).

The doors that *had* been told are the precedent, and they are in the same file: `rt_reverse`'s swap moves the
tag with the payload (**ADR 0187**), `rt_set_discard_tagged`'s shift does the same for the set door, and the
indexed store drops the old tag when it writes a new one. `pop` was the third positional door, and the one
nobody had walked.

## Decision

**One: the removal moves both words and reports the removed tag.** `@rt_pop` grows an out-parameter:

```
define internal i32 @rt_pop(i32 %h, i32 %i, i32* %outt)
```

The shift loop stores the tag from `@heap_tags[h][src]` into `@heap_tags[h][j]` beside the payload it already
moves, and the removed slot's tag is written through `%outt`. No new runtime entry point: the tag array is the
one `rt_tag_elem` writes and `rt_print_list_mixed` reads, so a popped element is labelled by the same word the
printer already consults and nothing in the module can disagree with anything else about what the element was.

**Two: one road for the removal, asked by every position.** `emitPopRoad` in `pkg/lang/pop_answer.go` lowers
the handle load, the length, the negative-index fix-up, the bounds raise, the removal and the shift; the
method road and the pair door both call it, so the position that discards the answer, the position that prints
it and the position that binds it cannot disagree about *which* element left the list. The door takes
`wantTag` and answers a second register only for the positions that read a value.

**Three: the answer travels as a (payload, tag) pair to the doors the position already asks.** No position got
a road written beside a door:

| position | the door it already asked, now given one popped arm |
|---|---|
| `print`, `str`, `repr`, an f-string field | `answerCallPair`, `pairCallPair` |
| a binding, a container-literal element | `pairElemPair` ← `isPairElem`, `literalHasPairElement` |
| `if xs.pop(0):` | the truth road, which asks the pair door above |
| `==`, `!=` | `comparePair`, gated by `isTaggedCompareSide` |
| `<`, `>`, `<=`, `>=` | `taggedOrderApplies` / `orderTagOf`, through `orderSide.popCall` |
| `+`, `*`, `-`, `//`, `%` | `arithWouldRefuse` → `taggedArithPair`, `slotArithmeticIsProven` |
| a function's parameter | `pairCallScan`'s needs half (`anyPoppedSlot`) and its supply gate |

The ordering arm is the one that changed a *wrong* answer into the reference's *raise*: with the tag beside it,
`xs = [1, "a"]` / `print(xs.pop(0) > 1)` reaches `unsupportedNumberOp` and emits
`TypeError: '>' not supported between instances of 'str' and 'int'`; without it the comparison beat `1` with an
interned index and printed `False` at exit 0.

**Four: which tag travels is the container's fact, and the third answer is DECLINE.** `popAnswerShape` answers
one of three things:

* a list whose slots describe themselves (`mixedLists` — the containers **ADR 0184**/**ADR 0232** promoted)
  hands over the tag the **object** stored, loaded by the removal itself;
* a list that still claims one element kind (`listElemStr`, `listElemInt`) hands over that claim — the
  notebook's own claim, the same one the printer, the loop and the comparison roads already act on, so
  `xs = ["a", "b"]` / `print(xs.pop(0))` prints `a` for the reason its print already prints `['a', 'b']`;
* a list whose kind no notebook describes is **declined**, and the element stays on the road it was on.
  Guessing is how a payload gets rendered under a kind the object never gave it, which is the class of bug
  this row exists to remove, not to add (**ADR 0166**).

A list that claims one kind keeps its ordinary road for arithmetic and for the ordering — the payload *is* the
number there, and opening a working program's road is churn, not a fix. That is what the pair arm's `ok=false`
means, and `TestThePopAnswerShapeHasThreeAnswers` asks the gate and the door the same question so they cannot
disagree (**ADR 0249**'s rule about a gate and a door that test different things).

**Five: the two bounds sentences are two sentences.** An empty list raises `IndexError: pop from empty list`;
a non-empty one raises `IndexError: pop index out of range`. The reference has always had both. This backend
had one, and had it for the empty case, which is the kind of detail that only shows up when somebody files the
program that asks. Both are recorded, both are catchable by the program's own `except IndexError:`.

## Machine path

Nothing new is discoverable that was not discoverable before, which is the point: the row changes an *answer*,
not the interface. What an agent can still ask, and what this row keeps honest:

* `gustyc --eval 'xs = [1, 2, "a"]
xs.pop(0)
print(xs)'` prints `[2, 'a']` at exit 0, and the same source is on the record leg with those bytes.
* `--emit-llvm` for a pop shows one `call i32 @rt_pop(i32 %h, i32 %i, i32* %t)` per removal and the tag
  out-parameter in the `define` — `TestThePopShiftMovesTheTagWithItsPayload` asserts both, plus the absence of
  any `@rt_pop_*` helper this row might have invented instead.
* the refusals the row leaves standing keep their capability codes — `lower.unsupported.tuple-is-immutable` for
  a tuple's `pop`, `lower.unsupported.container-method-over-a-name` for a dict's — with `ok:false`, `exit:1`,
  `phase:"codegen"`, a `suggestion` and `rows` naming the roadmap row (**ADR 0006**, **ADR 0346**).
* `--verify-llvm` is clean on every emitted module; exit 2 is a compiler bug and the suite fails on it.

## IR implications

* `@rt_pop(i32, i32) -> i32` becomes `@rt_pop(i32, i32, i32*) -> i32`. Its callers are the method road and the
  pair door, both of which go through `emitPopRoad`, so there is exactly one place that spells the call.
* The removed element's tag is an `icmp`-able register in the caller, which is what lets the equality door, the
  ordering door and the arithmetic door take the popped slot in the shape they already take a slot read — no
  new `rt_*` entry point in any of them.
* A popped payload that is an object handle is rooted with `rt_root_put` for the life of the expression that
  holds it, because an allocation during the print would otherwise recycle the object the payload still names
  (the same rule `taggedOperand` keeps for a pair operand).
* A literal whose element is a popped slot now takes the **tagged** builder — `heapListFromTagged` and the
  print road's mixed printer — because the tag is a register and `heapListFrom`'s static path has one word per
  slot (`literalHasPairElement` is the gate that decides it, and it asks the popped shape alongside the name
  and the subscript).

## Alternatives rejected

* **Answer the payload and let the reader ask for the tag.** That is what the backend did, and it is why
  `print(x)`, `str(x)` and `if x:` had three answers for one value. A door that reads a tag the removal did not
  carry reads the tag array at a slot the list already shifted away.
* **Have `@rt_pop` clear the tag it removes.** That fixes the *tail* and not the *answer*, and it fixes the
  tail by making the stale tag `TagInt` — which turns a visible wrong answer into a plausible one. The shift
  moves tags; nothing needs clearing.
* **Refuse `pop` on any list whose elements are not all one kind.** Cheap, honest, and already what the
  indexed read does at a `return` — which is exactly the problem: it would leave `print(xs.pop(0))` refusing
  while `print(xs[0])` beside it answered, and the pair door for a slot read already exists and works.
* **Guess `TagInt` for a container no notebook describes.** This is the one place the row could have shipped a
  quieter version of the bug it fixes, so the gate's third answer is a decline and
  `TestThePopAnswerShapeHasThreeAnswers` pins it.
* **Give `pop` a sibling runtime (`rt_pop_tagged`).** Every other positional door moves the tag inside its own
  helper; a second name for the same shift is a second place for the rule to be forgotten.
* **Route the pair through the arithmetic door's own arms only.** The first cut of this row answered the pair
  inside `numericExprPair`, and the ordering it produced said `False` for a text against a number where the
  reference raises — the pair arm had bypassed `taggedArithPair`'s refuse arms (**Gap R.296**'s rule). The row
  now points `arithWouldRefuse` at that door instead of stepping around it.

## Measurements (taken, not remembered)

Over the **63** programs these two files carry, a build of the parent commit (`0f798e8`) beside a build of this
one, each asked of the pinned CPython:

* **35 programs moved to the reference's answer** — `print(xs.pop(0))` for a text, a double, a verdict, the
  void, a list and a dict slot; the tail after a removal, read by the printer, by `len` and by position; the
  answer through `str()`, as a condition, as an argument, in a list literal, a tuple literal, a set and a dict
  value; the ordering that now raises; the drain in a `while`; the same list under two names; a pop in a method.
* **17 already agreed** and still do — the single-kind controls (`[7, 8, 9]`, `[1.5, 2.5]`, `["a", "b"]`), the
  neighbours that append, the traps, the two programs the fold has always answered.
* **11 still differ**, and each is a row: `Gap R.35` (a module-level container reached from a body), the
  container-method fold (`insert` over a name), **Gap R.412**, **Gap R.413** (three shapes: the repeat, the
  concatenation sentence, the text-against-text refusal), **Gap R.414** and its control (`return xs[1]`, which
  the backend *refuses*, which is what localises R.414 to the pop road), a tuple's `pop` (**L11.3** /
  **ADR 0361**) and a dict's `pop` (**Gap R.174**).

`tools/recmerge` wrote **79** entries from the reference for this family (the record to **8991**) and refused
the programs the compiled leg has no answer for — the record leg declining to testify for a backend that has
none (**ADR 0309**). The `integration` drift ledger gained one row: a dict's `pop` over a name, which the
record answers and this backend refuses with `lower.unsupported.container-method-over-a-name`.

`make testparallel` green; every emitted module verifier-clean; exit 2 forbidden in both new files.
