package lang

import (
	"strings"
	"testing"
)

// slice_answer_test.go — the slice door answers a copy of the base's slots *with their tags*, wherever the
// base lives (roadmap Gap R.230, ADR 0335).
//
// `xs[i:j]` had one lowering for every base: copy the base's *words* into a new heap list. For a list whose
// slots are all int that is the whole answer. For a list whose slots describe themselves — a text slot (a
// word that is an `@str_tab` index), a float slot (a word that is a box handle), a nested container (a word
// that is a heap handle) — the copy carried words with no tag beside them, and every read of the copy then
// read the word as the thing itself:
//
//	xs = [1.5, "a", [7]]
//	print(xs[0:2])      # CPython: [1.5, 'a']
//	                    # this toolchain before this row: [1, 0]   (exit 0 — a wrong number, not a refusal)
//	ys = xs[0:2]
//	print(ys)           # CPython: [1.5, 'a']
//	                    # this toolchain before this row: 1        (the handle, printed as a number)
//
// Three roads were involved and each is asserted below: the print road (which asked the tagless word road
// and so printed truncations), the build of the sliced *source* when the base is a literal (which built it
// untagged and let `rt_slice` copy tags nobody had written), and the binding road (which never recorded that
// the name received a list, so the copy's handle had no name that could be read back).
//
// The rows are the reference's, read off `python3`; `integration/slice_answer_test.go` runs the same programs
// through the CLI and derives its expectations from the reference rather than from this file.

// slicePrograms are the answered shapes, with the answer the reference gives. A tag is a property of the
// object, not of how the object was written: every base — a literal node, a name bound to one, a call's
// container answer (ADR 0333), and a slice's own answer — has to print the same copy.
var slicePrograms = []struct {
	src  string
	want string
}{
	// The base is a name bound to a mixed literal: the shape the row was measured on.
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2])", "[1.5, 'a']"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:1])", "[1.5]"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[1:2])", "['a']"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[2:3])", "[[7]]"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[1:])", "['a', [7]]"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[:2])", "[1.5, 'a']"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[::-1])", "[[7], 'a', 1.5]"},
	// An empty copy of a tagged base is still a tagged object: its slots are read with the tag table the
	// build wrote, and a copy with no slots has to carry that table too or the printer has nothing to ask.
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:0])", "[]"},
	// The base is a literal node: the sliced source is built as an object first, tagged, and the copy is
	// taken from that object rather than from the compile-time global's address.
	{"print([1.5, \"a\", [7]][0:2])", "[1.5, 'a']"},
	{"print([1, 2, 3][1:])", "[2, 3]"},
	{"print([\"a\", \"b\", \"c\"][1:])", "['b', 'c']"},
	// The base's slots are all one kind: no tag table, and the plain word copy is the right answer. These
	// rows are what a tagging fix must not disturb.
	{"xs = [1, 2, 3]\nprint(xs[0:2])", "[1, 2]"},
	{"xs = [1, 2, 3]\nprint(xs[1:])", "[2, 3]"},
	{"xs = [\"a\", \"b\"]\nprint(xs[0:1])", "['a']"},
	// The copy is a list, so every read of a list answers it: the printer, `len`, a loop, truthiness,
	// membership, equality, and a binding that names it.
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(ys)", "[1.5, 'a']"},
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(len(ys))", "2"},
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nfor v in ys:\n    print(v)", "1.5\na"},
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(ys[0], ys[1])", "1.5 a"},
	{"xs = [1.5, \"a\", [7]]\nprint(not xs[0:0])", "True"},
	{"xs = [1.5, \"a\", [7]]\nprint(not xs[0:2])", "False"},
	{"xs = [1.5, \"a\", [7]]\nprint(2 in xs[0:2])", "False"},
	{"xs = [1, 2]\nprint(2 in xs[0:2])", "True"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2] == [1.5, \"a\"])", "True"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2] == [1.5])", "False"},
	// The base is a call's container answer (ADR 0333): the slice asks the road that slices a container
	// variable, so the answer's own tags travel into the copy.
	{"def b():\n    return [1.5, \"a\", [7]]\nprint(b()[0:2])", "[1.5, 'a']"},
	{"def b():\n    return [1.5, \"a\", [7]]\nys = b()[0:2]\nprint(ys)", "[1.5, 'a']"},
	{"def b():\n    return [1.5, \"a\", [7]]\nprint(len(b()[0:2]))", "2"},
	{"def b():\n    return [1, 2, 3]\nprint(len(b()[0:2]))", "2"},
	{"def n():\n    return [10, 20, 30]\nprint(n()[1:])", "[20, 30]"},
	{"def b():\n    return [1, \"a\", None, [4]]\nprint(b()[1:3])", "['a', None]"},
	{"def b():\n    return [1, 2, 3]\nys = b()[0:2]\nfor v in ys:\n    print(v)", "1\n2"},
	// A dict's values and a list's slots are read with the same tags, and a slot that holds a list is read
	// by the same door whichever way it was written.
	{"def m():\n    return {\"k\": [1.5, \"a\"]}\nprint(m()[\"k\"])", "[1.5, 'a']"},
	{"def m():\n    return {\"k\": [1.5, \"a\"]}\nprint(m()[\"k\"][0])", "1.5"},
	{"def m():\n    return {\"a\": 1, \"b\": 2}\nprint(m()[\"b\"])", "2"},
	// A slice of a slice is a copy of a copy: the second door reads the object the first one built.
	{"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][0:1])", "[1.5]"},
	{"xs = [1, 2, 3]\nprint(xs[0:2][0:1])", "[1]"},
	{"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(ys[0:1])", "[1.5]"},
	// Strings keep their own road (the fold reads the interned text), and the row must not disturb it.
	{"s = \"abcdef\"\nprint(s[1:3])", "bc"},
	{"print(\"abcdef\"[::-1])", "fedcba"},
}

// TestASliceOfATaggedListAnswersACopyNotATruncation is the row's own claim, asked of the compiler rather than
// of the reference: the copy carries the base's slots as the base prints them.
func TestASliceOfATaggedListAnswersACopyNotATruncation(t *testing.T) {
	for _, tt := range slicePrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference answers %q and this toolchain owes an answer, not a refusal",
					tt.src, got, tt.want)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
		})
	}
}

// TestTheSliceDoorIsParityWithTheReference asks the same programs of the reference leg: the answer below is
// the one `python3` prints, and a row that moved is a row this file's table got wrong.
func TestTheSliceDoorIsParityWithTheReference(t *testing.T) {
	for _, tt := range slicePrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			want := tt.want
			if want == "" {
				want = ""
			} else {
				want += "\n"
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, want)
			}
			if out+"\n" != want {
				t.Fatalf("%q printed %q, the reference prints %q", tt.src, out, want)
			}
		})
	}
}

// TestASliceAnswersWhatTheReferenceAnswersWhenTheBaseHasNoAnswer covers the bases that are not lists at all.
// The reference does not answer these and does not refuse them: it raises, and the program's own `except` is
// allowed to take the raise. Exit 2 — the code the contract reserves for a compiler bug — is forbidden on
// every row (ADR 0166, ADR 0211).
func TestASlicePastTheEndRaisesWhatTheReferenceRaises(t *testing.T) {
	for _, tt := range []struct {
		src     string
		class   string
		message string
	}{
		{
			"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][0][9])",
			"TypeError", "'float' object is not subscriptable",
		},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("%q raised %s: %s; the reference raises %s: %s", tt.src, class, message, tt.class, tt.message)
			}
		})
	}
}

// TestATrapInsideASliceReadIsCatchable is the same raise asked of the program: a trap that ends the run is a
// crash, and a trap the program can take is a value the language has.
func TestATrapInsideASliceReadIsCatchable(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{
			"xs = [1.5, \"a\", [7]]\ntry:\n    print(xs[0:2][0][9])\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
		{
			"def b():\n    return [1.5, \"a\", [7]]\n\ntry:\n    print(b()[0:2][0][9])\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			if got := strings.TrimRight(compiledOut(t, tc.src), "\n"); got != tc.want {
				t.Fatalf("%q printed %q, want the handler to have printed %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestTheBoundFormAndTheDirectFormAnswerTheSameThing is the parity that makes this a defect rather than a
// missing feature: `ys = xs[0:2]` and `xs[0:2]` are one program written two ways, and both spellings are the
// shape the untagged list has always answered.
func TestTheBoundFormAndTheDirectFormAnswerTheSameThing(t *testing.T) {
	for _, tc := range []struct{ direct, bound string }{
		{
			"xs = [1.5, \"a\", [7]]\nprint(xs[0:2])",
			"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(ys)",
		},
		{
			"def b():\n    return [1.5, \"a\", [7]]\nprint(b()[0:2])",
			"def b():\n    return [1.5, \"a\", [7]]\nys = b()[0:2]\nprint(ys)",
		},
		{
			"xs = [1.5, \"a\", [7]]\nprint(xs[0:0])",
			"xs = [1.5, \"a\", [7]]\nys = xs[0:0]\nprint(ys)",
		},
		{
			"xs = [1, 2, 3]\nprint(xs[0:2])",
			"xs = [1, 2, 3]\nys = xs[0:2]\nprint(ys)",
		},
		{
			"def n():\n    return [10, 20, 30]\nprint(n()[1:])",
			"def n():\n    return [10, 20, 30]\nys = n()[1:]\nprint(ys)",
		},
	} {
		t.Run(strings.ReplaceAll(tc.direct, "\n", "; "), func(t *testing.T) {
			directOut, directTrap := runForParity(t, tc.direct)
			boundOut, boundTrap := runForParity(t, tc.bound)
			if directTrap != boundTrap || directOut != boundOut {
				t.Fatalf("one copy, two answers: %q gave (%q, %q), %q gave (%q, %q)",
					tc.direct, directOut, directTrap, tc.bound, boundOut, boundTrap)
			}
			if directTrap != "" && directOut != "" {
				t.Fatalf("%q wrote %q before the raise it shares with %q; the reference writes nothing",
					tc.direct, directOut, tc.bound)
			}
		})
	}
}

// TestTheSlicedSourceIsBuiltByTheSamePredicateAsTheLiteralRoad is the pairing rule (ADR 0187) made
// mechanical: the road that builds the object being sliced asks its own build site's predicate, and the copy
// it hands to `rt_slice` therefore has the tags the copy is read with. The negative control beside it is the
// shape that must keep its compile-time fold: a list of ints sliced is a list of ints, and a row that starts
// allocating for it is a row that got the predicate wrong.
func TestTheSlicedSourceIsBuiltByTheSamePredicateAsTheLiteralRoad(t *testing.T) {
	tagged := mainBodyOf(emittedModuleIR(t, "xs = [1.5, \"a\", [7]]\nprint(xs[0:2])"))
	if !strings.Contains(tagged, "@rt_slice") {
		t.Fatalf("a slice of a literal no longer copies an object at all:\n%s", tagged)
	}
	// The object being copied was built by the tagged builder: every slot wrote its tag beside it, which is
	// what makes the copy's tags mean anything (ADR 0187's pairing rule).
	if !strings.Contains(tagged, "@rt_tag_elem") {
		t.Fatalf("rt_slice is copying an object nobody tagged:\n%s", tagged)
	}
	if !strings.Contains(tagged, "@rt_print_list_mixed") {
		t.Fatalf("the copy is printed by the printer that ignores tags:\n%s", tagged)
	}
	// The negative control: the same read of a list whose slots are all numbers keeps the plain copy and the
	// plain printer — a row that started tagging those is a row that asked the wrong question.
	plain := mainBodyOf(emittedModuleIR(t, "xs = [1, 2, 3]\nprint(xs[0:2])"))
	if !strings.Contains(plain, "@rt_slice") || strings.Contains(plain, "@rt_print_list_mixed") {
		t.Fatalf("a slice of an untagged list now asks the tagged road:\n%s", plain)
	}
}

// TestEverySliceModuleVerifies is the exit-2 guard: the row began as a wrong number, and a row that ends as
// an `llc` rejection has only changed class, not been fixed.
func TestEverySliceModuleVerifies(t *testing.T) {
	for _, tt := range slicePrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			mustVerifyWithLLC(t, emittedModuleIR(t, tt.src))
		})
	}
}

// TestWhatTheSliceDoorStillCannotReachIsFiledNotFixed keeps the neighbours of this door visible, each with
// the reference's answer beside what this backend says today. A row that stops refusing moves into the
// tables above; it does not disappear.
func TestWhatTheSliceDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today ("" = it refuses)
		oracle   string // what the reference answers
		gap      string
	}{
		{
			"a slot of a copy that describes itself",
			"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][0])",
			"", "1.5",
			"roadmap Gap R.235 (the copy has slots and no road reads them direct: the subscript road's fold gate asks for a literal, and the bound copy answers while the direct one declines)",
		},
		{
			"a copy of a copy, read past its end",
			"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][9])",
			"", "IndexError: list index out of range",
			"roadmap Gap R.235 (the same read one position further out, where the reference raises rather than the compiler declining)",
		},
		{
			"a copy of a slot that is itself a list",
			"def m():\n    return {\"k\": [1.5, \"a\"]}\nprint(m()[\"k\"][0:1])",
			"", "[1.5]",
			"roadmap Gap R.235 (the base is a slot read: the records describe the container, not what its slot holds, and the copy's printer has to be told)",
		},
		{
			"a copy of a slot that is itself a list, of a nested literal",
			"d = [[1, 2]]\nprint(d[0][0:1])",
			"", "[1]",
			"roadmap Gap R.235 (the same shape written as a literal: the pair the slot carries stops the copy being describable)",
		},
		{
			"a copy of a comprehension's answer",
			"print([x for x in [1, 2, 3]][0:2])",
			"", "[1, 2, 3]",
			"roadmap Gap R.235 (a comprehension builds its object at the loop's end and leaves no record under a name to slice; at the commit before this one the shape reached `rt_slice` with a global address in its place and `llc` rejected the module — exit 2, which this row turned into a refusal)",
		},
		{
			"a slice of a set literal",
			"print({1, 2}[0:1])",
			"", "TypeError: 'set' object is not subscriptable",
			"roadmap Gap R.234 (a set has no position to slice from; at the commit before this one the literal's i32 global went to `rt_slice` and `llc` rejected the module — exit 2, ADR 0166's worst class, which this row's gate turned into a refusal)",
		},
		{
			"a slice of a dict literal",
			"print({1: 2}[0:1])",
			"", "KeyError: slice(0, 1, None)",
			"roadmap Gap R.234 (the same missing question, asked of a dict — and a dict's answer is a miss, not a type complaint: a slice of integers is hashable, so the reference hashes it and raises the KeyError that names it)",
		},
		{
			"a slice of a set kept in a name",
			"s = {1, 2}\nprint(s[0:1])",
			"", "TypeError: 'set' object is not subscriptable",
			"roadmap Gap R.234 (a set's words sliced are not a list: the reference raises, and until this row's gate the toolchain printed `[1]` at exit 0)",
		},
		{
			"a slice of a dict kept in a name",
			"d = {1: 2}\nprint(d[0:1])",
			"", "KeyError: slice(0, 1, None)",
			"roadmap Gap R.234 (the dict half of the same road, answered `[1]` at exit 0 before the gate; the reference's miss is the same one it would raise for any key the dict does not hold)",
		},
		// The bounds neighbour of these four — `xs = [1, 2]` / `print(xs["a":"b"])`, filed here as Gap R.233 with
		// the wrong answer `[1]` pinned — is answered by ADR 0336: the shape now raises the reference's
		// `TypeError: slice indices must be integers or None or have an __index__ method`. It is witnessed in
		// `slice_bounds_kind_test.go`, where a row that stops raising fails rather than quietly disappearing.
		{
			"a text-keyed dict subscripted by a number",
			"d = {\"a\": 1, \"b\": 2}\nprint(d[0], d[1])",
			"1 2", "KeyError: 0",
			"roadmap Gap R.231 (a number is a key like any other; the read road treats it as a position whatever the keys say)",
		},
		{
			"two text slots of a copy joined",
			"xs = [\"x\", \"y\"]\nprint(xs[0:2][0] + xs[0:2][1])",
			"", "xy",
			"roadmap Gap R.82 (a text slot used arithmetically: no road builds a joined text out of two slots)",
		},
		{
			"a copy written into",
			"xs = [1, 2, 3]\nxs[0:2][0] = 9\nprint(1)",
			"", "1",
			"roadmap Gap R.229 (the item-assignment road takes a variable on the left; a copy has no variable named for it)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, trap, refused := sliceFiledRow(t, tc.src)
			if tc.compiled == "" {
				if !refused {
					t.Fatalf("%q answered %q (trap %q) where the row records a refusal — the door grew, so move the row into the tables above (reference: %q)", tc.src, out, trap, tc.oracle)
				}
				return
			}
			if refused {
				t.Fatalf("%q refuses (%s); the row records the answer %q for %s", tc.src, out, tc.compiled, tc.gap)
			}
			if got := strings.TrimRight(out, "\n"); got != tc.compiled {
				t.Fatalf("%q answered %q, the answer recorded for %s was %q (reference: %q)", tc.src, got, tc.gap, tc.compiled, tc.oracle)
			}
		})
	}
}

// sliceFiledRow is what the filed rows are asked with: the program's answer, the program's exception, and
// whether the compiler declined at all. Exit 2 fails the test wherever it appears — a shape that reaches it
// is not filed as a limitation, it is a bug (ADR 0166).
func sliceFiledRow(t *testing.T, src string) (stdout, trap string, refused bool) {
	t.Helper()
	if _, err := Compile(src); err != nil {
		return err.Error(), "", true
	}
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("%q: jit: %v", src, err)
	}
	if res.Code == 2 {
		t.Fatalf("%q failed as a toolchain rejection (exit 2):\n%s", src, res.Stderr)
	}
	if _, class, message := ParseTrapReport(res.Stderr); class != "" {
		return strings.TrimSpace(res.Output), class + ": " + message, false
	}
	return strings.TrimSpace(res.Output), "", false
}

// TestTheSliceRowsAreRecorded is the record leg: every answered row has an answer on record, so the door
// cannot be deleted without deleting the evidence the suite reads (ADR 0302's rule that a missing record
// fails the case).
func TestTheSliceRowsAreRecorded(t *testing.T) {
	for _, tt := range slicePrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			rec := goldenLookup(t, tt.src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it before shipping the door that reads it", tt.src)
			}
		})
	}
}
