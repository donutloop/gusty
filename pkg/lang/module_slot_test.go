package lang

import (
	"strings"
	"testing"
)

// Artifact-level coverage for the compiled half of roadmap Gap R.35 (ADR 0227): what a compiled
// function body may know about the module it sits in. Three rules are asserted here — a literal the
// module never rebinds is a value, what the module rebinds is module state and lives in a global, and
// a binding inside a body is the body's own — plus the two refusals that must stay refusals and the
// closure-body rule that replaced the silent `ret i32 0`.

// TestModuleLiteralIsAValueForABody: a name bound once at module level to a literal, never rebound,
// needs no slot to be read from. Before this it was a refusal (`undefined name "MAX"`) whose message
// blamed the reader for a name that is plainly bound three lines above.
func TestModuleLiteralIsAValueForABody(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"int constant", "MAX = 40\n\n\ndef twice() -> int:\n    return MAX * 2\n\n\nprint(twice())\n", "80\n"},
		{"string constant", "GREET = \"hi\"\n\n\ndef greet():\n    return GREET\n\n\nprint(greet())\n", "hi\n"},
		{"None constant", "NOTHING = None\n\n\ndef none_ish():\n    return NOTHING\n\n\nprint(none_ish())\n", "None\n"},
		{"constant through a method", "k = 5\n\n\nclass C:\n    def get(self) -> int:\n        return k\n\n\nprint(C().get())\n", "5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwant, perrText, perr := PythonRun(tc.src)
			if perr != nil {
				t.Fatalf("CPython disagreed with the expectation %q: %v\n%s", tc.want, perr, perrText)
			}
			if cwant != tc.want {
				t.Fatalf("CPython printed %q, want %q — the expectation itself is wrong", cwant, tc.want)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if _, err := VerifyModuleIR(res.IR, 0); err != nil {
				t.Fatalf("the emitted module does not verify: %v\n%s", err, res.IR)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if fn := emptyFunctionBody(res.IR); fn != "" {
				t.Fatalf("function %s has no instructions at all: the body was dropped and the fallback `ret i32 0` is its whole meaning\n%s", fn, res.IR)
			}
			if got := captureStdout(t, tc.src); got != tc.want {
				t.Fatalf("interpreter printed %q, want %q", got, tc.want)
			}
		})
	}
}

// emptyFunctionBody returns the name of a define whose body holds no instructions beyond the frame
// bookkeeping and a fallback `ret i32 0`. That shape is what a dropped body looks like in the module --
// the failure that let a closure answer 0 for a program whose answer is 9 (ADR 0227) -- and it is
// checked at the artifact level because the printed answer alone would not always notice.
func emptyFunctionBody(ir string) string {
	for _, seg := range strings.Split(ir, "define ") {
		name := ""
		if i := strings.Index(seg, "("); i > 0 {
			name = seg[:i]
		}
		end := strings.Index(seg, "\n}")
		if end < 0 {
			continue
		}
		body := seg[:end]
		rest := strings.TrimSpace(strings.SplitN(body, "{", 2)[1])
		empty := true
		for _, line := range strings.Split(rest, "\n") {
			l := strings.TrimSpace(line)
			if l == "" || strings.HasPrefix(l, ";") || strings.HasPrefix(l, "%gc.") ||
				strings.HasPrefix(l, "call void @rt_frame_") || strings.HasPrefix(l, "call void @rt_gc(") ||
				strings.HasPrefix(l, "%gc.n") || l == "entry:" || l == "ret i32 0" {
				continue
			}
			empty = false
		}
		if empty && strings.HasSuffix(rest, "ret i32 0") {
			return name
		}
	}
	return ""
}

// TestReboundModuleNameIsModuleState: a name the module rebinds cannot be folded, and a main-frame
// alloca is gone by the time a callee runs. It lives in @gy_mod_<name>, read when the call happens —
// which is ADR 0220's rule, and the reason a slot in main could never have answered it.
func TestReboundModuleNameIsModuleState(t *testing.T) {
	src := "LATE = 0\n\n\ndef read() -> int:\n    return LATE\n\n\nLATE = 3\nprint(read())\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "@gy_mod_LATE = global i32") {
		t.Fatalf("a rebound module binding read by a body must be module state in a global the callee can read\n%s", res.IR)
	}
	if _, err := VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("the emitted module does not verify: %v\n%s", err, res.IR)
	}
	if got := captureStdout(t, src); got != "3\n" {
		t.Fatalf("interpreter printed %q, want 3", got)
	}
}

// TestBodyBindingBeatsModuleBinding is the scoping rule that keeps the fold safe: an assignment
// anywhere inside a body makes the name local to that body (ADR 0220), so a function that sets K=1
// next to a module K=5 must answer 1. Deciding this from which slots happen to exist already let the
// compiled backend print `5 5` for this program.
func TestBodyBindingBeatsModuleBinding(t *testing.T) {
	src := "K = 5\n\n\ndef f() -> int:\n    K = 1\n    return K\n\n\nprint(f(), K)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertNoForbiddenIR(t, src, res.IR)
	if got := captureStdout(t, src); got != "1 5\n" {
		t.Fatalf("interpreter printed %q, want `1 5`", got)
	}
}

// TestModuleContainersAndFloatsStillRefuse is the deliberate half: a container read by a body is
// refused because the operations behind the handle are missing, and a float module name because its
// slot is a double and L11.6 owns floats. Both refusals must name module state, not misdescribe the
// value — the old messages called a list a string (Gap R.38).
func TestModuleContainersAndFloatsStillRefuse(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"len of a module list", "xs = [1, 2]\n\n\ndef n() -> int:\n    return len(xs)\n\n\nprint(n())\n"},
		{"append on a module list", "xs = [1]\n\n\ndef add() -> None:\n    xs.append(2)\n\n\nadd()\nprint(len(xs))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("the compiled backend accepted a body reaching module container state it cannot operate on")
			}
			msg := err.Error()
			if !strings.Contains(msg, "bound at module level") {
				t.Fatalf("the refusal must name module state: %q", msg)
			}
			if strings.Contains(msg, "non-string") || strings.Contains(msg, "Gap R.38") {
				t.Fatalf("the refusal misdescribes a list as a string (Gap R.38): %q", msg)
			}
		})
	}
}

// TestClosureBodyIsNotDroppedIntoAZeroReturn is the swallow that caused the worst line in the table:
// a nested def reading a module name compiled to an empty body and printed 0 while the record and
// CPython printed 9. If a closure body cannot be lowered the compile must fail; it must not ship a
// function that returns 0.
func TestClosureBodyIsNotDroppedIntoAZeroReturn(t *testing.T) {
	src := "G = 9\n\n\ndef outer() -> int:\n    def inner() -> int:\n        return G\n    return inner()\n\n\nprint(outer())\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "ret i32 9") && !strings.Contains(res.IR, "ret i32 %") {
		t.Fatalf("the nested def's body is not computing the module value; a dropped body returns the fallback\n%s", res.IR)
	}
	if got := captureStdout(t, src); got != "9\n" {
		t.Fatalf("interpreter printed %q, want 9 — the nested def's body must reach the module", got)
	}
}

// TestDecoratedClosureSaysSoIntheModule covers the one documented exemption: the unreachable closure
// inside a decorator definition may go unlowered, because a decorated call runs the trampoline. The
// failure belongs in the module text, where --emit-llvm and a reader can both find it.
func TestDecoratedClosureSaysSoIntheModule(t *testing.T) {
	src := "def add1(g):\n    def wrap(x):\n        return g(x) + 1\n    return wrap\n\n\n@add1\ndef f(x):\n    return x * 2\n\n\nprint(f(3))\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the wrapping decorator must still compile: %v", err)
	}
	if !strings.Contains(res.IR, "; note: closure wrap: body not lowered") {
		t.Fatalf("a deferred closure-body failure must be named in the module, not hidden\n%s", res.IR)
	}
	if got := captureStdout(t, src); got != "7\n" {
		t.Fatalf("interpreter printed %q, want 7", got)
	}
}
