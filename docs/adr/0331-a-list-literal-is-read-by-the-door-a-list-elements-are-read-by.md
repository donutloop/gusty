# ADR 0331: a list literal is read by the door a list's elements are read by

Status: accepted (2026-07-14)
Amends: [ADR 0327](0327-a-dict-literal-is-read-by-the-door-a-dict-variable-uses.md) (symmetric case, taken for the other container)
Extends: [ADR 0210](0210-negative-indices-and-slices-in-the-compiled-backend.md) (positions), [ADR 0187](0187-payload-and-tag-travel-together.md) (a payload is never handed out without its tag), [ADR 0329](0329-an-index-that-is-not-a-position-names-its-type-at-the-doors-that-count.md) (Gap R.222's index gate), [ADR 0330](0330-the-bounds-check-is-told-which-door-it-stands-in.md) (the bounds sentence at the door this read reaches through)
Supersedes: nothing
Related: roadmap L11.1, L11.4, Gap R.215 (paid — the routing this row copies), Gap R.218 (closes), Gap R.146 (the standing one-word residue), Gap R.222 (extended), Gap R.224 (newly measured, owned by L11.1)
Owner: `pkg/lang/list_literal_read.go`, `pkg/lang/heapargs.go`

## Context

ADR 0327 closed the dict half of an asymmetry and left the list half standing. A subscript in this
backend has two roads. Where the compiler can read the whole thing — a literal whose elements are all
integer constants, at an integer position — the read is a compile-time lookup and the program gets the
number. Where it cannot, the read is a *message to the object*: the container is built in the heap, each
element's kind is written beside its payload as the builder runs, and the answer is a `(payload, tag)`
pair. The second road is the one a list **variable** has always walked, and it is the one that knows what
an element is.

`runtimeSlotPairDeep` (`pkg/lang/heapargs.go`) is that door. ADR 0327 gave it a `*DictLit` arm. Nothing
ever gave it the list arm beside it, so a list literal was never admitted, and the fold — one word,
nowhere to put a kind — stayed in charge of shapes it cannot read. Four shapes answered a payload at
**exit 0**:

```
print(["a", "b"][0])   # `a` in the reference; the program printed 0   — an index into @str_tab
print([1.5, 2][0])     # 1.5 in the reference; the program printed 1   — a @float_box handle
print([None, 2][0])    # None in the reference; the program printed 0
print([True, 2][0])    # True in the reference; the program printed 1
```

and the same four read off a variable (`xs = ["a", "b"]` / `print(xs[0])`) answered `a`, `1.5`, `None`,
`True`. That parity is what makes this a defect rather than a missing feature: a list literal is not a
different kind of list, it is a list the compiler happens to be looking at. The literal road was already
right for the elements that carry a tag wherever they sit (`print([[1, 2]][0])` is `[1, 2]`,
`print([{"a": 1}][0])` is `{'a': 1}`) because a nested container *is* a handle and a handle means the
same thing with or without a fold.

## Decision

**A list literal's element is read through the door a list variable's element is read through.**

- `runtimeSlotPairDeep` (`pkg/lang/heapargs.go`) grew the `*ListLit` arm beside the `*DictLit` one: build
  the list with `containerHandleOf` — the same builder a list *variable* uses, tags written slot by slot
  — and read the position with `containerSlotRead`, the same call `xs[i]` emits. `runtimeContainerSide`
  admits a non-empty `*ListLit`, and `render.go` (print, `str`/`repr`, an f-string field), the binding
  road, `len`, a membership test and a loop reach it unchanged.
- `pkg/lang/list_literal_read.go` is the gate and the residue. `listLiteralFoldAnswers` keeps the fold in
  charge of exactly the shape it reads — every element an integer constant **and** the position an integer
  the literal has — so `print([1, 2][0])` still emits a constant and allocates nothing, asserted by the IR
  test that fails if the fold's shape starts calling `rt_print_mixed_value`. `listLiteralPair` is the
  element as a pair; `listLiteralRead` is the one-word answer, given only where the payload *is* the value.
- **The fold never widens.** `listLiteralFoldAnswers` refuses a bool position (`["a","b"][True]`) rather
  than answer a question the reference answers at a door — that shape belongs to Gap R.222's gate, and a
  fold that grabbed it would be answering the wrong question with a right-looking number.
- **A position that keeps one word for a whole value refuses.** A `return`, a call argument, an operand of
  arithmetic cannot carry a tag, so `print(["a", "b"][0] + "!")` names the missing half, the door that
  does answer it and the rows that own the residue (Gap R.218 for the read, Gap R.146 for the one-word
  positions) instead of printing `0!`. This is ADR 0327's decision, and the same test list shape.
- **A condition is a pair position, not a one-word position.** `if [True][0]:`, `while`, and the operand a
  ternary chooses now ask `listLiteralPair` and decide with `@rt_pair_truth` — the same table `and`/`or`
  already ask. This is not a widening for symmetry's sake: the retired engine's own record contains
  `if [True][0]:` / `print("yes")`, and a fix that turned a program the record runs into a refusal would
  have registered as a new divergence — a *coverage loss dressed as a fix*. It is also where the fold was
  quietly wrong: `[0.0, 1][0]`, `[[]][0]` and `[""][0]` are all falsy in the reference, and every one of
  them is a non-zero payload (a box handle, a container handle, an interned index) tested against zero.
- **Gap R.222's index gate moved inside the door.** `containerSlotRead`'s list branch now asks
  `badIndexGate` before it lowers the index, exactly as its dict branch has always asked
  `checkKeyReadTagged` for Gap R.217's unhashable key. Without that move the widened door *introduced*
  Gap R.222's defect at a new site: `print([1, 2]["a"])` printed the element at whichever slot the text's
  `@str_tab` index counts to, and `print(["a","b"][[0]])` had no word to lower at all. Both now raise the
  reference's `TypeError: list indices must be integers or slices, not <type>`; a bool stays legal, because
  `["a","b"][True]` is `["a","b"][1]`.

## Consequences

- The four answers above are the reference's, on both legs, and the program
  `integration/programs/list_literal_read.gy` (matrix row `programs/list_literal_read`, oracle `match`)
  walks the whole family — the four kinds, the negative position, the nested literal, `str`/`repr`, `len`,
  `in`, a binding, a loop, the fold's own shape, the catchable `IndexError`, a dict entry read by a
  position, and the four conditions the payload cannot decide.
- The two roads cannot drift, because there is one read: the pair door *is* `containerSlotRead`, and
  `TestTheLiteralListRoadIsNeverWorseThanTheVariableRoad` asserts the literal road answers whatever the
  variable road answers (and skips only a row the variable road itself refuses — `xs = [[]]` / `if xs[0]:`
  is Gap R.82's, and this row does not claim it).
- The index gate now stands at three doors: the subscript road in `value()`, the store road in
  `assignIndex`, and the pair door. A fourth road that reads a position has to ask it too — that is the
  rule, and the reason both container gates (`unhashable_key.go`, `list_index_kind.go`) are shaped alike.
- `bool([None][0])`, `return ["a","b"][0]` and `id(["a","b"][0])` stay refusals, and that is the residue
  Gap R.146 already owns; nothing in this decision claims a one-word position can carry a kind.
- Measured on the way and **filed rather than bundled**: `print([{"a": "x"}["a"], 2])` answers `[1, 2]`
  where the reference answers `['x', 2]` — a dict-literal read *as an element of another literal* is the
  same missing half one layer out, and the literal builder's `pairElemPair` admits only a `*Name`. That is
  **Gap R.224**, not this row.

## IR / codegen consequences

- The fold's modules are unchanged: `print([1, 2][0])` still emits `call void @rt_print_int(i32 1)` and a
  guard fails if it grows an allocation or a `rt_print_mixed_value` call.
- A folded-out read is one `rt_alloc` plus one `rt_tag_elem`/`rt_set_elem` pair per element (`rt_list_from_mixed`
  is the builder both roads share), then `rt_get_elem`/`rt_tag_of` for the position — the two calls
  `xs[i]` emits, which is the point.
- Where the index cannot be a position the raise ends the block, a `bix.cont` continuation reopens it, and
  the pair the caller reads back is `0, 0` — unreachable, and the module stays valid. Two guards keep it
  that way: `--verify` on the CLI rows, and `emittedModuleIR` in the unit rows, which is also where the
  *absence* of a `@.lstN` global inside an `icmp` is asserted (Gap R.222's `llc` rejection at this door).
- A condition that reads an element now emits `call i32 @rt_pair_truth(i32 %payload, i32 %tag)` and an
  `icmp ne` on the answer — the same two instructions the `and`/`or` road emits for a pair operand, not a
  second truthiness table.

## Alternatives rejected

- **Widen the fold instead** (have it notice a text/float/None/bool element and emit the right constant
  printer). Rejected: that is a third read of the same question — the fold, the printer and the objects —
  and ADR 0327 already decided a literal's entry is read by the door the object uses, once. The fold keeps
  the one shape it can read outright, and the guard is that its IR did not change.
- **Let the fold answer conditions by its payload, as it did before.** Rejected: it was wrong for exactly
  the shapes a tag exists to distinguish (`[0.0, 1][0]`, `[[]][0]`, `[""][0]`), right only for the ones
  whose payload happens to mean zero-or-not, and it is a *regression* to lose `if [True][0]:` to a refusal,
  since the record runs that program.
- **Register `if [True][0]:` as a new divergence** and move on. Rejected: divergences are for cases the
  compiled backend genuinely cannot answer, and this one can. A ledger row bought by narrowing the door is
  a cover-up with a schema.
- **Also open the one-word positions** (`return`, a call argument, arithmetic) by threading a tag register
  through every caller. Rejected for this cycle and owed by Gap R.146: the caller has no slot, and
  inventing one per caller is the "just pass the tag everywhere" plan ADR 0187 has declined eight times.
- **Answer `bool([None][0])` by special-casing `bool`.** Rejected: it is a pair-consuming call wearing a
  value-consuming signature, and fixing it by name is how one gets a second, lazier type system.

## Verification

- `pkg/lang/list_literal_read_test.go`:
  - `TestAListLiteralAnswersTheElementThePositionNames` (28 answered rows: the four kinds, negative
    positions, the nested literal and its sub-read, `str`/`repr`, `len`, `in`, a binding, a loop, the fold's
    shape, a bool position);
  - `TestAListLiteralElementDecidesAConditionTheWayTheReferenceDecides` (18 rows, including the three the
    payload cannot decide and the parity rows against the variable road);
  - `TestTheListFoldStillAnswersWhatItCanRead` (the fold's IR stays a constant — no allocation, no
    `rt_print_mixed_value`);
  - `TestTheOneWordPositionsRefuseTheListLiteralRead` (return, call argument, arithmetic, an f-string
    field — each must refuse and name the missing half and its row);
  - `TestTheLiteralListRoadIsNeverWorseThanTheVariableRoad` (the parity that made this a defect);
  - `TestAnIndexThatIsNotAPositionIsRefusedAtTheLiteralDoor` (str/None/float/list/tuple indices, and a
    dict entry read by a position — the reference's TypeError, and the module still verifies);
  - `TestAPositionPastTheEndOfAListLiteralRaisesWhatTheReferenceRaises` (ADR 0330's sentence, catchable);
  - `TestTheListFoldGateAnswersTheShapeItCanRead` (the gate's decisions asked away from codegen);
  - `TestTheListLiteralRowsAreRecorded` (the record leg answers each row, so the gate cannot be deleted
    without deleting the evidence).
- `integration/list_literal_read_test.go`: the same answers, refusals, TypeError rows (under `--verify`)
  and the whole program through the CLI, line by line against CPython;
  `TestTheListLiteralReadProgramIsAnAssertedMatrixRow` keeps the row parity-asserted with an oracle verdict
  of `match`.
- `go test -tags=llvm20 -count=1 ./...` and `make testshards` green; every emitted module passes
  `opt -passes=verify` / `llc`.
