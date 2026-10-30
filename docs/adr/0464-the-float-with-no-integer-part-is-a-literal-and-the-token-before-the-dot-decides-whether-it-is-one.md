# ADR 0464 — the float with no integer part **is** a literal, and the token before the dot decides whether it is one

**Status:** Accepted
**Date:** 2026-10-30
**Decides:** roadmap **Gap R.245** (`.5` does not lex), and the reach of **ADR 0338**'s previous-token rule
**Filed:** nothing — the neighbouring shapes were measured and belong to rows that already exist
(**Gap R.135** owns the exponent marker; `1e5` is refused today, so `.5e2` is refused *with* it)
**Keeps open:** **Gap R.244** (unary plus), **Gap R.546**, **Gap R.547**, **Gap R.548**, **Gap R.550**
**Related:** ADR 0338 (the lexer's minus rule is asked what came before it), ADR 0245's number fold, ADR 0266
(the negation road owns its own sentence), ADR 0273 (a gate is asked as a function), ADR 0299 (an
interpolation's spec is cut before its field is lexed), ADR 0166, ADR 0229

## The rule

**When a character has two jobs, the token before it answers which job it is doing — and the answer is one walk
that the whole lexer shares, not one walk per operator.**

## Context

One character, five refusals:

```
$ gustyc --eval 'print(.5)'
gustyc: parse error at 1:7: unexpected token
$ python3 -c 'print(.5)'
0.5
```

and the same for `print(1+.5)`, `print(-.5)`, `print(.5+.5)` and `x = .5` — the reference answers `1.5`, `-0.5`,
`1.0` and `0.5`. The cause was in one function: `lexNumber` has a case for a digit, a case for the `0x`/`0b`/`0o`
prefixes, and no case for a leading `.`.

The obvious fix — accept `.` followed by digits — is wrong, and the row said so before this ADR existed:

> enter it only where the `.` cannot be the member operator — the previous token decides, exactly as
> `minusBeginsLiteral` decides for `-`, so `obj.5` stays an attribute error rather than becoming …

The reason is that `.` already has a job. It is the member operator, so `.` followed by a digit is exactly what
`obj.5` looks like — a program the reference *rejects*:

| source | CPython | the naive fix would give |
|---|---|---|
| `print(.5)` | `0.5` | `0.5` ✓ |
| `obj = 1` / `print(obj.5)` | `SyntaxError` | `obj` `0.5` → some answer at exit 0 |
| `print(1 .5)` | `SyntaxError` | `1` and `0.5` side by side |
| `print(._1)` | `SyntaxError` | a lex error about a numeric literal nobody wrote |

A lexer that decides from the character *after* the dot is a lexer that accepts a program the reference refuses —
the same class of drift that made `y = x-1` print `5` at exit 0 before **ADR 0338** taught the minus to ask what
came before it (**Gap R.242**).

## The decision

`pkg/lang/lexer.go` gains a leading-`.` arm in `lexNumber` and one case in `Lex`, and the case is entered only on
**ADR 0338**'s walk:

```go
case c == '.' && i+1 < n && isDigitForBase(src[i+1], 10) && dotBeginsLiteral(toks):
```

```go
// dotBeginsLiteral asks the same question, of the same walk, for the other operator whose leading-character
// meaning depends on what came before …
func dotBeginsLiteral(toks []Token) bool { return minusBeginsLiteral(toks) }
```

1. **One walk, two operators.** A `.` begins a literal exactly where a `-` would begin a literal: after the start
   of a line, an operator, `,` `:` `(` `[` `{` `=`, or a keyword such as `return`, `in`, `else`, `not`. After a
   name, a number, a string, `)` `]` `}`, or `True`/`False`/`None`, the previous token can END an expression, and
   the dot is the member operator. Writing the table twice would let the two rules drift, and the drift would be
   invisible until a program answered differently on the two sides of a dot. A float counts as an expression end
   for the same reason it does for the minus: `1.5 .5` is refused by the reference, so a rule that let the second
   dot begin a literal would accept a program the reference rejects.
2. **The token is an ordinary float, spelled as the program spelled it.** `TokFloat` with `Text = ".5"`, the
   program's span, and the value from `strconv.ParseFloat("0."+digits)`. Nothing downstream learns that this
   literal was once a hole: the fold, the printer, the float word and the interpolation roads all see the token
   they already knew. That is why the fix is a lexer arm rather than a parse-time special case.
3. **`-.5` stays the negation road's, by not merging it.** The minus takes a digit into its literal and nothing
   else (**ADR 0338**), so `-.5` reaches the parser as `OP:-` `FLOAT:.5`, and the answer `-0.5` is written by the
   unary-minus road — the one that owns `bad operand type for unary -: 'str'` (**Gap R.137**, **ADR 0266**). A
   lexer that merged `-.5` into one token would print `-0.5` today and lose that raise tomorrow, which is the
   trade this row refuses; the test asserts both halves.
4. **The `_` rules are the fraction's, not new ones.** The arm calls the same `scanDigits` the fraction below
   calls, with `allowLeading = false`: `.5_1` is `0.51` (the reference agrees) and `._1` is not a number on either
   tool. Inventing a separator rule for the new spelling would mean two grammars for one number.
5. **The exponent is left where it was found.** `.5e2` is refused, and so are `1e5` and `1.5e2` — the marker
   belongs to **Gap R.135**. The pin is deliberate: a row that accepted `.5e2` while its sibling refused `1e5`
   would ship a number grammar in two pieces, each half-implemented by a different cycle.

## What the walk costs, measured

The gate is a backwards walk over tokens already emitted, which the minus has paid since **ADR 0338** — no
lookahead past the digits, and no new state on the lexer. The token stream answers for the whole design:

```
print(.5)      →  IDENT:print OP:( FLOAT:.5 OP:)
print(1+.5)    →  IDENT:print OP:( INT:1 OP:+ FLOAT:.5 OP:)
print(-.5)     →  IDENT:print OP:( OP:- FLOAT:.5 OP:)
x = .5         →  IDENT:x OP:= FLOAT:.5
print(obj.5)   →  IDENT:print OP:( IDENT:obj OP:. INT:5 OP:)      ← refused, as the reference refuses it
print(1 .5)    →  IDENT:print OP:( INT:1 OP:. INT:5 OP:)          ← refused, as the reference refuses it
print(._1)     →  IDENT:print OP:( OP:. IDENT:_1 OP:)             ← refused, as the reference refuses it
```

## Why the two spellings must answer identically

`.5` and `0.5` are one number written twice, so the row carries an assertion the original five shapes do not
give it: for nine programs, the leading-dot spelling and the leading-zero spelling must produce **byte-identical**
output on the compiled leg (`TestTheTwoSpellingsOfTheSameFloatAnswerIdentically`). It is the cheap statement of
what this fix was not allowed to become — a second float road that means something slightly different. The rule
earned its keep during the cycle: `print(-.5)` was first written as one merged token, which would have passed
every answer row and quietly moved `-.5` off the negation road.

## Tests

- `pkg/lang/leading_dot_float_test.go` — the gate as a function (token streams, float payloads, spans), the
  five refusals the reference also refuses, the answered family on the compiled leg, the same family on the
  record leg (`HasGoldenAnswer`/`goldenStdout`, with the count of covered rows asserted so the leg cannot quietly
  stop being walked), the two-spelling parity family, the negation-road row, and the exponent-consistency row.
- `integration/leading_dot_float_test.go` — the family at the CLI against a live `python3` read inside the case
  (including the spellings that already worked, and `1-1`/`y = 5-1` so the minus's own rule is re-proved beside
  the dot's), the refusals pinned on **both** sides (a row cannot be closed by drifting the language), a program
  run by path, and the exponent siblings. No row may reach **ADR 0166**'s exit 2.
- `integration/programs/leading_dot_float.gy` — the answered half of the family as a `match` row of the
  conformance matrix. The refused spellings are deliberately **not** in it: a conformance program lists things
  the toolchain answers, and `obj.5` is not one.
- **38 record entries** (the family, its refusals, the two-spelling parity programs and the corpus program itself) came in from the reference through `tools/recmerge`
  with the compiled leg cross-checked; record **10649 → 10687** — 22 of them this row's five refusals and the positions a literal has to
  reach, 16 more from the parity and negation families, every one of them refused by `recmerge` unless the compiled leg agreed with the reference.

## Alternatives rejected

- **Accept `.digit` wherever it appears.** Accepts `obj.5` and `1 .5`, both of which the reference rejects;
  that is a language drift filed as a bug fix.
- **Decide by looking ahead past the dot.** Two characters of lookahead would settle `obj.5` too, but the lexer
  is single-pass and **ADR 0338** already ruled that these decisions are made from what was emitted, not from
  what follows — and a two-character window is where a `-`-versus-operator rule starts disagreeing with itself.
- **Fold `.5` in the parser** (see `.`, digits, build a `FloatLit`). Leaves the token stream wrong, so any road
  that reads tokens — the formatter, the LSP, the source-map table, error spans — sees a program the user did
  not type.
- **Merge `-.5` into one float token.** Prints `-0.5` today; removes **Gap R.137**'s raise tomorrow.
- **Support `.5e2` while at it.** The marker is **Gap R.135**'s row, its neighbours are refused, and half a
  number grammar is worse than none.
- **Refuse the spelling with a front-end message ("write `0.5`")**. The reference runs these programs; a refusal
  is exit 1 where the reference exits 0, and **ADR 0273**'s rule is that an exit-1 refusal costs a program.

## Consequences

- `docs/language.md` § Numeric literals states the two spellings, the member-operator rule, and the `_` rules
  that the fraction and the leading-dot form share.
- The record grew by 38 entries; the artifact counts in `AGENTS.md`, `docs/operations.md` and this file's
  Snapshot are recomputed by their guards (**Gap R.204**), not by this ADR's memory.
- **ADR 0338** gains a second customer, which is the point: the next character with two jobs (a `+` that is both
  an operator and — under **Gap R.244** — a sign; a `*` that is both a member and an unpack) asks the same walk,
  and the row that adds it should extend the walk rather than copy it.
