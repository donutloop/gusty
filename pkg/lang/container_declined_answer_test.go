package lang

import (
	"regexp"
	"strings"
	"testing"
)

// container_declined_answer_test.go — a container answer the slot table declines is still a container, and
// no door may read its handle as a number (roadmap Gap R.261, ADR 0344; ADR 0333's scan read a second time).
//
// `containerReturningFuncs` answers "which functions hand back a container, and which". It answered that in
// two halves, and only the first half was read: a function is a container answer when every `return` arm
// names one kind *and* when `containerAnswerDescribesItsSlots` can follow the value that fills each slot. The
// second half refuses for a slot written as a call, a comprehension or a format — righteously, because such a
// value can arrive as the (payload, tag) pair and a table that printed the pair as a number was the bug
// Gap R.67 recorded. What nobody read was what happened *after* the refusal:
//
//	def g():
//	    return 5
//
//	def f():
//	    out = {}
//	    out["k"] = g()       # a slot the table cannot follow
//	    return out
//
//	print(f())               # CPython: {'k': 5}
//	                         # this toolchain before the row: 0    (exit 0)
//
// The call fell through to the ordinary road, the ordinary road returned the heap handle, and `print` wrote
// it with the number printer. The digits on the screen were the slot the allocator handed out. Measured off
// `python3` before anything was changed, `print(f())` printed `0` for all three kinds the scan proves — a list
// filled by `out.append(g())`, a set filled by `out.add(g())`, a dict filled by `out["k"] = g()` — where the
// reference prints `[5]`, `{5}` and `{'k': 5}`; a dict whose slot was filled by a *float*-returning call
// printed `0` where the reference prints `{'k': 2.5}`; and `for v in f(): print(v)` printed nothing at all
// where the reference prints `5`. Every one of them at exit 0, the code that means the program was answered.
// `str(f())` and `repr(f())` already answered, because the render road hands the handle to the entry point
// that walks the object — the proof of what the rest of the row needed was that one position had been doing
// it correctly all along.
//
// The row reads the second half and then asks, of each position, WHO answers the question it is about to
// ask of the answer:
//
//   - print, `str`, `repr`, an f-string field, a binding and a condition hand the handle to a runtime entry
//     point that walks the object, so the kind the scan proved is the only fact they need — they take the
//     declined answer and answer for it; and
//   - an index read, a slice, a comparison and a loop's iterable read a slot back out of the object using
//     the notebooks the scan writes (is the element text, does the slot carry a tag). Those notebooks belong
//     to the described answer alone, so those positions refuse with the missing half named — which is the
//     direction that costs a capability and buys back the truth (roadmap Gap R.261's filed rows below).
//
// Membership is the first kind wearing the second kind's coat: `5 in f()` asks the OBJECT whether it holds
// the needle, so it keeps answering, and keeping it answering is half of why the door is asked before the
// notebooks are — a fix that turned that into a refusal would cost more than the bug.
//
// The rows are the reference's, read off `python3`; integration/container_declined_answer_test.go runs the
// same programs through the CLI and derives its expectations from the reference rather than from this file.

// containerDeclinePreamble binds the two functions every row is written with: a callee whose answer fills a
// slot (the value the slot table will not follow) and a builder whose `return` provably hands back a list.
const containerDeclinePreamble = "def g():\n    return 5\n\ndef f():\n    out = []\n    out.append(g())\n    return out\n\n"

// containerDeclineDictPreamble is the same shape over a dict, which is where the wrong answers printed: a
// dict literal or store whose VALUE slot is filled by a call keeps described keys, and every slot the
// printers of this kind read is reached through the object.
const containerDeclineDictPreamble = "def g():\n    return 5\n\ndef f():\n    out = {}\n    out[\"k\"] = g()\n    return out\n\n"

// containerDeclineAnswered are the shapes this row opens. Each is a position that asks the OBJECT, so the
// answer arrives as an object and the container roads that have always read one answer for it.
var containerDeclineAnswered = []struct {
	src  string
	want string
}{
	// The answers that printed a handle, or nothing, at exit 0 before the row: `print` over each of the three
	// kinds the scan proves, and over a slot filled by a float-returning call.
	{containerDeclinePreamble + "print(f())", "[5]"},
	{containerDeclineDictPreamble + "print(f())", "{'k': 5}"},
	{"def g():\n    return 5\n\ndef f():\n    out = set()\n    out.add(g())\n    return out\n\nprint(f())", "{5}"},
	{"def g():\n    return 2.5\n\ndef f():\n    out = {}\n    out[\"k\"] = g()\n    return out\n\nprint(f())", "{'k': 2.5}"},
	{containerDeclinePreamble + "if f():\n    print(\"yes\")\nelse:\n    print(\"no\")", "yes"},
	// `str` and `repr` asked the object before the row; asserted so the row cannot move them off it.
	{containerDeclinePreamble + "print(str(f()))", "[5]"},
	{containerDeclinePreamble + "print(repr(f()))", "[5]"},
	{containerDeclineDictPreamble + "print(str(f()))", "{'k': 5}"},
	// The position that was already right, asserted so the row cannot break it: the binding road records the
	// name it wrote the handle into, and every read of a container variable answers from there.
	{containerDeclinePreamble + "ys = f()\nprint(ys)", "[5]"},
	{containerDeclinePreamble + "ys = f()\nprint(len(ys))", "1"},
	{containerDeclinePreamble + "ys = f()\nprint(ys[0])", "5"},
	{containerDeclinePreamble + "ys = f()\nfor v in ys:\n    print(v)", "5"},
	{containerDeclineDictPreamble + "ys = f()\nprint(ys)", "{'k': 5}"},
	// Membership asked the object before the row too. Keeping it answering is half of why the door is asked
	// before the notebooks are: a fix that turned this into a refusal would cost more than the bug.
	{containerDeclinePreamble + "print(5 in f())", "True"},
	{containerDeclinePreamble + "print(6 in f())", "False"},
	{containerDeclineDictPreamble + "print(\"k\" in f())", "True"},
	{containerDeclinePreamble + "ys = f()\nprint(5 in ys)", "True"},
	// A container whose slots are filled by another container answer is still a container answer.
	{"def g():\n    return [1, 2]\n\ndef f():\n    out = []\n    out.append(g())\n    return out\n\nprint(f())", "[[1, 2]]"},
	// The slots that the table CAN follow keep answering exactly as they did — the negative control a
	// widening of this door has to leave green.
	{"def build():\n    return [1, 2]\nprint(build())", "[1, 2]"},
	{"def build():\n    return [1, 2]\nprint(build()[1])", "2"},
	{"def build():\n    return [1, 2]\nfor v in build():\n    print(v)", "1\n2"},
	{"def build():\n    return {\"a\": 1}\nprint(build())", "{'a': 1}"},
	{"def f(n):\n    return [n, n * 2]\nprint(f(3))", "[3, 6]"},
	{"def f():\n    return [x for x in [1, 2]]\nprint(f())", "[1, 2]"},
}

func TestAContainerAnswerTheTableDeclinesStillAnswersAsAContainer(t *testing.T) {
	t.Parallel()
	for _, tt := range containerDeclineAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference answers %q and this position asks the object",
					tt.src, got, tt.want)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
		})
	}
}

// containerDeclineRefused are the positions that keep one word for the value: arithmetic, an element, an
// equality, an f-string field, a loop's iterable, an index read. Before the row they printed the handle (or a
// truncation of it, or nothing) at exit 0; now every one of them is a refusal that names a roadmap row. The
// reference's answer is recorded beside each so that the day a door grows, the row is a capability to deliver
// rather than a mystery — and so that a regression back to a printed number fails here rather than in a
// notebook.
var containerDeclineRefused = []struct {
	name     string
	src      string
	oracle   string // what CPython answers, read off python3
	mentions string // the sentence must name this
	row      string // and this roadmap row, when it is THIS row that refuses
}{
	{"arithmetic", containerDeclinePreamble + "print(f() + 1)", "6", "hands back a container whose slots this backend will not describe", "Gap R.261"},
	{"an element of a list", containerDeclinePreamble + "print([f(), 9])", "[[5], 9]", "hands back a container whose slots this backend will not describe", "Gap R.261"},
	{"an equality", containerDeclinePreamble + "print(f() == [5])", "True", "hands back a container whose slots this backend will not describe", "Gap R.261"},
	{"an f-string field", containerDeclinePreamble + "print(f\"{f()}\")", "[5]", "hands back a container", ""},
	{"a subtraction", containerDeclinePreamble + "print(f() - 1)", "4", "hands back a container whose slots this backend will not describe", "Gap R.261"},
	// A loop reads every member back through the notebooks the scan writes, and a declined answer has none:
	// ADR 0339's rule for a loop's iterable, applied to an answer — refused, never walked for slots nobody
	// described. This row is the one that printed NOTHING at exit 0 before.
	{"a loop's iterable", containerDeclinePreamble + "for v in f():\n    print(v)", "5", "hands back a container whose slots this backend will not describe", "Gap R.261"},
	{"a loop over a dict answer", containerDeclineDictPreamble + "for k in f():\n    print(k)", "k", "hands back a container whose slots this backend will not describe", "Gap R.261"},
	// An index read of the answer is the same kind of reader. These two are stopped by the index doors in
	// front of this one, which have always refused an answer they cannot fold, so the refusal is checked for
	// its honesty (a refusal, at exit 1, of a program whose answer this table cannot describe) and not for a
	// row tag it was never meant to carry.
	{"an index read of a list answer", containerDeclinePreamble + "print(f()[0])", "5", "index", ""},
	{"an index read of a dict answer", containerDeclineDictPreamble + "print(f()[\"k\"])", "5", "index", ""},
}

// TestADeclinedContainerAnswerIsRefusedAndNeverPrintsAWordTheProgramDidNotWrite is the row's safety claim:
// each position above either refuses, or prints only what the reference prints. The second branch is the one
// that matters — a printed number that is not the reference's is the defect class this row was cut out to
// close, and it is what a fix that simply widened every door produced when it was tried first.
func TestADeclinedContainerAnswerIsRefusedAndNeverPrintsAWordTheProgramDidNotWrite(t *testing.T) {
	t.Parallel()
	names := regexp.MustCompile(`Gap R\.\d+`)
	for _, tt := range containerDeclineRefused {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if !refused {
				if strings.TrimSpace(got) == strings.TrimSpace(tt.oracle) {
					return // the door grew, and grew it honestly: the reference's own answer
				}
				t.Fatalf("%q printed %q; the reference answers %q — the handle of a container is not an answer "+
					"this position may print", tt.src, got, tt.oracle)
			}
			if !strings.Contains(got, tt.mentions) {
				t.Fatalf("%q refused without naming what is missing (%s):\n%s", tt.src, tt.mentions, got)
			}
			if tt.row != "" && !strings.Contains(got, tt.row) {
				t.Fatalf("%q refused without naming the row that owns it (%s):\n%s", tt.src, tt.row, got)
			}
			if tt.row != "" && !names.MatchString(got) {
				t.Fatalf("%q refused without naming the row that owns it:\n%s", tt.src, got)
			}
			// ADR 0166: a refusal names what is missing. A sentence carrying a Go representation, a
			// compiler temp or a hex address is the compiler describing its own internals (Gap R.232's shape).
			for _, forbidden := range []string{"%!s(", "*lang.", "0x", "__cans"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("the refusal leaks the compiler's own machinery (%s): %s", forbidden, got)
				}
			}
		})
	}
}

// TestTheScanRecordsTheDeclineAndNotOnlyTheAnswer is the row asked of the scan rather than of the runtime:
// the function whose slots the table cannot follow is in the second answer with its kind, and the function
// the table describes stays in the first. A decline is a fact the toolchain now carries; before the row it
// was indistinguishable from "not a container".
func TestTheScanRecordsTheDeclineAndNotOnlyTheAnswer(t *testing.T) {
	t.Parallel()
	declined := mustParseForDecline(t, containerDeclinePreamble+"print(f())\n")
	answers, undescribed := containerAnswerScan(declined)
	if _, hit := answers["f"]; hit {
		t.Fatalf("the table describes a slot it cannot follow: %v", answers["f"])
	}
	ans, hit := undescribed["f"]
	if !hit {
		t.Fatalf("a function whose `return` hands back a list is absent from the declined set: %v", answers)
	}
	if ans.kind != listAnswer {
		t.Fatalf("the declined answer knows its kind: %q, want list", ans.kind.String())
	}
	if ans.fullyDescribed {
		t.Fatalf("a table that could not read the slots does not claim they are described")
	}
	if len(ans.arms) != 0 {
		t.Fatalf("a table that could not read the slots does not get to describe them: %d arms", len(ans.arms))
	}
	// The same question, asked through the exported door rather than the map: this is the path a front end,
	// a `--json` capability report or an agent takes, and it must agree with the door codegen reads.
	if !ContainerAnswerIsUndescribed(declined, "f") {
		t.Fatalf("the exported predicate does not see the decline the scan recorded")
	}
	if ContainerAnswerIsUndescribed(declined, "g") {
		t.Fatalf("the callee that answers a number is not a container answer at all")
	}

	described := mustParseForDecline(t, "def build():\n    return [1, 2]\nprint(build())\n")
	answers, undescribed = containerAnswerScan(described)
	if a, hit := answers["build"]; !hit || a.kind != listAnswer {
		t.Fatalf("the described answer left the first set: %v", answers)
	}
	if _, hit := undescribed["build"]; hit {
		t.Fatalf("a function the table describes is in the declined set as well")
	}
	if ContainerAnswerIsUndescribed(described, "build") {
		t.Fatalf("the exported predicate declines a function the table describes")
	}

	// A body the scan cannot prove at all — it can fall off its end, so the answer may be None — is in
	// neither set: the row refuses the programs it proved, not the ones it guesses at.
	unproven := mustParseForDecline(t, "def g():\n    return 5\n\ndef f(n):\n    out = []\n    out.append(g())\n    if n:\n        return out\n\nprint(f(1))\n")
	answers, undescribed = containerAnswerScan(unproven)
	if _, hit := answers["f"]; hit {
		t.Fatalf("a body that can fall off its end answers nothing: %v", answers)
	}
	if _, hit := undescribed["f"]; hit {
		t.Fatalf("an unprovable body is not a proven container either")
	}
}

func mustParseForDecline(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := parseProgram(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

// TestTheDeclinedAnswerIsPrintedByTheDoorThatAsksTheObject is the row's claim about the emitted module rather
// than about the runtime: an answer the table declines reaches the printer as an *object* — a rooted slot the
// container roads can read — and not as the bare word the ordinary call road keeps. That the object prints
// right is already asserted above; what this pins down is that the row moved the *door*, not the printer.
func TestTheDeclinedAnswerIsPrintedByTheDoorThatAsksTheObject(t *testing.T) {
	t.Parallel()
	res, err := Compile(containerDeclinePreamble + "print(f())\n")
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference answers: %v", err)
	}
	for _, want := range []string{"call i32 @gy_f(", "call i32 @gy_g(", "@rt_append_tagged("} {
		if !strings.Contains(res.IR, want) {
			t.Fatalf("the module does not ask %s:\n%s", want, res.IR)
		}
	}
	main := res.IR[strings.Index(res.IR, "define i32 @main()"):]
	if !strings.Contains(main, "__cans") {
		t.Fatalf("the answer is not materialised to a rooted slot the container roads can read:\n%s", main)
	}
	// The answer is printed by the door that asks the object — @rt_print_container_value (which itself
	// dispatches to rt_print_list / rt_print_list_or_tuple / dict / set by the object's own words), or the
	// str() twin of the same door. A main that prints the handle with %d is what this guards (ADR 0232).
	if !strings.Contains(main, "rt_str_of_container(") &&
		!strings.Contains(main, "rt_print_list") &&
		!strings.Contains(main, "rt_print_container_value(") {
		t.Fatalf("main does not ask the container printer anything:\n%s", main)
	}
}

// TestTheRuleIsAskedOfTheRecordNotOfTheCaller is the row's claim about the SHAPE of the fix: which positions
// may take a declined answer is a fact the record answers, not a decision each door makes for itself — the
// ADR 0232 rule that a question asked in two places is two questions that will drift apart.
func TestTheRuleIsAskedOfTheRecordNotOfTheCaller(t *testing.T) {
	t.Parallel()
	declined := containerAnswer{kind: listAnswer}
	described := containerAnswer{kind: listAnswer, arms: []Expr{&ListLit{Elems: []Expr{&IntLit{Value: 1}}}}, fullyDescribed: true}
	for _, read := range []containerAnswerRead{containerReadWhole, containerReadShape, containerReadMembers} {
		if !declined.takesRead(read) {
			t.Fatalf("a position that asks the object (read %d) declined an answer the scan proved", read)
		}
	}
	for _, read := range []containerAnswerRead{containerReadSlots, containerReadWalk} {
		if declined.takesRead(read) {
			t.Fatalf("a position that reads a slot through the notebooks (read %d) took an undescribed answer", read)
		}
		if !described.takesRead(read) {
			t.Fatalf("a described answer was refused by an ordinary reader (read %d)", read)
		}
	}
}
