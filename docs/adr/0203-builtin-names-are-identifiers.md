# 0203. Built-in names are identifiers, not keywords

## Status

Accepted. Implemented this cycle in `pkg/lang/token.go` (the `keywords` table); pinned by
`pkg/lang/builtin_name_tokens_test.go`, `integration/builtin_name_tokens_test.go` and
`integration/programs/builtin_names_as_defs.gy`. Roadmap Gap R.9.

## Context

```gusty
def print(x):
    return x + 1
```

`gustyc --check` answered `error at 1:5: expected identifier`. The program never reached the
checker, the codegen, or the interpreter; the lexer had refused it. Same for `def range(a)`, a
parameter called `range`, a keyword argument called `print`, or a method named `range` — which is a
strange thing to be unable to write, because `shape(w, h)`, `print`, and `Counter.range(n)` are the
vocabulary of an ordinary program, and every one of them is a name a program should be free to
choose.

The cause was two levels below where the symptom appeared: `print` and `range` were listed in the
lexer's `keywords` map, so they tokenized as `TokKeyword` and `expectIdent` refused them. Nothing
about them needs a distinct token — the parser accepted them in *expression* position with a special
case (`t.Kind == TokKeyword && (t.Text == "print" || t.Text == "range")` → `&Name{...}`), and every
downstream component (parser name paths, the checker's built-in tables, `codegen.go`'s
`case "print"`, `case "range"`, the interpreter's `n.Value == "range"` fast paths, the closure
built-in set, LSP completion) already dispatches on the *text*, not the token kind. The keyword
status bought nothing and cost the ability to name those words at all.

It is also the second half of a decision already made: ADR 0199 established that a built-in name is
a *name* — shadowable, and honoured by every component that reads a name — but while the words were
keywords, the shadowing ADR 0199 protects could not even be written for these two.

## Decision

**The keyword table contains only words that change grammar.** `print` and `range` were removed
from it; they now lex as `TokIdent` like any other identifier, and are recognized as built-ins at
the point of use, exactly as `len`, `str`, `int`, `abs` and `sum` already were (`def len` and
`def str` have always parsed).

Pinned on both sides:

1. **Built-in names are usable everywhere a name may appear** — `def print`, `def range`, `def str`,
   `def len`, parameters named `print`/`range`/`len`, a method named `range`, a method named `print`,
   and keyword arguments `use_both(print=7, range=8)`.
2. **The built-ins themselves keep working**, because the fast paths are name-keyed: `print(1)`,
   `print(1, sep=",", end="!")`, `for i in range(3)`, `range(1, 9, 2)`, and a comprehension over
   `range(4)`.
3. **Real keywords stay reserved**, asserted for the whole grammar set (`def`, `if`, `while`, `for`,
   `in`, `class`, `return`, `match`, `case`, `try`/`except`/`finally`, `yield`, `lambda`, `import`,
   `with`, `as`, `not`/`and`/`or`, `is`, `None`/`True`/`False`), each with the specific message — so
   this is a narrowing of the table, not its abandonment, and the test names the mechanism (`Lex`
   must produce `TokIdent` for a built-in) so a future re-adding fails there rather than in someone's
   parse error.

The parser's `TokKeyword && Text == "print" || "range"` case became unreachable and its absence is
implied by the lexing test; removing the special case is the same statement in the other direction.

## Consequences

- A whole family of "expected identifier" failures disappears for the most natural names in the
  language. The corpus gained `programs/builtin_names_as_defs.gy`, which uses `range` as a module
  helper, `range` as a method, `print`/`range` as parameter names and `print=`/`range=` as keyword
  arguments — `6 12 15 9 12 6` identically on the interpreter, in the compiled binary and in CPython.
- Consistency with ADR 0199: the language can no longer be described as "built-ins are shadowable"
  with two exceptions that are unspellable.
- Measured while building that program, three behaviours around shadowed built-ins and statement
  values came apart between backends and are recorded as **R.12** (a program-defined `range` is
  visible to codegen *above* its definition, so `for i in range(2)` iterates the program's value
  while the interpreter uses the built-in), **R.13** (`--file`/`--interp` echo a final bare
  expression statement's value; `--aot` and CPython print nothing), and **R.14** (`for x in 5`
  iterates `0..4` on both backends but is undocumented, and CPython refuses it). The ledger program
  deliberately avoids all three, and says so in its header comment, so it stays a green parity
  program while those gaps are open.

## Alternatives considered

- **Allow keyword tokens only in `expectIdent`** (accept `print`/`range` after `def`, as a
  parameter, and after `.`). Rejected: it is a list of sites rather than a rule, and the next
  position that wants a name — an `import ... as` alias, an attribute in a pattern, a future
  `global` list — needs the same patch again. The name-keyed design already downstream means the
  token kind was never the thing that made these built-ins work.
- **Keep them keywords but add an escape hatch** (`` `print` `` or `print\`). Rejected: it makes
  legal Python-looking source illiterate, and asks every programmer to know which words in this
  language are secretly keywords — precisely the kind of trivia ADR 0199's "program facts outrank
  built-in facts" was meant to remove.
- **Remove the `range` fast path and require `for i in range(3)` to be a keyword-form loop.**
  Rejected: the fast path is what makes counted loops compile to a counter loop (ADR 0196), and it
  dispatches on the name already; keyword status was never what protected it.
