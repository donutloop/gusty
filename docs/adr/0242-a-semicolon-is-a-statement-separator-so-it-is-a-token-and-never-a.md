# ADR 0242: `;` is a statement separator, so it is a token and never a diagnostic

Status: accepted. Roadmap Gap R.72; follows ADR 0240 (`a count is not an error message`), which made the
old refusal legible without deciding the question.

## The measured starting point

One line of source, three verdicts:

```gy
x = 5; print(x+1)
```

| entry point | before |
|---|---|
| `--interp --file` / `--eval` / `--repl` | `6` — CPython's answer |
| `--jit` / `--aot` | exit 1, `jit: 1 error(s) in source` + `error at 1:6: unexpected character ";"` |
| `--emit-llvm` | exit 0, no diagnostic, and `llc-20 -filetype=null` accepts the module |

The lexer had no case for `;`. It fell through the operator scan and came back as an
`unexpected character ";"` token, which `filterLex` turned into an error diagnostic while *dropping the
token from the stream*. The parser never looked at it, so both statements parsed. Then each entry point
decided for itself what to do with the diagnostic: `JITWithOptions` treats any error-level diagnostic as
fatal, `Compile` collects them and returns the IR anyway, and the interpreter never asks. One program,
three stories — which is what an agent comparing backends reads as a backend bug and burns a round on.

## The decision

**`;` separates simple statements on one line, exactly as CPython defines it, and is therefore a token,
not a diagnostic.** `TokSemi` in `pkg/lang/token.go`, emitted by the lexer's line loop; `filterLex` keeps
it in the stream and records nothing. Three properties follow, and each is a rule about where a syntax
rule belongs rather than about semicolons:

1. **The parser owns the rule.** `skipSeparators`/`skipSemiRun` step over separators in the statement
   loops (top level and every block), and the empty statement — two separators with nothing between them
   (`x = 1;;y = 2`, `x = 1; ;y = 2`, a line beginning `;`) — is a `ParseError`. It is *not* a lexer
   diagnostic, because a diagnostic is exactly what the three entry points above disagreed about: the
   interpreter would have run the program the JIT refused. Where every path must agree, the rule lives in
   the parser.
2. **A separator belongs to the suite it follows.** `for i in xs: f(i); g(i)` puts **both** calls in the
   suite. The first version of this change dropped the token, like the old error token did, and printed
   `1 2 step` where CPython prints `1 step 2 step` — a wrong answer that every engine agreed on. That is
   why `TokSemi` stays in the token stream and `parseBlock`'s single-line branch loops over the separated
   statements, stopping at the NEWLINE that ends the physical line (the next line belongs to the enclosing
   block, not the suite).
3. **A trailing separator is not an empty statement.** `x = 5;` and `x = 5; # comment` are what CPython
   accepts, so the check fires on adjacency, not on position before the newline.

## Why not "reject `;` everywhere"?

That was the other honest answer, and Gap R.72 named it: either the lexer accepts the separator or the
error is fatal on *every* path — including `--interp` and `--emit-llvm` — and the interpreter's tolerance
becomes the bug. Both endings satisfy the contract; only one keeps running the programs people write.
Since the parser already understood the statements on either side, forbidding the separator would have
been a language with less surface for no gain in truth.

## Codegen/IR consequences

- None for the module: the statements were already being emitted on the paths that ran them. What changes
  is which programs are *reachable*: `;`-separated sources now compile on `--jit`/`--aot` instead of being
  refused by a diagnostic the interpreter ignored.
- `skipSeparators` is a loop, not a pair of calls: `x = 5;` leaves the cursor on the line's NEWLINE after
  the separator, and a loop that skipped only one kind would hand `parseStmt` a NEWLINE to parse as a
  statement (`unexpected token` at a position past the `;`).
- `skipNewlinesAt`, the incremental parser's boundary helper, skips separators too, so the parse cache
  keeps statement boundaries aligned when an edit lands between two `;`-separated statements.
- Indentation is untouched: the lexer emits `TokSemi` from the line loop, so INDENT/DEDENT still come one
  pair per physical line. A regression test (`TestSemicolonDoesNotDisturbIndentation`) guards the case
  where that would have been the subtle break — a mid-line separator read as a block start.

## Coverage

`pkg/lang/semicolon_test.go`:
- 13 programs × both backends (`Compile` + `runIR`, and the interpreter through `captureStdout`) against
  CPython's answers, including `if x: print("in"); print("body")`, a `for` whose inline suite has two
  statements (the per-iteration case that exposed the wrong-answer version of this change), an inline
  `def` body, an indented body with a separator, and a separator followed by a blank line;
- `TestSemicolonIsNotADiagnostic` — the point of the change: `parseProgram` records no error-level
  diagnostic, so no entry point can disagree about it;
- five rejection rows, each also asserted to be rejected by the *interpreter* path, so a parse error is
  one verdict and not three;
- the indentation regression guard above.

## Consequences

- `docs/language.md` documents the separator and the empty-statement rule, so the language answer is
  written down rather than inferred from whichever entry point was tried first.
- Gap R.72 closes. ADR 0240's `jit:` message stays as it is: a program with a genuine syntax error is
  still refused, and now refused with its message on every path.
- The general rule, again: an entry point may not keep its own opinion about a program. A diagnostic that
  one path enforces and another ignores is a backend bug with extra steps (ADR 0166 for codegen, ADR 0216
  for the two engines, this ADR for the front end).
