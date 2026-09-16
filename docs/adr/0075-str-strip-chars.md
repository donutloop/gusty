# ADR 0075: String `.strip([chars])` optional chars in interpreter

## Decision

Extend `str.strip()` to accept an optional `chars` argument — trim the
given chars instead of whitespace — mirroring Python's `str.strip([chars])`.
The interpreter applies `strings.Trim(s, chars)` when chars is provided,
else `strings.TrimSpace(s)`.

## Details

- **Interpreter**: the `callStrMethod` `strip` case now takes at most 1
  argument; with one argument it evaluates it to a `str` and returns
  `allocStr(strings.Trim(s, chars.sval))`; with no argument it returns
  `allocStr(strings.TrimSpace(s))`.

## Scope

- Interpreter path only: codegen string methods with optional string args
  are not folded for the chars case yet.
