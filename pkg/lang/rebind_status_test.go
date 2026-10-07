package lang

// pkg/lang/rebind_status_test.go — the records a name carries are retired by the binding that replaces
// the value, on the compiled backend (roadmap Gap R.145, ADR 0270).
//
// ADR 0172 states the rule — the variable's *latest* assignment decides how print, truthiness and
// equality lower, and any other assignment clears the status — and it has been applied one status at a
// time: `boolVars` has `forgetVarBool`, the tagged pair has `forgetTaggedBinding` (ADR 0267), the None
// singleton is deleted at the store. The interned-text pair (`strVals`, `internedVars`) had no door, so
// the record of a text binding survived the binding that replaced it and the compiled leg kept rendering
// the value the statement had overwritten: `x = "text"` then `x = [1, 2]` printed `text`, and
// `x = "text"` then `x = 5` printed `text` — both at exit 0, with the record and CPython printing
// `[1, 2]` and `5` beside them.
//
// Two things are pinned: the answers, and the *gate*. A test that only runs programs cannot tell a
// cleared record from a render path that happens to ignore the stale one, so `TestTheRebindingDoorClearsTheStatusItContradicts`
// asks the tables themselves — the same style ADR 0265/0267 use for their gates.

import (
	"testing"
)

// TestAReboundNameAnswersWhatTheLatestBindingGaveIt is the parity half: whatever the name is bound to
// *last* is what every question about it answers with, on both legs.
func TestAReboundNameAnswersWhatTheLatestBindingGaveIt(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a list after a text", "x = \"text\"\nx = [1, 2]\nprint(x)\n", "[1, 2]\n"},
		{"a dict after a text", "x = \"text\"\nx = {\"a\": 1}\nprint(x)\n", "{'a': 1}\n"},
		{"a set after a text", "x = \"text\"\nx = {1, 2}\nprint(x)\n", "{1, 2}\n"},
		{"a number after a text", "x = \"text\"\nx = 5\nprint(x)\n", "5\n"},
		{"a float after a text", "x = \"text\"\nx = 2.5\nprint(x)\n", "2.5\n"},
		{"None after a text", "x = \"text\"\nx = None\nprint(x)\n", "None\n"},
		{"a text after None", "x = None\nx = \"text\"\nprint(x)\n", "text\n"},
		{"a verdict after a number", "x = 5\nx = 1 == 1\nprint(x)\n", "True\n"},
		{"a number after a verdict", "x = 1 == 1\nx = 5\nprint(x)\n", "5\n"},
		{"a text after a number", "x = 5\nx = \"a\"\nprint(x)\n", "a\n"},
		{"a container after a container", "xs = [1]\nx = \"t\"\nx = xs\nprint(x[0])\n", "1\n"},
		{"a text keeps its own record", "x = \"a\"\nx = \"bb\"\nprint(len(x))\n", "2\n"},
		{"a rebound text is arithmetically a text", "x = 5\nx = \"abc\"\nprint(x.upper())\n", "ABC\n"},
		{"the arithmetic reads the new value", "x = \"text\"\nx = 5\nprint(x * 2)\n", "10\n"},
		{"str reads the new value", "x = \"abc\"\nx = 5\nprint(str(x))\n", "5\n"},
		{"a loop over the new container", "x = \"text\"\nx = [1, 2]\nfor v in x:\n    print(v)\n", "1\n2\n"},
		{"a text after a class instance", "class C:\n    pass\n\nx = C()\nx = \"hi\"\nprint(x)\n", "hi\n"},
		{"three bindings, the last one answers", "x = \"a\"\nx = [1]\nx = 7\nprint(x)\n", "7\n"},
		{"the same shape inside a function", "def f():\n    x = \"text\"\n    x = 5\n    print(x)\n\nf()\n", "5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.src)
			if out != tc.want {
				t.Errorf("interpreter: exit 0 stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheRebindingDoorClearsTheStatusItContradicts asks the gate itself, in the style
// `mixed_list_test.go` and `bool_element_test.go` use: build the generator, bind the record the earlier
// statement would have left, and ask the door what survives. Running the program can show a wrong answer;
// it cannot show that a render path is ignoring a record that is still standing — which is exactly how this
// defect hid, the print dispatch *asking* `internedVars` and getting true back for a name the last
// statement had bound to a list.
func TestTheRebindingDoorClearsTheStatusItContradicts(t *testing.T) {
	after := func(bind Expr) (text, interned, class bool) {
		g := &irGen{
			strVals:      map[string]string{"x": "text"},
			internedVars: map[string]bool{"x": true},
			varClasses:   map[string]string{"x": "C"},
		}
		g.retireVarStatuses("x", bind)
		_, text = g.strVals["x"]
		interned = g.internedVars["x"]
		class = g.varClasses["x"] != ""
		return
	}
	for _, tc := range []struct {
		name     string
		bind     Expr
		wantText bool
		wantCls  bool
	}{
		{"a list retires the text", &ListLit{}, false, false},
		{"a dict retires the text", &DictLit{}, false, false},
		{"a set retires the text", &SetLit{}, false, false},
		{"a number retires the text", &IntLit{Value: 5}, false, false},
		{"None retires the text", &NoneLit{}, false, false},
		{"a text keeps the text", &StrLit{Value: "bb"}, true, false},
		{"a construction keeps the class", &Call{Fn: &Name{Value: "C"}}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, interned, class := after(tc.bind)
			if got := text || interned; got != tc.wantText {
				t.Errorf("the interned-text record survived to %v, want %v", got, tc.wantText)
			}
			if class != tc.wantCls {
				t.Errorf("the class record survived to %v, want %v", class, tc.wantCls)
			}
		})
	}
}

// TestTheClassRecordIsRetiredByANonConstructionBinding is the same rule read off a program: the class a
// name was bound to decides print and method dispatch, and only a construction may leave it standing.
func TestTheClassRecordIsRetiredByANonConstructionBinding(t *testing.T) {
	const src = "class C:\n    pass\n\nx = C()\nx = 5\nprint(x)\n"
	if out := captureStdout(t, src); out != "5\n" {
		t.Errorf("interpreter: stdout %q, want \"5\\n\"", out)
	}
	res, cerr := Compile(src)
	if cerr != nil {
		t.Fatalf("compiled leg refused a program the oracle prints: %v", cerr)
	}
	if got := runIR(t, res.IR); got != "5\n" {
		t.Errorf("compiled: stdout %q, want \"5\\n\"", got)
	}
}

// TestARetirementLeavesTheRecordsTheNewBindingNeeds is the guard against the opposite mistake: a door
// that clears everything would take the text away from a name that is still a text, and `len(x)`,
// `x.upper()` and the container print dispatch all read those records.
func TestARetirementLeavesTheRecordsTheNewBindingNeeds(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the text's own length", "x = \"text\"\nx = \"abcde\"\nprint(len(x))\n", "5\n"},
		{"the text's own method", "x = \"text\"\nx = \"abc\"\nprint(x.upper())\n", "ABC\n"},
		{"the container's own elements", "x = \"text\"\nx = [1, 2, 3]\nprint(len(x))\n", "3\n"},
		{"the dict's own key", "x = \"text\"\nd = {\"k\": 1}\nx = d\nprint(x[\"k\"])\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheRetirementDoorKeepsTheRecordForATextBinding asks the door's own predicate, so a change that made
// it guess `str` for everything (or for nothing) fails here rather than showing up as a print regression:
// the whole reason the door may clear is that the new binding is not a text, and that question has to be
// the same one the print dispatch asks (ADR 0229's one predicate, read at the binding).
func TestTheRetirementDoorKeepsTheRecordForATextBinding(t *testing.T) {
	g := &irGen{
		strFuncs:     map[string]bool{},
		internedVars: map[string]bool{},
	}
	for _, tc := range []struct {
		name string
		bind Expr
		want bool
	}{
		{"a text literal", &StrLit{Value: "bb"}, true},
		{"an f-string", &FString{Parts: []FStringPart{{Lit: "a"}}}, true},
		{"a list literal", &ListLit{}, false},
		{"a number", &IntLit{Value: 1}, false},
		{"None", &NoneLit{}, false},
		{"a call that answers text", &Call{Fn: &Name{Value: "f"}}, true},
		{"a call that answers a number", &Call{Fn: &Name{Value: "len"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "a call that answers text" {
				g.strFuncs["f"] = true
			}
			if got := g.bindingIsText(tc.bind); got != tc.want {
				t.Errorf("bindingIsText = %v, want %v", got, tc.want)
			}
		})
	}
}
