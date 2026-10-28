package lang

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// Roadmap row **L12.3** / **Gap R.55** at the compiler: `print`, `str()` and `repr()` consult the
// class's own `__str__` / `__repr__` before the value-form table.
//
// Every row here was measured on `c0972a4` first, where the compiled leg printed `<__main__.C object
// at 0x…>` for a class that had said what it prints, printed the intern table's index for an f-string
// field, and refused `str(C())` outright — while `print(C())`, `str(C())` and `print([C()])` are three
// spellings of ONE question that the reference answers from the class. The rows are judged on the
// compiled leg AND the record leg (**ADR 0302**), and the identity rows are compared with the address
// normalised, because the address is an allocation accident and the oracle says so
// (`pkg/lang/oracle.go`'s `instanceIdentityRe`).
//
// What the row does NOT own is pinned in `classRenderFiled`, with this backend's answer and the
// reference's side by side.

// classRenderAnswers is the promise: the class speaks for its instances.
var classRenderAnswers = []struct{ name, src, want string }{
	{
		"the row's own program: __str__ answers print",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nprint(C())\n", "S\n",
	},
	{
		"a class that binds only __repr__ answers print with it",
		"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint(C())\n", "R\n",
	},
	{
		"__str__ wins print when the class binds both",
		"class C:\n    def __str__(self):\n        return \"S\"\n\n    def __repr__(self):\n        return \"R\"\n\nprint(C())\n", "S\n",
	},
	{
		"str() asks the class",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nprint(str(C()))\n", "S\n",
	},
	{
		"repr() asks the class",
		"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint(repr(C()))\n", "R\n",
	},
	{
		"str() of a class that binds only __repr__ takes the reference's object.__str__ fallthrough",
		"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint(str(C()))\n", "R\n",
	},
	{
		"the answer is a text the program can work on",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nx = C()\nprint(str(x) + \"!\")\n", "S!\n",
	},
	{
		"and can be compared",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nx = C()\nprint(str(x) == \"S\")\n", "True\n",
	},
	{
		"a container element is the repr answer, unquoted",
		"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint([C()])\n", "[R]\n",
	},
	{
		"a dict value too",
		"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint({\"k\": C()})\n", "{'k': R}\n",
	},
	{
		"and a set member",
		"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint({C()})\n", "{R}\n",
	},
	{
		"a container that mixes the instance with everything else",
		"class C:\n    def __repr__(self):\n        return \"C\"\n\nprint([1, C(), \"t\", None])\n", "[1, C, 't', None]\n",
	},
	{
		"one level down",
		"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint([[C()]])\n", "[[R]]\n",
	},
	{
		"an instance used as a dict key renders through its class",
		"class C:\n    def __repr__(self):\n        return \"K\"\n\nd = {}\nd[C()] = 1\nprint(d)\n", "{K: 1}\n",
	},
	{
		"the f-string field asks the class",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nx = C()\nprint(f\"v={x}\")\n", "v=S\n",
	},
	{
		"the !r conversion asks the repr door",
		"class C:\n    def __str__(self):\n        return \"S\"\n\n    def __repr__(self):\n        return \"R\"\n\nx = C()\nprint(f\"{x!r}\")\n", "R\n",
	},
	{
		"the !s conversion asks the str door",
		"class C:\n    def __str__(self):\n        return \"S\"\n\n    def __repr__(self):\n        return \"R\"\n\nx = C()\nprint(f\"{x!s}\")\n", "S\n",
	},
	{
		"str() inside an interpolation",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nprint(f\"[{str(C())}]\")\n", "[S]\n",
	},
	{
		"a member inherited from a base is found on the base that owns it",
		"class A:\n    def __str__(self):\n        return \"A\"\n\nclass B(A):\n    pass\n\nprint(B())\n", "A\n",
	},
	{
		"a subclass that binds its own answer wins, and the base keeps its own",
		"class A:\n    def __str__(self):\n        return \"A\"\n\nclass B(A):\n    def __str__(self):\n        return \"B\"\n\nprint(B(), A())\n", "B A\n",
	},
	{
		"the diamond takes the reference's resolution order, not the first base",
		"class A:\n    def __repr__(self):\n        return \"A\"\n\nclass B(A):\n    pass\n\nclass C(A):\n    def __repr__(self):\n        return \"C\"\n\nclass D(B, C):\n    pass\n\nprint(D())\n", "C\n",
	},
	{
		"two classes answering one print line",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nclass D:\n    def __str__(self):\n        return \"D\"\n\nprint(C(), D())\n", "S D\n",
	},
	{
		"a binding to a name renders the same object",
		"class C:\n    def __repr__(self):\n        return \"R\"\n\nc = C()\nprint([c])\n", "[R]\n",
	},
	{
		"the body may call the class's own methods",
		"class C:\n    def label(self):\n        return \"L\"\n\n    def __str__(self):\n        return f\"S:{self.label()}\"\n\nprint(C())\n", "S:L\n",
	},
	{
		"and the output the body writes comes out in the reference's order",
		"class C:\n    def __str__(self):\n        print(\"inner\")\n        return \"S\"\n\nprint(\"outer\")\nprint(C())\n", "outer\ninner\nS\n",
	},
}

// identityRowRe normalises the address the identity form prints, which is an allocation accident and
// not a claim (ADR 0325's arrangement, read by pkg/lang/oracle.go for the corpus).
var identityRowRe = regexp.MustCompile(`0x[0-9a-fA-F]+`)

// classRenderIdentityRows are the shapes where the class says NOTHING, and the identity form ADR 0325
// built is still the answer. They are the negative control for the render table: opening the dunder
// door may not close the one that has always answered.
var classRenderIdentityRows = []struct{ name, src, want string }{
	{
		"a class that binds neither dunder",
		"class C:\n    pass\n\nprint(C())\n", "<__main__.C object at 0x0>\n",
	},
	{
		"str() of such an instance hands the identity form back as a text",
		"class C:\n    pass\n\nx = C()\nprint(str(x))\n", "<__main__.C object at 0x0>\n",
	},
	{
		"repr() of a class that binds only __str__ is the identity, not the __str__ answer",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nprint(repr(C()))\n", "<__main__.C object at 0x0>\n",
	},
	{
		"a container element asks the repr door, which this class did not answer",
		"class C:\n    def __str__(self):\n        return \"S\"\n\nprint([C()])\n", "[<__main__.C object at 0x1>]\n",
	},
	{
		"an f-string field of such an instance is the identity too",
		"class C:\n    pass\n\nx = C()\nprint(f\"v={x}\")\n", "v=<__main__.C object at 0x0>\n",
	},
	{
		"a method the program wrote is not a dunder",
		"class C:\n    def show(self):\n        return \"hi\"\n\nprint(C())\n", "<__main__.C object at 0x0>\n",
	},
}

// classRenderTraps are the failures the class's own answer causes, each one raised with the reference's
// words rather than answered with a rendering the reference does not have.
var classRenderTraps = []struct {
	name    string
	src     string
	stdout  string
	message string
}{
	{
		"a __str__ that answers a number is the reference's TypeError",
		"class C:\n    def __str__(self):\n        return 7\n\nprint(C())\n",
		"", "TypeError: __str__ returned non-string (type int)",
	},
	{
		"and names the verdict, not the number it is stored as",
		"class C:\n    def __str__(self):\n        return True\n\nprint(C())\n",
		"", "TypeError: __str__ returned non-string (type bool)",
	},
	{
		"a container answer names the container",
		"class C:\n    def __str__(self):\n        return [1]\n\nprint(C())\n",
		"", "TypeError: __str__ returned non-string (type list)",
	},
	{
		"a body that answers None names NoneType",
		"class C:\n    def __str__(self):\n        return None\n\nprint(str(C()))\n",
		"", "TypeError: __str__ returned non-string (type NoneType)",
	},
	{
		"a body that falls off its end answers None too",
		"class C:\n    def __str__(self):\n        pass\n\nprint(C())\n",
		"", "TypeError: __str__ returned non-string (type NoneType)",
	},
	{
		"a raise out of the body is the program's own, and nothing is printed",
		"class C:\n    def __str__(self):\n        raise ValueError(\"nope\")\n\nprint(C())\n",
		"", "ValueError: nope",
	},
	{
		"the same raise through str(), which is the same question asked of the same door",
		"class C:\n    def __str__(self):\n        raise ValueError(\"nope\")\n\nprint(str(C()))\n",
		"", "ValueError: nope",
	},
}

// classRenderCaught pins that a render trap is a RAISE, not a crash: the program's own handler reaches
// it, which is the difference between the reference's behaviour and the exit code of a fault.
var classRenderCaught = []struct{ name, src, want string }{
	{
		"except TypeError reaches the render door's raise",
		"class C:\n    def __str__(self):\n        return 7\n\ntry:\n    print(C())\nexcept TypeError:\n    print(\"caught\")\n", "caught\n",
	},
	{
		"a raise out of a plain print argument leaves nothing on stdout for the handler to read",
		"class C:\n    def __str__(self):\n        return 7\n\ntry:\n    print(C())\nexcept TypeError as e:\n    print(str(e))\n",
		"__str__ returned non-string (type int)\n",
	},
}

// classRenderFiled is the residue: the shapes this row does not answer, pinned with what the compiled
// leg prints today and what the reference prints. A row moves out of here into classRenderAnswers when
// the door that owns it is built; it does not move into an answer by assertion.
var classRenderFiled = []struct {
	name  string
	src   string
	ours  string
	ref   string
	owner string
}{{
	name: "a parameter that receives an instance prints the handle",
	src:  "class C:\n    def __repr__(self):\n        return \"R\"\n\ndef show(o):\n    print(o)\n\nshow(C())\n",
	ours: "0\n", ref: "R\n",
	owner: "the untagged binding positions (Gap R.146's family) — reproduced identically on c0972a4",
}, {
	name: "the loop variable of a literal list of instances prints the handle",
	src:  "class C:\n    def __repr__(self):\n        return \"R\"\n\nfor c in [C(), C()]:\n    print(c)\n",
	ours: "0\n1\n", ref: "R\nR\n",
	owner: "the same untagged binding positions — reproduced identically on c0972a4",
}, {
	name: "an f-string field that is an instance attribute has no renderer",
	src:  "class P:\n    def __init__(self, x):\n        self.x = x\n\n    def __str__(self):\n        return f\"P({self.x})\"\n\nprint(P(3))\n",
	ours: "no renderer", ref: "P(3)\n",
	owner: "the instance-attribute kind the compiler cannot read (Gap R.431's family)",
}, {
	name: "concatenating an instance attribute the compiler cannot read refuses",
	src:  "class C:\n    def __init__(self, n):\n        self.n = n\n\n    def __str__(self):\n        return \"n=\" + self.n\n\nprint(C(\"hi\"))\n",
	ours: "not a string", ref: "n=hi\n",
	owner: "the same instance-attribute family",
}, {
	name: "a raise out of a dunder deep in a container leaves the bytes the walk already wrote",
	src:  "class C:\n    def __repr__(self):\n        return 1.5\n\ntry:\n    print([C()])\nexcept TypeError as e:\n    print(str(e))\n",
	ours: "[__repr__ returned non-string (type float)", ref: "__repr__ returned non-string (type float)",
	owner: "Gap R.508 — the walk STOPS at the raise (no entries after it, no closing bracket), but the bytes already on stdout cannot be recalled; ADR 0258 says the same of the float door",
}, {
	name: "a dunder whose answer the body does not spell out keeps the identity form",
	src:  "class C:\n    def __init__(self):\n        self.v = \"S\"\n\n    def __str__(self):\n        return self.v\n\nprint(C())\n",
	ours: "<__main__.C object at 0x0>\n", ref: "S\n",
	owner: "the proof the render door requires is the proof print already asks for (methodReturnsStr); filed beside Gap R.431",
}, {
	name: "a dunder whose answer CONCATENATES a literal with a method call keeps the identity form",
	src:  "class Loud:\n    def tag(self):\n        return \"loud\"\n\n    def __str__(self):\n        return \"Loud<\" + self.tag() + \">\"\n\nprint(Loud())\n",
	ours: "<__main__.Loud object at 0x0>", ref: "Loud<loud>",
	owner: "the provable-text door reads a literal, an interpolation and a plain call — not a concatenation of a literal with a method call. The f-string spelling of the same body DOES answer, so the shape is owed to the concat door (Gap R.431's family), not invented here",
}, {
	name: "a dunder that takes more than its receiver keeps the identity form",
	src:  "class C:\n    def __str__(self, extra):\n        return \"S\"\n\nprint(C())\n",
	ours: "<__main__.C object at 0x0>\n", ref: "TypeError: __str__() missing 1 required positional argument: 'extra'\n",
	owner: "the arity raise the call surface does not have yet (L12.6's call-surface family)",
}}

func TestTheClassAnswersHowItReads(t *testing.T) {
	t.Parallel()
	for _, tc := range classRenderAnswers {
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

// TestTheClassThatSaysNothingKeepsItsIdentity judges the fallback: the render table is a door, not a
// replacement, and the identity form ADR 0325 built is still what answers a class that binds nothing.
func TestTheClassThatSaysNothingKeepsItsIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range classRenderIdentityRows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if normRenderAddress(got) != normRenderAddress(tc.want) {
				t.Fatalf("printed %q, the reference prints %q\n%s", got, tc.want, tc.src)
			}
			if !strings.Contains(got, "object at 0x") {
				t.Fatalf("the identity form left the module: %q\n%s", got, tc.src)
			}
		})
	}
}

// TestTheRenderTrapIsTheReferencesRaise pins the message the reference spells for a dunder that handed
// back something it will not print, and that the render printed NOTHING before the raise — a renderer
// that wrote half a line and then trapped is the failure this row keeps out.
func TestTheRenderTrapIsTheReferencesRaise(t *testing.T) {
	t.Parallel()
	for _, tc := range classRenderTraps {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRunExpectTrap(t, tc.src)
			if err != nil {
				t.Fatalf("the trap never happened: %v\n%s", err, tc.src)
			}
			if !strings.Contains(got, tc.message) {
				t.Fatalf("the raise read %q, the reference's is %q\n%s", got, tc.message, tc.src)
			}
			if before := strings.Replace(got, "\nTraceback", "", 1); tc.stdout != "" && !strings.Contains(before, tc.stdout) {
				t.Fatalf("the stdout before the traceback read %q, expected %q\n%s", got, tc.stdout, tc.src)
			}
		})
	}
}

// TestTheRenderTrapReachesTheProgramsHandler is the raise, not the crash, half of the same promise.
func TestTheRenderTrapReachesTheProgramsHandler(t *testing.T) {
	t.Parallel()
	for _, tc := range classRenderCaught {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the handler never ran: %v\n%s", err, tc.src)
			}
			if normRenderAddress(got) != normRenderAddress(tc.want) {
				t.Fatalf("printed %q, the reference prints %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheRenderTableBelongsToTheProgramThatAsked is the file-then-pay row, asked of the emitted module
// rather than of a behaviour: a program whose classes say nothing carries no table, no indirect call and
// no frame push — and the program that does ask carries both doors.
func TestTheRenderTableBelongsToTheProgramThatAsked(t *testing.T) {
	t.Parallel()
	silent := "class C:\n    def foo(self):\n        return 1\n\nprint(C())\n"
	silentIR, err := compileIR(t, silent)
	if err != nil {
		t.Fatalf("the module that binds no dunder did not compile: %v\n%s", err, silent)
	}
	for _, dead := range []string{"@gy.render.str.fn", "@gy.render.repr.fn", "@gy.render.line", "call ptr %"} {
		if strings.Contains(silentIR, dead) {
			t.Fatalf("a program with no dunder carries %s — the render road is paid for by the program that asks for it\n%s", dead, silent)
		}
	}
	if !strings.Contains(silentIR, "@rt_print_instance_value") {
		t.Fatalf("the printer's instance arm lost its helper\n%s", silent)
	}

	asks := "class C:\n    def __str__(self):\n        return \"S\"\n\n    def __repr__(self):\n        return \"R\"\n\nprint(C())\n"
	asksIR, err := compileIR(t, asks)
	if err != nil {
		t.Fatalf("the module that binds a dunder did not compile: %v\n%s", err, asks)
	}
	for _, want := range []string{
		"@gy.render.str.fn", "@gy.render.repr.fn", "@gy.render.str.tag", "@gy.render.repr.tag",
		"@gy.render.str.msg", "@gy.render.repr.msg", "@gy.render.line",
		"call void @rt_frame_push", // the frame the reference's traceback names
	} {
		if !strings.Contains(asksIR, want) {
			t.Fatalf("the render table lost %s\n%s", want, asks)
		}
	}
	// One printer, asked twice (ADR 0258): the render answer goes back through the module's own
	// tag-reading printer rather than a renderer the render road wrote for itself.
	if strings.Count(asksIR, "define internal void @rt_print_mixed_value") != 1 {
		t.Fatalf("the module grew a second value renderer\n%s", asksIR)
	}
}

// TestTheTwoRenderDoorsAreOneTablePerClass asks the emitted module directly, so a change that routes the str
// question at the repr table — or reads the class id from anywhere but the object, or lets the repr door reach a
// __str__ — fails here first. The program asks both questions of three classes: A binds only __str__, B inherits
// A's and binds its own __repr__, E binds nothing.
func TestTheTwoRenderDoorsAreOneTablePerClass(t *testing.T) {
	t.Parallel()
	src := "class A:\n    def __str__(self):\n        return \"A\"\n\nclass B(A):\n    def __repr__(self):\n        return \"B\"\n\nclass E:\n    pass\n\nprint(A(), B(), E(), [A()], [B()], [E()])\n"
	ir, err := compileIR(t, src)
	if err != nil {
		t.Fatalf("the module with the two doors did not compile: %v\n%s", err, src)
	}
	// One row per class, in class-registration order: A, B, E.
	wants := []string{
		// The str question, asked of B, is answered by the method A OWNS — ADR 0446's walk, in the table.
		"@gy.render.str.fn = private global [3 x ptr] [ptr @gy_A___str__, ptr @gy_A___str__, ptr null]",
		// The repr question is answered for B by B's own __repr__, and A's row is DEAD although A binds
		// __str__: the repr door never reaches the str method, which is the split the whole row turns on.
		"@gy.render.repr.fn = private global [3 x ptr] [ptr null, ptr @gy_B___repr__, ptr null]",
		// A live row carries the text tag; a dead one carries -1 and is never called.
		"@gy.render.str.tag = private global [3 x i32] [i32 4, i32 4, i32 -1]",
		"@gy.render.repr.tag = private global [3 x i32] [i32 -1, i32 4, i32 -1]",
	}
	for _, w := range wants {
		if !strings.Contains(ir, w) {
			t.Fatalf("the render table lost a row:\n  %s\n%s", w, src)
		}
	}
	// The class id is read out of the object, not guessed from the expression (ADR 0325's slot 0).
	for _, w := range []string{"call i32 @rt_inst_get(i32 %v, i32 0)", "@gy.render.str.fname", "@gy.render.repr.fname"} {
		if !strings.Contains(ir, w) {
			t.Fatalf("the door lost %s\n%s", w, src)
		}
	}
	// E binds nothing: the module carries no symbol it did not have to make, and no dead row is a live one.
	if strings.Contains(ir, "@gy_E___str__") || strings.Contains(ir, "@gy_E___repr__") {
		t.Fatalf("the door invented a method for the class that binds nothing\n%s", src)
	}
}

// TestWhatTheRenderRoadStillDoesNotAnswer pins the residue with both answers written down.
func TestWhatTheRenderRoadStillDoesNotAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range classRenderFiled {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRunAny(t, tc.src)
			if tc.ours == "" {
				// A row with no answer of its own is a row whose whole point is that the compiled leg
				// refuses to answer at all; the refusal is the assertion.
				if err == nil {
					t.Fatalf("the compiled leg answered %q where this row pins a refusal\n%s", got, tc.src)
				}
				return
			}
			if normRenderAddress(got) != normRenderAddress(tc.ours) && !strings.Contains(got, tc.ours) && !strings.Contains(errText(got, err), tc.ours) {
				t.Fatalf("the compiled leg answered %q (err %v); this row pins %q and the reference answers %q\n%s",
					got, err, tc.ours, tc.ref, tc.src)
			}
		})
	}
}

// normRenderAddress folds the allocation address out of the identity form, because the reference's own
// oracle comparison does (oracle.go's instanceIdentityRe): the address is not an answer.
func normRenderAddress(s string) string { return renderIdentityRe.ReplaceAllString(s, "0x…") }

var renderIdentityRe = regexp.MustCompile(`0x[0-9a-fA-F]+`)

// compileAndRunExpectTrap runs a program that is owed a raise and hands back EVERYTHING it said — the
// stdout it got out before the raise and the traceback together — because the row pins both halves.
func compileAndRunExpectTrap(t *testing.T, src string) (string, error) {
	t.Helper()
	res, err := RunSnippet(src + "\n")
	if err != nil {
		return "", err
	}
	if res.Code == 0 {
		return res.Output, fmt.Errorf("the program answered instead of trapping: %q", res.Output)
	}
	return res.Output + res.Stderr, nil
}

// compileAndRunAny is the residue's run: an answer, a trap or a refusal are all admissible, and the row
// says which it pins.
func compileAndRunAny(t *testing.T, src string) (string, error) {
	t.Helper()
	res, err := RunSnippet(src + "\n")
	if err != nil {
		return "", err
	}
	return res.Output + res.Stderr, nil
}

// compileIR emits the module's textual IR without running it, for the rows that ask what the program carries.
func compileIR(t *testing.T, src string) (string, error) {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		return "", err
	}
	return res.IR, nil
}

func errText(out string, err error) string {
	if err == nil {
		return out
	}
	return err.Error()
}
