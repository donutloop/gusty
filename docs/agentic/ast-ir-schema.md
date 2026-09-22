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

## Runtime dispatch: the `%obj` tagged value representation

Every dynamic runtime value in the AOT IR is a tagged two-word value:

```llvm
%obj = type {i32, i32} ; {kind tag, payload}
```

- The **tag** word is a canonical kind constant from the shared dynamic type
  model (`pkg/lang/value.go`). Both the AOT runtime and the interpreter heap
  derive their tags from this single table, so the dynamic type model is
  shared by construction.
- The **payload** word is either a heap handle (for reference kinds:
  str/list/dict/set/tuple/class/instance/method/closure/exn/module) or the raw
  immediate (for int/bool/None).

Runtime helpers emitted into the prelude:

```llvm
%obj @rt_mkobj(i32 tag, i32 payload)   ; build a tagged value
i32   @rt_obj_tag(%obj)                ; read the kind tag word
i32   @rt_obj_payload(%obj)            ; read the payload word
i1    @rt_obj_is(%obj, i32 tag)        ; test the kind tag
```

Dynamic method dispatch wraps the receiver instance handle as
`{tag=TagInstance, payload=handle}`, verifies the tag (`rt_obj_is`), extracts
the payload (`rt_obj_payload`), then reads the class-id from instance slot 0
and switches on it. The interpreter mirrors the same tags via `obj.tag()` /
`tagOfVal`, so dispatch behaves identically on both backends (verified by the
`TestParityObjTaggedDispatch` parity test).

Canonical kind tags (value.go):

| tag | kind       | payload                        |
|-----|------------|--------------------------------|
| 0   | int        | immediate                      |
| 1   | float      | raw double bits                |
| 2   | bool       | 0/1                            |
| 3   | None       | singleton                      |
| 4   | str        | heap handle                    |
| 5   | list       | heap handle                    |
| 6   | dict       | heap handle                    |
| 7   | set        | heap handle                    |
| 8   | tuple      | heap handle                    |
| 9   | class      | heap handle                    |
| 10  | instance   | heap handle                    |
| 11  | method     | heap handle                    |
| 12  | closure    | heap handle                    |
| 13  | exn        | heap handle                    |
| 14  | module     | heap handle                    |
