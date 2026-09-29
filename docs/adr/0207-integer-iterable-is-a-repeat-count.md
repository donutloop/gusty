# 0207. An integer on the right of `for … in` is a repeat count

## Status

Accepted. No compiler change — this ADR declares an existing, backend-agreeing behaviour as a language
feature, and supplies the document, the ledger row and the tests that make it one. Pinned by
`pkg/lang/for_int_test.go`, `integration/for_int_test.go` and `integration/programs/for_int_count.gy`.
Roadmap Gap R.14.

## Context

```gusty
for i in 4:
    print(i)      # 0 1 2 3 — in the interpreter and in the compiled binary
```

`docs/language.md` described only `for … in range(n)`, `range(a, b)` and list-literal iteration. The
integer form had never been written down, yet both backends implemented it identically — the codegen
lowers it to the same private counter loop that `range(n)` compiles to (ADR 0196's `%_ctrN`), and the
interpreter treats an integer iterable as a count.

That combination — real, exercised, undocumented, and divergent from Python — is the worst kind of
surface to have in a compiler whose consumers are both programmers and agents:

- a programmer reading the docs cannot tell whether it is supported, and a reader of a program that uses
  it has to guess;
- an agent generating code has nothing to generate *from*, and an agent reading a corpus program cannot
  tell a feature from a bug that has not been found yet;
- a test suite that never asserts it can "fix" it away: nothing would fail.

The boundaries were unstated too, and boundaries are where these declarations either become real or
embarrass: `for i in 0:`, `for i in -2:`, and `[x for x in 3]`.

Measured, all of it, before writing anything:

| shape | interpreter | compiled | CPython |
|---|---|---|---|
| `for i in 4:` | `0 1 2 3` | `0 1 2 3` | `TypeError: 'int' object is not iterable` |
| `for j in n` (n=3) | `0 10 20` | `0 10 20` | — |
| `for k in 2 + 1:` | `0 1 2` | `0 1 2` | — |
| `for i in 0:` / `for i in -2:` | no iterations | no iterations | — |
| `[x for x in 3]` | `[0, 1, 2]` | refused: *"comprehension iterable must be an inline list literal, range(), or a container variable"* | — |

## Decision

**An integer on the right of `for … in` is a repeat count, and it is now language surface in the
documented, tested, ledger-pinned sense.**

1. `for x in n` binds `0, 1, … n-1`. The count may be a literal, a variable, or any integer expression —
   including a call, `for i in bound():` — and the loop variable behaves exactly like the one the
   `range(n)` form leaves behind (same name, same scope rule).
2. **`n <= 0` runs the body zero times**, the same as `range(0)`: a negative count is not an error and
   not an infinite loop. This is stated rather than left to emerge, because "what does a negative count
   do" is exactly the question a generator cannot answer by inspection.
3. **Divergence is declared, not discovered.** CPython rejects the construct, so
   `programs/for_int_count.gy` sits in the ledger's *gusty-only surface* section with
   `oracle: not-applicable`, and `TestIntegerRepeatCountAgreesOnEveryPath` additionally asserts that
   CPython still refuses it: an excluded oracle is a claim with a test behind it, not an excuse.
4. **Where the two backends genuinely differ, say so in the same breath.** In a comprehension the integer
   form is interpreter-only; the compiled backend refuses with a message that names the alternatives
   (`inline list literal, range(), or a container variable`). That asymmetry is documented as a
   compiled-side limitation instead of being averaged into a vague "supported in both paths".
5. **The construct earns its keep**, so this is not a blessing of an accident: it compiles to the same
   counter loop, and it needs no built-in — which is what makes it the way to write a counted loop in a
   module that has claimed the name `range` for itself (ADR 0205's refusal otherwise pushes people into
   precisely this shape).

## Consequences

- `docs/language.md` § Control flow states the form, the expression-ness of the count, the `n <= 0`
  behaviour, the comprehension asymmetry and the ADR/roadmap pointers; `docs/operations.md` tells an
  agent the same thing plus the generated-code angle.
- The conformance matrix gains a row with an excluded oracle, and the drift harness stays the guarantee
  that "excluded" remains true.
- Measuring the neighbours surfaced the next item rather than resolving it: `for c in "ab":` iterates
  characters in the interpreter, while codegen emits a module that `llc` rejects
  (`store i32 @.str1, i32* %_c` — *"global variable reference must have pointer type"*). Under the
  exit-code contract an LLVM verifier failure is a compiler bug, so that is opened as **R.15** and
  fixed on its own merits: either lower string iteration, or refuse it — but never emit a module that
  does not verify.

## Alternatives considered

- **Remove the integer form and require `range(n)`.** Rejected: it is implemented identically in both
  backends, is already used by corpus programs, and is the only counted-loop spelling that survives a
  program claiming the name `range` (ADR 0205). Removing a working, agreed construct to match Python is
  the same import-the-reflex error ADR 0206 declined.
- **Keep it undocumented because "it works".** Rejected: undocumented surface is how a feature becomes a
  regression — the next refactor with no test pointing at it would change a boundary (a negative count,
  say) and nothing would fail.
- **Make the compiled comprehension path accept integer iterables as part of this cycle.** Rejected as
  bundling: the message it gives today is already actionable and the interpreter path is a superset, so
  the limitation is documented; if someone wants it, it is a codegen feature with its own roadmap entry.
- **Call it a Python compatibility hole and open an oracle-debt row.** Rejected: debt rows are for where
  *we* fall short of Python. Here Python is the one that refuses, which is the ledger's `not-applicable`
  category — the same place positional set subscript and module-scope `await` live.
