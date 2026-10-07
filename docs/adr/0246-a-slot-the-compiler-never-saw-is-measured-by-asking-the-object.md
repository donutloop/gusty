# ADR 0246: a slot the compiler never saw is measured by asking the object

Status: accepted. Roadmap L11.1 (the tagged value word — this pays the run-time-built half of one clause:
`len(...)` of a slot of a container the program built rather than spelled). Related: ADR 0187 (payload and
tag are one write), ADR 0241 (the tagged element read, and the compile-time promise this ADR replaces for
one shape), ADR 0189/0210 (the KeyError/IndexError traps the read reuses), ADR 0192 (a name kept as a
compile-time list has no slot to load), ADR 0245 (the same door on the comprehension side).

## The measured starting point

`xs = [[1, 2]]` then `xs.append([9])` takes a name out of ADR 0241's provable set: the literal the name
was bound to no longer describes the object's slots, so every read that asked for a tag was refused. The
refusals were honest and they were also over-broad — the object knows what its own slots hold, because
every writer in this backend writes payload and tag together (ADR 0187). Measured before, on `--aot`,
with the interpreter and CPython agreeing throughout:

| program | CPython | compiled, before |
|---|---|---|
| `xs = [[1, 2]]; xs.append([9]); print(len(xs[0]))` | `2` | refused: *"len cannot reach into xs's slots…"* |
| `xs = []; xs.append([7, 8]); xs.append([9]); print(len(xs[0]), len(xs[1]))` | `2 1` | refused |
| `xs = []; xs.append("abc"); print(len(xs[0]))` | `3` | refused |
| `d = {}; d["a"] = [1, 2, 3]; print(len(d["a"]))` | `3` | refused |
| `xs = []; xs.append(5); print(len(xs[0]))` | `TypeError: object of type 'int' has no len()` | refused (compile time) |

Two of these had been sitting in the unit suite as *required refusals*
(`TestContainerSlotReadRefusesWhatItCannotProve`): the append row and, once that was lifted, the rebinding
and item-assignment rows too. All three now answer CPython's answer.

## Decision

1. **Ask the object.** `runtimeSlotPair` reads `base[key]` as a `(payload, tag)` pair out of a container
   the program holds at run time — the handle from the variable's own alloca, the slot through
   `containerSlotRead`, which already normalises negative indices, bounds-checks, and raises `KeyError`
   for a missing dict key. Nothing new is invented here; the door is the one `for`, `in` and `print`
   already walk. What was missing was somebody asking on behalf of a name with no literal behind it.
2. **The tag goes to the check, not just to the answer.** `lenOfTaggedSlot` dispatches on the tag at run
   time: text is measured in characters (`rt_str_len`), a list/dict/set in its own entries
   (`rt_heap_len`, which reads the object's record rather than the builder's claim), and an int, float,
   bool or `None` slot raises what CPython raises, per kind — `object of type 'int' has no len()`,
   `'float'`, `'bool'`, `'NoneType'`. A slot whose tag is none of these is told it has no length rather
   than measured as if it were a container, which would read a word that means nothing.
3. **Only where a real object exists.** The door opens for a name with an alloca and a container
   registration. A name the escape analysis kept as a compile-time list has no slot to load and still
   refuses by naming itself (ADR 0192) — emitting the load is the module `llc` rejects, which the
   exit-code contract charges the compiler with.
4. **Arithmetic on such a slot still refuses.** `xs[0][0] + 1` on a run-time-built container needs a
   payload *plus* its tag in a numeric context; that is L11.1's remaining clause and Gap R.79's, and the
   refusal stands, unchanged, in the same table.

## Agentic rationale

The refusal was good diagnostics for a compiler that could not do the thing; it became bad diagnostics
once the thing was doable, because it told the programmer to rewrite a program that was already correct
("the name was rebound, mutated, or handed to code this pass cannot see"). The machine path is unchanged
in shape — same `--json` diagnostics, same exit 1 for what still refuses, exit 2 reserved for a bad
module — and the new traps are ordinary exceptions with CPython's own text, so an agent sees the same
message it would get from `python3` instead of a compile-time lecture.

## Codegen / IR implications

```text
  %h   = load i32, i32* @_xs                                  ; the object the name holds now
  ; normalise + bounds-check the position inline (rt_list_len, a select for the negative form,
  ; IndexError via raiseTo on either bound) — the same statements an ordinary xs[i] emits:
  %p   = call i32 @rt_get_elem(i32 %h, i32 %pos)
  %t   = call i32 @rt_tag_of(i32 %h, i32 %pos)
  ; lenOfTaggedSlot: a chain of tag checks, each unmappable kind raising its own TypeError,
  ; then a two-way join on text-vs-container:
  %c1  = call i32 @rt_str_len(i32 %p)      ; tag == 4
  %c2  = call i32 @rt_heap_len(i32 %p)     ; tag in {5, 6, 7}
  %n   = phi i32 [ %c1, %len.str ], [ %c2, %len.cont ]
```

No new runtime helper: `rt_str_len`, `rt_heap_len`, `rt_get_elem`, `rt_tag_of`, `rt_dict_get_tagged` and
`rt_dict_value_tag` all existed. The new code is the dispatch, and the raises go through `raiseTo`, the
same path `checkIndexRead` uses, so an uncaught one prints the traceback and a caught one unwinds to the
handler.

## Alternatives rejected

- **Leave it refused.** The refusal names a real missing promise for *other* shapes (three rows still
  refuse, measured rather than assumed), but for this one the promise is not missing — it moved from the
  compiler's notebook into the object's tag array.
- **Measure every slot as a container.** `rt_heap_len` on an int slot answers with whatever word 0 of the
  heap slot happens to hold — the exact class of silent wrong answer this project keeps paying for. The
  tag check is the feature; the length is the payload.
- **Guess the kind from the first literal appended.** Then the answer depends on the order the program
  happened to append in, and a loop that appends two kinds picks one.
- **Do the numeric read (`xs[0][0] + 1`) in the same commit.** It needs a tagged operand in arithmetic,
  i.e. the value word still owed, and shipping it half-done would mean either a refusal in a table that
  claims an answer or a payload read alone — the wrong answer with a green tick.

## Consequences

- `pkg/lang/container_slot_read_test.go` — four rows moved from
  `TestContainerSlotReadRefusesWhatItCannotProve` to `TestContainerSlotReadsAnswerOnBothLegs`
  (`len(xs[0])` after an `append`, after a rebinding, after an item assignment, and after handing the name
  to a function the pass cannot see), plus eight new rows for containers built at run time, one per slot
  kind and a dict slot holding a dict. The refusal table keeps the shapes that still refuse — arithmetic on
  a slot, a text/None element used as a number, a loop variable used as a number — and says which rows left
  and where they went, so nobody reads the lift as the whole table having gone. The moved
  "handed to a function" row returns rather than prints: printing a container *parameter* is a separate
  measured defect (Gap R.80) and a row is not allowed to pin what the compiler does wrong just because the
  program is convenient.
- `integration/comprehension_brace_element_test.go` and `integration/container_element_test.go` — two
  refusal rows of the same kind (`len(d[0])` of a comprehension whose element is a set, and one whose
  element is a dict) promoted to the CPython-checked parity table, and the stale `mutated_container_slots`
  refusal deleted; each left a note saying it moved rather than vanishing.
- `integration/container_slot_read_test.go` — the same programs against CPython on both engines, and four
  trap rows (`int`, `float`, `None`, dict-slot) asserted against what the oracle really raises.
- Roadmap: L11.1's queue row narrows its run-time-built clause to the numeric and comparison reads;
  Gap R.80 (printing a container parameter) and Gap R.81 (a set accepts an unhashable member) measured
  while moving these rows and opened as their own rows rather than absorbed into this one.
- `docs/language.md`: the container-read section answers the run-time-built `len`, and the paragraph
  naming what still reports is narrowed to the reads that still need the value word.
