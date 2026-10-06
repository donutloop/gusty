# A text answers truth like any other value, and a string method answers with its own kind

## Decision

Two compiled-leg wrong numbers at exit 0, plus one that **both engines agreed on**, from the
2026-07-06 surface sweep (roadmap `Gap R.183`, `Gap R.184`, owner L11.1 / L11.2):

| program | reference | before | cause |
|---|---|---|---|
| `print(not "x")` | `False` | `--aot True` | `not` asked `asI1`, which emits `icmp ne i32 <word>, 0` — right for a number, and for a text it reads the **intern slot** |
| `print("ab".zfill(5))` | `000ab` | `--aot 0` | the print road listed three text-returning methods; the fold implements fourteen |
| `print("ab".ljust(4))` / `rjust` | `ab  ` / `  ab` | `--aot 0` | same table |
| `print("-42".zfill(5))` | `-0042` | **both engines** `00-42` | each backend padded the whole string, so the zeros landed between the sign and the digits |

The rules that decide them:

- **`not` asks the same road an `if` test asks.** `truthyValue` is what `if "x":` already used and it is
  text-, container-, void- and verdict-aware. `not` used `asI1` on the already-lowered register instead, so
  the two disagreed *in the same file*: `if "x":` said truthy and `print(not "x")` said True.
- **One table decides which string methods answer text.** `g.textMethodAnswersText(name, argc)` mirrors the
  fold's own implementation. The old code had a three-name list (`upper`, `lower`, `strip`) — **written
  twice** in `codegen.go` — against fourteen implemented methods.
- **`zfill` pads after a leading sign**, via one `zfillTo` shared by both engines, because CPython does:
  `"-42".zfill(5)` is `-0042`.

## The agentic rationale

The last row is the important one for an agent: **both backends printed `00-42`**, so the parity matrix —
which compares `--interp` against `--aot` — reported nothing, and only the oracle leg running CPython on the
same file could see it. That is the fourth consecutive cycle where the most instructive wrong answer is the
one the two engines agree on.

The `zfill`/`ljust`/`rjust` family answers `0` — a *plausible-looking* number for a text — at exit 0. An
agent scripting this compiler gets a clean exit code and an answer that looks like a count. Nothing in the
contract distinguishes it from success, which is why the whole class is the queue's priority-1 work.

No answer became a refusal. `print("a\tb".expandtabs())` still refuses AOT (a tab in the receiver; separate
road), and this change did not touch it.

## Codegen / IR implications

- **`asI1` is a number's predicate.** `icmp ne i32 %v, 0` is correct for anything whose value is a number
  and wrong for anything whose value is a HANDLE or an INTERN INDEX. Anything that tests truth must go
  through `truthyValue`; `asI1` should only ever be handed a value `truthyValue` produced. That this is now
  the *second* bug from that function (with `Gap R.149` nearby in the ledger) is the argument for the rule,
  not against it.
- **The duplicated three-name list is the finding, not the fix.** A print-side capability table that is
  maintained by hand against an operation-side `switch` will always be a subset of it. The new predicate is
  the only place the list exists, and a test fails if the old literal reappears.
- **The sign rule had to be shared.** Two left-pads that agree with each other and disagree with the
  reference is a parity blind spot; one `zfillTo` called by both backends makes the divergence
  unrepresentable, which is the same argument ADR 0279/0280 make about one question per expression.

## Alternatives rejected

- **Fold `not` to a compile-time constant when the operand is a literal.** Rejected: it fixes the four
  sweep rows and leaves `not s` (a name, a call, a container) in the same trap. The bug is the predicate,
  not the missing fold.
- **Add `zfill`/`ljust`/`rjust` to the print table as three more names.** Rejected: that is the same hand-
  maintained subset that produced the bug. The table is now derived from what the fold implements.
- **Trim the padded answer in the test** (to make `"ab".ljust(4)` compare equal). Rejected — the padding is
  the entire answer, and `strings.TrimSpace` on it would have made the test assert nothing. Those rows ask
  the reference for a `repr()` rendering and compare that.
- **Leave `zfill`'s sign to the eventual string rewrite (L11.5).** Rejected, for the fourth time this
  stretch: it is a wrong number at exit 0 today, the rule is four lines, and "a redesign will make this
  impossible" never stops the wrong number from shipping meanwhile.

## Related

`Gap R.183`, `Gap R.184`, `Gap R.42`/ADR 0224 (an interned index is not a text), ADR 0229 (print and the
operation ask one predicate), ADR 0279/0280 (one question per expression), `Gap R.179`/ADR 0294 and
`Gap R.182`/ADR 0296 (the same print-dispatch shape), ADR 0166 (exit codes).
