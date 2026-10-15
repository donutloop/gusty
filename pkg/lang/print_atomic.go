package lang

import "strings"

// print_atomic.go — Gap L.5: a `print` statement writes its line as one unit.
//
// Python evaluates every argument and only then writes the line. The compiled backend did the
// two at once: the argument loop emitted `printf` for an argument the moment it had lowered it,
// so an argument whose own evaluation writes something — a call whose body prints — landed
// *inside* the caller's line:
//
//	def twice(v):
//	    print("<<", v, ">>")
//	    return v + v
//
//	print("got", twice(21))     # CPython: "<< 21 >>" then "got 42"
//	                          # this backend, until now: "got << 21 >>" then "42"
//
// Both witness legs agreed on the wrong answer — the retired engine wrote as it evaluated too —
// which is exactly why no amount of leg-vs-leg parity could find it (ADR 0186's oracle leg did).
//
// # The decision: move the writes, not the evaluations
//
// Two shapes of fix were on the table.
//
// The first is the one a runtime would offer: point the renderer at a pending-line buffer, keep
// the emission order, and flush the buffer at the end of the statement. ADR 0258's capture sink
// already exists for `str()`/`repr()`, so the machinery looked free. It is not: the buffer is one
// global with one length, and a print statement is not the only thing that can be mid-line when
// something else writes. A call inside an argument runs a *nested* print, whose flush would write
// the outer statement's half-written line too, and `str()` resets the same length to 0 the moment
// it starts. Making that protocol sound needs a stack of marks, a rule for who owns a mark when an
// exception unwinds past it, and a reason to trust the depth counter after a caught exception that
// was raised inside an argument. Every one of those is a piece of state the module has to keep
// right, and a program that gets it wrong loses or duplicates a line at exit 0.
//
// The second fix has no state at all: the emitted IR for the argument list is already a straight
// line of *evaluations* with a handful of *writes* threaded through it, so thread the writes to
// the end instead. Nothing new is executed, nothing is cached, and the nested call's own print is
// already in the right place — it is a call in the evaluation phase, so it writes to stdout during
// the arguments, which is precisely CPython's ordering. An argument that raises stops the run at
// the raise and the line is never written, which is also precisely CPython's; the old emission
// wrote the leading arguments out first and then trapped, which no Python does.
//
// So: one rewrite over the emitted text of one statement, and one gate that decides whether the
// rewrite is sound to perform.
//
// # The gate, and what it refuses
//
// The rewrite moves write instructions to the end of the statement's region. That is only sound
// when a write cannot be *conditional* — a `printf` inside one arm of an `if`-shaped lowering is
// that arm's rendering of a value, and hoisting it out of its block would print it on the paths
// that never chose it. The rule is therefore per argument:
//
//	every write of an argument must sit after that argument's last block boundary
//
// where a boundary is a label or a terminator. A write in a chunk's trailing straight-line run is
// a write in the block the emitter stands in when the argument is finished; the block the
// statement finishes in is a descendant of it on the same straight-line path, so moving the write
// there keeps it executed exactly when the line is printed at all. A write anywhere earlier means
// the argument's own lowering branched, and the statement is emitted the way it always was: an
// honest interleaving rather than a conditional line that sometimes goes missing. The shapes that
// stay there are named by `printWriteDeferralReason` and pinned by the filed-not-fixed table in
// `print_atomic_test.go`; the `--emit-llvm` of one of them still carries the interleaving.
//
// Two further checks keep the rewrite from being clever at the program's expense: a write that
// *returns* a register must not have that register read anywhere else in the statement (a write
// whose byte count is being used as a value is not a write, it is an evaluation wearing one), and
// an `invoke` counts as a boundary, because its unwind edge is a second successor that the
// straight-line reasoning does not cover.
//
// Nothing here decides an answer, emits an instruction or advances a counter: it reads the text
// the emitter already wrote and returns either a rewritten text or the same one (ADR 0273's rule
// about gates). `printWriteDeferralReason` is exported to the tests so the decision can be asked
// as a function, in both directions, without a module in the way.

// printWriteSinks names the IR calls that put the *program's* bytes on a stream when they are
// emitted from a print statement's argument list: the printf calls the number, text and f-string
// roads write, and the runtime printers every container, tag and verdict road goes through.
//
// The list is a whitelist on purpose. Anything not on it stays where the emitter put it, so a
// printer added later without a line here costs the interleaving it already had — the state of the
// world before this row — rather than a line that is written in the wrong order or a call that is
// silently moved past the code that raised it. Two families are deliberately *not* writes:
// `rt_die`, whose report belongs to the raise that is happening right now and goes to stderr, and
// `rt_echo_*`, which is the REPL's channel and not the program's stdout (ADR 0204). The value
// doors the renderers are built from — `rt_str_of_value`, `rt_str_ptr`, `rt_fmt_double`,
// `rt_bool_text`, `rt_kind_name` — are evaluations: they compute text and never put it anywhere.
var printWriteSinks = map[string]bool{
	"@printf":                   true,
	"@rt_out_txt":               true,
	"@rt_out_int":               true,
	"@rt_print_none":            true,
	"@rt_print_bool":            true,
	"@rt_print_str":             true,
	"@rt_print_value":           true,
	"@rt_print_mixed_value":     true,
	"@rt_print_list":            true,
	"@rt_print_list_str":        true,
	"@rt_print_list_mixed":      true,
	"@rt_print_container_value": true,
	"@rt_dict_print":            true,
	"@rt_dict_print_s":          true,
	"@rt_dict_print_mixed":      true,
	"@rt_set_print":             true,
	"@rt_set_print_str":         true,
	"@rt_set_print_mixed":       true,
}

// printWriteLine reports whether one IR line is a call to one of those sinks. The callee is read
// from after the last `call` keyword, because a print line's operands routinely name globals of
// their own (`@.fmt3`, `@.fmtdictkeys`) and the first `@` on the line is not always the callee.
func printWriteLine(line string) bool {
	t := strings.TrimSpace(line)
	i := strings.LastIndex(t, "call ")
	if i < 0 {
		return false
	}
	at := strings.IndexByte(t[i:], '@')
	if at < 0 {
		return false
	}
	start := i + at
	end := start + 1
	for end < len(t) {
		c := t[end]
		if c == '(' || c == ' ' || c == ',' || c == ')' {
			break
		}
		end++
	}
	return printWriteSinks[t[start:end]]
}

// printBlockLine reports whether one IR line ends the straight-line run it stands in: a label, or
// one of the terminators this emitter writes. `phi` is not one — it stays with the block it heads,
// which is the block its arms join, and the reasoning in the gate above is about writes, not about
// the merges they read their operands through.
func printBlockLine(line string) bool {
	if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		t := strings.TrimSpace(line)
		for _, kw := range []string{"br ", "br\t", "ret ", "ret\t", "switch ", "invoke ", "invoke\t", "resume ", "unreachable"} {
			if strings.HasPrefix(t, kw) {
				return true
			}
		}
		return false
	}
	t := strings.TrimSpace(line)
	return t != "" && strings.HasSuffix(t, ":") && !strings.ContainsAny(t, " ")
}

// printWriteLines splits an emitted chunk into lines that still carry their own newline, so putting
// the halves back together reproduces the statement byte for byte. A split on "\n" followed by a join
// drops the last newline of every piece it re-joins, and two instructions end up on one line — which
// is not a cosmetic bug: `llc` rejects the module, and ADR 0166 reserves exit 2 for exactly that.
func printWriteLines(chunk string) []string {
	return strings.SplitAfter(chunk, "\n")
}

// printWriteDeferralReason answers the gate's question in the two directions: "" when every write
// of the argument list may be moved to the end of its evaluation, and the reason it may not
// otherwise. `marks` are byte offsets into `region`: one per argument, where each argument began
// emitting, plus the end of the list.
func printWriteDeferralReason(region string, marks []int) string {
	if len(marks) < 2 {
		return "the statement has no arguments"
	}
	for k := range marks {
		if marks[k] < 0 || marks[k] > len(region) {
			return "an argument's emitted region is not inside the statement"
		}
		if k > 0 && marks[k] < marks[k-1] {
			return "the arguments did not emit in order"
		}
	}
	for k := 0; k+1 < len(marks); k++ {
		lines := printWriteLines(region[marks[k]:marks[k+1]])
		lastBoundary := -1
		for i, ln := range lines {
			if printBlockLine(ln) {
				lastBoundary = i
			}
		}
		for i, ln := range lines {
			if !printWriteLine(ln) {
				continue
			}
			if lastBoundary >= 0 && i < lastBoundary {
				return "an argument writes inside a block its own lowering opened"
			}
			if reg, ok := printWriteResultReg(ln); ok {
				for _, other := range printWriteLines(region[marks[0]:]) {
					if other == ln {
						continue
					}
					if printLineMentions(other, reg) {
						return "an argument's write returns a register the statement reads"
					}
				}
			}
		}
	}
	return ""
}

// printLineMentions reports whether a line reads a register, with the register's name ending where
// its name ends: `%t1` is not a mention of anything on a line that says `%t10`, and a byte-for-byte
// substring test would read it as one. Registers in this emitter are `%` plus a run of word
// characters, so the character after a mention has to be neither.
func printLineMentions(line, reg string) bool {
	for i := 0; ; {
		j := strings.Index(line[i:], reg)
		if j < 0 {
			return false
		}
		at := i + j + len(reg)
		if at >= len(line) {
			return true
		}
		c := line[at]
		if !(c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '.') {
			return true
		}
		i = at
		if i >= len(line) {
			return false
		}
	}
}

// printWriteResultReg returns the register a line assigns its call's answer to, if it does.
func printWriteResultReg(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "%") {
		return "", false
	}
	eq := strings.Index(t, "=")
	if eq < 0 {
		return "", false
	}
	reg := strings.TrimSpace(t[:eq])
	if !strings.HasPrefix(reg, "%") || len(reg) < 2 {
		return "", false
	}
	if !strings.Contains(t, "call ") {
		return "", false
	}
	return reg, true
}

// deferPrintLineWrites performs the rewrite. It returns the text to emit, whether it differs from
// what was passed in, and the reason it declined when it did. The head of the region — everything
// the statement emitted before its first argument — travels along unchanged.
func deferPrintLineWrites(region string, marks []int) (string, bool, string) {
	if r := printWriteDeferralReason(region, marks); r != "" {
		return region, false, r
	}
	var kept, writes []string
	anyWrite := false
	for k := 0; k+1 < len(marks); k++ {
		for _, ln := range printWriteLines(region[marks[k]:marks[k+1]]) {
			if printWriteLine(ln) {
				writes = append(writes, ln)
				anyWrite = true
				continue
			}
			kept = append(kept, ln)
		}
	}
	if !anyWrite {
		return region, false, "the statement writes nothing"
	}
	out := region[:marks[0]] + strings.Join(kept, "") + strings.Join(writes, "")
	if out == region {
		return region, false, "the line is already written after every argument"
	}
	return out, true, ""
}
