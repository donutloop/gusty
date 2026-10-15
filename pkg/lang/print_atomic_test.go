package lang

import (
	"strings"
	"testing"
)

// print_atomic_test.go — a `print` statement writes its line as one unit, roadmap Gap L.5 (ADR 0355).
//
//	def twice(v):
//	    print("<<", v, ">>")
//	    return v + v
//
//	print("got", twice(21))     # CPython: "<< 21 >>" then "got 42"
//	                     # this backend, until now: "got << 21 >>" then "42"
//
// The callee's line arrived *inside* the caller's, because the argument loop wrote each argument as
// it lowered it. Python evaluates every argument and only then writes the line. Neither witness leg
// could see it — the retired engine wrote as it evaluated too, which is the whole reason ADR 0186
// built the reference leg for this corner of the corpus.
//
// The fix moves the writes, not the evaluations (`pkg/lang/print_atomic.go`). The two things this
// file exists to keep honest are the ordering, which only the reference leg can judge, and the gate,
// which decides whether a statement's writes may be moved at all: a write that sits inside a block
// the argument's own lowering opened belongs to that block, and hoisting it out would print a line
// on paths that never chose to print one. Refusing to reorder must therefore never become a wrong
// answer, so the shapes that stay interleaved are pinned below with their reason beside them.

const printAtomicCallee = `def twice(v):
    print("<<", v, ">>")
    return v + v
`

// printAtomicRows are programs the reference runs to completion; every `want` was read off `python3`
// before the case was written. An expectation copied from what the compiler does is not evidence —
// for this row it would be the bug wearing the test's clothes.
var printAtomicRows = []struct{ name, src, want string }{
	{
		"the row's own program",
		printAtomicCallee + "\nprint(\"got\", twice(21))\nprint(\"after\")\n",
		"<< 21 >>\ngot 42\nafter\n",
	},
	{
		"the callee is the first argument",
		printAtomicCallee + "\nprint(twice(21), \"got\")\n",
		"<< 21 >>\n42 got\n",
	},
	{
		"text between two printing callees",
		"def p(s):\n    print(\"[\", s, \"]\")\n    return s\n\nprint(\"a\", p(1), \"b\", p(2))\n",
		"[ 1 ]\n[ 2 ]\na 1 b 2\n",
	},
	{
		"a callee that answers None",
		"def emit():\n    print(\"side\")\n\nprint(\"got\", emit())\n",
		"side\ngot None\n",
	},
	{
		"a callee that answers a float",
		"def f():\n    print(\"f\")\n    return 2.5\n\nprint(\"v\", f())\n",
		"f\nv 2.5\n",
	},
	{
		"a callee that answers a list",
		"def mk():\n    print(\"mk\")\n    return [1, \"a\", None]\n\nprint(\"xs\", mk())\n",
		"mk\nxs [1, 'a', None]\n",
	},
	{
		"a callee that answers a dict",
		"def mk():\n    print(\"mk\")\n    return {\"a\": 1}\n\nprint(\"d\", mk())\n",
		"mk\nd {'a': 1}\n",
	},
	{
		"a callee that answers a set",
		"def mk():\n    print(\"mk\")\n    return {1, 2}\n\nprint(\"s\", mk())\n",
		"mk\ns {1, 2}\n",
	},
	{
		"a callee that answers a verdict",
		"def mk():\n    print(\"mk\")\n    return True\n\nprint(\"b\", mk())\n",
		"mk\nb True\n",
	},
	{
		"a callee that answers a text",
		"def mk():\n    print(\"mk\")\n    return \"hi\"\n\nprint(\"t\", mk())\n",
		"mk\nt hi\n",
	},
	{
		"sep and end travel with the line",
		printAtomicCallee + "\nprint(\"got\", twice(21), sep=\"-\", end=\"!\")\nprint(\"|done\")\n",
		"<< 21 >>\ngot-42!|done\n",
	},
	{
		"an f-string field over the printing call",
		printAtomicCallee + "\nprint(f\"v={twice(2)}\")\n",
		"<< 2 >>\nv=4\n",
	},
	{
		"a comprehension argument beside the callee",
		"def p():\n    print(\"side\")\n    return 9\n\nprint(\"c\", [x * 2 for x in range(3)], p())\n",
		"side\nc [0, 2, 4] 9\n",
	},
	{
		"a method that prints, between texts",
		"class C:\n    def __init__(self):\n        self.n = 1\n\n    def bump(self):\n        print(\"bumping\")\n        self.n = self.n + 1\n        return self.n\n\nprint(\"n =\", C().bump(), \"done\")\n",
		"bumping\nn = 2 done\n",
	},
	{
		"a nested print beside the caller's own text",
		"def inner(v):\n    print(\"<\", v, \">\")\n    return v\n\nprint(\"got\", \"mid\", inner(7))\n",
		"< 7 >\ngot mid 7\n",
	},
	{
		"the statement runs inside a loop",
		"def p(v):\n    print(\"S\", v)\n    return v * 2\n\nfor i in range(3):\n    print(\"i\", i, p(i))\n",
		"S 0\ni 0 0\nS 1\ni 1 2\nS 2\ni 2 4\n",
	},
	{
		"sorted() beside a printing callee",
		"def p():\n    print(\"side\")\n    return 1\n\nprint(\"s\", sorted([3, 1, 2]), p())\n",
		"side\ns [1, 2, 3] 1\n",
	},
	{
		"a slice argument beside a printing callee",
		"def p():\n    print(\"side\")\n    return 2\n\nxs = [1, 2, 3]\nprint(\"s\", xs[0:2], p())\n",
		"side\ns [1, 2] 2\n",
	},
	{
		"a tagged slot read beside a printing callee",
		"def p():\n    print(\"side\")\n    return 2\n\nxs = []\nxs.append(\"a\")\nprint(\"s\", xs[0], p())\n",
		"side\ns a 2\n",
	},
	{
		"a dict entry beside a printing callee",
		"def p():\n    print(\"side\")\n    return 2\n\nd = {\"a\": \"x\"}\nprint(\"d\", d[\"a\"], p())\n",
		"side\nd x 2\n",
	},
	{
		"a callee called from inside a loop body",
		"def row(i):\n    for j in range(2):\n        print(\"j\", j)\n    return i\n\nprint(\"r\", row(5))\n",
		"j 0\nj 1\nr 5\n",
	},
	{
		"several lines, each atomic, in order",
		"def p():\n    print(\"S\")\n    return 1\n\nprint(\"a\", p())\nprint(\"b\", p())\nprint(\"c\")\n",
		"S\na 1\nS\nb 1\nc\n",
	},
	{
		"the callee is the only argument",
		printAtomicCallee + "\nprint(twice(3))\n",
		"<< 3 >>\n6\n",
	},
	{
		"a print inside a function whose arguments print",
		"def p(v):\n    print(\"side\", v)\n    return v\n\ndef show():\n    print(\"got\", p(1), \"end\")\n\nshow()\n",
		"side 1\ngot 1 end\n",
	},
	{
		"two calls in one line, the second one prints",
		"def q(v):\n    return v + 1\n\ndef p():\n    print(\"S\")\n    return 1\n\nprint(\"a\", q(1), \"b\", p(), \"c\")\n",
		"S\na 2 b 1 c\n",
	},
}

// TestAPrintStatementWritesItsLineAfterEveryArgument is the row's answering half: the compiled leg
// prints what the reference prints, including where the callee's own line lands.
func TestAPrintStatementWritesItsLineAfterEveryArgument(t *testing.T) {
	t.Parallel()
	for _, tc := range printAtomicRows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := compiledOut(t, tc.src)
			if got != tc.want {
				t.Errorf("%s\n  compiled printed %q\n  the reference prints %q (roadmap Gap L.5: the line is written after the arguments)", tc.src, got, tc.want)
			}
		})
	}
}

// TestAPrintStatementAgreesWithTheReferenceOnBothLegs runs the same programs against CPython rather
// than against a remembered string, so a row cannot quietly become "what gusty does today".
func TestAPrintStatementAgreesWithTheReferenceOnBothLegs(t *testing.T) {
	t.Parallel()
	for _, tc := range printAtomicRows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want, err := pythonTwin(t, tc.src)
			if err != nil {
				t.Fatalf("the reference could not run the program: %v\n%s", err, tc.src)
			}
			if got := compiledOut(t, tc.src); got != want {
				t.Errorf("%s\n  compiled %q\n  CPython  %q", tc.src, got, want)
			}
		})
	}
}

// TestARecordedProgramThatPrintsFromAnArgumentStillAnswersTheRecord is the record leg. These sources
// are in `testdata/interpreter-golden.json`; three of them recorded the *interleaved* answer, and
// where the record and CPython disagree CPython is the language and the record is the debt (ADR 0302)
// — so the row is honest about which of its programs the retired engine answered wrongly.
func TestARecordedProgramThatPrintsFromAnArgumentStillAnswersTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"the callee's value is the only argument",
			"def greet(name):\n    print(\"hello\", name)\n    return 1\n\nprint(greet(\"ada\"))\n",
			"hello ada\n1\n",
		},
		{
			"a print statement whose argument is a method that prints",
			"def f(x, s):\n    x = x + 0.5\n    print(s)\n    return x\n\nprint(f(1.0, \"z\"))\n",
			"z\n1.5\n",
		},
		{
			"the operand a test skipped prints nothing",
			"def boom():\n    print(\"boom\")\n    return 9\n\nx = 0\nprint(x and boom())\n",
			"0\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg failed: %v", err)
			}
			if got != tc.want {
				t.Errorf("%s\n  record leg %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestAPrintStatementThatNeverReachesItsLineWritesNothing is the half of atomicity nobody asked for
// and every program that traps in an argument depends on: a line that is never completed is never
// written. The old emission had already written the leading arguments when the raise came, so the
// reader got half a line of program output ahead of the traceback — a line the reference never
// prints, at the exit code the reference reserves for the trap.
func TestAPrintStatementThatNeverReachesItsLineWritesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"the trap is in a later argument",
			"def boom():\n    return [1][5]\n\ntry:\n    print(\"partial\", boom())\nexcept IndexError:\n    print(\"caught\")\nprint(\"after\")\n",
			"caught\nafter\n",
		},
		{
			"the trap is in the middle of the list",
			"def boom():\n    return [1][5]\n\ntry:\n    print(\"a\", 1, \"b\", boom(), \"c\", 2)\nexcept IndexError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"the trap is in the middle of the list, in a missing dict key",
			"try:\n    print(\"a\", {\"k\": 1}[\"nope\"], \"b\")\nexcept KeyError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"the callee traps after it prints",
			"def boom():\n    print(\"side\")\n    return [1][5]\n\ntry:\n    print(\"a\", boom())\nexcept IndexError:\n    print(\"caught\")\n",
			"side\ncaught\n",
		},
		{
			"a line that completes after a caught trap writes its own line",
			"def boom():\n    return [1][5]\n\ntry:\n    print(\"x\", boom())\nexcept IndexError:\n    pass\nprint(\"y\", 1)\n",
			"y 1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s\n  compiled %q, want %q — a partial line is output the reference never produces", tc.src, got, tc.want)
			}
			want, err := pythonTwin(t, tc.src)
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			if got := compiledOut(t, tc.src); got != want {
				t.Errorf("%s\n  compiled %q, CPython %q", tc.src, got, want)
			}
		})
	}
}

// TestTheLineIsWrittenAfterTheArgumentsInTheModuleToo is the structural claim. Stdout can be right
// for a reason that is not this one, so the module is read: every write the statement emits sits
// after the last call the statement makes to a program function. A rewrite that dropped a print, and
// one that moved a print in front of the callee under a stdout test that happened not to look, both
// fail a row like this.
func TestTheLineIsWrittenAfterTheArgumentsInTheModuleToo(t *testing.T) {
	t.Parallel()
	ir := mustCompileIR(t, printAtomicCallee+"\nprint(\"got\", twice(21))\n")
	body := mainBodyOf(ir)
	callee := strings.LastIndex(body, "call i32 @gy_twice(")
	if callee < 0 {
		t.Fatalf("the module never called the program's own function:\n%s", body)
	}
	writes := 0
	offset := 0
	for _, line := range printWriteLines(body) {
		if printWriteLine(line) {
			writes++
			if offset < callee {
				t.Errorf("a write reaches the stream before the call that prints:\n  %s", strings.TrimRight(line, "\n"))
			}
		}
		offset += len(line)
	}
	// The line is four writes: the text, the separator, the answer, and the terminator the statement
	// owes even to a line that printed nothing else. The first three are the arguments'; the fourth
	// is written once, after them, which is the part a per-argument printer could not have got right.
	if writes != 4 {
		t.Errorf("the statement emitted %d writes, want 4 (\"got\", the separator, the answer, the terminator):\n%s", writes, body)
	}
	if n := strings.Count(body, "@printf("); n != 4 {
		t.Errorf("the line is %d printfs, want 4 — the rewrite is not preserving the statement's writes:\n%s", n, body)
	}
}

// TestTheGateIsAskedAsAFunction asks the decision directly, in both directions, over hand-built IR
// text: a straight-line argument list moves, and each shape that makes a move unsound stays where it
// was. This is the row's decision half — the one that keeps a widening of the whitelist from
// silently moving a conditional print out of its block (ADR 0273's rule about gates).
func TestTheGateIsAskedAsAFunction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		region string
		chunk  []int // how many IR lines each argument owns
		fake   []int // an argument list the test itself lays out, marks and all
		want   string
	}{
		{
			name: "a straight-line line moves",
			region: "  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1)\n" +
				"  %t2 = call i32 @gy_twice(i32 21)\n" +
				"  %t3 = call i32 (i8*, ...) @printf(i8* @.fmt2, i32 %t2)\n",
			chunk: []int{1, 2},
			want:  "",
		},
		{
			name: "a write inside a block the argument opened stays",
			region: "  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1)\n" +
				"  %t2 = icmp eq i32 %t0, 1\n" +
				"  br i1 %t2, label %yes, label %no\n" +
				"no:\n" +
				"  %t3 = call i32 (i8*, ...) @printf(i8* @.fmt2)\n" +
				"  br label %join\n" +
				"join:\n",
			chunk: []int{1, 6},
			want:  "an argument writes inside a block its own lowering opened",
		},
		{
			name: "a write whose answer the statement reads is an evaluation, not a write",
			region: "  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1)\n" +
				"  %t2 = add i32 %t1, 1\n",
			chunk: []int{1, 1},
			want:  "an argument's write returns a register the statement reads",
		},
		{
			name: "a write before an invoke stays where it is",
			region: "  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1)\n" +
				"  invoke i32 @gy_risk(i32 1)\n" +
				"    to label %ok unwind label %lpad\n",
			chunk: []int{3},
			want:  "an argument writes inside a block its own lowering opened",
		},
		{
			name: "a write after the invoke's own block is movable",
			region: "  invoke i32 @gy_risk(i32 1)\n" +
				"    to label %ok unwind label %lpad\n" +
				"ok:\n" +
				"  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1)\n",
			chunk: []int{3, 1},
			want:  "",
		},
		{
			name:   "a statement with no arguments is not the rewrite's business",
			region: "  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1)\n",
			chunk:  nil,
			want:   "the statement has no arguments",
		},
		{
			name:   "marks that lie outside the statement are refused",
			region: "  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1)\n",
			fake:   []int{0, 400, 2},
			want:   "an argument's emitted region is not inside the statement",
		},
		{
			name:   "marks that go backwards are refused",
			region: "  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1)\n",
			fake:   []int{0, 30, 10},
			want:   "the arguments did not emit in order",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			marks := tc.fake
			if marks == nil {
				marks = chunkMarks(tc.region, tc.chunk)
			}
			if got := printWriteDeferralReason(tc.region, marks); got != tc.want {
				t.Errorf("the gate answered %q, want %q\nregion:\n%s", got, tc.want, tc.region)
			}
		})
	}
}

// calleeOfLine returns the @-name a line calls, or "" when the line calls nothing. The callee sits
// after the last `call` keyword rather than at the first `@`, because a print line's operands name
// globals of their own (`@.fmt3`, `@.fmtdictkeys`) and the first `@` on the line is not the callee.
func calleeOfLine(line string) string {
	i := strings.LastIndex(line, "call ")
	if i < 0 {
		i = strings.LastIndex(line, "define ")
	}
	if i < 0 {
		return ""
	}
	at := strings.IndexByte(line[i:], '@')
	if at < 0 {
		return ""
	}
	end := i + at + 1
	for end < len(line) {
		c := line[end]
		if c == '(' || c == ' ' || c == ',' || c == ')' || c == '{' {
			break
		}
		end++
	}
	return line[i+at : end]
}

// chunkMarks turns "argument i owns these many IR lines" into the byte offsets the emitter would have
// recorded, so a hand-written region and a real one are asked the same question.
func chunkMarks(region string, chunk []int) []int {
	marks := []int{0}
	off := 0
	for _, n := range chunk {
		for i := 0; i < n && off < len(region); i++ {
			nl := strings.IndexByte(region[off:], '\n')
			if nl < 0 {
				off = len(region)
				break
			}
			off += nl + 1
		}
		marks = append(marks, off)
	}
	return marks
}

// TestTheRewritePreservesTheStatementIsThePreservationClaim: the rewritten statement holds the same
// instructions as the one the emitter wrote, each exactly once; the writes have only changed place,
// and they changed place to the end, in the order the line writes them.
func TestTheRewritePreservesTheStatement(t *testing.T) {
	t.Parallel()
	region := "  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1, i8* @.str1)\n" +
		"  %t2 = call i32 (i8*, ...) @printf(i8* @.fmt2)\n" +
		"  %t3 = call i32 @gy_twice(i32 21)\n" +
		"  %t4 = load i32, i32* @exn_flag\n" +
		"  %t5 = icmp eq i32 %t4, 1\n" +
		"  br i1 %t5, label %h, label %c\n" +
		"c:\n" +
		"  %t6 = call i32 (i8*, ...) @printf(i8* @.fmt3, i32 %t3)\n" +
		"  call void @rt_print_none(i32 0)\n"
	marks := chunkMarks(region, []int{2, 5, 2})
	out, moved, why := deferPrintLineWrites(region, marks)
	if !moved {
		t.Fatalf("the rewrite declined a straight-line statement: %s\n%s", why, region)
	}
	for _, line := range strings.Split(region, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Count(out, line) != strings.Count(region, line) {
			t.Errorf("line %q is not in the rewritten statement exactly once\n%s", line, out)
		}
	}
	var seenWrite bool
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if printWriteLine(ln) {
			seenWrite = true
			continue
		}
		if seenWrite {
			t.Errorf("an evaluation follows a write, so the writes are not all at the end:\n%s", out)
			break
		}
	}
	if i, j := strings.Index(out, "@.fmt1"), strings.Index(out, "@.fmt3"); i > j {
		t.Errorf("the separator's text precedes the answer's:\n%s", out)
	}
	if !strings.HasPrefix(out, region[:marks[0]]) {
		t.Errorf("the head of the region did not travel along unchanged:\n%s", out)
	}
}

// TestTheRewriteDeclinesWhenThereIsNothingToMove keeps the no-op honest: a statement whose line is
// already written after its arguments is emitted byte for byte the way it was before this row, which
// is what keeps every IR-shape guard in the package (the fold-purity rows, the line-table rows, the
// runtime-door rows) certifying the module they always certified.
func TestTheRewriteDeclinesWhenThereIsNothingToMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, region string
		chunk        []int
		want         string
	}{
		{
			name: "the writes are already last",
			region: "  %t1 = call i32 @gy_twice(i32 21)\n" +
				"  %t2 = call i32 (i8*, ...) @printf(i8* @.fmt1, i32 %t1)\n",
			chunk: []int{1, 1},
			want:  "the line is already written after every argument",
		},
		{
			name: "the statement writes nothing",
			region: "  %t1 = call i32 @gy_quiet(i32 1)\n" +
				"  store i32 %t1, i32* %_v\n",
			chunk: []int{1, 1},
			want:  "the statement writes nothing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, moved, why := deferPrintLineWrites(tc.region, chunkMarks(tc.region, tc.chunk))
			if moved {
				t.Errorf("the rewrite moved a statement that had nothing to move:\n%s", out)
			}
			if out != tc.region {
				t.Errorf("a declined rewrite changed the text anyway:\n%s\nwant\n%s", out, tc.region)
			}
			if why != tc.want {
				t.Errorf("why = %q, want %q", why, tc.want)
			}
		})
	}
}

// TestTheClassifierNamesAWriteAndABlock is the whitelist's own table. A printer added to the runtime
// without a row in `printWriteSinks` stays where the emitter put it — the pre-row behaviour, never a
// reordered line — and this table is what makes that a measured claim rather than a hope.
func TestTheClassifierNamesAWriteAndABlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line  string
		write bool
		block bool
	}{
		{"  %t1 = call i32 (i8*, ...) @printf(i8* @.fmt1, i32 1)", true, false},
		{"  call void @rt_print_list(i32 %h1, i32 0)", true, false},
		{"  call void @rt_print_list_mixed(i32 %h1, i32 0)", true, false},
		{"  call void @rt_print_mixed_value(i32 %t1, i32 3, i32 0)", true, false},
		{"  call void @rt_dict_print_s(i32 %h1, i32 0, i32 1, i32 1)", true, false},
		{"  call void @rt_set_print_str(i32 %h1, i32 0)", true, false},
		{"  call void @rt_out_txt(i8* @.fmtdictkeys)", true, false},
		{"  call void @rt_out_int(i32 3)", true, false},
		{"  call void @rt_print_none(i32 0)", true, false},
		{"  call void @rt_print_container_value(i32 %h1, i32 0)", true, false},
		// The doors the renderers are built from: text is computed and never put anywhere.
		{"  %s1 = call i32 @rt_str_of_value(i32 %t1, i32 2, i32 0)", false, false},
		{"  %p1 = call i8* @rt_str_ptr(i32 %s1)", false, false},
		{"  %d1 = call i8* @rt_fmt_double(double %f1)", false, false},
		{"  %b1 = call i8* @rt_bool_text(i32 %t1)", false, false},
		{"  %i1 = call i32 @rt_str_intern(i8* %p1)", false, false},
		// A raise belongs to the raise, and the REPL channel is not the program's stdout (ADR 0204).
		{"  call void @rt_die(i32 3, i8* @.msg1)", false, false},
		{"  call void @rt_echo_value(i32 %t1)", false, false},
		{"  call void @rt_echo_pair(i32 %t1, i32 %t2)", false, false},
		// An ordinary call, and a name that merely starts with a sink's name.
		{"  %t2 = call i32 @gy_twice(i32 21)", false, false},
		{"  %t3 = call i32 @rt_print_list_lengths(i32 1)", false, false},
		{"  %t4 = load i32, i32* @rt_print_list_count", false, false},
		{"  %t5 = getelementptr [4 x i8], [4 x i8]* @.printfmt, i32 0, i32 0", false, false},
		// Blocks and terminators.
		{"exn.cont1:", false, true},
		{"  br i1 %t5, label %h, label %c", false, true},
		{"  br label %join", false, true},
		{"  ret void", false, true},
		{"  ret i32 %t1", false, true},
		{"  switch i32 %k, label %d [ i32 1, label %a ]", false, true},
		{"  invoke i32 @gy_risk(i32 1)", false, true},
		{"  unreachable", false, true},
		{"  %t6 = add i32 %t1, 1", false, false},
		{"  %phi = phi i32 [ 0, %a ], [ 1, %b ]", false, false},
	} {
		t.Run(strings.TrimSpace(tc.line), func(t *testing.T) {
			t.Parallel()
			if got := printWriteLine(tc.line); got != tc.write {
				t.Errorf("printWriteLine(%q) = %v, want %v", tc.line, got, tc.write)
			}
			if got := printBlockLine(tc.line); got != tc.block {
				t.Errorf("printBlockLine(%q) = %v, want %v", tc.line, got, tc.block)
			}
		})
	}
}

// TestTheWhitelistNamesEveryPrinterTheRuntimeCarries keeps the runtime and the whitelist from being
// two lists: every renderer the module defines with a printing name is in the set, and the two that
// must never move are not.
func TestTheWhitelistNamesEveryPrinterTheRuntimeCarries(t *testing.T) {
	t.Parallel()
	ir := mustCompileIR(t, "print([1, \"a\"])\nprint({1: 2})\nprint({1, 2})\nprint(True)\nprint(None)\nprint(\"s\")\nprint(1.5)\n")
	seen := map[string]bool{}
	for _, line := range strings.Split(ir, "\n") {
		if !strings.HasPrefix(line, "define ") {
			continue
		}
		name := calleeOfLine(line)
		if name == "" {
			continue
		}
		trimmed := strings.TrimPrefix(name, "@")
		if !strings.HasPrefix(trimmed, "rt_") {
			continue
		}
		if !(strings.Contains(trimmed, "print") || strings.HasPrefix(trimmed, "rt_out_")) {
			continue
		}
		seen[trimmed] = true
		if !printWriteSinks[name] {
			t.Errorf("the runtime defines the renderer %s and the whitelist does not know it", name)
		}
	}
	for _, must := range []string{"rt_print_list", "rt_print_mixed_value", "rt_dict_print", "rt_out_txt"} {
		if !seen[must] {
			t.Errorf("the probe program was meant to drag %s into the module so the claim is measured", must)
		}
	}
	for _, never := range []string{"@rt_die", "@rt_echo_value", "@rt_echo_pair", "@rt_str_of_value", "@rt_fmt_double"} {
		if printWriteSinks[never] {
			t.Errorf("%s must never be a write: its output belongs to the raise or to a value, not to the line", never)
		}
	}
}

// TestARegisterMentionIsAWholeRegister is the guard on the guard: `%t1` is not a mention of the
// register on a line that says `%t10`, and a byte-for-byte substring test reads it as one. Without
// this, "the statement reads the write's answer" fires on any statement with ten printfs in it and
// the row silently stops reordering anything.
func TestARegisterMentionIsAWholeRegister(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line, reg string
		want      bool
	}{
		{"  %t10 = add i32 %t10, 1", "%t1", false},
		{"  %t1 = add i32 %t10, 1", "%t1", true},
		{"  %h12 = load i32, i32* %_xs", "%h12", true},
		{"  %h12 = load i32, i32* %_xs", "%h1", false},
		{"  call void @rt_print_list(i32 %h1, i32 0)", "%h1", true},
		{"  call void @rt_print_list(i32 %h11, i32 0)", "%h1", false},
		{"  %t2 = call i32 @gy_t(i32 1)", "%t", false},
		{"  store i32 %t12, i32* %_n", "%t12", true},
	} {
		t.Run(tc.reg+" on "+strings.TrimSpace(tc.line), func(t *testing.T) {
			t.Parallel()
			if got := printLineMentions(tc.line, tc.reg); got != tc.want {
				t.Errorf("printLineMentions(%q, %q) = %v, want %v", tc.line, tc.reg, got, tc.want)
			}
		})
	}
}

// TestWhatTheRewriteStillCannotReachIsFiledNotFixed pins the shapes the gate leaves interleaved, with
// the reason beside each. The gate exists because moving a conditional write is unsound; these are
// the language's own shapes, and they keep the ordering they always had rather than losing a line.
// What they owe is the union box's printer writing its line from a merge block instead of from each
// arm — that is L11.1's tagged value word, not this row's.
func TestWhatTheRewriteStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want, reason string
	}{
		{
			name:   "a union-annotated argument writes from inside the arm that runs",
			src:    "x: int | str = 1\ndef p():\n    print(\"S\")\n    return 2\n\nprint(\"a\", x, p())\n",
			want:   "a 1 S\n2\n",
			reason: "an argument writes inside a block its own lowering opened",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("%s\n  compiled %q, want %q — unchanged from before the row, because the gate declined (%s)", tc.src, got, tc.want, tc.reason)
			}
			got, err := pythonTwin(t, tc.src)
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			if got == tc.want {
				t.Errorf("the filed row is no longer a divergence — the gate now covers this shape; move it to the answer table")
			}
		})
	}
}
