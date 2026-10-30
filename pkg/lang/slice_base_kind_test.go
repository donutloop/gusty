package lang

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// The slice road asks what its BASE is (roadmap Gap R.234, ADR 0466).
//
// `rt_slice` walks a heap list and copies its slots, so a slice is a question about a container that HAS
// positions. The road used to never ask whether the thing in front of the colon had any: four shapes reached
// the copy helper with the wrong kind of word in its argument slot —
//
//	print({1, 2}[0:1])   # the literal's i32 global, which `llc` rejected: exit 2, ADR 0166's class
//	print({1: 2}[0:1])   # the same, and before ADR 0335's gate the copy of a dict printed `[1]` at exit 0
//	s = {1, 2}
//	print(s[0:1])        # `[1]` at the exit code of success
//	d = {1, 2}   ->      # `[1]` at the exit code of success
//
// and the reference answers none of them with a list. It answers with the OBJECT's complaint, or with the
// DICT's own miss:
//
//	python3 -c 'print({1, 2}[0:1])'   # TypeError: 'set' object is not subscriptable
//	python3 -c 'print({1: 2}[0:1])'   # KeyError: slice(0, 1, None)
//
// The reference's ORDER is the interesting half, and it is the reason the gate sits at the top of the road
// rather than beside the bounds. `badSliceBoundGate` (Gap R.233, ADR 0336) asks what a bound IS, and its
// sentence is the LIST's complaint — `slice indices must be integers or None or have an __index__ method` —
// which a set never earns, because a set has nothing to count with. A dict never earns it either: it hashes
// the slice as a key, and hashing a key asks only whether the key is hashable. So:
//
//	print([1, 2]["a":"b"])    # TypeError: slice indices must be integers …   (the list's bounds door)
//	print({1, 2}["a":"b"])    # TypeError: 'set' object is not subscriptable  (the object, not the bounds)
//	print({1: 2}["a":"b"])    # KeyError: slice('a', 'b', None)               (a miss, naming the key)
//	print({1: 2}[[1]:2])      # TypeError: unhashable type: 'list'            (hashing, before the miss)
//	print({1: 2}[0:1:0])      # KeyError: slice(0, 1, 0)                      (no ZeroDivisionError — the
//	                          #  step is never used as a step, only hashed as part of the key)
//
// and the same program written with a list base keeps ADR 0336's sentence, because the base is a list.
//
// Nothing here invents a kind. `staticContainerKind` is the predicate the comparison, the membership test and
// the loop already decide with, and it answers only where a record names what the name holds; the gate raises
// only where the SOURCE wrote the object (`{1, 2}`, `{1: 2}`, or a `def` whose answer record names), and every
// other base keeps the road — including the refusal — it had yesterday. The repr the KeyError names is read
// from the same notebooks `keyNotFoundMessage` quotes for an entry that is not there, so the two doors that
// name a missing key cannot disagree (ADR 0361's repr rule, Gap R.189's sentence rule).

// sliceBaseRaiseRows are the shapes the gate answers with the object's own complaint. `class`/`message` are
// CPython's, read off the reference for each source rather than paraphrased (Gap N's standing rule).
var sliceBaseRaiseRows = []struct {
	name    string
	src     string
	class   string
	message string
}{
	// ---- a set has no position to slice from ----
	{
		"a slice of a set literal",
		"print({1, 2}[0:1])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		"a slice of a set kept in a name",
		"s = {1, 2}\nprint(s[0:1])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		"a set sliced by nothing at all",
		"print({1, 2}[:])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		"a set sliced by a step alone",
		"print({1, 2}[::2])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		"a set sliced by a negative step",
		"s = {1, 2}\nprint(s[::-1])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		// The ORDER row: a float bound earns the list's bounds sentence and not this one — but only for a
		// base that counts. `print([1, 2][1.5:2])` raises the slice-indices words (ADR 0336); this row must
		// not borrow them.
		"a set sliced by a float bound says the object, not the bounds",
		"print({1, 2}[1.5:2])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		// The same order asked of the hashing door: an unhashable bound is the DICT's complaint, and a set
		// never gets to hashing.
		"a set sliced by a list bound says the object, not unhashable",
		"print({1, 2}[[1]:2])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		"a set sliced by text bounds",
		"print({1, 2}[\"a\":\"b\"])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		"a set filled by an unpacking",
		"a, b = {1, 2}, 0\nprint(a[0:1])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		"a set handed back by a call",
		"def m():\n    return {1, 2}\n\nprint(m()[0:1])",
		"TypeError", "'set' object is not subscriptable",
	},
	{
		"a set handed back by a call and then bound",
		"def m():\n    return {1, 2}\n\ns = m()\nprint(s[0:1])",
		"TypeError", "'set' object is not subscriptable",
	},

	// ---- a dict hashes the slice and reports the miss by naming it ----
	{
		"a slice of a dict literal",
		"print({1: 2}[0:1])",
		"KeyError", "slice(0, 1, None)",
	},
	{
		"a slice of a dict kept in a name",
		"d = {1: 2}\nprint(d[0:1])",
		"KeyError", "slice(0, 1, None)",
	},
	{
		"a dict sliced by nothing at all names three absent bounds",
		"print({1: 2}[:])",
		"KeyError", "slice(None, None, None)",
	},
	{
		"a dict sliced by None bounds spells them the way the omitted ones are spelled",
		"print({1: 2}[None:None])",
		"KeyError", "slice(None, None, None)",
	},
	{
		"a dict sliced by a float and a bool names them as reprs",
		"print({1: 2}[1.5:True])",
		"KeyError", "slice(1.5, True, None)",
	},
	{
		"a dict sliced by bool bounds keeps the True/False spelling, not 1/0",
		"print({1: 2}[False:True])",
		"KeyError", "slice(False, True, None)",
	},
	{
		"a dict whose own keys are bools, sliced by bools",
		"print({True: 2}[True:False])",
		"KeyError", "slice(True, False, None)",
	},
	{
		"a dict sliced by text bounds quotes them, because a KeyError names its key",
		"print({1: 2}[\"a\":\"b\"])",
		"KeyError", "slice('a', 'b', None)",
	},
	{
		// The step is hashed, never applied: ADR 0337's ZeroDivisionError belongs to a base that counts, and
		// a dict never asks the step what it steps by.
		"a dict sliced by a step of zero names the key rather than the division",
		"print({1: 2}[0:1:0])",
		"KeyError", "slice(0, 1, 0)",
	},
	{
		"a dict sliced by a parenthesised bound names the tuple the way repr does",
		"print({1: 2}[(1, \"a\"):2])",
		"KeyError", "slice((1, 'a'), 2, None)",
	},
	{
		"a dict whose VALUES are lists is still not a sequence",
		"d = {\"k\": [1, 2, 3]}\nprint(d[0:1])",
		"KeyError", "slice(0, 1, None)",
	},
	{
		"a dict handed back by a call",
		"def m():\n    return {1: 2}\n\nprint(m()[0:1])",
		"KeyError", "slice(0, 1, None)",
	},
	{
		// Hashing comes before the lookup, so the first bound the reference cannot hash owns the sentence —
		// and it owns it rather than the miss, which is why the check runs first.
		"a dict sliced by a list bound is the hashing door's complaint",
		"print({1: 2}[[1]:2])",
		"TypeError", "unhashable type: 'list'",
	},
	{
		"a dict sliced by an unhashable HIGH bound, asked of a name",
		"d = {1: 2}\nprint(d[0:[1]])",
		"TypeError", "unhashable type: 'list'",
	},
	{
		"a dict sliced by a dict bound",
		"print({1: 2}[{1: 2}:1])",
		"TypeError", "unhashable type: 'dict'",
	},
	{
		"a dict sliced by a set bound",
		"print({1: 2}[{1}:1])",
		"TypeError", "unhashable type: 'set'",
	},
	{
		// The tuple hash walks left to right and names the first member it cannot hash, so a hashable
		// looking bound says 'list', not 'tuple' (the same rule the subscript door follows, ADR 0328).
		"an unhashable member of a tuple bound names the member",
		"print({1: 2}[(1, [2]):1])",
		"TypeError", "unhashable type: 'list'",
	},
}

// sliceBaseCatchRows are the same raises as language values: the program's own `except` takes them, and a
// raise the program can handle is not a crash (ADR 0211's exit-code contract, ADR 0166's worst classes).
var sliceBaseCatchRows = []struct {
	name string
	src  string
	want string
}{
	{
		"a slice of a set caught by except TypeError",
		"s = {1, 2}\ntry:\n    print(s[0:1])\nexcept TypeError:\n    print(\"caught\")",
		"caught",
	},
	{
		"a slice of a dict caught by except KeyError",
		"d = {1: 2}\ntry:\n    print(d[0:1])\nexcept KeyError:\n    print(\"caught\")",
		"caught",
	},
	{
		"an unhashable bound caught by except TypeError",
		"d = {1: 2}\ntry:\n    print(d[[1]:2])\nexcept TypeError:\n    print(\"caught\")",
		"caught",
	},
	{
		// The raise names its key to the program as well as to the traceback: what `except … as err` binds is the
		// key object's own repr — a slice's repr is `slice(0, 1, None)`, unquoted, which is why the report and the
		// arg differ by the caption and nothing else (ADR 0406's `str(err)` rule).
		"the caught KeyError's own str names the slice it missed",
		"d = {1: 2}\ntry:\n    print(d[0:1])\nexcept KeyError as err:\n    print(err)",
		"slice(0, 1, None)",
	},
	{
		"a raise the program handles leaves the rest of the program running",
		"d = {1: 2}\ntry:\n    print(d[0:1])\nexcept KeyError:\n    print(\"missed\")\nprint(d[1])",
		"missed\n2",
	},
	{
		"a caught set slice and a real list slice in one program",
		"s = {1, 2}\nxs = [1, 2, 3]\ntry:\n    print(s[0:1])\nexcept TypeError:\n    print(xs[0:2])",
		"[1, 2]",
	},
}

// sliceBaseKindIsReadNotSpelled is the soundness half: the gate names a kind only where a record names one, so
// the name's CURRENT kind is what answers. A rebinding that turns a dict into a list must not keep raising the
// dict's miss, and a name that only the run time can describe must not be accused of a kind nobody wrote.
var sliceBaseKindIsReadNotSpelled = []struct {
	name string
	src  string
	want string
}{
	{
		"a name that held a dict and now holds a list slices like a list",
		"d = {1: 2}\nd = [1, 2]\nprint(d[0:1])",
		"[1]",
	},
	{
		"a name that held a set and now holds a text slices like a text",
		"s = {1, 2}\ns = \"abc\"\nprint(s[0:1])",
		"a",
	},
	{
		"a name that held a list and now holds a dict raises the dict's miss",
		"d = [1, 2]\nd = {1: 2}\nprint(d[0:1])",
		"", // raises; asserted by the raise leg of the same row below
	},
	{
		"a copy of a list is a list, whatever the base's road was",
		"xs = [1, 2, 3]\nprint(xs[0:2][0:1])",
		"[1]",
	},
}

// sliceBaseUntouchedRows are the neighbours the gate must leave alone: every base that HAS positions keeps its
// answer, and every door that already had a sentence keeps that sentence. A row here that changed is the gate
// repairing the wrong door (ADR 0273: a gate may not cost a program that compiles today).
var sliceBaseUntouchedRows = []struct {
	name string
	src  string
	want string
}{
	{"a list literal sliced", "print([1, 2, 3][0:2])", "[1, 2]"},
	{"a list in a name sliced", "xs = [1, 2, 3]\nprint(xs[1:3])", "[2, 3]"},
	{"a list sliced by a step", "print([1, 2, 3][::-1])", "[3, 2, 1]"},
	{"a tuple sliced", "print((1, 2)[0:1])", "(1,)"},
	{"a text sliced", "print(\"abcdef\"[1:3])", "bc"},
	{"a copy sliced", "xs = [1, 2, 3]\nys = xs[0:2]\nprint(ys[0:1])", "[1]"},
	{"a call's list sliced", "def b():\n    return [1, 2, 3]\n\nprint(b()[0:2])", "[1, 2]"},
	{"a dict read by a key still answers", "d = {1: 2}\nprint(d[1])", "2"},
	{"a dict read by a missing key still misses by its own key", "d = {1: 2}\nprint(d[9])", ""},
	{"a dict's length and membership are untouched", "d = {1: 2}\nprint(len(d), 1 in d)", "1 True"},
	{"a set's length and membership are untouched", "s = {1, 2}\nprint(len(s), 1 in s)", "2 True"},
	{"a list's bounds door keeps ADR 0336's sentence", "xs = [1, 2]\nprint(xs[\"a\":\"b\"])", ""},
}

// TestASliceOfASetOrADictRaisesWhatTheBaseIs is the row's claim, asked of the compiled leg and of the record:
// the class and the sentence are the reference's, and neither leg is allowed to answer with a value.
func TestASliceOfASetOrADictRaisesWhatTheBaseIs(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseRaiseRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("%q raised %s: %s; the reference raises %s: %s", tt.src, class, message, tt.class, tt.message)
			}
			// The record leg, so a raise cannot be witnessed by the compiled leg alone: an entry the record
			// does not hold fails here rather than disappearing with the row it justified (ADR 0302).
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

// TestASliceBaseTrapIsCatchable is the same raise asked of the program rather than of the traceback.
func TestASliceBaseTrapIsCatchable(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseCatchRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference runs the handler and prints %q", tt.src, got, tt.want)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, got)
			}
		})
	}
}

// TestTheBaseIsAskedBeforeTheBounds is the order the row exists to install, asked as pairs: one bound, three
// bases. A list earns the bounds sentence, a set earns the object's, a dict earns hashing and then the miss —
// and a row that swapped two of these sentences is what this test is for.
func TestTheBaseIsAskedBeforeTheBounds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		bases []struct {
			src     string
			class   string
			message string
		}
	}{
		{
			// The same text bounds, written over the three bases.
			"text bounds over a list, a set and a dict",
			[]struct {
				src     string
				class   string
				message string
			}{
				{"xs = [1, 2]\nprint(xs[\"a\":\"b\"])", "TypeError", "slice indices must be integers or None or have an __index__ method"},
				{"print({1, 2}[\"a\":\"b\"])", "TypeError", "'set' object is not subscriptable"},
				{"print({1: 2}[\"a\":\"b\"])", "KeyError", "slice('a', 'b', None)"},
			},
		},
		{
			// The same list bound: the list's bounds door, no complaint at all for the set (the object is
			// refused first), and the hashing door for the dict.
			"a list bound over a list, a set and a dict",
			[]struct {
				src     string
				class   string
				message string
			}{
				{"xs = [1, 2]\nprint(xs[[1]:2])", "TypeError", "slice indices must be integers or None or have an __index__ method"},
				{"print({1, 2}[[1]:2])", "TypeError", "'set' object is not subscriptable"},
				{"print({1: 2}[[1]:2])", "TypeError", "unhashable type: 'list'"},
			},
		},
		{
			// A step of zero: the base that counts divides by it, the base that hashes never asks.
			"a step of zero over a list and a dict",
			[]struct {
				src     string
				class   string
				message string
			}{
				{"xs = [1, 2]\nprint(xs[0:1:0])", "ValueError", "slice step cannot be zero"},
				{"print({1: 2}[0:1:0])", "KeyError", "slice(0, 1, 0)"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seen := map[string]bool{}
			for _, b := range tt.bases {
				class, message := jitTrap(t, b.src)
				if class != b.class || message != b.message {
					t.Fatalf("%q raised %s: %s; the reference raises %s: %s", b.src, class, message, b.class, b.message)
				}
				seen[class+": "+message] = true
			}
			// A pair whose rows all collapsed onto one sentence means the gate stopped distinguishing the
			// bases it was added to ask about.
			if len(seen) != len(tt.bases) {
				t.Fatalf("the %d bases answered with %d distinct sentences: %v", len(tt.bases), len(seen), seen)
			}
		})
	}
}

// TestTheSliceBaseGateNamesAKindOnlyWhereARecordNamesIt is the soundness half: the kind comes from the binding,
// so a rebinding changes the answer, and a base no record describes keeps the road it has always had.
func TestTheSliceBaseGateNamesAKindOnlyWhereARecordNamesIt(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseKindIsReadNotSpelled {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.want == "" {
				// The row's answer is a raise; the same source with the OTHER kind claim is the row beside it.
				class, message := jitTrap(t, tt.src)
				if class != "KeyError" || message != "slice(0, 1, None)" {
					t.Fatalf("%q raised %s: %s; the reference raises KeyError: slice(0, 1, None) once the name holds a dict", tt.src, class, message)
				}
				return
			}
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tt.src, got, tt.want)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
		})
	}
}

// TestTheSliceBaseGateRepairsNothingElse is the ADR 0273 half: every base that has positions, and every door
// beside the slice road, keeps exactly what it answered before the gate existed.
func TestTheSliceBaseGateRepairsNothingElse(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseUntouchedRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.want == "" {
				// These rows belong to another door; the promise is only that the SAME sentence still arrives.
				class, message := jitTrap(t, tt.src)
				if class == "" {
					t.Fatalf("%q no longer raises; the door beside this row answered a program the reference traps", tt.src)
				}
				t.Logf("%s: %s", class, message)
				return
			}
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q and this shape answered before the gate", tt.src, got, tt.want)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
		})
	}
}

// TestTheSliceBaseRaiseIsEmittedWhereTheReadWas asks the module rather than the run: the road stores the
// exception the reference's class names and never reaches the copy helper. A gate that merely refused would
// pass the first half of nothing, and a road that still built a copy of a set would pass the whole row's
// behavioural tests by accident — so the copy is the thing to forbid here (ADR 0166's history is exactly that
// call, fed a global instead of a handle).
func TestTheSliceBaseRaiseIsEmittedWhereTheReadWas(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseRaiseRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, tt.src)
			report := tt.class + ": " + tt.message
			// The two texts a raise leaves in the module are the report (`"Class: message"`, what an uncaught
			// raise prints) and the arg (`message`, what `str(err)` binds) — both module globals, so the claim is
			// asked of the module rather than of @main alone.
			if !strings.Contains(ir, report) {
				t.Fatalf("the module never carries the raise %q the program is expected to report:\n%s", report, mainBodyOf(ir))
			}
			wantCode := "store i32 " + strconv.Itoa(exnCode(tt.class)) + ", i32* @exn_code"
			if !strings.Contains(ir, wantCode) {
				t.Fatalf("the raise did not set the exception code for %s:\n%s", tt.class, mainBodyOf(ir))
			}
			for _, line := range strings.Split(mainBodyOf(ir), "\n") {
				if strings.Contains(line, "call i32 @rt_slice(") {
					t.Fatalf("the road built a copy of a base it should have refused to slice:\n%s", line)
				}
			}
		})
	}
}

// TestTheSliceBaseRaiseEndsTheBlockItBranchedFrom pins the non-local road every raise in this backend takes:
// the gate's raise closes the block it branched out of and opens its OWN continuation, so the two raises in one
// statement are two sites and not one shared label (the block a reopen shares is how a raise starts printing
// the other half of the statement).
func TestTheSliceBaseRaiseEndsTheBlockItBranchedFrom(t *testing.T) {
	t.Parallel()
	ir := mustCompileIR(t, "s = {1, 2}\nd = {1: 2}\nprint(s[0:1], d[0:1])")
	sites := map[string]bool{}
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "sbb.") && strings.HasSuffix(trimmed, ":") {
			sites[trimmed] = true
		}
	}
	if len(sites) < 2 {
		t.Fatalf("the two raising slices share a continuation (sites %v):\n%s", sites, mainBodyOf(ir))
	}
	if strings.Contains(ir, "call i32 @rt_slice(") {
		t.Fatalf("neither slice reached the copy helper, and the module says otherwise:\n%s", mainBodyOf(ir))
	}
}

// TestWhatTheSliceBaseDoorStillCannotReachIsFiledNotFixed keeps the shapes the gate declines as refusals rather
// than letting them become answers. Each is a program the reference traps; the toolchain declines at exit 1 and
// says which record it is missing. Exit 2 — the code the contract reserves for a compiler bug — fails the row
// wherever it appears (ADR 0166), and exit 0 would be the wrong answer this repo files loudest (ADR 0273).
func TestWhatTheSliceBaseDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		mustSay string
		oracle  string
		gap     string
	}{
		{
			// The base is a set the records do not describe: spelled by its constructor, so no binding ever
			// claimed what the call hands back. The gate will not name a kind nobody wrote.
			"a slice of a set spelled by its constructor",
			"print(set([1, 2])[0:1])",
			"cannot slice",
			"TypeError: 'set' object is not subscriptable",
			"roadmap Gap R.554 (the kind a call hands back travels by the answer record, and `set(...)` has none — the row beside ADR 0333's answer road)",
		},
		{
			// The base IS described as a dict, but the bounds are expressions: the key the KeyError names would
			// have to be built at run time, and a fabricated repr is the defect class this repo files.
			"a dict sliced by bounds the compiler cannot read",
			"d = {1: 2}\ni = 0\nprint(d[i:i+1])",
			"cannot slice",
			"KeyError: slice(0, 1, None)",
			"roadmap Gap R.555 (the run-time slice key: the miss needs `slice(low, high, step)` as an object, which is L11.1's tagged value word)",
		},
		{
			// The raise's word has no kind, so a second read of it asks the slot road for a record and gets a
			// refusal. The reference never gets there: the inner slice raises first.
			"a position read of a slice that raises",
			"print({1, 2}[0:1][0])",
			"index requires an inline list/dict/set literal",
			"TypeError: 'set' object is not subscriptable",
			"roadmap Gap R.556 (the dead word a raise hands back carries no kind; the reference raises before the outer read is ever asked)",
		},
		{
			// The name holds a slot the records describe only through the object it was read out of.
			"a slice taken from a name bound by a slot read",
			"def m():\n    return {\"k\": [1.5]}\n\nd = m()[\"k\"]\nprint(d[0:1])",
			"cannot slice",
			"[1.5]",
			"roadmap Gap R.540 (the copy's gate asks for a base the records describe, and a slot read left no record of its own)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, trap, refused := sliceFiledRow(t, tc.src)
			if !refused {
				t.Fatalf("%q answered %q (trap %q); this shape is filed as owed, not fixed — if it now raises or prints, move it to the table above (reference: %s)",
					tc.src, out, trap, tc.oracle)
			}
			if !strings.Contains(out, tc.mustSay) {
				t.Fatalf("the refusal does not name %q (ADR 0249): %s", tc.mustSay, out)
			}
		})
	}
}

// TestTheSliceBaseRowsAreRecorded is the record leg's own guard: every raise row has an entry in the record, so
// the door cannot be deleted by deleting the evidence the suite reads (ADR 0302's rule that a missing record
// fails the case).
func TestTheSliceBaseRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBaseRaiseRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.ExnType == "" && rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded trap: re-record it before shipping the door that reads it", tt.src)
			}
		})
	}
	for _, tt := range sliceBaseCatchRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Stdout == "" {
				t.Fatalf("%q has no recorded answer: the handler's bytes are the record's to hold", tt.src)
			}
		})
	}
}
