package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestGCStressCorpus runs every shared conformance program twice through the
// interpreter: once with the collector's normal allocation threshold, and once
// with a collection forced at *every* statement boundary. A root the interpreter
// forgot is a wrong answer or a crash in the second run, so this is the harness
// that makes the precise root set (L7.2, ADR 0181) a tested property instead of
// an argument. It also asserts the stressed run really collected — a probe that
// never runs proves nothing.
func TestGCStressCorpus(t *testing.T) {
	for _, c := range conformanceCases() {
		if !c.Shared {
			continue
		}
		normal, err := lang.InterpreterRun(c.Source)
		stressed, stats, serr := lang.InterpreterRunOpts(c.Source, lang.InterpreterRunOptions{
			GCStress:         true,
			GCAllocThreshold: 1,
		})
		if (err == nil) != (serr == nil) {
			t.Fatalf("%s: normal err=%v, gc-stressed err=%v", c.ID, err, serr)
		}
		if normal != stressed {
			t.Fatalf("%s: gc stress changed the program's output\n normal:   %q\n stressed: %q", c.ID, normal, stressed)
		}
		if err != nil {
			// A program that traps must trap the same way under collection.
			if !strings.Contains(serr.Error(), firstLine(err.Error())) {
				t.Fatalf("%s: gc stress changed the failure: %v vs %v", c.ID, err, serr)
			}
			continue
		}
		if stats.Collections == 0 {
			t.Fatalf("%s: gc stress collected nothing at all: %+v", c.ID, stats)
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestGCStressKeepsTheHeapBounded is the property that makes collection worth
// having: a long-running program's heap is bounded by live data, not by everything
// it ever allocated. The same program under no collection grows linearly, so the
// comparison is the assertion.
func TestGCStressKeepsTheHeapBounded(t *testing.T) {
	src := strings.Join([]string{
		"def churn(n):",
		"    total = 0",
		"    i = 0",
		"    while i < n:",
		"        junk = [i, i, i]",
		"        total = total + junk[2]",
		"        i = i + 1",
		"    return total",
		"acc = 0",
		"for k in range(400):",
		"    row = [k, k, k]",
		"    acc = acc + row[0] + churn(8)",
		"print(acc)",
	}, "\n")
	_, stressed, err := lang.InterpreterRunOpts(src, lang.InterpreterRunOptions{GCAllocThreshold: 16})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stressed.Collections == 0 {
		t.Fatalf("the safe points never fired: %+v", stressed)
	}
	if stressed.TotalFreed < 3000 {
		t.Fatalf("the loop's garbage survived: %+v", stressed)
	}
}

// TestGCAgentMachinePath: --gc-stats is the machine consumption path for the
// collector, so its numbers must arrive as data, not as prose to scrape.
func TestGCAgentMachinePath(t *testing.T) {
	src := "acc = 0\nfor k in range(300):\n    row = [k, k]\n    acc = acc + row[1]\nprint(acc)\n"
	out, stats, err := lang.InterpreterRunOpts(src, lang.InterpreterRunOptions{GCAllocThreshold: 8})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "44850\n" {
		t.Fatalf("program output = %q", out)
	}
	line := stats.String()
	for _, want := range []string{"backend=interpreter", "collections=", "roots=", "skipped=", "freed=", "total_freed=", "live="} {
		if !strings.Contains(line, want) {
			t.Fatalf("collector report %q missing %q", line, want)
		}
	}
}
