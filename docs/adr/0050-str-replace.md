# ADR 0050: String `.replace(old, new)` in interpreter + AOT codegen

## Decision

Add `str.replace(old, new)` — replace every occurrence of `old` with `new` —
in **both** paths, mirroring Python's `str.replace`. The interpreter evaluates
both arguments as strings and applies `strings.ReplaceAll`; the codegen
constant-folds it when the receiver and both arguments are string constants.

## Details

- **Interpreter**: the `callStrMethod` `replace` case requires exactly 2
  arguments, evaluates each to a `str` heap object, and returns
  `allocStr(strings.ReplaceAll(s, old.sval, new.sval))`.
- **Codegen**: `stringConst` and `stringVal` gain a `replace` case folding the
  receiver via the two constant string args; the runtime `call` attr path
  folds `replace` through `stringVal` on the receiver and args before falling
  back to "unsupported string method".

## Scope

- Only constant receivers/args fold in the codegen (matching the existing
  `.upper`/`.lower`/`.strip`/`.split` constant-folding pattern); a non-constant
  receiver errors as "unsupported string method". Interpreter path supports
  runtime string receivers.

## Alternatives rejected

- Emitting a runtime `str.replace` helper in the AOT path: deferred — the
  existing string-method AOT surface is constant-folding only, so keeping
  `.replace` in that same envelope is consistent.
