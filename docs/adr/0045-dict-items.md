# ADR 0045: Dict `.items()` in interpreter + AOT codegen

## Decision

Ship `.items()` on dicts in **both** paths, matching interpreter semantics for
the constant-receiver case. The interpreter builds a list of `[key, value]`
pairs; the AOT codegen folds `len({1: 2, 3: 4}.items())` to the pair count 2.

## Details

- **Interpreter**: `callDictMethod` gains an `items` case that builds a list of
  2-element lists (`[key, value]`) from the dict's `o.elems` (keys) and
  `o.dvals` (values).
- **Codegen**: `dictMethodElems` gains an `items` case returning the dict keys,
  so `len` folds `.items()` to the number of pairs (`len` counts keys).

## Scope / limitation

- The codegen folds only `len`/`sum` of `.items()` (pair count / key sum);
  emitting the full list of pairs at runtime is deferred. Non-constant dict
  receivers remain interpreter-only.
