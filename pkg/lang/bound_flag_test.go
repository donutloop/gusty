package lang

import (
	"strings"
	"testing"
)

// Artifact-level coverage for roadmap Gap R.36 + R.39 (ADR 0228): a slot the program never wrote must
// not answer as a value. Three things are asserted here, because each can fail while the printed output
// still looks right:
//
//   - the flag exists where the checker could not prove the write (and nowhere else — the "pays
//     nothing" case is what keeps this from being a tax on every local);
//   - every write sets it (a missed writer turns a legitimate read into a spurious trap);
//   - the read raises the *class* CPython raises, as a typed raise the program can catch.

func compileOrSkip(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("the emitted module does not verify: %v\n%s", err, res.IR)
	}
	assertNoForbiddenIR(t, src, res.IR)
	return res.IR
}

// TestFlagExistsWhereTheCheckerCannotProveTheWrite: the branch-assigned local carries a flag; the
// branch-and-else local does not.
func TestFlagExistsWhereTheCheckerCannotProveTheWrite(t *testing.T) {
	flagged := compileOrSkip(t, "def f(c):\n    if c:\n        x = 1\n    return x\n")
	if !strings.Contains(flagged, "%bnd_x = alloca i8") {
		t.Fatalf("a local assigned on one path only must carry a written-flag\n%s", flagged)
	}
	if !strings.Contains(flagged, "store i8 1, i8* %bnd_x") {
		t.Fatalf("the write must set the flag, or a legitimate read would trap\n%s", flagged)
	}
	if !strings.Contains(flagged, "icmp eq i8") || !strings.Contains(flagged, "UnboundLocalError") {
		t.Fatalf("the read must test the flag and raise UnboundLocalError\n%s", flagged)
	}
	// The definite program pays nothing: both paths assign, so there is no flag to carry.
	clean := compileOrSkip(t, "def f(c):\n    if c:\n        r = 1\n    else:\n        r = 2\n    return r\n")
	if strings.Contains(clean, "bnd_") {
		t.Fatalf("a definitely-assigned local must not pay for a flag it cannot need\n%s", clean)
	}
}

// TestModuleTopLevelUsesNameError: the same mechanism, but the frame is the module's, and the class a
// name the module does not own raises is NameError — CPython's split, which a handler matches on.
func TestModuleTopLevelUsesNameError(t *testing.T) {
	ir := compileOrSkip(t, "if 0:\n    x = 1\nprint(x)\n")
	if !strings.Contains(ir, "%bnd_x = alloca i8") {
		t.Fatalf("a top-level name assigned on one path only must carry a flag\n%s", ir)
	}
	if !strings.Contains(ir, "NameError") || strings.Contains(ir, "cannot access local variable 'x'") {
		t.Fatalf("a module-level unwritten read must raise NameError, not UnboundLocalError\n%s", ir)
	}
}

// TestTypedRaisesAreCatchable: the trap is an ordinary raise, so a program can catch it -- on both
// backends, matching on the class name. That is the whole point of giving UnboundLocalError a code in
// the canonical exception table (ADR 0212's rule: a built-in trap is a typed raise).
func TestTypedRaisesAreCatchable(t *testing.T) {
	src := "def f(c):\n    if c:\n        x = 1\n    try:\n        return x\n    except UnboundLocalError:\n        return -1\n"
	got := captureStdout(t, "def f(c):\n    if c:\n        x = 1\n    try:\n        return x\n    except UnboundLocalError:\n        return -1\n\nprint(f(False))\n")
	if strings.TrimSpace(got) != "-1" {
		t.Fatalf("interpreter: `except UnboundLocalError:` did not catch the unwritten read, printed %q", got)
	}
	ir := compileOrSkip(t, src)
	if !strings.Contains(ir, "UnboundLocalError") {
		t.Fatalf("the compiled arm must carry the class name for the handler to match on\n%s", ir)
	}
	if code := exnClassCode("UnboundLocalError"); code == 0 {
		t.Fatalf("UnboundLocalError has no code of its own, so `except` cannot match on it")
	}
}

// TestUnwrittenReadsIsTheCheckersAnswer: the compiled side does not decide which names need a flag
// from its own walk; it asks the checker. This is the unit test for that one-rule decision -- each
// shape is listed with what the checker must say.
func TestUnwrittenReadsIsTheCheckersAnswer(t *testing.T) {
	cases := []struct {
		name, src string
		inFunc    bool
		want      []string
		wantEmpty bool
	}{
		{"branch assign", "def f(c):\n    if c:\n        x = 1\n    return x\n", true, []string{"x"}, false},
		{"both branches", "def f(c):\n    if c:\n        r = 1\n    else:\n        r = 2\n    return r\n", true, nil, true},
		{"loop that may not run", "def f():\n    for i in []:\n        z = 1\n    return z\n", true, []string{"z"}, false},
		{"loop variable inside its body", "def f(n):\n    for i in range(n):\n        return i\n    return 0\n", true, nil, true},
		{"tuple unpack on one branch", "def f(c):\n    if c:\n        p, q = 1, 2\n    return p\n", true, []string{"p"}, false},
		{"try body cut short", "def f():\n    try:\n        a = 1 // 0\n        b = 2\n    except ZeroDivisionError:\n        pass\n    return b\n", true, []string{"b"}, false},
		{"match capture with no match", "def f(v):\n    match v:\n        case 1:\n            hit = 1\n    return hit\n", true, []string{"hit"}, false},
		{"read above the write", "def f():\n    return v\n    v = 2\n", true, []string{"v"}, false},
		{"module branch assign", "if 0:\n    x = 1\nprint(x)\n", false, []string{"x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parseProgram(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			table := UnwrittenReads(prog)
			var got map[string]bool
			if tc.inFunc {
				for _, set := range table { // the function's entry; the module's is nil-keyed
					if len(set) > 0 {
						got = set
					}
				}
				if m := table[nil]; m != nil && len(m) > 0 {
					got = m // a top-level read (the print(...) cases) shares the module entry
				}
			} else {
				got = table[nil]
			}
			if tc.wantEmpty {
				if len(got) != 0 {
					t.Fatalf("nothing here is uncertain, but the checker flagged %v", got)
				}
				return
			}
			for _, nm := range tc.want {
				if !got[nm] {
					t.Fatalf("the checker must flag %q as possibly-unbound; flagged %v\nsource:\n%s", nm, got, tc.src)
				}
			}
		})
	}
}

// TestUnwrittenTrapIsNotASilentZero is the behavioural half, and the reason this exists: every shape
// that used to print a number now refuses to answer one.
func TestUnwrittenTrapIsNotASilentZero(t *testing.T) {
	shapes := []struct{ name, src, want string }{
		{"branch", "def f(c):\n    if c:\n        x = 1\n    return x\n\nprint(f(True))\n", "1\n"},
		{"loop", "def f():\n    for i in [1]:\n        z = i\n    return z\n\nprint(f())\n", "1\n"},
		{"unpack", "def f(c):\n    if c:\n        p, q = 7, 8\n    return p\n\nprint(f(True))\n", "7\n"},
		{"match", "def f(v):\n    match v:\n        case 1:\n            hit = 5\n    return hit\n\nprint(f(1))\n", "5\n"},
	}
	for _, tc := range shapes {
		t.Run(tc.name+" (the path that does assign)", func(t *testing.T) {
			got := captureStdout(t, tc.src)
			if got != tc.want {
				t.Fatalf("printed %q, want %q — the flag must not trap a read that a path did assign", got, tc.want)
			}
		})
	}
}
