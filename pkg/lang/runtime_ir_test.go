package lang

import (
	"strings"
	"testing"
)

// The runtime is shipped as LLVM-IR text embedded in Go raw strings. Two ways to break
// it that cost real debugging time in this repo, because the failure surfaces far from the
// cause (as "expected top-level entity" / "invalid redefinition" from the module verifier):
//
//   - an LLVM comment is `;`, not `//` — a `//` line is a parse error in the module
//   - a backtick inside the Go raw string ends the literal mid-comment, so the rest of
//     the IR leaks into the Go file (or Go code leaks into the IR)
//
// This test scans every embedded IR block for both, so the mistake is caught by a test
// instead of by `opt -passes=verify` at build time.
func TestEmbeddedRuntimeIRIsWellFormedText(t *testing.T) {
	blocks := map[string]string{
		"heapRuntimeIR":  heapRuntimeIR,
		"raiseRuntimeIR": raiseRuntimeIR,
	}
	for name, ir := range blocks {
		if strings.TrimSpace(ir) == "" {
			t.Fatalf("%s: embedded runtime block is empty", name)
		}
		if strings.Contains(ir, "`") {
			t.Errorf("%s: the IR text contains a backtick, which cannot appear inside a Go raw string", name)
		}
		// Each `define` must be closed; an unbalanced block is what the module verifier
		// reports as "input module is broken".
		if strings.Count(ir, "\ndefine ") != strings.Count(ir, "\n}\n") {
			t.Errorf("%s: %d defines but %d closing braces — a block is unterminated",
				name, strings.Count(ir, "\ndefine "), strings.Count(ir, "\n}\n"))
		}
		for i, line := range strings.Split(ir, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				t.Errorf("%s line %d: LLVM comments use ';', not '//': %q", name, i+1, line)
			}
			if strings.HasPrefix(trimmed, "#") {
				t.Errorf("%s line %d: LLVM does not use '#' comments: %q", name, i+1, line)
			}
			// A stray Go statement inside the IR block means Go leaked into the IR.
			if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "b.WriteString") {
				t.Errorf("%s line %d: Go code leaked into the embedded IR: %q", name, i+1, line)
			}
		}
		// Every define must be balanced by a closing brace on its own line count.

	}
}

// TestRuntimeHelpersAreDefinedOnce guards the other failure I hit: adding a helper that
// already exists gives the verifier "invalid redefinition of function".
func TestRuntimeHelpersAreDefinedOnce(t *testing.T) {
	all := heapRuntimeIR + "\n" + raiseRuntimeIR
	seen := map[string]int{}
	for _, line := range strings.Split(all, "\n") {
		if !strings.HasPrefix(line, "define internal ") {
			continue
		}
		// `define internal <rettype> @name(...)` — the name is the token starting with @
		fn := ""
		for _, f := range strings.Fields(line) {
			if strings.HasPrefix(f, "@") {
				fn = strings.SplitN(f, "(", 2)[0]
			}
		}
		if fn == "" {
			continue
		}
		seen[fn]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("runtime helper %s is defined %d times across the embedded IR blocks", name, n)
		}
	}
	// The helpers this cycle added must actually be present.
	for _, want := range []string{"rt_pop", "rt_set_discard", "rt_set_clear", "rt_dict_has", "rt_put_elem", "rt_die"} {
		if !strings.Contains(all, "define internal") || !strings.Contains(all, want) {
			t.Errorf("embedded runtime is missing %s", want)
		}
	}
}
