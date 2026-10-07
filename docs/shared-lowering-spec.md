# Lowering Spec

This document is the lowering contract for the **one** gusty backend:

- the **LLVM AOT compiler** (`lang.Compile`, `pkg/lang/codegen.go` + `pkg/lang/closure.go`), run
  in-process by the LLVM JIT (`pkg/lang/jit_llvm.go`) or linked and run by `cc`.

The AST interpreter that used to be the second half of this contract (`lang.EvalExpr`,
`pkg/lang/jit.go`) is **retired** — deleted by ADR 0302, with `--interp` a usage error and no
`EvalExpr` to call. What the second implementation used to prove is now proved by the two
**witness legs**, and the wording of every claim below names them:

| Witness | The claim | Where it lives |
|---|---|---|
| **the record leg** | the compiled answer equals the answer the retired engine recorded for that source | `pkg/lang/testdata/interpreter-golden.json`, read by `pkg/lang/golden.go` |
| **the reference leg** | the compiled answer equals CPython's | `gustyc --oracle`, `integration/conformance-matrix.json` |

The two implementations could **drift**; a single implementation can only **disagree with a
witness**, which is a weaker and different thing. The conformance matrix (see
`integration/conformance_test.go` and `integration/conformance-matrix.json`) and the two drift
ledgers (`pkg/lang` and `integration` each hold an `interpreter-golden-drift.json`; CPython
disagreements are in `integration/testdata/cpython-debt.json`) are the mechanical checks for that.

## The contract

For every whole-program case, compiling the source, writing the IR, running `llc` and `cc` and
executing the resulting native binary must print **what CPython prints** for the same source; and
where a program has a recorded answer, the compiled run must agree with it or the disagreement must
be a row in a ledger, never a silent pass.

```
artifact(src).stdout  ==  CPython(src).stdout        # the reference leg
artifact(src).stdout  ==  record(src).stdout         # the record leg, or a ledger row
```

The matrix asserts the reference leg for every case in `integration/conformance_cases.go` and
records the observed outputs so a failure is visible, not silently accepted. `pkg/lang/golden.go`
asserts the record leg for the unit cases, and **a missing record fails the case** — so a record
cannot be deleted to make coverage disappear. There is no engine-vs-engine equality left to assert,
and nothing that compares an artifact with itself: a check whose operands are the same expression is
not a check (ADR 0308 deleted three such leftovers).

## Value model

The compiled backend implements one small dynamic value model; the middle column is the retired
engine's, kept because the record leg is stated in its terms.

| kind      | retired engine (record leg) | the compiled backend (codegen) |
|-----------|-----------------------------|--------------------------------|
| int       | `int64`                     | `i32` (signed)                 |
| float     | `float64`                   | `double`                       |
| bool      | `0`/`1`                     | `i1`/`i32 0|1`                 |
| none      | `None`                      | sentinel `0`                   |
| string    | `*str` (heap)               | `%str` global + i8*            |
| list/dict/set | heap object handle      | `%obj`-tagged runtime heap     |

Numeric promotion follows Python-style gradual rules: mixing `int` and `float`
in a binary op promotes to `float` (`sitofp`), and `int(f)`/`float(i)` are the
explicit conversions. Integer division is floor (`//` -> `sdiv`), modulo is the
remainder (`%` -> `srem`), `**` is exact binary exponentiation (folded for
literals, `@llvm.pow.f64` for runtime values).

## Evaluation semantics

A program is a statement sequence evaluated in order, and `print` writes to stdout. The `print`
builtin is the observable contract point: each argument is rendered by the module's one tag-reading
printer (`rt_str_of_value` → `rt_print_mixed_value`, ADR 0303), and each argument is followed by a
newline.

| construct        | the compiled backend                                                       |
|------------------|----------------------------------------------------------------------------|
| `if`/`elif`/`else` | `br` on `icmp`                                                            |
| `while` + `else`   | the `else` runs iff the loop exits normally                                |
| `for x in range(n)`| `rt_range`/unroll, step and negative handled                               |
| `break`/`continue` | `br` to exit/continue blocks                                               |
| `def` + call       | function `alloca` + env closure (nested closures: an open gap, see below)  |
| default/keyword args | bound at call                                                            |
| generators/`yield`  | lowered for the shapes the roadmap lists as answered; the rest refuse     |

## What this backend does not answer (and who owns it)

There is no "shared surface" left to divide the corpus by: one backend answers a program or it does
not. A construct the compiler cannot lower is **a gap with a roadmap row**, never a silent zero and
never a fallback to an engine that no longer exists — the refusal has to name the missing door, say
what the reference does there, and cite the row (ADR 0166, and the suite's `refusesHonestly` rule in
`docs/operations.md` §"The suite's own interface").

The standing examples live in `docs/operations.md` §"One backend, and what it does not lower"
(nested closures, higher-order calls, run-time iteration and `sorted` over a computed container,
`dict(<container>)` copies); the older entries above — generators, non-identity decorators — have
since been lowered, which is why they are named by their roadmap rows rather than here.

## Machine-readable matrix

`lang.ConformanceMatrix` (`pkg/lang/conformance.go`) is the JSON schema for the
emitted artifact (schema `1.1`, ADR 0186). A row records, for one case, the
interpreter stdout, the AOT stdout **and the CPython stdout**, whether each leg
ran clean, the `parity` flag, the per-leg `*_matches_python` flags, the computed
`oracle` verdict with the `oracle_declared` claim it was checked against, the
`oracle_reason` / `oracle_ref` / `oracle_rules` / `oracle_notes` / `oracle_drift`
fields, and the per-leg pins that hold a debt row to its recorded wrong answer.
The matrix header names the toolchain that produced it (`toolchain.python`,
`toolchain.llvm`), so a claim about Python is a claim about a specific interpreter.
The artifact is deterministic for a given registry + toolchain, so a script can diff
two runs to detect a **new** drift; `--schema` → `definitions.conformanceRow` /
`definitions.oracleReport` document the shapes for agents.

## Keeping the matrix green

When adding a language construct:

1. Add a whole-program case (single-file or merged) to
   `integration/conformance_cases.go` covering it — as a *parity* case, not a probe.
2. Implement it in the one backend (`Compile`), judged on both legs — the record and the
   reference. There is no second engine to make agree, and no `EvalExpr` to call.
3. Run `go test ./integration/ -run TestConformanceMatrix` — parity must stay green **and**
   the new row must come out `oracle: match`. If it does not, either fix it or write a ledger
   row (reason + roadmap ref + a pin per leg, taken from `go run ./tools/oracleprobe <name>`,
   never from memory). A case with no ledger row is declared `match`, so "nobody compared it to
   Python" is not a state the matrix can represent (ADR 0186).
4. Reproducing an existing defect instead of adding a feature? Add it under
   `integration/programs/probe_*.gy` and register it in `conformanceProbes()` with a debt row.
   When it is fixed, the harness fails with "oracle debt is paid" and the program gets promoted
   into `conformanceStandalone()` — that promotion is the definition of done.
5. If the construct is intentionally backend-only, leave `shared` false and
   document the divergence here.

## async / await (L5.6, minimal synchronous-coroutine model)

Both backends accept `async def`, `async for`, `async with`, and `await expr` as
first-class syntax. `async`/`await` are lexed keywords; `async` sets the `Async`
flag on `FuncDef`/`ForStmt`/`WithStmt`.

Because the language has no suspension primitives yet (no I/O, no sleep), a
coroutine completes immediately, so the shared lowering is:

- `async def f(...)` — lowered exactly like `def f(...)` (the `Async` flag is
  informational until Phase 7).
- `async for x in it:` — lowered exactly like `for x in it:`.
- `async with m as x:` — lowered exactly like `with m as x:`.
- `await e` — reduces to `e` (awaiting an immediately-completing coroutine
  yields its value).

This gives byte-for-byte parity between the AST interpreter and the LLVM AOT/JIT
backends (see conformance `async_basic.gy`). The cooperative event-loop runtime
(coroutines as state machines, async iterator/context protocols, a first-class
`AwaitExpr`) is Phase 7 (L7.1).
