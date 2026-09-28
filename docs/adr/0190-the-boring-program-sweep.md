# The boring-program sweep: every feature needs its least interesting program

## Context

Three of this project's worst defects were found by probing *interesting* shapes, and one by
accident. `print(True)` → `1`, `len("café")` → `5`, `"abc"[1]` → `98`, `xs[-1]` trapping, the
stale-`@estr` `{(null), (null)}` — all exotic-ish. Then, one cycle after the CPython oracle landed,
I wrote twelve programs shaped like a tutorial and five of them diverged:

| program | interpreter | compiled | CPython |
|---|---|---|---|
| `print([square(x) for x in range(5)])` | `[0, 1, 4, 9, 16]` | **refuses**: "comprehension element must be constant" | `[0, 1, 4, 9, 16]` |
| `xs.sort()` / `xs.reverse()` | raises "no such list method sort" | **`string method sort on non-constant string`** | sorts |
| `sorted(xs)` on a variable | `[1, 2, 3]` | refuses: "sorted: codegen folds only an inline list literal" | `[1, 2, 3]` |
| `greeting + ", " + name` in a function | works | refuses (Gap J.5, known) | works |
| `print(True)`-style bool results | `1` | `1` | `True` |

Two of those were already-ledgered causes showing new faces. Two were new, and one of them — the
AOT diagnostic calling `xs.sort()` a *string method* — is a bug in the error message rather than in
the language. The corpus had `print` with a mixed container tested seventeen ways and `print([1, 2])`
not at all; the latter crashed the compiler (ADR 0188).

The pattern is not that the probes were bad. It is that a corpus grown from bug reports and
roadmap items inherits their shape: it tests what we already knew to doubt. The oracle leg made
cheap what had been expensive — running an arbitrary program against CPython is one command — and
the unglamorous programs are where the payoff was.

## Decision

**1. A standing rule: every feature ships its most boring program to the corpus, not only its
most interesting one.** For each language surface there must be a program that a tutorial would
contain: print a list literal, sort a list, index a string, build a list with a call inside a
comprehension, print an empty container. Interesting shapes are *additional* rows. This is a rule
for corpus growth, not a test-coverage metric — the point is that the boring program is written by
someone with no reason to doubt it, which is precisely when a wrong answer is invisible.

**2. Sweeps are a first-class cycle type, and their output is a ledger row.** This cycle's diff is
corpus + ledger only: two new probes (`probe_sort_methods.gy`, `probe_comprehension_call.gy`), each
with a reason, a roadmap owner, and per-leg pins of what both backends currently do — including
the *diagnostic text* (`Err: "string method sort"`, `Err: "comprehension element must be
constant"`), so a future cycle that changes the message without fixing the feature trips the drift
check, and a cycle that fixes the feature has to delete the pin. A sweep that only produced
prose findings would have lost all of this.

**3. A wrong-family diagnostic is itself a defect worth pinning.** `xs.sort()` on the AOT path
answers `string method sort on non-constant string`: the call fell into the string-method dispatch,
so the user is told their list is a string. AGENTS.md makes diagnostics part of the interface, so
that row is recorded as debt against the diagnostic, not only against the missing method.

**4. Where both backends fail identically, the probe still carries the CPython column.** For
`xs.sort()` both legs fail — the interpreter raises, the compiler refuses — and the row is
nonetheless `debt` with CPython's answer as the expected column, because "both of ours agree" is
the state this project has already been burned by.

## Alternatives rejected

- **Fix on sight.** Both sweep findings are real features (a runtime sort; a runtime comprehension
  path); landing them inside a conformance commit would bundle two features and hide the ledger
  change in a code diff. The sweep's job is to make the gaps durable and cheap to pick up; the next
  cycle's job is to close them one at a time.
- **Track sweep findings in the roadmap prose only.** Prose rots; a pinned probe cannot silently
  become stale, and it fails the build in both directions. The roadmap rows added here point at the
  probes rather than restating them.
- **A "random program" generator over the corpus.** Property-testing the surface is worthwhile
  (there is a seeded cross-backend proptest already) but it optimises for shapes a generator can
  imagine. The boring tutorial program is the one neither a bug report nor a generator produces.

## Consequences

- Corpus: 65 rows, 48 parity, 0 fail; oracle 32 `match` / 23 `debt` / 10 `not_applicable`, 0 drift.
- Two named, pinned next-cycle items, both in the L11.7 "functions are values that compile"
  cluster: sorting/reversing a container (runtime sort for ints and interned strings, plus
  `sorted()` on a variable, plus the diagnostic family), and a comprehension whose element is a
  call.
- The sweep harness itself is not new tooling: `tools/oracleprobe` prints the three legs for any
  program, which is what made twelve programs a ten-minute exercise. If a cycle cannot run a
  program on three legs in one command, that tooling is the first thing to build.
