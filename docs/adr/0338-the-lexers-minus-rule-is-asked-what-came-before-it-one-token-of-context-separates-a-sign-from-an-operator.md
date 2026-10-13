# ADR 0338: the lexer's `-` rule is asked what came before it — one token of context separates a sign from an operator

Status: accepted (2026-10-14)
Amends: nothing directly — this is the lexer's own rule, and the first ADR to say that a token's *meaning* is decided by the token before it as well as the characters after it
Extends: [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md) (a refusal is a diagnostic, and a parse error is the front end's; the shapes this row *answered* were never capability boundaries, so a refusal there is a wrong answer), [ADR 0249](0249-a-refusal-names-what-is-missing-or-it-is-a-bug.md) (what the row leaves refused still has to name itself), [ADR 0273](0273-a-gate-is-pure-no-trial-emission-no-counters-advanced.md) (a rule that reads its context advances nothing and emits nothing — it decides a token boundary and stops), [ADR 0302](0302-the-ast-interpreter-is-retired-one-execution-backend-remains.md) (the newly answered programs are recorded from the reference, and the compiled leg is cross-checked by the matrix)
Supersedes: nothing
Related: roadmap L11.1/L11.2 (the tagged value word, which owns the comprehension the row leaves refused, ADR 0192), Gap R.242 (closes), **Gap R.244 (newly measured here, filed here)**, Gap R.82 (string arithmetic, which the row's probe found already filed)
Owner: `pkg/lang/lexer.go`, `pkg/lang/lexer_test.go`, `pkg/lang/minus_operator_test.go`, `integration/minus_operator_test.go`, `integration/programs/minus_operator.gy`

## Context

Measuring the slice door (ADRs 0335–0337) turned up a program that is not a slice at all, and it is the worst class
the queue carries — a wrong value on stdout at the code that means the program ran:

```
x = 5
y = x-1
print(y)      # CPython: 4
              # this toolchain: 5      at exit 0, with nothing on stderr
```

The subtraction was not mis-evaluated. It was **never parsed**. `pkg/lang/lexer.go` carried

```go
case c == '-' && i+1 < n && unicode.IsDigit(rune(src[i+1])):
```

which reads `-1` as a single `TokInt` whenever a minus is *directly followed by a digit*, whatever token came
before it. So the program above is lexed as `y = x` followed by an expression statement holding `-1`, and an
expression statement's value is not printed by anything. The answer is the value the subtraction never produced,
delivered at exit 0, and there is no diagnostic in the system that could have said otherwise.

The same rule met a second face where the parser had nowhere to put the stray literal:

```
print(1-1)                 # parse error at 1:8: expected ")"
print(len(xs)-1)           # parse error at 1:19: expected ")"
print(xs[2-1])             # parse error: expected "]"
for i in range(3-1):       # two parse errors
def g(n): return n-1       # answered "5 0" — the reference answers "4 -1"
```

one defect, two faces: the silent one and the refused one. And the tell was in plain sight: `x - 1`, with a space,
was right every time. That is why a corpus written in this house's style — where the space is always there — never
saw it, and why the row ranks with the exit-2 shapes despite being merely a wrong number: it is wrong *silently*,
in the most ordinary arithmetic in the language, for the one character a beginner types without a space.

## Decision

**The `-` before a digit is asked what it follows.** One question, asked of the token already in hand at the point
`emit` is called (`minusBeginsLiteral`), with no look-ahead past the digit and no redesign:

- **after something that can END an expression** — a number, a name, a text (any of the five string forms), a
  closing `)` `]` `}`, or one of the three literal keywords `True`, `False`, `None` — the minus is the **operator**,
  and the parser's existing unary-minus rule takes whatever comes next;
- **everywhere else** — start of a line, after an operator, after `,` `:` `(` `[` `{` `=`, after a keyword such as
  `return`, `in`, `else`, `not` — `-N` stays **one literal**, exactly as it was.

Three details are the whole row, and each is asserted:

1. **`True`, `False` and `None` end an expression.** They are keywords, so a rule keyed on "keyword → sign position"
   would lex `print(True-1)` as `True` and a stray `-1`; the reference does arithmetic there (`0`), and so now does
   the compiler. Every *other* keyword begins a position, which is what keeps `return -1`, `in -1` and an `else`
   branch's `-1` merging.
2. **A `TokWarning` is skipped when looking back.** The homoglyph path appends a warning token after an identifier;
   a rule that read it as "not an expression ender" would hand the sign treatment to `а-1` and change the answer of
   a program nobody should write but whose diagnosis must not depend on a warning.
3. **`-=` is untouched.** It is matched by the longest-match operator path, which only sees a `-` the number rule
   declined, so `x -= 1` is still one augmented-assignment token and `x-=-1` is `-=` followed by the literal `-1`.

**What must not move, and is asserted not to.** `print(-1)`, `print(-1.5)`, `xs[-1]`, `[1, 2, 3][-1]`, `{-1: "n"}`,
`f(-1)`, `print(1, -1)`, `def f(a=-1)`, `print(2 ** -1)`, `print(5*-1)`, `xs[-1] = 9`, `d[-1] = "k"` all answer
exactly as before: the merge still happens in every position that reads a sign, so no index, argument, key, default
or folded constant changed representation. `print(5--1)` and `print(5- -1)` both answer `6`, because the second
minus sits after the operator and becomes a sign again — the reference's own double negation, arriving by the route
it already used.

## The token stream is the test

The contract for this row is not an output, it is a *token boundary*: `INT:-1` beside `OP:- INT:1` is the whole
difference between the two answers. So the tests are written at that altitude and at every altitude above it
(`pkg/lang/lexer_test.go`):

- **`TestTheMinusBeforeADigitIsAskedWhatItFollows`** — 31 rows of rendered token stream, both ways: the operator
  rows (`y = x-1`, `print(1-1-1)`, `print(w["a"]-1)`, `print(True-1)`, `print("ab"-1)`, the spaced spelling,
  `x-=1`) and the sign rows (`print(-1)`, `xs[-1]`, `{-1: "n"}`, `f(-1)`, `x = -1`, `5--1`, the `-` inside a text and
  the `-` inside a comment, which is not a token at all). A lexer that drops the context question fails the operator
  rows; one that always splits fails the sign rows. Seventeen assertions fail when the guard is deleted, which is the
  measurement that says the rows bite.
- **`TestTheContextQuestionIsAskedOfThePreviousToken`** — the predicate itself, over hand-built token slices, so a
  future edit cannot hide behind a token stream that happens to agree.
- **`TestASubtractionIsOneExpressionNotTwo`** — the parse: `y = x-1` is **one** statement whose value is a `*BinOp`,
  and `x = -1` is one statement whose value is an `*IntLit` of `-1`, not a `*UnOp` the parser rebuilt. The second
  half is what stops "fix it by always lexing a bare `-`" from passing.
- **`TestSubtractionAnswersWhatTheReferenceAnswers`** (59 rows, `pkg/lang/minus_operator_test.go`) and
  **`TestSubtractionAnswersWhatTheReferenceAnswersThroughTheCLI`** (43 rows, `integration/`) — every `want` read off
  `python3`, both legs (`HasGoldenAnswer` + the record's stdout, and the CLI against the reference), and a refusal is
  a failure: these programs were never a capability boundary.
- **`TestTheSubtractionIsInTheModule`** — the defect's own shape in the emitted IR: the arithmetic appears as
  `sub i32` in `@main` or in the program's own `@gy_*` function, and the negative controls (a plain copy, a negated
  literal, a negative index, `5*-1`) must contain **no** `sub i32`, because a sign that stopped merging would become
  a subtraction from zero the compiler invented. The module for `y = x-1` before this ADR had no `sub` in it at all.
- **`integration/programs/minus_operator.gy`** — the family as one 22-line program, its expectation what CPython
  printed for it, matrix row `programs/minus_operator` at `parity` / oracle `match`.

## Alternatives rejected

- **Look ahead instead of back** (merge `-N` only if the *next* token after the digits also cannot continue an
  expression). Rejected: it cannot distinguish `y = x-1` from `y = x - 1` at all — both are followed by `1` — and it
  puts the decision further from the evidence than the evidence is.
- **Merge nothing; let the parser's unary minus handle every `-`.** Rejected: it answers the arithmetic and moves
  everything else. A `-1` used as an index, a dict key, a parameter default or a `print` argument stops being a
  literal, and the fold that answers `xs[-1]`, `{-1: "n"}` and `def f(a=-1)` — plus the constant-index and constant
  slice-bound doors that read literal integers — is what breaks. `TestASubtractionIsOneExpressionNotTwo`'s sign half
  is this rejection, written as an assertion.
- **Fix it in the parser** (treat a stray expression statement after an assignment as a continuation). Rejected: the
  parser would have to un-see a token the lexer already built, and `y = x -1` genuinely *is* two statements in the
  stream the lexer produced. The defect is a token boundary, so it is repaired where the boundary is cut.
- **Keep the merge and special-case a following identifier.** Rejected: the two faces are one rule; keeping it would
  leave `print(1-1)` — the single most ordinary expression in a tutorial — a parse error.
- **Add unary `+` while here, since the file is open.** Rejected and filed: `print(+1)` is refused (`unexpected
  token`) where CPython prints `1`, and that is the parser's missing rule, not this one. It is **Gap R.244**, pinned
  in `TestWhatTheMinusRowStillCannotAnswerThroughTheCLI` with what the backend says today and what the reference
  says, so it fails the day it is fixed rather than staying quietly unfixed. The comprehension over a list the
  compiler kept static is refused for the same reason and is already owned by L11.1/L11.2 (ADR 0192).

## Consequences

- The lexer's number rule is now context-sensitive, and it is the first rule in it that is. A future rule of the same
  kind (`.` followed by a digit — roadmap L11.2/Gap N.4 wants `.5`) must ask the same question and will find the
  predicate beside it.
- Every program in the wild that wrote `a-1`, `len(xs)-1` or `i-1` and *got an answer* was getting the wrong one;
  those answers change here, and change to CPython's. Nothing in the record held an affected source — the eight
  candidate entries are all `-` inside a text or a comment — and no conformance program spelled a subtraction
  without its space, which is the fact that let the defect live. The record grows 56 newly answered programs plus the
  one conformance program.
- Exit 2 is untouched by this row, and the two refusals it leaves standing are named: Gap R.244 (unary plus) and the
  static-list comprehension (L11.1/L11.2).
