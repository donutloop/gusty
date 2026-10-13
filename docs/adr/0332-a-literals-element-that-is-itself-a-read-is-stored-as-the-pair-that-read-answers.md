# ADR 0332: a literal's element that is itself a read is stored as the pair that read answers

Status: accepted (2026-10-13)
Amends: [ADR 0306](0306-a-container-element-is-one-of-those-positions-no-longer.md) (the element door, opened for a name the pair road bound)
Extends: [ADR 0331](0331-a-list-literal-is-read-by-the-door-a-list-elements-are-read-by.md) (the literal read this element is), [ADR 0327](0327-a-dict-literal-is-read-by-the-door-a-dict-variable-uses.md) (the dict side of the same rule), [ADR 0187](0187-payload-and-tag-travel-together.md) (a payload is never written without its tag), [ADR 0249](0249-a-gate-and-a-door-that-test-different-things-ship-a-refusal-nobody-measured.md) (the gate asks the door's question), [ADR 0310](0310-a-dict-entry-and-a-set-member-are-those-positions-no-longer.md) (the two builders this element reaches)
Supersedes: nothing
Related: roadmap L11.1, Gap R.224 (closes), Gap R.146 (the standing one-word residue), Gap R.220 (the call-argument twin), Gap R.225 / R.226 / R.227 (newly measured, filed here), Gap R.67 (a container that crosses a `return`)
Owner: `pkg/lang/pair_element_read.go`, `pkg/lang/heapargs.go`

## Context

ADR 0331 made a list literal's element answer the position the program named. It did that by admitting a
`*ListLit` to the one subscript door that answers a `(payload, tag)` pair, and by leaving the fold in charge
of the single shape it can read. What it left standing was named in its own consequences section as
**Gap R.224**, one layer out: the same read, used as an *element of another literal*.

A container literal is built by asking each position for two words — a payload, and the tag that says what
the payload means. Those two words come from one of two places. For an element the compiler can read off its
spelling (`"a"`, `1.5`, `None`, `[1, 2]`) the tag is a **constant** written into the
`rt_tag_elem` / `rt_dict_put_tagged` / `rt_set_add_tagged` call. For an element whose kind is the object's
fact rather than the compiler's — a name the pair road bound (`n = xs[0]`), or a **subscript** (`xs[1]`,
`{"a": "x"}["a"]`, `xs[0][1]`) — the tag is a **register** the runtime helpers returned. ADR 0306 discovered
exactly that when it opened the door for `[n]`, `{"k": n}` and `{n}`: "the builders below take the tag as a
constant, which is right for a literal element and wrong for a pair".

`pairElemPair` asked only for a `*Name`. A subscript therefore fell through to the ordinary element lowering,
which hands the builder one word and lets the builder label the slot from the element's *spelling*:

```
print([{"a": "x"}["a"], 2])   # ['x', 2] in the reference;  the program printed [1, 2]  — @str_tab's position
print([["a"][0], 2])          # ['a', 2] in the reference;  the program refused  — the one-word sentence
print([[1.5, 2][0], 3])       # [1.5, 3] in the reference;  the program refused
xs = [1, "a"]
print([xs[1], 2])             # ['a', 2] in the reference;  the program refused  — Gap R.82's sentence
```

while `print(xs[1])` had printed `a` for as long as the pair door existed, and `ys = [{"a": "x"}["a"], 2]` /
`print(ys[0])` printed `a` as soon as the literal was bound rather than nested. One word, three refusals, and
one answer that was a number the program never wrote — all of them the same missing half. The asymmetry is
what made it a defect rather than a missing feature: the print door, the binding door, the membership door and
the length door all ask a subscript for **two** words. The container builder alone asked for one.

## Decision

**An element that is a read is stored as the pair that read answers, by the door that answers it — and the
gate that admits it asks the door's own questions.**

- `pairElemPair` (`pkg/lang/heapargs.go`) grew the `*Index` arm beside the `*Name` one. The arm does not
  re-implement a read: it calls `taggedSlotPair`, the same call `print(xs[i])`, `v = xs[i]`, `len(xs[i])` and
  ADR 0331's literal read emit. That is the single-read rule (ADR 0241, ADR 0251) applied to a builder, and
  it is what makes "the slot the builder wrote" and "the slot the printer reads" unable to mean different
  things.
- `pkg/lang/pair_element_read.go` is the gate. `subscriptIsPairElem` asks, emitting nothing, the two
  questions the door asks and in the same order: is the container side one the run time owns
  (`runtimeSlotReadOf` — a list or dict variable the backend holds storage for, a list or dict literal, or
  another such read; a **set** is not a side, because a set has no slots), and is the read one the
  compile-time fold cannot answer alone (`listLiteralFoldAnswers`, `dictLiteralFoldAnswers`). `isPairElem` is
  the whole element question — bound name or read — and it is the question every taggedness gate now asks:
  `elemTaggableOrPair` (the dict's and the set's per-position gate) and `literalHasPairElement` (the list
  gate that decides heap-or-static, and the half that sets bit 8, "the slots describe themselves"). A gate
  that asks a narrower question than the builder asks is how this row would have shipped a refusal nobody
  measured (ADR 0249), which is why the three of them were changed together.
- **The fold keeps everything it can read.** `[[1, 2][0], 3]` and `[{1: 2}[1], 3]` are numbers the compiler
  can see, need no tag, and are asserted to allocate nothing and call none of the object doors —
  `TestTheFoldStillOwnsTheReadItCanAnswer` (unit) and `TestTheFoldRoadCarriesNoPairDoorThroughTheCLI`.
- **A name is still a pair element.** `isPairElem` is a union, not a replacement: ADR 0306's `[n]`,
  `{"k": n}` and `{n}` rows keep passing, and a decision row fails if the union swallows an ordinary
  untagged name.
- **The positions that keep one word for a whole value keep refusing.** An element that is a call argument
  (`[id({"a": "x"}["a"]), 1]`) has nowhere to put a register, and the answer is the Gap R.146 sentence, not
  a payload at exit 0. Widening the door must not quietly convert a refusal into a lie — the direction of
  change this contract forbids.
- **A raise inside an element is the program's exception.** `[{"a": 1}["b"], 2]` raises `KeyError: 'b'`,
  `[xs[5], 1]` raises `IndexError: list index out of range`, `[xs["a"], 1]` raises Gap R.222's `TypeError`,
  `[xs[0][0], 1]` raises `'int' object is not subscriptable` — each catchable by the program's own `except`,
  with exit 2 (the code the contract reserves for a compiler bug) forbidden on all of them. That is
  ADR 0326's door and ADR 0329's gate reached *through* the pair read, unchanged; the builder inherited
  neither a trap table nor a bounds check of its own.

## Consequences

- The row's four shapes answer the reference, on both legs, and
  `integration/programs/pair_element_read.gy` (matrix row `programs/pair_element_read`, oracle `match`) walks
  the family: all three containers, both literals and both variable kinds, a container the program built, a
  three-level chain, `len`/`in`/subscript/loop/`str`/`repr`/`==` on the list the element landed in, the fold's
  own shape, and the two raises caught by their own class.
- **Measured on the way and filed rather than bundled**, each with a row that fails the day the door grows:
  - **Gap R.225** — an element that is an *operation over* a read (`[str({"a":"x"}["a"]), 1]` is `[1, 1]`,
    `[{"a":"x"}["a"] + "!", 1]` is `[3, 1]`, `[str(xs[1]), 1]` is `[0, 1]`, `[d["a"].upper(), 1]` is `[2, 1]`,
    all at exit 0). The gate asks about the element, not about the element's operands.
  - **Gap R.226** — `for v in [{"a": "x"}["a"], 2]:` prints `1`/`2`: the direct-literal loop is unrolled and
    stores each element through the one-word road, while `ys = …` / `for v in ys:` (ADR 0185's runtime loop)
    prints `x`/`2`.
  - **Gap R.227** — `print([[1, 2][0][0], 1])` refuses `index requires an inline list/dict/set literal`
    where the reference raises `TypeError: 'int' object is not subscriptable` (Gap R.214's shape one level in).
  - **Gap R.67** owns `def f(): return [{"a": "x"}["a"], 2]` / `print(f())`, which prints `0`. Measured here
    because this row moved it: it used to stop the build with the static-array sentence, and it now takes the
    road `return ["a", 2]` has always taken — the handle printed as the number it is. Filed, not fixed, and
    named in the filed-not-fixed table so the move is visible.
- Bit 8 ("the slots describe themselves") is set by the same predicate that routes the element to the pair
  builder, so a container printed through `rt_print_list`, through `str`/`repr` (ADR 0258's one renderer with
  a switched sink) or compared by `rt_container_eq` asks the slots and not the list's single kind.

## IR / codegen consequences

- The element's tag travels as a register: `call void @rt_tag_elem(i32 %t1, i32 0, i32 %t9)` beside
  `call void @rt_set_elem(i32 %t1, i32 0, i32 %t8)`, where `%t8`/`%t9` are `rt_dict_get_tagged` /
  `rt_dict_value_tag` (or `rt_get_elem` / `rt_tag_of` for a list slot). The sibling plain-integer element
  keeps its constant tag — only the element that is a read needs a register, and a builder that took one for
  every slot would be re-deriving what the spelling says.
- The container announces itself: `call void @rt_mark_estr(i32 %t1, i32 8)`.
- The fold's modules are byte-for-byte what they were: no `rt_alloc`, no `rt_get_elem`, no
  `rt_dict_get_tagged` in `@main` for `[[1, 2][0], 3]`, `[{1: 2}[1], 3]` or `[1, 2][0]`.
- The gate emits nothing, so a declined element leaves no dead registers behind — the reason the question
  exists in two forms (`subscriptIsPairElem` for the whole-literal decision, `pairElemPair` for the emit),
  exactly as ADR 0306 shaped `nameIsPairElem` beside `pairElemPair`.
- Every module asserted here still passes `opt -passes=verify` and `llc`; the CLI rows run `--verify-llvm`
  beside the run, so a raise that ends a block without reopening it cannot pass by printing the right thing.

## Alternatives rejected

- **Widen the *fold* to notice a subscript element.** Rejected: the fold has one word and no place for a
  kind, which is the whole reason ADR 0327 and ADR 0331 refused to let it grow. It keeps the shapes it can
  read outright and declines the rest.
- **Teach the builder to read the slot itself** (a second `rt_get_elem` pair inside `heapListFrom`).
  Rejected: two reads of one question, and the two diverge the first time a kind is added to the tag table —
  the ADR 0241 argument, asked of a builder.
- **Let the tag be a constant the compiler guesses from the expression's apparent kind.** That is the
  status quo this row replaces: it printed `[1, 2]` for `['x', 2]`. `elemKindTag` remains what it is for
  spelled literals; a read is not a spelling.
- **Convert the one-word refusals into answers by threading a tag register through every caller.** Rejected
  for this cycle and owned by Gap R.146/Gap R.220; and no refusal in the family was relaxed to make a green
  suite — the call-argument row is asserted to *keep* refusing, with its sentence pinned.
- **Ship the widened door without the three filed rows, or fold them into this row.** Rejected by the
  tracker's own rule: a newly measured defect is a fresh row, and R.225/R.226/R.227 have different owners
  (an operand gate, the unrolled loop, the subscript road) — three owners cannot live in one row without
  making two of them undiscoverable.
- **Set bit 8 unconditionally on every built list.** Rejected: it is the object's claim that its slots are the
  truth, and a container whose slots all carry constant tags does not need a printer that asks them — the
  uniform-container static print paths (ADR 0232) are what keep `print([1, 2])` cheap.

## Verification

- `pkg/lang/pair_element_read_test.go`:
  - `TestALiteralElementThatIsAReadAnswersTheValueItDenotes` (36 answered rows: the four shapes of the row,
    each of the three containers, both literal and variable bases, a container the program built, three-level
    chains, the kinds whose payload means nothing alone, the list read back by `len`/subscript/`in`/`for`/
    `str`/`repr`/`==`, and the fold's two shapes);
  - `TestAPairElementWritesTheTagTheObjectsWrote` (the tag is a register, the payload is the other register,
    bit 8 is set, and the sibling integer slot keeps its constant tag);
  - `TestTheFoldStillOwnsTheReadItCanAnswer` (the negative control, over `@main` only — the runtime library
    legitimately calls the element door);
  - `TestThePairElementGateDecidesTheReadTheDoorAnswers` (15 gate decisions asked directly, including the set
    base, the escape-analysed name, the out-of-range position, the chain, and the union's name half);
  - `TestTheElementRoadIsNeverWorseThanThePrintRoad` (the parity that made this a defect, with the expectation
    asked of CPython in the case itself rather than remembered);
  - `TestTheOneWordPositionsStillRefuseAPairElement` (a call argument keeps refusing, naming the half and the
    row);
  - `TestATrapInsideALiteralElementIsRaisedNotRefused` (5 raises with class **and** sentence compared to the
    reference's, each then caught by the program's own `except`);
  - `TestWhatThePairElementDoorStillCannotReachIsFiledNotFixed` (Gaps R.225, R.226, R.227 and R.67 — each row
    carries this backend's answer and the reference's beside it, and fails when the answer changes, which is
    what makes it a ledger rather than a comment);
  - `TestThePairElementRowsAreRecorded` (the record leg answers each row, so the gate cannot be deleted
    without deleting the evidence).
- `integration/pair_element_read_test.go`: the answers through the CLI against CPython (expectations derived,
  not remembered), the traps by exit class with stdout empty and no `codegen:` sentence, the fold's IR under
  `--emit-llvm` + `--verify-llvm`, and the whole program line for line against
  `testdata/pair_element_read.expected`;
  `TestThePairElementProgramIsAnAssertedMatrixRow` keeps the row parity-asserted with an oracle verdict of
  `match`.
- `go test -tags=llvm20 -count=1 ./...` and `make testshards` green; every emitted module passes
  `opt -passes=verify` / `llc`.
