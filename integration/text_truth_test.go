// End-to-end cover for the text-truth and string-method-answer rows (roadmap Gap R.183, Gap R.184,
// ADR 0297). The CLI's exit code and stdout are the contract: `not` of a text answers a verdict, and a
// text-returning method prints its text rather than the intern table's position.
package integration

import (
	"strings"
	"testing"
)

func cliTextOut(t *testing.T, backend, src string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	f := writeSrc(t, dir, "text.gy", src)
	return cliRunMerged(t, backend, "--file", f)
}

// `not` and `if` are the same question about the same value; the compiled leg answered them
// differently, which is the whole defect.
func TestCLITextTruthAgreesWithTheReference(t *testing.T) {
	for _, src := range []string{
		`print(not "x")`,
		`print(not "")`,
		`print(not "0")`,
		`print(not None)`,
		`print(not 0)`,
		`print(not 3)`,
		`print(not [])`,
		`print(not [1])`,
		"if \"x\":\n    print(1)\nelse:\n    print(0)\n",
		"if \"\":\n    print(1)\nelse:\n    print(0)\n",
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		for _, backend := range []string{"--interp", "--aot"} {
			out, code := cliTextOut(t, backend, src)
			if code != 0 {
				t.Fatalf("%s %q: exit %d, the reference exits 0:\n%s", backend, src, code, out)
			}
			if got := strings.TrimSpace(out); got != want {
				t.Fatalf("%s %q: we printed %q, the reference prints %q", backend, src, got, want)
			}
		}
	}
}

// A text-returning string method answers TEXT; before this the print road knew three of them and nine
// printed their intern INDEX.
func TestCLIStringMethodsAnswerText(t *testing.T) {
	for _, src := range []string{
		`print("ab".zfill(5))`,
		`print("42".zfill(5))`,
		`print("abc".zfill(1))`,
		`print("abc".capitalize())`,
		`print("a b".title())`,
		`print("ab".swapcase())`,
		`print("abc".replace("b", "X"))`,
		`print("abc".removeprefix("a"))`,
		`print("abc".removesuffix("c"))`,
		`print("abc".count("b"))`,
		`print("abc".find("b"))`,
		`print("abc".startswith("a"))`,
		`print("-42".zfill(5))`,
		`print("+42".zfill(5))`,
		`print("-1".zfill(4))`,
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		for _, backend := range []string{"--interp", "--aot"} {
			out, code := cliTextOut(t, backend, src)
			if code != 0 {
				t.Fatalf("%s %q: exit %d, the reference exits 0:\n%s", backend, src, code, out)
			}
			if got := strings.TrimSpace(out); got != want {
				t.Fatalf("%s %q: we printed %q, the reference prints %q", backend, src, got, want)
			}
		}
	}
}

// The sign rule both engines had wrong TOGETHER, which is the class the parity matrix cannot see.
func TestCLIZfillPadsAfterTheSignOnBothEngines(t *testing.T) {
	for _, src := range []string{
		`print("-42".zfill(5))`,
		`print("+42".zfill(5))`,
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		oi, ci := cliTextOut(t, "--interp", src)
		oa, ca := cliTextOut(t, "--aot", src)
		if strings.TrimSpace(oi) != want || strings.TrimSpace(oa) != want {
			t.Fatalf("%s: interp %q / aot %q, reference %q — both engines padded the whole string and "+
				"agreed with each other instead (Gap R.184)", src, strings.TrimSpace(oi), strings.TrimSpace(oa), want)
		}
		if ci != 0 || ca != 0 {
			t.Fatalf("%s: exit %d/%d where the reference exits 0", src, ci, ca)
		}
	}
}
