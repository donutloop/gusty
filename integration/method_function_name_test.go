package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestMethodAndModuleFunctionOfOneNameAgree closes Gap R.8 (ADR 0200).
//
// A module function and a method may share a name — `def area` next to `Rect.area`, `def time`
// next to `Timer.time` is the ordinary vocabulary of a program with classes. They are two
// definitions, and the checker's function table used to give them one key: the method, analyzed
// later, replaced the module function, so a call to the module function was then analyzed against
// the *method* — whose arity counts `self`. The argument count came out one short, the last
// parameter was never bound, and the program was refused with an undefined-name error on the
// callee's own parameter.
func TestMethodAndModuleFunctionOfOneNameAgree(t *testing.T) {
	src := readProgramSrc("method_function_name_clash")
	want := "6\n7\n6\n12\n"

	if got := runInterp(t, src); got != want {
		t.Errorf("interpreted output =\n%q\nwant\n%q", got, want)
	}
	built, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
	pyOut, pyErr, perr := lang.PythonRun(src)
	if perr != nil {
		t.Skipf("no usable oracle: %v\n%s", perr, pyErr)
	}
	if pyOut != want {
		t.Errorf("CPython output =\n%q\nwant\n%q", pyOut, want)
	}
}

// TestMethodAndModuleFunctionChecksClean is the part that was a refusal: the same shape must earn
// no diagnostic at all — and the module call must still be checked against the *module's*
// definition, which is the positive form of the same fact.
func TestMethodAndModuleFunctionChecksClean(t *testing.T) {
	clean := `def time(x):
    return x + 5


class Timer:
    def time(self, x):
        return x * 3

    def run(self, x):
        return self.time(x) + 1


print(time(1))
print(Timer().run(2))
`
	prog, err := lang.Parse(clean)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range lang.Analyze(prog) {
		if d.Level == lang.LevelError {
			t.Errorf("a method and a module function of one name earned an error: %v", d)
		}
	}

	typed := `def time(x: int) -> int:
    return x


class Timer:
    def time(self, x):
        return x * 3


print(time("text"))
`
	tp, err := lang.Parse(typed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var got string
	for _, d := range lang.Analyze(tp) {
		if d.Level == lang.LevelError {
			got = d.Msg
		}
	}
	if !strings.Contains(got, "expected int, got str") {
		t.Errorf("the module call must still be checked against the module's definition, got %q", got)
	}
}

// TestMethodBodyErrorsAreStillReported: keeping methods out of the bare-name table must not stop
// the checker from looking inside them.
func TestMethodBodyErrorsAreStillReported(t *testing.T) {
	src := `class T:
    def bad(self):
        return nope


print(1)
`
	prog, err := lang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var got string
	for _, d := range lang.Analyze(prog) {
		if d.Level == lang.LevelError {
			got = d.Msg
		}
	}
	if !strings.Contains(got, `undefined name "nope"`) {
		t.Errorf("an error inside a method body went unreported: %q", got)
	}
}
