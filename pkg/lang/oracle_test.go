package lang

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// The oracle leg (roadmap L11.9, ADR 0186; two legs since ADR 0302 retired the AST interpreter).
//
// The classification functions here decide what a row of the conformance matrix *means*, so they
// are tested the way the rest of the toolchain is: with the cases that must fail, not only the ones
// that pass. A harness that can only report success is a harness that reports success for the wrong
// answer — which is precisely the bug L11.9 was written to catch (print(True) == "1" sat in a green
// build for 100+ ADRs, because the only other thing it could compare against agreed).
//
// With one backend the oracle is not a tiebreaker, it is the whole verdict: a row is `match` only
// when the compiled program prints what CPython prints.

func reportFor(aotOK bool, aot, aotErr string, pyOK bool, py, pyErr string, rules []string) OracleReport {
	return BuildOracleReport(aotOK, aot, aotErr, rules, pyOK, py, pyErr)
}

func TestOracleReportMatchWhenTheCompiledLegPrintsPythonsAnswer(t *testing.T) {
	t.Parallel()
	rep := reportFor(true, "True\n", "", true, "True\n", "", nil)
	if rep.Status != OracleMatch {
		t.Fatalf("status = %q, want %q", rep.Status, OracleMatch)
	}
	for _, want := range []string{"aot", "python"} {
		if _, ok := rep.Leg(want); !ok {
			t.Fatalf("missing leg %s", want)
		}
	}
	l, _ := rep.Leg("aot")
	if !l.Matches {
		t.Errorf("the compiled leg printed the oracle's answer and does not say so: %+v", l)
	}
	if len(rep.Notes) != 0 {
		t.Errorf("a matching row should carry no notes, got %v", rep.Notes)
	}
}

// TestOracleReportTheOldParityTrapIsGone records why this item exists. The bug it was written for
// was `print(True)` answering "1" on both legs for 100+ ADRs: two implementations agreeing is
// not evidence, and the matrix only saw the truth because a third leg disagreed with both.
//
// One backend remains, so the trap cannot recur in the same shape — but its successor can: an
// answer the compiler agrees with itself about. The oracle leg is now the *only* verdict a row can
// carry, which is why these classification functions matter more than they did, not less.
func TestOracleReportAgreeingWithItselfIsDebt(t *testing.T) {
	t.Parallel()
	rep := reportFor(true, "1\n", "", true, "True\n", "", nil)
	if rep.Status != OracleDebt {
		t.Fatalf("status = %q, want %q — the compiler agreeing with itself is not a verdict", rep.Status, OracleDebt)
	}
	if rep.Legs[0].Matches {
		t.Errorf("the compiled leg disagrees with the oracle and may not report a match: %+v", rep.Legs[0])
	}
	if len(rep.Notes) != 1 {
		t.Errorf("want one note for the disagreeing leg, got %v", rep.Notes)
	}
}

func TestOracleReportRefusalIsDebtNotPass(t *testing.T) {
	t.Parallel()
	// The compiled backend refuses the program while CPython runs it.
	rep := reportFor(false, "", "codegen: unsupported expression", true, "[1, 'a']\n", "", nil)
	if rep.Status != OracleDebt {
		t.Fatalf("status = %q, want %q: a refusal is a divergence, not a skip", rep.Status, OracleDebt)
	}
	if rep.Legs[0].OK || rep.Legs[0].Matches {
		t.Errorf("a leg that failed may not be reported as ok or matching: %+v", rep.Legs[0])
	}
	if !strings.Contains(strings.Join(rep.Notes, "; "), "compiled leg failed") {
		t.Errorf("notes should say which leg failed: %v", rep.Notes)
	}
}

func TestOracleReportALegThatDiedViolentlyIsStillNotAMatch(t *testing.T) {
	t.Parallel()
	// A Go panic inside the compiler is the worst thing a leg can do, and the report has to
	// treat it as a failed leg rather than let it look like a verdict. This is the shape
	// `print([1, 2, 3][-1])` used to produce before ADR 0210 gave the constant folder a bounds
	// test; the fixture at the CLI level had to change when the bug went away, so the contract
	// is pinned here, where a violent leg can be described exactly.
	panicText := "panic: runtime error: index out of range [-1]\n\ngoroutine 1 [running]:"
	rep := reportFor(false, "", panicText, true, "3\n", "", nil)
	if rep.Status == OracleMatch {
		t.Fatalf("a program whose compiled leg panicked may not be reported as a match: %+v", rep)
	}
	if rep.Legs[0].OK || rep.Legs[0].Matches {
		t.Errorf("a panicked leg may not be ok or matching: %+v", rep.Legs[0])
	}
	if !strings.Contains(rep.Legs[0].Error, "panic:") {
		t.Errorf("the panic text has to survive into the report so it is readable: %q", rep.Legs[0].Error)
	}
}

func TestOracleReportNotApplicableOnlyWhenTheOracleCannotJudge(t *testing.T) {
	t.Parallel()
	rep := reportFor(true, "3\n", "", false, "", "Traceback: SyntaxError", nil)
	if rep.Status != OracleNA {
		t.Fatalf("status = %q, want %q", rep.Status, OracleNA)
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "CPython leg did not complete") {
		t.Errorf("an NA row must say why there is no verdict: %v", rep.Notes)
	}
	// A compiler panic is recorded like any other leg failure: the report survives it.
	rep = reportFor(false, "", "compiler panic: index out of range [-1]", true, "3\n", "", nil)
	if rep.Status != OracleDebt {
		t.Errorf("a compiler panic should be debt, got %q", rep.Status)
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "compiler panic") {
		t.Errorf("the panic must be visible in the notes: %v", rep.Notes)
	}
}

func TestOracleSetOrderRuleNormalizesSetsAndOnlySets(t *testing.T) {
	t.Parallel()
	rules := DefaultOracleRules()
	// A set rendering: order is unspecified in CPython, so both sides are sorted.
	if got := OracleNormalize("{3, 1, 2}\n", rules); got != "{1, 2, 3}\n" {
		t.Errorf("set line = %q, want {1, 2, 3}", got)
	}
	if OracleNormalize("{1, 2, 3}\n", rules) != OracleNormalize("{3, 2, 1}\n", rules) {
		t.Errorf("two orders of the same set should compare equal")
	}
	// A dict rendering keeps its order: dict order is insertion order in both languages.
	if OracleNormalize("{'b': 1, 'a': 2}\n", rules) == OracleNormalize("{'a': 2, 'b': 1}\n", rules) {
		t.Errorf("a dict's order is observable in gusty and must not be normalised away")
	}
	// A set whose element contains a colon is still a set — the ':' is inside a string.
	if got := OracleNormalize("{'b:1', 'a'}\n", rules); got != "{'a', 'b:1'}\n" {
		t.Errorf("set with a colon inside a string = %q, want the elements sorted", got)
	}
	// A line that is not a bare container rendering is left alone.
	const nested = "  set of = {3, 1}\n"
	if OracleNormalize(nested, rules) != nested {
		t.Errorf("only a bare rendering is a container line: %q", OracleNormalize(nested, rules))
	}
	// Nested containers split on top-level commas only, and a nested set is sorted at
	// every level — while a list inside the line keeps its order, because that order
	// is observable in gusty.
	if got := OracleNormalize("{1, {3, 2}}\n", rules); got != "{1, {2, 3}}\n" {
		t.Errorf("nested set = %q, want the inner set sorted too", got)
	}
	if got := OracleNormalize("{1, [3, 2]}\n", rules); got != "{1, [3, 2]}\n" {
		t.Errorf("a list element must keep its order: %q", got)
	}
	// The trailing newline of the last line is preserved byte for byte.
	if got := OracleNormalize("{2, 1}", rules); got != "{1, 2}" {
		t.Errorf("output without a trailing newline should keep it that way: %q", got)
	}
}

func TestOracleCheckRequiresADeclaredState(t *testing.T) {
	t.Parallel()
	c := ConformanceCase{ID: "programs/x", Name: "x.gy"} // no Oracle declared
	r := &ConformanceResult{}
	drift := c.OracleCheck(r)
	if len(drift) == 0 || !strings.Contains(drift[0], "classify") {
		t.Fatalf("an undeclared case must be reported, got %v", drift)
	}
	c.Oracle = "bogus"
	if drift := c.OracleCheck(&ConformanceResult{}); len(drift) == 0 {
		t.Fatalf("an unknown declared state must be reported")
	}
}

func TestOracleCheckDebtNeedsReasonOwnerAndPin(t *testing.T) {
	t.Parallel()
	c := ConformanceCase{ID: "programs/x", Name: "x.gy", Oracle: OracleDebt}
	drift := c.OracleCheck(&ConformanceResult{})
	joined := strings.Join(drift, " | ")
	for _, want := range []string{"reason", "roadmap reference", "pin"} {
		if !strings.Contains(joined, want) {
			t.Errorf("debt without a %s should be drift: %v", want, drift)
		}
	}
	c.Reason, c.Ref = "bools print as 1", "roadmap L11.2"
	c.Pins = []OraclePin{{Backend: "aot", Stdout: "1\n"}}
	// The compiled leg prints "1" and CPython prints "True": still debt, and the pin matches, so
	// the row is honest and reports nothing.
	r := &ConformanceResult{AOTOK: true, AOTOut: "1\n", PythonOK: true, PythonOut: "True\n"}
	if drift := c.OracleCheck(r); len(drift) != 0 {
		t.Errorf("an accurately pinned debt row should report no drift, got %v", drift)
	}
	if r.AOTMatchesPython {
		t.Errorf("the backend does not print Python's answer, so it may not claim a match")
	}
}

// TestOracleCheckFailsWhenADebtIsPaid is the ratchet: an unrecorded fix is as much a
// failure as a new divergence, because a ledger nobody maintains is a fiction.
func TestOracleCheckFailsWhenADebtIsPaid(t *testing.T) {
	t.Parallel()
	c := ConformanceCase{
		ID: "programs/x", Name: "x.gy", Oracle: OracleDebt, Reason: "r", Ref: "L11.2",
		Pins: []OraclePin{{Backend: "aot", Stdout: "1\n"}},
	}
	r := &ConformanceResult{
		AOTOK: true, AOTOut: "True\n",
		PythonOK: true, PythonOut: "True\n",
	}
	drift := c.OracleCheck(r)
	joined := strings.Join(drift, " | ")
	if !strings.Contains(joined, "debt is paid") {
		t.Fatalf("a closed debt must fail with 'debt is paid', got %v", drift)
	}
	for _, want := range []string{"pin says the aot leg prints"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the stale pins should also be reported (%s): %v", want, drift)
		}
	}
}

func TestOracleCheckPinsCatchEveryKindOfLegDrift(t *testing.T) {
	t.Parallel()
	base := func(pins ...OraclePin) ConformanceCase {
		return ConformanceCase{ID: "p", Name: "p.gy", Oracle: OracleDebt, Reason: "r", Ref: "L", Pins: pins}
	}
	cases := []struct {
		name string
		c    ConformanceCase
		r    ConformanceResult
		want string
	}{
		{"stdout moved",
			base(OraclePin{Backend: "aot", Stdout: "1\n"}),
			ConformanceResult{AOTOK: true, AOTOut: "one\n", PythonOK: true, PythonOut: "True\n"},
			"pin says the aot leg prints"},
		{"a leg that used to fail now runs",
			base(OraclePin{Backend: "aot", Missing: true}),
			ConformanceResult{AOTOK: true, AOTOut: "1\n", PythonOK: true, PythonOut: "True\n"},
			"the refusal is gone"},
		{"the failure changed shape",
			base(OraclePin{Backend: "aot", Missing: true, Err: "compiler panic"}),
			ConformanceResult{AOTErr: "codegen: unsupported expression", PythonOK: true, PythonOut: "True\n"},
			"pin says the aot leg fails with"},
	}
	for _, tc := range cases {
		drift := tc.c.OracleCheck(&tc.r)
		if !strings.Contains(strings.Join(drift, " | "), tc.want) {
			t.Errorf("%s: want drift containing %q, got %v", tc.name, tc.want, drift)
		}
	}
}

func TestOracleCheckAppliesTheDeclaredComparisonRules(t *testing.T) {
	t.Parallel()
	c := ConformanceCase{
		ID: "p", Name: "p.gy", Oracle: OracleMatch, Rules: []string{RuleSetOrder},
	}
	r := &ConformanceResult{
		AOTOK: true, AOTOut: "{1, 2, 3}\n",
		PythonOK: true, PythonOut: "{2, 3, 1}\n",
	}
	if drift := c.OracleCheck(r); len(drift) != 0 {
		t.Fatalf("three orders of one set are one answer: %v", drift)
	}
	if r.Oracle != OracleMatch {
		t.Errorf("status = %q, want match", r.Oracle)
	}
	if !r.AOTMatchesPython {
		t.Errorf("the compiled leg should match under the rule")
	}
	// The rule list the row was judged under is echoed into the result, so a reader
	// (or a stub check that drops the rule) can see exactly what was normalised.
	if !strings.Contains(strings.Join(r.OracleRules, ","), RuleSetOrder) {
		t.Errorf("the artifact should name the rules that were applied: %v", r.OracleRules)
	}
	extra := &ConformanceResult{}
	c.Rules = append(c.Rules, "not-a-real-rule")
	c.OracleCheck(extra)
	if !strings.Contains(strings.Join(extra.OracleRules, ","), "not-a-real-rule") {
		t.Errorf("a declared-but-unknown rule must still be visible, not silently dropped: %v", extra.OracleRules)
	}
}

func TestOracleVerdictVocabularyIsStable(t *testing.T) {
	t.Parallel()
	// Agents branch on these strings in the matrix artifact and in --oracle JSON;
	// renaming one is a breaking change to the CLI's ABI.
	for got, want := range map[string]string{OracleMatch: "match", OracleDebt: "debt", OracleNA: "not_applicable"} {
		if got != want {
			t.Errorf("oracle constant = %q, want %q", got, want)
		}
	}
	if RuleSetOrder != "set-order" {
		t.Errorf("rule name = %q", RuleSetOrder)
	}
}

func TestSchemaDescribesTheOracleOutputs(t *testing.T) {
	t.Parallel()
	var doc map[string]any
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("--schema is not valid JSON: %v", err)
	}
	defs, _ := doc["definitions"].(map[string]any)
	for _, name := range []string{"conformanceRow", "oracleReport", "valueTag"} {
		if _, ok := defs[name]; !ok {
			t.Errorf("schema is missing definitions.%s — the oracle output would be undocumented", name)
		}
	}
}

func TestPythonRunRunsTheOracle(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath(PythonBinary()); err != nil {
		t.Skipf("no CPython available for the oracle leg: %v", err)
	}
	out, _, err := PythonRun("print(1 + 1)\n")
	if err != nil {
		t.Fatalf("oracle run: %v", err)
	}
	if out != "2\n" {
		t.Errorf("oracle stdout = %q, want 2\\n", out)
	}
	out, stderr, err := PythonRun("print(nope)\n")
	if err == nil {
		t.Fatalf("a NameError should be reported as a failed leg")
	}
	if out != "" {
		t.Errorf("stdout should be empty, got %q", out)
	}
	if !strings.Contains(stderr, "NameError") {
		t.Errorf("stderr should carry the traceback, got %q", stderr)
	}
}

func TestPythonBinaryIsOverridable(t *testing.T) {
	t.Parallel()
	// The oracle interpreter is tunable (`GUSTY_PYTHON`), and the interesting half of that is a set value
	// that must win over the default — and an unset or empty one that must not. Asked as a table over the
	// environment the resolver reads, so the case does not take the process's environment away from the
	// cases running beside it (ADR 0343; the resolver is pythonBinaryFrom, with PythonBinary the thin
	// process-wide entry on top of it).
	lookup := func(v string, present bool) func(string) (string, bool) {
		return func(string) (string, bool) { return v, present }
	}
	for _, tc := range []struct {
		name    string
		value   string
		present bool
		want    string
	}{
		{"unset falls back to python3", "", false, "python3"},
		{"set-but-empty falls back to python3", "", true, "python3"},
		{"a named interpreter is used", "/opt/py3.13/bin/python3", true, "/opt/py3.13/bin/python3"},
		{"a bare name is used as named", "python3.12", true, "python3.12"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pythonBinaryFrom(lookup(tc.value, tc.present)); got != tc.want {
				t.Errorf("GUSTY_PYTHON=%q (present=%v): got %q, want %q", tc.value, tc.present, got, tc.want)
			}
		})
	}
	// And the chain end to end: a process started with the variable actually resolves it through
	// PythonBinary, which is what the oracle legs then exec.
	if got := envProbe(t, []string{"GUSTY_PYTHON=/opt/py3.13/bin/python3"}, "python"); got != "python=/opt/py3.13/bin/python3" {
		t.Errorf("GUSTY_PYTHON did not reach the interpreter a process resolves: %q", got)
	}
	if got := envProbe(t, nil, "python"); got != "python=python3" {
		t.Errorf("with nothing set a process resolved %q, want the documented default python3", got)
	}
}

// The pinned oracle (ADR 0193). The corpus is validated against a *named* CPython,
// and a row declared `match` means "matches that oracle": on Ubuntu 22.04's Python
// 3.10, `type Count = int` is a SyntaxError on line 1, so programs/typealias drifted
// from match to not_applicable and the note pointed at the compiler instead of at the
// runner. These tests hold the pieces that make that read correctly.

func TestOracleVersionParsesTheVersionBanner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		banner       string
		major, minor int
		ok           bool
	}{
		{"Python 3.12.3", 3, 12, true},
		{"Python 3.10.12", 3, 10, true},
		{"Python 3.13", 3, 13, true},
		{"Python 2.7.18", 2, 7, true},
		// Unidentifiable text is "unknown", never "too old": a machine whose oracle
		// cannot be read must not be told it is unsupported.
		{"no version here", 0, 0, false},
		{"", 0, 0, false},
	} {
		major, minor, ok := OracleVersion(tc.banner)
		if major != tc.major || minor != tc.minor || ok != tc.ok {
			t.Fatalf("OracleVersion(%q) = %d.%d ok=%v, want %d.%d ok=%v",
				tc.banner, major, minor, ok, tc.major, tc.minor, tc.ok)
		}
	}
}

func TestOracleTooOldIsComparedAgainstThePin(t *testing.T) {
	t.Parallel()
	if OracleVersionTooOld("Python 3.12.3") {
		t.Fatal("the pinned oracle must not be too old for itself")
	}
	if !OracleVersionTooOld("Python 3.10.12") {
		t.Fatal("3.10 is older than the 3.12 pin — that is the CI case that produced the false drift")
	}
	if OracleVersionTooOld("Python 3.13.0") {
		t.Fatal("a newer oracle is supported")
	}
	if OracleVersionTooOld("something else") {
		t.Fatal("an unidentifiable banner is not evidence of an old oracle")
	}
}

func TestOracleTooOldHintNamesTheRemedy(t *testing.T) {
	t.Parallel()
	hint := OracleTooOldHint("  File \"prog.py\", line 1\n    type Count = int\n    ^^^^^\nSyntaxError: invalid syntax")
	for _, want := range []string{"SyntaxError", "3.12", "GUSTY_PYTHON"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint must mention %q to be actionable: %q", want, hint)
		}
	}
	// A runtime error is not a version problem, and must not be answered with one.
	if got := OracleTooOldHint("ZeroDivisionError: division by zero"); got != "" {
		t.Fatalf("a genuine program error should not be blamed on the oracle: %q", got)
	}
}

func TestBuildOracleReportKeepsAnOldOracleDiagnosingItself(t *testing.T) {
	t.Parallel()
	// The python leg died on a SyntaxError: the row is not_applicable (no oracle
	// answer exists), and the note must say the oracle may be the problem rather than
	// leaving a reader to conclude the compiler regressed.
	rep := BuildOracleReport(true, "42\n", "", nil, false, "", "line 1: SyntaxError: invalid syntax")
	if rep.Status != OracleNA {
		t.Fatalf("status = %s, want %s", rep.Status, OracleNA)
	}
	joined := strings.Join(rep.Notes, "\n")
	if !strings.Contains(joined, "SyntaxError") || !strings.Contains(joined, OracleMinPython) {
		t.Fatalf("notes should name the failure and the pin:\n%s", joined)
	}
}

// TestOracleInstanceIdentityRuleComparesClassesNotAddresses pins the second default comparison rule.
// The address is an allocation fact, but the module-qualified class is the claim Gap R.209 makes; the
// rule must normalize one and preserve the other, and it must do that only where the reference's identity
// form actually appears.
func TestOracleInstanceIdentityRuleComparesClassesNotAddresses(t *testing.T) {
	t.Parallel()
	rules := DefaultOracleRules()
	left := "<__main__.C object at 0x7fff1234>\n[<__main__.C object at 0x1>, 'x']\n"
	right := "<__main__.C object at 0x99>\n[<__main__.C object at 0x22>, 'x']\n"
	if OracleNormalize(left, rules) != OracleNormalize(right, rules) {
		t.Errorf("two identities of one class must compare equal:\n%s\n%s",
			OracleNormalize(left, rules), OracleNormalize(right, rules))
	}
	if OracleNormalize("<__main__.C object at 0x1>\n", rules) == OracleNormalize("<__main__.D object at 0x1>\n", rules) {
		t.Errorf("two different classes must not normalize to the same identity")
	}
	if got := OracleNormalize("0xFF\n", rules); got != "0xFF\n" {
		t.Errorf("an ordinary hex integer is not an object identity: %q", got)
	}
}
