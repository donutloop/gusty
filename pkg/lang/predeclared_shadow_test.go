package lang

import (
	"strings"
	"testing"
)

// A built-in name belongs to the program that claims it (ADR 0199) — but only from its definition
// onwards, because that is when the binding exists. Above the definition the two backends meant two
// different things: the interpreter, executing in order, still reached the built-in, while codegen —
// which emits every function before the module body — resolved the call to the program's own
// definition. One file, two outputs (roadmap Gap R.12, ADR 0205). The checker now refuses the program
// where the ambiguity is written.

func shadowDiags(t *testing.T, src string) []Diagnostic {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Analyze(prog)
}

// TestClaimingABuiltInNameBelowItsUseIsRefused is the measured divergence, made a refusal.
func TestClaimingABuiltInNameBelowItsUseIsRefused(t *testing.T) {
	src := "for i in range(2):\n    print(i * 100)\n\n\ndef range(x):\n    return x * 3\n\n\nprint(range(4))\n"
	diags := shadowDiags(t, src)
	var found bool
	for _, d := range diags {
		if d.Level == LevelError && strings.Contains(d.Msg, `"range" is a built-in here`) &&
			strings.Contains(d.Msg, "line 5") {
			found = true
		}
	}
	if !found {
		t.Errorf("the call above the definition was not refused, naming the definition: %v", diags)
	}
}

// TestTheRefusalIsTheOnlyDiagnostic: as in ADR 0201, an error that manufactures follow-on warnings
// about a call that was perfectly typed is worse than no error at all. The analysis continues down
// the built-in path precisely so the loop variable keeps the type it would have had.
func TestTheRefusalIsTheOnlyDiagnostic(t *testing.T) {
	src := "for i in range(2):\n    print(i * 100)\n\n\ndef range(x):\n    return x * 3\n"
	errors, warnings := 0, 0
	for _, d := range shadowDiags(t, src) {
		switch d.Level {
		case LevelError:
			errors++
		case LevelWarning:
			warnings++
		}
	}
	if errors != 1 || warnings != 0 {
		t.Errorf("want exactly one error and no derived warnings, got %d errors %d warnings", errors, warnings)
	}
}

// TestDefinitionAboveEveryUseIsClean is the positive space: claiming a built-in name is legal, and
// the fix must not make it illegal.
func TestDefinitionAboveEveryUseIsClean(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"range as a helper", "def range(x):\n    return x * 3\n\nprint(range(4))\n"},
		{"print as a helper", "def print(x):\n    return x + 1\n\nz = print(2)\n"},
		{"len as a helper", "def len(x):\n    return 3\n\nprint(len([1, 2]))\n"},
		{"use before and after, all below the def", "def abs(x):\n    return x + 7\n\na = abs(-1)\nb = abs(a)\nprint(b)\n"},
		{"a nested def of a built-in name, called below", "def outer():\n    def len(x):\n        return 1\n    return len([1])\n\nprint(outer())\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, d := range shadowDiags(t, tc.src) {
				if d.Level == LevelError {
					t.Errorf("%s: a program that owns a built-in name in order was refused: %v", tc.name, d)
				}
			}
		})
	}
}

// TestMethodsDoNotClaimBuiltInNames: `Counter.range` is a method, and a method has never shadowed a
// module name (ADR 0200) — so a class full of `range` methods must not silence the built-in loop.
func TestMethodsDoNotClaimBuiltInNames(t *testing.T) {
	src := "class C:\n    def range(self, n):\n        return n * 4\n\n    def print(self, n):\n        return n\n\nfor i in range(2):\n    print(i)\n"
	for _, d := range shadowDiags(t, src) {
		if d.Level == LevelError {
			t.Errorf("a method named after a built-in silenced the built-in: %v", d)
		}
	}
}

// TestCallInsideAFunctionBodyIsNotRefused pins the boundary of the rule. Inside a function body the
// question is not ordered — the body runs after every module definition has executed, so both
// backends agree there, and refusing it would be over-reach.
func TestCallInsideAFunctionBodyIsNotRefused(t *testing.T) {
	src := "def wrap(v):\n    return range(v)\n\n\ndef range(x):\n    return x * 3\n\n\nprint(wrap(5))\n"
	for _, d := range shadowDiags(t, src) {
		if d.Level == LevelError && strings.Contains(d.Msg, "is a built-in here") {
			t.Errorf("a call inside a function body was refused, though both backends resolve it the same way: %v", d)
		}
	}
}

// TestTheBuiltInPathIsStillAnalysedAfterTheRefusal: reporting the ambiguity must not stop checking
// the call's own arguments.
func TestTheBuiltInPathIsStillAnalysedAfterTheRefusal(t *testing.T) {
	src := "print(len(nope_here))\n\n\ndef len(x):\n    return 3\n"
	diags := shadowDiags(t, src)
	var undef, shadow bool
	for _, d := range diags {
		if d.Level != LevelError {
			continue
		}
		if strings.Contains(d.Msg, `undefined name "nope_here"`) {
			undef = true
		}
		if strings.Contains(d.Msg, "is a built-in here") {
			shadow = true
		}
	}
	if !shadow || !undef {
		t.Errorf("want both the ambiguity and the argument error, got %v", diags)
	}
}
