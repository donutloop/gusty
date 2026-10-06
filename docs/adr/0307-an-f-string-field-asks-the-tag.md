# 0307. An f-string field asks the tag

Status: accepted. Cycle: roadmap `L11.1` (tagged value word) — `Gap R.146`'s **rendering** positions, plus the
conversion half of `Gap R.192` (measured this cycle). Land: compiled backend only (`pkg/lang`), verified
through `llc-20`.

## The decision

**An f-string field is a rendering position, not a one-word position.** A field whose expression is a name the
pair road bound — a slot read, a loop variable, an arithmetic answer, a call's answer — contributes `%s` over
the bytes the module's **one tag-reading printer** captured (`rt_str_of_value` → `rt_print_mixed_value`), the
same door `print()`, `str()`, `repr()` and the REPL prompt were routed through in ADR 0303.

An expression *built over* such a name (`n - 1`, `-n`, `n // 2`) asks the same door through the arithmetic
road ADR 0265 built, so the field, `print()` and `str()` cannot disagree about `6` versus `6.0`; an operator
that road will not vouch for (`+`, `%` over a slot whose kind is a run-time fact) answers "not proven" and the
field keeps the refusal it has always printed.

**A conversion is a rendering too.** `!r` asks the same printer with the quote flag on — what `repr()` does —
and `!s` asks it without. A field that asked for a format **spec** still goes to the compile-time spec engine,
which this cycle narrowed to the fields that engine can actually read: constant literals.

## What this cycle ends

Three measured defects lived on this road, all of them the same failure wearing different clothes:

- `print(f"{n}")` for `n = xs[0]` **refused**, while `print(n)` had answered since ADR 0303 and `print([n])`
  since ADR 0306 — Gap R.146's list of one-word positions losing its last rendering entry.
- `f"{x!r}"` and `f"{x!s}"` over any **variable** printed **nothing at all, at exit 0**: the conversion went to
  the compile-time spec engine, that engine returns the empty text for a field it cannot see, and the road
  accepted it as an answer. A wrong answer with the exit code of success is not counted by
  `compiled refusals this run`, so the suite could not see it — which is exactly why this row needed a
  reference comparison, not a pin.
- `print(f"{'a'!r}")` spent the contract's **forbidden exit 2**: quoting a text literal spliced quote
  characters into the module's format-string global, and `llc-20` rejected the module
  (`@.fmt1 = private constant [0 x i8]` against a `[4 x i8]` use). ADR 0166 counts that exit class as the
  compiler's own bug, whatever the program.

## The rule this restates (and one it corrects)

ADR 0303: one renderer per value, and the renderer reads the tag. A second renderer for f-strings — or a
`%d` fed the pair's payload — is how a text field prints `0` and a float field prints `140737488355328`, both
at exit 0. ADR 0305's lesson applies unchanged: `rt_lift_num` was **not** an option here, because it unboxes
a float tag and `sitofp`s every other payload, turning interned indexes into plausible numbers.

The correction worth recording: ADR 0299 narrowed the *spec* road and left the *conversion* road folding
whatever it was handed. A fold that answers `""` for an unreadable field is worse than a refusal, because it
looks like an answer. `fieldIsConstantLiteral` is the guard that road was missing.

## Codegen and IR implications

- `pkg/lang/heapargs.go` gains `filedPairOf` (the field-side question: does this expression travel as a pair,
  and what are its two words), `fieldIsConstantLiteral`, and `fieldWantsPrinterQuotes`.
- `pkg/lang/codegen.go`, the print road's f-string chain: the pair arm runs **before** the spec/conversion
  branch (a field asked for `!r` is still a rendering); the spec branch is narrowed to constant literals; the
  interned-text arm honours `!r` through `rt_str_of_value(index, TagStr, 1)`.
- Emitted IR per pair field: `rt_str_of_value(payload, tag, quote)` → `rt_str_ptr` → one `%s` operand in the
  line's `printf`. No new runtime function, tag, object kind or signature; the IR row in
  `pkg/lang/pair_fstring_test.go` fails if the road grows its own renderer or feeds `printf` an `i32` where it
  feeds the printer's bytes.

## Alternatives rejected

- **`%d` the payload and let the number look right.** Answers a float field with its box handle and a text
  field with its interned index, at exit 0. This is the family Gaps R.38/R.95/R.115 keep filing.
- **Lift the pair (`rt_lift_num`) into the printf's double arm.** Rejected for the same reason ADR 0305
  rejected it, and the trap is identical: the lift converts everything that is not a float.
- **Fold `!r` into the format string with quotes around it.** That is what the road did, and it is the exit 2.
  Quoting is the printer's job — the flag exists for exactly this.
- **Refuse every f-string that is not a literal.** Would have been the cheap "honest" option and would have
  kept `f"{n}"` broken while making the refusal table longer. The door already existed; opening it was less
  code than describing why it could not be opened.
- **Leave the blank `!r` answer and file it as debt.** Rejected: an exit-0 blank is invisible to the drift
  machinery and to users, and the fix is a flag on an existing call. The debt row now records the *answer*
  rather than the silence.

## Evidence

- `pkg/lang/pair_fstring_test.go` — 18 field answers (int, text, float, `None`, `bool`, a nested container, a
  dict slot by key, arithmetic over a slot, arithmetic/negation/floor division/true division/verdict/builtin
  inside the field, the loop variable) and 12 conversion answers (`!r`/`!s` over text, int, float variables,
  slot reads and literals, plus a format spec that must still reach the spec engine), each checked against the
  record and through the module; 6 refusals pinned as refusals; and the IR row above.
- `integration/pair_fstring_test.go` — the same programs through the CLI against **CPython** (these shapes have
  no record to read: the retired engine never answered them), refusals required to be exit 1, exit 2 forbidden.
- `programs/probe_fstring_field_asks_the_tag.gy` is the pinned parity program: fifteen lines, `match` on both
  legs, chosen so that a missing tag cannot print a plausible number (the text lines print `a`/`xay`, the
  float line `2.5`, the container line `[1, 2]`).
- Paid rows moved, not deleted: the `f"{n - 1}"` refusal row left `pkg/lang/pair_number_float_test.go` and
  `integration/pair_number_float_test.go` for this cycle's answer tables.
