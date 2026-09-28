package lang

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestSchemaIsValidJSON confirms the AST/IR schema document is itself valid
// JSON (draft-07), so agents can parse it deterministically.
func TestSchemaIsValidJSON(t *testing.T) {
	var v any
	if err := json.Unmarshal([]byte(ASTIRSchema), &v); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("schema root is not an object")
	}
	if obj["$schema"] != "http://json-schema.org/draft-07/schema#" {
		t.Fatalf("unexpected $schema: %v", obj["$schema"])
	}
	if _, ok := obj["definitions"]; !ok {
		t.Fatalf("schema has no definitions")
	}
}

// TestSchemaDescribesASTDump parses a real program, emits its AST JSON exactly
// as --emit-ast would, and structurally checks the first statement against the
// schema's stmt oneOf required-field shapes. This is a smoke contract test: an
// agent validating a dump should be able to identify the node kind.
func TestSchemaDescribesASTDump(t *testing.T) {
	prog, err := Parse("x = 1\ny = x + 2\nprint(y)\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dump, err := json.Marshal(prog)
	if err != nil {
		t.Fatalf("marshal AST: %v", err)
	}
	var doc struct {
		Stmts []map[string]any `json:"stmts"`
	}
	if err := json.Unmarshal(dump, &doc); err != nil {
		t.Fatalf("unmarshal AST dump: %v", err)
	}
	if len(doc.Stmts) < 3 {
		t.Fatalf("expected >=3 statements, got %d", len(doc.Stmts))
	}
	// First statement is an assignment: it must carry target+value per schema.
	first := doc.Stmts[0]
	if _, hasTarget := first["target"]; !hasTarget {
		t.Fatalf("first stmt lacks target field required by assignStmt schema")
	}
	if _, hasValue := first["value"]; !hasValue {
		t.Fatalf("first stmt lacks value field required by assignStmt schema")
	}
	// Third statement is an ExprStmt wrapping a call to print: the call node
	// must carry fn+args per the schema's call definition.
	third := doc.Stmts[2]
	expr, ok := third["expr"].(map[string]any)
	if !ok {
		t.Fatalf("third stmt lacks expr wrapping the call")
	}
	if _, hasFn := expr["fn"]; !hasFn {
		t.Fatalf("call node lacks fn field required by call schema")
	}
	if _, hasArgs := expr["args"]; !hasArgs {
		t.Fatalf("call node lacks args field required by call schema")
	}
}

// TestSchemaHasIRDumpDefinition confirms the IR dump (--emit-llvm) is
// documented as text/plain in the same schema document.
func TestSchemaHasIRDumpDefinition(t *testing.T) {
	var v struct {
		Definitions map[string]struct {
			ContentMediaType string `json:"contentMediaType"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(ASTIRSchema), &v); err != nil {
		t.Fatalf("schema JSON: %v", err)
	}
	if d, ok := v.Definitions["irDump"]; !ok || d.ContentMediaType != "text/plain" {
		t.Fatalf("irDump definition missing or wrong media type: %+v", d)
	}
}

// TestSchemaDeclaresGCStats keeps the collector's machine path honest: every
// field GCStats marshals must be declared in definitions.gcStats, so an agent can
// validate a --gc-stats payload against the schema instead of reading Go structs.
func TestSchemaDeclaresGCStats(t *testing.T) {
	var doc struct {
		Definitions struct {
			GCStats struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			} `json:"gcStats"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	props := doc.Definitions.GCStats.Properties
	if len(props) == 0 {
		t.Fatal("schema declares no gcStats properties")
	}
	want := reflect.TypeOf(GCStats{})
	for i := 0; i < want.NumField(); i++ {
		name := strings.Split(want.Field(i).Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if _, ok := props[name]; !ok {
			t.Fatalf("GCStats reports %q but the schema does not declare it", name)
		}
	}
	for _, req := range doc.Definitions.GCStats.Required {
		if _, ok := props[req]; !ok {
			t.Fatalf("gcStats requires %q which is not a declared property", req)
		}
	}
}

// TestGCStatsShapeInJSON: the --json payload member is produced by the CLI, so the
// CLI test covers it; here assert the report survives marshalling with stable keys.
func TestGCStatsMarshalKeysAreStable(t *testing.T) {
	st := GCStats{Collections: 1, Roots: 2, Skipped: 3, Marked: 4, Freed: 5, TotalFreed: 6, Live: 7, Frames: 1, Protected: 2, Generational: true, Backend: "interpreter"}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"collections":1`, `"roots":2`, `"skipped":3`, `"marked":4`, `"freed":5`, `"total_freed":6`, `"live":7`, `"frames":1`, `"protected":2`, `"generational":true`, `"backend":"interpreter"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("gc stats JSON %s missing %s", b, key)
		}
	}
}
