# ADR 0074: String `.expandtabs(tabsize)` in interpreter

## Decision

Add `str.expandtabs(tabsize)` — replace each tab with spaces to the next
tab stop of size `tabsize` — mirroring Python's `str.expandtabs`. The
interpreter evaluates the tabsize arg (an int), tracks the current column,
and writes spaces for each tab.

## Details

- **Interpreter**: the `callStrMethod` `expandtabs` case requires exactly 1
  argument, evaluates it to an int tabsize (rejecting non-positive), scans
  runes tracking the column, writes `tabsize - (col % tabsize)` spaces per
  tab, and returns the built string.

## Scope

- Interpreter path only: codegen string methods with int args are not folded
  yet.
