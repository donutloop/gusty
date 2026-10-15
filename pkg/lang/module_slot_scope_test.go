package lang

// pkg/lang/module_slot_scope_test.go — a module name has ONE slot, whichever statement binds it, and
// a `def`'s parameter is not a module binding (roadmap Gap R.49, ADR 0351).
//
// The row was written as an exit-2 bug and the suite is full of programs that exercise it —
//
//	def f(x):
//	    return x * 2
//
//	x = 8
//	x /= 2
//	print(x)     # CPython: 4.0     here: llc rejected the module, exit 2
//
// — and it had quietly become a wrong answer. Two things were both wrong, and only the first was
// ever named:
//
//   - **The question "does this module name need real storage?" was asked of the whole program
//     instead of each scope.** A `def` body that mentions `y` marked `y` as a name needing a
//     `@gy_mod_y` global — even when the mention was `return y` inside `def g(y)`, where `y` is the
//     PARAMETER and the module was never meant. The reference has one rule for this and this
//     toolchain now has the same one: a body's parameters and its own bindings shadow the module
//     inside that body.
//   - **Two roads bound a module name without asking where the name lives.** The plain assignment
//     consulted `moduleSlots` and wrote the global; `y += 3` and an unrolled `for` did not, and wrote
//     a main-frame `%_y` instead. One of those stores went to a slot no reader consults — `y = 2` /
//     `for y in [10, 20]: print(y)` printed `2`, `2` at the exit code of success — and the other went
//     to a slot that had never been allocated, which is how an ordinary program reached `llc` and
//     came back with exit 2, the code the exit-code contract reserves for a compiler bug (ADR 0166).
//
// Every `want` below is what `python3` prints for that program; the reference leg through the CLI is
// integration/module_slot_scope_test.go, and the record leg is asserted here by name, so a source
// whose answer the record does not hold fails this file rather than skipping.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// moduleSlotScopeAnswered is the family the reference answers and this toolchain must answer
// identically. The shapes are deliberately crossed — a name that is a parameter AND a module
// variable, a name that is a loop target AND a module variable, a name that is all three — because
// each of the two defects above hid one of the crossings.
var moduleSlotScopeAnswered = []struct {
	src  string
	want string
}{
	// ---- the row's own program, and its integer twin (the float path had taken the alloca road
	// and so had been answering all along; `+=` on an int was the store that never existed) ----
	{"def f(x):\n    return x * 2\n\nx = 8\nx /= 2\nprint(x)", "4.0"},
	{"def f(x):\n    return x * 2\n\nx = 8\nx += 2\nprint(x)", "10"},

	// ---- a parameter's name, bound by the module's own statements ----
	{"def g(y):\n    return y\n\ny = 2\nfor y in [10, 20]:\n    print(y)\nprint(y)", "10\n20\n20"},
	{"def g(y):\n    return y\n\ny = 0\nfor y in [1, 2, 3]:\n    y += 1\n    print(y)\nprint(y)", "2\n3\n4\n4"},
	{"def g(y):\n    return y\n\ny = 4\ny += 1\nprint(y)", "5"},
	{"def g(y):\n    return y\n\ntotal = 0\nfor y in [1, 2]:\n    total += y\nprint(total, y)", "3 2"},
	{"def g(y):\n    return y\n\nfor y in [1, 2]:\n    print(y)\nprint(y)", "1\n2\n2"},
	{"def g(y):\n    return y\n\nys = [1, 2]\nfor y in ys:\n    print(y)", "1\n2"},
	{"def g(y):\n    return y\n\ny = 2\nfor e in [10, 20]:\n    y = y + e\nprint(y)", "32"},

	// ---- the name a body reads without binding it: this is the ONE case that earns a global ----
	{"def g():\n    return y\n\ny = 0\nfor y in [1, 2]:\n    print(y)\nprint(y)", "1\n2\n2"},
	{"def g():\n    return i\n\ni = 0\nfor i in [7, 8]:\n    pass\nprint(i)", "8"},
	{"def g():\n    return y\n\ny = 1\ny += 3\nprint(y)", "4"},
	{"def g():\n    return total\n\ntotal = 1\nwhile total < 4:\n    total += 1\nprint(total)", "4"},
	// a parameter in one function, a module read in another: the name needs its global, and the
	// parameter still sees its own argument.
	{"def g(y):\n    return y\n\ndef h():\n    return y\n\ny = 4\nprint(g(9), h(), y)", "9 4 4"},

	// ---- a module function that takes the name as a parameter and is called with the module's value ----
	{"total = 0\n\ndef add(total, v):\n    return total + v\n\ntotal = add(total, 3)\nprint(total)\ntotal += 4\nprint(total)", "3\n7"},
	{"def g(n):\n    return n\n\ncount = 0\nfor k in [1, 2, 3]:\n    count += k\nprint(count, g(9))", "6 9"},

	// ---- the scoping the reference applies inside a body ----
	{"def g(y):\n    y = y + 1\n    return y\n\ny = 1\nprint(g(1), y)", "2 1"},
	{"def f(a=2):\n    return a\n\na = 9\nprint(a, f())", "9 2"},
	{"STEP = 2\n\ndef f(a=STEP):\n    return a\n\nprint(f(), f(5))", "2 5"},
	{"def g():\n    return s\n\ns = \"a\"\ns = \"b\"\nprint(s)", "b"},
}

// TestAModuleNameIsBoundInOneSlotWhereverItIsBound is the row itself on the compiled leg: every
// crossing of module binding, parameter and loop target prints what the reference prints, and a
// refusal is a failure — each of these is a program the reference runs.
func TestAModuleNameIsBoundInOneSlotWhereverItIsBound(t *testing.T) {
	t.Parallel()
	for _, tt := range moduleSlotScopeAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference answers %q", tt.src, got, tt.want+"\n")
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
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

// TestAModuleBindingIsOnTheRecord asks the same programs of the record leg. These are newly answered
// sources — the compiled backend used to print `2`, `2` or refuse the shape — so an absent record
// entry fails the case instead of skipping it: a row cannot be closed by losing its expectation.
func TestAModuleBindingIsOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tt := range moduleSlotScopeAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tt.src) {
				t.Fatalf("%q is not on the record: a newly answered program is recorded from the reference, not self-certified", tt.src)
			}
			rec := goldenLookup(t, tt.src)
			if !rec.HasStd {
				t.Fatalf("%q: the record holds no stdout for a program that prints", tt.src)
			}
			if rec.Stdout != tt.want+"\n" {
				t.Errorf("%q: the record printed %q, the reference prints %q", tt.src, rec.Stdout, tt.want+"\n")
			}
		})
	}
}

// moduleSlotScopeCases assert the gate itself, asked as a function: which module names get real
// storage. `want` is the set of names that MUST have a global and `without` the set that must not —
// the second half is the row, and a walk that forgets the scope fails it.
var moduleSlotScopeCases = []struct {
	name    string
	src     string
	want    []string
	without []string
}{
	{
		// The row's root: a name only ever used as a parameter is not a module binding, and the
		// module's own statements (the literal, then the loop that disqualifies it) must not be
		// given a second slot to disagree with the first.
		name:    "a parameter's name is not a module read",
		src:     "def g(y):\n    return y\n\ny = 2\nfor y in [10, 20]:\n    pass\nprint(y)\n",
		without: []string{"y"},
	},
	{
		// The other half of the same door: the name a body reads WITHOUT binding it is exactly the
		// name that needs the global. Narrowing the walk past parameters into real module reads
		// would break this row while passing the one above.
		name:    "a body's free read still earns the global",
		src:     "def g():\n    return y\n\ny = 2\nfor y in [10, 20]:\n    pass\nprint(y)\n",
		want:    []string{"y"},
		without: nil,
	},
	{
		name:    "a parameter in one body and a module read in another still earns it",
		src:     "def g(y):\n    return y\n\ndef h():\n    return y\n\ny = 2\nfor y in [10, 20]:\n    pass\nprint(y)\n",
		want:    []string{"y"},
		without: nil,
	},
	{
		// A body's own assignment shadows the module for the whole body, exactly as it does in the
		// reference — the reason `def g(z): z = z + 1` is not a module read of `z` either.
		name:    "a body's local assignment shadows the module",
		src:     "def g(z):\n    z = z + 1\n    return z\n\nz = 1\nfor z in [2, 3]:\n    pass\nprint(z)\n",
		without: []string{"z"},
	},
	{
		name:    "a loop variable inside a body is that body's own binding",
		src:     "def g(y):\n    for y in [1, 2]:\n        return y\n    return 0\n\ny = 5\nfor y in [6, 7]:\n    pass\nprint(y)\n",
		without: []string{"y"},
	},
	{
		// The negative control for the whole feature: nothing in this program is read by a body, so
		// no name carries storage at all — the gate is not paying for names nobody asks a body about.
		name:    "a module nobody's body reads carries no globals",
		src:     "def g(n):\n    return n\n\ncount = 0\nfor k in [1, 2, 3]:\n    count += k\nprint(count, g(9))\n",
		without: []string{"count", "k", "n"},
	},
}

// TestAParameterNameIsNotAModuleBinding asks the gate directly, as a function over the AST. Done this
// way on purpose (the pattern ADR 0273 and ADR 0271 established): a table of printed answers can be
// satisfied by any number of accidents, and this file's fix is a decision about which names deserve
// storage, so the decision itself is what fails when it is widened or narrowed.
func TestAParameterNameIsNotAModuleBinding(t *testing.T) {
	t.Parallel()
	for _, tc := range moduleSlotScopeCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			consts, all := moduleEnvFor(mustParse(t, tc.src))
			slots := moduleSlotNames(mustParse(t, tc.src), consts, all)
			for _, nm := range tc.want {
				if _, ok := slots[nm]; !ok {
					t.Errorf("%q reads %s from a body that does not bind it, so it needs a module global; the gate gave it none (slots: %s)", tc.src, nm, slotKeys(slots))
				}
			}
			for _, nm := range tc.without {
				if sym, ok := slots[nm]; ok {
					t.Errorf("%q gave %s the module global @%s that this row exists to withhold: a body that binds the name reads its own binding, not the module's (slots: %s)",
						tc.src, nm, sym, slotKeys(slots))
				}
			}
		})
	}
}

func slotKeys(slots map[string]string) string {
	if len(slots) == 0 {
		return "none"
	}
	out := make([]string, 0, len(slots))
	for k, v := range slots {
		out = append(out, fmt.Sprintf("%s=@%s", k, v))
	}
	return strings.Join(out, ", ")
}

var (
	moduleAllocaRe = regexp.MustCompile(`^\s*%_(\w+) = alloca `)
	moduleStoreRe  = regexp.MustCompile(`^\s*store \w+ \S+, (i32|double|i8)\* %_(\w+)\s*$`)
)

// TestTheModuleAllocasAreHoistedAndPerScope is the Definition of done's other clause: a store to a
// variable slot is legal only where that slot was allocated, in the same function, before the store.
// The exit-2 class this row is named for is exactly this shape missing (`store i32 %t4, i32* %_y`
// with no `%_y = alloca i32`), so the invariant is asserted over the whole main frame rather than
// over the two programs that happened to be measured.
func TestTheModuleAllocasAreHoistedAndPerScope(t *testing.T) {
	t.Parallel()
	for _, tt := range moduleSlotScopeAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tt.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			for fn, body := range functionBodies(res.IR) {
				seen := map[string]bool{}
				for _, line := range strings.Split(body, "\n") {
					if m := moduleAllocaRe.FindStringSubmatch(line); m != nil {
						seen[m[1]] = true
						continue
					}
					m := moduleStoreRe.FindStringSubmatch(line)
					if m == nil {
						continue
					}
					if !seen[m[2]] {
						t.Errorf("%s: %q stores to the variable slot %%%s, which this function never allocated — llc rejects this and the exit-code contract calls it a compiler bug (roadmap Gap R.49, ADR 0351)",
							fn, strings.TrimSpace(line), m[2])
					}
				}
			}
		})
	}
}

// functionBodies splits an emitted module into named definitions. Only the definitions matter: a
// store to an unallocated slot in a builtin's stub is not this row's problem.
func functionBodies(ir string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(ir, "\n")
	var name string
	var body strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "define ") {
			if name != "" {
				out[name] = body.String()
			}
			name = strings.TrimSpace(line)
			if i := strings.Index(name, "("); i > 0 {
				name = name[:i]
				name = name[strings.LastIndex(name, " ")+1:]
			}
			body.Reset()
			continue
		}
		if name != "" {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	if name != "" {
		out[name] = body.String()
	}
	return out
}

// TestEveryBindingOfAModuleNameWritesItsSlot pins the two roads that had gone their own way, one
// assertion each: the loop that binds a module name the module also reads writes the global once per
// element, and the augmented assignment writes it at all. Both numbers are the point — a road that
// emits only the alloca store fails, and so does one that forgets the loop's second element.
func TestEveryBindingOfAModuleNameWritesItsSlot(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		sym  string
		min  int
	}{
		{
			name: "the unrolled loop binds the global per element",
			src:  "def g():\n    return y\n\ny = 0\nfor y in [1, 2, 3]:\n    print(y)\n",
			sym:  "@gy_mod_y", min: 3,
		},
		{
			name: "the container walk binds the global per element",
			src:  "def g():\n    return y\n\nys = [1, 2]\ny = 0\nfor y in ys:\n    print(y)\n",
			sym:  "@gy_mod_y", min: 2,
		},
		{
			name: "augmented assignment binds the global",
			src:  "def g():\n    return y\n\ny = 1\ny += 3\nprint(y)\n",
			sym:  "@gy_mod_y", min: 1,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			got := strings.Count(res.IR, "i32* "+tc.sym)
			if got < tc.min {
				t.Fatalf("%q bound %s %d time(s), want at least %d: a road that writes only the alloca leaves every read of the name looking at a value nothing produced (roadmap Gap R.49, ADR 0351)",
					tc.src, tc.sym, got, tc.min)
			}
		})
	}
}

// TestAModuleNameCarriesNoGlobalItsBodyNeverReads is the cost half of the row: the gate that decides
// storage must stay as narrow as the question it answers, or every module program pays for a global
// and a store that no body will ever read.
func TestAModuleNameCarriesNoGlobalItsBodyNeverReads(t *testing.T) {
	t.Parallel()
	res, err := Compile("def g(y):\n    return y\n\ny = 2\nfor y in [10, 20]:\n    print(y)\nprint(y)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(res.IR, "@gy_mod_y") {
		t.Fatalf("the module carries @gy_mod_y for a name whose only body read is a parameter: %s", moduleSlotSymbols(res.IR))
	}
}

func moduleSlotSymbols(ir string) string {
	found := []string{}
	for _, line := range strings.Split(ir, "\n") {
		if i := strings.Index(line, "@gy_mod_"); i >= 0 && strings.HasPrefix(strings.TrimSpace(line), "@gy_mod_ = internal global") {
			found = append(found, strings.TrimSpace(strings.SplitN(strings.TrimSpace(line), " ", 2)[0]))
		}
	}
	if len(found) == 0 {
		return "none"
	}
	return strings.Join(found, ", ")
}

// TestNoModuleScopeProgramExitsTwo is the exit-code half: an ordinary program the reference runs must
// never leave the compiler as exit 2 (ADR 0166). The row's defect produced exactly that, from `llc`.
func TestNoModuleScopeProgramExitsTwo(t *testing.T) {
	t.Parallel()
	for _, tt := range moduleSlotScopeAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tt.src, 0)
			if err != nil {
				t.Fatalf("jit: %v", err)
			}
			if res.Code == 2 {
				t.Fatalf("%q left the toolchain as exit 2 (a compiler bug under ADR 0166):\n%s", tt.src, res.Stderr)
			}
		})
	}
}

// TestWhatTheModuleScopeRowLeavesStandingIsFiledNotFixed pins the neighbour this cycle measured and
// did NOT fix, with today's answer beside the reference's so the divergence cannot hide. Same shape
// as above — a module name rebound by a road other than the plain assignment — but a different root:
// the name's earlier literal is still being read after the rebind, which is the module's constant and
// interned-string records not being retracted by that road (roadmap Gap R.273).
func TestWhatTheModuleScopeRowLeavesStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc        string
		src         string
		want, today string
	}{
		{
			"a call's answer rebound over a literal text",
			"def f():\n    return \"z\"\n\nm = \"n\"\nm = f()\nprint(m)",
			"z", "n",
		},
		{
			"a loop target rebound over a literal text",
			"label = \"n\"\nfor label in [\"p\", \"q\"]:\n    print(label)\nprint(label)",
			"p\nq\nq", "n\nn\nn",
		},
		{
			// The same road with no first binding: the loop answers, and the print after it reads a
			// number for a name that holds text — the fold chose the wrong table, not the wrong slot.
			"a loop target read after the loop, with no earlier binding",
			"def g(label):\n    return label\n\nfor label in [\"p\", \"q\"]:\n    print(label)\nprint(label)",
			"p\nq\nq", "p\nq\n1",
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			out, trap := runForParity(t, tc.src)
			if trap != "" {
				t.Fatalf("%q raised %s", tc.src, trap)
			}
			if got := strings.TrimRight(out, "\n"); got != tc.today {
				t.Fatalf("Gap R.273's pinned answer moved: %q printed %q, this file recorded %q (the reference prints %q — if the row was closed, delete this row, do not edit it)",
					tc.src, got, tc.today, tc.want)
			}
		})
	}
}

// TestWhatTheModuleScopeCycleFoundNextDoorIsFiledNotFixed pins the two exit-2 shapes this cycle walked
// into while testing the fix, and did NOT fix. Both are the same neighbourhood — a definition is minted
// an IR name from its own NAME, as if a name were an identity — and both are ordinary programs the
// reference runs, so both leave the toolchain at exit 2, the code the exit-code contract reserves for a
// compiler bug (ADR 0166).
//
//   - **Two nested `def`s that share a name collide in the arena** (roadmap Gap R.274): `def a(): x = 1 /
//     def h(): return x / return h()` and `def b(): x = 2 / def h(): return x / return h()` is the
//     reference's `1 2`; here `llc` answers `redefinition of global '@h_slot'`. Name the two inners
//     differently and the same program compiles, which is the proof that what collided is the NAME and
//     not the two definitions.
//   - **A name defined twice at module level collides in the function table** (roadmap Gap R.275):
//     `def h(): return 1` above `def h(): return 2` is the reference's `2` — the later binding wins, the
//     first is dead — and here `llc` answers `invalid redefinition of function 'gy_h'`.
//
// Each case asserts today's answer, not the wanted one: the test exists so the divergence cannot quietly
// become normal, and each row is DELETED (not edited) when its fix lands.
func TestWhatTheModuleScopeCycleFoundNextDoorIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc     string
		src      string
		want     string // what the reference prints
		contains string // what llc says today
	}{
		{
			"two nested defs sharing a name",
			"def a():\n    x = 1\n    def h():\n        return x\n    return h()\n\ndef b():\n    x = 2\n    def h():\n        return x\n    return h()\n\nprint(a(), b())",
			"1 2", "redefinition of global",
		},
		{
			"a module name defined twice",
			"def h():\n    return 1\n\ndef h():\n    return 2\n\nprint(h())",
			"2", "invalid redefinition of function",
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			// `JIT` returns an error for what the CLI reports as exit 2 — llc refused the module we
			// emitted — so today's answer is in the error, not in a result.
			res, err := JIT(tc.src, 0)
			msg := ""
			if err != nil {
				msg = err.Error()
			} else if res.Code == 0 {
				t.Fatalf("the shape this file pins as broken now compiles and prints %q — the reference agrees, so the row was closed: delete this case, do not edit it (roadmap Gap R.274/R.275)", res.Output)
			} else {
				msg = res.Stderr
			}
			if !strings.Contains(msg, tc.contains) {
				t.Fatalf("%q failed as %q, not the pinned %q — llc's answer changed, so this filing needs re-measuring (roadmap Gap R.274/R.275)",
					tc.src, msg, tc.contains)
			}
		})
	}
}
