# ADR 0156: Literal-container membership (`in` / `not in`) in the AOT backend

## Status
Accepted.

## Context

Gap G: `x in [1,2,3]` (and `not in`) with a runtime `x` was wrong in the AOT
(codegen) backend. List/set/dict literals are lowered to compile-time global
structs (`@.lstN`, `@.setN`, `@.dictN`), not heap objects. The old lowering
passed the literal's global-struct address straight into `rt_contains`, which
indexes `@heap` by that address — undefined behavior that only "worked" by
coincidence for tiny literals. The interpreter builds real heap lists/dicts
for literals, so it was correct.

## Decision

When the right operand of `in`/`not in` is a *literal* list/set/dict whose
membership-test values (list/set elements, or dict keys) are all integer
constants, emit an unrolled `l == val0 || l == val1 || ...` comparison chain
instead of `rt_contains`:

- `[1,2,3]` tests its elements.
- `{1,2,3}` (set literal) tests its elements.
- `{1: a, 2: b}` (dict literal) tests its *keys* — matching the interpreter's
  dict `in` semantics.
- Empty literal containers are a compile-time constant: `x in {}` is `0`,
  `x not in {}` is `1`.
- `not in` inverts the whole chain.
- A string-typed tested value (`StrLit`/`FString`) is guarded out: unrolling a
  string pointer against integer constants would produce invalid i32 IR.
- Non-integer literal elements fall through to the existing `rt_contains`
  heap-handle path unchanged.

New helpers: `intMembershipValues` (extracts the test values), `constIntMemberVal`
(constant-folds `IntLit`/`NoneLit`/`BoolLit`/unary-minus), and `isStringExpr`.

## Consequences
- `x in [1,2,3]`, `x in {1,2,3}`, `x in {1: a, ...}`, empty-container `in`,
  and `not in` now produce identical output on the interpreter and AOT
  backends (covered by `TestParityLiteralMembership`).
- Heap-list membership (`x in lst` where `lst` is a runtime list) still uses
  `rt_contains` and is unchanged.
- No behavior change for non-literal containers.

## Alternatives considered
- Materializing literal containers into heap lists at runtime: rejected — the
  literals are compile-time constants; unrolled comparisons are simpler and
  avoid allocation.
- A new runtime helper operating on literal structs: rejected — unrolled IR is
  self-contained and needs no new runtime entrypoint.
