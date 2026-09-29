# 0202. A diagnostic is said once

## Status

Accepted. Implemented this cycle in `pkg/lang/semantic.go` (`addDiag`, `addDiagFull`); pinned by
`pkg/lang/diag_dedupe_test.go` and `integration/diag_dedupe_test.go`. Roadmap Gap R.7.

## Context

The checker repeated itself, structurally rather than by accident. Per-call-site return inference
(`inferReturn`, the mechanism behind ADR 0190's mypy-style flow typing) walks a callee's body with
the argument types of the call site, which means every *source-level* fact inside that body gets
re-derived once per call. The scaling was directly measurable:

```gusty
def f(x):
    match x:                      # warning: match is not exhaustive…
        case 1:
            return "one"
```

| call sites for `f` | times the warning was printed |
|---|---|
| 0 | 1 |
| 1 | 2 |
| 2 | 3 |

and in the corpus, five programs (`dispatch_gc`, `dispatch_gc_stress`, `dispatch_nested`,
`gc_precise`, `match_literal`) each emitted a byte-identical line twice.

Two things made this worth fixing rather than merely untidy:

- **The diagnostic list is an agent-facing artifact.** `--json` publishes an array; a consumer
  reasonably reads `len(diagnostics)` as "how many problems does this program have", and may group
  by code or dedupe by identity. That number was wrong, and correct only after a consumer
  re-implemented deduplication — over a format whose entire purpose is to make the old scraping
  unnecessary (ADR 0004).
- **It buried the real message.** The point of a warning at a position is that the position is
  wrong; saying it three times adds no information and makes the one true error harder to see in a
  wall of text.

## Decision

**A diagnostic is a fact about a position, and the analyzer records each fact once.**

1. Every diagnostic goes through one funnel — `addDiag` / `addDiagFull` — keyed by
   `(level, line, column, code, message)`. A fact about a position is either true or it is not; it
   cannot become more true by being said again.
2. **The key is deliberately narrow, and that narrowness is the tested part.** Not collapsed:
   - two *different* messages at one position (a wrong argument type and a non-numeric operand on
     the same line are two findings, and an agent's "fix the first one" plan needs both);
   - the same message at two positions (the count is the useful signal: two wrong calls, not one);
   - the same sentence at two levels, because only an error decides whether the program can run
     (ADR 0164's exit-code contract), and the key carries the level so a warning can never swallow
     an error;
   - the same message with two different codes, since the code is the machine-readable identity
     (ADR 0004/0006) and distinct codes are distinct findings by contract.
3. **Deduplication lives at the source, not at the printers.** Doing it in the CLI or the JSON
   encoder would leave every other consumer — LSP, schema dumps, tests, future emitters — to redo
   it, and would let the analyzer's own view keep lying to anything that reads it directly.
4. **The walk itself is kept, not memoized.** Re-inferring a callee per call site is what makes
   argument-type inference work; skipping it would trade a display defect for a correctness one. The
   defect was in *reporting* the derived facts repeatedly, so that is where the rule goes.

## Consequences

- `len(diagnostics)` is now a count of findings; `--json` output is safe to group, diff and
  deduplicate-by-identity, and the CLI never prints the same line twice.
- The corpus-wide invariant is tested rather than hoped for: `TestWholeCorpusReportsNoDiagnosticTwice`
  walks every program in `integration/programs` and fails if any diagnostic appears twice — the
  automated form of the measurement that found the gap.
- Diagnostic counts in older docs, tests or agent scripts that were written against the duplicated
  output may need to be re-read; no test in the repository asserted a duplicated pair, so the suite
  went green without edits — which is itself evidence the old duplicates were never intentional.
- Gap R.7 stays listed in the roadmap's closed section with this ADR as its explanation, because the
  *cause* — per-call-site re-walking — remains, and is a deliberate trade-off; anyone tempted to
  "optimise" `inferReturn` should read consequence 4 first.

## Alternatives considered

- **Memoize `inferReturn` per `(fd, argTypes)` tuple.** Rejected as the primary fix: it removes most
  repeats for the common single-signature case and keeps the rest, so the invariant would be
  probabilistic instead of structural — and a consumer cannot rely on "usually unique". Worth
  revisiting purely as a performance measure.
- **Dedupe in the CLI/JSON printers.** Rejected: every consumer would need its own copy of the rule,
  and the analyzer's own `Diags` — used by LSP and tests — would stay wrong.
- **Attach the call site to each diagnostic so repeats become distinguishable** (one warning per
  call, mentioning the caller). Rejected for this gap: it invents a new diagnostic surface (a
  source-level fact attributed to a call site) and would multiply output volume for programs with
  many calls; if that information is ever wanted it deserves its own note-style feature, not a fix
  to duplication.
- **Suppress derived warnings when the underlying expression is already an error.** Attractive
  (gap J.3 asks for something like it) but orthogonal: it decides *whether* a finding is reported,
  not whether a reported finding may appear twice. Left to its own roadmap entry.
