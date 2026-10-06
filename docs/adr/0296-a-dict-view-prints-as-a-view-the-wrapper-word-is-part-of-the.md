# A dict view prints as a view: the wrapper word is part of the value

## Decision

`d.keys()`, `d.values()` and `d.items()` answer a **dict view**, and a view renders as the reference
renders it — `dict_keys(['a'])`, not `['a']` (roadmap `Gap R.182`, owner L11.1). Three defects closed:

| program | reference | before, `--interp` | before, `--aot` |
|---|---|---|---|
| `print({"a": 1}.keys())` | `dict_keys(['a'])` | `['a']` | `0` (handle through `%d`) |
| `print({1: 2}.values())` | `dict_values([2])` | `[2]` | **exit 2** |
| `print({1: "a"}.values())` | `dict_values(['a'])` | `['a']` | `0` |

The design decision is *how a view is represented*:

- **A view stays a list-shaped object.** `sum`, `min`, `max`, `sorted`, `len`, `for`, `in` all work on a
  view, and each of them tests `o.kind == "list"` (sixteen such sites in the evaluator). Giving a view its
  own `kind` broke four pre-existing tests on the first try — `sum({1:2,3:4}.keys())` raised `sum expects a
  list or set`. So the object keeps `kind: "list"` and gains a `view` field carrying the wrapper word; only
  the rendering reads it.
- **The compiled leg prints the word from the module**, via three private constants
  (`@.fmtdictkeys`, `@.fmtdictvalues`, `@.fmtdictclose`) around `rt_print_list_mixed`, rather than teaching
  the heap printer about a kind the heap does not have.
- **`items()` stays a refusal on the compiled leg.** Its elements are pairs, and a pair has no value
  representation until L11.3 (`Gap R.182`'s record says so, and a test fails if the printer claims `items()`
  while the fold refuses it). The interpreter renders pairs `(k, v)` because the reference does; the
  compiled leg declines rather than inventing a bracket-shaped value.

## The agentic rationale

Two of the three were exit-code lies. `print({1: 2}.values())` produced
`call void @rt_print_list_mixed(i32 @.lst1, i32 0)` — llc rejects our module, which ADR 0166 counts as
**exit 2**, the compiler's own bug, on a two-line program the reference runs. The other shapes printed `0`
at exit 0, the class an agent scripting `--aot` cannot distinguish from success.

And the *rendering* half — `['a']` where the reference says `dict_keys(['a'])` — was wrong on **both**
engines identically, so parity was satisfied and only the oracle leg could see it. That is the third cycle
running where the interesting bug is the one both backends agree on.

## Codegen / IR implications

- **The fold returns a heap handle; print must know.** `keys`/`values` lower through the call road to a list
  literal, and the printer had no arm for a dict-view call, so it fell through to `printf("%d")` and printed
  the handle. Same failure shape as `Gap R.179`'s slice arm one line above: a print dispatch that switches on
  node type is silent about the node types it does not list.
- **Ask `containerOperand`, not `g.value`.** A view over an **int-valued** dict folds to a literal that
  `g.value` renders as the address of a compile-time global — and handing `@.lst1` to a heap walker is
  precisely the exit-2 shape ADR 0188 removed for literals. My first version used `g.value` and reproduced
  the bug on the very shape the row was about.
- **One predicate for the view question** (`g.dictViewElems`, returning the elements, the wrapper word and
  whether it is a view at all), asked of the same call shape the fold recognises — the rule `Gap R.180`
  learned, that a hand-copied method-name switch is how a fold and a printer drift apart.
- **IR comments are code, again.** An IR comment containing backticks broke the Go raw string holding the
  module (`expected ';', found dict_keys`) before any IR was emitted. Second cycle in a row.

## Alternatives rejected

- **Give a view its own object kind** (`dict_keys`/`dict_values`/`dict_items`). Rejected by measurement:
  four tests broke immediately because the numeric and container roads gate on `kind == "list"`. A view is
  list-shaped everywhere except in what it says when printed, and `kind` is the wrong axis for one word.
- **Print a view as a plain list and record the difference only in `--json` output.** Rejected: stdout is
  what the reference is compared against and what an agent diffs; a machine-readable side channel does not
  make `['a']` correct.
- **Answer `dict_items([...])` on the compiled leg too** by inventing a pair rendering. Rejected: no pair
  value exists, and a plausible-looking `[(…)]` built from list internals is a fabricated representation.
  The refusal is the honest answer until L11.3.
- **Refuse `keys()`/`values()` outright.** Rejected: both engines answered them (wrongly), the reference
  answers them, and the ladder forbids trading an answer for a refusal.

## Related

`Gap R.182` (this row), `Gap R.179`/ADR 0294 (a handle is not a list, same print-dispatch shape),
ADR 0188 (a global address is not a heap handle), ADR 0166 (exit codes), ADR 0295 (`dict.get`'s answer
kind, the one-lookup rule), L11.3 (tuples, which owns `items()`).
