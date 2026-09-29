package lang

import (
	"strings"
	"testing"
)

// Unit coverage for the compiled half of Gap R.21 (ADR 0213's clearing rule, ADR 0218).
//
// The behaviour tests in integration/exception_clear_test.go are the ones that matter, but a
// silence-shaped regression — someone "optimises away" a store they cannot see a use for — would
// restore the bug quietly, and it did happen to this feature's sibling (the arm chain, Gap R.20).
// These assert the *shape* of the module: an accepted exception is cleared, and the exception that
// is still travelling is not.

func clearModule(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("emitted module does not verify: %v\n%s", err, res.IR)
	}
	return res.IR
}

const clearLine = "store i32 0, i32* @exn_flag"
const setLine = "store i32 1, i32* @exn_flag"

func TestHandledExceptionEmitsAClear(t *testing.T) {
	src := "def f() -> int:\n    try:\n        z = 1 // 0\n    except KeyError:\n        pass\n    except:\n        pass\n    return 1\n\nprint(f())\n"
	ir := clearModule(t, src)
	if n := strings.Count(ir, clearLine); n < 1 {
		t.Fatalf("no clear of @exn_flag in the module; the next call would resurrect the handled exception\n%s", ir)
	}
	// The unmatched path still has to hand the exception outward, so the re-raise store must be
	// there too: a module where every store is a 0 is a module that has lost exceptions.
	if !strings.Contains(ir, setLine) {
		t.Fatalf("the propagate/re-raise path no longer sets @exn_flag\n%s", ir)
	}
}

func TestClearPrecedesTheReturnFromAnArm(t *testing.T) {
	// The arm leaves by `return`, so the clear has to be on that edge — before the ret, not just
	// somewhere in the function. Asserting the order is what distinguishes a real fix from a store
	// that the optimizer will never reach.
	src := "def f() -> int:\n    try:\n        z = 1 // 0\n    except:\n        return 42\n    return 1\n\nprint(f())\n"
	ir := clearModule(t, src)
	body := funcBody(ir, "@gy_f")
	if body == "" {
		body = funcBody(ir, "@f")
	}
	if body == "" {
		t.Fatalf("could not find f's body in the module\n%s", ir)
	}
	ci := strings.Index(body, clearLine)
	ri := strings.Index(body, "ret i32 42")
	if ci < 0 {
		t.Fatalf("returning from an arm emitted no clear; the caller's next call would re-raise\n%s", body)
	}
	if ri < 0 {
		t.Fatalf("the arm's return is missing from the module\n%s", body)
	}
	if ci > ri {
		t.Fatalf("the clear at %d comes after the return at %d, so it cannot run", ci, ri)
	}
}

func TestRaiseInsideAnArmDoesNotGetCleared(t *testing.T) {
	// `raise` in an arm is a *new* exception travelling outward, not a handled one. This arm can
	// never complete normally, so the arm's normal-exit clear must not be emitted at all: a module
	// for this program that clears the flag anywhere is a module that can swallow the raise.
	src := "def f() -> int:\n    try:\n        z = 1 // 0\n    except:\n        raise ValueError(\"from the arm\")\n    return 1\n"
	ir := clearModule(t, src)
	body := funcBody(ir, "@gy_f")
	if body == "" {
		body = funcBody(ir, "@f")
	}
	if body == "" {
		t.Fatalf("could not find f's body\n%s", ir)
	}
	if !strings.Contains(body, setLine) {
		t.Fatalf("the raise in the arm does not set @exn_flag\n%s", body)
	}
	if strings.Contains(body, clearLine) {
		t.Fatalf("an arm that always raises still cleared @exn_flag; the raise would be swallowed\n%s", body)
	}
}

// funcBody returns the text of the named function's definition, or "" when it is absent.
func funcBody(ir, name string) string {
	var out []string
	inFn := false
	for _, ln := range strings.Split(ir, "\n") {
		if strings.HasPrefix(ln, "define ") {
			inFn = strings.Contains(ln, name+"(")
			continue
		}
		if inFn {
			if ln == "}" {
				break
			}
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}
