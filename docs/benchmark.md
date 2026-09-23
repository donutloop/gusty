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

- `pkg/lang/bench_test.go` — unit coverage: both backends, runs clamping,
  compile-error rejection, runtime-error tolerance.
- `integration/bench_test.go` — end-to-end: both backends report, and AOT
  beats the interpreter on a hot numeric loop (`Speedup > 1`).
