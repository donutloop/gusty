# ADR 0069: String `.zfill(width)` in interpreter

## Decision

Add `str.zfill(width)` — pad with leading zeros to `width` — mirroring
Python's `str.zfill`. The interpreter evaluates the width arg (an int),
and returns the padded string (unchanged when the string is already at/over
width).

## Details

- **Interpreter**: the `callStrMethod` `zfill` case requires exactly 1
  argument, evaluates it to an int width, rejects negative widths, and
  returns `allocStr(strings.Repeat("0", width-len(s)) + s)` when
  `len(s) < width`, else the original string.

## Scope

- Interpreter path only: codegen string methods with int args are not folded
  yet.
