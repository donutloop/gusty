package lang

import "testing"

// TestValueTagTableIsCanonical locks the %obj kind-tag numbering. The AOT
// runtime IR emits these constants into LLVM and the interpreter heap uses
// them via objKindTag; both read from the same table, so the dynamic type
// model is shared by construction. Renumbering here changes the wire format.
func TestValueTagTableIsCanonical(t *testing.T) {
	want := map[string]ValueTag{
		"str":      TagStr,
		"list":     TagList,
		"dict":     TagDict,
		"set":      TagSet,
		"tuple":    TagTuple,
		"class":    TagClass,
		"instance": TagInstance,
		"method":   TagMethod,
		"closure":  TagClosure,
		"exn":      TagExn,
		"module":   TagModule,
	}
	for kind, tag := range want {
		if objKindTag(kind) != tag {
			t.Errorf("objKindTag(%q) = %d, want %d", kind, objKindTag(kind), tag)
		}
		// AOT and interpreter must round-trip: the canonical name for the tag
		// must be the heap kind string the interpreter tags.
		if kindForTag(tag) != kind {
			t.Errorf("kindForTag(%d) = %q, want %q", tag, kindForTag(tag), kind)
		}
	}
}

// TestObjTagMirrorsAOT verifies the interpreter heap obj tag mirrors the
// canonical AOT dispatch tag for each reference kind.
func TestObjTagMirrorsAOT(t *testing.T) {
	for _, kind := range []string{"str", "list", "dict", "set", "tuple", "class", "instance", "method", "closure", "exn", "module"} {
		o := &obj{kind: kind}
		if o.tag() != objKindTag(kind) {
			t.Errorf("obj(%q).tag() = %d, want %d", kind, o.tag(), objKindTag(kind))
		}
	}
}

// TestTagOfVal verifies tagOfVal agrees with the AOT representation: a heap
// value reports its canonical reference tag and a plain value reports the
// immediate tag (payload carries the raw value in AOT IR).
func TestTagOfVal(t *testing.T) {
	e := &Evaluator{heap: map[int64]*obj{}}
	h := e.allocObj("instance")
	if e.tagOfVal(h) != TagInstance {
		t.Fatalf("tagOfVal(instance handle) = %d, want TagInstance", e.tagOfVal(h))
	}
	if e.tagOfVal(7) != TagInt {
		t.Fatalf("tagOfVal(plain) = %d, want TagInt", e.tagOfVal(7))
	}
}
