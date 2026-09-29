package lang

import (
	"strings"
	"testing"
)

// Gap R.17 (ADR 0211) lived in one discarded return value: `dlopenRun` called the generated
// `main` and dropped what it returned, so nothing above could tell a program that finished
// from a program that died. These tests pin the status where it is produced — and pin the
// direction that matters most, that a *caught* exception is still a clean run.

func TestJITReportsAStatusOfItsOwn(t *testing.T) {
	res, err := JIT("print(1 + 1)\n", 0)
	if err != nil {
		t.Fatalf("JIT: %v", err)
	}
	if res.Code != 0 {
		t.Errorf("a program that ran to completion reported status %d (stderr %q)", res.Code, res.Stderr)
	}
	if res.Output != "2\n" {
		t.Errorf("output = %q, want \"2\\n\"", res.Output)
	}
}

func TestJITReportsANonZeroStatusForAnUncaughtRaise(t *testing.T) {
	res, err := JIT("raise ValueError(\"boom\")\n", 0)
	if err != nil {
		t.Fatalf("JIT: %v", err)
	}
	if res.Code == 0 {
		t.Fatalf("an uncaught exception reported success; the CLI has nothing else to ask (stderr %q)", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "ValueError") {
		t.Errorf("the trap should name the exception class on stderr, got %q", res.Stderr)
	}
	if res.Output != "" {
		t.Errorf("stdout is only what the program printed, got %q", res.Output)
	}
}

func TestJITReportsANonZeroStatusForATrapInsideABuiltin(t *testing.T) {
	// The trap is raised by emitted bounds code rather than an explicit `raise`, which is
	// the path most likely to forget the exit status: it goes through the same exception
	// machinery, so it must reach the same status.
	res, err := JIT("xs = [1, 2, 3]\nprint(xs[-4])\n", 0)
	if err != nil {
		t.Fatalf("JIT: %v", err)
	}
	if res.Code == 0 {
		t.Fatalf("an out-of-range subscript reported success (stderr %q)", res.Stderr)
	}
	if !strings.Contains(res.Stderr, "IndexError") {
		t.Errorf("want an IndexError on stderr, got %q", res.Stderr)
	}
}

// The other direction, which is the one a naive "make traps non-zero" would break: a
// program that raises and handles it ran to completion and says so.
func TestJITStillReportsZeroWhenTheExceptionIsCaught(t *testing.T) {
	res, err := JIT("try:\n    xs = [1, 2, 3]\n    print(xs[-4])\nexcept IndexError:\n    print(\"caught\")\n", 0)
	if err != nil {
		t.Fatalf("JIT: %v", err)
	}
	if res.Code != 0 {
		t.Fatalf("a program that handled its exception reported status %d (stderr %q, output %q)", res.Code, res.Stderr, res.Output)
	}
	if res.Output != "caught\n" {
		t.Errorf("output = %q, want \"caught\\n\"", res.Output)
	}
}
