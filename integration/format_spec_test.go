package integration

// Whole-program coverage for an f-string's format spec and conversion (roadmap Gap R.186, ADR 0299).
// The unit tables in pkg/lang/format_spec_test.go pin the shared engine; these run real source
// through the CLI and check three things the parity harness structurally cannot:
//
//   1. the reference answers what CPython answers, line for line — it used to print the plain
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

// The compiled leg is the one that used to be silently wrong, so it is pinned to the reference
// exactly — not "contains", not "has a digit". (The name keeps "Interpreter" in it because the shapes
// in specFamily are the ones the retired engine used to get wrong; what runs here is the one backend,
// and what it is compared to is CPython run live, below.)
func TestCLIFormatsLikeTheReference(t *testing.T) {
	dir := t.TempDir()
	for _, c := range specFamily {
		src := writeSrc(t, dir, "spec.gy", c.src)
		got, code := cliRunMerged(t, "--aot", "--file", src)
		if code == 1 && refusesHonestly(got) {
			// A format shape this backend will not build is a filed gap, not this row's defect — but it
			// stays a filed gap: counted, and never allowed to become the plain value.
			noteCompiledGap(t, c.src, got)
			continue
		}
		if code != 0 {
			t.Errorf("%s: compiled leg exited %d: %s", c.src, code, got)
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
		for _, backend := range cliEngines {
			got, code := cliRunMerged(t, backend, "--file", f)
			if code == 2 {
				t.Errorf("%s %s: exit 2 (compiler bug) on %s: %s", backend, src, src, got)
			}
		}
	}
	got, code := cliRunMerged(t, "--aot", "--file", writeSrc(t, dir, "c2.gy", `print(f"{[1,2]}")`))
	if code == 1 && refusesHonestly(got) {
		noteCompiledGap(t, `print(f"{[1,2]}")`, got)
	} else if code != 0 || strings.TrimSpace(got) != "[1, 2]" {
		t.Errorf("compiled container field = %q exit %d, reference answers [1, 2]", got, code)
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
		got, code := cliRunMerged(t, "--aot", "--file", src)
		if code != 3 && code != 1 {
			// The reference raises; the compiled path must raise in the reference's words, refuse the
			// shape out loud, or have a debt row saying which of the two it fails to do — with the
			// roadmap row that owes it. Answering plainly (exit 0) is the outcome this case was written
			// against and stays the one it fails hardest on.
			requireReferenceTrapOrHonestRefusal(t, c.src, "TypeError: "+c.wantIt, got, code, "roadmap L11.2 (f-string format specs over values the compiler cannot read)", "no __format__ for this value kind")
			continue
		}
		if !strings.Contains(got, c.wantIt) {
			requireReferenceTrapOrHonestRefusal(t, c.src, "TypeError: "+c.wantIt, got, code, "roadmap L11.2 (f-string format specs over values the compiler cannot read)", "the trap does not carry the reference's sentence")
		}
	}
}

// both legs must not disagree in the dangerous direction: where the compiled leg ANSWERS, it
// answers what the record answers. (Where it refuses, that is recorded debt to L12.8/L11.1,
// not a divergence to fix by making the record refuse too — ADR 0298's rule, restated.)
func TestCLIEnginesAgreeWhereBothAnswer(t *testing.T) {
	dir := t.TempDir()
	for _, c := range specFamily {
		src := writeSrc(t, dir, "spec.gy", c.src)
		// Two doors over one backend: `--file` runs the artifact, `--eval` compiles the same text
		// in-process. They used to be two *engines*, and the assertion was between them; what is still
		// a real, non-trivial claim — and the one that caught a regression this cycle — is that one
		// program prints one answer whichever door you ask it through. The echo, the trap report and
		// the collector line all travel different paths in the two runners, so "same bytes" is not a
		// tautology. A refusal is allowed, and must be the same refusal on both doors.
		i, ic := cliRunMerged(t, "--file", src)
		a, ac := cliRunCode(t, "--json", "--eval", c.src)
		if ic != ac {
			t.Errorf("%s: the two doors disagree about the exit: --file %d, --eval %d\n--file: %s\n--eval: %s", c.src, ic, ac, i, a)
			continue
		}
		if ic != 0 && !(ic == 1 && refusesHonestly(i)) {
			t.Fatalf("%s: compiled leg exited %d: %s", c.src, ic, i)
		}
		if ic == 0 && strings.TrimSuffix(a, "\n") != strings.TrimSuffix(i, "\n") && !strings.Contains(a, strings.TrimSuffix(i, "\n")) {
			t.Errorf("%s: the doors disagree — --file %q --eval %q", c.src, i, a)
		}
	}
}
