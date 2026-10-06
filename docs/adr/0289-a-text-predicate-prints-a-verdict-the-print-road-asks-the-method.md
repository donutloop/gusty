# ADR 0289 — a text predicate prints a verdict: the print road asks the method's own name

Date: 2026-07-06
Status: Accepted
Roadmap: closes **Gap R.172** (found while scanning the surface 2026-07-06).
Depends on: ADR 0257 (a bool is a value the printer must be told about), ADR 0259 (a verdict is still a
number), ADR 0215 (the reference's own error sentence), Gap R.38 (a refusal names what the program asked
for), ADR 0166 (exit codes).

## The measure

`python3` 3.12.3 is the oracle. Both engines, exit 0, no refusal, agreeing with each other:

| program | CPython | `--interp` before | `--aot` before | both after |
| --- | --- | --- | --- | --- |
| `print("abc".startswith("ab"))` | `True` | `1` | `1` | `True` |
| `print("abc".startswith("z"))` | `False` | `0` | `0` | `False` |
| `print("abc".endswith("bc"))` | `True` | `1` | `1` | `True` |
| `print("12a".isdigit())` | `False` | `0` | `0` | `False` |
| `print("abc".isalpha())` | `True` | `1` | `1` | `True` |
| `print("abc".islower())`, `isupper`, `isalnum`, `isspace` | verdicts | `1`/`0` | `1`/`0` | verdicts |
| `print("abc".upper())` | `ABC` | `ABC` | `ABC` | `ABC` |
| `print("1".isdigit() + 1)` | `2` | `2` | `2` | `2` |

Five of the six probe lines were wrong. Note what is *not* in this table: the methods were never wrong
about their **answer**, and never wrong as **numbers** — `"abc".upper()` and `"1".isdigit() + 1` behaved
correctly before and after. Only the rendering of a verdict was missing.

## The diagnosis

`IsBoolExpr` (`pkg/lang/boolvalue.go`) is the single question both backends and the CLI's `--json` report
ask before choosing `rt_print_bool` versus `printf("%d")`. Its `*Call` arm began:

```go
nm, ok := c.Fn.(*Name)
if !ok { return false }
```

A method call is not a name call. `"abc".startswith("ab")` parses as `Call{Fn: Attr{Obj: "abc", Name:
startswith}}`, so the question returned "not a bool" on its first line — before any table was consulted —
and the printer fell through to the integer road. The eight methods had been answering with the correct
0/1 all along (`callStrMethod` / the codegen string-method road, both folding to the same words a
comparison produces). The bug was never in the method.

**The thirteen pins that hid it.** `integration/lang_test.go` carried, for each method, lines like

```go
assertOutput(t, `print("123".isdigit())`, "1\n")   // "isdigit folds to 1 or 0"
```

with an `assertOutput` against `1` and `0` — a wrong answer written down as the expected one, and a comment
explaining the fold as though it were the contract. Those tests went green for the wrong thing and would
have blocked this fix as a regression. Thirteen of them moved to the reference's answer in this commit
(ladder rule: pinned values follow the reference, never the implementation).

## The decision

**The predicate table asks the method's own name when the callee is an attribute.** `stringBoolMethods`
lists `startswith`, `endswith`, `isdigit`, `isalpha`, `isalnum`, `isspace`, `islower`, `isupper` — only what
both backends actually implement, in the spirit of `boolReturningBuiltins`: a table never promises an answer
the backends cannot give (`isnumeric`, `isdecimal`, `istitle`, `isprintable`, `isidentifier`, `casefold`
stay unimplemented and raise the reference's `AttributeError`, pinned as exit 3 elsewhere).

**The receiver is asked first.** `overloadedReceiver` consults `env.Instance`, the same predicate that keeps
`print(a < 4)` printing `1` for dunder.gy. A class the program defines may name one of these eight
attributes itself — `class Box: def isdigit(self): return 1` — and then the value is whatever that method
returns, an integer printing as a number. Pinning this (`TestAPredicateOnAnInstanceIsNotAssumedAVerdict`)
is what keeps a name table from becoming a guess about the program.

**Value methods stay on the text road.** `upper`, `strip`, `replace`, `join`, `count`, `find` answer texts
and numbers, and `TestAPredicateIsNotConfusedWithAValueMethod` fails if the verdict road ever swallows them.
This test is the reason the change is a table rather than "any attribute starting with `is`".

## Errors made while writing the tests, recorded because each is a class

* **I pinned `"ab".join(["x","y"])` as `xy`.** The separator is the receiver, so the answer is `xaby`; both
  engines were right and my row was wrong. A test row that disagrees with the reference is a bug in the test,
  even when it is a row meant to protect against my own bug.
* **I wrote one row as `def check(s): return s.isalpha()`**, which is the *parameter-receiver* program — the
  compiled leg's pre-existing refusal — not the constant-receiver program I meant to test. The row now uses a
  module-level name, and the refusal is pinned separately as what it is.
* **I first wrote the compiled-leg refusal test for both engines**, then measured that `--interp` answers
  `True` (it evaluates the receiver at run time) and scoped the test to `--aot`. A test that assumes a
  refusal must measure who refuses.

## Alternatives rejected

* **Have the methods allocate a boxed bool in the interpreter and a tagged value in codegen.** Rejected:
  an untagged word is what a word is, and giving bools a kind of their own is L11.1's destination, not this
  rung (ADR 0171 — the front end reads the program so the runtime does not have to guess).
* **Match any attribute whose name begins with `is`.** Rejected: `is`-prefixed names are not a semantic
  class, the rule would silently capture the next method added, and a value method rendered as `True` is a
  brand-new wrong answer.
* **Teach the printer to ask the runtime.** Rejected: there is no kind to ask (that is L11.1), and the
  compiled side would need a new representation to carry it.
* **Leave the thirteen pins as "documented behaviour".** Rejected by the ladder rule and by ADR 0261's
  precedent: a pinned wrong answer is not documentation, it is the wrong answer with a green check.

## Agentic rationale

A predicate that prints `1` is invisible to every check an agent has: exit 0, a valid module, a plausible
digit. The observable now is the answer, pinned against `python3` in three places (a registered probe, a
unit table, a CLI table). The compiled refusal that remains — a **parameter** receiver, which has no
compile-time text to fold — is pinned to stay exit 1 in words naming the method, never exit 2 and never a
number, so a harness can rewrite rather than re-read IR. Exit codes and JSON output unchanged.

## Tests

* `pkg/lang/string_predicate_test.go` — 30 rows × both engines (each method true and false, empty string,
  `not`/`and`/`or`, compared against `True`, bound to a name and rebound to a number, as a test, inside a
  list); value methods not swept in; an instance's own attribute; a verdict still counting as a number.
* `integration/string_predicate_test.go` — the CLI table over both engines with a live `python3`
  cross-check per row and an exit-2 ban, plus the parameter-receiver refusal.
* `integration/programs/probe_a_text_predicate_prints_a_verdict.gy` — six lines, registered in
  `conformanceStandalone()`; the pre-cycle binary got **five of six** wrong.
* `integration/lang_test.go` — thirteen `assertOutput` pins moved from `1`/`0` to `True`/`False`.
* Suite green; 165-file sweep against the pre-cycle binary moved nothing except the new probe.
