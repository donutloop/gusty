# CHANGELOG

Single clean list of features, newest first.

## Current

- `feat(interp)`: `for i in range(a, b)` iterates `a`..`b-1` (interpreter).
- `feat(semantic)`: reject `break`/`continue` outside a loop with a diagnostic;
  track loop depth through `while`/`for` bodies.
- `feat(lang)`: add `break` and `continue` loop control in the parser,
  interpreter, and IR emitter (loop-label tracking; llc-clean). Loop bodies now
  go through the full statement dispatcher, fixing nested `if`/control flow
  inside `while`/`for` bodies.
- `feat(codegen)`: emit `match` as a chain of integer comparisons with
  terminators on every basic block (llc-clean IR).
- `feat(interp)`: evaluate `match` statements with literal patterns in the
  interpreter.
- `feat(interp)`: evaluate `if`/`elif`/`else`, `while`, and `for ... in
  range(n)` in the interpreter.
- `feat(interp)`: register and call user-defined functions in the
  interpreter; bind params in a fresh scope and return via `evalBody`.
- `feat(semantic)`: infer user function signatures by binding call argument
  types to parameters and analyzing the body; check argument counts.
- `feat(semantic)`: indentation-aware lexer producing `INDENT`/`DEDENT`.
- `feat(codegen)`: deterministic textual LLVM IR emitter (opaque pointers)
  replacing the crashing go-llvm `CreateCall` path; verified by `llc
  -opaque-pointers`.
- `feat(lang)`: Python-like indentation-based language with functions, control
  flow, match, print, and range.
