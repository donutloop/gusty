package lang

import (
	"strings"
	"testing"
)

// Gap K.10 — a typo must be reported by the front end, not by LLVM.
//
// `print(undefined_thing)` at module level used to pass the checker (print's arguments were
// never analysed), codegen emitted a load from a slot that does not exist, and LLVM's
// module verifier rejected the module — which the exit-code contract reports as *compiler
// bug* (exit 2) for what is an ordinary typo. Three layers now hold:
//
//   1. the checker analyses builtin-call arguments;
//   2. every built-in call name is predeclared from one table, so a real built-in is never
//      "undefined" (the drift that made `print(sum(xs))` fail to compile);
//   3. codegen refuses to lower a name it has no binding for, so the next checker hole
//      produces a diagnostic instead of an invalid module.

func TestUndefinedNameInBuiltinCallIsAFrontEndError(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"print argument", "print(undefined_thing)\n"},
		{"print argument in expression", "print(undefined_thing + 1)\n"},
		{"len argument", "print(len(undefined_thing))\n"},
		{"range argument", "for i in undefined_iterable:\n    print(i)\n"},
		{"print keyword argument", "print(1, sep=undefined_sep)\n"},
	}
	for _, tc := range cases {
		diags := analyzeSrc(t, tc.src)
		if !diagHas(diags, "undefined name") {
			t.Errorf("%s: the checker should report the undefined name, got %v", tc.name, diags)
		}
	}
	// the report names the identifier
	diags := analyzeSrc(t, "print(undefined_thing)\n")
	if !diagHas(diags, `"undefined_thing"`) {
		t.Errorf("the diagnostic should quote the offending name: %v", diags)
	}
}

func TestUndefinedNameBuildIsADiagnosticNotAnInvalidModule(t *testing.T) {
	// ADR 0166: an unsupported/unlowerable construct is a compile diagnostic. The old
	// behaviour emitted `%_undefined_thing` and let LLVM find it.
	_, err := Compile("print(undefined_thing)\n")
	if err == nil {
		t.Fatalf("codegen should refuse an unbound name")
	}
	if !strings.Contains(err.Error(), "undefined name") || !strings.Contains(err.Error(), "undefined_thing") {
		t.Errorf("diagnostic should name the identifier, got %q", err.Error())
	}
	// and the interpreter says the same thing, so the two backends agree on the error
	if _, evalErr := evalCapture(t, "print(undefined_thing)\n"); evalErr == nil {
		t.Errorf("the interpreter should also reject the undefined name")
	}
}

// TestBuiltinsArePredeclaredInTheChecker: `sum`, `enumerate`, `zip`, `round`, … work in
// both backends, so the checker must not call them undefined. This is the drift that
// predeclared.go exists to prevent.
func TestBuiltinsArePredeclaredInTheChecker(t *testing.T) {
	for _, nm := range predeclaredCallables() {
		switch nm {
		case "True", "False", "None", "super":
			continue // literals, and super() which must appear as super().method()
		}
		src := "xs = [1, 2, 3]\nprint(" + nm + "(xs))\n"
		if _, err := Parse(src); err != nil {
			t.Errorf("%s: a predeclared name must be callable, but %q does not parse: %v", nm, src, err)
			continue
		}
		if diags := analyzeSrc(t, src); diagHas(diags, "undefined name \""+nm+"\"") {
			t.Errorf("%s: built-in reported as undefined name by the checker", nm)
		}
	}
	// A name that is genuinely not built-in is still undefined.
	if diags := analyzeSrc(t, "print(not_a_real_builtin(1))\n"); !diagHas(diags, "undefined name") {
		t.Errorf("an unknown call should still be an undefined name: %v", diags)
	}
}

// TestPredeclaredTableCoversTheLSPList keeps the completion list and the semantic table
// from drifting apart (they were separate before, which is how `sum` went missing).
func TestPredeclaredTableCoversTheLSPList(t *testing.T) {
	for _, n := range builtins {
		if !isPredeclaredName(n) {
			t.Errorf("LSP offers %q but the checker/codegen table does not know it", n)
		}
	}
}

// TestWithAsTargetIsBound: the `with … as m:` path allocated m's slot without recording it,
// so the new guard rejected `m.n` — an example of the guard finding bindings that were
// registered inconsistently rather than of the guard being wrong.
func TestWithAsTargetIsBound(t *testing.T) {
	res, err := Compile("class M:\n    def __init__(self):\n        self.n = 0\n\n    def __enter__(self):\n        return self\n\n    def __exit__(self, a, b, c):\n        return 0\n\ndef f():\n    x = 0\n    with M() as m:\n        x = m.n + 1\n\n    return x\n\nprint(f())\n")
	if err != nil {
		t.Fatalf("with-as must compile: %v", err)
	}
	if v, verr := VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
		t.Errorf("with-as module must verify: %v %v", v.Errors, verr)
	}
}
