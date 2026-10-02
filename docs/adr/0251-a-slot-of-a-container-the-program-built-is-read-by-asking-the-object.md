# ADR 0251 — a slot of a container the program built is read by asking the object what it is

Date: 2026-10-02 · Status: Accepted · Roadmap: L11.1 (the nested read of a run-time-built container), ADR 0241, ADR 0246, ADR 0187, ADR 0210, ADR 0225, ADR 0205, ADR 0166, Gap R.94 (filed)

## Context

ADR 0241 granted the read `xs[0][1]` on a **compile-time promise**: the name was bound exactly once to a
container literal, nothing mutated the object afterwards, so the tag the literal's builder wrote beside
each slot is still the tag the slot holds. ADR 0246 extended the same trust to `len(xs[0])` of a container
the program *grew* — `xs = []` then `xs.append([7, 8])` — because every writer, literal or `append`, goes
through ADR 0187's one-write-per-`(payload, tag)` door. The one shape neither covered was the read **one
level below** such a container:

```
xs = []
xs.append([7, 8])
print(xs[0][0])          # CPython 7 · --interp 7 · --aot exit 1: "index cannot reach into xs's slots"
d = {}
d["a"] = [1, 2]
print(d["a"][1])         # same three legs, same three answers
xs.append({"k": 5})
print(xs[3]["k"])        # the inner kind differs per slot: no static answer exists at all
```

This was the clause L11.1 names as its own remainder, and the refusal — honest as refusals go — left the
program unanswered while both other engines printed the value. Three shapes were stuck behind it, not one:
the print and binding position (`print(xs[0][0])`, `y = xs[0][1]`), the equality (`xs[0][1] == 8`), and the
length (`len(xs[0][0])`). The static door could not open because `xs`'s slots were never described by a
literal; the untagged path beneath it could not open because an `i32` payload with no tag is a number
wearing another object's bits — reading one as a handle prints whatever object happens to share its index.

## Decision

A subscript whose container side is itself a slot read is lowered to a **dispatch on the tag the object
carries**, not on a table the compiler kept. `slotReadUnderTag` tests the outer slot's tag and emits one
arm per kind the payload can name:

- **a list slot** is read by position — `@rt_get_elem` and `@rt_tag_of` behind `normalizeIndex`, so the
  same negative-index rule and the same `IndexError` an explicit `xs[i]` raises (ADR 0210) are reached
  *through* the tag rather than around it;
- **a dict slot** is read by its `(payload, tag)` key — `@rt_dict_get_tagged`/`@rt_dict_value_tag` behind
  `checkKeyReadTagged`, so a missing key raises `KeyError` exactly as a dict variable's does;
- **a text slot** answers a one-character string via `@rt_str_char`, `s[i]` being text and not a byte
  (ADR 0225), with the sentinel checks that turn "no such position" into `IndexError: string index out of
  range`;
- **a set slot** is asked the membership question, because that is what a set subscript means in this
  language at all — a documented gusty extension (`docs/language.md § Dicts & sets`, ledger rows
  `programs/data_b`/`programs/features_b` recorded `oracle: not_applicable` because CPython rejects any set
  subscript). One question, one answer, at every depth; the arm is `@rt_set_find` with `KeyError: not in
  set` when no member matches;
- **a slot the tag reports as a number or `None`** — or as any kind with nothing to read — raises the
  sentence CPython raises, per kind: `'int' object is not subscriptable`, `'float' …`, `'bool' …`,
  `'NoneType' object is not subscriptable`, and a generic `object is not subscriptable` for a kind the
  language has not grown `__getitem__` for. It is a *trap*, not a refusal: the exit class is 3, the
  program can catch it, and the compiled leg stops being the only engine that answers a program with exit
  1.

The value that comes back **is** the pair: the four arms merge through one `phi` over the two words, so the
print, the binding, the equality, the length and a further subscript all take the same answer, and
`xs[0][1][2]` is the same door one more time. Each value-producing arm ends in its own tail block and the
raise arms end at the handler instead, so the merge names only predecessors that actually produce a value —
ADR 0205's rule, the one ADR 0250's ordering arms follow too.

Two doors now answer "which pair does this subscript denote?" and they are tried in order inside one
function (`taggedSlotPair`): the **static** door (ADR 0241, `containerHandleOf` + `containerSlotRead`) and
the **run-time** door (this ADR). The gate over the run-time door (`runtimeSlotReadOf`) is the same
predicate the door itself checks before emitting, so a shape the gate admits can be declined by the door
(the caller keeps its own refusal) but a shape the gate rejects is never lowered by the door — the
disagreement that would ship a silently-unsupported shape. The nested gate also steps aside wherever the
static door already answers, so a container the literal still describes keeps its existing, already-pinned
read rather than getting a second one.

## Consequences

- `programs/probe_nested_list.gy` matches CPython on every line and is promoted from the oracle ledger to
  `conformanceStandalone()`; `integration/comprehension_brace_element_test.go` loses the refusal row
  `indexing a dict slot of a built comp` (`d = [{"k": x} for x in [1, 2]]` / `print(d[1]["k"])`), which
  moves to the parity table beside a new list-slot row. A refusal pin that never breaks is not measuring
  anything — the break is the evidence the door opened.
- Pinned new: `pkg/lang/runtime_slot_read_test.go` (parity on both engines, traps that must be **raised**
  rather than refused, the refusals that remain, and an IR row asserting the dispatch really branches on
  `@rt_tag_of`'s answer with one helper per arm and no `i32 @.` global in a value position) and
  `integration/runtime_slot_read_test.go` (the same three tables against CPython through the CLI, plus a
  table that pins the gusty-only set-subscript extension with the two engines against *each other*, since
  the oracle cannot judge it).
- A set **slot** now behaves like a set **variable**: both read a member, both say `KeyError: not in set`
  for a member that is not there. What does *not* follow, and is filed rather than quietly absorbed:
  **Gap R.94** — a set *variable* read directly (`sa = {5, 6, 7}` then `print(sa[6])`) is answered by the
  interpreter and refused by the compiler (`index of a non-literal variable`), which is the documented
  extension reaching only one backend.
- Still refused by naming itself, all of them needing the *kind* or the *number* rather than the tag: the
  numeric use of such a slot (`xs[0][0] + 1`), its negation and its ordering (L11.1's remaining clauses,
  Gaps R.85 and R.93), a membership test or a loop whose haystack is such a slot (`7 in xs[0]`,
  `for v in xs[0]`), and a comparison against an expression whose kind cannot be proven (Gap R.83). The
  refusal sentence was widened with this cycle and `in` was **removed** from its list of things that work,
  because it does not work and Gap R.38 counts a refusal that overstates the backend as its own defect.
- Agentic path: nothing new to learn. The shape moved from *compile-time refusal* to *answer*, so an agent
  that had learned "`xs[0][0]` needs a literal" no longer has to; the refusals that remain are one stable
  sentence each, `--json` reports the trap class and exit 3 for the raises, and the exit-code contract is
  unchanged (ADR 0166): 0 answers, 3 traps, 1 names a missing capability, 2 stays a compiler bug — tested
  for by asserting `code != 2` in every new table.

## Alternatives rejected

- **Fold the run-time read into `containerHandleOf`.** One handle-plus-kind function serving print, `len`,
  `in`, `for` and the subscript looks tidier, and it is wrong: those callers select a *helper by kind* at
  compile time, so answering with a handle whose kind only the object knows routes a dict slot to
  `@rt_list_len`, or prints a dict as a list. The tag is a run-time fact; a `kind string` is not where it
  belongs.
- **Widen `isTaggedSlotRead` to every nested read and let the comparison door decide.** This changes the
  path a *literal*-backed read takes today (`m[0][1]`, `t[0][0][0]`), for which tests already pin CPython's
  answer — two doors producing one answer is fine, two doors *racing* for the same shape is how a passing
  suite starts disagreeing with itself. The gate therefore answers yes only where the static door declines.
- **Raise `'set' object is not subscriptable` from the set arm, matching CPython.** Considered seriously,
  because this language's oracle is CPython and the interpreter had been made to agree with the new arm for
  about ten minutes. Rejected: the shape is *documented surface* with `oracle: not_applicable` ledger rows
  behind it, so removing it here would be a language decision smuggled in under a codegen fix. What this
  cycle does instead is make the extension consistent across engines and depths, and correct the paragraph
  in `docs/language.md` that called it a positional read while both engines had always read it as a
  membership one.
- **A single IR helper (`@rt_slot_read`) doing the dispatch inside the module.** It saves the emitter ~100
  lines and costs the traps: `normalizeIndex` and `checkKeyReadTagged` raise through `raiseTo`, which
  resolves the innermost `try`/`except` handler from the *generator's* frame stack, and the traceback frame
  from the current function's source. A helper cannot see either, so bounds checks would have to become
  sentinels compared at every call site — more IR, not less, and one more place for a check to be forgotten.
