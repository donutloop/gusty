# The bounds check is told which door it stands in

## Status

Accepted — 2026-07-14

## Context

`Gap R.222`'s conformance program had to assert an out-of-range row by **class** rather than by sentence,
because the sentences did not match:

```python
xs = [1, 2]
print(xs[5])    # IndexError: index out of range        — reference: list index out of range
xs[5] = 1       # IndexError: index out of range        — reference: list assignment index out of range
s  = "ab"
print(s[5])     # IndexError: string index out of range — and here it was already right
```

Three doors, three sentences in the reference, two in this backend — and the one it shared between the two
list roads named *no container at all*. The string road beside them was already specific, which is what made
this a defect rather than a style choice: one helper was being precise while the doors next to it were not.
`Gap R.35` established the principle this row applies — a trap reported with the wrong words is a trap
reported wrongly, because the traceback *is* the interface for a program that died.

`normalizeIndex` is where the fault sat. It normalises a negative index, bounds the result and raises
`IndexError` — a job the read and the store road share — so it raised one sentence for both, and neither of
them named the container.

The record leg made this interesting. `pkg/lang/testdata/interpreter-golden-drift.json` held
`IndexError: index out of range` for the list rows: the **retired engine had its own wording**, and the
compiled backend had been matching the engine rather than the reference. `constant_trap_test.go` said so out
loud — its table carried `ours` and `theirs` columns, one pair per row, recording a difference nobody had
closed.

## Decision

**Tell the bounds check which door it is standing in**, and give it the reference's two sentences:

- `listIndexOutOfRange()` now returns `list index out of range`, and the new
  `listAssignmentIndexOutOfRange()` returns `list assignment index out of range`. Both live in
  `constant_trap.go` beside the string sentence, so the family is one table (ADR 0181's shared raise road).
- `normalizeIndex` keeps its signature and delegates to `normalizeIndexFor(b, h, idx, sp, sentence)`. The
  read road passes the read sentence; `assignIndex` passes the assignment sentence. One normaliser, one
  bounds rule, two sentences — the caller's knowledge reaches the raise, which is where it belongs.
- Where the record disagrees with the reference, the reference wins and the record is the debt (AGENTS.md).
  Nine rows — eight in `pkg/lang`, one in `integration` — moved onto
  `testdata/interpreter-golden-drift.json` with `expected` holding the retired engine's wording and `actual`
  the reference's, so the two-way ratchet still fails if the sentence drifts back *or* the divergence
  disappears. The record itself is left untouched: it is evidence about the language, not a thing to edit
  into agreement.
- `constant_trap_test.go`'s `ours`/`theirs` table keeps its two columns and they are now **equal**, which is
  the assertion: the compiled raise and CPython's raise are the same sentence, class and exit code included.

## Alternatives rejected

- **Leaving the record's wording in place.** The cheapest option and the wrong one: it keeps the compiled
  backend agreeing with a retired engine's prose instead of with the language, and the drift ledger's whole
  purpose is to make that choice visible rather than silent.
- **Naming the container inside `normalizeIndex` by inspecting the handle.** The normaliser has an i32 and a
  span; the door has the expression and knows whether it is reading or writing. Putting the knowledge where
  it is cheapest to obtain is the same argument ADR 0329 made for gating an index before it is lowered.
- **One sentence with the container appended at the raise site.** Two call sites build their own raise for a
  job one function does, which is how a trap ends up with two wordings — the failure mode this file's own
  header warns about.

## Consequences

- `docs/operations.md`'s artifact table and `roadmap.md`'s snapshot moved to 340 + 22 ledger rows, both
  recomputed from the files by `TestTheArtifactTableCountsAreTheFilesOwn` rather than typed.
- The out-of-range rows in `integration/programs/list_index_kind.gy` now print the reference's sentence —
  the row that owed the words is closed, and the program stopped being able to assert only the class.
- Slices, `pop` (`pop index out of range`) and the string road are untouched; they were already the
  reference's.
