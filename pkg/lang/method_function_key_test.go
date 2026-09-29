package lang

import (
	"strings"
	"testing"
)

// A method and a module function of the same name are two definitions. They used to share one
// key in the checker's function table, so the method — analyzed after the module function — won,
// and a call to the module function was then read against the method: one parameter too many,
// `self` counted as an argument slot, and the last parameter left unbound. What the program got
// back was an undefined-name error on the callee's own parameter (roadmap Gap R.8, ADR 0200).

func methodKeyDiags(t *testing.T, src string) []Diagnostic {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Analyze(prog)
}

func TestMethodDoesNotOverwriteTheModuleFunction(t *testing.T) {
	src := `def time(x):
    return x + 5


class Timer:
    def time(self, x):
        return x * 3

    def run(self, x):
        return self.time(x) + 1


print(time(1))
print(Timer().run(2))
`
	for _, d := range methodKeyDiags(t, src) {
		if d.Level == LevelError {
			t.Errorf("the shape that names a method after a helper was refused: %v", d)
		}
	}
}

// TestModuleCallIsCheckedAgainstTheModuleDefinition is the positive half: the name resolves to the
// right definition, not merely to *a* definition.
func TestModuleCallIsCheckedAgainstTheModuleDefinition(t *testing.T) {
	src := `def time(x: int) -> int:
    return x


class Timer:
    def time(self, x):
        return x * 3


print(time("text"))
`
	diags := methodKeyDiags(t, src)
	if !hasMsg(diags, "expected int, got str") {
		t.Errorf("the call to the module function was not checked against its annotation: %v", diags)
	}
}

// TestCallToAShadowingMethodIsNotReadAsTheModuleFunction checks the reverse direction: defining a
// method must not silence the module function's checks either, and a call placed before the class
// sees the same definition as one placed after it.
func TestCallToAShadowingMethodIsNotReadAsTheModuleFunction(t *testing.T) {
	before := `def area(w: int, h: int) -> int:
    return w * h


class Rect:
    def area(self):
        return 6


print(area(2, "three"))
`
	after := `class Rect:
    def area(self):
        return 6


def area(w: int, h: int) -> int:
    return w * h


print(area(2, "three"))
`
	for name, src := range map[string]string{"call before the class": before, "call after the class": after} {
		diags := methodKeyDiags(t, src)
		if !hasMsg(diags, "expected int, got str") {
			t.Errorf("%s: the module function's annotation was not enforced: %v", name, diags)
		}
	}
}

// TestTwoClassesMayShareAMethodName is the same defect one level in: with a single key, the second
// class's method also overwrote the first's.
func TestTwoClassesMayShareAMethodName(t *testing.T) {
	src := `class A:
    def m(self, x):
        return x


class B:
    def m(self, y):
        return y * 2


print(A().m(3))
print(B().m(4))
`
	for _, d := range methodKeyDiags(t, src) {
		if d.Level == LevelError {
			t.Errorf("two classes with the same method name were refused: %v", d)
		}
	}
}

// TestMethodBodiesAreStillAnalyzed guards against fixing the key by simply skipping methods.
func TestMethodBodiesAreStillAnalyzed(t *testing.T) {
	src := `class T:
    def bad(self):
        return nope


print(1)
`
	diags := methodKeyDiags(t, src)
	if !hasMsg(diags, `undefined name "nope"`) {
		t.Errorf("a method body is no longer being analyzed: %v", diags)
	}
}

// TestArityIsCheckedForTheRightDefinition: the false refusal was an arity confusion, so which
// definition a call is checked against is asserted explicitly — through the parameter the error
// names. (Missing arguments themselves are not yet reported for any function, annotated or not:
// roadmap R.10.)
func TestArityIsCheckedForTheRightDefinition(t *testing.T) {
	src := `def time(x: int) -> int:
    return x


class Timer:
    def time(self, label: str) -> str:
        return label


print(time(1))
`
	// The call is legal against the module function. Read against the method — one parameter
	// short, because `self` has no argument slot — it would be blamed on `label`.
	for _, d := range methodKeyDiags(t, src) {
		if d.Level == LevelError && strings.Contains(d.Msg, "label") {
			t.Errorf("the module call was checked against the method's parameters: %v", d)
		}
	}

	wrongArg := `def time(x: int) -> int:
    return x


class Timer:
    def time(self, label: str) -> str:
        return label


print(time("text"))
`
	diags := methodKeyDiags(t, wrongArg)
	if !hasMsg(diags, `argument "x"`) {
		t.Errorf("the argument error should name the module function's parameter: %v", diags)
	}
	if hasMsg(diags, "label") {
		t.Errorf("the argument error named the method's parameter instead: %v", diags)
	}
}
