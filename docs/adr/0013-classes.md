# ADR 0013: Classes with instance attributes and method dispatch

## Decision
`class Name:` bodies hold method `def`s whose first parameter is `self`.
`Name(args)` constructs an instance and calls `__init__(self, args)` if defined.
`obj.attr` reads/writes instance attributes; `obj.method(args)` binds `self`
automatically; `Cls.method(self, args)` is an unbound call where the first
argument is the receiver. Implemented end-to-end in the interpreter
(REPL/`--eval` path).

## Agentic rationale
Classes are the headline Python ergonomic for structuring agent state and
behavior. A single heap registry (`objects`) holds classes, instances, and
bound/unbound method references, so values stay `int64` handles and the
interpreter remains deterministic.

## Codegen/IR implications
- Interpreter: `heap map[int64]*obj` with kinds `class`/`instance`/`method`;
  `self` is bound as a local in `callMethod`; `__init__` runs on construction.
- Semantic analyzer: class names are registered so `Point(2,3)` and `self`
  resolve without "undefined name" diagnostics; Attr assignment targets are
  analyzed.
- LLVM codegen returns "unsupported" for class statements for now; class
  programs are valid through `EvalExpr`/REPL (documented limitation).

## Alternatives rejected
- A boxed class/object value type (no object IR type exists yet).
- Method dispatch via vtables in codegen before the interpreter semantics are
  settled (deferred).
