# Citations in the record must resolve

## Status

Accepted (cycle 167). Enforced by `integration/docs_citations_test.go`.

## Context

Writing the ADR for the compiled exception-clearing fix (0218) meant re-reading the roadmap entry it
closed. That entry cited two programs as its standing evidence — `probe_raise_in_func` and
`probe_try_return_except`, both written as `.gy` files that were never written — and neither exists in
`integration/programs/`. Sweeping the whole
normative record found seven dangling citations, of three different kinds:

- **a rename nobody followed through**: the corpus file is `empty_set.gy`, the citation says
  `empty_set.gy` cited under `probe_empty_set`; `comprehension_calls.gy` cited under
  `probe_comprehension_call`; `method_function_name_clash.gy` cited under
  `probe_method_function_name_clash`; `probe_operand_types.gy` cited under
  `probe_operator_operand_types`.
- **a historical name kept in prose after the file was promoted**: ADR 0190 describes "two new probes
  (`probe_sort_methods`, `probe_comprehension_call`)" which ADR 0191 records as having become
  `sorting.gy` and `comprehension_calls.gy`. Both sentences are true; read together, one cites files
  that no longer exist.
- **a claim of pinned debt with no file and no ledger row**: Gap R.31 said two `%`-formatting shapes
  "stay pinned as `programs/probe_percent_format.gy`". Nothing of the kind existed — the gap had a
  measured-looking sentence instead of a measurement.

Citations rot this way because they are written once, by hand, from memory, and nothing reads them
again until a cycle trusts one and goes looking.

## Decision

**A citation in the record is a claim that a file exists, and a test enforces it.**
`TestRecordCitationsResolveToRealPrograms` scans `roadmap.md`, `README.md`, `docs/language.md`,
`docs/operations.md` and every `docs/adr/*.md` for corpus citations and fails when one does not name a
file in `integration/programs/`:

- `programs/NAME.gy` must exist, **unless** the citation is immediately followed by the marker
  `(planned)` — the notation for a Definition-of-Done program a roadmap item still owes. A promise
  about the future is not a statement about the corpus, and saying `(planned)` is what lets the rest
  of them be checked.
- A bare `probe_NAME.gy` must exist too: the `probe_` prefix is a claim about this corpus whatever the
  sentence around it says.
- Any other bare `NAME.gy` fails only if it is a *near-miss* of a real file (add/remove `probe_`,
  trailing `s`) — that shape is drift from a rename, so the test reports "did you mean
  `programs/X.gy`?" instead of just "missing". A name with no relative in the corpus is left alone,
  because `import math` talks about a `math.gy` on disk, not about a conformance program.
- `_001_session_learnings.md` is exempt, and said so in the test: it is narrative, and it has to be
  able to discuss a filename precisely in order to report that it does not exist.
- The test also fails if it checked fewer than 20 citations, so that a change in citation style turns
  this into a visible failure instead of a vacuous pass — the same rule that made the property
  generator itself a testable thing.

The three claims that could not be repaired were repaired properly: two were renamed to the files that
exist, one ADR sentence was reworded to name the promoted files, and Gap R.31's invented pin became
real — `programs/probe_percent_format.gy` now exists, is a `debt` ledger row with a reason, an owner
and per-leg pins, and its measured interpreter output (`str % int: TypeError` … plus `7 % 2` and
`-7 % 2` so the probe distinguishes "no formatting" from "no percent at all") is what the roadmap now
describes.

## Agentic rationale

An agent consumes this record the way it consumes `--schema`: as facts it does not re-derive. When the
roadmap says a shape "stays pinned as programs/probe_percent_format.gy", the reasonable action is to
read that file, find nothing, and either re-derive the behaviour from scratch or believe the sentence.
Making citation validity a build check means the record cannot quietly become fiction, which is the
same property the ledger gives the oracle state: declared and verified, not remembered.

## Alternatives rejected

- **Delete the citations and let prose stand alone.** Rejected: they are the parts with the most
  value — a name a future cycle can run, diff and delete when the debt is paid.
- **Scan the session learnings too.** Rejected: that file is the narrative of what was believed when,
  and it legitimately names files in order to say they were missing (this ADR's own origin). The
  normative documents are where a dangling name misleads.
- **Allow any citation whose sentence is future-tense, detected from wording.** Rejected: parsing
  English for tense is not a check, and it would fail in the direction that matters — silently passing.
  An explicit `(planned)` marker is cheap to write and impossible to misread.
- **Fold this into the ledger's drift tests.** Rejected: the ledger verifies each program's *state*;
  this verifies that documents *refer* to programs that exist. Different question, different failure.
