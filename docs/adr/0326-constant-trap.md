# ADR 0326: the constant fold raises what the reference raises

- **Status**: Accepted
- **Date**: 2026-07-06
- **Deciders**: Agent (cycle), Language designer (roadmap)
- **Agentic relevance**: `gustyc` reported **exit 1** — the class the exit-code contract reserves for *"your
  program is wrong"* — for programs that had not run a single instruction, and could not report a traceback
  for a trap it had refused to build. Where the fold had been widened instead of refusing, it **answered**:
  `float("x")` printed `0.0`, `float(None)` printed `0.0`, `ord("ab")` printed `97`. An agent reading the JSON
  payload could not tell a program with a bad index from a program with a syntax error, and a script reading
  stdout could not tell a wrong number from a right one.
- **Roadmap references**: L11.1 (the container/position reads), L11.3 (the conversion builtins), Gap R.37
  (this row, ⏳ `OPEN` → ✅ `DONE`), Gap I.2 (the `int on non-integer string` refusal, paid here), Gap R.90
  (the wording debt the constant road inherits rather than deepens), L12.12 (the bounded-vs-arbitrary `int`
  decision that keeps one row a refusal), and three rows this cycle opens or closes in the same commit:
  Gap R.213 (a `bool` asked to be a number — paid by this door), Gap R.214 (a subscript on a call result —
  measured, still a refusal), Gap R.215 (a text key into a dict literal — measured, still a refusal)
- **Supersedes / extends**: extends ADR 0211 (every built-in trap carries a class) to the one place a trap
  used to be decided before the program ran; continues ADR 0166 (exit-code contract) and ADR 0154
  (`--verify`/`llc` verifier as the exit-2 witness); reuses ADR 0197's trap-sentence table rather than adding
  a second one; the raise itself is ADR 0181's (`rt_raise_ptr` + the per-function unwind exit ADR 0315 built).

## Context

ADR 0211 gave every built-in trap a class, and ADR 0181 gave the raise a frame-closing unwind. Both describe
what happens **when a value is reached at run time**. There was a second place a trap got decided, and it had
never been visited: the **constant fold**. When the subscript's base was a literal — `[1,2]`, `"abc"`,
`{1:2}`, a comprehension's object, a function returning a literal — the compiler knew the element count
statically, and five roads asked it what to do when the position did not fit. All five answered the same way:

```
codegen: index of a literal list is out of range
codegen: index of a literal string is out of range
codegen: key not present in literal dict
```

That is a *refusal*, at `gustyc`'s exit 1 — the exit code the contract reserves for a program that is wrong.
But the comparison was never "refusal versus a correct answer"; it was "refusal versus **the reference**", and
the reference does not refuse. `print([][0])` prints a traceback and exits nonzero; a `try:` around it is
legal Python and the `except IndexError:` runs. The compiler had converted a *fact about the value at the
moment it is read* into a *verdict about the whole program*, delivered before the program had executed
anything — and in doing so it had taken away the `except`, which is not a cosmetic loss: wrapping the read is
how a person finds out which index was wrong.

The second half was worse, and is the half that made this row urgent rather than merely unlovely. The
conversion folds had been widened rather than refused, and widening a fold without a raise is how a wrong
answer gets shipped:

| program | what the fold answered | what the reference says |
|---------|------------------------|-------------------------|
| `print(float("x"))` | `0.0` | `ValueError: could not convert string to float: 'x'` |
| `print(float(" "))` | `0.0` | `ValueError: could not convert string to float: ' '` |
| `print(float(None))` | `0.0` | `TypeError: float() argument must be a string or a real number, not 'NoneType'` |
| `print(float([1, 2]))` | `0.0` | `TypeError: … not 'list'` |
| `print(float({"a": 1}))` | `0.0` | `TypeError: … not 'dict'` |
| `print(ord("ab"))` | `97` — the first byte | `TypeError: ord() expected a character, but string of length 2 found` |
| `print(ord(""))` | `0` | `TypeError: ord() expected a character, but string of length 0 found` |
| `print(chr(-1))`, `print(chr(1114112))` | a 1-element list of a nonsense code point | `ValueError: chr() arg not in range(0x110000)` |
| `print(int("abc"))` | (refused: `int on non-integer string`, Gap I.2) | `ValueError: invalid literal for int() with base 10: 'abc'` |

A silent `0.0` is the failure mode this repository counts as worse than a refusal, and here it was the normal
outcome of a normal program. Both halves — the refusal and the silent answer — are the same missing
capability: **the fold had no door to the raise.**

## Alternatives Considered

1. **Keep refusing, and make the message better.** Rejected: it keeps the exit code wrong (1 for a program
   the reference runs), it keeps the `except` unreachable, and it cannot be repaired by wording because the
   claim being made — "this program will not build" — is false. `[][0]` builds perfectly; it raises.
2. **Fold the trap into a compile-time error for the *whole* program, but at exit 3.** Rejected: the exit code
   would look right while the mechanism stays wrong. A program whose trap is inside a function nobody calls
   (`class C: def m(self): return [][0]` with no call) does not trap, and a compile-time verdict cannot know
   that; it would report the failure of a path the program never takes.
3. **Emit the raise inline and keep going.** Rejected on the first build: `g.raiseTo` ends the current basic
   block (it emits `br label %…unwind`), so the caller's next instruction landed after a terminator and the
   module failed the verifier — ADR 0166's exit 2, the class of bug this contract treats as a toolchain
   defect rather than a disagreement. The fold has to open a fresh block on the other side of the raise, which
   is exactly what `normalizeIndex` already does with its `ix.bad` / `ix.ok` pair.
4. **Reuse the dynamic road's sentences.** Accepted. The five constant roads and their dynamic counterparts
   now read the same table (`listIndexOutOfRange()`, `stringIndexOutOfRange()`, the `chr`/`ord`/conversion
   sentences), so one trap has one wording. Inventing `list index out of range` for the constant path would
   have created a *second* wording of the same error and made Gap R.90 harder to pay, not easier.
5. **Answer CPython's wordings while at it (Gap R.90).** Rejected as a bundle: those wordings are the record's
   too, and changing them changes what the record leg expects for the *dynamic* road, which is a different
   row with its own witnesses. The constant road adopts the table as it stands and leaves the rename to the
   row that owns it.
6. **Fold `int("99999999999999999999999999")` to whatever the arithmetic lands on.** Rejected: the reference
   has an unbounded `int`, this backend has a 64-bit slot, and a plausible wrong number is the one answer this
   language has agreed never to give. That row stays an honest refusal naming the word and naming L12.12 —
   refusing is correct here because *no* raise is correct: the number is fine, the slot is not.

## Decision

**A constant trap is emitted as a trap, and the runtime raises it.** The fold may compute a value only when
the reference would have produced a value; when the reference would have raised, the fold emits
`g.raiseTo(class, msg)` and the raise reaches the program's own `except` or the top-level report, with the
class the reference raises and the sentence from the shared table.

Concretely, in `pkg/lang/constant_trap.go`:

- `func (g *irGen) constantTrap(b *block, class, msg string, sp Span)` is the door: `g.raiseTo` for the
  class and message, then a **fresh** block returned to the caller, so a fold that raises and hands back a
  value never leaves the caller emitting into an ended block.
- A sentence table, one entry per trap the fold can reach — `listIndexOutOfRange()`,
  `stringIndexOutOfRange()`, `intLiteralSentence(text)`, `floatLiteralSentence(text)`,
  `ordSentence(nCodePoints)`, `chrSentence()`, `convertArgumentSentence(fn, kind)` — consulted by both the
  constant roads and the dynamic ones.
- `func (g *irGen) unconvertibleArg(e Expr) string` decides *what kind is being converted* the way the
  reference does — a literal container answers its own name (`list`, `dict`, `set`), `None` answers
  `NoneType`, and a name recorded as holding a container answers that — and returns `""` when the compiler
  does not know, in which case the road keeps its existing dynamic behaviour rather than guessing a class.
- `intFromText` / `floatFromText` implement the reference's grammar for a literal: surrounding whitespace
  stripped, an optional sign, underscores only *between* digits, no digits after a space, no second sign.
  Text that parses answers; text that does not answers `ValueError` with the reference's sentence and the
  original text quoted; text that parses but does not fit the word answers the L12.12 refusal.

The five constant read roads — literal list, literal string, literal dict, comprehension object, and the
method-body/call-result case the same fold reaches — all call `constantTrap`. The conversion folds (`int`,
`float`, `ord`, `chr`, and `print`'s `chr`-folding arm, which had no bound at all) call it too. `bool`
arguments answer rather than trap: `int(True)` is `1` and `float(True)` is `1.0`, which is what the reference
says and what the fold previously refused (Gap R.213, paid in the same door).

## Consequences

- **Positive**:
  - `print([][0])`, `print([1,2][5])`, `print([1,2][-3])`, `print("abc"[7])`, `print("abc"[-4])`,
    `print({1:2}[3])`, a position below a comprehension, and `return [][0]` inside a method all run and raise
    `IndexError`/`KeyError` at exit 3 with a traceback naming `C.m` where the frame is a method.
  - The conversion family agrees with the reference on all five shapes: `int("abc")`, `int("1 0")`,
    `int("0x10")`, `float("x")`, `float(" ")`, `float(None)`, `float([1,2])`, `float({"a":1})`,
    `float({1})`, `int([1])`, `ord("")`, `ord("ab")`, `chr(-1)`, `chr(1114112)`.
  - Five rows leave the drift ledger as **paid**: `print(int("abc"))`, `print(float("zzz"))`,
    `s = "abc"; print(s[9])`, and the two `try:/except ValueError:` versions of the first pair, which were
    recorded as "the compiled backend cannot build a program the engine ran" and "answered 0.0 where the
    record prints `handled`". The ledger goes 337 → 332.
  - 35 new record entries through `tools/recmerge` (6095 → 6130), every one of them a source the recorder
    cross-checked against the compiled leg.
  - `cmd/gustyc`'s JSON payload reports `exception: "ValueError"` / `exception_message: "invalid literal for
    int() with base 10: 'abc'"` for `print(int("abc"))` where it used to report a compile failure — the
    machine path stops lying about which half of the system failed.
  - `programs/constant_trap.gy` joins the conformance corpus (190 → 191 programs, 179 → 180 matrix rows,
    141 → 142 parity-asserted, 126 → 127 `match`), printing the same twelve lines as CPython.
- **Negative**:
  - The list-index traps carry this language's wording (`index out of range`), not the reference's
    (`list index out of range`). That is Gap R.90, which the constant road now shares with the dynamic one —
    one row, two roads, one fix. It is also why three rows in the unit table assert the two wordings
    separately (`constantTrapBothLegs`) rather than being recorded: `tools/recmerge` refuses to write an entry
    the two legs disagree on, which is the recorder being right.
  - A fold that raises is slightly more IR than a fold that refuses was: the raise plus a fresh block. It is
    dead code in the sense that nothing after it in that statement executes, and the optimiser removes it; it
    is not dead in the sense that matters, because the program reaches it.
  - `build()[7]` — a subscript on a **call** result — still refuses (`index requires an inline list/dict/set
    literal`), and `{"a": 1}["b"]` — a **text** key into a dict literal — still refuses (`index must be a
    constant`) even though `d = {"a": 1}; d["b"]` traps correctly. Both are container-*source* gaps rather
    than trap-door gaps, so they are Gap R.214 and Gap R.215, filed with the compiled answer beside them,
    and each names the door in this commit as the half it will reuse.
- **Neutral**:
  - The `except` forms stay plain (`except IndexError:` and friends); `except IndexError as e:` is a parser
    debt that pre-dates this row and is not made worse by it.

## Verification

- Unit (`pkg/lang/constant_trap_test.go`): the record leg over 17 sources — positions, keys and every
  conversion class — asserted against what the record holds; the reference leg over 5 sources where the
  record cannot hold an entry (the wording debt); the answering side over 10 conversions including
  `int(" 12 ")`, `int("+42")`, `int("1_0")`, `int(True)`, `float("1_0.5")`, `float(True)` and the no-argument
  forms; catchability for all five classes, cross-checked against the reference's own output; and a
  block-shape test that compiles a trap followed by more code in the same block (the verifier's customer,
  ADR 0166).
- Unit (`pkg/lang/ircheck_test.go`): `print(ord("hello"))` is asserted to carry
  `ord() expected a character, but string of length 5 found` in the module and **not** to carry `104`.
- Unit (`pkg/lang/method_unwind_test.go`, `pkg/lang/string_subscript_test.go`): the rows that pinned the
  refusal are pinned to the raise instead — `TestConstantTrapInsideMethodBodyIsARuntimeTrap` catches an
  `IndexError` raised out of a method's frame in its *caller*'s handler, and
  `TestRefusalInsideMethodBodyIsACompileError` keeps a witness for the exit-1 class by using a construct the
  backend genuinely cannot lower (`return len(5)`), so the exit-code half of the claim did not lose its
  example when the out-of-range read stopped being one.
- Integration (`integration/constant_trap_test.go`): 11 CLI rows asserting the reference raises the same
  class and sentence, that the compiled run exits **3** and never 1, never 0, never 2, that the traceback is
  printed, and that no `codegen:` sentence appears; 4 catchable rows asserted through `--aot` and `--build`
  against the reference's stdout; and the corpus program asserted as a conformant matrix row.
- Integration (`integration/builtin_trap_classes_test.go`): the CLI's "these exit 1" list loses
  `print(int("abc"))` and gains the number-wider-than-the-word refusal, so the compile-failure class keeps a
  witness instead of losing one.
- IR: every module the new tests emit is built by the JIT and, on the link path, verified by `llc`/`opt`
  (ADR 0154); the full emitted-IR sweeps in `pkg/lang` and `integration` are green.
- Artifact guards: `TestTheArtifactTableCountsAreTheFilesOwn`,
  `TestTheRecordedSourceCountInTheAgentFacingDocsIsTheFilesOwn`,
  `TestTheRoadmapSnapshotIsMeasuredNotRemembered` recompute every count quoted here.

## Measured, not remembered

| Claim | Number | Where it lives |
|-------|--------|----------------|
| Constant reads routed through the trap door | 5 roads (`literal list`, `literal string`, `literal dict`, `comprehension object`, `method body`) | `pkg/lang/codegen.go` |
| Conversion traps | 4 builtins (`int`, `float`, `ord`, `chr`) + `print`'s `chr` arm | `pkg/lang/codegen.go`, `pkg/lang/constant_trap.go` |
| Drift ledger rows paid | 5 (337 → 332) | `pkg/lang/testdata/interpreter-golden-drift.json` |
| Record entries added | 35 (6095 → 6130) | `pkg/lang/testdata/interpreter-golden.json` |
| Conformance programs / matrix rows | 191 / 180 (142 parity + 38 recorded) | `integration/programs`, `integration/conformance-matrix.json` |
| Rows opened by this cycle | 3 — Gap R.213 (paid here), Gap R.214 and Gap R.215 (measured, owed) | `roadmap.md` |
| CLI refusals still asserted at exit 1 after the change | 3 (`int` too wide for the word, `len(5)`, `x = 5; x[0]`) | `integration/builtin_trap_classes_test.go` |
