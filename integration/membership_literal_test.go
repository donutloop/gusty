package integration

// Membership asked of a container the SOURCE spells, at the CLI, against the reference.
//
// roadmap Gap R.523 · ADR 0456
//
// One literal, three roads, two answers:
//
//	x = 5
//	print([x])        →  [5]
//	print(len([x]))   →  1
//	print(1 in [x])   →  exit 1: codegen: list literal elements must be integers, not int
//
// The membership road lowered its haystack through the number road, whose answer for a literal is a folded
// `@.lstN` global with `i32` element slots — so an element that is a NAME, a CALL or an EXPRESSION had no
// word to write into one, and a program the reference answers in one line was refused with a sentence about
// an element kind the literal was not asking about. The door these rows pin asks the same container door the
// printer asks and puts the question to the object (`rt_contains_tagged`).
//
// Every expectation is read out of a live `python3` inside the case, so a row this file disagrees with is
// stale rather than green — including the rows the folded payload-only compare used to get WRONG (`None in
// [0]`, which answered `True`) and the one it used to fail to build at all (`[1] in [1, 2]`, which reached
// the untagged scan in a shape `llc` rejected: exit 2, the compiler's own mistake).
//
// The `--emit-llvm` rows are the same claims in the module's own bytes: a haystack the fold serves builds no
// object and asks no runtime scan; a haystack the door builds asks the tagged scan and never the payload-only
// one.

import (
	"strings"
	"testing"
)

// membershipLiteralRowsCLI is the answered family: a haystack written as a list, set or dict whose elements
// are names, calls, expressions, containers — anything a fold could not have written into a global.
func membershipLiteralRowsCLI() []string {
	return []string{
		"x = 5\nprint(1 in [x])\nprint(5 in [x])\nprint(1 not in [x])\n",
		"y = \"a\"\nprint(\"a\" in [y])\nprint(\"b\" in [y])\n",
		"def f():\n    return 5\n\nprint(5 in [f()])\nprint(1 in [f()])\n",
		"x = 4\nprint(5 in [x + 1])\nprint(4 in [x + 1])\n",
		"x = 5\nprint(5 in [1, \"a\", x])\nprint(2 in [1, \"a\", x])\n",
		"a = 1\nb = 2\nprint(b in [a, b])\nprint(9 in [a, b])\n",
		"x = 2.5\nprint(1 in [1, x])\nprint(3 in [1, x])\n",
		"x = 5\nprint(5 in {x})\nprint(1 in {x})\n",
		"a = 1\nb = 2\nprint(b in {a, b})\n",
		"x = 5\nprint(5 in {x: \"v\"})\nprint(7 in {x: \"v\"})\n",
		"k = \"key\"\nprint(\"key\" in {k: 1})\nprint(\"other\" in {k: 1})\n",
		"x = [1]\nprint(x in [x])\nprint([2] in [x])\n",
		"print([1] in [[1], [2]])\n",
		"def g():\n    x = 5\n    print(1 in [x])\n\ng()\n",
		"x = 5\ndef g():\n    print(5 in [x])\n\ng()\n",
		"x = 5\nif 1 in [x]:\n    print(\"yes\")\n\nelse:\n    print(\"no\")\n",
		"n = 0\nwhile n in [0, 1]:\n    print(n)\n    n = n + 1\n",
		"x = 5\nfor v in [x]:\n    print(v)\n",
		// An ELEMENT WHOSE ANSWER IS A CONTAINER: `[f()]` with `def f(): return [1, 2]` — the shape **Gap R.530**
		// was filed with, refused by the folded global in a sentence about an element kind the program had
		// already proven while `print(len([f()]))` beside it compiled (**ADR 0457**).
		"def f():\n    return [1, 2]\n\nprint([1, 2] in [f()])\nprint(1 in [f()])\nprint(f() in [f()])\n",
		"def g():\n    return {\"a\": 1}\n\nprint({\"a\": 1} in [g()])\nprint(\"a\" in [g()])\n",
		"def s():\n    return {1, 2}\n\nprint({1, 2} in [s()])\nprint(1 in [s()])\n",
		"def f():\n    return [1, 2]\n\nprint(7 in [f(), 7])\nprint([f(), 7])\n",
		"def f():\n    return [1, 2]\n\nfor v in [f()]:\n    print(v)\n",
		"def f():\n    return [1, 2]\n\nprint(len([f()]))\nprint(str([f()]))\nprint(repr([f()]))\n",
		// The haystack's elements are evaluated: a print inside one is the program's, not the door's.
		"def probe():\n    print(\"needle\")\n\n    return 5\n\nprint(5 in [probe()])\n",
	}
}

func TestMembershipOfASpelledLiteralHaystackAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, src := range membershipLiteralRowsCLI() {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Fatalf("the reference does not run this program to completion: %q", want)
			}
			p := writeSrc(t, dir, "membership_literal", src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 on the compiler's own module (ADR 0166): %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q\n%s", engine, out, want, src)
				}
			}
		})
	}
}

// membershipFoldBlindRowsCLI is the family the folded unroll answered wrongly or refused: it compares
// payloads alone, so the void was a member of a list of whole numbers, a double was a member of nothing, and
// a container needle reached the untagged scan in a shape `llc` rejected. Each row's expectation is the
// reference's own bytes, read inside the case.
func membershipFoldBlindRowsCLI() []string {
	return []string{
		"print(None in [0])\nprint(None in [1])\n",
		"x = 0\nprint(None in [x])\n",
		"print(1.0 in [1])\nprint(1.5 in [1, 2])\n",
		"x = 2.5\nprint(x in [1, 3])\nprint(x in [1, 2.5])\n",
		"print(\"a\" in [1, 2])\n",
		"print([1] in [1, 2])\nprint([1] in [[1], [2]])\n",
		"print(True in [1])\nprint(True in [0])\n",
	}
}

func TestTheObjectAnswersAtTheCLITheNeedlesTheFoldCannotName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, src := range membershipFoldBlindRowsCLI() {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Fatalf("the reference does not run this program to completion: %q", want)
			}
			p := writeSrc(t, dir, "membership_fold_blind", src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 on the compiler's own module (ADR 0166): %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d where the reference answers %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — the payload-only fold answered where the object should have\n%s", engine, out, want, src)
				}
			}
		})
	}
}

// TestTheFoldedHaystackBuildsNothingAtTheCLI is the door's economy at the CLI: the literal the fold can serve
// still costs the program no object and no runtime scan, so this row did not buy its answers with an
// allocation every ordinary membership test now pays for.
func TestTheFoldedHaystackBuildsNothingAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "x = 2\nprint(1 in [1, 2, 3])\nprint(x in [1, 2, 3])\nprint(4 not in [1, 2, 3])\n"
	want, ok := cpythonPlainOut(t, dir, src)
	if !ok {
		t.Fatalf("the reference does not run this program: %q", want)
	}
	p := writeSrc(t, dir, "membership_folded", src)
	ir, stderr, code := cliStreams(t, "--emit-llvm", src)
	if code != 0 {
		t.Fatalf("--emit-llvm exited %d: %s%s", code, ir, stderr)
	}
	main := inCLIMain(t, ir)
	if strings.Contains(main, "@rt_contains") {
		t.Errorf("a haystack the fold serves still asks a runtime scan:\n%s", main)
	}
	for _, build := range []string{"@rt_alloc(", "@rt_set_elem(", "@rt_tag_elem("} {
		if strings.Contains(main, build) {
			t.Errorf("a haystack the fold serves still builds an object (%s):\n%s", build, main)
		}
	}
	for _, engine := range cliEngines {
		out, code := cliRunMerged(t, engine, "--file", p)
		if code != 0 || out != want {
			t.Fatalf("%s printed %q at exit %d, want %q at exit 0", engine, out, code, want)
		}
	}
}

// TestTheLiteralHaystackDoorAsksTheTaggedScanAtTheCLI is the one-question claim read out of the CLI's own
// module: the literal is built by the same builder the printer asks, the question goes to the tagged scan, and
// the payload-only scan — the answer this row replaces — is not in the program at all.
func TestTheLiteralHaystackDoorAsksTheTaggedScanAtTheCLI(t *testing.T) {
	t.Parallel()
	// The program the answered table runs at exit 0, asked of the CLI's own emitter instead of its runner.
	const src = "x = 5\nprint(1 in [x])\n"
	ir, stderr, code := cliStreams(t, "--emit-llvm", src)
	if code != 0 {
		t.Fatalf("--emit-llvm exited %d: %s%s", code, ir, stderr)
	}
	main := inCLIMain(t, ir)
	if n := strings.Count(main, "@rt_contains_tagged("); n != 1 {
		t.Errorf("the program asks the tagged scan %d times, want 1:\n%s", n, main)
	}
	if strings.Contains(main, "@rt_contains(") {
		t.Errorf("the program still asks the payload-only scan:\n%s", main)
	}
	for _, build := range []string{"@rt_alloc(", "@rt_set_elem(", "@rt_tag_elem("} {
		if !strings.Contains(main, build) {
			t.Errorf("the haystack was not built by the container door (%s is absent):\n%s", build, main)
		}
	}
}

// TestWhatTheLiteralHaystackDoorStillRefusesAtTheCLI pins the residue at the CLI, with the refusal's own
// words and the row that owns each; the rows leave with the rows they name.
func TestWhatTheLiteralHaystackDoorStillRefusesAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct{ name, src, contains, owner string }{
		// A container answer whose kind the slot cannot wear: the object carries the tuple bit, and the outer
		// list's printer has no road that reads it through the slot (**ADR 0361**'s bit, one handle too deep).
		{"a tuple answer as an element", "def t():\n    return (1, 2)\n\nprint([t()])\n", "hands back a container", "Gap R.533"},
		{"an element no door names an answer for", "import math\nprint(1 in [math.sqrt(4)])\n", "list literal elements must be integers", "Gap R.531"},
		{"an element whose kind has no name at all", "print(1 in [lambda: 1])\n", "list literal elements must be integers", "Gap R.531"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Fatalf("row is stale: the reference does not print this program: %q", want)
			}
			p := writeSrc(t, dir, "membership_refusal", r.src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 0 {
					t.Fatalf("%s answered %q — the row that owns this shape (%s) has paid, so move it into the answered table\n%s", engine, out, r.owner, r.src)
				}
				if code == 2 {
					t.Fatalf("%s exited 2 on the compiler's own module (ADR 0166): %s", engine, out)
				}
				if !strings.Contains(out, r.contains) {
					t.Fatalf("%s says %q; the row pins the refusal %s owns\n%s", engine, out, r.owner, r.src)
				}
			}
		})
	}
}
