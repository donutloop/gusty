# 0226 — A container slot is a word: ask what fits before writing it

- Status: Accepted
- Date: cycle 174 (roadmap Gap R.40; ADR 0166 owns refusal-vs-invalid-module, ADR 0215 owns operand kinds, ADR 0221 owns numeric cross-kind equality, ADR 0187/0189 own element tags)
- Affects: `pkg/lang/codegen.go` (`emitList`, `valueText`, the comparison's operand-kind gate), `pkg/lang/heapargs.go` (`heapElemKind`), `docs/language.md`

## The measurement

Eighteen shapes through both engines, expectations from CPython. Six of them ended in **exit 2** —
`llc` rejecting a module the compiler had invented an operand for — and three more ended in an answer
nobody had checked:

```
print(1 if [1] == [1.0] else 0)   -> exit 2   @.lst2 = private global {i32, [1 x i32]} { i32 1, [1 x i32] [@env_store = internal global ...
print([1.5, 2])                   -> exit 2   %t1 = sitofp i32  to double
print(1 if 1.0 == [1] else 0)     -> exit 2   %t2 = sitofp i32 @.lst1 to double
xs = [1.5]; print(xs[0])          -> printed 1        (a float read back as a truncated int)
{1.5} == {1.6}                    -> printed 1        (CPython: 0 — both elements truncated to one word)
{"a": 1.5} == {"a": 1.6}          -> printed 1        (CPython: 0 — same)
```

The last three are the ones worth writing down: they were **green**. `{1.0} == {1.0}` printed 1 in the
compiled backend and looked like support; it was truncation giving the right answer by luck, and
`{1.5} == {1.6}` was the same code path saying True.

## The decision

**A container slot is an i32 word, and every emitter that writes one must ask what fits.** One
question, asked in one place:

- `heapElemKind` — the helper every container path already goes through — now refuses an element whose
  compiled representation is not a word: a float. The message names the element kind, the missing
  representation, and the roadmap item that owns it (L11.6), so the refusal is actionable rather than
  a wall.
- `emitList` validates its elements **before** touching the globals buffer. It used to write the
  opening `@.lstN = private global {i32, [N x i32]} { i32 N, [N x i32] [` and only then look; when an
  element failed, it returned an error and left the unterminated definition in the module. Everything
  downstream that caught and ignored that error shipped it — which is how `[1 x i32] [@env_store = ...`
  reached `llc`. Build the whole line, then write it once.
- `valueText` no longer swallows. It had the shape this repo keeps having to remove:
  `v, _ := g.value(b, e); return v` — an error thrown away and an empty string returned in its place,
  which the float path then formatted into `sitofp i32  to double`. The failure is recorded on the
  generator (the same channel ADR 0223 gave the method emitter) and `GenerateIR` refuses. An emission
  path with no error channel of its own reports into the generator; nothing that is about to be
  refused may also be executed.
- A comparison between a number and a container is **not a numeric question** (ADR 0215's gate, with
  ADR 0221's numeric pair as the deliberate exception). CPython answers `1.0 == [1]` with False without
  converting the list, and so does this backend now, by kind — rather than coercing a container global
  through `sitofp`.

## Alternatives rejected

- **Box every float so it fits a slot.** That is the L11.6 design (a heap float, tags that can describe
  it, printing and comparison for it), and doing it here would have bundled a numeric-model decision
  with an exit-2 cleanup. What this cycle guarantees is only that the answer is never garbage.
- **Truncate a float to its integer part in a slot, as the fold had quietly been doing.** Rejected with
  force: it produced True for `{1.5} == {1.6}`. A wrong answer that matches the oracle half the time is
  worse than a refusal, because the half that matches is what gets tested.
- **Answer `1.0 == [1]` by coercing both sides to double.** That is what emitted the rejected module; a
  container is not a number.
- **Leave `valueText` swallowing and fix the four call sites.** Rejected: the swallow is the defect. Any
  future caller re-introduces the same class, and the failure mode (an empty operand) is invisible until
  an external tool rejects the module.
- **Keep the two shapes that "worked" (`{1.0} == {1.0}`, `{"a": 1.0} == {"a": 1.0}`).** Rejected after
  measuring them rather than trusting the green row: they were True by truncation, and their neighbours
  `{1.5} == {1.6}` and `{"a": 1.5} == {"a": 1.6}` answered True.

## Checks

- `pkg/lang/container_element_test.go` — an artifact blacklist (`sitofp i32  to double`,
  `sitofp i32 @.lst`, `[1 x i32] [@`, `ret i32 @.`, `icmp eq i32 @.`) run over every shape in the
  family, refusal messages asserted to name the kind and the roadmap item, and the kind-gated
  equalities run through the JIT and compared to CPython's answers.
- `integration/container_element_test.go` — the answers on both engines; the refusals asserted to be
  class 1 with a message, never class 2 and never class 0; and the shapes the *interpreter* answers
  asserted on the interpreter alone, so the compiled hole stays visible.
- `integration/programs/kind_mismatch_equality.gy` — 17 lines, all three legs identical.

## Follow-ups

- **L11.6** owns the real fix: a float that can live in a container slot, which turns each of these
  refusals into an answer. Each refusal is asserted to fail the test if it ever starts answering, so
  the day L11.6 lands, this file says so.
- **R.37** owns the ordering case: `1.0 < [1]` is a TypeError in CPython; refusing at compile time is
  honest today and wrong the day runtime type errors exist.
