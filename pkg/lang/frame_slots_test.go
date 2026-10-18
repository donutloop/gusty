package lang

import (
	"os"
	"strings"
	"testing"
)

// frame_slots_test.go — a body's stack slots belong to the frame that emits the body
// (roadmap L11.6 · Gap R.342; ADR 0378).
//
// `g.allocd` answers 「does this name already have a stack slot in the frame being emitted」, and one map
// answered it for the whole module. The `def` road resets it (`beginScope`); the two roads that emit a body
// without passing through `funcDef` — the closure lift's nested `def` and the class road's method — did not,
// and neither did anything else. The measured shape is three lines:
//
//	def outer():
//	    def inner():
//	        w = 3
//	        return w
//	    w = 1.5
//	    return inner()
//
//	print(outer())            # CPython 3 · this toolchain: exit 2, `use of undefined value '%_w'`
//
// `inner` was emitted inside `outer`'s scope, so `inner`'s `w` asked `outer`'s table, was told 「allocated」,
// and emitted no `alloca`; `outer` then asked the same table, was told the same thing, and emitted
// `store i32 %t7, i32* %_w` against a slot its own `define` never had. The written-flags table (ADR 0228)
// had the same hole one level down — a nested `def` or a method with a local on a path that may not write it
// emitted `store i8 1, i8* %bnd_z` with no `%bnd_z` in its frame — which is what the same probe found for
// `def inner(c): if c: z = 1; return z` and for `class C: def m(self, c): …`.
//
// The cure is one rule with two halves: every road that emits a body enters the frame that body's slots live
// in (`enterFrameSlots`, taken by `enterBody`, which `beginScope` also takes — so there is no second copy of
// the table to forget), and the written-flag table is installed before the entry-block allocas are written, so
// the allocas are the body's own. `frame_audit.go` is the guard: the compiler now audits the module it
// produced for exactly this disease, in the class ADR 0166 reserves for a compiler bug, instead of forwarding
// an `llc` rejection of its own artifact.

// frameSlotPrograms is the answer table: every `want` is CPython's own bytes for that exact source, and every
// row is also run on the record leg where the record covers it (the guard at the bottom fails if none does).
var frameSlotPrograms = []struct {
	name, src, want string
}{
	// ---- the row the gap was named for.
	{
		name: "a nested def sharing a name with its enclosing body's float local",
		src: "def outer():\n    def inner():\n        w = 3\n        return w\n" +
			"    w = 1.5\n    return inner()\n\nprint(outer())\n",
		want: "3\n",
	},
	// ---- the container and text forms, which collide on the same tables through a different door:
	// a container binding also asks `allocd` (to release the old binding) and `freshSlots`.
	{
		name: "a nested def binding the container its enclosing body binds",
		src: "def outer():\n    def inner():\n        xs = [7, 8]\n        return xs[0] + len(xs)\n" +
			"    xs = [1, 2]\n    return inner() + len(xs)\n\nprint(outer())\n",
		want: "11\n",
	},
	{
		name: "a nested def binding the text its enclosing body binds",
		src: "def outer():\n    def inner():\n        s = \"hi\"\n        return len(s)\n" +
			"    s = \"hello\"\n    return inner() + len(s)\n\nprint(outer())\n",
		want: "7\n",
	},
	{
		name: "two nested defs and the enclosing body, all three binding the same name",
		src: "def outer():\n    def a():\n        v = 1\n        return v\n" +
			"    def b():\n        v = 2\n        return v\n    v = 3\n    return a() + b() + v\n\nprint(outer())\n",
		want: "6\n",
	},
	{
		name: "a nested def binding the list its enclosing method binds",
		src: "class C:\n    def m(self):\n        ys = [1, 2]\n        return len(ys)\n\n" +
			"def use():\n    ys = [3, 4, 5]\n    return C().m() + len(ys)\n\nprint(use())\n",
		want: "5\n",
	},
	// ---- the module's frame, which is a body too: a lifted closure emitted while main's written-flags
	// were installed used to allocate MAIN's locals inside the closure's define.
	{
		name: "a lifted def does not pay for the module's unwritten local",
		src: "if 1 > 0:\n    q = 5\nprint(q)\n\ndef holder():\n    def nested():\n        return 1\n" +
			"    return nested()\n\nprint(q + holder())\n",
		want: "5\n6\n",
	},
	// ---- the controls: the roads the rule must not disturb.
	{
		name: "a nested def that reads the number its frame owns still reads it",
		src:  "def outer():\n    n = 40\n    def add(k):\n        return k + n\n    return add(2)\n\nprint(outer())\n",
		want: "42\n",
	},
	{
		name: "a decorator's closure still runs through the trampoline",
		src: "def deco(f):\n    def wrapper(x):\n        return f(x) + 1\n    return wrapper\n\n" +
			"@deco\ndef add1(x):\n    return x + 1\n\nprint(add1(1))\n",
		want: "3\n",
	},
	{
		name: "two module-level bodies sharing a container name keep two lengths",
		src: "def a():\n    d = [1, 2]\n    return len(d)\n\ndef b():\n    d = [3, 4, 5]\n    return len(d)\n\n" +
			"print(a() + b())\n",
		want: "5\n",
	},
	{
		name: "a method's local is its own beside a module name of the same spelling",
		src:  "m = 2.5\n\nclass C:\n    def m1(self):\n        m = 3\n        return m\n\nprint(C().m1())\n",
		want: "3\n",
	},
}

// TestWhatTheFrameEntryLeavesStandingIsFiledNotFixed pins the three shapes this cycle's probe reached and
// that ADR 0378 does NOT answer, so that a reader of the table above does not conclude they are covered. Each
// row fails the day its shape starts answering the reference, and the message then says where the row goes.
//
//   - Gap R.345 — a LIFTED `def` whose body answers a double answers `0`. The frame entry gave the closure its
//     own slot for `w`, and the slot is a `double` (`%_w = alloca double`), but the lift road still opens its
//     `define internal i32 @inner_env` whatever the body computes and reads the slot back with `ret i32
//     %_w.ld1`: the value is gone by truncation, at exit 0. It is ADR 0377's return-word fold unwritten on
//     this one road — the half Gap R.341's row named as next-open — and not this commit's.
//   - Gap R.332 / Gap R.337 — a `def` nested inside a METHOD is not lifted at all: the call reaches the
//     ordinary road's generic 「not a function this backend can build a call to」 refusal.
//   - Gap R.346 — two nested `def`s of the SAME spelling in two different frames collide on the global the
//     lift mints for the closure (`@inner_slot = internal global` twice) and `llc` refuses the module:
//     exit 2 again, measured with this cycle's conformance program and identical on the pre-ADR 0378 build.
func TestWhatTheFrameEntryLeavesStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, today, gap string
		dies, refuses         bool
	}{
		{
			name:  "a lifted def whose body answers a double",
			src:   "def outer():\n    def inner():\n        w = 1.5\n        return w\n    w = 3\n    return inner()\n\nprint(outer())\n",
			today: "0\n",
			gap:   "Gap R.345",
		},
		{
			name: "two nested defs of the same spelling, two frames",
			src:  "def a():\n    def inner():\n        return 1\n    return inner()\n\ndef b():\n    def inner():\n        return 2\n    return inner()\n\nprint(a() + b())\n",
			gap:  "Gap R.346",
			dies: true,
		},
		{
			name:    "a def nested inside a method",
			src:     "class C:\n    def m(self):\n        def helper():\n            t = 2\n            return t\n        return helper()\n\nprint(C().m())\n",
			gap:     "Gap R.332",
			refuses: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RunSource(tc.src)
			if tc.dies {
				if err == nil {
					t.Fatalf("%s: the module that `llc` refused now compiles and prints %q — the shape is answered; "+
						"move this source into `frameSlotPrograms` above and close the roadmap row\nsrc: %s", tc.gap, got, tc.src)
				}
				if !strings.Contains(err.Error(), "redefinition of global") {
					t.Fatalf("%s: the module no longer dies the filed way (`redefinition of global`); it says:\n%v", tc.gap, err)
				}
				return
			}
			if tc.refuses {
				if err == nil {
					t.Fatalf("%s: the shape that used to be refused now compiles and prints %q — the row belongs in "+
						"`frameSlotPrograms` above\nsrc: %s", tc.gap, got, tc.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: the compiled leg refused (%v); the reference prints the answer this row files", tc.gap, err)
			}
			if got != tc.today {
				t.Fatalf("%s: the compiled leg now prints %q, not the filed %q — the shape is on its way to answered; "+
					"move this source into `frameSlotPrograms` above and close the roadmap row\nsrc: %s", tc.gap, got, tc.today, tc.src)
			}
		})
	}
}

// TestABodysSlotsAreItsOwnFrame is the answer leg: the compiled run prints CPython's bytes for every shape
// where a name is bound by two frames at once, and the record is asked for the same source so the table
// cannot quietly stop being cross-checked.
func TestABodysSlotsAreItsOwnFrame(t *testing.T) {
	asked := 0
	for _, tc := range frameSlotPrograms {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s: refused: %v\nsrc: %s", tc.name, err, tc.src)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
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

// TestAnUnwrittenLocalOfALiftedDefOrMethodTrapsAsTheReferenceTraps is the written-flag half of the same
// frame entry. ADR 0228 gives a possibly-unwritten local a bit, an alloca and a trap; the closure lift and
// the method emitter asked for the allocas while the ENCLOSING body's flag table was installed, so the bit
// they set belonged to no slot in their own frame — `store i8 1, i8* %bnd_z` reached `llc` as
// `use of undefined value` and the program left at exit 2. CPython raises UnboundLocalError; so must we,
// catchably, and never as a module the assembler refuses (ADR 0166).
func TestAnUnwrittenLocalOfALiftedDefOrMethodTrapsAsTheReferenceTraps(t *testing.T) {
	t.Parallel()
	const message = "cannot access local variable 'z' where it is not associated with a value"
	for _, tc := range []struct {
		name, src string
	}{
		{
			"a nested def's own unwritten local",
			"def outer():\n    def inner(c):\n        if c:\n            z = 1\n        return z\n" +
				"    return inner(False)\n\nprint(outer())\n",
		},
		{
			"a method's own unwritten local",
			"class C:\n    def m(self, c):\n        if c:\n            z = 1\n        return z\n\n" +
				"print(C().m(False))\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, msg := jitTrap(t, tc.src)
			if class != "UnboundLocalError" || msg != message {
				t.Fatalf("%s: raised %s: %s; the reference raises UnboundLocalError: %s\nsrc: %s",
					tc.name, class, msg, message, tc.src)
			}
		})
	}
}

// TestTheUnwrittenLocalTrapIsTheProgramsExceptionNotTheCompilers: the arm runs, the program carries on, and
// the exit code is the program's — the difference between a frame slot the compiler owns and a raise the
// program owns (ADR 0214).
func TestTheUnwrittenLocalTrapIsTheProgramsExceptionNotTheCompilers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			name: "the nested def's arm",
			src: "def outer():\n    def inner(c):\n        if c:\n            z = 1\n        return z\n" +
				"    try:\n        return inner(False)\n    except UnboundLocalError:\n        return -1\n\n" +
				"print(outer())\n",
			want: "-1\n",
		},
		{
			name: "the method's arm",
			src: "class C:\n    def m(self, c):\n        if c:\n            z = 1\n        return z\n\n" +
				"try:\n    print(C().m(False))\nexcept UnboundLocalError:\n    print(\"caught\")\nprint(\"after\")\n",
			want: "caught\nafter\n",
		},
		{
			name: "the written path stays a plain answer",
			src: "def outer():\n    def inner(c):\n        if c:\n            z = 1\n        return z\n" +
				"    return inner(True)\n\nprint(outer())\n",
			want: "1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				t.Fatalf("%s: refused (%s); the reference prints %q\nsrc: %s", tc.name, got, tc.want, tc.src)
			}
			if got != tc.want {
				t.Fatalf("%s: printed %q, want %q\nsrc: %s", tc.name, got, tc.want, tc.src)
			}
		})
	}
}

// TestEachDefineAllocatesTheSlotItStoresThrough is the IR leg, and it is the half a green answer cannot
// prove: the bug was a store with no alloca in the SAME function, which the two legs above only notice
// because `llc` happens to look. `gy_outer` must own an `%_w` of its own even though a closure that also
// binds `w` was emitted inside its scope, and the closure must own the flag its own body sets.
func TestEachDefineAllocatesTheSlotItStoresThrough(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, sym, mustAllocate, mustStoreThrough string
	}{
		{
			name: "the enclosing body allocates the name its closure also binds",
			src: "def outer():\n    def inner():\n        w = 3\n        return w\n" +
				"    w = 1.5\n    return inner()\n\nprint(outer())\n",
			sym: "gy_outer", mustAllocate: "%_w", mustStoreThrough: "store double",
		},
		{
			name: "the lifted closure allocates the flag its own body sets",
			src: "def outer():\n    def inner(c):\n        if c:\n            z = 1\n        return z\n" +
				"    return inner(False)\n\nprint(outer())\n",
			sym: "inner_env", mustAllocate: "%bnd_z", mustStoreThrough: "store i8",
		},
		{
			name: "the lifted closure allocates the slot behind that flag",
			src: "def outer():\n    def inner(c):\n        if c:\n            z = 1\n        return z\n" +
				"    return inner(False)\n\nprint(outer())\n",
			sym: "inner_env", mustAllocate: "%_z", mustStoreThrough: "store i32",
		},
		{
			name: "the method allocates the flag its own body sets",
			src: "class C:\n    def m(self, c):\n        if c:\n            z = 1\n        return z\n\n" +
				"print(C().m(False))\n",
			sym: "gy_C_m", mustAllocate: "%bnd_z", mustStoreThrough: "store i8",
		},
		{
			name: "the method allocates the container slot its own body binds",
			src: "class C:\n    def m(self):\n        ys = [1, 2]\n        return len(ys)\n\n" +
				"def use():\n    ys = [3, 4, 5]\n    return C().m() + len(ys)\n\nprint(use())\n",
			sym: "gy_C_m", mustAllocate: "%_ys", mustStoreThrough: "store i32",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s: refused: %v\nsrc: %s", tc.name, err, tc.src)
			}
			body := defineBodyOf(t, res.IR, tc.sym)
			if !strings.Contains(body, tc.mustAllocate+" = alloca ") {
				t.Errorf("%s: %s stores through %s without allocating it — no `%s = alloca` in this define:\n%s",
					tc.name, tc.sym, tc.mustAllocate, tc.mustAllocate, body)
			}
			if !storesThrough(body, tc.mustStoreThrough, tc.mustAllocate) {
				t.Errorf("%s: %s never %ss through %s, so this row is not witnessing what it claims:\n%s",
					tc.name, tc.sym, tc.mustStoreThrough, tc.mustAllocate, body)
			}
			if err := auditFrameSlots(res.IR); err != nil {
				t.Errorf("%s: the frame-slot audit refuses the module we emitted:\n%v", tc.name, err)
			}
		})
	}
}

// TestTheFrameAuditRefusesAModuleThatReadsASlotItsDefineNeverAllocated asks the guard directly, because the
// programs above can only show it agreeing — and a guard that is never seen to refuse is not a guard. The
// broken module is Gap R.342's emitted shape with the enclosing body's `alloca` taken away: exactly what the
// closure lift used to produce.
func TestTheFrameAuditRefusesAModuleThatReadsASlotItsDefineNeverAllocated(t *testing.T) {
	t.Parallel()
	broken := `define internal i32 @inner_env(i32 %env) {
  %gc.frame.base = alloca i32
  call void @rt_frame_open(i32* %gc.frame.base)
  store i32 3, i32* %_w
  ret i32 0
}
`
	err := auditFrameSlots(broken)
	if err == nil {
		t.Fatal("the audit accepted a define that stores through %_w, which it never allocated")
	}
	msg := err.Error()
	for _, want := range []string{"inner_env", "%_w", "internal invariant", "compiler"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the audit's sentence does not name %q:\n%v", want, err)
		}
	}
	if _, ok := err.(*IRInvariantError); !ok {
		t.Errorf("the audit's failure is %T, want *IRInvariantError (the class ADR 0166 gives exit 2)", err)
	}

	// …and the shapes it must NOT refuse: the same body with its own slot, a slot read through its load
	// register, the written-flag road, and a runtime helper whose allocas are hand-written.
	for _, good := range []string{
		"define internal i32 @inner_env(i32 %env) {\n  %_w = alloca i32\n  store i32 3, i32* %_w\n" +
			"  %_w.ld1 = load i32, i32* %_w\n  ret i32 %_w.ld1\n}\n",
		"define internal i32 @inner_env(i32 %env) {\n  %bnd_z = alloca i8\n  %_z = alloca i32\n" +
			"  store i8 1, i8* %bnd_z\n  store i32 1, i32* %_z\n  ret i32 0\n}\n",
		"define internal i32 @gy_outer() {\n  %_xs = alloca i32\n  %h = call i32 @rt_alloc(i32 1)\n" +
			"  store i32 %h, i32* %_xs\n  call void @rt_root_put(i32* %_xs)\n  ret i32 0\n}\n",
		"define internal void @rt_free(i32 %h) {\n  %p = inttoptr i32 %h to ptr\n  ret void\n}\n",
		// a slot the function was HANDED, not one it allocated: the helper's parameter is its own name, and
		// the runtime's hand-written blocks (`codegen_ir_*.go`) are allowed to keep their own pointers.
		"define internal void @rt_root_put_slot(i32* %_w) {\n  store i32 3, i32* %_w\n" +
			"  call void @rt_root_put(i32* %_w)\n  ret void\n}\n",
	} {
		if err := auditFrameSlots(good); err != nil {
			t.Errorf("the audit refused a legal define:\n%v\n%s", err, good)
		}
	}
}

// TestTheAuditIsAskedOfTheModuleBeforeItLeavesTheCompiler is the wiring. The guard lives in the pipeline, not
// in a test that re-implements it — and a rule that only exists where it is checked is a rule that can be
// deleted without anything noticing (the same reason TestTheBodyFloatTableIsInstalledByEveryRoadThatEmitsABody
// reads the helpers rather than the answers).
func TestTheAuditIsAskedOfTheModuleBeforeItLeavesTheCompiler(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("codegen.go")
	if err != nil {
		t.Fatalf("cannot read codegen.go: %v", err)
	}
	text := string(src)
	at := strings.Index(text, "auditFrameSlots(moduleText)")
	if at < 0 {
		t.Fatal("GenerateIRReport never asks auditFrameSlots of the module it produced — the guard is dead")
	}
	// Asked of the SHIPPED text: after the pass that may add instructions, before the hoist that moves them.
	frame := strings.Index(text[:at], "attachFrameStack(moduleText")
	if frame < 0 {
		t.Fatal("the audit is not asked after attachFrameStack, so it audits a module that is not the one shipped")
	}
	hoist := strings.Index(text[at:], "hoistAllocas(moduleText)")
	if hoist < 0 {
		t.Fatal("the audit is not asked before the module is returned")
	}
}

// TestTheFrameEntryIsAnEntryAndNotARename asks the rule of the Go value rather than of a program: a body that
// takes the frame entry starts owning no slots at all, what it allocates stays in its own table, and the
// frame that contains it gets its own bookkeeping back whole. The answer tables above can only notice this
// when a name happens to collide; this row cannot be satisfied by a program that answers right anyway.
func TestTheFrameEntryIsAnEntryAndNotARename(t *testing.T) {
	t.Parallel()
	g := &irGen{
		allocd:     map[string]bool{},
		gcRootSeen: map[string]bool{},
		freshSlots: map[string]bool{},
	}
	g.allocd["w"] = true
	g.gcRootSeen["w"] = true
	g.freshSlots["w"] = true

	undo := g.enterFrameSlots()
	if !g.frameSlotsAreTheFramesOwn() {
		t.Fatalf("enterFrameSlots cleared the frame instead of entering it: allocd=%v freshSlots=%v",
			g.allocd, g.freshSlots)
	}
	if g.gcRootSeen["w"] || g.allocd["w"] || g.freshSlots["w"] {
		t.Fatalf("the entered frame inherited the enclosing frame's slots: %v / %v / %v", g.allocd, g.gcRootSeen, g.freshSlots)
	}
	// …and the tables are DIFFERENT maps: a write in the body must not be visible to the frame around it.
	g.allocd["v"] = true
	g.gcRootSeen["v"] = true
	g.freshSlots["v"] = true
	undo()

	if !g.allocd["w"] || !g.gcRootSeen["w"] || !g.freshSlots["w"] {
		t.Fatalf("the enclosing frame lost its slot bookkeeping on the way out: %v / %v / %v",
			g.allocd, g.gcRootSeen, g.freshSlots)
	}
	if g.allocd["v"] || g.gcRootSeen["v"] || g.freshSlots["v"] {
		t.Fatalf("the body wrote through the ENCLOSING frame's table — the tables are aliased, not entered: %v",
			g.allocd)
	}
}

// storesThrough reports whether a define body contains a store instruction whose last operand is the slot.
// A slot is only ever written through its address, so this is the shape that says 「this `define` owns the
// slot AND uses it」 — the pair whose absence was Gap R.342.
func storesThrough(body, storeWord, slot string) bool {
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, storeWord+" ") && strings.HasSuffix(t, " "+slot) {
			return true
		}
	}
	return false
}

// TestEveryRoadThatEmitsABodyEntersTheFrameItEmits is the other half of the wiring: the two roads that emit a
// body without passing through `funcDef` must take the frame entry, and they must take it BEFORE the
// entry-block allocas — the ordering is the bug, and an ordering rule cannot be witnessed by an answer.
func TestEveryRoadThatEmitsABodyEntersTheFrameItEmits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		file, fn, marker string
	}{
		{"closure.go", "func (g *irGen) emitClosureDef(", "g.enterBody(fd.Body)"},
		{"codegen_class.go", "func (g *irGen) emitClassMethod(", "g.enterBoundFlags(fd, fd.Body)"},
	} {
		src, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("cannot read %s: %v", tc.file, err)
		}
		text := string(src)
		at := strings.Index(text, tc.fn)
		if at < 0 {
			t.Fatalf("%s has no %s", tc.file, tc.fn)
		}
		end := strings.Index(text[at:], "\n}")
		body := text[at : at+end]
		if !strings.Contains(body, tc.marker) {
			t.Errorf("%s: %s does not take the frame entry (%s), which is the rule frame_slots.go states",
				tc.file, tc.fn, tc.marker)
		}
		allocas := strings.Index(body, "g.emitBoundAllocas(")
		flags := strings.Index(body, "g.enterBoundFlags(")
		if allocas >= 0 && flags >= 0 && flags > allocas {
			t.Errorf("%s: %s asks emitBoundAllocas before enterBoundFlags, so the entry block allocates another "+
				"body's slots (roadmap Gap R.342, ADR 0378)", tc.file, tc.fn)
		}
	}
}
