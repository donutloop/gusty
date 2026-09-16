# ADR 0076: String `.lstrip([chars])` / `.rstrip([chars])` optional chars

## Decision

Extend `str.lstrip()` / `str.rstrip()` to accept an optional `chars`
argument — trim the given chars instead of whitespace — mirroring Python's
`str.lstrip([chars])` / `str.rstrip([chars])`.

## Details

- **Interpreter**: the `callStrMethod` `lstrip`/`rstrip` cases now take at
  most 1 argument; with one argument they evaluate it to a `str` and return
  `allocStr(strings.TrimLeft(s, chars.sval))` /
  `allocStr(strings.TrimRight(s, chars.sval))`; with no argument they keep
  the existing `TrimLeftFunc`/`TrimRightFunc` whitespace trimming.

## Scope

- Interpreter path only: codegen string methods with optional string args
  are not folded for the chars case yet.
