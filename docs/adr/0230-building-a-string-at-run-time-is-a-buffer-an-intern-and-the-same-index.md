# Building a string at run time is a buffer, an intern, and the same index

Status: accepted. Closes roadmap Gap R.47 in full (ADR 0229 closed its read half; this closes the
write half). Cites: ADR 0224 (a string value is an `@str_tab` index), ADR 0229 (the table grows at
run time), ADR 0225/0210 (positions are code points, negative ones count from the end), ADR 0196
(a loop owns its counter), ADR 0212/0214 (a built-in trap is a typed raise), ADR 0166 (a refusal,
never an invalid module), ADR 0209 (runtime blocks travel with their references).

## What the write half had been

| shape | CPython | interpreter | compiled (before) |
| --- | --- | --- | --- |
| `print(s[0] + s[2])` | `ac` | ✓ | refuses: concatenating a runtime string |
| `print("a" + get() + "c")` | `abc` | ✓ | refuses |
| `def f(i): s="abcdef"; return s[i:i+2]` | `bc` | ✓ | refuses: cannot fold string slice |
| `for c in get()` | a,b,c | ✓ | refuses; and before that, **printed nothing**, exit 0 |
| `print(str(get()))` | `42` | ✓ | refuses: str on non-integer |
| `get().strip()` | `hi` | ✓ | refuses: string method on non-constant string |

The iteration row carries the lesson of the whole family. It used to *compile cleanly and print
nothing*: `for c in txt()` read the string's table index as a repeat count (Gap R.16). The refusal
that replaced it was honest, and this cycle replaced the refusal with an answer. A silent zero is
the failure mode this family keeps producing, which is why every new capability here ships with an
assertion about the *output*, not merely about acceptance.

## The helpers

Four more runtime operations, all `internal i32(i32…)`, all returning indices or the sentinels:

- `rt_str_cat(a, b)` — allocate both lengths plus a terminator, copy, intern.
- `rt_str_byteoff(s, k)` — the byte offset where code point k begins, clamped the way a slice
  clamps (negative from the end, out of range to an end).
- `rt_str_slice(s, lo, hi)` — two offsets and a copy. An absent bound cannot be "missing" in an
  `i32` argument, so the source's omission is spelled with a sentinel (`INT_MIN` = from the start,
  `INT_MAX` = to the end) decided by the helper and known only to codegen — an agreement between
  two files, stated in the comment on both sides.
- `rt_str_strip(s)` — trims bytes at or below space. The interpreter trims the Unicode set; the
  compiled backend's narrower set is a stated limit, not an approximation to be discovered later.
- `rt_str_of_int(v)` — `str(n)` for a number the compiler cannot read: digits written backwards
  into the block, interned from the first digit. Floats keep refusing (L11.6): truncating one to
  render it would be exactly the silent-truncation bug that gap exists to keep dead.

Interning does the semantics for free: dedup is by content, so `"a" + word()` of `"b"` is the same
index as the literal `"ab"`, and equality between a built string and a literal needs no special
case anywhere.

## The loop, and the counter that had to be its own

`for c in <runtime string>` is the ordinary counter loop with the element made by `rt_str_char`:
the count is `rt_str_nchars`, the counter is the loop's own allocation (sharing it with the
variable's slot let a body assignment move the iteration — ADR 0196), the variable is bound at the
top of each body (that is when Python binds it), and the iterated expression is evaluated *once*,
before the loop, so a call in the header runs once and not per character. The loop variable is
registered as holding an index, so the body prints the character rather than a number.

## Three things the suite caught, and one it caught twice

- **A counter bug that made every slice empty.** `rt_str_byteoff` advanced its code-point count on
  *continuation* bytes instead of their complement: for pure ASCII the count never left zero, the
  walk ran to the terminator, both bounds landed on the end, and `s[1:3]` returned `""`. Output —
  not the emission — is what caught it.
- **`phi` predecessors.** `rt_str_of_int` first shipped as a while-loop whose header phi named a
  block that was not its predecessor; the module failed verification for *every* program that
  printed anything, within a second of building.
- **Print classification again, one layer up.** A slice's value was right and `print(f(1))` printed
  `1`: the call-site classification of a function's return kind had no case for `return s[i:j]`, and
  the print predicate had no `*Slice` case. The rule from ADR 0229 restated: one predicate, every
  consumer — operations, print, and the whole-program kind scan.
- **Fixtures that quietly stop refusing.** `for c in txt()` was the canonical "codegen refuses"
  fixture in four tests (the exit-code contract, the trap-class contract, the oracle's
  refused-leg check, and a diagnostics table). The shape became answerable, so those tests went
  green *without exercising anything*. Four fixtures moved to `print("ab" * 2)`, which still
  refuses (Gap R.33). A pinned refusal is a claim about the future: when the gap closes, the pin
  has to move or it stops saying anything — the same reason the pinned refusal *tables* in
  `string_subscript_test.go`/`string_value_test.go` were emptied into CPython-checked answer
  tables rather than softened.

## Still open, named rather than approximated

`%s` formatting (Gap R.31), `str * int` and list concatenation in codegen (Gap R.33), floats in
containers and `str(<float>)` (L11.6), Unicode case folding and the full whitespace set in the
compiled backend, and the 4096-entry table bound (a catchable `RuntimeError`, ADR 0229).

## Verified by

`integration/string_subscript_test.go`: `TestCompiledStringWritesAnswerAtRuntime` — twelve shapes
(concatenation, dedup-by-content, run-time/open/negative slices, three iteration shapes including
Unicode, `str` of positive and negative numbers, `strip`) each run through CPython first, with the
test refusing to proceed when the table itself disagrees; and
`TestCompiledStringLoopsAreNotSilentlyWrong`, which asserts the loop goes through the table with a
counter of its own. `pkg/lang/runtime_block_emit_test.go` and the two `string_args_test.go` tables
were re-aimed from "must refuse" to "must answer" where the gap moved.
`programs/runtime_string_writes.gy` is a matrix row declared `match`: 101 rows, 78 pass, 60
oracle-match, 0 fail, 0 drift.
