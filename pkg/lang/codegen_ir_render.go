// The runtime blocks' rendering step: the arena/arith/pair-fold templates are text with placeholders,
// and fillRootTemplate is the only thing that fills them (the IR is full of `%` register names, so it is
// strings.ReplaceAll, not fmt). llvmCString is the one Go-string-to-LLVM-constant encoder every road that
// hands a text to the runtime uses.
package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// llvmCString renders a Go string as an LLVM byte-string constant, NUL terminated.
// Escapes count as one byte each, so the declared array length stays honest.
func llvmCString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\0A`)
		default:
			if c < 0x20 || c > 0x7e {
				b.WriteString(fmt.Sprintf(`\%02X`, c))
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteString(`\00"`)
	return b.String()
}

// gcRootReportLine is what the compiled backend prints for --gc-stats, mirroring the
// record's report so one assertion can read either backend.
const gcRootReportLine = "gc: backend=aot collections=%d roots=%d skipped=%d marked=%d freed=%d total_freed=%d live=%d top=%d capacity=%d headroom=%d grown=%d exhausted=%d\n"

// gcRootOverflowMessage is the loud failure for a root stack that ran out. A handle
// that cannot be pushed may be swept while still live, so the alternative to
// stopping is corruption.
const gcRootOverflowMessage = "gustyc: the compiled root stack is full (recursion too deep); see ADR 0181\n"

// renderedRootRuntime fills in the root runtime's capacities and string constants.
func renderedRootRuntime() string {
	return fillRootTemplate(rootRuntimeIR)
}

// renderedRootGlobals renders the root stack's data half, which is emitted for every
// module (rt_gc reads @gc.kinds and the counters even in a program that never pushes).
func renderedRootGlobals() string {
	return fillRootTemplate(rootGlobalsIR)
}

// fillRootTemplate substitutes the capacities and string constants into a root-runtime
// block. Placeholders are used instead of fmt because the IR is full of `%` register
// names, which Sprintf would read as verbs.
func fillRootTemplate(t string) string {
	cap := strconv.Itoa(gcRootCap)
	t = strings.ReplaceAll(t, "@CAP@", cap)
	t = strings.ReplaceAll(t, "@MSGLEN@", strconv.Itoa(len(gcRootOverflowMessage)+1))
	t = strings.ReplaceAll(t, "@MSG_BYTES@", strconv.Itoa(len(gcRootOverflowMessage)))
	t = strings.ReplaceAll(t, "@MSG@", llvmCString(gcRootOverflowMessage))
	t = strings.ReplaceAll(t, "@FMTLEN@", strconv.Itoa(len(gcRootReportLine)+1))
	t = strings.ReplaceAll(t, "@FMT@", llvmCString(gcRootReportLine))
	// One spelling of the ceiling: the allocator's bound, the report's `capacity=` and the trap's sentence
	// all read `HeapCapacity`, so a run cannot report a headroom against a different arena than the one
	// that raised (roadmap Gap Q.2, ADR 0347).
	t = strings.ReplaceAll(t, "@HEAPCAP@", strconv.Itoa(HeapCapacity))
	// The arena's two sizes are the same pair everywhere: the reservation the tables are declared with
	// and the window a run starts in. Both are read from heap_guard.go, so a module cannot allocate from
	// one arena and report another (roadmap Gap Q.2, ADR 0350).
	t = strings.ReplaceAll(t, "@HEAPSLOTS@", strconv.Itoa(HeapCapacity))
	t = strings.ReplaceAll(t, "@HEAPINIT@", strconv.Itoa(HeapInitialCapacity))
	t = strings.ReplaceAll(t, "@HEAPGROW@", strconv.Itoa(HeapGrowthFactor))
	return t
}

// arenaRuntimeVars renders the runtime blocks that touch the arena. They are package-level because
// the text is a pure function of the two constants and every consumer — the emitter, the block-usage
// probe, the IR-shape tests — must read the same string.
var (
	heapRuntimeIR     = fillRootTemplate(heapRuntimeIRTmpl)
	numArithRuntimeIR = fillRootTemplate(numArithRuntimeIRTmpl)
	pairFoldRuntimeIR = fillRootTemplate(pairFoldRuntimeIRTmpl)
	// The sequence doors read the arena's tables and intern through the heap block's own door, so they
	// are filled by the same door as every other arena-touching block (roadmap Gap R.33, ADR 0365).
	seqRuntimeIR = fillRootTemplate(seqRuntimeIRTmpl)
)

// printfCall emits a call to @printf with the given operand type and format.
func (g *irGen) printfCall(ty, v, format string) string {
	name, size := g.fmtStr(format)
	res := g.newTmp()
	return fmt.Sprintf("%s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), %s %s)", res, size, size, name, ty, v)
}
