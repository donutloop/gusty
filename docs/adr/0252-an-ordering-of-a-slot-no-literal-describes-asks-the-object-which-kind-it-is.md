# ADR 0252 — an ordering of a slot no literal describes asks the object which kind it is

Date: 2026-10-02 · Status: Accepted · Roadmap: L11.1 (the ordering of a run-time-built slot), Gap R.93 (closed), Gap R.82, Gap R.85, Gap R.87 (extra shapes measured), Gap R.96 and Gap R.97 (filed), ADR 0250, ADR 0251, ADR 0248, ADR 0205, ADR 0187, ADR 0166

## Context

ADR 0250 gave `<`, `<=`, `>`, `>=` three arms — two numbers compared as doubles, two texts compared by their characters, and CPython's `TypeError` for the pair that does not order at all — and it read those arms off the **literal** that built the container: the elements the program wrote are the kinds a slot can report. ADR 0251 then opened the subscript of a container the program *built* rather than spelled out. What neither covered was the ordering of such a container's slot, and the sweep that landed ADR 0251 measured it:

```
xs = []
for i in [1, 2]:
    xs.append(i)
print(1 if xs[0] > "a" else 0)   # CPython TypeError · --interp TypeError · --aot printed 1

d = {}
d["k"] = 1
print(1 if d["k"] > "a" else 0)  # same three legs, same three answers

xs = []
xs.append([3, "a"])
print(1 if xs[0][0] > 1 else 0)  # 1 everywhere; the compiled leg exited 1 on the subscript refusal
```

The first two are the worst of the three classes in this line of work (Gap R.82's, Gap R.85's, Gap R.91's and Gap R.93's shared shape): the program crashes under the oracle and under the interpreter, and the compiled backend **prints a verdict for it** and exits 0. The third is ADR 0251's own remainder — the read was answered one level down and the comparison above it was not.

Why the literal is the wrong source here is worth stating exactly, because the tempting fix was a table. `xs` has no literal: its slots were written by `append`, in a loop, from values the compiler cannot enumerate. A table of "what `xs` can hold" would be either a guess or a lie the first time a second `append` lands. And the tags are already there — ADR 0187's writers put one beside every payload, and ADR 0246 and ADR 0251 taught `len` and the subscript to ask the object instead of the notebook.

## Decision

An ordering operand is now read in two ways, and the second one is the object. `orderShapeOf` first asks the literal (ADR 0250's door, unchanged, so every pinned row keeps the lowering that answers it today); when no literal describes the side — a container built by `append`/assignment, or a read one level below such an object — the gate `orderSlotIsObject` admits the side as **`fromTag`**: it may report any kind ADR 0187's writers can store, and all three arms stand, chosen at run time by the tests `orderAskTags` puts on the tag register.

The arm that neither numbers nor texts is the interesting one, because CPython's sentence names **both operand types**: `'>' not supported between instances of 'int' and 'str'`. With one side settled by the compiler and the other reported by the object, the raise arm is therefore not one raise but a **chain over the closed tag set** — `int`, `bool`, `float`, `NoneType`, `list`, `dict`, `set`, and the text tag as the unconditional last arm — each link raising the sentence for its own kind, the settled side contributing its own name. The set is closed because the writers are ADR 0187's: nothing outside that list can appear beside a payload, which is what lets the last link be an `else` rather than a fourteenth test.

An ordering — and only an ordering — can be lowered this way at all, because its **verdict is a bool whatever the operands turn out to be**. That is also the line this ADR does not cross: `xs[0] + 1` of the same slot still refuses, because `+` must know whether it answers `4` or `4.5` before the module exists (roadmap L11.1's remaining clause). The gate is written so the door cannot overreach:

- **one `fromTag` side at a time.** The sentence names two types, so two sides that each need a chain would need one branch per *pair* of kinds. A `fromTag` side is admitted only against an operand the compiler read itself (`ix == nil`); anything else steps aside. Container-against-container ordering therefore stays with the door that compares contents (Gap R.86) instead of gaining a wrong `TypeError` where CPython compares elements. "Read itself" extends to a **variable whose kind the compiler can name** — `xs[0] >= i` after `i = 3`, `xs[0] > t` after `t = "a"` — because `orderShapeOf` asks those names of the same predicates the arithmetic and print paths use (`numericUseKind`, `exprIsString`/`printsAsInternedStr`): a name whose word is an index into `@str_tab` is text to the ordering exactly as it is text to the printer, or one door's operand becomes another door's bug.
- **the read is ADR 0251's door, not a new one.** `orderTagOf` asks `taggedSlotPair`, which tries the static read and then the object, so a nested operand (`xs[0][0]`, `xs[0]["k"]`) is read by exactly the code the print, the equality and `len` use.
- **the checks keep their blocks.** The payload and tag are read in the block the comparison starts in, so an out-of-range position still raises `IndexError` there and a missing key still raises `KeyError` — the trap belongs to the subscript, not to the arm that never ran (ADR 0210, ADR 0205).
- **the merge stays legal by construction.** Only value-producing arms are predecessors of the `phi`; every link of the raise chain ends at the handler (ADR 0205's rule, the same one ADR 0250's arms and ADR 0251's subscript arms follow).

Both engines now answer every shape above, and the traps are *raised*: exit 3, catchable by `except TypeError`, with the slot's real kind named — `'list'`, `'dict'`, `'set'`, `'NoneType'`, `'float'`, `'int'`, or `'str'`, whichever arrived.

## Consequences

- Gap R.93 closes: a loop-built container's slot ordered against a text raises CPython's `TypeError` on both engines instead of printing `1`. The integration table holds the oracle's own sentence beside each row, so the wording is checked and not merely similar.
- ADR 0251's refusal list loses its "ordering" clause (the row moved to parity in `slot_order_object_test.go`); its *number* clauses — `xs[0][0] + 1`, `-xs[0][0]` — stay refused and still say so.
- Pinned new: `pkg/lang/slot_order_object_test.go` (29 parity rows × both engines, 12 traps that must be raised not refused, the catchability of the raise, the refusals that remain, and an IR row asserting the module branches on the tag and carries one CPython sentence **per kind**) and `integration/slot_order_object_test.go` (26 parity and 10 trap tables through the CLI against `python3`, exit 2 failing the file, plus a debt row for the bool slot whose sentence says `'int'` until L11.2 gives bool its own tag). `integration/programs/slot_order_object.gy` joined the conformance corpus as a `match` row on all three legs, so the feature is exercised by the same harness that checks every other program.
- Two shapes measured beside this change and **filed rather than absorbed**, because each needs a decision this ADR does not make: **Gap R.96** — `xs.append(3)` then `print(xs[0] / 4)` answers `0.0` and exit 0 where every other engine answers `0.75`; the true-division arm lifts an operand the tag never described, and `/` is the one arithmetic operator whose result kind *is* settled (always a float), so it is the arithmetic door's next row, not this one's — and **Gap R.97** — `xs[0] > ys[0]` over two built containers answers by the two payloads, printing a verdict for the same `TypeError` this ADR raises, because the gate refuses to guess one branch per pair of kinds.
- Extra shapes of two rows already open, measured while writing the tables: an ordering whose settled side is a **container literal** (`print(1 if 3 > [0] else 0)`) reaches `llc` as `%t1 = icmp sgt i32 3, @.lst1` and exits 2, which is Gap R.87's emission in a new costume; and an ordering against a **call of two kinds** (`xs[0] > pick(1)`) answers `1` where the oracle traps, which is Gap R.83's missing return tag on the ordering side. Both are now pinned by a table that fails when the compiler catches up.
- Agentic path: no new flag and no new exit class. What changed is that three programs an agent could only route around now answer, and the refusals that remain are one stable sentence each; `--json` reports the trap class (`TypeError`, `IndexError`, `KeyError`) and exit 3 for every raise in this ADR (ADR 0166's contract unchanged).

## Alternatives rejected

- **A per-variable table of "the kinds `xs` can hold", consulted at the comparison.** It is what the literal door already is, extended by guessing: the container was built in a loop, so the compiler's list is either incomplete the first `append` it misses or a claim about data it has not seen. The measured case for rejecting it is `xs.append(3); xs.append("a"); xs.append(1.5)` — three slots, three kinds, and no static list that serves all three without being the tag array itself.
- **Emitting the numeric arm unconditionally and letting the raise be the `else` for everything.** One `phi`, one arm, least IR. It raises `'>' not supported between instances of 'int' and 'str'` for a slot that holds a list, and 'list'-naming was the whole point: the sentence is the program's contract (`except TypeError` reads it, and a user reads it first).
- **Taking the two-`fromTag`-side shape too, with an n×m chain.** Emitted once behind a helper, this is 64 blocks per comparison and a sentence table nobody can audit; and the pair it would most often meet — two containers — is exactly the pair CPython *does* order, so the chain would be wrong for the common case (Gap R.86). Declined; measured as Gap R.97.
- **Routing the ordering through the same helper that answers `rt_payload_eq`.** Equality is closed over the pair comparison and returns one answer; an ordering has to *raise differently per kind* and has to reach `rt_str_order`/`rt_float_of`, and a helper cannot see the generator's frame stack to raise through `raiseTo` (ADR 0251 rejected the same idea for the same reason).
- **Letting the arithmetic door answer `/` in the same patch**, since its result kind is settled. Tempting and out of scope: `/` reaches a different lowering with its own operand-lifting bug (Gap R.88's history), and mixing a wrong-answer fix into an ordering feature is how a row gets closed by the wrong commit. Filed as Gap R.96 with the measurement.
