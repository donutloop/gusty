package lang

import (
	"strings"
	"testing"
)

// module_container_test.go — a compiled function body is handed the module's container
// (roadmap Gap R.35's container half; ADR 0407).
//
// ADR 0227 gave the compiled backend one mechanism for module state and built it twice: a name the
// module rebinds lives in a `global i32` that `@main` writes and any body loads, and — since ADR 0376
// — a name whose every binding is a literal double lives in a `global double`. A container was left out
// on purpose both times, because a container's word is a heap handle that has to be rooted and whose
// element operations live in the name-keyed notebooks ADR 0341 settles at a binding. Every body that
// named a module-level list therefore drew the module-state refusal at exit 1:
//
//	xs = [1, 2, 3]
//	def n() -> int:
//	    return len(xs)
//	print(n())            # CPython 3 · this toolchain, before ADR 0407: exit 1, Gap R.35's sentence
//
// The answer is not a third global. A copy of the module's literal, bound in the reading frame by
// `stmtAssign`, gives the frame the slot, the root, the kind claim, the element notebook, the mixed bit
// and the fold record — all written by the one road that has ever written them — and the twenty-odd
// doors that read those records by name ask a frame that owns what they are told about. A load of a
// shared handle with a fresh opinion beside it is what the frame-slot audit catches as
// `%h1 = load i32, i32* %_xs` in a `define` that never allocated `%_xs`: exit 2, the code ADR 0166
// reserves for this compiler's bug.
//
// A copy is honest only where nothing writes through the object, so the gate (`module_container.go`) is
// the whole feature: a name earns the road only when its ONE module binding is a plain assignment of a
// container literal whose every slot is itself a literal, when no `for`/`with as`/`match`/unpacking/import
// binds the name, and when nothing anywhere in the program — under the name or any name the handle
// reached — appends, assigns a slot, augments, hands it out of a `return`, or asks it an identity
// question. Every refusal row below is that gate declining, and every one of them is a program CPython
// answers: the row is about what the gate will NOT buy, because a gate that guessed here would trade an
// honest refusal at exit 1 for a stale container at exit 0.

// moduleContainerPrograms is the answer table. Every `want` is CPython's own bytes, every source is in
// the record, and the guard at the bottom fails if the record stops covering the table.
var moduleContainerPrograms = []struct {
	name, src, want string
}{
	// ---- the row the half-row was named for.
	{"a body takes the module list's length",
		"xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n", "3\n"},
	{"a body prints the module's list",
		"xs = [1, 2, 3]\n\ndef show():\n    print(xs)\n\nshow()\n", "[1, 2, 3]\n"},
	{"a body prints the module's dict",
		"d = {\"a\": 1, \"b\": 2}\n\ndef show():\n    print(d)\n\nshow()\n", "{'a': 1, 'b': 2}\n"},
	{"a body prints the module's set",
		"s = {1, 2, 3}\n\ndef show():\n    print(s)\n\nshow()\n", "{1, 2, 3}\n"},
	{"a body prints the module's tuple",
		"t = (1, 2)\n\ndef show():\n    print(t)\n\nshow()\n", "(1, 2)\n"},
	// ---- the module keeps its own object: the copy is the frame's, the literal is main's.
	{"the module's list is still there after the call",
		"xs = [1, 2, 3]\n\ndef show():\n    print(xs)\n\nshow()\nprint(xs)\n", "[1, 2, 3]\n[1, 2, 3]\n"},
	{"the module and a body read the same list in one print",
		"xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n(), xs)\n", "3 [1, 2, 3]\n"},
	{"two calls read the same literal",
		"xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n(), n())\n", "3 3\n"},
	// ---- the roads a container is asked about, one door at a time.
	{"a body walks the module's list",
		"xs = [1, 2, 3]\n\ndef total() -> int:\n    t = 0\n    for x in xs:\n        t += x\n    return t\n\nprint(total())\n", "6\n"},
	{"a body walks the module's dict by its keys",
		"d = {\"a\": 1, \"b\": 2}\n\ndef keys():\n    for k in d:\n        print(k)\n\nkeys()\n", "a\nb\n"},
	{"a body reads one slot of the module's list",
		"xs = [\"b\", \"a\"]\n\ndef show():\n    print(xs[0])\n\nshow()\n", "b\n"},
	{"a body reads one slot of the module's dict",
		"d = {1: \"a\", 2: \"b\"}\n\ndef show():\n    print(d[1])\n\nshow()\n", "a\n"},
	{"a body reads a container inside the module's list",
		"xs = [[1, 2], [3, 4]]\n\ndef show():\n    print(xs[1])\n\nshow()\n", "[3, 4]\n"},
	{"a body measures a container inside the module's dict",
		"d = {\"a\": [1, 2], \"b\": [3]}\n\ndef show():\n    print(len(d[\"b\"]))\n\nshow()\n", "1\n"},
	{"a body renders the module's list with str",
		"xs = [1, 2, 3]\n\ndef text():\n    return str(xs)\n\nprint(text())\n", "[1, 2, 3]\n"},
	{"a body compares the module's list to a literal",
		"xs = [1, 2, 3]\n\ndef same():\n    return xs == [1, 2, 3]\n\nprint(same())\n", "True\n"},
	{"a body sorts the module's list for a print",
		"xs = [3, 1, 2]\n\ndef show():\n    print(sorted(xs))\n\nshow()\n", "[1, 2, 3]\n"},
	{"a body asks membership of a parameter against the module's list",
		"xs = [1, 2, 3]\n\ndef has(v):\n    return v in xs\n\nprint(has(2), has(9))\n", "True False\n"},
	{"a body reads every slot of the module's list",
		"xs = [1, 2, 3]\n\ndef show():\n    print(len(xs), xs[0], xs[1], xs[2])\n\nshow()\n", "3 1 2 3\n"},
	{"a body asks the module's list for membership",
		"xs = [1, 2, 3]\n\ndef has():\n    return 2 in xs\n\nprint(has())\n", "True\n"},
	{"a body asks the module's dict for membership",
		"d = {\"a\": 1}\n\ndef has():\n    return \"a\" in d\n\nprint(has())\n", "True\n"},
	{"a body asks whether the module's list is empty",
		"xs = [1, 2, 3]\n\ndef empty():\n    return not xs\n\nprint(empty())\n", "False\n"},
	{"a body builds a fresh list out of the module's list",
		"xs = [1, 2, 3]\n\ndef doubled():\n    out = []\n    for x in xs:\n        out.append(x * 2)\n    return out\n\nprint(doubled())\n", "[2, 4, 6]\n"},
	// ---- the value a body hands back is a value, and the module's object never leaves the frame.
	{"a body folds the module's list into its own accumulator",
		"xs = [3, 1, 2]\n\ndef total():\n    t = 0\n    for v in xs:\n        t = t + v\n    return t\n\nprint(total())\n", "6\n"},
	{"a body returns a verdict built out of the module's list",
		"xs = [1, 2, 3]\n\ndef big():\n    return len(xs) > 1\n\nprint(big())\n", "True\n"},
	// ---- several bodies, several containers, several doors in one program.
	{"two bodies read the same module list",
		"xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\ndef show():\n    print(xs)\n\nprint(n())\nshow()\n", "3\n[1, 2, 3]\n"},
	{"one body reads two module containers",
		"xs = [1, 2, 3]\nd = {\"a\": 1}\n\ndef show():\n    print(len(xs), len(d))\n\nshow()\n", "3 1\n"},
	{"a body calls a body that reads the module's list",
		"xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\ndef m() -> int:\n    return n()\n\nprint(m())\n", "3\n"},
	{"a recursive body reads the module's list at every level",
		"xs = [1, 2, 3]\n\ndef down(n: int) -> int:\n    if n <= 0:\n        return 0\n    return len(xs) + down(n - 1)\n\nprint(down(2))\n", "6\n"},
	{"a method reads the module's list",
		"xs = [1, 2]\n\nclass C:\n    def show(self):\n        print(xs)\n        return 0\n\nC().show()\n", "[1, 2]\n"},
	{"a method measures the module's list",
		"xs = [1, 2, 3]\n\nclass C:\n    def n(self):\n        return len(xs)\n\nprint(C().n())\n", "3\n"},
	{"a body reads a list the module bound above another def",
		"xs = [1, 2, 3]\n\ndef helper(v):\n    return v + 1\n\ndef show():\n    print(len(xs), helper(1))\n\nshow()\n", "3 2\n"},
	{"a body reads the module's list beside the module's int and double",
		"COUNT = 4\nRATE = 2.5\nxs = [1, 2]\n\ndef show():\n    print(len(xs), COUNT, RATE)\n\nshow()\n", "2 4 2.5\n"},
	// ---- the empty spellings, and the element kinds.
	{"the empty list is a container a body reads",
		"xs = []\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n", "0\n"},
	{"the empty dict is a container a body reads",
		"d = {}\n\ndef n() -> int:\n    return len(d)\n\nprint(n())\n", "0\n"},
	{"a body reads a list of floats",
		"xs = [1.5, 2.5]\n\ndef show():\n    print(xs)\n\nshow()\n", "[1.5, 2.5]\n"},
	{"a body reads a list of verdicts",
		"xs = [True, False]\n\ndef show():\n    print(xs)\n\nshow()\n", "[True, False]\n"},
	{"a body reads a tuple of tuples",
		"t = (1, (2, 3))\n\ndef n() -> int:\n    return len(t[1])\n\nprint(n())\n", "2\n"},
	// ---- a slot the body hands back: the escape question, asked of the literal the module bound.
	// An INTEGER slot travels — it is a number, and the caller cannot write through a number. Every other
	// element kind keeps the refusal (the table below) because the road out of a `return` carries an integer
	// honestly and nothing else, which is roadmap Gap R.400's half, not this row's.
	{"a body hands back a slot of the module's list",
		"xs = [1, 2, 3]\n\ndef first():\n    return xs[0]\n\nprint(first())\n", "1\n"},
	{"a body hands back the last slot, spelled from the other end",
		"xs = [1, 2, 3]\n\ndef last():\n    return xs[-1]\n\nprint(last())\n", "3\n"},
	{"a body hands back a slot of the module's tuple",
		"t = (1, 9)\n\ndef one():\n    return t[1]\n\nprint(one())\n", "9\n"},
	{"a body hands back a dict entry",
		"d = {\"a\": 1, \"b\": 2}\n\ndef one():\n    return d[\"b\"]\n\nprint(one())\n", "2\n"},
	{"a body adds two slots of the module's list",
		"xs = [1, 2, 3]\n\ndef s():\n    return xs[0] + xs[2]\n\nprint(s())\n", "4\n"},
	{"a body reads a slot of a list inside the module's list",
		"nested = [[1, 2], [3, 4]]\n\ndef s():\n    return nested[0][0] + nested[1][1]\n\nprint(s())\n", "5\n"},
	{"a body reads a slot three deep",
		"deep = [[[1]]]\n\ndef one():\n    return deep[0][0][0]\n\nprint(one())\n", "1\n"},
	{"a body asks membership of a list inside the module's list",
		"nested = [[1, 2], [3, 4]]\n\ndef member():\n    return 3 in nested[1]\n\nprint(member())\n", "True\n"},
	{"a body builds a fresh list out of the module's slots",
		"xs = [1, 2]\nd = {\"a\": 7}\n\ndef pair():\n    return [xs[1], d[\"a\"]]\n\nprint(pair())\n", "[2, 7]\n"},
	{"the module keeps its object while a body hands back a slot",
		"xs = [1, 2, 3]\n\ndef first():\n    return xs[0]\n\nprint(first(), xs, len(xs))\n", "1 [1, 2, 3] 3\n"},
	// ---- shadowing: a body that BINDS the name owns it, and the module's literal never enters that frame.
	{"a body that binds the name reads its own list",
		"xs = [1, 2, 3]\n\ndef show():\n    xs = [7, 8]\n    print(xs)\n\nshow()\nprint(xs)\n", "[7, 8]\n[1, 2, 3]\n"},
	{"a parameter named like the module's list is the parameter's",
		"xs = [1, 2, 3]\n\ndef show(xs):\n    print(xs)\n\nshow([9])\nprint(xs)\n", "[9]\n[1, 2, 3]\n"},
	// ---- the control that says this row did not quietly become module state: a name the module rebinds
	// keeps the i32 door it always had, and a container no body reads keeps main's alloca.
	{"a module container no body reads is still main's own binding",
		"xs = [1, 2, 3]\nprint(xs)\n", "[1, 2, 3]\n"},
}

// TestACompiledBodyIsHandedTheModulesContainer is the answer leg: the compiled run answers what CPython
// answers, and the record is asked for the same source so a fix here cannot quietly disagree with the
// answer on record.
//
// Serial on purpose, like the ADR 0376 table beside it: the record-coverage counter below is one number
// for the whole table, and a parallel subtest would check it before the cases that increment it had run.
func TestACompiledBodyIsHandedTheModulesContainer(t *testing.T) {
	asked := 0
	for _, tc := range moduleContainerPrograms {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: the compiled leg ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if HasGoldenAnswer(tc.src) {
				asked++
				want := goldenStdout(silentReporter{}, tc.src)
				if out := compiledRunIsTheBackend(t, tc.src); out != want {
					t.Errorf("%s: the compiled leg ran %q, the record says %q\nsrc: %s", tc.name, out, want, tc.src)
				}
			}
		})
	}
	if asked == 0 {
		t.Fatal("no row of this table is covered by the record: the record leg is not being walked")
	}
}

// TestTheModuleContainerBindingIsAFrameBindingAndNotAGlobalLoad is the IR leg. An answer table can be
// satisfied by a lucky constant; the shape of the binding is what makes the answer mean something. The
// copy is built INSIDE the reading body's own `define` — slot, root, allocator call, element writes — and
// what the body then reads is that slot, not a module global: the two module doors that were here first
// (`@gy_mod_<name>`, `moduleSlots` and `moduleFloatSlots`) are denied here because a container that
// reached the screen through one of them would be a handle with nobody's notebook beside it.
func TestTheModuleContainerBindingIsAFrameBindingAndNotAGlobalLoad(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, sym string
		need           []string
		deny           []string
	}{
		{
			name: "the copy is built in the body that reads it",
			src:  "xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n",
			sym:  "gy_n",
			need: []string{
				`%_xs = alloca i32`,
				`store i32 0, i32* %_xs`,
				`call void @rt_root_put(i32* %_xs)`,
				`call i32 @rt_alloc(i32 1)`,
				`call void @rt_set_elem(i32 %h`,
				`store i32 %h`,
				`call i32 @rt_list_len(i32 %h`,
			},
			// Neither of the two module doors' spellings: the container is not module state, it is a
			// binding the frame owns, and a global here would be one name with two owners.
			deny: []string{`@gy_mod_xs`, `global i32`},
		},
		{
			// The restraint row: main builds the module's literal exactly once and the body builds its
			// own. A copy that replaced the module's binding would make the module's own print read a
			// different object than the one the program wrote.
			name: "the module still builds its own literal in main",
			src:  "xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\nprint(xs)\n",
			sym:  "main",
			need: []string{`%_xs = alloca i32`, `call void @rt_root_put(i32* %_xs)`, `call i32 @rt_alloc(i32 1)`},
		},
		{
			// A body that takes the name for itself is NOT handed the module's copy — the frame already
			// owns the spelling, and two bindings of one name in one frame is the Gap R.258 shape.
			name: "a body that binds the name mints no second slot for it",
			src:  "xs = [1, 2, 3]\n\ndef show():\n    xs = [7, 8]\n    print(xs)\n\nshow()\n",
			sym:  "gy_show",
			// One binding, and it is the BODY's literal — 7 and 8, not the module's 1, 2, 3.
			need: []string{`%_xs = alloca i32`, `call void @rt_set_elem(i32 %h1, i32 0, i32 7)`},
			deny: []string{`@gy_mod_xs`, `i32 0, i32 1)` /* the module's own first element */},
		},
		{
			// The dictionary road: the copy writes ENTRIES, not slots, and the door that reads one asks
			// the same frame.
			name: "a dict copy writes its entries through the dict door",
			src:  "d = {\"a\": 1}\n\ndef n() -> int:\n    return len(d)\n\nprint(n())\n",
			sym:  "gy_n",
			need: []string{`%_d = alloca i32`, `call void @rt_root_put(i32* %_d)`, `call void @rt_dict_put(i32 %h1`, `store i32 %h1, i32* %_d`, `call i32 @rt_dict_len`},
			deny: []string{`@gy_mod_d`},
		},
		{
			// A method is emitted by its own road (`emitMethod`), and takes the same prologue.
			name: "a method's frame gets the same binding",
			src:  "xs = [1, 2]\n\nclass C:\n    def show(self):\n        print(xs)\n        return 0\n\nC().show()\n",
			sym:  "gy_C_show",
			need: []string{`%_xs = alloca i32`, `call void @rt_root_put(i32* %_xs)`},
			deny: []string{`@gy_mod_xs`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			body := defineBodyOf(t, res.IR, tc.sym)
			for _, frag := range tc.need {
				if !strings.Contains(body, frag) {
					t.Errorf("%s: the body of @%s has no %q\nsrc: %s\n%s", tc.name, tc.sym, frag, tc.src, body)
				}
			}
			for _, frag := range tc.deny {
				if strings.Contains(body, frag) {
					t.Errorf("%s: the body of @%s carries %q, which is the road this row does not take\nsrc: %s", tc.name, tc.sym, frag, tc.src)
				}
			}
		})
	}
}

// TestTheCopyIsBuiltBeforeTheBodyReadsIt pins the ORDER, which a Contains check cannot: a body whose
// read precedes its binding is the frame-slot audit's own complaint — a load of a slot that holds nothing
// yet, answered at the exit code of success.
func TestTheCopyIsBuiltBeforeTheBodyReadsIt(t *testing.T) {
	t.Parallel()
	src := "xs = [1, 2, 3]\n\ndef n() -> int:\n    return len(xs)\n\nprint(n())\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	body := defineBodyOf(t, res.IR, "gy_n")
	built := strings.Index(body, "store i32 %h")
	read := strings.Index(body, "call i32 @rt_list_len")
	if built < 0 || read < 0 {
		t.Fatalf("the body neither builds nor reads the copy\n%s", body)
	}
	if built > read {
		t.Errorf("the body reads the container before the copy is written into it\n%s", body)
	}
}

// moduleContainerRefusals is what the gate will NOT buy. Each row is a program CPython answers and this
// backend stops at exit 1, and the sentence has to say why — the same family of sentences that has named
// Gap R.35 for five weeks. The rows are grouped by the clause of the gate that declined them, because the
// point of the table is that the copy answers whichever literal the emitter copied, not whichever object
// the call reads: a gate that let any of these through would trade an honest refusal for a stale
// container printed at exit 0, which this project ranks above any refusal.
var moduleContainerRefusals = []struct {
	name, src, want string
}{
	// ---- the object escapes the frame, so a copy is not the object.
	{"a body that hands the module's list back",
		"xs = [1, 2, 3]\n\ndef give():\n    return xs\n\nprint(give())\n", "bound at module level"},
	{"a body that hands back a slot of the module's list",
		"xs = [[1, 2]]\n\ndef give():\n    return xs[0]\n\nprint(give())\n", "index of a non-literal variable"},
	{"a body that hands back the module's dict",
		"d = {\"a\": 1}\n\ndef give():\n    return d\n\nprint(len(give()))\n", "bound at module level"},
	// ---- the program WRITES through the object, under the name or under a name the handle reached.
	{"the module appends and a body reads the list",
		"xs = [1, 2, 3]\n\ndef show():\n    print(xs)\n\nxs.append(4)\nshow()\n", "bound at module level"},
	{"the module writes a slot and a body reads the list",
		"xs = [1, 2, 3]\n\nxs[0] = 9\n\ndef show():\n    print(xs)\n\nshow()\n", "bound at module level"},
	{"the module augments the list and a body reads it",
		"xs = [1, 2, 3]\n\nxs += [4]\n\ndef show():\n    print(xs)\n\nshow()\n", "bound at module level"},
	{"the module rebinds the name and a body reads it",
		"xs = [1, 2, 3]\n\ndef show():\n    print(xs)\n\nxs = [4, 5]\nshow()\n", "bound at module level"},
	{"a body writes through the module's list",
		"xs = [1, 2, 3]\n\ndef push():\n    xs.append(4)\n\npush()\nprint(xs)\n", "bound at module level"},
	{"the module aliases the list and writes through the alias",
		"xs = [1, 2, 3]\n\ndef show():\n    print(xs)\n\nys = xs\nys.append(9)\nshow()\n", "bound at module level"},
	{"a body writes through an element of the module's list",
		"xs = [[1, 2]]\n\ndef push():\n    xs[0].append(3)\n\npush()\nprint(xs)\n", "receiver that is not a text"},
	{"the module writes through a loop variable over its list",
		"xs = [[1], [2]]\n\ndef show():\n    print(xs)\n\nfor row in xs:\n    row.append(3)\nshow()\n", "bound at module level"},
	{"the module's list is emptied by a method a body does not call",
		"xs = [1, 2, 3]\n\ndef show():\n    print(xs)\n\nxs.clear()\nshow()\n", "bound at module level"},
	// ---- a road this door does not write is not this door's binding.
	{"the module binds the name with a for loop",
		"for xs in [1, 2]:\n    print(xs)\n\ndef show():\n    print(xs)\n\nshow()\n", "bound at module level"},
	{"a slot of the module's list is a call's answer",
		"def one():\n    return 1\n\nxs = [one()]\n\ndef show():\n    print(xs)\n\nshow()\n", "bound at module level"},
	{"a slot of the module's list is a name",
		"one = 1\nxs = [one]\n\ndef show():\n    print(xs)\n\nshow()\n", "bound at module level"},
	// ---- the identity question, which a copy cannot answer whichever way the answer comes out.
	{"the module asks whether two names are one object",
		"xs = [1, 2, 3]\nys = xs\n\ndef show():\n    print(xs)\n\nprint(ys is xs)\nshow()\n", "bound at module level"},
	{"the module's list is used as a dict key",
		"xs = [1, 2, 3]\nd = {}\n\ndef show():\n    print(xs)\n\nd[xs] = 1\nshow()\n", "bound at module level"},
	// ---- a slot the body hands back whose kind the road out of a `return` cannot carry: the gate asks the
	// literal what is at that position, and anything that is not an integer keeps the refusal rather than
	// trade it for the payload word the same program prints today (roadmap Gap R.400).
	{"a body hands back a TEXT slot of the module's list",
		"xs = [\"b\", \"a\"]\n\ndef first():\n    return xs[1]\n\nprint(first())\n", "index of a non-literal variable"},
	{"a body hands back the singleton's slot",
		"xs = [None, 1]\n\ndef first():\n    return xs[0]\n\nprint(first())\n", "index of a non-literal variable"},
	{"a body hands back a slot that is itself a list",
		"table = [[1], [2]]\n\ndef row():\n    return table[0]\n\nprint(row())\n", "index of a non-literal variable"},
	{"a body hands back a slot read at a computed position",
		"i = 1\nxs = [1, 2]\n\ndef first():\n    return xs[i]\n\nprint(first())\n", "index must be a constant"},
	{"a body hands back a dict entry chosen by a computed key",
		"k = \"a\"\nd = {\"a\": 1}\n\ndef one():\n    return d[k]\n\nprint(one())\n", "index must be a constant"},
	{"a body hands back a slot past the end of the module's list",
		"xs = [1, 2]\n\ndef first():\n    return xs[9]\n\nprint(first())\n", "index of a non-literal variable"},
	// ---- the shapes the roads still decline, kept visible so a half-closed row does not read as closed.
	{"a comprehension walks the module's list",
		"xs = [1, 2, 3]\n\ndef doubled():\n    return [x * 2 for x in xs]\n\nprint(doubled())\n", "compile-time constant"},
	{"a nested def reads the module's list", // the closure road takes no prologue (Gap R.401)
		"xs = [1, 2, 3]\n\ndef outer():\n    def inner():\n        return len(xs)\n    print(inner())\n\nouter()\n", "bound at module level"},
	{"a lambda applied at module level reads the module's list", // the same road, the other spelling (Gap R.401)
		"xs = [1, 2, 3]\n\nprint((lambda: len(xs))())\n", "bound at module level"},
	{"a lambda bound to a name prints the module's list", // Gap R.401
		"xs = [1, 2, 3]\n\nf = lambda: xs\n\nprint(f())\n", "bound at module level"},
	{"a body slices the module's list",
		"xs = [1, 2, 3]\n\ndef show():\n    print(xs[0:2])\n\nshow()\n", "cannot slice xs"},
	{"a body folds the module's list with sum",
		"xs = [3, 1, 2]\n\ndef total():\n    return sum(xs)\n\nprint(total())\n", "sum requires an inline"},
	{"a body picks the largest of the module's list",
		"xs = [3, 1, 2]\n\ndef biggest():\n    return max(xs)\n\nprint(biggest())\n", "max requires an inline"},
	{"a body hands the module's list to join",
		"xs = [\"a\", \"b\"]\n\ndef show():\n    print(\", \".join(xs))\n\nshow()\n", "join() argument must be a constant list"},
	{"a body sorts the module's list into a return",
		"xs = [3, 1, 2]\n\ndef show():\n    return sorted(xs)\n\nprint(show())\n", "sorted(xs) is refused"},
	{"a body unpacks the module's list into two names",
		"xs = [1, 2]\n\ndef f():\n    a, b = xs\n    return a + b\n\nprint(f())\n", "unsupported tuple assignment"},
}

// moduleContainerWrongBytes is the class this row found by widening the gate and refuses to hide: a body
// that DOES compile, prints at the exit code of success, and prints the wrong word — the payload of a value
// whose kind nothing carried across the `return`. Both rows are identical on the commit before this one (a
// `git worktree` build of `HEAD` was run beside this one for every claim in this file), which is the
// evidence that they are the answer-direction's road and not this door's: the same program with the list
// written INSIDE the body prints the same wrong bytes. The gate refuses these slots precisely so that this
// table stays two rows rather than becoming the answer table.
var moduleContainerWrongBytes = []struct {
	name, src, today, want, row string
}{
	{
		name:  "a body hands back a FLOAT slot of the module's list",
		src:   "xs = [1.5, 2.5]\n\ndef one():\n    return xs[1]\n\nprint(one())\n",
		today: "2\n", want: "2.5\n", row: "Gap R.400",
	},
	{
		name:  "a body hands back a VERDICT slot of the module's list",
		src:   "xs = [True, False]\n\ndef one():\n    return xs[1]\n\nprint(one())\n",
		today: "0\n", want: "False\n", row: "Gap R.400",
	},
}

// TestWhatTheModuleContainerDoorsAnswerWrong pins the wrong answer rather than the celebration: the
// compiled run prints the bytes pinned here, the reference prints something else, and the day the answer
// direction carries the kind the row's own test fails and says DELETE.
func TestWhatTheModuleContainerDoorsAnswerWrong(t *testing.T) {
	t.Parallel()
	for _, tc := range moduleContainerWrongBytes {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s: the compile refused (%v) — the row is now a refusal, re-measure it against %s", tc.name, err, tc.row)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			out := compiledRunIsTheBackend(t, tc.src)
			if out == tc.want {
				t.Fatalf("%s: the compiled leg now answers %q, the reference's own bytes — DELETE this row, %s has closed",
					tc.name, out, tc.row)
			}
			if out != tc.today {
				t.Fatalf("%s: the compiled leg printed %q, pinned %q (%s)", tc.name, out, tc.today, tc.row)
			}
		})
	}
}

// TestWhatTheModuleContainerGateWillNotBuy walks that table. Every row must be refused — at exit 1, with
// the sentence that names the missing half — and none of them may reach exit 2, which the exit-code
// contract (ADR 0166) reserves for a module `llc` had to reject.
func TestWhatTheModuleContainerGateWillNotBuy(t *testing.T) {
	t.Parallel()
	for _, tc := range moduleContainerRefusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err == nil {
				out := runIR(t, res.IR)
				t.Fatalf("%s: the gate accepted the program and it printed %q — the reference's answer, "+
					"but a copy printed at exit 0 where the object may have been written is exactly the "+
					"answer this table exists to keep out\nsrc: %s", tc.name, out, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: refused with %q, want the sentence that says %q\nsrc: %s", tc.name, err.Error(), tc.want, tc.src)
			}
			if strings.Contains(err.Error(), "internal invariant") || strings.Contains(err.Error(), "llc") {
				t.Errorf("%s: the refusal blames the compiler, not the program: %q", tc.name, err.Error())
			}
		})
	}
}

// moduleContainerDebt is what the doors OWN and refuse to build at all, pinned to the refusal (or the exit-2
// module) so the day it is fixed this test fails and says so. The row reproduces identically on the commit
// before this one — measured against a `git worktree` build of it — which is why it is filed rather than
// absorbed.
var moduleContainerDebt = []struct {
	name, src, want, today, row string
}{
	{
		// The sequence door reads `xs + [3]` out of the fold notebook instead of the frame, so the
		// module's name is read by a `define` that never allocated its slot and the frame-slot audit
		// stops the build. Identical on the commit before this one — a `git worktree` build of HEAD —
		// which is why it is filed rather than absorbed. Filed as roadmap Gap R.399.
		name:  "a body that concatenates onto the module's list dies in the frame-slot audit",
		src:   "xs = [1, 2]\n\ndef give():\n    return xs + [3]\n\nprint(give())\n",
		want:  "[1, 2, 3]\n",
		today: "internal invariant violated (frame-slot audit)",
		row:   "Gap R.399",
	},
}

// TestWhatTheModuleContainerDoorsStillOwes asserts the compiled answer, not a celebration: the row leaves
// exit 2 with the frame-slot audit's sentence, which is neither the reference's answer nor a refusal the
// program can act on.
func TestWhatTheModuleContainerDoorsStillOwes(t *testing.T) {
	t.Parallel()
	for _, tc := range moduleContainerDebt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				if out := compiledRunIsTheBackend(t, tc.src); out == tc.want {
					t.Fatalf("%s: the compiled leg now answers %q, the reference's own bytes — DELETE this row, %s has closed", tc.name, out, tc.row)
				} else {
					t.Fatalf("%s: the compiled leg now compiles and prints %q, which is neither the pinned answer nor %s", tc.name, out, tc.row)
				}
			}
			if !strings.Contains(err.Error(), tc.today) {
				t.Errorf("%s: refused with %q, pinned %q (%s)", tc.name, err.Error(), tc.today, tc.row)
			}
		})
	}
}

// moduleContainerGateFor asks the gate as a function — the SAME `moduleTablesFor` the generator calls, so
// a table asserted here is the table that decides a program, not a second opinion of it (ADR 0232).
func moduleContainerGateFor(t *testing.T, src string) map[string]containerAnswer {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	consts, all := moduleEnvFor(prog)
	described, _ := containerAnswerScan(prog)
	_, _, containers := moduleTablesFor(prog, consts, all, described)
	return containers
}

// TestTheModuleContainerGateDecidesEachNameItsOwnWay asks the gate as a function, the way roadmap Gap
// Q.1's fourteen-claims table asks its door: several names in one program, and the table is checked name
// by name. A program-level "did it work" test cannot tell a gate that declines one name from a gate that
// declines all of them, which is how a fix that deletes the feature would still pass.
func TestTheModuleContainerGateDecidesEachNameItsOwnWay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      map[string]bool
	}{
		{
			name: "one clean name earns it and one mutated name does not",
			src:  "xs = [1, 2, 3]\nys = [4]\n\ndef show():\n    print(xs)\n\nys.append(9)\nshow()\n",
			want: map[string]bool{"xs": true, "ys": false},
		},
		{
			// The restraint row: `xs`'s write says nothing about `ys`, which the same body reads in the
			// same breath — and a gate that cleared the whole table when anything moved (or claimed every
			// name in the program because one name moved) is the same broom roadmap Gap Q.1 refused.
			name: "a write through one name leaves the other name's answer alone",
			src:  "xs = [1, 2, 3]\nys = [4]\n\ndef show():\n    print(xs)\n    print(ys)\n\nxs.append(9)\nshow()\n",
			want: map[string]bool{"xs": false, "ys": true},
		},
		{
			name: "a name bound twice by the module is two objects",
			src:  "xs = [1]\nxs = [2]\n\ndef show():\n    print(xs)\n\nshow()\n",
			want: map[string]bool{"xs": false},
		},
		{
			name: "a name bound by a for loop is not the literal road",
			src:  "for xs in [1, 2]:\n    print(xs)\n\ndef show():\n    print(xs)\n\nshow()\n",
			want: map[string]bool{"xs": false},
		},
		{
			name: "a name the module augments is not one binding of one literal",
			src:  "xs = [1]\nxs += [2]\n\ndef show():\n    print(xs)\n\nshow()\n",
			want: map[string]bool{"xs": false},
		},
		{
			name: "a nested literal of literals is still a literal",
			src:  "xs = [[1, 2], {\"a\": (3,)}]\n\ndef show():\n    print(xs)\n\nshow()\n",
			want: map[string]bool{"xs": true},
		},
		{
			name: "a set literal earns it",
			src:  "s = {1, 2}\n\ndef show():\n    print(s)\n\nshow()\n",
			want: map[string]bool{"s": true},
		},
		{
			name: "a tuple literal earns it",
			src:  "t = (1, 2)\n\ndef show():\n    print(t)\n\nshow()\n",
			want: map[string]bool{"t": true},
		},
		{
			name: "an empty literal earns it",
			src:  "xs = []\n\ndef show():\n    print(xs)\n\nshow()\n",
			want: map[string]bool{"xs": true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			earned := moduleContainerGateFor(t, tc.src)
			for nm, want := range tc.want {
				_, ok := earned[nm]
				if ok != want {
					t.Errorf("%s: the gate %s %q, want earned=%v\nsrc: %s\ntable: %v",
						tc.name, map[bool]string{true: "claims", false: "declines"}[ok], nm, want, tc.src, earned)
				}
			}
		})
	}
}

// TestABodyThatTakesTheNameForItselfIsNotHandedTheModuleS is the emitter-side half of the same restraint:
// the frame that binds the name anywhere in itself (ADR 0220's rule) is NOT given a copy, so a program
// cannot end up with two bindings of one slot — and the module's literal is not what that body prints.
func TestABodyThatTakesTheNameForItselfIsNotHandedTheModuleS(t *testing.T) {
	t.Parallel()
	res, err := Compile("xs = [1, 2, 3]\n\ndef show():\n    xs = [7, 8]\n    print(xs)\n\nshow()\nprint(xs)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if out := runIR(t, res.IR); out != "[7, 8]\n[1, 2, 3]\n" {
		t.Fatalf("the body read the module's copy: %q", out)
	}
	body := defineBodyOf(t, res.IR, "gy_show")
	if n := strings.Count(body, "%_xs = alloca i32"); n != 1 {
		t.Errorf("the body allocated the name %d times, want one slot for one name\n%s", n, body)
	}
}
