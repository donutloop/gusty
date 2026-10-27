# ADR 0433 — a generator's buffer carries the kind of every element it yielded: one door writes payload and tag, and the doors that receive the buffer ask the slot

## Status

Accepted, 2026-10-27. Closes **roadmap Gap R.468**, filed by **ADR 0432**. Files **Gap R.477** (a comprehension
over a bound buffer, and `str()`/an f-string field of one, still render the word underneath the value) and **Gap
R.478** (the membership test compares an untagged payload, so an integer walks in as a text whose interned index
happens to be the same number). Depends on **ADR 0187**/**ADR 0189** (payload and tag are one operation, never two),
**ADR 0311** (the tagged append `xs.append(...)` asks), **ADR 0232** (one question, one reading — here, one model at
two doors), **ADR 0185**/`Gap R.76` (a tagged element's payload means nothing without its tag), **Gap R.146**/`L11.1`
(the one-word position that cannot carry a pair, whose standing refusal this row walks up to and does not widen),
**ADR 0081**/**ADR 0140** (the eager-buffer generator the record holds), **ADR 0432** (the reading, the buffer, and
the four doors that receive the answer), **ADR 0302** (both witness legs, never two engines) and **ADR 0166** (a wrong
answer at the exit code of success is the artifact this contract ranks worst). Supersedes nothing.

## Context

```python
class C():
    def m(self):
        yield "a"
        yield 2
        yield None
        yield 1.5

c = C()
for v in c.m():
    print(v)
```

prints, on the build this row opens:

```
0
2
0
1
```

at exit **0**. The reference prints `a`, `2`, `None`, `1.5`. Three of the four lines are not the value the body
handed over: the text is the integer the intern table happens to index it at, the float is the float box's heap
slot (`1` — the second allocation in the program), and the void is its zero payload. Nothing in the module says which
of the four is which, and nothing asks.

**Gap R.468** was filed by the round that made a generator METHOD reachable at all (**ADR 0432**), and it was filed
with the measurement that locates the hole rather than with the symptom: the same four statements written at module
scope —

```python
def g():
    yield "a"
    yield 2
    yield None
    yield 1.5
```

— printed the same wrong four bytes, byte for byte. So this was never the class round's hole. It is the append
door's.

Every other list this backend builds writes its element's kind beside its payload. `xs.append(1.5)` lowers to
`@rt_append_tagged(i32 %h, i32 %v, i32 %t)`, and the reason it is one instruction and not two is **ADR 0187**'s:
`rt_append` alone stores the payload and leaves the tag slot holding whatever the *previous tenant* of that slot was
tagged as, because the tag array is not cleared on free. A list whose appends forget the tag is a list that prints an
interned string's index as a number — the exact failure the generator road had, arrived at from the other side. The
container roads learned this across **ADR 0187**, **ADR 0189**, **ADR 0232**, **ADR 0233** and **ADR 0311**; the
generator's buffer is the one list in the module that never knocked on that door:

```go
// stmtYield — before this row, the whole of the lowering.
v, err := g.value(b, n.Expr)
fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, v)
```

One word in, no word for what it means, and four doors — the walk, the print door, a binding, the membership test —
left choosing their helper from a kind the register does not carry.

## Decision

**One door writes a generator's slots, and it writes the kind with the value.**

`genBufferAppend` (`pkg/lang/codegen_genbuf.go`) is the single place a `yield` reaches the buffer. It asks the yielded
expression the question `xs.append(...)` asks its argument — `elemPayloadAndTag`, the pair the container slots are
built from, which reaches the pair road for a name the pair bound, the compile-time tag for a literal, the intern
table's index beside `TagStr` for a text — and emits one `@rt_append_tagged`. `stmtYieldFrom` asks the same door for
its literal spelling, and for the heap spelling it reads the source's tag with `@rt_tag_of` and carries the pair
across the copy, because an element that travels from one buffer to another without its kind arrives as a stranger's
tag.

An element the tag doors cannot name — a parameter the keyed-call pass never classified, a nested container they
decline — keeps the word today's road appended and the tag that word was printed as (`TagInt`). The untagged printer
showed it as a number, so the answer on screen does not move; what changed is that the slot no longer inherits
whoever held it last. That is the never-worse rule read at the store: a fix may not spend bytes the program already
printed correctly.

**The reading that decides how the buffer is READ is earned, and it is asked at the body's own door.**

`markGenBuffer` makes two claims beside an operand — the register a call left the answer in, or the name a binding
parked it under:

- `genBuffers` — the list standing there IS a generator's buffer. The `yield from` copy asks it, and it is universal:
  every slot of such a buffer is now written by the door that tags.
- `genBufferSlots` — the doors that walk or render it must ask each SLOT what it holds. This one is earned by the
  callee's body: `generatorBodyWritesKinds` walks the body beside **ADR 0432**'s reading (`containsYield`'s own walk,
  minus the nested `def`, whose yields fill that body's buffer) and asks whether any yielded element writes its kind
  in its spelling — a text, a fractional number, a verdict, the void, a container, an interpolation. Where every
  element is written as a plain integer the claim is not made, and the roads the program always had are the roads it
  keeps.

That split is the difference between an answer and a refusal. A generator that yields `10` and `20` is walked by
`for v in c.m(): s = s + v`, and `v` is a number in that program: hand the loop the pair and `Gap R.146`'s standing
refusal — "using it as a number needs a tagged value word" — fires on a program that was adding integers up. Pinning
the control matters as much as pinning the fix, so `an integer buffer still SUMS rather than carrying a pair` stands
in the answered family, and the IR row asserts that a walk over an integer buffer never asks `@rt_tag_of`.

A `yield from` is asked of its SOURCE, and that is a decision with a story. The first version claimed kinds for any
non-literal source and called it conservative; it was a guess, and the corpus read it back: `def gen(n):` /
`    yield i * i` under `def outer():` / `    yield from gen(3)` printed nothing but **Gap R.146**'s pair refusal where
the record and the parent both hold `104`. The walk now takes a resolver: a literal answers for itself, a call is asked
through the body of every arm that can fill it, and a source the pass cannot attribute (a parameter, a self-copy) earns
nothing. The verdict is therefore read at the receiving door off `genBodies` rather than written at the registration
door, so the emission order cannot decide it — and a buffer forwarded from another generator answers, which it did not
before.

The claim is asked of EVERY candidate body where the receiver's class is a run-time fact (the class-id `switch`), the
same conservative rule **ADR 0432** uses for the split yield census: the arm that runs is not written in the line, and
a join that claimed kinds only one arm earned would have the walk read a stale word as the other arm's kind.

**Three receiving doors ask the earned claim.**

- `stmtFor` sets `mixedIter` for a generator iterable, so the loop variable is bound through payload+tag per iteration
  exactly as a mixed list's is (**ADR 0185**), for the call spelling and for a name bound earlier.
- The print door picks `@rt_print_list_mixed` for a generator buffer instead of `@rt_print_list`, which is the
  difference between `['a', 2, None, 1.5]` and `[0, 2, 0, 1]` — and it asks the claim that the walk asks, so a buffer
  of integers keeps the printer it has always had.
- `srcCarriesTags` decides whether a `yield from` may read its source's tags: a generator buffer and a registered
  mixed list may; an object the pass cannot attribute may not, and keeps the word it always copied.

## Consequences

- The row's program prints `a` `2` `None` `1.5`; `print(c.m())` prints `['a', 2, None, 1.5]`, byte-identical at both
  doors. A yielded verdict prints `True` where the build printed `1`, and a yielded container prints `[1, 2]` where
  the build reached `llc` with `@.lst1` in an operand list — **exit 2**, on the very shape this row now answers
  (**Gap R.474**'s argument and `__init__`-field spellings stay open; only the `yield` spelling is paid here).
- **One shape moves from a wrong answer to a refusal, and that is the decision, not an oversight.**
  `for v in c.m(): s = s + v` over a buffer that yields `1.5` and `2.5` printed `3` (the two boxes' heap slots added)
  and now refuses with `Gap R.146`'s standing sentence — the same sentence, byte for byte, that
  `xs = [1.5, 2.5]` / `for v in xs: s = s + v` has always spent on a program with no generator in it. The ladder in
  `TestTheYieldedKindIsTheSameAnswerAtBothDoors` is the witness: a mixed buffer is now treated exactly as a mixed
  list is, which is what **ADR 0232**'s one-model rule asks and what the `3` was violating.
- Two doors still ask the OLD tables and are filed, not fixed: the comprehension classifies its iterable out of
  `mixedLists` (**Gap R.477**) and the rendering road out of `listOperands` (**Gap R.477**), and the membership scan
  compares payload words with nobody asking the tag beside them (**Gap R.478**, where `0 in c.m()` answers `True`
  because `"a"` interned to slot `0`). None of them moved on the parent build, which is what files them as their own
  rows rather than this row's damage.
- The buffer's tags are now read by the walk, so a generator that yields a mix of kinds and is walked into an
  arithmetic position is refused rather than answered. `L11.1`'s tagged value word is the row that opens it, at the
  generator as at the list.

## Alternatives rejected

- **Answer every generator buffer through the pair.** One table instead of two, and `for v in c.m(): s = s + v` over
  `[10, 20]` — the ordinary accumulator the tutorials write — would have died at exit 1 with a pair-road refusal it
  never earned. Rejected: a fix that turns a working sum into a refusal is a regression wearing a fix's clothes.
- **Tag only the non-integer elements, and leave the integers to `rt_append`.** Rejected: **ADR 0187**'s whole point is
  that the payload and the tag are one store, and a buffer whose slots are half-written reads back the previous
  tenant's kind.
- **Claim kinds where ANY dispatch arm earned them.** Rejected for the dynamic `switch`: the arm that runs is a
  run-time fact, and reading a tag the running arm never wrote is how a value walks around wearing another object's
  bits (**ADR 0187** again, at the join).
- **Ask the CALL site what the yielded kinds are** (the keyed-call pass's shape). Rejected: the yielded kind is a fact
  about the body, not about one call, and the call-site reading is what leaves a method body unclassified today
  (**Gap R.475**). The claim is taken where the body is registered, beside the reading **ADR 0432** already takes.
- **Widen the print door's `listOperands` arm to the mixed printer for every list register.** Rejected: that table
  also holds the untagged lists a comprehension and a `sorted(...)` build, whose slots were never tagged; reading
  them through the tag printer would print stale kinds. Hence `genBuffers` beside `listOperands`, not instead of it.
- **Fix `str()`/comprehension/membership in the same commit.** Rejected under the one-feature rule, and pinned: the
  bytes those doors print are the parent's bytes, and **Gap R.477**/**Gap R.478** carry them with the door named.

## Machine path

No new refusal, so no new capability code (**L11.8**, **ADR 0346**): the row moves answers from exit 0-wrong to
exit 0-right, and the one refusal it exposes is `Gap R.146`'s already-registered pair-road sentence. The existing
generator capability codes (`lower.unsupported.split-generator-method-dispatch`,
`lower.unsupported.generator-method-parameter-walk`) are unchanged.

- `gustyc --emit-llvm` on a mixed generator shows `call void @rt_append_tagged(i32 %gh1, i32 %v, i32 %t)` in the
  callee and `call i32 @rt_tag_of(i32 %h, i32 %i)` in the walk — the IR rows in
  `pkg/lang/generator_buffer_kind_test.go::TestTheYieldWritesPayloadAndTagTogether` assert both, scoped to the
  program's own `define`s, and assert `rt_tag_of` ABSENT from an integer buffer's walk.
- Witnesses: `pkg/lang/generator_buffer_kind_test.go` (**23** answered programs on the compiled leg AND the record leg
  with a MISSING record failing the case, **9** module/method never-worse ladder pairs, **4** eager-model rows, **16**
  rows asking `generatorBodyWritesKinds` DIRECTLY beside `methodIsGenerator`/`containsYield`, **3** IR rows with their
  negative control, **8** owed rows) and `integration/generator_buffer_kind_test.go` (**23** CLI programs each
  re-derived from a live `python3` inside the case, **1** record audit, **4** eager-model rows, **3** exit-code rows,
  **5** owed rows with the reference's bytes beside this backend's, **1** corpus-program check), plus
  `integration/programs/generator_buffer_kind.gy` — `match` on all three legs, registered in the conformance matrix.
- The two pins this row made stale are DELETED rather than re-worded: the owed row in
  `pkg/lang/generator_method_test.go` and its twin in `integration/generator_method_test.go` both stand in the answered
  families, and `TestTheGeneratorMethodModuleCarriesTheBufferDoors` now reads the tagged door.
- **21** sources newly recorded from the reference with the compiled leg cross-checked (**9814 → 9835**), none written
  by the compiled leg (**ADR 0302**).
- Measured beside a `git worktree` build of the parent (`bb4abc9`, **ADR 0432**), every source run through the same CLI
  on both builds: the answered family's **23** deduplicated sources split **17 moved / 6 held**, the six held being the
  membership row that already answered correctly, the forwarded integer sum the record holds `104` for, and the four
  integer-, parameter- and expression-fed controls that say the tag door opens for a KIND and not for a generator. One of the 15 is a move off **exit 2**: `yield [1, 2]` /
  `yield 3` handed `llc` the container literal's `@.lst1` GLOBAL inside the `define` (the
  `lower.unsupported`-less **Gap R.474** shape) and answers `[1, 2]` / `3` at exit **0** here, because the tagged door
  is handed the literal's WORD and not its global. The owed, eager and exit-code tables split **5 moved / 10 held** — the three
  eager-print rows, the exit-code table's own row program, and the mixed-buffer sum off its truncated `3`.
