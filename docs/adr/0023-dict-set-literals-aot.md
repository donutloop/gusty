# ADR 0023: Inline dict/set literals with constant-key indexing in the LLVM AOT codegen

## Decision

The interpreter already indexes boxed dicts and sets at runtime (`{1: 10, 2: 20}[1]`
and `{1, 2, 3}[2]`). Mirror this in the LLVM AOT codegen path (`pkg/lang/codegen.go`)
so inline dict/set literals with constant-key indexing and `len` ship in **both**
execution backends per the two-path contract.

## Codegen/IR implications

- A dict literal lowers to a dedicated global struct `{i32 count, [n x i32] keys,
  [n x i32] vals}`; a set literal lowers to `{i32 count, [n x i32] elems}`.
- `dict[key]` with a constant key resolves at **compile time** to the matching
  constant value (no runtime scan); `set[el]` with a constant element resolves to
  that element if present.
- `len({...})` loads the count field (field 0) of the global struct.
- Because literals are lowered as inline globals, they must be used inline (no
  assignment-to-variable indirection) — the same limitation already documented for
  list literals.

## Alternatives rejected

- Emitting a runtime scan over the keys/elems for non-constant keys — requires
  dynamic indexing into a global struct, which this llc build rejects (only
  constant GEP indices are accepted). Constant-key folding is deterministic and
  matches the existing list-literal inline-global approach.
- Sharing the interpreter's heap-allocated dict/set representation — the AOT path
  has no allocator; inline constant globals keep codegen straight-line and
  verifiable.
