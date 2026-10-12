# 0323. A loop that picks its helper from the compiler's notebook walks an object the program never made

Status: accepted. Roadmap: closes `Gap R.95` (the membership test and the loop of `L11.1`'s run-time-built slot
read), files `Gap R.209` and `Gap R.210`. Continues ADR 0251 (the read below such a slot, answered by the tag),
ADR 0241 / ADR 0246 (the static slot door, and the refusal that says which promise ran out), ADR 0187 (a payload
travels with its tag or not at all), ADR 0232 (a text compared by its characters, never by its intern index),
ADR 0185 / ADR 0225 (a loop variable and a character carry the kind they are), ADR 0228 / ADR 0211 (a trap the
program's own `except` reaches), ADR 0271 (two questions do not share one raise sentence), ADR 0205 (a `phi`'s
predecessors are all blocks that stored a value), and ADR 0173 / ADR 0192 (a runtime helper is emitted because
some module asks for it).

## Context

The row read as two refusals: `print(7 in xs[0])` and `for v in xs[0]: print(v)` over a container built with
`append`. Both exited 1 with ADR 0249's mixed-list sentence — an admissible answer, since a door that cannot see
far enough says so. Re-measuring the family found **four more shapes that were not refusals at all**, and each of
them was the thing this compiler is supposed to have retired.

| program | CPython | compiled before | compiled after |
| --- | --- | --- | --- |
| `xs.append([7, 8])` / `print(7 in xs[0])` | `True` | exit 1, `… this context needs a single static kind (roadmap L11.1, Gap R.82)` | `True` |
| `xs.append([7, 8])` / `for v in xs[0]: print(v)` | `7`, `8` | exit 1, the same sentence | `7`, `8` |
| `d["a"] = [1, 2]` / `for v in d["a"]: print(v)` | `1`, `2` | **`0` at exit 0** | `1`, `2` |
| `d["a"] = "abc"` / `for v in d["a"]: print(v)` | `a`, `b`, `c` | **nothing at exit 0** | `a`, `b`, `c` |
| `xs.append(5)` / `print(1 in xs[0])` | `TypeError: argument of type 'int' is not iterable` | **`False` at exit 0** | that raise, catchable |
| `xs.append(5)` / `for v in xs[0]: print(v)` | `TypeError: 'int' object is not iterable` | **`0`, `1`, `2`, `3`, `4` at exit 0** | that raise, catchable |

Measured with three binaries the same afternoon: CPython 3.12, the HEAD build (`git worktree` at HEAD, built
separately), this cycle's build. The refusals and the wrong answers are both in the table because the fix had to
be judged against both: a row that only moved a refusal to an answer says nothing about the rows that had been
*answering*. One cell was wrong when first written down — the loop over a number slot was recorded as printing
nothing and in fact printed five fabricated iterations — which is why the column was measured rather than recalled.

Two existing pins broke, which is what landing a row is supposed to do:

* `integration/comprehension_brace_element_test.go` had `membership_in_a_slot_of_a_built_comp` —
  `d = [{1, 2} for x in [1]]` / `print(1 if 2 in d[0] else 0)` — in the table of shapes the backend *honestly
  refuses*, demanding the sentence "more than one kind". The set arm answers it now, so the row moved to the
  parity table and is checked against CPython instead of against a refusal.
* That same program sat in `pkg/lang/testdata/interpreter-golden-drift.json` as compiled-vs-record debt described
  as "the compiled backend cannot build a program the engine ran". Both legs and the record now agree on `1`, so
  the ledger row retired (338 → 337), rewritten by the sanctioned path — `GUSTY_GOLDEN_UPDATE=1` on a **serial**
  run, never a sharded one, because the ledger is per-process state (ADR 0315).

Both wrong-answer families have the same anatomy. A container **variable** has a kind the compiler wrote down —
`g.runtimeDicts`, `g.listVars`, `g.mixedLists` — and both these callers chose their runtime helper from that
notebook. When the iterable or haystack was a *slot*, the notebook described the object **holding** the slot, so:

* the loop asked `@rt_dict_len` of `d` and walked `d`'s own `[key, value]` words one entry deep, over a value
  that had never been a dict entry; a text slot had no entries to walk, so the loop body never ran and the
  program exited 0 having printed nothing;
* the membership test lowered `xs[0]` through the value path, collected the *payload* — the integer 5 — and handed
  it to `@rt_contains` as a heap handle, so the answer was whatever object happened to be numbered 5. `False`,
  confidently, at the exit code of success.

ADR 0251 had already solved this exact problem one level down. `slotReadUnderTag` emits one `icmp`/`br` per kind
and asks `rt_get_elem`, `rt_dict_get_tagged`, `rt_str_char` or `rt_set_find` in the arm the tag selects, with the
arms that can only fail leaving through the program's own `except`. The two callers in this row had simply never
been handed that door: they asked *what the compiler had written down* where the read asks *what the object is*.

## Decision

**Hand over the existing door; do not build a second one.**

* **`builtSlotPair(b, ix) (payload, tag, ok)`** (`pkg/lang/heapargs.go`) reads a slot the way a slot is read when
  nothing describes it — `rt_get_elem`/`rt_tag_of` for a list or set base, `rt_dict_get_tagged`/`rt_dict_value_tag`
  for a dict base, which is ADR 0187's pairing rule. Its first act is to **decline the shape ADR 0241's static
  door already serves** (`g.staticSlotTag` answers, so the literal still describes the slot). The two roads are
  disjoint by construction, which is why landing this one moved no existing row.
* **`membershipOfBuiltSlotHaystack`** walks the tag: `@rt_contains_tagged` for a list, `@rt_dict_has_tagged` for a
  dict — its **keys**, because keys are what Python tests a dict by — `@rt_set_contains_tagged` for a set, and
  `@rt_str_contains` for a text. The text arm asks a *different question* of its operand, and can only ask it of a
  needle whose kind is provable: ADR 0232's rule stays whole, and a needle the compiler cannot describe declines
  so the old refusal stands rather than comparing an `@str_tab` index with the integer it indexes. `not in` is the
  same door with the verdict turned over, not a second door.
* **The loop asks the object three questions** — how many steps, which word, which kind — through three new
  internal helpers beside the ones they call:

  ```
  %n  = call i32 @rt_iter_len(i32 %h, i32 %t)          ; list → rt_list_len, dict → rt_dict_len,
  %e  = call i32 @rt_iter_elem(i32 %h, i32 %t, i32 %i) ; set → rt_set_len, text → rt_str_nchars
  %tg = call i32 @rt_iter_tag(i32 %h, i32 %t, i32 %i)
  ```

  A list yields elements, a set members, a dict its keys **at the stride 2 the dict's own reader walks**, a text
  its characters — interned and tagged `str`, so `for c in s` prints `a` and not the index of `a` (ADR 0225,
  ADR 0185). One loop skeleton serves all four kinds; the compiler no longer knows which one it is compiling, and
  does not need to. The three helpers never raise: they are reached only from an arm that already proved the kind
  iterable, which keeps the source span, the exception class and the handler stack where `raiseTo` can see them.
* **Iterability is a raise, not a refusal, and the two questions do not share a sentence.** CPython says
  `argument of type 'int' is not iterable` to a membership test and `'int' object is not iterable` to a loop over
  the same value. They are not stylistic variants; one door writing the other's wording is exactly the mistake
  ADR 0271 recorded when `abs` borrowed the unary minus's sentence. So there are two helpers,
  `notIterableSentence` and `notIterableLoopSentence`, and the membership door's text haystack adds the third,
  `'in <string>' requires string as left operand, not int` — the only one of the three that names the **needle**.
  Every arm leaves through `raiseTo`, so the program's own `except TypeError:` reaches it (ADR 0228), which the
  trap tables assert by class **and** message and then by catching.
* **A kind with no verdict is named, not guessed.** What is left after the four walkable tags and the four
  raisable ones raises the reference's sentence for an `object`. Naming a user class would be inventing an object
  this pass cannot see, so it waits for the writer (Gap R.209).

## IR

Three new `define internal`s in `heapRuntimeIR`, all reached through `heapUsed` and therefore carried only by a
module that asks a slot one of these questions. The
membership door emits one `icmp`/`br` pair per kind, one `call` per arm, and a merge `phi` over **only the arms
that answer** — the raise arms have no edge into it, which is ADR 0205's rule and the reason a caught raise does
not wake up with an undef payload. The loop's `tagged` shape keeps the existing `alloca`-counter skeleton, so
`break`, `continue` and the `for … else` arm are the same instructions they always were; only the three helper
calls differ. The `read` classification of the three helpers is added to `heapArgKind` (`pkg/lang/opt.go`) so the
same aliasing reasoning the other readers get is applied to them.

What did **not** change is the proof that the new road did not leak: the IR table in
`TestRunTimeBuiltSlotMembershipAndLoopAsksTheObjectInTheModule` asserts a loop the compiler
*can* describe calls none of `@rt_iter_len`, `@rt_iter_elem`, `@rt_iter_tag`. The assertion is on the `call`, not
the name — these definitions are emitted in the shared heap block, so `@rt_iter_len(` in a module means nothing
and `call i32 @rt_iter_len(` means the door opened.

## Agentic rationale

An agent's whole budget is spent on answers that are not what they look like. Before this cycle a program could
ask gusty "is 1 in this container?" and be told `False` with exit 0, and the agent has no way to suspect that the
`False` came from the object numbered 5. The fix is therefore measured twice: the shapes that used to refuse now
answer like the reference, **and** the shapes that used to answer keep answering — the static door's shapes
(`for v in [1,2,3]`, `7 in [1,2]`, `for k in {"a":1}`) are pinned in the same tables precisely so a widening that
breaks them fails loudly. The refusal that survives says which half is missing and at which depth (`Gap R.210`),
so the next agent edits the door and not the program.

## Alternatives rejected

* **Give the loop and the test a `runtimeBuiltSlots` notebook** (record the kind of every slot any `append` or
  `d[k] =` writes). Rejected on the same ground ADR 0233 rejected compile-time promises about mutated objects:
  the kind of a slot is a fact about the object, decided by whoever wrote it, and a second notebook that goes
  stale is a second story that can diverge — ADR 0322's `blocked bool` was one bit of exactly that story.
* **Reuse the membership door's sentence for the loop** (or the reverse). One string, one test, green immediately
  — and every row of ADR 0271's table about a door that wrote the other's wording.
* **Make `@rt_iter_len` raise too**, so the loop has one call. Rejected because a raise from inside a helper loses
  the source span and the landing-pad bookkeeping the compiler owns at the call site, and because the four scalar
  kinds are known at emit time and cost four `icmp`s, not a runtime string table.
* **Answer `1 in xs[0]` for a number slot with `False`.** It is what an unwary reader expects, and it is the
  reference *raising* while this backend answers: a whole error class lost, and uncatchable by the program's
  `except TypeError:`.
* **Reach two slots deep as well.** The read below such a slot is answered (ADR 0251) because it dispatches the
  inner read under the outer tag; these two callers would have to dispatch a dispatch, and their merge is a `phi`
  over verdicts, not over pairs. It filed as `Gap R.210` with the recursive `builtSlotPair` named as the fix,
  rather than being attempted in the same commit as the one-deep shape.

## References

`pkg/lang/heapargs.go` (`builtSlotPair`, `membershipOfBuiltSlotHaystack`, `iterLenUnderTag`,
`notIterableSentence`, `notIterableLoopSentence`, `inStrNeedleSentence`), `pkg/lang/codegen.go`
(`heapRuntimeIR`'s `rt_iter_len`/`rt_iter_elem`/`rt_iter_tag`, the `in`/`not in` hook in `value`, `ForStmt`'s
`iterKind == "tagged"` arm), `pkg/lang/opt.go` (`heapArgKind`),
`pkg/lang/runtime_slot_read_test.go` (`TestRunTimeBuiltSlotMembershipAndLoopAnswerOnBothLegs`,
`…TrapLikeCPython`, `…AsksTheObjectInTheModule`), `integration/runtime_slot_read_test.go`,
`integration/programs/runtime_slot_haystack.gy`, `integration/conformance_cases.go`,
roadmap `Gap R.95` (closed), `Gap R.210`, `Gap R.209`, `Gap R.82`, `Gap R.83`, `L11.1`,
docs/roadmap-details.md#gap-r-95, ADRs 0251, 0241, 0246, 0187, 0232, 0185, 0225, 0228, 0211, 0271, 0205, 0173,
0192, 0322.
