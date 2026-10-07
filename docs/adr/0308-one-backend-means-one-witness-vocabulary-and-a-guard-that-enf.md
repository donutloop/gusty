# One backend means one witness vocabulary — and a guard that enforces it

Status: accepted. Roadmap: `Gap R.190` (closed by this record), `Gap R.38` (refusal sentences that lie
about the other backend), `L13.1` (the REPL's echo). ADR 0302 retired the engine; this record finishes
the retirement.

## Context

ADR 0302 deleted the AST interpreter. It deleted the code, the flags and the payloads' second leg, and it
replaced the engine with a record (`pkg/lang/testdata/interpreter-golden.json`) plus two ledgers. What it
did **not** delete was the language the repository had been written in for 300 ADRs:

- `AGENTS.md`, the file that *is* the loop's contract, still opened with "Two execution paths — both are
  first-class", still named `pkg/lang/jit.go` and `EvalExpr` as the place a feature is implemented
  first, and still required "an interpreter integration/unit case (`EvalExpr`)" for every feature. The
  next cycle reading that file would have re-added the engine.
- `roadmap.md`'s column contract defined `Path` as `both` = "interpreter + LLVM AOT", and ~40 rows'
  evidence said "on both engines" / "three engines".
- ~500 test comments and failure messages said the same thing: "checked on both engines", "the
  interpreter prints %q", "the backends disagree".
- The CLI's own help text advertised an interpreter leg: `--bench` "through both backends (interpreter +
  AOT JIT)", `--bench-suite` "(interpreter vs AOT)", `--bench-gate` "aot, interpreter or both",
  `--oracle` "run interpreter + compiled backend + CPython". An agent discovers this tool by that text.
- Test helpers and cases carried the engine in their names: `zdInterp`, `boolInterp`, `interpRun`,
  `Test…OnBothBackends`, `Test…InTheInterpreter`, and a file called `jit_test.go` whose every case
  compiles.

None of that is cosmetic. A claim a reader cannot execute is not weak evidence, it is a false one: a
contributor (human or agent) who reads "every row is checked on both engines" goes looking for the
engine, cannot run it, and therefore cannot check the claim — which is the exact property this project's
machine-consumption contract exists to guarantee. And a contract file that mandates two backends is not
stale documentation, it is an instruction to build the deleted thing again.

## Decision

**1. Name the witnesses, once.** There is one backend and two **witness legs**. Every behavioural claim in
this repository is made against one of them, and the wording says which:

| Phrase | The claim | Where the witness lives |
|---|---|---|
| the **record leg** | the compiled answer equals the answer the retired interpreter recorded for that source | `pkg/lang/testdata/interpreter-golden.json`, read by `pkg/lang/golden.go`; disagreements in `testdata/interpreter-golden-drift.json` |
| the **reference leg** | the compiled answer equals CPython's answer | `gustyc --oracle`, `integration/conformance-matrix.json`; disagreements in `integration/testdata/cpython-debt.json` |
| **both legs** | both of the above | — |

"Engine" is reserved for something that runs. There is one.

**2. Restate the present tense, keep the past tense.** Test comments, failure messages, CLI help text,
the tracker's prose sections and the docs were rewritten into the vocabulary above. Historical
measurements were **not**: "both engines printed `1` before ADR 0297" stays, because it records what two
implementations said on a date, which is evidence — and the only evidence this project will ever have of
the class of bug two engines catch by disagreeing (ADR 0302's own list: `1 + True` vs `True + 1`,
`print(True)` printing `1` for 100+ ADRs). Rewriting those into "both legs" would have converted a
measurement into a claim about a record that never ran, which is worse than stale.

**3. Enforce it with a guard, not a sweep.** `pkg/lang/witness_claim_test.go` scans the surfaces where
claims are made — `pkg/lang` and `integration` tests, `cmd/gustyc`, `roadmap.md`, `README.md`,
`AGENTS.md`, `docs/operations.md`, `docs/language.md` — and fails on any line that matches a phrase from
`pkg/lang/testdata/witness-banned-phrases.txt` ("on both engines", "both backends print", "the
interpreter prints", `EvalExpr`, `pkg/lang/jit.go`, …) unless the same line carries a marker from
`pkg/lang/testdata/witness-history-markers.txt` (`was`, `used to`, `before`, `retir`, `measured 20`, …).
It also fails if a flag description advertises an interpreter run that is not a retirement notice, and if
`pkg/lang/jit.go` comes back. The two lists are data, not code — the same reason the drift ledgers are
data: a cycle is allowed to tighten the rule without touching the guard, and the guard's own file is
skipped by name so that a future mass-restatement cannot eat the rules it enforces.

**4. Rename what the names lie about.** Helpers and cases that said "Interp" while running the compiler
say what they run: `zdInterp`→`zdRun`, `boolInterp`→`boolRun`, `interpRun`→`compiledRun`,
`interpReport`→`compiledReport`, `Test…InBothBackends`→`Test…OnTheCompiledBackend`,
`Test…OnBothEngines`→`Test…OnBothLegs`, `Test…InTheInterpreter`→`Test…OnTheCompiledBackend`, and
`pkg/lang/jit_test.go` → `pkg/lang/compiled_eval_test.go`. `pkg/lang/jit_llvm.go` keeps its name: it is
the LLVM execution engine, which exists.

**5. Fix what the retirement left unable to fail.** Three harness assertions compared the one remaining
leg with itself, because the second operand was rewritten to the surviving engine and never deleted:
`integration/text_truth_test.go` ran `cliTextOut(t, "--aot", src)` twice and announced "the compiled path
agreed with each other instead"; `integration/for_container_literal_test.go` asserted
`byEngine["--aot"] != byEngine["--aot"]`; `pkg/lang/runtime_block_emit_test.go` parked a dead
`if true { return }` in front of an assertion about a refusal that no longer exists, in words that
promised the reader an interpreter. All three are deleted rather than kept as ceremony, and
`integration/string_containers_test.go`'s refusal assertion no longer requires the word "interpreter" in
a diagnostic (Gap R.38's rule, generalised: a refusal names what is missing, not an engine).

**6. A flag that selects a retired thing is a bad argument, not a no-op.** `--bench-gate` still
accepted `interpreter` and `both`: `lang.CompareBenchSuite` treats an unknown gate as "no regressions",
so the CLI had been printing a clean gate verdict while gating **nothing** — while its own source
comment claimed an unknown gate "fails loudly at the flag". It now exits 4 (usage) naming the retirement,
like `--interp` does (`TestCLIBenchGateIsAUsageErrorWhenTheGateIsGone`). A comment that describes a check
which does not exist is worse than no comment: it stops anyone from looking.

## Consequences

- The contract file now tells a cycle to implement a feature **once**, in codegen, and to judge it on two
  legs. `AGENTS.md`'s step 2 no longer has an "interpreter first" bullet, and its testing rule names the
  record and the reference.
- `roadmap.md` gained a **Witness vocabulary** section that defines the three phrases and reads the
  pre-retirement `Path` values through them (`both` → the compiled backend judged on both legs;
  `interp` → a claim whose remaining witness is the record). Existing rows keep their wording; the guard
  re-states a row when a cycle touches it, which is why the sweep and the guard are one change.
- New tests must use the vocabulary or carry a history marker. That is a real friction, and it is
  deliberate: the alternative is a suite whose comments describe an engine nobody runs.
- Nothing about execution changed. This record is about the *record*: what may be said, by whom, and with
  what witness.

## Alternatives rejected

- **Leave the stale prose alone** ("comments are not code"). Rejected: `AGENTS.md` is not a comment, it is
  the loop's instruction; and CLI help text is the interface an agent parses.
- **Rewrite history too**, so that no line in the repo mentions two engines. Rejected: those lines are
  measurements taken while two engines ran, and their whole value is that they show what one engine
  cannot see. Fabricating a leg that never ran is the failure mode of a retired-engine suite — the record
  replaces the engine for *expectations*, not for *measurements*.
- **A linter in CI instead of a test.** Rejected: this repository's checks are `go test` checks, so that
  an agent running the suite cannot pass while the contract rots; a lint an agent can ignore is the same
  stale comment with a nicer error.
- **Rename the golden artifacts** (`interpreter-golden.json` → `record.json`). Rejected: the file names
  what its contents are — the retired engine's answers — and the drift ledgers, docs and 300 ADR
  references name it. A rename would make the record *less* self-describing, not more.
