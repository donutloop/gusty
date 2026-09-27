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

## Diagnostics: stable rule codes

Every diagnostic the checker emits (`--verify`, `--check`, `--json`, the LSP)
carries a machine-readable `code` beside `msg`, so an agent branches on the rule
instead of pattern-matching prose:

```json
{
  "level": "error",
  "span": { "line": 5, "col": 5 },
  "msg": "argument \"xs\": expected list[int], got list[str] — list[int] is invariant in T: str is not int",
  "code": "type.variance.invariant",
  "suggestion": "the destination can WRITE through this container, so the type arguments must match — use the same T, drop the type argument, or take a read-only Sequence[T] view (covariant)"
}
```

| `code` | rule |
|--------|------|
| `type.mismatch` | plain kind mismatch (`expected int, got str`) |
| `type.variance.invariant` | `list[T]` / `set[T]` / `dict[K, V]` type arguments must match exactly |
| `type.variance.covariant` | `Sequence[T]` / `iter[T]` / `tuple[...]` elements may be widened, not narrowed |
| `type.variance.contravariant` | a callable must accept everything the destination will pass |
| `type.variance.nominal` | a class annotation accepts only that class or a subclass |
| `type.callable.arity` | callable / tuple arity mismatch |
| `type.union.members` | no union member accepts the value |

`code` is stable; `msg` wording may improve. Both the `diagnostic` shape (with
its code enum) and the `varianceRule` shape are declared in `gustyc --schema`.

## Variance table (self-describing)

`gustyc --variance` prints the variance model the checker implements, as JSON:

```json
{
  "schema_version": "1.0",
  "language_version": "0.10.0",
  "generated_by": "gustyc --variance",
  "rules": [
    {
      "constructor": "list[T]", "params": ["T"], "variance": ["invariant"],
      "mutable": true, "read_only": "Sequence[T]", "code": "type.variance.invariant",
      "rationale": "lists are mutable through every alias, so a widened element type would let a write through one name corrupt another"
    }
  ]
}
```

An agent planning a compilation reads this table instead of guessing which
substitutions are legal: `params[i]` pairs with `variance[i]`, `mutable` says
why, `read_only` names the covariant alternative to suggest, and `code` is the
diagnostic emitted when the rule is broken.
