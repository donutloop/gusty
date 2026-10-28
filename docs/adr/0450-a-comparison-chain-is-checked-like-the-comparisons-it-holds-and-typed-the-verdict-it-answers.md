# ADR 0450 — a comparison chain is checked like the comparisons it holds, and typed the verdict it answers

- **Status**: Accepted
- **Date**: 2026-10-30
- **Cycle**: the one after **ADR 0449** (which closed **Gap R.54**'s compiled half; row **L12.1** was the top of
  the Open queue below the rows that row had just touched)
- **Roadmap**: **L12.1** (`Comparison chains mean what they say (1 < x < 3)`) done — the half **ADR 0288** left,
  which was never the parse and never the lowering but the analyzer. Two lowering defects the sweep measured are
  filed rather than fixed here: **Gap R.520** (a chain evaluates EVERY link, including the ones the reference
  never reaches) and **Gap R.521** (`print(1 in 5)` answers `False` at exit 0 where the reference raises
  `argument of type 'int' is not iterable`).
- **Builds measured**: parent `b37d618` (**ADR 0449**) and this commit, every byte from the gate's `-tags=llvm20`.

## Context

`a < b < c` has been one node since **ADR 0288**: a `ChainCompare` over `n + 1` operands and `n` operators,
which is what makes "the middle operand is read exactly once" a property of the emitted module instead of a
hope about road order, and which killed the family of exit-0 wrong verdicts that left-associativity produced
(`print(1 > 2 < 3)` answered `True` where CPython answers `False`). Codegen stores each operand into its own
slot and reads it back, `chain_operand.go` decides which of those slots carries a tagged pair or a double,
`compare_chain_test.go` and `integration/compare_chain_test.go` pin the verdicts, and the conformance corpus
carries `probe_comparison_chains.gy`.

What the node never had was an arm in the semantic analyzer. `inferExprTy` is a `switch` over the expression
node kinds; `*ChainCompare` was not in it, so the walk reached a chain, matched nothing, returned `nil`, and
moved on — and every operand inside the chain was never visited at all.

Two consequences, both measured on the parent before anything was changed:

```
$ gustyc --check 'y = 1 < zzz < 3'
ok                                              exit 0     ← `zzz` is bound by nothing in the program
$ gustyc --check 'y = 1 < zzz'
error at 1:9: undefined name "zzz"              exit 1

$ gustyc --check 'def f(x):
    return x

y = 1 < f() < 3'
ok                                              exit 0     ← the callee asks for one argument
$ gustyc --check 'def f(x):
    return x

y = 1 < f()'
error at 4:10: function "f" expects 1 argument, got 0   exit 1

$ gustyc --check 'y: int = 1 < 2 < 3'
ok                                              exit 0
$ gustyc --check 'y: int = 1 < 2'
error at 1:1: type mismatch: expected int, got bool      exit 1
```

The first family is a **hole**: the operand-level rules (`undefined name`, an arity mismatch, a call to a
name nothing defines) live in the roads that *infer an operand*, and those roads were never called for a
chain's operands. The program is not unsound — codegen still refuses the undefined name when it reaches it —
but `--check`, `gusty check`, the LSP and every `--json` consumer were answering "clean" about a program the
same toolchain later refuses, and answering it with a **false** `ok` is the failure mode an agent driving the
compiler cannot see: the JSON says the program is fine.

The second family is a **missing type**. `inferExpr` writes the inferred type into the node's `Ty` field from
the same switch, so with no arm a chain had no type, and every rule that reads a sub-expression's type was
reading `any` — the gradual type, the one every rule steps aside for. A verdict standing in an annotated
binding, a typed parameter or an arithmetic operand stopped being checked. `--emit-ast`, the machine-readable
answer to "what did the front end understand", printed the chain's operands and no `inferred` beside the node.

## Decision

**1. The chain gets one arm, which infers its operands and returns `bool`.** `inferExprTy` gains
`case *ChainCompare: return an.inferChainCompare(n)`; `inferChainCompare` infers each operand left to right —
the order the values are produced in, so a name bound between two links is bound for the links after it —
and returns `TBool()`. `inferExpr`'s `Ty` switch gains the matching `case`, which is what puts the verdict
back under the annotation and variance rules and writes `"inferred": "bool"` into the AST document.

A chain answers the conjunction of its links. That is a verdict, and the single-comparison form has always
been typed one. The two readings of the missing field ("no type" and "any type") are indistinguishable to a
rule that steps aside for `any`, which is exactly why the hole survived: nothing ever misbehaved loudly
enough to point at the arm.

**2. The operand diagnostics are not chain-specific — they are the operand's own.** `inferChainCompare`
calls the ordinary `an.inferExpr` on each operand, so a chain reports what the same expressions report with
one operator removed, in the same words at the same spans. The tests are written as PAIRS for this reason:
"the checker says something about chains" is satisfiable by a checker that says something wrong about them;
"the checker says the same thing about a chain and about the comparison it holds" is not.

**3. A link no pair of values can answer is NAMED, not refused.** The row's own definition-of-done cell asked
for a refusal. What ships is a warning at `--check` with the stable code `type.compare.uncomparable`, worded
in the reference's own sentence:

```
$ gustyc --check 'print(1 < "a" < 3)'
warning at 1:11: comparison chain: '<' not supported between instances of 'int' and 'str'
warning at 1:17: comparison chain: '<' not supported between instances of 'str' and 'int'
ok                                              exit 0
```

An error-level diagnostic would have been a second defect: `error` level stops the build, and the reference
RUNS that program — it raises, and it raises inside the program's own `except TypeError:` if the author wrote
one. **ADR 0166** is the house's standing answer to that trade, quoted at Gap R.242's lexer row and now at
this one: *a refusal is the wrong answer to a program the reference runs*. So the checker names the fact and
the run time raises it, and the run path is unchanged, byte for byte, at every exit code.

**4. The question is asked once, in the table the compiled leg already reads.** `order_kind.go` holds
`orderFamilyOf`, `orderPairHasNoOrdering` and `unsupportedOrderMessage`, because the compiled leg needs to
know when an ordering must raise (**ADR 0288**'s sibling, 37 measured wrong verdicts). `compare_check.go`
does not restate that table; it maps a checker `*Type` to the reference's kind word and asks those functions.
A checker that worded the same fact its own way would hand an agent two sentences about one defect, which is
what `abs_kind_test.go` already polices for the print, negation and index doors. The membership question
(`x in y`, `argument of type 'int' is not iterable`) has no such table and states its own rule — silently
declining to speak about a class, a union, a dynamic value or anything else whose members a dunder could
supply.

**5. Where the checker cannot name both kinds, it says nothing.** Both sides are widened over union members
and the sentence is only spoken when every pairing fails; a container against a container is left to the
tagged order door, which ANSWERS `a = [1]` / `b = [2]` / `print(a < b)`, and an opinion there would trade a
correct answer for a raise — the shape the ladder forbids (ADR 0273). The stay-silent table in
`compare_chain_check_test.go` is the false-positive guard: 12 shapes, from an unannotated parameter to a
class defining `__lt__`, that must produce no diagnostic with the code.

**6. A malformed chain is said, not walked past.** The parser builds `Ops` and `Operands` together, so a
chain whose counts disagree is a broken AST; the arm reports it at error level (`malformed comparison chain:
n operators over m operands`) rather than indexing out of range.

## Agentic rationale

`--check`, `gusty check`, `--json`, the LSP and `--emit-ast` are the agent's eyes on a program it has not run.
A false `ok` is worse than a wrong verdict, because a wrong verdict is visible in the program's own output and
a false `ok` is visible nowhere: an agent that asked the toolchain whether `y = 1 < zzz < 3` was sound was
told yes, and only found out at codegen — with a sentence that has no span, no level and no code. Pairing the
chain to the single comparison makes the contract checkable per position rather than per construct, and the
stable `type.compare.uncomparable` code (declared in `gustyc --schema`, in `docs/operations.md`'s diagnostic
table and in `docs/agentic/ast-ir-schema.md`) lets a consumer branch on the rule instead of matching on prose.
The `Suggestion` field says what to do, and says the conditional honestly: the reference raises *if the chain
reaches that link*, because whether it does is a run-time fact.

## Codegen / IR implications

None, deliberately. This commit changes no emitted instruction: `chainValue`, `chain_operand.go` and the
comparison road are untouched, so every chain that answered an answer today answers the same one, and the
`--emit-llvm` census is byte-identical. The only module-level claim is asserted rather than assumed:
`TestTheOpinionIsTheSameSentenceTheCompiledLegRaisesWith` requires the module to carry, verbatim, the sentence
the checker warns with, and the run to raise it — the two doors cannot drift apart without failing a row.

The two defects the sweep found ARE codegen facts about the node, and are queued rather than papered over:
**Gap R.520** (links are lowered eagerly, so a link the reference never reaches still raises — `print(1 == x
< 3)` with `x = None` dies at exit 3 where CPython prints `False`; the ADR 0275 skeleton is the fix) and
**Gap R.521** (`print(1 in 5)` folds to `False` at exit 0 where the runtime door one step away raises the
reference's sentence).

## Alternatives rejected

- **Refusing an unanswerable link at `error` level.** It satisfies the row's wording and breaks the contract:
  a program the reference runs (by raising, catchably) would exit 1 before any of its effects. ADR 0166;
  Gap R.242's parse-error face is the precedent.
- **Desugaring a chain into `a < b and b < c` over the original nodes, in the checker or anywhere else.**
  That is the bug **ADR 0288** was written to kill — the middle operand would run twice — and it also hands
  `and` a link the reference never asked about. The node earns its existence by keeping the operands once.
- **A chain-specific diagnostic vocabulary.** "Chain link comparison unsupported" would read differently from
  the ordinary comparison's, and two sentences about one pair of kinds is one bug report too many. The chain's
  caption is a prefix ("comparison chain: ") on the reference's sentence, not a new sentence.
- **Restating the ordering families for the checker.** A second table means a second drift; the compiled leg
  already reads `order_kind.go` to decide when to raise, and `abs_kind_test.go`'s rule is that doors name one
  operand one way.
- **Skipping the `in`/`not in` rule until the run-time fold is fixed.** The checker's opinion and the fold's
  wrong answer are different claims about the same program, and **Gap R.521** owns the second; silence in the
  front end would have hidden the measurement, not resolved it.
- **Warning inside function bodies only, the way `arithmetic on non-numeric operands` gates itself on
  `!an.inFunc`.** That gate exists because arithmetic infers through tables the checker fills imperfectly.
  This door speaks only when BOTH kinds are fully known, and the annotation case (`def f(x: str): 1 < x < 3`)
  is precisely where an agent wants it; the stay-silent table is the guard the gate was standing in for.
