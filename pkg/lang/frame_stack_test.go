package lang

// Frame-stack tests for Gap K.8 (ADR 0354): the pass that lays a shadow stack over an
// assembled module, the report the runtime prints from it, and the host that reads that report
// back.
//
// The behaviour lives in three places and each gets asked a different question:
//
//   - the pass (`attachFrameStack`) — does every call to a program function open a frame, close
//     it, and leave the module's *lines* where the line table expects them?
//   - the runtime block (`frameRuntimeIR`) — do the names it pushes read as the program wrote
//     them, and does a recursion deeper than the table truncate rather than corrupt?
//   - the host (`ParseTrapFrames`) — does it read what `rt_die` wrote and nothing else?
//
// What the report a real run produced is asserted in `integration/traceback_test.go`, against
// CPython (the reference leg); what the frames must never be is asserted here, where a stub is
// cheap: unbalanced pairs, an invented frame, a name from the module's innards.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// framePushNames returns the display names the shipped module's pushes can print, read out of
// the constants the pass minted rather than from the emitter's belief about them.
func framePushNames(t *testing.T, module string) []string {
	t.Helper()
	var out []string
	for _, ln := range strings.Split(module, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "@rt.frame.name.") || !strings.Contains(t, "c\"") {
			continue
		}
		name := t[strings.Index(t, "c\"")+len("c\""):]
		if i := strings.Index(name, "\\00"); i >= 0 {
			name = name[:i]
		}
		out = append(out, name)
	}
	return out
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// frameCalleePushes counts the pushes that open a *callee's* frame — the module frame is opened
// once per program and is the runtime's own bookkeeping, not a call the program wrote.
func frameCalleePushes(module string) int {
	pushes, _, moduleOpens, _ := frameStackBalance(module)
	return pushes - moduleOpens
}

// TestEveryProgramCallOpensAndClosesAFrame is the pass's own contract, taken from the artifact:
// a call to a function the program wrote gets a push before it and a pop after it, whoever wrote
// the call. The eleven roads that lower a call are not enumerated here on purpose — the pass is
// one door, and a program that reaches a def by a road nobody thought of is instrumented too.
func TestEveryProgramCallOpensAndClosesAFrame(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		pushes    int      // call sites the program wrote
		names     []string // frames the module must be able to name
	}{
		{
			name:   "a call two deep",
			src:    "def bar(x):\n  return x // 0\ndef foo(x):\n  return bar(x)\nfoo(10)\n",
			pushes: 3, // module→foo, foo→bar, and the <module> frame itself
			names:  []string{"<module>", "foo", "bar"},
		},
		{
			name:   "a method, a constructor and a free function",
			src:    "class C:\n  def __init__(self):\n    self.n = 0\n  def bump(self, k):\n    return self.n // k\ndef go(c, k):\n  return c.bump(k)\nc = C()\nprint(go(c, 0))\n",
			pushes: 4, // <module>, the constructor, go(…), and the dispatch into bump
			names:  []string{"<module>", "go", "bump", "__init__"},
		},
		{
			name:   "recursion",
			src:    "def down(n):\n  if n == 0:\n    return 1 // 0\n  return down(n - 1)\nprint(down(3))\n",
			pushes: 3, // <module>, the print's argument call, and the recursive call
			names:  []string{"<module>", "down"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mod := compileSrc(t, tc.src)
			pushes, pops, moduleOpens, ok := frameStackBalance(mod)
			if !ok {
				t.Fatalf("the shadow stack leaks: %d pushes, %d pops, %d of them the module frame\n%s", pushes, pops, moduleOpens, mod)
			}
			if pushes != tc.pushes {
				t.Errorf("the module carries %d push sites, the case expects %d (the module frame included): %v",
					pushes, tc.pushes, framePushNames(t, mod))
			}
			if pops != pushes-moduleOpens {
				t.Errorf("%d pops for %d pushes: a call site stopped closing the frame it opened", pops, pushes)
			}
			names := framePushNames(t, mod)
			for _, want := range tc.names {
				if !hasName(names, want) {
					t.Errorf("no frame can be named %q; the module names %v", want, names)
				}
			}
		})
	}
}

// TestTheFrameStackIsNotPaidByAProgramThatCannotTrap guards the gate. The pass runs on a module
// that can report a trap and on no other, so the programs that never print a traceback ship the
// module they always shipped — IR line numbers included, which is what ADR 0231's line-table
// guards and the fold-purity rows check. The three list programs are the interesting half: they
// can trap (`raiseUsed` is set by the conversion the container printer needs), so they open the
// `<module>` frame — but they call no function the program wrote, so no *callee* frame is opened
// and no call site pays for a pop.
func TestTheFrameStackIsNotPaidByAProgramThatCannotTrap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src        string
		calleeOpen int
	}{
		{"x = 41\nprint(x + 1)\n", 0},
		{"def twice(x):\n  return x + x\nprint(twice(21))\n", 0},
		{"total = 0\nfor i in range(5):\n  total = total + i\nprint(total)\n", 0},
		// The list printer can trap (an element whose kind it cannot render raises), so this module
		// does carry the frame doors — what it must not carry is a callee frame, because the
		// program called nothing.
		{"xs = [1, 2, 3]\nprint(xs)\n", 0},
	} {
		mod := compileSrc(t, tc.src)
		if n := frameCalleePushes(mod); n != tc.calleeOpen {
			t.Errorf("%q opens %d callee frames, want %d", tc.src, n, tc.calleeOpen)
		}
		if _, _, _, ok := frameStackBalance(mod); !ok {
			t.Errorf("%q leaks a frame: %s", tc.src, mod)
		}
		if frameStackCalls(mod) == 0 && strings.Contains(mod, "call void @rt_frame_here(") {
			t.Errorf("%q instruments a raise that can never happen", tc.src)
		}
	}
}

// TestTheFrameNamesWhatTheProgramWrote is the naming half: the IR symbol is what the call names
// and the frame is what the reader reads. A lifted closure is emitted as a trampoline and a body,
// a hoisted lambda carries the callable pass's infix, and a method is hung on its class — three
// shapes where the module's name is true of the module and false of the program.
func TestTheFrameNamesWhatTheProgramWrote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
		banned          []string
	}{
		{name: "a def", src: "def helper(x):\n  return x // 0\nprint(helper(1))\n", want: "helper"},
		{name: "a method says the method, the way the reference does", src: "class Counter:\n  def boom(self):\n    return 1 // 0\nc = Counter()\nprint(c.boom())\n", want: "boom", banned: []string{"Counter.boom", "Counter boom"}},
		{name: "a lifted closure loses the trampoline's suffix", src: "def outer(n):\n  def inner(k):\n    return k // n\n  return inner(1)\nprint(outer(0))\n", want: "inner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := framePushNames(t, compileSrc(t, tc.src))
			if !hasName(names, tc.want) {
				t.Fatalf("no frame named %q; the module can name %v", tc.want, names)
			}
			for _, bad := range tc.banned {
				if hasName(names, bad) {
					t.Errorf("a frame is named %q, which is the module's vocabulary and not the program's", bad)
				}
			}
		})
	}
}

// TestTheFrameStackKeepsTheLineTableOnTheRightLine is the offset discipline (ADR 0231). The pass
// inserts instructions into finished text; every `!dbg` record after an insertion points at a byte
// offset the insertion moved. A pass that forgot to shift would leave the table intact and wrong —
// every statement after the first call attributed to the line before it — which no verifier sees.
func TestTheFrameStackKeepsTheLineTableOnTheRightLine(t *testing.T) {
	t.Parallel()
	src := "def a(x):\n  return x // 0\n\ndef b(x):\n  y = a(x)\n  z = y + 1\n  return z\n\nprint(b(7))\nprint(b(8))\n"
	_, dbg := debugModuleOf(t, src, &DebugOptions{File: "frames.gy"})
	if dbg == nil {
		t.Fatal("debug requested and no table returned")
	}
	if dbg.Defect != "" {
		t.Fatalf("the module's metadata disagrees with what the compiler meant: %s", dbg.Defect)
	}
	if dbg.Tagged == 0 || dbg.Tagged != dbg.Instructions {
		t.Fatalf("%d of %d instructions carry a position after the pass inserted its own", dbg.Tagged, dbg.Instructions)
	}
	// Every line of the program is still reachable by line number, including the last one — the
	// statement a missing shift would silently move to the line above it.
	covered := map[int]bool{}
	for _, l := range dbg.SourceLineSet() {
		covered[l] = true
	}
	for _, want := range []int{2, 5, 6, 7, 9, 10} {
		if !covered[want] {
			t.Errorf("no emitted instruction is attributed to source line %d (lines covered: %v)", want, dbg.SourceLineSet())
		}
	}
	for _, row := range dbg.Lines {
		if row.Function == "" {
			t.Errorf("IR line %d names no function", row.IRLine)
		}
	}
}

// TestTheFrameStackPassIgnoresWhatIsNotAWholeInstruction is the pass's own safety rule: it may put
// instructions on both sides of a call only when the call is one instruction. Splitting one whose
// operand list is still open is the invalid module ADR 0166 refuses to ship.
func TestTheFrameStackPassIgnoresWhatIsNotAWholeInstruction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"call void @gy_foo(i32 %x)", true},
		{"%r = call i32 @gy_foo(i32 %x)", true},
		{"%r = call i32 @gy_foo(", false}, // the emitter is about to write the argument on its own line
		{"; call i32 @gy_foo(i32 1)", false},
		{"declare i32 @gy_foo(i32)", false},
		{"!20 = !{}, call i32 @gy_foo(i32 1)", false},
		{"%r = add i32 %a, %b", false},
	} {
		if got := frameIsCallLine(strings.TrimSpace(tc.line)); got != tc.want {
			t.Errorf("frameIsCallLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
	if got := frameCallee("  %r = call i32 @gy_outer(i32 %v)"); got != "gy_outer" {
		t.Errorf("frameCallee read %q, want gy_outer", got)
	}
	if got := frameDefineSym("define internal i32 @gy_outer(i32 %v) {"); got != "gy_outer" {
		t.Errorf("frameDefineSym read %q, want gy_outer", got)
	}
}

// TestFrameReaderNameStripsWhatTheProgramDidNotWrite checks the naming rule on its own, so a
// regression names the rule that broke rather than a program that happens to reach it.
func TestFrameReaderNameStripsWhatTheProgramDidNotWrite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"outer", "outer"},
		{"<module>", "<module>"},
		{"<global>", "<module>"},
		{"", "<module>"},
		{"Counter.bump", "bump"},           // the reference prints the method, not the class
		{"a.b.helper", "helper"},           // only what the reader typed last is the frame's name
		{"helper_impl", "helper"},          // the closure pass's body
		{"helper_apply", "helper"},         // …and its trampoline
		{"Counter.boom_impl", "boom"},      // both rewrites at once
		{"helper$lambda$abcd_0", "helper"}, // the callable pass's clone
		{"__init__", "__init__"},
		{"dotted.", "dotted."}, // a name whose dot has nothing after it is the name
	} {
		if got := frameReaderName(tc.in); got != tc.want {
			t.Errorf("frameReaderName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFrameNameGlobalStatesItsOwnLength is the lesson ADR 0171 learned the hard way: a frame
// constant whose array length disagrees with its bytes makes `llc` reject the program, and the
// first thing to trip it was a traceback frame, which contains a quote.
func TestFrameNameGlobalStatesItsOwnLength(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"<module>", "bump", `we"ird`, "tab\there", "unicode\u00e9", "back\\slash"} {
		decl := frameNameGlobal(sanitizeFrameSym(name), name)
		// [N x i8] must be len(text)+1 — the trailing NUL counted, the escapes irrelevant to it.
		// The last time this was hand-spelled, `llc` rejected a traceback frame for saying
		// `[31 x i8]` about 32 bytes (ADR 0171).
		declared, ok := frameDeclaredArrayLen(decl)
		if !ok {
			t.Fatalf("no array length in %q", decl)
		}
		wantLen := len(name)
		if declared != wantLen+1 {
			t.Errorf("%q declares [%d x i8], the name is %d bytes: %s", name, declared, wantLen, decl)
		}
		if !strings.HasSuffix(decl, "\\00\"\n") {
			t.Errorf("a frame constant that does not end in a NUL is not a C string: %s", decl)
		}
		if strings.Contains(decl, `c"`) && strings.Count(decl, `"`) != 2 && !strings.Contains(decl, "\\22") {
			t.Errorf("an unescaped quote in %s ends the LLVM literal early", decl)
		}
	}
}

// frameDeclaredArrayLen reads the `[N x i8]` a global declaration states about itself.
func frameDeclaredArrayLen(decl string) (int, bool) {
	i := strings.Index(decl, "[ ")
	if i < 0 {
		i = strings.Index(decl, "[")
	}
	if i < 0 {
		return 0, false
	}
	rest := strings.TrimSpace(decl[i+1:])
	j := strings.Index(rest, " ")
	if j < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:j]))
	return n, err == nil
}

// TestParseTrapFramesReadsWhatRtDieWrites — the host half. The frames are the target's to report
// and the host's to read back; the two must never tell two stories about one run (ADR 0214's rule
// for the class and message, extended to the stack by ADR 0354).
func TestParseTrapFramesReadsWhatRtDieWrites(t *testing.T) {
	t.Parallel()
	stderr := "Traceback (most recent call last):\n" +
		"  File \"prog\", line 7, in <module>\n" +
		"  File \"prog\", line 5, in outer\n" +
		"  File \"prog\", line 2, in inner\n" +
		"IndexError: list index out of range\n"
	got := ParseTrapFrames(stderr)
	want := []Frame{{Name: "<module>", Line: 7}, {Name: "outer", Line: 5}, {Name: "inner", Line: 2}}
	if len(got) != len(want) {
		t.Fatalf("read %v from %q, want %v", got, stderr, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d = %v, want %v", i, got[i], want[i])
		}
	}
	// Nothing that is not a frame becomes one: the header, the message, and the shapes a runtime
	// could emit that are not a frame line at all. A fabricated frame is worse than a missing one.
	for _, text := range []string{
		"",
		"Traceback (most recent call last):\n",
		"IndexError: list index out of range\n",
		"  File \"prog\", line ?, in mystery\n",
		"  File \"prog\"\n",
		"  File \"prog\", line 3\n",
		"File \"prog\", line x, in nope\n",
		"warning: File \"other\", line 1, in decoy\n", // not at the start of the line
	} {
		if f := ParseTrapFrames(text); len(f) != 0 {
			t.Errorf("ParseTrapFrames(%q) invented %v", text, f)
		}
	}
	// The rendering is the shape everyone already reads, and it renders the stack it was handed.
	rendered := (&TrapError{Msg: "list index out of range", ExnType: "IndexError", ExnMsg: "list index out of range", Traceback: got}).RenderTraceback()
	for _, wantLine := range []string{"Traceback (most recent call last):", `File "prog", line 7, in <module>`, `File "prog", line 2, in inner`, "IndexError: list index out of range"} {
		if !strings.Contains(rendered, wantLine) {
			t.Errorf("rendered traceback %q is missing %q", rendered, wantLine)
		}
	}
}

// TestATrapDeeperThanTheTableStillReportsItsException is the limit, said out loud: 64 frames. A
// recursion deeper than that prints the newest frames the window holds, collapsed the way the
// reference collapses a recursion — three copies and a count — and never drops the exception line.
// The run is taken raw (`runRecordedWith`) rather than through the record: this shape, a recursion
// deeper than the frame table, was never a program the retired engine was asked about, so the
// record has nothing to say about it and the report is the only claim here.
func TestATrapDeeperThanTheTableStillReportsItsException(t *testing.T) {
	t.Parallel()
	res, trapped, err := runRecordedWith(moduleSearch{stdlib: stdlibRoot()}, "def down(n):\n  if n == 0:\n    return 1 // 0\n  return down(n - 1)\nprint(down(120))\n")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !trapped {
		t.Fatalf("the program was meant to trap and answered %q", res.Output)
	}
	frames := ParseTrapFrames(res.Stderr)
	if len(frames) == 0 {
		t.Fatalf("no frame stack in %q", res.Stderr)
	}
	if len(frames) > frameStackDepth {
		t.Fatalf("the report printed %d frames from a %d-frame table", len(frames), frameStackDepth)
	}
	for _, f := range frames {
		if f.Name != "down" && f.Name != "<module>" {
			t.Errorf("a recursion reported a frame named %q", f.Name)
		}
	}
	if last := frames[len(frames)-1]; last.Line != 3 {
		t.Errorf("the innermost frame is line %d, want the raise's own line 3", last.Line)
	}
	// A 120-deep recursion is one fact, and the report says it once: three copies of the recursing
	// line and a count of the rest, which is the reference's shape and not an invention of ours.
	hidden := frameRepeatMarkerRe.FindStringSubmatch(res.Stderr)
	if hidden == nil {
		t.Fatalf("a 120-deep recursion reported %d frames with no collapse marker:\n%s", len(frames), res.Stderr)
	}
	count, cerr := strconv.Atoi(hidden[1])
	if cerr != nil {
		t.Fatalf("the collapse marker counts %q", hidden[1])
	}
	if count < frameStackDepth-8 {
		t.Errorf("the report hid %d repeated frames; a 120-deep recursion through a %d-frame window hides most of them",
			count, frameStackDepth)
	}
	if !strings.Contains(res.Stderr, "ZeroDivisionError") {
		t.Errorf("the exception line was lost while printing the frames: %q", res.Stderr)
	}
}

// frameRepeatMarkerRe is the reference's own sentence for a run of identical frames, and the
// number in it is what this backend spared the reader.
var frameRepeatMarkerRe = regexp.MustCompile(`\[Previous line repeated (\d+) more times\]`)
