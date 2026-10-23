package lang

import (
	"strings"
	"testing"
)

// Roadmap Gap R.403 / ADR 0408 — `str.join` over a list whose slots the compiler cannot read.
//
//	"-".join(["a", "b"])      answered, always: the fold concatenated two constants
//	"-".join([f"a{n}", "z"])  exit 1: `join() list elements must be constant strings`
//
// The second program is the one people write. The refusal was not a missing runtime — the `+` road has
// built a text while the program ran since **ADR 0229**, one `@rt_str_cat` per seam, the same interned-index
// convention every text door uses. It was a fold that only accepted `*StringLit`s, so a list written at
// the call with one interpolation in it fell out of the road entirely, and the sentence it printed
// described a limitation the backend did not have.
//
// What this row decided: the seam is the concatenation, so `join` asks the concatenation's own question of
// each element (`strReg`: is this a text, and where is its index) and emits one `@rt_str_cat` between each
// neighbour and the separator, in the order the source wrote. No new runtime entry point; the separator is
// interned once; a one-element list never reaches a seam and answers its element's own index; the empty list
// keeps the fold's answer.
//
// An element that is provably NOT a text is a raise and not a refusal, because the reference raises:
// `sequence item 0: expected str instance, int found`, catchable by the program's own `except TypeError:`.
// The index the reference counted is carried, and the kind is the reference's word for the kind — including
// `bool`, which the reference does not call `int` here.
//
// What stays refused is the two shapes this door has no road to: the list the program built and named
// (**Gap R.404**, which wants a run-time walk of the object) and the separator the program built
// (**Gap R.405**, which wants the method's receiver in a text register, the door above it). And what it
// answers WRONGLY is one shape, filed rather than hidden: a body that hands back a NAME the pre-scan holds as
// text beside an arm of another kind (**Gap R.406**, pinned by
// `TestWhatTheJoinSeamAnswersWrongIsFiledNotFixed`, and answered the same way by the proven `"ab".upper()` road
// on a build of the parent commit — the census's defect, not the seam's).
//
// The measurement, taken over the 36 unique programs this family carries in these two files, with a build of
// the parent commit `72d1755` beside a build of this one, each asked of the pinned CPython: **3 agreed before,
// 31 after — 28 newly answering, none regressed.** The 5 that do not agree are the owed rows named above, plus the
// slot whose kind no road can name (Gap R.400 / L11.1), which is declined with
// `lower.unsupported.join-element-kind-unknown`. `tools/recmerge` wrote 33 entries from CPython for this family and
// REFUSED the programs the compiled leg has no answer for, which is the record leg refusing to testify for a
// backend that has none (ADR 0309).

func joinSeamAnswered() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"the fold's own control", "print(\"-\".join([\"a\", \"b\"]))\n", "a-b\n"},
		{"the row's own program", "n = 3\nprint(\"-\".join([f\"a{n}\", \"z\"]))\n", "a3-z\n"},
		{"one runtime slot, no seam", "n = 3\nprint(\"-\".join([f\"a{n}\"]))\n", "a3\n"},
		{"an empty separator", "n = 3\nprint(\"\".join([f\"a{n}\", \"z\"]))\n", "a3z\n"},
		{"a tuple written at the call", "n = 3\nprint(\"-\".join((\"a\", f\"b{n}\")))\n", "a-b3\n"},
		{"a call's answer in a slot", "def t():\n    return \"q\"\n\nn = 3\nprint(\"-\".join([f\"a{n}\", t()]))\n", "a3-q\n"},
		{"a method's answer in a slot", "n = 3\nprint(\"-\".join([f\"a{n}\".upper(), \"z\"]))\n", "A3-z\n"},
		{"three runtime slots", "n = 1\nprint(\", \".join([f\"x{n}\", f\"y{n}\", \"z\"]))\n", "x1, y1, z\n"},
		{"a non-ASCII separator", "n = 1\nprint(\"é\".join([f\"a{n}\", \"z\"]))\n", "a1éz\n"},
		{"a concatenated slot", "n = 3\nprint(\"-\".join([f\"a{n}\" + \"!\", \"z\"]))\n", "a3!-z\n"},
		{"a ternary slot", "c = 1\nn = 2\nprint(\"-\".join([f\"a{n}\" if c else \"f\", \"z\"]))\n", "a2-z\n"},
		{"the join inside a list", "n = 3\nprint([\"-\".join([f\"a{n}\", \"z\"]), 1])\n", "['a3-z', 1]\n"},
		{"the join bound and asked twice", "n = 3\ns = \"-\".join([f\"a{n}\", \"z\"])\nprint(s.upper(), len(s))\n", "A3-Z 4\n"},
		{"the join compared by content", "n = 3\nprint(\"-\".join([f\"a{n}\", \"z\"]) == \"a3-z\")\n", "True\n"},
		{"a repeated separator", "n = 3\nprint(\"--\".join([f\"a{n}\", \"z\"]))\n", "a3--z\n"},
		{"the empty list", "print(\"-\".join([]))\n", "\n"},
		{"the one-element list", "print(\"-\".join([\"a\"]))\n", "a\n"},
		{"an empty separator and one runtime slot", "n = 3\nprint(\"\".join([f\"a{n}\"]) == \"a3\")\n", "True\n"},
		// The four below are the shapes the SEAM's own answer has to survive leaving a body: the caller holds one
		// word and a name, and which printer that word reaches is decided by a pre-scan that never sees the body.
		{"the seam handed back by a body", "def f():\n    n = 3\n    return \"-\".join([f\"a{n}\", \"z\"])\n\nprint(f())\n", "a3-z\n"},
		{"the seam bound in a frame and handed back", "def f(n):\n    s = \"-\".join([f\"a{n}\", \"z\"])\n    return s\n\nprint(f(3))\nprint(f(4))\n", "a3-z\na4-z\n"},
		{"the seam as a call argument", "def show(s):\n    print(s)\n\nshow(\"-\".join([f\"a{3}\", \"z\"]))\n", "a3-z\n"},
		{"the seam beside a body's number arm", "def f(k):\n    if k:\n        return \"-\".join([\"a\", \"b\"])\n    return 7\n\nprint(f(True))\nprint(f(False))\n", "a-b\n7\n"},
		{"the seam in a method's answer", "class T:\n    def row(self, n):\n        return \"-\".join([f\"a{n}\", \"z\"])\n\nprint(T().row(3))\n", "a3-z\n"},
		{"the seam compared from a body's answer", "def f(n):\n    return \"-\".join([f\"a{n}\", \"z\"])\n\nif f(3) == \"a3-z\":\n    print(\"eq\")\nelse:\n    print(\"ne\")\n", "eq\n"},
	}
}

// TestTheJoinSeamAnswersWhatTheFoldRefused is the answered family on both witness legs: the record's stdout
// for the same source, and the module the compiler built and ran.
func TestTheJoinSeamAnswersWhatTheFoldRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range joinSeamAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the join seam refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q for this source and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheJoinSeamIsBuiltByTheDoorTheConcatenationRoadAlreadyUses is the IR claim: the seam is a left-leaning
// chain of `@rt_str_cat`, the door the `+` road has used since **ADR 0229**, and this row added no runtime
// entry point of its own. The elements are parameters — runtime texts that no other door assembles — so the
// only concatenations in the module are the seam's: two per neighbour (the separator, then the element),
// each fed by the one before it.
func TestTheJoinSeamIsBuiltByTheDoorTheConcatenationRoadAlreadyUses(t *testing.T) {
	t.Parallel()
	ir := irOf(t, "def f(a, b, c):\n    return \"-\".join([a, b, c])\n\nprint(f(\"x\", \"y\", \"z\"))\n")
	seams := joinSeamCalls(ir)
	if len(seams) != 4 {
		t.Fatalf("the seam emitted %d `@rt_str_cat` instruction(s), want two per neighbour (4) for three elements:\n%s", len(seams), strings.Join(seams, "\n"))
	}
	// Left-leaning: every seam after the first reads a register an earlier seam wrote.
	for _, line := range seams {
		if !strings.Contains(line, "= call i32 @rt_str_cat(") {
			t.Fatalf("a seam line is not a `@rt_str_cat` call: %q", line)
		}
	}
	// Left-leaning: the answer is built `((e0 + sep) + e1 + sep) + e2`, which in single-assignment form is
	// one chain — every seam after the first appends to the register the seam before it wrote. A fold that
	// emitted its pieces side by side instead would be a different program, and this assertion is what tells
	// the two apart from the outside.
	for i := 1; i < len(seams); i++ {
		prev, cur := joinSeamDest(seams[i-1]), firstJoinOperand(argsOf(seams[i]))
		if prev != cur {
			t.Fatalf("seam %d appends to %q, not to the %q the previous seam wrote — the chain broke:\n%s",
				i, cur, prev, strings.Join(seams, "\n"))
		}
	}

	for _, forbidden := range []string{"@rt_str_join", "@rt_join(", "@rt_str_build"} {
		if strings.Contains(ir, forbidden) {
			t.Fatalf("the join seam invented a runtime door (%s); the row's whole claim is that the seam is the concatenation door", forbidden)
		}
	}
	// …and the fold's own program still folds: a constant list of constants emits no seam at all.
	if got := joinSeamCalls(irOf(t, "print(\"-\".join([\"a\", \"b\"]))\n")); len(got) != 0 {
		t.Fatalf("the constant join stopped folding and now builds at run time: %v", got)
	}
}

// joinSeamCalls lists the `@rt_str_cat` *instructions* in a module — the seam — skipping the runtime helper's
// own body, which is written once in every program that uses a text at all and is not this row's claim.
func joinSeamCalls(ir string) []string {
	var out []string
	for _, line := range strings.Split(ir, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "%") && strings.Contains(t, "= call i32 @rt_str_cat(") {
			out = append(out, t)
		}
	}
	return out
}

// argsOf reads a call instruction's argument list.
func argsOf(line string) string {
	at := strings.Index(line, "(")
	if at < 0 {
		return line
	}
	end := strings.LastIndex(line, ")")
	if end < at {
		return line[at+1:]
	}
	return line[at+1 : end]
}

// firstJoinOperand reads the first operand out of the argument list of a seam instruction — the accumulator
// the seam appends to.
func firstJoinOperand(args string) string {
	at := strings.Index(args, ",")
	if at < 0 {
		return strings.TrimPrefix(strings.TrimSpace(args), "i32 ")
	}
	return strings.TrimPrefix(strings.TrimSpace(args[:at]), "i32 ")
}

// joinSeamDest reads the register a seam instruction writes.
func joinSeamDest(line string) string {
	at := strings.Index(line, " = ")
	if at < 0 {
		return ""
	}
	return strings.TrimSpace(line[:at])
}

// joinSeamTraps are the elements the reference raises over. The class, the sentence and the position the
// reference counted are all pinned, because a refusal that names nothing is a different defect from a raise
// that names the item.
func joinSeamTraps() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"an int slot", "n = 3\nprint(\"-\".join([1, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, int found"},
		{"an int after a text", "n = 3\nprint(\"-\".join([f\"a{n}\", 1]))\n", "TypeError: sequence item 1: expected str instance, int found"},
		{"a dict slot", "n = 3\nprint(\"-\".join([{\"a\": 1}, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, dict found"},
		{"a singleton slot", "print(\"-\".join([None, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, NoneType found"},
		{"a verdict slot", "print(\"-\".join([True, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, bool found"},
		{"a double slot", "print(\"-\".join([1.5, \"z\"]))\n", "TypeError: sequence item 0: expected str instance, float found"},
		{"a list slot", "print(\"-\".join([[1], \"z\"]))\n", "TypeError: sequence item 0: expected str instance, list found"},
	}
}

// TestTheJoinSeamRaisesWhatTheReferenceRaises pins the raise family: exit 3, the class, the sentence, and the
// fact that the program's own handler catches it — a refusal at exit 1 would be the same lie the fold told.
func TestTheJoinSeamRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range joinSeamTraps() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := RunSnippet(tc.src)
			if err != nil {
				t.Fatalf("the join seam refused a program the reference raises over: %v\n%s", err, tc.src)
			}
			if res.Code == 0 || !strings.Contains(res.Stderr, tc.want) {
				t.Fatalf("the trap reads %q, want %q\n%s", strings.TrimSpace(res.Stderr), tc.want, tc.src)
			}
		})
	}
}

// TestTheJoinTrapIsCatchableByTheProgram is the half that separates a raise from a compiler error: the
// program that handles the TypeError keeps running, printing what it printed before the raise.
func TestTheJoinTrapIsCatchableByTheProgram(t *testing.T) {
	t.Parallel()
	src := "n = 3\nprint(1)\ntry:\n    print(\"-\".join([1, f\"a{n}\"]))\nexcept TypeError as e:\n    print(\"caught:\", e)\n"
	got, err := compileAndRun(t, src)
	if err != nil {
		t.Fatalf("the raise escaped the program's own handler: %v\n%s", err, src)
	}
	if want := "1\ncaught: sequence item 0: expected str instance, int found\n"; got != want {
		t.Fatalf("printed %q, want %q\n%s", got, want, src)
	}
}

// joinSeamOwed is the pair this door has no road to. A row that stops being true FAILS, and the case is
// DELETED rather than edited.
func joinSeamOwed() []struct {
	name     string
	gap      string
	src      string
	mustMach string
	want     string
} {
	return []struct {
		name     string
		gap      string
		src      string
		mustMach string
		want     string
	}{
		{"the list the program built", "Gap R.404",
			"n = 3\nxs = [f\"a{n}\", \"z\"]\nprint(\"-\".join(xs))\n", "join() argument must be a constant list", "a3-z\n"},
		{"the separator the program built", "Gap R.405",
			"n = 3\ns = f\"-{n}\"\nprint(s.join([\"a\", \"z\"]))\n", "join on a receiver that is not a text the compiler can read", "a-3z\n"},
		// The third is not owed a SEAM, it is owed a KIND: the slot the roads cannot name. Where the compiler can see
		// an element is not a text it raises the reference's own TypeError (see TestTheJoinSeamRaisesWhatTheReference
		// _Raises); where no road can say what the slot holds it declines, because interning somebody's repr where the
		// reference raises is a wrong answer at the exit code of success. The tagged value word (L11.1) is the answer;
		// until then the refusal must keep its capability code, which is what TestTheJoinSeamKeepsTheCapabilityRegis
		// tryHonest asks of this row.
		{"a slot whose kind no road can name", "Gap R.400 / L11.1",
			"def g(v):\n    return v\n\nprint(\"-\".join([g(1), \"z\"]))\n", "join() element", "TypeError: sequence item 0: expected str instance, int found"},
	}
}

// TestWhatTheJoinSeamStillOwes pins the refusals with the sentence they carry today and the bytes the
// reference prints beside them, so closing either row is a failing test rather than a rumour.
func TestWhatTheJoinSeamStillOwes(t *testing.T) {
	t.Parallel()
	for _, tc := range joinSeamOwed() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := percentRun(tc.src)
			if err == nil {
				t.Fatalf("%s closed: the seam now answers %q without an error — delete this row and move the program up\n%s", tc.gap, tc.want, tc.src)
			}
			if !strings.Contains(err.Error(), tc.mustMach) {
				t.Fatalf("%s: the refusal reads %q, which does not carry %q\n%s", tc.gap, err.Error(), tc.mustMach, tc.src)
			}
		})
	}
}

// TestTheJoinSeamRefusesWhatIsNotAListKeepsTheFoldersSentence is the negative control: the argument that is
// not a list at all is still answered by the sentence the family has always carried, so the new road did not
// swallow the old door.
func TestTheJoinSeamRefusesWhatIsNotAListKeepsTheFoldersSentence(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(\"-\".join(1))\n",
		"print(\"-\".join(\"ab\"))\n",
		"print(\"-\".join({\"a\": 1}))\n",
	} {
		src := src
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			_, err := percentRun(src)
			if err == nil || !strings.Contains(err.Error(), "join() argument must be a constant list") {
				t.Fatalf("the non-list argument lost the fold's own sentence: %v\n%s", err, src)
			}
		})
	}
}

// TestTheJoinSeamKeepsTheCapabilityRegistryHonest asserts the machine face: the refusals this row leaves
// standing still resolve to the codes the registry advertises, so an agent that asked before still gets a
// code and not a bare sentence.
func TestTheJoinSeamKeepsTheCapabilityRegistryHonest(t *testing.T) {
	t.Parallel()
	for _, tc := range joinSeamOwed() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := percentRun(tc.src)
			if err == nil {
				t.Skipf("%s closed", tc.gap)
			}
			msg := err.Error()
			kind, ok := ClassifyCapabilityRefusal(msg)
			if !ok || kind == nil || kind.Code == "" {
				t.Fatalf("the refusal %q reaches an agent with no capability code\n%s", msg, tc.src)
			}
		})
	}
}

// TestWhatTheJoinSeamAnswersWrongIsFiledNotFixed is the honest half of this family: the one shape the seam
// answers at **exit 0 with the wrong bytes**, pinned with the reference's answer beside the compiled one so the
// day the census learns the arm the row fails and can be closed. It is NOT the seam's own defect: the same body
// spelled with the proven case road — `s = "ab".upper()` / `return s` beside `return 7` — prints `AB` and
// `(null)` on a build of the parent commit `72d1755`, byte for byte what this build prints for the join. The
// shape is a body that hands back a NAME whose textness lives in `internedVars` beside an arm of another kind:
// `answerArmOf` classifies a literal, an interpolation, a text door and a call, but not a name the pre-scan
// holds as text, so the body is never carried and the caller keeps one printer for both arms (**Gap R.219**'
// family, **ADR 0368**'s gate, filed as **Gap R.406** by **ADR 0408**). Exit 2 is never allowed here; exit 0
// with these bytes is allowed only as long as this table says so out loud.
func TestWhatTheJoinSeamAnswersWrongIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		src      string
		python   string
		compiled string
		why      string
	}{
		{
			name:     "the seam bound in a frame, beside a number arm",
			src:      "def f(k):\n    if k:\n        s = \"-\".join([\"a\", \"b\"])\n        return s\n    return 7\n\nprint(f(True))\nprint(f(False))\n",
			python:   "a-b\n7\n",
			compiled: "a-b\n(null)\n",
			why:      "a name arm the answer census cannot see, beside a number arm — Gap R.406, beside Gap R.219 / ADR 0368",
		},
		{
			name:     "the same body spelled with the case road (the parent build answers it the same way)",
			src:      "def f(k):\n    if k:\n        s = \"ab\".upper()\n        return s\n    return 7\n\nprint(f(True))\nprint(f(False))\n",
			python:   "AB\n7\n",
			compiled: "AB\n(null)\n",
			why:      "the control that keeps Gap R.406 honest: the defect is the census's, not the seam's — it predates this row",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the shape refused rather than answered wrong (%v) — if the road now refuses it, file that "+
					"as the refusal it is; an exit 1 and an exit 0 with wrong bytes are different debts: %s", err, r.src)
			}
			if got == r.python {
				t.Fatalf("Gap R.406 closed: the body now prints %q as the reference does — delete this row and move the "+
					"program to joinSeamAnswered()\n%s", got, r.src)
			}
			if got != r.compiled {
				t.Fatalf("the wrong answer changed shape: compiled prints %q, this row pins %q (the reference prints %q)\n%s",
					got, r.compiled, r.python, r.src)
			}
		})
	}
}
