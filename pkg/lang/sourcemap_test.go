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

	smBytes, err := GenerateSourceMap(prog, ir)
	if err != nil {
		t.Fatalf("source map: %v", err)
	}
	var sm SourceMap
	if err := json.Unmarshal(smBytes, &sm); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, smBytes)
	}
	if sm.Version != 1 {
		t.Fatalf("version: got %d", sm.Version)
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
}
