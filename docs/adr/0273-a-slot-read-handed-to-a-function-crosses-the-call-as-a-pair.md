# 0273. A slot read handed to a function crosses the call as a pair, in both directions

## Status

Accepted. Ships as one pure AST scan (`pkg/lang/paircall.go`, `pairCallSpecs`) consulted at both ends of the
call — the `define` that takes the extra `i32` and the `call` that passes it — plus four emitting doors
(`pairArgWords`, `pairCallPair`/`pairCallPrint`, `bindPairCallResult`, `pairReturnWords`) that all ask the
arithmetic helper ADR 0265 built. No new runtime helper, no new instruction, no change to `ret`'s type.

Closes roadmap **Gap R.139**: `def twice(v): return v * 2` with `xs = []` / `xs.append([7, 8])` /
`print(twice(xs[0][0]))` is CPython's `14` and the interpreted leg's `14`; the compiled leg spent exit 1 on
`index cannot reach into xs's slots`.

Files what the same sweep found and this commit does not fix: **Gap R.154** (a pair-carrying parameter beside
an ordinary one — the shared arithmetic door has no kind to name for a parameter the caller never tagged).
Extends **Gap R.146**'s list with the answer-side neighbours measured here: the answer read as one number,
the answer as a container element, the answer handed to a second function, and a call whose argument is
itself such an answer.

## Context

Six doors opened the same missing word from six directions. A slot of a container the program *built* has a
kind only the object can tell (ADR 0241, ADR 0251), so its arithmetic answers a `(payload, tag)` pair
(ADR 0265), and that pair then had to be carried to every place a value is asked about: the print dispatch
(ADR 0265), a binding (ADR 0267), every position that wants one static number (ADR 0268), the operators that
choose an operand (ADR 0269), the operand's sign (ADR 0266, ADR 0271). A call was the position left:

| program | CPython | before · interpreter | before · compiled |
|---|---|---|---|
| `print(twice(xs[0][0]))` | `14` | ✅ | exit 1, `index cannot reach into xs's slots` |
| `print(twice(v=xs[0][1]))` | `16` | ✅ | exit 1, same sentence |
| `print(twice(xs[0][0] + 1))` | `16` | ✅ | exit 1, same sentence |
| `n = xs[0][0] * 2` / `print(twice(n))` | `28` | ✅ | exit 1, `… the tag has nowhere to go` |
| `n = twice(xs[0][0])` / `print(n)` | `14` | ✅ | exit 1, same sentence |
| `def show(v): print(v)` / `show(xs[0][0])` | `7` | ✅ | exit 1, same sentence |
| `d = {}` / `d["k"] = 40` / `print(twice(d["k"]))` | `80` | ✅ | `80` (the dict's own literal still described the slot) |

The asymmetry is what made this row the last of the family rather than the easiest. In every earlier door
the expression and the consumer live in the *same* function, so one pass can ask the pair and use it. Here
the value with two words is in the caller and the code that consumes it is in the callee — which has no view
of `xs`, no `containerLits` entry, and no way to know that its parameter ever had a kind at all.

Two properties were non-negotiable, both from the exit-code contract (ADR 0166):

- a program that compiled before must compile the same way after, emitting the same `define` — the pair is
  added, never substituted, so an existing `call`/`define` pair cannot be re-typed under anyone;
- no refusal may turn into a wrong number, so an argument whose kind neither road can name stays a refusal.

## Decision

**Two words, one each way.**

- *In.* A pair-carrying parameter is declared as a second `i32` beside its payload (`define i32 @gy_twice(i32
  %p0, i32 %q0)`), and bound inside the callee through the one door every tagged value uses —
  `bindTaggedVar`, with the arithmetic origin recorded (`numericPairVar`) so the body asks the same door the
  caller asked. The callee's own arithmetic is untouched: `v * 2` is ADR 0265's `@rt_num_arith` call, with two
  more operands filled in.
- *Out.* The body's answer already *is* a pair; what it lacked was a way to bring the tag along. The callee
  stores that tag beside its own return in one `internal global i32` named off the function's own symbol
  (`@gy_twice.anst`), and returns the payload in the word it always returned. A pair-aware caller loads the
  word immediately after the `call`, so the kind read is the one the objects chose. `ret` keeps its type —
  ADR 0196 / ADR 0254's return convention is not touched, and no function's ABI changes unless the scan
  opened it.

**Three return roads, three stores.** A body that can fall off the end, and a body whose arithmetic raises,
give no value — and a tag word left holding the *previous* answer's kind would be read by the next caller as
though it were this one's. Both roads store the None tag (`store i32 3, i32* @gy_twice.anst`) before they
leave, which is why the fall-off-the-end case prints `None` and not a number-shaped lie.

**The scan is pure, and it is the only thing that decides.** `pairCallSpecs(prog)` is a function of the AST:
it walks the whole program once for calls (`recordCall`, positional and keyword, defaults counted), for
bindings (`s.bound`, iterated a bounded four rounds so `n = xs[0][0] * 2` is the same missing word one
operator further out), and for containers the program built while it ran (`pairMutators`, empty-literal
targets, `xs[i] = v`). The `define` and every `call` read the *same* table, emitted before any IR exists —
which is the only way a function written below its first call can agree with that call about arity. The
emitting side never widens the decision; `pairSpecFor` can only narrow it (a float-returning or string-
returning body, a decorated or nested or yielding function keeps its convention).

**The gate is what makes landing this safe.** A parameter is opened only where all four hold:

1. some call site *needs* the pair — the argument is, or reaches through, a container the literal no longer
   describes (`exprNeedsWord`), which is `indexKindIsRuntimeObject`'s own set and therefore exactly the set of
   reads the ordinary road refuses;
2. every call site of that parameter can *supply* one from its own spelling (`argsNumberish`) — one argument
   this pass cannot name (a call, a text, a container, a name with no evidence behind it) closes it;
3. the body reads the parameter only where a pair door answers (`pairUsesServed`, whose operand rule allows a
   number literal and nothing else beside the pair — see Gap R.154);
4. the body's answer can carry a tag (`pairBodyAnswers`) and the function does not call itself — a recursion
   would have the inner frame write the tag word the outer caller has not read yet.

Anything that fails keeps its old convention *and its old refusal*, character for character: measured below.

**The caller asks the same door the print position asks.** `pairArgWords` tries `taggedArithPair` first, then
`arithOperandPair`, so an argument that is itself arithmetic over a slot (`twice(xs[0][0] + 1)`) gets the kind
`print(xs[0][0] + 1)` gets. One door, four positions (print, binding, number position, argument) — which is
the standing rule of this family since ADR 0267.

## Measurement

Before is `3c0f59a` built into a second binary from a `git worktree`; after is this commit. Both legs are the
CLI's forced engines (`--interp`, `--aot`), and the reference is `python3` on the same file. Every row is a
`/tmp` file with no harness between it and the three engines.

| program | reference | before · `--aot` | after · both engines |
|---|---|---|---|
| `print(twice(xs[0][0]))` | `14` | exit 1, `index cannot reach into` | `14` |
| `print(twice(v=xs[0][1]))` | `16` | exit 1, same | `16` |
| `print(twice(xs[1][0]))` (a float slot) | `3.0` | exit 1, same | `3.0` |
| `print(twice(xs[0][0] + 1))` | `16` | exit 1, same | `16` |
| `n = xs[0][0] * 2` / `print(twice(n))` | `28` | exit 1, `the tag has nowhere to go` | `28` |
| `n = twice(xs[0][0])` / `print(n)` | `14` | exit 1, `index cannot reach into` | `14` |
| two answers, one callee, int and float | `14 3.0` | exit 1, same | `14 3.0` |
| `def show(v): print(v)` / `show(xs[0][0])` | `7` | exit 1, same | `7` |
| both arms of a condition over the parameter | `36`, `7` | exit 1, same | the reference's bytes |
| a callee that falls off the end | `None` | exit 1, same | `None` |
| a bool slot (Python's bool is a number) | `2` | exit 1, same | `2` |
| three levels, `twice(xs[0][0][1])` | `16` | exit 1, same | `16` |
| a loop-built container, `twice(xs[1][0])` | `14` | exit 1, same | `14` |
| a dict slot by key | `80` | `80` | `80` (unchanged — the literal still described the slot) |
| a None slot under the callee's `*`, caught | `caught` | exit 1, refused | `caught`, exit 0 |
| a None slot under the callee's `*`, uncaught | `TypeError: unsupported operand type(s) for *: 'NoneType' and 'int'` | exit 1, refused | that sentence, exit 3, both legs |
| `print(add(xs[0][0], xs[0][1]))` | `15` | exit 1, same | `15` |
| the `function_calls` benchmark | `11103` | `11103` | `11103`, and its module carries **no** `.anst`, no `%q0` |

**What stayed refused, refused the same words.** Each of these printed the same sentence before this commit
as after it — the point is not that they refuse but that landing a door moved none of them:

| program | reference | refusal (unchanged before and after) |
|---|---|---|
| `print(twice(twice(xs[0][0])))` | `28` | `index cannot reach into xs's slots` — an argument that is itself a pair-returning call cannot supply the pair |
| `print(twice(xs[0][0]) + 1)` | `15` | `twice hands back the (payload, tag) pair…` — the answer read as one number is the payload alone (Gap R.146) |
| `print([twice(xs[0][0])])` | `[14]` | same sentence, the container element's one word (Gap R.146) |
| `print(show(twice(xs[0][0])))` | `14` | same sentence, the second function's one word (Gap R.146) |
| `def shift(a, b=100): return a + b` | `108` | `index cannot reach into xs's slots` — **Gap R.154**, filed by this sweep |
| `print(twice("hi"))` at one call site | `hihi` | `operator "*" on a string (v) is not supported…` — Gap R.82's repetition, closing the parameter for the whole function |

The emitted module, asserted as IR rows rather than hoped (`TestAPairCrossingACallWritesBothWords`):

```
@gy_twice.anst = internal global i32 0
define i32 @gy_twice(i32 %p0, i32 %q0) {
  store i32 %q0, i32* %_v_tag         ; the argument's kind, bound through ADR 0187's door
  store i32 %t12, i32* @gy_twice.anst ; the answer's kind, chosen by @rt_num_arith
  store i32 3, i32* @gy_twice.anst    ; the fall-off-the-end road says None
  store i32 3, i32* @gy_twice.anst    ; the unwinding road says it too
```

## Consequences

- `programs/probe_slot_read_handed_to_a_function.gy` leaves the debt ledger and joins `conformanceStandalone`
  with eight lines (`14 6 3.0 16 16 14 15 9`) that all three engines print byte-identically;
  `integration/conformance-matrix.json` records the row as `match`, `oracle_declared: match`.
- `pkg/lang/pair_call_test.go` (unit: the answer table on both engines, the IR-shape table, the gate asked
  directly as a function, the trap table) and `integration/pair_call_test.go` (the same shapes through the
  CLI against `python3`, the traps with their exit class pinned and exit 2 forbidden, the refusals) are the
  feature's coverage; `TestThePairSpecGateIsAskedDirectly` is the machine path — a widening or narrowing of
  the door fails a decision row rather than quietly changing which programs compile.
- `integration/numeric_slot_arith_test.go`'s "the answer handed to a function" row moved from the refusal
  table to the two rows that still refuse, and its doc comment now names the call as an answered position.
- `docs/language.md` § Tagged values states the rule for programs ("a slot of a container you built at run
  time is a value, and a value can be printed, bound, compared and passed"), and names the positions that
  still decline.
- The GC rule holds: the payload may be a float box, so every word that holds a pair payload is registered as
  a root (`gcReg`) — the same rule ADR 0181 applies to every handle-carrying store, plus the collection run
  between a call and its print in the integration table.
- **Gap R.154** is the deliberate non-fix. Teaching `slotArithmeticIsProven` to answer for an ordinary
  parameter means the shared door would assume a kind for a value it cannot see, in every program in the
  corpus, not just this one. That is a measurement of its own, and the scan's body gate keeps such a
  function entirely on the convention it already had.

## Agentic rationale

- The gate is asked *as a function* (`pairCallSpecs`), so an agent — or a future cycle — can read "what will
  this program's call boundary carry" without compiling it or parsing IR text; `--emit-llvm` shows the second
  parameter and the `.anst` word for the programs that take the road, and shows neither for the ones that do
  not (`TestAPairCrossingACallWritesBothWords` asserts absence as well as presence).
- The rule is discoverable rather than folk knowledge: `gustyc --lang` and `docs/language.md` say a built
  container's slot reaches a function, and the refusal messages name which half is missing (`… cannot reach
  into`, `… hands back the (payload, tag) pair … this position keeps one word`), which is what an agent needs
  to decide whether to rewrite the program or file the row.
- Exit codes unchanged: refusals stay exit 1, traps stay exit 3 and catchable, and the new tables carry the
  exit-2-is-a-compiler-bug fatal in every leg (ADR 0166).

## Alternatives rejected

- **Boxing the argument (`rt_box_*`) and passing one word.** It re-types the parameter for every existing
  program, needs an unboxing road at each use, and duplicates a value representation the language already
  has twice (the tagged variable and the heap object). The `(payload, tag)` pair is what the last six ADRs
  converged on; a seventh representation is the failure this family keeps rejecting.
- **A shadow tag word per parameter (a stack slot in the callee).** Works for one call at a time and breaks on
  the recursion this commit excludes: the callee's own call would overwrite the frame's word before the outer
  caller read it. A per-*function* global plus the recursion exclusion is the smaller claim.
- **Deciding the arity at the first `call` seen, and matching the `define` to it.** Measured, not hypothetical:
  it produced exactly the module-verifier `mismatched type` failure ADR 0166 classes as exit 2, on a program
  whose `def` happened to sit below its call — a compiler bug spent on a file layout. Hence the pure scan
  (`TestThePairScanIsAskedOfTheProgramNotTheEmittingOrder`).
- **Marking a parameter whenever its body mentions it in arithmetic.** This is the version that broke the
  `function_calls` benchmark the day it was written: `(a * 31 + b * 17) % 100003` uses `%`, a door the pair
  does not own, so the tag was passed into a body with nothing to read it with, and the module stopped
  compiling. Hence gate rule 3, and the benchmark row in the table above.
- **Refusing every call with a built-container argument until the whole family is done.** Honest, and already
  the status quo — six doors' worth of programs print `14` because the alternative was taken instead, and the
  ladder rule (refusals become answers, never answers become refusals) is what lets the last door land while
  the neighbours stay filed.
