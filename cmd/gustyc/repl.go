package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/donutloop/gusty/pkg/lang"
)

// replMode runs the interactive REPL. It accumulates input line by line so multi-line
// constructs (functions, classes, blocks) can be entered, prints primary/continuation prompts
// when attached to a terminal, and recovers from panics so an internal bug never kills the
// session.
//
// Every turn is compiled and run by the one backend (codegen -> llc -> cc -> dlopen -> run), and
// the answer to a turn that ends in an expression is the module's own self-report — the REPL echo
// of ADR 0302 — not a value the tool held in a variable. A REPL that interpreted and a CLI that
// compiled used to disagree about a program's meaning; there is no longer a way to ask for that.
//
// The engine used to carry the session: bindings lived in a runtime object the tool kept alive
// between turns. There is no such object any more — a turn is a program, and its state dies with
// the process that ran it — so the session is carried the way a compiled language can carry it: as
// source. Turns that *establish* state (definitions, classes, assignments, compound statements) are
// recompiled into every later turn; turns that merely *ask* (a bare expression, a call whose value
// the caller wanted to see) are not, so `print(1)` at the prompt does not print again on turn two.
// It is replay, not a live heap: an assignment's right-hand side runs again, and the day the forked
// compile-server lands (roadmap L13.x) it replaces this while the tests stay where they are.
func replMode() int {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	interactive := isTTY()
	var buf strings.Builder
	for {
		if interactive {
			if buf.Len() > 0 {
				fmt.Print("... ")
			} else {
				fmt.Print("> ")
			}
		}
		if !sc.Scan() {
			// EOF: flush any buffered input as a final complete unit.
			if buf.Len() > 0 {
				replRun(&buf, &session)
			}
			if interactive {
				fmt.Println()
			}
			return exitOK
		}
		line := sc.Text()
		buf.WriteString(line)
		buf.WriteByte('\n')
		if lang.IsIncomplete(buf.String()) {
			continue
		}
		replRun(&buf, &session)
	}
}

// session is the source of the turns that established state, in the order they were typed.
var session []string

// establishesState reports whether a completed turn is the kind that leaves something behind —
// a definition, a class, an import, an assignment, a compound statement. The test is on the last
// statement at column zero, because that is what a turn ends on: a bare expression (`x`, `f(21)`,
// `print(1)`) asks a question and owes an answer, and replaying it would reprint its effects on
// every later turn. Everything else — `def`, `class`, `if`, `while`, `for`, `try`, `x = 1` —
// changes what the next turn means, so the next turn has to run it again to mean the same thing.
func establishesState(src string) bool {
	last := ""
	for _, ln := range strings.Split(src, "\n") {
		t := strings.TrimRight(ln, " \t")
		if t == "" || strings.HasPrefix(strings.TrimSpace(t), "#") {
			continue
		}
		if strings.HasPrefix(t, " ") || strings.HasPrefix(t, "\t") {
			continue // inside a block; the statement that opened it is what counts
		}
		last = t
	}
	if last == "" {
		return false
	}
	if strings.HasSuffix(last, ":") {
		return true // a compound statement's header: its body binds
	}
	for _, kw := range []string{"def ", "class ", "async def ", "async for ", "async with ", "import ", "from ", "global ", "nonlocal "} {
		if strings.HasPrefix(last, kw) {
			return true
		}
	}
	if at := strings.IndexAny(last, "=!<>"); at >= 0 && last[at] == '=' {
		// An assignment (or an augmented one). `==` and friends are comparisons, which belong to
		// the question side, so a lone `=` is what marks a binding here.
		return strings.Contains(last, "=") && !strings.Contains(last, "==") && !strings.Contains(last, "!=") &&
			!strings.Contains(last, "<=") && !strings.Contains(last, ">=")
	}
	return false
}

// replRun compiles and runs one complete REPL input unit (possibly built from several accumulated
// lines). Panics are recovered so an internal bug never kills the session.
func replRun(buf *strings.Builder, session *[]string) {
	src := buf.String()
	buf.Reset()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "gustyc: REPL panic recovered: %v\n", r)
		}
	}()
	program := src
	if len(*session) > 0 {
		program = strings.Join(*session, "\n") + "\n" + src
	}
	res, err := lang.RunSnippet(program)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return
	}
	// A turn is only carried forward if it built and ran. Carrying a turn that failed would let one
	// bad line poison every later turn, which is the difference between a REPL and a brick.
	if establishesState(src) && res.Code == 0 {
		*session = append(*session, strings.TrimRight(src, "\n"))
	}
	fmt.Print(res.Output)
	// The answer to the turn, written by the program the compiler just built.
	if res.Result != nil {
		fmt.Println(res.Result.Repr)
	}
	// A turn whose program died says so, on the tool's own channel. Swallowing it left the REPL
	// showing a blank line for a program that raised (Gap R.17).
	if res.Stderr != "" {
		fmt.Fprint(os.Stderr, res.Stderr)
	}
	if res.Code != 0 {
		fmt.Fprintf(os.Stderr, "gustyc: program exited with status %d\n", res.Code)
	}
}
