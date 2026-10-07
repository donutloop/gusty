package integration

import (
	"testing"
)

// Integration coverage for roadmap Gap R.41 (ADR 0223): a method is a call like any other. Before
// this, a `try` in a method body emitted a branch to an empty label (toolchain rejection, exit 2),
// a `raise` in a method was invisible to its caller (the call returned the unwind value and the
// program carried on printing), and a construct the compiler refuses inside a `def` was silently
// dropped inside a method. The expectations below are what CPython prints for the same source.

func TestMethodExceptionsMatchCPythonOnBothLegs(t *testing.T) {
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

// TestRefusalInsideAMethodIsNotAToolchainRejection separates the two answers a method body can
// get for a construct the compiler cannot lower. the record traps at runtime the way Python
// does (exit 3, our runtime-error class); the compiled backend refuses during codegen (exit 1, a
// compile error) because a constant `[][0]` is folded before it can become a catchable trap --
// that asymmetry is roadmap Gap R.37, asserted here rather than hidden. What must never happen is
// what used to happen: the refusal dropped on the floor, half a function emitted, and exit 2
// blaming the toolchain (ADR 0166, Gap R.41).
func TestRefusalInsideAMethodIsNotAToolchainRejection(t *testing.T) {
	src := "class C:\n    def m(self) -> int:\n        return [][0]\n\nprint(C().m())\n"
	path := writeSrc(t, t.TempDir(), "method_refusal.gy", src)
	// The trap is a runtime event in the reference; the compiled path either traps the same way or
	// refuses the shape at the compile door naming the half it lacks (Gap R.37: a trap the compiler
	// can see coming should still be a runtime trap, and until it is, the refusal is the honest class
	// — never exit 2, and never an answer).
	if out, code := cliRunMerged(t, "--aot", path); code != 3 {
		if code == 1 && refusesHonestly(out) {
			noteCompiledGap(t, src, out)
		} else {
			t.Fatalf("compiled exited %d, want 3 (a runtime trap) or an honest refusal naming the missing half (Gap R.37):\n%s", code, out)
		}
	}
	out, code := cliRunCode(t, "--aot", path)
	if code == 2 {
		t.Fatalf("the compiled leg reported a toolchain rejection for a source error (Gap R.41):\n%s", out)
	}
	if code != 1 {
		t.Fatalf("compiled exit %d, want 1 (a compile error naming the refusal); Gap R.37 covers why it is not a trap:\n%s", code, out)
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
