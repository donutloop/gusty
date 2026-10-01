package lang

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateSourceMap(t *testing.T) {
	src := `def f():
    return 1
class C:
    def m(self):
        return 2
def g():
    return 3
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	Analyze(prog)
	ir, err := GenerateIR(prog)
	if err != nil {
		t.Fatalf("codegen: %v", err)
	}
	if !strings.Contains(ir, "define") {
		t.Fatalf("IR missing function defs:\n%s", ir)
	}

	smBytes, err := GenerateSourceMap(prog, ir, nil)
	if err != nil {
		t.Fatalf("source map: %v", err)
	}
	var sm SourceMap
	if err := json.Unmarshal(smBytes, &sm); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, smBytes)
	}
	// Version 2 is the line table (L8.5, ADR 0231): `functions` alone was version 1.
	if sm.Version != 2 {
		t.Fatalf("version: got %d, want 2", sm.Version)
	}
	if sm.Lines != nil {
		t.Fatalf("a map built without debug info must not claim a line table: %v", sm.Lines)
	}

	want := map[string]int{"f": 0, "m": 0, "g": 0}
	for _, e := range sm.Functions {
		want[e.Name] = e.IRLine
	}
	for name, irLine := range want {
		if irLine == 0 {
			t.Errorf("function %q not mapped to an IR line", name)
		}
	}
	if len(sm.Functions) != 3 {
		t.Errorf("expected 3 functions, got %d: %s", len(sm.Functions), smBytes)
	}
}

func TestEmitSourceMap(t *testing.T) {
	src := "def twice(x):\n    return x + x\ntwice(4)"
	sm, err := EmitSourceMap(src)
	if err != nil {
		t.Fatalf("emit source map: %v", err)
	}
	var smp SourceMap
	if err := json.Unmarshal(sm, &smp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if len(smp.Functions) != 1 || smp.Functions[0].Name != "twice" {
		t.Fatalf("expected one twice entry: %s", sm)
	}
	if smp.Functions[0].IRLine <= 0 {
		t.Fatalf("twice missing IR line: %s", sm)
	}
	// The line table is the point of version 2: every row points at a source line the
	// program actually has, and at an IR line the map also describes.
	if len(smp.Lines) == 0 {
		t.Fatalf("source map carries no line table: %s", sm)
	}
	if smp.Debug == nil {
		t.Fatalf("source map names no compile unit: %s", sm)
	}
	for _, r := range smp.Lines {
		if r.Line < 1 || r.Line > 3 {
			t.Fatalf("row %v points outside the 3-line program", r)
		}
		if r.IRLine <= 0 {
			t.Fatalf("row %v has no IR line", r)
		}
	}
	if smp.Debug.Tagged == 0 || smp.Debug.Lines != nil {
		t.Fatalf("debug summary wrong (rows belong in `lines`, not duplicated in `debug`): %+v", smp.Debug)
	}
}

// TestSourceMapLineTableMatchesTheModule is the honesty check on `lines`: a row's IR
// line really carries the !dbg record the row claims, in the module the same map
// describes — so a table nobody could verify against the artifact cannot ship.
func TestSourceMapLineTableMatchesTheModule(t *testing.T) {
	src := "def twice(x):\n    y = x + x\n    return y\ntwice(4)\nprint(1)"
	sm, err := EmitSourceMap(src)
	if err != nil {
		t.Fatalf("emit source map: %v", err)
	}
	var smp SourceMap
	if err := json.Unmarshal(sm, &smp); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	prog, perr := Parse(src)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	Analyze(prog)
	// The rows come from a debug build, so that is the module they must read against;
	// numbering is identical in a plain one, which is what lets this map serve both.
	ir, genErr := GenerateIRWithOptions(prog, &IRGenOptions{Debug: &DebugOptions{File: "rows.gy"}})
	if genErr != nil {
		t.Fatalf("codegen: %v", genErr)
	}
	irLines := strings.Split(ir, "\n")
	for _, r := range smp.Lines {
		if r.IRLine > len(irLines) {
			t.Fatalf("row %v names IR line %d of a %d-line module", r, r.IRLine, len(irLines))
		}
		ln := irLines[r.IRLine-1]
		if !strings.Contains(ln, ", !dbg !") && !strings.HasPrefix(strings.TrimSpace(ln), "define ") {
			t.Errorf("IR line %d (%q) carries no !dbg record, but the map says it is source line %d", r.IRLine, ln, r.Line)
		}
	}
}
