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

// TestKindTagRoundTrip holds the canonical tag table together. It used to compare the interpreter
// heap's obj tag against the AOT dispatch tag, kind by kind — the two engines' encodings had to
// mirror each other, so one table could not move without the other. ADR 0302 left one encoding, and
// the invariant that survives is the one that matters: every reference kind names a tag, and that
// tag names the kind back. A container that round-tripped to the wrong name would print a dict as a
// set, which is exactly the class of defect this table exists to make impossible.
func TestKindTagRoundTrip(t *testing.T) {
	for _, kind := range []string{"str", "list", "dict", "set", "tuple", "class", "instance", "method", "closure", "exn", "module"} {
		tag := objKindTag(kind)
		if tag == 0 {
			t.Errorf("kind %q has no dispatch tag", kind)
			continue
		}
		if got := kindForTag(tag); got != kind {
			t.Errorf("objKindTag(%q) = %d, which names %q back — the tag table has drifted", kind, tag, got)
		}
	}
}
