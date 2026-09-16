# ADR 0070: String `.ljust(width)` / `.rjust(width)` in interpreter

## Decision

Add `str.ljust(width)` / `str.rjust(width)` — pad with spaces on the right /
left to `width` — mirroring Python's `str.ljust` / `str.rjust`. The
interpreter evaluates the width arg (an int) and returns the padded string
(unchanged when the string is already at/over width).

## Details

- **Interpreter**: the `callStrMethod` `ljust`/`rjust` cases require exactly
  1 argument, evaluate it to an int width, reject negative widths, and
  return `allocStr(s + strings.Repeat(" ", width-len(s)))` /
  `allocStr(strings.Repeat(" ", width-len(s)) + s)` when `len(s) < width`,
  else the original string.

## Scope

- Interpreter path only: codegen string methods with int args are not folded
  yet.
