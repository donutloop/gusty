// Roadmap **Gap R.285** / **ADR 0419** — the dict's static key door reads a `None` and a verdict key,
// and every road that LABELS an operand from it agrees with the road that reads the object.
//
//	d = {None: "a"}
//	print(d[None] + "b")     #  ab    ← this backend: refused, exit 1, in a sentence that is FALSE of it
//	print(d[None] * 2)       #  aa    ← and here: 0, at the exit code of success
//	d = {1: "a"}
//	print(d[1] + "b")        #  ab    ← the integer-key twin answered all along
//
// One construct, two answers, decided by which KIND the key was written with. The door that reads an
// entry out of a dict the literal still describes (`constKeysMatch`) had no arm for the void or the
// verdict, so every road that asks *what kind is this operand* — the concatenation, the repeat, the
// method road, the print road — said "not a text" for the `None`-keyed dict and "a text" for the
// integer-keyed one.
//
// The arm was not simply missing. **ADR 0353** built it, measured it, and reverted it, because two
// *callers* of that door then answered wrong: the membership test and the `for` loop each read an
// entry's kind to decide what question to ask the entry at all — walk its slots, its keys, its members,
// or test a substring — and neither can walk a text, a number, a verdict or the void. They read the
// payload as a heap handle and answered at exit 0 where the reference raises. So this row is one row:
// widen the two label roads to ask the OBJECT first (`slotPairWhereStaticLabelCannotWalk`), and only
// then open the door. That order is what turns the four measurements below from regressions into
// controls — and it fixed two more silently-wrong programs that never had a `None` in them at all
// (`xs = [5]` / `1 in xs[0]` answered False; `xs = ["ab"]` / `for x in xs[0]` printed nothing).
//
// The second half of the row is the crossing the old comment called "harmless only because every road
// that reaches it still asks the object": `foldConstInt` reads a `NoneLit` as the integer 0, so the
// door once said `{0: "a"}` has an entry keyed by `None`. That is refused at the door now, and the
// crossing is pinned at both altitudes raising the reference's `KeyError: None`.
//
// What this file pins: the two named programs and the operator family beside them; the four controls;
// the key-identity table asked of the door AS A FUNCTION in both directions; the IR the two roads emit;
// and the refusals this row leaves standing, with today's answer beside the reference's.
package lang

import (
	"strings"
	"testing"
)

// noneKeyOperatorFamily is the row's answering half: the operand is a read out of a dict whose key the
// compiler could not read, and every road that labels it must reach the same verdict the integer-key
// twin already reached. Every `want` was read off CPython 3.12 before the case was written.
func noneKeyOperatorFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The two programs the row is named for.
		{"the row's concatenation", "d = {None: \"a\"}\nprint(d[None] + \"b\")\n", "ab\n"},
		{"the row's repeat", "d = {None: \"a\"}\nprint(d[None] * 2)\n", "aa\n"},
		{"the verdict-key twin of the concatenation", "d = {True: \"a\"}\nprint(d[True] + \"b\")\n", "ab\n"},
		{"the false-key twin of the repeat", "d = {False: \"a\"}\nprint(d[False] * 2)\n", "aa\n"},
		// The roads beside them, which ask the same label question for a different verb.
		{"the method road", "d = {None: \"a\"}\nprint(d[None].upper())\n", "A\n"},
		{"the length road", "d = {None: \"a\"}\nprint(len(d[None]))\n", "1\n"},
		{"the interpolation field", "d = {None: \"a\"}\nprint(f\"v={d[None]}\")\n", "v=a\n"},
		{"str and repr", "d = {None: \"a\"}\nprint(str(d[None]), repr(d[None]))\n", "a 'a'\n"},
		{"the equality door", "d = {None: \"a\"}\nprint(d[None] == \"a\")\n", "True\n"},
		{"the condition door", "d = {None: \"a\"}\nif d[None]:\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "yes\n"},
		{"the condition door on a falsy entry", "d = {None: \"\"}\nif d[None]:\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "no\n"},
		{"the container-element door", "d = {None: \"a\"}\nprint([d[None], 1])\n", "['a', 1]\n"},
		{"the call-argument door", "d = {None: \"a\"}\ndef f(v):\n    return v\nprint(f(d[None]))\n", "a\n"},
		{"a character of the entry", "d = {None: \"hello\"}\nprint(d[None][1])\n", "e\n"},
		{"a slice of the entry", "d = {None: \"hello\"}\nprint(d[None][0:2])\n", "he\n"},
		{"two entries, two key kinds", "d = {None: \"a\", 1: \"b\"}\nprint(d[None] + d[1])\n", "ab\n"},
		// A void key is not a number, and the entry under a NUMBER key is a different entry.
		{"a number entry still answers arithmetic", "d = {None: 5}\nprint(d[None] + 1)\n", "6\n"},
		{"a fractional entry still repeats as a float", "d = {None: 2.5}\nprint(d[None] * 2)\n", "5.0\n"},
		{"a container entry still concatenates", "d = {None: [1, 2]}\nprint(d[None] + [3])\n", "[1, 2, 3]\n"},
		{"a container entry reaches its own slot", "d = {None: [1, 2]}\nprint(d[None][0] + 1)\n", "2\n"},
		// The copy of the list a void key names: the base is a slot read, which is the base **ADR 0459**'s
		// door learned to describe (roadmap **Gap R.235** — this row was a filed refusal until then).
		{"a slice of the list a void key names", "d = {None: [1, 2, 3]}\nprint(d[None][1:])\n", "[2, 3]\n"},
	}
}

// TestABoundKeyCarriesTheKindItsBindingWrote is roadmap **Gap R.286**'s answering half: the KEY of a read
// arrived through a NAME, and the door asked the object with the name's payload word and no tag at all —
// which for the void is the integer 0 wearing the wrong label, and for a verdict the number 1 or 0.
//
// The read road had an arm for exactly this and it was unreachable: it sat in the `else` of a test on
// `n.Idx.(*IntLit)`, so it could only ever run for an index that was BOTH an `*IntLit` and a `*Name`. And
// the door every other position asks — `elemKindTag` — had arms for the void and the verdict SPELLINGS
// and none for the name whose binding wrote one, so it fell through to the blanket `TagInt` at its foot.
// `d = {None: 3.5}` / `k = None` / `print(d[k])` died `KeyError` where the reference prints `3.5`; the IR
// asked `rt_dict_has_tagged(i32 %h, i32 %_k.ld1, i32 0)` while the builder had stored the entry under
// (0, TagNone). The tag is the only thing the binding ever failed to carry (**ADR 0420**, beside the
// **ADR 0419** that gave the integer KEY its tag).
//
// Every `want` was read off CPython 3.12 before the case was written, and every source is on the record —
// ADR 0302's rule, so an answer here is not certified by the backend that earned it.
func TestABoundKeyCarriesTheKindItsBindingWrote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"the void key through a name prints its entry", "d = {None: 3.5}\nk = None\nprint(d[k])\n", "3.5\n"},
		{"the void key through a name concatenates", "d = {None: \"a\"}\nk = None\nprint(d[k] + \"b\")\n", "ab\n"},
		{"the void key through a name repeats", "d = {None: \"a\"}\nk = None\nprint(d[k] * 2)\n", "aa\n"},
		{"the void key through a name takes the method road", "d = {None: \"a\"}\nk = None\nprint(d[k].upper())\n", "A\n"},
		{"the void key through a name loops over the entry", "d = {None: \"ab\"}\nk = None\nfor ch in d[k]:\n    print(ch)\n", "a\nb\n"},
		{"the void key through a name measures a list entry", "d = {None: [1, 2]}\nk = None\nprint(len(d[k]))\n", "2\n"},
		{"the void and the number stay two keys through names", "d = {None: 1, 0: 2}\nk = None\nprint(d[k])\n", "1\n"},
		{"the number key through a name is untouched", "d = {None: 1, 0: 2}\nk = 0\nprint(d[k])\n", "2\n"},
		{"a verdict reads the verdict key", "d = {True: \"one\"}\nk = True\nprint(d[k])\n", "one\n"},
		{"a verdict reads the number key it is", "d = {1: \"one\"}\nk = True\nprint(d[k])\n", "one\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
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
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheReadStillDrawsTheLineTheReferenceDraws is the trap half of the same row: a key that names no
// entry raises, and the void names itself in the traceback the way the reference names it. Where the
// message cannot name the key the row says so and names the row that owes the sentence, rather than
// leaving a generic line to look like the reference's.
func TestTheReadStillDrawsTheLineTheReferenceDraws(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		wantTrap string
		dossier  string
	}{
		{
			name:     "a void key over a number-keyed dict raises, and names the void",
			src:      "d = {0: \"zero\"}\nk = None\nprint(d[k])\n",
			wantTrap: "KeyError: None",
			dossier:  "the reference raises `KeyError: None`; the key's own repr is the record the read road now carries",
		},
		{
			name:     "a number key over a void-keyed dict raises",
			src:      "d = {None: \"a\"}\nk = 0\nprint(d[k])\n",
			wantTrap: "KeyError",
			dossier:  "the reference raises `KeyError: 0` — the class answers here, the key's NAME is owed by **Gap R.189**",
		},
		{
			name:     "a verdict key over a void-keyed dict raises",
			src:      "d = {None: 1}\nk = True\nprint(d[k])\n",
			wantTrap: "KeyError",
			dossier: "the reference raises `KeyError: True` — the class answers here; the record says a name holds a " +
				"verdict and not WHICH verdict, so the key's NAME stays owed by **Gap R.189**",
		},
		{
			name:     "a number key the program bound by name raises, and stays unnamed",
			src:      "d = {0: \"zero\"}\nk = 5\nprint(d[k])\n",
			wantTrap: "KeyError",
			dossier: "the reference raises `KeyError: 5`. The miss and its class answer here; the key's NAME is the " +
				"half **Gap R.189** still owes — a TEXT key through a name names itself and so does the void, so the " +
				"debt is precisely a number's repr, which no record carries for a bound name",
		},
		{
			name:     "a text key the program bound by name names itself",
			src:      "d = {\"a\": 1}\nk = \"zz\"\nprint(d[k])\n",
			wantTrap: "KeyError: 'zz'",
			dossier:  "the reference raises `KeyError: 'zz'`, and the message reads the same `strVals` record the printer asks",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("%s (%s): the compiled leg refused a program the reference runs: %v\n%s", tc.name, tc.dossier, err, tc.src)
			}
			if res.Code == 0 || !strings.Contains(res.Stderr+res.Output, tc.wantTrap) {
				t.Fatalf("%s (%s): exit %d stdout %q stderr %q — expected a trap carrying %q\n%s",
					tc.name, tc.dossier, res.Code, res.Output, res.Stderr, tc.wantTrap, tc.src)
			}
		})
	}
}

// TestAVoidKeyedEntryAnswersEveryRoadThatLabelsAnOperand is the row's answer, judged on the compiled
// leg and the record leg (ADR 0302: a missing record fails the case, so an answer is not certified by
// the backend that earned it).
func TestAVoidKeyedEntryAnswersEveryRoadThatLabelsAnOperand(t *testing.T) {
	t.Parallel()
	for _, tc := range noneKeyOperatorFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
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
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheFourControlsThatKeptTheDoorShutStillAnswer is the measurement table from ADR 0353's revert,
// asked in the order the fix has to go in. These are the four programs that made the `None` arm come
// back out; the reason the arm can live now is that the two roads which labelled an operand from it
// ask the object instead. Each of these four was a WRONG ANSWER with the door open and the roads
// unchanged — which is why this case fails if any of them stops raising, or starts answering.
func TestTheFourControlsThatKeptTheDoorShutStillAnswer(t *testing.T) {
	t.Parallel()
	answers := []struct{ name, src, want string }{
		{"a text entry is searched for a substring", "d = {None: \"a\"}\nprint(\"a\" in d[None])\n", "True\n"},
		{"a text entry searched for the absent substring", "d = {None: \"ab\"}\nprint(\"z\" in d[None])\n", "False\n"},
		{"a text entry is stepped over character by character", "d = {None: \"ab\"}\nfor x in d[None]:\n    print(x)\n", "a\nb\n"},
		{"a list entry is stepped over", "d = {None: [1, 2]}\nfor x in d[None]:\n    print(x)\n", "1\n2\n"},
		{"a dict entry is walked by its keys", "d = {None: {\"k\": 1}}\nfor k in d[None]:\n    print(k)\n", "k\n"},
		{"a list entry is asked for a member", "d = {None: [1, 2]}\nprint(1 in d[None])\n", "True\n"},
	}
	for _, tc := range answers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := compileAndRun(t, tc.src); err != nil {
				t.Fatalf("refused: %v\n%s", err, tc.src)
			} else if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
		})
	}

	// The two that RAISE. A number has no elements, keys, members or characters: membership names the
	// type and a loop names the object, and the two sentences are not variations of one another.
	traps := []struct{ name, src, class, sentence string }{
		{"membership of a number entry", "d = {None: 5}\nprint(1 in d[None])\n", "TypeError", "argument of type 'int' is not iterable"},
		{"a loop over a number entry", "d = {None: 5}\nfor x in d[None]:\n    print(x)\n", "TypeError", "'int' object is not iterable"},
		{"membership of a void entry", "d = {None: None}\nprint(1 in d[None])\n", "TypeError", "argument of type 'NoneType' is not iterable"},
		{"a loop over a void entry", "d = {None: None}\nfor x in d[None]:\n    print(x)\n", "TypeError", "'NoneType' object is not iterable"},
		{"a loop over a verdict entry", "d = {None: True}\nfor x in d[None]:\n    print(x)\n", "TypeError", "'bool' object is not iterable"},
		// The same two roads over a LIST slot: the class these programs used to answer with, and the
		// reason the label roads are one row and not two. `xs = [5]` / `1 in xs[0]` answered False at
		// exit 0 and `xs = ["ab"]` / `for x in xs[0]` printed nothing — no `None` anywhere in either.
		{"membership of a number slot", "xs = [5]\nprint(1 in xs[0])\n", "TypeError", "argument of type 'int' is not iterable"},
		{"a loop over a number slot", "xs = [5]\nfor x in xs[0]:\n    print(x)\n", "TypeError", "'int' object is not iterable"},
	}
	for _, tc := range traps {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference runs (%v): %s", err, tc.src)
			}
			if strings.Contains(res.Stderr, "codegen:") {
				t.Fatalf("%s was refused rather than run: %s", tc.src, res.Stderr)
			}
			if res.Code != 3 {
				t.Fatalf("%s exited %d printing %q — the reference raises %s: %s, and a wrong answer at exit 0 is the class this row exists to forbid\n%s",
					tc.src, res.Code, res.Output, tc.class, tc.sentence, tc.src)
			}
			if !strings.Contains(res.Stderr, tc.class) || !strings.Contains(res.Stderr, tc.sentence) {
				t.Errorf("%s raised %q, the reference raises %s: %s", tc.src, strings.TrimSpace(res.Stderr), tc.class, tc.sentence)
			}
		})
	}
}

// TestTheLoopOverAnUnwalkableStaticLabelIsTheLoopThatAsksTheObject is the loop's control pair: the same
// shape over a container the literal describes keeps the static road (nothing new is emitted), while
// the text and number slots go through the runtime loop helpers that read the slot's tag.
func TestTheLoopOverAnUnwalkableStaticLabelIsTheLoopThatAsksTheObject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		need   []string
		forbid []string
	}{
		{
			name:   "a void-keyed text slot walks the tagged loop",
			src:    "d = {None: \"ab\"}\nfor x in d[None]:\n    print(x)\n",
			need:   []string{"@rt_iter_len", "@rt_iter_elem", "@rt_dict_get_tagged"},
			forbid: nil,
		},
		{
			name:   "a void-keyed number slot raises from the tag",
			src:    "d = {None: 5}\nfor x in d[None]:\n    print(x)\n",
			need:   []string{"@rt_iter_len", "@rt_die"},
			forbid: nil,
		},
		{
			name:   "a void-keyed list slot is asked for a member under the tag",
			src:    "d = {None: 5}\nprint(1 in d[None])\n",
			need:   []string{"@rt_contains_tagged", "@rt_dict_get_tagged"},
			forbid: nil,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			for _, helper := range tc.need {
				if !strings.Contains(body, helper) {
					t.Errorf("%s: the road did not reach %s in the program's own body:\n%s", tc.name, helper, body)
				}
			}
		})
	}
}

// TestTheOperatorLabelRoadIsThePrinterThatReadsAText is the IR half of the answering family: the
// concatenation and the repeat of a void-keyed text entry go to the text doors, and the numeric
// formatter that printed `0` for the repeat is absent from the program's own body. A refusal would pass
// this row by emitting nothing, so the text door is required as well as the formatter forbidden.
func TestTheOperatorLabelRoadIsThePrinterThatReadsAText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		need   []string
		forbid string
	}{
		{"concatenation", "d = {None: \"a\"}\nprint(d[None] + \"b\")\n", []string{"@rt_str_cat", "@rt_print_str"}, "%d"},
		{"repeat", "d = {None: \"a\"}\nprint(d[None] * 2)\n", []string{"@rt_str_repeat", "@rt_print_str"}, "%d"},
		{"method", "d = {None: \"a\"}\nprint(d[None].upper())\n", []string{"@rt_str_case", "@rt_print_str"}, "%d"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := mainRegion(t, tc.src)
			for _, helper := range tc.need {
				if !strings.Contains(body, helper) {
					t.Errorf("%s: the label road never reached %s:\n%s", tc.name, helper, body)
				}
			}
			if tc.forbid != "" && strings.Contains(body, tc.forbid) {
				t.Errorf("%s: the program's own body still uses the numeric formatter %s — the answer is a payload again:\n%s", tc.name, tc.forbid, body)
			}
		})
	}
}

// TestTheStaticKeyDoorAnswersItsOwnTableInBothDirections asks the door AS A FUNCTION. This is the only
// case that can pin the crossing the row's second half is about: `{0: "a"}` read by `None` must not be
// an entry, because `foldConstInt` reads a `NoneLit` as the integer 0 and the door used to answer yes.
// A door tested only through programs that already reach it proves nothing about the keys it turns away.
func TestTheStaticKeyDoorAnswersItsOwnTableInBothDirections(t *testing.T) {
	t.Parallel()
	g := &irGen{}
	i := func(v int64) Expr { return &IntLit{Value: v} }
	s := func(v string) Expr { return &StrLit{Value: v} }
	b := func(v bool) Expr { return &BoolLit{Value: v} }
	n := func() Expr { return &NoneLit{} }
	f := func(v float64) Expr { return &FloatLit{Value: v} }
	for _, tc := range []struct {
		name       string
		key, index Expr
		want       bool
	}{
		// The void is a key of its own kind: equal to nothing but itself.
		{"none reads none", n(), n(), true},
		{"none does not read zero", n(), i(0), false},
		{"zero does not read none", i(0), n(), false},
		{"none does not read false", n(), b(false), false},
		{"false does not read none", b(false), n(), false},
		{"none does not read the empty text", n(), s(""), false},
		{"none does not read zero-point-zero", n(), f(0), false},
		{"none does not read a name", n(), &Name{Value: "k"}, false},
		// The reference's own identity rule, kept for the numeric keys: `1`, `True` and `1.0` are one key.
		{"one reads one", i(1), i(1), true},
		{"one reads true", i(1), b(true), true},
		{"true reads one", b(true), i(1), true},
		{"true reads true", b(true), b(true), true},
		{"true does not read false", b(true), b(false), false},
		{"false reads zero", b(false), i(0), true},
		{"zero reads false", i(0), b(false), true},
		{"zero does not read one", i(0), i(1), false},
		// A text is never a number, whatever bits the intern table handed it (ADR 0232).
		{"text reads text", s("a"), s("a"), true},
		{"text does not read itself-and-a-number", s("a"), i(1), false},
		{"number does not read text", i(1), s("a"), false},
		// A key with no spelling has no arm: the literal cannot be walked to the entry.
		{"a name key is not read statically", &Name{Value: "k"}, n(), false},
		{"a list key is not read statically", &ListLit{Elems: []Expr{i(1)}}, n(), false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := g.constKeysMatch(tc.key, tc.index); got != tc.want {
				t.Errorf("constKeysMatch(%s, %s) = %v, want %v", tc.name, labelRoadKeyName(tc.key), labelRoadKeyName(tc.index), got)
			}
		})
	}
}

// labelRoadKeyName names an expression for a failure message without leaking a Go type name into a user's
// sentence (the rule is Gap R.116; this is a test's own diagnostic, so the surface is allowed here).
func labelRoadKeyName(e Expr) string {
	switch e.(type) {
	case *IntLit:
		return "an integer key"
	case *StrLit:
		return "a text key"
	case *BoolLit:
		return "a verdict key"
	case *NoneLit:
		return "the void key"
	case *FloatLit:
		return "a fractional key"
	case *Name:
		return "a name key"
	case *ListLit:
		return "a list key"
	}
	return "a key"
}

// TestTheVoidAndVerdictKeyCrossingsRaiseWhatTheReferenceRaises is the row's second half at the program
// altitude: the door refuses the crossing, and the object refuses it too, in the reference's own class
// and sentence — the key is NAMED, which is what `KeyError: key not found` never did.
func TestTheVoidAndVerdictKeyCrossingsRaiseWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, sentence string }{
		{"zero_keyed_dict_read_by_none", "d = {0: \"a\"}\nprint(d[None])\n", "KeyError: None"},
		{"false_keyed_dict_read_by_none", "d = {False: \"a\"}\nprint(d[None])\n", "KeyError: None"},
		{"none_keyed_dict_read_by_zero", "d = {None: \"a\"}\nprint(d[0])\n", "KeyError: 0"},
		{"none_keyed_dict_read_by_false", "d = {None: \"a\"}\nprint(d[False])\n", "KeyError: False"},
		{"one_keyed_dict_read_by_none", "d = {1: \"a\"}\nprint(d[None])\n", "KeyError: None"},
		{"none_keyed_dict_read_by_true", "d = {None: \"a\"}\nprint(d[True])\n", "KeyError: True"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference runs (%v): %s", err, tc.src)
			}
			if strings.Contains(res.Stderr, "codegen:") {
				t.Fatalf("%s was refused rather than run: %s", tc.src, res.Stderr)
			}
			if res.Code != 3 {
				t.Fatalf("%s exited %d printing %q; the reference raises %s\n%s", tc.src, res.Code, res.Output, tc.sentence, tc.src)
			}
			if !strings.Contains(res.Stderr, tc.sentence) {
				t.Errorf("%s raised %q, the reference raises %q", tc.src, strings.TrimSpace(res.Stderr), tc.sentence)
			}
		})
	}
	// And the crossing is catchable, because a trap the program cannot reach is a different program.
	t.Run("the missing void key is catchable", func(t *testing.T) {
		t.Parallel()
		src := "d = {0: \"a\"}\ntry:\n    print(d[None])\nexcept KeyError as e:\n    print(\"caught\", e)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("refused: %v\n%s", err, src)
		}
		if res.Code != 0 || res.Output != "caught None\n" {
			t.Fatalf("%s printed %q exit %d stderr %q, want \"caught None\"", src, res.Output, res.Code, res.Stderr)
		}
	})
}

// TestTheNumberKeyRoadIsUntouchedByTheVoidArm is the control family: the two arms this row opened sit
// in front of the number road, and the number road's answers — including the ones the reference shares
// with the verdict keys — must not have moved.
func TestTheNumberKeyRoadIsUntouchedByTheVoidArm(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"integer key", "d = {1: \"one\"}\nprint(d[1])\n", "one\n"},
		{"true reads the one key", "d = {1: \"one\"}\nprint(d[True])\n", "one\n"},
		{"false reads the zero key", "d = {0: \"zero\"}\nprint(d[False])\n", "zero\n"},
		{"a float key is the same key as the integer", "d = {1: \"one\"}\nprint(d[1.0])\n", "one\n"},
		{"the later write of one key wins", "d = {True: 1.5, 1: \"one\"}\nprint(d[True])\n", "one\n"},
		{"none and zero are two keys of one dict", "d = {None: 1, 0: 2}\nprint(len(d))\n", "2\n"},
		{"both entries print", "d = {None: 1, 0: 2}\nprint(d[None], d[0])\n", "1 2\n"},
		{"the whole dict prints", "print({None: \"x\", True: 2})\n", "{None: 'x', True: 2}\n"},
		{"a text entry over an integer key", "d = {1: \"a\"}\nprint(d[1] + \"b\")\n", "ab\n"},
		{"a list slot read is still walked", "xs = [[1, 2]]\nfor v in xs[0]:\n    print(v)\n", "1\n2\n"},
		// The four controls that caught this row's own regression, measured against `gusty.stub` built from
		// the pre-row build (7cbffb5) and this row's parent (2b49a2a): every one of them printed its number
		// there and every one came back REFUSED here,
		// because `dictLiteralHasNoSuchKey` asked whether the LITERAL's keys were readable and never asked
		// whether the key being LOOKED UP was. A loop variable matched nothing, so the walk claimed the entry
		// was absent, the label road believed the read was a text, and `t + d[k]` — two integers — met the
		// concatenation's sentence. `integration/dict_iteration_test.go` is where the gate found this; the pin
		// belongs here, beside the door, so the door cannot drift again (roadmap Gap R.285).
		{"a dict loop adds an integer slot", "d = {1: 2, 3: 4}\nt = 0\nfor k in d:\n    t = t + d[k]\nprint(t)\n", "6\n"},
		{"two dict loops reuse the loop variable", "p = {1: 10, 2: 20}\nq = {3: 30}\nt = 0\nfor k in p:\n    t = t + p[k]\n\nfor k in q:\n    t = t + q[k]\nprint(t)\n", "60\n"},
		{"a dict loop after a range loop reuses the name", "d = {4: 40, 5: 50}\ns = 0\nfor k in range(3):\n    s = s + k\n\nfor k in d:\n    s = s + d[k]\nprint(s)\n", "93\n"},
		{"a list loop and a dict loop share a name", "xs = [1, 2, 3]\nd = {7: 70}\nt = 0\nfor k in xs:\n    t = t + k\n\nfor k in d:\n    t = t + d[k]\nprint(t)\n", "76\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := compileAndRun(t, tc.src); err != nil {
				t.Fatalf("refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			} else if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatTheVoidKeyLabelRoadStillCannotReachIsFiledNotFixed pins the neighbours this row leaves
// standing, with today's answer beside the reference's. A case that starts ANSWERING fails here, which
// is what makes the owed rows closeable on purpose instead of by accident.
func TestWhatTheVoidKeyLabelRoadStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantOut    string // "" when the row is a refusal or a trap
		wantStderr string
		dossier    string
	}{
		// “a slice of a void-keyed list entry” left this table with **ADR 0459**: the copy door names the
		// object a slot read hands back, and the row is witnessed as answered in
		// `noneKeyOperatorFamily` above rather than filed here.
		{
			name:     "sorting a dict whose keys are of more than one kind",
			src:      "d = {None: 1, 0: 2}\nprint(sorted(d))\n",
			wantExit: 1, wantStderr: "more than one kind",
			dossier: "**Gap R.429** / **ADR 0417** — the reference RAISES " +
				"`TypeError: '<' not supported between instances of 'int' and 'NoneType'`; this door refuses",
		},
		{
			// Measured while this row opened the repeat road on a void-keyed slot: the pair arrives with its
			// tag, the sequence door asks a KIND, and a slot read has no kind the door can name — so the list's
			// word wins and the brackets are the wrong pair. The int-key twin prints `[1, 1]` on this row's
			// parent too (`/tmp/gusty.stub` at 7cbffb5), which is the evidence that the void arm did not write
			// this defect; it only walked into it. `t = (1,)` / `print(t * 2)` prints `(1, 1)`, so the road is
			// sound for a name and loses the tuple only on the way out of a slot (roadmap Gap R.434).
			name:     "a tuple-valued slot, repeated",
			src:      "d = {None: (1,)}\nprint(d[None] * 2)\n",
			wantExit: 0, wantOut: "[1, 1]\n",
			dossier: "**Gap R.433** — the reference prints `(1, 1)`; the repeat road reads a KIND where the " +
				"pair door handed it a TAG, and the list arm answered for the tuple",
		},
		{
			// The all-TEXT twin of the loop row the CLI suite pins: the reference concatenates the entries into
			// `xy`; this backend enters the loop, reads each slot into a position that holds one word, and prints
			// the empty accumulator. Byte-identical on this row's parent (`/tmp/gusty.stub` at 7cbffb5 prints the
			// blank line too), so the label road did not write it — the one-word index position did. Same door as
			// the void-keyed `for k in d: r = r + d[k]` the CLI owed table pins; both wait on the same row.
			name:     "a text slot concatenated through a loop variable",
			src:      "d = {\"a\": \"x\", \"b\": \"y\"}\nr = \"\"\nfor k in d:\n    r = r + d[k]\nprint(r)\n",
			wantExit: 0, wantOut: "\n",
			dossier: "**Gap R.146** — the reference prints `xy`; the slot's (payload, tag) pair has no word to " +
				"travel in through a loop variable's read, and the accumulator never grows",
		},
		{
			// The same door, one arm over. The reference concatenates the slot's tuple with a tuple and prints
			// `(1, 2)`; here the runtime was handed a LIST tag for the slot's payload, so `rt_seq_concat` raises
			// its own true sentence about a program that never mentions a list. A trap the reference does not
			// raise is the worst of the three shapes, because the program dies for a reason it cannot have.
			name:     "a tuple-valued slot, concatenated",
			src:      "d = {None: (1,)}\nprint(d[None] + (2,))\n",
			wantExit: 3, wantStderr: `can only concatenate list`,
			dossier: "**Gap R.434** — the reference prints `(1, 2)`; the concatenation arm reads the slot's " +
				"tuple as a list and raises a sentence about a list the program never wrote",
		},
		{
			// The same measurement, one road over: a fractional slot declined the min/max fold (that is what
			// keeps the module out of `call i32 @gy_f(i32 %t13)` over `fadd double` and out of exit 2), so the
			// pair road answers and the pair road hands one word to the call — the fraction is gone by then.
			name:     "a fractional slot through min/max into a call",
			src:      "def f(v):\n    return v\nd = {None: 3.5}\nprint(f(max(d[None], 2)))\n",
			wantExit: 0, wantOut: "3\n",
			dossier: "**Gap R.433** — the reference prints 3.5; the fold declines a fractional slot read (the " +
				"alternative is a double in an i32 argument slot and exit 2), and the pair road it falls back to " +
				"carries one word",
		},
		{
			// The number twin of the row this commit closes, measured on the pre-row build and on this row's parent too (all three refuse):
			// the READ answers (`d = {None: 1, 0: 2}` / `k = 0` / `print(d[k])` is `2`) and the CONST-key twin
			// answers since ADR 0419 (`print(s[0] + "!!")` is `zero!!`), but the operator road over an entry
			// addressed by a name holding a number asks the static door, which has no number-in-a-name record.
			name:     "an operator road over an entry addressed by a bound number key",
			src:      "s = {None: \"a\", 0: \"zero\"}\nk = 0\nprint(s[k] + \"!!\")\n",
			wantExit: 1, wantStderr: "concatenating a string with a value that is not a string",
			dossier: "**Gap R.436** — the reference prints `zero!!`; the door can retire a name the VOID record " +
				"claims (**ADR 0420**) and has no number's value to retire a name the INT binding left behind",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				if tc.wantExit != 1 || !strings.Contains(err.Error(), tc.wantStderr) {
					t.Fatalf("%s (%s): failed in a way this pin does not describe: %v", tc.name, tc.dossier, err)
				}
				return
			}
			if res.Code != tc.wantExit {
				t.Fatalf("%s (%s): exited %d with stdout %q stderr %q — this pin expects exit %d, and an answer that arrived here means the row was closed and this table did not follow\n%s",
					tc.name, tc.dossier, res.Code, res.Output, res.Stderr, tc.wantExit, tc.src)
			}
			if tc.wantOut != "" && res.Output != tc.wantOut {
				t.Fatalf("%s (%s): printed %q, want %q", tc.name, tc.dossier, res.Output, tc.wantOut)
			}
			if tc.wantStderr != "" && !strings.Contains(res.Stderr+res.Output, tc.wantStderr) {
				t.Fatalf("%s (%s): stderr %q does not carry %q", tc.name, tc.dossier, res.Stderr, tc.wantStderr)
			}
		})
	}
}
