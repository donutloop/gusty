# ADR 0142: `%obj`-tagged runtime value representation

## Status
Implemented (Round 12).

## Context
The AOT backend was "i32-only": list/dict/set values were raw heap handles,
ints were raw i32s, and dynamic method dispatch trusted a receiver handle
without carrying any kind information. The interpreter heap used string kinds
(`obj.kind`) that had no numeric counterpart in the AOT IR. The two backends
therefore did not agree on a dynamic type model, and runtime dispatch could not
verify the kind of a receiver value.

## Decision
Introduce a canonical tagged runtime value, `%obj = type {i32, i32}`, whose
first word is a kind tag and whose second word is a payload (heap handle for
reference kinds, immediate for int/bool/None). The kind tags are defined in a
single Go-side canonical table (`pkg/lang/value.go`) that both the AOT runtime
IR (emitting the constants into LLVM) and the interpreter heap (`obj.tag()`,
`tagOfVal`) read, so AOT and interpreter agree on the dynamic type model by
construction.

Runtime helpers emitted into the prelude: `rt_mkobj`, `rt_obj_tag`,
`rt_obj_payload`, `rt_obj_is`.

Dynamic method dispatch now operates on a tagged receiver: it wraps the
instance handle as `{tag=instance, payload=handle}`, verifies the tag with
`rt_obj_is`, and reads the payload with `rt_obj_payload` before the class-id
switch. The interpreter mirrors the same tags.

## Consequences
- AOT and interpreter share one dynamic type model; dispatch is kind-checked.
- New runtime surface: `%obj` type + four helpers (verified by llc IR test).
- Parity test `TestParityObjTaggedDispatch` proves polymorphic dispatch
  produces identical stdout on both backends.
- The rest of the codegen pipeline remains i32; `%obj` is the canonical
  dispatch value type and the foundation for full dynamic value tagging.
