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

// TestSchemaDeclaresEffectSummary keeps the L7.6 machine path honest: every field
// EffectSummary marshals must be declared in definitions.effectSummary, so an agent
// can validate a `gustyc --effects` document against the schema instead of reading
// Go structs (roadmap Phase 7, ADR 0195).
func TestSchemaDeclaresEffectSummary(t *testing.T) {
	var doc struct {
		Definitions struct {
			EffectSummary struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			} `json:"effectSummary"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	props := doc.Definitions.EffectSummary.Properties
	if len(props) == 0 {
		t.Fatal("schema declares no effectSummary properties")
	}
	want := reflect.TypeOf(EffectSummary{})
	for i := 0; i < want.NumField(); i++ {
		name := strings.Split(want.Field(i).Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if _, ok := props[name]; !ok {
			t.Fatalf("EffectSummary reports %q but the schema does not declare it", name)
		}
	}
	for _, req := range doc.Definitions.EffectSummary.Required {
		if _, ok := props[req]; !ok {
			t.Fatalf("effectSummary requires %q which is not a declared property", req)
		}
	}
	// The effect vocabulary is a closed set in both places, or an agent branching
	// on a name the emitter can produce would be guessing.
	eff, ok := props["effects"].(map[string]any)
	if !ok {
		t.Fatalf("effects property missing")
	}
	items, _ := eff["items"].(map[string]any)
	enum, _ := items["enum"].([]any)
	got := map[string]bool{}
	for _, e := range enum {
		got[e.(string)] = true
	}
	for _, name := range []string{EffectAwait, EffectYield, EffectRaise} {
		if !got[name] {
			t.Fatalf("schema effects enum lost %q", name)
		}
	}
}

// TestSchemaDeclaresEffectsDocument is the document half of the same contract: every
// field EffectsDocument marshals must be declared in definitions.effectDocument, and
// the two $refs it makes must resolve — an agent validating a `gustyc --effects
// --json` payload against `gustyc --schema` should not be the one to find the hole
// (roadmap Phase 7, ADR 0195).
func TestSchemaDeclaresEffectsDocument(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(ASTIRSchema), &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	defs, _ := schema["definitions"].(map[string]any)
	def, _ := defs["effectDocument"].(map[string]any)
	props, _ := def["properties"].(map[string]any)
	if len(props) == 0 {
		t.Fatal("schema declares no effectDocument properties")
	}
	want := reflect.TypeOf(EffectsDocument{})
	for i := 0; i < want.NumField(); i++ {
		name := strings.Split(want.Field(i).Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if _, ok := props[name]; !ok {
			t.Fatalf("EffectsDocument reports %q but the schema does not declare it", name)
		}
	}
	// The document's two references have to exist, or the shape is a promise the
	// schema cannot keep.
	for _, ref := range []string{"#/definitions/effectSummary", "#/definitions/diagnostic"} {
		b, _ := json.Marshal(def)
		if !strings.Contains(string(b), ref) {
			t.Fatalf("effectDocument does not reference %s", ref)
		}
	}
	for _, name := range []string{"effectSummary", "diagnostic"} {
		if _, ok := defs[name]; !ok {
			t.Fatalf("effectDocument references %s, which the schema does not define", name)
		}
	}
	// And the emitter agrees with the declaration on the members that carry the
	// verdict — an agent branches on ok/exit, not on prose.
	doc, err := EffectsJSON(parseOrFatal(t, "async def f(x):\n    return x\nprint(await f(1))\n"), "t.gy", []Diagnostic{{Level: LevelError, Msg: "x", Code: CodeCoroNeverAwaited}})
	if err != nil {
		t.Fatalf("EffectsJSON: %v", err)
	}
	for _, key := range []string{`"ok": false`, `"exit": 1`, `"code": "async.coro.never_awaited"`} {
		if !strings.Contains(doc, key) {
			t.Fatalf("effects document is missing %s:\n%s", key, doc)
		}
	}
}

// TestGCStatsShapeInJSON: the --json payload member is produced by the CLI, so the
// CLI test covers it; here assert the report survives marshalling with stable keys.
func TestGCStatsMarshalKeysAreStable(t *testing.T) {
	st := GCStats{Collections: 1, Roots: 2, Skipped: 3, Marked: 4, Freed: 5, TotalFreed: 6, Live: 7, Frames: 1, Protected: 2, Top: 9, Generational: true, Backend: "interpreter"}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"collections":1`, `"roots":2`, `"skipped":3`, `"marked":4`, `"freed":5`, `"total_freed":6`, `"live":7`, `"frames":1`, `"protected":2`, `"top":9`, `"generational":true`, `"backend":"interpreter"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("gc stats JSON %s missing %s", b, key)
		}
	}
}

// TestGCStatsLineRoundTrip pins the one documented shape shared by the two backends:
// the interpreter renders GCStats with String, the compiled runtime prints the same
// fields with rt_gc_report's printf format, and ParseGCStatsLine reads either back.
// The CLI turns the compiled program's line into JSON this way, so the two halves must
// agree byte-for-byte on the field names.
func TestGCStatsLineRoundTrip(t *testing.T) {
	for _, st := range []GCStats{
		{Collections: 3, Roots: 4, Skipped: 5, Marked: 6, Freed: 7, TotalFreed: 9, Live: 2, Frames: 1, Protected: 3, Generational: true, Backend: "interpreter"},
		{Collections: 102, Roots: 3, Skipped: 0, Marked: 51, Freed: 0, TotalFreed: 4243, Live: 51, Top: 17, Backend: "aot"},
	} {
		got, ok := ParseGCStatsLine(st.String())
		if !ok {
			t.Fatalf("report line %q does not parse", st.String())
		}
		if got != st {
			t.Fatalf("round trip changed the stats:\n in:  %+v\nout: %+v\nline: %s", st, got, st.String())
		}
	}
	// The interpreter's line must not grow a top= field: existing assertions and user
	// scripts read it.
	if strings.Contains(GCStats{Backend: "interpreter"}.String(), "top=") {
		t.Errorf("interpreter report gained a top= field: %s", GCStats{Backend: "interpreter"}.String())
	}
	for _, bad := range []string{"", "gc:", "gc: backend", "hello world", "gc: backend=aot nope"} {
		if _, ok := ParseGCStatsLine(bad); ok {
			t.Errorf("%q should not parse as a collector report", bad)
		}
	}
}
