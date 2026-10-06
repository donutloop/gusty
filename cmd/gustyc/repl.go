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
				replRun(&buf)
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
		replRun(&buf)
	}
}

// replRun compiles and runs one complete REPL input unit (possibly built from several accumulated
// lines). Panics are recovered so an internal bug never kills the session.
func replRun(buf *strings.Builder) {
	src := buf.String()
	buf.Reset()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "gustyc: REPL panic recovered: %v\n", r)
		}
	}()
	res, err := lang.RunSnippet(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return
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
