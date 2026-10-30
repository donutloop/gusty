# ADR 0469 — a prefix the grammar never had is a door of its own, not a zero added to the operand

**Status:** Accepted
**Date:** 2026-10-31
**Decides:** roadmap **Gap R.244** (unary plus is not in the grammar: `print(+1)`, `print(+1.5)`, `print(+True)`, `print(-+1)` and `x = 5` / `print(x-+1)` were parse errors at exit 1 where the reference answers `1`, `1.5`, `1`, `-1`, `4`)
**Files:** **Gap R.564** (the `+` declines an operand no record names a number — a parameter, a slot two levels deep, the winner of a `min`, a callee's container or `None` answer), **Gap R.565** (a door that declines its argument before lowering it never asks the operand's own raise — `len(+"ab")`), **Gap R.566** (the prefix rule accepts `<prefix> not` — `print(+not 1)`, and its sibling `print(-not 1)` — where Python's grammar stops)
**Keeps open:** **Gap R.240**, **Gap R.242**'s neighbours, L11.1
**Related:** ADR 0338 (the lexer's `-N` rule this row's `-` twin came from, and the filed row it pinned for `+`), ADR 0266 (the negation's kind door, `negationOperandKind`, which this row asks rather than re-write), ADR 0271 (one table, one wording per operation — the reason `pos` is a new key and not a copy of `neg`), ADR 0265 (the tagged-slot door, whose `slotNumberFamily` proof this row borrows), ADR 0232 (one question asked once; a kind read from a record, never inferred from spelling), ADR 0249 (a refusal must name what is missing), ADR 0337 (a negative control beside every absence-assertion), ADR 0302 (the two witness legs), ADR 0166 / ADR 0211 (exit 2 is the compiler's bug; a trap is exit 3, a refusal is exit 1)

## The rule

**`+x` is the operand, or the reference's `TypeError` — and never an arithmetic instruction.** The prefix
asks its operand what kind it is, once; a kind the records name as having no sign raises
`bad operand type for unary +: '…'` catchably at exit 3, a number the records can name answers the operand's
own word, and anything else is refused in words. There is no `add` in the road, because there is nothing to add.

## Context

The prefix `+` was not a missing feature with a missing lowering. It was missing from the **grammar**:

```
$ gustyc --eval 'print(+1)'
gustyc: parse error at 1:7: unexpected token                                        # exit 1
$ gustyc --eval 'print(+1.5)'   # parse error                                       # exit 1
$ gustyc --eval 'print(+True)'  # parse error                                       # exit 1
$ gustyc --eval 'x = 5
               print(x-+1)'     # parse error at 2:9                                # exit 1
```

while `print(-1)`, `print(-1.5)`, `print(-True)` and `print(5--1)` had always answered. ADR 0338 repaired the
lexer's `-N` rule two rows ago, discovered the asymmetry, and filed it as **Gap R.244** — with a row in
`minus_operator_test.go` and two in its CLI twin pinned to the parse error, which is the way a filed row is
supposed to behave: it fails on the day the fix lands, and moves with it.

## The measurement that decided the shape

The obvious lowering is the additive identity: rewrite `+x` as `x + 0`, reuse every arithmetic road, ship.
It was built, and it was measured against CPython. Three ways it is wrong:

| program | CPython | `x + 0` |
|---|---|---|
| `print(+1)` | `1` | `1` ✓ |
| `print(+1.5)` | `1.5` | **`1`** — the neutral element was an INT literal and the binary road chooses its domain from the operand spellings |
| `x = 5.5` / `print(+x)` | `5.5` | **`0`** |
| `print(+None)` | `TypeError: … 'NoneType'` | **`0` at exit 0** |
| `print(+"a")` | `TypeError: … 'str'` | front-end refusal: *can only concatenate str (not "int" to str)* |
| `print(+[1, 2])` | `TypeError: … 'list'` | `TypeError: can only concatenate list (not "int" to list)` |
| `print(+{1, 2})` | `TypeError: … 'set'` | `TypeError: unsupported operand type(s) for +: 'set' and 'int'` |

The last three sentences are about a program with a `0` in it that nobody wrote. An identity element is a
guess about the domain — int or float, list or text — and the domain is exactly the thing this operator exists
to ask. That is why the reference has `__pos__` rather than `__add__` with a zero, and why this backend now has
a door rather than a desugaring.

## The decision

**One new prefix node, one new road, zero new arithmetic.**

1. **The parser** grows a `+` beside the `-` it already has in `parsePrefix` (`pkg/lang/parser.go`), producing
   `&UnOp{Op: "+"}`. The AST keeps the prefix the source wrote, so `--emit-ast` shows `{"op": "+", "x": …}` and
   an agent reading the tree sees the program, not the lowering. `x-+1` parses as `x - (+1)`, `+=` is still one
   token and still the augmentation, and `1 ++ 2` is still the reference's `3`.
2. **The road is the negation's shape, in its own file** (`pkg/lang/unary_plus.go`), sharing everything
   shareable with `pkg/lang/negation.go` and differing in the two places the reference differs: the sentence
   and the value.
   - **The raise** is asked of `negationOperandKind` — the ADR 0266 door that names the kind from a literal,
     from the last-binding records, from the class table, from `callReturnsStr` — and emitted through
     `emitBadSignless(b, "pos", kind, span)`, whose wording comes from the new `"pos"` arm of
     `unsupportedNumberOp`: `bad operand type for unary +: '%s'`. A slot whose kind only the object knows
     walks `emitBadSignlessOfSlot`, one branch per tag, so every path out of the read raises.
   - **The value** is `g.value(b, n.X)` in the integer domain and `g.floatValue(b, n.X)` in the double one,
     reached through an `isFloat` arm that is transparent to the prefix. No `sub i32 0`, no `fsub double 0.0`,
     no `sitofp` of a word that is not a double.
3. **The proof is positive, not the absence of a name.** `-x` can afford to hand its operand to the numeric
   road and trust that road's refusals, because a subtraction of a handle is still a number-shaped word. `+x`
   hands back the operand *itself*, so an operand whose kind is unknown would print a text, a container or an
   exception message as the answer to a program the reference stops on. The road therefore answers only where
   `unaryPosOperandIsANumber` can **state** the number-ness from a record: a literal; a verdict (`<`, `==`,
   `in`, a chain, `not`) which the reference's `__pos__` answers as an int; an arithmetic tree of proven arms;
   a name whose every non-number record is empty and whose binding the walk recorded; a slot the literal
   describes as holding numbers (`slotNumberFamily`, the ADR 0265 proof) or whose folded element the record
   names (`constantSubscriptKind`, ADR 0420); a builtin whose contract is "a number or a stop" (`int`, `float`,
   `len`, `ord`, `abs`, `round`, `pow`, `sum` — and NOT `min`/`max`, which answer whichever operand wins); or a
   callee every one of whose `return`s the same walk proves. Anything else refuses in words (Gap R.564).
4. **The refusals name themselves.** `cannot answer +<summary>: the operand's kind is not a number this
   backend can name, and unary + hands the operand back unchanged (roadmap Gap R.564)` — ADR 0249's rule that a
   refusal is a name for a missing shape, not the word `unsupported`.

## Alternatives rejected

- **`+x` → `x + 0` (the additive identity).** Measured above; three wrong answers, two of them at exit 0, and
  three sentences about a literal the source never mentions.
- **`+x` → `-(-x)`, the double negation.** Value-correct, and it reuses the door; rejected because the raise
  carries the *inner* prefix's sentence (`unary -`), and because lowering the operand twice is how
  **Gap R.365**'s "printed `boom` twice" family is born.
- **Desugar in the parser to a `BinOp`.** One fewer AST node, and it makes the AST dump lie: `--emit-ast`
  would show a `+` node with a `0` the human never wrote, which is the artifact an agent plans against.
- **Trust the operand when the raise door cannot name it.** This is the shape that prints an interned `@str_tab`
  index as a number; the whole history of this directory (ADR 0166, ADR 0265, ADR 0266, Gap R.137, Gap R.140)
  is the history of that decision being wrong. A refusal costs exit 1 and a roadmap row.
- **A `+` arm written into the numeric road's operator switch.** It would make the sentence the binary one and
  put the raise after the operand had already been lowered — the order ADR 0266 exists to prevent.

## What the row leaves standing

Measured on the same binary, each filed under its own row rather than guessed at:

| program | CPython | this backend |
|---|---|---|
| `def f(a): return +a` / `print(f(3))` | `3` | refusal (Gap R.564) |
| `xs = [[1], [2]]` / `print(+xs[0][0])` | `1` | refusal (Gap R.564) |
| `print(+min([3, 1]))` | `1` | refusal (Gap R.564) |
| `x = 5` / `def g(): return +x` / `print(g())` | `5` | refusal — the binding is the module's, the function's records never saw it (Gap R.564) |
| `def h(): return [1]` / `print(+h())` | `TypeError: … 'list'` | refusal (Gap R.564 with Gap R.229) |
| `def h(): pass` / `print(+h())` | `TypeError: … 'NoneType'` | refusal (Gap R.564) |
| `print(len(+"ab"))` | `TypeError: … 'str'` | refusal at `len`, which never lowers its argument (Gap R.565) |
| `print(+not 1)` | `SyntaxError` | answers `0` — the prefix rule accepts `<prefix> not`, exactly as the `-` rule ADR 0338 shipped already does (Gap R.566) |

None of them is exit 0 with a wrong value, and none is exit 2.

## IR implications

- `+x` emits **no arithmetic instruction**: the witness asserts `sub i32 0,` and `fsub double 0.0,` are absent
  from the program's own functions for six `+` programs, with two counter-rows (`x = 5` / `print(-x)`,
  `x = 1.5` / `print(-x)`) that must still contain them, so the absence cannot be earned by deleting the
  negation (ADR 0337's negative-control discipline).
- The raise is the ordinary emitted one (`raiseTo`), so the block it lands in is terminated and a fresh label
  opens for whatever the caller still emits — the same shape ADR 0265 established for a tagged slot.
- The double domain's `isFloat` grew one arm, and it is transparent: `+1.5` is a float because what stands
  behind the prefix is one. The float leg returns the operand's own double, never a widened word.

## Agentic rationale

- `--emit-ast` shows the prefix the source wrote (`{"op":"+"}`), so an agent reading the tree is reading the
  program and not the lowering; `--check` types `+x` as the operand's type through the road `not`/`-` already use.
- The refusals are greppable, cite the row that owes the work, and never appear where a value was returned;
  the exit-code contract is unchanged (0 answered, 1 refusal, 3 trap).
- 76 new record entries carry the answer, the trap class and the sentence for every program in the row, so the
  behaviour is checkable without running anything (`tools/recmerge` recorded them from the reference with the
  compiled leg cross-checked; none self-certifying).

## Witnesses

`pkg/lang/unary_plus_test.go` — 54 answered rows (compiled leg + JIT + record, each `want` measured from
CPython), 17 raise rows (class AND whole sentence, on the compiled leg and against the record), 4 handler rows,
the proof table (10 operands the road must prove, 6 it must decline, plus the nil-generator soundness row), the
IR absence with its counter-rows, the neighbour table (16 rows: the negation's answers, the negation's and
`abs`'s own sentences, `-=`, `*=`, the infix `+`, a list concatenation, `2 ** -1`, `xs[-1]`), and the filed table.
`integration/unary_plus_test.go` — 41 CLI answers cross-checked against a live `python3` inside the case, 14 CLI
raises with the traceback's last line read out of the reference, 3 handler rows, the program-vs-`.expected`
line-for-line witness, the corpus program's record guard, the IR absence with its counter-rows, and 7 filed rows
each carrying both backends' answers. **76 record entries** came in from the reference with the compiled leg
cross-checked (**10786 → 10862**), none self-certifying, including the corpus program's own entry. `integration/programs/unary_plus.gy` joined the conformance corpus (289 →
290; matrix 275 → 276 rows, parity-asserted 239 → 240, oracle `match` 225 → 226, `fail` 0, `drift` 0).
