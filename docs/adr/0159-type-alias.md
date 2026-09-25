# ADR 0159 — Structural type aliases (`type X = T`)

## Status
Accepted

## Context
L5.7 adds `type NAME = <type-annotation>`: a compile-time type alias. Users
want to give meaningful names to annotation types (e.g. `type Vec = list[int]`)
so annotations read better and a single type can be referenced in several
places. The type checker must resolve references to an alias. Python's
PEP-695/3.12 aliases are nominal, but this language is a small, compiled
interpreter where nominal aliases would add a whole registry of runtime type
names; the roadmap requires structural resolution "by default".

## Decision
- The parser recognizes `type` as a keyword statement:
  `type NAME = <type-annot>` parses into a new `TypeAliasStmt` AST node with
  `Name` and `Annot`.
- Aliases are **structural**: when an annotation references an alias name,
  `buildType` substitutes a deep structural copy (`cloneType`) of the
  registered annotation. A reference therefore never aliases the registered
  `*Type` in the map (which could mutate on re-registration).
- `TypeAliasStmt` is **compile-time only**: it has no runtime effect. The
  interpreter, AOT codegen, and semantic analyzer all treat it as a no-op
  statement (alongside `PassStmt`).
- The canonical formatter writes `type NAME = <annot>`; because aliases are
  resolved structurally at parse time, later annotations print as the expanded
  type (e.g. `v: list[int]`), matching how the AST stores them.
- The JSON schema gains a `typeAliasStmt` definition and its stmt `oneOf` ref.

## Consequences
- Users can write `type Vec = list[int]` and then `def f(v: Vec)`: `f`'s param
  annotation resolves to `list[int]` and runtime checking behaves exactly as if
  the annotation were written inline.
- Aliases must be declared before use (parse-time resolution is sequential);
  forward references are reported as "unknown type annotation".
- The feature adds no runtime cost: the statement is dropped at every backend.
- A conformance parity program (`typealias.gy`) exercises scalar aliases and is
  registered in the AOT/interpreter parity suite.
