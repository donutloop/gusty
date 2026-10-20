package integration

// integration/comp_target_capture_test.go — the reference leg for a comprehension's own target read by an
// applied `lambda`'s body (roadmap L11.7 · **Gap R.348**).
//
// `pkg/lang/comp_target_capture_test.go` decides the shape — which programs the emitter answers, which the
// capture gate refuses, and what the emitted `define` and its call carry. This file asks the two questions
// only a run can answer: does the compiled program print what `python3` prints, on BOTH compiled paths (the
// in-process compile-and-run and the linked binary), and does the module those paths were built from pass
// LLVM's own verifier?
//
// Every expectation is read off the reference at run time, so a row cannot pass by agreeing with the thing it
// is being checked against, and an answer copied out of a previous build is not evidence about a language.
// The record leg is pinned separately below: these sources are on the record because `tools/recmerge` asked
// CPython and refused to write an entry the compiled leg would not agree to — for the `x = 5` row that is the
// whole proof, since the build before the row answered `[6, 6]` there and the recorder turned it away.
//
// Three outcomes are distinguished, because the row spans all three:
//
//   - the reference ANSWERS → both compiled paths print the reference's bytes at exit 0;
//   - the door cannot answer (a target bound to a value the one capture word cannot carry) → an exit-1
//     refusal naming what is missing and the row that owns the missing word, never exit 2, which ADR 0166
//     reserves for a compiler bug;
//   - the shared front end REFUSES what the reference raises (the target read after the comprehension) →
//     the refusal is the answer, at exit 1, in the checker's own words.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// compTargetCapturePrograms is the answered family — each one a program `python3` prints, asked of the
// reference inside the case rather than remembered here.
var compTargetCapturePrograms = []struct{ name, src string }{
	{
		"the row: an applied lambda's body reads the comprehension's target",
		"print([(lambda z: z + x)(1) for x in [10, 20]])\n",
	},
	{
		"the body's own parameter beside the target",
		"print([(lambda y: y + x)(2) for x in [10, 20]])\n",
	},
	{
		// The chain ADR 0380 carries, written inside a comprehension instead of beside a loop: the target
		// travels one step per frame, exactly as the `for` target two rows above does.
		"the chain, inside a comprehension",
		"print([(lambda y: (lambda z: z + x + y)(1))(2) for x in [10, 20]])\n",
	},
	{
		"the target read behind a filter",
		"print([(lambda z: z + x)(1) for x in [10, 20] if x > 15])\n",
	},
	{
		"the target of a run-time loop over a container variable",
		"xs = [1, 2, 3]\nprint([(lambda z: z * x)(2) for x in xs])\n",
	},
	{
		"the target of a loop over a range",
		"print([(lambda z: z + x)(1) for x in range(3)])\n",
	},
	{
		// THE WRONG ANSWER the row closes: the module spells the same name, the comprehension binds it
		// nearer, and the nearer binding is the one the read sees. `[6, 6]` at exit 0 before the row.
		"a module name of the same spelling does not answer for the target",
		"x = 5\nprint([(lambda z: z + x)(1) for x in [10, 20]])\n",
	},
	{
		"the target of a loop inside a def, over its parameter",
		"def f(xs):\n    return [(lambda z: z + x)(1) for x in xs]\n\nprint(f([10, 20]))\n",
	},
	{
		"a def local beside the target",
		"def f(base):\n    return [(lambda z: z + base + x)(1) for x in [10, 20]]\n\nprint(f(100))\n",
	},
	{
		// The shape that answered BEFORE the row, kept in every table so the fix is seen to be a widening
		// and not a rewrite: the target reached as an ARGUMENT.
		"the target handed to the call instead",
		"print([(lambda fn: fn(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]])\n",
	},
	{
		"a body's own target shadows a host comprehension's target of the same spelling",
		"print([(lambda z: z)(1) for x in [1, 2]])\n\ndef f():\n    return [(lambda z: z + x)(1) for x in [10, 20]]\n\nprint(f())\n",
	},
}

// compTargetCaptureRefusals is the family the door cannot answer. Each row is the SAME read, with a value the
// one word a capture travels in cannot carry: the target is now a name the frame owns, so the gate that
// speaks is ADR 0370's, and it says so instead of handing the print door a heap handle, an @str_tab position
// or a truncated number. The reference ANSWERS every one of them, which is the point.
var compTargetCaptureRefusals = []struct{ name, src, want string }{
	{
		"a target bound to a container",
		"xs = [[1], [2]]\nprint([(lambda z: x)(1) for x in xs])\n",
		`reads "x" from the frame that encloses it`,
	},
	{
		"a target bound to a float",
		"xs = [1.5, 2.5]\nprint([(lambda z: z + x)(1) for x in xs])\n",
		`reads "x" from the frame that encloses it`,
	},
	{
		"a target bound to a text",
		"xs = [\"a\", \"b\"]\nprint([(lambda z: z + x)(1) for x in xs])\n",
		`reads "x" from the frame that encloses it`,
	},
	{
		// The mirror image, kept beside the three above to prove the road did not simply turn every loop
		// variable into module state: a `for` target at the module's top level IS leaked by the reference,
		// and the body reading one meets the module-global refusal (roadmap Gap R.35), in different words.
		"a for target at the module level stays module state",
		"for x in [10, 20]:\n    print((lambda z: z + x)(1))\n",
		"is bound at module level",
	},
}

// TestAComprehensionTargetPrintsWhatTheReferencePrints is the reference leg on both compiled paths.
func TestAComprehensionTargetPrintsWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range compTargetCapturePrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, path := range []struct {
				what string
				ask  func(*testing.T, string) string
			}{
				{"the compiled run", runCompiled},
				{"the linked binary", runAOT},
			} {
				if got := path.ask(t, tc.src); got != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", path.what, got, want, tc.src)
				}
			}
		})
	}
}

// TestWhatTheComprehensionTargetFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this
// row answers has an answer on record, and a record that disappeared fails here rather than passing by having
// nothing to compare against. The records were written from the reference, never from this backend.
func TestWhatTheComprehensionTargetFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range compTargetCapturePrograms {
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestWhatTheComprehensionTargetDoorRefusesExitsOneAndNamesTheRow pins the other half: the family the back
// end cannot answer is a front-end refusal at exit 1, worded with the missing thing and the roadmap row that
// owns it, and it is never exit 2 and never a value.
func TestWhatTheComprehensionTargetDoorRefusesExitsOneAndNamesTheRow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range compTargetCaptureRefusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The reference has to ANSWER this source, or the row describes the wrong program: the point of
			// the refusal family is that it is a door that is shut, not a program that is broken.
			if _, ok := cpythonPlainOut(t, dir, tc.src); !ok {
				t.Skip("no python3 available to cross-check")
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "comp_target_capture", tc.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("exit 2 on a program the reference answers — ADR 0166 reserves it for a compiler bug:\n%s", out)
				}
				if code == 0 {
					t.Fatalf("answered %q with no refusal; the capture word this row refuses to fabricate is roadmap L11.1", out)
				}
				if code != 1 {
					t.Fatalf("exited %d, want 1 (a front-end refusal): %s", code, out)
				}
				if !strings.Contains(out, tc.want) {
					t.Fatalf("the refusal does not say what is missing (%q): %s", tc.want, out)
				}
				if !strings.Contains(out, "roadmap") {
					t.Fatalf("the refusal names no roadmap row: %s", out)
				}
			}
		})
	}
}

// TestTheComprehensionTargetCaptureModuleVerifies is the leg neither witness can answer: the row's claim is
// about the EMITTED module — the target arrives in the body as a parameter the application fills from the
// host frame's slot — so the module has to be a module LLVM accepts, at every optimisation level the CLI
// will run it at.
func TestTheComprehensionTargetCaptureModuleVerifies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"the row", "print([(lambda z: z + x)(1) for x in [10, 20]])\n"},
		{"the chain inside the comprehension", "print([(lambda y: (lambda z: z + x + y)(1))(2) for x in [10, 20]])\n"},
		{"the run-time loop", "xs = [1, 2, 3]\nprint([(lambda z: z * x)(2) for x in xs])\n"},
		{"the module name the target shadows", "x = 5\nprint([(lambda z: z + x)(1) for x in [10, 20]])\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			built, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("the backend refused a program the row answers: %v", err)
			}
			mod := built.IR
			for _, level := range []int{0, 2} {
				v, err := lang.VerifyModuleIR(mod, level)
				if err != nil {
					t.Fatalf("opt %d: the module did not verify: %v", level, err)
				}
				if v == nil {
					t.Fatalf("opt %d: no verifier verdict at all", level)
				}
				if v.Skipped {
					t.Skipf("no %s on PATH: the verdict is not earned", v.Tool)
				}
				if !v.OK {
					t.Fatalf("opt %d: %s rejected the module: %s", level, v.Tool, strings.Join(v.Errors, "\n"))
				}
			}
		})
	}
}
