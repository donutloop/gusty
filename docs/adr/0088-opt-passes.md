# ADR 0088: IR optimization pass pipeline

## Status

Accepted (revised — supersedes the regex-transform optimizer).

## Context

`opt.go` was originally a regex-ish text transform: it deleted globals whose
`@name` never appeared in function bodies and otherwise left the emitted IR
untouched. The codegen emits a small, regular subset of textual LLVM IR
(typed arithmetic/compare/select/zext/sitofp/fptosi, loads/stores into
allocas, calls, `br`/`ret` terminators). Phase 2 requires a real IR-level
optimizer: CFG construction, dead-block elimination, constant
propagation/folding, and mem2reg-style alloca promotion, verified against
`llc` output.

## Decision

Replace the regex transform with a genuine pass pipeline over a parsed
textual-LLVM-IR module:

1. **Parser**: `parseModule` separates globals/declares from function bodies;
   `parseInstr` tokenizes each instruction into a structured `irInstr`
   (opcode, predicate, operand type, operands, load/store pointer, branch
   operands). Complex call/gep lines are kept opaque; only register
   extraction applies to them. Instruction text is preserved verbatim and
   rewritten token-wise, so serialization stays byte-compatible with
   `llvm-as`/`llc`.

2. **CFG construction**: `buildCFG` derives successor/predecessor edges from
   each block's terminator. Dominators are computed with the classic
   iterative dominator-set fixed point (`domSets`).

3. **Constant propagation + folding** (`constFold`): a fixpoint worklist folds
   `add/sub/mul/sdiv/srem`, `icmp/fcmp`, `and/or/xor`, `select`,
   `zext/sitofp/fptosi` when all operands resolve to constants (div-by-zero
   and inexact `fptosi` are never folded). Folded defs are recorded in a
   `def -> value` map and substituted into uses token-wise, including inside
   call/store lines. Constant branch conditions are folded to unconditional
   `br label` (via `foldBranches`).

4. **mem2reg-style alloca promotion** (`promote`): an alloca with no loads is
   dead (alloca + all its stores deleted); an alloca with exactly one store
   that dominates every load (dominance + same-block program order checked) is
   promoted — loads are replaced by the store's value and the alloca/store/
   loads deleted. Multi-store allocas that would need phi insertion are
   conservatively left alone.

5. **Dead instruction elimination** (`dce`): instructions whose def register
   is never used are dropped, except side-effecting instructions (store,
   call, terminators).

6. **Dead-block elimination** (`deadBlockElim`): blocks unreachable from the
   entry block are removed after CFG reachability analysis.

The passes run per function to a fixed point, then the module is
re-serialized. Public API is unchanged: `OptimizeIR(ir, level)` with
`level <= 0` returning identity. Unit tests cover each pass and an
integration test assembles/lowers the optimized output through `llvm-as`/`llc`
when those tools are installed.

## Consequences

- IR is genuinely optimized: unreachable blocks, dead allocas/stores,
  constant expressions, and promoted single-store variables disappear; loop
  -carried variables (which need phis) are left intact.
- Output remains valid textual IR for the whole emitted subset (verified with
  `llvm-as`/`llc` in `TestOptimizedIRValidForLLC`).
- The old regex transform and its dead-global text hack are removed; the
  dead-global elimination is reimplemented over the parsed module.
