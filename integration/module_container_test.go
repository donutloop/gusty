package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/module_container_test.go — the REFERENCE leg of **ADR 0407** (roadmap Gap R.35's container
// half): what the LINKED PROGRAM answers when a compiled body reads the module's literal container.
//
// ADR 0227 gave a compiled body the module's `i32` and ADR 0376 its `double`; a container was left out on
// purpose, because a container's word is a heap handle that has to be rooted and whose element operations
// live in the name-keyed notebooks the binding road writes. `pkg/lang/module_container.go` closes that by
// handing the reading frame a BINDING of the module's literal — the same `stmtAssign` road the program's own
// `xs = [1, 2]` goes through — for every name the gate can vouch for is bound once to a literal container
// and never written through. This file asks the programs, through the CLI, against the pinned CPython.
//
// The measurement, taken with a build of the parent commit (`0c54a58`) beside a build of this one over the
// 178 programs this file's and `pkg/lang/module_container_test.go`'s tables carry: **22 of them agreed with
// the reference before, 111 after — 89 newly answering, none regressed.** 61 are refused at exit 1 by the
// gate, which is the door's own boundary and is pinned shape by shape below; 2 reach the frame-slot audit at
// **exit 2** (`return xs + [3]` and its unindented twin), and 4 print a word the answer-kind family already
// printed on the parent build for a function-LOCAL container (`[1.5, 2.5]` → `2`, `[True, False]` → `0` —
// byte-for-byte what `0c54a58` prints, so none of it is this door's, and roadmap Gap R.324 · Gap R.219 ·
// Gap R.331 own the word it needs). Nothing in the answered family reaches the assembler with the container
// in a word slot: the copy is allocated, rooted and written in the body that reads it, which is the IR claim
// `pkg/lang/module_container_test.go` reads for itself.
//
// What this row deliberately did NOT open is the write: a program that mutates the module's object keeps the
// refusal, because a per-frame copy answers whichever literal the emitter copied rather than whichever object
// the call reads — roadmap **Gap R.398**. The closures and `lambda`s that take no prologue are **Gap R.401**,
// the doors that still ask the module's records rather than the frame's are **Gap R.402**, the slot whose
// element kind the answer direction cannot carry is **Gap R.400**, and the one exit-2 shape is **Gap R.399**.

// moduleContainerCLIRow is one answered program: the source, and the bytes the reference prints for it.
type moduleContainerCLIRow struct {
	name string
	src  string
	want string
}

// moduleContainerCLIRows is the answered family at this altitude: the four containers, the doors that read
// one, the module's own reads beside the body's, and the scoping controls. Every `want` is what the pinned
// CPython printed for the same source (Gap N's standing rule), re-asked inside the case so a stale
// expectation fails rather than passing quietly.
func moduleContainerCLIRows() []moduleContainerCLIRow {
	rows := []moduleContainerCLIRow{
		{"the length of the module's list", "xs = [1, 2, 3]\ndef n() -> int:\n    return len(xs)\n\nprint(n())", "3\n"},
		{"the module's list, printed by a body", "xs = [1, 2, 3]\ndef show():\n    print(xs)\n\nshow()", "[1, 2, 3]\n"},
		{"the module's dict, printed by a body", "d = {\"a\": 1, \"b\": 2}\ndef show():\n    print(d)\n\nshow()", "{'a': 1, 'b': 2}\n"},
		{"the module's set, printed by a body", "s = {1, 2, 3}\ndef show():\n    print(s)\n\nshow()", "{1, 2, 3}\n"},
		{"the module's tuple, printed by a body", "t = (1, 2)\ndef show():\n    print(t)\n\nshow()", "(1, 2)\n"},
		{"the module and the body read one list in one print", "xs = [1, 2, 3]\ndef n() -> int:\n    return len(xs)\n\nprint(n(), xs)", "3 [1, 2, 3]\n"},
		{"two calls read one literal", "xs = [1, 2, 3]\ndef n() -> int:\n    return len(xs)\n\nprint(n(), n())", "3 3\n"},
		{"a body walks the module's list", "xs = [1, 2, 3]\ndef total() -> int:\n    t = 0\n    for x in xs:\n        t += x\n    return t\n\nprint(total())", "6\n"},
		{"a body walks the module's dict by its keys", "d = {\"a\": 1, \"b\": 2}\ndef keys():\n    for k in d:\n        print(k)\n\nkeys()", "a\nb\n"},
		{"a body reads one slot of the module's list", "xs = [\"b\", \"a\"]\ndef show():\n    print(xs[0])\n\nshow()", "b\n"},
		{"a body reads one entry of the module's dict", "d = {1: \"a\", 2: \"b\"}\ndef show():\n    print(d[1])\n\nshow()", "a\n"},
		{"a body reads a container inside the module's list", "xs = [[1, 2], [3, 4]]\ndef show():\n    print(xs[1])\n\nshow()", "[3, 4]\n"},
		{"a body measures a container inside the module's dict", "d = {\"a\": [1, 2], \"b\": [3]}\ndef show():\n    print(len(d[\"b\"]))\n\nshow()", "1\n"},
		{"a body renders the module's list with str", "xs = [1, 2, 3]\ndef text():\n    return str(xs)\n\nprint(text())", "[1, 2, 3]\n"},
		{"a body compares the module's list to a literal", "xs = [1, 2, 3]\ndef same():\n    return xs == [1, 2, 3]\n\nprint(same())", "True\n"},
		{"a body sorts the module's list for a print", "xs = [3, 1, 2]\ndef show():\n    print(sorted(xs))\n\nshow()", "[1, 2, 3]\n"},
		{"a body asks membership with a parameter", "xs = [1, 2, 3]\ndef has(v):\n    return v in xs\n\nprint(has(2), has(9))", "True False\n"},
		{"a body asks the module's list for membership", "xs = [1, 2, 3]\ndef has():\n    return 2 in xs\n\nprint(has())", "True\n"},
		{"a body asks the module's dict for membership", "d = {\"a\": 1}\ndef has():\n    return \"a\" in d\n\nprint(has())", "True\n"},
		{"a body asks whether the module's list is empty", "xs = [1, 2, 3]\ndef empty():\n    return not xs\n\nprint(empty())", "False\n"},
		{"a body builds a fresh list out of the module's list", "xs = [1, 2, 3]\ndef doubled():\n    out = []\n    for x in xs:\n        out.append(x * 2)\n    return out\n\nprint(doubled())", "[2, 4, 6]\n"},
		{"a body prints a slot of the module's list — the twin that witnesses the interpolated field", "xs = [1, 2]\ndef show():\n    print(xs[0])\n\nshow()", "1\n"},
		{"a body hands back an integer slot of the module's list", "xs = [1, 2, 3]\ndef first():\n    return xs[0]\n\nprint(first())", "1\n"},
		{"a body hands back the last slot, spelled from the other end", "xs = [1, 2, 3]\ndef last():\n    return xs[-1]\n\nprint(last())", "3\n"},
		{"a body hands back an entry of the module's dict", "d = {\"a\": 1, \"b\": 2}\ndef one():\n    return d[\"b\"]\n\nprint(one())", "2\n"},
		{"a body adds two slots of the module's list", "xs = [1, 2, 3]\ndef s():\n    return xs[0] + xs[2]\n\nprint(s())", "4\n"},
		{"a body reads a slot of a list inside the module's list", "nested = [[1, 2], [3, 4]]\ndef s():\n    return nested[0][0] + nested[1][1]\n\nprint(s())", "5\n"},
		{"a body asks membership of a slot of the module's list", "nested = [[1, 2], [3, 4]]\ndef member():\n    return 3 in nested[1]\n\nprint(member())", "True\n"},
		{"a body builds a fresh list out of the module's slots", "xs = [1, 2]\nd = {\"a\": 7}\ndef pair():\n    return [xs[1], d[\"a\"]]\n\nprint(pair())", "[2, 7]\n"},
		{"two bodies read the same module list", "xs = [1, 2, 3]\ndef n() -> int:\n    return len(xs)\n\ndef show():\n    print(xs)\n\nprint(n())\nshow()", "3\n[1, 2, 3]\n"},
		{"one body reads two module containers", "xs = [1, 2, 3]\nd = {\"a\": 1}\ndef show():\n    print(len(xs), len(d))\n\nshow()", "3 1\n"},
		{"a body calls a body that reads the module's list", "xs = [1, 2, 3]\ndef n() -> int:\n    return len(xs)\n\ndef m() -> int:\n    return n()\n\nprint(m())", "3\n"},
		{"a recursive body reads the module's list at every level", "xs = [1, 2, 3]\ndef down(n: int) -> int:\n    if n <= 0:\n        return 0\n    return len(xs) + down(n - 1)\n\nprint(down(2))", "6\n"},
		{"a method reads the module's list", "xs = [1, 2]\nclass C:\n    def show(self):\n        print(xs)\n        return 0\n\nC().show()", "[1, 2]\n"},
		{"a method measures the module's list", "xs = [1, 2, 3]\nclass C:\n    def n(self):\n        return len(xs)\n\nprint(C().n())", "3\n"},
		{"a body reads the module's list beside the module's int and double", "COUNT = 4\nRATE = 2.5\nxs = [1, 2]\ndef show():\n    print(len(xs), COUNT, RATE)\n\nshow()", "2 4 2.5\n"},
		{"the empty list is a container a body reads", "xs = []\ndef n() -> int:\n    return len(xs)\n\nprint(n())", "0\n"},
		{"the empty dict is a container a body reads", "d = {}\ndef n() -> int:\n    return len(d)\n\nprint(n())", "0\n"},
		{"a body reads a list of floats", "xs = [1.5, 2.5]\ndef show():\n    print(xs)\n\nshow()", "[1.5, 2.5]\n"},
		{"a body reads a list of verdicts", "xs = [True, False]\ndef show():\n    print(xs)\n\nshow()", "[True, False]\n"},
		{"a body reads a nested literal of literals", "xs = [[1, 2], {\"a\": (3,)}]\ndef show():\n    print(xs)\n\nshow()", "[[1, 2], {'a': (3,)}]\n"},
		{"a body takes the name for itself and reads its own list", "xs = [1, 2, 3]\ndef show():\n    xs = [7, 8]\n    print(xs)\n\nshow()\nprint(xs)", "[7, 8]\n[1, 2, 3]\n"},
		{"a parameter named like the module's list is the parameter's", "xs = [1, 2, 3]\ndef show(xs):\n    print(xs)\n\nshow([9])\nprint(xs)", "[9]\n[1, 2, 3]\n"},
		{"a body reads the module's list under a condition", "xs = [1, 2, 3]\ndef show(c):\n    if c:\n        print(xs)\n    else:\n        print(len(xs))\n\nshow(True)\nshow(False)", "[1, 2, 3]\n3\n"},
		{"an except arm reads the module's list", "xs = [1, 2, 3]\ndef show():\n    try:\n        print(xs[9])\n    except IndexError:\n        print(len(xs))\n\nshow()", "3\n"},
		{"a while loop walks the module's list by position", "xs = [1, 2, 3]\ndef s():\n    i = 0\n    t = 0\n    while i < len(xs):\n        t = t + xs[i]\n        i = i + 1\n    return t\n\nprint(s())", "6\n"},
		// The controls (ADR 0273): the module's own reads, which this row did not touch, and a container no
		// body reads at all. These answered before this cycle; the claim above is only meaningful beside them.
		{"a module container no body reads is still main's own binding", "xs = [1, 2, 3]\nprint(xs)", "[1, 2, 3]\n"},
		{"a module int read by a body keeps the door ADR 0227 built", "LATE = 0\ndef read() -> int:\n    return LATE\n\nLATE = 3\nprint(read())", "3\n"},
		{"a module double read by a body keeps the door ADR 0376 built", "RATE = 2.5\ndef warm():\n    return RATE > 2\n\nprint(warm())", "True\n"},
	}
	// Every source carries its trailing newline and a blank line before each top-level `def`/`class`, which
	// is how the record keys it and how the `pkg/lang` table writes the same programs: one text, one answer,
	// both altitudes. A table that wrote `print(x)` where the record holds `print(x)\n` would ask for a record
	// it could never find and report the miss as a missing family.
	for i := range rows {
		src := rows[i].src
		if !strings.HasSuffix(src, "\n") {
			src += "\n"
		}
		var out []string
		for i, line := range strings.Split(strings.TrimSuffix(src, "\n"), "\n") {
			if i > 0 && (strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "class ")) && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, line)
		}
		rows[i].src = strings.Join(out, "\n") + "\n"
	}
	return rows
}

// TestTheModuleContainerFamilyAnswersLikeTheReference runs every answered program on the CLI: the reference
// prints what the table says (or the table is stale and the case says so), and the linked program prints the
// same bytes at an exit code that is not 2 — ADR 0166 reserves that one for a module `llc` had to reject,
// which is exactly the failure a container handed to a word-sized slot would produce.
func TestTheModuleContainerFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range moduleContainerCLIRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "module_container.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q for this program (prints %q) — this table's expectation went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 reserves exit 2 for a compiler bug):\n%s\n%s", engine, stderr, tc.src)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q — the module's container was refused:\n%s",
						engine, code, want, stderr)
				}
				if stdout != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// TestTheModuleContainerFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row
// answers has an answer on record, written from the reference by tools/recmerge and cross-checked against the
// compiled leg — never written by this backend. A row whose record disappeared fails here.
func TestTheModuleContainerFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range moduleContainerCLIRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one module-container program is on the record: the record leg is not being walked")
	}
}

// TestTheModuleContainerProgramIsTheReferenceLineForLine is the conformance corpus's own claim, asked of the
// file rather than of a table: integration/programs/module_container.gy prints the reference's bytes for
// every line, with no line dropped between the two.
func TestTheModuleContainerProgramIsTheReferenceLineForLine(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("module_container")
	want, ok := cpythonPlainOut(t, t.TempDir(), src)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "module_container.gy", src)
		out, code := cliRunCode(t, engine, path)
		if code != 0 {
			t.Fatalf("%s exited %d: %s", engine, code, out)
		}
		if out != want {
			wl, gl := strings.Split(want, "\n"), strings.Split(out, "\n")
			for i := 0; i < len(wl) || i < len(gl); i++ {
				var w, g string
				if i < len(wl) {
					w = wl[i]
				}
				if i < len(gl) {
					g = gl[i]
				}
				if w != g {
					t.Fatalf("%s differs on line %d: printed %q, CPython prints %q", engine, i+1, g, w)
				}
			}
		}
	}
}

// TestTheModuleContainerProgramIsOnTheRecord keeps the corpus program itself on the record leg.
func TestTheModuleContainerProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("module_container")
	if !lang.HasGoldenAnswer(src) {
		t.Fatalf("integration/programs/module_container.gy is not on the record — record it from the reference with tools/recmerge")
	}
}

// TestTheModuleContainerCopyIsRootedInEveryRoadThatReadsIt is the CLI's half of the IR claim: the same source
// through the LINKED binary and through the in-process JIT must answer the same bytes, and neither may reach
// for a module global — a container handed to a body is a binding that frame owns, and a global for it would
// be one name with two owners (the two module doors ADR 0227 and ADR 0376 built are `@gy_mod_<name>`).
func TestTheModuleContainerCopyIsRootedInEveryRoadThatReadsIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"a length read", "xs = [1, 2, 3]\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n", "3\n"},
		{"a print read", "xs = [1, 2]\ndef show():\n    print(xs)\n\nshow()\nprint(xs)\n", "[1, 2]\n[1, 2]\n"},
		{"a slot read handed back", "xs = [1, 2, 3]\ndef first():\n    return xs[0]\n\nprint(first())\n", "1\n"},
		{"a method read", "xs = [1, 2]\nclass C:\n    def n(self):\n        return len(xs)\n\nprint(C().n())\n", "2\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("refused: %v\n%s", err, tc.src)
			}
			if strings.Contains(res.IR, "@gy_mod_xs") {
				t.Fatalf("the module's container went to a global; the copy is a frame binding, not module state\n%s", res.IR)
			}
			if !strings.Contains(res.IR, "call void @rt_root_put(i32* %_xs)") {
				t.Fatalf("the frame's copy is never rooted — an unrooted handle is how the list reads back empty\n%s", res.IR)
			}
			if _, err := lang.VerifyModuleIR(res.IR, 0); err != nil {
				t.Fatalf("the emitted module does not verify: %v\n%s", err, res.IR)
			}
			path := writeSrc(t, t.TempDir(), "module_container_rooted.gy", tc.src)
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 || stdout != tc.want {
					t.Fatalf("%s printed %q at exit %d, want %q\n%s", engine, stdout, code, tc.want, stderr)
				}
			}
		})
	}
}

// moduleContainerOwedCLI is one shape this row measured and does NOT own: the exit code the linked program
// gives today, the sentence or bytes it gives with it, the reference's answer beside it, and the roadmap row
// that owns the fix. A row that stops being true FAILS, and the case is DELETED rather than edited.
type moduleContainerOwedCLI struct {
	name     string
	gap      string
	src      string
	code     int
	today    string // the pinned stdout, for the class that runs and prints the wrong word
	mustMach string // the substring stderr has to carry, for the class that is refused
	want     string // what CPython prints for the same source
}

func moduleContainerOwedRows() []moduleContainerOwedCLI {
	return []moduleContainerOwedCLI{
		// Gap R.398 — the write half. Every one of these is a program CPython runs; the gate declines them
		// because a copy answers whichever literal the emitter copied, not whichever object the call reads.
		{"a body hands back a slot holding a double", "Gap R.324 · Gap R.219 · Gap R.331",
			"xs = [1.5, 2.5]\ndef one():\n    return xs[1]\n\nprint(one())", 0, "2\n", "", "2.5\n"},
		{"a body hands back a slot holding a verdict", "Gap R.324 · Gap R.219 · Gap R.331",
			"xs = [True, False]\ndef one():\n    return xs[1]\n\nprint(one())", 0, "0\n", "", "False\n"},
		{"the module appends and a body reads the list", "Gap R.398",
			"xs = [1, 2, 3]\ndef show():\n    print(xs)\n\nxs.append(4)\nshow()", 1, "", "bound at module level", "[1, 2, 3, 4]\n"},
		{"the module writes a slot and a body reads it", "Gap R.398",
			"xs = [1, 2, 3]\nxs[0] = 9\ndef show():\n    print(xs)\n\nshow()", 1, "", "bound at module level", "[9, 2, 3]\n"},
		{"the module augments the list and a body reads it", "Gap R.398",
			"xs = [1, 2, 3]\nxs += [4]\ndef show():\n    print(xs)\n\nshow()", 1, "", "bound at module level", "[1, 2, 3, 4]\n"},
		{"the module rebinds the name and a body reads it", "Gap R.398",
			"xs = [1, 2, 3]\ndef show():\n    print(xs)\n\nxs = [4, 5]\nshow()", 1, "", "bound at module level", "[4, 5]\n"},
		{"a body writes through the module's list", "Gap R.398",
			"xs = [1, 2, 3]\ndef push():\n    xs.append(4)\n\npush()\nprint(xs)", 1, "", "bound at module level", "[1, 2, 3, 4]\n"},
		{"the module aliases the list and writes through the alias", "Gap R.398",
			"xs = [1, 2, 3]\ndef show():\n    print(xs)\n\nys = xs\nys.append(9)\nshow()", 1, "", "bound at module level", "[1, 2, 3, 9]\n"},
		{"the module asks which object two names hold", "Gap R.398",
			"xs = [1, 2, 3]\nys = xs\ndef show():\n    print(xs)\n\nprint(ys is xs)\nshow()", 1, "", "bound at module level", "True\n[1, 2, 3]\n"},
		{"a body hands the module's list back", "Gap R.398",
			"xs = [1, 2, 3]\ndef give():\n    return xs\n\nprint(give())", 1, "", "bound at module level", "[1, 2, 3]\n"},
		{"the module binds the name twice and a body reads it", "Gap R.398",
			"xs = [1]\nxs = [2]\ndef show():\n    print(xs)\n\nshow()", 1, "", "bound at module level", "[2]\n"},
		{"a slot of the module's list is a name", "Gap R.398",
			"one = 1\nxs = [one]\ndef show():\n    print(xs)\n\nshow()", 1, "", "bound at module level", "[1]\n"},
		{"a slot of the module's list is a call's answer", "Gap R.398",
			"def one():\n    return 1\n\nxs = [one()]\ndef show():\n    print(xs)\n\nshow()", 1, "", "bound at module level", "[1]\n"},
		{"the module binds the name with a for loop", "Gap R.398",
			"for xs in [1, 2]:\n    print(xs)\n\ndef show():\n    print(xs)\n\nshow()", 1, "", "bound at module level", "1\n2\n2\n"},
		// Gap R.399 — the one shape in the family the frame-slot audit KILLS: exit 2, the code ADR 0166
		// reserves for a compiler bug, which is why it is filed loudly rather than quietly.
		{"a body concatenates onto the module's list", "Gap R.399",
			"xs = [1, 2]\ndef give():\n    return xs + [3]\n\nprint(give())", 2, "", "internal invariant violated (frame-slot audit)", "[1, 2, 3]\n"},
		// Gap R.400 — the slots the answer direction carries wrongly today: the gate refuses them, and the
		// reference answers them; the tagged return road owns the answer.
		{"a body hands back a TEXT slot of the module's list", "Gap R.400",
			"xs = [\"b\", \"a\"]\ndef first():\n    return xs[1]\n\nprint(first())", 1, "", "index of a non-literal variable", "a\n"},
		{"a body hands back the singleton's slot", "Gap R.400",
			"xs = [None, 1]\ndef first():\n    return xs[0]\n\nprint(first())", 1, "", "index of a non-literal variable", "None\n"},
		{"a body hands back a slot that is itself a list", "Gap R.400",
			"table = [[1], [2]]\ndef row():\n    return table[0]\n\nprint(row())", 1, "", "index of a non-literal variable", "[1]\n"},
		// Gap R.401 — the roads that emit a body without the prologue: a nested `def` and a `lambda` are
		// still answered by `emitClosureDef` and the lambda road, which read module state the `def` road's
		// hook never reached them.
		{"a nested def reads the module's list", "Gap R.401",
			"xs = [1, 2, 3]\ndef outer():\n    def inner():\n        return len(xs)\n\n    print(inner())\n\nouter()", 1, "", "bound at module level", "3\n"},
		{"a lambda applied at module level reads the module's list", "Gap R.401",
			"xs = [1, 2, 3]\nprint((lambda: len(xs))())", 1, "", "bound at module level", "3\n"},
		{"a lambda bound to a name prints the module's list", "Gap R.401",
			"xs = [1, 2, 3]\nf = lambda: xs\n\nprint(f())", 1, "", "bound at module level", "[1, 2, 3]\n"},
		// Gap R.402 — the doors that ASK the name a question still read the MODULE's records rather than the
		// frame's, so a program that answers at module level refuses in a body. Both of these answer at
		// module level today (`print([x * 2 for x in xs])`, `print(xs[0:2])`), which is the measurement that
		// localises them to the frame's records and not to the doors.
		{"a comprehension walks the module's list", "Gap R.402",
			"xs = [1, 2, 3]\ndef doubled():\n    return [x * 2 for x in xs]\n\nprint(doubled())", 1, "", "compile-time constant", "[2, 4, 6]\n"},
		{"a body slices the module's list", "Gap R.402",
			"xs = [1, 2, 3]\ndef show():\n    print(xs[0:2])\n\nshow()", 1, "", "cannot slice xs", "[1, 2]\n"},
		{"a body interpolates the module's list", "Gap R.402",
			"xs = [1, 2]\ndef show():\n    print(f\"{xs}\")\n\nshow()", 1, "", "bound at module level", "[1, 2]\n"},
		{"a body interpolates the length of the module's list", "Gap R.402",
			"xs = [1, 2]\ndef show():\n    print(f\"{len(xs)}\")\n\nshow()", 1, "", "bound at module level", "2\n"},
		{"a body hands back an interpolation of the module's list", "Gap R.402",
			"xs = [1, 2]\ndef show():\n    return f\"{xs}\"\n\nprint(show())", 1, "", "has no renderer", "[1, 2]\n"},
		// The two rows that print the WRONG WORD while compiling and running at exit 0: the slot whose kind
		// nothing carried across the `return`. Identical on the parent commit, which is the evidence that
		// they are the answer direction's road and not this door's.
		{"a body hands back a FLOAT slot of the module's list", "Gap R.400",
			"xs = [1.5, 2.5]\ndef one():\n    return xs[1]\n\nprint(one())", 0, "2\n", "", "2.5\n"},
		{"a body hands back a VERDICT slot of the module's list", "Gap R.400",
			"xs = [True, False]\ndef one():\n    return xs[1]\n\nprint(one())", 0, "0\n", "", "False\n"},
	}
}

// TestWhatTheModuleContainerRoadStillOwesAtTheCLI holds the shapes this row measured and left on the tracker.
// Three assertions per row: the reference answers (so the row describes a program, not a broken one), the
// program behaves exactly as pinned — refused at the pinned exit code with the pinned sentence, or answering
// the pinned wrong bytes at exit 0 — and the refusal sentence never blames the compiler's own machinery.
func TestWhatTheModuleContainerRoadStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range moduleContainerOwedRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "module_container_owed.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q for this program (prints %q) — this table's expectation went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 0 && stdout == want {
					t.Fatalf("%s: %s now prints the reference's %q — the row closed and this pin has to be DELETED, "+
						"with the program moved into the answered table\n%s", engine, tc.gap, want, tc.src)
				}
				if code != tc.code {
					t.Fatalf("%s: %s pinned exit %d, the program exited %d (the reference prints %q)\nstdout %q\nstderr %q",
						engine, tc.gap, tc.code, code, want, stdout, stderr)
				}
				if tc.today != "" {
					// The wrong-answer class: it runs, it prints, and what it prints is the payload word.
					if stdout != tc.today {
						t.Fatalf("%s: %s printed %q, pinned the wrong answer %q (the reference prints %q) — re-measure the row",
							engine, tc.gap, stdout, tc.today, want)
					}
					continue
				}
				if stdout != "" {
					t.Fatalf("%s: %s printed %q while the program was refused — a program that does not run prints nothing\n%s",
						engine, tc.gap, stdout, tc.src)
				}
				if !strings.Contains(stderr, tc.mustMach) {
					t.Fatalf("%s: %s was filed as %q and now behaves differently (exit %d, stderr %q): the row needs "+
						"re-measuring — and if it now prints %q, close the row and delete this case",
						engine, tc.gap, tc.mustMach, code, strings.TrimSpace(stderr), want)
				}
				if strings.Contains(stderr, "%!s(") || strings.Contains(stderr, "*lang.") || strings.Contains(stderr, "__cans") {
					t.Fatalf("%s: %s refuses in the compiler's own vocabulary instead of the program's (ADR 0249):\n%s",
						engine, tc.gap, stderr)
				}
			}
		})
	}
}

// TestTheModuleContainerRefusalIsMachineReadable pins the agent's half of the family: a refusal has to be
// consumable, which means the `--json` document says which phase refused, carries the sentence, and does not
// leak a Go type name where the program's text belongs.
func TestTheModuleContainerRefusalIsMachineReadable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"a write through the object", "xs = [1, 2, 3]\ndef push():\n    xs.append(4)\n\npush()\nprint(xs)\n"},
		{"a name the module binds twice", "xs = [1]\nxs = [2]\n\ndef show():\n    print(xs)\n\nshow()\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "module_container_json.gy", tc.src)
			out, code := cliRunCode(t, "--json", path)
			if code != 1 {
				t.Fatalf("a refused program exited %d, want 1 (ADR 0006): %s", code, out)
			}
			for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`} {
				if !strings.Contains(out, frag) {
					t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
				}
			}
			if !strings.Contains(out, "bound at module level") {
				t.Fatalf("the JSON refusal does not carry the sentence that names the missing half:\n%s", out)
			}
			if strings.Contains(out, "%!s(") || strings.Contains(out, "*lang.") {
				t.Fatalf("the JSON refusal prints the compiler's own types at the agent:\n%s", out)
			}
		})
	}
}
