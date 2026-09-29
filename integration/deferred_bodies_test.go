package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Integration coverage for roadmap Gap R.23 (ADR 0222): a deferred `finally` body runs on every
// exit from its `try`, on both backends, and an `except` arm catches exceptions rather than
// transfers. Both backends were wrong in the same way, which is exactly what the parity contract
// cannot see; every expectation below is the answer CPython gives for that source.

type deferredCase struct {
	name     string
	src      string
	want     string
	wantCode int
}

var deferredCases = []deferredCase{
	{
		name: "return_from_try",
		src:  "def f() -> int:\n    try:\n        return 1\n    finally:\n        print(\"fin\")\n\nprint(f())\n",
		want: "fin\n1\n",
	},
	{
		name: "exception_propagates_through_finally",
		src:  "def f() -> int:\n    try:\n        x = 1 / 0\n    finally:\n        print(\"fin\")\n    return 0\n\ntry:\n    print(f())\nexcept:\n    print(\"caught\")\n",
		want: "fin\ncaught\n",
	},
	{
		name: "handled_then_deferred",
		src:  "try:\n    x = 1 / 0\nexcept:\n    print(\"caught\")\nfinally:\n    print(\"fin\")\n",
		want: "caught\nfin\n",
	},
	{
		name: "return_in_finally_wins",
		src:  "def f() -> int:\n    try:\n        return 1\n    finally:\n        return 2\n\nprint(f())\n",
		want: "2\n",
	},
	{
		name: "break_in_try",
		src:  "for i in range(3):\n    try:\n        break\n    finally:\n        print(\"fin\")\nprint(\"done\")\n",
		want: "fin\ndone\n",
	},
	{
		name: "continue_in_try",
		src:  "for i in range(3):\n    try:\n        continue\n    finally:\n        print(\"fin\")\nprint(\"done\")\n",
		want: "fin\nfin\nfin\ndone\n",
	},
	{
		name: "nested_finallys_innermost_first",
		src:  "def f() -> int:\n    try:\n        try:\n            return 1\n        finally:\n            print(\"inner\")\n    finally:\n        print(\"outer\")\n\nprint(f())\n",
		want: "inner\nouter\n1\n",
	},
	{
		name: "deferred_runs_once_per_call",
		src:  "def f() -> int:\n    try:\n        return 5\n    finally:\n        print(\"once\")\n\nprint(f())\nprint(f())\n",
		want: "once\n5\nonce\n5\n",
	},
	{
		name: "return_from_arm",
		src:  "def f() -> int:\n    try:\n        x = 1 / 0\n    except:\n        return 7\n    finally:\n        print(\"fin\")\n    return 0\n\nprint(f())\nprint(1)\n",
		want: "fin\n7\n1\n",
	},
	{
		name: "value_is_taken_before_the_finally_runs",
		src:  "def f() -> int:\n    n = 1\n    try:\n        return n\n    finally:\n        n = 99\n        print(\"fin\", n)\n\nprint(f())\n",
		want: "fin 99\n1\n",
	},
	{
		name: "finally_containing_a_loop",
		src:  "def f() -> int:\n    try:\n        return 1\n    finally:\n        for i in range(2):\n            print(\"tick\", i)\n\nprint(f())\n",
		want: "tick 0\ntick 1\n1\n",
	},
	{
		name: "finally_containing_an_if",
		src:  "def f() -> int:\n    n = 1\n    try:\n        return n\n    finally:\n        if n == 1:\n            print(\"one\")\n        else:\n            print(\"other\")\n        print(\"after if\")\n\nprint(f())\n",
		want: "one\nafter if\n1\n",
	},
	{
		name: "uncaught_through_two_finallys",
		src:  "def f() -> int:\n    try:\n        try:\n            x = 1 / 0\n        finally:\n            print(\"inner fin\")\n    finally:\n        print(\"outer fin\")\n\ntry:\n    print(f())\nexcept:\n    print(\"top caught\")\n",
		want: "inner fin\nouter fin\ntop caught\n",
	},
	{
		name: "arms_outside_two_finallys",
		src:  "def f() -> int:\n    try:\n        try:\n            x = 1 / 0\n        finally:\n            print(\"inner fin\")\n    finally:\n        print(\"outer fin\")\n\ntry:\n    print(f())\nexcept ZeroDivisionError:\n    print(\"handled\")\nprint(\"after\")\n",
		want: "inner fin\nouter fin\nhandled\nafter\n",
	},
	{
		name: "deferred_in_a_loop_body",
		src:  "for i in range(3):\n    try:\n        print(\"b\", i)\n    finally:\n        print(\"f\", i)\nprint(\"end\")\n",
		want: "b 0\nf 0\nb 1\nf 1\nb 2\nf 2\nend\n",
	},
	{
		name: "bare_return_with_finally",
		src:  "def f():\n    try:\n        return\n    finally:\n        print(\"fin\")\n\nprint(f())\n",
		want: "fin\nNone\n",
	},
	{
		// The control a fix that simply ran deferred bodies everywhere could not pass: an arm
		// that must not run when nothing raised, and a deferred body that must run once.
		name: "arm_does_not_run_without_an_exception",
		src:  "try:\n    print(\"body\")\nexcept:\n    print(\"arm\")\nfinally:\n    print(\"fin\")\n",
		want: "body\nfin\n",
	},
	{
		// The other half of the statement: transfers are not exceptions.
		name: "arm_does_not_catch_a_return",
		src:  "def f() -> int:\n    try:\n        return 1\n    except:\n        print(\"caught a return\")\n    return 3\n\nprint(f())\n",
		want: "1\n",
	},
	{
		name: "arm_does_not_catch_a_break",
		src:  "for i in range(2):\n    try:\n        break\n    except:\n        print(\"caught a break\")\nprint(\"done\")\n",
		want: "done\n",
	},
	{
		// A raise inside the deferred body replaces what was in flight, and the program still
		// dies of it: exit 1, ValueError named.
		name:     "raise_in_finally_replaces_and_traps",
		src:      "try:\n    x = 1 / 0\nexcept ZeroDivisionError:\n    print(\"caught zero\")\nfinally:\n    raise ValueError(\"boom\")\n",
		want:     "caught zero\n",
		wantCode: 3, // the documented runtime-error class (docs/operations.md, ADR 0211), both backends
	},
}

func TestDeferredBodiesMatchCPythonOnBothEngines(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range deferredCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".gy")
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code != tc.wantCode {
					t.Fatalf("%s exited %d, want %d:\n%s", engine, code, tc.wantCode, out)
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want %q", engine, out, tc.want)
				}
			}
		})
	}
}

func TestUncaughtExceptionAfterDeferredBodiesTrapsOnBothEngines(t *testing.T) {
	src := "def f() -> int:\n    try:\n        x = 1 / 0\n    finally:\n        print(\"fin\")\n    return 0\n\nprint(f())\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "trap_after_finally.gy")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliRunCode(t, engine, path)
		if code != 3 {
			t.Fatalf("%s exited %d, want 3 — the documented runtime-error class, the same on both backends (ADR 0211):\n%s", engine, code, out)
		}
		// The traceback is a diagnostic and goes where diagnostics go; the run's own output is
		// `out`, so the message is asserted on the combined stream.
		out = cliRun(t, engine, path)
		if !strings.Contains(out, "fin") {
			t.Fatalf("%s trapped without running the deferred body: %q", engine, out)
		}
		if !strings.Contains(out, "division by zero") {
			t.Fatalf("%s trapped without naming the exception: %q", engine, out)
		}
	}
}

// TestMethodWithTryIsPinnedAsPreExistingCompiledDebt records a compiled hole this cycle's work
// ran into and did not cause: a method containing a `try` emits `br label %` with an empty
// raise-exit target, so `llc` rejects the module and the run exits 2 — verified against the
// binary from before this change, which fails the same way on the same source. The interpreter
// answers it correctly. Recorded as roadmap Gap R.41; delete this test when the shape compiles.
func TestMethodWithTryIsPinnedAsPreExistingCompiledDebt(t *testing.T) {
	src := "class C:\n    def m(self) -> int:\n        try:\n            return 3\n        finally:\n            print(\"m fin\")\n\nc = C()\nprint(c.m())\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "method_try.gy")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, code := cliRunCode(t, "--interp", path)
	if code != 0 || out != "m fin\n3\n" {
		t.Fatalf("the interpreter leg is expected to work and print the deferred line, got (%d) %q", code, out)
	}
	out, code = cliRunCode(t, "--aot", path)
	if code != 2 {
		t.Fatalf("compiled leg exited %d, want the recorded toolchain rejection (2):\n%s", code, out)
	}
	ir, irc := cliRunCode(t, "--emit-llvm", src)
	if irc != 0 {
		t.Fatalf("--emit-llvm exited %d:\n%s", irc, ir)
	}
	if !strings.Contains(ir, fmt.Sprintf("br label %%%s\n", "")) {
		t.Fatalf("the emitted module no longer contains the empty branch target this gap is about:\n%s", ir)
	}
}
