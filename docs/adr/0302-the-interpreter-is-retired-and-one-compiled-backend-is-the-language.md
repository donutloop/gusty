# The interpreter is retired; one compiled backend is the language

## Decision

The AST interpreter (`pkg/lang/jit.go`, entered by `EvalExpr`/`EvalProgram`) is **deleted**. There is one
execution path in gusty: parse → check → LLVM codegen → `llc`/`cc` → run the artifact. `--interp` is a
retired usage error rather than a silently-accepted flag, `--aot` and `--jit` stay accepted as
compatibility no-ops, every machine-readable payload names one backend (`"backend": "aot"`), and the
conformance oracle has two legs (`aot`, `python`) instead of three.

This reverses the two-backends-must-stay-in-sync rule that opened this repository's agent contract. That
rule earned its keep while both paths were young — every disagreement it caught was a real bug — but it
cost a factor of two in every feature, and by this cycle the interpreter had become the thing that made
features *expensive* rather than the thing that made them *trustworthy*: decorators, nested closures,
closures over mutable state, and every tagged-value question had already been settled by the compiled
path, with the interpreter either duplicating the answer from different source or lagging it.

What the interpreter was actually for — a fast, richly-diagnosing second opinion, the REPL's engine, the
oracle's second leg — is replaced by four things, and this ADR's real content is those four:

| The interpreter gave | What gives it now |
|---|---|
| 5378 recorded reference answers, used as test expectations | `pkg/lang/testdata/interpreter-golden.json`, a committed record, consulted by the shared harness |
| "does the compiler agree with the other engine?" | two ledgers: `testdata/interpreter-golden-drift.json` (compiled vs the retired engine's record) and `integration/testdata/cpython-debt.json` (compiled vs the reference) |
| the REPL and `--eval` | the compiled backend, with an echo on the tool channel (`pkg/lang/echo.go`) and source-replay persistence |
| the oracle's `interp` leg | the `aot` and `python` legs; conformance is judged against CPython, and against registry pins where CPython cannot answer |

## Why this shape, and not just "delete it"

Deleting an engine is easy and it is how suites silently die. The failure mode is specific and it is the
one this cycle had to design against: a test that asserted `eval(src) == "42"` does not become false when
the engine goes away, it becomes **absent**, and an absent test is greener than a failing one. Roughly
2900 cases in `pkg/lang` and several hundred in `integration` asked the interpreter a question. Deleting
them would have removed the coverage while leaving `go test ./...` perfect, which is the worst outcome
available to a compiler test suite: the perfection is the bug.

So the deletion went in this order, and the order is the decision:

1. **Record before removing.** Before any interpreter code was touched, the engine was instrumented to
   answer every source its own suite asked about, and each answer was written to
   `testdata/interpreter-golden.json` with everything a case needs to be re-checked without the engine:
   repr and type name of the value, program stdout, the trap's class and message, whether the *shared
   front end* refused the source, and a `note` where the entry itself needed correcting. 5378 entries.
2. **Convert, then delete.** Each case was rewritten to run the compiled backend and compare against the
   record. Only after a package's cases were converted did its interpreter calls go. `go test` was green
   at every commit boundary in that sequence.
3. **A missing record is a failure.** `goldenLookup` reports `no recorded interpreter expectation` when a
   case asks about a source the file does not hold. Deleting an entry therefore cannot delete coverage;
   it deletes the run.
4. **A disagreement is a ledger row, never a passing expectation.** Where the compiled answer differs from
   the record — `cba` vs `abc`, a program the compiler refuses to build, a trap whose class moved — the
   case does not assert the compiled answer (that would be a pin on a wrong value) and does not fail the
   build (the suite would stop being runnable). It *skips with the reason and both answers in its message*
   and registers a divergence. `TestMain` then holds that set against the ledger file: a **new**
   divergence fails, and a divergence that got **fixed** also fails, until the row is removed. A ledger
   that only catches new debt is a way of grinding debt into permanence; the two-way ratchet is what makes
   the file a work list.
5. **The reference is the arbiter that outlives the record.** A golden record is an answer the retired
   engine happened to give, which includes its mistakes. Where the compiled backend disagrees with
   CPython — `print(sum([1, 2, 3].append(4)))` answers `10` where the reference raises `TypeError` — the
   row goes to `integration/testdata/cpython-debt.json`, which names the reference's answer, the compiled
   answer, and the roadmap row that owes the fix. That ledger is also two-way, also requires ownership on
   every row, and additionally refuses to pass when a case that owns a row stops running it.

### What a test may now assert, and why each is allowed

The converted cases are not weaker; there are exactly three permitted verdicts and each is checked:

- **the answer** — the compiled run's stdout/value equals the record's (and, in reference-checked cases,
  CPython's live answer too);
- **a trap** — exit class 3 with the reference's exception class and message, catchable where the
  reference is catchable;
- **an honest refusal** — exit class 1 with a sentence that names the missing door, the reference's
  behaviour at that door, and a roadmap row or ADR. `refusesHonestly` measures this; the three-word
  refusals the old suite accepted (`unsupported call "f"`, `str on non-integer`, `unsupported list method
  index`, `sorted: codegen folds only an inline list literal`, `list index out of range`) are **not**
  honest and each one had to be rewritten as part of this cycle.

Anything else — exit 0 with a wrong value, exit 2 blaming the toolchain for a source problem (ADR 0166), a
mute refusal — fails. Every refusal taken during a run is counted and the count is printed
(`compiled refusals this run: 56 (filed gaps, not answers)`), because a suite whose green depends on
refusals needs that number visible: it going up while everything passes *is* the suite going soft.

### The REPL and `--eval` are compiled, which changes what they are

The interpreter made a REPL session a program with a live heap. A compiled turn is a process, and a
process's state dies with it. The honest model, and the one implemented:

- each turn compiles and runs the state-establishing turns before it, as **source**, so `def`/`class`/
  imports/assignments persist across prompts;
- a turn that is a bare expression or call is a **query** and is not replayed — replaying `print(...)` or
  a call with effects would run the side effect twice, which the user would read as the compiler being
  wrong about their own program;
- the value of a snippet is echoed on fd 2 (`gusty: result <kind> <repr>`), which is the tool channel, so
  the program's stdout stays exactly the program's stdout;
- the echo renders with `FormStr`, matching what the retired REPL displayed (`cba`, not `'cba'`).

The echo has a rule that looks like a hole and is a deliberate one: **a final expression that is a
user-defined call is not echoed.** Rendering it means lowering the call a second time, and a second
lowering of `show(x)` prints twice. Silence beats duplicating an effect the user wrote once, so the value
of `f(5)` is not shown while the value of `str([1, 2])` is — the builtins on that list are pure, and a
program that shadows one loses the privilege. The gap is `L13.1`, `TestREPL`'s
`TestREPLCallResultEchoIsFiledNotFixed` row pins it, and a "quiet prompt" is a behaviour an agent can
misread as `None`; that is why it has a roadmap row and a test rather than only a comment.

### The compiled path's own gaps became visible, and are filed

Running the whole suite through codegen exposed refusals that the interpreter's answers had been hiding —
not regressions, but capabilities that were never in the compiler and were never named: iteration and
comprehensions over a text at run time, `sorted` over a container that is not a literal, `dict(<container>)`
copies, `list.index`/`dict.pop` as expressions, higher-order calls and functions as values, `str()`/repr of
a value whose kind is a run-time fact. Each is a `noteCompiledGap` row, and each is now either owned by an
existing row (`L11.1`'s tagged value word, `L12.11`'s receiver tables, `Gap I.2`'s container runtime doors,
`Gap R.37`'s compile-time-known traps) or got a fresh one. Retiring the interpreter did not create these;
it removed the engine whose answers made them invisible.

## Codegen and IR implications

- `strArgIsNumberish`/`strArgBindings` recursed infinitely on `s = s + x` inside a function; a guard
  (`strArgInFlight`) refuses the shape instead of blowing the stack, and `TestStackOverflowGuardRejectsSelfReferentialStrArgBindings`
  checks the guard rather than the compiler's capacity to hang.
- The `dict(<container>)` copy road was a placeholder whose body did not parse (`store { … }`), and the
  module verifier had never seen it because `Compile` refuses it first. It now refuses by naming the
  missing runtime door and its roadmap row; the IR unit test that reached it checks the refusal, not the IR.
- `KeyError` messages render the key's repr where the key is a literal (`KeyError: 'z'`), which is the
  reference's sentence; where the key arrives through a computed path, the message is still the module's
  generic one, because naming that key means rendering a value whose kind is a run-time fact (`L11.1`).
  Three existing pins asserted the old asymmetry and moved with this change — that is what the record is
  for.
- Constant out-of-range reads (`[][0]`, `s[9]`) refuse at compile time with a sentence naming
  `Gap R.37`; a bare `list index out of range` from the compiler was indistinguishable from a runtime trap
  with the same words.
- Calling something that is not a function refuses by naming the value's actual kind, what CPython raises
  (`TypeError: 'int' object is not callable`), and the row that owns the fix.
- A generated lambda's arity sentence says `<lambda>`, never the compiler's `lambda_0`: an arity message is
  the sentence a reader acts on, and blaming a name nobody wrote is a diagnostic bug.

## Alternatives rejected

- **Keep the interpreter as a test oracle only.** It costs the same maintenance as a supported engine, and
  its answers stop being checked the moment nobody runs it for real; a stale oracle is worse than none,
  because it manufactures agreement. The record gives the same answers, is diffable, and cannot drift.
- **Delete the interpreter and its tests together.** The fast option, and the one that produces a
  perfectly-green suite over removed coverage. Rejected for the reason in §"Why this shape".
- **Snapshot the interpreter as a separate `gusty-interp` tool.** Moves the fork rather than paying it; a
  second engine nobody is forced to keep in sync becomes a second engine nobody believes.
- **Keep `--interp` working for a release as a deprecation path.** Two backends is the cost this decision
  exists to stop paying, and a flag that works while undocumented-as-deprecated invites exactly the
  "which engine answered?" question this cycle has to answer about every old test.
- **Have the golden harness ask CPython instead of keeping a record.** The reference answers about a
  language subset (imports, the extension classes, `run`/`suspend`, the set-literal probes have no
  counterpart), is not installed on every machine a developer tests on, and re-derives 5378 expectations
  per run. The record is the fast path; CPython is the check on the record, and is asked live in the cases
  where the claim is about the reference.
- **Silence the echo for all calls, or echo everything.** Echoing everything duplicates effects; silencing
  everything loses the REPL's core affordance. The purity rule is the middle that does not lie in either
  direction, and the remaining hole is a roadmap row.

## References

`roadmap.md` Gap R.190 (this decision's tracker row) · `roadmap.md` L13.1 (echo of a call's value) ·
`docs/operations.md` (CLI, exit codes, the JSON `result`/`type`/`backend` fields, the recording
environment variables) · `docs/language.md` §"One backend" · `_001_session_learnings_` (the cycle's
process lessons, including the `TestMain`-in-a-non-test-file bug that made the drift ratchet a no-op for a
whole cycle, and the golden-record cleanup that had to be reverted).
