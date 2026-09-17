package lang

import (
	"encoding/json"
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
