# ADR 0049: String equality in interpreter + AOT codegen

## Decision

Fold string equality `==` / `!=` on constant string operands in **both** paths
by comparing contents, not handles/pointers. Previously the interpreter
compared string handles (0 for equal-but-distinct literals) and the codegen
compared string-global references.

## Details

- **Interpreter**: the `==` eval case gains a string check: when both operands
  are `str` heap objects, compare `lo.sval` / `ro.sval` and return 1/0 before
  the handle equality.
- **Codegen**: the BinOp comparison constant-folds `==`/`!=` via `stringVal`
  on both operands, returning 1/0 (contents) before the integer comparison.

## Scope

- Only constant string operands fold in the codegen; non-constant string
  receivers fall through to the existing handle/int comparison.
