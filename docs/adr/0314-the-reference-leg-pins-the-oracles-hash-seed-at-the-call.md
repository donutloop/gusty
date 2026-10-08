# 0314. The reference leg pins the oracle's hash seed at the call, not at the caller

Status: accepted. Roadmap: `Gap R.196`. Continues: ADR 0193 (the oracle is a *named*, versioned toolchain —
`GUSTY_PYTHON`, `toolchain.python`, `lang.OracleMinPython`), ADR 0186 (CPython is the conformance oracle, so
the leg's answer is evidence), ADR 0166 (a refusal must name what is missing — and a leg that wobbles names
nothing), ADR 0302/0308 (the two witness legs: the record and the reference; only one of them is a program
this compiler owns).

## Context

The first full run after ADR 0313's sharding was red on an untouched tree:

```
--- FAIL: TestCLIAgentPairBoundNameMutatesAContainerAgreesWithCPython/the_a_text_slot_added_to_a_set#02
    xs = [] / xs.append("a") / n = xs[0] / s2 = {1} / s2.add(n) / print(s2)
    reference: stdout "{'a', 1}\n"
    compiled:  stdout "{1, 'a'}\n"
```

Run alone: green. Run again: green. The compiled answer had not changed — gusty's sets are insertion-ordered
and always have been, so the second line is the only answer the compiler is capable of printing. The first
line came from CPython, and CPython orders a set by the hashes of its members, and the hash of a **text** is
randomised per process unless the environment says otherwise:

```
$ for i in $(seq 20); do python3 -c "s={1}; s.add('a'); print(s)"; done | sort | uniq -c
     17 {1, 'a'}
      3 {'a', 1}
```

So `Test…/the_a_text_slot_added_to_a_set` was a coin flip on where `hash("a")` landed, in a suite whose
contract is that a red row is a finding.

The pin itself was not new. `lang.PythonRun` — the conformance matrix's oracle call — has set
`PYTHONHASHSEED=0` from the beginning, with the reason spelled out: the matrix is a committed artifact, and a
set whose order changes between two regenerations is a diff that means nothing (ADR 0193's honesty about what
the oracle is). What never generalised was the *rule*: nine reference-leg spawns across `integration` (seven
helpers) and `pkg/lang` (two) built their own `exec.Command(py, …)`, inherited the environment, and compared
whatever answer came back against the compiler's.

## Decision

**A leg is only evidence if it is a function of the source**, and the environment is part of the source's
meaning. Every process that answers a reference question therefore gets its environment from one helper, not
from the call site:

| helper | serves | pins |
|---|---|---|
| `lang.PythonRun` | the conformance matrix, `--oracle` | `GUSTY_PYTHON`, `PYTHONHASHSEED=0`, scrubs the run dir from stderr |
| `integration.oracleCommand(t, args…)` | the CLI-level cases in `integration` | the same two |
| `pythonTwin(t, src)` / `pythonTwinLine` | the in-package cases in `pkg/lang` that want the combined stream (a raise's sentence arrives on stderr and the row prints its last line) | the same two |

The two pins are the same pair for the same reason: `GUSTY_PYTHON`, because judging gusty against an
interpreter the harness would not name in the matrix is judging nothing (ADR 0193); `PYTHONHASHSEED=0`,
because judging it against a *different answer each run* is worse than judging nothing — it produces a
finding that does not exist.

The call sites keep their own shape (who captures stdout, who merges stderr, who reads an exit code) and lose
their ownership of the process environment. `TestTheReferenceLegsSetOrderIsPinned` asks the reference the same
set-shaped question twenty times and fails if it ever gets two answers; twenty is chosen from the measured
flip rate (3 in 20 → a run without the pin catches it with probability ≈0.9997), so the guard's passing is not
luck. `TestTheReferenceLegRunsTheFileItIsGiven` pins the other way that a shared helper can fail: running the
wrong path makes every reference answer in the package wrong in the same direction, quietly.

What this is **not** is a debt row. Filing `{'a', 1}` in `cpython-debt.json` would have recorded — and
permanently pinned — a bug the compiler does not have, in the file whose whole purpose is to hold bugs it
does. When the disagreement lives in the leg, the fix is the leg; `TestMain`'s ratchet is for the other case.

## Agentic rationale

An agent told "this row diverges from CPython" will go and change the compiler. When the divergence is an
artifact of the harness's environment, that instruction is not merely unhelpful, it is actively destructive:
it spends the loop chasing a bug that is not in the tree, and if it "succeeds" it pins the coin flip as the
expected answer. Determinism of the *reference* is what makes a `debt` row worth reading, which is why the pin
lives in the helper that starts the process: a call site that has to remember it is a call site that will
forget it, and nine of them did.

## Consequences

* Any new reference-leg call reaches one of the three helpers; adding a fourth `exec.Command(py, …)` is the
  mistake this ADR exists to make loud, and the guard makes it fail a test rather than fail a run.
* `PYTHONHASHSEED=0` makes CPython's set iteration *stable*, not equal to gusty's. It does not make the two
  agree — gusty prints insertion order, `RuleSetOrder` in the oracle legs compares sets as unordered
  multisets for exactly that reason — it makes the reference's answer repeatable, which is a weaker claim and
  the only one a harness is entitled to make.
* Set-shaped CLI cases stop being intermittently red. The one that started this was, on a quiet tree, red
  ~15% of runs and green otherwise — the class of failure that gets described as "flaky CI" and left alone.
* `docs/operations.md` § The CPython oracle leg now states the pin as a rule for call sites, with the
  measurement in it.

## Alternatives rejected

* **Sort both sides before comparing in that one case.** Fixes the row, hides the disease: the same
  nondeterminism was in eight other call sites, and each would be handled by its own "just normalise it
  here", until the leg compares nothing but sorted bags of characters.
* **Give every set-printing case a deterministic-looking expected string.** Pins CPython's answer for one
  process-launch's worth of hash randomness. The row would pass until the next time the randomiser disagreed,
  which is precisely the status quo with a nicer comment.
* **Skip the case when the reference's answer is ambiguous.** "We could not tell" reported as success is the
  failure mode `docs/operations.md` refuses elsewhere (7 vs 0), and here it was avoidable with one env var.
* **Set the seed in CI's environment instead.** Then local runs stay flaky, and the suite's determinism
  depends on a YAML file in a different repository-of-record than the suite. The pin belongs where the process
  starts.
* **Assume `PythonRun` covers the tree.** It covers the matrix; the CLI-level cases wanted a merged stream and
  grew their own calls — which is the actual lesson, and why the helpers are three and not one.

## References

`integration/oracle_spawn_test.go`, `pkg/lang/python_twin_test.go`, `pkg/lang/oracle.go` (`PythonRun`,
`RuleSetOrder`, `PythonBinary`), `docs/operations.md` (§ The CPython oracle leg, § The three pinned
toolchains), ADR 0193, ADR 0186, ADR 0313 (the run that surfaced this).
