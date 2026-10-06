# A container in a numeric operand raises or refuses; it never emits a module the assembler rejects

## Decision

A container literal (`[...]`, `{...: ...}`, `{...}`, `(...)`) that reaches a **numeric operand position** —
an arithmetic operator's side, or the double road's `sitofp` lift — is handled by two new guards, and the
outcome is chosen by **what the reference does**, not by what this backend lacks:

1. **Where CPython raises**, both engines raise CPython's own sentence, so `except TypeError:` runs and
   `e`'s text is the reference's text (ADR 0215). `[1] - [2]`, `[1] / 2`, `[1] % 2`, `{1: 2} * 2`,
   `{1} * 2`, `[1] + {}`, `{} + []`, `[] < {}`, `[1] < 2` all land here.
2. **Where CPython answers** and no runtime helper exists — `[0] * 3`, `3 * [0]`, `[1, 2] + [3]`,
   `[1] < [2]` over literals — the compiled leg **refuses in words** at exit 1, naming the missing helper
   and the reason. The interpreter, which has the semantics, keeps answering.

Neither outcome is exit 2, and neither is a number.

## The agentic rationale

`gustyc --aot --eval 'print([0] * 3)'` used to exit **2** with `llc-20: global variable reference must
have pointer type`. ADR 0166 reserves exit 2 for *our* bug: an agent scripting this toolchain reads a 2
as "the compiler is broken, retry later / file it", never as "my program has a type error". The same
source through `--interp` answered `[0, 0, 0]`, so the two engines disagreed about the exit code as well
as the value. Now every shape in the family returns 1 (front-end refusal) or 3 (a trap the reference
traps on), both of which an agent can branch on, and `--emit-llvm` on a container operand returns a
diagnostic instead of IR that cannot be assembled.

## Codegen / IR implications

- `value()` renders a container literal as `@.lst1`, `@.dict2`, … — a *global address*, not a value. Any
  `add`/`mul`/`sitofp`/`icmp` that receives one is malformed, and **the LLVM verifier is not a safety
  net**: `llc-20` rejects the module first, so the emitted shape has to be pinned by test, not trusted.
- The guards sit on the **roads**, not beside the consumer that happened to be measured:
  `arithOperandIsContainer` in the int arithmetic case, and a container arm in `floatValue`'s lift.
- The lift arm's raise must **open its own label** after `raiseTo` (which ends the block in a `br`) and
  return `0.0`, not `0` — the caller splices the string after a `double`, and `fdiv double 0, %t1` is llc
  saying *integer constant must have integer type*.
- Two roads beside the guard keep their questions and had to be protected from it:
  - the **call-argument** path (`isFloat` describes the callee's *return*, not the argument), so
    `def half(xs): return xs[0] / 2` with `half([1.5])` still answers `0.75`; the lift guard fires only
    when `g.numCtx != nil`, i.e. inside an operator;
  - the **tagged order door** (`taggedOrderApplies`/`emitTaggedOrder`), which answers `a = [1]` /
    `b = [2]` / `print(a < b)` = `True`. Claiming a same-kind comparison from the arithmetic guard turned
    that True into a refusal, which the ladder forbids.
- `+`'s wording is a fact about the **left** operand's type: a list says
  `can only concatenate list (not "dict") to list`; a dict, which defines no concatenation, says
  `unsupported operand type(s) for +: 'dict' and 'list'`. Rows 3 and 4 of the probe are not mirrors.

## Alternatives rejected

- **Desugar `[1] * 3` into a repeat loop / `[1,2] + [3]` into an append loop.** Rejected for the same
  reason ADR 0288 rejected desugaring a comparison chain into `and`: it fabricates a lowering the road
  does not have, and the shape would be untested against the runtime helpers it implicitly claims. This
  belongs to L11.1's tagged value word, which gives a container a real value representation.
- **Refuse everything, including the pairs the reference raises for.** Rejected: a compile-time refusal
  for `[1] - [2]` replaces the reference's sentence with ours, so a program that catches `TypeError` and
  prints the message reads invented text (ADR 0215's rule, violated three times this stretch).
- **Refuse any container reaching the double road.** Rejected after it broke `half([1.5])`: a container is
  a legal *argument*; only an arithmetic *operand* has no legal container reading.
- **Claim the comparison family wholesale.** Rejected after it broke `a < b` over two names: the answer
  already existed on that road, and an answer may not become a refusal.
- **Emit `trap` with a generic message.** Rejected: the message must be the reference's, byte-for-byte,
  which is what makes the raise catchable *and* testable against `python3`.
