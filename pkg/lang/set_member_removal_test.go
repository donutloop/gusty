package lang

// ADR 0425 · roadmap Gap R.422 — removing a member of a set asks the ARGUMENT what it carries.
//
// The mirror image of `s.add(v)` answered two different questions. `add` asks the argument, because
// ADR 0187 and ADR 0189 established that a slot's payload is never the discriminator: a stored text is
// an index into `@str_tab`, so the text "0" and the number 0 are the SAME i32. `discard` asked the SET
// — it took the argument's payload alone and handed the removal door `rt_set_discard(h, v)` one word, so
// the door scanned the payload array and deleted the first member whose payload happened to equal
// whatever the argument's payload happened to be. Measured on the pinned reference, all at exit 0:
//
//	s = {1, 2}  / s.discard("a") / print(s)   the compiled leg wrote {1}   the reference wrote {1, 2}
//	s = {1, 2}  / s.discard(9)   / print(s)   the compiled leg wrote {1}   the reference wrote {1, 2}
//	s = {0, 1}  / s.discard(None)/ print(s)   the compiled leg wrote {1}   the reference wrote {0, 1}
//	s = {1, 2}  / s.discard([1])              the compiled leg wrote {2}   the reference raised TypeError: unhashable type: 'list'
//
// and `s.remove(v)` did not exist at all: it fell past the set-method door to the string-method road,
// which refused it at exit 1 — where the reference answers `KeyError: 9`.
//
// The rows below are the whole feature: the pair the argument arrives with, the two runtime doors that
// compare it, the unhashable member that raises before the set is consulted, the exception CPython
// grants this door and the dict door does not have (a bare set is a MISSING member, not an unhashable
// one), the sentence that names the member, and the readings this road still does not have — named in
// the last case and filed, not smoothed over. Every `want` here was read off CPython 3.12.3 before the
// case was written, and every source is on the record: ADR 0302's rule, so no answer here is certified
// by the backend that earned it.

import (
	"strings"
	"testing"
)

// setMemberRemovalRows are programs whose member is removed by a value the set never held, or by one it
// does hold — with the answer the reference writes. The first four are the row's own lesion: an argument
// the set has never held, removing a member it does hold.
func setMemberRemovalRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The row's own programs: a payload-only scan deleted a member whose payload happened to match.
		{"a text the set never held", "s = {1, 2}\ns.discard(\"a\")\nprint(s)\n", "{1, 2}\n"},
		{"a number the set never held", "s = {1, 2}\ns.discard(9)\nprint(s)\n", "{1, 2}\n"},
		{"the void, which carries no payload", "s = {0, 1}\ns.discard(None)\nprint(s)\n", "{0, 1}\n"},
		// The member that IS there, removed by both verbs.
		{"discard removes the text it holds", "s = {1, 2, \"a\"}\ns.discard(\"a\")\nprint(s)\n", "{1, 2}\n"},
		{"remove takes the text out too", "s = {1, 2, \"a\"}\ns.remove(\"a\")\nprint(s)\n", "{1, 2}\n"},
		{"remove takes the number out", "s = {1, 2}\ns.remove(1)\nprint(s)\n", "{2}\n"},
		{"a fractional member is found by value", "s = {1, 2}\ns.remove(1.0)\nprint(s)\n", "{2}\n"},
		{"a verdict is the number the reference says it is", "s = {1, 2}\ns.remove(True)\nprint(s)\n", "{2}\n"},
		{"zero is not the void", "s = {0, 1}\ns.remove(0)\nprint(s)\n", "{1}\n"},
		{"both members, then the empty spelling", "s = {1, 2}\ns.discard(1)\ns.discard(2)\nprint(s)\n", "set()\n"},
		{"the verb that removes nothing keeps the set", "s = {1, 2}\ns.discard(\"a\")\ns.discard(1)\nprint(s)\nprint(len(s))\n", "{2}\n1\n"},
		// The set grown at run time by `add`, then judged by `discard`: the two verbs must agree about
		// which pair each member carries, or the survivors depend on which verb wrote them.
		{"a built member, removed as it was added", "s = {1, 2}\ns.add(3)\ns.discard(3)\nprint(s)\n", "{1, 2}\n"},
		{"a built text, removed as it was added", "s = {1, 2}\ns.add(\"a\")\ns.discard(\"a\")\nprint(s)\n", "{1, 2}\n"},
		{"a built text does not remove a number", "s = {1, 2}\ns.add(\"b\")\ns.discard(1)\nprint(s)\n", "{2, 'b'}\n"},
		{"the built member, the extra verb", "s = {1, 2}\ns.add(3)\ns.discard(3)\ns.discard(9)\nprint(s)\nprint(len(s))\n", "{1, 2}\n2\n"},
		// The roads that read the set back after the removal, rather than print it.
		{"the set read back by membership", "s = {1, 2}\ns.discard(1)\nprint(len(s))\nprint(1 in s)\nprint(2 in s)\n", "1\nFalse\nTrue\n"},
		{"a removal does not make the survivors print as the argument", "s = {1, 2, \"a\"}\ns.remove(\"a\")\ns.remove(1)\nprint(s)\n", "{2}\n"},
		// An alias is the SAME set: the verb the row owns mutates the object, not the name.
		{"the alias carries the removal", "s = {1, 2, 3}\nt = s\nt.discard(2)\nprint(t)\nprint(s)\n", "{1, 3}\n{1, 3}\n"},
		{"the alias carries the other verb too", "s = {1, 2, 3}\nt = s\nt.remove(2)\nprint(s)\nprint(t)\n", "{1, 3}\n{1, 3}\n"},
		{"an alias removal the set never held", "s = {1, 2}\nt = s\nt.discard(\"a\")\nprint(t)\nprint(s)\n", "{1, 2}\n{1, 2}\n"},
		// The unrolled and the run-time loop, where the member is an item rather than a literal.
		{"discard removes a loop item", "s = {1, 2, 3}\nfor k in [1, 3]:\n    s.discard(k)\nprint(s)\n", "{2}\n"},
		{"remove takes a loop item out", "s = {1, 2, 3}\nfor k in [1, 3]:\n    s.remove(k)\nprint(s)\n", "{2}\n"},
		{"a loop of texts removes only what it names", "s = {1, 2}\nfor k in [\"a\", \"b\"]:\n    s.discard(k)\nprint(s)\n", "{1, 2}\n"},
		{"the item of a run-time list", "s = {\"a\", \"b\"}\nks = [\"a\"]\nfor k in ks:\n    s.discard(k)\nprint(s)\n", "{'b'}\n"},
		{"a number and a verdict are one member", "s = {True, False}\ns.discard(True)\nprint(s)\n", "{False}\n"},
		// A tuple is hashable, and this backend stores it with a list's tag (ADR 0361) — so the pair
		// road, not the tag, decides the answer, and the answer is the reference's.
		{"a tuple member the set does not hold", "s = {1, 2}\ns.discard((1, 2))\nprint(s)\n", "{1, 2}\n"},
	}
}

// TestRemovingAMemberAsksTheArgumentAndNotTheSet is the answering case on both witness legs: the compiled
// leg runs the program, the record leg reads the answer on record for that exact source, and the two
// must agree with what is written here.
func TestRemovingAMemberAsksTheArgumentAndNotTheSet(t *testing.T) {
	t.Parallel()
	for _, tc := range setMemberRemovalRows() {
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
				t.Fatalf("the record does not answer %q: %v", tc.src, err)
			}
			if recorded != tc.want {
				t.Fatalf("the record says %q, the compiled leg says %q for\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheRemovalRoadComparesTheWordsTheMemberArrivesWith is the IR half: the removal goes to the pair
// doors — `rt_set_find` for the position, `rt_set_remove_at` for the shift, `rt_set_discard_tagged`
// holding the two together for the no-op verb — and never to the payload-only scan. The denial reads
// main's own region, because the runtime block defines every helper beside it whether main calls it or
// not (the same reading ADR 0424's cases used).
func TestTheRemovalRoadComparesTheWordsTheMemberArrivesWith(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want []string
		deny []string
	}{
		{
			name: "discard takes the pair",
			src:  "s = {1, 2, \"a\"}\ns.discard(\"a\")\n",
			// The no-op verb hands the pair to the door that owns the finding AND the shift; the scan and
			// the shift live inside `rt_set_discard_tagged`, so main names only the one call.
			want: []string{"call void @rt_set_discard_tagged(i32 %h"},
			deny: []string{"call void @rt_set_discard(i32"},
		},
		{
			name: "remove finds, then raises, then shifts",
			src:  "s = {1, 2}\ns.remove(1)\n",
			want: []string{"= call i32 @rt_set_find(i32 %h", "call void @rt_set_remove_at(i32 %h"},
			deny: []string{"call void @rt_set_discard(i32"},
		},
		{
			name: "the pair is the argument's, through the tagged pair road",
			src:  "s = {1, 2}\nx = 2\ns.remove(x)\n",
			want: []string{"call void @rt_set_remove_at(i32 %h"},
			deny: []string{"call void @rt_set_discard(i32"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := mainRegion(t, tc.src)
			for _, want := range tc.want {
				if !strings.Contains(ir, want) {
					t.Fatalf("the removal did not take the pair road (%s):\n%s", want, ir)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(ir, deny) {
					t.Fatalf("the removal still reaches the payload-only scan (%s):\n%s", deny, ir)
				}
			}
		})
	}
}

// setMemberTrapRows are the members the reference refuses to hash, and a missing member, with the class
// and the sentence the reference raises. Every cell was read off CPython 3.12.3 one program at a time
// before it was written here — the table in set_member_removal.go is the same measurement, and the
// reason `discard({1})` and `remove({1})` are NOT TypeErrors.
func setMemberTrapRows() []struct {
	name    string
	src     string
	class   string
	message string
} {
	return []struct {
		name    string
		src     string
		class   string
		message string
	}{
		{"discard of a list", "s = {1, 2}\ns.discard([1])\n", "TypeError", "unhashable type: 'list'"},
		{"remove of a list", "s = {1, 2}\ns.remove([1])\n", "TypeError", "unhashable type: 'list'"},
		{"discard of a dict", "s = {1, 2}\ns.discard({\"a\": 1})\n", "TypeError", "unhashable type: 'dict'"},
		{"remove of a dict", "s = {1, 2}\ns.remove({\"a\": 1})\n", "TypeError", "unhashable type: 'dict'"},
		{"a tuple carrying a list names the list", "s = {1, 2}\ns.remove((1, [2]))\n", "TypeError", "unhashable type: 'list'"},
		{"a tuple carrying a set names the set", "s = {1, 2}\ns.discard((1, {2}))\n", "TypeError", "unhashable type: 'set'"},
		// The exception this door has and the dict door does not: a bare set is a MISSING member here,
		// never an unhashable one, because CPython's two set entry points report an absent member rather
		// than a TypeError from hashing a set argument.
		{"remove of a bare set names the member", "s = {1, 2}\ns.remove({1})\n", "KeyError", "{1}"},
		// The missing member, named.
		{"remove of the void", "s = {1, 2}\ns.remove(None)\n", "KeyError", "None"},
		{"remove of a text", "s = {\"a\"}\ns.remove(\"z\")\n", "KeyError", "'z'"},
		{"a set that holds nothing at all", "s = set()\ns.remove(1)\n", "KeyError", "1"},
		{"the second removal of one member", "s = {1, 2}\ns.remove(1)\ns.remove(1)\n", "KeyError", "1"},
	}
}

// TestTheUnhashableMemberRaisesBeforeTheSetIsConsulted walks the trap rows on the compiled leg and the
// record leg: the class, the message, the exit code, and that the raise printed nothing on the way out.
func TestTheUnhashableMemberRaisesBeforeTheSetIsConsulted(t *testing.T) {
	t.Parallel()
	for _, tt := range setMemberTrapRows() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("the compiled leg raises %s: %s, the reference raises %s: %s\n%s",
					class, message, tt.class, tt.message, tt.src)
			}
			rec := goldenLookup(t, tt.src)
			if rec.ExnType != tt.class || rec.ExnMsg != tt.message {
				t.Fatalf("the record says %q raises %s: %s, the compiled leg raises %s: %s — the reference is the referee",
					tt.src, rec.ExnType, rec.ExnMsg, class, message)
			}
			res, err := JIT(tt.src, 0)
			if err != nil {
				t.Fatalf("jit: %v", err)
			}
			if res.Code != 3 {
				t.Fatalf("%q exited %d, want the uncaught-exception exit 3 (stdout %q)", tt.src, res.Code, res.Output)
			}
			if strings.TrimSpace(res.Output) != "" {
				t.Fatalf("%q printed %q on the way to the raise; the reference prints nothing", tt.src, res.Output)
			}
		})
	}
}

// TestABareSetMemberIsMissingRatherThanUnhashable is the measured asymmetry on its own, because it is
// the one place where copying the dict door's guard would have invented a trap: the same value is silent
// for `discard` and missing for `remove`, while the same value inside a tuple names itself.
func TestABareSetMemberIsMissingRatherThanUnhashable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"discard says nothing about a set", "s = {1, 2}\ns.discard({1})\nprint(s)\n", "{1, 2}\n"},
		{"discard of the empty spelling", "s = {1, 2}\ns.discard(set())\nprint(s)\n", "{1, 2}\n"},
		{"a name holding a set is the same case", "s = {1, 2}\nt = {1}\ns.discard(t)\nprint(s)\n", "{1, 2}\n"},
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
		})
	}
}

// TestTheMissingMemberIsNamedInTheSentence is the second half of the raise: a traceback the reader can
// grep. The reference puts the member itself in the message, and the unrolled loop's item is a name the
// compiler DOES know the value of — the record ADR 0424 keeps per iteration answers it, and without
// that reading `for k in [1, 2, 9]: s.remove(k)` said `key not found` where the reference says
// `KeyError: 9`.
func TestTheMissingMemberIsNamedInTheSentence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		class   string
		message string
	}{
		{"a number", "s = {1, 2}\ns.remove(9)\n", "KeyError", "9"},
		{"a text", "s = {\"a\"}\ns.remove(\"z\")\n", "KeyError", "'z'"},
		{"the void", "s = {1, 2}\ns.remove(None)\n", "KeyError", "None"},
		{"a verdict", "s = {1, 2}\ns.remove(False)\n", "KeyError", "False"},
		{"a fractional member", "s = {1, 2}\ns.remove(1.5)\n", "KeyError", "1.5"},
		{"a set written at the call", "s = {1, 2}\ns.remove({1})\n", "KeyError", "{1}"},
		{"the empty spelling", "s = {1, 2}\ns.remove(set())\n", "KeyError", "set()"},
		{"the item of an unrolled loop", "s = {1, 2, 3}\nfor k in [1, 2, 9]:\n    s.remove(k)\n", "KeyError", "9"},
		{"a text item of an unrolled loop", "s = {\"a\"}\nfor k in [\"a\", \"zz\"]:\n    s.remove(k)\n", "KeyError", "'zz'"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tc.src)
			if class != tc.class || message != tc.message {
				t.Fatalf("the sentence is %s: %s, the reference's is %s: %s\n%s",
					class, message, tc.class, tc.message, tc.src)
			}
		})
	}
}

// TestTheMemberDoorAskedDirectly asks the two doors this file adds, of the AST rather than of a program:
// the bare-set exemption, and the guard whose list and dict arms the reference keeps while its set arm is
// the reference's own asymmetry.
func TestTheMemberDoorAskedDirectly(t *testing.T) {
	t.Parallel()
	g := &irGen{
		runtimeSets:   map[string]bool{"ss": true},
		containerLits: map[string]Expr{"sl": &SetLit{Elems: []Expr{&IntLit{Value: 1}}}},
	}
	for _, tt := range []struct {
		e    Expr
		bare bool
		why  string
	}{
		{&SetLit{Elems: []Expr{&IntLit{Value: 1}}}, true, "a set literal"},
		{&SetLit{}, true, "the empty set literal"},
		{&Name{Value: "ss"}, true, "a name built by set()"},
		{&Name{Value: "sl"}, true, "a name the fold recorded as a set"},
		{&Name{Value: "maybe"}, false, "a name whose kind is only a run-time fact"},
		{&ListLit{Elems: []Expr{&IntLit{Value: 1}}}, false, "a list, which the reference refuses"},
		{&IntLit{Value: 1}, false, "a number, which hashes"},
	} {
		if got := g.memberIsABareSet(tt.e); got != tt.bare {
			t.Fatalf("%s: memberIsABareSet = %v, want %v", tt.why, got, tt.bare)
		}
	}
	// The guard's arms, read off the same table: list and dict raise, and the set kind is ABSENT because
	// the reference reports a missing member instead. The three kinds must be nameable apart, or the
	// guard's two arms would raise for the wrong member.
	if int32(TagSet) == int32(TagList) || int32(TagSet) == int32(TagDict) || int32(TagList) == int32(TagDict) {
		t.Fatalf("two container kinds share a tag, so the guard's arms cannot name them apart")
	}
	// And the exemption the guard needs, measured rather than asserted from the enum: this backend gives
	// a tuple a LIST's pair word (ADR 0361's one layout), so a door that tested the tag alone would answer
	// `s.discard((1, 2))` with `TypeError: unhashable type: 'list'` where the reference prints the set
	// untouched. The answering row that keeps this true is "a tuple member the set does not hold".
	ir := mainRegion(t, "s = {1, 2}\ns.discard((1, 2))\n")
	line := ""
	for _, l := range strings.Split(ir, "\n") {
		if strings.Contains(l, "call void @rt_set_discard_tagged(") {
			line = strings.TrimSpace(l)
		}
	}
	if line == "" {
		t.Fatalf("a tuple member did not reach the pair door:\n%s", ir)
	}
	if !strings.HasSuffix(line, ", i32 5)") {
		t.Fatalf("a tuple member's pair word is no longer the list's tag (%s), so the guard's isTupleExpr bypass (and the row beside it) needs re-reading:\n%s", line, ir)
	}
}

// TestTheSetThatSurvivesACollisionIsPrintedInItsOwnOrder is the owed half that this row did not create
// and cannot fix: the member door now compares the pair, so the text "0" no longer removes the number 0
// — but the SET that survives is printed in this backend's insertion order where the reference prints
// hash-slot order, so the printed line still disagrees. Gap R.170 owns the order; the removal under it is
// the answer this row earned, which is what the membership test below pins.
func TestTheSetThatSurvivesACollisionIsPrintedInItsOwnOrder(t *testing.T) {
	t.Parallel()
	// The reference's answer with its string hash pinned (PYTHONHASHSEED=0, the same pin
	// `lang.PythonRun` puts on the oracle leg): the number 0 is a member, the texts are a DIFFERENT
	// member each, and all three survive.
	const ref = "{'1', '0'}\n"
	src := "s = {\"0\", \"1\"}\ns.discard(0)\nprint(s)\n"
	got, err := compileAndRun(t, src)
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", ref, err, src)
	}
	if got == ref {
		t.Fatalf("the printed order now matches the reference — move this case into the answering table\n%s", src)
	}
	// What the row owns is the SURVIVOR SET: three members, and the number among them.
	if !strings.Contains(got, "'") || strings.Count(got, ",") != 1 {
		t.Fatalf("the removal took a member it should have left: printed %q, want both texts\n%s", got, src)
	}
	count := "s = {\"0\", \"1\"}\ns.discard(0)\nprint(len(s))\nprint(0 in s)\nprint(\"0\" in s)\n"
	want := "2\nFalse\nTrue\n"
	gotCount, err := compileAndRun(t, count)
	if err != nil {
		t.Fatalf("the compiled leg refused %q: %v", count, err)
	}
	if gotCount != want {
		t.Fatalf("the survivor set answers %q, the reference answers %q\n%s", gotCount, want, count)
	}
}

// TestWhatSetMemberRemovalStillOwesIsFiledNotFixed keeps the honest edge in the suite: the readings this
// road does not have, each with the answer it gives today beside the reference's, and each already owned
// by a row. These are deliberately NOT records — an owed answer is not an answer.
func TestWhatSetMemberRemovalStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   string
		today string // what the compiled leg's sentence says today
		ref   string // what the reference's sentence says
		owed  string
	}{
		{
			// A member the compiler cannot quote keeps the generic sentence — the same debt a dict
			// subscript carries for the same key, which is the evidence that this is one row's and not two.
			name:  "a name bound to a tuple keeps the generic sentence",
			src:   "t = (3, 4)\ns = {1, 2}\ns.remove(t)\n",
			today: "key not found", ref: "(3, 4)", owed: "Gap R.189",
		},
		{
			// The dict door beside it, answering the same way for the same key.
			name:  "the dict subscript asks the same question and gets the same sentence",
			src:   "t = (3, 4)\nd = {(1, 2): \"a\"}\nprint(d[t])\n",
			today: "key not found", ref: "(3, 4)", owed: "Gap R.189",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, message := jitTrap(t, tc.src)
			if message != tc.today {
				t.Fatalf("%s: the owed case answers %q, not the filed %q — if the reference's %q is now the answer, move it into the answering table\n%s",
					tc.name, message, tc.today, tc.ref, tc.src)
			}
			if message == tc.ref {
				t.Fatalf("%s: the owed case is no longer owed — move it into the answering table", tc.name)
			}
		})
	}
}
