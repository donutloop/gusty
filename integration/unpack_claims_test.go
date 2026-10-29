package integration

// A container that travels through an unpacking is still the object — at the CLI, against the reference.
//
// roadmap Gap R.527 · ADR 0455
//
// `xs = [1, 2]` / `a, b = xs, 5` / `print(a)` printed the handle (`0`) at the exit code of success;
// `a = [1]` / `b = 2` / `a, b = b, a` printed `[]` and `0` where CPython prints `2` and `[1]`;
// `d = {"k": 1}` / `a, b = d, 2` printed `1`, and `len(a)` of the same binding refused the build. The plain
// binding road has always carried a container's compile-time claims across a copy (ADR 0412); the unpacking's
// bind loop wrote the payload word and asked none of them.
//
// Every expectation here is the reference's own bytes, read out of a live `python3` inside the case, so a row
// this table disagrees with is stale rather than green. The IR rows ask the module the question stdout cannot:
// did the name that received the handle end up with the SAME records the plain binding would have given it —
// which is what decides whether `print(a)` renders a list or a number.

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

func unpackClaimsCLIRows() []string {
	return []string{
		"xs = [1, 2]\na, b = xs, 5\nprint(a)\nprint(b)\n",
		"s = {1, 2}\nd = {\"k\": 1}\na, b = s, d\nprint(a)\nprint(b)\nprint(len(a))\nprint(\"k\" in b)\n",
		"xs = [1, 2, 3]\na, n = xs, 0\nprint(len(a))\nprint(a[1])\nprint(2 in a)\n",
		"xs = [7, 8]\na, n = xs, 0\nfor v in a:\n    print(v)\n",
		"xs = [1]\na, n = xs, 0\na.append(9)\nprint(xs)\nprint(a)\n",
		"xs = [1]\na, n = xs, 0\nxs.append(\"z\")\nprint(a)\nprint(xs)\n",
		"a = [1]\nb = 2\na, b = b, a\nprint(a)\nprint(b)\n",
		"d = {\"a\": 1}\nxs = [1]\nd, xs = xs, d\nprint(d)\nprint(xs)\n",
		"p, q, r = [1], [2], 3\np, q, r = r, p, q\nprint(p)\nprint(q)\nprint(r)\n",
		"a = [1]\na, n = 1.5, 2\nprint(a)\nprint(n)\n",
		"xs = [5]\na = [1]\na, n = xs[0], 2\nprint(a)\nprint(n)\n",
		"def f():\n    xs = [1, 2]\n    ys = [3]\n    a, b = xs, ys\n    print(a)\n    print(b)\n    a, b = b, a\n    print(a)\n    print(b)\n\nf()\n",
		"d = {\"a\": 1}\nx, y = d, 0\nx[\"b\"] = 2\nprint(d)\nprint(len(d))\n",
		"m = [[1]]\np, q = m, 0\nprint(p[0])\nprint(p)\nprint(str(p))\n",
		"a = [1]\nb = 2\na, b = b, a\na = 7\nprint(a)\nprint(b)\n",
	}
}

func TestAContainerTravellingThroughAnUnpackingAtTheCLIBindsTheObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, src := range unpackClaimsCLIRows() {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Fatalf("the reference does not run this program to completion: %q", want)
			}
			p := writeSrc(t, dir, "unpack_claims", src)
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

// cliIRCallRE finds the runtime calls in the emitted module; the print-door filter keeps the claim about how a
// value is RENDERED, not about how many registers the program needs.
var cliIRCallRE = regexp.MustCompile(`call [^@]*@(rt_[A-Za-z0-9_]+)\(`)

func cliPrintDoors(t *testing.T, src string) []string {
	t.Helper()
	out, code := cliRunCode(t, "--emit-llvm", src)
	if code != 0 {
		t.Fatalf("--emit-llvm exited %d: %s", code, out)
	}
	start := strings.Index(out, "define i32 @main(")
	if start < 0 {
		t.Fatalf("no @main in the module:\n%s", out)
	}
	var doors []string
	for _, m := range cliIRCallRE.FindAllStringSubmatch(out[start:], -1) {
		if strings.Contains(m[1], "print") {
			doors = append(doors, m[1])
		}
	}
	sort.Strings(doors)
	return doors
}

// TestTheSwapLeavesTheNotebookThePlainBindingsLeave is the claim the stdout cannot make on its own: printing
// the right bytes through the wrong door is exactly what this row shipped before it was filed (`[]` for the
// number, `0` for the list — both renders of a payload under a label the object never gave it). So the module
// is asked, twice, and the two answers have to be the same doors.
func TestTheSwapLeavesTheNotebookThePlainBindingsLeave(t *testing.T) {
	t.Parallel()
	t.Run("a container copied by an unpacking, against a container copied by an assignment", func(t *testing.T) {
		t.Parallel()
		plain := cliPrintDoors(t, "xs = [1, 2]\nb = xs\nprint(b)\n")
		unpacked := cliPrintDoors(t, "xs = [1, 2]\na, b = xs, 0\nprint(a)\n")
		if len(plain) == 0 {
			t.Fatal("the plain copy asks no print door — the pair of rows proves nothing")
		}
		if strings.Join(plain, ",") != strings.Join(unpacked, ",") {
			t.Errorf("the two bindings render one object through different doors:\n plain   %v\n unpacked %v",
				plain, unpacked)
		}
	})
	t.Run("a swap, against the bindings it is meant to be indistinguishable from", func(t *testing.T) {
		t.Parallel()
		swapped := cliPrintDoors(t, "a = [1]\nb = 2\na, b = b, a\nprint(a)\nprint(b)\n")
		plain := cliPrintDoors(t, "a = 2\nb = [1]\nprint(a)\nprint(b)\n")
		if len(plain) == 0 {
			t.Fatal("the plain program asks no print door — the pair of rows proves nothing")
		}
		if strings.Join(plain, ",") != strings.Join(swapped, ",") {
			t.Errorf("after the swap the programs render through different doors:\n plain  %v\n swapped %v",
				plain, swapped)
		}
	})
	t.Run("the received handle is registered as a root", func(t *testing.T) {
		t.Parallel()
		out, code := cliRunCode(t, "--emit-llvm", "xs = [1, 2]\na, b = xs, 0\nprint(a)\n")
		if code != 0 {
			t.Fatalf("--emit-llvm exited %d: %s", code, out)
		}
		if !strings.Contains(out, "call void @rt_root_put(i32* %_a)") {
			t.Errorf("the module never roots the slot the unpacking bound a container into:\n%s", out)
		}
	})
}
