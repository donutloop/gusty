package lang

import (
	"regexp"
	"strings"
	"testing"
)

// sorted_container_operand_test.go — the doors BESIDE the loop ask a `sorted`/`reversed` what its
// answer IS (roadmap **Gap R.253**, **ADR 0472**).
//
// ADR 0333 made a container answer a pair: the door that DECIDES an expression is a container and the
// door that BUILDS it are one function, because the failure mode of keeping them apart is not a wrong
// sentence but a wrong module. ADR 0339 then gave the `for` loop two questions to ask of the two
// builtins whose whole job is to hand back a list — write the container down, or name it — and the
// doors beside the loop were left asking nothing. What they did instead, for a program CPython answers,
// was read off the builtin's own value road, which folds the elements into a compile-time constant:
//
//	print(1 in sorted([3, 1, 2]))              %t2 = call i32 @rt_contains(i32 @.lst1, i32 1)
//	print(2 in reversed([1, 2, 3]))            %t2 = call i32 @rt_contains(i32 @.lst1, i32 2)
//	print(sorted([3,1,2]) == sorted([1,2,3]))  %t1 = icmp eq i32 @.lst1, @.lst2
//	xs = []                                    call void @rt_append_tagged(i32 %h2, i32 @.lst1, i32 0)
//	xs.append(sorted([3, 1, 2]))               llc: "global variable reference must have pointer type"
//	                                           → EXIT 2, the code ADR 0166 reserves for a compiler bug
//
// and, one shape away, a refusal at exit 1 for `print(sorted([3, 1, 2]) == [1, 2, 3])` — ADR 0189's
// "comparing a container with *lang.Call needs a tagged value", because the door could not see that the
// call on the other side of the `==` IS the container.
//
// The second half of the row is what the same missing question did to an ELEMENT. Writing a
// `sorted(...)` into a slot — `[sorted([3, 1, 2])]`, `xs[0] = sorted([...])`, `d["k"] = sorted([...])`,
// `xs.append(sorted([...]))` — put the fold's global in the slot's i32, which `llc` refuses; and once
// the slot is written through the container door, the container around it has to know that one of ITS
// slots holds a handle, which is `literalNeedsTags`' question (**ADR 0187**'s rule that no builder
// writes a payload without its tag, **ADR 0457**'s element door beside it).
//
// `reversed` is the half with a second answer to give. Its value in the reference is an ITERATOR: the
// loop and the membership test are right to walk the flipped copy — that is what it yields — but an
// iterator has no `__eq__`, so it is equal to no list, and two calls are two objects. Answering
// `print(reversed([1, 2]) == [2, 1])` by comparing the copy this door builds prints True where the
// reference prints False, at the exit code of success; that is the class ADR 0166 ranks worst, so the
// door answers the reference instead (**Gap R.251**, **Gap R.195** keep the rest of that story).

// containerGlobalInParamIR is the instruction shape this row exists to remove: a container CONSTANT —
// the fold's `@.lstN`/`@.setN`/`@.dictN` global, whose layout is a length plus an array — handed to a
// runtime helper or an `icmp` that takes an i32. `llc` rejects every one of them, and the contract
// charges the compiler for the rejection (ADR 0166).
var containerGlobalInParamIR = regexp.MustCompile(`(?m)(call i32 @rt_contains(?:_tagged)?\([^)]*@\.|call void @rt_(?:append|set_add|put_elem|set_elem|dict_put)(?:_tagged)?\([^)]*@\.|call i32 @rt_container_eq\([^)]*@\.|icmp eq i32 @\.)`)

// membershipAndEqualityAnswered are the programs this row newly answers. `want` is what CPython prints
// for the same source — measured, never remembered — and each row is asked of the compiler, of the JIT,
// and in the integration file of the CLI against the pinned reference.
var membershipAndEqualityAnswered = []struct {
	src  string
	want string
}{
	// the two membership shapes that made `llc` reject the module, and their `not in` twins
	{"print(1 in sorted([3, 1, 2]))", "True"},
	{"print(5 not in sorted([3, 1, 2]))", "True"},
	{"print(2 in reversed([1, 2, 3]))", "True"},
	{"print(1 in sorted([]))", "False"},
	{"print(1 in reversed([]))", "False"},
	{"print(3 in sorted([1, 2, 3], reverse=True))", "True"},
	// the equality door: ADR 0189's refusal, and the `icmp eq i32 @.lst1, @.lst2` that beat it to exit 2
	{"print(sorted([3, 1, 2]) == [1, 2, 3])", "True"},
	{"print([1, 2] == sorted([2, 1]))", "True"},
	{"print(sorted([3, 1, 2]) == sorted([1, 2, 3]))", "True"},
	{"print(sorted([]) == [])", "True"},
	{"print(sorted([3, 1, 2]) == [1, 2, 4])", "False"},
	{"print(sorted([3, 1, 2]) != [1, 2, 3])", "False"},
	{"print(sorted([1, 2, 3], reverse=True) == [3, 2, 1])", "True"},
	{"print(1 in sorted([1, 2]) and 2 in sorted([1, 2]))", "True"},
	// a CONTAINER needle: the haystack is a literal, the needle is the sorted answer, and the door has
	// to match content rather than the two handles
	{"print(sorted([3, 1, 2]) in [[1, 2, 3]])", "True"},
	{"print(sorted([3, 1, 2]) in [[1, 2, 4]])", "False"},
	// the container reached through a variable and through a call: the runtime copy, named
	{"xs = [3, 1, 2]\nprint(sorted(xs) == [1, 2, 3])", "True"},
	{"xs = [3, 1, 2]\nprint(1 in sorted(xs))", "True"},
	{"xs = [3, 1, 2]\nprint(sorted(xs) == sorted(xs))", "True"},
	{"def b():\n    return [3, 1, 2]\nprint(1 in sorted(b()))", "True"},
	{"print(1 in sorted([3, 1, 2][0:2]))", "True"},
	{"print(1 in sorted([3, 1, 2], key=abs))", "True"},
	{"def f(xs):\n    return sorted(xs) == [1, 2, 3]\nprint(f([3, 1, 2]))", "True"},
	// text elements, ordered by the runtime comparator rather than by arrival
	{"print(\"a\" in sorted([\"b\", \"a\"]))", "True"},
	{"print(sorted([\"b\", \"a\"]) == [\"a\", \"b\"])", "True"},
	// NESTED builtins: sorted over a reversed is a list, and the answer to `print` came with the door
	{"print(2 in sorted(reversed([3, 1, 2])))", "True"},
	{"print(sorted(sorted([3, 1, 2])))", "[1, 2, 3]"},
	// an ITERATOR is not the copy: it yields elements, and equals nothing
	{"print(reversed([1, 2]) == [2, 1])", "False"},
	{"print(reversed([1, 2]) != [2, 1])", "True"},
	{"print(reversed([1, 2]) == reversed([1, 2]))", "False"},
	{"print(reversed([1, 2]) in [[2, 1]])", "False"},
	{"print(reversed([1, 2, 3]) == [1, 2, 3])", "False"},
	// the element door: a slot that holds the answer holds the handle of it, with its tag
	{"print([sorted([3, 1, 2])])", "[[1, 2, 3]]"},
	{"print([sorted([3, 1, 2]), 1])", "[[1, 2, 3], 1]"},
	{"print({\"k\": sorted([3, 1, 2])})", "{'k': [1, 2, 3]}"},
	{"print((sorted([3, 1, 2]),))", "([1, 2, 3],)"},
	{"xs = []\nxs.append(sorted([3, 1, 2]))\nprint(xs)", "[[1, 2, 3]]"},
	{"xs = [0, 0]\nxs[0] = sorted([3, 1, 2])\nprint(xs)", "[[1, 2, 3], 0]"},
	{"d = {}\nd[\"k\"] = sorted([3, 1, 2])\nprint(d[\"k\"])", "[1, 2, 3]"},
	// …and the SAME door with the NAMEABLE argument, which is where the name half of the decide door is
	// load-bearing: the fold cannot write a variable down, and reading the builtin's word instead printed
	// the length of the list (`[2]`) where the reference prints the list itself.
	{"xs = [3, 1, 2]\nprint([sorted(xs)])", "[[1, 2, 3]]"},
	{"xs = [3, 1, 2]\nprint({\"k\": sorted(xs)})", "{'k': [1, 2, 3]}"},
	{"def b():\n    return [3, 1, 2]\nprint([sorted(b())])", "[[1, 2, 3]]"},
	{"xs = [sorted([3, 1, 2])]\nprint(xs[0])\nprint(xs[0][0])\nprint(xs == [[1, 2, 3]])", "[1, 2, 3]\n1\nTrue"},
	{"xs = sorted([3, 1, 2])\nxs.append(9)\nprint(xs)", "[1, 2, 3, 9]"},
}

// membershipAndEqualityRefused are the shapes this row leaves refused — each one a program a door cannot
// answer, refused BY NAME at exit 1, which is what keeps the repair from becoming a guess. None of them
// may reach exit 2, and the sentence has to say which half is missing (ADR 0249, ADR 0166's ladder).
var membershipAndEqualityRefused = []struct {
	src      string
	sentence string
	roadmap  string
	cites    bool // the sentence names a roadmap row (the builtin's own ancient sentences do not)
}{
	{"print(1 in sorted([1, \"a\"]))", "more than one kind", "Gap R.248", true},
	{"print(1 in reversed([1, \"a\"]))", "list literal elements must be integers", "Gap R.571", false},
	{"print(1 in sorted(\"bca\"))", "is refused", "Gap R.247", true},
	{"print(1 in sorted([2, 1]) == True)", "comparison chain", "Gap R.53", true},
	{"print(1 in reversed([1, 2, 3]) == True)", "comparison chain", "Gap R.53", true},
	{"print(sorted({\"a\": 1}) == [\"a\"])", "needs a tagged value", "Gap R.572", true},
	{"print(sorted(reversed([3, 1, 2])) == [1, 2, 3])", "needs a tagged value", "Gap R.573", true},
	// The OPTION the builtins refuse, met at the container doors. The value road folds `sorted(xs, True)`
	// to its constant and prints `[3, 2, 1]`; a door that read that word would hand the global to
	// @rt_contains/@rt_set_elem, which is this row's own exit-2 shape on a program the reference STOPS
	// (the option door is **Gap R.247**/**Gap R.249**; the refusal, not an answer, is what ADR 0166 asks).
	{"print(1 in sorted([3, 1, 2], True))", "second POSITIONAL argument is the reference's key", "Gap R.249", true},
	{"print([sorted([3, 1, 2], True)])", "second POSITIONAL argument is the reference's key", "Gap R.249", true},
	// The same argument met at the OTHER slot roads: an element of a set literal, and an `append` to a
	// list the program built. Each road has its own gate that would describe the shape wrongly — the
	// mixed-element-kind gate, Gap J.6 — and each now asks the container door first (**ADR 0249**).
	{"print({sorted([3, 1, 2], True)})", "second POSITIONAL argument is the reference's key", "Gap R.249", true},
	{"xs = []\nxs.append(sorted([3, 1, 2], True))\nprint(xs)", "second POSITIONAL argument is the reference's key", "Gap R.249", true},
	{"d = {}\nd[\"k\"] = sorted([3, 1, 2], True)\nprint(d)", "second POSITIONAL argument is the reference's key", "Gap R.249", true},
	{"print(sorted([3, 1, 2], True) == [3, 2, 1])", "second POSITIONAL argument is the reference's key", "Gap R.249", true},
}

// membershipNeighboursAreUntouched are the roads this row must not disturb: the literal haystack the
// fold answers with no object at all, the loop ADR 0339 already walks, the `len` that already answered,
// and the print that reads the fold. Each is the reference's own program and answers the reference here.
var membershipNeighboursAreUntouched = []struct {
	src  string
	want string
}{
	{"print(1 in [3, 1, 2])", "True"},
	{"print(sorted([3, 1, 2]))", "[1, 2, 3]"},
	{"print(sorted([\"b\", \"a\"]))", "['a', 'b']"},
	{"print(len(sorted([3, 1, 2])))", "3"},
	{"for v in sorted([3, 1, 2]):\n    print(v)", "1\n2\n3"},
	{"for v in reversed([1, 2, 3]):\n    print(v)", "3\n2\n1"},
	{"if sorted([3, 1, 2]):\n    print(\"truthy\")", "truthy"},
	{"xs = [3, 1, 2]\nprint(1 in sorted(xs))", "True"},
	{"print(2 in [1, 2, 3])", "True"},
	{"xs = [3, 1, 2]\nprint(sorted(xs))", "[1, 2, 3]"},
}

// membershipOneOrderingRows ask the SAME call at every door at once: printed, walked, matched and
// compared. One ordering function (`sortedLiteralElems`, **ADR 0258**) is what makes the four lines of
// each program agree, and a row that asks only one of them would never see a disagreement.
var membershipOneOrderingRows = []struct {
	src  string
	want string
}{
	{
		"print(sorted([3, 1, 2]))\nfor v in sorted([3, 1, 2]):\n    print(v)\nprint(1 in sorted([3, 1, 2]))\nprint(sorted([3, 1, 2]) == [1, 2, 3])",
		"[1, 2, 3]\n1\n2\n3\nTrue\nTrue",
	},
	{
		"print(reversed([1, 2, 3]))\nfor v in reversed([1, 2, 3]):\n    print(v)\nprint(3 in reversed([1, 2, 3]))",
		"[1, 2, 3]\n3\n2\n1\nTrue",
	},
	{
		"print(sorted([3, 1, 2], reverse=True))\nprint(3 in sorted([3, 1, 2], reverse=True))\nprint(sorted([3, 1, 2], reverse=True) == [3, 2, 1])",
		"[3, 2, 1]\nTrue\nTrue",
	},
}

// filedNotFixed pins what this row measured and did NOT fix, with the reference's answer beside the
// compiled one. These are exit-0 divergences, so they are pinned rather than skipped: a change that
// fixes one must delete its row here, and a change that silently changes the compiled answer has to
// pass this file first.
var filedNotFixed = []struct {
	src      string
	compiled string
	oracle   string // the reference's own answer, or `Class: message` when it raises
	row      string
}{
	{"print(1 in sorted([1.5, 0.5]))", "True", "False", "Gap R.248"},
	{"print(None in sorted([1, None]))", "True", "TypeError: '<' not supported between instances of 'NoneType' and 'int'", "Gap R.571"},
	{"print(\"a\" in reversed(\"abc\"))", "False", "True", "Gap R.570"},
	{"print(\"c\" in reversed(\"abc\"))", "False", "True", "Gap R.570"},
	{"print(len(reversed([1, 2, 3])) == 3)", "True", "TypeError: object of type 'list_reverseiterator' has no len()", "Gap R.251"},
	{"print([reversed([1, 2, 3])])", "[[3, 2, 1]]", "[<list_reverseiterator object at 0x…>]", "Gap R.251"},
	{"sa = set()\nsa.add(sorted([3, 1, 2]))\nprint(len(sa))", "1", "TypeError: unhashable type: 'list'", "Gap R.574"},
	{"print(1 in sorted([\"b\", \"a\"]))", "True", "False", "Gap R.575"},
}

// TestTheDoorsNeverHandAContainerGlobalToAHelper is the row's IR claim: no module this suite compiles
// puts a container constant where a runtime helper reads an i32. The guard runs over every program in
// every table — answered, refused, neighbour, parity — because the shape is a toolchain rejection
// whatever the program's final verdict is (ADR 0166 counts it as a compiler bug, exit 2).
func TestTheDoorsNeverHandAContainerGlobalToAHelper(t *testing.T) {
	t.Parallel()
	probes := []string{}
	for _, tt := range membershipAndEqualityAnswered {
		probes = append(probes, tt.src)
	}
	for _, tt := range membershipAndEqualityRefused {
		probes = append(probes, tt.src)
	}
	for _, tt := range membershipNeighboursAreUntouched {
		probes = append(probes, tt.src)
	}
	for _, tt := range membershipOneOrderingRows {
		probes = append(probes, tt.src)
	}
	for _, src := range probes {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := Compile(src)
			if err != nil {
				return // a refusal is the other honest answer; the IR cannot contain what was never emitted
			}
			if m := containerGlobalInParamIR.FindString(res.IR); m != "" {
				t.Fatalf("the module hands a container global to an i32 parameter (%s) — the shape llc rejects and the contract files as a compiler bug (ADR 0166):\n%s", m, res.IR)
			}
		})
	}
}

// TestTheGuardIsNotVacuous feeds the guard a module of the shape it exists to catch. A guard that can
// fire on nothing is how a fixed row quietly becomes an untested one (ADR 0232's soundness rule, read
// as a rule about tests).
func TestTheGuardIsNotVacuous(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"\t%t2 = call i32 @rt_contains(i32 @.lst1, i32 1)\n",
		"\t%t5 = call i32 @rt_contains_tagged(i32 %t1, i32 @.lst1, i32 0)\n",
		"  call void @rt_append_tagged(i32 %h2, i32 @.lst1, i32 0)\n",
		"  call void @rt_put_elem(i32 %_xs.ld1, i32 %t5, i32 @.lst2)\n",
		"  call void @rt_dict_put(i32 %t1, i32 %t3, i32 @.lst1)\n",
		"  %t1 = icmp eq i32 @.lst1, @.lst2\n",
		"  call void @rt_set_elem(i32 %t1, i32 0, i32 @.lst1)\n",
		"  call void @rt_dict_put_tagged(i32 %_d.ld1, i32 %t2, i32 @.lst2, i32 4, i32 0)\n",
		"  %t5 = call i32 @rt_contains_tagged(i32 %t1, i32 @.set1, i32 0)\n",
		"  %t5 = call i32 @rt_container_eq(i32 @.lst1, i32 %t3)\n",
	} {
		if containerGlobalInParamIR.MatchString(bad) == false {
			t.Fatalf("the guard let through the very shape it exists to catch:\n%s", bad)
		}
	}
	for _, good := range []string{
		"\t%t4 = call i32 @rt_contains(i32 %t2, i32 1)\n",
		"  call void @rt_append_tagged(i32 %h2, i32 %t7, i32 4)\n",
		"  %t5 = call i32 @rt_container_eq(i32 %t1, i32 %t3)\n",
		"  %t3 = icmp eq i32 %t1, %t2\n",
	} {
		if containerGlobalInParamIR.MatchString(good) {
			t.Fatalf("the guard fired on a register, which is what every answered row emits:\n%s", good)
		}
	}
}

// TestTheMembershipAndEqualityDoorsAnswerTheReference is the row's answer, on the compiler and the JIT.
// A refusal is a failure here: these are the reference's own programs, and the door exists precisely so
// they are not refused (ADR 0249).
func TestTheMembershipAndEqualityDoorsAnswerTheReference(t *testing.T) {
	t.Parallel()
	for _, tt := range membershipAndEqualityAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tt.src, got, tt.want)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), tt.want)
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, tt.want)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTheDoorsRefuseWhatTheBuiltinsRefuse is the guard against the other failure: a door that answers
// whatever it is handed would turn ADR 0191's honest mixed-kind refusal, and ADR 0339's refusal of an
// argument it cannot walk, into a fabricated verdict. Every row stays a refusal that names its missing
// half, and none of them may reach exit 2.
func TestTheDoorsRefuseWhatTheBuiltinsRefuse(t *testing.T) {
	t.Parallel()
	for _, tt := range membershipAndEqualityRefused {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tt.src)
			if err == nil {
				t.Fatalf("%q compiled and answered through the door; the row expects the refusal that names %s", tt.src, tt.roadmap)
			}
			msg := err.Error()
			if strings.Contains(msg, "llc") || strings.Contains(msg, "Verification") {
				t.Fatalf("%q reached the toolchain (exit 2 territory): %s", tt.src, msg)
			}
			if !strings.Contains(msg, tt.sentence) {
				t.Fatalf("%q refused with %q, which does not name the half the reference is missing (%s)", tt.src, msg, tt.roadmap)
			}
			if tt.cites && !strings.Contains(msg, "roadmap") {
				t.Fatalf("%q refused without citing the roadmap row that owns the half it lacks: %s", tt.src, msg)
			}
		})
	}
}

// TestTheNeighboursStillAnswer holds the row to its promise of a narrow door: the roads that already
// answered — the literal haystack the fold answers with no object, the loop, `len`, `print` — keep their
// answers, byte for byte.
func TestTheNeighboursStillAnswer(t *testing.T) {
	t.Parallel()
	for _, tt := range membershipNeighboursAreUntouched {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); it answered before this row", tt.src, got)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), tt.want)
			}
		})
	}
}

// TestTheDoorsShareOneOrderingWithPrintAndTheLoop is the one-answer rule: `print`, the `for` loop, the
// membership test and the comparison all read the same ordered elements. The rows ask ALL of them in one
// program, which is the only spelling in which a disagreement between two doors can be seen at all.
func TestTheDoorsShareOneOrderingWithPrintAndTheLoop(t *testing.T) {
	t.Parallel()
	for _, tt := range membershipOneOrderingRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s)", tt.src, got)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q — two doors read two orderings", tt.src, strings.TrimSpace(got), tt.want)
			}
		})
	}
}

// TestTheFoldStillAnswersWhereItAlwaysAnswered is the counter-row to the row's own IR claim: the door
// takes the CONTAINER doors, and the value position keeps the fold it has always had. A repair that
// deleted the fold would pass the guard above by never emitting the global at all.
func TestTheFoldStillAnswersWhereItAlwaysAnswered(t *testing.T) {
	t.Parallel()
	// The value position — a name bound to the builtin's answer — keeps the constant the value road has
	// always emitted. The door takes the CONTAINER doors (`in`, `==`, a slot) and nothing else; a repair
	// that deleted the fold everywhere would pass the guard above by never emitting the global at all.
	const bound = "xs = sorted([3, 1, 2])\nprint(xs[0])"
	ir := mustCompileIR(t, bound)
	if !strings.Contains(ir, "@.lst") {
		t.Fatalf("%q no longer emits the fold's container global — the value road's constant answer is what this row left alone:\n%s", bound, ir)
	}
	if got, refused := compiledOutOrRefusal(t, bound); refused || strings.TrimSpace(got) != "1" {
		t.Fatalf("%q printed %q (refused=%v), want 1", bound, got, refused)
	}
	// …and the door's own build is a register, not that global: the membership spelling of the same call
	// asks the helper for an i32 and gets one.
	const inSrc = "print(1 in sorted([3, 1, 2]))"
	inIR := mustCompileIR(t, inSrc)
	if !regexp.MustCompile(`call i32 @rt_contains\(i32 %`).MatchString(inIR) {
		t.Fatalf("%q does not ask @rt_contains a register — the door built nothing:\n%s", inSrc, inIR)
	}
	if strings.Contains(inIR, "rt_contains(i32 @.") {
		t.Fatalf("%q still hands the fold's global to @rt_contains:\n%s", inSrc, inIR)
	}
}

// TestTheDecideDoorAndTheBuildDoorAreOneQuestion asks the pair directly, of the node the PARSER built —
// the same call the compiled leg sees. `sortedReversedIsAContainer` is what `isContainerExpr` asks; the
// build half is `containerOperand`, and a shape the first admits and the second refuses would hand the
// caller an empty register, while a shape the first refuses and the second builds is a container the
// doors walk past (**ADR 0333**'s pair, roadmap **Gap R.253**).
func TestTheDecideDoorAndTheBuildDoorAreOneQuestion(t *testing.T) {
	t.Parallel()
	// The fold's own shapes, and the shapes it declines, as the parser writes them.
	cases := []struct {
		expr      string
		fold      bool // the compiler can WRITE the container down (the builtin's own literal road)
		name      bool // the compiler can NAME the container the runtime builds (ADR 0339's gate)
		container bool // the decide door: is this expression a container?
		beyond    bool // …or an ordering builtin the doors must REFUSE rather than read as a word?
	}{
		{"sorted([3, 1, 2])", true, true, true, false},
		{"sorted([3, 1, 2], reverse=True)", true, true, true, false},
		{"sorted([])", true, false, true, false}, // nothing to order is an order the fold answers
		{"reversed([1, 2, 3])", true, true, true, false},
		{"reversed([])", true, false, true, false},
		{"sorted([3, 1, 2], key=abs)", false, true, true, false}, // the keyed door's copy: named, not written (ADR 0340)
		{"sorted([1.5, 0.5])", false, false, false, false},       // no comparator for the element (Gap R.248)
		{"sorted([1, \"a\"])", false, false, false, false},       // one kind per container, or nothing
		// A second POSITIONAL argument is the reference's `key`: the builtin's literal gate
		// (`reverseFlagIsSpellable`, shared with the loop's fold) declines it, so NEITHER door builds it,
		// and the third clause is what makes the value doors refuse it rather than read the fold's global
		// (Gap R.247, Gap R.249, ADR 0340; the exit-2 class is ADR 0166's).
		{"sorted([3, 1, 2], True)", false, false, false, true},
		{"sorted(reversed([3, 1, 2]))", false, false, false, false}, // the argument is a list neither door can reach
		{"reversed([1, \"a\"])", false, false, false, false},        // the builtin's own emitList refusal
		{"reversed(\"abc\")", false, false, false, false},           // a text, not a container — the text roads own it
		{"sorted(\"bca\")", false, false, false, false},             // Gap R.247's argument
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			g := newSortedContainerProbeGen()
			call, ok := parseExprForTest(t, "x = "+tc.expr).(*Call)
			if !ok {
				t.Fatalf("%q does not parse as a call expression", tc.expr)
			}
			if got := g.sortedReversedIsAContainer(call); got != tc.container {
				t.Fatalf("the decide door said %v for %q, want %v", got, tc.expr, tc.container)
			}
			if _, ok := g.sortedReversedFoldIsAContainer(call); ok != tc.fold {
				t.Fatalf("the fold half said %v for %q, want %v", ok, tc.expr, tc.fold)
			}
			if ok := g.sortedReversedContainerIsNameable(call); ok != tc.name {
				t.Fatalf("the naming half said %v for %q, want %v", ok, tc.expr, tc.name)
			}
			// The pair rule, asserted rather than assumed: one door composes the two halves, so a caller
			// that decides cannot be handed a shape the builder declines, and cannot walk past one it builds.
			if got := g.sortedReversedIsAContainer(call); got != (tc.fold || tc.name) {
				t.Fatalf("the decide door is the two halves: %q decided %v, fold %v, name %v", tc.expr, got, tc.fold, tc.name)
			}
			if got := g.sortedReversedIsBeyondTheDoor(call); got != tc.beyond {
				t.Fatalf("the refusal clause said %v for %q, want %v", got, tc.expr, tc.beyond)
			}
			// The value doors ask (build || refuse): a shape that is neither is the only one they may read
			// as a word, and a shape that is both would answer and refuse the same program at once.
			if got := g.sortedReversedDoorIsAskedOf(call); got != (tc.container || tc.beyond) {
				t.Fatalf("the doors' question is build-or-refuse: %q asked %v, container %v, beyond %v", tc.expr, got, tc.container, tc.beyond)
			}
			// ...and when the door says container, the tag the element road writes for it is the container's
			// own — the two read the same two halves (**ADR 0233**'s tag-and-store pair, **ADR 0457**'s element
			// door). A declined shape is the tag road's own business: an untagged word is what a value is.
			if tc.container {
				if tag, okTag := g.elemKindTag(call); !okTag || tag != int32(TagList) {
					t.Fatalf("the tag road and the container door disagree about %q: tag %d, ok %v", tc.expr, tag, okTag)
				}
			}
		})
	}
}

// newSortedContainerProbeGen builds the barest `irGen` the three doors can be asked of: the maps the
// container records read have to exist (a nil map reads fine, a nil map behind a `for … range` does not),
// and NOTHING else — which is the point. A door that needed a program to have been compiled to answer the
// question would be a door that only agrees with itself after the fact; asked of the empty generator, the
// three clauses answer the expression the parser built, which is what the pair table asserts
// (roadmap **Gap R.253**, **ADR 0472**, **ADR 0333**).
func newSortedContainerProbeGen() *irGen {
	return &irGen{
		listVars:      map[string]bool{},
		mixedLists:    map[string]bool{},
		runtimeDicts:  map[string]bool{},
		runtimeSets:   map[string]bool{},
		listElemStr:   map[string]bool{},
		listElemInt:   map[string]bool{},
		internedVars:  map[string]bool{},
		containerLits: map[string]Expr{},
		containerRet:  map[string]containerAnswer{},
	}
}

// TestTheKindDoorNamesWhatTheElementsAre asserts the STRING the tag half returns, at the door that returns
// it. Why a test of its own: `sortedReversedContainerIsNameable` conjoins the walkability gate and the kind
// gate, so deleting either one reddens the same program rows — which means the program rows witness the
// CONJUNCTION and not the second clause. The kind is a string today precisely because the element road and
// the loop will want to say what an element IS (**ADR 0233**'s tag-and-store pair, **Gap R.248**'s missing
// comparator); a door that returned the right boolean with the wrong name behind it would leave every
// program row green. This is the row that would notice (roadmap **Gap R.253**, **ADR 0472**).
func TestTheKindDoorNamesWhatTheElementsAre(t *testing.T) {
	t.Parallel()
	cases := []struct {
		expr string
		kind string // "", "number" or "text" — the loop's own vocabulary
		ok   bool
	}{
		// The orderings the numeric comparator answers.
		{"sorted([3, 1, 2])", "number", true},
		{"sorted([3, 1, 2], reverse=True)", "number", true},
		{"sorted([3, 1, 2], key=abs)", "number", true}, // the keyed door's copy — the kind is not the key's business
		{"reversed([3, 1, 2])", "number", true},
		// Text: `sorted` asks the comparator question too, `reversed` does not.
		{"sorted([\"b\", \"a\"])", "text", true},
		{"reversed([\"b\", \"a\"])", "text", true},
		// The argument's OWN kind is what is asked: a slice copies its base, and @rt_sort chooses its
		// comparator from the spelling it was handed, so a text arriving through a slice is the comparator's
		// refusal while a number arrives fine (ADR 0191, Gap R.247).
		{"sorted([3, 1, 2][0:2])", "number", true},
		// One kind per container or nothing at all — the mixed list, and the float list whose comparator the
		// sort's own road cannot choose (**Gap R.248**: this is the answer that keeps a tag from being invented).
		{"sorted([1, \"a\"])", "", false},
		{"reversed([1, \"a\"])", "", false},
		{"sorted([1.5, 0.5])", "", false},
		// A dict's elements are its KEYS; the tag road reads a dict literal's VALUES. The argument is what
		// is asked, and the pair that would be built is a number tag over text — which is why this is no kind.
		{"sorted({\"a\": 1})", "", false},
		{"reversed({\"a\": 1})", "", false},
		// Neither ordering builtin answers a bare text, so neither names its characters as a container kind.
		{"sorted(\"bca\")", "", false},
		{"reversed(\"abc\")", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			g := newSortedContainerProbeGen()
			call, ok := parseExprForTest(t, "x = "+tc.expr).(*Call)
			if !ok {
				t.Fatalf("%q does not parse as a call expression", tc.expr)
			}
			kind, okKind := g.sortedReversedContainerElemKind(call)
			if okKind != tc.ok || kind != tc.kind {
				t.Fatalf("the kind door said (%q, %v) for %q, want (%q, %v)", kind, okKind, tc.expr, tc.kind, tc.ok)
			}
			// And the gate that consumes it agrees: no kind, no container to name.
			if ok := g.sortedReversedContainerIsNameable(call); ok && !okKind {
				t.Fatalf("%q named a container with no kind behind it", tc.expr)
			}
		})
	}
}

// TestWhatTheRowLeavesFiledNotFixed pins the divergences this row measured and filed rather than fixed —
// each with the reference's answer in the table, so the next cycle inherits the measurement instead of
// re-deriving it (and cannot fix one by deleting the row: the compiled answer is asserted too).
func TestWhatTheRowLeavesFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tt := range filedNotFixed {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the filed row %s expects the compiled answer %q", tt.src, got, tt.row, tt.compiled)
			}
			if strings.TrimSpace(got) != tt.compiled {
				t.Fatalf("%q: the compiled leg moved from %q to %q — %s was fixed, or this row drifted; update both",
					tt.src, tt.compiled, strings.TrimSpace(got), tt.row)
			}
		})
	}
}

// TestTheRowsAnswersAreOnTheRecord is the record leg: every program the row newly answers has an entry
// in testdata/interpreter-golden.json, and the entry holds the same stdout the table asserts. A missing
// entry fails the case, so coverage of this row cannot be deleted by deleting a record (ADR 0302).
func TestTheRowsAnswersAreOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tt := range membershipAndEqualityAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			g := goldenLookup(t, tt.src)
			if !g.HasStd {
				t.Fatalf("%q is on the record with no stdout, but the row prints %q", tt.src, tt.want)
			}
			if g.Stdout != tt.want+"\n" {
				t.Fatalf("%q: the record holds %q and the row expects %q — one of them has to move, and the reference is the referee", tt.src, g.Stdout, tt.want+"\n")
			}
		})
	}
}

// The door above asks the parser's own node: `parseExprForTest` (string_containers_test.go) is the one
// helper that builds it, so this row does not mint an AST the compiler never constructs.
