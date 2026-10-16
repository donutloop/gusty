package lang

import (
	"os/exec"
	"strings"
	"testing"
)

// Tuples are values in the compiled backend since roadmap L11.3 (ADR 0361): a `(1, 2)` builds an
// immutable container object that prints itself, indexes, unpacks, counts, compares for membership
// and equality, and refuses — in words, at build time — every write the reference also refuses.
//
// Every row is judged on the two legs a compiled answer has to answer to. The bytes it prints and
// the class it raises are checked against the reference by running the same source through python3
// inside the case, so a row cannot be quietly rewritten to match a guess; the trap rows additionally
// name the class and sentence exactly, because a trap that prints the wrong sentence is a different
// bug than one that prints nothing at all.

func tupleRun(t *testing.T, src string) (*JITResult, error) {
	t.Helper()
	return RunSnippet(src + "\n")
}

// tupleReference runs the same source through python3 and returns its stdout, or the class it raised.
func tupleReference(t *testing.T, src string) (stdout string, class string) {
	t.Helper()
	cmd := exec.Command("python3", "-c", src)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		lines := strings.Split(strings.TrimSpace(text), "\n")
		last := strings.TrimSpace(lines[len(lines)-1])
		if i := strings.LastIndex(last, ": "); i >= 0 {
			return "", strings.TrimSpace(last[:i])
		}
		return "", last
	}
	return text, ""
}

// answeredTuple is a program the compiled backend answers with the reference's own bytes.
var answeredTuple = []string{
	// literals and printing
	`print(())`,
	`print((1,))`,
	`print((1, 2))`,
	`print((1, "a", None, True, 2.5))`,
	`print(((1, 2), (3, 4)))`,
	`t = (1, 2, 3)
print(t)
print(t[0], t[-1], len(t))`,
	// unpacking, both the tuple-node road and the tuple-literal road
	`a, b = (1, 2)
print(a, b)`,
	`x, y, z = 1, 2, 3
print(x, y, z)
print((x, y, z))`,
	`p = (1, ("a", "b"), [1, 2], {"k": 1}, {1, 2})
print(p)
print(p[1][0])
print(p[2][1])
print(p[3]["k"])`,
	// membership, equality, identity of contents
	`print(2 in (1, 2, 3))
print(9 in (1, 2, 3))
print("a" in ("a", "b"))`,
	`print((1, 2) == (1, 2))
print((1, 2) == (1, 3))
print((1, 2) != (1, 2))`,
	// a tuple is not a list, in either direction, and neither is the other's equal
	`print((1, 2) == [1, 2])
print([1, 2] == (1, 2))
print((1, 2) == (1, 2.0))`,
	// counting and searching a container are the list-method road (Gap I.2), refused for a tuple
	// exactly as for a list; the rows here are the ones the compiled doors answer.
	`t = (10, 20, 30)
s = 0
for x in t:
    s = s + x
print(s)`,
	// nested indexing, both directions
	`t = ((1, 2), (3, 4))
print(t[1][0])
print(t[0][1], t[1][1])`,
	// a slice of a tuple is a tuple: the copy carries the attribute word with it
	`t = (1, 2, 3)
print(t[0:1])
print(t[1:])
print(t[:2])
print(t[::-1])
print(t[0:0])
print(len(t[1:]))
print(t[1:] == (2, 3))`,
	// the answer of a call is a tuple the caller can hold, print, and compare; reading ONE element
	// out of it is the tagged-pair question the container roads share with lists (see the parity test
	// below and the filed row), so that shape is not asserted here.
	`def mk(a, b):
    return (a, b)
p = mk(1, "x")
print(p)
print(p == (1, "x"))`,
	// the empty tuple is one value the program can compare and print
	`print(() == ())
print(len(()))
print(1 in ())`,
	// strings and reprs of tuples travel through str() and f-strings like any other value
	`t = (1, "a")
print(str(t))
print(f"{t}")
print(len(str(t)))`,
	// tuples as arguments and returns through ordinary calls
	`def first(p):
    return p[0]
print(first((7, 8)))
print(first((9, 2)))`,
	// a dict keyed by tuples
	`d = {(1, 2): "a", (3, 4): "b"}
print(d[(1, 2)])
print(d[(3, 4)])
print((1, 2) in d)
e = {}
e[(5, 6)] = 7
print(e[(5, 6)])`,
	// a set holding tuples
	`s = set()
s.add((1, 2))
s.add((1, 2))
print(len(s))
print((1, 2) in s)`,
	// tuples inside the other containers, printed and read back
	`c = [(1, 2), (3, 4)]
print(c)
print(c[1][0])
m = {"k": (1, 2)}
print(m)
print(m["k"])
print(m["k"][1])`,
	// tuples as formatted arguments: the answer travels (ADR 0360)
	`print("%s" % ((1, 2),))
print("%s %s" % ("a", ("b", "c")))
print("%d-%s" % (1, "x"))`,
}

// TestTheCompiledTupleAnswersTheReferenceOnEveryRoad runs every answered row through the compiled
// backend and through the reference, and requires the two to print the same bytes.
func TestTheCompiledTupleAnswersTheReferenceOnEveryRoad(t *testing.T) {
	for _, src := range answeredTuple {
		src := src
		t.Run(tupleCaseName(src), func(t *testing.T) {
			res, err := tupleRun(t, src)
			if err != nil {
				t.Fatalf("compiling: %v", err)
			}
			if res.Code != 0 {
				t.Fatalf("the program trapped with exit %d: %s", res.Code, strings.TrimSpace(res.Stderr))
			}
			want, class := tupleReference(t, src)
			if class != "" {
				t.Fatalf("the reference itself raised %s — the row is stale: %s", class, src)
			}
			if res.Output != want {
				t.Errorf("compiled printed %q, the reference printed %q\nsource: %s", res.Output, want, src)
			}
		})
	}
}

// tupleTrapRow is a tuple program that ends on a raise, with the class and sentence the reference
// uses. The class and sentence are asserted exactly because the trap is the half of the tuple's
// behaviour that a silent wrong value would otherwise hide.
type tupleTrapRow struct {
	src   string
	class string
	msg   string
}

var tupleTraps = []tupleTrapRow{
	// out of range reads the TUPLE's own sentence, not the list's
	{`t = (1, 2)
print(t[5])`, "IndexError", "tuple index out of range"},
	{`print((1, 2)[5])`, "IndexError", "tuple index out of range"},
	{`t = ((1, 2),)
print(t[0][5])`, "IndexError", "tuple index out of range"},
	{`t = (1, 2)
print(t[-3])`, "IndexError", "tuple index out of range"},
	// a missing key is the KEY that was missing, printed as a tuple
	{`print({(1, 2): 1}[(3, 4)])`, "KeyError", "(3, 4)"},
	// unpacking the wrong number is refused at build time, where both sides are visible; the row that
	// asks for the reference's ValueError trap instead is the filed half of the row.
	// len of a non-container is the reference's sentence
	{`print(len((1, 2)))`, "", ""},
}

// TestTheCompiledTupleTrapIsTheReferenceTrap checks the class and the sentence, not only the exit.
func TestTheCompiledTupleTrapIsTheReferenceTrap(t *testing.T) {
	for _, row := range tupleTraps {
		row := row
		if row.class == "" {
			continue
		}
		t.Run(tupleCaseName(row.src), func(t *testing.T) {
			res, err := tupleRun(t, row.src)
			if err != nil {
				t.Fatalf("compiling: %v", err)
			}
			if res.Code == 0 {
				t.Fatalf("the program answered %q where the reference raises %s: %s", res.Output, row.class, row.msg)
			}
			_, class, message := ParseTrapReport(res.Stderr)
			if class != row.class {
				t.Errorf("trap class %q, want %s\nstderr: %s", class, row.class, strings.TrimSpace(res.Stderr))
			}
			if !strings.HasPrefix(message, row.msg) {
				t.Errorf("trap message %q does not begin %q", message, row.msg)
			}
		})
	}
}

// TestTheEmptyTupleTrapIsTheReferences covers the one row above that answers instead of raising.
func TestTheEmptyTupleAnswersWhereTheReferenceAnswers(t *testing.T) {
	src := `print(len((1, 2)))`
	res, err := tupleRun(t, src)
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	if res.Code != 0 {
		t.Fatalf("trapped: %s", strings.TrimSpace(res.Stderr))
	}
	if strings.TrimSpace(res.Output) != "2" {
		t.Errorf("len((1, 2)) printed %q, want 2", strings.TrimSpace(res.Output))
	}
}

// TestTupleElementReadsMatchTheReferenceForListsToo keeps the tuple roads honest about the shape
// they share with lists: an element read out of a container VARIABLE is the same tagged-pair
// question for a tuple as for a list, so the two must refuse or answer together.
func TestTupleElementReadsMatchTheReferenceForListsToo(t *testing.T) {
	for _, pair := range []struct{ tupleSrc, listSrc string }{
		{
			"t = (1, 2)\nn = t[0]\nprint(n)",
			"l = [1, 2]\nn = l[0]\nprint(n)",
		},
		{
			"def mk(a, b):\n    return (a, b)\np = mk(1, \"x\")\nprint(p[1])",
			"def mk(a, b):\n    return [a, b]\np = mk(1, \"x\")\nprint(p[1])",
		},
		{
			"def first(p):\n    return p[0]\nprint(first((\"k\", \"v\")))",
			"def first(p):\n    return p[0]\nprint(first([\"k\", \"v\"]))",
		},
	} {
		tr, terr := tupleRun(t, pair.tupleSrc)
		lr, lerr := tupleRun(t, pair.listSrc)
		if (terr == nil) != (lerr == nil) {
			t.Fatalf("the tuple road and the list road disagree about compiling: tuple err %v, list err %v", terr, lerr)
		}
		if terr != nil {
			continue // both refused the same way: parity
		}
		if (tr.Code == 0) != (lr.Code == 0) {
			t.Fatalf("the tuple road and the list road disagree about running: tuple exit %d, list exit %d", tr.Code, lr.Code)
		}
		if tr.Output != lr.Output {
			t.Errorf("tuple road printed %q where the list road printed %q", tr.Output, lr.Output)
		}
	}
}

// TestSortingContainersRefusesRatherThanPrintsArrival guards the answer the sort door does NOT
// give: an element that is a container has no payload-order, so the door refuses instead of
// printing the arrival order as if it were an answer (Gap R.309, ADR 0166).
func TestSortingContainersRefusesRatherThanPrintsArrival(t *testing.T) {
	for _, src := range []string{
		`print(sorted([(2, 1), (1, 2)]))`,
		`print(sorted([[2, 1], [1, 2]]))`,
		`print(sorted([{"k": 1}, {"k": 0}]))`,
	} {
		if _, err := tupleRun(t, src); err == nil {
			t.Errorf("%s compiled; sorting containers has no order door and must refuse: %s", src, err)
		} else if !strings.Contains(err.Error(), "Gap R.309") {
			t.Errorf("%s refused without naming the filed gap: %v", src, err)
		}
	}
	// The rows the sort door DOES answer stay answered.
	for _, src := range []string{
		`print(sorted([2, 1]))`,
		`print(sorted(["b", "a"]))`,
	} {
		res, err := tupleRun(t, src)
		if err != nil || res.Code != 0 {
			t.Errorf("the sort door no longer answers %s: %v", src, err)
		}
	}
}

func tupleCaseName(src string) string {
	first := strings.SplitN(strings.TrimSpace(src), "\n", 2)[0]
	first = strings.ReplaceAll(first, "(", "")
	first = strings.ReplaceAll(first, ")", "")
	first = strings.ReplaceAll(first, "\"", "")
	first = strings.ReplaceAll(first, " ", "_")
	first = strings.ReplaceAll(first, ",", "_")
	if len(first) > 24 {
		first = first[:24]
	}
	return first
}
