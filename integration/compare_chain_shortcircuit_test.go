package integration

// A comparison chain stops where the reference stops — measured against the reference, at the CLI.
//
// roadmap Gap R.520 · ADR 0451
//
// The three programs below were each a different kind of wrong on the parent, and two of them were wrong at
// an exit code that means "the program ran":
//
//	x = None / print(1 == x < 3)      CPython: False   gustyc: TypeError … 'NoneType' and 'int'   exit 3
//	x = 5    / print(x > 9 < 1 // 0)  CPython: False   gustyc: ZeroDivisionError                   exit 3
//	print(3 < 2 < boom())             CPython: False   gustyc: "called" then False
//
// The third is the one with no failing assertion available to a test that only reads the verdict: the chain
// answered True/False correctly while performing a call the reference never made. So the rows here count the
// calls, and the expectations come from `python3` inside each case rather than from this file's table — a row
// that CPython would not sign is stale, not green.

import (
	"strings"
	"testing"
)

func TestAChainStopsAtTheLinkTheVerdictRejectedAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{
			"an unreached ordering that cannot answer",
			"x = None\nprint(1 == x < 3)\n", "False\n",
		},
		{
			"an unreached division by zero",
			"x = 5\nprint(x > 9 < 1 // 0)\n", "False\n",
		},
		{
			"an unreached call that prints",
			"def boom():\n    print(\"called\")\n\n    return 5\n\nprint(3 < 2 < boom())\n", "False\n",
		},
		{
			"a long chain stops at its first false link",
			"def boom():\n    print(\"called\")\n\n    return 5\n\nprint(1 < 2 < 3 < 0 < boom())\n", "False\n",
		},
		{
			"the handler of an unreached raise is never asked",
			"x = None\ntry:\n    print(1 == x < 3)\nexcept TypeError:\n    print(\"caught\")\n", "False\n",
		},
		{
			// The pair the row needs: the SAME chain with a true first link does call, and calls once.
			"a reached call still calls, once",
			"def boom():\n    print(\"called\")\n\n    return 5\n\nprint(1 < 2 < boom())\n", "called\nTrue\n",
		},
		{
			"a reached link still raises, and the handler still catches it",
			"x = None\ntry:\n    print(x == x < 3)\nexcept TypeError:\n    print(\"caught\")\n", "caught\n",
		},
		{
			"a reached link raises uncaught, at the trap's exit class",
			"x = None\nprint(x == x < 3)\n", "TypeError:'<' not supported between instances of 'NoneType' and 'int'\n",
		},
		{
			"a counter shows which links ran",
			"calls = 0\n\ndef probe(v):\n    global calls\n\n    calls = calls + 1\n\n    return v\n\nprint(3 < 2 < probe(9))\nprint(1 < 2 < probe(9))\nprint(calls)\n",
			"False\nTrue\n1\n",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			// The table is a convenience, not an expectation: the reference is the expectation.
			if strings.Contains(r.want, "TypeError:") {
				if !strings.Contains(want, "TypeError") || !strings.Contains(want, "not supported between instances") {
					t.Fatalf("row is stale: CPython printed %q, want the trap", want)
				}
			} else if want != r.want {
				t.Fatalf("row is stale: python3 prints %q, the row pins %q", want, r.want)
			}
			p := writeSrc(t, dir, "chainlink", r.src)
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 on the compiler's own module (ADR 0166): %s", engine, out)
				}
				if strings.Contains(r.want, "TypeError:") {
					if code == 0 {
						t.Errorf("%s answered %q where the reference raises", engine, out)
					}
					if !strings.Contains(out, "TypeError") || !strings.Contains(out, "'<' not supported between instances of 'NoneType' and 'int'") {
						t.Errorf("%s says %q, want the reference's class and sentence", engine, out)
					}
					continue
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — the chain evaluated a link the reference never reached", engine, out, want)
				}
			}
		})
	}
}

// TestTheChainShortCircuitProgramIsTheCountingWitness runs the conformance program, whose `calls` prints are
// the shape no verdict-only test can see: an operand the run skipped must not have run, and one it reached
// must have run exactly once.
func TestTheChainShortCircuitProgramIsTheCountingWitness(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := readProgramSrc("chain_short_circuit")
	want, ok := cpythonPlainOut(t, dir, src)
	if !ok {
		t.Skip("no python3 available to cross-check")
	}
	if strings.Count(want, "probe ran") != 1 {
		t.Fatalf("the program no longer asks the question: CPython prints %q, want exactly one `probe ran`", want)
	}
	if !strings.Contains(want, "caught the reached link") {
		t.Fatalf("the program no longer reaches the link that raises: %q", want)
	}
	p := writeSrc(t, dir, "chain_short_circuit", src)
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

// TestTheOldConjunctionShapeIsGoneFromTheModule is the CLI-side shape claim: a flat `and i32` over every
// link is the artifact this row was filed about, and the module must not grow one back.
func TestTheOldConjunctionShapeIsGoneFromTheModule(t *testing.T) {
	t.Parallel()
	src := "x = None\nprint(1 == x < 3)\n"
	out, code := cliRunCode(t, "--emit-llvm", src)
	if code != 0 {
		t.Fatalf("--emit-llvm exited %d: %s", code, out)
	}
	main := out[strings.Index(out, "define i32 @main("):]
	if !strings.Contains(main, "br i1 ") {
		t.Fatalf("the module never branches on a link's verdict — the chain is a flat conjunction again:\n%s", main)
	}
	if strings.Contains(main, "  %and") || strings.Contains(main, "and i32") {
		t.Errorf("the module conjoins the link verdicts again, which is the shape Gap R.520 filed:\n%s", main)
	}
}
