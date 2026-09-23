package lang

import (
	"strings"
	"testing"
)

// TestResolveImportPathStdlib verifies that on-disk stdlib modules resolve
// from the discovered stdlib root, and that unknown modules return "".
func TestResolveImportPathStdlib(t *testing.T) {
	for _, mod := range []string{"math", "string", "collections", "json"} {
		path := ResolveImportPath(mod)
		if path == "" {
			t.Fatalf("ResolveImportPath(%q) = \"\", want a stdlib path", mod)
		}
		if !strings.HasSuffix(path, "/"+mod+".gy") {
			t.Fatalf("ResolveImportPath(%q) = %q, want .../%s.gy", mod, path, mod)
		}
		if !strings.Contains(path, "stdlib") {
			t.Fatalf("ResolveImportPath(%q) = %q, want a path under the stdlib root", mod, path)
		}
	}
	if p := ResolveImportPath("definitely_not_a_module"); p != "" {
		t.Fatalf("ResolveImportPath(unknown) = %q, want \"\"", p)
	}
}

// TestInterpreterStdlibImport verifies that the interpreter can `import math`
// from the standard library and read a module constant.
func TestInterpreterStdlibImport(t *testing.T) {
	out, err := InterpreterRun("import math\nprint(math.PI)\n")
	if err != nil {
		t.Fatalf("InterpreterRun import math: %v", err)
	}
	if !strings.Contains(out, "3.141592653589793") {
		t.Fatalf("InterpreterRun import math output = %q, want math.PI", out)
	}
}
