# ADR 0445 — a call whose body hands back one of its own parameters answers with the CALLER'S kind word, and the container slot it lands in wears that word

- **Status**: Accepted
- **Date**: 2026-10-29
- **Cycle**: the one after **ADR 0444** (which closed **Gap R.501**; **Gap R.502** was standing, filed by **ADR 0443**)
- **Roadmap**: **Gap R.502** closed; **Gap R.485** and **Gap R.492** closed by the guard the same gate made askable (the set member door — see Consequences); **Gap R.503** filed by the same sweep (a mixed set literal whose member is a call crashes the run time, measured byte-identical on `9605949` and on this commit) and **Gap R.504** filed the same day (a hand-off answer that carries a HANDLE into a container slot builds a dict that points at itself and kills the linked artifact, where the JIT answers the same source correctly and `9605949` printed digits and stayed alive — this cycle's own damage, named the day it was measured, caught by `tools/recmerge` refusing to record it); **Gap R.146**, **Gap R.453**, **Gap R.81** and **Gap R.164** stay on their own rows.
- **Builds measured**: parent `9605949` (**ADR 0444**) and this commit, every byte from a build made with the gate's `-tags=llvm20`.

## Context

**ADR 0443** gave the comprehension's loop, condition and `bool()` the OBJECT the run-time builder had made, and
recorded that the element still came back without its kind (**Gap R.502**). The three rows it filed were all answers
at the exit code of success:

```
$ gustyc --eval 'def g(v):
    return v
for v in [g(x) for x in [1, "a", 2.5, None]]:
    print(v)'
1                                               reference: 1
0                                               a
1                                               2.5
0                                               None
$ gustyc --eval 'def g(v):
    return v
print({g(x): 1 for x in ["a", "b"]})'
{0: 1, 1: 1}                                    reference: {'a': 1, 'b': 1}
```

The element expression is a `*Call`, and every container road asked the same table — `elemKindTag`/`elemTagFor` — which
reads what a function's `return` SPELLS. A body of `return v` spells nothing: the table fell through to `TagInt`, the
builder wrote the tag of a number beside a payload that was an interned text index or a float box, and `rt_tag_of`
later handed that tag to the print door as if it were the truth. The same fall-through answered for a list literal's
element, a dict entry's value, a set member, an `append` and an assigned slot — 63 further programs were measured into
this one shape.

A hand-off function has no answer of its own. Its answer IS the caller's argument, unchanged, and the argument is the
only thing in the program that knows the kind. That is **ADR 0273**'s element position finally getting the door it was
written for: the container element is one of the positions that read BOTH words of an answer that arrives as a
(payload, tag) pair, and until now no function answered with a pair whose second word came from the call site.

## Decision

**Ask the argument at the call site, and carry the pair to the store that already takes a kind.**

1. **`pkg/lang/forwarded_answer.go` — the permission.** `forwardedParameterOperand` accepts a call through a
   module-level `*Name` only, and only when the callee is neither a class constructor, an extern, an async/decorated
   function, a lambda capture, a generator, a recursive self-call, nor a body that can fall off without returning;
   every `return` must name the SAME parameter, and the body must never rebind it (`bodyBindsName`, with the
   assignment/augmented-assignment/comprehension-target and `match`-pattern readers that make "rebind" mean what the
   checker means by it). The operand for that parameter is resolved positionally, by keyword, or from the declared
   DEFAULT — so `[g(v=2.5), 1]` and `def g(v = 2.5): return v` / `[g(), 1]` answer too.

2. **One tag reading, shared by gate and door.** `forwardedAnswerTagUnder` is the single place the hand-off's kind is
   read: the argument's constant tag when the compiler knows it, and `fromRegister` when the argument is a name the
   pair road bound — a load of allocas that road already wrote. A gate never emits: re-lowering a call, a subscript or
   a `.pop()` "to find out" would run the program twice, which is the failure **ADR 0249** files as a gate and a door
   that test different things. `forwardedCallIsPairElem`, `forwardedCallSlotsItself`, `setLiteralHandsBackATag` and
   `dictLiteralHandsBackATag` all consult that one reading.

3. **`forwardedCallPair` is the door.** It emits the call once and hands back `(payload, tag)`, marking
   `g.pairCallAsked[callee]` while it lowers the payload so that `value`'s **Gap R.146**/**ADR 0273** refusal does not
   reject a position that is legitimately asking BOTH words, and rooting a handle-bearing answer (`rt_root_put`)
   because the object was built in a frame the `ret` has already closed (**ADR 0181**).

4. **`pkg/lang/heapargs.go` — the stores.** `elemPayloadAndTag` asks the hand-off pair before the kind table;
   `elemKindTag` asks `forwardedAnswerKindTag` before its `TagInt` default; `pairElemPair` falls back to
   `forwardedCallPair`; `literalNeedsTags`/`literalHasPairElement` claim a hand-off slot; the set and dict literal roads
   open their tagged (`rt_set_add_tagged`, `rt_dict_put_tagged`) builder for a member/key/value that comes through a
   hand-off (`setLiteralHandsBackATag`/`dictLiteralHandsBackATag`), and ask the hashability question of the tag they
   got, whoever produced it. `pkg/lang/codegen_assign.go` asks the same two claims for `xs[0] = g(2.5)` and for a
   mixed `set`/`dict` assignment.

5. **`pkg/lang/codegen_comp.go` + `codegen_comp_setdict.go` — the builders.** An element, key, member or value that is
   a hand-off pair marks the built container self-describing (`rt_mark_estr(h, 8)`) and is pushed through the tagged
   store; `compOperandTagKind` recognises a hand-off whose argument IS the loop variable and asks the ITEM's kind, which
   is what keeps **Gap R.358**'s rows answering.

6. **`pkg/lang/pair_element_read.go` — the gates.** `isPairElem` gained the fifth shape (`forwardedCallIsPairElem`) and
   keeps the sixth (`pairReturningCallIsPairElem`) narrowed to an answer the pair road can actually name:
   `handOffOrSpelledAnswer` — a hand-off, or a body whose every `return` spells a literal.

7. **Hashability keeps the AST as the last word.** `guardHashableExprTag` skips the raise when the slot's expression is
   a hashable TUPLE, reached through the hand-off's argument if need be (`exprIsHashableTupleAnswer` +
   `tupleElementsHashable`). A tag cannot make that call: a tuple and a list share the heap's layout (**ADR 0361**), and
   `{(1, 2)}` and `{[1, 2]}` are not the same answer to the reference (**Gap R.81**).

## Consequences

- The three filed answers and 70 further programs measured into this shape print the reference: `1`, `a`, `2.5`,
  `None`; `[1, 'a', 2.5, None]`; `a`, `b`; `{'a': 1, 'b': 1}`; `{1, 'a'}`; `[None, 1]`; `[[1, 2], 3]`; `[(1, 2), 3]`;
  `{1: None}`; `[2.5]`; `[2.5, 1]` — all at **exit 0**, where the class **ADR 0166** files loudest was an answered
  wrong at success.
- The hashability guard could not previously be asked at all for these slots, so `{g([1, 2])}` stored an address and
  printed `{[1, 2]}`; it now dies `TypeError: unhashable type: 'list'` at **exit 3**, catchably, exactly where the
  reference dies — and `{g((1, 2))}` still builds.
- Asking that guard closed two neighbouring rows the sweep found standing open, because they were the same unwritten
  question rather than a second bug: `print({[1], [2]})`, `sa = {1: "a", "b": 2}` / `print({sa})`, `print({{1}})` and
  `print({[x for x in [1]]})` all printed the member the reference refuses to hash (**Gap R.485**, **Gap R.492**), and
  all four now raise the reference's own sentence at the program's exit **3**. The gate, not the sentence, was the
  missing part: `guardHashableTag` had always existed, and the untagged set road gave it nothing to compare. The four
  filed rows moved into two answering tables on both legs, and the raises got record entries — none of the four had
  one while they printed an address, which is the tell that a filed "answer" was never an answer (**Gap R.204**).
  **Gap R.81**'s `add` door (`sa.add([1])`) is a different road and stays open.
- The gate's narrowing is load-bearing, not decoration. Widening `pairReturningCallIsPairElem` to every pair-shaped
  callee made `def g(v): return v + 0` print `[2, 1]` — a float box's address read as an integer — where the same
  program refused before and refuses after. A wrong answer that replaces a refusal is the trade this file refuses to
  make twice (**Gap R.204**).
- 59 record entries (10109 → 10168) were written by `tools/recmerge`, none refused: it records only where the live
  reference and the compiled leg print the same bytes. `integration/programs/forwarded_answer.gy` joined the corpus
  line-for-line, byte-identical to `python3` on the whole program.
- Two 464-source sweeps (`/tmp` harness, one process per program, parent binary beside this one) graded the change:
  73 sources newly matching the reference, and no source that matched the reference on the parent failing on this
  build — the only two apparent exceptions were `set` ORDER, which CPython seeds per process (`PYTHONHASHSEED` flips
  `{'q', 'r'}` and `{'r', 'q'}` for the same program), and both binaries agree with each other on it.

## Alternatives rejected

- **Widen `elemKindTag`'s default from `TagInt` to "dynamic" and let the position refuse.** Honest, but it converts
  programs that answer today — the whole homogeneous-int family — into refusals, and the corpus gate would measure
  that as a regression. The default stays; the hand-off door answers BEFORE it is reached.
- **Read the kind off the callee's return, through the pair tag slot.** A hand-off's tag slot holds whatever the last
  call wrote, and the callee's frame is gone by the time a container builder wants the word; the caller's argument is
  the fact, and it is available without touching the callee.
- **Emit the call twice — once to learn the tag, once for the payload.** Only the pair road's own allocas may be read
  twice (**ADR 0249**); a second lowering re-runs the program's effects.
- **Give the set/dict literal roads a `literalNeedsTags`-shaped widening** (floats, verdicts, containers generally).
  Right family, wrong row: it changes the builder for programs that answer today. This commit claims only the
  hand-off (`setLiteralHandsBackATag`/`dictLiteralHandsBackATag`), leaving the general version on **Gap R.146**'s road.
- **Decide hashability from the runtime tag.** A tuple is a list to the heap; the AST has the final say, and asks it
  of the argument the hand-off carried.
- **Fold the iterable to decide the tag array.** `[g(x) for x in xs]` has no compile-time element to inspect — the
  whole point of the builder being the run time's.

## Machine path

The tag a slot received is visible in the emitted module, which is what the module-shape rows assert and what an agent
can assert without running anything: `@rt_append_tagged(`, `@rt_set_add_tagged(`, `@rt_dict_put_tagged(` and
`@rt_mark_estr(` are present on every row this door served; `@rt_append(`, `@rt_set_add(` and `@rt_dict_put(` stay on
the roads that never needed a kind. The CLI path is `gustyc --emit-llvm "<source>"` — `--emit-llvm` takes a SOURCE
STRING, not a path, and a path handed to it is parsed as source and answers a parse error (`docs/operations.md`'s flag
table is the authority) — with `--eval` the JIT path and `--file`/`--build` the link-and-run path; all three lower
through the same compiler roads, so an answer is meant to be portable between them, and the CLI rows check both
execution legs (exit **2** forbidden on every row). Portability is asserted on the rows that hold rather than assumed:
this cycle found the exception, and **Gap R.504** is a program the JIT answers with the reference's bytes and the
linked artifact kills.

## Where the proof lives

`pkg/lang/forwarded_answer_test.go` (**22** container positions + **9** built-comprehension positions, each judged on the
compiled leg AND the record leg; **6** module-shape rows; **12** hashability rows checked against `jitTrap` AND
`goldenRunError`; **5** filed-not-fixed rows — one of them **Gap R.504**'s misprint) and
`integration/forwarded_answer_test.go` (**33** CLI programs re-derived from a live `python3`, **5** trap rows at exit
**3**, **3** catchable rows at exit **0**, **3** refusal rows that keep today's exit **1**). The rows this door does not reach stay in
`pkg/lang/comp_loop_truth_test.go` and its CLI twin, whose emptied slots name this file.
