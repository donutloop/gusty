package integration

// Membership asked of a value with no members raises at the CLI, against the reference.
//
// roadmap Gap R.521 · ADR 0452
//
// Four programs, all of them printing `False` at the exit code that means the program ran, where `python3`
// ends them with a `TypeError`:
//
//	print(1 in 5)          argument of type 'int' is not iterable
//	print(1 in None)       argument of type 'NoneType' is not iterable
//	print("a" in 1.5)      argument of type 'float' is not iterable
//	print(1 in True)       argument of type 'bool' is not iterable
//
// The compiled runtime already answered this correctly for a haystack whose kind arrives at run time —
// `xs = [1]` / `print(1 in xs[0])` raises the reference's own sentence — so the rows below are not asking for
// a new behaviour, they are asking for the one behaviour at the second door. Each expectation is read out of a
// live `python3` inside the case: a row this table disagrees with is stale, not green.

import (
	"strings"
	"testing"
)

func TestMembershipOfAValueWithNoMembersRaisesAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		kind string // the word the reference puts inside `argument of type '…' is not iterable`
	}{
		{"an int haystack", "print(1 in 5)\n", "int"},
		{"the void haystack", "print(1 in None)\n", "NoneType"},
		{"a float haystack", "print(\"a\" in 1.5)\n", "float"},
		{"a verdict haystack", "print(1 in True)\n", "bool"},
		{"the negated question", "print(1 not in 5)\n", "int"},
		{"a chain link asks it", "print(1 == 1 in 5)\n", "int"},
		{"the runtime slot door, which already answered", "xs = [1]\nprint(1 in xs[0])\n", "int"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			sentence := "argument of type '" + r.kind + "' is not iterable"
			// The reference first: it must raise, and it must raise that sentence.
			ref := cpythonRun(t, dir, r.src)
			if !strings.Contains(ref, "TypeError") || !strings.Contains(ref, sentence) {
				t.Fatalf("row is stale: python3 says %q, want %q", ref, sentence)
			}
			if cpythonPlainOutOK(t, dir, r.src) {
				t.Fatalf("row is stale: python3 exited 0 on %q", r.src)
			}
			p := writeSrc(t, dir, "membership", r.src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 0 {
					t.Fatalf("%s answered %q where the reference raises %q", engine, out, sentence)
				}
				if code == 2 {
					t.Fatalf("%s exited 2 on the compiler's own module (ADR 0166): %s", engine, out)
				}
				if code != 3 {
					t.Errorf("%s exited %d, want 3 (the trap's exit class): %s", engine, code, out)
				}
				if !strings.Contains(out, "TypeError") || !strings.Contains(out, sentence) {
					t.Errorf("%s says %q, want the reference's class and sentence %q", engine, out, sentence)
				}
				if strings.Contains(out, "object is not iterable") {
					t.Errorf("%s spoke the LOOP's sentence (%s) — ADR 0271's one-trap-one-wording", engine, out)
				}
			}
		})
	}
}

// TestTheUnanswerableMembershipIsCatchableAtTheCLI is the half that separates a raise from a refusal: the
// program's own handler reaches it, at exit 0, and the exception's `str()` is the reference's sentence.
func TestTheUnanswerableMembershipIsCatchableAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []string{
		"try:\n    print(1 in 5)\nexcept TypeError:\n    print(\"caught\")\n",
		"try:\n    print(\"a\" in 1.5)\nexcept TypeError as e:\n    print(str(e))\n",
		"hits = 0\nfor k in [1, 2]:\n    try:\n        if 1 in 5:\n            hits = hits + 1\n\n    except TypeError:\n        hits = hits + 1\n\nprint(hits)\n",
		"def f():\n    print(\"needle\")\n\n    return 1\n\ntry:\n    print(f() in 5)\nexcept TypeError:\n    print(\"caught\")\nprint(\"still running\")\n",
	}
	for _, src := range rows {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Fatalf("the reference does not run this program to completion: %q", want)
			}
			p := writeSrc(t, dir, "membership_catch", src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q", engine, out, want)
				}
			}
		})
	}
}

// TestMembershipStillAnswersAtTheCLI is the control: the gate must not turn a working operator into a trap.
// Every row here is byte-identical to the reference, on the compiled leg.
func TestMembershipStillAnswersAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []string{
		"print(1 in [1, 2])\nprint(3 not in [1, 2])\n",
		"print(\"b\" in \"abc\")\nprint(\"z\" not in \"abc\")\n",
		"print(1 in {1, 2})\nprint(\"k\" in {\"k\": 1})\n",
		"xs = [10, 20]\nprint(20 in xs)\nprint(10 in (10, 20))\n",
		"xs = [[1], [2]]\nprint([2] in xs)\n",
		"s = {None, 1}\nprint(None in s)\n",
		"xs = [1]\ntry:\n    print(1 in xs[0])\nexcept TypeError as e:\n    print(str(e))\n",
	}
	for _, src := range rows {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Fatalf("the reference does not run this program: %q", want)
			}
			p := writeSrc(t, dir, "membership_ok", src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — a membership answer changed", engine, out, want)
				}
			}
		})
	}
}

// TestTheFoldLeavesNoContainerCallInTheModule is the IR claim at the CLI: the fold replaces the runtime
// container question rather than answering beside it, and the raise it writes is the catchable kind.
func TestTheFoldLeavesNoContainerCallInTheModule(t *testing.T) {
	t.Parallel()
	out, code := cliRunCode(t, "--emit-llvm", "print(1 in 5)\n")
	if code != 0 {
		t.Fatalf("--emit-llvm exited %d: %s", code, out)
	}
	main := out[strings.Index(out, "define i32 @main("):]
	if end := strings.Index(main, "\n}"); end >= 0 {
		main = main[:end]
	}
	if strings.Contains(main, "@rt_contains(") {
		t.Errorf("@main still asks the runtime container door about an int:\n%s", main)
	}
	if !strings.Contains(main, "@exn_flag") {
		t.Errorf("@main carries no non-local raise, so the trap is not catchable:\n%s", main)
	}
	if !strings.Contains(out, "argument of type 'int' is not iterable") {
		t.Errorf("the module does not carry the reference's sentence")
	}
}

// cpythonPlainOutOK reports whether the reference ran a source to a zero exit status.
func cpythonPlainOutOK(t *testing.T, dir, src string) bool {
	t.Helper()
	_, ok := cpythonPlainOut(t, dir, src)
	return ok
}
