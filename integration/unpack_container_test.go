package integration

// A container the source spells, unpacked to a tuple target, binds the object — at the CLI, against the reference.
//
// roadmap Gap R.524 · ADR 0454
//
// `x = [1]` has compiled and printed `[1]` for hundreds of rows. `x, y = [1], 2` emitted
// `store i32 @.lst1, i32* %_x` and `llc-20` refused the module — `global variable reference must have pointer
// type` — so an ordinary program died in the toolchain rather than in the language, at the exit class the
// contract reserves for a compiler bug (ADR 0166). The unpacking now hands each spelled container element to
// the same plain binding road the ordinary assignment walks, after every element has been read and before any
// is stored.
//
// Every expectation here is the reference's own bytes, read out of a live `python3` inside the case, so a row
// this table disagrees with is stale rather than green — including the row that shows the reference RUNS what
// this backend refuses (Gap R.526's snapshot), which is the difference between a named decline and a silent one.

import (
	"strings"
	"testing"
)

func TestASpelledContainerUnpackedAtTheCLIBindsTheObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []string{
		"x, y = [1], 2\nprint(x)\nprint(y)\n",
		"a, b = {1, 2}, {\"k\": 1}\nprint(a)\nprint(b)\nprint(len(a))\nprint(\"k\" in b)\n",
		"p, q = [], {}\nprint(p)\nprint(q)\np.append(1)\nprint(p)\nq[\"z\"] = 3\nprint(q)\nprint(len(p), len(q))\n",
		"xs, ys = [1, \"a\", None], [2.5]\nprint(xs)\nprint(ys)\nprint(xs[0])\nprint(xs[1])\nprint(len(xs))\n",
		"xs, ys = [1, 2], [3]\nxs.append(9)\nprint(xs)\nprint(len(xs))\nprint(ys)\n",
		"m, n = [[1], {\"a\": [1, 2]}]\nprint(m)\nprint(n)\nprint(n[\"a\"])\nprint(m[0])\nprint(len(n))\n",
		"xs, ys = [\"a\", \"b\"], 1\nprint(xs)\nprint(xs[0])\nprint(str(xs))\nprint(repr(xs))\n",
		"x, y = [1], 2\nprint(1 in x)\nprint(3 in x)\n",
		"xs, n = [7, 8], 0\nfor v in xs:\n    print(v)\n",
		"d, k = {}, \"a\"\nd[k] = 1\nprint(d)\nprint(len(d))\n",
		"a, b = [1, [2, [3]]], 4\nprint(a)\nprint(a[1][1][0])\n",
		"t, u = (1, 2), \"x\"\nprint(t)\nprint(u)\nprint(len(t))\nprint(t[1])\n",
		"a, b = [1], [2]\na, b = b, a\nprint(a)\nprint(b)\n",
		"xs = [0]\nn = 0\nxs, n = [4, 5], 9\nprint(xs)\nprint(n)\nprint(len(xs))\n",
	}
	for _, src := range rows {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Fatalf("the reference does not run this program to completion: %q", want)
			}
			p := writeSrc(t, dir, "unpack_container", src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want the reference's %q\n%s", engine, out, want, src)
				}
			}
		})
	}
}

// TestAnUnpackedContainerReachesTheLLVMVerifier is the claim the stdout cannot make: before this row the program
// never produced an executable image at all, so no amount of reading printed bytes could distinguish "fixed"
// from "the failure moved". `--verify-llvm-file` runs the module through LLVM's own checker, and the emitted
// text is asked for the IR that `llc` rejected.
func TestAnUnpackedContainerReachesTheLLVMVerifier(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, src := range []string{
		"x, y = [1], 2\nprint(x)\n",
		"a, b = {1, 2}, {\"k\": 1}\nprint(a)\n",
		"p, q = [], {}\nprint(p)\nprint(q)\n",
	} {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			out, code := cliRunCode(t, "--emit-llvm", src)
			if code != 0 {
				t.Fatalf("--emit-llvm exited %d: %s", code, out)
			}
			if strings.Contains(out, "store i32 @") {
				t.Errorf("the module stores a global into a value slot — the IR `llc` rejects:\n%s", out)
			}
			gy := writeSrc(t, dir, "unpack_verify", src)
			if out, code := cliReport(t, "--verify-llvm-file", gy); code != 0 || !strings.Contains(out, "module verified") {
				t.Errorf("--verify-llvm-file did not verify the module (code %d): %s\n%s", code, out, src)
			}
		})
	}
}

// TestTheUnpackingNamesWhatItCannotBuild is the honest half: the one shape this row declines instead of answering
// is refused in words that name the missing snapshot and the row that owns it, and the reference's own answer is
// read out of the reference inside the case — so the decline is on the record as a decline, not written off.
func TestTheUnpackingNamesWhatItCannotBuild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "a, b = 1, 2\nb, a = [b], [a]\nprint(a)\nprint(b)\n"
	want, ok := cpythonPlainOut(t, dir, src)
	if !ok {
		t.Fatalf("the reference does not run this program: %q", want)
	}
	if strings.TrimSpace(want) != "[1]\n[2]" {
		t.Fatalf("row is stale: the reference prints %q", want)
	}
	p := writeSrc(t, dir, "unpack_alias", src)
	for _, engine := range cliEngines {
		out, code := cliRunMerged(t, engine, "--file", p)
		if code == 0 {
			t.Fatalf("%s answered where the row declines to guess:\n%s", engine, out)
		}
		if code == 2 {
			t.Fatalf("%s exited 2 on the compiler's own module (ADR 0166): %s", engine, out)
		}
		if !strings.Contains(out, "Gap R.526") {
			t.Errorf("%s refused without naming the row that owns the missing snapshot: %s", engine, out)
		}
		if strings.Contains(out, "llc") || strings.Contains(out, "global variable reference") {
			t.Errorf("%s reached the toolchain instead of refusing: %s", engine, out)
		}
	}
}
