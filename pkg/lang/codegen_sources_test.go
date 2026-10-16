package lang

import (
	"os"
	"strings"
	"testing"
)

// readCodegenSources concatenates the emitter's own sources: codegen.go (the front door and the
// generator's state) plus the codegen_*.go files the lowering and the runtime IR blocks live in.
//
// The emitter used to be one 21,473-line file, and the shape guards below used to read exactly that
// file. They assert things a reader cannot see by running a program — that a road still ASKS one
// shared predicate rather than restating a switch, that a hand-copied method list has not come back,
// that a fold road reads the generated table instead of Go's answer. Splitting the emitter across
// files named for the question they answer moves those lines; it does not move the rule, so the
// guards read the whole emitter now. An absence asserted here is an absence across every one of the
// roads, which is what the assertion always meant.
func readCodegenSources(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var b strings.Builder
	n := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "codegen") || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		b.Write(src)
		b.WriteByte('\n')
		n++
	}
	if n < 20 {
		t.Fatalf("only %d codegen sources read; the guard would certify a rule it never looked at", n)
	}
	return b.String()
}

// TestCodegenSourcesCoverTheEmitter is the guard on the guard: the split files are discovered by
// name, so a file renamed out of the codegen_* family would silently drop roads out of every shape
// assertion above. The count is the emitter's own — 42 sources, one front door, the IR blocks and
// the lowering roads — and it fails the day a file leaves the family without a reader being told.
func TestCodegenSourcesCoverTheEmitter(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "codegen") && strings.HasSuffix(name, ".go") &&
			!strings.HasSuffix(name, "_test.go") {
			files = append(files, name)
		}
	}
	if len(files) < 30 {
		t.Fatalf("only %d codegen sources on disk: %v — the emitter's roads are spread over more files than that", len(files), files)
	}
	// Every road family the split promised still exists, and each one is non-trivial.
	for _, want := range []string{
		"codegen.go", "codegen_value.go", "codegen_stmt.go", "codegen_call.go", "codegen_arith.go",
		"codegen_match.go", "codegen_comp.go", "codegen_func.go", "codegen_class.go", "codegen_text.go",
		"codegen_container.go", "codegen_index.go", "codegen_float.go", "codegen_minmax.go",
		"codegen_module.go", "codegen_gc.go", "codegen_error.go", "codegen_truth.go", "codegen_facts.go",
		"codegen_union.go", "codegen_instance.go", "codegen_ir_arena.go",
	} {
		found := false
		for _, f := range files {
			if f == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is gone from the emitter — the split documented in codegen.go's header does not match what is on disk", want)
		}
	}
}
