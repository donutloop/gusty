package lang

// A container the SOURCE spells, unpacked to a tuple target, binds the OBJECT — and the module says so in
// its own bytes.
//
// roadmap Gap R.524 · ADR 0454 (measured while sweeping the residue of ADR 0453)
//
// `x = [1]` compiles, prints `[1]`, and has for hundreds of rows. `x, y = [1], 2` — the same literal, the same
// value, one binding position over — emitted
//
//	store i32 @.lst1, i32* %_x
//
// and `llc-20` rejected the module: `global variable reference must have pointer type`. That is not a wrong
// answer, a missing feature or a refusal; it is invalid IR, which the exit-code contract (ADR 0166) charges to
// the compiler for an ordinary program. The unpacking road lowered the element with the ordinary one-word road,
// and the ordinary road's answer for a container literal is the compile-time global's NAME — a length beside an
// array, a whole struct that cannot live in an `i32`.
//
// The fix is not a second container builder. The unpacking hands each spelled container element to the SAME
// plain binding road that `x = [1]` walks — the same `rt_alloc`, the same per-slot tags, the same twelve
// container claims, the same rooting store (**ADR 0163**, **Gap J.2** / **ADR 0234**) — and it hands it over
// *after* every element has been read out of its slot and *before* any element is stored, which is what keeps an
// unpacking simultaneous. `TestTheUnpackedContainerIsStoredAsAnObjectNotAName` is the claim in the module's own
// bytes: no `store i32 @.` anywhere, and the heap door asked in `@main`.
//
// The one shape this row does not answer is named rather than answered: a container element that mentions a name
// the same statement re-binds (`b, a = [b], [a]`). The reference builds it from the value the name HAD; this
// road can only build it from the slot as the binding left it, and before this row it printed `[[[[…` until the
// process died. That is **Gap R.526**, pinned below with the refusal's words.

import (
	"os"
	"strings"
	"testing"
)

// unpackedContainerRows is the family the door now reaches. Every `want` is the reference's own bytes, read by
// running the same source through python3 outside this file before it was written here; the record leg then
// re-asserts each one from `testdata/interpreter-golden.json` (ADR 0302: a row a leg cannot self-certify).
func unpackedContainerRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"the row that was rejected by llc",
			"x, y = [1], 2\nprint(x)\nprint(y)\n",
			"[1]\n2\n",
		},
		{
			"a set and a dict in the same unpacking",
			"a, b = {1, 2}, {\"k\": 1}\nprint(a)\nprint(b)\nprint(len(a))\nprint(\"k\" in b)\n",
			"{1, 2}\n{'k': 1}\n2\nTrue\n",
		},
		{
			"both empty, and then written to",
			"p, q = [], {}\nprint(p)\nprint(q)\np.append(1)\nprint(p)\nq[\"z\"] = 3\nprint(q)\nprint(len(p), len(q))\n",
			"[]\n{}\n[1]\n{'z': 3}\n1 1\n",
		},
		{
			"a container whose slots describe themselves",
			"xs, ys = [1, \"a\", None], [2.5]\nprint(xs)\nprint(ys)\nprint(xs[0])\nprint(xs[1])\nprint(len(xs))\n",
			"[1, 'a', None]\n[2.5]\n1\na\n3\n",
		},
		{
			"appended to after the binding",
			"xs, ys = [1, 2], [3]\nxs.append(9)\nprint(xs)\nprint(len(xs))\nprint(ys)\n",
			"[1, 2, 9]\n3\n[3]\n",
		},
		{
			"a container inside a container",
			"m, n = [[1], {\"a\": [1, 2]}]\nprint(m)\nprint(n)\nprint(n[\"a\"])\nprint(m[0])\nprint(len(n))\n",
			"[1]\n{'a': [1, 2]}\n[1, 2]\n1\n1\n",
		},
		{
			"a text, which has always interned rather than stored a pointer",
			"xs, ys = [\"a\", \"b\"], 1\nprint(xs)\nprint(xs[0])\nprint(str(xs))\nprint(repr(xs))\n",
			"['a', 'b']\na\n['a', 'b']\n['a', 'b']\n",
		},
		{
			"asked a membership question",
			"x, y = [1], 2\nprint(1 in x)\nprint(3 in x)\n",
			"True\nFalse\n",
		},
		{
			"looped over",
			"xs, n = [7, 8], 0\nfor v in xs:\n    print(v)\n",
			"7\n8\n",
		},
		{
			"a dict entry written through the name the unpacking opened",
			"d, k = {}, \"a\"\nd[k] = 1\nprint(d)\nprint(len(d))\n",
			"{'a': 1}\n1\n",
		},
		{
			"three deep",
			"a, b = [1, [2, [3]]], 4\nprint(a)\nprint(a[1][1][0])\n",
			"[1, [2, [3]]]\n3\n",
		},
		{
			"a tuple element, which was already an object",
			"t, u = (1, 2), \"x\"\nprint(t)\nprint(u)\nprint(len(t))\nprint(t[1])\n",
			"(1, 2)\nx\n2\n2\n",
		},
		{
			// The two container bindings beside a name swap: the swap is the simultaneity claim, and the
			// containers are the reason it had to be re-checked (each is bound by a road that stores).
			"containers swapped through names keep the swap",
			"a, b = [1], [2]\na, b = b, a\nprint(a)\nprint(b)\n",
			"[2]\n[1]\n",
		},
		{
			"a container and a scalar rebound in one statement",
			"xs = [0]\nn = 0\nxs, n = [4, 5], 9\nprint(xs)\nprint(n)\nprint(len(xs))\n",
			"[4, 5]\n9\n2\n",
		},
	}
}

func TestASpelledContainerUnpackedToATupleTargetBindsTheObject(t *testing.T) {
	t.Parallel()
	for _, r := range unpackedContainerRows() {
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

// TestTheUnpackedContainerIsStoredAsAnObjectNotAName is the claim this row was filed for, in the module's own
// bytes rather than in its stdout. `store i32 @.lst1, i32* %_x` is what `llc` rejected; no amount of reading the
// printed answer can tell a heap object from a global reference that happened to survive, because before this
// row nothing survived. So the module is asked directly: no store of a global anywhere, and the heap door
// actually called in `@main`.
func TestTheUnpackedContainerIsStoredAsAnObjectNotAName(t *testing.T) {
	t.Parallel()
	t.Run("no global is stored into a value slot", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			"x, y = [1], 2\nprint(x)\n",
			"a, b = {1, 2}, {\"k\": 1}\nprint(a)\n",
			"p, q = [], {}\nprint(p)\n",
			"xs, ys = [1, \"a\", None], [2.5]\nprint(xs)\n",
		} {
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("compile: %v\n%s", err, src)
			}
			if strings.Contains(res.IR, "store i32 @") {
				t.Errorf("the module stores a global into a value slot — this is the IR `llc` rejects:\n%s", inMainBody(t, res.IR))
			}
			if strings.Contains(res.IR, "ret i32 @") {
				t.Errorf("the module returns a global as a value:\n%s", inMainBody(t, res.IR))
			}
		}
	})
	t.Run("the heap door is asked, for each of the three kinds", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			src, want string
		}{
			{"x, y = [1], 2\nprint(x)\n", "@rt_set_elem("},
			{"a, b = {1, 2}, 3\nprint(a)\n", "@rt_set_add("},
			{"a, b = {\"k\": 1}, 3\nprint(a)\n", "@rt_dict_put("},
		}
		for _, c := range cases {
			c := c
			t.Run(c.want, func(t *testing.T) {
				t.Parallel()
				res, err := Compile(c.src)
				if err != nil {
					t.Fatalf("compile: %v\n%s", err, c.src)
				}
				main := inMainBody(t, res.IR)
				if !strings.Contains(main, c.want) {
					t.Errorf("@main never asked %s — the element was not built as an object:\n%s", c.want, main)
				}
				if !strings.Contains(main, "@rt_alloc(") {
					t.Errorf("@main never allocated a container:\n%s", main)
				}
			})
		}
	})
	t.Run("the plain binding and the unpacking emit one shape, not two", func(t *testing.T) {
		t.Parallel()
		// One door, both roads: the same literal through `x = [1]` and through `x, y = [1], 2` must reach the
		// same builders the same number of times, or the two bindings have quietly become two features
		// (roadmap ADR 0232's one question, and the reason the unpacking delegates rather than copies).
		direct, err := Compile("x = [1, \"a\"]\nprint(x)\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		unpacked, err := Compile("x, y = [1, \"a\"], 2\nprint(x)\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		dm, um := inMainBody(t, direct.IR), inMainBody(t, unpacked.IR)
		for _, call := range []string{"@rt_alloc(", "@rt_set_elem(", "@rt_tag_elem("} {
			if strings.Count(dm, call) == 0 {
				t.Fatalf("the plain binding does not ask %s either — the pair of rows proves nothing", call)
			}
			if strings.Count(um, call) != strings.Count(dm, call) {
				t.Errorf("the two bindings ask %s %d and %d times:\n%s\n%s", call,
					strings.Count(dm, call), strings.Count(um, call), dm, um)
			}
		}
	})
}

// TestWhatTheUnpackingStillRefusesToPretend pins the one shape this row declines instead of answering. The
// reference reads a value that the statement is about to overwrite; the unpacking can only build a container
// from the slot as the binding left it, and the result before this row was a self-referential literal printed
// until the process died. A refusal that names the missing half is the artifact ADR 0166 asks for, and the row
// that owns the snapshot is Gap R.526.
func TestWhatTheUnpackingStillRefusesToPretend(t *testing.T) {
	t.Parallel()
	rows := []struct{ name, src, contains string }{
		{
			"a container element that mentions a name the statement re-binds",
			"a, b = 1, 2\nb, a = [b], [a]\nprint(a)\n",
			"Gap R.526",
		},
		{
			"the same, one side only",
			"a, b = 1, 2\nb, a = [a], 9\nprint(b)\n",
			"Gap R.526",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			_, err := RunSnippet(r.src)
			if err == nil {
				t.Fatalf("the shape compiled — Gap R.526 has paid its snapshot, so move this row to the table above\n%s", r.src)
			}
			if !strings.Contains(err.Error(), r.contains) {
				t.Fatalf("it failed for another reason (%v); the row pins the refusal %s owns\n%s", err, r.contains, r.src)
			}
			if strings.Contains(err.Error(), "llc") {
				t.Fatalf("the shape still reaches `llc` — a refusal must not become a module rejection: %v", err)
			}
		})
	}
}

// TestTheUnpackingRowsAreTheRowsTheRowClaims keeps the tables honest: every answer row must be on the record, and
// the module-wide guard is only worth running if the module it runs on is the one the table claims.
func TestTheUnpackingRowsAreTheRowsTheRowClaims(t *testing.T) {
	t.Parallel()
	rows := unpackedContainerRows()
	if len(rows) < 12 {
		t.Fatalf("the table holds %d rows; this row's claim is a family, not a single shape", len(rows))
	}
	for _, r := range rows {
		if !strings.Contains(r.src, ", ") {
			t.Fatalf("row %q is not an unpacking — it belongs in the container-binding rows", r.name)
		}
	}
}

// TestTheUnpackingRecordSourcesTheTableAsks is the recorder, not a case: with GUSTY_RECORD_UNPACK set it writes every
// source the table above asks about, in the shape `tools/recmerge` reads, and skips. Without it, it skips — and
// like the other artifact-writing modes it is scrubbed from every subprocess the suite starts
// (`integration/parallelism_guard_test.go`, Gap R.410 / ADR 0409), because a recorder that N shards each
// overwrite is not evidence.
//
// The list is read from the table rather than retyped: a recorder that drifts from its own cases is what lets a
// row ship with no record under it (ADR 0302).
func TestTheUnpackingRecordSourcesTheTableAsks(t *testing.T) {
	out := os.Getenv("GUSTY_RECORD_UNPACK")
	if out == "" {
		t.Skip("the recorder: set GUSTY_RECORD_UNPACK=<file> to dump these sources for tools/recmerge")
	}
	var srcs []string
	for _, r := range unpackedContainerRows() {
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
