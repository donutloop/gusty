# ADR 0097: codegen `any`/`all` builtins

## Decision

Lower `any(iter)` and `all(iter)` in the AOT codegen for **list-literal**
arguments (`ListLit`/`SetLit`):

- Evaluate each element to an i32 temp.
- Emit `icmp ne i32 <el>, 0` -> a boolean temp.
- For `any`, `or i1` the boolean temps; for `all`, `and i1` them.
- Empty iterables: `any` is 0, `all` is 1 (vacuous truth).

This mirrors the existing `sum`/`min`/`max` codegen pattern (per-element temp
evaluation + IR accumulation), so `print(any([0,0,1]))` now compiles instead
of "unsupported call any".

## Motivation

The interpreter already had `any`/`all` (ADR 0090); the codegen rejected
them ("unsupported call"), so programs using them could not be AOT-compiled.
This closes the stdlib parity gap for the boolean quantifiers.

## Tests

`integration/lang_test.go` (`TestAnyAllCodegenIR`): `print(any([0,0,1]))`
and `print(all([1,1,1]))` compile and the IR contains `icmp ne i32`.

## Alternatives rejected

- Control-flow early-return lowering: rejected — OR/AND accumulation is
  branch-free and matches the existing `sum`/`min`/`max` codegen style.
