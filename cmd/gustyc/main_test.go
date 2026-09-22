package main

import (
	"encoding/json"
	"os"
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

func TestCLIEmitLLVMScalarMinMax(t *testing.T) {
	// min/max accept a single scalar value (treated as a one-element
	// collection); the emitted IR must contain the scalar itself.
	for _, tc := range []struct{ src, want string }{
		{`print(min(5))`, "i32 5"},
		{`print(max(7))`, "i32 7"},
	} {
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

// TestCLIJIT verifies the in-process dlopen JIT path end-to-end through the
// CLI: `--jit --eval` compiles, links, dlopen's, and runs the generated
// native `main`, returning the same stdout as the AST interpreter.
func TestCLIJIT(t *testing.T) {
	got := cli(t, "--jit", "--eval", "x = 6\nprint(x * 7)\n")
	if got != "42\n" {
		t.Fatalf("jit eval output = %q, want 42", got)
	}
}

// TestCLIJITJSON verifies the JSON output mode captures the JIT stdout.
func TestCLIJITJSON(t *testing.T) {
	got := cli(t, "--jit", "--json", "--eval", "print(2 + 3)\n")
	if got != `{"output": "5\n", "exit": 0}`+"\n" {
		t.Fatalf("jit json output = %q", got)
	}
}

// cliExit runs the CLI and returns stdout plus the exit code (0/1/2) without
// failing on a non-zero exit, which check mode uses for its exit contract.
func cliExit(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "-tags=llvm20", "./cmd/gustyc"}, args...)...)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	ec := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			ec = ee.ExitCode()
		} else {
			t.Fatalf("cliExit: %v", err)
		}
	}
	return string(out), ec
}

func TestCheckModeExitCodes(t *testing.T) {
	// clean source -> exit 0, "ok" printed
	_, ec := cliExit(t, "--check", "def add(a: int, b: int) -> int:\n    return a + b\nx = add(1, 2)\n")
	if ec != 0 {
		t.Fatalf("clean check exit=%d, want 0", ec)
	}
	// type error -> exit 1
	_, ec = cliExit(t, "--check", "def f() -> int:\n    return \"bad\"\n")
	if ec != 1 {
		t.Fatalf("error check exit=%d, want 1", ec)
	}
	// parse error -> exit 2 from the real binary; `go run` maps it to 1,
	// so assert any non-zero error exit here.
	_, ec = cliExit(t, "--check", "def f(:")
	if ec == 0 {
		t.Fatalf("parse-error check exit=%d, want non-zero", ec)
	}
}

func TestCheckModeJSON(t *testing.T) {
	out, ec := cliExit(t, "--json", "--check", "def f() -> int:\n    return \"bad\"\n")
	if ec != 1 {
		t.Fatalf("json check exit=%d, want 1", ec)
	}
	var res struct {
		Files       []string `json:"files"`
		Diagnostics []struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		} `json:"diagnostics"`
		OK   bool `json:"ok"`
		Exit int  `json:"exit"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal check json: %v\nout=%s", err, out)
	}
	if res.OK || res.Exit != 1 {
		t.Fatalf("expected ok=false exit=1, got ok=%v exit=%d", res.OK, res.Exit)
	}
}

func TestCheckFilesSubcommand(t *testing.T) {
	dir := t.TempDir()
	bad := dir + "/bad.gy"
	if err := os.WriteFile(bad, []byte("def greet(name: str) -> str:\n    return name + \"!\"\ny = greet(42)\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, ec := cliExit(t, "--json", "check", bad)
	if ec != 1 {
		t.Fatalf("check files exit=%d, want 1", ec)
	}
	if !strings.Contains(out, "argument") {
		t.Fatalf("expected argument-mismatch diagnostic, out=%s", out)
	}
}
