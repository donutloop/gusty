# ADR 0092: fix decorator codegen panic on 0-param functions

## Decision

Fix a panic in the closure IR emission (`pkg/lang/closure.go`) when a
**decorated function has zero parameters**: `strings.Repeat("i32, ", n-1)`
with `n == 0` produced a negative `Repeat` count, panicking
("strings: negative Repeat count") during `--emit-llvm` of
`@deco def h(): ...`.

The fix guards the count via `repeatParamTypes(n)`: returns `""` for
`n <= 1`, else `"i32, "` repeated `n-1`. A 1-param function signature needs no
separator, so the old `n-1` was also wrong for `n == 1` (it would emit a
stray trailing ", " in some callers).

## Motivation

Decorator codegen is a mission item; a panic on the common `@deco def h():`
shape is a correctness regression (crash, not a clear error).

## Tests

`integration/lang_test.go` (`TestDecoratorCodegenZeroParam`): compiles
`@twice def h(): return 1; print(h())` and asserts the IR keeps `main()` and
does not panic.

## Alternatives rejected

- Go's builtin `max` (Go 1.21): rejected — the project targets Go 1.20; a
  small helper keeps the version constraint.
