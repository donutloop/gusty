package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// cli runs `go run -tags=llvm20 ./cmd/gustyc` with args and returns stdout.
func cli(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", "run", "-tags=llvm20", "./cmd/gustyc")
	cmd.Args = append([]string{"go", "run", "-tags=llvm20", "./cmd/gustyc"}, args...)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gustyc %v: %v", args, err)
	}
	return string(out)
}

func TestCLIVersion(t *testing.T) {
	if got := cli(t, "--version"); !strings.Contains(got, "gustyc") {
		t.Fatalf("version output = %q", got)
	}
}

func TestCLIEval(t *testing.T) {
	got := cli(t, "--eval", "x = 2 + 3\nx")
	if got != "5\n" {
		t.Fatalf("eval got %q, want 5", got)
	}
}

func TestCLIEvalString(t *testing.T) {
	got := cli(t, "--eval", "s = \"hi\"\ns")
	if got != "hi\n" {
		t.Fatalf("string eval got %q, want hi", got)
	}
}

func TestCLILang(t *testing.T) {
	if got := cli(t, "--lang"); !strings.Contains(got, "statements:") {
		t.Fatalf("--lang output = %q", got)
	}
}

func TestCLIVerify(t *testing.T) {
	got := cli(t, "--verify", "def f(x):\n    return x * 2")
	if got != "ok\n" {
		t.Fatalf("verify got %q, want ok", got)
	}
}

func TestCLIJSON(t *testing.T) {
	got := cli(t, "--json", "--eval", "x = 1 + 2\nx")
	if !strings.Contains(got, `"result": "3"`) {
		t.Fatalf("json eval got %q", got)
	}
}

func TestCLISchema(t *testing.T) {
	got := cli(t, "--schema")
	// The schema must be valid, self-describing JSON (draft-07).
	var v map[string]any
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatalf("--schema output is not valid JSON: %v", err)
	}
	if v["$schema"] != "http://json-schema.org/draft-07/schema#" {
		t.Fatalf("schema $schema = %v", v["$schema"])
	}
	if _, ok := v["definitions"]; !ok {
		t.Fatalf("schema has no definitions")
	}
}

func TestCLIEmitLLVMOptLevel(t *testing.T) {
	// --opt-level must come before --emit-llvm (emit-llvm is a string flag
	// that consumes the next argument as its value).
	ir0 := cli(t, "--opt-level=0", "--emit-llvm=\"hi\"")
	ir1 := cli(t, "--opt-level=1", "--emit-llvm=\"hi\"")
	// Level 1 runs the optimization pipeline: the dead string global @.strN
	// (the literal's value is unused by the body) must be pruned.
	if strings.Contains(ir1, "@.str") {
		t.Fatalf("opt-level 1 failed to prune dead string global:\n%s", ir1)
	}
	if !strings.Contains(ir0, "define i32 @main()") {
		t.Fatalf("emit-llvm produced no main():\n%s", ir0)
	}
}


func TestCLIEmitLLVMIndexCallElems(t *testing.T) {
	// Element access into list-producing call expressions in the AOT codegen:
	// keys(), values(), sorted(...), reversed(...), split(...). The emitted IR
	// must contain the exact element constant selected by the literal index.
	cases := []struct{ src, want string }{
		{`print({1: 10, 2: 20}.keys()[0])`, "i32 1"},
		{`print({1: 10, 2: 20}.values()[1])`, "i32 20"},
		{`print(sorted([3, 1, 2])[1])`, "i32 2"},
		{`print(sorted([3, 1, 2], reverse=True)[0])`, "i32 3"},
		{`print(reversed([1, 2, 3])[0])`, "i32 3"},
		{`print("a b c".split()[2])`, "@.str"},
	}
	for _, tc := range cases {
		out := cli(t, "--emit-llvm", tc.src)
		if !strings.Contains(out, tc.want) {
			t.Fatalf("emit-llvm %q: missing %q in IR:\n%s", tc.src, tc.want, out)
		}
	}
}

func TestCLIJSONType(t *testing.T) {
	// --json --eval emits a structured result with a dynamic type field.
	out := cli(t, "--json", "--eval=x = 42\nx")
	var doc struct {
		Result string `json:"result"`
		Type   string `json:"type"`
		Exit   int    `json:"exit"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json parse: %v\n%s", err, out)
	}
	if doc.Type != "int" {
		t.Fatalf("type = %q, want int", doc.Type)
	}
	if doc.Exit != 0 {
		t.Fatalf("exit = %d, want 0", doc.Exit)
	}
}
