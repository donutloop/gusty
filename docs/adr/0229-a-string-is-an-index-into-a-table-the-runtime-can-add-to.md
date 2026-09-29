# A string is an index into a table the runtime can add to

Status: accepted. Closes the read half of roadmap Gap R.47; the write half (building a string at run
time) and iteration stay open, measured. Cites: ADR 0224 (a string value is an `@str_tab` index),
ADR 0225 (a subscript of a string is a one-character string, counted in code points), ADR 0210
(negative positions), ADR 0212/0214 (a built-in trap is a typed raise the program can name),
ADR 0166 (a refusal, never an invalid module), ADR 0228 (the kind maps are consulted after `beginScope`).

## Measured before anything was decided

Sixteen shapes, three legs. Thirteen were compiled-only compile-time refusals for programs CPython
runs; two (`eq`, `in`) already worked; none was right on all three legs, because printing a boolean
diverges separately (L11.2).

| shape | CPython | interpreter | compiled (before) |
| --- | --- | --- | --- |
| `s = get(); print(s[1])` | `b` | `b` | refuses: index of a non-literal variable |
| `def f(s): return s[1]` | `b` | `b` | **prints `1`** — the interned index, exit 0 |
| `s[i]` with `i` a variable | `a`/`c` | ✓ | refuses: index must be a constant |
| `len(get())` | `5` | ✓ | refuses: len requires an inline literal |
| `get()[1].upper()` | `B` | ✓ | **prints `2`**, exit 0 |
| `ord(get()[1])` | `98` | ✓ | refuses: folds only a constant string arg |
| `for c in get()` | a,b,c | ✓ | refuses: iterating a string computed at run time |
| `s[i:i+2]` | `bc` | ✓ | refuses: cannot fold string slice |
| `"a" + get()` | `abc` | ✓ | refuses: concatenating a runtime string |
| `len(get())` on `"café"` | `4 é` | ✓ | refuses |

The two rows that printed a **number** are the important ones. Both backends were green-shaped: exit 0,
plausible output. The value produced by the char path was correct; the *print* path had asked a different
question and rendered the index.

## The decision, which is mostly an existing fact

A compiled string is an index into `@str_tab` (ADR 0224). `rt_str_intern` is content-addressed and
appends — the table already grows at run time. Put together, an operation asked about at run time has
somewhere to live: **the operations take indices and return indices**, and the convention never changes,
so `print`, `==`, `in`, container slots and dict keys keep working on a string the compiler never saw,
with no new value representation anywhere.

Six runtime helpers, all `internal i32(i32…)`:

- `rt_str_from_bytes(ptr, i32)` — the only way a string that did not exist at compile time becomes a
  value: copy, NUL-terminate, intern. Dedup by content is what makes a runtime-built `"b"` and the
  literal `"b"` the same index, so equality needs no special case.
- `rt_str_nchars(i32)` — code points, counted as "bytes that are not UTF-8 continuation bytes", because
  a position in this language is a code point (ADR 0225) and bytes would call `"café"` five long.
- `rt_str_char(i32, i32)` — walk to the i-th code point, measure the sequence, intern it. Negative
  counts from the end (ADR 0210). `-1` for no such position.
- `rt_str_codepoint(i32)` — `ord`: decode one sequence, `-1` if the string is not exactly one.
- `rt_str_case(i32, i32 mode)` — ASCII fold, other bytes copied. The interpreter has the full case
  tables; the difference is stated in the roadmap, not hidden.
- plus the capacity change below.

## The rule that had been missing is about asking, not about values

`"abc"[1]` worked and `get()[1]` refused. `s[1].lower()` printed text and `s[1].upper()` printed `2`.
Every one of those was the same bug: an operation asked *"can the compiler read this string's text?"*
when the question it needs is *"is this a string?"*. So the fixes are in the predicates, and the same
predicate now serves every consumer:

- `exprIsString` learned that a subscript of a string is a string, and that a no-argument `upper()` /
  `lower()` on a string receiver returns one.
- `printsAsInternedStr` asks the same predicate — the divergent answers were the two paths disagreeing,
  not a missing lowering.
- A module-level function whose body returns a string is registered as returning an index at the
  **pre-scan**, before any call site is lowered; methods had been registered that way since ADR 0224 and
  the module's own functions had not, which is why `get()[1]` was "not a literal".
- `scanStringBindings` records, over the whole program, which names hold indices (`s = get()`, tuple
  unpacking, a `for` variable over a string container) and which functions hand one back — including an
  unannotated `def f(s): return s[1]` whose parameter kind comes from the call site.
- The scan runs **after** `beginScope`, because that reset clears the per-scope kind maps: a scan
  answered before it is silently thrown away. This is the same lesson ADR 0228 learned with the
  module-level written-flags one cycle earlier, and it was re-learned here in the same shape.

## `async` functions are not string-returning

Marking an `async def g(): … return "ok"` as a string function made its caller read a *coroutine handle*
as a table index; `llc` rejected the module and the failure looked like a compiler bug (ADR 0166 — the
suite caught it, `TestR2ReproCompilesAndRuns`). The kind of what a call yields is the eventual value's
kind only for a call that *is* the value; a coroutine call yields a handle, so async definitions are
excluded from both marking sites while their bodies are still walked.

## The table was full, and nothing said so

`rt_str_intern` bounded the table at 256 entries and, on overflow, **reused the last one**: the program
went on printing a different string than it had. Raising runtime-created strings made that reachable —
iterating a text interns a one-character string per distinct character. Two changes:

- capacity 256 → 4096, everywhere the array type is spelled;
- overflow returns `-2`, and the caller raises `RuntimeError("the program created too many distinct
  string values")` through the ordinary unwind path, catchable like any other trap (ADR 0212, ADR 0214).

The first attempt at the overflow path called `rt_die`, which lives in another runtime block; when a
module didn't include that block, `llc` reported `use of undefined value '@rt_die'` — again the compiler
blamed for an ordinary program. Declaring `write` in this block instead produced `invalid redefinition of
function 'write'`. The lesson generalises past strings: **a runtime helper may only reach for what is in
its own block**, and a sentinel plus a raise in the caller is both more honest and less fragile than
printing from the runtime — a printed sentence is not something the program can catch.

## Still refused, with measurements

Concatenating a runtime string, slicing with run-time bounds, `for c in <runtime string>`,
`str(<runtime int>)`, `strip()`. All need to *build* a buffer (or to be driven by a loop), which is the
next piece of the same runtime: `rt_str_cat`, `rt_str_slice`, and a `for`-over-string taking the
container loops' iterator. They stay in Gap R.47 with the measured refusal messages, and the two
`upper`/`lower` folds are ASCII-only by the same honesty rule.

## Verified by

`integration/string_subscript_test.go` — `TestCompiledStringSubscriptAnswersAtRuntime` (eleven shapes,
CPython-first: the test refuses to run if CPython disagrees with the table) and
`TestStringTableOverflowIsACatchableTrap` (the emitted module verifies, contains the sentinels, and does
not reach across runtime blocks); the refusal table is trimmed to what genuinely still refuses, so a
shape that starts answering has to be promoted rather than quietly soft-pinned.
`programs/runtime_string_ops.gy` is a matrix row declared `match`: 100 rows, 77 pass, 59 oracle-match,
0 fail, 0 drift.
