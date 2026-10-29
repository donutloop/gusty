package lang

// A container that TRAVELS through an unpacking is still the object — the claims that make it printable,
// measurable, iterable and comparable have to arrive at the name that receives it.
//
// roadmap Gap R.527 · ADR 0455 (measured while checking ADR 0454's own swap rows against the reference)
//
// `x, y = [1], 2` binds the object (ADR 0454). The element beside it — the one that is a NAME holding a
// container — did not:
//
//	xs = [1, 2]                    a = [1]                  d = {"k": 1}
//	a, b = xs, 5                   b = 2                    a, b = d, 2
//	print(a)                       a, b = b, a              print(a)
//	→ 0                            print(a)                 → 1        reference: {'k': 1}
//	                             print(b)
//	→ [] and 0                     reference: 2 and [1]
//
// every one of them at the exit code that means the program ran. A container is not its handle: what makes
// `print(xs)` print a list is a dozen compile-time claims keyed by the NAME — `listVars`, the element-kind
// notebooks, the "my slots describe themselves" bit, the twelve **Gap Q.1** settled into one door. The plain
// binding road has always carried them across a copy (`ys = xs`, **ADR 0412** — "the handle travels and the
// notebook claims do not" is that row's own finding). The unpacking's bind loop wrote the payload word and
// asked none of those doors, so the receiving name was a number wearing another object's bits and the name it
// displaced kept claiming a container it no longer held.
//
// The fix is the same door, asked per element (**ADR 0232**): `settleUnpackedClaims` settles the kind the
// element arrived in, copies the record the source name owned, keeps the shared-handle link when the source
// is not ALSO a target of this statement, and roots the handle (**ADR 0163**). A swap makes the ORDER
// load-bearing — `a, b = b, a` reads `a` while settling `b` and settles `a` with what `b` was — so the record
// is read into a snapshot before any of this statement's bindings land, and written afterwards.
//
// `TestTheUnpackedContainerAsksTheSameDoorsAsThePlainCopy` is the claim that there is one feature here: the
// multiset of print doors a program asks must be the same whether the binding wore commas or not, and the
// multiset a swap leaves behind must be the multiset the plain bindings answer with. Two roads that "should"
// agree are the drift this suite exists to catch.
//
// Every `want` below is the reference's own bytes, read by running the same source through python3 outside
// this file before it was written here; the record leg then re-asserts each one from
// `testdata/interpreter-golden.json` (ADR 0302: a row a leg cannot self-certify).

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func unpackedClaimsRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"the row that printed the handle",
			"xs = [1, 2]\na, b = xs, 5\nprint(a)\nprint(b)\n",
			"[1, 2]\n5\n",
		},
		{
			"a set and a dict travelling together",
			"s = {1, 2}\nd = {\"k\": 1}\na, b = s, d\nprint(a)\nprint(b)\nprint(len(a))\nprint(\"k\" in b)\n",
			"{1, 2}\n{'k': 1}\n2\nTrue\n",
		},
		{
			"measured, indexed and asked a membership question",
			"xs = [1, 2, 3]\na, n = xs, 0\nprint(len(a))\nprint(a[1])\nprint(2 in a)\n",
			"3\n2\nTrue\n",
		},
		{
			"looped over",
			"xs = [7, 8]\na, n = xs, 0\nfor v in a:\n    print(v)\n",
			"7\n8\n",
		},
		{
			// The alias is not a frozen copy: a mutation through EITHER name is one event on ONE object
			// (ADR 0232's promotion, ADR 0412's link), and the unpacking that shares a handle keeps the link
			// precisely because the source is not also a target here.
			"mutated through the name the unpacking opened",
			"xs = [1]\na, n = xs, 0\na.append(9)\nprint(xs)\nprint(a)\n",
			"[1, 9]\n[1, 9]\n",
		},
		{
			"and through the name that built it",
			"xs = [1]\na, n = xs, 0\nxs.append(\"z\")\nprint(a)\nprint(xs)\n",
			"[1, 'z']\n[1, 'z']\n",
		},
		{
			// The mixed swap is the row this file was filed for: the handle and the number change places,
			// and the claims have to change places with them.
			"the mixed swap",
			"a = [1]\nb = 2\na, b = b, a\nprint(a)\nprint(b)\n",
			"2\n[1]\n",
		},
		{
			"a list swapped against a dict",
			"d = {\"a\": 1}\nxs = [1]\nd, xs = xs, d\nprint(d)\nprint(xs)\n",
			"[1]\n{'a': 1}\n",
		},
		{
			"a three-way rotate with two containers in it",
			"p, q, r = [1], [2], 3\np, q, r = r, p, q\nprint(p)\nprint(q)\nprint(r)\n",
			"3\n[1]\n[2]\n",
		},
		{
			// The float face of the same missing retirement: a double is not a container either, and the
			// box the float road stores carries its own kind.
			"a container replaced by a double",
			"a = [1]\na, n = 1.5, 2\nprint(a)\nprint(n)\n",
			"1.5\n2\n",
		},
		{
			"a container replaced by a slot the run time filled",
			"xs = [5]\na = [1]\na, n = xs[0], 2\nprint(a)\nprint(n)\n",
			"5\n2\n",
		},
		{
			"twice, inside a frame",
			"def f():\n    xs = [1, 2]\n    ys = [3]\n    a, b = xs, ys\n    print(a)\n    print(b)\n    a, b = b, a\n    print(a)\n    print(b)\n\nf()\n",
			"[1, 2]\n[3]\n[3]\n[1, 2]\n",
		},
		{
			"a dict entry written through the received name, seen from the name that built it",
			"d = {\"a\": 1}\nx, y = d, 0\nx[\"b\"] = 2\nprint(d)\nprint(len(d))\n",
			"{'a': 1, 'b': 2}\n2\n",
		},
		{
			"compared, and asked about itself",
			"xs = [1, 2]\nys = [1, 2]\na, b = xs, ys\nprint(a == b)\nprint(a is a)\n",
			"True\nTrue\n",
		},
		{
			"a container inside a container, read all the way down",
			"m = [[1]]\np, q = m, 0\nprint(p[0])\nprint(p)\nprint(str(p))\n",
			"[1]\n[[1]]\n[[1]]\n",
		},
		{
			"swapped, then rebound to a plain number",
			"a = [1]\nb = 2\na, b = b, a\na = 7\nprint(a)\nprint(b)\n",
			"7\n[1]\n",
		},
	}
}

func TestAContainerTravellingThroughAnUnpackingIsStillTheObject(t *testing.T) {
	t.Parallel()
	for _, r := range unpackedClaimsRows() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", r.want, err, r.src)
			}
			if got != r.want {
				t.Fatalf("printed %q, want the reference's %q\n%s", got, r.want, r.src)
			}
			// The record leg: the same bytes, from the answer on record.
			onRecordIs(t, r.src, r.want)
		})
	}
}

// irCallRE finds every runtime call in a body; the print-door filter below is what makes the comparison a
// claim about how a value is RENDERED rather than about how many registers the program happens to need.
var irCallRE = regexp.MustCompile(`call [^@]*@(rt_[A-Za-z0-9_]+)\(`)

// printDoors lists the rendering doors @main asks, sorted, with repeats kept. Two programs that bind the
// same values must ask the same doors: a name that prints a handle because the notebook never heard it holds
// a container differs from one that prints a list exactly here.
func printDoors(t *testing.T, src string) []string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	var out []string
	for _, m := range irCallRE.FindAllStringSubmatch(inMainBody(t, res.IR), -1) {
		if strings.Contains(m[1], "print") {
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

func TestTheUnpackedContainerAsksTheSameDoorsAsThePlainCopy(t *testing.T) {
	t.Parallel()
	t.Run("copying a container into a new name", func(t *testing.T) {
		t.Parallel()
		plain := printDoors(t, "xs = [1, 2]\nb = xs\nprint(b)\n")
		unpacked := printDoors(t, "xs = [1, 2]\na, b = xs, 0\nprint(a)\n")
		if len(plain) == 0 {
			t.Fatal("the plain copy asks no print door at all — the pair of rows proves nothing")
		}
		if strings.Join(plain, ",") != strings.Join(unpacked, ",") {
			t.Errorf("the two bindings render the same object through different doors:\n plain   %v\n unpacked %v",
				plain, unpacked)
		}
	})
	t.Run("a swap leaves the notebook the plain bindings leave", func(t *testing.T) {
		t.Parallel()
		// `a = [1]` / `b = 2` / `a, b = b, a` must end with the same records `a = 2` / `b = [1]` starts
		// with — otherwise `print(a)` renders the number through the list door, which is the `[]` this row
		// was filed for, and no amount of reading stdout separates "the swap works" from "the print lied".
		swapped := printDoors(t, "a = [1]\nb = 2\na, b = b, a\nprint(a)\nprint(b)\n")
		plain := printDoors(t, "a = 2\nb = [1]\nprint(a)\nprint(b)\n")
		if len(plain) == 0 {
			t.Fatal("the plain program asks no print door at all — the pair of rows proves nothing")
		}
		if strings.Join(plain, ",") != strings.Join(swapped, ",") {
			t.Errorf("after the swap the two programs render through different doors:\n plain  %v\n swapped %v",
				plain, swapped)
		}
	})
	t.Run("the handle the unpacking stores is rooted", func(t *testing.T) {
		t.Parallel()
		// A payload that names a heap object must be a root for as long as the name holds it (ADR 0163,
		// Gap J.2 / ADR 0234): an unrooted handle is one collection away from the segfault ADR 0181 exists
		// to prevent, and a wrong answer at exit 0 is what this row's first shape was anyway.
		res, err := Compile("xs = [1, 2]\na, b = xs, 0\nprint(a)\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		main := inMainBody(t, res.IR)
		if !strings.Contains(main, "call void @rt_root_put(i32* %_a)") {
			t.Errorf("@main never registers %%_a as a root — the container the unpacking bound is unreachable to the collector:\n%s", main)
		}
	})
}

// TestWhatTheUnpackingStillBindsAsAWord pins the residue this row does NOT pay, with today's bytes and the row
// that owns each. A pair-bound element carries its own tag and is answered by the tagged doors (**ADR 0367**),
// and the shape below is pinned so that a change in either road moves a row instead of silently changing an
// answer.
func TestWhatTheUnpackingStillBindsAsAWord(t *testing.T) {
	t.Parallel()
	rows := []struct{ name, src, want, owner string }{
		{
			// The element is a slot the literal never described, so it arrives as a pair; the name it lands
			// on happens to answer today, and stays pinned because nothing here owns the pair's container
			// claims — that residue is L11.1's tagged word at a one-word position (Gap R.146).
			"a pair-bound element over a name that used to hold a container",
			"xs = [[7]]\na = [1]\na, n = xs[0], 2\nprint(a)\n",
			"[7]\n", "L11.1 / Gap R.146",
		},
		{
			"a container read out of a slot, not out of a name",
			"xs = [[1, 2]]\na, n = xs[0], 0\nprint(a)\n",
			"[1, 2]\n", "L11.1 / Gap R.146",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", r.want, err, r.src)
			}
			if got != r.want {
				t.Fatalf("printed %q, want today's %q (owner %s) — the pair road moved, so move the row\n%s",
					got, r.want, r.owner, r.src)
			}
			// The pair road answers these today, and the answer is the reference's — so the record leg is
			// asked too, and a residue row that stops being true stops being green rather than agreeing
			// with itself (**ADR 0302**).
			onRecordIs(t, r.src, r.want)
		})
	}
}

func TestTheUnpackingClaimsRowsAreTheRowsTheRowClaims(t *testing.T) {
	t.Parallel()
	rows := unpackedClaimsRows()
	if len(rows) < 14 {
		t.Fatalf("the table holds %d rows; this row's claim is a family, not a single shape", len(rows))
	}
	for _, r := range rows {
		if !strings.Contains(r.src, ", ") {
			t.Fatalf("row %q is not an unpacking — it belongs in the container-binding rows", r.name)
		}
		if !strings.Contains(r.src, "[") && !strings.Contains(r.src, "{") {
			t.Fatalf("row %q binds no container — the table is about a container travelling", r.name)
		}
	}
}

// TestTheUnpackingClaimsRecordSourcesTheTableAsks is the recorder, not a case: with GUSTY_RECORD_UNPACK_CLAIMS
// set it writes every source the table above asks about, in the shape `tools/recmerge` reads, and skips. Like
// the other artifact-writing modes it is scrubbed from every subprocess the suite starts
// (`integration/parallelism_guard_test.go`, Gap R.410 / ADR 0409), and the guard in that file reads
// docs/operations.md's recording-knob table — so documenting this knob without scrubbing it fails the build.
func TestTheUnpackingClaimsRecordSourcesTheTableAsks(t *testing.T) {
	out := os.Getenv("GUSTY_RECORD_UNPACK_CLAIMS")
	if out == "" {
		t.Skip("the recorder: set GUSTY_RECORD_UNPACK_CLAIMS=<file> to dump these sources for tools/recmerge")
	}
	var srcs []string
	for _, r := range unpackedClaimsRows() {
		srcs = append(srcs, r.src)
	}
	body, merr := marshalGoldenMissing(srcs)
	if merr != nil {
		t.Fatalf("rendering the source list: %v", merr)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("writing %s: %v", out, err)
	}
	t.Logf("wrote %d sources to %s", len(srcs), out)
}
