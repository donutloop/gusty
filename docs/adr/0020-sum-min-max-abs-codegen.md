# ADR 0020: `sum` / `min` / `max` / `abs` builtins in the LLVM AOT codegen

## Decision

Mirror the interpreter's `sum`, `min`, `max`, and `abs` builtins in the LLVM
AOT codegen path (`pkg/lang/codegen.go`), so these numeric builtins ship in
**both** execution backends per the two-path contract.

## Codegen/IR implications

- `sum([a, b, c])` lowers to an **unrolled** `add` chain over the inline list
  literal's global struct (`{i32, [n x i32]}`), one constant-GEP load per
  element. This avoids non-constant GEP indices, which this llc build rejects.
- `min([...])` / `max([...])` fold with `icmp slt`/`icmp sgt` + `select`,
  keeping the running best value in SSA temporaries.
- `abs(x)` emits `sub i32 0, x`, `icmp slt x, 0`, and `select`; a literal
  argument is constant-folded to the positive value.
- Because lists are lowered only as inline literals in AOT, `sum`/`min`/`max`
  require an **inline list literal** argument in the AOT path; the interpreter
  accepts any list/set value and is unchanged.

## Alternatives rejected

- A runtime loop over the list struct — requires non-constant GEP indices,
  which this llc build rejects; unrolling is deterministic and simple.
- Sharing a single helper function per builtin — unnecessary for these simple
  folds and would complicate SSA generation.
