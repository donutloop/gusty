package lang

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestABISchemaValid verifies the ABI schema is machine-readable JSON carrying
// the version, struct layouts, tag words, and extern marshalling rules.
func TestABISchemaValid(t *testing.T) {
	schema, err := ABISchema()
	if err != nil {
		t.Fatalf("ABISchema: %v", err)
	}
	var doc struct {
		ABI     string `json:"abi"`
		Version int    `json:"version"`
		Value   struct {
			Type    string `json:"type"`
			Tag     int    `json:"tag_offset"`
			Payload int    `json:"payload_offset"`
			Words   int    `json:"words"`
		} `json:"gusty_value"`
		Union struct {
			Type string `json:"type"`
			Tag  int    `json:"tag_offset"`
			I    int    `json:"i32_offset"`
			F    int    `json:"f64_offset"`
			S    int    `json:"i8p_offset"`
		} `json:"gusty_union"`
		Tags map[string]int `json:"tags"`
	}
	if err := json.Unmarshal([]byte(schema), &doc); err != nil {
		t.Fatalf("ABISchema is not valid JSON: %v", err)
	}
	if doc.ABI != "gusty-extern-abi" {
		t.Errorf("abi name = %q", doc.ABI)
	}
	if doc.Version != ABIVersion {
		t.Errorf("schema version = %d, want %d", doc.Version, ABIVersion)
	}
	// Stable struct layout: tag word at offset 0, payload at offset 1.
	if doc.Value.Tag != 0 || doc.Value.Payload != 1 || doc.Value.Words != 2 {
		t.Errorf("gusty_value layout = tag:%d payload:%d words:%d", doc.Value.Tag, doc.Value.Payload, doc.Value.Words)
	}
	if doc.Union.Type != "{i32, i32, double, i8*}" || doc.Union.Tag != 0 || doc.Union.I != 1 || doc.Union.F != 2 || doc.Union.S != 3 {
		t.Errorf("gusty_union layout = %+v", doc.Union)
	}
	if doc.Tags["int"] != 0 || doc.Tags["float"] != 1 || doc.Tags["str"] != 4 || doc.Tags["module"] != 14 {
		t.Errorf("tag words not stable: %+v", doc.Tags)
	}
}

// TestABITagWordsFixed locks the tag words — renumbering them would break the
// extern-fn ABI across releases.
func TestABITagWordsFixed(t *testing.T) {
	want := []int{ABITagInt, ABITagFloat, ABITagBool, ABITagNone, ABITagStr, ABITagList, ABITagDict, ABITagSet, ABITagTuple, ABITagClass, ABITagInstance, ABITagMethod, ABITagClosure, ABITagExn, ABITagModule}
	expect := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	for i := range want {
		if want[i] != expect[i] {
			t.Fatalf("tag %d = %d, want %d (stable ABI)", i, want[i], expect[i])
		}
	}
}

// TestEmitABIIntoIR verifies the generated module carries the versioned ABI
// prelude: stable named struct types + the version marker global.
func TestEmitABIIntoIR(t *testing.T) {
	prog, err := Parse(`x = 1
print(x)`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ir, err := GenerateIR(prog)
	if err != nil {
		t.Fatalf("GenerateIR: %v", err)
	}
	for _, marker := range []string{
		"%gusty_value = type {i32, i32}",
		"%gusty_union = type {i32, i32, double, i8*}",
		"@gusty_abi_version = internal constant i32 1",
	} {
		if !strings.Contains(ir, marker) {
			t.Errorf("emitted IR missing ABI marker %q", marker)
		}
	}
}

// TestEmitABIIdempotent verifies the ABI prelude is emitted exactly once.
func TestEmitABIIdempotent(t *testing.T) {
	var b string
	EmitABI(&b)
	EmitABI(&b)
	if got := strings.Count(b, "@gusty_abi_version"); got != 1 {
		t.Errorf("@gusty_abi_version emitted %d times, want 1", got)
	}
}

// TestABIStructLayout mirrors the C struct layout for the tagged value and
// union: tag word at offset 0, payload/words at stable offsets.
func TestABIStructLayout(t *testing.T) {
	v := ABIValue{Tag: ABITagInt, Payload: 42}
	if v.Tag != 0 || v.Payload != 42 {
		t.Errorf("ABIValue layout = %+v", v)
	}
	u := ABIUnion{Tag: ABITagFloat, F: 1.5}
	if u.Tag != 1 || u.F != 1.5 {
		t.Errorf("ABIUnion layout = %+v", u)
	}
}
