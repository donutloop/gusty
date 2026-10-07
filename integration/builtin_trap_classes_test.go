package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Gap R.25 (ADR 0214) from the outside: the record must agree with CPython on what the
// handlers *print*, and the shapes the compiled backend will not lower must stay honest refusals —
// a named, non-zero failure, never a substitute value. That second half matters because the
// compiled answer to a missing attribute used to be `0`, which is the difference between a gap
// someone can plan around and a wrong answer nobody can find.

const (
	btcSource = "class P:\n    pass\n\np = P()\n\ntry:\n    print(p.nope)\nexcept AttributeError:\n    print(\"attr ok\")\n\n" +
		"try:\n    print(int(\"abc\"))\nexcept ValueError:\n    print(\"value ok\")\n\n" +
		"try:\n    a, b = [1]\nexcept ValueError:\n    print(\"unpack ok\")\n\n" +
		"try:\n    x = 5\n    x()\nexcept TypeError:\n    print(\"call ok\")\n\n" +
		"try:\n    print(len(5))\nexcept TypeError:\n    print(\"len ok\")\n"
	btcWant = "attr ok\nvalue ok\nunpack ok\ncall ok\nlen ok\n"
)

func TestCompiledHandlesEveryBuiltInTrapLikePython(t *testing.T) {
	got := runCompiled(t, btcSource)
	if got != btcWant {
		t.Errorf("the record leg output =\n%q\nwant\n%q", got, btcWant)
	}
	pyOut, pyErr, perr := lang.PythonRun(btcSource)
	if perr != nil {
		t.Skipf("no usable oracle: %v\n%s", perr, pyErr)
	}
	if pyOut != btcWant {
		t.Errorf("CPython output =\n%q\nwant\n%q (the compiled backend must not diverge on its own handlers)", pyOut, btcWant)
	}
	if pyOut != got {
		t.Errorf("interpreter and CPython disagree:\n%q\n%q", got, pyOut)
	}
}

// An honest refusal is the acceptable failure for what the compiled backend cannot lower: it is
// named, it is non-zero, and it is not an answer. `0` for a missing attribute (Gap R.19) is the
// failure mode this asserts against — a shape that silently *answers* is the bug, whether it
// refuses at compile time or traps at run time.
func TestCompiledBackendRefusesOrTrapsEveryBuiltInTrap(t *testing.T) {
	shapes := []struct {
		name string
		src  string
	}{
		{"int on a bad string", "print(int(\"abc\"))\n"},
		{"unpacking too few", "a, b = [1]\nprint(a)\n"},
		{"len of an int", "print(len(5))\n"},
		{"subscripting an int", "x = 5\nprint(x[0])\n"},
	}
	bin := cliBin(t)
	for _, sh := range shapes {
		if _, err := lang.Compile(sh.src); err != nil {
			// A refusal is fine; a crash or an empty message is not. (What the message
			// lacks today is a stable code — three of these four are bare prose, which is
			// exactly roadmap L11.8's remaining ask, so the assertion is on honesty rather
			// than on a prefix nobody defined.)
			if strings.TrimSpace(err.Error()) == "" {
				t.Errorf("%s: refused with no message at all", sh.name)
			}
			if strings.Contains(err.Error(), "panic") {
				t.Errorf("%s: the refusal arrived as a panic: %v", sh.name, err)
			}
			continue
		}
		out, err := runAOTWithTimeout(t, sh.src, 120*time.Second)
		if err == nil {
			t.Errorf("%s compiled and exited 0 printing %q; it must refuse or trap", sh.name, out)
			continue
		}
		if strings.TrimSpace(out) != "" && !strings.Contains(out, "Error") {
			t.Errorf("%s printed %q and did not name an exception class", sh.name, out)
		}
	}

	// The machine-visible half of an honest refusal: the CLI reports a *compile* failure for
	// these shapes, not a runtime one, so a caller can tell "we could not lower this" from
	// "your program crashed".
	dir := t.TempDir()
	for _, src := range []string{"print(int(\"abc\"))\n", "print(len(5))\n", "x = 5\nprint(x[0])\n"} {
		file := writeTrapCase(t, dir, "refuse.gy", src)
		if code := runCode(t, bin, "--aot", file); code != 1 {
			t.Errorf("gustyc --aot on %q: exit = %d, want 1 (a refusal is the compile class)", src, code)
		}
	}
}

// The Gap R.19 divergence, pinned rather than hidden: the compiled backend has no missing-attribute
// trap yet, so the handler does not run and `print` receives a value. If that ever starts agreeing
// with the record this test stops being a warning and should be deleted along with the gap.
func TestCompiledMissingAttributeIsStillAGap(t *testing.T) {
	src := "class P:\n    pass\n\np = P()\ntry:\n    print(p.nope)\nexcept AttributeError:\n    print(\"caught\")\n"
	got, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		return // refusing is acceptable too
	}
	if got == "caught\n" {
		t.Log("compiled backend now traps on a missing attribute — Gap R.19 looks fixed; delete this test and close the gap")
	}
}
