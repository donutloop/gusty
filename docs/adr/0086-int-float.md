# ADR 0086: int(x) / float(x) conversion builtins

## Decision

Add `int(x)` and `float(x)` as standard-library conversion builtins.

- `int(x)` converts a value to an integer:
  - `int("42")` -> 42 (decimal string parse)
  - `int(3.9)` -> 3 (float truncation toward zero)
  - `int(7)` -> 7 (identity)
  - non-numeric strings raise an `EvalError` ("int: cannot parse string").
- `float(x)` converts to a float:
  - `float("2.5")` -> 2.5 (decimal string parse)
  - `float(3)` -> 3.0
  - `float(2.5)` -> 2.5 (identity)
  - non-numeric strings raise an `EvalError` ("float: cannot parse string").

## Agentic rationale

The language's dynamic-feeling ergonomics need the same scalar-conversion
builtins Python programmers reach for (`int(...)` / `float(...)`). They are
small, well-understood, and give the standard library a real conversion
surface rather than forcing users to hand-roll parsing.

## Codegen / IR implications

- **`int` ships in BOTH backends.** The AOT codegen folds `int` on literal
  args to a compile-time i32 constant: `int(StrLit)` parses the decimal string
  via `strconv.ParseInt`; `int(IntLit)` is the identity. Non-literal args are
  rejected ("codegen folds only literal int/string args").
- **`float` is interpreter-only.** The AOT codegen represents only i32 integers
  (no float/`double` type in the current IR lowering), so `float` has no
  codegen representation and stays in the interpreter (`pkg/lang/jit.go`),
  where heap float objects (`kind == "float"`) already exist.

## Alternatives rejected

- Adding `float` to codegen via an `f64` type: rejected — the current codegen
  lowers only integers; introducing a floating-point type is a larger memory
  model change (ADR 0011) than this feature warrants.
- A generic `convert(x, type)` builtin: rejected — two explicit, discoverable
  builtins are simpler and match Python's naming.
