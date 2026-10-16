# 0367 — Tuple unpacking takes the pair road

## Context

An assignment asks one question of its right-hand side: *does this value travel as a
(payload, tag) pair, and if so what are its two words?* ADR 0187 made the answer one door for
every pair a program binds, ADR 0267 gave it to the plain binding for the arithmetic a slot's
answer arrives in, ADR 0273 to the answer of a call, ADR 0316 to a fold, ADR 0241/ADR 0251 to the
slot the program built at run time.

Tuple unpacking asked none of them. `emitTupleAssign` evaluated each right-hand element with the
ordinary one-word `value`/`expr` road and stored the word into a freshly opened slot, recording
nothing about the value it had just bound. The same expression through `=` asked the pair doors
first. So the language had two answers to one question, and which one a program got depended on
whether its binding wore commas:

| source | reference | this backend, before |
|---|---|---|
| `xs = []; xs.append([7, 8])` / `a, b = xs[0][0] + 1, xs[0][1] + 2` / `print(a)` | `8` | exit 1 — `index cannot reach into xs's slots: it is not a list, tuple or dict` |
| `a, b = 1, None` / `print(a, b)` | `1 None` | `1 0` |
| `a, b = None, True` / `print(a, b)` | `None True` | `0 1` |
| `a, b = "a", "b"` / `print(a, b)` | `a b` | `0 1` — the `@str_tab` index |
| `a, b = 1, 2.5` / `print(a, b)` | `1 2.5` | `1 2` |
| `a, b = "a", 2.0` / `print(a, b)` | `a 2.0` | `a 2` |
| `a, b, c, d = 1, "x", None, True` / `print(a, b, c, d)` | `1 x None True` | `1 0 0 1` |
| `def f(): a, b = 1, "z"; return b` / `print(f())` | `z` | `0` |
| `xs = []; xs.append([7, 8])` / `a, b = xs[0][0], xs[0][1]` / `a = 5` / `print(a, b)` | `5 8` | exit 1, same refusal |

Four of those are answers the backend invented at exit 0 — a table index, a box handle's position,
`0` for `None`, `1` for `True` — which is the family L11.1's payload-never-without-tag rule exists
to refuse. The rest is one road's refusal meeting a program the other road answers.

## Decision

**An unpacking element is a binding position, and a binding position asks the pair doors.**
`pkg/lang/assign_pair.go` folds the doors that had been spread over four files
(`emitCallArgPair`, `emitFoldArgPair`, `emitIndexSlotReadPair`, `emitAssignValuePair` and their
four `bind*Pair` helpers) into **one ordered list**, `assignPairWords`, whose two halves the plain
assignment still asks at its own two positions (`indexPairWords` before its container roads,
`valuePairWords` after them) so its behaviour is unchanged by construction:

1. the subscript half — `taggedContainerRead`, the mixed dict, the literal dict entry, the tagged
   list, each of which hands back payload *and* tag;
2. the expression half — the pair a call already carries, the arithmetic the print position
   answers (`taggedArithPair`, only where the ordinary numeric road would refuse), the fold that
   hands back one of the values it was given, the run-time slot of a container the program built;
3. one door the unpacking owns alone, `namePairWords`: a name the pair road already bound is two
   allocas, and binding it again carries both words.

`emitTupleAssign` then walks that list **per element**, and binds every element afterwards through
`bindAssignPair` — `bindTaggedVar` (ADR 0187's one binding door) plus the two records that door
does not own: the origin the tag came from (so a later refusal names where the value it cannot
carry was read, Gap R.38) and the rooting a heap payload needs (ADR 0181). Evaluating every
element before storing any is what keeps the swap simultaneous: a pair door answers into
registers, so every load reads the old binding and every store happens afterwards.

One door had to be widened rather than moved. A name the pair road bound, rebound to a plain int,
read back `0`: the rebinding opened a fresh `i32` beside the pair slot instead of retyping the
pair's own payload, because the rebinding road asked only `isTaggedName` and the unpacking had
never registered its names there. The name door now asks `taggedVars` — the map the pair roads
write — so the rebinding goes by load-and-retype onto the pair's own slot. That is Gap R.142's
rule carried to the second binding road; the first road's version of it is ADR 0267's.

Nothing else changed shape. `int` elements still store one `i32`; the float road still binds
through `floatValue`/`bindFloatValue` and opens a `double` slot only where the plain binding is
allowed to; the front end's `unpacking N values into M names` build-time refusal (ADR 0361) and
the star-target cell layout are untouched; a shape no door answers keeps the ordinary road's own
answer or refusal, word for word.

## Consequences

* Both binding roads are witnessed by the same tables, so they cannot drift:
  `pkg/lang/unpack_pair_test.go` (40 answer rows, 3 IR rows, 7 refusal rows),
  `integration/pair_binding_test.go` (a fourth table: an unpacking answers what the reference
  binds, beside the three tables that already held the plain binding, the rebinding and the
  positions the pair does not reach), and the CLI table in the same file.
* `integration/programs/probe_pair_from_a_tuple_unpack.gy` stops being debt and becomes an
  answered row of the matrix; `integration/programs/unpack_pair_binds_by_its_tag.gy` is the
  parity program that walks every element kind through the door. 208 matrix rows, 176
  parity-asserted, 32 recorded divergences.
* The record leg gains both sources (`tools/recmerge`, 7743 entries): the answer is CPython's and
  the compiled leg cross-checked it, so a later cycle that breaks an unpacking fails on a record
  rather than inventing a new answer.
* The positions the pair still does not reach stay refused rather than answered by a payload:
  `print(a + b)` over two pair-bound names, `if a:` / `while a:` / a ternary condition,
  `print(a[0])`, and a call answer beside a slot read in one unpacking (**Gap R.323**, pinned as
  refusals). The unpacking of **one** collection (`a, b = xs[0]`, `a, b = t`, `a, b = d`), a
  non-name target (`a, d[0] = 1, 2`) and binding a pair-bound name to a *second* name are
  **Gap R.321**. Two wrong answers this row found and did not cause — an interned text name that
  answers from the intern fold, so `a, b = "x", "y"; a, b = b, a` prints `y y`, and a `None`
  returned out of a function that prints `0` — are **Gap R.322**, filed as debt rows with the
  reference's answer beside them.
* `docs/language.md` now says the unpacking binds by the element's own tag, and the capability
  registry's `lower.unsupported.slot-read-of-rebound-name` sample is the condition shape that
  still refuses, with `Gap R.321` among its rows; `go run ./tools/captable` regenerated the docs
  table the registry guard compares.

## Alternatives rejected

* **Widen the numeric gate so the unpacking's element stops refusing.** The refusal was the
  symptom, not the disease: the same gate, widened, would have kept answering `1 0` for
  `a, b = 1, None` — the elements the numeric road accepts are exactly the ones that never asked
  for a tag.
* **Give `emitUnpackedElement` its own pair branch** (the shape this cycle first attempted). It
  would have copied the plain road's four doors a second time, which is ADR 0232's "one question,
  asked once" broken in the direction that produced this bug, and it left the four `bind*Pair`
  helpers in place to drift from it.
* **Lower every unpacking element as an object.** `a, b = 1, 2` then answers correctly and pays a
  heap cell per element, and the IR-shape rows here (no `rt_get_elem`, no `rt_num_arith`, no
  `rt_lift_num for a literal unpacking) exist to make that trade visible rather than accidental.
* **Route the whole right-hand side through one container value.** The reference unpacks an
  iterable, but this backend has no iterator for `a, b = xs[0]` yet; pretending otherwise would
  have needed the star cell layout rewritten before the slot reads could land, and would have
  hidden the Gap R.321 refusal instead of pinning it.
* **Land the swap-of-text-names fix here too.** Its fold lives in the text value road, not in a
  pair door; patching it beside this row would have retired one fold instance while its siblings
  kept answering, which is the ADR 0172 lesson. It is Gap R.322 with the reference's answer
  recorded beside it.
