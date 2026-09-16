# ADR 0046: `min`/`max` fold dict-method Call args

## Decision

Wire the `min`/`max` builtins' elems resolution to the existing
`g.dictMethodElems` helper, so `max({1: 2, 3: 4}.keys())` -> 3 and
`min({1: 2, 3: 4}.values())` -> 2 fold in the AOT codegen — matching
`len`/`sum` which already fold dict-method Call args.

## Details

- `min`/`max` shared an elems resolution (`var elems []Expr` + type checks on
  ListLit/SetLit/DictLit). A `dictMethodElems(c.Args[0])` block initializes
  `elems` from a dict-method Call, so the existing fold loop runs over the
  keys/vals.

## Scope

- `.items()` still folds only `len`/`sum` (pair count / key sum); non-constant
  receivers and runtime pair-list emission remain interpreter-only.
