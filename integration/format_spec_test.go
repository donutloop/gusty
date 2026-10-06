package integration

// Whole-program coverage for an f-string's format spec and conversion (roadmap Gap R.186, ADR 0299).
// The unit tables in pkg/lang/format_spec_test.go pin the shared engine; these run real source
// through the CLI and check three things the parity harness structurally cannot:
//
//   1. the interpreted leg answers what CPython answers, line for line — it used to print the plain
//      number for every one of these, at exit 0, because the spec never reached the AST;
//   2. the compiled leg either answers or refuses with a sentence naming the spec, and NEVER emits
//      the unformatted number — a wrong answer at exit 0 is the failure this row exists to end;
//   3. no program in the family reaches exit 2. `f"{[1,2]}"` used to emit `printf(..., i32 @.lst1)`,
//      which llc rejects as an invalid module, and ADR 0166 forbids that exit class outright.

import (
	"strings"
	"testing"
)

// specFamily is one program per shape, each with the answer the reference gives.
var specFamily = []struct {
	src  string
	want string
}{
	{`print(f"{3.5:.2f}")`, "3.50"},
	{`print(f"{3.5:.0f}")`, "4"},
	{`print(f"{2.5:.0f}")`, "2"},
	{`print(f"{7:05d}")`, "00007"},
	{`print(f"{-4:05d}")`, "-0004"},
	{`print(f"{255:x}")`, "ff"},
	{`print(f"{255:X}")`, "FF"},
	{`print(f"{1234:,.2f}")`, "1,234.00"},
	{`print(f"{3.5:>6}|")`, "   3.5|"},
	{`print(f"{3.5:<6}|")`, "3.5   |"},
	{`print(f"{3.5:*^7}|")`, "**3.5**|"},
	{`print(f"{0.25:.2%}")`, "25.00%"},
	{`print(f"{3.14159:.3e}")`, "3.142e+00"},
	{`print(f"{2}")`, "2"},
	{`print(f"{2.0}")`, "2.0"},
	{`print(f"{7!r}")`, "7"},
	{`print(f"{3.5!r:>8}|")`, "     3.5|"},
	{`n = 7
print(f"{n:05d}")`, "00007"},
	{`n = 7
print(f"{n + 1:04d}")`, "0008"},
	{`print(f"pi={3.14159:.2f} e={2.71828:.2f}")`, "pi=3.14 e=2.72"},
}

// The interpreted leg is the one that used to be silently wrong, so it is pinned to the reference
// exactly — not "contains", not "has a digit".
func TestCLIInterpreterFormatsLikeTheReference(t *testing.T) {
	dir := t.TempDir()
	for _, c := range specFamily {
		src := writeSrc(t, dir, "spec.gy", c.src)
		got, code := cliRunMerged(t, "--interp", "--file", src)
		if code != 0 {
			t.Errorf("%s: interpreted leg exited %d: %s", c.src, code, got)
			continue
		}
		// Compared exactly, NOT trimmed: several of these answers ARE trailing spaces, and a
		// harness that trims cannot see missing padding -- the same trap ADR 0297 recorded for the
		// padding methods.
		if strings.TrimSuffix(got, "\n") != c.want {
			t.Errorf("%s: interpreted leg printed %q, reference answers %q", c.src, got, c.want)
		}
	}
}

// The compiled leg must never answer a formatted field with the UNFORMATTED number. It may render
// the right answer or refuse; both are legal, a silent plain value is not. This test would have
// failed on every row before the spec reached the AST.
func TestCLICompiledLegNeverAnswersAPlainNumberForASpec(t *testing.T) {
	dir := t.TempDir()
	for _, c := range specFamily {
		src := writeSrc(t, dir, "spec.gy", c.src)
		got, code := cliRunMerged(t, "--aot", "--file", src)
		switch code {
		case 0:
			if strings.TrimSuffix(got, "\n") != c.want {
				t.Errorf("%s: compiled leg printed %q, reference answers %q — a formatted field must "+
					"either answer the reference or refuse, never answer the plain value", c.src, got, c.want)
			}
		case 1:
			// A refusal is legal only if it names the spec, so a reader can act on it.
			if !strings.Contains(got, "format spec") && !strings.Contains(got, "f-string field") {
				t.Errorf("%s: compiled leg refused without naming the format: %s", c.src, got)
			}
		default:
			t.Errorf("%s: compiled leg exited %d (%s); exit 2 is a compiler bug and any other class "+
				"is not this program's to return", c.src, code, got)
		}
	}
}

// `f"{[1,2]}"` used to emit `printf(..., i32 @.lst1)` — a global list address where a heap handle
// belongs — and llc rejected the module, which is exit 2, the forbidden class. The interpreted leg
// answers the reference; the compiled leg refuses. Neither may reach exit 2.
func TestCLIContainerFieldHasNoExitTwo(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		`print(f"{[1,2]}")`,
		`print(f"{[1,2]!r}")`,
		`print(f"{{1:2}}")`,
		`xs = [1, 2]
print(f"{xs}")`,
	} {
		f := writeSrc(t, dir, "cont.gy", src)
		for _, backend := range []string{"--interp", "--aot"} {
			got, code := cliRunMerged(t, backend, "--file", f)
			if code == 2 {
				t.Errorf("%s %s: exit 2 (compiler bug) on %s: %s", backend, src, src, got)
			}
		}
	}
	got, code := cliRunMerged(t, "--interp", "--file", writeSrc(t, dir, "c2.gy", `print(f"{[1,2]}")`))
	if code != 0 || strings.TrimSpace(got) != "[1, 2]" {
		t.Errorf("interpreted container field = %q exit %d, reference answers [1, 2]", got, code)
	}
}

// A spec the language cannot honour, and a spec on a value that has no __format__, must raise or
// refuse with the reference's own sentence — not silently answer the plain value, which is exactly
// what every one of these did before.
func TestCLIUnhonourableSpecRaisesRatherThanAnswersPlainly(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		src    string
		wantIt string // the reference's own words, on the leg that runs it
	}{
		{`print(f"{[1,2]:>8}")`, "unsupported format string passed to list.__format__"},
		{"s = {1, 2}\nprint(f\"{s:>8}\")", "unsupported format string passed to set.__format__"},
	}
	for _, c := range cases {
		src := writeSrc(t, dir, "bad.gy", c.src)
		got, code := cliRunMerged(t, "--interp", "--file", src)
		if code != 3 {
			t.Errorf("%s: interpreted leg exited %d, the reference raises TypeError here; output %q",
				c.src, code, got)
			continue
		}
		if !strings.Contains(got, c.wantIt) {
			t.Errorf("%s: interpreted leg said %q, reference raises %q", c.src, got, c.wantIt)
		}
	}
}

// The two engines must not disagree in the dangerous direction: where the compiled leg ANSWERS, it
// answers what the interpreter answers. (Where it refuses, that is recorded debt to L12.8/L11.1,
// not a divergence to fix by making the interpreter refuse too — ADR 0298's rule, restated.)
func TestCLIEnginesAgreeWhereBothAnswer(t *testing.T) {
	dir := t.TempDir()
	for _, c := range specFamily {
		src := writeSrc(t, dir, "spec.gy", c.src)
		i, ic := cliRunMerged(t, "--interp", "--file", src)
		a, ac := cliRunMerged(t, "--aot", "--file", src)
		if ic != 0 {
			t.Fatalf("%s: interpreted leg exited %d: %s", c.src, ic, i)
		}
		if ac == 0 && strings.TrimSuffix(a, "\n") != strings.TrimSuffix(i, "\n") {
			t.Errorf("%s: engines disagree — interp %q aot %q", c.src, i, a)
		}
	}
}
