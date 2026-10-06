// End-to-end cover for the text-as-iterable row (roadmap Gap R.185, ADR 0298). The CLI's exit code is
// the contract here in a specific way: the compiled leg is ALLOWED to refuse (exit 1) and is NOT allowed
// to answer wrongly at exit 0, so both cases are checked and neither is pinned as a limitation.
package integration

import (
	"strings"
	"testing"
)

func cliIterOut(t *testing.T, backend, src string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	f := writeSrc(t, dir, "iter.gy", src)
	return cliRunMerged(t, backend, "--file", f)
}

// The interpreter must answer these the way the reference does — they are exit-0 wrong numbers, not
// refusals, which is the class an agent cannot distinguish from success.
func TestCLIInterpreterIteratesATextLikeTheReference(t *testing.T) {
	for _, src := range []string{
		`print([c for c in "abc"])`,
		`print([c for c in "abc" if c != "b"])`,
		`print([c + c for c in "ab"])`,
		`print([c for c in ""])`,
		`print(len([c for c in "abcd"]))`,
		`print(sorted([c for c in "cba"]))`,
		`print(max("abc"))`,
		`print(min("abc"))`,
		`print(max([1, 2, 3]))`,
		`print(min([1, 2, 3]))`,
		`print(max(3, 5))`,
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		out, code := cliIterOut(t, "--interp", src)
		if code != 0 {
			t.Fatalf("--interp %q: exit %d, the reference exits 0:\n%s", src, code, out)
		}
		if got := strings.TrimSpace(out); got != want {
			t.Fatalf("--interp %q: we printed %q, the reference prints %q", src, got, want)
		}
	}
}

// The compiled leg may refuse (exit 1) but must never answer wrongly at exit 0. A refusal here is a
// road an earlier cycle has not lifted, so it is reported rather than pinned.
func TestCLICompiledLegNeverInventsATextIteration(t *testing.T) {
	for _, src := range []string{
		`print([c for c in "abc"])`,
		`print(max("abc"))`,
		`print(min("abc"))`,
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		out, code := cliIterOut(t, "--aot", src)
		switch {
		case code == 2:
			t.Fatalf("--aot %q spent the contract's exit 2 (compiler bug):\n%s", src, out)
		case code == 1:
			t.Logf("--aot %q still refuses (exit 1): %s — the interpreter answers %q; promote this row "+
				"into the table above when the road lifts", src, strings.TrimSpace(out), want)
		case code != 0:
			t.Fatalf("--aot %q: unexpected exit %d:\n%s", src, code, out)
		default:
			if got := strings.TrimSpace(out); got != want {
				t.Fatalf("--aot %q: we printed %q, the reference prints %q", src, got, want)
			}
		}
	}
}

// The three roads that ask "iterate this text" must give the same characters.
func TestCLIForAndComprehensionAgreeOverAText(t *testing.T) {
	forLoop := "for c in \"abc\":\n    print(c)\n"
	comp := "print([c for c in \"abc\"])\n"
	forWant, ok := cpythonPlainOut(t, t.TempDir(), forLoop)
	if !ok {
		t.Skip("no reference on this host")
	}
	compWant, ok := cpythonPlainOut(t, t.TempDir(), comp)
	if !ok {
		t.Skip("no reference on this host")
	}
	if strings.TrimSpace(compWant) != "['a', 'b', 'c']" {
		t.Fatalf("the reference itself did not answer the comprehension as expected: %q", compWant)
	}
	oi, ci := cliIterOut(t, "--interp", forLoop)
	if strings.TrimSpace(oi) != strings.TrimSpace(forWant) {
		t.Fatalf("`for` over a text: %q, reference %q", strings.TrimSpace(oi), strings.TrimSpace(forWant))
	}
	if ci != 0 {
		t.Fatalf("`for` over a text: exit %d", ci)
	}
	got := strings.TrimSpace(mustCLI(t, "--interp", comp))
	flat := strings.NewReplacer("[", "", "]", "", "'", "", " ", "", ",", "").Replace(got)
	if flat != "abc" {
		t.Fatalf("the comprehension yielded %q where `for` yields %q", got, strings.TrimSpace(forWant))
	}
}

func mustCLI(t *testing.T, backend, src string) string {
	t.Helper()
	out, code := cliIterOut(t, backend, src)
	if code != 0 {
		t.Fatalf("%s %q: exit %d:\n%s", backend, src, code, out)
	}
	return out
}
