package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/the_alias_shares_the_record_test.go — the REFERENCE leg of **ADR 0412** (roadmap Gap R.415):
// what the LINKED PROGRAM prints when a list is read through the name the program bound to it.
//
// The row's claim is that `ys = xs` is one object with two names, and that the record which says what the
// object's slots hold belongs to the object: the binding copies the handle, and until this row it copied
// nothing else, so the alias answered from an empty notebook. The programs below are the same reads asked
// of both names — the popped slot through the printer, `str`, `==`, `is`, a condition, a literal element, a
// method's return, the slot read, the loop item, a chain of three names — plus the three mutations, each
// written against a different name and read back through another, which is where the copy alone or the link
// alone leaves a wrong answer (or, in the case that reached the printer with a text slot nobody had
// labelled, a segmentation fault at exit 139).
//
// Every expectation is re-derived from a live `python3` inside the case, so a table whose premise drifted
// fails rather than certifies the drift; the traps are checked as traps (exit **3**, never exit 1 for a
// refusal and never exit **2**, which ADR 0166 reserves for a module `llc` had to reject).
//
// The family grew the set's ORDER with **ADR 0415** (**Gap R.421**, closed): the void added to a set of
// numbers, the number that joins a set the void already stands in, the verdict, the member added twice, the
// promotion through the alias with the void keeping its place, and the LOOP over such a set — the linked
// binary and `python3` printing the same bytes for each, at exit 0. The order is decided by the door that
// inserts, not by the printer, and the loop program is the case that holds that claim: print and `for` read
// one array and may not disagree. What the row measured and did not buy is pinned as owed at the CLI with
// its bytes — the set built from a literal, which writes no tags and no order (**Gap R.423**), and the
// collision and the table that outgrows its first 8 buckets (**Gap R.424**).

// aliasRecordFamily is the answered family at the CLI: each program is run by the linked binary and by the
// reference, and both must answer the same way.
func aliasRecordFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"the row's own program", "xs = [None, 1]\nys = xs\nprint(ys.pop(0))\nprint(ys)\n", "None\n[1]\n"},
		{"the name it was built under", "xs = [None, 1]\nprint(xs.pop(0))\nprint(xs)\n", "None\n[1]\n"},
		{"the alias's popped slot asked whether it is the void", "xs = [None, 1]\nys = xs\nprint(ys.pop(0) is None)\n", "True\n"},
		{"and whether it is not", "xs = [None, 1]\nys = xs\nprint(ys.pop(0) is not None)\n", "False\n"},
		{"the alias's popped slot asked with ==", "xs = [None, 1]\nys = xs\nprint(ys.pop(0) == None)\n", "True\n"},
		{"a text's slot through the alias", "xs = [\"a\", \"b\"]\nys = xs\nprint(ys.pop(0))\nprint(str(ys.pop(0)))\n", "a\nb\n"},
		{"a double's slot", "xs = [1.5, 2.5]\nys = xs\nprint(ys.pop(0))\n", "1.5\n"},
		{"a nested list's slot", "xs = [[1, 2], 3]\nys = xs\nprint(ys.pop(0))\nprint(ys)\n", "[1, 2]\n[3]\n"},
		{"a dict's slot", "xs = [{\"a\": 1}, None]\nys = xs\nprint(ys.pop(0))\n", "{'a': 1}\n"},
		{"a text first and a number after", "xs = [\"a\", 1]\nys = xs\nprint(ys.pop(0))\nprint(ys)\n", "a\n[1]\n"},
		{"the slot read", "xs = [None, 1]\nys = xs\nprint(ys[0])\nprint(ys[1])\n", "None\n1\n"},
		{"the loop item", "xs = [None, \"a\"]\nys = xs\nfor v in ys:\n    print(v)\n", "None\na\n"},
		{"the loop item's identity", "xs = [None, \"a\"]\nys = xs\nfor v in ys:\n    print(v is None)\n", "True\nFalse\n"},
		{"a chain of three names", "xs = [None, \"a\"]\nys = xs\nzs = ys\nprint(zs[0])\nprint(zs.pop(0) is None)\nfor v in zs:\n    print(v)\n", "None\nTrue\na\n"},
		{"the popped slot as a condition", "xs = [None, 1]\nys = xs\nif ys.pop(0):\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"the popped slot in a literal", "xs = [None, 1]\nys = xs\nprint([ys.pop(0)])\n", "[None]\n"},
		{"the popped slot out of a method", "class T:\n    def m(self):\n        xs = [None, 1]\n        ys = xs\n        return ys.pop(0) is None\n\nprint(T().m())\n", "True\n"},
		{"a negative index", "xs = [1, None]\nys = xs\nprint(ys.pop(-1) is None)\n", "True\n"},
		{"the default index", "xs = [1, None]\nys = xs\nprint(ys.pop() is None)\n", "True\n"},
		{"the alias mutated, the name reads back", "xs = [1, 2]\nys = xs\nys.append(\"a\")\nprint(xs.pop(2))\nprint(xs)\n", "a\n[1, 2]\n"},
		{"the source mutated, the alias reads back", "xs = [1, 2]\nys = xs\nxs.append(\"a\")\nprint(ys.pop(2))\nprint(ys)\n", "a\n[1, 2]\n"},
		{"the far end of a chain mutated", "xs = [\"a\", \"b\"]\nys = xs\nzs = ys\nzs.append(7)\nprint(xs.pop(2))\nprint(xs)\n", "7\n['a', 'b']\n"},
		{"append through the alias reaches the object", "xs = [1, 2]\nys = xs\nys.append(3)\nprint(xs)\nprint(ys)\nprint(ys.pop(0))\n", "[1, 2, 3]\n[1, 2, 3]\n1\n"},
		{"the source rebound under its own alias", "xs = [None, 1]\nys = xs\nxs = [2, None]\nprint(ys)\nprint(ys[0])\n", "[None, 1]\nNone\n"},
		{"the printer asks the alias", "xs = [None, 1]\nys = xs\nprint(ys)\n", "[None, 1]\n"},
		{"the length asks the alias", "xs = [None, \"a\"]\nys = xs\nprint(len(ys))\n", "2\n"},
		{"a single-kind list keeps the road it had", "xs = [7, 8, 9]\nys = xs\nprint(ys.pop(0))\nprint(ys)\n", "7\n[8, 9]\n"},
		{"a scalar alias carries no list claim", "a = 1\nb = a\nxs = [None, 1]\nprint(b)\nprint(xs[0])\n", "1\nNone\n"},
		// The dict's and the set's half, which **Gap R.419** measured as a payload printed under a label and
		// a member that reached `@str_tab` untagged. The reference's bytes are re-derived inside every case.
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
		{"a text value read out of the alias and printed twice", "d = {\"a\": 1, \"b\": \"x\"}\ne = d\nprint(e[\"b\"])\nprint(str(e[\"b\"]))\n", "x\nx\n"},
		{"the void read out of the alias and bound", "d = {\"a\": None, \"b\": \"x\"}\ne = d\nv = e[\"a\"]\nprint(v)\n", "None\n"},
		{"the aliased dict handed to a function", "def w(x):\n    print(x)\n\nd = {\"a\": 1}\ne = d\nw(e)\nw(d)\n", "{'a': 1}\n{'a': 1}\n"},
		{"a set member added through the alias", "s = {1, 2}\nt = s\nt.add(\"a\")\nprint(s)\nprint(t)\n", "{1, 2, 'a'}\n{1, 2, 'a'}\n"},
		{"a set member added through the source", "s = {1}\nt = s\ns.add(\"a\")\nprint(t)\nprint(s)\n", "{1, 'a'}\n{1, 'a'}\n"},
		{"a chain of three names, one member added", "s = {1}\nt = s\nu = t\nu.add(\"a\")\nprint(s)\nprint(t)\nprint(u)\n", "{1, 'a'}\n{1, 'a'}\n{1, 'a'}\n"},
		{"a float added through the alias", "s = {1}\nt = s\nt.add(1.5)\nprint(t)\n", "{1, 1.5}\n"},
		{"a loop over the alias's members", "s = {1, \"a\"}\nt = s\nfor v in t:\n    print(v)\n", "1\na\n"},
		{"the set rebound under its own alias", "s = {1}\nt = s\ns = {2, 3}\nprint(s)\nprint(t)\n", "{2, 3}\n{1}\n"},
		// The set's ORDER, which is the reference's bucket order and not the order the source named the
		// members in (**Gap R.421**, ADR 0415). The linked binary and `python3` print these byte for byte.
		{"the void added to a set through the alias", "s = {1}\nt = s\nt.add(None)\nprint(t)\nprint(s)\n", "{None, 1}\n{None, 1}\n"},
		{"the void added to a set of numbers", "s = set()\ns.add(1)\ns.add(None)\nprint(s)\n", "{None, 1}\n"},
		{"a set the number joins after the void", "s = {2}\ns.add(None)\ns.add(1)\nprint(s)\n", "{None, 1, 2}\n"},
		{"the verdict added to a set of numbers", "s = {2}\ns.add(True)\nprint(s)\n", "{True, 2}\n"},
		{"the void added through the alias, the number after it", "s = {1}\nt = s\nt.add(None)\nt.add(3)\nprint(t)\n", "{None, 1, 3}\n"},
		{"the loop over a set the void joined", "s = {2}\ns.add(1)\ns.add(None)\nfor x in s:\n    print(x)\n", "None\n1\n2\n"},
		{"the member added twice is one member", "s = {1, 2}\ns.add(1)\nprint(len(s))\nprint(s)\n", "2\n{1, 2}\n"},
		{"the alias promotes and the void keeps its place", "s = {1}\nt = s\nt.add(\"a\")\nt.add(None)\nprint(s)\nprint(t)\n", "{None, 1, 'a'}\n{None, 1, 'a'}\n"},
		{"zero and the void, both in bucket 0", "s = set()\ns.add(4)\ns.add(None)\ns.add(0)\nprint(s)\n", "{None, 0, 4}\n"},
	}
}

// TestTheAliasFamilyAnswersLikeTheReference is the reference leg: each program runs against a live `python3`
// whose bytes are re-derived inside the case, and the linked binary must print the same thing at exit 0.
func TestTheAliasFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range aliasRecordFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_alias_record.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("this file's premise drifted: the reference prints %q and the table pins %q\n%s", want, tc.want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 {
					t.Fatalf("%s: exit %d where the reference prints %q: %s\n%s", engine, code, want, stderr, tc.src)
				}
				if stdout != want {
					t.Fatalf("%s: printed %q, the reference printed %q\n%s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// TestTheAliasFamilyIsOnTheRecord makes the family's coverage auditable: every program this row pays for has
// the reference's answer on the record, so the answer cannot be deleted by deleting a test and cannot be
// certified by the backend that produced it (ADR 0302, ADR 0309).
func TestTheAliasFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	seen := map[string]bool{}
	for _, tc := range aliasRecordFamily() {
		if seen[tc.src] {
			continue
		}
		seen[tc.src] = true
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d of the %d programs this row pays for are not on the record — write them from the reference "+
			"with tools/recmerge:\n  %s", len(missing), len(seen), strings.Join(missing, "\n  "))
	}
}

// TestTheAliasTrapAtTheCLIIsTheReferenceTrap asks the family's traps: a list emptied through one name and
// popped through the other raises the reference's own IndexError, at exit 3 — never exit 1 (a refusal, which
// would be the compiler declining a program the reference runs) and never exit 2 (ADR 0166's broken module).
func TestTheAliasTrapAtTheCLIIsTheReferenceTrap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, sentence string
	}{
		{"popped past the end through the alias", "xs = [None, 1]\nys = xs\nprint(ys.pop(0))\nprint(ys.pop(0))\nprint(ys.pop(0))\n", "pop from empty list"},
		{"popped past the end through the name", "xs = [None]\nys = xs\nprint(ys.pop(0))\nprint(xs.pop(0))\n", "pop from empty list"},
		{"a bad index through the alias", "xs = [None, 1]\nys = xs\nprint(ys.pop(7))\n", "pop index out of range"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_alias_trap.gy", tc.src)
			for _, engine := range cliEngines {
				_, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: exit 2 — the module was the compiler's mistake, not the program's (ADR 0166)\n%s", engine, stderr)
				}
				if code == 1 {
					t.Fatalf("%s: the compiler refused a program the reference runs to a trap: %s\n%s", engine, stderr, tc.src)
				}
				if code != 3 {
					t.Fatalf("%s: exit %d, want 3 for the reference's trap: %s\n%s", engine, code, stderr, tc.src)
				}
				if !strings.Contains(stderr, "IndexError: "+tc.sentence) {
					t.Fatalf("%s: the raise reads %q, which does not carry %q\n%s", engine, stderr, "IndexError: "+tc.sentence, tc.src)
				}
			}
		})
	}
}

// TestTheAliasTrapIsCatchableByTheProgram keeps the trap inside the language: the program's own handler sees
// it, whichever name the removal was written against — the difference between a trap the runtime raises and a
// diagnostic the compiler prints.
func TestTheAliasTrapIsCatchableByTheProgram(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"xs = []\nys = xs\ntry:\n    print(ys.pop(0))\nexcept IndexError as e:\n    print(\"caught\", e)\n",
		"xs = [1]\nys = xs\ntry:\n    print(ys.pop(0))\n    print(ys.pop(0))\nexcept IndexError as e:\n    print(\"caught\", e)\n",
	} {
		src := src
		t.Run("catch", func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_alias_catch.gy", src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 {
					t.Fatalf("%s: exit %d where the reference prints %q: %s\n%s", engine, code, want, stderr, src)
				}
				if stdout != want {
					t.Fatalf("%s: printed %q, the reference printed %q\n%s", engine, stdout, want, src)
				}
			}
		})
	}
}

// TestWhatTheAliasRowStillOwesAtTheCLI is the reference leg of the honest half: each row pins the reference's
// bytes beside this backend's, and names the row that owns the fix. When the program starts printing the
// reference's bytes the row is DELETED, not edited.
func TestWhatTheAliasRowStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name, src, gap, python, compiled string
		crashes                          bool
		refuses                          bool // the shape is DECLINED in words, which is a different debt
	}{
		{
			name: "the callee's printer handed the alias's popped slot", gap: "Gap R.417",
			src:    "def w(v):\n    print(v)\n\nxs = [None, 1]\nys = xs\nw(ys.pop(0))\n",
			python: "None\n", compiled: "0\n",
		},
		// The residue of the dict's and the set's half, each byte-identical on the parent build `8aa3029`
		// and therefore a ROW rather than this commit's damage (**ADR 0166**, **ADR 0309**).
		{
			name: "the dict rebound under its own alias", gap: "Gap R.420",
			src:    "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\n",
			python: "{'b': 2}\n{'a': 1}\n", compiled: "{'b': 2}\n{'b': 2}\n",
		},
		{
			name: "a text discarded from a set of numbers through the alias", gap: "Gap R.422",
			src:    "s = {1, 2}\nt = s\nt.discard(\"a\")\nprint(t)\nprint(s)\n",
			python: "{1, 2}\n{1, 2}\n", compiled: "{1}\n{1}\n",
		},
		{
			name: "the set built from a literal, joined by a number", gap: "Gap R.423",
			src:    "s = {7, 2}\ns.add(9)\nprint(s)\n",
			python: "{9, 2, 7}\n", compiled: "{9, 7, 2}\n",
		},
		{
			name: "the set built from a literal and never reordered", gap: "Gap R.423",
			src:    "s = {3, 1, 2}\nprint(s)\n",
			python: "{1, 2, 3}\n", compiled: "{3, 1, 2}\n",
		},
		{
			name: "the text that joins a set of numbers", gap: "Gap R.425",
			src:    "s = set()\ns.add(3)\ns.add(\"a\")\nprint(s)\n",
			python: "{'a', 3}\n", compiled: "{3, 'a'}\n",
		},
		{
			name: "two members whose buckets collide", gap: "Gap R.424",
			src:    "s = set()\ns.add(5)\ns.add(3)\ns.add(9)\ns.add(1)\nprint(s)\n",
			python: "{9, 3, 5, 1}\n", compiled: "{9, 1, 3, 5}\n",
		},
		{
			name: "the set that outgrows the first table", gap: "Gap R.424",
			src:    "s = set()\nfor i in range(9):\n    s.add(i * 4)\nprint(s)\n",
			python: "{0, 32, 4, 8, 12, 16, 20, 24, 28}\n", compiled: "{0, 8, 16, 24, 32, 4, 12, 20, 28}\n",
		},
		{
			name: "a loop over the alias's key/value pairs", gap: "Gap R.174",
			src:    "d = {\"a\": 1, \"b\": 2}\ne = d\nfor k, v in e.items():\n    print(k, v)\n",
			python: "a 1\nb 2\n", compiled: "", refuses: true,
		},
		{
			name: "a set member removed by value through the alias", gap: "Gap R.174",
			src:    "s = {1, \"a\"}\nt = s\nt.remove(1)\nprint(t)\n",
			python: "{'a'}\n", compiled: "", refuses: true,
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_alias_owed.gy", r.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != r.python {
				t.Fatalf("the reference no longer prints %q for this program (it prints %q) — this row's premise "+
					"drifted\n%s", r.python, want, r.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 0 && stdout == r.python {
					t.Fatalf("%s closed at the CLI: the program prints the reference's bytes — delete this row and "+
						"move the program into aliasRecordFamily()\n%s", r.gap, r.src)
				}
				if r.crashes {
					continue // the artifact is at fault (ADR 0166's class), which is the row's own subject
				}
				if r.refuses {
					// A refusal is the standing of a door this row does not open — a container METHOD over a
					// NAME (**Gap R.174**) — and it must stay a refusal in words, never an answer and never
					// the assembler's exit 2 (ADR 0006's exit-code contract).
					if code == 2 {
						t.Fatalf("%s: exit 2 — the module was the compiler's mistake, not the program's (ADR 0166)\n%s",
							engine, stderr)
					}
					if code != 1 {
						t.Fatalf("%s: the shape exited %d, want 1 for a refusal in words: %s\n%s",
							r.gap, code, stderr, r.src)
					}
					continue
				}
				if code != 0 {
					t.Fatalf("%s: the shape was REFUSED (exit %d) rather than answered wrongly — an answer and a "+
						"refusal are different debts; move this row\n%s", engine, code, r.src)
				}
				if stdout != r.compiled {
					t.Fatalf("%s changed shape at the CLI: compiled prints %q, this row pins %q\n%s",
						r.gap, stdout, r.compiled, r.src)
				}
			}
		})
	}
}

// TestTheAliasRefusalsAreMachineReadable is the agentic face: the shape the alias road declines reaches an
// agent as a capability code, a suggestion and the roadmap rows that own it — never as prose alone
// (ADR 0006, ADR 0346) — and it does so identically whether the read is written against the name or against
// the alias, which is the point of a row about names.
func TestTheAliasRefusalsAreMachineReadable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, code string }{
		{
			name: "a dict's pop over an alias of it",
			src:  "d = {\"a\": 1}\ne = d\nprint(e.pop(\"a\"))\n",
			code: "lower.unsupported.container-method-over-a-name",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_alias_json.gy", tc.src)
			out, code := cliRunCode(t, "--json", path)
			if code != 1 {
				t.Fatalf("a refused program exited %d, want 1 (ADR 0006): %s", code, out)
			}
			for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"code":"` + tc.code, `"suggestion":`, `"rows":`} {
				if !strings.Contains(out, frag) {
					t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
				}
			}
			if strings.Contains(out, "%!s(") || strings.Contains(out, "*lang.") {
				t.Fatalf("the JSON refusal prints the compiler's own types at the agent:\n%s", out)
			}
		})
	}
}

// TestWhatTheAliasRowStillRefusesWithoutACode is the second honest half, and it is a row about the INTERFACE
// rather than the answer: the one-word position's refusal — the same sentence whether the read is written
// against the name or its alias — reaches an agent as `"capability":null`, which is **Gap R.408**'s defect
// measured on a refusal nobody had pointed `--json` at since the road grew. The row is pinned WITH today's
// artifact rather than fixed here, because a row about a door this cycle did not open belongs on the tracker
// with the bytes beside it (ADR 0006, ADR 0346).
func TestWhatTheAliasRowStillRefusesWithoutACode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, sentence string }{
		{
			name:     "a slot of the alias bound and asked as a condition",
			src:      "xs = [None, \"a\"]\nys = xs\nv = ys[0]\nif v:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
			sentence: "this position keeps one word for its operand",
		},
		{
			name:     "the same program with no alias in it at all",
			src:      "xs = [None, \"a\"]\nv = xs[0]\nif v:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
			sentence: "this position keeps one word for its operand",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_alias_nocode.gy", tc.src)
			out, code := cliRunCode(t, "--json", path)
			if code != 1 {
				t.Fatalf("the one-word position now answers rather than refusing (exit %d): %s\n%s", code, out, tc.src)
			}
			if !strings.Contains(out, tc.sentence) {
				t.Fatalf("the refusal no longer says %q — the row owns a different sentence now:\n%s", tc.sentence, out)
			}
			if !strings.Contains(out, `"capability":null`) {
				t.Fatalf("this refusal now names a capability code — **Gap R.408**'s defect is paid for it, so delete "+
					"this row and move the program into TestTheAliasRefusalsAreMachineReadable:\n%s", out)
			}
			if !strings.Contains(out, "Gap R.146") {
				t.Fatalf("the refusal no longer names the row that owns it:\n%s", out)
			}
		})
	}
}
