package lang

import (
	"encoding/json"
	"strings"
	"testing"
)

// Roadmap Gap R.113 / ADR 0406 — the arm that NAMES what it caught.
//
// `except ZeroDivisionError as err:` is the single most common exception idiom in the language the
// reference defines, and this backend could not read it: the grammar stopped after the type name, so
//
//	try:
//	    1/0
//	except ZeroDivisionError as err:
//	    print(err)
//
// died with `parse error at 3:26: expected ":"` and exit 1 — on every path, since the arm's name was
// never a token the parser accepted. That is the unimplemented-surface face of ADR 0166: the program is
// ordinary, the reference answers it, and the toolchain exits 1 on a program nobody asked it to judge.
//
// What the row decided: the reference hands the arm an exception OBJECT, and this backend's value is one
// word wide, so the name is bound to what that object WRITES — the message the raise carried, interned
// into @str_tab by the one interning door every other text uses. That choice is what makes the family
// agree: `print(err)`, `str(err)`, `f"{err}"`, `return err` and the print of that return all say
// `division by zero`, because all of them read the same record the binding wrote.
//
// The record it writes is `caughtVars`, deliberately NOT `internedVars`. The print roads ask
// `printsAsInternedStr` and are told yes; the operand roads ask `exprIsString` and are told no, which is
// the split the reference itself draws — an exception prints like its message and is not a string to
// `len`, to `.upper()`, to `+` or to `==`. Those doors are refused in words, and the object the reference
// still answers about there (its type name, its args, its repr, and the name's life after the arm) is
// roadmap Gap R.395.

// exceptAsAnswered is the family this row answers: every program prints the reference's own bytes, judged
// on the record leg (the golden's stdout) and on the compiled leg (the module built and run).
func exceptAsAnswered() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"the row's own program", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err)\n", "division by zero\n"},
		{"a ValueError the builtin raises", "try:\n    int(\"abc\")\nexcept ValueError as err:\n    print(err)\n", "invalid literal for int() with base 10: 'abc'\n"},
		{"the control: an arm that never runs", "try:\n    print(1)\nexcept ValueError as err:\n    print(\"not taken\")\n", "1\n"},
		{"a machine raise names itself", "try:\n    xs = [1]\n    print(xs[9])\nexcept IndexError as err:\n    print(err)\n", "list index out of range\n"},
		{"a missing key says which key", "try:\n    d = {}\n    print(d[\"k\"])\nexcept KeyError as err:\n    print(err)\n", "'k'\n"},
		{"a raise that carried a message", "try:\n    raise ValueError(\"boom\")\nexcept ValueError as err:\n    print(err)\n", "boom\n"},
		{"a message that contains a colon", "try:\n    raise ValueError(\"a: b\")\nexcept ValueError as err:\n    print(err)\n", "a: b\n"},
		{"a raise that carried nothing", "try:\n    raise ValueError\nexcept ValueError as err:\n    print(\"[\" + str(err) + \"]\")\n", "[]\n"},
		{"the name inside an interpolation", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(f\"got: {err}\")\n", "got: division by zero\n"},
		{"the program continues after the arm", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err)\nprint(\"after\")\n", "division by zero\nafter\n"},
		{"the arm returns the name", "def f():\n    try:\n        1/0\n    except ZeroDivisionError as err:\n        return err\nprint(f())\n", "division by zero\n"},
		{"a stale constant of the same name", "err = \"stale\"\ntry:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err)\n", "division by zero\n"},
		{"the second arm is the one that matches", "try:\n    1/0\nexcept TypeError as te:\n    print(\"te\")\nexcept ZeroDivisionError as ze:\n    print(ze)\n", "division by zero\n"},
		{"the catch-all arm names it too", "try:\n    1/0\nexcept Exception as err:\n    print(err)\n", "division by zero\n"},
		{"the name beside another argument", "try:\n    xs = [1]\n    print(xs[9])\nexcept IndexError as err:\n    print(\"caught\", err, \"end\")\n", "caught list index out of range end\n"},
		{"the arm calls, then reads the name", "def h():\n    print(\"arm\")\ntry:\n    xs = []\n    print(xs[3])\nexcept IndexError as err:\n    h()\n    print(\"after\", err)\n", "arm\nafter list index out of range\n"},
		{"an arm that re-raises for the outer arm", "try:\n    try:\n        1/0\n    except ZeroDivisionError as inner:\n        print(\"inner\", inner)\n        raise ValueError(\"second\")\nexcept ValueError as outer:\n    print(\"outer\", outer)\n", "inner division by zero\nouter second\n"},
		{"the name inside a loop", "i = 0\nwhile i < 2:\n    try:\n        print(7/0)\n    except ZeroDivisionError as err:\n        print(\"loop\", err)\n    i = i + 1\n", "loop division by zero\nloop division by zero\n"},
		{"the name between two literals", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(f\"x{err}y\")\n", "xdivision by zeroy\n"},
		{"the name inside a method", "class C:\n    def m(self):\n        try:\n            1/0\n        except ZeroDivisionError as err:\n            return err\nprint(C().m())\n", "division by zero\n"},
		{"the name copied to a second name", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    msg = err\nprint(msg)\n", "division by zero\n"},
		{"the name read twice", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err, err)\n", "division by zero division by zero\n"},
		{"a finally clause beside the arm", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err)\nfinally:\n    print(\"fin\")\n", "division by zero\nfin\n"},
		{"the first matching arm wins", "try:\n    [0][0] / 0\nexcept ZeroDivisionError as err:\n    print(\"a\", err)\nexcept IndexError as err:\n    print(\"b\", err)\n", "a division by zero\n"},
		{"str of the name", "try:\n    raise TypeError(\"bad type\")\nexcept TypeError as err:\n    print(str(err))\n", "bad type\n"},
		{"the name printed twice in one arm", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err)\n    print(err)\nprint(\"tail\")\n", "division by zero\ndivision by zero\ntail\n"},
		{"a raise from inside a call, caught outside it", "def g():\n    return 1/0\n\ndef f():\n    try:\n        g()\n    except ZeroDivisionError as err:\n        return \"caught \" + str(err)\n\nprint(f())\n", "caught division by zero\n"},
		// The control (ADR 0273): a program that names nothing keeps the road it had. The binding must not
		// reach into an arm the source wrote without an `as`, and the row's claim is only meaningful beside
		// this row.
		{"the control: an arm that names nothing", "try:\n    1/0\nexcept ZeroDivisionError:\n    print(\"caught\")\n", "caught\n"},
	}
}

// TestWhatAnExceptArmNamesPrintsWhatTheReferencePrints is the row's answer table. Each row is judged on
// the record leg — the golden's stdout, whose bytes the case must match, so coverage cannot be deleted by
// deleting a record — and on the compiled leg, the module built and run beside it.
func TestWhatAnExceptArmNamesPrintsWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	for _, tc := range exceptAsAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			goldenStdoutIs(t, strings.TrimSuffix(tc.src, "\n"), tc.want)
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers (%v): %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want the reference's %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheParserKeepsTheNameTheArmWrote is the front-end half of the row: the grammar carries the name, the
// AST JSON — the surface an agent reads — names it, and the formatter writes the clause back the way the
// program wrote it. A formatter that dropped `as err` would print a DIFFERENT program: one that catches
// the exception and binds nothing.
func TestTheParserKeepsTheNameTheArmWrote(t *testing.T) {
	t.Parallel()
	src := "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("the clause the reference parses is a parse error here: %v", err)
	}
	ts, ok := prog.Stmts[0].(*TryStmt)
	if !ok || len(ts.Excepts) != 1 {
		t.Fatalf("the program is not the try the source wrote: %#v", prog.Stmts)
	}
	if got := ts.Excepts[0].As; got != "err" {
		t.Fatalf("`except ZeroDivisionError as err:` parsed the name as %q", got)
	}
	if got := ts.Excepts[0].Exn.Value; got != "ZeroDivisionError" {
		t.Fatalf("the arm's type name came out %q", got)
	}
	doc, jerr := json.Marshal(prog)
	if jerr != nil {
		t.Fatalf("the AST does not marshal: %v", jerr)
	}
	if !strings.Contains(string(doc), `"as":"err"`) {
		t.Fatalf("the AST JSON carries no `as` member, so an agent reading --emit-ast cannot see the binding:\n%s", doc)
	}
	text, ferr := FormatSrc(src)
	if ferr != nil {
		t.Fatalf("the formatter refused the program: %v", ferr)
	}
	if !strings.Contains(text, "except ZeroDivisionError as err:") {
		t.Fatalf("the formatter dropped the arm's name, so `--fmt` prints another program:\n%s", text)
	}
	// A clause with no `as` keeps its own spelling: the binding road must not reach it (ADR 0273).
	plain, perr := Parse("try:\n    1/0\nexcept ZeroDivisionError:\n    print(1)\n")
	if perr != nil {
		t.Fatalf("an arm with no name stopped parsing: %v", perr)
	}
	if pt, ok := plain.Stmts[0].(*TryStmt); !ok || pt.Excepts[0].As != "" {
		t.Fatalf("an arm the source wrote without `as` came back naming something")
	}
	// A name the source did not write is a usage error, not a silent no-op.
	if _, berr := Parse("try:\n    1/0\nexcept ZeroDivisionError as:\n    print(1)\n"); berr == nil {
		t.Fatalf("`except T as:` — with no name after it — compiled; the reference reports a syntax error")
	} else if !strings.Contains(berr.Error(), "expected a name after `as`") {
		t.Fatalf("`except T as:` refused with %q, which does not say what is missing", berr.Error())
	}
}

// TestTheCaughtNameIsBoundInTheArmThatNamesIt pins the lowering: the message is read out of the raise's own
// record, interned by the one interning door every text goes through, and written into the arm's slot — and
// the raise writes the caption and the message as two separate stores, so a message containing ": " keeps
// its own colon (roadmap Gap R.113, ADR 0406).
func TestTheCaughtNameIsBoundInTheArmThatNamesIt(t *testing.T) {
	t.Parallel()
	res, err := Compile("try:\n    raise ValueError(\"boom\")\nexcept ValueError as err:\n    print(err)\n")
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference answers (%v)", err)
	}
	if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the module does not verify: %v\n%s", verr, res.IR)
	}
	if !strings.Contains(res.IR, "load i8*, i8** @exn_arg") {
		t.Fatalf("the arm never read @exn_arg, so the name cannot hold what the raise carried:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "call i32 @rt_str_intern2(i8* %t") {
		t.Fatalf("the message was not interned by the door every other text uses:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "@exn_arg = internal global i8* null") {
		t.Fatalf("the module reads @exn_arg without declaring it — the Gap R.2 pairing rule:\n%s", res.IR)
	}
	// The caption and the message are one raise's two stores: @exn_msg carries "ValueError: boom" for the
	// uncaught report, @exn_arg carries "boom" for the name.
	var msgLine, argLine string
	for _, ln := range strings.Split(res.IR, "\n") {
		if strings.Contains(ln, "@exn_msg") && strings.Contains(ln, "store") {
			msgLine = strings.TrimSpace(ln)
		}
		if strings.Contains(ln, "@exn_arg") && strings.Contains(ln, "store") {
			argLine = strings.TrimSpace(ln)
		}
	}
	if msgLine == "" || argLine == "" {
		t.Fatalf("the raise wrote %q and %q; both the report and the message are owed a store", msgLine, argLine)
	}
	if msgLine == argLine {
		t.Fatalf("the report and the bound message are the same string, so a message with a colon in it loses letters:\n%s\n%s", msgLine, argLine)
	}
}

// TestTheArmWithoutAnAsClauseBindsNothing is the control the row's own claim needs (ADR 0273): a program
// that names nothing pays nothing. No read of the raise's message, no intern call, no slot.
func TestTheArmWithoutAnAsClauseBindsNothing(t *testing.T) {
	t.Parallel()
	res, err := Compile("try:\n    raise ValueError(\"boom\")\nexcept ValueError:\n    print(\"caught\")\n")
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference answers (%v)", err)
	}
	if strings.Contains(res.IR, "load i8*, i8** @exn_arg") {
		t.Fatalf("an arm that names nothing read the raise's message anyway:\n%s", res.IR)
	}
	if strings.Contains(res.IR, "rt_str_intern2") {
		t.Fatalf("an arm that names nothing interned a text it was never asked for:\n%s", res.IR)
	}
}

// TestTheMessageIsBoundInTheBlockTheArmRuns is the ADR 0374 rule read at this door: the binding belongs to
// the accepting edge, so an arm the dispatch rejected binds nothing — and the arm that DOES run has its
// name before its first statement, which is what lets the arm print it.
func TestTheMessageIsBoundInTheBlockTheArmRuns(t *testing.T) {
	t.Parallel()
	res, err := Compile("try:\n    1/0\nexcept TypeError as te:\n    print(\"te\", te)\nexcept ZeroDivisionError as ze:\n    print(\"ze\", ze)\n")
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference answers (%v)", err)
	}
	lines := strings.Split(res.IR, "\n")
	// Every arm block that binds a name reads @exn_arg inside itself; the binding is not hoisted above the
	// dispatch that decides which arm runs.
	blocks := map[string][]string{}
	cur := ""
	for _, ln := range lines {
		if strings.HasSuffix(ln, ":") && !strings.HasPrefix(strings.TrimSpace(ln), "%") && !strings.HasPrefix(ln, " ") {
			cur = strings.TrimSuffix(strings.TrimSpace(ln), ":")
		}
		if cur != "" {
			blocks[cur] = append(blocks[cur], ln)
		}
	}
	bindingBlocks := 0
	for name, body := range blocks {
		if !strings.HasPrefix(name, "try.arm") {
			continue
		}
		for _, ln := range body {
			if strings.Contains(ln, "load i8*, i8** @exn_arg") {
				bindingBlocks++
			}
		}
	}
	if bindingBlocks != 2 {
		t.Fatalf("the two arms bind their names in %d arm blocks; each arm reads the message inside its own block\n%s", bindingBlocks, res.IR)
	}
	// Inside an arm, the order is the claim: the message is interned and stored BEFORE the arm's first
	// statement reads the slot, so the arm reads what the raise carried and not what the last raise left.
	for name, body := range blocks {
		if !strings.HasPrefix(name, "try.arm") {
			continue
		}
		storeAt, readAt := -1, -1
		for i, ln := range body {
			if storeAt < 0 && strings.Contains(ln, "store i32 %") && strings.Contains(ln, "i32* %_") {
				storeAt = i
			}
			if readAt < 0 && strings.Contains(ln, "= load i32, i32* %_") {
				readAt = i
			}
		}
		if storeAt < 0 || readAt < 0 || storeAt > readAt {
			t.Fatalf("in %s the name was read at %d and written at %d — the arm must bind before it reads:\n%s", name, readAt, storeAt, strings.Join(body, "\n"))
		}
	}
}

// TestTheOperandDoorsRefuseWhatTheReferenceRaises keeps the row honest about the half it does not have. The
// name an `as` clause bound prints as the message, but the reference's exception is not a string: `len`, a
// text method and a concatenation each RAISE there, and answering them with the message the way a text is
// answered is the exit-0 wrong answer ADR 0166 refuses to ship. Each door is pinned by its exit code, by
// the sentence, and by empty stdout.
func TestTheOperandDoorsRefuseWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		door string
	}{
		{"len of what was caught", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(len(err))\n", "len"},
		{"a text method of what was caught", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err.upper())\n", "upper"},
		{"comparing what was caught with a text", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err == \"division by zero\")\n", "=="},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tt.src)
			if err == nil {
				got := runIR(t, res.IR)
				t.Fatalf("the door answered %q at exit 0 where the reference raises: %s\nsrc: %s", got, tt.door, tt.src)
			}
			if !strings.Contains(err.Error(), "Gap R.113") {
				t.Fatalf("the refusal does not name the row that owns it: %v", err)
			}
		})
	}
}

// exceptAsOwed pins what the row leaves for roadmap Gap R.395: the exception OBJECT, which this backend
// does not have. Each row is the program, the reference's own bytes, and what this backend says today —
// pinned, not forgiven. A row that starts answering the reference is this test failing.
func exceptAsOwed() []struct {
	name       string
	src        string
	refOut     string
	refExit    int
	ourOut     string
	ourExit    int
	ourSays    string // a substring the refusal sentence carries, when the door refuses
	roadmapRef string
} {
	return []struct {
		name       string
		src        string
		refOut     string
		refExit    int
		ourOut     string
		ourExit    int
		ourSays    string
		roadmapRef string
	}{
		{
			name:       "the repr of what was caught",
			src:        "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(repr(err))\n",
			refOut:     "ZeroDivisionError('division by zero')\n",
			refExit:    0,
			ourOut:     "division by zero\n",
			ourExit:    0,
			roadmapRef: "Gap R.395",
		},
		{
			name:       "what was caught, inside a list",
			src:        "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print([err])\n",
			refOut:     "[ZeroDivisionError('division by zero')]\n",
			refExit:    0,
			ourOut:     "[division by zero]\n",
			ourExit:    0,
			roadmapRef: "Gap R.395",
		},
		{
			name:       "the name read after the arm",
			src:        "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(\"x\")\nprint(err)\n",
			refOut:     "x\n",
			refExit:    1,
			ourOut:     "x\ndivision by zero\n",
			ourExit:    0,
			roadmapRef: "Gap R.395",
		},
		{
			name:       "the caught name copied to a second name, then measured",
			src:        "try:\n    1/0\nexcept ZeroDivisionError as err:\n    s = err\nprint(len(s))\n",
			refOut:     "",
			refExit:    1,
			ourOut:     "16\n",
			ourExit:    0,
			roadmapRef: "Gap R.395",
		},
		{
			name:       "membership of what was caught",
			src:        "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(\"a\" in err)\n",
			refOut:     "",
			refExit:    1,
			ourOut:     "False\n",
			ourExit:    0,
			roadmapRef: "Gap R.395",
		},
		{
			name:       "the args of what was caught",
			src:        "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err.args)\n",
			refOut:     "('division by zero',)\n",
			refExit:    0,
			ourOut:     "",
			ourExit:    1,
			ourSays:    "unsupported attr expression",
			roadmapRef: "Gap R.395",
		},
	}
}

// TestWhatTheCaughtNameStillOwesPinsTheDivergence is the row's debt, read today: the reference still has an
// object where this backend has the message it wrote.
func TestWhatTheCaughtNameStillOwesPinsTheDivergence(t *testing.T) {
	t.Parallel()
	for _, tt := range exceptAsOwed() {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tt.src)
			if err != nil {
				if tt.ourExit != 1 {
					t.Fatalf("the program was refused (%v) where the row records an answer of %q", err, tt.ourOut)
				}
				if tt.ourSays != "" && !strings.Contains(err.Error(), tt.ourSays) {
					t.Fatalf("refused with %q, which does not carry %q", err.Error(), tt.ourSays)
				}
				return
			}
			if tt.ourExit != 0 {
				t.Fatalf("the program built and ran where the row records a refusal: %s", tt.src)
			}
			got := runIR(t, res.IR)
			if got != tt.ourOut {
				t.Fatalf("the compiled leg changed shape: stdout %q, the row pins %q (the reference says %q) — if this now answers the reference, DELETE the row and close %s\nsrc: %s", got, tt.ourOut, tt.refOut, tt.roadmapRef, tt.src)
			}
			if got == tt.refOut {
				t.Fatalf("the divergence silently disappeared: the compiled leg answers %q, the reference's own bytes — %s is closed and this row must go\nsrc: %s", tt.refOut, tt.roadmapRef, tt.src)
			}
		})
	}
}
