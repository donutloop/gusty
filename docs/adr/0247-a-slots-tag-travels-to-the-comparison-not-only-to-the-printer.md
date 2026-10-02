# ADR 0247: a slot's tag travels to the comparison, not only to the printer

Status: accepted. Roadmap **L11.1** (the tagged value word — this pays the "compared with text"
clause of the element read) and **Gap R.79** (comparing a slot of a run-time-built mixed list with
text). Related: ADR 0187 (payload and tag are one pair, written and read together), ADR 0232 (a
container word means its payload *and* its tag; the lookup reads the tag, and `rt_mixed_eq` was
this ADR's subject until it was retired), ADR 0238 (a float slot is the handle of a box), ADR 0241
(the tagged element read, and the compile-time promise), ADR 0246 (ask the object, not the
compiler's notebook), ADR 0166 (a refusal, never an invalid module), ADR 0211 (exit 1 means "your
program is wrong", so it must not be issued for a program CPython runs).

## The measured starting point

`print(out[1])` asked the slot and rendered `a`. `out[1] == "a"` refused. One read, two doors, and
the ordinary question was the one that could not be asked. Measured on `--aot` against the parent
binary, with the interpreter and CPython agreeing throughout:

| program | CPython | compiled, before |
|---|---|---|
| `xs = [1, "a"]; print(1 if xs[1] == "a" else 0)` | `1` | refused: *"this context needs a single static kind"* |
| `xs = [1, "a"]; print(1 if xs[0] == xs[1] else 0)` | `0` | refused |
| `xs = [1, "a"]; i = 1; print(1 if xs[i] == "a" else 0)` | `1` | refused |
| `xs = [None, 1]; print(1 if xs[0] == None else 0)` | `1` | refused |
| `xs = []; xs.append([1, 2]); print(1 if xs[0] == [1, 2] else 0)` | `1` | refused |
| `d = {}; d["k"] = [1, 2]; print(1 if d["k"] == [1, 2] else 0)` | `1` | refused: *"comparing a container with \*lang.Index"* |
| Gap R.79's comprehension slot against text | `1` | refused |
| `xs = [1.5, "a"]; y = xs[0]; print(1 if y == 1.5 else 0)` | `1` | **answered `0`** |
| `xs = [1, "a"]; print(1 if xs[0] == pick(1) else 0)`, `pick` returning `"z"` then `7` | `0` | **answered `1`** |
| `xs = [1, "a"]; print(1 if xs[1] > "a" else 0)` | `0` | refused (still refused — new row Gap R.82) |

The last three are the ones that decide the shape of this change. A refusal is a claim about the
compiler and it can go stale (ADR 0246 learned that the hard way); an *answer* that arrives by
comparing two words is worse, because it is a wrong answer with a green tick next to it — and both
of those wrong answers were in the shipped build.

## Decision

1. **One equality answers a pair.** `rt_payload_eq(payload, tag, payload, tag)` — the function
   `rt_slot_eq` already calls to walk two containers — is now also the answer to a source-level
   `==` between two tagged values. Within a tag it is payload equality (which is what keeps interned
   text and the number it is indexed by apart, ADR 0232); across the two numeric tags it is `fcmp
   oeq`, so `1 == 1.0`; two container slots answer by content through `rt_container_eq`.

   `rt_mixed_eq` is **deleted**. It compared the tags and then the words, which is sound for text
   and numbers and unsound for exactly one thing the tag table has grown since: a float slot holds a
   *box handle*, so two slots holding 1.5 held two handles, and `y == 1.5` answered false one line
   after `print(y)` printed `1.5`. Two answers to "are these the same value?" is the defect; keeping
   the cheaper one around is how the next caller finds it again.

2. **The pair on both sides.** `comparePair` turns either side of an equality into the pair: a
   variable carrying a tag from a mixed read (ADR 0185/0187, unchanged), or a **slot read** — an
   element of a container whose slots describe themselves (`mixedLists`/`mixedDicts`), or a slot of
   a run-time list/dict reached through an index or key the program computes (`runtimeSlotPair`,
   ADR 0246's door). Ordering comparisons (`<`, `>`, `<=`, `>=`) are deliberately untouched: they
   need the same treatment on the *relational* side, and that is Gap R.82.

3. **The door opens only where the ordinary path runs out.** `isTaggedSlotRead` excludes the read
   ADR 0241 already licenses — a constant position of a container the literal still describes, which
   has its own checked read and ADR 0243's numeric fold. Existing programs keep the lowering they
   had; new ones get an answer instead of a refusal.

4. **A door that declines must leave the caller's block intact.** The slot read emits its own bounds
   check, whose `IndexError` branch is a terminator. Emitting it into the caller's buffer and then
   falling through to the ordinary path would leave instructions after a terminator, and `llc` would
   report the compiler's mistake as the program's (exit 2, ADR 0166). So the whole door is built in a
   scratch buffer and committed only once both sides have answered.

5. **Where a tag cannot be had, refuse rather than guess.** If one side's kind only the object knows
   and the other side's kind cannot be proven at all — the `pick()` above, text on one path and a
   number on another — the comparison is refused by naming the missing kind. The alternative was the
   shipped behaviour: compare the words, and answer `1` because the interned index of `"z"` happened
   to equal the number in the slot. Same rule ADR 0232 settled for membership tests.

## Agentic rationale

Nothing about the machine interface moved: the same `--json` diagnostics, exit 1 for what still
refuses, exit 2 reserved for a module `llc` rejects. What changed is that a whole family of programs
stopped being *refused* — an agent that previously had to rewrite `out[1] == "a"` into `print` plus a
string comparison now gets CPython's verdict — and one program that previously got a confident wrong
answer now gets a refusal that names the missing half. A refusal is only good diagnostics while the
limitation is real; this cycle re-measured the refusal table and moved the rows whose limitation had
moved, which is the same audit ADR 0246 ran on `len`.

## Codegen / IR implications

```text
  %h  = load i32, i32* @_xs                                  ; the object the name holds now
  %ix = …                                                    ; normalised + bounds-checked (ADR 0210)
  %p  = call i32 @rt_get_elem(i32 %h, i32 %ix)               ; the payload
  %t  = call i32 @rt_tag_of(i32 %h, i32 %ix)                 ; …with the tag the builder wrote
  %o  = call i32 @rt_str_intern(i8* @.str)                   ; the other side, as its own pair
  %q  = call i32 @rt_payload_eq(i32 %p, i32 %t, i32 %o, i32 4)
  %c  = icmp ne i32 %q, 0
  %r  = zext i1 %c to i32
```

No new runtime helper: `rt_payload_eq`, `rt_get_elem`, `rt_tag_of`, `rt_dict_get_tagged` and
`rt_dict_value_tag` all existed, and one runtime block (the heap's) now pulls in the float block for
the unboxing, exactly as the container printers already did. One helper left the module:
`rt_mixed_eq`. `gustyc --lang` and the schema are unchanged — this cycle moved a capability from
"refused" to "answered", not a name.

## Consequences

- `pkg/lang/slot_equality_test.go` — 17 parity rows × both engines against CPython (the payload
  collision between `0` and `"zero"`, `None` against `0` and against `"None"`, the float-box row,
  the runtime-index and negative-runtime-index rows, container and set slots, the Gap R.79
  comprehension program), one IR-shape row asserting the module asks `rt_payload_eq` and no longer
  contains `@rt_mixed_eq(`, and a two-row refusal table.
- `integration/slot_equality_test.go` — the same programs through the CLI on both engines against
  CPython, three trap rows (out-of-range position, missing key, and the ordering comparison the door
  does not open, each first checked against what the oracle really raises), and the unprovable-kind
  refusal row asserting exit 1 and the message's wording.
- `integration/comprehension_brace_element_test.go` — `comparing_a_slot_with_text` left the refusal
  table for the parity table; the row stays visible as a comment saying where it went.
- `pkg/lang/mixed_dict_set_test.go` — the iteration/comparison IR row now asks for `rt_payload_eq`
  and fails if `rt_mixed_eq` comes back.
- Roadmap: Gap R.79 → ✅ `DONE`; L11.1's queue row loses the "compared with text" clause; **Gap
  R.82** (ordering comparisons of a tagged slot read refuse) and **Gap R.83** (comparing a tagged
  slot with an expression whose kind cannot be proven refuses — the wrong answer is gone, the verdict
  is still owed) are filed as fresh rows from these measurements.
- `docs/language.md`: the container-read section answers equality, and the paragraph naming what
  still reports is narrowed to the numeric use and the nested run-time-built read.

## Alternatives rejected

- **Leave the comparison refused.** It was the honest answer while the pair was unreachable; the
  printer had already proved it reachable, and a refusal that survives its own obsolescence is how
  ADR 0246's `len` refusal looked.
- **Keep `rt_mixed_eq` and use it for the cheap cases.** Two equalities, agreeing on everything
  except the case that was already wrong, is a trap for the next caller. One question, one helper —
  ADR 0238's rule for comparisons, applied to the source-level operator.
- **Answer the unprovable-kind comparison by assuming a tag.** `pick(1)` returning `"z"` would be
  compared as a number, which is how the build answered `1` for a program CPython answers `0`. The
  right fix is a tagged return value from such a call (Gap R.83); the interim answer is a refusal.
- **Fold the ordering comparisons in.** `>` across two tags needs a relational operand built from the
  tag and CPython's own `TypeError` across kinds; landing it beside this change would have meant a
  second engine in one commit (Gap R.82 owns it).
- **Emit the read into the caller's buffer and undo on decline.** There is no undo for a terminator;
  the scratch buffer is the only version of "ask first" that keeps `llc` out of it.
