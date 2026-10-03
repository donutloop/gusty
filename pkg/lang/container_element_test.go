package lang

import (
	"strings"
	"testing"
)

// Artifact-level coverage for roadmap Gap R.40 (ADR 0226): a container slot is an i32 word, so the
// emitter must ask what fits *before* writing it. Three shapes used to skip that question and reach
// `llc` as garbage — `[1 x i32] [@env_store = ...`, `%t1 = sitofp i32  to double`,
// `%t2 = sitofp i32 @.lst1 to double` — which the exit-code contract then reported as exit 2: the
// compiler blamed for an ordinary program (ADR 0166).

// forbiddenIR is the shape blacklist: instructions this backend must never put in a module, whatever
// the source says. They are all "an operand the compiler invented" — an empty one, a container in a
// numeric position, or a definition spliced into an initializer list.
var forbiddenIR = []struct{ why, needle string }{
	{"an instruction with an empty operand", "sitofp i32  to double"},
	// The float twin of the row above, and the shape roadmap Gap R.96 was: an `fdiv` with an operand
	// the compiler could not lift. `llc` rejects it (exit 2, ADR 0166's own class), and the version that
	// substitutes a literal instead verifies happily and prints `0.0` — so the blacklist, not the
	// verifier, is what has to catch it.
	{"an instruction with an empty operand", "fdiv double ,"},
	{"an instruction with an empty operand", "fadd double ,"},
	{"an instruction with an empty operand", "fmul double ,"},
	{"an instruction with an empty operand", "fsub double ,"},
	{"a container global in a numeric conversion", "sitofp i32 @.lst"},
	{"a container global in a numeric conversion", "sitofp i32 @.dict"},
	{"a container global in a numeric conversion", "sitofp i32 @.set"},
	{"a global definition spliced into an element list", "[1 x i32] [@"},
	{"a global definition spliced into an element list", "[2 x i32] [@"},
	{"a global reference in an i32 return slot", "ret i32 @."},
	{"a global reference in an i32 comparison", "icmp eq i32 @."},
	// A double the compiler folded to a non-finite value used to be written with Go's own spelling plus
	// this emitter's exponent suffix — `inf.0e+00`, `nan.0e+00` — which LLVM 20's parser has no token
	// for. `llc` stops, which is exit 2 on a program the reference prints (roadmap Gap R.134, ADR 0264);
	// the IEEE bit pattern is the spelling the parser takes.
	{"a non-finite double in a spelling the parser has no token for", "inf.0e+00"},
	{"a non-finite double in a spelling the parser has no token for", "nan.0e+00"},
}

func assertNoForbiddenIR(t *testing.T, src, ir string) {
	t.Helper()
	for _, f := range forbiddenIR {
		if strings.Contains(ir, f.needle) {
			t.Fatalf("%s: %q appears in the module compiled from\n%s\n\n%s", f.why, f.needle, src, ir)
		}
	}
}

// TestFloatElementsRefuseRatherThanBreakTheModule: the shapes in this list used to be refusals —
// a float had no representation in an i32 slot, so the honest answer was the diagnostic and the
// dishonest one was a truncated read. ADR 0233 gave the float a representation (a box, tagged
// TagFloat, compared by rt_payload_eq and rendered by the mixed printer), so the same programs are
// now expected to compile and print CPython's answer. What still has to refuse stays in
// TestMixedListElementUsesStillRefuse: a nested container, which the collector cannot mark yet.
func TestFloatElementsRefuseRatherThanBreakTheModule(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(1 if [1] == [1.0] else 0)\n", "1\n"},
		{"print(1 if [1.0] == [1] else 0)\n", "1\n"},
		{"print(1 if [1.5, 2] == [1.5, 2] else 0)\n", "1\n"},
		{"print([1.5, 2])\n", "[1.5, 2]\n"},
		{"xs = [1.5]\nprint(xs[0])\n", "1.5\n"},
		{"print(1 if {1.0} == {1.0} else 0)\n", "1\n"},
		{"d = {\"a\": 1.5}\ne = {\"a\": 1.6}\nprint(1 if d == e else 0)\n", "0\n"},
		{"d = {\"a\": 1.5}\ne = {\"a\": 1.5}\nprint(1 if d == e else 0)\n", "1\n"},
		{"print([0.0] == [-0.0])\n", "True\n"}, // fcmp oeq, the way Python compares floats
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q refused after ADR 0233 gave the element a representation: %v", tc.src, err)
		}
		if !strings.Contains(res.IR, "@rt_float_new(") && strings.Contains(tc.src, ".") {
			// Every one of these stores a float somewhere; the box allocation is the proof.
			if !strings.Contains(res.IR, "@rt_float") {
				t.Errorf("%q compiled without a float box:\n%s", tc.src, res.IR)
			}
		}
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("%q ran to %q, want CPython's %q", tc.src, out, tc.want)
		}
	}
}

// TestNoProgramInTheFloatFamilyShipsForbiddenIR is the sweep the exit-2 contract needs: for every
// shape in the family, either the compile refuses or the module it produces contains none of the
// invented-operand shapes and verifies. A blacklist test like this is what makes the fix
// unfalsifiable-proof rather than anecdote-proof.
func TestNoProgramInTheFloatFamilyShipsForbiddenIR(t *testing.T) {
	sources := []string{
		"print(1 if 2.0 == 2 else 0)\n",
		"print(1 if 1.0 == \"a\" else 0)\n",
		"print(1 if \"a\" == 1.0 else 0)\n",
		"print(1 if 1.0 == [1] else 0)\n",
		"print(1 if [1] == 1.0 else 0)\n",
		"print(1 if [1] == \"a\" else 0)\n",
		"print(1 if 1 == [1] else 0)\n",
		"print(len([1.5, 2.5]))\n",
		"print(1 if [1.0][0] == 1.0 else 0)\n",
		"print(1 if [1, \"a\"] == [1, \"a\"] else 0)\n",
		"print(1 if {1} == {1.0} else 0)\n",
	}
	for _, src := range sources {
		res, err := Compile(src)
		if err != nil {
			continue // an honest refusal is allowed; garbage is not
		}
		assertNoForbiddenIR(t, src, res.IR)
		if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
			t.Fatalf("the module compiled from %q does not verify: %v", src, verr)
		}
	}
}

// TestMismatchedKindEqualityIsAnsweredByKind: `1.0 == [1]` is not a numeric question. CPython answers
// False without converting the list, and so must this backend — via the operand-kind gate, not by
// coercing a container through a float conversion (which is what emitted the rejected module).
func TestMismatchedKindEqualityIsAnsweredByKind(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(1 if 1.0 == [1] else 0)\n", "0\n"},
		{"print(1 if [1] == 1.0 else 0)\n", "0\n"},
		{"print(1 if 1.0 != [1] else 0)\n", "1\n"},
		{"print(1 if 1.0 is [1] else 0)\n", "0\n"},
		{"print(1 if 1 == [1] else 0)\n", "0\n"},
		{"print(1 if 1.0 == 2 else 0)\n", "0\n"},
		{"print(1 if 2.0 == 2 else 0)\n", "1\n"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q must answer, not refuse: %v", tc.src, err)
		}
		assertNoForbiddenIR(t, tc.src, res.IR)
		out := runIR(t, res.IR)
		if out != tc.want {
			t.Fatalf("%q printed %q, want CPython's %q", tc.src, out, tc.want)
		}
	}
}

// TestPartialGlobalIsNeverShipped is the mechanism behind half the rejections: emitList wrote the
// global's opening text into the module and *then* discovered an element it could not hold, leaving
// an unterminated definition that everything downstream shipped. The emitter now validates first and
// writes once; a refusal must leave no half-line behind.
func TestPartialGlobalIsNeverShipped(t *testing.T) {
	// A shape that still refuses — a dict keyed by a container has no hashing rule — checked for
	// the failure mode rather than the message: the refusal must be a clean stop, not a
	// half-written global left in the module (roadmap Gap R.40, ADR 0166).
	res, err := Compile("print({[1.5, 2]: 3})\n")
	if err == nil {
		t.Fatalf("expected a refusal, got a %d-byte module", len(res.IR))
	}
	// A refusal must not leave the runtime's own globals looking like the broken half-line.
	for _, g := range []string{"@env_store", "@gc.roots"} {
		if strings.Contains(err.Error(), g) {
			t.Fatalf("the refusal leaked a half-emitted global (%s) into the report: %v", g, err)
		}
	}
}
