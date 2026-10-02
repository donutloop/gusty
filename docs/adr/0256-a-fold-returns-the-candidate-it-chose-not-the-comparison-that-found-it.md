# ADR 0256 — a fold returns the candidate it chose, not the comparison that found it

Date: 2026-10-02 · Status: Accepted · Roadmap: L11.6 (numeric truth), Gap R.73 / Gap R.104 (closed), Gap R.107–R.110 (filed), ADR 0110 (the scalar fold), ADR 0186 (the oracle), ADR 0221 (kind-preserving builtins), ADR 0233 (the boxed float), ADR 0248 (text order is strcmp's), ADR 0253 (the double domain), ADR 0254 (one predicate answers a value's kind), ADR 0166 (a broken module is our bug)

## Context

Gap R.104 was filed from one asymmetry: `print(min(1.0, 2), max(1, 2.5))` printed `1.0 2.5` in CPython and on `--aot`, while `--interp` raised `min/max expects 1 argument`. Measuring the rest of the family on the way to the fix found that the asymmetry was the smallest of three answers to the same question.

| program | CPython | `--interp`, before | `--aot`, before |
|---|---|---|---|
| `min(1, 5)` | `1` | `min/max expects 1 argument` | `min expects one argument` |
| `min(1.0, 2)` | `1.0` | the same refusal | `1.0` |
| `min(2.5, 1)` | `1` | the same refusal | **`1.0`** |
| `min(1, 5)` through `xs = [3, 1, 2]` / `min(xs)` | `1` | `1` | refusal |
| `min(["b", "a"])` | `a` | `a` | the intern index `0` |
| `min([1, "a"])` | `TypeError` | `TypeError` | the intern index `0`, exit 0 |

The compiled backend had one path for a single container and one float-domain path for several values. The float path promoted every candidate to `double` and selected a `double`. That is right for `max(1.0, 2.5)` and wrong for `min(2.5, 1)`: Python returns the winning element, so the answer's type is the winner's own type. The integer path existed for the old list fold but rejected `len(args) != 1`, and text fell through to an `icmp` over `@str_tab` indices, because an interned text is also an `i32`. Meanwhile the interpreter had a hard arity rule inherited from the list fold.

The row claimed the compiled leg already answered. It answered the one spelling that happened to have a float on the left of the first comparison; Gap R.104's written premise was half true, and both halves needed measuring. This cycle pays the feature rather than the recorded premise.

## Decision

**A fold's answer is the candidate. The comparison is evidence, not the value.** Interpreter and codegen share that rule:

- The interpreter accepts `min(a, b, ...)`, `max(a, b, ...)` and keeps the original candidate handles. Its shared `compareOrder(a, b, op)` is the comparator `sortElems` already used, with the operator threaded through it because the reference implementation puts the failing operator in the message: `min` reaches `'<'`, `max` reaches `'>'`. `compareElems` is now its `<` specialization, so sorting and folding cannot invent two different orders.
- The compiled backend classifies candidates before lowering. All-number candidates use the domain the candidates actually have: the fold when every value is compile-time visible, `icmp`/`select` over `i32` for ints, `fcmp`/`select` over `double` for floats. An int winner of an int/float fold returns as `i32` and is printed as an int. All-text candidates go to `rt_str_order` (ADR 0248), not to an integer comparison of interned positions.
- A source-visible comparison that CPython cannot make is emitted as its TypeError, not refused. Text, `None`, and a container each have a distinct word; the fold sees the first pair whose incumbent kind and candidate kind cannot meet, and emits exactly `'<'` or `'>' not supported between instances of '<candidate>' and '<incumbent>'`, including the order Python's fold would have reached them. The exception is catchable and takes exit 3. `min(1, "a")` and `max("a", 1)` differ in their message for a reason, and the test pins which reason.
- Runtime int/double mixes and runtime containers refuse. The comparison could be made, but the chosen candidate's kind cannot cross out of the call without the tagged value word (L11.1). Returning a `double` anyway is not the half-paid version of the feature — it is the wrong answer `min(2.5, 1)` was already producing. The refusal names the winner's kind as the missing word.

The call's printing kind comes from the same candidate rule. `isFloat` for `min`/`max` asks the winner, not whether any operand is a float. A text fold's returned `@str_tab` index is recorded as text for printing and binding. `None`-only candidates print `None`, not the untagged integer 0.

`programs/min_max_values.gy` joined the conformance corpus, and the three engines print the same nine lines.

## Consequences

- Gap R.104 closes: the interpreter no longer has an arity rule the compiled backend lacks. Gap R.73's source-visible varargs form closes; its runtime half is filed as Gaps R.107–R.110, rather than hiding behind a now-stale row.
- `pkg/lang/min_max_values_test.go` pins 25 parity rows × both engines, 13 exact traps × both engines, three catchable traps, the interpreter's `TypeError`/`ValueError` classes, four IR shapes, the filed-not-fixed boundary, and the bool divergence that belongs to L11.1's bool step rather than this feature.
- `integration/min_max_values_test.go` drives the same parity table through the shipped CLI against CPython, checks every trap exits 3 and not 1/2, checks each refusal exits 1 and names the missing half, and pins the runtime-boundary rows separately.
- The interpreter's old text/list/None ordering now raises where it compared raw heap handles. That is the same comparator change `sortElems` shares, and the suite is green on the rule rather than on the old handle order.
- Four new gaps are measured and owned: **R.107** a runtime numeric container, **R.108** a runtime text container, **R.109** runtime int/double candidates, and **R.110** a function whose parameter-kind convention settled both candidates from the first call before the winner's kind could be a fact about the call.
- No CLI flag, JSON field or exit class changed. The machine path is the same structured exit contract: answers exit 0, traps exit 3, capability refusals exit 1, and the stable refusal substrings in `docs/operations.md` are what a script matches.

## Alternatives rejected

- **Close Gap R.104 by changing only the interpreter.** It was the row's literal wording, and it would have left `min(2.5, 1)` answering `1.0` on the leg that nominally already answered correctly. A feature whose definition of done is one program's stdout has not defined the behavior.
- **Promote all varargs numeric candidates to double.** This was the codegen's starting position, and its wrong answers were not exotic: an int among doubles loses its `int` printing, and a tie with an int first answers `1.0` for `1`. Kind preservation is the rule ADR 0221 already states for these builtins; the varargs path had never learned it.
- **Refuse text varargs rather than order them.** The interpreter already ordered texts by content and the runtime has had `rt_str_order` since ADR 0248. Refusing would have traded a correct answer for an engine split that this ADR exists to close.
- **Compare a text, None and container by their existing untagged `i32` words and emit no refusal.** They are all `i32`, but a heap index and intern index are implementation addresses, not language order. Answering from them is how `min([1, "a"])` exited 0.
- **Invent a compile-time TypeError for runtime unknowns.** The compiler may emit a raise only when it can name the operands' kinds from source. A runtime candidate whose kind only the object can report is a tagged-value gap, not a TypeError the compiler has earned the right to print.
- **Change every call's result to a pair now.** The general `(payload, tag)` value word is L11.1. A scalar fold can get far with the kinds the source already names; pretending the tag arrived early would make the feature depend on the unfinished work it is meant to precede.
