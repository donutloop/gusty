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

- The **tag** word is a canonical kind constant from the one dynamic type model
  (`pkg/lang/value.go`). The compiled runtime and the retired AST interpreter's
  heap both derived their tags from this table, which is how the model was
  cross-checked while two implementations existed; with one backend the table is
  the definition, and `lang.ValueTagNames()`, the `--lang` listing and the schema
  documents below are pinned to it so no dispatch can mean something else by a
  number (ADR 0302, ADR 0308).
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
and switches on it. The retired AST interpreter mirrored the same tags via its `obj.tag()` /
`tagOfVal`, which is how the tag table was checked while two implementations existed; with one
backend the table itself is the pinned artifact — `lang.ValueTagNames()`, the `--lang` listing and
the schema document below are asserted to agree with it, so a dispatch cannot disagree with a
number (ADR 0302, ADR 0308).

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

## The function-value pass: `gustyc --callables <src>` (L11.7, ADR 0345)

`gustyc --callables` prints what the defunctionalisation pass decided about one source, as JSON — the same
document the schema declares as `definitions.callablePassReport`. For the source

```python
def twice(fn, v):
    return fn(fn(v))

def down(n, f):
    if n <= 0:
        return 0

    return f(n) + down(n - 1, f)

xs = sorted([3, 1, 2], key=lambda x: 0 - x)
print(twice(lambda x: x + 1, 1), down(3, lambda v: v), xs)   # 3 6 [3, 2, 1]
```

the document is:

```json
{
  "tool": "callable-values",
  "pass": "defunctionalize-callables",
  "hash": "f12e2d00a8653563",
  "rewritten": true,
  "clones": 2,
  "hoisted_lambdas": 3,
  "rewritten_call_sites": 2,
  "recursive_sites": 1,
  "higher_order_functions": ["twice", "down"],
  "symbols": [
    "down$callable$421441b4_0",
    "module$lambda$2cef32aa_0",
    "module$lambda$2def343d_0",
    "sorted$lambda$c4319c9a_0",
    "twice$callable$7946524_0"
  ],
  "refused": []
}
```

An agent planning a higher-order program reads this instead of guessing whether the backend will call its
function:

- `rewritten` is `true` when the pass produced a new program and `false` when it **declined** — and a decline
  leaves the program exactly as it was, including the refusal the backend already had. The pass can only add
  answers, never take them away.
- `refused` carries the sentence the pass declined with, cited to the roadmap row that owns the shape. It is not
  the sentence the *user* of the program is shown: that one comes from the door the program still hits, and both
  name what is missing (ADR 0166).
- `clones`, `hoisted_lambdas` and `rewritten_call_sites` say how much was rewritten, and `symbols` the internal
  names — which never appear in a diagnostic: a clone reports the function the program wrote (`twice`) and a
  hoisted lambda reports `<lambda>`, the reference's own word for it.
- `recursive_sites` says how many calls written **inside a higher-order body** now name that body's own clone
  (`down` recursing with `f` in hand is the one above). Zero means either that no higher-order function recursed,
  or that the pass declined the recursion — in which case `refused` says which shape, naming the parameter and the
  line (ADR 0364, roadmap Gap R.267). A clone that recursed through the un-specialised entry could not leave this
  field at a plausible value by accident.
- `hash` is a digest of the rewrite. One source gives one hash; two runs that disagree are a compiler that cannot
  compile the same program twice.

Exit codes: `0` with the document on stdout, `1` when the source cannot be parsed (`gustyc: callables: …` on
stderr).
