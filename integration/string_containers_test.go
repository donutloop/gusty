package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Gap I.2 (interim) — every way of getting a string into a runtime container used to emit
// IR that LLVM's module verifier rejected: `rt_set_elem(i32 %h, i32 0, i32 @.str1)`, a global
// in an i32 parameter. The exit-code contract then reported valid Python-like code as a
// *compiler bug* (exit 2). ADR 0166 says an unsupported lowering is a compile diagnostic, so
// all of these must now fail the same actionable way — and none of them may reach the
// verifier. Real support needs an interned string table (the rest of Gap I.2).

func TestStringInContainerIsADiagnosticNotAnInvalidModule(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"assigned string list literal", "xs = [\"a\", \"b\"]\nprint(xs)\n"},
		{"string list literal with len", "xs = [\"a\", \"b\"]\nprint(len(xs))\n"},
		{"append string", "xs = []\nxs.append(\"s\")\nprint(len(xs))\n"},
		{"list item assign string", "xs = [1]\nxs[0] = \"s\"\nprint(xs)\n"},
		{"set literal string", "s = {\"a\"}\nprint(len(s))\n"},
		{"set add string", "s = set()\ns.add(\"s\")\nprint(len(s))\n"},
		{"dict string key via item assign", "d = {}\nd[\"k\"] = 1\nprint(len(d))\n"},
		{"dict string value via item assign", "d = {}\nd[1] = \"s\"\nprint(len(d))\n"},
		{"folded string element", "xs = []\nxs.append(str(42))\nprint(len(xs))\n"},
		{"string variable element", "t = \"hi\"\nxs = []\nxs.append(t)\nprint(len(xs))\n"},
	}
	for _, tc := range cases {
		_, err := lang.Compile(tc.src)
		if err == nil {
			t.Errorf("%s: must not compile today; it used to emit IR the verifier rejected", tc.name)
			continue
		}
		msg := err.Error()
		// the two things a caller needs: which backend does support it, and why not this one
		if !strings.Contains(msg, "AOT backend yet") {
			t.Errorf("%s: diagnostic should say what is unsupported: %q", tc.name, msg)
		}
		if !strings.Contains(msg, "interpreter") {
			t.Errorf("%s: diagnostic should name the working backend: %q", tc.name, msg)
		}
		if !strings.Contains(msg, "Gap I.2") {
			t.Errorf("%s: diagnostic should point at the tracking entry: %q", tc.name, msg)
		}
		// never leak a verifier verdict for what is a supported language shape
		if strings.Contains(msg, "rejected the module") || strings.Contains(msg, "global variable reference") {
			t.Errorf("%s: must be a codegen diagnostic, not an LLVM rejection: %q", tc.name, msg)
		}
	}
}

// TestIntContainersStillBuild guards the other side of the rule: the guard must not become a
// blanket refusal to compile containers.
func TestIntContainersStillBuild(t *testing.T) {
	cases := []string{
		"xs = [1, 2, 3]\nprint(len(xs))\n",
		"xs = []\nxs.append(7)\nprint(xs[0])\n",
		"s = set()\ns.add(3)\nprint(len(s))\n",
		"d = {}\nd[1] = 2\nprint(d[1])\n",
		"xs = [1]\nxs[0] = 9\nprint(xs)\n",
	}
	for _, src := range cases {
		res, err := lang.Compile(src)
		if err != nil {
			t.Errorf("int container %q must compile: %v", src, err)
			continue
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("int container %q must verify: %v %v", src, v.Errors, verr)
		}
	}
}
