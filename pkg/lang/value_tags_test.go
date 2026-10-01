package lang

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// L11.1 begins where the value model has to begin: one table says what a value *is*.
// Three vocabularies claimed that role — the canonical ValueTag list, the extern-fn
// ABI's tag constants, and the compiled heap's `kind` word (which numbered list as 1
// while TagList is 5, and was written into IR as `rt_alloc(i32 1)` at a dozen sites).
// These tests are what stop them drifting apart again as per-element tagging lands.

func TestValueTagTableIsPinned(t *testing.T) {
	// The numbers are a wire format: the interpreter's heap, the compiled %obj values
	// and exported ABI tags all read them. Renumbering is a breaking change, so it has
	// to be a decision someone makes here, not an accident in a refactor.
	want := map[string]int{
		"int": 0, "float": 1, "bool": 2, "None": 3, "str": 4, "list": 5, "dict": 6,
		"set": 7, "tuple": 8, "class": 9, "instance": 10, "method": 11, "closure": 12,
		"exn": 13, "module": 14,
	}
	got := map[string]int{}
	for _, entry := range ValueTagNames() {
		name, num, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("ValueTagNames entry %q is not name=number", entry)
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			t.Fatalf("ValueTagNames entry %q has a non-numeric tag: %v", entry, err)
		}
		got[name] = n
	}
	if len(got) != len(want) {
		t.Fatalf("tag table has %d entries, want %d: %v", len(got), len(want), got)
	}
	for name, n := range want {
		if got[name] != n {
			t.Errorf("tag %q = %d, want %d", name, got[name], n)
		}
	}
	// The schema's definition must describe the same table (it quotes the list).
	var doc struct {
		Definitions struct {
			ValueTag struct {
				Maximum     int    `json:"maximum"`
				Description string `json:"description"`
			} `json:"valueTag"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if doc.Definitions.ValueTag.Maximum != len(want)-1 {
		t.Errorf("schema valueTag maximum = %d, want %d", doc.Definitions.ValueTag.Maximum, len(want)-1)
	}
	for name := range want {
		if !strings.Contains(doc.Definitions.ValueTag.Description, name+"=") {
			t.Errorf("schema valueTag description lost %q", name)
		}
	}
}

func TestABITagsAgreeWithCanonicalTags(t *testing.T) {
	// The ABI's tag words must be the canonical numbers, or an exported `list` would
	// arrive as one thing in-process and another across the C boundary.
	pairs := []struct {
		abi  int
		tag  ValueTag
		name string
	}{
		{ABITagInt, TagInt, "int"}, {ABITagFloat, TagFloat, "float"}, {ABITagBool, TagBool, "bool"},
		{ABITagNone, TagNone, "None"}, {ABITagStr, TagStr, "str"}, {ABITagList, TagList, "list"},
		{ABITagDict, TagDict, "dict"}, {ABITagSet, TagSet, "set"}, {ABITagTuple, TagTuple, "tuple"},
		{ABITagClass, TagClass, "class"}, {ABITagInstance, TagInstance, "instance"},
		{ABITagMethod, TagMethod, "method"}, {ABITagClosure, TagClosure, "closure"},
		{ABITagExn, TagExn, "exn"}, {ABITagModule, TagModule, "module"},
	}
	for _, p := range pairs {
		if p.abi != int(p.tag) {
			t.Errorf("ABI tag for %s is %d, canonical tag is %d", p.name, p.abi, int(p.tag))
		}
	}
}

func TestHeapKindProjectionRoundTrips(t *testing.T) {
	// The compiled heap numbers only what it allocates, so its kind word is a
	// projection — but it must be invertible and total over what it claims.
	for _, tag := range []ValueTag{TagList, TagDict, TagSet, TagInstance} {
		k := HeapKindFor(tag)
		if k == HeapKindNone {
			t.Fatalf("HeapKindFor(%v) says %s does not live on the compiled heap", tag, kindForTag(tag))
		}
		if got := HeapTagFor(k); got != tag {
			t.Errorf("HeapTagFor(%d) = %v, want %v", k, got, tag)
		}
		if name := HeapKindNameOf(k); name != kindForTag(tag) {
			t.Errorf("heap kind %d is named %q, the tag table says %q", k, name, kindForTag(tag))
		}
		if name := HeapKindName(int(k)); name != kindForTag(tag) {
			t.Errorf("heapargs HeapKindName(%d) = %q, want %q", k, name, kindForTag(tag))
		}
	}
	// Scalars and interned strings are not heap kinds: the projection must say so
	// rather than inventing a number.
	for _, tag := range []ValueTag{TagInt, TagFloat, TagBool, TagNone, TagStr, TagClosure, TagExn, TagModule} {
		if HeapKindFor(tag) != HeapKindNone {
			t.Errorf("HeapKindFor(%v) = %d, want HeapKindNone", tag, HeapKindFor(tag))
		}
	}
	if HeapTagFor(HeapKindNone) != TagInt || HeapKindNameOf(HeapKindNone) != "none" {
		t.Errorf("kind 0 must mean not-heap-allocated")
	}
	if HeapTagFor(99) != TagInt {
		t.Errorf("an unknown heap kind must fall back to the same default as the tag table")
	}
	// The aliases in heapargs.go are the same numbers, not copies that can drift.
	if HeapList != HeapKindList || HeapDict != HeapKindDict || HeapSet != HeapKindSet || HeapInst != HeapKindInstance || HeapNone != HeapKindNone {
		t.Errorf("heapargs aliases diverged from the canonical heap kinds")
	}
	if got := strings.Join(HeapKindNames(), " "); got != "list dict set instance" {
		t.Errorf("HeapKindNames() = %q", got)
	}
}

var rtAllocRe = regexp.MustCompile(`call i32 @rt_alloc\(i32 (\d+)\)`)

func TestEmittedHeapKindsComeFromTheTable(t *testing.T) {
	// Magic numbers in codegen are how the two vocabularies would come back: this
	// compiles each container form and asserts the number written into the IR is the
	// table's, and that no unrecognised kind is ever allocated.
	cases := []struct {
		src  string
		want int32
	}{
		{"xs = [1, 2]\nprint(len(xs))\n", HeapKindList},
		{"xs = []\nxs.append(1)\nprint(len(xs))\n", HeapKindList},
		{"d = {'a': 1}\nprint(len(d))\n", HeapKindDict},
		{"s = {1, 2}\nprint(len(s))\n", HeapKindSet},
	}
	seen := map[int32]bool{}
	for _, c := range cases {
		res, err := Compile(c.src)
		if err != nil {
			t.Fatalf("Compile %q: %v", c.src, err)
		}
		found := map[int32]bool{}
		for _, m := range rtAllocRe.FindAllStringSubmatch(res.IR, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("unparsable kind %q", m[1])
			}
			found[int32(n)] = true
			seen[int32(n)] = true
		}
		if !found[c.want] {
			t.Errorf("%q allocated kinds %v, want one of them to be %d (%s)", c.src, keysOf(found), c.want, HeapKindNameOf(c.want))
		}
	}
	// A generator's accumulator is a list too.
	gen, err := Compile("def nums():\n    yield 1\n    yield 2\n\nxs = nums()\nprint(len(xs))\n")
	if err != nil {
		t.Fatalf("Compile generator: %v", err)
	}
	if !strings.Contains(gen.IR, "call i32 @rt_alloc(i32 "+strconv.Itoa(HeapKindList)+")") {
		t.Errorf("the generator accumulator is not allocated as a list")
	}
	for k := range seen {
		if !HeapKindKnown(k) {
			t.Errorf("codegen allocated an unknown heap kind %d", k)
		}
	}
}

func TestInstanceKindsInRuntimeAndCodegenAgree(t *testing.T) {
	// The runtime block allocates instances with a literal kind (it is IR text), and
	// codegen does the same with the named constant: they must be the same number, or
	// rt_inst_get would look at an object the collector thinks is a list.
	src := "class P:\n    def __init__(self, x):\n        self.x = x\n\np = P(3)\nprint(p.x)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := "call i32 @rt_alloc(i32 " + strconv.Itoa(HeapKindInstance) + ")"
	if !strings.Contains(res.IR, want) {
		t.Fatalf("no instance allocation %q in the module", want)
	}
	for _, m := range rtAllocRe.FindAllStringSubmatch(res.IR, -1) {
		n, _ := strconv.Atoi(m[1])
		if !HeapKindKnown(int32(n)) {
			t.Errorf("module allocates unknown heap kind %d", n)
		}
	}
	if kind := HeapKindFor(TagInstance); kind != HeapKindInstance {
		t.Errorf("TagInstance projects to %d, want %d", kind, HeapKindInstance)
	}
}

func keysOf(m map[int32]bool) []int32 {
	out := make([]int32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
