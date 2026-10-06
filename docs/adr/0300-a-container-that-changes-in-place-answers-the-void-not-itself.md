# A container that changes in place answers the void, not itself

## Decision

A receiver method that **mutates its container and returns nothing** answers `None` on both engines
(roadmap `Gap R.187`, ADR 0300). Until now the interpreter handed back the receiver — and the compiled
leg handed the print door *no value at all*:

| program | reference | `--interp` before | `--aot` before |
|---|---|---|---|
| `print(xs.append(2))` | `None` | `[1, 2]` (exit 0) | `printf(i8* @.fmt1, i32 )` → **exit 2** |
| `print(s.add(2))` | `None` | `{1, 2}` | **exit 2** |
| `print(s.discard(1))` | `None` | `set()` | **exit 2** |
| `print(s.remove(1))` | `None` | `{2}` | refuses |
| `print(s.clear())` | `None` | `set()` | refuses |
| `print(xs.sort())`, `print(xs.reverse())` | `None` | ✅ already right | ✅ |
| `print(xs.pop())` | `2` | ✅ `2` | ✅ `2` |

The mutation was never broken. Only the answer the statement throws away was — which is exactly why
this survived contact with users: a program writes `xs.append(2)`, not `print(xs.append(2))`.

## The agentic rationale

Two failure classes in one line, and the loud one was not the loud-looking one. The interpreter's
`[1, 2]` at exit 0 is a wrong answer that no exit code, no diagnostic and no parity check reports —
`sum([1, 2, 3].append(4))` answering **10** is worse still, because a program can *depend* on a value
the reference refuses to produce. The compiled leg's exit 2 is at least honest about being broken. Both
go.

The rule that makes this discoverable rather than a list of patches: **a void must be lowered as the
void, and recognised as the void by the one predicate print, `str()` and the REPL already consult**
(`isNoneExpr`). Before, the call road lowered a mutation to "no value" and the print road assumed
whatever came back was a number. Two roads each half-right, producing invalid IR. Now the mutation
table is a single `map[string]bool` that both roads read, so a mutator cannot be lowered as a void and
printed as a value.

## Codegen / IR implications

- **`printf(i8* @.fmt1, i32 )` — a call with a missing operand.** The `append` road returns
  `("", nil)`: correct for the *statement* `xs.append(2)`, whose value nobody wants, and a silent
  malformed instruction for the *expression*. `llc-20` rejects the module, and ADR 0166 makes exit 2
  the forbidden class. A textual emitter has no type system to catch "I emitted an operand slot and
  filled it with nothing"; the verifier is the only net, and it runs a process away.
- **Void lives in `isNoneExpr`, and that is the only reason this is one fix.** `isNoneExpr` already
  knew `NoneLit`, None-valued names, functions whose bodies return nothing, all-None `min`/`max`, and
  a folded `dict.get` miss (ADR 0291). A mutator is the same fact about a different call shape. The
  print road evaluates the argument before writing `None`, so `print(xs.append(f()))` still runs `f()`
  and interleaves its output correctly.
- **The table is keyed on the call's shape, not its name.** `inPlaceMutations` holds the names, but
  `callIsInPlaceMutation` fires only for an attribute call whose receiver *is* a container — a literal
  by `exprIsContainerShape`, or a name the container records know. A user class with a method named
  `append` answers whatever its own body returns, and `TestAUserMethodNamedAppendKeepsItsOwnAnswer`
  pins that. The alternative — name-only dispatch — is how `xs.sort()` used to be diagnosed as a
  *string* method (ADR 0191's note).
- **`pop` and `popitem` are not mutators in this sense.** They remove *and answer with* what they
  removed; `while xs: x = xs.pop()` is the idiom that proves it. Sweeping them into the void table
  would be a plausible-looking regression, so `TestPopStillAnswersWithWhatItTook` is written to fail
  if that happens.
- **The interpreter's `return recv, nil` was load-bearing for nothing.** The comment said "returns the
  (updated) list handle, so the REPL can show the resulting list" — but the REPL echoes the value of
  the final `ExprStmt`, which in the reference *is* `None` for that program. Convenience was buying a
  language-level wrong answer, and no test depended on it except one that had baked the wrongness in.

## Alternatives rejected

- **Keep returning the container and special-case `print`.** Rejected: the answer is wrong everywhere,
  not just at the printer — `sum(...)`, `len(...)`, a binding, an argument all take it as a value. The
  REPL convenience is one call site of a value that should not exist.
- **Match the compiled leg down by refusing on the interpreter too.** Rejected on ADR 0298's ground:
  the ladder forbids trading a correct answer for agreement. `--interp` now answers `None`; `--aot`
  refuses `s.remove(1)` over a *name* because it folds container methods only over a literal written
  at the call, and that refusal is recorded as owed to `L12.11` / `Gap R.63`.
- **Give the mutation roads an explicit "void value" return instead of `("", nil)`.** Rejected as
  insufficient on its own: `("", nil)` is fine for statements, and the bug was the print road not
  knowing. A richer return type would still need the predicate; the predicate alone is what closes it,
  and the table keeps the two roads from disagreeing.
- **Make `pop` void-returning for consistency.** Rejected: the reference answers with the removed item,
  and the drain-a-container loop is real code.
- **Add the mutators the compiled leg lacks (`extend`, `insert`, `update`, `clear`, `popitem`,
  dict `pop`) in this commit.** Rejected as bundling: those are *missing methods* — a
  reference-runs/we-refuse gap with its own row and owner (`L12.11`), not a wrong answer. This row
  changes only what the methods that exist answer.

## Related

`Gap R.187` (this row), `Gap R.63` / `L12.11` (receiver tables: the mutators that do not exist yet),
ADR 0291 (a builtin that hands back void hands back `None` — the sibling rule, extended here to
receiver methods), ADR 0172 (`None` prints as `None`, decided statically), ADR 0191 (`xs.sort()`
diagnosed as a string method — the same receiver-blindness), ADR 0298 (refusal symmetry declined),
ADR 0166 (exit codes; exit 2 forbidden), ADR 0259 (`slotVal`: why the mutation still boxes its
argument correctly while its answer becomes the void).
