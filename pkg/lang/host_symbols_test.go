package lang

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// An emitted function name is a link name. A program that defined a function called `sync`
// was emitted as `define i32 @sync()`, and the linker answered its own call from libc: the
// interpreter printed the program's 7, the compiled binary printed the C library's 0, and
// nothing complained (roadmap Gap R.4, ADR 0198). Program-owned names are therefore emitted
// under irSymbolPrefix; names the program does not define — the runtime helpers, the C
// library, and what `extern fn` binds — keep theirs.

func mustIRForSymbols(t *testing.T, src string) string {
	t.Helper()
	ir, err := GenerateIR(parseOrFatal(t, src))
	if err != nil {
		t.Fatalf("GenerateIR: %v", err)
	}
	return ir
}

func TestUserFunctionIsEmittedUnderTheLinkPrefix(t *testing.T) {
	ir := mustIRForSymbols(t, "def sync():\n    return 7\n\nprint(sync())\n")
	if !strings.Contains(ir, "define i32 @gy_sync()") {
		t.Errorf("the program's own function must be defined under the prefix:\n%s", ir)
	}
	if !strings.Contains(ir, "call i32 @gy_sync()") {
		t.Errorf("the call must go to the prefixed symbol:\n%s", ir)
	}
	// The un-prefixed name must not be defined or called by the program at all: that is
	// precisely the symbol libc owns, and the defect was a silent agreement with it.
	for _, line := range strings.Split(ir, "\n") {
		text := strings.TrimSpace(line)
		if strings.HasPrefix(text, "define") && strings.Contains(text, "@sync(") {
			t.Errorf("the module defines the bare host name: %s", text)
		}
		if strings.Contains(text, "call") && strings.Contains(text, "@sync(") {
			t.Errorf("a call still binds the bare host name: %s", text)
		}
	}
}

// TestHostAbiNameBattery is the measured set: every one of these answers `8` on the
// interpreter and answered something else — 0, 1, or a duplicate-definition failure — on
// the compiled path before the prefix existed.
func TestHostAbiNameBattery(t *testing.T) {
	// Names the host ABI owns that are NOT also built-in call names of the language.
	// `abs`, `floor`, `min`, `sum` and friends are a different defect, recorded as
	// roadmap R.6: a call to a name the builtin table knows is resolved against the
	// builtin before the program's own `def` of that name is consulted, so the function
	// is never emitted at all. Fixing that means ordering the two tables the way the
	// interpreter and CPython order them, which is not this gap.
	names := []string{
		"sync", "printf", "exit", "strlen", "free", "malloc", "write", "read",
		"open", "time", "rand", "system", "abort", "main", "cmp", "init",
	}
	for _, name := range names {
		src := "def f(x):\n    return " + name + "(x)\n\ndef " + name + "(x):\n    return x + 7\n\nprint(f(1))\n"
		ir := mustIRForSymbols(t, src)
		if !strings.Contains(ir, "define i32 @gy_"+name+"(i32 %p0)") {
			t.Errorf("%s: not defined under the prefix:\n%s", name, ir)
		}
		if !strings.Contains(ir, "call i32 @gy_"+name+"(") {
			t.Errorf("%s: the call does not bind the prefixed symbol:\n%s", name, ir)
		}
	}
}

// TestExternKeepsItsCName: an `extern fn` is an FFI surface — its link name is the C name
// the program asked to bind, and prefixing it would break the very call it declares.
func TestExternKeepsItsCName(t *testing.T) {
	ir := mustIRForSymbols(t, "extern fn abs(x: int) -> int\n\nprint(abs(-5))\n")
	if !strings.Contains(ir, "declare i32 @abs(i32)") {
		t.Errorf("the extern declaration lost its C name:\n%s", ir)
	}
	if !strings.Contains(ir, "call i32 @abs(") {
		t.Errorf("the extern call must bind the C name:\n%s", ir)
	}
	if strings.Contains(ir, "@gy_abs") {
		t.Errorf("an extern must not be prefixed:\n%s", ir)
	}
}

// TestGeneratedEntryPointIsNotPrefixed: `main` is generated, not user-owned, and the
// runtime helpers are named by the compiler. The user's `def main` lives beside it.
func TestGeneratedEntryPointIsNotPrefixed(t *testing.T) {
	ir := mustIRForSymbols(t, "def main(x):\n    return x + 7\n\nprint(main(0))\n")
	if !strings.Contains(ir, "define i32 @main()") {
		t.Errorf("the generated entry point must stay @main:\n%s", ir)
	}
	if !strings.Contains(ir, "define i32 @gy_main(i32 %p0)") {
		t.Errorf("the program's own main must live beside it as @gy_main:\n%s", ir)
	}
	// Before the prefix these two were the same symbol, which is why a program with
	// `def main` failed to build at all rather than answering wrongly.
	if strings.Contains(ir, "define i32 @main(i32") {
		t.Errorf("the program's main collided with the entry point:\n%s", ir)
	}
	// The runtime's own helpers keep the names the compiler gives them.
	if !strings.Contains(ir, "@rt_frame_open(") {
		t.Errorf("runtime helpers must keep their own names:\n%s", ir)
	}
}

func TestMethodClosureAndDecoratorSymbolsArePrefixed(t *testing.T) {
	t.Run("method", func(t *testing.T) {
		ir := mustIRForSymbols(t, "class Point:\n    def x(self):\n        return 3\n\nprint(Point().x())\n")
		if !strings.Contains(ir, "define i32 @gy_Point_x(") {
			t.Errorf("a method symbol must carry the prefix:\n%s", ir)
		}
		if !strings.Contains(ir, "call i32 @gy_Point_x(") {
			t.Errorf("a method call must bind the prefixed symbol:\n%s", ir)
		}
	})
	t.Run("decorated body", func(t *testing.T) {
		ir := mustIRForSymbols(t, "def twice(f):\n    return f\n\n@twice\ndef g():\n    return 42\n\nprint(g())\n")
		if !strings.Contains(ir, "@gy_g_impl") {
			t.Errorf("the decorated body and its references must agree on one prefixed symbol:\n%s", ir)
		}
		if !strings.Contains(ir, "@g_ptr = internal global i32()* @gy_g_impl") {
			t.Errorf("the function-pointer global must point at the prefixed body:\n%s", ir)
		}
	})
	t.Run("lambda", func(t *testing.T) {
		ir := mustIRForSymbols(t, "f = lambda x: int: x * 2\n\nprint(f(3))\n")
		if !strings.Contains(ir, "@gy_lambda_") {
			t.Errorf("a generated lambda is program-owned too:\n%s", ir)
		}
	})
	t.Run("imported module function", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/lib.gy", []byte("def f(x):\n    return x * 2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		old, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(old)
		res, err := Compile("import lib\nprint(lib.f(21))")
		if err != nil {
			t.Fatalf("AOT import should compile: %v", err)
		}
		// The mangle already makes the name unique; the prefix keeps it out of the host
		// namespace too, and both halves must agree.
		if !strings.Contains(res.IR, "define i32 @gy_lib$f(") {
			t.Errorf("a lowered module function must be defined under the prefix:\n%s", res.IR)
		}
		if !strings.Contains(res.IR, "call i32 @gy_lib$f(") {
			t.Errorf("the cross-module call must bind it:\n%s", res.IR)
		}
	})
}

// TestSourceMapNamesTheSourceAndTheLinkSeparately: the map exists to point a tool at a
// function, so `name` is what the program wrote and `symbol` is what the linker sees.
func TestSourceMapNamesTheSourceAndTheLinkSeparately(t *testing.T) {
	src := "def sync(x):\n    return x + 1\n\nprint(sync(1))\n"
	ir := mustIRForSymbols(t, src)
	raw, err := GenerateSourceMap(parseOrFatal(t, src), ir)
	if err != nil {
		t.Fatalf("GenerateSourceMap: %v", err)
	}
	sm := SourceMap{}
	if err := json.Unmarshal(raw, &sm); err != nil {
		t.Fatalf("bad source map: %v\n%s", err, raw)
	}
	if len(sm.Functions) != 1 {
		t.Fatalf("want one entry, got %s", raw)
	}
	e := sm.Functions[0]
	if e.Name != "sync" {
		t.Errorf("name = %q, want the source name %q", e.Name, "sync")
	}
	if e.Symbol != "gy_sync" {
		t.Errorf("symbol = %q, want the link name %q", e.Symbol, "gy_sync")
	}
	if e.IRLine == 0 {
		t.Errorf("the entry is not mapped to a line of the emitted module:\n%s", ir)
	}
}
