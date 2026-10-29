package lang

// Membership asked of a value that has no members raises, on the compiled leg and on the record leg.
//
// roadmap Gap R.521 · ADR 0452
//
// The runtime road already answered this correctly one door away: `xs = [1]` / `print(1 in xs[0])` ends at
// exit 3 with CPython's own sentence, because there the haystack's kind is a run-time fact the object is
// asked about. What this file pins is the OTHER spelling — the haystack the front end can fold — where
// `rt_contains(i32 5, i32 1)` answered `False` and the program exited 0. A fold may substitute a verdict only
// when the verdict is the answer (**ADR 0266**, **ADR 0329**); here the answer is a raise, so the fold raises
// through the same non-local road (`exnCode`/`raiseTo`) and the same door that writes the sentence
// (`notIterableSentence`, **ADR 0271**).
//
// The integration twin (`integration/membership_fold_test.go`) asks the same program shapes at the CLI with
// each expectation read out of a live `python3` inside the case, so a row this table disagrees with is stale
// rather than green.

import (
	"strings"
	"testing"
)

// membershipTrapRows is the four kinds the reference refuses membership for, spelled as literals, plus the
// two shapes the sweep found beside them: `not in` (which asks the same question and raises the same way) and
// a chain link (`print(1 == 1 in 5)`, which reaches the same door through the chain's ordinary comparison
// road, ADR 0288).
func membershipTrapRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"an int haystack", "print(1 in 5)\n", "TypeError: argument of type 'int' is not iterable"},
		{"the void haystack", "print(1 in None)\n", "TypeError: argument of type 'NoneType' is not iterable"},
		{"a float haystack", "print(\"a\" in 1.5)\n", "TypeError: argument of type 'float' is not iterable"},
		{"a verdict haystack", "print(1 in True)\n", "TypeError: argument of type 'bool' is not iterable"},
		{"the negated question", "print(1 not in 5)\n", "TypeError: argument of type 'int' is not iterable"},
		{"a chain link asks it", "print(1 == 1 in 5)\n", "TypeError: argument of type 'int' is not iterable"},
	}
}

func TestMembershipOfAValueWithNoMembersRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range membershipTrapRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The compiled leg: exit 3, the reference's class and sentence, and no printed verdict.
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference raises in: %v\n%s", err, tc.src)
			}
			if res.Code != 3 {
				t.Fatalf("exited %d with stdout %q — membership asked of a value with no members must raise, not answer\n%s",
					res.Code, res.Output, tc.src)
			}
			if got := res.Stderr + res.Output; !strings.Contains(got, tc.want) {
				t.Fatalf("raised %q, not the reference's sentence %q\n%s", got, tc.want, tc.src)
			}
			if strings.Contains(res.Output, "False") || strings.Contains(res.Output, "True") {
				t.Fatalf("the program printed a verdict AND raised: %q\n%s", res.Output, tc.src)
			}
			// The record leg: the trap on record is the reference's, so the answer this row ships is not
			// self-certified (ADR 0302).
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			if rerr := RecordedRunError(t, tc.src); rerr == nil {
				t.Fatalf("the record says this program answers, and the compiled leg raises\n%s", tc.src)
			} else if !strings.Contains(rerr.Error(), tc.want) {
				t.Fatalf("the record leg reports %q, not the reference's sentence %q\n%s", rerr, tc.want, tc.src)
			}
		})
	}
}

// TestTheUnanswerableMembershipIsCatchable is the half that makes this a RAISE rather than a refusal: the
// program's own `except TypeError:` reaches it, at the program's own exit 0 (**ADR 0166**'s ladder — a
// front-end refusal would stop a program the reference runs with a handler on it).
func TestTheUnanswerableMembershipIsCatchable(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{
			"the handler runs instead of the print",
			"try:\n    print(1 in 5)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"the exception names its type, as the reference's does",
			"try:\n    print(\"a\" in 1.5)\nexcept TypeError as e:\n    print(str(e))\n",
			"argument of type 'float' is not iterable\n",
		},
		{
			"a caught membership raise leaves the program running",
			"hits = 0\nfor k in [1, 2]:\n    try:\n        if 1 in 5:\n            hits = hits + 1\n\n    except TypeError:\n        hits = hits + 1\n\nprint(hits)\n",
			"2\n",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference runs: %v\n%s", err, r.src)
			}
			if got != r.want {
				t.Errorf("printed %q, want %q\n%s", got, r.want, r.src)
			}
			onRecordIs(t, r.src, r.want)
		})
	}
}

// TestTheMembershipNeedleStillRuns is the order the reference uses: the needle is evaluated, THEN membership
// is asked. A raise emitted before the needle would drop an effect the program performed — the same mistake
// Gap R.520 was about, at the other end of the operator.
func TestTheMembershipNeedleStillRuns(t *testing.T) {
	t.Parallel()
	src := "def f():\n    print(\"needle\")\n\n    return 1\n\nprint(f() in 5)\n"
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("compile/JIT: %v", err)
	}
	if res.Code == 0 {
		t.Fatalf("answered %q where the reference raises\n%s", res.Output, src)
	}
	if !strings.Contains(res.Output, "needle") {
		t.Errorf("the needle's own output is missing — %q", res.Output)
	}
	if n := strings.Count(res.Output, "needle"); n != 1 {
		t.Errorf("the needle ran %d times, want 1: %q", n, res.Output)
	}
	if !strings.Contains(res.Stderr+res.Output, "argument of type 'int' is not iterable") {
		t.Errorf("the raise says %q, want the reference's sentence", res.Stderr+res.Output)
	}
}

// TestMembershipAnswersWhatHasMembers is the control table, and the load-bearing one: a fold that raises too
// eagerly turns a working language feature into a trap, which is worse than the bug this row filed. Every row
// here answered before the gate existed and must answer now, on both legs.
func TestMembershipAnswersWhatHasMembers(t *testing.T) {
	t.Parallel()
	rows := []struct{ src, want string }{
		{"print(1 in [1, 2])\n", "True\n"},
		{"print(3 not in [1, 2])\n", "True\n"},
		{"print(\"b\" in \"abc\")\n", "True\n"},
		{"print(1 in {1, 2})\n", "True\n"},
		{"print(\"k\" in {\"k\": 1})\n", "True\n"},
		{"xs = [10, 20]\nprint(20 in xs)\n", "True\n"},
		{"d = {\"a\": 1}\nprint(\"b\" in d)\n", "False\n"},
		{"print(1 in [1, 2] and \"a\" in \"abc\")\n", "True\n"},
		{"print(1 in (1, 2))\n", "True\n"},
		{"xs = [[1], [2]]\nprint([2] in xs)\n", "True\n"},
		{"s = {None, 1}\nprint(None in s)\n", "True\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", r.want, err, r.src)
			}
			if got != r.want {
				t.Fatalf("printed %q, want %q\n%s", got, r.want, r.src)
			}
			onRecordIs(t, r.src, r.want)
		})
	}
}

// The half this row left unpaid — a NAME whose kind the program settles, and a CALL every road of which
// answers a no-member literal — is paid by Gap R.522 in `membership_name_test.go`, whose
// `TestWhatTheMembershipDoorStillDeclines` is the residue table now. A residue table is a defect list with
// today's bytes: when the row that owned it pays, the table moves rather than keeping a claim that is no
// longer true.

// TestTheFoldAndTheRuntimeObjectDoorReadOneTable is the ADR 0232 claim stated as a test: the compile-time
// raise over a spelled literal and the run-time raise over a tag name the SAME set of values, because both
// read `notIterableKinds`. Before this row the runtime road spelled the list inline and the fold did not
// exist, which is how `1 in xs[0]` raised while `1 in 5` answered.
func TestTheFoldAndTheRuntimeObjectDoorReadOneTable(t *testing.T) {
	t.Parallel()
	t.Run("the table is the four kinds the reference refuses", func(t *testing.T) {
		t.Parallel()
		got := []string{}
		for _, k := range notIterableKinds {
			got = append(got, k.Name)
		}
		want := "int float bool NoneType"
		if strings.Join(got, " ") != want {
			t.Fatalf("the table reads %q, want %q — the four kinds are CPython's, not this file's", strings.Join(got, " "), want)
		}
		for _, k := range notIterableKinds {
			if !strings.Contains(notIterableSentence(k.Name), "argument of type '"+k.Name+"' is not iterable") {
				t.Fatalf("kind %s does not travel the one sentence door", k.Name)
			}
		}
	})
	t.Run("every kind in the table raises its own word from the fold", func(t *testing.T) {
		t.Parallel()
		literal := map[string]string{
			"int":      "5",
			"float":    "1.5",
			"bool":     "True",
			"NoneType": "None",
		}
		for _, k := range notIterableKinds {
			k := k
			t.Run(k.Name, func(t *testing.T) {
				t.Parallel()
				src := "print(1 in " + literal[k.Name] + ")\n"
				res, err := JIT(src, 0)
				if err != nil {
					t.Fatalf("compile/JIT: %v", err)
				}
				if res.Code == 0 {
					t.Fatalf("the fold answered the %q haystack (stdout %q)\n%s", k.Name, res.Output, src)
				}
				if got := res.Stderr + res.Output; !strings.Contains(got, notIterableSentence(k.Name)) {
					t.Errorf("the raise says %q, want %q", got, notIterableSentence(k.Name))
				}
				if got := res.Stderr + res.Output; strings.Contains(got, "object is not iterable") {
					t.Errorf("the membership door spoke the LOOP's sentence (%s) — ADR 0271's one-trap-one-wording", got)
				}
			})
		}
	})
	t.Run("the runtime door carries the same four sentences in the module", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("xs = [1]\nprint(1 in xs[0])\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		for _, k := range notIterableKinds {
			if !strings.Contains(res.IR, notIterableSentence(k.Name)) {
				t.Errorf("the tagged membership road does not carry %q — it is not reading the table", notIterableSentence(k.Name))
			}
		}
	})
}

// TestTheFoldRaisesAndAsksNoContainer is the IR claim: the fold replaces the runtime call rather than adding
// a raise beside it, and the raise is the non-local kind so the block that follows is unreachable.
func TestTheFoldRaisesAndAsksNoContainer(t *testing.T) {
	t.Parallel()
	t.Run("no rt_contains for a haystack the source says has no members", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("print(1 in 5)\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		main := inMainBody(t, res.IR)
		if strings.Contains(main, "@rt_contains(") {
			t.Errorf("@main still asks the runtime container door about an int:\n%s", main)
		}
		if !strings.Contains(main, "@exn_flag") {
			t.Errorf("@main carries no non-local raise — the trap is not catchable:\n%s", main)
		}
		if !strings.Contains(res.IR, notIterableSentence("int")) {
			t.Errorf("the module does not carry the reference's sentence:\n%s", res.IR)
		}
	})
	t.Run("a real container still asks the container door and raises nothing", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("print(1 in [1, 5])\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if main := inMainBody(t, res.IR); strings.Contains(main, "argument of type") {
			t.Errorf("the membership raise is in a program that answers:\n%s", main)
		}
	})
}
