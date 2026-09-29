# The oracle is a versioned toolchain, and a stale one must not read as a compiler bug

## Context

CI went red on a row nobody had touched:

```
oracle drift programs/typealias (typealias.gy): declared oracle "match", observed
"not_applicable" (the CPython leg did not complete: exit status 1:
  File "/tmp/gusty-oracle…/prog.py", line 1)
```

Nothing in the compiler had changed. The program begins `type Count = int`, which is gusty's
type-alias spelling and also CPython's **PEP 695** — valid only on Python **3.12+**. My laptop has
3.12.3, so the row passed locally and had passed for weeks. The CI runner was `ubuntu-22.04`, whose
`python3` is 3.10: the oracle leg died with a `SyntaxError` on line 1, the harness classified the
row `not_applicable`, the ledger had declared `match`, and the drift detector did exactly what it is
supposed to do — while the *cause* was in the runner image.

Three things about that failure are worth writing down.

It **blamed the compiler**. The drift note is "the CPython leg did not complete", which points a
reader (human or agent) at gusty. The actual fault was the environment, and nothing in the output
said so.

It was **invisible in the artifact that could have shown it**. The matrix already records
`toolchain.python` — `"Python 3.12.3"` — so a green matrix from my machine and a red one from CI
were distinguishable by a field nobody compared. Recording a toolchain that nothing checks is
half-measurement.

And it was **a pin I had already made in words and not in code**. `docs/operations.md` called
CPython "a named toolchain"; no constant, no check, no CI step said what the name was. The LLVM side
of this project learned this lesson earlier (the pinned version is recorded and asserted); the
oracle side had not caught up.

## Decision

**1. Pin the oracle: CPython >= 3.12**, as `lang.OracleMinPython`, documented in
`docs/operations.md` next to the LLVM pin. It is a requirement, not a preference: for a corpus whose
contract is "the expectation comes from CPython", a construct the oracle cannot *parse* has no
expectation to compare against.

**2. The CI runner provides the pinned oracle.** `.github/workflows/go.yml` moves to
`ubuntu-24.04` (Python 3.12) and the LLVM apt line moves to `noble` with a `signed-by` keyring
(`apt-key` is gone from that image). The runner image is now a *declared dependency* of the
conformance suite, in the same clause as LLVM 20.

**3. A stale oracle fails loudly, at the top, with the remedy.** `TestConformanceMatrix` runs
`requirePinnedOracle` before any leg:

```
the oracle is Python 3.10.12 but the corpus is validated against 3.12 (docs/operations.md):
install a newer python3 or set GUSTY_PYTHON. A matrix run on an older oracle reports drift
that is not the compiler's.
```

One clear failure beats twenty drift lines that each accuse the compiler of a language gap. CI
additionally has a setup step that prints `go` / `llc-20` / `python3` and exits non-zero below the
pin: a red *setup* step is a different category of red, and the log says which.

**4. A `SyntaxError` in the oracle leg now carries its own hint.** `lang.OracleTooOldHint` adds a
note naming the pin and `GUSTY_PYTHON`, because "line 1 SyntaxError" on a program the interpreter
runs is almost always toolchain, not language. A runtime error (`ZeroDivisionError`) gets no such
note — the hint is keyed to the symptom, so it cannot launder a real bug into an environment complaint.

**5. Unknown is not "too old".** `lang.OracleVersion` parses `python3 --version` banners; an
unrecognisable banner is *unknown*, and `OracleVersionTooOld` returns false for unknown. A machine
whose oracle cannot be read must not be told it is unsupported — that would be a new way to fail
closed on a stranger's laptop, which is exactly the wrong lesson for a toolchain that wants agents to
drive it.

**6. The matrix records the pin.** `toolchain.min_python` joins `toolchain.python` and
`toolchain.llvm`; matrix `schema_version` goes **1.1 → 1.2**, and `--schema`'s `conformanceRow`
description says plainly that `oracle: "match"` means "matches the pinned oracle", pointing at the
block to compare when two machines disagree.

## Alternatives rejected

- **Reclassify `programs/typealias` as `not_applicable`.** This is what the drift detector asked
  for, and it is the wrong answer: the compiler is *right* on that program and CPython agrees on any
  interpreter that can read it. Silencing the row to satisfy the harness would delete a real
  assertion to fit the environment — the same mistake as editing an expected output to match what the
  program printed, one layer up.
- **Per-row minimum oracle versions.** Honest and precise, but premature: one construct family
  (PEP 695) forces the pin today, and N rows each carrying a version invites N stale claims. When a
  second family appears and the pins genuinely differ, the ledger field is a small addition to
  `ConformanceCase`. One pin, checked, is the amount of mechanism the evidence supports.
- **Have the harness translate PEP 695 away before running the oracle** (rewrite `type X = int` to
  `X = int`). Tempting and cheap, and it fabricates the oracle's answer: the whole point of the third
  leg is that CPython speaks for itself about the surface we implemented. A rewritten program is a
  different program.
- **Trust `toolchain.python` after the fact.** It is in the artifact, but nothing failed when it
  disagreed with reality — the CI run was already distinguishable from mine by that field, and CI
  was still red before anyone read it. A recorded fact that gates nothing is documentation, not a
  contract.

## Consequences

- Local runs and CI now share an explicit oracle version; `GUSTY_PYTHON` is the documented escape
  hatch for a machine whose default `python3` is old (and the check is what makes using it visible).
- `TestOracleVersionParsesTheVersionBanner`, `TestOracleTooOldIsComparedAgainstThePin`,
  `TestOracleTooOldHintNamesTheRemedy` and `TestBuildOracleReportKeepsAnOldOracleDiagnosingItself`
  hold the four behaviours; the harness preflight was verified by pointing `GUSTY_PYTHON` at a
  `python3` that prints `Python 3.10.12` — the run fails with the remedy line, not with drift.
- Process lesson, and it generalises beyond this bug: **an expectation produced by an external tool
  is a versioned dependency of the test suite.** Recording the version is cheap; the value arrives
  when the check makes a mismatch say "environment" instead of "compiler". The same argument applies
  to the LLVM pin, `cc`, and anything else the corpus consults as an authority.
