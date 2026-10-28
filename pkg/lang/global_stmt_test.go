package lang

import (
	"strings"
	"testing"
)

// Roadmap row **Gap R.48** at the compiler: the `global` statement, and the module storage a body is
// allowed to write. Before this row `global` lexed as an ordinary name and the resolver answered
// `undefined name "global"`, so the counter a helper bumps, the flag a setup function sets and the
// accumulator a loop body fills could not be written at all.
//
// Every row here was measured against the reference. The answers are judged on the compiled leg AND the
// record leg (**ADR 0302**); the refusals are judged on the sentence and the row that owns the fix, because
// a gate that compiles a program the reference rejects is the failure this file keeps out.

// globalStmtAnswers is the promise: a body that declares a name writes the MODULE's binding, and the
// module reads what the body wrote.
var globalStmtAnswers = []struct{ name, src, want string }{
	{
		"the row's own program: a body bumps the module's counter",
		"count = 0\n\ndef bump():\n    global count\n    count = count + 1\n\nbump()\nbump()\nprint(count)\n", "2\n",
	},
	{
		"a module-level `global` is legal and means nothing",
		"global x\nx = 1\nprint(x)\n", "1\n",
	},
	{
		"a body's declaration needs no module-level binding to write into",
		"def f():\n    global y\n    y = 5\n\nf()\nprint(y)\n", "5\n",
	},
	{
		"a read of a declared name is the module's value",
		"x = 10\n\ndef f():\n    global x\n    return x\n\nprint(f())\n", "10\n",
	},
	{
		"the body's write replaces the module's binding",
		"x = 1\n\ndef f():\n    global x\n    x = 2\n\nf()\nprint(x)\n", "2\n",
	},
	{
		"one declaration names several",
		"a = 0\nb = 0\n\ndef f():\n    global a, b\n    a = 7\n    b = 8\n\nf()\nprint(a, b)\n", "7 8\n",
	},
	{
		"a declaration inside a branch still writes the module",
		"flag = False\n\ndef f():\n    global flag\n    if True:\n        flag = True\n\nf()\nprint(flag)\n", "True\n",
	},
	{
		"a nested body gets the module too, not the enclosing frame",
		"v = 0\n\ndef outer():\n    def inner():\n        global v\n        v = 3\n    inner()\n\nouter()\nprint(v)\n", "3\n",
	},
	{
		"a loop body accumulates into the module",
		"total = 0\n\ndef acc():\n    global total\n    for i in [1, 2, 3]:\n        total = total + i\n\nacc()\nprint(total)\n", "6\n",
	},
	{
		"a declared name read beside a local of the body",
		"x = 5\n\ndef f():\n    global x\n    y = x + 1\n    return y\n\nprint(f(), x)\n", "6 5\n",
	},
	{
		"a while body counts down the module's name",
		"n = 0\n\ndef down():\n    global n\n    n = 3\n    while n > 0:\n        n = n - 1\n\ndown()\nprint(n)\n", "0\n",
	},
	{
		"a try body writes what it declares",
		"x = 0\n\ndef f():\n    global x\n    try:\n        x = 1\n    except Exception:\n        pass\n\nf()\nprint(x)\n", "1\n",
	},
	{
		"two bodies, one name, in the order the program ran them",
		"a = 1\n\ndef f():\n    global a\n    a = a + 1\n\ndef g():\n    global a\n    a = a * 10\n\nf()\ng()\nprint(a)\n", "20\n",
	},
	{
		"a parameter beside a declared name is not the declared name",
		"def f(a):\n    global b\n    b = a * 2\n\nf(4)\nprint(b)\n", "8\n",
	},
	{
		"the module reads the name the body first wrote",
		"def f():\n    global x\n    x = 1\n\nf()\nprint(x + 1)\n", "2\n",
	},
	{
		"a body that returns what it bumped: the call and the module agree",
		"n = 0\n\ndef bump():\n    global n\n    n = n + 1\n    return n\n\nprint(bump(), bump(), n)\n", "1 2 2\n",
	},
	{
		"a method may declare the module's name too",
		"hits = 0\n\nclass Counter:\n    def tick(self):\n        global hits\n        hits = hits + 1\n\nc = Counter()\nc.tick()\nc.tick()\nprint(hits)\n", "2\n",
	},
}

// globalStmtRefusals is the checker: the two shapes the REFUSE in the reference, and the three the
// reference answers from state this backend's module word cannot carry. All five stop the build in words
// that name the reference's own sentence and the row that owns the difference — a program the reference
// rejects, or whose answer would be a handle printed as a number, must not reach the printer.
var globalStmtRefusals = []struct {
	name  string
	src   string
	says  string
	wants string // the row the sentence must name
}{
	{
		// CPython: SyntaxError: name 'x' is parameter and global
		name: "a parameter and a global are one name two ways",
		src:  "def f(x):\n    global x\n    return x\n\nprint(f(1))\n",
		says: "is parameter and global", wants: "Gap R.48",
	},
	{
		// CPython: SyntaxError: name 'x' is used prior to global declaration
		name: "a read above its own declaration",
		src:  "x = 1\n\ndef f():\n    print(x)\n    global x\n    x = 2\n\nf()\n",
		says: "used prior to global declaration", wants: "Gap R.48",
	},
	{
		// CPython: SyntaxError: name 'x' is assigned to before global declaration
		name: "a binding above its own declaration",
		src:  "def f():\n    x = 1\n    global x\n    x = 2\n\nf()\n",
		says: "assigned to before global declaration", wants: "Gap R.48",
	},
	{
		// CPython: NameError: name 'z' is not defined — the raise, not the answer.
		name: "a declaration nothing ever writes",
		src:  "def f():\n    global z\n    pass\n\nf()\nprint(z)\n",
		says: `undefined name "z"`, wants: "",
	},
	{
		// CPython: NameError from the body — the read precedes any write, with nothing at module level.
		name: "a read of a name only the body could have written",
		src:  "def f():\n    global q\n    q = q + 1\n\nf()\n",
		says: "NameError: name 'q' is not defined", wants: "Gap R.48",
	},
	{
		// The probe ADR 0228's pass filed: the guarded write may never happen, so the read is unbound.
		name: "a write the program guards does not license the read",
		src:  "def touch():\n    global gz\n    if 0:\n        gz = 1\n    return gz\n\nprint(touch())\n",
		says: "NameError: name 'gz' is not defined", wants: "Gap R.48",
	},
	{
		// The reference prints 4 there; the member table and the module would each own one reader.
		name: "a class body may not declare a name into the module",
		src:  "x = 0\n\nclass A:\n    global x\n    x = 4\n\nprint(x)\n",
		says: "as a MEMBER of the class", wants: "Gap R.48",
	},
	{
		// The reference prints [1, 2]; the module's slot is one word with no kind beside it.
		name: "a slot whose writes spell two kinds is the tagged word L11.1 owns",
		src:  "reg = 0\n\ndef f():\n    global reg\n    reg = [1, 2]\n\nf()\nprint(reg)\n",
		says: "one word", wants: "L11.1",
	},
	{
		// The reference prints `ready`; a text is an (index, length) pair and the module's slot is one
		// word, so the honest answer is unavailable and the dishonest one prints the intern index.
		name: "a text a body writes is the tagged word L11.1 owns, not the module's one word",
		src:  "word = \"\"\n\ndef f():\n    global word\n    word = \"ready\"\n\nf()\nprint(word)\n",
		says: "interned", wants: "L11.1",
	},
}

func TestTheGlobalStatementWritesTheModulesBinding(t *testing.T) {
	t.Parallel()
	for _, tc := range globalStmtAnswers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, the reference prints %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

func TestTheGlobalCheckerRefusesWhatTheReferenceRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range globalStmtRefusals {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := compileAndRun(t, tc.src)
			if err == nil {
				t.Fatalf("the compiled leg ANSWERED a program this row pins as a refusal; the reference refuses it or reads state "+
					"the module word cannot carry\n%s", tc.src)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.says) {
				t.Fatalf("the refusal read\n  %s\nand must contain %q\n%s", msg, tc.says, tc.src)
			}
			if tc.wants == "" {
				return
			}
			if !strings.Contains(msg, tc.wants) {
				t.Fatalf("the refusal did not name the row that owns the fix (%s):\n  %s\n%s", tc.wants, msg, tc.src)
			}
		})
	}
}

// TestTheDeclarationBelongsToTheBodyThatWritesIt is the file-then-pay and scope row asked of the emitted
// module: the storage is the module slot ADR 0227 built (no new global form for a declaration to invent),
// a body that declares a name does not also own a frame slot for it, and a program with no declaration in
// it carries none of it.
func TestTheDeclarationBelongsToTheBodyThatWritesIt(t *testing.T) {
	t.Parallel()
	silentIR, err := compileIRForGlobal(t, "count = 0\n\ndef bump():\n    step = 1\n    step = step + 1\n\nbump()\nprint(count)\n")
	if err != nil {
		t.Fatalf("the module with no declaration did not compile: %v", err)
	}
	if strings.Contains(silentIR, "@gy_mod_count") {
		t.Fatalf("a body that never declared `global count` was given module storage for it — the frame slot is the rule,\n"+
			"and the declaration is the one exception (roadmap Gap R.48):\n%s", silentIR)
	}
	asksIR, err := compileIRForGlobal(t, "count = 0\n\ndef bump():\n    global count\n    count = count + 1\n\nbump()\nprint(count)\n")
	if err != nil {
		t.Fatalf("the module with a declaration did not compile: %v", err)
	}
	if !strings.Contains(asksIR, "@gy_mod_count") {
		t.Fatalf("the declaration did not route the write to the module's slot (ADR 0227's storage):\n%s", asksIR)
	}
	// One slot, not two: the frame must not keep a second word the reads never consult.
	if strings.Contains(asksIR, "store i32") && strings.Count(asksIR, "@gy_mod_count") < 2 {
		t.Fatalf("the declaration wrote a slot nothing reads (ADR 0351's defect): %d mentions\n%s",
			strings.Count(asksIR, "@gy_mod_count"), asksIR)
	}
}

// TestTheGlobalCheckerAsksTheBodiesNobodyCalls is why the checker is a program walk and not an emit-time
// pass: the reference's refusals are SyntaxErrors, and a SyntaxError does not wait for the call. A draft
// that checked only the bodies it lowered passed this program — there was nothing to lower — and shipped
// a compiler that accepts what the reference refuses to even compile.
func TestTheGlobalCheckerAsksTheBodiesNobodyCalls(t *testing.T) {
	t.Parallel()
	src := "def never_called():\n    local_first = 1\n    global local_first\n    local_first = 2\n\nprint(\"the program still runs\")\n"
	got, err := compileAndRun(t, src)
	if err == nil {
		t.Fatalf("a never-called body's misdeclared `global` was accepted; the reference refuses it as a SyntaxError\n%s", src)
	}
	if strings.Contains(got, "the program still runs") {
		t.Fatalf("the program ran, so the refusal was not at compile time: %q", got)
	}
	if !strings.Contains(err.Error(), "assigned to before global declaration") {
		t.Fatalf("the refusal did not name the defect: %v", err)
	}
}

func compileIRForGlobal(t *testing.T, src string) (string, error) {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		return "", err
	}
	return res.IR, nil
}
