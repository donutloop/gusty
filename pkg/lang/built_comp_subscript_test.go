package lang

import (
	"errors"
	"strings"
	"testing"
)

// built_comp_subscript_test.go — a comprehension the RUN TIME BUILDS is an object, so a subscript
// standing on it asks the OBJECT for its slot and its kind (roadmap **Gap R.501**, **ADR 0442**'s
// two-comprehensions-one-door rule, **ADR 0443**'s brace for the kind, **ADR 0334**'s call-result door).
//
// ADR 0442 closed the count door of the same object: `len([g(x) for x in [1, 2, 3]])` asks
// `rt_heap_len`, because the register a builder filled — not the fold's `{i32, [N x i32]}` global —
// is where the answer lives. ADR 0443 gave the loop, the condition and `bool()` that same object. The
// subscript was the sibling door still reading the fold's table:
//
//	def g(v):
//	    return v * 2
//	print([g(x) for x in [1, 2, 3]][1])   # CPython: 4
//	                                     # this toolchain: IndexError at exit 3
//
// The fold had never filled the slot, and had never filled the count either; bounds-checking against
// a zero-length global therefore made EVERY position past the end. The answer is not a second reader:
// `containerHandleOf` already turns a comprehension into the handle its builder filled, and
// `containerSlotRead` is the one read a container variable gets — length, bounds, negative positions,
// KeyError, payload and tag. The fold keeps its own road; the built comprehension gets the object's.
//
// A set is the same question answered the other way: its brace says there are no positions at all, so
// the door raises the reference's `TypeError: 'set' object is not subscriptable` instead of borrowing
// the slot-extension road's membership answer or refusing the shape at exit 1.
//
// Every expectation below is CPython's, checked at the record leg (`testdata/interpreter-golden.json`)
// and through the CLI against a live `python3` in `integration/built_comp_subscript_test.go`. The
// residue that this row deliberately does NOT claim is the element whose kind a call hands back —
// `[g(x) for x in ["a", "b"]][0]` — which is **Gap R.502**'s tag, not the subscript door's position.

const builtCompSubscriptPrologue = "def g(v):\n    return v * 2\n\n"
const builtCompIdentityPrologue = "def g(v):\n    return v\n\n"

// builtCompSubscriptAnswers are the positions the built object answers. Each is the same slot a bound
// name would read, written one statement shorter; the last rows are the object's answers being USED,
// not merely printed, which is what keeps the read from being a print-specific coincidence.
var builtCompSubscriptAnswers = []struct {
	name string
	src  string
	want string
}{
	{"the row as filed: the middle slot of a built list", builtCompSubscriptPrologue + "print([g(x) for x in [1, 2, 3]][1])", "4"},
	{"the first slot", builtCompSubscriptPrologue + "print([g(x) for x in [1, 2, 3]][0])", "2"},
	{"a negative position is the object's position", builtCompSubscriptPrologue + "print([g(x) for x in [1, 2, 3]][-1])", "6"},
	{"the read is bound, then printed", builtCompSubscriptPrologue + "v = [g(x) for x in [1, 2, 3]][1]\nprint(v)", "4"},
	{"the read is used in arithmetic", builtCompSubscriptPrologue + "print(len([g(x) for x in [1, 2, 3]]) + [g(x) for x in [1, 2, 3]][1])", "7"},
	{"the read is compared", builtCompIdentityPrologue + "print([g(x) for x in [1, 2, 3]][1] == 4)", "False"},
	{"the read is compared against its own value", builtCompSubscriptPrologue + "print([g(x) for x in [1, 2, 3]][1] == 4)", "True"},
	{"a nested built comprehension reads its inner object", builtCompSubscriptPrologue + "print([[g(y) for y in [1, 2]] for x in [1, 2]][0][1])", "4"},
	{"the second level reads the last inner list", builtCompSubscriptPrologue + "print([[g(y) for y in [1, 2]] for x in [1, 2]][-1][0])", "2"},
	{"a built dict answers an int key", builtCompIdentityPrologue + "print({g(x): 1 for x in [1, 2]}[2])", "1"},
	{"a built dict answers a value that is not one", builtCompIdentityPrologue + "print({g(x): g(x) for x in [1, 2]}[2])", "2"},
	{"a built comprehension over a container variable", "xs = [1, 2, 3]\n" + builtCompSubscriptPrologue + "print([g(x) for x in xs][2])", "6"},
	{"a built comprehension over range()", builtCompSubscriptPrologue + "print([g(x) for x in range(3)][1])", "2"},
	{"a built comprehension whose iterable is a container of containers answers the ITEM, not its index", "print([[x for x in [[1]]][0], 2])", "[[1], 2]"},
	{"a built set comprehension subscript raises the set's own TypeError", builtCompIdentityPrologue + "try:\n    print({g(x) for x in [1, 2]}[0])\nexcept TypeError as e:\n    print(\"caught\", e)", "caught 'set' object is not subscriptable"},
}

// TestASubscriptOfABuiltComprehensionAsksTheObject is the answering half: the compiled leg, then the
// record leg, for every slot. `runGoldenStdout` is not optional here — **Gap R.501**'s row says the
// answer must live on the record, and the recorder wrote these entries only after the reference leg and
// the compiled leg agreed.
func TestASubscriptOfABuiltComprehensionAsksTheObject(t *testing.T) {
	t.Parallel()
	for _, tt := range builtCompSubscriptAnswers {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := strings.TrimRight(compiledOut(t, tt.src), "\n")
			if out != tt.want {
				t.Fatalf("%q printed %q, the reference prints %q", tt.src, out, tt.want)
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, out)
			}
		})
	}
}

// TestASubscriptPastTheEndOfABuiltComprehensionRaisesTheProgramsOwnAccident is the other half of the
// fix. Before this door the object had no count, so an in-range position raised; now the program's own
// out-of-range position raises, and the exception is the one CPython raises. Exit 2 remains forbidden:
// an instruction shape llc rejects is never a program result (**ADR 0166**).
func TestASubscriptPastTheEndOfABuiltComprehensionRaisesTheProgramsOwnAccident(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		src     string
		class   string
		message string
	}{
		{
			"past the end",
			builtCompSubscriptPrologue + "print([g(x) for x in [1, 2, 3]][7])",
			"IndexError", "list index out of range",
		},
		{
			"too far negative",
			builtCompSubscriptPrologue + "print([g(x) for x in [1, 2, 3]][-9])",
			"IndexError", "list index out of range",
		},
		{
			"a missing built dict key",
			builtCompIdentityPrologue + "print({g(x): 1 for x in [1, 2]}[3])",
			"KeyError", "3",
		},
		{
			"a float where a position belongs",
			builtCompIdentityPrologue + "print([g(x) for x in [1, 2]][1.5])",
			"TypeError", "list indices must be integers or slices, not float",
		},
		{
			"a built set has no position",
			builtCompIdentityPrologue + "print({g(x) for x in [1, 2]}[0])",
			"TypeError", "'set' object is not subscriptable",
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("%q raised %s: %s; the reference raises %s: %s", tt.src, class, message, tt.class, tt.message)
			}
			// The same trap on the record leg, so the filed row cannot be replaced by a compiled-only
			// assertion that would pass if the program's answer disappeared from the record.
			err := goldenRunError(t, tt.src)
			if err == nil {
				t.Fatalf("%q has no recorded trap, although the compiled run raises %s: %s", tt.src, class, message)
			}
			var te *TrapError
			if errors.As(err, &te) {
				if te.ExnType != tt.class || te.ExnMsg != tt.message {
					t.Fatalf("the record traps %s: %s; the reference raises %s: %s", te.ExnType, te.ExnMsg, tt.class, tt.message)
				}
			} else if !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("the record's failure %q does not carry %q (reference %s: %s)", err, tt.message, tt.class, tt.message)
			}
		})
	}
}

// TestATrapStandingOnABuiltComprehensionIsCatchable is the raise as a language value, not as a crash:
// the program's own `except` takes it, at exit 0.
func TestATrapStandingOnABuiltComprehensionIsCatchable(t *testing.T) {
	t.Parallel()
	for _, tc := range builtCompSubscriptAnswers {
		if !strings.Contains(tc.want, "caught") {
			continue
		}
		tt := tc
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("%q printed %q, want the handler to print %q", tt.src, got, tt.want)
			}
			if rec, err := runGoldenStdout(t, tt.src); err != nil {
				t.Fatalf("the record does not answer the caught raise: %v", err)
			} else if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q", rec, tt.want)
			}
		})
	}
}

// TestTheSubscriptOfABuiltComprehensionReadsTheObjectNotTheFold is the instruction-shape half. The
// old door loaded from a `{i32, [N x i32]}` shape whose slots were never filled, or bounds-checked a
// position against a count that was not in the object. The built door now reads the handle the
// builder returned, and asks it for the payload and the tag beside it.
func TestTheSubscriptOfABuiltComprehensionReadsTheObjectNotTheFold(t *testing.T) {
	t.Parallel()
	ir := mustCompileIR(t, builtCompSubscriptPrologue+"print([g(x) for x in [1, 2, 3]][1])")
	main := runtimeFnBody(ir, "define i32 @main()")
	if main == "" {
		t.Fatalf("the module has no main:\n%s", ir)
	}
	for _, want := range []string{
		"call i32 @rt_alloc(i32 1)",
		"call void @rt_append_tagged(i32 %h",
		"call i32 @rt_list_len(i32 %h",
		"call i32 @rt_get_elem(i32 %h",
		"call i32 @rt_tag_of(i32 %h",
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("the built comprehension subscript does not carry %q:\n%s", want, main)
		}
	}
	if strings.Contains(main, "load i32, i32* getelementptr({i32, [0 x i32]}") {
		t.Fatalf("the built comprehension subscript still reads a count out of a zero-length fold global:\n%s", main)
	}

	// The folded comprehension beside it keeps the fold. This commit chose between two comprehensions by
	// who built the object; it did not move the fold onto the runtime road.
	folded := runtimeFnBody(mustCompileIR(t, "print([x * 2 for x in [1, 2, 3]][1])"), "define i32 @main()")
	if !strings.Contains(folded, "load i32, i32* getelementptr({i32, [3 x i32]}, {i32, [3 x i32]}* @.lst") {
		t.Fatalf("a folded comprehension subscript no longer reads the global the fold filled:\n%s", folded)
	}
	if strings.Contains(folded, "call i32 @rt_get_elem(i32") {
		t.Fatalf("the fold was widened onto the built object's read:\n%s", folded)
	}
}

// TestTheSetDoorDoesNotMistakeMembershipForAPosition is the negative control for the one brace that
// has no slot. A slot standing on a set may ask the documented membership question, but a SET itself
// standing under a subscript has no position and raises the reference's TypeError.
func TestTheSetDoorDoesNotMistakeMembershipForAPosition(t *testing.T) {
	t.Parallel()
	src := builtCompIdentityPrologue + "print({g(x) for x in [1, 2]}[0])"
	class, message := jitTrap(t, src)
	if class != "TypeError" || message != "'set' object is not subscriptable" {
		t.Fatalf("%q raised %s: %s, want TypeError: 'set' object is not subscriptable", src, class, message)
	}
}
