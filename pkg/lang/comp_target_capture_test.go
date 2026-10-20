package lang

// pkg/lang/comp_target_capture_test.go — a comprehension's own target, read by a `lambda` body written
// inside the comprehension (roadmap L11.7 · **Gap R.348**).
//
// A comprehension has a scope of its own, and its target is a binding OF THAT SCOPE. The road that lowers
// the loop always allocated the target's slot and stored each item into it, and always read the name back
// for the positions it lowers itself — the element copy, the filter, the dict key. What it never did was
// RECORD the name anywhere, so every road that asks a frame what that frame owns came back saying nothing.
// The one road that asks is the capture road: the body of an applied `lambda` is emitted as a `define` of
// its own, a `define` reaches neither another `define`'s registers nor the module's storage for a name the
// module never bound, and the read of the target inside the body therefore died as `undefined name "x"`:
//
//	print([(lambda z: z + x)(1) for x in [10, 20]])   # the reference's [11, 21]
//
// The same target reached as an ARGUMENT answered all along — `[(lambda fn: fn(x) * 2)(lambda y: y + 1) for
// x in [1, 2, 3]]` is `[4, 6, 8]` on both legs — which is what located the hole in the body's read rather
// than in the loop, and is why the tables below keep both shapes: the row is only believed fixed if the
// shape that worked still works.
//
// The failure had a second, worse face. Where the MODULE happened to bind the same spelling, the generated
// body read the module's value instead of the target's and printed the wrong list at the exit code of
// success — `x = 5` then `print([(lambda z: z + x)(1) for x in [10, 20]])` answered `[6, 6]` where the
// reference answers `[11, 21]`. A wrong answer outranks every refusal in this project's defect ranking, so
// that shape is a row of its own below rather than a footnote, and it is the reason the ordering inside
// `lambdaFrameCaptures` (nearer binding first) is asserted and not assumed.
//
// What the row does NOT claim: that a comprehension's target is module state. It is not — the reference
// leaks a module-level comprehension's target nowhere, `print([x for x in [1, 2]])` followed by `print(x)`
// is a `NameError` there and a front-end refusal here, and that pair is pinned below. The `for` statement
// is the mirror image and keeps the mirror-image rule: a module-level `for` target IS module state, and a
// body reading one still meets the module-global refusal (roadmap Gap R.35). `frame_targets.go` is where
// the difference between the two is written down; this file is where it is enforced.

import (
	"strings"
	"testing"
)

// compTargetCapturePrograms is the answered family. `want` is what the reference prints, read off it; the
// same sources are on the record (tools/recmerge asked CPython first and refused to write an entry the
// compiled leg disagreed with — which, for the `x = 5` row, is the whole proof that the fix happened).
func compTargetCapturePrograms() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			// The row's own program: the target is the name the body is free in.
			"the row: an applied lambda's body reads the comprehension's target",
			"print([(lambda z: z + x)(1) for x in [10, 20]])\n",
			"[11, 21]\n",
		},
		{
			// The same read with the target bound one frame further out — the second program the row names.
			"the body's own parameter beside the target",
			"print([(lambda y: y + x)(2) for x in [10, 20]])\n",
			"[12, 22]\n",
		},
		{
			// The three-frame chain the row names, written inside a comprehension: the target travels through
			// the frames between the binding and the read (ADR 0380) exactly as a `for` target does.
			"the chain, inside a comprehension",
			"print([(lambda y: (lambda z: z + x + y)(1))(2) for x in [10, 20]])\n",
			"[13, 23]\n",
		},
		{
			// The element and the FILTER are both positions the body reads the target from; the road installs
			// the name for the whole element-and-filter span and releases it after.
			"the target read behind a filter",
			"print([(lambda z: z + x)(1) for x in [10, 20] if x > 15])\n",
			"[21]\n",
		},
		{
			// The iterable is a container VARIABLE: the loop runs at run time, so the name the body reads is
			// the slot the run-time loop stores each item into.
			"the target of a run-time loop over a container variable",
			"xs = [1, 2, 3]\nprint([(lambda z: z * x)(2) for x in xs])\n",
			"[2, 4, 6]\n",
		},
		{
			"the target of a loop over a range",
			"print([(lambda z: z + x)(1) for x in range(3)])\n",
			"[1, 2, 3]\n",
		},
		{
			// THE WRONG ANSWER. The module spells the same name and the comprehension binds it too: the
			// nearer binding is the one the read sees, so the target's value answers and not the module's.
			// Before the row this printed `[6, 6]` at exit 0.
			"a module name of the same spelling does not answer for the target",
			"x = 5\nprint([(lambda z: z + x)(1) for x in [10, 20]])\n",
			"[11, 21]\n",
		},
		{
			// The host frame is a `def` and the iterable is its parameter: the frame-owned target road,
			// walked inside a body rather than at the module's top level.
			"the target of a loop inside a def, over its parameter",
			"def f(xs):\n    return [(lambda z: z + x)(1) for x in xs]\n\nprint(f([10, 20]))\n",
			"[11, 21]\n",
		},
		{
			// The shape that answered BEFORE the row and must keep answering: the target reached as an
			// ARGUMENT, where the comprehension road has always recorded the name for itself.
			"the target handed to the call instead (the shape that already answered)",
			"print([(lambda fn: fn(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]])\n",
			"[4, 6, 8]\n",
		},
		{
			// A frame's own targets are the ones written INSIDE it: the module's comprehension binds `x`,
			// the body's comprehension binds a DIFFERENT `x`, and the body must read its own. What would
			// break is the host's table left standing over the body (ADR 0378's disease, one table over).
			"a body's own target shadows a host comprehension's target of the same spelling",
			"print([(lambda z: z)(1) for x in [1, 2]])\n\ndef f():\n    return [(lambda z: z + x)(1) for x in [10, 20]]\n\nprint(f())\n",
			"[1, 1]\n[11, 21]\n",
		},
		{
			// A def local beside the target: the two reach the body by different doors — the local as a
			// capture of the frame, the target as a capture of the frame's OWN binding — and both arrive.
			"a def local beside the target",
			"def f(base):\n    return [(lambda z: z + base + x)(1) for x in [10, 20]]\n\nprint(f(100))\n",
			"[111, 121]\n",
		},
	}
}

// ---------------------------------------------------------------------------
// The two witness legs.
// ---------------------------------------------------------------------------

// TestWhatAComprehensionTargetAnswersToABodyWrittenInsideIt is the reference leg: the compiled backend is
// asked, the table holds the reference's own bytes, and the record is not consulted.
func TestWhatAComprehensionTargetAnswersToABodyWrittenInsideIt(t *testing.T) {
	for _, tc := range compTargetCapturePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("compiled=%q reference=%q", got, tc.want)
			}
		})
	}
}

// TestWhatAComprehensionTargetAnswersOnTheRecord is the record leg over the same table. A source with no
// record is skipped rather than deleted — the record cannot be widened from here — and the count guard
// below is what stops the whole leg from quietly becoming nothing.
func TestWhatAComprehensionTargetAnswersOnTheRecord(t *testing.T) {
	asked := 0
	for _, tc := range compTargetCapturePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if !HasGoldenAnswer(tc.src) {
				t.Skipf("no record covers this program")
			}
			asked++
			want := goldenStdout(silentReporter{}, tc.src)
			if got := compiledRunIsTheBackend(t, tc.src); got != want {
				t.Fatalf("compiled=%q record=%q", got, want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestWhatAComprehensionTargetRefusesNothingItClaimsToAnswer is ADR 0273's rule over this table: a row that
// answers on the reference leg may not come back as a refusal — the class this project ranks worst.
func TestWhatAComprehensionTargetRefusesNothingItClaimsToAnswer(t *testing.T) {
	for _, tc := range compTargetCapturePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("refused a program the table claims to answer: %s", msg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// What the row does not open. Each of these is the SAME read, with a value the one word a capture travels
// in cannot carry: the target is now a name the frame owns, so it is the capture gate (ADR 0370) that
// speaks, and it speaks its own sentence rather than answering the print door with a heap handle, an
// @str_tab position or a truncated number.
// ---------------------------------------------------------------------------

func TestWhatAComprehensionTargetRefusesWhenTheWordCannotCarryIt(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// A container bound to the target: the capture would cross as the allocator's slot.
			"a target bound to a container",
			"xs = [[1], [2]]\nprint([(lambda z: x)(1) for x in xs])\n",
			`reads "x" from the frame that encloses it`,
		},
		{
			// A float bound to the target: the position arrives as a `double` or a (payload, tag) pair,
			// never as the one word.
			"a target bound to a float",
			"xs = [1.5, 2.5]\nprint([(lambda z: z + x)(1) for x in xs])\n",
			`reads "x" from the frame that encloses it`,
		},
		{
			// A text bound to the target: the word would arrive as its index into @str_tab.
			"a target bound to a text",
			"xs = [\"a\", \"b\"]\nprint([(lambda z: z + x)(1) for x in xs])\n",
			`reads "x" from the frame that encloses it`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("the gate stayed silent on a capture the value word cannot carry:\n%s", tc.src)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal=%q, want it to name %q", msg, tc.want)
			}
			// A refusal that does not say which road would carry the value is a dead end, not a
			// diagnostic (ADR 0249's rule a refusal names the row that owns the missing thing).
			if !strings.Contains(msg, "L11.1") {
				t.Fatalf("the refusal does not name the row that owns the missing word: %q", msg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The scoping rule, from the other side: the target is a binding of the comprehension and of nothing else.
// ---------------------------------------------------------------------------

func TestThatAComprehensionTargetLeaksNothingBeyondItself(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// A `for` target at the module's top level IS module state (the reference leaks it), so the body
			// reading one meets the module-global refusal and NOT the frame-owned-target road. The two
			// sentences are different on purpose, and this is the row that keeps them apart.
			"a for target at the module level stays module state (Gap R.35's sentence)",
			"for x in [10, 20]:\n    print((lambda z: z + x)(1))\n",
			"is bound at module level",
		},
		{
			// The iterable is evaluated outside the comprehension's own scope: a name read there is not the
			// target, and the capture road must not have invented one.
			"a lambda in the iterable reads no target",
			"def f(y):\n    return y + 1\n\nprint([(lambda z: z)(1) for x in [1, 2] if f(1) > 1])\n",
			"",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := compiledRefusalOf(tc.src)
			if tc.want == "" {
				if msg != "" {
					t.Fatalf("the row invented a refusal where the program only reads the frame: %s", msg)
				}
				return
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal=%q, want it to say %q", msg, tc.want)
			}
		})
	}
}

// TestThatAComprehensionTargetLeaksNothingIntoTheModule is the same rule asked of the SHARED FRONT END,
// which is the road that owns an unread name: the reference raises `NameError` for it, this back end
// refuses it, and the record agrees — and none of that is changed by the target now being a name the frame
// owns while the comprehension is being lowered.
func TestThatAComprehensionTargetLeaksNothingIntoTheModule(t *testing.T) {
	src := "print([(lambda z: z + x)(1) for x in [10, 20]])\nprint(x)\n"
	if diags := analyzeSrc(t, src); !diagHas(diags, `undefined name "x"`) {
		t.Fatalf("the target leaked into the module: %v", diags)
	}
	if _, err := RunSource(src); err == nil || !strings.Contains(err.Error(), `undefined name "x"`) {
		t.Fatalf("the run answered where the reference raises: %v", err)
	}
}

// TestWhatATargetReadBeyondTheComprehensionStillTraps is the record leg of the same scoping rule: the
// reference raises for a name the comprehension never leaked, and the front end refuses the same source.
func TestWhatATargetReadBeyondTheComprehensionStillTraps(t *testing.T) {
	src := "print([x for x in [1, 2]])\nprint(x)\n"
	if diags := analyzeSrc(t, src); !diagHas(diags, `undefined name "x"`) {
		t.Fatalf("the target leaked into the module: %v", diags)
	}
}

// ---------------------------------------------------------------------------
// The module, not just the answer. The row is a claim about the emitted `define` — that the target reaches
// the body as a parameter the application fills from the host frame's slot — so it is checked there.
// ---------------------------------------------------------------------------

func TestTheComprehensionTargetEntersTheBodyAsACaptureParameter(t *testing.T) {
	mod := compileSrc(t, "print([(lambda z: z + x)(1) for x in [10, 20]])\n")
	define := ""
	for _, line := range strings.Split(mod, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "define ") && strings.Contains(line, "lambda_") {
			define = strings.TrimSpace(line)
			break
		}
	}
	if define == "" {
		t.Fatalf("no `define` was emitted for the applied lambda:\n%s", headLines(mod, 40))
	}
	// `(lambda z: z + x)` spells ONE parameter and reaches the target through a second.
	if params := strings.Count(define, "i32 %p"); params != 2 {
		t.Fatalf("the generated body carries %d parameter words, want 2 (the spelled one plus the target forwarded): %s", params, define)
	}
	// The application fills it from the host frame's slot — the target's own word, not a constant.
	if !strings.Contains(mod, "call i32 @gy_lambda_") || !strings.Contains(mod, "%_x.ld") {
		t.Fatalf("no application forwarded the target's slot into the body:\n%s", headLines(mod, 60))
	}
	calls := 0
	for _, line := range strings.Split(mod, "\n") {
		if strings.Contains(line, "call i32 @gy_lambda_") && strings.Contains(line, "%_x.ld") {
			calls++
		}
	}
	if calls != 2 {
		t.Fatalf("%d applications carry the target's word, want one per item: %s", calls, mod)
	}
}

// TestTheFrameOwnsOnlyTheTargetsWrittenInsideIt is the witness that the table the capture road reads is a
// FRAME fact and not a module cache: the helper installs a new map per body and hands the host's back on the
// way out (ADR 0378's rule for the slot table, `frame_targets.go` for this one). A table left standing would
// let a body answer for a name its own frame never bound — and mint a parameter for a register no `define`
// owns, which is the module `llc` rejects at exit 2 (ADR 0166's code for a compiler bug).
func TestTheFrameOwnsOnlyTheTargetsWrittenInsideIt(t *testing.T) {
	g := &irGen{}
	g.noteFrameTarget("x")
	g.noteFrameTarget("x")
	g.allocd = map[string]bool{"x": true}
	if !g.frameOwnsTarget("x") {
		t.Fatal("a name the frame installed twice does not own it")
	}
	g.forgetFrameTarget("x")
	if !g.frameOwnsTarget("x") {
		t.Fatal("releasing one of two nested bindings unbound the other")
	}
	g.forgetFrameTarget("x")
	if g.frameOwnsTarget("x") {
		t.Fatal("the target outlived the comprehension that bound it")
	}
	// The slot has to exist, and the two facts are asked together: a comprehension that folded to
	// compile-time constants has a record and no register, and a parameter minted for it would be read
	// from nothing.
	g.allocd = map[string]bool{}
	g.noteFrameTarget("y")
	if g.frameOwnsTarget("y") {
		t.Fatal("a name with no slot was answered as owned")
	}
	// The undo hands the host frame its own table back: what the host bound is there again, and what only
	// the body bound is gone.
	g.noteFrameTarget("x")
	done := g.beginFrameTargets()
	if len(g.frameTargets) != 0 {
		done()
		t.Fatal("a body began its table with its host's bindings in it")
	}
	g.noteFrameTarget("inner")
	done()
	if g.frameTargets == nil || g.frameTargets["x"] != 1 {
		t.Fatalf("the host frame did not get its own table back: %v", g.frameTargets)
	}
	if g.frameOwnsTarget("inner") {
		t.Fatal("the body's table outlived the body")
	}
}
