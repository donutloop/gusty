# ADR 0353 — A key is asked, not spelled: a `None` or bool key reaches the doors that read a dict slot's own tag

Status: accepted
Date: 2026-10-16
Amends: [ADR 0327](0327-a-dict-literal-is-a-dict-so-its-entries-are-read-by-the-doors-a-dict-variables-entries-are-read-by.md), [ADR 0233](0233-a-float-slot-carries-its-box-handle-and-its-tag-so-a-mixed-container-can-print-and-compare-one.md)
Depends on: [ADR 0232](0232-a-container-records-its-kind-its-per-slot-tag-and-what-the-slots-hold.md), [ADR 0273](0273-a-pair-crosses-a-call-because-the-callee-runs-where-the-callers-slots-do-not.md), [ADR 0249](0249-a-refusal-naming-the-missing-half-is-the-answer-an-agent-can-branch-on.md), [ADR 0302](0302-one-execution-backend-the-record-leg-and-the-reference-leg-are-the-two-witnesses.md)
Roadmap row: Gap R.221 (the row L11.1 filed when ADR 0327 opened the pair door for a dict *literal*)

## The gap, as measured

The same program, three keys, three answers — all of them at **exit 0**, nothing raised, nothing refused:

```gusty
d = {None: 3.5}
print(d[None])          # 3        ← this toolchain      3.5   ← the reference
d = {None: "hi"}
print(d[None])          # 0                                   hi
d = {None: [2]}
print(d[None])          # 3                                   [2]
d = {False: True}
print(d[False])         # 1                                   True
```

`3` is the handle of a `@float_box` object, `0` is the position of `"hi"` in `@str_tab`, and the third `3` is
the allocator's slot for the list. Each is the **payload** of an entry whose **kind** stayed in the slot. The
family is fifteen programs wide — `None`, `True` and `False` as keys, crossed with float, text, `None`, bool,
list, dict and int values — and every one of them printed a number the program never wrote.

The same reads with an `int`, a text or a float key already answered correctly, and the *bound* form
(`x = d[None]` then `print(x)`) answered correctly too. That asymmetry is what named the defect: the door was
not missing, the road into it was.

## The decision

**The keys a dict read may be asked with are the keys that carry a tag, and that list is one door's answer,
not four.** `pkg/lang/dict_key_pair.go` is that file: `dictKeyCarriesPair` names the key kinds — a name, an
int, a text, a float, `None`, `True`, `False` — each of which has a payload *and* a tag in the runtime's
table. `mixedDictIndexRead`, the gate in front of `mixedDictPair` (the `rt_dict_get_tagged` +
`rt_dict_value_tag` pair ADR 0327 built for a literal's entries and ADR 0233 had built for a float key), asks
that one function. Its four callers — the print door, the binding door and the two `and`/`or` doors —
therefore cannot each keep a list of spellings, which is how a gate and a door grow apart (ADR 0249's lesson,
the one ADR 0332 wrote as "a gate and a door that disagree is how this row ships a refusal nobody measured").

The change is the gate. `rt_dict_value_tag` already answered the value's kind beside the payload;
`rt_print_mixed_value` already dispatches on it; `bindTaggedVar` already carries a pair into a name. Nothing
in the runtime, the builder, or the printers moved: the entry was always written with its tag (`bit 8`, ADR
0232), and it was always read back with it — except on the road whose `switch` had no arm for a `NoneLit` or a
`BoolLit`.

Thirty-six measured shapes changed answer (a 301-program probe plus the whole conformance corpus, no other
program changing answer), including the run-built forms (`d = {}` / `d[None] = 3.5`) and the multi-key form
(`d = {None: 1.5, True: "a", False: [2]}`).

## What was rejected

- **Adding a `NoneLit` arm to `constKeysMatch`** — the static key door that answers "*what kind is in this
  entry?*" by walking the dict's own literal. It was tempting because it converts a refusal into an answer
  (`d = {None: [1, 2]}` / `print(d[None][0] + 1)` goes from `codegen: index cannot use an element of d as a
  plain number` to `2`). It was tried, built and measured, and it **regressed four shapes**: with the arm,
  `"a" in d[None]` over a text answers `False` where the reference answers `True`, over the integer `5`
  answers `False` where the reference raises `argument of type 'int' is not iterable`, and `for x in d[None]`
  prints nothing over a text and `0 1 2` over `5` where the reference raises `TypeError` for both. Two roads
  consult the static door to label an operand and cannot yet read a `None` slot; widening the door handed
  them a label they cannot honour. The arm is reverted, the measurement is kept in the comment above
  `constKeysMatch`, and the whole question — including the latent half where `foldConstInt` reads a `NoneLit`
  as the integer `0` — is filed as **Gap R.285**'s sibling claim in the tracker.
- **Refusing a `None`/bool-keyed read instead of answering it.** There is no honesty in refusing a program
  whose answer the objects already hold: the pair door answers it, and the ladder this repo runs on says a
  cycle turns refusals into answers, never answers into refusals.
- **Widening the value-kind notebooks** (`dictValStr`, `dictKeyStr`) so the text and numeric ladders label a
  `None`-keyed read the way they label an int-keyed one. This is the *real* root of the two asymmetries left
  standing — a dict whose values all share one kind still gets its notebooks cleared, because a `None` or
  bool *key* is self-describing and forces the object's slots to speak for themselves. It is the right fix and
  it is a row of its own (**Gap R.285**), because the notebooks feed `rt_dict_print_s` and the comparison
  ladder, and a cycle that changes what a printer believes about a dict's values without measuring every
  printer that reads that belief is how this file's own genre of bug ships.
- **Teaching the read door to ask the object for the key tag instead.** The object already receives the key
  tag from `taggedOperand`; the missing word was upstream, in whether the road took the pair at all.

## What the row leaves on the tracker, and how it is pinned

Five shapes the row does not own, and the one sentence beside them, each with today's compiled answer pinned
in a test, so none of them can arrive as a surprise:

| Row | Shape | Today | Reference |
|---|---|---|---|
| Gap R.283 | `print(f"{d[None]}")` | `3` at exit 0 | `3.5` |
| Gap R.284 | `if d[None]:` over a text | `n` (the payload tested against zero) | `y` |
| Gap R.285 | `d[None] + "b"` / `d[None] * 2` | refusal / `0` at exit 0 | `"ab"` / `"aa"` |
| Gap R.286 | `k = None` / `print(d[k])` | `KeyError: key not found` | prints the entry |
| Gap R.287 | `print(abs(d[None]))` over a float | `3` at exit 0 | `3.5` |
| Gap R.288 | `d = {[1]: "x"}` / `print(d[[1]])` — the refusal the **untaggable-key** control produced | refuses, and the sentence ends `…or run it interpreted` | raises `TypeError: unhashable type: 'list'` (a refusal is honest; the offer is not — there is one backend since ADR 0302) |

Two of them (R.283, R.284) are **not** key-dependent — the int-key twin answers the same wrong thing — which
is why they are separate rows and not clauses of this one. R.288 arrived from the other control the probe ran:
asking what the gate does with a key it **cannot** tag, where the refusal that came back still advertises the
execution engine ADR 0302 retired. R.285 is the key-dependent pair, and its two faces
point in opposite directions: where the int-key twin *refuses*, this road *answers* a payload, which is the
worse of the two.

## Agentic rationale

`--json` and `--capabilities` needed no new entry: the row removes a wrong answer and adds no refusal, so the
capability registry is untouched and stays the single list it is (ADR 0346). What an agent gets is the gate as
a function — `dictKeyCarriesPair` is asserted as a table in both directions in
`pkg/lang/dict_none_bool_key_test.go`, so a future cycle can ask which key kinds the backend believes without
reading four callers, and a gate that only ever answers `true` fails a case. The IR-shape rows read the
program's own `@main` body for `@rt_dict_get_tagged`, `@rt_dict_value_tag` and `@rt_print_mixed_value`, and
**against** the `%d` formatter: every wrong answer in this family was a payload written by a numeric format.

## Witnesses

- Record leg: 77 sources added to `pkg/lang/testdata/interpreter-golden.json` (6931 → 7008) through
  `tools/recmerge`, which asks CPython first and refuses to write an entry the compiled leg will not agree to.
- Reference leg: `integration/programs/dict_none_bool_key.gy`, 26 lines, `match` on the matrix; the ledger
  row is an asserted row, not debt, so a regression breaks the build rather than a pin.
- Unit: `pkg/lang/dict_none_bool_key_test.go` — 33 answering print rows, 25 door positions, the missing-key
  traps with class *and* sentence, the gate's own table, the tag-reading-printer rows, the single-kind
  negative control, the never-worse ladder with the int-key twins' rows named, and the filed-not-fixed table.
- Integration: `integration/dict_none_bool_key_test.go` — the same programs through the CLI (both legs, exit 2
  forbidden), the trap class asserted as exit 3 rather than the compiler's exit 1, and the filed rows pinned
  at the command line too.
- `make testparallel` green, and the 301-program probe reports no other program changing answer.
