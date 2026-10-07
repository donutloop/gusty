package lang

import (
	"testing"
)

// Unit coverage for roadmap Gap R.23 (ADR 0222): a `finally` is a deferred body and runs on
// every exit from its `try`, and an `except` arm catches exceptions, not control transfers.
// Both halves were wrong in the record and both are asserted here from the Python answer,
// written out below rather than observed from an emission.

func deferredRun(t *testing.T, src string) string {
	t.Helper()
	got := captureStdout(t, src)
	if got == "" {
		t.Fatalf("%q printed nothing at all", src)
	}
	return got
}

func TestDeferredBodyRunsWhenTheBodyReturns(t *testing.T) {
	src := "def f() -> int:\n    try:\n        return 1\n    finally:\n        print(\"fin\")\n\nprint(f())\n"
	if got := deferredRun(t, src); got != "fin\n1\n" {
		t.Fatalf("return through a finally printed %q, want \"fin\\n1\\n\" — the deferred body was skipped", got)
	}
}

func TestDeferredBodyRunsWhenTheExceptionPropagates(t *testing.T) {
	src := "def f() -> int:\n    try:\n        x = 1 / 0\n    finally:\n        print(\"fin\")\n    return 0\n\ntry:\n    print(f())\nexcept:\n    print(\"caught\")\n"
	if got := deferredRun(t, src); got != "fin\ncaught\n" {
		t.Fatalf("a propagating exception printed %q, want \"fin\\ncaught\\n\"", got)
	}
}

func TestDeferredBodyRunsOncePerExit(t *testing.T) {
	src := "def f() -> int:\n    try:\n        return 5\n    finally:\n        print(\"once\")\n\nprint(f())\nprint(f())\n"
	if got := deferredRun(t, src); got != "once\n5\nonce\n5\n" {
		t.Fatalf("two calls printed %q, want each deferred body exactly once per exit: %q", got, "once\n5\nonce\n5\n")
	}
}

func TestDeferredBodyRunsOnBreakAndContinue(t *testing.T) {
	brk := "for i in range(2):\n    try:\n        break\n    finally:\n        print(\"fin\")\nprint(\"done\")\n"
	if got := deferredRun(t, brk); got != "fin\ndone\n" {
		t.Fatalf("`break` through a finally printed %q, want \"fin\\ndone\\n\"", got)
	}
	cont := "for i in range(3):\n    try:\n        continue\n    finally:\n        print(\"fin\")\nprint(\"done\")\n"
	if got := deferredRun(t, cont); got != "fin\nfin\nfin\ndone\n" {
		t.Fatalf("`continue` through a finally printed %q, want one run per iteration", got)
	}
}

func TestReturnInFinallyOverridesThePendingValue(t *testing.T) {
	src := "def f() -> int:\n    try:\n        return 1\n    finally:\n        return 2\n\nprint(f())\n"
	if got := deferredRun(t, src); got != "2\n" {
		t.Fatalf("a `return` in `finally` gave %q, want the finally's value: 2", got)
	}
}

func TestNestedDeferredBodiesRunInnermostFirst(t *testing.T) {
	src := "def f() -> int:\n    try:\n        try:\n            return 1\n        finally:\n            print(\"inner\")\n    finally:\n        print(\"outer\")\n\nprint(f())\n"
	if got := deferredRun(t, src); got != "inner\nouter\n1\n" {
		t.Fatalf("nested finallys printed %q, want \"inner\\nouter\\n1\\n\"", got)
	}
}

func TestDeferredBodyRunsAfterTheArms(t *testing.T) {
	src := "try:\n    x = 1 / 0\nexcept:\n    print(\"caught\")\nfinally:\n    print(\"fin\")\n"
	if got := deferredRun(t, src); got != "caught\nfin\n" {
		t.Fatalf("arm then finally printed %q, want the arm first and the deferred body after", got)
	}
}

func TestRaiseInFinallyReplacesThePendingException(t *testing.T) {
	// The class is what this asserts: the arm took the ZeroDivisionError, the deferred body then
	// raised ValueError, and the ValueError is what the program dies of. (What the run printed is
	// asserted end-to-end on the compiled backend in integration/deferred_bodies_test.go; the
	// record's own capture helper refuses to return stdout for a trapping program.)
	ee := trapRun(t, "try:\n    x = 1 / 0\nexcept ZeroDivisionError:\n    print(\"caught zero\")\nfinally:\n    raise ValueError(\"boom\")\n")
	if ee.ExnType != "ValueError" {
		t.Fatalf("the deferred body's raise did not replace the pending ZeroDivisionError: got %q", ee.ExnType)
	}
}

// TestArmsDoNotCatchTransfers is the other half of the same statement: a `return`, `break` or
// `continue` is a transfer, and Python's arms never see one. the record moved transfers with
// the same Go-error mechanism it uses for raises, so a bare `except:` used to catch a `return`.
func TestArmsDoNotCatchTransfers(t *testing.T) {
	ret := "def f() -> int:\n    try:\n        return 1\n    except:\n        print(\"caught a return\")\n    return 3\n\nprint(f())\n"
	if got := deferredRun(t, ret); got != "1\n" {
		t.Fatalf("a bare arm caught a `return`: printed %q, want \"1\\n\" — an arm catches exceptions only", got)
	}
	brk := "for i in range(2):\n    try:\n        break\n    except:\n        print(\"caught a break\")\nprint(\"done\")\n"
	if got := deferredRun(t, brk); got != "done\n" {
		t.Fatalf("a bare arm caught a `break`: printed %q, want \"done\\n\"", got)
	}
	typed := "def f() -> int:\n    try:\n        return 1\n    except ValueError:\n        print(\"caught\")\n    return 3\n\nprint(f())\n"
	if got := deferredRun(t, typed); got != "1\n" {
		t.Fatalf("a typed arm caught a `return`: printed %q, want \"1\\n\"", got)
	}
}

// TestDeferredBodyStillSeesItsOwnExceptionsFailed is the control the "mute-shaped fix" would
// pass: running the deferred body must not turn it into a place where exceptions disappear.
func TestDeferredBodyRaisingIsNotSilenced(t *testing.T) {
	src := "def f() -> int:\n    try:\n        pass\n    finally:\n        z = 1 / 0\n\ntry:\n    print(f())\nexcept:\n    print(\"caught\")\n"
	if got := deferredRun(t, src); got != "caught\n" {
		t.Fatalf("an exception raised by the deferred body printed %q, want \"caught\\n\" (it propagates)", got)
	}
}
