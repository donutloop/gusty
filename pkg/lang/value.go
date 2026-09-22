package lang

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
	case "int", "float", "bool", "None":
		// ints/bools/none are plain immediate values in the interpreter; the
		// heap only allocates reference kinds, so these never appear as objs.
		return TagInt
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
