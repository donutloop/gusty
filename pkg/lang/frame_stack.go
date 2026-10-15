package lang

// frame_stack.go — Gap K.8: the AOT frame stack.
//
// A compiled program has no unwinder to walk. By the time `rt_die` runs, every frame the
// raise travelled through has already `ret`urned, so the compiled report could name one
// frame — the raise site's own — and the roadmap row has said so ever since. That is the
// difference between
//
//	Traceback (most recent call last):
//	  File "prog", line 7, in <module>
//	  File "prog", line 5, in outer
//	  File "prog", line 2, in inner
//	IndexError: list index out of range
//
// and what a reader used to get: the last two lines and a guess. The reference names every
// frame; a report that names one is a report that blames the wrong line for a program whose
// bug is in the interaction of three.
//
// # The decision: a shadow stack the program keeps itself
//
// The alternative ADR 0231 hinted at was to read the frames back out of the artifact — walk
// what an unwinder can read. It cannot name them. A native backtrace gives `gy_outer` and
// `gy_Counter_bump` and the closure trampolines, not `outer` and `Counter.bump`; the lifted
// closure and the defunctionalised callable passes already rewrite which define a program's
// statement becomes, so the native stack is a different program's stack. And a DWARF walk
// needs `--debug`, which an ordinary run does not ask for: a trap is exactly when a reader
// wants the frames, whatever the build asked for.
//
// So the program carries its own stack, in three small doors and one copy:
//
//   - every call site to a program function pushes the callee's frame and, in the same store,
//     records the call's own line as the *caller's* currently-executing line — which is what
//     the reference shows for a frame that is not where the raise happened;
//   - the call site pops it when the call returns, unconditionally, because what gets printed
//     is the snapshot and not the live stack;
//   - the raise site records its own line in the frame it stands in and copies the live stack
//     out, and `rt_die` prints that copy.
//
// Popping at the call site rather than at the callee's exits is the whole reason this is a
// pass and not a hunt through every `ret`-emitting road in the emitter: a call site that
// forgets its pop costs one stale frame, never a corrupt answer, and a caught exception
// cannot leak a frame because the pop sits on the path every call returns by.
//
// # Where the pass runs, and why it is a pass
//
// The calls are inserted by one post-pass over the assembled module (`attachFrameStack`),
// not by each call-emitting road. Eleven roads call a program function — a `def`, a method,
// a `super()` call, a constructor, the dynamic-dispatch switch, a module function, a hoisted
// lambda, a closure `_impl`, the sort hook — and a feature whose correctness depends on
// remembering all eleven is a feature with a missing frame in it. The pass instead asks the
// module which defines belong to the program (the same record ADR 0231 keeps for the line
// table: a define is program code when the emitter registered a display name for it) and
// instruments every call to one, whoever wrote it.
//
// It runs only for a module that can raise: `raiseUsed` is set by `setExn`, the one door
// every raise — the program's `raise` and the machine-detected index, key and conversion
// traps alike — goes through. A program that can never trap gets the module it got before,
// byte for byte, which is what keeps ADR 0166's promise that this change does not touch the
// IR the fold-purity and line-table guards pin.
//
// The frame's line is the mark the statement already made for the line table: the pass reads
// the last `dbgMark` at or before the call, so the frame says the line the call is written on
// rather than the `def` above it (L8.5, ADR 0231 — the marks exist for exactly this question).
//
// # The limit, said out loud
//
// The stack is 64 frames deep. A recursion deeper than that prints the outermost 64 and the
// exception line — truncated, not wrong, and named in `docs/language.md` rather than
// discovered by a reader who needed the 65th frame.

import (
	"fmt"
	"sort"
	"strings"
)

// frameStackDepth is how many frames the shadow stack holds. Deeper recursion prints the
// frames the table can name; the exception line is never dropped for them.
const frameStackDepth = 64

// frameModuleFrame is the name the reference gives the program's top-level frame.
const frameModuleFrame = "<module>"

// frameRuntimeIR is the shadow stack itself: the two parallel tables, the depth counter, the
// snapshot the report prints from, and the four doors codegen calls. It travels with
// raiseRuntimeIR — one block, because rt_die reads the tables, and a raise-capable module is
// exactly the module that has both (ADR 0173: a referenced internal function that was never
// emitted is the invalid module, not the warning).
//
// It is assembled rather than written: the captions it prints are declared by Go, with the
// length counted from the Go string. An LLVM array literal has to state its own length, and the
// first time a traceback frame's was spelled by hand the module said `[31 x i8]` about 32 bytes
// and `llc` turned the program away (ADR 0171).
var frameRuntimeIR = frameRuntimeBlock()

// frameReportStrings are the captions the report writes, beside the frames themselves. The
// collapse pair is the reference's own wording for a recursion: three copies of a line and then
// `[Previous line repeated N more times]`, because a traceback of 200 identical frames answers
// one question 200 times.
var frameReportStrings = []struct{ name, text string }{
	{".rt.frames.pre", "  File \"prog\", line "},
	{".rt.frames.mid", ", in "},
	{".rt.frames.unk", "?"},
	{".rt.frames.rep", "  [Previous line repeated "},
	{".rt.frames.rep1", " more time]\n"},
	{".rt.frames.repn", " more times]\n"},
}

func frameRuntimeBlock() string {
	var b strings.Builder
	b.WriteString(frameRuntimeTables)
	for _, s := range frameReportStrings {
		b.WriteString(frameStrGlobal(s.name, s.text))
	}
	b.WriteString(frameRuntimeCode)
	return b.String()
}

// frameCaptionRefs is the substitution table the raise block uses to point at the captions this
// block declares: `%%PRE%%` becomes the getelementptr for `@.rt.frames.pre`, and so on.
//
// A getelementptr has to name the array type it indexes, so the length appears twice — once in the
// declaration and once at every use. That pair is what broke in ADR 0171, when a traceback frame's
// hand-typed `[31 x i8]` described 32 bytes and `llc` turned the program away with a nonsense
// array length. Both halves come out of one Go string here, so the pair cannot disagree again.
func frameCaptionRefs() []string {
	out := make([]string, 0, 2*len(frameReportStrings))
	for _, s := range frameReportStrings {
		token := "%%" + strings.ToUpper(strings.TrimPrefix(s.name, ".rt.frames.")) + "%%"
		ref := fmt.Sprintf("getelementptr inbounds [%[1]d x i8], [%[1]d x i8]* @%s, i32 0, i32 0", len(s.text)+1, s.name)
		out = append(out, token, ref)
	}
	return out
}

// frameRuntimeTables is the block's data: the live stack, the compacted copy the report prints,
// and the run bookkeeping the compaction keeps.
const frameRuntimeTables = `
; ── the AOT frame stack (roadmap Gap K.8, ADR 0354) ────────────────────────
; Two parallel tables plus the depth. A frame is (the function the program wrote, the line it
; is currently on); the pair is written at a call site and read at a raise.
@rt.frames.name = internal global [64 x i8*] zeroinitializer
@rt.frames.line = internal global [64 x i32] zeroinitializer
@rt.frames.depth = internal global i32 0
; The copy the report prints, already collapsed the way the reference collapses a recursion:
; three copies of a frame and a count of the ones that were hidden. The live stack is empty by
; the time rt_die runs, because every frame the raise travelled through has returned by then.
@rt.frames.tb.name = internal global [64 x i8*] zeroinitializer
@rt.frames.tb.line = internal global [64 x i32] zeroinitializer
@rt.frames.tb.coll = internal global [64 x i32] zeroinitializer
@rt.frames.tb.n = internal global i32 0
@rt.frames.prev.name = internal global i8* zeroinitializer
@rt.frames.prev.line = internal global i32 0
@rt.frames.runlen = internal global i32 0
@rt.frames.dig = internal global [16 x i8] zeroinitializer
`

// frameRuntimeCode is the block's code: the four doors, and the digits helper they print through.
const frameRuntimeCode = `
; Open a frame for a call: the caller's currently-executing line becomes the line the call is
; written on (that is what the reference shows for a frame that is not where the raise
; happened), and the callee's own line stays unknown until it either calls or raises.
;
; A table that is already full slides the oldest frame out rather than dropping the new one. A
; report wants the newest frames: the path a raise travelled is read from the inside out, and a
; 200-deep recursion whose 64 frames are the outermost is a traceback of the module frame calling itself.
; The reference has the same bottom frame and a real unwinder for it; this table is a window,
; and docs/language.md says so.
define internal void @rt_frame_push(i8* %name, i32 %line) {
entry:
  %d = load i32, i32* @rt.frames.depth
  %full = icmp sge i32 %d, 64
  br i1 %full, label %slide, label %parent
slide:
  %i = phi i32 [ 1, %entry ], [ %nx, %slide ]
  %im = sub i32 %i, 1
  %from = getelementptr inbounds [64 x i8*], [64 x i8*]* @rt.frames.name, i32 0, i32 %i
  %who = load i8*, i8** %from
  %to = getelementptr inbounds [64 x i8*], [64 x i8*]* @rt.frames.name, i32 0, i32 %im
  store i8* %who, i8** %to
  %lfrom = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.line, i32 0, i32 %i
  %num = load i32, i32* %lfrom
  %lto = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.line, i32 0, i32 %im
  store i32 %num, i32* %lto
  %nx = add i32 %i, 1
  %more = icmp slt i32 %nx, 64
  br i1 %more, label %slide, label %slid
slid:
  %short = sub i32 %d, 1
  br label %parent
parent:
  %cur = phi i32 [ %d, %entry ], [ %short, %slid ]
  %hasparent = icmp sgt i32 %cur, 0
  %known = icmp sgt i32 %line, 0
  %both = and i1 %hasparent, %known
  %pi = sub i32 %cur, 1
  br i1 %both, label %setparent, label %slot
setparent:
  %pl = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.line, i32 0, i32 %pi
  store i32 %line, i32* %pl
  br label %slot
slot:
  %np = getelementptr inbounds [64 x i8*], [64 x i8*]* @rt.frames.name, i32 0, i32 %cur
  store i8* %name, i8** %np
  %lp = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.line, i32 0, i32 %cur
  store i32 0, i32* %lp
  %nd = add i32 %cur, 1
  store i32 %nd, i32* @rt.frames.depth
  ret void
}

; Close the frame. The depth never goes below zero, so a module whose pairs do not balance
; prints a shorter traceback rather than walking off the front of the table.
define internal void @rt_frame_pop() {
entry:
  %d = load i32, i32* @rt.frames.depth
  %down = icmp sgt i32 %d, 0
  br i1 %down, label %dec, label %done
dec:
  %nd = sub i32 %d, 1
  store i32 %nd, i32* @rt.frames.depth
  br label %done
done:
  ret void
}

; The raise site's own line, in the frame the raise stands in. A line of zero says the
; statement had no position (Gap K.6), and leaves whatever the frame already knew alone — the
; report would rather show the call that got here than a line that never existed.
define internal void @rt_frame_here(i32 %line) {
entry:
  %known = icmp sgt i32 %line, 0
  br i1 %known, label %have, label %done
have:
  %d = load i32, i32* @rt.frames.depth
  %has = icmp sgt i32 %d, 0
  %top = sub i32 %d, 1
  %fits = icmp slt i32 %top, 64
  %ok = and i1 %has, %fits
  br i1 %ok, label %wrt, label %done
wrt:
  %p = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.line, i32 0, i32 %top
  store i32 %line, i32* %p
  br label %done
done:
  ret void
}

; Copy the live stack out, at the raise, and collapse it as the reference collapses it: three
; copies of a frame that repeats itself, then a count of the ones the reader was spared. This is
; done here rather than in the printer because the decision is about the *stack*, not its layout:
; a run of identical frames is one fact, and a 200-deep recursion that prints 200 lines says it
; once and says it 200 times.
;
; This is also why the pop at the call site can be unconditional: a caught exception emptying the
; live stack cannot take the frames away from the report of the raise being handled, and an
; uncaught one prints the path the exception actually travelled.
define internal void @rt_frames_snapshot() {
entry:
  %d = load i32, i32* @rt.frames.depth
  %deep = icmp sgt i32 %d, 64
  %cap = select i1 %deep, i32 64, i32 %d
  store i32 0, i32* @rt.frames.tb.n
  store i8* null, i8** @rt.frames.prev.name
  store i32 -1, i32* @rt.frames.prev.line
  store i32 0, i32* @rt.frames.runlen
  br label %walk
walk:
  %i = phi i32 [ 0, %entry ], [ %next, %after ]
  %kept = phi i32 [ 0, %entry ], [ %written, %after ]
  %more = icmp slt i32 %i, %cap
  br i1 %more, label %read, label %done
read:
  %np = getelementptr inbounds [64 x i8*], [64 x i8*]* @rt.frames.name, i32 0, i32 %i
  %nm = load i8*, i8** %np
  %lp = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.line, i32 0, i32 %i
  %lv = load i32, i32* %lp
  %pn = load i8*, i8** @rt.frames.prev.name
  %pl = load i32, i32* @rt.frames.prev.line
  %samename = icmp eq i8* %nm, %pn
  %sameline = icmp eq i32 %lv, %pl
  %same = and i1 %samename, %sameline
  %seen = load i32, i32* @rt.frames.runlen
  %again = add i32 %seen, 1
  %next = add i32 %i, 1
  br i1 %same, label %repeat, label %fresh
repeat:
  store i32 %again, i32* @rt.frames.runlen
  %show = icmp sle i32 %again, 3
  br i1 %show, label %copy, label %hide
fresh:
  store i8* %nm, i8** @rt.frames.prev.name
  store i32 %lv, i32* @rt.frames.prev.line
  store i32 1, i32* @rt.frames.runlen
  br label %copy
copy:
  %fits = icmp slt i32 %kept, 64
  br i1 %fits, label %put, label %after
put:
  %tn = getelementptr inbounds [64 x i8*], [64 x i8*]* @rt.frames.tb.name, i32 0, i32 %kept
  store i8* %nm, i8** %tn
  %tl = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.tb.line, i32 0, i32 %kept
  store i32 %lv, i32* %tl
  %tc = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.tb.coll, i32 0, i32 %kept
  store i32 0, i32* %tc
  %grown = add i32 %kept, 1
  br label %after
hide:
  ; The frame is the one before this in the report, and it is one more of whatever it was.
  %hi = sub i32 %kept, 1
  %hp = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.tb.coll, i32 0, i32 %hi
  %hc = load i32, i32* %hp
  %hn = add i32 %hc, 1
  store i32 %hn, i32* %hp
  br label %after
after:
  %written = phi i32 [ %grown, %put ], [ %kept, %copy ], [ %kept, %hide ]
  br label %walk
done:
  %shown = phi i32 [ %kept, %walk ]
  store i32 %shown, i32* @rt.frames.tb.n
  ret void
}

; The line number, as digits, in a buffer the caller writes straight to fd 2. The runtime
; already refuses to pull a printf into the raise block for the sake of one %d.
define internal i8* @rt_frame_digits(i32 %vin) {
entry:
  %neg = icmp slt i32 %vin, 0
  %v = select i1 %neg, i32 0, i32 %vin
  %nul = getelementptr inbounds [16 x i8], [16 x i8]* @rt.frames.dig, i32 0, i32 15
  store i8 0, i8* %nul
  br label %fill
fill:
  %i = phi i32 [ 14, %entry ], [ %down, %fill ]
  %x = phi i32 [ %v, %entry ], [ %q, %fill ]
  %dig = urem i32 %x, 10
  %q = udiv i32 %x, 10
  %co = add i32 %dig, 48
  %c8 = trunc i32 %co to i8
  %p = getelementptr inbounds [16 x i8], [16 x i8]* @rt.frames.dig, i32 0, i32 %i
  store i8 %c8, i8* %p
  %down = sub i32 %i, 1
  %more = icmp ne i32 %q, 0
  br i1 %more, label %fill, label %shown
shown:
  %first = phi i32 [ %i, %fill ]
  %fp = getelementptr inbounds [16 x i8], [16 x i8]* @rt.frames.dig, i32 0, i32 %first
  ret i8* %fp
}
`

// frameStrGlobal declares one C string constant, its array length counted from the Go string and
// its bytes escaped for an LLVM literal. Everything the report prints goes through it, so no
// caption's length is ever hand-stated again (ADR 0171).
func frameStrGlobal(name, text string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "@%s = private unnamed_addr constant [%d x i8] c\"", name, len(text)+1)
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '"':
			b.WriteString("\\22")
		case c == '\\':
			b.WriteString("\\5C")
		case c == '\n':
			b.WriteString("\\0A")
		case c == '\t':
			b.WriteString("\\09")
		case c == '\r':
			b.WriteString("\\0D")
		case c < 0x20 || c > 0x7e:
			fmt.Fprintf(&b, "\\%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("\\00\"\n")
	return b.String()
}

// frameNameGlobal mints one private constant for a frame's name, keyed by `sym` so two
// frames whose display names sanitise alike still get two globals. The length is counted
// from the Go string rather than typed: the last time a traceback frame's constant was
// spelled out by hand the module said `[31 x i8]` for a 32-byte string and `llc` rejected
// the program.
func frameNameGlobal(sym, text string) string {
	return frameStrGlobal("rt.frame.name."+sym, text)
}

// attachFrameStack lays the shadow frame stack over an assembled module: every call to a
// program function gets a push before it and a pop after it, `main` opens the `<module>`
// frame, and the constants the pushes name the frames by join the module's globals.
//
// It returns the new text, the statement marks *shifted* by what was inserted, and how many
// call sites were instrumented. The shift is not decoration: attachDebugInfo resolves every
// `!dbg` record against these offsets (ADR 0231), and a pass that inserted lines without
// moving the marks it shares would attribute each statement after the first call to the line
// before it.
//
// `funcs` is the emitter's own record of which defines are program code — the same list the
// line table is built from, so the two can never disagree about what the program wrote.
func attachFrameStack(text string, marks []dbgMark, funcs []dbgFunc, globalsPrefix, bodyPrefix int) (string, []dbgMark, int) {
	names := frameDisplayNames(funcs)
	if len(names) == 0 || text == "" {
		return text, marks, 0
	}
	// Resolve a position in the assembled module to the source line of the statement whose IR
	// was being written there. The marks are builder-relative; the same two prefixes
	// attachDebugInfo uses make them absolute.
	resolvable := make([]dbgMark, 0, len(marks))
	for _, m := range marks {
		if m.line <= 0 {
			continue
		}
		m.off = frameAbsOffset(m, globalsPrefix, bodyPrefix)
		resolvable = append(resolvable, m)
	}
	sort.SliceStable(resolvable, func(i, j int) bool { return resolvable[i].off < resolvable[j].off })
	lineAt := func(off int) int {
		i := sort.Search(len(resolvable), func(k int) bool { return resolvable[k].off > off })
		if i == 0 {
			return 0
		}
		return resolvable[i-1].line
	}

	// Names are interned per display name: the eleven roads that call the same `def` share one
	// constant, the way the string table shares one entry per text.
	constants := map[string]string{} // display name -> its IR constant
	globals := map[string]string{}   // display name -> its global name
	used := map[string]bool{}        // display names a pushed frame actually named
	mint := func(display string) string {
		if g, ok := globals[display]; ok {
			used[display] = true
			return g
		}
		g := "@rt.frame.name." + sanitizeFrameSym(display)
		for i := 1; constants[g] != ""; i++ {
			g = fmt.Sprintf("@rt.frame.name.%s.%d", sanitizeFrameSym(display), i)
		}
		globals[display] = g
		constants[display] = frameNameGlobal(strings.TrimPrefix(g, "@rt.frame.name."), display)
		used[display] = true
		return g
	}

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+32)
	sites := 0
	// Where the pass inserted, in the *original* text, and how many bytes: the line table
	// resolves its `!dbg` records against the shipped offsets, so every mark at or after an
	// insertion has to move by the bytes that went in before it.
	type insertion struct{ at, n int }
	var inserts []insertion
	// The `<module>` push goes *after* `main`'s entry label, not before it: a label must be the
	// first thing in a block, so an instruction wedged between `define` and `entry:` is the
	// parse error `llc` reports as exit 2.
	wantModulePush := false
	inDefine := ""
	off := 0
	emit := func(s string) {
		out = append(out, s)
		inserts = append(inserts, insertion{at: off, n: len(s) + 1})
	}
	appendLine := func(ln string) {
		out = append(out, ln)
		off += len(ln) + 1
		trim := strings.TrimSpace(ln)
		if wantModulePush && trim != "" && !strings.HasPrefix(trim, ";") {
			emit("  call void @rt_frame_push(i8* " + mint(frameModuleFrame) + ", i32 0)")
			wantModulePush = false
		}
	}
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "define ") {
			inDefine = frameDefineSym(trim)
			// The define line goes out as it stands: the `<module>` push belongs after the entry
			// label that follows it, and an instruction between the brace and the label is not IR.
			out = append(out, ln)
			off += len(ln) + 1
			if inDefine == "main" {
				wantModulePush = true
			}
			continue
		}
		if trim == "}" {
			inDefine = ""
		}
		if inDefine != "" && frameIsCallLine(trim) {
			if callee := frameCallee(trim); callee != "" {
				if _, ok := names[callee]; ok {
					emit(fmt.Sprintf("  call void @rt_frame_push(i8* %s, i32 %d)", mint(calleeDisplay(names, callee)), lineAt(off)))
					out = append(out, ln)
					off += len(ln) + 1
					emit("  call void @rt_frame_pop()")
					sites++
					continue
				}
			}
		}
		appendLine(ln)
	}

	// The names the pushes call the frames by join the module's globals. The runtime block
	// itself came in beside the raise block, so a module that has pushes always has the doors
	// they call, and a module with no pushes has neither (ADR 0173).
	//
	// The order is sorted, not map order: two compiles of one source must produce one module,
	// and the guard that checks it (`TestThePassRewritessTwiceToTheSameBytes`) is exactly the
	// kind of case that a map iteration would fail — intermittently, on the shard that ran the
	// two compiles back to back.
	shown := make([]string, 0, len(used))
	for display := range used {
		shown = append(shown, display)
	}
	sort.Strings(shown)
	var added strings.Builder
	for _, display := range shown {
		added.WriteString(constants[display])
	}
	if sites == 0 && added.Len() == 0 {
		return text, marks, 0
	}
	added.WriteString("\n")
	newText := strings.Join(out, "\n")
	insertAt := len(newText)
	if i := strings.Index(newText, "!llvm.module.flags"); i >= 0 {
		insertAt = i
	}
	newText = newText[:insertAt] + added.String() + newText[insertAt:]
	moved := make([]dbgMark, len(marks))
	for i, m := range marks {
		abs := frameAbsOffset(m, globalsPrefix, bodyPrefix)
		for _, ins := range inserts {
			if ins.at <= abs {
				abs += ins.n
			}
		}
		if abs >= insertAt {
			abs += added.Len()
		}
		if m.inGlobals {
			m.off = abs - globalsPrefix
		} else {
			m.off = abs - bodyPrefix
		}
		moved[i] = m
	}
	return newText, moved, sites
}

// frameAbsOffset turns a mark's builder-relative offset into a position in the assembled
// module, with the same two prefixes attachDebugInfo uses (ADR 0231): the two builders' text
// becomes one address space here, and both passes have to agree about where.
func frameAbsOffset(m dbgMark, globalsPrefix, bodyPrefix int) int {
	if m.inGlobals {
		return globalsPrefix + m.off
	}
	return bodyPrefix + m.off
}

// calleeDisplay answers what the report calls the function a call names.
func calleeDisplay(names map[string]string, sym string) string {
	if n, ok := names[sym]; ok {
		return n
	}
	return sym
}

// frameDisplayNames maps each define the emitter registered to the name the *program* wrote
// for it — `outer`, `Counter.bump`, `<module>` — which is what a traceback owes the reader.
// The IR symbol is what the call names; the display name is what the report prints.
func frameDisplayNames(funcs []dbgFunc) map[string]string {
	out := map[string]string{}
	for _, f := range funcs {
		if f.sym == "" {
			continue
		}
		name := f.name
		if f.sym == "main" {
			name = frameModuleFrame
		}
		if name == "" {
			name = f.sym
		}
		out[f.sym] = frameReaderName(name)
	}
	return out
}

// frameReaderName strips what the reader did not write. A lifted closure is emitted twice — an
// `_apply` trampoline and an `_impl` body — and a lambda clone carries this pass's infix; the
// native define names are true of the module and false of the program, in the same way a
// clone's symbol is (ADR 0320's rule for a rewritten definition, applied to the frame).
func frameReaderName(name string) string {
	if base := callableGeneratedBase(name); base != "" {
		name = base
	}
	for _, thunk := range []string{"_impl", "_apply"} {
		if strings.HasSuffix(name, thunk) && len(name) > len(thunk) {
			name = strings.TrimSuffix(name, thunk)
		}
	}
	// A method's frame names the method and not the class it hangs on: the reference writes
	// `File "prog", line 3, in bump`, and a frame that said `Counter.bump` is one no grep of a
	// Python-shaped log, no `except`-driven tool and no CPython comparison could match — the
	// reference leg of AGENTS' two witnesses is the definition here, and the record is silent
	// about frames (it stores a trap's class and message, never its stack). The qualified name is
	// not lost: it is what the line table's DISubprogram says (ADR 0231), where a debugger — not a
	// traceback reader — is the audience.
	if i := strings.LastIndexByte(name, '.'); i >= 0 && i+1 < len(name) {
		name = name[i+1:]
	}
	if name == "" || name == "<global>" {
		return "<module>"
	}
	return name
}

// frameDefineSym pulls the symbol out of a `define` line.
func frameDefineSym(trim string) string {
	i := strings.Index(trim, "@")
	if i < 0 {
		return ""
	}
	rest := trim[i+1:]
	if j := strings.IndexAny(rest, "( "); j >= 0 {
		return rest[:j]
	}
	return rest
}

// frameCallee pulls the callee symbol out of a call instruction.
func frameCallee(trim string) string {
	i := strings.Index(trim, "@")
	if i < 0 {
		return ""
	}
	rest := trim[i+1:]
	if j := strings.IndexAny(rest, "( "); j >= 0 {
		return rest[:j]
	}
	return rest
}

// frameIsCallLine reports whether a line is a whole call instruction — one the pass may put
// instructions on both sides of. A call whose operand list is still open (a road that wrote
// `%r = call i32 @f(` and is about to emit an argument on its own line) is not a whole
// instruction, and splitting one is the invalid module ADR 0166 refuses.
func frameIsCallLine(trim string) bool {
	if strings.HasPrefix(trim, ";") || strings.HasPrefix(trim, "!") || strings.HasPrefix(trim, "declare") {
		return false
	}
	if !strings.HasPrefix(trim, "call ") && !strings.Contains(trim, " = call ") {
		return false
	}
	return strings.HasSuffix(trim, ")")
}

// sanitizeFrameSym turns a display name into a global-name fragment. The name only has to be
// unique and printable, but a frame named `Counter.bump` shares the module with the C
// identifiers libc provides, so the punctuation goes.
func sanitizeFrameSym(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "frame"
	}
	return b.String()
}

// frameStackCalls reports how many push sites a shipped module carries. It exists so a test
// can ask the module — rather than the emitter's own belief — whether a program's calls are
// instrumented (ADR 0164's rule: a claim about an artifact is taken from the artifact).
func frameStackCalls(module string) int {
	return strings.Count(module, "call void @rt_frame_push(")
}

// frameStackBalance reports what a shipped module's push/pop pairs add up to: how many push
// sites, how many pop sites, and how many of the pushes open the `<module>` frame. It is
// balanced when every push but those has a pop beside it.
//
// The module frame is the one frame a program never closes: `main` hands back to the C runtime
// rather than returning to a call site that could pop it, and the reference's traceback has the
// same permanent frame at its bottom. A leak is every *other* unclosed frame — one stale call in
// the next traceback of the same run, which is what this asks the artifact about (ADR 0164).
func frameStackBalance(module string) (pushes, pops, moduleOpens int, ok bool) {
	pushes = strings.Count(module, "call void @rt_frame_push(")
	pops = strings.Count(module, "call void @rt_frame_pop()")
	// Which global the `<module>` frame is named by is read out of the module rather than
	// reconstructed: the pass mints `@rt.frame.name.<sanitised>`, and a program whose own
	// function sanitises to the same fragment makes the minting start a `.1` suffix.
	for _, ln := range strings.Split(module, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "@rt.frame.name.") || !strings.Contains(t, `c"`+frameModuleFrame+`\00"`) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(t, " ", 2)[0], "@"), "")
		moduleOpens += strings.Count(module, "call void @rt_frame_push(i8* @"+name+", ")
	}
	return pushes, pops, moduleOpens, pushes-pops == moduleOpens
}
