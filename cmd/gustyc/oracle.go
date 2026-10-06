package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/donutloop/gusty/pkg/lang"
)

// oracleMode is the CLI's answer to "does this program behave like Python?" —
// the same comparison the conformance harness runs, exposed for one ad-hoc program so an
// agent can check a construct before trusting it (roadmap L11.9, ADR 0186).
//
// Two legs: the program gusty compiles, and the pinned CPython. A third leg used to run the
// AST interpreter on the same source; ADR 0302 retired that engine, and with it the
// engine-vs-engine half of the verdict — what remains is the half that ever decided anything.
//
// The verdict comes from lang.BuildOracleReport, the identical function the matrix
// uses, so a program cannot pass here and fail there. Exit codes follow the table
// in docs/operations.md: 0 match, 6 debt (gusty printed something else), 7
// not_applicable (CPython could not run the source, so there is no verdict).
func oracleMode(src, file string, jsonOut bool) int {
	s, err := srcOrFile(src, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitUsage
	}

	aotOut, aotErr := aotLeg(s)
	pyOut, pyErr := pythonLeg(s)

	rep := lang.BuildOracleReport(aotErr == nil, aotOut, errLine(aotErr), nil,
		pyErr == nil, pyOut, errLine(pyErr))

	if jsonOut {
		b, jerr := json.Marshal(rep)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", jerr)
			return exitCompileError
		}
		fmt.Println(string(b))
	} else {
		fmt.Printf("oracle: %s\n", rep.Status)
		for _, l := range rep.Legs {
			state := "ok"
			if !l.OK {
				state = "FAILED: " + l.Error
			}
			verb := "differs from CPython"
			if l.Backend == "python" {
				verb = "the oracle"
			} else if l.Matches {
				verb = "matches CPython"
			}
			fmt.Printf("  %-11s %-8s %s\n", l.Backend, state, verb)
			for _, line := range strings.Split(strings.TrimRight(l.Stdout, "\n"), "\n") {
				fmt.Printf("      | %s\n", line)
			}
		}
		for _, n := range rep.Notes {
			fmt.Printf("  note: %s\n", n)
		}
		if len(rep.Rules) > 0 {
			fmt.Printf("  rules: %s\n", strings.Join(rep.Rules, ", "))
		}
	}

	return oracleExit(rep.Status)
}

// oracleExit maps a verdict to the documented exit code (docs/operations.md
// § Exit codes). Three outcomes, three codes: a program that behaves like Python,
// a program that does not, and a program the oracle could not judge.
func oracleExit(status string) int {
	switch status {
	case lang.OracleMatch:
		return exitOK
	case lang.OracleDebt:
		return exitOracleDivergence
	case lang.OracleNA:
		return exitOracleNoVerdict
	default:
		// An unknown verdict is a bug in the classifier, not a verdict: say so with
		// the code that means "the tool failed", never with success.
		return exitOracleNoVerdict
	}
}

// aotLeg runs the compiled backend through the same in-process runner the CLI's run paths use,
// and records a Go panic instead of dying: a program that crashes the compiler is a finding, and
// the oracle has to survive it long enough to report what the other legs said (L11.8).
func aotLeg(src string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("compiler panic: %v", r)
		}
	}()
	res, err := lang.JIT(src, 0)
	if res != nil {
		if res.Code != 0 {
			// A compiled program that died from an uncaught exception is a leg that did
			// not complete, not a leg that printed something different — the difference
			// matters to the report, because "our backend refused" must never be
			// recorded as an answer (ADR 0166). Gap R.17 is why the status could be asked
			// for in the first place.
			return res.Output, fmt.Errorf("compiled program trapped (exit %d): %s", res.Code, firstLine(res.Stderr))
		}
		return res.Output, err
	}
	return "", err
}

// pythonLeg runs the oracle interpreter.
func pythonLeg(src string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("oracle panic: %v", r)
		}
	}()
	out, stderr, err := lang.PythonRun(src)
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, firstLine(stderr))
	}
	return out, nil
}

func errLine(err error) string {
	if err == nil {
		return ""
	}
	return firstLine(err.Error())
}

// firstLine keeps a diagnostic to one line: a whole CPython traceback or an llc
// dump does not belong in a leg column.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
