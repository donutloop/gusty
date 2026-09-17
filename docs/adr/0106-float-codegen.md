# ADR 0106: codegen `float` folding

## Decision

Add the scalar builtin `float(x)` to the AOT codegen builtin Call switch:

- `float(int_literal)` folds to the int value (float of an int).
- `float(string_literal)` parses the string to a float (`strconv.ParseFloat`)
  then truncates to int64.

The AOT backend has **no float type**; `value()` already truncates
`FloatLit` to `int64`. So `float` folds to an i32 constant, mirroring the
interpreter's `allocFloat` under the backend's truncated-int representation.

## Motivation

`float` was interpreter-only; the codegen fell through to "unsupported
call". The int/string-literal cases fold cleanly under the existing
truncated-float convention.

## Tests

- `TestIRFloatFolds` (ircheck): llc-verifies `print(float(42))` folds to
  `42` and `print(float("42.5"))` truncates to `42`.

## Alternatives rejected

- Adding a full `double` IR representation (double globals, `%f` print):
  rejected — out of scope; the existing FloatLit-truncation convention keeps
  `float` a pure int/string-literal fold.
