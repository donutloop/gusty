# A set counts distinct members, and a `dict.get` answers with the kind its slot has

## Decision

Two compiled-leg wrong numbers at exit 0, both invisible to the two-backend parity matrix because the
interpreter answered every shape correctly, both found by the oracle leg (roadmap `Gap R.181` and
`Gap R.180`, owner L11.1):

**A set literal's member count is the number of DISTINCT values, not the number the source spelled.**
The static set global reserved one slot per source element, so `len({1, 2, 2, 3})` answered **4** and
`len({1, 1, 1})` answered **3**. `setLiteralElems` now deduplicates in insertion order and `emitSet`
derives the global's count field from the deduplicated slice, so a global's length and its elements can
no longer disagree.

**A `dict.get` answers with the kind of the slot it read** (or of the default it fell back to). The fold
returned the *word* the value is stored as, and print rendered that word:

| program | reference | `--aot` before | cause |
|---|---|---|---|
| `print({1: "a"}.get(1))` | `a` | `0` | interned text's index through `%d` |
| `print({"k": None}.get("k"))` | `None` | `0` | the void word printed, not rendered |
| `print({1: True}.get(1))` | `True` | `1` | the verdict word printed, not rendered |
| `print({1: True}.get(9, True))` | `True` | `1` | **also on `--interp`** — the default took an unboxed path the hit did not |
| `print({1: 2}.get(1))` | `2` | `2` | the int case, which already worked and hid the rest |

## The agentic rationale

`len({1, 2})` answers `2` on every implementation, so a set that counts duplicates is invisible until
someone writes a **duplicate** — a corpus of correct-looking programs cannot find this bug, and neither can
a differential test that never repeats an element. Likewise `get` on an int-valued dict always looked fine.
The two rows are the argument for a sweep that deliberately includes the degenerate shape (a repeated
element, a missing key, a default argument), and for the oracle leg that runs it.

The interpreter half (`{1: True}.get(9, True)` → `1`) is the one the sweep caught that a hand-written list
of forty shapes missed: the two-backend matrix compares interp against aot, and those two **agreed** —
both printed `1`. Only CPython on the same file said `True`.

No answer was traded for a refusal anywhere in this change. A float slot (`{1: 1.5}.get(1)` → `1`) is the
same class of bug and remains **wrong on the compiled leg**, pinned by a test that logs rather than passes
silently, because the honest move is Gap R.105's tag work, not a new refusal that would also take the
interpreter's correct answer away from `--aot` users.

## Codegen / IR implications

- **One lookup, the fold's own.** `g.dictFoldSlot` is now asked by the text road (`stringVal`), the void
  road (`isNoneExpr`) and — through the pure `dictFoldSlotOf` — the verdict road (`IsBoolExpr`). The first
  draft hand-copied the key scan into `stringVal`, and a test that counts duplicate slot scans caught it.
  Key-scanning code that exists twice is how the fold and the printer drifted apart in the first place.
- **The verdict half belongs in the shared pure predicate**, `BoolEnv.callReturnsBool`, not in a print-road
  special case: the interpreter and the compiler must answer the same question, or a REPL echoes `True`
  beside a binary echoing `1` (ADR 0257's rule, ADR 0289's table).
- **The interpreter's `get` had two paths and only one boxed.** A dict literal boxes its slots with
  `slotVal`; the default argument was returned raw, so a verdict default lost its box. The fix is one call
  to the same helper the builder uses — the hit and the miss must produce values of the same kind.
- **The set global's count field is load-bearing.** `{i32, [N x i32]}` carries its own length and `len`
  loads it; fixing `setLiteralElems` while leaving `n := len(sl.Elems)` in `emitSet` would have produced a
  global that *prints* 3 members and *counts* 4. The IR-shape test asserts the emitted array literally.

## Alternatives rejected

- **Deduplicate at print time** (render a distinct count, keep the global as-is). Rejected: `len`, `==`,
  `in` and the printer would each need the same fix, which is four places to forget.
- **Answer `0`/`1` and let `print` special-case `get` per kind.** Rejected: that is what shipped, and it is
  how four kinds became four separate wrong numbers. The kind belongs to the *slot*, so one lookup that
  returns the slot answers all four.
- **Refuse `dict.get` on a non-integer slot** to make the wrong numbers disappear. Rejected: the interpreter
  answers them, the reference answers them, and ADR 0215/0166's ladder forbids trading a working answer for
  a refusal — which is why the float case stays a logged wrong number on an open row instead.
- **Fold the set literal into a heap object at every use** (which would deduplicate via `rt_set_add`).
  Rejected as a perf regression for the common no-duplicate case, and unnecessary: the static path is
  correct once the builder deduplicates.

## Related

`Gap R.180`, `Gap R.181`, `Gap R.105` (the float half, still open), `Gap R.174`/ADR 0291 (the void),
ADR 0257/0289 (a verdict prints a verdict word), ADR 0166 (exit codes), ADR 0187 (payload–tag pairing),
`Gap R.179`/ADR 0294 (the sibling compiled-only wrong number).
