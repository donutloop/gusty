package integration

// Membership asked of a kind the SOURCE promises raises at the CLI, against the reference.
//
// roadmap Gap R.522 · ADR 0453
//
// ADR 0452 paid the haystack the source spells (`print(1 in 5)`). This file asks the haystack the source only
// promises — a NAME whose one and only binder is a top-level assignment of a literal with no members, and a
// CALL every `return` road of which answers such a literal:
//
//	x = 5                        def f():
//	print(1 in x)             →      return 5
//	False, at exit 0                 print(1 in f())
//	                                 False, at exit 0
//
// Every expectation is read out of a live `python3` inside the case, so a row this table disagrees with is
// stale rather than green — including the trap rows, whose sentence the reference writes and this file never
// copies, and the control rows, which are the reason the door is allowed to exist: a raise where the reference
// answers is a worse defect than the `False` this row was filed for.
//
// The `--emit-llvm` rows are the same claim in the module's own bytes: for a promised kind the compiled leg
// asks no container door and carries the reference's sentence; for a haystack the door declines, the sentence
// is not in the module at all.

import (
	"strings"
	"testing"
)

// inCLIMain is the body of the `define i32 @main(...)` the CLI emitted — the scope an IR claim about THIS
// program's code has to be made in. The module as a whole carries the runtime's four membership sentences for
// any program that asks a container at run time (`pkg/lang/membership_fold_test.go` asserts that as the
// one-table claim), so a module-wide "the raise is not here" is a claim about the runtime, not about the
// program. `pkg/lang` has the same reader as `inMainBody`; this is the CLI's copy of it, and the two files
// share the claim they make rather than each inventing one.
func inCLIMain(t *testing.T, ir string) string {
	t.Helper()
	i := strings.Index(ir, "define i32 @main(")
	if i < 0 {
		t.Fatalf("the module has no @main:\n%s", ir)
	}
	main := ir[i:]
	if end := strings.Index(main, "\n}"); end >= 0 {
		main = main[:end]
	}
	return main
}

// membershipPromiseRowsCLI is the family the door reaches. `kind` is the word the reference puts inside
// `argument of type '…' is not iterable`; the row fails if the reference stops writing it, which is the only
// way a row can be shown to be stale rather than believed.
func membershipPromiseRowsCLI() []struct{ name, src, kind string } {
	return []struct{ name, src, kind string }{
		{"a name bound to a whole number", "x = 5\nprint(1 in x)\n", "int"},
		{"a name bound to the void", "x = None\nprint(1 in x)\n", "NoneType"},
		{"a name bound to a double", "x = 1.5\nprint(\"a\" in x)\n", "float"},
		{"a name bound to a verdict", "x = True\nprint(1 in x)\n", "bool"},
		{"the negated question, asked of a name", "x = 5\nprint(1 not in x)\n", "int"},
		{"a chain link asks it of a name", "x = 5\nprint(1 == 1 in x)\n", "int"},
		{"the same question inside a loop", "x = 5\nfor i in range(2):\n    print(i in x)\n", "int"},
		{"a call every road of answers a number", "def f():\n    return 5\n\nprint(1 in f())\n", "int"},
		{"both arms of a call answer the same kind", "def f(c):\n    if c:\n        return 1\n\n    return 2\n\nprint(1 in f(1))\n", "int"},
		{"a call every road of answers the void", "def f():\n    print(\"side\")\n\nprint(1 in f())\n", "NoneType"},
		{"a call that answers a double", "def f():\n    return 1.5\n\nprint(\"a\" in f())\n", "float"},
		{"a name bound from a call's answer", "def f():\n    return 5\n\nx = f()\nprint(1 in x)\n", "int"},
		{"a builtin's answer", "xs = [1, 2]\nprint(1 in len(xs))\n", "int"},
		{"a conversion's answer", "print(1 in int(\"5\"))\n", "int"},
		{"a module name read from inside a body", "x = 5\ndef g():\n    print(1 in x)\n\ng()\n", "int"},
		{"the same name two frames down", "x = 5\ndef outer():\n    def inner():\n        print(1 in x)\n\n    inner()\n\nouter()\n", "int"},
		{"a name bound inside the body that asks it", "def g():\n    x = 5\n    print(1 in x)\n\ng()\n", "int"},
	}
}

func TestMembershipOfAPromisedKindRaisesAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, r := range membershipPromiseRowsCLI() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			sentence := "argument of type '" + r.kind + "' is not iterable"
			// The reference first, inside the case: it must fail, and fail with that sentence.
			ref := cpythonRun(t, dir, r.src)
			if !strings.Contains(ref, "TypeError") || !strings.Contains(ref, sentence) {
				t.Fatalf("row is stale: python3 says %q, want the trap %q", ref, sentence)
			}
			if cpythonPlainOutOK(t, dir, r.src) {
				t.Fatalf("row is stale: python3 exited 0 on %q", r.src)
			}
			p := writeSrc(t, dir, "membership_promise", r.src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 0 {
					t.Fatalf("%s answered %q where the reference raises %q\n%s", engine, out, sentence, r.src)
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
					t.Errorf("%s spoke the LOOP's sentence — ADR 0271's one-trap-one-wording: %s", engine, out)
				}
				if strings.Contains(out, "False") {
					t.Errorf("%s printed a verdict AND raised: %s", engine, out)
				}
			}
		})
	}
}

// TestTheCallUnderAMembershipRaiseStillRunsAtTheCLI is the order the reference uses, asked of the half this
// row pays: the haystack is an expression, and the raise replaces the ANSWER and never the call. A leg that
// folded the raise into the compile-time constant would run the reference's side effect and this file would
// never notice, because every other row here traps before printing anything.
func TestTheCallUnderAMembershipRaiseStillRunsAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{
			"the haystack's call prints before the raise",
			"def probe():\n    print(\"haystack ran\")\n\n    return 5\n\nprint(1 in probe())\n",
			"haystack ran",
		},
		{
			"a call that prints and falls off its end",
			"def quiet():\n    print(\"quiet ran\")\n\nprint(1 in quiet())\n",
			"quiet ran",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			ref := cpythonRun(t, dir, r.src)
			if !strings.Contains(ref, r.want) || !strings.Contains(ref, "not iterable") {
				t.Fatalf("row is stale: python3 says %q, want %q then the raise", ref, r.want)
			}
			p := writeSrc(t, dir, "membership_order", r.src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 0 {
					t.Fatalf("%s answered where the reference raises: %s", engine, out)
				}
				if !strings.Contains(out, r.want) {
					t.Errorf("%s lost the effect the reference performs (%q) — the raise replaced the answer and the call too: %s", engine, r.want, out)
				}
				if n := strings.Count(out, r.want); n != 1 {
					t.Errorf("%s ran the haystack %d times, want 1: %s", engine, n, out)
				}
				if !strings.Contains(out, "not iterable") {
					t.Errorf("%s raised no membership sentence: %s", engine, out)
				}
			}
		})
	}
}

// TestTheMembershipPromiseRaiseIsCatchableAtTheCLI is the half that makes this a RAISE and not a refusal: the
// program's own handler reaches it, at the program's own exit 0, and `str(e)` is the reference's sentence —
// read from the reference, in the case, through the reference's own traceback.
func TestTheMembershipPromiseRaiseIsCatchableAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []string{
		"x = 5\ntry:\n    print(1 in x)\nexcept TypeError:\n    print(\"caught\")\nprint(\"still running\")\n",
		"x = 1.5\ntry:\n    print(\"a\" in x)\nexcept TypeError as e:\n    print(str(e))\n",
		"def f():\n    return 5\n\ntry:\n    print(1 in f())\nexcept TypeError as e:\n    print(str(e))\n",
		"hits = 0\nx = 5\nfor k in [1, 2]:\n    try:\n        if 1 in x:\n            hits = hits + 1\n\n    except TypeError:\n        hits = hits + 1\n\nprint(hits)\n",
	}
	for _, src := range rows {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			// The reference's handler runs too — and where a row asks for `str(e)`, the reference's own
			// traceback supplies the sentence, so this file copies no words of its own.
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Fatalf("the reference does not run this program to completion: %q", want)
			}
			p := writeSrc(t, dir, "membership_catch", src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference runs and prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q", engine, out, want)
				}
			}
		})
	}
}

// TestMembershipStillAnswersAtTheCLIAcrossAPromise is the control table, and the load-bearing one: every row
// is a program whose haystack is a NAME, and the reference runs it to completion. A door that raised at any of
// them would turn a working operator into a trap, which is the trade this row is not allowed to make. Each
// expectation is the reference's own bytes, read inside the case.
func TestMembershipStillAnswersAtTheCLIAcrossAPromise(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []string{
		// The promise retires where the program writes the name again.
		"x = 5\nx = [1]\nprint(1 in x)\n",
		"x = [1]\nif 1 > 2:\n    x = 5\n\nprint(1 in x)\n",
		"x = 5\ndef g():\n    x = [1, 2]\n\n    return 1 in x\n\nprint(g())\n",
		// The ordinary answers, none of which the door may touch.
		"x = [1, 2]\nprint(1 in x)\nprint(3 not in x)\n",
		"x = \"hello\"\nprint(\"ell\" in x)\nprint(\"zz\" in x)\n",
		"d = {\"a\": 1}\nprint(\"a\" in d)\nprint(\"b\" in d)\n",
		"s = {1, 2}\nprint(1 in s)\nprint(5 in s)\n",
		"t = (1, 2)\nprint(1 in t)\n",
		"xs = []\nxs.append(3)\nprint(3 in xs)\n",
		"def g():\n    x = [1]\n\n    return x\n\nprint(1 in g())\n",
		"x = 5\nprint(x in [5])\n",
	}
	for _, src := range rows {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Fatalf("the reference does not run this program: %q", want)
			}
			p := writeSrc(t, dir, "membership_answer", src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — a membership answer changed, or the door raised where the reference answers", engine, out, want)
				}
			}
		})
	}
}

// TestTheMembershipDoorLeavesNoContainerCallInTheModule is the same claim at the CLI in the module's own bytes:
// for a promised kind the door replaces the runtime container question rather than answering beside it, and for
// a haystack it declines the sentence is not in the module at all.
func TestTheMembershipDoorLeavesNoContainerCallInTheModule(t *testing.T) {
	t.Parallel()
	t.Run("a name the program settles asks no container door", func(t *testing.T) {
		t.Parallel()
		out, code := cliRunCode(t, "--emit-llvm", "x = 5\nprint(1 in x)\n")
		if code != 0 {
			t.Fatalf("--emit-llvm exited %d: %s", code, out)
		}
		main := out[strings.Index(out, "define i32 @main("):]
		if end := strings.Index(main, "\n}"); end >= 0 {
			main = main[:end]
		}
		if strings.Contains(main, "@rt_contains(") {
			t.Errorf("@main still asks the runtime container door about a promised int:\n%s", main)
		}
		if !strings.Contains(main, "@exn_flag") {
			t.Errorf("@main carries no non-local raise, so the trap is not catchable:\n%s", main)
		}
		if !strings.Contains(out, "argument of type 'int' is not iterable") {
			t.Errorf("the module does not carry the reference's sentence")
		}
	})
	t.Run("a name the program writes again carries no raise in its own block", func(t *testing.T) {
		t.Parallel()
		// Scoped to `@main`, not to the module: the runtime membership door carries all four sentences for
		// every program that asks a container at run time (`TestTheFoldAndTheRuntimeObjectDoorReadOneTable`
		// asserts that in `pkg/lang`), so a module-wide claim here would be a claim about the runtime.
		out, code := cliRunCode(t, "--emit-llvm", "x = [1]\nprint(1 in x)\n")
		if code != 0 {
			t.Fatalf("--emit-llvm exited %d: %s", code, out)
		}
		if strings.Contains(inCLIMain(t, out), "argument of type") {
			t.Errorf("the membership raise is in a program whose haystack has members:\n%s", inCLIMain(t, out))
		}
	})
	t.Run("a call the program re-points at another value carries no raise", func(t *testing.T) {
		t.Parallel()
		// The call door answers from the module `def`'s roads; a program that re-binds the name has taken
		// that promise away, and the door says so by emitting no raise at all — it does not raise and then
		// explain. What the program ANSWERS (`False`, where the reference raises) is the residue, pinned with
		// its own bytes in `pkg/lang/membership_name_test.go`; this row only claims the module is quiet.
		out, code := cliRunCode(t, "--emit-llvm", "def f():\n    return 5\n\nf = lambda: 5\n\nprint(1 in f())\n")
		if code != 0 {
			t.Fatalf("--emit-llvm exited %d: %s", code, out)
		}
		if strings.Contains(inCLIMain(t, out), "argument of type") {
			t.Errorf("the call door raised for a callee the program re-pointed:\n%s", inCLIMain(t, out))
		}
	})
}
