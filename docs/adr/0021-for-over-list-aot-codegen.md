# ADR 0021: `for` loops over inline list literals in the LLVM AOT codegen

## Decision

Mirror the interpreter's for-over-list iteration in the LLVM AOT codegen path
(`pkg/lang/codegen.go`), so `for x in [1, 2, 3]: ...` ships in **both**
execution backends per the two-path contract. The interpreter already iterates
boxed list/set/dict elements; this ADR closes the AOT gap (previously
range-only).

## Codegen/IR implications

- `for x in [a, b, c]:` lowers to **unrolled** body blocks, one per constant
  element of the inline list literal, each preceded by `store i32 <el>, i32* %x`
  into the loop-variable alloca. An empty literal iterates zero times.
- `break` branches to the loop-end label, skipping the `else:` clause;
  `continue` branches to the next element's body block. Normal completion
  (after the last element) enters the `else:` clause when present — identical
  to the existing `range` loop lowering.
- This mirrors the interpreter semantics exactly (break skips `else`, continue
  advances to the next element).

## Alternatives rejected

- A runtime loop over the list struct — requires non-constant GEP indices,
  which this llc build rejects; unrolling over constant elements is
  deterministic and mirrors the inline-list-literal lowering already used for
  `sum`/`min`/`max`.
- Lowering `else` as a separate branch per element — unnecessary; a single
  fall-through after the last body block preserves Python semantics.
