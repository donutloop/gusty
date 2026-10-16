# ADR 0360 — A text on the left of `%` formats, and the answer is a text value

Status: accepted
Date: 2026-10-17
Amends: [ADR 0215](0215-operand-kind-checks-turn-silent-wrong-answers-into-catchable-traps.md) (whose operand gate turned `text % x` into a refusal — the refusal this ADR retires, and the wrong answer it replaced stays replaced), [ADR 0282](0282-a-format-string-is-not-a-double-the-operator-can-widen.md) (whose guard stays: the guard was right that a format string has no double to widen from, and this ADR gives the operator the road the guard was standing in front of)
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md), [ADR 0229](0229-a-string-is-an-index-into-a-table-the-runtime-can-add-to.md), [ADR 0230](0230-building-a-string-at-run-time-is-a-buffer-an-intern-and-the-same-index.md), [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md), [ADR 0258](0258-str-and-repr-are-one-printer-pointed-at-two-sinks.md), [ADR 0171](0171-generated-runtime-constants-come-from-the-canonical-tables.md), [ADR 0173](0173-a-runtime-block-travels-with-the-module-that-references-it.md), [ADR 0182](0182-one-kind-table-for-the-printer-the-comparison-and-the-raise.md), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md), [ADR 0212](0212-exceptions-are-caught-by-class-and-a-trap-is-an-exit-code.md), [ADR 0214](0214-the-trap-message-is-the-reference-s-wording-not-the-runtime-s.md), [ADR 0249](0249-a-refusal-names-what-is-missing-and-never-blames-a-construct-the.md), [ADR 0359](0359-an-interpolation-is-a-text-value-not-only-a-printing-instruction.md), [ADR 0355](0355-a-print-statement-writes-one-line-and-writes-it-after-every-argument-is-val.md)
Roadmap rows: Gap R.31 (paid — printf-style `%` on a text), L11.1 (advanced — a formatting joins the positions that answer an `@str_tab` index), Gap R.165 (the wrong answer this closes the second time), Gap R.146 (the one-word position: a formatting is a text, so the position keeps the word the language uses for a text), Gap R.33 (`str * int`, still owed)

## The gap, as measured

`%` has two operators spelled with one character, and this backend had one of them. Between two numbers it is
the remainder and answers (`7 % 3` is `1`, `7.5 % 2.5` is `0.0`). On the left of a text it is the reference's
**formatting**, and for eleven ADRs the answer was a refusal:

```gusty
print("%s" % 2)                                     # TypeError: not all arguments converted …  in CPython: 2
print("hi %s, you are %d" % ("Ada", 36))             # refused at compile time                    in CPython: hi Ada, you are 36
print("%05d|%-5d|%+d|% d" % (42, 42, 7, 7))          # refused                                    in CPython: 00042|42   |+7| 7
print("%(who)s scored %(score)d" % {"who": "Ada"})   # refused                                    in CPython: Ada scored 1 … (KeyError for the rest)
```

`integration/programs/probe_percent_format.gy` recorded that state as a debt row with the compiled leg
**missing**: the record leg raised the operand `TypeError` ADR 0215 gave it, the compiled backend refused to
lower the line at all, and CPython formatted all three. Roadmap Gap R.31 held it, `L11.1` beside it, because the
reason was not taste — it was that a compiled text is an index into `@str_tab` (ADR 0229), and the only thing the
numeric road could do with an index was widen it. Which is exactly what it had done:

```gusty
print("%.2f" % 3.5)   # printed 0.0 at exit 0 — the format's @str_tab index, sitofp'd and frem'd against 3.5
```

That is Gap R.165, closed by ADR 0282's guard. The guard was correct — a text has no double to widen from — and
it stayed correct: nothing in this ADR widens a format string. What the guard left behind was a language without
`%`-formatting, and a numeric door standing in front of an operator that was never numeric.

## The decision

**1. The formatting is a run-time door, and it is the only text-`%` road in the module.**
`pkg/lang/percent_runtime.go` emits `@rt_format`: it walks the format's bytes, parses each conversion with the
reference's grammar (flags `- + ␠ # 0`, field width, `.precision`, `*` and `.*`, positional `%2$s`, mapping
`%(key)s`), and renders each argument. Parsing it at run time is not a shortcut — the reference parses at run
time, and a compile-time reader would have to refuse every format that arrives from a variable
(`print(fmt % 3)`), which the door answers by running.

**2. Every argument is handed to the door as a `(payload, tag)` pair, and the renderers are the ones the module
already has.** `%s`, `%r` and `%a` ask `rt_str_of_value` — ADR 0258's one printer, the door `str`, `repr`, an
f-string field and `print` share — so a container renders as its slots and the void renders as `None` rather than
as the number underneath them. The numeric conversions ask `rt_lift_num` (ADR 0232's family) and the C library's
`snprintf` through a spec the door assembles; the wrong-kind raises ask `rt_kind_name`, so the sentence names the
kind the pair carries and no per-kind string table is spelled here (ADR 0182). The mapping form asks
`rt_dict_find`/`rt_dict_get_tagged`/`rt_dict_value_tag` — the same doors a subscript asks.

**3. A formatting is a text, and one predicate says so.** The door answers an `@str_tab` index, which is the
single word a text is in this backend (ADR 0229), and `percentIsTextExpr` is the only answer to "is this
expression a text" for every road that reads one: `exprIsString`, `printsAsInternedStr`, the container builder's
`heapElemKind`, an instance slot's `scanStringAttrs`, and the two pre-scan doors that register a function as
returning a text. That is ADR 0229's rule and ADR 0359's rule applied to a third construct. It is not
cosmetic: with only the print road taught, `print(f())` where `def f(): return "%d items" % n` printed `1` —
the table's position — and `print(d["k"])` printed `2`.

**4. The tuple on the right is an argument list, not a value.** `("%d-%d" % (1, 2))` spends two arguments;
`(1, 2)` is never built, because tuples are still not values here (`L11.3`). This is the one place in the
language where a tuple spelling is consumed rather than lowered, and it is sound because the tuple's only
meaning in that position is positional. A tuple anywhere else keeps refusing exactly as it did, and a tuple that
would have to *be* an argument (`"%s %s" % ("a", ("b", "c"))`) refuses by naming the argument whose pair cannot
be built.

**5. The traps are the reference's, and the first one wins.** `TypeError: %d format: a real number is required,
not str`, `KeyError: 'missing'`, `OverflowError: %c arg not in range(0x110000)`, `ValueError: unsupported format
character 'Q' (0x51) at index 1`, `TypeError: * wants int` — raised through the module's one raise road
(ADR 0212, ADR 0214), catchable by the `except` arm the program wrote, and reported by the door's own message
buffer rather than by a sentence the compiler guessed. A walk that raises twice reports the **first** raise:
`@rt.pct.sealed` is set by the first `rt_pct_msg_begin` and every message piece is written through
`rt_pct_mput`, which writes nothing while the seal is down. Without it, `"%(missing)s"` against a dict without
that key reported `KeyError: ''incomplete format` — the second complaint appended to the first — where the
reference unwinds at the key.

**6. What the door declines, it says at compile time, naming its own cause.** A conversion with no renderer
(`%b`, `%B`, `%u`) and an argument with no value (a nested tuple, a lambda, a generator) are refused where they
are written, at exit 1, with the capability code `lower.unsupported.printf-style-format` and a suggestion that
lists the conversions that do work. The dispatch asks the **plan**, not the kind predicate, precisely so a
refused conversion is refused *by the door that owns it*: falling through to the generic text-`%` sentence told
the reader the operator was missing, which it is not (ADR 0249).

**7. The kind and exception constants are generated, never hand-spelled.** `percentRuntimeBlock` substitutes
every `@TAG_*@` token from `ValueTagList()` — the canonical table in `pkg/lang/value.go` — and every
`@CODE_*@` from `exnClassCode`, so the door cannot drift from the table that decides what `tag == 5` means
(ADR 0171). A test row fails the build if an unsubstituted token survives into the block, or if a backtick ever
appears inside it, which would silently end the Go raw string that holds it.

## Why the door is IR and not a Go-side evaluator

Two reasons, and the second is the one that decided it.

The first is that the emitter has no builder: `pkg/lang/codegen.go` writes textual IR, and a formatting has to be
lowered where the values are. The second is ADR 0359's lesson and Gap R.146's: a formatting answers from the
`(payload, tag)` pair a value carries, and the pair is only knowable where the pair lives. A Go-side evaluator
would have to be told the kind, and being told the kind is how `print(f"v={None}")` came to write `v=0` and how
`%.2f` came to print `0.0` — the compiler's assumption replacing the object's answer. The door is handed two
`i32`s per argument and no third word in which to put an assumption; the compile-time gate's job is only to
refuse the arguments whose pair it cannot build at all.

## Alternatives rejected

- **Keep refusing.** Ten ADRs of refusals, each honest, and a language that cannot write `"%.2f" % v` — the
  single most-used formatting idiom of the language it imitates. Refusing was right when the alternative was a
  wrong number; it stopped being right the moment the door existed.
- **Lower the whole format at compile time into a printf format string.** `%*`, `.*`, `%(key)s` and a format that
  arrives from a variable are all run-time facts, and the failure mode is the old one: the compiler choosing a
  rendering for a kind it cannot see. ADR 0299 made the same argument about f-string specs and filed the residue
  as Gap R.308 rather than guessing.
- **Make the answer a heap string instead of an `@str_tab` index.** The module has one way for a text to travel
  (ADR 0229); a second representation would need a second everything downstream — equality, concatenation,
  `len`, container slots, the printer.
- **Build a tuple to hold the arguments.** Tuples are not values in this backend (`L11.3`); building one to
  unpack it immediately would add a representation, a collector rule and a printer for zero observable benefit.
- **Answer `%b`/`%u` as `%s`/`%d`.** `%b` belongs to bytes and `%u` is the obsolete conversion the reference
  removed; a rendering for a conversion the reference does not have in this domain is a feature nobody can write
  against.

## Consequences

- `integration/programs/probe_percent_format.gy` leaves the debt ledger and becomes parity surface
  (`conformanceStandalone()`), byte-for-byte against CPython, with the traps caught by the program's own arms and
  a remainder line at the bottom so the probe distinguishes "formatting answered" from "the operator is gone".
- The record leg gains 31 sources (`7119 → 7150`, recomputed by `pkg/lang/golden_artifact_counts_test.go`), all
  of them recorded **from the reference** — the retired engine never had an answer for `text % x`, which is what
  the record's `meta.added_after_0302` exists to say. `pkg/lang/percent_golden_test.go` asks each one.
- `pkg/lang/capability.go`'s `lower.unsupported.printf-style-format` row changes meaning: it no longer reports a
  missing operator, it reports the conversion or the argument the door declines.
- Two neighbouring defects surfaced while the probe was being made to match and are recorded as their own rows:
  `%10s` beside a non-ASCII text was one byte short because `rt_str_byteoff` stopped at a lead byte instead of
  stepping over a code point, and `print("a\0b")` writes `a` because the print door hands `printf` a `%s` — both
  are the reference-vs-compiled divergences they now are recorded as.

## The answer has to travel, not just exist

Rendering the text is half the feature. A text is a value in this module — it binds, measures, concatenates,
compares, enters a container slot, sits in an instance slot, crosses a call boundary and comes back from a
`return` — and each of those is a separate road that asks "what kind is this expression?". A door that answers
`rt_format` while four predicates keep their own opinions is the Gap R.146 family in a new spelling, and three
of them were measured this round, each producing a digit rather than a refusal:

- `isStringExpr`, the shape question the pre-passes ask, did not know a formatting. Without it the call-site
  census never learned that `w("%d up" % 7)` hands `w` a text: `def w(t): print("<" + t + ">")` refused the
  concatenation and `print(t)` printed `1`, the position of an interned string.
- `isFloat` read `"%s" % 3.5` as a remainder with a float operand, so the assignment road asked the double lift
  for the format itself and the L11.2 text guard refused the program: `u = "%s" % 3.5` could not be bound.
  The operator is one character and two questions; the plan tells them apart, and `percentFormatOwnsBinOp` is
  the recursion-safe form the numeric predicates ask.
- `pairSpecFor` narrowed a string-returning body's parameters along with its answer, so `def f(v): return
  "%.2f" % v` called as `f(3.5)` rendered `3.00` and `"%s" % v` rendered `3`, both at exit 0. The answer keeps
  the string convention (an `@str_tab` index); the *parameters* are a different boundary, and `formatsReadAParam`
  is what opens the tag word for them.

The rule taken from this: the plan is asked once, by the census and by the builder alike
(`percentArgKindKnown` and `percentArgPair` read the same three predicates), and where no record can name an
argument's kind the program is refused rather than rendered sideways. `pkg/lang/percent_roads_test.go` is the
regression for the roads, and `TestTheTwoRoadsOfOneFormattingAnswerTheSameThing` is the guard that keeps the
bound print and the direct print from drifting apart again.

## What a probe can witness, and what a doc table owes

The residue shapes — an integer beyond the bounded-word row (`"%d" % (2 ** 70)`), the float literal's exponent,
a tuple where a value goes, a code point the program computed — were first written as four lines of one probe
program. The conformance matrix reported one verdict for that program, and the verdict was true: the compiled
leg refuses. What the verdict hid is that a front-end refusal ends a program before its first line runs, so the
three refusals behind the first were never reached, and a probe that is one program can carry one witness. The
row now keeps the honest version — the probe is the bounded-integer line, and it says in its own comment which
siblings it does not cover and where they live — and each sibling is a program of its own in
`TestPercentArgumentResidueAtTheCLI`, with the reference's answer written into the row beside this backend's.
That includes the shape nobody likes writing down: a computed `%c` of `0xD800` prints the three bytes the
encoder refused (`0xED 0xA0 0x80`) at exit 0 while the reference raises `UnicodeEncodeError`, so the test pins
those bytes. A pinned wrong answer that names its missing class is worth more here than a refusal invented to
make the family look uniform.

The same reasoning moved the documentation from prose to artifact. `docs/language.md` § *Printf-style `%`*
states its promises as a fenced block whose comment is the answer the line prints and a table of
`program | Class: message` rows; `TestTheLanguageDocsPercentSectionAnswersWhatItSays` parses both, compiles and
links each documented program, and fails when the compiled leg prints anything else — the positive rows are
compared against CPython as well, because a documented answer that only the compiled leg gives is a documented
bug. The case floors the row counts, so the section cannot be thinned from the doc side either.
