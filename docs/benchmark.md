# Benchmarking & profiling

gusty ships a benchmark + profiling harness that runs one source program
through **both** execution backends — the AST interpreter and the in-process
AOT JIT — and reports structured wall-clock numbers. It exists to support the
"compiler is the product" principle: on your own machine you can see how much
faster the AOT-compiled artifact is than the interpreter for a given workload.

## CLI

```sh
gustyc --bench 's = 0
for i in range(50000):
    s = s + i
print(s)
' --bench-runs 3 --bench-opt 2
```

Flags:

| Flag | Meaning |
|------|---------|
| `--bench <src>` | benchmark a source string |
| `--bench-file <path>` | benchmark a source file |
| `--bench-runs <n>` | runs per backend (default 3) |
| `--bench-opt <n>` | AOT optimization level (default 2) |
| `--json` | print the full `BenchResult` as JSON |

Human output shows an interpreter phase profile (parse / analyze / exec) and,
for each backend, total / mean / best wall-clock milliseconds, plus the
speedup ratio (`interp best / aot best`; `> 1` means AOT is faster).

## Semantics

`lang.Benchmark(src, runs, optLevel)`:

- Parses and type-checks the source once; semantic errors are rejected with
  diagnostics.
- Measures the interpreter through `runs` fresh evaluators (wall clock).
- Compiles the AOT artifact **once** (LLVM IR → `llc` → `cc` shared object),
  dlopens it **once**, and calls its generated `main` `runs` times. The shared
  object stays loaded across all runs, so the AOT number is *warm execution*,
  not build time.
- `Speedup = interpreter.BestMs / aot.BestMs`.

## JSON schema

`BenchResult`:

```json
{
  "source": "...",
  "runs": 3,
  "opt_level": 2,
  "interpreter": { "total_ms": ..., "mean_ms": ..., "best_ms": ... },
  "aot":         { "total_ms": ..., "mean_ms": ..., "best_ms": ... },
  "speedup": 12.5,
  "profile": [ { "phase": "parse", "ms": ... }, ... ]
}
```

## Tests

- `pkg/lang/bench_test.go` — unit coverage: the compiled backend, runs clamping,
  compile-error rejection, runtime-error tolerance.
- `integration/bench_test.go` — end-to-end: the compiled backend reports, and AOT
  beats the interpreter on a hot numeric loop (`Speedup > 1`).

## Benchmark suite + regression gate (L10.4)

One program at a time is a measurement; a corpus is a regression detector.
`--bench-suite` measures a fixed corpus of compute-heavy programs on **both**
execution backends (AST interpreter and AOT), prints a table, and emits one
stable JSON artifact. `--bench-baseline` gates a run against a saved baseline so
"the compiler got slower" shows up as a number and a failing exit code.

```sh
# measure the corpus
gustyc --bench-suite --bench-runs 5 --bench-opt 2

# the integration/ parity programs double as benchmark cases
gustyc --bench-dir integration/programs --bench-runs 3

# record a baseline, then gate against it
gustyc --bench-suite --bench-runs 5 --bench-baseline-update benchmarks/baseline.json
gustyc --bench-suite --bench-runs 5 --bench-baseline benchmarks/baseline.json; echo $?
```

Flags:

| Flag | Meaning |
|------|---------|
| `--bench-suite` | benchmark the built-in corpus |
| `--bench-dir <dir>` | benchmark every `*.gy` in a directory (sorted by name) |
| `--bench-runs <n>` | runs per backend; the *best* run is the reported number |
| `--bench-opt <n>` | AOT optimization level |
| `--bench-baseline <path>` | gate this run against a saved baseline (exit 5 on a regression) |
| `--bench-baseline-update <path>` | write the measured suite to `<path>` as a baseline |
| `--bench-gate <leg>` | `aot` (default), `interpreter`, or `both` |
| `--bench-tolerance <f>` | slowdown multiplier that counts as a regression (default 1.5) |
| `--bench-min-ms <f>` | noise floor: baseline times below this are never gated (default 0.25) |
| `--json` | print the suite artifact (suite + `regressions` + `new_cases` + `exit`) |

Human output:

```
benchmark suite: 8 cases, 5 runs, AOT opt=2
  case                    interp ms     aot ms  speedup
  class_dispatch             83.691      1.341   62.42x
  ...
  TOTAL                     456.720      3.390  161.36x  (geomean)
  gate: benchmarks/baseline.json leg=aot (tolerance 1.50x, noise floor 0.25 ms)
  REGRESSION class_dispatch/aot: 0.82 ms -> 1.34 ms (1.63x)
            aot best time for "class_dispatch" is 1.63x the baseline (0.82 ms -> 1.34 ms, tolerance 1.50x); ...
```

### What the gate does and does not claim

* **Best-of-N, not mean.** Each case is run `--bench-runs` times and the fastest
  run is the number: on a shared machine the floor is the only stable statistic.
* **The AOT leg is gated by default.** That leg *is* the compiler's performance
  contract. The tree-walking interpreter swings tens of percent run to run (GC
  and allocation churn), so gating it would read as noise; pass
  `--bench-gate both` if you want it anyway.
* **A noise floor, not a grace period.** A case whose *baseline* time is under
  `--bench-min-ms` is never gated — at that scale you are measuring the OS, not
  the compiler. Corpus cases are deliberately sized (millions of operations) so
  the interesting ones sit well above the floor; a case that LLVM folds into a
  constant is measuring nothing and is excluded exactly as it should be.
* **A new case is not a failure.** A suite case with no baseline row is reported
  under `new_cases` with a suggestion, and does not fail the gate.
* **Exit codes are part of the contract.** `0` clean, `5`
  (`exitBenchRegression`) when the gate fires, `1` for a tooling error such as a
  missing or malformed baseline.

Machine path: `lang.BenchmarkSuite(cases, runs, opt)`,
`lang.BaselineFromSuite`, `lang.SaveBenchBaseline` / `lang.LoadBenchBaseline`,
`lang.CompareBenchSuite(suite, base, tolerance, minMs, gate)`, and the
`benchSuite` / `benchRegression` / `benchBaseline` definitions in
`gustyc --schema`. The artifact is deterministic: cases are sorted by name, a
case that fails keeps its row with an `error`, and times are rounded to two
decimals so two runs diff cleanly.

See ADR 0162.
