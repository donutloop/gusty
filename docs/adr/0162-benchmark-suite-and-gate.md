# ADR 0162 — Benchmark suite as a first-class artifact + regression gate (L10.4)

## Status
Accepted

## Context
`lang.Benchmark` could already measure *one* program on both execution backends
(AST interpreter, AOT shared object) and report `speedup`. What the roadmap
actually asks for in L10.4 is different: "`integration/` parity programs become
benchmark cases (interp vs AOT vs JIT) so regressions surface as **numbers**."

Before this round, answering "did the compiler get slower?" required a human to
run a program, remember a number, and compare it later. That answer does not
survive contact with an agent or CI:

* timings were only available for a source handed to the CLI on the command
  line — there was no corpus and no artifact;
* nothing recorded a reference, so a slowdown was invisible unless somebody
  happened to notice;
* there was no exit code that meant "the compiler regressed", so a script could
  not distinguish it from "the program is broken";
* and any naive gate over wall-clock time is **lying**: measurements at the tens
  of microseconds scale swing by 2x between processes, and the interpreter leg
  swings even more because it allocates.

The interesting design work is therefore not measurement (a stopwatch around a
loop) but making the numbers *trustworthy and machine-consumable*: what to
measure, how to compare, and when to refuse to conclude anything.

## Decision
1. **A suite is an artifact, not a terminal transcript.** `pkg/lang/bench_suite.go`
   defines `BenchSuite` (schema-versioned, `generated_by`, runs, opt level,
   cases, totals) rendered by `JSON()`. Cases are sorted by name, times are
   rounded to two decimals, and a case that fails keeps its row with an `error`
   string — a suite that silently drops cases would make a regression
   indistinguishable from a capability gap. `BenchDir(dir)` loads every `*.gy`,
   which is how the integration parity programs become benchmark cases;
   `BenchCorpus()` ships a built-in corpus so the feature works from a clean
   checkout.
2. **Corpus cases must resist the optimiser.** A case whose work LLVM folds into
   a constant measures the optimiser's calendar, not its speed (the first
   `loop_sum` case reported `0.000 ms`). Cases use data-dependent work — heap
   lists, class dispatch, recursion, modulo arithmetic — so both backends do the
   work. `TestBenchCorpusLowers` asserts every shipped case runs on both
   backends, so a case cannot rot into a no-op.
3. **Program stdout is silenced while measuring** (`silenceStdout`, `/dev/null`):
   corpus programs print, and terminal writes are not the quantity under test.
4. **Best-of-N is the only statistic that survives a shared machine.** Each case
   runs `--bench-runs` times per backend and `best_ms` is the reported number;
   the AOT leg calls the already-`dlopen`'d `main` so build cost is excluded
   (warm execution, not compile time).
5. **The gate watches the AOT leg by default.** The AOT leg is the compiler's own
   performance contract; the tree-walking interpreter's timings swing by tens of
   percent (GC, allocation churn), so an interpreter-leg gate would report
   scheduler noise as a regression. `--bench-gate both` opts in to both legs.
   Interpreter numbers are always *reported* — they just do not fail a build.
6. **A tolerance plus a noise floor, never a tolerance alone.** A case is a
   regression when `current > baseline × tolerance` **and** its baseline time is
   at or above `--bench-min-ms`. Refusing to judge sub-millisecond cases is the
   difference between a gate people keep and a gate people mute. Defaults:
   tolerance 1.5, floor 0.25 ms, gate `aot`.
7. **New cases are information, not failure.** A suite case with no baseline row
   appears in `new_cases` with a suggestion; adding a benchmark must never
   require editing a baseline first.
8. **A dedicated exit code.** `exitBenchRegression = 5` (documented in
   `docs/operations.md` alongside the existing contract, ADR 0006). `0` clean,
   `1` tooling error (missing/malformed baseline, unreadable directory), `5`
   regression. Anything else conflates "slow" with "broken".
9. **Every regression carries a `suggestion`** naming case, leg, both times, the
   ratio and the tolerance, and saying how to resolve it (re-measure with more
   runs, or `--bench-baseline-update` if the change is intended) — the same rule
   the type checker follows for diagnostics: an agent must be able to act on the
   record alone.
10. **Machine path all the way down.** `lang.BenchmarkSuite`,
    `lang.BaselineFromSuite`, `lang.SaveBenchBaseline`/`LoadBenchBaseline`,
    `lang.CompareBenchSuite(...)`; `gustyc --json` prints the suite plus
    `regressions`/`new_cases`/`exit`; and `benchSuite`, `benchCaseResult`,
    `benchReport`, `benchRegression`, `benchBaseline` are defined in
    `gustyc --schema`.

## Alternatives rejected
- **Compare mean instead of best.** The mean absorbs scheduler and GC noise into
  the result; best-of-N is the only statistic stable enough to gate on.
- **Gate on the interpreter leg too, by default.** Made the suite fail roughly
  as often for noise as for real slowdowns (observed swings of 1.4–1.7x on
  identical code at `--bench-runs 2`).
- **Commit a baseline file to the repository.** Wall-clock numbers are
  machine- and load-dependent; a committed baseline would fail on every other
  machine and be muted within a week. Baselines are generated per machine/CI
  image with `--bench-baseline-update` and treated as configuration.
- **Fixed iteration counts derived from the conformance programs.** The parity
  programs are tiny and mostly foldable; measuring them produces sub-floor
  numbers. `--bench-dir` still allows it, while the shipped corpus is sized to
  sit above the floor.
- **Statistical testing (e.g. Mann-Whitney over many runs).** Right for a
  dedicated benchmarking rig, wrong here: it multiplies suite time and still
  needs a floor; a coarse tolerance plus best-of-N catches real regressions at
  acceptable cost.
- **Emitting timings only to stderr/human format.** Breaks the agentic contract
  (ADR 0004): a number an agent must scrape from prose is not an interface.
