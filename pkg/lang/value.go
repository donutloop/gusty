package lang

import "fmt"

// ValueTag is the canonical numeric tag for a dynamic value kind. Both the
// AOT runtime IR (%obj tagged values) and the interpreter heap derive their
// tags from this single table, so AOT and interpreter agree on the dynamic
// type model by construction: the codegen emits the same constants into the
// LLVM IR that the interpreter uses when tagging heap objects.
type ValueTag int

// Canonical dynamic-kind tags. These are the values placed in the `tag` word
// of an `%obj` tagged runtime value and mirrored by the interpreter's
// obj.tag/tagOfVal. Keep the numeric values fixed: the AOT runtime IR and the
// interpreter both read them from this table, so renumbering here would
// silently change the wire format.
const (
	TagInt      ValueTag = iota // plain integer (immediate payload)
	TagFloat                    // double stored as raw payload bits
	TagBool                     // 0/1 boolean (immediate payload)
	TagNone                     // None singleton
	TagStr                      // heap string handle (payload)
	TagList                     // heap list handle (payload)
	TagDict                     // heap dict handle (payload)
	TagSet                      // heap set handle (payload)
	TagTuple                    // heap tuple handle (payload)
	TagClass                    // heap class handle (payload)
	TagInstance                 // heap instance handle (payload)
	TagMethod                   // heap bound-method handle (payload)
	TagClosure                  // heap closure handle (payload)
	TagExn                      // heap exception handle (payload)
	TagModule                   // heap module handle (payload)
)

// objKindTag maps an interpreter heap object's kind string to its canonical
// ValueTag. Heap kinds not representable as a single dynamic value (break /
// continue signals) are not part of the %obj value model.
func objKindTag(kind string) ValueTag {
	switch kind {
	case "int", "float":
		// ints and floats are plain immediate values for everything the interpreter does from
		// the source; the heap allocates a box when the value has to outlive the expression —
		// a float whose bits travel, and now a bool stored in a container slot (Gap R.112).
		return TagInt
	case "bool":
		// A bool in a container slot is a heap object carrying a 0/1, and the tag table has
		// always had a name for it: the slot says bool, and the printer and the comparison read
		// that from the same table the compiled runtime reads (ADR 0182's one-table rule).
		return TagBool
	case "none", "None":
		// None is the one null that *is* heap-allocated (the singleton), because
		// ints are raw int64s and no int value is free to stand for "no value".
		return TagNone
	case "str":
		return TagStr
	case "list":
		return TagList
	case "dict":
		return TagDict
	case "set":
		return TagSet
	case "tuple":
		return TagTuple
	case "class":
		return TagClass
	case "instance":
		return TagInstance
	case "method":
		return TagMethod
	case "closure":
		return TagClosure
	case "exn":
		return TagExn
	case "module":
		return TagModule
	}
	return TagInt
}

// kindForTag returns the canonical dynamic kind name for a ValueTag.
func kindForTag(t ValueTag) string {
	switch t {
	case TagInt:
		return "int"
	case TagFloat:
		return "float"
	case TagBool:
		return "bool"
	case TagNone:
		return "None"
	case TagStr:
		return "str"
	case TagList:
		return "list"
	case TagDict:
		return "dict"
	case TagSet:
		return "set"
	case TagTuple:
		return "tuple"
	case TagClass:
		return "class"
	case TagInstance:
		return "instance"
	case TagMethod:
		return "method"
	case TagClosure:
		return "closure"
	case TagExn:
		return "exn"
	case TagModule:
		return "module"
	}
	return "int"
}

// --- The compiled heap's kind word: one projection of the tag table ---------------
//
// Every object in the compiled heap carries a small integer `kind` in its header: it is
// `rt_alloc`'s parameter, and it is what `rt_gc`, `rt_inst_get` and the container
// helpers switch on. That number and the canonical `ValueTag` above were two unrelated
// vocabularies — `TagList` is 5 while a list's heap kind is 1 — and codegen wrote
// `call i32 @rt_alloc(i32 1)` by hand at a dozen sites, so nothing tied the two together.
//
// L11.1 (the tagged value word) needs the two to be one model: the same integer has to
// mean "list" in the object header, in a per-element tag word, and in an exported
// `%gusty_value`. This is the step that makes that true: the numbers below are the wire
// format (they are constants, because they are emitted into IR), while names, tags and
// diagnostics are read from the canonical table, and a test ties the two together so
// the projection cannot drift.
const (
	// HeapKindNone is "not a runtime heap object" — a scalar, or a value the compiled
	// backend represents without allocating.
	HeapKindNone = 0
	// HeapKindList/Dict/Set/Instance are the allocatable heap kinds. Their values are
	// part of the runtime's wire format; heapKindOrder is the same list read as tags.
	HeapKindList     = 1
	HeapKindDict     = 2
	HeapKindSet      = 3
	HeapKindInstance = 4
	// HeapKindFloat is a float box, and it is deliberately outside heapKindOrder: a float is
	// not an allocatable value kind in the source sense, it is storage the compiled backend
	// invents because a container slot is one i32 word and a double does not fit in one
	// (roadmap L11.1). So HeapKindFor(TagFloat) stays HeapKindNone — nothing asks to allocate
	// "an object of tag float" — while rt_float_new allocates this kind and the collector, which
	// marks and sweeps by index rather than by kind, recycles boxes with their containers.
	HeapKindFloat = 5
)

// heapKindOrder is the projection: heapKindOrder[i] is the canonical tag of the heap
// kind numbered i+1. It is deliberately the only place that relates a heap kind to a
// name or a tag — HeapKindName and HeapKindFor both read it.
var heapKindOrder = []ValueTag{TagList, TagDict, TagSet, TagInstance}

// HeapKindFor returns the compiled heap's kind number for a canonical tag, or
// HeapKindNone for a kind the compiled heap does not allocate (int, float, bool, None,
// str are immediates or interned, and closure/exn/module have their own storage).
func HeapKindFor(t ValueTag) int32 {
	for i, k := range heapKindOrder {
		if k == t {
			return int32(i + 1)
		}
	}
	return HeapKindNone
}

// HeapKindKnown reports whether a number handed to rt_alloc is one the compiled heap understands.
// Most of them are the projected kinds; HeapKindFloat is the one kind outside the projection, and
// naming it here is what keeps the tag/kind agreement tests from reading a float box as a stray
// allocation (roadmap L11.1, ADR 0233).
func HeapKindKnown(kind int32) bool {
	if kind == HeapKindNone || kind == HeapKindFloat {
		return true
	}
	return kind > 0 && int(kind) <= len(heapKindOrder)
}

// HeapTagFor is the inverse: the canonical tag a heap kind stands for. An unknown kind
// reports TagInt, matching kindForTag's fallback for a value with no better answer.
func HeapTagFor(kind int32) ValueTag {
	if kind <= HeapKindNone {
		return TagInt
	}
	if i := int(kind) - 1; i < len(heapKindOrder) {
		return heapKindOrder[i]
	}
	return TagInt
}

// HeapKindNameOf names a heap kind for diagnostics and JSON. The name comes from the tag
// table, so a kind cannot be called one thing in `--lang` and another in an error.
// (heapargs.go keeps an `int`-typed HeapKindName wrapper for its inference results.)
func HeapKindNameOf(kind int32) string {
	if kind == HeapKindNone {
		return "none"
	}
	return kindForTag(HeapTagFor(kind))
}

// HeapKindNames lists the allocatable heap kinds in wire order, for self-description.
func HeapKindNames() []string {
	out := make([]string, 0, len(heapKindOrder))
	for range heapKindOrder {
		out = append(out, HeapKindNameOf(int32(len(out)+1)))
	}
	return out
}

// ValueTagNames lists the canonical tags in table order, with their numbers: the
// machine-readable form of the dynamic type model (--lang reports it, so an agent can
// target a tag without reading Go).
func ValueTagNames() []string {
	all := []ValueTag{TagInt, TagFloat, TagBool, TagNone, TagStr, TagList, TagDict, TagSet,
		TagTuple, TagClass, TagInstance, TagMethod, TagClosure, TagExn, TagModule}
	out := make([]string, 0, len(all))
	for _, t := range all {
		out = append(out, fmt.Sprintf("%s=%d", kindForTag(t), int(t)))
	}
	return out
}
