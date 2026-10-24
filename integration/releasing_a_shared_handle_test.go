package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/releasing_a_shared_handle_test.go — the REFERENCE leg of **ADR 0414** (roadmap **Gap R.420**):
// what the LINKED PROGRAM prints when a container is rebound under one name while another name still holds it.
//
//	d = {"a": 1}          d = {"a": 1}
//	e = d          →      e = d
//	d = {"b": 2}          d = {"b": 2}
//	print(d)              print(d)   # {'b': 2}   both answered
//	print(e)              print(e)   # {'b': 2} ← before, at exit 0 — the reference prints {'a': 1}
//
// The rebinding released the object the name held — `emitFreeOld` pushed its slot on the allocator's free
// list, a road with no notion of who else is reading — and the very next `rt_alloc` took that slot back and
// wrote the second dict into it, so the alias's handle denoted the NEW object. `print(e["a"])` raised
// `KeyError: 'a'` over a dict that had never lost its `a`: the same lie told twice, once as bytes and once as
// a raise the program had not earned. The collector could not undo it (rt_gc marks the free list dead before
// it marks from the roots, and rt_gc_mark only marks an unmarked slot), so the question moved to the door in
// front of the push: `@rt_release` asks the collector's own root table and declines to free what a name still
// names.
//
// Every expectation here is re-derived from a live `python3` inside the case, so a table whose premise drifted
// fails rather than certifies the drift (ADR 0166). The trap rows are checked as traps: exit **3**, never
// exit 1 (a front-end refusal) and never exit **2** (a module `llc` had to reject).
//
// The cost side is measured and not promised: a release the door declined is billed to the arena until the
// sweep, so the churn case below runs 2000 iterations that each vend one container and hold another, and the
// collector's own report has to say the arena never ran out.

// sharedHandleCLIFamily is the answered family at the CLI. The reference's bytes are asked inside each case;
// these strings are the shape, not the oracle.
func sharedHandleCLIFamily() []struct {
	name, src string
} {
	return []struct {
		name, src string
	}{
		{"the row's own program", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\n"},
		{"the held entry read through the alias", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\nprint(e[\"a\"])\n"},
		{"the entry the alias never lost", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(e[\"a\"])\nprint(\"a\" in e)\nprint(\"b\" in e)\nprint(len(e))\n"},
		{"a chain of three names", "d = {\"a\": 1}\ne = d\nf = e\nd = {\"b\": 2}\nprint(d)\nprint(e)\nprint(f)\n"},
		{"the same name rebound twice", "d = {\"a\": 1}\ne = d\nf = d\nd = {\"b\": 2}\nd = {\"c\": 3}\nprint(e)\nprint(f)\nprint(d)\n"},
		{"the alias itself rebound", "d = {\"a\": 1}\ne = d\nf = e\ne = {\"b\": 2}\nprint(d)\nprint(e)\nprint(f)\n"},
		{"a list held by a second name", "xs = [1]\nys = xs\nxs = [2]\nprint(ys)\nprint(xs)\n"},
		{"a set held by a second name", "s = {1}\nt = s\ns = {2, 3}\nprint(t)\nprint(s)\n"},
		{"a dict's slot and a name hold one list", "xs = [1, 2]\nd = {\"k\": xs}\nxs = [3]\nprint(d)\nprint(xs)\nprint(d[\"k\"])\n"},
		{"a value read out of a dict, the dict rebound", "d = {\"a\": [1, 2]}\nv = d[\"a\"]\nd = {\"a\": [3]}\nprint(v)\nprint(d)\n"},
		{"held across a frame's own rebinding", "def f():\n    d = {\"a\": 1}\n    e = d\n    d = {\"b\": 2}\n    return e\n\nprint(f())\n"},
		{"the questions a container answers, asked of the held object", "d = {\"a\": 1, \"b\": 2}\ne = d\nd = {\"c\": 3}\nprint(len(e))\nprint(\"a\" in e)\nfor k in e:\n    print(k)\n"},
	}
}

// TestARebindingAtTheCLIKeepsWhatTheOtherNameHolds is the reference leg: the linked binary and `python3`
// answer the same program with the same bytes, at exit 0. The record leg is asked in the same case — a
// program this row pays is on all three legs or it is not paid (ADR 0302).
func TestARebindingAtTheCLIKeepsWhatTheOtherNameHolds(t *testing.T) {
	t.Parallel()
	for _, tc := range sharedHandleCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := writeSrc(t, dir, "rebound_alias.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
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

// TestTheCorpusProgramReboundsUnderAnAlias is the matrix leg: one file, every line the reference's.
func TestTheCorpusProgramReboundsUnderAnAlias(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("rebound_alias")
	want, refErr, rerr := lang.PythonRun(src)
	if rerr != nil {
		t.Fatalf("the reference could not answer the corpus program: %v (%s)", rerr, refErr)
	}
	if !lang.HasGoldenAnswer(src) {
		t.Fatal("programs/rebound_alias.gy is not on the record — the record leg cannot check a program that is not on it")
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/rebound_alias.gy")
	if code != 0 {
		t.Fatalf("--file: exit %d: %s", code, stderr)
	}
	if stdout != want {
		t.Fatalf("the corpus program drifted from the reference\n compiled: %q\n    python: %q", stdout, want)
	}
}

// TestTheUncaughtMissOverAHeldContainerIsTheReferencesTrap keeps the trap half of the row. Before the fix the
// program raised `KeyError: 'a'` for an entry the held dict still had; now the miss arrives only where the
// reference misses, with the reference's class and sentence, catchably, at exit 3.
func TestTheUncaughtMissOverAHeldContainerIsTheReferencesTrap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, sentence string
		raises              bool
	}{
		{
			name:     "the entry really gone",
			src:      "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(e[\"b\"])\n",
			sentence: "KeyError: 'b'", raises: true,
		},
		{
			name:     "the entry that was never gone",
			src:      "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(e[\"a\"])\n",
			sentence: "1", raises: false,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "rebound_miss.gy", tc.src)
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: exit 2 — the module was rejected, which ADR 0166 reserves for a compiler bug\n%s: %s",
						engine, tc.src, stderr)
				}
				if tc.raises {
					if code != 3 {
						t.Fatalf("%s: exit %d, want the trap's exit 3 for %s\n%s: %s", engine, code, tc.sentence, tc.src, stderr)
					}
					if !strings.Contains(stderr, tc.sentence) {
						t.Fatalf("%s: the raise is not the reference's — wanted %q in %q\n%s", engine, tc.sentence, stderr, tc.src)
					}
					if strings.Contains(stdout, "b") {
						t.Fatalf("%s: printed %q where the reference raises\n%s", engine, stdout, tc.src)
					}
					continue
				}
				if code != 0 || stdout != tc.sentence+"\n" {
					t.Fatalf("%s: exit %d, stdout %q, want %q (%s)\n%s", engine, code, stdout, tc.sentence, stderr, tc.src)
				}
			}
			// The reference agrees about which of the two programs raises.
			_, _, refErr := lang.PythonRun(tc.src)
			if tc.raises && refErr == nil {
				t.Fatalf("the reference no longer raises for this program — the row's premise drifted\n%s", tc.src)
			}
			if !tc.raises && refErr != nil {
				t.Fatalf("the reference started to raise for a program that answers\n%s", tc.src)
			}
		})
	}
}

// TestTheLinkedModuleReleasesThroughTheDoor reads the claim off the emitted IR at the CLI: the rebinding goes
// to `@rt_release`, and `@rt_free` — the free list's push — is reached from nowhere else in the program's own
// code. An agent that audits a build with `--emit-llvm` sees the same one door the compiler decided on.
func TestTheLinkedModuleReleasesThroughTheDoor(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\n",
		"xs = [1]\nys = xs\nxs = [2]\nprint(ys)\n",
		"s = {1}\nt = s\ns = {2, 3}\nprint(t)\n",
	} {
		src := src
		t.Run("module", func(t *testing.T) {
			t.Parallel()
			ir, stderr, code := cliStreams(t, "--emit-llvm", src)
			if code != 0 {
				t.Fatalf("--emit-llvm: exit %d: %s\n%s", code, stderr, src)
			}
			start := strings.Index(ir, "define i32 @main(")
			if start < 0 {
				t.Fatalf("no main in the emitted module:\n%s", src)
			}
			rest := ir[start:]
			end := strings.Index(rest, "\n}")
			body := rest[:end]
			if !strings.Contains(body, "call void @rt_release(") {
				t.Errorf("main releases nothing through the door — the rebinding went straight past it:\n%s", src)
			}
			if strings.Contains(body, "call void @rt_free(") {
				t.Errorf("main calls @rt_free directly: the free list was pushed without asking who holds the handle, "+
					"which is Gap R.420's shape:\n%s", src)
			}
			if n := strings.Count(ir, "call void @rt_free("); n != 1 {
				t.Errorf("the module calls @rt_free %d times; the release door is its only caller (the sweep pushes "+
					"its own slots inline):\n%s", n, src)
			}
			if v, verr, vcode := cliStreams(t, "--verify-llvm", src); vcode != 0 || !strings.Contains(v, "module verified") {
				t.Errorf("--verify-llvm did not certify the module (exit %d): %s %s\n%s", vcode, v, verr, src)
			}
		})
	}
}

// TestWhatASkippedReleaseCostsIsBilledUntilTheSweepAtTheCLI is the cost side, read from the collector's own
// report: the loop venders one container and holds another 2000 times, so each iteration leaves a release the
// door declined. If a declined release were a permanent pin the 1024-slot arena would run out and the answer
// would stop being the reference's; the report has to say it never did.
func TestWhatASkippedReleaseCostsIsBilledUntilTheSweepAtTheCLI(t *testing.T) {
	t.Parallel()
	const src = "total = 0\nfor i in range(2000):\n    d = {\"a\": i}\n    e = d\n    d = {\"b\": 1}\n    d = {\"c\": 2}\n    total = total + e[\"a\"]\nprint(total)\n"
	res, err := runReportingGC(src)
	if err != nil {
		t.Fatalf("the churn program could not be run: %v\n%s", err, src)
	}
	if res.Output != "1999000\n" {
		t.Fatalf("printed %q, want %q\n%s", res.Output, "1999000\n", src)
	}
	st, line := aotGCReport(t, res.Stderr)
	if st.Exhausted != 0 {
		t.Errorf("the arena ran out (%s): the sweep is not billing back what the release door declined", line)
	}
	if st.TotalFreed == 0 {
		t.Errorf("nothing was reclaimed across 2000 iterations that allocate four containers each (%s): the sweep is "+
			"the road that decides liveness, not the door that declines to push", line)
	}
	if st.Collections == 0 {
		t.Errorf("the collector never ran, so this case proved nothing about reclamation: %s", line)
	}
}
