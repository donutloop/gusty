# A format spec is part of the program, not a suffix to throw away

## Decision

An f-string field's **format spec and conversion travel in the AST** and are honoured by both engines
through **one shared engine** (`pkg/lang/format_spec.go`). Until now `stripFormatSpec` returned
`src[:i]` at the first top-level `:` and the remainder was discarded — no AST field existed — so every
road that rendered an interpolation answered the **plain value** (roadmap `Gap R.186`, the f-string half
of L12.8 / Gap R.60):

| program | reference | both engines before | now (`--interp`) | `--aot` |
|---|---|---|---|---|
| `f"{3.5:.2f}"` | `3.50` | `3.5` (exit 0) | ✅ `3.50` | ✅ or refuses |
| `f"{7:05d}"` | `00007` | `7` | ✅ | ✅ |
| `f"{-4:05d}"` | `-0004` | `-4` | ✅ | ✅ |
| `f"{255:x}"` | `ff` | `255` | ✅ | ✅ |
| `f"{1234:,.2f}"` | `1,234.00` | `1234` | ✅ | ✅ |
| `f"{3.5:>6}"` | `··3.5` | `3.5` | ✅ | ✅ |
| `f"{0.25:.2%}"` | `25.00%` | `0.25` | ✅ | ✅ |
| `f"{7!r}"` | `7` | conversion dropped | ✅ | ✅ |
| `f"{2}"` / `f"{2.0}"` | `2` / `2.0` | — | ✅ they differ | ✅ |

Eleven shapes, **both** backends, **exit 0**, engines in perfect agreement — which is precisely why
nothing noticed: parity compares the engines to each other, and only the oracle leg compares either to
CPython.

## The agentic rationale

A compiled program that silently ignores a directive the programmer wrote is worse than one that
refuses: the author gets an answer and no reason to doubt it. `f"{3.5:.2f}"` printing `3.5` reads like
"the language has no format specs", a fact nobody can discover from a run — the output is a well-formed
number. So the rule is: **a spec is honoured, or the field refuses with a sentence naming it.** No
third state. Where the AOT backend cannot read the field's value at compile time (`f"{n:05d}"` over a
variable, `f"{3.5 + 1:.2f}"`) it says *"the format spec `"05d"` needs a value this backend can read at
compile time"* and exits 1. It never emits the unformatted digits.

## Codegen / IR implications

- **The spec was destroyed at the earliest possible moment.** `buildFString` called
  `stripFormatSpec`, which cut at the first depth-0 `:`. That one call is the whole bug list: once the
  text is gone, no later road can recover it, and every road is then *locally* correct — printing the
  value is the right thing to do when you were never told a spec existed. Deleting the loss, not
  patching the roads, is the fix.
- **Bracket- and quote-awareness had to be shared.** `f"{xs[1:]}"`, `f"{d['a:b']}"` and `f"{'x':>5}"`
  all contain a `:` that is not a spec separator. The old scanner got the *split* right and threw the
  tail away; the new `splitSpecConv` gets the same split and returns `(expr, conv, spec)`. One scan,
  three answers — a conversion peeled from the expression's end only, because a `!` elsewhere is a
  not-operator inside the expression.
- **The value's own type is an input, not an inference.** `format(2, "")` is `"2"` and
  `format(2.0, "")` is `"2.0"`; a backend that recovers "is this a float?" from `f == math.Trunc(f)`
  makes `f"{2}"` and `f"{2.0}"` agree, which the reference forbids. So the engine takes
  `FormatNumber` (the caller holds a float) and `FormatRawInt` (the caller holds an int) as two
  entries, and the interpreter passes the heap object's `kind == "float"` as the answer.
- **`pyFloatRepr` is the only float renderer.** The empty-spec float case first went through
  `strconv.FormatFloat(f, 'g', -1, 64)`, which answers `"2"` for `2.0` — Go's shortest-round-trip has
  no obligation to keep the `.0` Python's repr does. Reusing the module's existing Python-shaped repr
  (which also owns the `1e-4 … 1e16` notation switch and `-0.0`) fixed three rows at once and kept the
  spec engine from becoming a second, subtly-different float printer.
- **A field that is a container has no word to travel in.** `f"{[1,2]}"` emitted
  `printf(i8* @.fmt1, i32 @.lst1)` — a list *global address* handed to `%d`, which `llc-20` rejects as
  an invalid module. That is exit 2, ADR 0166's forbidden class, and it pre-dated this row. The print
  door already renders a container via `rt_print_list_mixed`, but that is a *sink*, and an f-string
  builds one `printf` format string; the two do not compose. So the road **refuses** the container
  field instead of lowering it, and the interpreter — which has no such limit — answers `[1, 2]`.
- **A spec on a container raises what the reference raises.** `format([1,2], ">8")` is
  `TypeError: unsupported format string passed to list.__format__`, because a list has no `__format__`
  beyond the object default. The first attempt padded `"[1, 2]"` and exited 0 — a wrong answer the
  reference does not give at all. The road now refuses the *whole* spec for a container, not just its
  numeric fields.

## Alternatives rejected

- **Parse the spec into `FStringPart` but keep the old print behaviour when it is unrecognised.**
  Rejected — that is the defect with a parser added. An unrecognised spec refuses.
- **Teach the compiled leg to format at run time by emitting a call into the spec engine.** Rejected for
  this row: it needs the engine's Go logic in the runtime module (a second implementation in a second
  language, the C-again-to-match-C trap ADR 0156 records), and a refusal costs nothing meanwhile. The
  compiled half is owed to L12.8 with L11.1, whose tagged word makes the field's kind readable.
- **Match the compiled leg down to the interpreter by refusing in both.** Rejected on ADR 0298's ground:
  the ladder forbids trading a correct answer for agreement.
- **Reuse `printf`'s own `%05d` / `%.2f` for the compiled constants.** Rejected: printf and Python
  diverge on exactly the cases that matter — `%.0f` rounding (banker's vs half-away), `%g`'s trailing
  zero and exponent rules, and `%d` on a value the language calls `float`. The digits come from the
  shared engine; `printf` only ever receives `%s` for a formatted field.
- **Support `!a`, `#`, `;`-grouping, `_`-grouping, `'n'`.** Rejected as absent, not refused-silently:
  each gets its own sentence, and `--lang`'s manifest is where absence is published.

## Related

`Gap R.186` (this row), `L12.8` / `Gap R.60` (the item it advances), `Gap R.63` (a list/dict method
name answered by the wrong road — same "the print position decides" family), ADR 0298 (a text iterates
one character at a time: the same "one question, one road" rule, and the same refusal-symmetry refusal),
ADR 0297 (`not` of a text: another road that asked a different question than `if`), ADR 0233 (a float's
`str` is its `repr` — why the conversion and the spec share one renderer), ADR 0224 (a string value is a
`@str_tab` index, not bytes: the reason the container field had no legal word), ADR 0166 (exit codes;
exit 2 is forbidden), ADR 0156 (do not reimplement a formatter to match a formatter).
