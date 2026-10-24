package lang

import (
	"regexp"
	"strings"
	"testing"
)

// rebind_kind_test.go — a container variable's kind is the kind of the object its LATEST binding gave
// it, and exactly one door writes that fact (roadmap Gap Q.1 · owner L11.1; ADR 0341).
//
// `d = {1: 10}` then `d = [3, 4]` is two programs to the object and one program to the reader. The
// assignment frees the dict, allocates a list and stores the handle; the read then asks a notebook the
// compiler kept by *name*. Before this row six binding roads wrote those fourteen name-keyed claims and
// no two wrote them the same way — the dict road cleared the list and set claims, the set road cleared the
// list and dict claims, the comprehension road cleared four of them, the non-container road cleared three,
// and the **list-literal road cleared nothing at all**. The measured result, all of it at the exit code
// that means the program ran:
//
//	d = {1: 10}; d = [3, 4]
//	print(d)      # python3 [3, 4]      this backend: {3: 4, 0: 0}      AT EXIT 0
//	print(d[0])   # python3 3           this backend: 0                AT EXIT 0
//	print(d[1])   # python3 4           this backend: KeyError: 1
//	d.append(9)
//	print(d)      # python3 [3, 4, 9]   this backend: {3: 4, 9: 0, 0: 0}
//	d = {1: "x"}; d = [2, 3]; print(d)  # this backend: llc rejects the module → EXIT 2
//
// The three verdicts a door can honestly give are all in this file too: ANSWER (the last binding's kind is
// the one every read uses), RAISE (the last binding's kind decides which of CPython's sentences the trap
// speaks), and REFUSE (what needs the run-time object — a parameter, a value across a call — is named, not
// guessed; those rows belong to L11.1 and are pinned in TestWhatTheRebindingRowDoesNotOwn).

// reboundKindParity is the answered family. `want` is what CPython prints for the same source (each row
// re-measured with /usr/bin/python3 whenever this file is extended), and every row is asked of the record
// leg, of the module the compiler writes, of the JIT, and in integration/rebind_kind_test.go of the CLI.
var reboundKindParity = []struct {
	src, want string
}{
	// the five programs the row was filed on
	{"d = {1: 10}\nd = [3, 4]\nprint(d)", "[3, 4]"},
	{"d = {1: 10}\nd = [3, 4]\nprint(d[0])", "3"},
	{"d = {1: 10}\nd = [3, 4]\nprint(d[1])", "4"},
	{"d = {1: 10}\nd = [3, 4]\nprint(d[-1])", "4"},
	{"d = {1: 10}\nd = [3, 4]\nd.append(9)\nprint(d)", "[3, 4, 9]"},
	{"d = {1: \"x\"}\nd = [2, 3]\nprint(d)", "[2, 3]"},
	// the reads that had been asking the dict the name no longer holds
	{"d = {1: 10}\nd = [3, 4]\nprint(len(d))", "2"},
	{"d = {1: 10}\nd = [3, 4]\nprint(3 in d)", "True"},
	{"d = {1: 10}\nd = [3, 4]\nprint(9 in d)", "False"},
	{"d = {1: 10}\nd = [3, 4]\nfor v in d:\n    print(v)", "3\n4"},
	{"d = {1: 10}\nd = [3, 4]\nprint(str(d))", "[3, 4]"},
	{"d = {1: 10}\nd = [3, 1, 2]\nd.sort()\nprint(d)", "[1, 2, 3]"},
	{"d = {1: 10}\nd = [3, 'a']\nprint(d)", "[3, 'a']"},
	{"d = {'a': 1}\nd = [7, 8]\nprint(d[0])", "7"},
	{"d = {1: 'x'}\nd = [2, 3]\nprint(d[1])", "3"},
	// the directions that already answered must keep answering — the row is symmetric by rule, not by luck
	{"xs = [3, 4]\nxs = {1: 10}\nprint(xs)", "{1: 10}"},
	{"xs = [3, 4]\nxs = {1: 10}\nprint(xs[1])", "10"},
	{"xs = [3, 4]\nxs = {7}\nprint(7 in xs)", "True"},
	{"s = {1, 2}\ns = [5, 6]\nprint(s)", "[5, 6]"},
	{"s = {1, 2}\ns = [5, 6]\nprint(s[0])", "5"},
	{"s = {1, 2}\ns = [5, 6]\ns.append(7)\nprint(s)", "[5, 6, 7]"},
	{"s = {1, 2}\ns = [5, 6]\nprint(5 in s)", "True"},
	{"s = {2, 3}\ns = [2, 3]\nprint(s[1])", "3"},
	// the detour through a scalar: the container claims retire with the container
	{"v = [1, 2]\nv = {1: 2}\nv = 7\nprint(v)", "7"},
	{"v = {1: 2}\nv = 5\nv = [7, 8]\nprint(v[0])", "7"},
	{"v = {1, 2}\nv = 5\nprint(v)", "5"},
	{"x = [1, 2]\nx = {1: 2}\nx = {5, 6}\nprint(5 in x)", "True"},
	{"x = [1, 2]\nx = {1: 2}\nx = {5, 6}\nprint(x)", "{5, 6}"},
	// the same kind, a new object: nothing may be inherited from the freed container
	{"xs = [1, 2]\nxs = [7, 8]\nprint(xs[0])", "7"},
	{"xs = [1, 2]\nxs = [7, 8]\nprint(len(xs))", "2"},
	{"xs = [\"a\"]\nxs = [1, 2]\nprint(xs)", "[1, 2]"},
	{"xs = [1, 2]\nxs = [\"a\"]\nprint(xs)", "['a']"},
	{"xs = [\"a\"]\nxs = [1, 2]\nxs.append(3)\nprint(xs)", "[1, 2, 3]"},
	{"xs = [\"a\"]\nxs = [1, 2]\nprint(xs[0])", "1"},
	{"xs = [1, 2]\nxs = [\"a\", \"b\"]\nprint(xs[1])", "b"},
	{"d = {\"a\": 1}\nd = {2: 3}\nprint(d)", "{2: 3}"},
	{"d = {\"a\": 1}\nd = {2: 3}\nprint(d[2])", "3"},
	{"s = {\"a\"}\ns = {1}\nprint(s)", "{1}"},
	// an answer handed back by a call settles the kind the same way
	{"def two_lists():\n    return [7, 8]\n\nt = [1, 2]\nt = two_lists()\nprint(t)", "[7, 8]"},
	{"def two_dicts():\n    return {\"m\": 9}\n\nt = [1, 2]\nt = two_dicts()\nprint(t)", "{'m': 9}"},
	{"def two_lists():\n    return [7, 8]\n\nt = {1: 2}\nt = two_lists()\nprint(t[1])", "8"},
	// a comprehension binds a container too
	{"sq = []\nfor i in [1, 2, 3]:\n    sq.append(i * i)\nprint(sq)", "[1, 4, 9]"},
	{"sq = [1, 2]\nsq = [x for x in [4, 5, 6]]\nprint(sq)", "[4, 5, 6]"},
	{"sq = {1, 2}\nsq = [x for x in [4, 5]]\nprint(sq[0])", "4"},
	// inside a function body, where the binding is emitted under a convention
	{"def f():\n    d = {1: 10}\n    d = [3, 4]\n    print(d[0])\n    print(d)\n\nf()", "3\n[3, 4]"},
	{"def f():\n    xs = [1, 2]\n    xs = {\"a\": 1}\n    print(xs[\"a\"])\n\nf()", "1"},
	// a rebinding that shares a handle: the name being rebound reads its own new object
	{"a = {1: 2}\nb = a\nb = [7, 8]\nprint(b[0])", "7"},
	{"a = {1: 2}\nb = a\nb = [7, 8]\nprint(b)", "[7, 8]"},
	// a text and a number index into the container the last binding made
	{"xs = {1: 2}\nxs = [3, 4]\nprint(xs[0], xs[1])", "3 4"},
	{"d = [1, 2]\nd = {\"k\": 1, \"j\": 2}\nprint(d[\"j\"])", "2"},
	// a mixed container rebound to a homogeneous one: the "my slots describe themselves" bit
	// belongs to the object, and the readers must not still walk per-slot tags nobody wrote
	{"xs = [1, \"a\"]\nxs = [1, 2]\nprint(xs)", "[1, 2]"},
	{"xs = [1, \"a\"]\nxs = [1, 2]\nprint(xs[0])", "1"},
	{"xs = [1, \"a\"]\nxs = [1, 2]\nxs.sort()\nprint(xs)", "[1, 2]"},
	{"d = {1: \"x\", \"a\": 2}\nd = {1: 2}\nprint(d)", "{1: 2}"},
	{"d = {1: \"x\", \"a\": 2}\nd = {1: 2}\nprint(d[1])", "2"},
}

// TestAReboundContainerAnswersWhatTheLatestBindingGaveIt runs the answered family on both witness legs:
// the record (the answer the retired engine left behind for the source) and the compiled backend. One
// table on purpose — an answer asserted on one leg and not the other is a row nobody measured.
func TestAReboundContainerAnswersWhatTheLatestBindingGaveIt(t *testing.T) {
	t.Parallel()
	for _, tc := range reboundKindParity {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tc.src) {
				t.Errorf("%q has no entry in the record leg (pkg/lang/testdata/interpreter-golden.json); a new answer needs a record in the same commit", tc.src)
			}
			got, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tc.src, got, tc.want)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Fatalf("%q printed %q, want CPython's %q", tc.src, strings.TrimSpace(got), tc.want)
			}
			out, trap := runForParity(t, tc.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tc.src, trap, tc.want)
			}
			if out != tc.want {
				t.Fatalf("%q printed %q through the JIT, want CPython's %q", tc.src, out, tc.want)
			}
		})
	}
}

// TestTheContainerKindTrapNamesTheContainerTheLastBindingMade: the trap is the program's, and its sentence
// belongs to the object the last binding made. A dict's claim answering for a list would raise the wrong
// class, which ADR 0212 does not allow to be handled by an `except` that never matched the program.
func TestTheContainerKindTrapNamesTheContainerTheLastBindingMade(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src            string
		class, message string
	}{
		{"d = {1: 10}\nd = [3, 4]\nprint(d[9])", "IndexError", "list index out of range"},
		{"d = {1: 10}\nd = [3, 4]\nprint(d[\"a\"])", "TypeError", "list indices must be integers or slices, not str"},
		{"s = {1, 2}\ns = [5]\nprint(s[3])", "IndexError", "list index out of range"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tc.src)
			if class != tc.class || message != tc.message {
				t.Fatalf("%q raised %s: %s, want %s: %s", tc.src, class, message, tc.class, tc.message)
			}
		})
	}
}

// TestTheContainerKindTrapIsCatchable keeps the raise inside the program: ADR 0218's handled exception is
// over, and the name after the arm is still the container the last binding made.
func TestTheContainerKindTrapIsCatchable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{"d = {1: 10}\nd = [3, 4]\ntry:\n    print(d[9])\nexcept IndexError:\n    print(\"caught\")", "caught"},
		{"d = {1: 10}\nd = [3, 4]\ntry:\n    print(d[\"a\"])\nexcept TypeError:\n    print(\"caught\")", "caught"},
		{"d = {1: 10}\nd = [3, 4]\ntry:\n    print(d[9])\nexcept IndexError:\n    print(d[0])", "3"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, trap := runForParity(t, tc.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tc.src, trap, tc.want)
			}
			if out != tc.want {
				t.Fatalf("%q printed %q, want %q", tc.src, out, tc.want)
			}
		})
	}
}

// seedClaims is the state a name can arrive at: every container claim the compiler keeps, all set for it.
// The gate tables below start from it so "cleared" and "was never set" are different answers.
func seedClaims(g *irGen, name string) {
	for _, m := range []map[string]bool{
		g.listVars, g.mixedLists, g.runtimeDicts, g.mixedDicts, g.runtimeSets, g.mixedSets,
		g.listElemStr, g.listElemInt, g.setElemStr, g.setElemInt,
		g.dictKeyStr, g.dictKeyInt, g.dictValStr, g.dictValInt,
	} {
		m[name] = true
	}
}

func claimSurvivors(g *irGen, name string) []string {
	var kept []string
	for _, c := range []struct {
		what string
		on   bool
	}{
		{"listVars", g.listVars[name]}, {"mixedLists", g.mixedLists[name]},
		{"runtimeDicts", g.runtimeDicts[name]}, {"mixedDicts", g.mixedDicts[name]},
		{"runtimeSets", g.runtimeSets[name]}, {"mixedSets", g.mixedSets[name]},
		{"listElemStr", g.listElemStr[name]}, {"listElemInt", g.listElemInt[name]},
		{"setElemStr", g.setElemStr[name]}, {"setElemInt", g.setElemInt[name]},
		{"dictKeyStr", g.dictKeyStr[name]}, {"dictKeyInt", g.dictKeyInt[name]},
		{"dictValStr", g.dictValStr[name]}, {"dictValInt", g.dictValInt[name]},
	} {
		if c.on {
			kept = append(kept, c.what)
		}
	}
	return kept
}

// TestTheRebindingDoorClearsWhatTheBindingContradicts asks the fourteen tables themselves, which a program
// cannot: a read that happens to work would hide a claim that stayed set. Every name starts with all
// fourteen claims on; the survivors are the answer.
func TestTheRebindingDoorClearsWhatTheBindingContradicts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind string
		want []string
	}{
		{
			"a list takes the name over", containerKindList,
			[]string{"listVars", "listElemStr", "listElemInt"},
		},
		{
			"a dict takes the name over", containerKindDict,
			[]string{"runtimeDicts", "dictKeyStr", "dictKeyInt", "dictValStr", "dictValInt"},
		},
		{
			"a set takes the name over", containerKindSet,
			[]string{"runtimeSets", "setElemStr", "setElemInt"},
		},
		{
			"a scalar takes the name over", containerKindScalar,
			nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &irGen{
				listVars: map[string]bool{}, mixedLists: map[string]bool{},
				runtimeDicts: map[string]bool{}, mixedDicts: map[string]bool{},
				runtimeSets: map[string]bool{}, mixedSets: map[string]bool{},
				listElemStr: map[string]bool{}, listElemInt: map[string]bool{},
				setElemStr: map[string]bool{}, setElemInt: map[string]bool{},
				dictKeyStr: map[string]bool{}, dictKeyInt: map[string]bool{},
				dictValStr: map[string]bool{}, dictValInt: map[string]bool{},
				containerLits: map[string]Expr{},
			}
			seedClaims(g, "d")
			g.settleContainerKind("d", tc.kind)
			got := claimSurvivors(g, "d")
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("after binding a %s the name still claims [%s], want [%s]",
					tc.name, strings.Join(got, ","), strings.Join(tc.want, ","))
			}
		})
	}
}

// TestTheRebindingDoorLeavesWhatTheBindingAgreesWith is the restraint half: a door that clears everything
// is a broom, and ADR 0241's fold record — the thing that licenses reading a literal's slot statically —
// is exactly a claim the binding agrees with.
func TestTheRebindingDoorLeavesWhatTheBindingAgreesWith(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind string
		keep []string
		drop []string
	}{
		{
			"a name that stays a list keeps what its elements are", containerKindList,
			[]string{"listVars", "listElemStr"},
			[]string{"runtimeDicts", "dictKeyStr", "dictValInt", "runtimeSets", "setElemInt"},
		},
		{
			"a name that stays a dict keeps its key and value kinds", containerKindDict,
			[]string{"runtimeDicts", "dictKeyStr", "dictValInt"},
			[]string{"listVars", "listElemStr", "runtimeSets", "setElemInt"},
		},
		{
			"a name that stays a set keeps its member kind", containerKindSet,
			[]string{"runtimeSets", "setElemStr"},
			[]string{"listVars", "runtimeDicts", "dictKeyStr"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &irGen{
				listVars: map[string]bool{}, mixedLists: map[string]bool{},
				runtimeDicts: map[string]bool{}, mixedDicts: map[string]bool{},
				runtimeSets: map[string]bool{}, mixedSets: map[string]bool{},
				listElemStr: map[string]bool{}, listElemInt: map[string]bool{},
				setElemStr: map[string]bool{}, setElemInt: map[string]bool{},
				dictKeyStr: map[string]bool{}, dictKeyInt: map[string]bool{},
				dictValStr: map[string]bool{}, dictValInt: map[string]bool{},
				containerLits: map[string]Expr{},
			}
			// Seed the claims the binding agrees with, then settle on the same kind.
			for _, k := range tc.keep {
				(*claimMapFor(g, k))["d"] = true
			}
			g.settleContainerKind("d", tc.kind)
			for _, k := range tc.keep {
				if !(*claimMapFor(g, k))["d"] {
					t.Errorf("the %s binding retired %s, which it agrees with", tc.kind, k)
				}
			}
			for _, k := range tc.drop {
				if (*claimMapFor(g, k))["d"] {
					t.Errorf("the %s binding left %s standing", tc.kind, k)
				}
			}
		})
	}
}

// claimMapFor names the table a claim lives in, so the restraint rows above ask the tables by name.
func claimMapFor(g *irGen, claim string) *map[string]bool {
	switch claim {
	case "listVars":
		return &g.listVars
	case "mixedLists":
		return &g.mixedLists
	case "runtimeDicts":
		return &g.runtimeDicts
	case "mixedDicts":
		return &g.mixedDicts
	case "runtimeSets":
		return &g.runtimeSets
	case "mixedSets":
		return &g.mixedSets
	case "listElemStr":
		return &g.listElemStr
	case "listElemInt":
		return &g.listElemInt
	case "setElemStr":
		return &g.setElemStr
	case "setElemInt":
		return &g.setElemInt
	case "dictKeyStr":
		return &g.dictKeyStr
	case "dictKeyInt":
		return &g.dictKeyInt
	case "dictValStr":
		return &g.dictValStr
	case "dictValInt":
		return &g.dictValInt
	}
	panic("not a container claim: " + claim)
}

// TestTheRebindingDoorClearsNothingForAnotherName: the door is name-scoped. `mixedLists` in particular is
// the one notebook shared across scopes, so this is asked rather than assumed.
func TestTheRebindingDoorClearsNothingForAnotherName(t *testing.T) {
	t.Parallel()
	g := &irGen{
		listVars: map[string]bool{}, mixedLists: map[string]bool{},
		runtimeDicts: map[string]bool{}, mixedDicts: map[string]bool{},
		runtimeSets: map[string]bool{}, mixedSets: map[string]bool{},
		listElemStr: map[string]bool{}, listElemInt: map[string]bool{},
		setElemStr: map[string]bool{}, setElemInt: map[string]bool{},
		dictKeyStr: map[string]bool{}, dictKeyInt: map[string]bool{},
		dictValStr: map[string]bool{}, dictValInt: map[string]bool{},
		containerLits: map[string]Expr{},
	}
	seedClaims(g, "other")
	before := claimSurvivors(g, "other")
	g.settleContainerKind("d", containerKindList)
	g.settleContainerKindFromLiteral("d", containerKindDict)
	g.settleContainerKind("d", containerKindScalar)
	if got := claimSurvivors(g, "other"); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Fatalf("settling another name moved this one: [%s] became [%s]", strings.Join(before, ","), strings.Join(got, ","))
	}
}

// TestTheLiteralDoorClearsItsOwnKindsNotebookToo is the one question the literal variant answers: a
// literal writes every slot again, so the element notebook the freed object left describes bytes this
// statement replaces. Without it, `xs = ["a"]` then `xs = [1, 2]` is refused as a mixed list.
func TestTheLiteralDoorClearsItsOwnKindsNotebookToo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind string
		keep []string
		drop []string
	}{
		{"a list literal", containerKindList,
			[]string{"listVars"},
			[]string{"listElemStr", "listElemInt"}},
		{"a dict literal", containerKindDict,
			[]string{"runtimeDicts"},
			[]string{"dictKeyStr", "dictKeyInt", "dictValStr", "dictValInt"}},
		{"a set literal", containerKindSet,
			[]string{"runtimeSets"},
			[]string{"setElemStr", "setElemInt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &irGen{
				listVars: map[string]bool{}, mixedLists: map[string]bool{},
				runtimeDicts: map[string]bool{}, mixedDicts: map[string]bool{},
				runtimeSets: map[string]bool{}, mixedSets: map[string]bool{},
				listElemStr: map[string]bool{}, listElemInt: map[string]bool{},
				setElemStr: map[string]bool{}, setElemInt: map[string]bool{},
				dictKeyStr: map[string]bool{}, dictKeyInt: map[string]bool{},
				dictValStr: map[string]bool{}, dictValInt: map[string]bool{},
				containerLits: map[string]Expr{},
			}
			seedClaims(g, "d")
			g.settleContainerKindFromLiteral("d", tc.kind)
			for _, k := range tc.keep {
				if !(*claimMapFor(g, k))["d"] {
					t.Errorf("the %s lost %s, which the binding itself writes", tc.name, k)
				}
			}
			for _, k := range tc.drop {
				if (*claimMapFor(g, k))["d"] {
					t.Errorf("the %s left %s standing for an object it rebuilt slot by slot", tc.name, k)
				}
			}
		})
	}
}

// TestTheRebindingDoorDropsAFoldRecordOfAnotherKind: ADR 0241's fold record is a claim about one object.
// A name that goes from a list to a dict cannot keep a list literal answering reads of it, and a fold
// record of the kind the binding IS stays (the restraint rows above).
func TestTheRebindingDoorDropsAFoldRecordOfAnotherKind(t *testing.T) {
	t.Parallel()
	lit := &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}}
	g := &irGen{
		listVars: map[string]bool{}, mixedLists: map[string]bool{},
		runtimeDicts: map[string]bool{}, mixedDicts: map[string]bool{},
		runtimeSets: map[string]bool{}, mixedSets: map[string]bool{},
		listElemStr: map[string]bool{}, listElemInt: map[string]bool{},
		setElemStr: map[string]bool{}, setElemInt: map[string]bool{},
		dictKeyStr: map[string]bool{}, dictKeyInt: map[string]bool{},
		dictValStr: map[string]bool{}, dictValInt: map[string]bool{},
		containerLits: map[string]Expr{"xs": lit},
	}
	g.settleContainerKind("xs", containerKindDict)
	if _, kept := g.containerLits["xs"]; kept {
		t.Fatal("a dict binding left a list literal licensed to answer reads of the name")
	}
	// The restraint half: a fold record naming a literal of the kind the binding IS describes the object
	// this statement wrote (ADR 0241), so the door leaves it standing.
	dictLit := &DictLit{Keys: []Expr{&IntLit{Value: 1}}, Vals: []Expr{&IntLit{Value: 10}}}
	g2 := &irGen{
		listVars: map[string]bool{}, mixedLists: map[string]bool{},
		runtimeDicts: map[string]bool{}, mixedDicts: map[string]bool{},
		runtimeSets: map[string]bool{}, mixedSets: map[string]bool{},
		listElemStr: map[string]bool{}, listElemInt: map[string]bool{},
		setElemStr: map[string]bool{}, setElemInt: map[string]bool{},
		dictKeyStr: map[string]bool{}, dictKeyInt: map[string]bool{},
		dictValStr: map[string]bool{}, dictValInt: map[string]bool{},
		containerLits: map[string]Expr{"xs": dictLit},
	}
	g2.settleContainerKind("xs", containerKindDict)
	if _, kept := g2.containerLits["xs"]; !kept {
		t.Errorf("the dict binding dropped the fold record that describes its own object (ADR 0241)")
	}
}

// readerHelpers are the calls that consult a container's kind notebook: the printers, the slot read, the
// length, the membership hash. A name rebound to another kind that is still read by these is the defect,
// whatever the program printed.
var readerHelpers = []string{
	"rt_print_list", "rt_print_list_mixed", "rt_print_list_str",
	"rt_print_value", "rt_print_mixed_value", "rt_print_container_value",
	"rt_get_elem", "rt_list_len", "rt_contains", "rt_contains_tagged",
	"rt_dict_get", "rt_dict_get_tagged", "rt_dict_find", "rt_dict_has", "rt_dict_has_tagged",
	"rt_dict_len", "rt_dict_print", "rt_dict_print_mixed", "rt_dict_print_s",
	"rt_set_find", "rt_set_len", "rt_set_print", "rt_set_print_mixed", "rt_set_print_str",
	"rt_set_contains_tagged",
}

// programReaderCalls counts the reader calls the PROGRAM's own code makes — the bodies of `@main` and the
// generated `@gy_*` functions only, so a runtime helper definition or a helper calling a helper is never
// mistaken for the program asking the question (the lesson ADR 0340's structural row learned).
func programReaderCalls(t *testing.T, src string) map[string]int {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference prints (%v): %s", err, src)
	}
	var body []string
	inside := false
	for _, line := range strings.Split(res.IR, "\n") {
		switch {
		case strings.HasPrefix(line, "define") && (strings.Contains(line, "@main(") || strings.Contains(line, "@gy_")):
			inside = true
		case strings.HasPrefix(line, "}"):
			inside = false
		case inside:
			body = append(body, line)
		}
	}
	program := strings.Join(body, "\n")
	counts := map[string]int{}
	for _, h := range readerHelpers {
		re := regexp.MustCompile(`call [^@]*@` + h + `\(`)
		if n := len(re.FindAllString(program, -1)); n > 0 {
			counts[h] = n
		}
	}
	if len(counts) == 0 {
		t.Fatalf("the program asks no container question at all; the row is not being witnessed:\n%s", src)
	}
	return counts
}

// TestAReboundContainerIsReadByTheSameRoadsAsAContainerBoundOnce is the structural half: each pair below is
// one program written two ways — the name bound once, and the name bound to another kind first — reading
// the container through the same doors. The reader calls those two programs emit must be identical.
//
// On the commit before this row the dict-then-list pair emitted `@rt_dict_get`, `@rt_dict_has`,
// `@rt_dict_has_tagged` and `@rt_dict_print` where the list-only program emitted `@rt_get_elem`,
// `@rt_list_len` and `@rt_print_list`; the set-then-list pair emitted `@rt_set_print` and
// `@rt_set_contains_tagged`; and the text-list-then-number-list pair was REFUSED outright for want of the
// notebook the freed object left behind.
func TestAReboundContainerIsReadByTheSameRoadsAsAContainerBoundOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		rebound  string
		single   string
		mustHave []string // the readers the reference's kind has to be read by
	}{
		{
			"a dict rebound to a list",
			"d = {1: 10}\nd = [3, 4]\nprint(d)\nprint(d[0])\nprint(len(d))\nprint(3 in d)\nfor v in d:\n    print(v)\n",
			"d = [3, 4]\nprint(d)\nprint(d[0])\nprint(len(d))\nprint(3 in d)\nfor v in d:\n    print(v)\n",
			[]string{"rt_get_elem", "rt_list_len", "rt_print_list"},
		},
		{
			"a mixed dict rebound to a list",
			"d = {1: \"x\"}\nd = [2, 3]\nprint(d)\nprint(d[1])\nprint(len(d))\n",
			"d = [2, 3]\nprint(d)\nprint(d[1])\nprint(len(d))\n",
			[]string{"rt_get_elem", "rt_list_len"},
		},
		{
			"a set rebound to a list",
			"s = {1, 2}\ns = [5, 6]\nprint(s)\nprint(s[0])\nprint(len(s))\nprint(5 in s)\n",
			"s = [5, 6]\nprint(s)\nprint(s[0])\nprint(len(s))\nprint(5 in s)\n",
			[]string{"rt_get_elem", "rt_list_len", "rt_print_list"},
		},
		{
			"a list rebound to a dict",
			"x = [1, 2]\nx = {1: 10}\nprint(x)\nprint(x[1])\nprint(len(x))\nprint(1 in x)\nfor k in x:\n    print(k)\n",
			"x = {1: 10}\nprint(x)\nprint(x[1])\nprint(len(x))\nprint(1 in x)\nfor k in x:\n    print(k)\n",
			[]string{"rt_dict_get", "rt_dict_print"},
		},
		{
			"a text list rebound to a number list",
			"xs = [\"a\"]\nxs = [1, 2]\nprint(xs)\nprint(xs[0])\nprint(len(xs))\n",
			"xs = [1, 2]\nprint(xs)\nprint(xs[0])\nprint(len(xs))\n",
			[]string{"rt_get_elem", "rt_list_len"},
		},
		{
			"a number list rebound to a text list",
			"xs = [1, 2]\nxs = [\"a\", \"b\"]\nprint(xs)\nprint(xs[1])\n",
			"xs = [\"a\", \"b\"]\nprint(xs)\nprint(xs[1])\n",
			[]string{"rt_get_elem"},
		},
		{
			"a dict rebound to a scalar and back to a list",
			"v = {1: 2}\nv = 5\nv = [7, 8]\nprint(v)\nprint(v[0])\nprint(len(v))\n",
			"v = [7, 8]\nprint(v)\nprint(v[0])\nprint(len(v))\n",
			[]string{"rt_get_elem", "rt_list_len", "rt_print_list"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rebound := programReaderCalls(t, tc.rebound)
			single := programReaderCalls(t, tc.single)
			if len(rebound) != len(single) {
				t.Fatalf("the rebound name is read by %d reader helpers, the same program bound once reads %d\nrebound: %v\nsingle:  %v",
					len(rebound), len(single), rebound, single)
			}
			for h, n := range single {
				if rebound[h] != n {
					t.Fatalf("@%s is called %d times by the rebound program and %d by the single-binding one\nrebound: %v\nsingle:  %v",
						h, rebound[h], n, rebound, single)
				}
			}
			for _, h := range tc.mustHave {
				if rebound[h] == 0 {
					t.Fatalf("the reference's kind is not being read by @%s at all: %v", h, rebound)
				}
			}
		})
	}
}

// TestTheAliasedContainerIsNotThisRowsDebt is the shape the kind claim never owned and never broke: `b = a`
// copies the handle, so both names hold one object, and the rebinding `b = [7, 8]` used to free it while `a`
// still named it — the read of `a` then died or printed a plausible number depending on whether the allocator
// recycled the freed slot, which is the worst property a heap can have. **ADR 0414** closed it (roadmap
// **Gap R.257**): the release door asks the collector's own root table before it pushes, so both names answer
// and the program prints the reference's bytes. The parity assertion here is the same program on the record leg
// and the compiled leg; the CLI leg, the loop form and the negative controls live in
// `integration/rebind_kind_test.go::TestTheAliasReboundKeepsWhatTheOtherNameHolds`.
func TestTheAliasedContainerIsNotThisRowsDebt(t *testing.T) {
	t.Parallel()
	const src = "y = {1: 2}\ny = 5\ny = [7, 8]\nprint(y[0])\na = {1: 2}\nb = a\nb = [7, 8]\nprint(b[0])\nprint(a[1])\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the alias program failed to compile: %v", err)
	}
	assertNoForbiddenIR(t, src, res.IR)
	// The row's own half of that program does answer, and it is the reason the free was the bug: the name
	// being rebound reads its own new object through the list roads.
	if out, trap := runForParity(t, "a = {1: 2}\nb = a\nb = [7, 8]\nprint(b[0])\n"); trap != "" || out != "7" {
		t.Fatalf("the rebound alias printed %q (trap %q), want \"7\"", out, trap)
	}
	// And the half that used to die answers too — Gap R.257's promoted program, on both legs.
	if out, trap := runForParity(t, src); trap != "" || out != "7\n7\n2" {
		t.Fatalf("the alias whose twin was rebound printed %q (trap %q), want \"7\\n7\\n2\\n\" — the release door "+
			"released an object the other name still holds\n%s", out, trap, src)
	}
	if !HasGoldenAnswer(src) {
		t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", src, src)
	}
	if recorded, err := runGoldenStdout(t, src); err != nil || recorded != "7\n7\n2\n" {
		t.Fatalf("the record leg reads %q (err %v), want \"7\\n7\\n2\\n\"\n%s", recorded, err, src)
	}
}

// TestWhatTheRebindingRowDoesNotOwn pins the shapes this row leaves to other rows. Each is refused in words
// that name the half that is missing — never an answer at exit 0, never exit 2.
func TestWhatTheRebindingRowDoesNotOwn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, phrase string
	}{
		{
			"min/max over a container variable is L11.1's, not the kind claim's",
			"d = [3, 1, 2]\nprint(max(d))\n", "max requires an inline list/set/dict literal",
		},
		{
			"a set asked for a position is the membership road's",
			"s = {1, 2}\nprint(s[0])\n", "index of a non-literal variable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("%q answered %q at exit 0; the roadmap row the name cites owes this shape", tc.src, got)
			}
			if !strings.Contains(got, tc.phrase) {
				t.Fatalf("%q refused without naming %q: %s", tc.src, tc.phrase, got)
			}
			if strings.Contains(got, "codegen: ") && strings.Contains(got, "llc") {
				t.Fatalf("%q failed as a toolchain rejection: %s", tc.src, got)
			}
		})
	}
}
