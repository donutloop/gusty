# ADR 0186: CPython is the conformance oracle; parity is not enough

Status: Accepted (L11.9)
Date: 2026-09-28
Decides: the shape of the conformance matrix (schema 1.1), the third leg (CPython) and
where it runs, the debt ledger that records every measured divergence, the promotion rule
that turns a fixed probe into parity surface, and two new CLI exit codes (6, 7).

## Context

The conformance matrix compared **two implementations to each other**. Interpreter stdout
== AOT stdout was the whole contract, and 41/41 cases passed. That is exactly the wrong
question, because two implementations that share a misconception agree:

| program | CPython | `--interp` | `--aot` | matrix verdict before this ADR |
|---|---|---|---|---|
| `print(True)` | `True` | `1` | `1` | ✅ pass |
| `print(len("café"))` | `4` | `5` | `5` | ✅ pass |
| `xs = [1,2,3]; print(xs[-1])` | `3` | IndexError | IndexError | ✅ pass |
| `print("abc"[1])` | `b` | `98` | `98` | ✅ pass |
| `print(sorted([3,1,2]))` | `[1, 2, 3]` | ✅ | invalid IR | ✗ (only because it does not compile) |

The roadmap's Phase 11 table was assembled by *manually* running `--interp`, `--aot` and
`python3` side by side, precisely because CI could not answer. Only two test files
(`integration/escapes_test.go`, `integration/division_test.go`) ever invoked CPython, each
with a private helper, and each normalising booleans away (`True` → `1`) so that the
comparison could pass.

Two more things made the situation worse than "a few wrong values":

- **A Go panic in codegen killed the test binary.** `print([1, 2, 3][-1])` panics inside
  `irGen.value`; one such program in a corpus means "the suite crashed", not "row 27 failed".
- **The corpus was a list of names, not a claim.** Nothing recorded *what* a program was
  supposed to print, so "we know this one is wrong" lived in prose in `roadmap.md` and could
  quietly become false (in either direction) without anything noticing.

## Decision

**CPython is the third leg, and a row is only green when the registry's claim matches the
observation — in both directions.**

1. **One classifier, shared by harness and CLI.** `lang.BuildOracleReport`
   (`pkg/lang/oracle.go`) takes the three observed legs and returns
   `match` / `debt` / `not_applicable` plus per-leg `matches_python` flags and notes. The
   integration harness and `gustyc --oracle` both call it, so a program cannot pass in one
   place and fail in the other. Two rules of classification are worth stating because they
   are the whole point:
   - a leg that *did not run* (refusal, trap, compiler panic) **does not match**. A refusal
     is a debt, not a skip — this is ADR 0166's rule expressed in the matrix;
   - `not_applicable` is reserved for "CPython cannot run this source at all". It is a
     declared state with a reason, never a default, and the CLI reports it with its own exit
     code so "we never checked" cannot read as "it matches".
2. **Comparison rules are named, documented, and echoed into the artifact.** Exactly one rule
   is on by default: `set-order`, which compares a bare `{…}` rendering (with no `k: v`
   entry) as a sorted multiset. CPython's set iteration order depends on the hash seed and
   insertion history, so byte-comparing it would pin an accident of one CPython build. Dict
   renderings keep their order — that order is insertion order in both languages and is
   therefore observable. `PYTHONHASHSEED=0` is set for the oracle leg so a run is
   reproducible; the rule list travels in every row so a reader can see what was normalised.
3. **The ledger is the spec.** `integration/conformance_cases.go` declares each case's oracle
   state, its reason, the roadmap item that owns it, and a **pin** per leg: the exact stdout
   that leg produces today, or that it fails (optionally with an error substring, e.g.
   `compiler panic`). A case with *no* ledger row is declared `match` — so a new divergence
   cannot enter the corpus silently, and a new program is expected to be conformant until an
   explained exception exists. `ConformanceCase.OracleCheck` returns the drift list, and the
   harness fails the build when it is non-empty.
4. **Drift is bidirectional.** A row that gets worse fails. A row that gets *better* fails
   too — "oracle debt is paid: both backends now print CPython's answer — update the
   registry". A ledger nobody can be forced to maintain is a fiction, so maintenance is a
   build failure rather than good intentions.
5. **Probes are corpus members with a different contract.** 16 new programs under
   `integration/programs/probe_*.gy` reproduce the Phase 11 rows (nested containers,
   heterogeneous elements, tuples, negative indexing including the compiler panic, code-point
   strings, stdlib constant types, floored `//`/`%`, `sorted`/`enumerate`, calling a function
   through a parameter, `print(set())`, print atomicity). They are recorded in the matrix and
   pinned but not parity-asserted. **Promotion rule:** when a probe's pins stop matching
   because the answers became Python's, the row fails with a message that says to delete the
   ledger entry and move the program from `conformanceProbes` to `conformanceStandalone` — so
   a fixed defect becomes permanent parity surface instead of a deleted TODO.
6. **Nothing in the corpus may crash the harness.** All three legs run behind `recover()`
   (interpreter, codegen, oracle). A compiler panic becomes a row whose aot error is
   `compiler panic: runtime error: index out of range [-1]` — measured, named, owned by L11.8
   — and the other 58 rows still report.
7. **Machine path.** Matrix schema `1.1` adds `python_stdout`/`python_ok`/`python_error`,
   `interp_matches_python`/`aot_matches_python`, the computed `oracle` with its
   `oracle_declared` counterpart, `oracle_reason`/`oracle_ref`/`oracle_rules`/`oracle_notes`,
   the `oracle_drift` list, and matrix counters `rows`/`skipped`/`oracle_match`/
   `oracle_debt`/`oracle_not_applicable`/`oracle_drift`, plus a `toolchain` block naming the
   interpreter and LLVM that produced the artifact. `--schema` gains
   `definitions.oracleReport` and `definitions.conformanceRow`. The ad-hoc form is
   `gustyc --oracle <src>` / `--oracle-file <path>`, `--json` for the report.
   `tools/oracleprobe` prints the three legs for any program or merged group, which is how a
   ledger row is written from data instead of memory.
8. **Two new exit codes** (docs/operations.md § Exit codes): **6** = the program ran and
   printed something other than what CPython prints; **7** = the oracle could not run the
   source, so there is no verdict. Neither may share a code with a compile error (1) or a
   runtime trap (3): a wrong answer, a broken program and a missing check are three different
   events, and an agent has to be able to branch on them.

## What this does *not* claim

- **It fixes nothing.** Every one of the 22 debt rows still prints what it printed before;
  they now print it *measurably*. Seven of the 41 rows that used to *pass* the matrix print
  something other than what CPython prints (`features_a`, `print_args`, `container_methods`,
  `none_values`, `string_containers`, `string_escapes`, `string_params`), and eight more are
  gusty-only surface the oracle cannot run. All fifteen were previously recorded as conformant.
- **The oracle is a specific toolchain, not an abstraction**: CPython 3.12.3 on this machine,
  overridable with `GUSTY_PYTHON`, recorded per artifact. A future CPython may legitimately
  change a rendering; that will show up as drift and be a decision, not a surprise.
- **`not_applicable` is not an escape hatch.** It requires a reason naming the gusty-only
  surface (`await` at module scope, positional set subscript, `string.DIGITS`, `Literal[...]`)
  and, in practice, pins both backends' output anyway.

## Consequences

- The corpus is 59 rows (64 programs under `integration/programs/`, 16 of them probes):
  43 parity cases (unchanged) + 16 probes; 27 `match`, 22 `debt`, 10 `not_applicable`. `TestConformanceMatrix` now fails on drift as well as on parity.
- The oracle leg found something on its first run that no roadmap row contained: **`print`
  writes while it evaluates**. `print("got", twice(21))` where `twice` prints prints
  `got << 21 >>` / `42` where Python prints `<< 21 >>` / `got 42` — both backends, identically,
  and `docs/language.md` had documented the wrong behaviour as intentional ("keeps its place
  in the line, and both backends interleave it identically"). It is now Gap L.5, pinned by
  `programs/probe_print_atomic.gy`, and the doc says what is really true.
- Adding a program to the corpus now costs an assertion, not just a name: conformant by
  default, or an explained exception with an owner. That is the intended friction.
- Debt rows are the queue. Every Phase 11 item (L11.1 remaining, L11.2 bools, L11.3 tuples,
  L11.4 indexing, L11.5 strings, L11.6 numerics, L11.7 functions) now has at least one
  program whose pins have to be rewritten when it is fixed, and the harness says so out loud.

## Alternatives rejected

- **Keep parity as the contract and add oracle tests ad hoc.** That is what existed: two test
  files with private helpers, each normalising away the divergences it could not explain. It
  found the escape-sequence and division bugs and then stopped, because nothing was
  *obliged* to be compared.
- **Make the matrix compare only interpreter-vs-CPython (drop AOT).** Loses the
  shared-lowering contract, which is a real and separate property: two backends must agree
  even when both are wrong (a divergence between them is a compiler bug, not a language
  question). The matrix now asserts both.
- **Free-form expected-output golden files.** Expected-output files record *what gusty
  printed*, which is how goldens become rubber stamps. Here the expected value is CPython's,
  and gusty's current answer is a *pin* whose only job is to detect unrecorded change.
- **Normalise booleans (`True`→`1`) globally, as the two existing helpers did.** It turns nine
  real divergences into silence. Under this ADR they are nine rows with a reason
  ("bools are not values yet"), a reference (L11.2) and pins — and `--oracle 'print(True)'`
  exits 6 instead of agreeing.
- **Assert the matrix counters (e.g. "≥ 27 match")** as a progress ratchet. It would fail
  whenever a probe is legitimately promoted, and it rewards inflating `not_applicable`. The
  bidirectional drift check is the stricter and simpler rule.
- **Run the oracle leg only in a nightly job.** The comparison is ~50 ms per program against
  ~600 ms for the compiled leg; the cost is irrelevant next to the value of never shipping a
  divergence that CI could have seen.
