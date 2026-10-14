package integration

import (
	"strings"

	"testing"
)

// Integration coverage for roadmap Gap R.41 (ADR 0223): a method is a call like any other. Before
// this, a `try` in a method body emitted a branch to an empty label (toolchain rejection, exit 2),
// a `raise` in a method was invisible to its caller (the call returned the unwind value and the
// program carried on printing), and a construct the compiler refuses inside a `def` was silently
// dropped inside a method. The expectations below are what CPython prints for the same source.

func TestMethodExceptionsMatchCPythonOnBothLegs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		src      string
		want     string
		wantCode int
	}{
		{
			name: "deferred_body_in_a_method",
			src:  "class C:\n    def m(self) -> int:\n        try:\n            return 3\n        finally:\n            print(\"m fin\")\n\nprint(C().m())\n",
			want: "m fin\n3\n",
		},
		{
			name: "arm_in_a_method",
			src:  "class C:\n    def m(self) -> int:\n        try:\n            x = 1 / 0\n        except:\n            print(\"handled\")\n        return 4\n\nprint(C().m())\n",
			want: "handled\n4\n",
		},
		{
			name:     "raise_out_of_a_method_reaches_the_caller",
			src:      "class C:\n    def m(self) -> int:\n        raise ValueError(\"boom\")\n\ntry:\n    print(C().m())\nexcept ValueError:\n    print(\"caught value error\")\nexcept:\n    print(\"caught something else\")\n",
			want:     "caught value error\n",
			wantCode: 0,
		},
		{
			name: "nested_self_call_raises",
			src:  "class C:\n    def bad(self) -> int:\n        x = 1 / 0\n        return 0\n\n    def m(self) -> int:\n        return self.bad()\n\ntry:\n    print(C().m())\nexcept:\n    print(\"caught in caller\")\n",
			want: "caught in caller\n",
		},
		{
			name: "break_through_a_method_finally",
			src:  "class C:\n    def m(self) -> int:\n        for i in range(3):\n            try:\n                break\n            finally:\n                print(\"fin\")\n        return 9\n\nprint(C().m())\n",
			want: "fin\n9\n",
		},
		{
			name: "constructor_raises_is_caught",
			src:  "class Q:\n    def __init__(self, v: int):\n        if v == 0:\n            raise ValueError(\"zero\")\n        self.v = v\n\ntry:\n    q = Q(0)\n    print(\"made\")\nexcept:\n    print(\"init raised\")\n\nq2 = Q(5)\nprint(q2.v)\n",
			want: "init raised\n5\n",
		},
		{
			// The control: a method that never raises must still return normally after the
			// call site started reading the exception flag on every call.
			name: "ordinary_method_call_after_the_check",
			src:  "class Counter:\n    def __init__(self):\n        self.n = 0\n\n    def bump(self) -> int:\n        self.n = self.n + 1\n        return self.n\n\nc = Counter()\nfor i in range(5):\n    c.bump()\nprint(c.n)\n",
			want: "5\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method.gy", tc.src)
			for _, engine := range cliEngines {
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

// TestTrapInsideAMethodIsNotAToolchainRejection separates the two answers a method body can get for a
// construct the compiler cannot lower. A construct it CAN lower but the reference refuses to answer is
// a runtime trap: exit 3, our runtime-error class, the class the record names — which is what a constant
// `[][0]` is since ADR 0326 closed roadmap Gap R.37 (it used to be folded into a compile-time refusal,
// a different language event that no `except IndexError:` could catch). What must never happen is what
// used to happen: the failure dropped on the floor, half a function emitted, and exit 2 blaming the
// toolchain (ADR 0166, Gap R.41).
func TestTrapInsideAMethodIsNotAToolchainRejection(t *testing.T) {
	t.Parallel()
	src := "class C:\n    def m(self) -> int:\n        return [][0]\n\nprint(C().m())\n"
	path := writeSrc(t, t.TempDir(), "method_trap.gy", src)
	out, code := cliRunMerged(t, "--aot", path)
	if code != 3 {
		t.Fatalf("compiled exited %d, want 3 (a runtime trap); Gap R.37 is closed, so a refusal here means the fold stopped trapping:\n%s", code, out)
	}
	if !strings.Contains(out, "IndexError") {
		t.Fatalf("the compiled leg raised without naming the class the reference raises:\n%s", out)
	}
	// The reference dies on the same program, which is the whole content of the row.
	if _, pyCode := oracleTrap(t, path); pyCode == 0 {
		t.Fatalf("the oracle answered a program both other legs die on")
	}
	if out, code := cliRunCode(t, "--aot", path); code == 2 {
		t.Fatalf("the compiled leg reported a toolchain rejection for a source error (Gap R.41):\n%s", out)
	}
}

// TestRefusalInsideAMethodIsNotAToolchainRejection is the half of the claim that still is a refusal: a
// construct with no lowering at all stops the program at the compile door, in words, at exit 1 — never
// at the toolchain's own exit 2 (ADR 0166, Gap R.41).
func TestRefusalInsideAMethodIsNotAToolchainRejection(t *testing.T) {
	t.Parallel()
	src := "class C:\n    def m(self) -> int:\n        return len(5)\n\nprint(C().m())\n"
	path := writeSrc(t, t.TempDir(), "method_refusal.gy", src)
	if out, code := cliRunMerged(t, "--aot", path); code != 1 || !refusesHonestly(out) {
		t.Fatalf("compiled exit %d, want 1 (a compile error naming the refusal):\n%s", code, out)
	}
	out, code := cliRunCode(t, "--aot", path)
	if code == 2 {
		t.Fatalf("the compiled leg reported a toolchain rejection for a source error (Gap R.41):\n%s", out)
	}
	combined := cliRun(t, "--aot", path)
	if combined == "" {
		t.Fatalf("the compiled leg refused without saying why")
	}
}

// TestUncaughtRaiseFromMethodIsRuntimeClass asserts the exit-code contract rather than CPython's
// number: an uncaught exception is the runtime-error class on the compiled path (ADR 0211), and a
// method raising is no exception to that.
func TestUncaughtRaiseFromMethodIsRuntimeClass(t *testing.T) {
	t.Parallel()
	src := "class C:\n    def m(self) -> int:\n        raise ValueError(\"boom\")\n\nprint(C().m())\n"
	path := writeSrc(t, t.TempDir(), "method_raise.gy", src)
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, path)
		if code != 3 {
			t.Fatalf("%s exited %d, want 3 (the documented runtime-error class):\n%s", engine, code, out)
		}
		combined := cliRun(t, engine, path)
		if combined == "" {
			t.Fatalf("%s trapped without reporting anything", engine)
		}
	}
}
