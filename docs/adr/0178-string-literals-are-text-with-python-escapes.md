# ADR 0178: string literals are text — one decoder, one string representation

## Status

Accepted.

## Context

A program with a non-ASCII string in it had never worked, and nothing noticed:

```console
$ gustyc --file u8.gy      # s = "héllo"
hÃ©llo                     # not "héllo"
8                          # len: neither bytes (6) nor characters (5)
```

Two independent defects lived in the same few lines of the lexer:

- escapes were "decoded" by **dropping the backslash and keeping the next
  byte** — `print("a\nb")` printed `anb`, `"tab\there"` printed `tabhere`,
  `"\x41"` printed `x41`. Every escape in every program was silently wrong, and
  `parser.go`'s `unescapeStr` documented the same rule for f-strings;
- the ordinary-string scanner built its value with `val += string(src[j])`. In
  Go, `string(byte)` converts to a **rune**, so each non-ASCII byte was
  re-encoded as a two-byte sequence — `é` (`C3 A9`) became `Ã©` (`C3 83 C2 A9`),
  and `len` reported the mojibake's length. The result was indefensible in every
  direction: not Python's answer, not even a byte count.

The parity harness could not see this because it compares the two backends to
*each other*, and both backends share the front end.

Fixing the front end then exposed three AOT-only defects that the CLI had been
hiding — because `gustyc --file` runs the **interpreter**, so every manual
"compiled" probe in this session was actually interpreted. The conformance
harness (which really does `llc` + `cc`) caught them:

1. `"é" in greeting` lowered to `rt_contains(i32 @.str14, …)` — a **global in an
   i32 slot**, rejected by `llc`, so valid user code looked like a compiler bug;
2. `def g(): return "hello"` emitted `ret i32 @.str1` — the same violation from
   the function side;
3. `print(d["k"])` printed `6` — the interned-table **index** — when the dict's
   values were interned strings, while `print(d)` printed the dict correctly.

## Decision

**One decoder, one representation, and a rule for what stays divergent.**

- **One escape decoder** (`appendEscape`, `pkg/lang/lexer.go`) implements
  Python's rules — `\n \t \r \a \b \f \v \0 \\ \' \"`, hex `\xHH`, `\uHHHH`,
  `\UHHHHHHHH`. An unrecognised escape is kept **verbatim** (backslash included),
  as Python does; a malformed numeric escape (too few digits, a surrogate, a code
  point past Unicode) is kept as written rather than guessed at. `scanString` and
  the parser's `unescapeStr` both call it, so the lexer and the f-string path can
  never disagree. The duplicated inline ordinary-string scanner is gone — one
  `scanString` for every string form, which also deletes the `string(byte)` bug
  rather than patching it.
- **A string value is the source's own bytes**, preserved through lexing,
  interning, containers and printing.
- **A string in a compiled position is a `@str_tab` index — everywhere.** That
  already held for container elements (ADR 0173) and parameters (ADR 0174); this
  cycle closes the remaining two positions: the **haystack and needle of `in`**
  (new `@rt_str_contains`, a substring scan written in IR — no libc `strstr`,
  which would need a second declaration of a libc symbol and the `strlen`
  collision of ADR 0173 is still in living memory), and a function's **`return`**
  when the function is known to yield strings. `printsAsInternedStr` now knows a
  dict's *values* are interned too, which is what makes `print(d["k"])` print
  text.
- **Byte measurement stays, and is pinned.** `len`, `s[i]`, `s[i:j]` and
  `for c in s` measure UTF-8 bytes in both backends — `len("café")` is 5 where
  Python says 4. Changing that is a representation change for both backends at
  once, so it is its own roadmap item (**Gap N.2**), and a test
  (`TestStringLengthIsBytesForNow`) records the current answer *and* Python's, so
  the migration has to arrive as a failing test rather than a shrug.
- **Every expectation is asserted against CPython, mechanically.** The new
  integration tests run the same source through `python3` and fail if the
  hard-coded expectation disagrees with what CPython prints. A claim about Python
  that turns out to be wrong now breaks the build instead of living in a comment.
  Cases where Python *refuses* to run the program (a malformed `\xZZ` is a
  SyntaxError there) are pinned separately, as our own documented behaviour.

## Consequences

- `print("a\nb")` prints two lines; `"\x41"` is `A`; `print("café")` prints
  `café`; `"é" in "café"` is true; `def g(): return "hi"` compiles. The corpus
  gained `string_escapes.gy` (**41 conformance programs**), all green in both
  backends.
- **The AOT backend got honest.** Three shapes that produced invalid IR or wrong
  output now produce valid IR and Python's answer; the tests run `llc` + `cc`
  rather than trusting the CLI, because the CLI's default path is the
  interpreter.
- **`--file` running the interpreter is now a known trap**, recorded as roadmap
  Gap M.2: the default CLI path silently hides every AOT-only bug, which is how
  the three above survived while their tests "passed" in manual use.
- **One divergence is documented rather than fixed**: byte vs code-point
  measurement, with Python's expected values written into the pinning test.
- Escape semantics are now uniform across `"…"`, `"""…"""`, `r"…"` and f-strings,
  removing an entire class of "it works in one string form but not another".

## Alternatives rejected

- **Keep "drop the backslash"** — rejected: it is not a simplification, it is a
  silent corruption of every escape sequence, and Python compatibility is the
  language's premise.
- **Fix only the non-ASCII bug, leave escapes** — rejected: found in the same
  lines, and half-fixing means `print("a\nb")` keeps printing `anb`.
- **Patch `string(byte)` into `string([]byte{b})` in place** — rejected: the
  duplicate scanner is the reason the bug existed. Deleting the copy removes the
  whole class (and the risk that the two drift again).
- **Byte semantics for escapes, code-point semantics for `len`, interpreter only** —
  rejected: interpreter/AOT parity is the harness's foundation; a half-migration
  would show up as divergence the harness flags, and would be wrong anyway.
- **Call libc `strstr` from `@rt_str_contains`** — rejected: the runtime already
  declares `strlen` with an `i64` return, and a second, differently-typed libc
  declaration produced `invalid redefinition of function 'strlen'` last time
  (ADR 0173). An IR scan has no such coupling.
- **Emit a diagnostic instead of supporting `in` on strings** — rejected: it is
  plain Python and cheap to support; refusing it would be ADR 0166's escape hatch
  used to avoid work rather than to avoid lying.
- **Make `--file` run AOT as part of this cycle** — rejected: a large behaviour
  change that would turn AOT-unsupported programs from "works" into "fails";
  recorded as Gap M.2 instead, with this cycle's tests as the evidence that the
  AOT path needs a real default.

## References

- `pkg/lang/lexer.go` — `appendEscape`, `scanString` (one path for every string form)
- `pkg/lang/parser.go` — `unescapeStr`, `buildFString` (byte-slice accumulation)
- `pkg/lang/codegen.go` — `rt_str_contains`, string-haystack `in`, string-returning
  `ret`, `printsAsInternedStr` (dict values)
- `pkg/lang/escapes_test.go`, `integration/escapes_test.go` — unit + integration,
  including `TestAOTStringPrintsMatchPython` and `TestStringLengthIsBytesForNow`
- `integration/programs/string_escapes.gy` — the 41st conformance program
- ADR 0173 (interned strings in containers), ADR 0174 (interned indices across
  functions), ADR 0166 (unsupported lowering is a diagnostic)
- ADR 0177 (a failed parse is a list of diagnostics) — the previous cycle
