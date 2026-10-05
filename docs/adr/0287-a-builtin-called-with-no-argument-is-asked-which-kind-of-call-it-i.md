# ADR 0287 — a builtin called with no argument is asked which kind of call it is, before its operand is reached

Date: 2026-07-05
Status: Accepted
Roadmap: closes **Gap R.131** (the last open half; `round()`'s share was paid by ADR 0263).
Depends on: ADR 0166 (exit 2 is the compiler's bug — this row is four of its last reachable shapes),
ADR 0215 (trap wording is a contract, so the reference's sentence is not interchangeable with ours),
ADR 0263 (`round()`'s arity, the template this cycle copies), ADR 0257 (a verdict prints its own name),
Gap R.6 (a program that takes a builtin's name owns it), ADR 0284 (arity asked once on the shared road).

## The measure

`python3` 3.12.3 is the oracle. One line each, run on both engines:

| program | CPython | before, `--interp` | before, `--aot` | after, both |
| --- | --- | --- | --- | --- |
| `print(int())` | `0` | **Go panic, exit 2** | refusal, exit 1 | `0`, exit 0 |
| `print(float())` | `0.0` | **Go panic, exit 2** | refusal, exit 1 | `0.0`, exit 0 |
| `print(bool())` | `False` | `NameError`, exit 3 | `unsupported call "bool"`, exit 1 | `False`, exit 0 |
| `print(str())` | (empty line) | `str expects 1 argument`, exit 3 | refusal, exit 1 | empty line, exit 0 |
| `print(ord())` | TypeError | **Go panic, exit 2** | refusal, exit 1 | TypeError, exit 3 |
| `print(chr())` | TypeError | **Go panic, exit 2** | **Go panic, exit 2** | TypeError / refusal |
| `print(abs())` | TypeError | `abs expects 1 argument`, exit 3 | refusal, exit 1 | TypeError, exit 3 |
| `print(repr())` | TypeError | `str expects 1 argument`, exit 3 | refusal, exit 1 | TypeError, exit 3 |

Four of the eight reached `n.Args[0]` with no argument in hand and produced `index out of range [0] with
length 0` — a Go stack trace and **exit 2**, the code the exit-code contract reserves for a compiler bug.
`chr()` reached the same missing check *in codegen as well*, so it was an exit 2 on both engines.

## The diagnosis

Not eight bugs. One missing question, asked by nobody, in front of eight names: **is there an argument at
all?** The evaluator's builtin dispatch reads `n.Args[0]` to build the answer and the arity check, when
present at all, checks `!= 1` afterwards — a shape that cannot help, because the read happens first.

The reason it stayed unfixed for so long is that the eight cases need **two different answers**, and the
code had one slot for them:

* `int()`, `float()`, `bool()`, `str()` are **constructors**. Zero arguments *is* a call they answer — `0`,
  `0.0`, `False`, and the empty text. For these, an arity refusal is itself wrong: ADR 0166 keeps exit 1
  for programs that have a compile error, and `print(int())` is a program CPython runs in one line.
* `ord()`, `chr()`, `abs()`, `repr()` are **conversions of a required value**. The reference raises
  `TypeError: <name>() takes exactly one argument (0 given)`, so the honest answer is a trap (exit 3) with
  that sentence, not ours.

## The decision

**Ask the arity before evaluating, at each builtin's entry**, in the two shapes the reference has. The
template is ADR 0263's `round()`: one sentence, raised by the evaluator and refused by codegen, exit 3 and
exit 1, exit 2 nowhere. Extending it to the constructors is the part that needed measuring rather than
copying: `int()` and friends do not want the sentence at all, they want a value.

**`bool()` answers a verdict word, not a text.** The compiled road first selected between the interned
strings `"True"`/`"False"` and handed the result to `rt_print_bool` — which asks only whether its `i32` is
zero, so it printed `True` for the index of `"False"` and `False` for the index of `"True"`. The pair of
answers was **inverted**, which no amount of reading the `select` reveals. A verdict in this backend is the
word 1 or 0; the strings are the printer's business.

**A text argument asks a different truthiness question than a number.** `bool("")` is False and
`bool("x")` is True, but a text's word is its interned **index**, and "the index is not zero" says nothing
about whether the text has characters. The text road measures it with `rt_str_len`; only the number road
gets to use "the word is not zero".

**A container refuses rather than being guessed at.** `bool([1])` is True and `bool([])` is False —
truthiness is the object's **length**. My first version compared the slot's word against zero, which failed
twice: for a list literal the word is a GLOBAL and llc-20 rejected the module
(`icmp ne i32 @.lst1, 0` — *"global variable reference must have pointer type"*), and for any allocated
container it would have answered True whatever the contents, `bool([])` included. So the compiled leg
declines a container operand in words; the interpreter answers it. L11.1's tagged value word owns the rest.

**`float()` is answered by the fold, not at the call site.** Emitting a textual double from the call road
made the print road widen a value that was already a double: `%t1 = sitofp i32 0.0 to double`, rejected by
llc-20 with *"floating point constant invalid for type"*. The answer belongs in `floatEval` beside the other
folds, plus an arm in `floatValue`, so the emitted module is indistinguishable from `print(0.0)`'s. A test
asserts exactly that ("one of the two did not take the float renderer", and no `sitofp i32 0.0`).

## `repr()` is not `str()`, and that was measured, not assumed

`str()` and `repr()` share one renderer, one table and one refusal message (ADR 0258), so the natural
implementation gives them the same arity. The reference disagrees: `str()` answers the empty text and
`repr()` **raises**. A single `if len(n.Args) == 0` over the pair would have shipped four wrong answers
with two characters' difference from correct. `TestReprIsNotStrWithNoArgument` pins the asymmetry, because
nothing in the code's structure would remind a later cycle that the pair parts company here.

## Alternatives rejected

* **One arity guard for all builtins.** Rejected: it is exactly the flattening that produced this row. Half
  the family must answer and half must raise; a shared guard can only do one of the two, and getting it wrong
  on the constructor half is an exit 1 for a program the reference runs.
* **Return the interned `"True"`/`"False"` text from `bool()`.** Rejected after the inverted answers; see
  above.
* **Answer `bool(container)` as "handle is not zero".** Rejected: it is True for an empty list, a wrong
  number dressed as a verdict, and it emits IR the assembler rejects for a literal.
* **Reuse our own arity sentences** (`abs expects 1 argument`) rather than the reference's. Rejected under
  ADR 0215: these programs exit 3 either way, so wording is the only observable, and a traceback quoting a
  sentence CPython never produces is a divergence the oracle can and does see.
* **Refuse `bool(container)` in the checker.** Rejected: the interpreter answers it correctly today, and a
  checker-level ban would remove a working answer from the one engine that has it — the ladder rule.

## Agentic rationale

Exit 2 was the whole story here: a harness driving this compiler cannot distinguish "my program is wrong"
from "your compiler crashed" except by the exit code, and for eight programs it got the same code the
compiler uses for its own bugs. After the change: exit 0 is the reference's answer, exit 1 names a missing
feature in words (`a container's truthiness is its length…`), exit 3 carries the reference's own TypeError,
and a 21-spelling integration table asserts exit 2 is unreachable from any of them. `--json` is unchanged.

## Tests

* `pkg/lang/builtin_arity_test.go` — four tables: the constructors (13 rows × both engines, each pinned
  against `python3`, including Gap R.6's name-ownership row), the raising builtins pinned to the
  reference's exact sentence, a **no-panic sweep** over 15 spellings asserting structurally that neither
  evaluator nor codegen dies and that no refusal leaks a Go runtime message, and the `repr()`/`str()`
  asymmetry.
* `pkg/lang/builtin_arity_test.go` also keeps the two bugs this cycle produced on the way:
  `TestCompiledBoolAnswersAVerdictNotAText` and `TestFloatConstructorTravelsAsADouble`, plus
  `TestBoolOfAContainerRefusesRatherThanComparingAHandle` for the honest half.
* `integration/builtin_arity_test.go` — CLI tables over both engines with a live `python3` cross-check, the
  trap-wording rows, and `TestBuiltinWithNoArgumentNeverExitsTwo` over 21 builtin names.
* `integration/programs/probe_builtin_without_arguments.gy` **promoted** from a recorded debt to
  `conformanceStandalone()` — it now answers `0`, `0.0`, `False`, `` on all three legs. Its exit-6 row was
  **moved** out of `integration/bool_element_test.go`, not deleted, following ADR 0261's precedent: a
  contract row left behind expecting exit 6 for a paid debt passes forever without asserting anything.
* Measured against the stashed baseline first: the new table fails there with the real
  `panic: runtime error: index out of range [0] with length 0`.
* Suite green; 164-file sweep against the pre-cycle binary moved nothing except the promoted probe.
