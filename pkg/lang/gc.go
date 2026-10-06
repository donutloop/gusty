package lang

import (
	"os"
	"strconv"
	"strings"
)

// Precise stack roots (roadmap L7.2, ADR 0181).
//
// The interpreter heap holds every container, instance, closure, boxed string,
// boxed float and coroutine. Before this file the collector had exactly one
// root — the module environment — and nothing called it while a program ran,
// which meant two things at once: a REPL session grew without bound, and no
// collection could happen *during* a program because the values that are live
// at that moment live in Go locals the collector cannot see.
//
// The root set is now explicit, and collection happens at real safe points:
//
//   - frames:      the local scope of every active call (pushed by callFunc).
//   - rootGroups:  handles a statement keeps in Go locals while it runs a
//                  nested body — a for-loop's iterable, a with-statement's
//                  manager, a match subject, the value of the previous
//                  statement. Each construct declares them before the body.
//   - permanent:   the None singleton, the generator accumulator, the super()
//                  receiver, and every class object.
//   - gcWatermark: the soundness floor. Everything allocated after the watermark
//                  is unconditionally live, which is what makes it safe to
//                  collect while expressions are half-evaluated: their
//                  temporaries are younger than the watermark by construction.
//
// The watermark only advances at a *statement boundary with an empty expression
// stack* (exprDepth == 0) reached from a construct that declared its root
// groups. That single invariant is the whole soundness argument: when it holds,
// no interpreter frame is mid-expression, so nothing lives only in a register.
// The AOT backend mirrors the same idea with a per-slot handle tag so its
// collector knows exactly which stack slots hold handles (see codegen.go,
// @gc.kinds and rt_gc).

// heapIDBase is the first heap id the interpreter hands out, and therefore the
// line between "an integer the program computed" and "an object the interpreter
// made". It is not a tuning knob. Values are an untagged int64 until roadmap
// L11.1 gives the language real tagged values, so an integer that happens to
// equal a live object id is read back as that object — and the first consumer to
// *depend* on the difference found the collision in ordinary code: a bench loop
// computing i*i reached 1<<20 exactly, and `self.x * self.x + self.y * self.y`
// came back as the class's own method object. At 1<<48 the window is far outside
// anything arithmetic produces (a squared loop counter reaching it would need
// 168 million iterations, past the point where the loop itself is the problem),
// and one predicate — isHandle below — decides handle-ness for the collector and
// for operator dispatch, so the two can never disagree about what a value is.
const heapIDBase = 1 << 48

// gcAllocThresholdDefault is how many heap allocations may accumulate before a
// statement boundary triggers a collection. It is a constant, not a clock or a
// heuristic: two runs of one program collect at exactly the same statements,
// which is what keeps interpreter output deterministic (and parity testable).
const gcAllocThresholdDefault = 256

// GCStats is the collector's self-report. The compiled backend produces the
// same fields from its own runtime, so --gc-stats means the same thing for both
// backends and an agent can read either without scraping prose.
type GCStats struct {
	// Collections is how many mark-and-sweep passes have run.
	Collections int `json:"collections"`
	// Roots is how many root handles the last collection traced.
	Roots int `json:"roots"`
	// Skipped is how many root candidates the last collection proved were raw
	// immediates rather than handles, and never scanned. This is the number
	// precision buys: a conservative collector would have guessed at all of them.
	Skipped int `json:"skipped"`
	// Marked is how many heap objects the last collection found reachable.
	Marked int `json:"marked"`
	// Freed is how many heap objects the last collection reclaimed.
	Freed int `json:"freed"`
	// TotalFreed is cumulative across every collection this run: the size of the
	// garbage the program produced. Freed alone describes only the last pass.
	TotalFreed int `json:"total_freed"`
	// Live is how many heap objects remain after the last collection.
	Live int `json:"live"`
	// Frames is how many call frames were rooted by the last collection.
	Frames int `json:"frames"`
	// Protected is how many objects the watermark kept alive without tracing.
	Protected int `json:"protected"`
	// Generational is true when the last collection was a young (nursery) GC.
	// Top is the high-water mark of the compiled backend's root stack (0 for the
	// interpreter, which has no fixed-capacity stack): how close the program came to
	// the point where a handle could not be rooted.
	Top          int  `json:"top"`
	Generational bool `json:"generational"`
	// Backend names the collector that produced these numbers.
	Backend string `json:"backend"`
}

// String renders the stats as the stable key=value line the CLI prints. One
// format for the human path and for --json, so a script can grep the human line
// while a machine reads the object.
func (s GCStats) String() string {
	var b strings.Builder
	b.WriteString("gc: backend=")
	b.WriteString(s.Backend)
	b.WriteString(" collections=")
	b.WriteString(strconv.Itoa(s.Collections))
	b.WriteString(" roots=")
	b.WriteString(strconv.Itoa(s.Roots))
	b.WriteString(" skipped=")
	b.WriteString(strconv.Itoa(s.Skipped))
	b.WriteString(" marked=")
	b.WriteString(strconv.Itoa(s.Marked))
	b.WriteString(" freed=")
	b.WriteString(strconv.Itoa(s.Freed))
	b.WriteString(" total_freed=")
	b.WriteString(strconv.Itoa(s.TotalFreed))
	b.WriteString(" live=")
	b.WriteString(strconv.Itoa(s.Live))
	b.WriteString(" frames=")
	b.WriteString(strconv.Itoa(s.Frames))
	b.WriteString(" protected=")
	b.WriteString(strconv.Itoa(s.Protected))
	if s.Generational {
		b.WriteString(" kind=young")
	} else {
		b.WriteString(" kind=full")
	}
	if s.Top != 0 {
		// Only the compiled backend reports a root-stack high-water mark; keeping it
		// conditional leaves the interpreter's line byte-for-byte stable.
		b.WriteString(" top=")
		b.WriteString(strconv.Itoa(s.Top))
	}
	return b.String()
}

// ParseGCStatsLine reads back one collector self-report line: the shape written by
// GCStats.String (interpreter) and by the compiled runtime's rt_gc_report (--gc-stats
// on the AOT path, whose numbers live in the child program's globals and so reach the
// CLI as this line on fd 2). Splitting the documented line into fields — instead of
// scraping prose — is what lets `--json --aot --gc-stats` hand an agent data.
// Fields the reporting backend does not emit are simply left at zero.
func ParseGCStatsLine(line string) (GCStats, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 3 || fields[0] != "gc:" {
		return GCStats{}, false
	}
	num := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0
		}
		return n
	}
	st := GCStats{Backend: "aot"}
	seen, numbered := false, false
	for _, f := range fields[1:] {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "backend":
			st.Backend = v
			seen = true
		case "collections":
			st.Collections = num(v)
			numbered = true
		case "roots":
			st.Roots = num(v)
		case "skipped":
			st.Skipped = num(v)
		case "marked":
			st.Marked = num(v)
		case "freed":
			st.Freed = num(v)
		case "total_freed":
			st.TotalFreed = num(v)
		case "live":
			st.Live = num(v)
		case "frames":
			st.Frames = num(v)
		case "protected":
			st.Protected = num(v)
		case "kind":
			st.Generational = v == "young"
		case "top":
			st.Top = num(v)
		}
	}
	// A report always names its backend and always counts its collections: both are
	// required so a stray "gc:" line in program output cannot be mistaken for one.
	if !seen || !numbered {
		return GCStats{}, false
	}
	return st, true
}

// gcEnvStress forces a collection at every statement boundary instead of only
// when allocation pressure asks for one. It exists so the soundness of the root
// set can be *tested* rather than assumed: the conformance corpus runs twice,
// once normally and once with this on, so a root the interpreter forgot shows up
// as a failing test instead of a heisenbug. Enabled with GUSTY_GC_STRESS=1.
func gcEnvStress() bool {
	v := os.Getenv("GUSTY_GC_STRESS")
	if v == "" {
		return false
	}
	on, err := strconv.ParseBool(v)
	return err == nil && on
}


















// gcReport is the process switch that makes the *compiled* backend's runtime print its
// collector report at the end of main — the AOT counterpart of the interpreter's
// --gc-stats. Codegen has no option object, so this follows the SetStdlibDir pattern:
// one package switch, set once by the CLI before generating.
var gcReport bool

// SetGCReport turns the compiled backend's collector self-report on or off.
func SetGCReport(on bool) { gcReport = on }

// GCReportEnabled reports whether compiled programs print their collector report.
func GCReportEnabled() bool { return gcReport }
