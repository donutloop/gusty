# An index that is not a position names its type at the doors that count

## Status

Accepted — 2026-07-14

## Context

ADR 0328 closed the hashing half of the language's key rule: a dict or a set asks *can this be hashed*,
and a container cannot. Its sibling half was measured the same afternoon, on the committed binary, and it
was worse — not a wrong class, but a wrong **answer**, four times over, at exit 0:

| program | the reference | this backend |
|---|---|---|
| `xs = [1, 2]` / `print(xs["a"])` | `TypeError: list indices must be integers or slices, not str` | **`1`**, exit 0 |
| `xs = [1, 2]` / `print(xs[1.5])` | `… not float` | **`2`**, exit 0 |
| `xs = [1, 2]` / `print(xs[None])` | `… not NoneType` | **`1`**, exit 0 |
| `s = "ab"` / `print(s["a"])` | `TypeError: string indices must be integers, not 'str'` | **`b`**, exit 0 |
| `s = "ab"` / `print(s[1.5])` | `… not 'float'` | **`b`**, exit 0 |
| `xs = [1, 2]` / `xs["a"] = 1` | `… not str` | stores into slot 0, exit 0 |
| `xs = [1, 2]` / `print(xs[[0]])` | `… not list` | **exit 2** — `llc`: *global variable reference must have pointer type* |
| `xs = [1, 2]` / `print(xs[(0, 1)])` | `… not tuple` | exit 1, `unsupported expression *lang.Tuple` |

The mechanism was the same one ADR 0328 named, one construct over. A text value in this backend is an
index into `@str_tab`; a list literal is a `@.lstN` global. Neither is a position, but `normalizeIndex`
— the bounds check that stands on the list roads and raises `IndexError` — is handed an i32 and was never
asked what it meant. `xs["a"]` counted to the slot the interned number named and printed the element
sitting there. A number the program never wrote, delivered as a correct answer.

The list-valued index was the sharper finding: the literal lowered to a bare global reference inside the
check's `icmp slt`, `llc` refused the module, and the process exited **2** — the code this repo's own
exit-code contract reserves for a compiler bug. A program with a typo in it was indistinguishable from a
broken compiler.

## Decision

**Ask, at the two doors that subscript a container which counts, whether the index has a kind a position
can have** — `pkg/lang/list_index_kind.go`, shaped deliberately like `unhashable_key.go`:

- `nonIntIndexTypeName(e)` answers the reference's name for the index's type (`str`, `float`, `NoneType`,
  `list`, `dict`, `set`, `tuple`), read from what the source wrote: a literal, or a name the program most
  recently filled with one (the same kind maps the print and membership doors already use). `*IntLit` and
  `*BoolLit` are positions — `xs[True]` is `xs[1]`, and the reference agrees — and a name whose kind is only
  a run-time fact answers *no*, so the door keeps the bounds check it always had rather than accuse
  something the compiler cannot see.
- `badIndexMessage(typeName, isString)` words the two sentences, which are not one sentence: a list names
  the type bare, a string quotes it. Getting this wrong is the kind of detail that makes a traceback a lie.
- `badIndexGateAtSubscript` works out which container the subscript is asking — a list, a string — and
  raises before the subscript is lowered at all, through `raiseTo`, so `except TypeError:` catches it. A
  dict or a set is subscripted by key, not by position, and is left to the door beside it.
- It runs **before the index is lowered**, at the read road (`case *Index:` in `value()`) and at the store
  road (`assignIndex`). That ordering is the whole point: it is what keeps a list-valued index from ever
  reaching the `icmp`, which is why the exit-2 toolchain failure and the wrong answer are one fix rather
  than two.

## Alternatives rejected

- **Type the index inside `normalizeIndex`.** It has an i32 and a span, not an expression: by the time it
  runs, the index has already been lowered and the bad instruction is in the block. The gate has to sit at
  the doors, which is where the `Expr` still exists.
- **A runtime tag check on the index.** The general answer, and the same L11.1 tagged-value debt ADR 0328
  records; the source states the kind in every measured case, and the run-time-only case is left honest
  rather than guessed.
- **Refusing to compile.** `print(xs["a"])` is a program — the reference runs it, up to the raise. A
  refusal would also be unconditional, killing a program whose bad line sits behind a branch that never
  runs. A trap is the dynamic answer to a dynamic construct.
- **Answering `0`/`None` for a bad index.** What the fold had effectively been doing; it is the class of
  defect this repo treats as worse than a refusal.

## Consequences

- `pkg/lang/list_index_kind.go` + `pkg/lang/list_index_kind_test.go` (the reference's sentences asserted as
  the reference states them; the answering half — positions, negative positions, a bool, a slice, a nested
  list, a string character, a dict key — kept whole; the classifier asked directly, including the name
  whose kind is only a run-time fact; and `TestTheListValuedIndexBuildsAModuleInsteadOfBreakingOne`, which
  asserts exit ≠ 2 so the toolchain can never again be the thing that failed) and
  `integration/list_index_kind_test.go` (the same doors through the CLI with exit code and empty stdout
  asserted, and `integration/programs/list_index_kind.gy` compared line-for-line with CPython as an asserted
  `match` matrix row).
- The exit-code contract gets a concrete enforcement: an emitted module that `llc` rejects is now asserted
  against for this construct, in a test whose name says what exit 2 means here.
- Measured on the way and filed rather than bundled: **Gap R.223** — the bounds trap says `index out of
  range` where the reference says `list index out of range`, and the store road says plain `index out of
  range` where the reference says `list assignment index out of range`; the string road already names its
  container, so one helper is being specific while its neighbours are not.
- Slices (`xs[0:2]`) are untouched and still answer; `*Tuple` at the store road and the dict/set doors are
  unchanged.
