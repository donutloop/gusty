# Richer AST IR JSON schema (`--emit-ast`)

`gustyc -emit-ast "<src>"` prints the parsed AST as JSON. Each node now carries
two optional fields alongside its syntactic fields:

- `span` — the source span `{"line": L, "col": C}` (or `{line,col,end_line,end_col}`)
  of the node, serialized from the node's internal `Src` field. Statements and
  expressions both carry their span.
- `inferred` — the static type inferred by the semantic pass for an expression,
  as a readable name string, e.g. `"int"`, `"str"`, `"list[int]"`,
  `"dict[str, int]"`, `"fn"`. This comes from the same inference the
  semantic analyzer already computes (it is annotated onto each Expr node
  during `Analyze`).

Because these fields are additive and `omitempty`, the JSON remains backward
compatible: nodes that could not be typed simply omit `inferred`.

## Example

    echo 'x = 1 + 2' | gustyc -emit-ast

produces (abridged):

```json
{
  "stmts": [
    {
      "target": { "name": "x", "span": {...}, "inferred": "any" },
      "value": {
        "op": "+",
        "left": { "value": 1, "span": {...}, "inferred": "int" },
        "right": { "value": 2, "span": {...}, "inferred": "int" },
        "span": {...},
        "inferred": "int"
      },
      "span": {...}
    }
  ]
}
```

The machine-readable schema in `pkg/lang/schema.go` (`ASTIRSchema`) documents
the `span` and `inferred` properties on node definitions.

## Correlation

An agent can walk the JSON to find each expression's `inferred` type directly
on the node, and use `span` to locate it in source. This makes the AST dump
self-describing for static-analysis agents: no separate type table or span
reconstruction is required.
