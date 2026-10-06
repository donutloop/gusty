package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Integration coverage for the compiled half of Gap R.21 (ADR 0213's rule, ADR 0218).
//
// @exn_flag is one module-wide bit meaning "an exception is in flight, unhandled". The compiled
// backend never cleared it on the handled path, so a program that caught an exception kept
// announcing it: the next user-function call's check branched back to the handler — or, with no
// handler left in scope, straight to the raise-exit — and the program died reporting an exception it
// had already handled. The interpreter has cleared it since cycle 161; these are the programs that
// were right under --aot and dead under --aot.

func TestHandledExceptionDoesNotReturnOnCompiledLeg(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			// The minimal repro: a call after the try decides whether the bug shows.
			"module try, user call afterwards",
			"try:\n    crash = 1 // 0\nexcept:\n    recovered = 1\n\ndef f() -> int:\n    return 5\n\nprint(recovered, f())\n",
			"1 5\n",
		},
		{
			"function try, call afterwards",
			"def f() -> int:\n    try:\n        z = 1 // 0\n    except:\n        pass\n    return 41\n\nprint(f())\n",
			"41\n",
		},
		{
			// Proves the arm really ran: its own output appears, and nothing dies after it.
			"arm that prints, then the function returns",
			"def f() -> int:\n    try:\n        z = 1 // 0\n    except:\n        print(\"handled\")\n    return 41\n\nprint(f())\n",
			"handled\n41\n",
		},
		{
			"a user raise inside a function, caught by its own arm",
			"def f() -> int:\n    try:\n        raise ValueError(\"x\")\n    except ValueError:\n        pass\n    return 1\n\nprint(f())\n",
			"1\n",
		},
		{
			// The exception escapes the inner arm and the module-level arm catches it — and a
			// later call must still be safe.
			"unhandled inside, handled outside",
			"def g() -> int:\n    try:\n        z = 1 // 0\n    except KeyError:\n        pass\n    return 5\n\ntry:\n    v = g()\nexcept ZeroDivisionError:\n    v = 99\nprint(v)\n",
			"99\n",
		},
		{
			// The arm leaves by `return`: the transfer escapes the clear at the arm's end,
			// which is why the return itself has to clear.
			"arm that returns",
			"def f() -> int:\n    try:\n        z = 1 // 0\n    except:\n        return 42\n    return 1\n\nprint(f(), f())\n",
			"42 42\n",
		},
		{
			"second arm matches, and it returns",
			"def f() -> int:\n    try:\n        z = 1 // 0\n    except KeyError:\n        return 11\n    except ZeroDivisionError:\n        return 22\n    return 33\n\ndef h() -> int:\n    return 1\n\nprint(h(), f())\n",
			"1 22\n",
		},
		{
			"a try handled inside a loop, calls afterwards",
			"n = 0\nwhile n < 3:\n    try:\n        q = 1 // 0\n    except:\n        pass\n    n = n + 1\n\ndef d() -> int:\n    return 6\n\nprint(d(), n)\n",
			"6 3\n",
		},
		{
			"finally after a handled exception, then a call",
			"def f() -> int:\n    try:\n        z = 1 // 0\n    except:\n        pass\n    finally:\n        w = 3\n    return w\n\ndef g() -> int:\n    return 4\n\nprint(g(), f())\n",
			"4 3\n",
		},
		{
			// break/continue out of an arm are the same transfer-in-a-different-dress.
			"break out of an arm",
			"i = 0\nwhile i < 5:\n    try:\n        z = 1 // 0\n    except:\n        i = i + 100\n        break\n    i = i + 1\n\ndef f() -> int:\n    return 7\n\nprint(f(), i)\n",
			"7 100\n",
		},
		{
			// A raise inside an arm is NOT handled by that arm: it must still travel outward.
			// This is the assertion that keeps the clear from becoming a mute.
			"raise from inside an arm propagates",
			"def f() -> int:\n    try:\n        z = 1 // 0\n    except:\n        raise ValueError(\"from the arm\")\n    return 1\n\ntry:\n    f()\nexcept ValueError:\n    print(\"outer caught it\")\n",
			"outer caught it\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iout := runInterp(t, tc.src)
			if iout != tc.want {
				t.Fatalf("interpreter printed %q, want %q", iout, tc.want)
			}
			aout, err := runAOTWithTimeout(t, tc.src, 60*time.Second)
			if err != nil {
				t.Fatalf("compiled program failed (a resurrected exception is the likely cause): %v\n%s", err, aout)
			}
			if aout != tc.want {
				t.Fatalf("compiled program printed %q, want %q", aout, tc.want)
			}
			pout, perrText, perr := lang.PythonRun(tc.src)
			if perr != nil {
				t.Fatalf("CPython disagreed with the expectation %q: %v\n%s", tc.want, perr, perrText)
			}
			if pout != tc.want {
				t.Fatalf("CPython printed %q, want %q — the expectation itself is wrong", pout, tc.want)
			}
		})
	}
}

// TestUncaughtTrapStillTrapsOnBothBackends is the control the positive cases cannot provide: a fix
// that merely silenced the flag everywhere would pass every one of them. Here nothing handles the
// exception, so the compiled path must report it, print nothing, and fail.
func TestUncaughtTrapStillTrapsOnBothBackends(t *testing.T) {
	src := "def half() -> int:\n    try:\n        z = 1 // 0\n    except:\n        pass\n    return 1\n\ndef boom() -> int:\n    return 7 // 0\n\na = half()\nb = boom()\nprint(a, b)\n"
	// The first call's exception IS handled; the second's is not. If the clear had become a mute,
	// this program would print "1 0" or similar instead of dying.
	path := filepath.Join(t.TempDir(), "uncaught.gy")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if iout, code := cliRunCode(t, "--file", path); iout != "" || code == 0 {
		t.Fatalf("interpreter leg printed %q with exit %d; nothing handles this exception", iout, code)
	}
	if combined := cliRun(t, "--file", path); !strings.Contains(combined, "ZeroDivisionError") {
		t.Fatalf("interpreter did not report ZeroDivisionError: %s", combined)
	}
	aout, aerr := runAOTWithTimeout(t, src, 60*time.Second)
	if aerr == nil {
		t.Fatalf("compiled program exited 0 on an unhandled exception: %q", aout)
	}
	if !strings.Contains(aout, "ZeroDivisionError") {
		t.Fatalf("compiled program did not report ZeroDivisionError: %s", aout)
	}
	if strings.Contains(aout, "1") {
		t.Fatalf("compiled program printed %q; the uncaught call comes before any output", aout)
	}
	_, pyErrText, perr := lang.PythonRun(src)
	if perr == nil {
		t.Fatalf("CPython accepted an unhandled exception — check the program")
	}
	if !strings.Contains(pyErrText, "ZeroDivisionError") {
		t.Fatalf("CPython reported something else: %s", pyErrText)
	}
}
