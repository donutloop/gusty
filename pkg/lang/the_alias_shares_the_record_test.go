package lang

import (
	"strings"
	"testing"
	"time"
)

// Roadmap Gap R.415 / ADR 0412 — the name a list was bound to answers what the name that built it
// answers, because the two names hold ONE object and the record of what its slots hold belongs to the
// object.
//
//	xs = [None, 1]            xs = [None, 1]
//	print(xs.pop(0))  # None  ys = xs
//	                          print(ys.pop(0))   # None    ← this backend, before: 0   — at exit 0
//
// Two programs that differ in the NAME the read is written against, and one of them answered. The handle
// travels through `ys = xs` — that is what an alias IS — and the three container claims do not:
// `mixedLists`, `listElemStr` and `listElemInt` are keyed by name, and the binding that shares an object
// writes none of them. So every door that consults the notebook before it consults the object consulted
// a record that had never been written, and answered from the payload: the void's `0`, the intern
// table's index, the float box's handle (ADR 0232's collision, reached by writing the most ordinary
// statement in the language beside a door that had only ever been asked about one name).
//
// What this row decided:
//
//   - the alias link is recorded at the one place that lowers an assignment, beside the door that
//     settles the kind, because the kind and the claims are the same fact about the same object;
//   - the link is resolved at READ time. A claim copied at the binding freezes the kind the list had
//     before `xs.append("a")`, and ADR 0232's promotion is exactly the event that makes a copied claim
//     a lie;
//   - the promotion writes to EVERY name that holds the object (`markListMixed`), because the write is
//     one event on one object wherever in the source it is written. Resolve-at-read without
//     promote-everywhere is half a fix: `ys.append("a")` would tell the alias and leave the name
//     claiming integers;
//   - the third answer survives. A list no record describes is still DECLINED, alias included — a door
//     that guessed a kind is how a payload gets printed under a label the object never gave it
//     (ADR 0166), which is the bug this family exists to remove, not to add;
//   - the roads gained a lookup, not a road: `popAnswerShape`, `mixedIndexRead` and the loop's tagged
//     arm all ask `listElementClaims`/`mixedListRead`, so the pop, the slot read, the loop and the
//     printer cannot be given different answers about one element by a future widening
//     (ADR 0249's gate-and-door rule, ADR 0232's one question).
//
// The measurement, over the 42 programs these two files carry as ANSWERED, with a build of the parent
// commit beside a build of this one and every program asked of the pinned CPython: **26 moved to the
// reference's answer — the popped slot through the printer, `str`, a condition, a literal element, the
// identity question and its negation, the slot read, the loop item, a chain of three names, every one of
// the three promotions — and 16 controls held: none moved away from the reference.** Three shapes stay
// owed and each is a row: the callee's own printer is **Gap R.417**'s, reached through the alias and
// byte-identical to the non-alias twin; an alias of a DICT or a SET is the same missing record one
// container over and is filed as **Gap R.419**, two wrong answers and two crashes beside it; and a slot
// of the alias bound to a name and asked as a condition is **Gap R.146**'s one-word position, refusal and
// all, which is what the non-alias twin does too.
//
// The family grew a third dimension with **ADR 0415** (**Gap R.421**, closed): the set's ORDER. A set is an
// open-addressed table in the reference and the array every reader walks is that table in bucket order, so
// the void — whose hash lands it in bucket 0 — comes before every small number, and `print(s)` and
// `for x in s` must answer the same sequence or the same set has two orders. The order is kept by the door
// that inserts (`rt_set_add_tagged` over `rt_member_bucket`), never by the printer, which is why the loop
// program sits in this table beside its `print` twin rather than in a printer file. Measurement with a
// build of the parent (**ADR 0413**) beside this one over the 89 programs the battery and these two files
// carry: **9 moved to the reference's answer, 65 controls held, none moved away**, 4 still differ — and
// those 4 are rows too: the literal road that writes members with `rt_set_elem` and no tag at all is
// **Gap R.423**, and the reference's perturb probe and its resize/rehash are **Gap R.424**.

// theAliasAnswered is the family this row pays: every read of a list asked through the name the program
// bound to it, and the control that keeps the single-kind road standing.
func theAliasAnswered() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own pair: the same three lines, asked of the two names.
		{"the name answers its own pop", "xs = [None, 1]\nprint(xs.pop(0))\nprint(xs)\n", "None\n[1]\n"},
		{"the alias answers the same pop", "xs = [None, 1]\nys = xs\nprint(ys.pop(0))\nprint(ys)\n", "None\n[1]\n"},
		// The identity question, both spellings, through the alias (Gap R.412's door, reached by R.415's link).
		{"the alias's popped slot asked whether it is the void", "xs = [None, 1]\nys = xs\nprint(ys.pop(0) is None)\n", "True\n"},
		{"and whether it is not", "xs = [None, 1]\nys = xs\nprint(ys.pop(0) is not None)\n", "False\n"},
		{"the alias's popped slot asked with ==", "xs = [None, 1]\nys = xs\nprint(ys.pop(0) == None)\n", "True\n"},
		// Which kind came out, not merely that it was not the void.
		{"a text's slot through the alias", "xs = [\"a\", \"b\"]\nys = xs\nprint(ys.pop(0))\nprint(str(ys.pop(0)))\n", "a\nb\n"},
		{"a double's slot", "xs = [1.5, 2.5]\nys = xs\nprint(ys.pop(0))\n", "1.5\n"},
		{"a nested list's slot", "xs = [[1, 2], 3]\nys = xs\nprint(ys.pop(0))\nprint(ys)\n", "[1, 2]\n[3]\n"},
		{"a dict's slot", "xs = [{\"a\": 1}, None]\nys = xs\nprint(ys.pop(0))\n", "{'a': 1}\n"},
		{"a text first and a number after", "xs = [\"a\", 1]\nys = xs\nprint(ys.pop(0))\nprint(ys)\n", "a\n[1]\n"},
		// The two other reads the link now reaches.
		{"the slot read", "xs = [None, 1]\nys = xs\nprint(ys[0])\nprint(ys[1])\n", "None\n1\n"},
		{"the loop item", "xs = [None, \"a\"]\nys = xs\nfor v in ys:\n    print(v)\n", "None\na\n"},
		{"the loop item's identity", "xs = [None, \"a\"]\nys = xs\nfor v in ys:\n    print(v is None)\n", "True\nFalse\n"},
		{"a chain of three names", "xs = [None, \"a\"]\nys = xs\nzs = ys\nprint(zs[0])\nprint(zs.pop(0) is None)\nfor v in zs:\n    print(v)\n", "None\nTrue\na\n"},
		// The positions that read the answer.
		{"the popped slot as a condition", "xs = [None, 1]\nys = xs\nif ys.pop(0):\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"the popped slot in a literal", "xs = [None, 1]\nys = xs\nprint([ys.pop(0)])\n", "[None]\n"},
		{"the popped slot out of a method", "class T:\n    def m(self):\n        xs = [None, 1]\n        ys = xs\n        return ys.pop(0) is None\n\nprint(T().m())\n", "True\n"},
		{"a negative index", "xs = [1, None]\nys = xs\nprint(ys.pop(-1) is None)\n", "True\n"},
		{"the default index", "xs = [1, None]\nys = xs\nprint(ys.pop() is None)\n", "True\n"},
		// The promotion, told from either side of the alias, and read back through either side. These three
		// are the reason the claims are COPIED at the binding as well as followed at the read: a road that
		// reads an empty record for the alias decides an incoming element needs no tag written with it, and
		// the untagged slot is what the printer then reads back.
		{"the alias mutated, the name reads back", "xs = [1, 2]\nys = xs\nys.append(\"a\")\nprint(xs.pop(2))\nprint(xs)\n", "a\n[1, 2]\n"},
		{"the source mutated, the alias reads back", "xs = [1, 2]\nys = xs\nxs.append(\"a\")\nprint(ys.pop(2))\nprint(ys)\n", "a\n[1, 2]\n"},
		{"the far end of a chain mutated, the first name reads back", "xs = [\"a\", \"b\"]\nys = xs\nzs = ys\nzs.append(7)\nprint(xs.pop(2))\nprint(xs)\n", "7\n['a', 'b']\n"},
		{"the source mutated under the alias", "xs = [\"a\", \"b\"]\nys = xs\nxs.append(1)\nprint(ys.pop(0))\nprint(ys)\n", "a\n['b', 1]\n"},
		{"the alias mutated under the name", "xs = [1, 2]\nys = xs\nys.append(\"a\")\nprint(xs.pop(0))\nprint(xs)\n", "1\n[2, 'a']\n"},
		{"append through the alias reaches the object", "xs = [1, 2]\nys = xs\nys.append(3)\nprint(xs)\nprint(ys)\nprint(ys.pop(0))\n", "[1, 2, 3]\n[1, 2, 3]\n1\n"},
		// A name's own record is what survives its source being rebound: `ys` still holds the FIRST list,
		// and the claim it was handed at the binding is the claim about THAT object.
		{"the source rebound under its own alias", "xs = [None, 1]\nys = xs\nxs = [2, None]\nprint(ys)\nprint(ys[0])\n", "[None, 1]\nNone\n"},
		// Controls: the roads that were already right, and the name that is not a list at all.
		{"the printer asks the alias", "xs = [None, 1]\nys = xs\nprint(ys)\n", "[None, 1]\n"},
		{"the length asks the alias", "xs = [None, \"a\"]\nys = xs\nprint(len(ys))\n", "2\n"},
		{"an empty alias is still false", "xs = []\nys = xs\nif ys:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"a single-kind list keeps the road it had", "xs = [7, 8, 9]\nys = xs\nprint(ys.pop(0))\nprint(ys)\n", "7\n[8, 9]\n"},
		{"a scalar alias carries no list claim", "a = 1\nb = a\nxs = [None, 1]\nprint(b)\nprint(xs[0])\n", "1\nNone\n"},
		// The two other containers, which the same door now records (roadmap **Gap R.419**). A dict's value
		// written through the alias, and a set member added through it, are the two shapes this row was
		// filed for: the entry arrived with a tag the record had nowhere to put it, so the printer rendered
		// the text's intern index, and the member reached `@str_tab` at an entry nobody had written.
		{"a dict's value written through the alias", "d = {\"a\": 1}\ne = d\ne[\"b\"] = \"x\"\nprint(d)\nprint(e)\n", "{'a': 1, 'b': 'x'}\n{'a': 1, 'b': 'x'}\n"},
		{"a dict's second key, an int, through the alias", "d = {\"a\": 1}\ne = d\ne[7] = 2\nprint(d)\n", "{'a': 1, 7: 2}\n"},
		{"the value read back through both names", "d = {\"a\": 1}\ne = d\ne[\"b\"] = \"x\"\nprint(d[\"b\"])\nprint(e[\"b\"])\nprint(len(e))\n", "x\nx\n2\n"},
		{"the alias reads what the source wrote", "d = {\"a\": 1}\ne = d\nd[\"b\"] = \"x\"\nprint(e)\nprint(d)\n", "{'a': 1, 'b': 'x'}\n{'a': 1, 'b': 'x'}\n"},
		{"the source reads what the alias wrote", "d = {\"a\": 1}\ne = d\ne[\"b\"] = 2\nd[\"c\"] = \"z\"\nprint(d)\nprint(e)\n", "{'a': 1, 'b': 2, 'c': 'z'}\n{'a': 1, 'b': 2, 'c': 'z'}\n"},
		{"a dict built empty and filled through the source", "d = {}\ne = d\nd[\"a\"] = 1\nprint(e)\nprint(len(e))\n", "{'a': 1}\n1\n"},
		{"a chain of three names, mutated at the far end", "d = {\"a\": 1}\ne = d\nf = e\nf[\"b\"] = \"x\"\nprint(d)\nprint(e)\nprint(f)\n", "{'a': 1, 'b': 'x'}\n{'a': 1, 'b': 'x'}\n{'a': 1, 'b': 'x'}\n"},
		{"a double written through the alias", "d = {\"a\": 1}\ne = d\ne[\"b\"] = 1.5\nprint(d)\nprint(e)\n", "{'a': 1, 'b': 1.5}\n{'a': 1, 'b': 1.5}\n"},
		{"the void written through the alias", "d = {\"a\": 1}\ne = d\ne[\"b\"] = None\nprint(d)\nprint(e)\n", "{'a': 1, 'b': None}\n{'a': 1, 'b': None}\n"},
		{"a nested list read out of the alias", "d = {\"a\": [1, 2]}\ne = d\nprint(e[\"a\"])\n", "[1, 2]\n"},
		{"membership asked of the alias", "d = {\"a\": 1, \"b\": 2}\ne = d\nprint(\"a\" in e)\nprint(\"z\" in e)\n", "True\nFalse\n"},
		{"a loop over the alias's keys", "d = {\"a\": 1, \"b\": 2}\ne = d\nfor k in e:\n    print(k)\n", "a\nb\n"},
		{"the alias of a dict compared with its source", "d = {\"a\": 1}\ne = d\nprint(d == e)\nd[\"b\"] = 2\nprint(d == e)\nprint(len(d) == len(e))\n", "True\nTrue\nTrue\n"},
		{"a text value read out of the alias and printed twice", "d = {\"a\": 1, \"b\": \"x\"}\ne = d\nprint(e[\"b\"])\nprint(str(e[\"b\"]))\n", "x\nx\n"},
		{"the void read out of the alias and bound", "d = {\"a\": None, \"b\": \"x\"}\ne = d\nv = e[\"a\"]\nprint(v)\n", "None\n"},
		{"the aliased dict handed to a function", "def w(x):\n    print(x)\n\nd = {\"a\": 1}\ne = d\nw(e)\nw(d)\n", "{'a': 1}\n{'a': 1}\n"},
		{"a set member added through the alias", "s = {1, 2}\nt = s\nt.add(\"a\")\nprint(s)\nprint(t)\n", "{1, 2, 'a'}\n{1, 2, 'a'}\n"},
		{"a set member added through the source", "s = {1}\nt = s\ns.add(\"a\")\nprint(t)\nprint(s)\n", "{1, 'a'}\n{1, 'a'}\n"},
		{"a chain of three names, one member added", "s = {1}\nt = s\nu = t\nu.add(\"a\")\nprint(s)\nprint(t)\nprint(u)\n", "{1, 'a'}\n{1, 'a'}\n{1, 'a'}\n"},
		{"a float added through the alias", "s = {1}\nt = s\nt.add(1.5)\nprint(t)\n", "{1, 1.5}\n"},
		{"a loop over the alias's members", "s = {1, \"a\"}\nt = s\nfor v in t:\n    print(v)\n", "1\na\n"},
		{"the alias of a set compared with its source", "s = {1, 2}\nt = s\nprint(s == t)\nt.add(3)\nprint(s == t)\n", "True\nTrue\n"},
		{"the set rebound under its own alias", "s = {1}\nt = s\ns = {2, 3}\nprint(s)\nprint(t)\n", "{2, 3}\n{1}\n"},
		// The order the members come back in, which is the reference's order and not the order the source
		// named them in (roadmap **Gap R.421**, ADR 0415). A set is an open-addressed table there and every
		// reader walks it in bucket order, so the void — whose hash lands it in bucket 0 — comes first,
		// ahead of every small number. These are the shapes that were pinned as owed bytes here until this
		// row paid them; the loop case is the one that decides WHERE the order lives, because print and the
		// loop read the same array and may not disagree.
		{"the void added to a set through the alias", "s = {1}\nt = s\nt.add(None)\nprint(t)\nprint(s)\n", "{None, 1}\n{None, 1}\n"},
		{"the void added to a set of numbers", "s = set()\ns.add(1)\ns.add(None)\nprint(s)\n", "{None, 1}\n"},
		{"a set the number joins after the void", "s = {2}\ns.add(None)\ns.add(1)\nprint(s)\n", "{None, 1, 2}\n"},
		{"the verdict added to a set of numbers", "s = {2}\ns.add(True)\nprint(s)\n", "{True, 2}\n"},
		{"the void added through the alias, the number after it", "s = {1}\nt = s\nt.add(None)\nt.add(3)\nprint(t)\n", "{None, 1, 3}\n"},
		{"the loop over a set the void joined", "s = {2}\ns.add(1)\ns.add(None)\nfor x in s:\n    print(x)\n", "None\n1\n2\n"},
		{"the member added twice is one member", "s = {1, 2}\ns.add(1)\nprint(len(s))\nprint(s)\n", "2\n{1, 2}\n"},
		{"the alias promotes and the void keeps its place", "s = {1}\nt = s\nt.add(\"a\")\nt.add(None)\nprint(s)\nprint(t)\n", "{None, 1, 'a'}\n{None, 1, 'a'}\n"},
		{"the void and the number counted", "s = {1}\ns.add(None)\nprint(len(s))\nprint(1 in s)\nprint(None in s or s is None)\n", "2\nTrue\nTrue\n"},
		{"zero and the void, both in bucket 0", "s = set()\ns.add(4)\ns.add(None)\ns.add(0)\nprint(s)\n", "{None, 0, 4}\n"},
	}
}

// TestTheAliasOfAListAnswersWhatTheNameAnswers is the answered family on both witness legs: the record's
// stdout for the same source, and the module the compiler built and ran. A MISSING record fails the
// case — an answer may not be certified by the backend that earned it (ADR 0302).
func TestTheAliasOfAListAnswersWhatTheNameAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range theAliasAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the alias road refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
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

// TestTheTwoNamesAnswerOneQuestionOneWay is this row's own asymmetry guard: one object, two names, and
// every question asked of both in the SAME program text. It is the shape the defect was measured in — a
// pop that answered under one name and printed a payload under the other — so it is pinned as a
// disagreement test rather than as twenty separate expectations a future change could satisfy
// one-sidedly (ADR 0258's one-printer rule, pointed at a name instead of a road).
func TestTheTwoNamesAnswerOneQuestionOneWay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tmpl, want string
	}{
		{"the popped slot through the printer", "xs = [None, 1]\nys = xs\nprint(%s.pop(0))\n", "None\n"},
		{"the popped slot's identity", "xs = [None, 1]\nys = xs\nprint(%s.pop(0) is None)\n", "True\n"},
		{"the popped text", "xs = [\"a\", \"b\"]\nys = xs\nprint(%s.pop(0))\n", "a\n"},
		{"the popped slot as a condition", "xs = [None, 1]\nys = xs\nif %s.pop(0):\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"the slot read", "xs = [None, 1]\nys = xs\nprint(%s[0])\n", "None\n"},
		{"the whole list", "xs = [None, 1]\nys = xs\nprint(%s)\n", "[None, 1]\n"},
		{"its length", "xs = [None, 1]\nys = xs\nprint(len(%s))\n", "2\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var byName, byAlias string
			for _, which := range []struct {
				word string
				out  *string
			}{{"xs", &byName}, {"ys", &byAlias}} {
				src := strings.Replace(tc.tmpl, "%s", which.word, 1)
				got, err := compileAndRun(t, src)
				if err != nil {
					t.Fatalf("the %s spelling refused a program the reference prints %q: %v\n%s", which.word, tc.want, err, src)
				}
				*which.out = got
			}
			if byName != tc.want {
				t.Fatalf("the name that BUILT the list answered %q, want %q\n%s", byName, tc.want, tc.tmpl)
			}
			if byAlias != tc.want {
				t.Fatalf("the name the program BOUND to it answered %q, want %q — one object, two names, one answer\n%s",
					byAlias, tc.want, tc.tmpl)
			}
		})
	}
}

// TestTheAliasSharesThePopsRoadAndInventsNoHelper is the row's claim stated as an IR fact: the alias is
// a NAME, not a new kind of container, so the removal it asks is the same `@rt_pop` the name asks, with
// the same out-parameter carrying the slot's tag, called exactly once per question.
func TestTheAliasSharesThePopsRoadAndInventsNoHelper(t *testing.T) {
	t.Parallel()
	alias := theAliasMainBody(irOf(t, "xs = [None, 1]\nys = xs\nprint(ys.pop(0) is None)\n"))
	if !strings.Contains(alias, "call i32 @rt_pop(") {
		t.Fatalf("the alias's program does not remove the slot through the one pop road:\n%s", alias)
	}
	if strings.Count(alias, "call i32 @rt_pop(") != 1 {
		t.Fatalf("one removal emitted %d removals — the door's memo is not holding for the alias\n%s",
			strings.Count(alias, "call i32 @rt_pop("), alias)
	}
	// The tag travels through the out-parameter, which is ADR 0410's signature and the only thing that
	// makes the alias's answer the OBJECT's answer rather than a guess about it.
	if !strings.Contains(alias, "i32* %") {
		t.Fatalf("@rt_pop is called without the out-parameter that carries the removed slot's tag:\n%s", alias)
	}
	// The handle is loaded from the ALIAS's own slot: the link is a lookup into the record, not a
	// redirection of the read, and a program that read the source's slot instead would follow a rebinding
	// that the alias survived — `xs = [None, 1]` / `ys = xs` / `xs = [2, None]` / `print(ys[0])`, which the
	// answered family pins, and that is why this half is pinned as an IR fact rather than trusted.
	if !strings.Contains(alias, "%_ys") {
		t.Fatalf("the alias's read does not load the handle the alias itself holds:\n%s", alias)
	}
	for _, src := range []string{
		"xs = [None, 1]\nys = xs\nprint(ys.pop(0) is None)\n",
		"xs = [None, 1]\nys = xs\nprint(ys[0])\n",
		"xs = [None, \"a\"]\nys = xs\nfor v in ys:\n    print(v)\n",
	} {
		ir := irOf(t, src)
		for _, helper := range []string{"@rt_pop_aliased", "@rt_pop_alias", "@rt_alias_", "@rt_tag_elem_aliased"} {
			if strings.Contains(ir, helper) {
				t.Fatalf("the alias invented a runtime helper (%s): the link is a compile-time lookup, not a second road\n%s", helper, src)
			}
		}
	}
}

// TestTheAliasGateFollowsTheLinkAndStillDeclines is the gate itself, asked directly: the link changes
// WHICH RECORD is asked and never WHETHER it may be guessed. The three answers ADR 0410 pinned — the
// object's own tag, the record's claim, DECLINE — must survive the alias unchanged, and the decline is
// the load-bearing one (ADR 0166, ADR 0249's gate-and-door rule).
func TestTheAliasGateFollowsTheLinkAndStillDeclines(t *testing.T) {
	t.Parallel()
	type probe struct {
		name       string
		src        string
		alias      map[string]string
		listVars   map[string]bool
		mixed      map[string]bool
		elemStr    map[string]bool
		elemInt    map[string]bool
		wantName   string
		wantObject bool
		wantOK     bool
	}
	for _, tc := range []probe{
		{
			name:     "the alias of a self-describing list lends the object's tag",
			src:      "xs = [1, \"a\"]\nys = xs\nprint(ys.pop(0))\n",
			alias:    map[string]string{"ys": "xs"},
			listVars: map[string]bool{"xs": true, "ys": true}, mixed: map[string]bool{"xs": true},
			wantName: "ys", wantObject: true, wantOK: true,
		},
		{
			name:     "the alias of a list that claims one kind lends that claim",
			src:      "xs = [\"a\", \"b\"]\nys = xs\nprint(ys.pop(0))\n",
			alias:    map[string]string{"ys": "xs"},
			listVars: map[string]bool{"xs": true, "ys": true}, elemStr: map[string]bool{"xs": true},
			wantName: "ys", wantObject: false, wantOK: true,
		},
		{
			name:     "a chain of two links is followed to the end",
			src:      "xs = [1, \"a\"]\nys = xs\nzs = ys\nprint(zs.pop(0))\n",
			alias:    map[string]string{"ys": "xs", "zs": "ys"},
			listVars: map[string]bool{"xs": true, "ys": true, "zs": true}, mixed: map[string]bool{"xs": true},
			wantName: "zs", wantObject: true, wantOK: true,
		},
		{
			name:     "a name whose own record speaks outranks the link",
			src:      "xs = [1, \"a\"]\nys = xs\nys.append(\"b\")\nprint(ys.pop(0))\n",
			alias:    map[string]string{"ys": "xs"},
			listVars: map[string]bool{"xs": true, "ys": true},
			mixed:    map[string]bool{"ys": true}, elemInt: map[string]bool{"xs": true},
			wantName: "ys", wantObject: true, wantOK: true,
		},
		{
			// The third answer, kept. An alias is a name, not a proof: following a link into an empty
			// record must not become a licence to guess.
			name:     "the alias of a list no record describes is DECLINED",
			src:      "xs = [1, \"a\"]\nys = xs\nprint(ys.pop(0))\n",
			alias:    map[string]string{"ys": "xs"},
			listVars: map[string]bool{"xs": true, "ys": true},
			wantName: "", wantObject: false, wantOK: false,
		},
		{
			name:     "a cycle in the links is a program, not a hang",
			src:      "xs = [1, \"a\"]\nprint(xs.pop(0))\n",
			alias:    map[string]string{"xs": "ys", "ys": "xs"},
			listVars: map[string]bool{"xs": true}, mixed: map[string]bool{"xs": true},
			wantName: "xs", wantObject: true, wantOK: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("the gate's own program does not parse: %v\n%s", err, tc.src)
			}
			call := firstPopCall(prog)
			if call == nil {
				t.Fatalf("no `NAME.pop(…)` in the program the gate is meant to read\n%s", tc.src)
			}
			g := &irGen{
				listVars: tc.listVars, mixedLists: tc.mixed,
				listElemStr: tc.elemStr, listElemInt: tc.elemInt,
				containerAlias: tc.alias,
			}
			name, fromObject, ok := g.popAnswerShape(call)
			if name != tc.wantName || fromObject != tc.wantObject || ok != tc.wantOK {
				t.Fatalf("popAnswerShape answered (%q, %v, %v), want (%q, %v, %v)\n%s",
					name, fromObject, ok, tc.wantName, tc.wantObject, tc.wantOK, tc.src)
			}
			_, _, served, _ := g.popAnswerPair(new(strings.Builder), call)
			if served != tc.wantOK {
				t.Fatalf("the gate says %v and the door says %v — one question, one asking place (ADR 0232)\n%s",
					tc.wantOK, served, tc.src)
			}
		})
	}
}

// TestThePromotionTellsEveryNameThatHoldsTheObject is the other half of the record's design, and the
// reason a copied claim is not simply the stale claim it looks like. A promotion is ONE event on ONE
// object, wherever in the source it is written: a notebook that told one name about it would leave the
// other names describing a list that no longer exists. The behavioural half lives in the answered family
// above (`the alias mutated, the name reads back` and its two siblings); this is the door itself.
func TestThePromotionTellsEveryNameThatHoldsTheObject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		victim string // the name the mutation is written against
	}{
		{name: "the name that built the list is appended to", victim: "xs"},
		{name: "the alias is appended to", victim: "ys"},
		{name: "the far end of the chain is appended to", victim: "zs"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &irGen{
				listVars:       map[string]bool{"xs": true, "ys": true, "zs": true, "other": true},
				mixedLists:     map[string]bool{},
				listElemStr:    map[string]bool{},
				listElemInt:    map[string]bool{"xs": true, "ys": true, "zs": true, "other": true},
				containerAlias: map[string]string{"ys": "xs", "zs": "ys"},
			}
			g.markListMixed(tc.victim)
			for _, nm := range []string{"xs", "ys", "zs"} {
				if !g.mixedLists[nm] {
					t.Fatalf("%s still claims one element kind after %s was promoted — one object, one event, three names\n%v",
						nm, tc.victim, g.mixedLists)
				}
				if g.listElemInt[nm] || g.listElemStr[nm] {
					t.Fatalf("%s still claims a single kind the object contradicts", nm)
				}
			}
			// A name that holds a DIFFERENT object keeps its claim: the promotion is about an object, and
			// the walk may not become a sweep over the notebook (which is how one list's promotion would
			// silence every list in the program).
			if !g.listElemInt["other"] || g.mixedLists["other"] {
				t.Fatalf("the promotion reached a name that holds another object: %v %v", g.mixedLists, g.listElemInt)
			}
		})
	}

	// The walk is cycle-guarded: `a = b` above `b = a` is a program, and a compiler that hung on it would
	// be the compiler's fault (ADR 0166).
	dup := map[string]string{"a": "b", "b": "a"}
	g := &irGen{
		listVars: map[string]bool{"a": true, "b": true}, mixedLists: map[string]bool{},
		listElemStr: map[string]bool{}, listElemInt: map[string]bool{"a": true, "b": true},
		containerAlias: dup,
	}
	done := make(chan []string, 1)
	go func() { done <- g.containerSharers("a") }()
	select {
	case got := <-done:
		if len(got) != 2 {
			t.Fatalf("containerSharers(\"a\") over a cycle answered %v, want both names and nothing else", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("containerSharers walked a cycle forever")
	}
}

// TestWhatTheAliasRowStillOwesIsFiledNotFixed is the honest half. Each row pins this backend's bytes
// beside the reference's and names the row that owns the fix; a row that stops being true FAILS, and the
// program moves to the answered table rather than being edited here.
func TestWhatTheAliasRowStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name, src, gap, compiled, python, why string
		crashes                               bool
	}{
		{
			name:     "the callee's printer handed the alias's popped slot",
			src:      "def w(v):\n    print(v)\n\nxs = [None, 1]\nys = xs\nw(ys.pop(0))\n",
			gap:      "Gap R.417",
			compiled: "0\n",
			python:   "None\n",
			why: "the pair crosses the frame — `pairCallScan` opens a parameter's pair for a popped slot — and the " +
				"body asks the payload. The same shape the NON-alias twin prints, byte for byte, which is what makes " +
				"it the callee's row and not this one's",
		},
		{
			name:     "a slot of the alias bound and asked as a condition",
			src:      "xs = [None, \"a\"]\nys = xs\nv = ys[0]\nif v:\n    print(\"t\")\nelse:\n    print(\"f\")\nprint(v)\n",
			gap:      "Gap R.146",
			compiled: "",
			python:   "f\nNone\n",
			why: "REFUSED, as the same program without the alias is: the binding keeps one word for its operand, so " +
				"the tag has nowhere to go. The refusal is the standing of the one-word position, not a regression " +
				"this row introduced",
		},
		// What the dict's and the set's half of the record still leaves owed, each measured on the parent
		// build `8aa3029` as well as on this one and each therefore a ROW rather than this commit's damage
		// (ADR 0166's wrong-answer class, ADR 0309's rule that an answer is filed with its bytes).
		{
			name:     "the dict rebound under its own alias",
			src:      "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\n",
			gap:      "Gap R.420",
			compiled: "{'b': 2}\n{'b': 2}\n",
			python:   "{'b': 2}\n{'a': 1}\n",
			why: "the rebinding RELEASES the object the name held (`emitFreeOld` → `rt_free`) and the allocator " +
				"hands that same slot straight to the new dict, so the alias is left holding a handle that now " +
				"denotes the second object — the list and the set answer this program correctly, which localises " +
				"it in the dict's release road and not in the notebook (the **Gap Q.2**/**Q.1** memory family, not " +
				"the alias's)",
		},
		{
			name:     "a text discarded from a set of numbers through the alias",
			src:      "s = {1, 2}\nt = s\nt.discard(\"a\")\nprint(t)\nprint(s)\n",
			gap:      "Gap R.422",
			compiled: "{1}\n{1}\n",
			python:   "{1, 2}\n{1, 2}\n",
			why: "`discard` asks the SET'S recorded kind instead of the ARGUMENT's tag, so the text's intern index " +
				"is passed as a number and the member whose payload happens to equal it is removed — a wrong answer " +
				"at exit 0 that the program with no alias answers identically, byte for byte",
		},
		{
			name:     "the set built from a literal, joined by a number",
			src:      "s = {7, 2}\ns.add(9)\nprint(s)\n",
			gap:      "Gap R.423",
			compiled: "{9, 7, 2}\n",
			python:   "{9, 2, 7}\n",
			why: "the literal road has two arms: the MIXED one fills through `rt_set_add_tagged` and so keeps the " +
				"bucket order, the single-kind one fills positionally with `rt_set_elem` beside `rt_tag_elem` — the " +
				"tags ARE written (**ADR 0187**, **ADR 0189**), what that arm withholds is the PLACE. The add door " +
				"inserts relative to the array it is handed, and that array is in the source's order, so the new " +
				"member lands ahead of two members that were never placed at all",
		},
		{
			name:     "the set built from a literal and never reordered",
			src:      "s = {3, 1, 2}\nprint(s)\n",
			gap:      "Gap R.423",
			compiled: "{3, 1, 2}\n",
			python:   "{1, 2, 3}\n",
			why: "the same road's other face: a literal's members go into the array in the order the SOURCE named " +
				"them, because nothing on that road asks the bucket question the add door now asks. One road, one " +
				"rule — until it carries the tag and the order both, a set written one way and added to another has " +
				"two orders to disagree about",
		},
		{
			name:     "the text that joins a set of numbers",
			src:      "s = set()\ns.add(3)\ns.add(\"a\")\nprint(s)\n",
			gap:      "Gap R.425",
			compiled: "{3, 'a'}\n",
			python:   "{'a', 3}\n",
			why: "the payload of a text is an intern index and the reference's hash of it is salted per " +
				"process, so `rt_member_bucket` (**ADR 0415**) answers its own \"not participating\" and the " +
				"member keeps the insertion's position, after the numbers. Over fifteen words the pinned " +
				"reference puts EIGHT of them ahead of the 3 and this backend puts none: the arm of the rule " +
				"the payload cannot answer for, recorded rather than smoothed",
		},
		{
			name:     "two members whose buckets collide",
			src:      "s = set()\ns.add(5)\ns.add(3)\ns.add(9)\ns.add(1)\nprint(s)\n",
			gap:      "Gap R.424",
			compiled: "{9, 1, 3, 5}\n",
			python:   "{9, 3, 5, 1}\n",
			why: "5, 3 and 1 all hash into the same bucket the reference's probe sequence walks with its perturb " +
				"rule (`j = (5*j + 1 + perturb) mod size`), where this door keeps equal buckets in insertion order. " +
				"The order is right for every set whose members land in distinct buckets, which is every set the " +
				"record and this family carries",
		},
		{
			name:     "the set that outgrows the first table",
			src:      "s = set()\nfor i in range(9):\n    s.add(i * 4)\nprint(s)\n",
			gap:      "Gap R.424",
			compiled: "{0, 8, 16, 24, 32, 4, 12, 20, 28}\n",
			python:   "{0, 32, 4, 8, 12, 16, 20, 24, 28}\n",
			why: "the reference resizes at three-fifths occupancy and REHASHES every member against the new size; " +
				"this door divides by the first table's 8 whatever the count. The bucket rule is stated once, in " +
				"`rt_member_bucket`, and the size is the half of it that is not yet there",
		},
		{
			name:     "a loop over the alias's key/value pairs",
			src:      "d = {\"a\": 1, \"b\": 2}\ne = d\nfor k, v in e.items():\n    print(k, v)\n",
			gap:      "Gap R.174",
			compiled: "",
			python:   "a 1\nb 2\n",
			why: "REFUSED, exactly as the same loop written against `d` is: a container method over a NAME is the " +
				"standing **Gap R.174** wall, and an alias is a name — the row this one pays is the RECORD of the " +
				"object, not a new road for the method",
		},
		{
			name:     "a set member removed by value through the alias",
			src:      "s = {1, \"a\"}\nt = s\nt.remove(1)\nprint(t)\n",
			gap:      "Gap R.174",
			compiled: "",
			python:   "{'a'}\n",
			why: "REFUSED, as `s.remove(1)` is: the same method over the same kind of NAME. Paying it wants the " +
				"tagged-method door, not this file's notebook",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if r.crashes {
				// The artifact is at fault here, which is the row's subject rather than this case's: the
				// program may not answer the reference's bytes, and nothing more is pinned than that.
				if err == nil && got == r.python {
					t.Fatalf("%s closed: the program prints the reference's bytes — delete this row and move the "+
						"program into theAliasAnswered()\n%s", r.gap, r.src)
				}
				return
			}
			if r.compiled == "" {
				if err == nil {
					t.Fatalf("%s: the shape now ANSWERS (%q) — a refusal that became an answer is a different debt; "+
						"move this row\n%s", r.gap, got, r.src)
				}
				if !strings.Contains(err.Error(), "tagged value word") && !strings.Contains(err.Error(), "Gap R.146") {
					t.Fatalf("%s: expected the one-word position's refusal, got %v\n%s", r.gap, err, r.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: the shape was REFUSED rather than answered wrongly (%v) — an answer and a refusal are "+
					"different debts\n%s", r.gap, err, r.src)
			}
			if got != r.compiled {
				t.Fatalf("%s changed shape: compiled prints %q, this row pins %q (the reference prints %q)\n%s",
					r.gap, got, r.compiled, r.python, r.src)
			}
			if got == r.python {
				t.Fatalf("%s closed: the program prints the reference's bytes — delete this row and move the "+
					"program into theAliasAnswered()\n%s", r.gap, r.src)
			}
		})
	}
}

// theAliasMainBody is the program's own `define i32 @main()` body: a claim about what a program emits is a
// claim about its code, and the runtime helpers are full of `rt_pop` definitions.
func theAliasMainBody(ir string) string {
	start := strings.Index(ir, "define i32 @main(")
	if start < 0 {
		return ""
	}
	end := strings.Index(ir[start:], "\n}")
	if end < 0 {
		return ir[start : start+len("\n}")]
	}
	return ir[start : start+end+2]
}
