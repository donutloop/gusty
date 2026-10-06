# ADR 0288 — a comparison chain is one node over n operands, each middle operand read once

Date: 2026-07-06
Status: Accepted
Roadmap: closes **L12.1 / Gap R.53** (both backends, the wrong-verdict class).
Depends on: L12.1's own reasoning (the AST-level node and the rejection of an `and` desugaring),
ADR 0257 (a verdict writes its own name), ADR 0234 (a container global in an `i32` operand is a compiler
bug), ADR 0243 (a literal container's slot *is* the number it was written from), ADR 0166 (exit 2),
Gap R.38 (a refusal must describe the program the reader is holding).

## The measure

`python3` 3.12.3 is the oracle. Both engines previously agreed with each other and disagreed with the
reference, at exit 0, with no refusal:

| program | CPython | `--interp` before | `--aot` before | both after |
| --- | --- | --- | --- | --- |
| `print(1 < 2 < 3)` | `True` | `True` (accidental) | `True` | `True` |
| `print(3 < 2 < 1)` | `False` | `True` | `True` | `False` |
| `print(1 < 2 > 1)` | `True` | `False` | `False` | `True` |
| `print(1 > 2 < 3)` | `False` | `True` | `True` | `False` |
| `x = 50` / `print(1 < x < 10)` | `False` | `True` | `True` | `False` |
| `print(1 < 2 < 3 < 1)` | `False` | `True` | `True` | `False` |
| `if 1 < 5 < 3:` branch | `out` | `in` | `in` | `out` |
| `print(1 < g() < 10)` (call in the middle) | `True` | ✓ | `True`, `g` run **3×** | ✓, `g` run **1×** |

Four of the six probe lines were wrong on both engines. The one that was right — `1 < 2 < 3` — is the
reason a chain test must contain a *failing* chain: L12.1 already noted that this row passes for the wrong
reason, and a suite built around it would never have noticed.

## The diagnosis

The Pratt loop is left-associative at `precCompare`, so `a < b < c` built `BinOp(<, BinOp(<, a, b), c)`.
That is not a Python construct: it asks `a < b`, gets a verdict, and then asks whether that verdict is less
than `c`. This front end **answers** a comparison between an int and a boolean rather than refusing it — so
the type error that should have surfaced as a diagnostic became a number.

Two separate holes sat under that one:
* the **print** road asked `printsAsBool` and got "not a bool" for anything whose outer node was a `BinOp`,
  so a chain's verdict printed through `printf("%d")` as `1`/`0`;
* the **`in`/`is` two-word operators** are consumed by a step that ran *after* the loop's operator
  dispatch, so any chain handling placed before it left `1 not in [1]` and `1 is not 2` unparseable.

## The decision

**A `ChainCompare` node, at the AST level**, holding `Ops []string` and `Operands []Expr` (n operators,
n+1 operands), exactly as L12.1 prescribed. The parser recognises a *run* of comparison operators and folds
it into one node instead of nesting. A single comparison stays a `BinOp`: giving every ordinary comparison
in the language a second codegen path to get wrong is not a small trade.

**Not a desugaring to `a < b and b < c`** — L12.1 rejected this and the reason is measurable: with a call in
the middle, the reference calls it **once**. Desugaring over the original expression nodes calls it twice,
and `and` short-circuits the tail comparisons the reference still evaluates. My first compiled lowering
committed exactly the first error: it handed the *original operands* to the links and
`print(1 < g() < 10)` printed the call's output three times at exit 0 — the bug re-imported by the code
written to fix it. The fix is that each repeated operand is evaluated **once into a slot**, and the links
read the slot.

**A slot that is still a name.** The first version allocated a bare `alloca` and handed the comparison roads
a raw load. That lost every per-name record the roads consult and answered `index of a non-literal
variable` for `0 < xs[1] < 3`, where the baseline answered `True`. Two corrections: bind the operand to an
ordinary **local name** (so `dictVals`, `listVars`, the tagged-slot tables stay reachable), and carry the
literal-container records (`containerLits`, `staticLists`, `staticDicts`, `staticSets`) onto the slot's
name. Setting `g.numCtx` for the chain mattered too — the comparison roads read it to know which operator an
operand is being lowered for.

**The choke point, not the consumer.** `xs[1]` in a numeric position was answered by a fold that the
`case *Name:` arm of the `Index` road only consults in its mixed-list branch; elsewhere it fell to a
refusal. ADR 0243's promise — a slot of a literal container the program never changed *is* the number it was
written from — is now asked before that refusal, which fixes the chain case and the ordinary ones beside it.

**A container operand refuses.** A container literal's compiled value is the *address of a compile-time
global*; a chain's slot is an `i32` alloca. The store becomes `store i32 @.lst1, i32* %_chain1`, which llc
rejects — ADR 0234's compiler bug for an ordinary program. So the compiled leg declines a container operand
in words naming what is missing, while the interpreter chains over containers normally. This is owed to
L11.1's tagged value word, and `TestChainWithAContainerOperandRefusesOnTheCompiledLeg` fails if the refusal
silently becomes a number or leaks the module it could not build.

## Errors made in this cycle, recorded because each is a class

* **`not in` / `is not` became unparseable** (5 tests failed) by building the chain before the two-token
  step. Fixed by ordering; the regression was caught by tests I did not write, which is the argument for
  keeping precedence tests.
* **A wrong answer in my own test table.** I pinned `print(1 not in [2] == 1)` as `True` from arithmetic
  about precedence. CPython answers `False`: the chain shares `[2]`, so the second link is `[2] == 1`. The
  row now records the measured answer and notes that the draft pinned a guess.
* **The chain's verdict printed `1`.** Fixed by teaching `IsBoolExpr` that a chain is a verdict (ADR 0257).
* **`___chain1`** — the backend's local convention is `_%s` on the *name*, and my name already began with
  `_`; the alloca and the store disagreed and llc said "expected value token".
* **`i32* _chain1`** — an alloca'd local is addressed as `%_name`; the `%` is the register sigil, not part
  of the name.
* **Two `default:` clauses in one type switch**, and a duplicated `return` left by a trace-stripping
  substitution — both caught by the compiler, neither by reading.

## Alternatives rejected

* **Keep left-associativity and special-case bool operands.** Rejected by L12.1: it fixes the observed
  cases and leaves the operator wrong.
* **Refuse chains.** Rejected by L12.1 and by this loop's repeated decision: a refusal for the most ordinary
  comparison in the language is the wrong deliverable. (The container-operand *sub*-case is a refusal — for
  the different reason that answering it would emit IR llc rejects.)
* **Desugar in the parser.** Rejected; see above, and see the three-times `evaluated`.
* **Chain the `and` over original expressions.** Rejected: short-circuiting skips operand evaluations the
  reference performs. And-ing *slots* is safe precisely because the operands have already run.

## Agentic rationale

A chain wrong-verdict is invisible to every check an agent has: exit 0, a well-formed module, a plausible
`True`. The observable now is the answer itself, pinned against `python3` in a CLI table and in a registered
probe. The compiled refusal that remains says *which* operand shape is unsupported and *what* it needs
("a container operand of a comparison chain… the compiled leg waits for the tagged value word"), so a
harness can decide "rewrite with an int" rather than re-reading IR. Exit codes unchanged.

## Tests

* `pkg/lang/compare_chain_test.go` — 24 rows × both engines each pinned against `python3` (ascending,
  descending, mixed up/down, four operands, names, texts, calls, container and dict slots, chains as tests
  and in function bodies, mixed `not in`/`is` links); a once-only evaluation test counting an effectful
  middle operand on both engines; the verdict-rendering rows; `TestChainParsesAsOneNode` (a chain is one
  node; **a single comparison must stay a `BinOp`**); `TestChainsDoNotSwallowBooleanOperators`; and the
  container-operand refusal.
* `integration/compare_chain_test.go` — the CLI table over both engines with a live `python3` cross-check
  per row and an exit-2 ban, plus the once-only check through the shipped binary.
* `integration/programs/probe_comparison_chains.gy` — six lines, registered in `conformanceStandalone()`;
  the pre-cycle binary got **four of six** wrong.
* Suite green; 164-file sweep against the pre-cycle binary moved nothing except the new probe.
