# ADR 0361 — A tuple is a container whose object says it is a tuple

Status: accepted
Date: 2026-10-16
Amends: [ADR 0251](0251-a-container-owns-its-element-kinds.md) (whose rule — the object, not the expression that built it, owns what a container holds — is extended to one more attribute of the object: whether it is immutable and prints with parentheses), [ADR 0187](0187-a-slot-carries-its-tag-beside-its-payload.md) (the pairing rule that `rt_slice` had never followed for the object's own attribute word)
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md), [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md), [ADR 0171](0171-generated-runtime-constants-come-from-the-canonical-tables.md), [ADR 0173](0173-a-runtime-block-travels-with-the-module-that-references-it.md), [ADR 0182](0182-one-kind-table-for-the-printer-the-comparison-and-the-raise.md), [ADR 0214](0214-the-trap-message-is-the-reference-s-wording-not-the-runtime-s.md), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md), [ADR 0249](0249-a-refusal-names-what-is-missing-and-never-blames-a-construct-the.md), [ADR 0229](0229-a-string-is-an-index-into-a-table-the-runtime-can-add-to.md), [ADR 0258](0258-str-and-repr-are-one-printer-pointed-at-two-sinks.md), [ADR 0339](0339-a-sort-is-a-door-not-a-repetition-of-the-comparator.md), [ADR 0360](0360-a-text-on-the-left-of-percent-formats-and-the-answer-is-a-text-value.md)
Roadmap rows: L11.3 (paid — tuples are values, not syntax sugar), Gap R.309 (filed — a container has no order door), Gap R.310 (filed — the empty tuple is one object to the reference), Gap I.2 (still owed — `count`/`index`), Gap R.146 (still owed — the element of a container the compiler cannot kind), Gap R.288 (the `unsupported expression *lang.Tuple` sentence this retires)

## The gap, as measured

`probe_tuple.gy` began with `t = (1, 2, 3)`. At the head of this branch, before this ADR, the compiled
backend answered every one of those lines with the same sentence:

```
gustyc: jit: codegen: unsupported expression *lang.Tuple
```

an AST type name in a user's face (the half of it Gap R.288 already files), issued for
`print((1, 2))`, for `t[5]`, for `(1, 2) == (1, 2)`, for `sorted([(2,1),(1,2)])`, and for
`def mk(a,b): return (a,b)` — six of the eight shapes measured below. Two more did worse than refuse:

| program | compiled at HEAD | CPython |
|---|---|---|
| `print((1, 2))` | `unsupported expression *lang.Tuple` | `(1, 2)` |
| `a = (); b = (); print(a is b)` | **2 parse errors** (the empty tuple did not parse) | `True` |
| `t = (1,2); t.append(3)` | `unsupported expression *lang.Tuple` | `AttributeError: 'tuple' object has no attribute 'append'` |
| `print((1,2)[5])` | `unsupported expression *lang.Tuple` | `IndexError: tuple index out of range` |
| `print(-("a", 1))` | raised `bad operand type for unary -: 'list'` | `… unary -: 'tuple'` |
| `d = {(1,2): 1}; print(d[(1,2)])` | `a compiled dict literal holds constant integer keys only` | `1` |
| `print(sorted([(2,1),(1,2)]))` | `unsupported expression *lang.Tuple` | `[(1, 2), (2, 1)]` |
| `def pair(): return (1,2); print(pair())` | `unsupported expression *lang.Tuple` | `(1, 2)` |

The record leg was no help and had to be read as what it is: the retired engine rendered **every tuple as
a list**. `testdata/interpreter-golden.json` still carries `print(-("a", 1))` with
`exnMsg: "bad operand type for unary -: 'list'"` — the retired engine's own habit, recorded verbatim.
Under ADR 0302 the reference is the definition and the record is evidence, so this ADR answers CPython's
`'tuple'` and leaves the record's `'list'` on the drift ledger as a divergence **from the record**, which
is what a record-is-wrong row looks like.

## The decision

1. **A tuple is a `ListLit` carrying one boolean, not a second value kind.** `ListLit` gains
   `Tuple bool` with `IsTuple()` / `AsTupleList()` (`pkg/lang/ast.go`). The parser produces that node for
   every spelling the reference calls a tuple: `(1, 2)`, a trailing comma `(1,)`, the bare right-hand side
   of `a, b = 1, 2`, and — newly parsed — the empty `()`. `*Tuple` **stays** as a pattern and legacy node,
   because `match`, the analyzer's `tuple[int, str]` type and the old roads already speak it; what this
   ADR forbids is a *third* representation, so every road asks one predicate, `g.isTupleExpr`
   (`pkg/lang/tuple_value.go`), which accepts either spelling.

2. **The tuple fact lives on the object, in the attribute word it already had.** The compiled heap has one
   container layout — `{kind, length, [256 x i32]}` beside a per-element tag table (ADR 0232) — and a
   tuple is that object with one bit set: `estrBitTuple = 16` inside `@estr[h]`, beside the bits that
   claim "these positions hold interned text" and "the slots are self-describing" (ADR 0187/0251).
   `rt_alloc` clears the word, so a recycled slot cannot inherit a dead tuple's flag. The printer, the
   container dispatcher and every door that would write into a slot read that bit; nothing re-derives
   tuple-ness from the expression that built the value, because — ADR 0251's reason — the expression is
   not the thing that knows what the container is.

3. **A tuple literal is always heap materialised.** The compile-time global shape (`@.lstN`) has payload
   and tag rows and **no attribute word**, so a folded tuple literal would arrive claiming to be a list.
   `value()` routes every `*ListLit` with `IsTuple()` to `heapListFrom` instead of the global table.

4. **Immutability is a refusal, not a silent write.** `append`, `extend`, `insert`, `remove`, `pop`,
   `sort`, `reverse`, `clear` and item assignment `t[0] = v` stop the build through
   `refusesTupleMutation`, quoting the reference's own `AttributeError: 'tuple' object has no attribute
   'append'` / `TypeError: 'tuple' object does not support item assignment`. The compiled layout *could*
   carry those writes — it is the list's layout — so a road that did not ask the tuple question would
   happily mutate a tuple and print an answer the reference never produces. A build-time refusal spends
   exit 1 to prevent that; a runtime no-op would let the program keep running on a value the language
   does not have (ADR 0166). The catchable raise is filed, not pretended to.

5. **Which sentence a bounds trap uses is asked twice, of the only witness each road has.** A road that
   can see the source (`t[5]` on a literal, `d["k"][5]`) asks `g.isTupleExpr` and emits the fixed sentence
   from `indexOutOfRangeSentenceFor`. A road that holds only a handle at run time (`normalizeIndex` over a
   slot) calls `rt_tuple_flag(i32 %h)` on the bad branch, because there the object is the only thing that
   knows. Both answer `tuple index out of range` for a tuple and `list index out of range` for a list;
   neither guesses.

6. **A copy carries the object's attributes, and a slice of a tuple is therefore a tuple.** `rt_slice`
   already copies payloads and, since ADR 0187's pairing rule was applied to it, tags. This ADR adds the
   third thing the source object owns: the `@estr[h]` word travels with the slots. Without it
   `t[1:]` printed `[2, 3]` — a tuple slice arriving with no attribute word and printing as a list. The
   same rule is why `sorted(...)`, `reversed(...)` and the dict-view roads keep their **specific**
   printers (`rt_print_list_mixed`) rather than being switched to the object dispatcher: those roads build
   copies that carry no attribute word, and a dispatcher that trusts a missing word prints a list where
   the program wrote a tuple, or the reverse. Use the dispatcher (`rt_print_container_value` →
   `rt_print_list_or_tuple`) only where the object owns the answer.

7. **A tuple is hashable when its elements are, and it says so elementwise.** `dictWantsContainerKey`
   accepts a tuple key whose every element is hashable (`tupleElementsHashable`), the unhashable-key road
   walks inside and names the **first inner container** — `d = {(1, [2]): 1}` says `'list'`, the reference's
   left-to-right hashability walk — and a literal tuple key routes to the tagged-pair read road
   (`dictKeyCarriesPair` → `tupleKeyCarriesPair`) so that a missing key prints `KeyError: (3, 4)` instead
   of an interned index or `key not found`.

8. **A tuple on the right of `%` is an argument list and a value** (ADR 0360's door extended): the
   elements spread as the arguments, and the tuple itself renders as `(1, 2)` when a `%s` asks for it.

9. **`probe_tuple` is promoted** from the oracle-debt ledger to an asserted parity program
   (`conformanceStandalone`), and `probe_enumerate`'s reason is narrowed: its tuple half is paid, its
   remaining debt is `enumerate`/`zip`/`list(<container>)` lowering (L11.7), not the tuple.

## Why one layout and a bit, rather than a tuple layout

A second heap layout for tuples would duplicate the printer, the equality road, the slicing door, the GC
marking and every `@heap_elems`/`@heap_tags` GEP in the module — about a dozen doors that would each have
to be taught a second shape, and each pair of shapes is one more pair whose answers can drift apart (that
drift is what ADR 0232 and ADR 0182 exist to prevent). The tag already distinguishes the *kind* the value
prints as for every other kind, and `@estr` already exists precisely to hold "facts about this object that
the tag cannot say". One bit reuses the whole container system and keeps a tuple and a list from disagreeing
about `len`, `in`, `==`, and slicing while differing about brackets.

The bit is on the **object**, not on the value word, for the same reason ADR 0251 gave for element kinds:
`p = mk(1, "x")` binds a name to a handle, and the statement that printed `(1, 'x')` correctly is the one
that asked the handle. A compiler-side-only fact could not survive `p = mk(...)` followed by `print(p)`,
and an object-side-only fact could not stop `t.append(3)` before the build, which is the half this ADR
needs a refusal for. Both are kept, and each is asked where it is knowable.

## Alternatives rejected

- **Render tuples as lists and keep the parentheses in the parser only.** The record's own answer, and the
  one a reader of this repository would have found familiar. Rejected: `print((1,2))` is `(1, 2)` to the
  reference, `t is` a tuple, and `t == [1,2]` is `False`; a value that prints as the wrong type cannot be
  the language's tuple.
- **A new `KindTuple` tag.** Rejected: tags index `ValueTagList()`, the printer, the comparison and the
  raise (ADR 0182), and a new one would have to be added to every consumer at once — and it still would not
  give the equality road a way to say "a tuple is not a list with the same elements", which the tuple bit
  answers with one compare inside `rt_container_eq`.
- **A runtime no-op for tuple mutation with a warning.** Rejected: exit 0 with an extra line is the silent
  near-wrong answer ADR 0166 forbids, and the warning is not catchable by the `except AttributeError` the
  reference's version reaches.
- **Emitting `AttributeError` at build time as a trap** (a raise the program could catch). Deferred: the
  raise needs the exception-code table and a frame, and the mutation roads run before the raise machinery
  is open; filed as part of the row rather than faked.
- **Interning the empty tuple so `() is ()` answers `True`.** Rejected for now: `[] is []` is `False` to the
  reference, so a canonical slot for every empty container would be wrong in the other direction; the
  canonical slot has to be tuple-only and minted by `rt_new_list` under the tuple bit. Filed as Gap R.310.

## Consequences

Answered on the compiled leg, checked against CPython in `pkg/lang/tuple_answer_test.go` (row by row, in
the same process, and the reference re-run inside the case so a row cannot be quietly rewritten to match a
guess): literals and the three printings `(1, 2)` / `(1,)` / `()`, indexing including negatives, `len`,
membership, equality (a tuple never equals a list, in either direction, and `(1,2) == (1,2.0)` is `True`),
unpacking through both the pattern road and the literal road, iteration, nested indexing, slices that stay
tuples, tuples as dict keys and set members, tuples inside lists/dicts printed and read back, tuples as
formatted arguments, and a function returning a tuple the caller can print and compare.

Traps answered with the reference's class **and** sentence: `IndexError: tuple index out of range` (four
roads), `KeyError: (3, 4)`.

Refused, in words, with the row that owns the fix named: mutation (this ADR's own refusals), sorting a
container whose elements are containers (`Gap R.309` — see below), `count`/`index` (`Gap I.2`, refused for a
list exactly as for a tuple), and ordering operators between containers (`Gap R.309`).

Two answers are still wrong on both legs and are parity-guarded rather than asserted: reading **one
element** out of a container the compiler cannot kind — `def mk(a,b): return (a,b)` then `print(mk(1,"x")[1])`
prints the interned index as a number — is the same wrong answer for a list, and the parity test
(`TestTupleElementReadsMatchTheReferenceForListsToo`) requires the tuple road and the list road to stay
wrong or right **together**, so a future fix cannot land on one leg alone. That shape is the
one-word-position family (`Gap R.146`), not this row's.

## The sort door, and why a tuple made it a wrong answer rather than a missing one

`@rt_sort` (ADR 0339) compares by payload under one mode per kind: numbers by the number, interned texts by
`strcmp` of the text. A container has no payload-order — its payload is a **heap slot**, i.e. the order the
value was allocated. So `sorted([(2,1),(1,2)])` compared 0 with 1, found the input already "sorted", and
printed the arrival order at exit 0. That is a fabricated answer of exactly the shape ADR 0166 exists for,
and it was reachable for lists too (`sorted([[2,1],[1,2]])` did the same at HEAD).

The door now asks `sortElementsAreContainers` before it claims an order and refuses, naming
`Gap R.309` — the container comparator, elementwise, shorter-is-less on a tie, raising `TypeError` for a
pair with no order — which is a runtime door of its own (`rt_elem_pair_order`) that this cycle did not have
time to build and test against the ordering ledger. Refusing is the honest placeholder: the program stops,
the reader is told which rule is missing, and nobody prints an order that was never computed.

## What the record disagrees about

`print(-("a", 1))`: the record says `'list'` (the retired engine's wording), the reference says `'tuple'`.
This ADR follows the reference, so the source is a **divergence from the record** and lives on
`testdata/interpreter-golden-drift.json` by the harness's own rule; deleting it would mean agreeing with a
record that is the debt. The unpack-count shape (`a, b = 1, 2, 3`) has no record at all, is refused at build
time where both sides of the binding are visible, and says the reference's own
`ValueError: too many values to unpack (expected 2, got 3)` sentence inside the refusal so a reader who
greps the wording finds the road.

## The three things the plan did not predict

**A tuple never takes the compile-time global shape.** The fold `@.lstN = private global {i32, [N x i32]}`
is the fast road for a constant list, and it has no attribute word beside it. A tuple that reached it would
print with the wrong brackets, and worse: handing that *global* to a door that takes a handle —
`call i32 @rt_heap_kind(i32 @.lst1)` — is the exit-2 shape ADR 0188 removed for lists, which `llc` rejects
and the exit-code contract counts as the compiler's bug. ADR 0188's rule was "the object has to be a heap
object because its own bits answer the question"; a tuple's bits include a bit the list never had, so the
tuple asks the same question and gets the same answer. The one predicate that decides it is the literal's
own tuple flag, asked at the fold door (`value()`'s list branch), not a caller's guess about how the value
will be used.

**The container-return scan records a tuple answer as a kind of its own.** `containerAnswerKind` learned
`tupleAnswer`, and the choice mattered twice. Recording a tuple-returning callee as `listAnswer` put its
answer on the list's roads — the static global above, and a printer named by the road, which renders
`(1, 2)` as `[1, 2]`. Declining it entirely was worse: the number door then printed the allocator's word for
a container the scan had *proved* was a container. So the answer is recorded, its physical kind is settled
as the list's — the slots, the tag array and the object's kind word all say `list` — and its bracket claim
rides beside it in the binding notebook, so `print(b())` hands the handle to `@rt_print_container_value`
and the object answers. A tuple answer and a list answer are the same object with one extra bit, and the
scan now says so in two fields instead of lying in one.

**A refusal names the kind the program wrote.** `{"a": 1}[(1, 2)]` and `{"a": 1}[[1, 2]]` reach the same
door with the same node, and the door's sentence used to say `a list literal` for both. That is the disease
this row's sibling rows keep curing — Go where the source belongs — in a new place: the snippet helper now
asks the literal's own flag, because the only fact that distinguishes the two programs is the spelling the
reader typed (roadmap L11.3).
