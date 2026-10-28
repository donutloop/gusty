package integration

// A comparison chain at the CLI: checked like the comparisons it stands for, and still run like one.
//
// roadmap L12.1 / Gap R.53 · ADR 0450
//
// `gustyc --check` is the agent-facing door of the front end — the JSON answer to "is this program
// sound?" — and for a program whose condition is a chain it used to answer `ok` at exit 0 for names
// nothing binds, calls nothing defines, and arguments nobody passed:
//
//	gustyc --check 'y = 1 < zzz < 3'   →  {"ok":true}  exit 0
//	gustyc --check 'y = 1 < zzz'       →  undefined name "zzz"  exit 1
//
// The node had no arm in the analyzer, so the walk stopped at its door. Every row here is a PAIR — the
// same program with one operator removed — because "the checker says something about chains" is
// satisfiable by a checker that says the wrong thing; "the checker says the same thing about a chain and
// about the comparison it holds" is not.
//
// The second half is the reference leg, and it is the half that keeps the first honest: the opinion about
// a link no pair of values can answer is a WARNING, and the program under it still compiles, still runs,
// and still dies with CPython's own class and message — which is what these rows measure against a live
// `python3`, sentence for sentence. A checker that turned that opinion into a refusal would be refusing
// a program the reference runs, and would fail the rows below (ADR 0166).

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// chainCheckPairs is the CLI's half of the hole: each row's `plain` and `chain` are the same program
// with one comparison operator removed, so the diagnostics must not care which was written.
func TestTheCheckerLooksInsideAChainAtTheCLI(t *testing.T) {
	t.Parallel()
	bin := cliBin(t)
	dir := t.TempDir()
	rows := []struct {
		name  string
		plain string
		chain string
		want  string
	}{
		{
			name:  "a name nothing binds, in the middle",
			plain: "y = 1 < zzz\nprint(y)\n",
			chain: "y = 1 < zzz < 3\nprint(y)\n",
			want:  `undefined name "zzz"`,
		},
		{
			name:  "a name nothing binds, first",
			plain: "y = zzz < 2\nprint(y)\n",
			chain: "y = zzz < 2 < 3\nprint(y)\n",
			want:  `undefined name "zzz"`,
		},
		{
			name:  "a call to a name nothing defines",
			plain: "y = 1 < nofn(1)\nprint(y)\n",
			chain: "y = 1 < nofn(1) < 3\nprint(y)\n",
			want:  `undefined name "nofn"`,
		},
		{
			name:  "an argument the function will ask for",
			plain: "def f(x):\n    return x\n\ny = 1 < f()\nprint(y)\n",
			chain: "def f(x):\n    return x\n\ny = 1 < f() < 3\nprint(y)\n",
			want:  `function "f" expects 1 argument, got 0`,
		},
		{
			name:  "a name bound nowhere inside a body",
			plain: "def f():\n    return 1 < zzz\n\nprint(f())\n",
			chain: "def f():\n    return 1 < zzz < 3\n\nprint(f())\n",
			want:  `undefined name "zzz"`,
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			plainCode, plainOK, plainDiags := checkJSON(t, bin, r.plain)
			chainCode, chainOK, chainDiags := checkJSON(t, bin, r.chain)
			if plainCode != chainCode || plainOK != chainOK {
				t.Fatalf("removing one operator changed the answer: exit %d/ok %v vs exit %d/ok %v\n  single: %v\n  chain:  %v",
					plainCode, plainOK, chainCode, chainOK, plainDiags, chainDiags)
			}
			if len(plainDiags) != len(chainDiags) {
				t.Fatalf("a chain changed the diagnostic count: %v vs %v", plainDiags, chainDiags)
			}
			for i := range plainDiags {
				if plainDiags[i].Msg != chainDiags[i].Msg || plainDiags[i].Code != chainDiags[i].Code {
					t.Errorf("the same operand says one thing alone and another inside a chain: %+v vs %+v", plainDiags[i], chainDiags[i])
				}
			}
			if !strings.Contains(diagText(chainDiags), r.want) {
				t.Fatalf("the chain reported %v, want %q", chainDiags, r.want)
			}
			// The exit-code contract: a front-end refusal is 1, never 2 (ADR 0166), and it is the
			// answer for the chain exactly as it is for the comparison.
			if chainCode != 1 {
				t.Errorf("`--check` exited %d on a program whose name the reference cannot bind, want 1", chainCode)
			}
			// The reference leg: CPython runs the same source and dies too.
			if !cpythonDies(t, dir, r.chain) {
				t.Skip("no python3 available to cross-check")
			}
		})
	}
}

// TestAnUnanswerableLinkIsNamedAtTheCLIAndTheProgramStillRuns is the reference leg of the opinion: the
// sentence the checker warns with is the sentence the runtime raises, taken from a live CPython rather
// than from this file's expectation column.
func TestAnUnanswerableLinkIsNamedAtTheCLIAndTheProgramStillRuns(t *testing.T) {
	t.Parallel()
	bin := cliBin(t)
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		// the sentence the checker names, which must be the one CPython's traceback ends with
		sentence string
	}{
		{"an int against a text in a chain", "print(1 < \"a\" < 3)\n", "'<' not supported between instances of 'int' and 'str'"},
		{"an int against a text, one operator", "print(1 < \"a\")\n", "'<' not supported between instances of 'int' and 'str'"},
		{"the void against a number", "print(1 < None < 3)\n", "'<' not supported between instances of 'int' and 'NoneType'"},
		{"a bound text ordered against a number", "x = \"a\"\nprint(1 < x < 3)\n", "'<' not supported between instances of 'int' and 'str'"},
		{"the ordering the program wrote backwards", "x = \"a\"\nprint(x > 1 < 3)\n", "'>' not supported between instances of 'str' and 'int'"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			// 1. the reference: CPython raises, and its traceback names this sentence.
			stderr := cpythonRun(t, dir, r.src)
			if !strings.Contains(stderr, "TypeError") || !strings.Contains(stderr, r.sentence) {
				t.Fatalf("row is stale: CPython does not raise %q for %q (%s)", r.sentence, r.src, stderr)
			}
			// 2. the checker: named at warning level with the stable code, and `--check` stays green.
			code, okCheck, diags := checkJSON(t, bin, r.src)
			if code != 0 || !okCheck {
				t.Errorf("`--check` refused a program the reference RUNS: exit %d ok=%v %v", code, okCheck, diags)
			}
			var named bool
			for _, d := range diags {
				if d.Code == "type.compare.uncomparable" && strings.Contains(d.Msg, r.sentence) {
					named = true
					if d.Level != "warning" {
						t.Errorf("the opinion was said at level %q; an error would stop the build on a program the reference runs", d.Level)
					}
					if d.Suggestion == "" {
						t.Error("the diagnostic carries no suggestion for the agent reading it")
					}
				}
			}
			if !named {
				t.Errorf("`--check` did not name the link (%s %v)", r.sentence, diags)
			}
			// 3. the compiled leg: still runs, still raises the reference's own sentence, at the trap
			//    exit class — and never at exit 2, the contract's code for a compiler bug.
			p := writeSrc(t, dir, "chainlink", r.src)
			for _, engine := range cliEngines {
				out, exit := cliRunMerged(t, engine, "--file", p)
				if exit == 2 {
					t.Fatalf("exit 2 on the compiler's own module (ADR 0166): %s", out)
				}
				if exit == 0 {
					t.Fatalf("the program answered %q where the reference raises it", out)
				}
				if !strings.Contains(out, "TypeError") || !strings.Contains(out, r.sentence) {
					t.Errorf("%s says %q, want the reference's %q", engine, out, r.sentence)
				}
			}
		})
	}
}

// TestTheChainCarriesItsVerdictIntoTheASTJSON is the machine path: `--emit-ast` is what an agent reads to
// find out what the front end understood, and a chain node with no `inferred` field was a node the front
// end had never been told about.
func TestTheChainCarriesItsVerdictIntoTheASTJSON(t *testing.T) {
	t.Parallel()
	out, code := cliRunCode(t, "--emit-ast", "x = 5\ny = 1 < x < 10\nprint(y)\n")
	if code != 0 {
		t.Fatalf("--emit-ast exited %d: %s", code, out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("--emit-ast is not JSON: %v\n%s", err, out)
	}
	var chain map[string]any
	var walk func(any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if _, hasOps := n["ops"]; hasOps {
				if _, hasOperands := n["operands"]; hasOperands {
					chain = n
				}
			}
			for _, c := range n {
				walk(c)
			}
		case []any:
			for _, c := range v.([]any) {
				walk(c)
			}
		}
	}
	walk(doc)
	if chain == nil {
		t.Fatalf("no chain node in the AST document: %s", out)
	}
	if got, _ := chain["inferred"].(string); got != "bool" {
		t.Errorf("the chain node carries inferred=%q, want \"bool\": %v", got, chain)
	}
}

// TestCompareChainsProgramIsTheRoadmapsFirstTwoLines runs the conformance program the row's definition of
// done names, and asserts the two lines that row names in it: `out`, then `True`.
func TestCompareChainsProgramIsTheRoadmapsFirstTwoLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := readProgramSrc("compare_chains")
	want, ok := cpythonPlainOut(t, dir, src)
	if !ok {
		t.Skip("no python3 available to cross-check")
	}
	lines := strings.Split(strings.TrimRight(want, "\n"), "\n")
	if len(lines) < 2 || lines[0] != "out" || lines[1] != "True" {
		t.Fatalf("the row's own definition of done is `out` then `True`; CPython prints %q", want)
	}
	p := writeSrc(t, dir, "compare_chains", src)
	for _, engine := range cliEngines {
		out, code := cliRunMerged(t, engine, "--file", p)
		if code != 0 {
			t.Fatalf("%s exited %d on a program the reference prints:\n%s", engine, code, out)
		}
		if out != want {
			t.Errorf("%s printed:\n%s\nwant:\n%s", engine, out, want)
		}
	}
}

// TestTheCheckerStaysQuietAboutTheChainsThatAnswer is the CLI-side false-positive guard: a warning on a
// chain that answers is a defect report about a program that is not defective.
func TestTheCheckerStaysQuietAboutTheChainsThatAnswer(t *testing.T) {
	t.Parallel()
	bin := cliBin(t)
	src := "x = 5\nys = [1, 2]\nprint(1 < x < 10)\nprint(\"a\" < \"b\" < \"c\")\nprint(0 < ys[1] < 3)\nprint(1 == 1 is True)\ndef f(v):\n    return 0 < v < 10\n\nprint(f(3))\n"
	_, ok, diags := checkJSON(t, bin, src)
	for _, d := range diags {
		if d.Code == "type.compare.uncomparable" {
			t.Errorf("the checker named a link it cannot name: %+v", d)
		}
	}
	if !ok {
		t.Errorf("`--check` refused chains that answer: %v", diags)
	}
}

// diagText flattens diagnostics for a substring assertion.
func diagText(diags []checkDiag) string {
	var b strings.Builder
	for _, d := range diags {
		b.WriteString(d.Level)
		b.WriteString(": ")
		b.WriteString(d.Msg)
		b.WriteString("\n")
	}
	return b.String()
}

// cpythonRun runs the reference on a source and hands back its combined output. `oracleCommand`, not a
// private exec.Command: the reference leg is only evidence if the same source gets the same answer twice
// (oracle_spawn_test.go), and it is the one place that knows whether a python3 exists at all.
func cpythonRun(t *testing.T, dir, src string) string {
	t.Helper()
	p := writeSrc(t, dir, "ref.py", src)
	cmd := oracleCommand(t, p)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// cpythonDies reports whether the reference exits non-zero on a source — the answer the pair rows above
// ask for, measured rather than assumed.
func cpythonDies(t *testing.T, dir, src string) bool {
	t.Helper()
	p := writeSrc(t, dir, "dies.py", src)
	cmd := oracleCommand(t, p)
	_, err := cmd.CombinedOutput()
	if err == nil {
		return true
	}
	_, ok := err.(*exec.ExitError)
	return ok
}
