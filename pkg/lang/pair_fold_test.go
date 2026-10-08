package lang

// pkg/lang/pair_fold_test.go — `min`, `max` and `sum` answer the (payload, tag) pair (roadmap L11.1,
// Gap R.146's "`min(n, 3)`" and "a literal `sum`/`min`/`max` folds into a static array"; ADR 0316).
//
// A fold hands back one of the values it was given, so the answer's kind is the WINNER's kind. That is the
// one thing a fold cannot lower by lifting both sides and returning one word: the word is the loser's shape
// as often as the winner's, which is why `max([1, 2.5])` used to answer `2` (Gap R.104) and why the varargs
// road still refuses a mixed int/double pair it cannot settle at compile time (`Gap R.73`).
//
// Every row below is judged on both witness legs: the record leg (`RecordedStdoutIs`, the answer the retired
// engine left for the source — a source with no record FAILS, so coverage cannot be deleted by deleting a
// record) and, through `integration/pair_fold_test.go`, the reference leg (the same programs through the CLI
// against CPython). The traps are RAISED with CPython's own sentence and are catchable; the positions the
// door declines stay refusals in words, at exit 1 and never exit 2 (ADR 0166).

import (
	"strings"
	"testing"
)

// The six slot kinds a fold can be asked about. The kind lives in the object, which is the whole reason the
// fold needs a tag: nothing in the source says which of these a name holds.
const (
	foldIntSlot   = "xs = []\nxs.append(7)\nn = xs[0]\n"
	foldNegSlot   = "xs = []\nxs.append(-7)\nn = xs[0]\n"
	foldFloatSlot = "xs = []\nxs.append(2.5)\nn = xs[0]\n"
	foldTextSlot  = "xs = []\nxs.append(\"a\")\nn = xs[0]\n"
	foldNoneSlot  = "xs = []\nxs.append(None)\nn = xs[0]\n"
	foldBoolSlot  = "xs = []\nxs.append(True)\nn = xs[0]\n"
)

// TestAFoldAnswersWithTheWinnersOwnKind is the answer table: the varargs spelling, the container-literal
// spelling, `sum` as the left fold over `+` seeded with the integer 0, and the answer bound to a name and
// read back by every position that asks the tag. The rows that decide the kind are the ones that used to
// truncate — `min(2.5, 3)` is the float, `max(2.5, 3)` is the integer, `max(True, 1)` is the verdict.
func TestAFoldAnswersWithTheWinnersOwnKind(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ---- the varargs spelling, an int slot -------------------------------------------
		{"an int slot — min of the slot and a literal", foldIntSlot + "print(min(n, 3))\n", "3\n"},
		{"an int slot — max of the slot and a literal", foldIntSlot + "print(max(n, 3))\n", "7\n"},
		{"an int slot — the literal on the left", foldIntSlot + "print(min(3, n))\n", "3\n"},
		{"an int slot — the slot on the left of a bigger literal", foldIntSlot + "print(max(3, n))\n", "7\n"},
		{"an int slot — the slot against itself", foldIntSlot + "print(min(n, n))\n", "7\n"},
		{"an int slot — three candidates", foldIntSlot + "print(min(n, 3, 1))\n", "1\n"},
		{"an int slot — a double it beats", foldIntSlot + "print(max(n, 2.5))\n", "7\n"},
		{"an int slot — a double that beats it", foldIntSlot + "print(min(n, 2.5))\n", "2.5\n"},
		{"an int slot — beside a verdict it loses to", foldIntSlot + "print(min(n, True))\n", "True\n"},
		{"a negative slot", foldNegSlot + "print(min(n, 3))\n", "-7\n"},
		{"a negative slot — max", foldNegSlot + "print(max(n, 3))\n", "3\n"},
		// ---- the winner's kind is the winner's own ---------------------------------------
		{"a float slot — min keeps the float", foldFloatSlot + "print(min(n, 3))\n", "2.5\n"},
		{"a float slot — max returns the INTEGER", foldFloatSlot + "print(max(n, 3))\n", "3\n"},
		{"a float slot — max keeps the float", foldFloatSlot + "print(max(n, 1))\n", "2.5\n"},
		{"a float slot — the slot against itself", foldFloatSlot + "print(min(n, n))\n", "2.5\n"},
		{"a float slot — a literal it loses to", foldFloatSlot + "print(max(n, 3.5))\n", "3.5\n"},
		{"a verdict slot — min keeps the verdict", foldBoolSlot + "print(min(n, 3))\n", "True\n"},
		{"a verdict slot — max returns the number", foldBoolSlot + "print(max(n, 3))\n", "3\n"},
		{"a verdict slot — the tie keeps the incumbent", foldBoolSlot + "print(max(n, 1))\n", "True\n"},
		// ---- texts order by content, not by the intern table's arrival order -------------
		{"a text slot — min of two texts", foldTextSlot + "print(min(n, \"b\"))\n", "a\n"},
		{"a text slot — max of two texts", foldTextSlot + "print(max(n, \"b\"))\n", "b\n"},
		{"a text slot — the tie keeps the incumbent", foldTextSlot + "print(max(n, n))\n", "a\n"},
		// ---- sum is the left fold over + seeded with the integer 0 -----------------------
		{"an int slot — sum of one", foldIntSlot + "print(sum([n]))\n", "7\n"},
		{"an int slot — sum of two", foldIntSlot + "print(sum([n, 1]))\n", "8\n"},
		{"an int slot — sum with the literal first", foldIntSlot + "print(sum([1, n]))\n", "8\n"},
		{"an int slot — sum of the slot twice", foldIntSlot + "print(sum([n, n]))\n", "14\n"},
		{"a float slot — sum keeps the float", foldFloatSlot + "print(sum([n]))\n", "2.5\n"},
		{"a float slot — sum with an int beside it", foldFloatSlot + "print(sum([n, 1]))\n", "3.5\n"},
		{"a verdict slot — sum answers a number", foldBoolSlot + "print(sum([n]))\n", "1\n"},
		{"a verdict slot — sum of two", foldBoolSlot + "print(sum([n, 1]))\n", "2\n"},
		// ---- the container-literal spelling ---------------------------------------------
		{"an int slot — min over a literal list", foldIntSlot + "print(min([n, 3]))\n", "3\n"},
		{"an int slot — max over a literal list", foldIntSlot + "print(max([n, 3]))\n", "7\n"},
		{"a float slot — min over a literal list", foldFloatSlot + "print(min([n, 3]))\n", "2.5\n"},
		{"a float slot — max over a literal list keeps the winner's kind", foldFloatSlot + "print(max([n, 2.5]))\n", "2.5\n"},
		{"a text slot — min over a literal list", foldTextSlot + "print(min([n, \"b\"]))\n", "a\n"},
		// ---- the answer bound to a name, then read back ----------------------------------
		{"a bound min prints", foldIntSlot + "m = min(n, 3)\nprint(m)\n", "3\n"},
		{"a bound max keeps the winner's float", foldFloatSlot + "m = max(n, 1)\nprint(m)\n", "2.5\n"},
		{"a bound sum prints", foldIntSlot + "m = sum([n, 1])\nprint(m)\n", "8\n"},
		{"a bound min enters a list literal", foldIntSlot + "m = min(n, 3)\nprint([m])\n", "[3]\n"},
		{"a bound min is measured by len", foldIntSlot + "m = min(n, 3)\nprint(len([m, 1]))\n", "2\n"},
		{"a bound min is asked for membership", foldIntSlot + "m = min(n, 3)\nprint(3 in [m])\n", "True\n"},
		{"a bound min renders through str", foldIntSlot + "m = min(n, 3)\nprint(str(m))\n", "3\n"},
		{"a bound min renders through repr", foldFloatSlot + "m = max(n, 1)\nprint(repr(m))\n", "2.5\n"},
		{"a bound min fills an f-string field", foldFloatSlot + "m = min(n, 3)\nprint(f\"{m}\")\n", "2.5\n"},
		{"a bound min is folded again by a second fold", foldIntSlot + "m = min(n, 3)\nprint(max(m, 5))\n", "5\n"},
		{"a bound sum of a float slot", foldFloatSlot + "m = sum([n, 1])\nprint(m)\n", "3.5\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAFoldRaisesWhatTheReferenceRaises is the trap half. A fold of two values with no ordering is a
// RUN-TIME event: the tags decide it, the program can catch it, and the sentence is CPython's character for
// character with the candidate named first and the incumbent second — the order the failing `<` or `>` had
// (ADR 0271's "a trap says what the program wrote", applied to a comparison the program never wrote out).
// A compile-time refusal for these would be a compiler opinion, not a language answer (ADR 0166, ADR 0228).
func TestAFoldRaisesWhatTheReferenceRaises(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		{"a number folded against a text (min)", foldIntSlot + "print(min(n, \"a\"))\n", "TypeError", "'<' not supported between instances of 'str' and 'int'"},
		{"a number folded against a text (max)", foldIntSlot + "print(max(n, \"a\"))\n", "TypeError", "'>' not supported between instances of 'str' and 'int'"},
		{"a number folded against None (min)", foldIntSlot + "print(min(n, None))\n", "TypeError", "'<' not supported between instances of 'NoneType' and 'int'"},
		{"a number folded against None (max)", foldIntSlot + "print(max(n, None))\n", "TypeError", "'>' not supported between instances of 'NoneType' and 'int'"},
		{"a float folded against a text", foldFloatSlot + "print(max(n, \"a\"))\n", "TypeError", "'>' not supported between instances of 'str' and 'float'"},
		{"a float folded against None", foldFloatSlot + "print(min(n, None))\n", "TypeError", "'<' not supported between instances of 'NoneType' and 'float'"},
		{"a verdict folded against a text names 'bool'", foldBoolSlot + "print(min(n, \"a\"))\n", "TypeError", "'<' not supported between instances of 'str' and 'bool'"},
		{"a verdict folded against None names 'bool'", foldBoolSlot + "print(max(n, None))\n", "TypeError", "'>' not supported between instances of 'NoneType' and 'bool'"},
		{"a text folded against a number", foldTextSlot + "print(min(n, 3))\n", "TypeError", "'<' not supported between instances of 'int' and 'str'"},
		{"a text folded against a double", foldTextSlot + "print(max(n, 2.5))\n", "TypeError", "'>' not supported between instances of 'float' and 'str'"},
		{"None folded against None", foldNoneSlot + "print(min(n, None))\n", "TypeError", "'<' not supported between instances of 'NoneType' and 'NoneType'"},
		{"None folded against a number", foldNoneSlot + "print(max(n, 3))\n", "TypeError", "'>' not supported between instances of 'int' and 'NoneType'"},
		{"a container slot folded against a number", "xs = []\nxs.append([1, 2])\nn = xs[0]\nprint(min(n, 3))\n", "TypeError", "'<' not supported between instances of 'int' and 'list'"},
		{"a dict slot folded against a number", "xs = []\nxs.append({\"k\": 1})\nn = xs[0]\nprint(max(n, 3))\n", "TypeError", "'>' not supported between instances of 'int' and 'dict'"},
		{"a set slot folded against a text", "xs = []\nxs.append({1})\nn = xs[0]\nprint(min(n, \"a\"))\n", "TypeError", "'<' not supported between instances of 'str' and 'set'"},
		// The sum's sentence is the OPERATOR's, and the 'int' it names first is the seed the program never
		// wrote — CPython's sum starts at 0, so `sum([n, "a"])` is `0 + "a"`.
		{"a sum of a number and a text", foldIntSlot + "print(sum([n, \"a\"]))\n", "TypeError", "unsupported operand type(s) for +: 'int' and 'str'"},
		{"a sum of a number and None", foldIntSlot + "print(sum([n, None]))\n", "TypeError", "unsupported operand type(s) for +: 'int' and 'NoneType'"},
		{"a sum of a float and a text", foldFloatSlot + "print(sum([n, \"a\"]))\n", "TypeError", "unsupported operand type(s) for +: 'float' and 'str'"},
		{"a sum of a number and a container slot", "xs = []\nxs.append(7)\nxs.append([1])\nn = xs[0]\nm = xs[1]\nprint(sum([n, m]))\n", "TypeError", "unsupported operand type(s) for +: 'int' and 'list'"},
		// A fold over a built container that mixes kinds, where the winner is decided by the run time.
		{"a mixed container's fold raises on the kind that has no ordering", "xs = []\nxs.append(3)\nxs.append(\"a\")\nprint(min(xs[0], xs[1]))\n", "TypeError", "'<' not supported between instances of 'str' and 'int'"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := RecordedRunError(t, tc.src)
			if err == nil {
				t.Fatalf("the program answered where the reference raises")
			}
			if ee, ok := err.(*TrapError); ok {
				if ee.ExnType != tc.class {
					t.Errorf("class = %q, want %q", ee.ExnType, tc.class)
				}
				if ee.ExnMsg != tc.message {
					t.Errorf("message = %q, want %q", ee.ExnMsg, tc.message)
				}
			} else if !strings.Contains(err.Error(), tc.class+": "+tc.message) {
				t.Errorf("the record's trap is %q, want %s: %s", err, tc.class, tc.message)
			}
			res, jerr := JIT(tc.src, 0)
			if jerr != nil {
				t.Fatalf("compiled to nothing where the reference raises: %v", jerr)
			}
			if !strings.Contains(res.Stderr, tc.class+": "+tc.message) {
				t.Fatalf("compiled raised %q, want %s: %s", firstLine(res.Stderr), tc.class, tc.message)
			}
			if res.Code == 0 {
				t.Fatalf("the trap left at the exit code of success")
			}
			if res.Code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this raise was emitted into (ADR 0166)")
			}
		})
	}
}

// TestAFoldTrapIsCatchableByTheProgramsOwnArm is the half that makes a raise a raise: the handler runs, the
// program continues, and the exit code is success — a trap the program cannot reach is a refusal with extra
// steps, and a refusal for a shape the reference answers is this row's own defect class.
func TestAFoldTrapIsCatchableByTheProgramsOwnArm(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"min caught", foldIntSlot + "try:\n    print(min(n, \"a\"))\nexcept TypeError:\n    print(\"caught-the-ordering\")\n", "caught-the-ordering\n"},
		{"max caught", foldNoneSlot + "try:\n    print(max(n, 1))\nexcept TypeError:\n    print(\"caught-the-none-ordering\")\n", "caught-the-none-ordering\n"},
		{"the sum's raise caught", foldIntSlot + "try:\n    print(sum([n, \"a\"]))\nexcept TypeError:\n    print(\"caught-the-sum-of-text\")\n", "caught-the-sum-of-text\n"},
		{"a caught fold leaves the name usable", foldIntSlot + "ok = 0\ntry:\n    ok = max(n, 3)\nexcept TypeError:\n    ok = 1\nprint(ok)\n", "7\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			RecordedStdoutIs(t, tc.src, tc.want)
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if strings.TrimSuffix(res.Output, "\n") != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", res.Output, tc.want)
			}
			if res.Code != 0 {
				t.Fatalf("a caught raise left the program at exit %d", res.Code)
			}
		})
	}
}

// foldMainBody is the program's own `main`, without the runtime blocks the module carries. "How was THIS
// print lowered?" is a question about the code the program generated; the helpers next door call the
// untagged printer for containers they print, and a whole-module substring test would blame the program for
// them (ADR 0303's one printer, read at the right scope).
func foldMainBody(ir string) string {
	i := strings.Index(ir, "define i32 @main() {")
	if i < 0 {
		return ""
	}
	rest := ir[i:]
	if j := strings.Index(rest, "\nmain.raiseexit:"); j >= 0 {
		return rest[:j]
	}
	if j := strings.Index(rest, "\n}"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// TestAFoldRunsThroughThePairDoor is the IR half: the fold goes to the one helper that takes payload AND
// tag and writes back the winning pair, the sum goes to the door `+` already had, and a program the door has
// no business in carries neither. A fold that grows a second road of its own is how a "fixed" feature
// silently re-acquires the one-word bug (ADR 0309's rule, asked of the fold).
func TestAFoldRunsThroughThePairDoor(t *testing.T) {
	for _, tc := range []struct {
		name, src   string
		must        []string
		mustNotHave []string
		// noDoubleIntoPairSlot, when set, is a name whose pair slot must never be written by a
		// `store double` — checked line by line, because the runtime's own float-box writer legitimately
		// stores a double and a whole-module substring test would blame it (ADR 0305's rejection is about
		// the BINDING writing eight bytes into the pair's four-byte word).
		noDoubleIntoPairSlot string
		// mainMustNotHave is checked against the program's own `main`, not the whole module: an assertion
		// about how THIS print was lowered is an assertion about the code the program generated, and the
		// runtime blocks a module carries for other programs' use legitimately call the untagged printer.
		mainMustNotHave []string
	}{
		{
			name: "the varargs fold asks the pair door",
			src:  foldIntSlot + "print(min(n, 3))\n",
			must: []string{"call i32 @rt_pair_fold(", "call void @rt_print_mixed_value(i32", "@rt_fold_bad", "@rt.fold.fmt"},
			// The winner reaches the module's ONE tag-reading printer. `@rt_print_value` is the printer that
			// cannot take a kind — it is the one-word reading this ADR exists to refuse — and a payload lifted
			// into the numeric door is the other half of it. (`printf` itself is not on trial: the print
			// statement's separator goes out through it for every print in the language.)
			mustNotHave:     []string{"call i32 @rt_lift_num(i32 %_n)"},
			mainMustNotHave: []string{"call void @rt_print_value("},
		},
		{
			name: "the container-literal fold asks the pair door",
			src:  foldIntSlot + "print(max([n, 3]))\n",
			must: []string{"call i32 @rt_pair_fold("},
		},
		{
			name: "the sum goes to the door + already has",
			src:  foldIntSlot + "print(sum([n, 1]))\n",
			must: []string{"call i32 @rt_num_arith(i32 0,", "@rt.num.fmt"},
			// The sum does not order anything, so it must not carry the ordering's formatter (ADR 0309's
			// gate, asked of this door: a module carries the sentence per kind it reaches and none besides).
			mustNotHave: []string{"@rt.fold.fmt", "@rt_pair_fold", "@rt_fold_bad"},
		},
		{
			name: "a bound fold writes the payload AND the tag",
			src:  foldFloatSlot + "m = min(n, 3)\nprint(m)\n",
			must: []string{"call i32 @rt_pair_fold(", "store i32 %t", "i32* %_m_tag"},
			// A float winner travels as a BOX HANDLE in the pair's i32 word; a `store double` into that word
			// is the module llc rejects, and the wrong answer before it was rejected (ADR 0305).
			mustNotHave:          []string{},
			mainMustNotHave:      []string{"call void @rt_print_value("},
			noDoubleIntoPairSlot: "m",
		},
		{
			name: "an all-literal fold carries no door at all",
			src:  "print(min(2.5, 3))\nprint(sum([1, 2]))\n",
			must: []string{"fadd double"},
			mustNotHave: []string{
				"@rt_pair_fold", "@rt_fold_bad", "@rt.num.msg",
				// The constant road is untouched: a fold the compiler can settle keeps its compile-time
				// answer, which is what makes the module carry neither helper (ADR 0309's same rule).
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			for _, want := range tc.must {
				if !strings.Contains(res.IR, want) {
					t.Errorf("the module stopped asking the pair door — %q is gone", want)
				}
			}
			for _, absent := range tc.mustNotHave {
				if strings.Contains(res.IR, absent) {
					t.Errorf("the module carries %q, which is the one-word reading this door refuses", absent)
				}
			}
			if tc.noDoubleIntoPairSlot != "" {
				slot := "i32* %_" + tc.noDoubleIntoPairSlot
				for _, line := range strings.Split(res.IR, "\n") {
					if strings.Contains(line, "store double") && strings.Contains(line, slot) {
						t.Errorf("eight bytes were written into the pair's four-byte word — %q (ADR 0305)", strings.TrimSpace(line))
					}
				}
			}
			if len(tc.mainMustNotHave) > 0 {
				body := foldMainBody(res.IR)
				if body == "" {
					t.Fatalf("the module has no `main` to read")
				}
				for _, absent := range tc.mainMustNotHave {
					if strings.Contains(body, absent) {
						t.Errorf("the program's own code carries %q, which is the one-word reading this door refuses", absent)
					}
				}
			}
		})
	}
}

// TestTheFoldDoorIsAskedOnlyOfTheShapeItOwns is the gate asked directly, so a future widening or narrowing
// fails a decision row instead of quietly rerouting a program: the door takes values side by side and ONE
// literal list, and refuses a set literal (whose element set and order the source does not fix), a dict, a
// keyword argument (`sum(xs, start=1)`, `min(a, b, key=f)` — L11.7's call surface), a name bound to a list
// (a built container's elements are Gaps R.95/R.83's row), and a program that owns the name `min`.
func TestTheFoldDoorIsAskedOnlyOfTheShapeItOwns(t *testing.T) {
	g := &irGen{}
	ask := func(src string) (door int, args int, ok bool) {
		prog, err := parseProgram(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		c, isCall := prog.Stmts[0].(*ExprStmt).Expr.(*Call)
		if !isCall {
			t.Fatalf("%q is not a call", src)
		}
		door, els, ok := g.foldCallShape(c)
		return door, len(els), ok
	}
	for _, tc := range []struct {
		name, src string
		ok        bool
		args      int
	}{
		// Each source is the call the door is asked about, spelled as a statement: the gate is put to the
		// door itself, not to a print wrapper around it, so a row that changes shape fails the decision it
		// belongs to.
		{"two values side by side", "min(n, 3)\n", true, 2},
		{"three values side by side", "max(a, b, c)\n", true, 3},
		{"one literal list", "sum([n, 1])\n", true, 2},
		{"one element in the literal list", "sum([n])\n", true, 1},
		{"a set literal is not a fold this door lowers", "min({n, 3})\n", false, 0},
		{"a dict literal is not a fold this door lowers", "sum({1: 2})\n", false, 0},
		{"a name bound to a list is not a literal", "sum(xs)\n", false, 0},
		{"a keyword argument is the call surface's row", "sum(xs, start=1)\n", false, 0},
		{"a key= argument is the call surface's row", "min(a, b, key=f)\n", false, 0},
		{"no argument at all", "min()\n", false, 0},
		{"an empty literal has nothing to fold", "sum([])\n", false, 0},
		{"a method is not the builtin", "xs.min(1)\n", false, 0},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, got, ok := ask(tc.src)
			if ok != tc.ok {
				t.Fatalf("foldCallShape = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.args {
				t.Errorf("the fold sees %d elements, want %d", got, tc.args)
			}
		})
	}

	// A program that defines the name owns it: nothing below the builtin's shape may read the call.
	shadowed := "def min(a, b):\n    return a\nxs = []\nxs.append(7)\nn = xs[0]\nprint(min(n, 3))\n"
	res, err := Compile(shadowed)
	if err != nil {
		t.Fatalf("a program that defines `min` was refused by the builtin's door: %v", err)
	}
	if strings.Contains(res.IR, "@rt_pair_fold") {
		t.Errorf("the user's `min` was lowered through the builtin's fold door")
	}

	// The cheap gate: a fold of nothing but literals stays on the ordinary road, so landing this door
	// cannot reroute one program that was already answered.
	g2 := &irGen{taggedVars: map[string]bool{"n": true}, taggedOrigin: map[string]string{"n": taggedOriginFold}}
	literalOnly, err := parseProgram("sum([1, 2])\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := literalOnly.Stmts[0].(*ExprStmt).Expr.(*Call)
	if _, els, ok := g2.foldCallShape(c); !ok {
		t.Fatalf("the shape question declined a literal list")
	} else if g2.foldMayNeedPair(els) {
		t.Errorf("an all-literal fold asked the pair door, which costs instructions and answers nothing")
	}
}

// TestWhatTheFoldDoorStillRefusesIsStillRefusedInWords is the honest half, and the measure of where the
// door stops. Three classes remain, each naming the origin, the missing half and the row — and none of them
// allowed to spend the exit-code contract's "the compiler is broken" code (ADR 0166):
//
//   - the fold answer used where ONE word is kept (`min(n, 3) + 1`, `abs(min(n, 3))`, `m + 1`, `m > 1`).
//     The arithmetic door is asked of a fold's ARGUMENT and never of the fold, which is the recursion ADR 0309
//     had to kill and the truncation Gap R.161 measures;
//   - a fold over a set literal, whose element set and order are the objects' hash fact and not the source's
//     (Gap R.198) — folding the source order would answer `sum({n, 3})` as 6 where the reference answers 3;
//   - a container literal as a fold operand (Gap R.198), which the operand lowering will not label.
func TestWhatTheFoldDoorStillRefusesIsStillRefusedInWords(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		must      []string
	}{
		{"a fold answer as an arithmetic operand", foldIntSlot + "print(min(n, 3) + 1)\n",
			[]string{"(payload, tag) pair", "one word", "roadmap L11.1"}},
		{"a fold answer as an abs operand", foldIntSlot + "print(abs(min(n, 3)))\n",
			[]string{"(payload, tag) pair", "one word", "roadmap L11.1"}},
		{"a bound fold answer as an arithmetic operand", foldIntSlot + "m = min(n, 3)\nprint(m + 1)\n",
			[]string{"the answer of a fold the built-in chose", "(payload, tag) pair", "roadmap L11.1"}},
		{"a bound fold answer in an ordering", foldIntSlot + "m = min(n, 3)\nprint(m > 1)\n",
			[]string{"the answer of a fold the built-in chose", "roadmap L11.1"}},
		{"a fold over a set literal", foldIntSlot + "print(min({n, 3}))\n",
			[]string{"(payload, tag) pair", "one word", "roadmap L11.1"}},
		{"a container literal as a fold operand", foldIntSlot + "print(sum([n, [1]]))\n",
			[]string{"sum adds numbers", "list literal is a container", "Python raises TypeError"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("answered where the door still does not reach\n%s", firstLines(res.IR, 12))
			}
			msg := err.Error()
			for _, want := range tc.must {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal does not name the missing half (%q):\n%s", want, msg)
				}
			}
			if strings.Contains(msg, "loop over a mixed list") {
				t.Errorf("the refusal blames a loop for a program that has none (Gap R.38): %v", err)
			}
			if strings.Contains(msg, "LLVM ERROR") || strings.Contains(msg, "verifier") {
				t.Fatalf("failed as an IR problem instead of a front-end refusal: %v", err)
			}
		})
	}
}
