package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/donutloop/gusty/pkg/lang"
)

// replMode runs the interactive REPL. It accumulates input line by line so
// multi-line constructs (functions, classes, blocks) can be entered, prints
// primary/continuation prompts when attached to a terminal, and recovers from
// panics so an internal bug never kills the session.
func replMode(jitMode bool) int {
	ev := lang.NewEvaluator()
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
				replRun(ev, &buf, jitMode)
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
		replRun(ev, &buf, jitMode)
	}
}

// replRun parses and evaluates one complete REPL input unit (possibly built
// from several accumulated lines). Panics are recovered so an internal bug
// never kills the session.
func replRun(ev *lang.Evaluator, buf *strings.Builder, jitMode bool) {
	src := buf.String()
	buf.Reset()
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "gustyc: REPL panic recovered: %v\n", r)
		}
	}()
	if jitMode {
		res, err := lang.JIT(src, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
			return
		}
		fmt.Print(res.Output)
		return
	}
	prog, err := lang.Parse(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	v, err := ev.EvalProgram(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return
	}
	fmt.Println(ev.Repr(v))
}
