package lang

import (
	"strings"
	"testing"
)

// membership_needle_pair_test.go — a membership test whose needle IS a slot read answers, because the needle
// arrives as the (payload, tag) pair the equality door already computes. roadmap **Gap R.232**, **ADR 0474**.
//
// The row started as a refusal that named nothing:
//
//	def words():
//	    return ["hi", "ho"]
//
//	print(words()[0] in words())   # CPython True · this toolchain: exit 1 with
//	                               # `testing &{%!s(*lang.Call=&{0x4000… [] … list[str]}) …} against the
//	                               #  container "__cans10" … needs a needle whose kind the compiler can prove`
//
// and it was never a missing capability. `xs[i] in xs` and `xs[i] == xs[j]` ask the SAME question — is this
// value one of the values those slots hold — and the equality door had answered its half since **ADR 0247**:
// `comparePair` hands back the payload and the tag the objects wrote, with the bounds check the read carries
// built beside it. The membership door asked `taggedOperand` (a needle whose kind the SOURCE settled) and,
// when that failed, refused — a door answering one question two ways, which is **ADR 0232**'s defect class,
// with **ADR 0249**'s dishonest sentence on top of it: the reader was shown the needle's Go representation
// and the compiler's own temp name where the program's text belongs.
//
// The fix is therefore not a new representation but a new edge: ask the pair door. A needle whose kind is a
// run-time fact the source never settled still refuses, and refuses in the program's words — the
// `pick`-two-armed-answer row in the refusal table below is **Gap R.83**'s and **Gap R.212**'s shape, not
// this row's, and stays a refusal.
//
// Two hazards decided the shape of the code, and both are asserted below rather than assumed:
//
//   - The pair is built in a SCRATCH builder. The slot read emits its own bounds check, and the IndexError
//     branch TERMINATES a block; a pair that failed halfway would leave instructions after a terminator and
//     `llc` would report the compiler's mistake as the program's (**ADR 0166** — exit 2, the code a compiler
//     bug owns). The row's trap table is the witness: `xs[9] in xs` raises `IndexError`, it does not hang up
//     the module.
//   - The needle's own base may be a call that hands back a container. That road already exists —
//     `containerAnswerSubscript` materialises the answer under the rooted slot a container variable occupies
//     and hands back the subscript over the name (**ADR 0334**) — so `words()[0] in words()` and
//     `w = words()` / `w[0] in w` are one program written two ways. Asserted as a pair of rows, because a
//     door that answers one spelling and declines the other is how this row was filed.
//
// Every `want` below is CPython's: the answering rows run the same source through `PythonRun` in the case
// itself, and `integration/membership_needle_pair_test.go` re-derives each of them from a live `python3`
// rather than from this file (the standing rule that an expectation is read, not remembered).

// needlePairRows is the family: a needle that is a slot read, at the door whose slots describe themselves.
// Each row is asked of the compiled leg, of the reference and — in the record-leg test — of the record.
var needlePairRows = []struct{ src, want string }{
	// The row's own two spellings: the answer as the needle's base, and the same answer bound first.
	{"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words())", "True"},
	{"def words():\n    return [\"hi\", \"ho\"]\n\nw = words()\nprint(w[0] in w)", "True"},
	{"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words(), words()[1] not in words())", "True False"},
	{"def mk():\n    return [1, \"x\"]\n\nprint(mk()[1] in mk())", "True"},
	// A slot of a container the SOURCE spelled out, in every kind the tag carries. A payload on its own is
	// meaningless for each of these: a stored text is an @str_tab index, and only the tag says so.
	{"xs = [1, \"a\"]\nprint(xs[0] in xs)", "True"},
	{"xs = [1, \"a\"]\nprint(xs[1] in xs)", "True"},
	{"xs = [1, \"a\"]\nprint(xs[0] not in xs)", "False"},
	{"xs = [1, \"a\"]\nprint(xs[0] in xs, xs[1] in xs, 3 in xs)", "True True False"},
	{"xs = [1, \"a\"]\nprint(xs[-1] in xs)", "True"},
	{"xs = [1, \"a\", None, 2.5]\nprint(xs[2] in xs)", "True"},
	{"xs = [1, \"a\", None, 2.5]\nprint(xs[3] in xs, xs[1] in xs)", "True True"},
	{"xs = [1, \"a\", None, 2.5]\nprint(xs[0] in xs, xs[2] in xs, xs[3] in xs)", "True True True"},
	// A slot read through a position the program computes: the read's own bounds check travels with it.
	{"xs = [1, \"a\"]\ni = 1\nprint(xs[i] in xs)", "True"},
	{"xs = [\"abc\", 2]\ni = 0\nprint(xs[i] in xs)", "True"},
	// A container slot: the payload is a heap handle, and a handle compared payload-only is the fabricated
	// number this family exists to keep out (ADR 0187's rule, asked of `in`).
	{"xs = [[7, 8], \"a\"]\nprint(xs[0] in xs)", "True"},
	{"xs = [[7, 8], \"a\"]\nprint(xs[1] in xs)", "True"},
	{"xs = [[7, 8], \"a\"]\nprint(xs[0][0] in xs[0])", "True"},
	{"xs = [{\"k\": 1}, \"a\"]\nprint(xs[0] in xs)", "True"},
	// A container the program BUILT rather than spelled: only the object knows what its slots hold.
	{"xs = []\nxs.append(\"q\")\nxs.append(1)\nprint(xs[0] in xs)", "True"},
	{"xs = []\nxs.append(\"q\")\nxs.append(1)\nprint(xs[1] in xs)", "True"},
	{"xs = []\nxs.append([7, 8])\nxs.append(3)\nprint(xs[0] in xs)", "True"},
	// The tag is what discriminates: the same text payload as an integer key of another container is a
	// different value, and the pair — not the number — is what says so (ADR 0232's collision, asked of a
	// needle read out of one container and asked of another).
	{"ints = [1, 2]\ntexts = [\"zz\", 1]\nprint(texts[0] in ints)", "False"},
	{"ints = [1, 2]\ntexts = [\"zz\", 1]\nprint(texts[1] in ints)", "True"},
	{"nums = [0, 5]\ntexts = [\"q\", 7]\nprint(texts[0] in nums)", "False"},
	// Every position a verdict reaches: a condition, a ternary arm, a compound, a loop body, a binding.
	{"xs = [1, \"a\"]\nif xs[1] in xs:\n    print(\"yes\")", "yes"},
	{"xs = [1, \"a\"]\nif xs[0] in xs:\n    print(\"left\")\nelse:\n    print(\"right\")", "left"},
	{"xs = [1, \"a\"]\nprint(1 if xs[1] in xs else 0)", "1"},
	{"xs = [1, \"a\"]\nprint(xs[1] in xs and xs[0] in xs)", "True"},
	{"xs = [1, \"a\"]\nprint(xs[1] in xs or xs[0] in xs)", "True"},
	{"xs = [1, \"a\"]\nfor v in xs:\n    print(v in xs)", "True\nTrue"},
	{"xs = [1, \"a\"]\nprint(xs[0] == xs[1], xs[0] in xs)", "False True"},
	// The door is the membership road, not the equality road wearing its coat: an answer that is a container
	// still asks the object, and the equality beside it keeps answering as it always did.
	{"xs = [1, \"a\"]\nprint(xs[1] in xs, xs[1] == \"a\")", "True True"},
}

// TestAMembershipNeedleThatIsASlotReadAnswers is the answering half: the compiled leg prints what the
// reference prints, for the same source, in the same case.
func TestAMembershipNeedleThatIsASlotReadAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range needlePairRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got := strings.TrimRight(compiledOut(t, tt.src), "\n")
			if got != tt.want {
				t.Fatalf("compiled %q printed %q, want %q", tt.src, got, tt.want)
			}
			want, _, err := PythonRun(tt.src + "\n")
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			if strings.TrimRight(want, "\n") != tt.want {
				t.Fatalf("this row's expectation has drifted from the reference: CPython prints %q, the row says %q", strings.TrimRight(want, "\n"), tt.want)
			}
		})
	}
}

// TestAMembershipNeedleIsJudgedOnTheRecordLeg too: every answered source has an answer ON RECORD, so a later
// cycle cannot take the door away without taking the evidence away with it (**ADR 0302**'s rule that a
// missing record fails the case rather than passing quietly).
func TestAMembershipNeedleIsJudgedOnTheRecordLeg(t *testing.T) {
	t.Parallel()
	for _, tt := range needlePairRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it (tools/recmerge) before shipping the door that reads it", tt.src)
			}
			if out := goldenStdout(t, tt.src); strings.TrimRight(out, "\n") != tt.want {
				t.Fatalf("the record answers %q, the compiled leg answers %q", strings.TrimRight(out, "\n"), tt.want)
			}
		})
	}
}

// The IR shape the row turns on: the needle's tag is the REGISTER the objects wrote, not a constant the
// compiler guessed, and the read underneath it is the ordinary element door — one door, not a second decoder
// (**ADR 0232**). Delete the register and every container needle in the language goes back to a payload,
// which is exactly the wrong answer this row's filed table pins.
func TestTheMembershipNeedleArrivesAsThePairTheObjectsWrote(t *testing.T) {
	t.Parallel()
	ir := mainBodyOf(emittedModuleIR(t, "xs = [1, \"a\"]\nprint(xs[1] in xs)"))
	for _, want := range []string{
		"call i32 @rt_get_elem(",          // the needle is read by the door every slot read uses
		"call i32 @rt_tag_of(",            // …and its kind is asked of the same object
		"call i32 @rt_contains_tagged(i32 %", // the haystack is the loaded handle, the needle is two registers
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the module lacks %q:\n%s", want, ir)
		}
	}
	// The tag argument of that call is a register, not a constant: a constant tag is the compiler stating an
	// opinion about a value it has not looked at.
	call := ir[strings.Index(ir, "call i32 @rt_contains_tagged("):]
	args := call[strings.Index(call, "(")+1:]
	args = args[:strings.Index(args, ")")]
	fields := strings.Split(args, ", ")
	if len(fields) != 3 || !strings.HasPrefix(fields[2], "i32 %") {
		t.Fatalf("the membership call's needle tag is not a register (%q):\n%s", args, ir)
	}
	// The same claim, for a needle whose base is a call's container answer: the answer is materialised under a
	// rooted slot and read by the ordinary door (ADR 0334), not lowered to the handle it was built from.
	ir2 := mainBodyOf(emittedModuleIR(t, "def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words())"))
	for _, want := range []string{"call i32 @rt_get_elem(", "call i32 @rt_tag_of(", "call i32 @rt_contains_tagged(i32 %"} {
		if !strings.Contains(ir2, want) {
			t.Fatalf("the call-answer needle lacks %q:\n%s", want, ir2)
		}
	}
}

// The negative control the door must keep: a needle whose kind the SOURCE settled never goes through the pair
// door, and a haystack the fold answers never reaches the object doors at all. Without this row the door
// could be widened into the fold — the module `llc` rejects at exit 2 (**ADR 0166**, **ADR 0192**) — and
// every row above would stay green.
func TestTheMembershipNeedleDoorLeavesTheFoldAndTheProvenKindAlone(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"xs = [1, 2]\nprint(1 in xs)",
		"xs = [\"a\", \"b\"]\nprint(\"a\" in xs)",
		"xs = [1, \"a\"]\nprint(\"a\" in xs)",
		"print(1 in [1, 2])",
		"print(\"k\" in {\"k\": 1})",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			ir := mainBodyOf(emittedModuleIR(t, src))
			for _, forbidden := range []string{"call i32 @rt_get_elem(", "call i32 @rt_tag_of("} {
				if strings.Contains(ir, forbidden) {
					t.Fatalf("%q opened the slot-read door for a needle the source settles (%s):\n%s", src, forbidden, ir)
				}
			}
		})
	}
	// A literal haystack keeps the compile-time road: no container is allocated for a membership the fold
	// answers, so the door cannot have taken the fold's answer through the heap.
	ir := mainBodyOf(emittedModuleIR(t, "print(1 in [1, 2])"))
	if strings.Contains(ir, "@rt_alloc") {
		t.Fatalf("a folded membership allocated an object:\n%s", ir)
	}
}

// The trap half, which is the half that decides whether the scratch builder is load-bearing: the needle's own
// bounds check ends a block, and a pair built into the statement's builder would leave instructions after the
// terminator. The class, the sentence and the catchability are the reference's, and exit 2 — the code the
// contract reserves for a compiler bug — is forbidden on every one of them (**ADR 0166**, **ADR 0214**).
func TestATrapInsideAMembershipNeedleIsRaisedNotRefused(t *testing.T) {
	t.Parallel()
	// `pre` is the program above the statement, kept out of the indenting so a `def`'s own body is never
	// re-entered by the handler the row wraps around the raise.
	for _, tt := range []struct {
		pre, stmt      string
		class, message string
		catch          string
	}{
		{"xs = [1, \"a\"]", "print(xs[9] in xs)", "IndexError", "list index out of range", "IndexError"},
		{"xs = [1, \"a\"]", "print(xs[-3] in xs)", "IndexError", "list index out of range", "IndexError"},
		{"def words():\n    return [\"hi\", \"ho\"]", "print(words()[5] in words())", "IndexError", "list index out of range", "IndexError"},
		{"d = {\"a\": 1}", "print(d[\"b\"] in d)", "KeyError", "'b'", "KeyError"},
		{"xs = [1, 2]", "print(xs[0][0] in xs)", "TypeError", "'int' object is not subscriptable", "TypeError"},
		{"xs = {\"a\": 1}", "print(xs[\"a\"][0] in xs)", "TypeError", "'int' object is not subscriptable", "TypeError"},
	} {
		t.Run(tt.stmt, func(t *testing.T) {
			t.Parallel()
			src := tt.pre + "\n" + tt.stmt
			class, message := jitTrap(t, src)
			if class != tt.class || message != tt.message {
				t.Fatalf("raised %s: %s, want %s: %s", class, message, tt.class, tt.message)
			}
			prog := tt.pre + "\ntry:\n    " + tt.stmt + "\nexcept " + tt.catch + ":\n    print(\"caught\")"
			if out := strings.TrimRight(compiledOut(t, prog), "\n"); out != "caught" {
				t.Fatalf("the raise is not catchable from the program's own handler: %q", out)
			}
		})
	}
}

// The trap inside a LOOP is the shape that makes the scratch builder a requirement rather than a style: the
// read's IndexError branch terminates a block inside the loop body, and the statement goes on to ask the
// container. The reference's output — two lines printed, then the raise — is the expectation, line for line.
func TestATrapInsideALoopNeedleKeepsTheBlocksWellFormed(t *testing.T) {
	t.Parallel()
	src := "xs = [1, \"a\"]\ni = 0\nwhile i < 3:\n    print(i, xs[i] in xs)\n    i += 1"
	want := "0 True\n1 True\n"
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("the module does not build: %v", err)
	}
	if res.Code == 2 {
		t.Fatalf("exit 2 — the code a compiler bug owns — on a program the reference runs:\n%s", res.Stderr)
	}
	if res.Output != want {
		t.Fatalf("the run printed %q, want %q", res.Output, want)
	}
	if _, class, message := ParseTrapReport(res.Stderr); class != "IndexError" || message != "list index out of range" {
		t.Fatalf("the loop raised %s: %s, want IndexError: list index out of range\n%s", class, message, res.Stderr)
	}
	if wantPy, _, err := PythonRun(src); err == nil && wantPy != want {
		t.Fatalf("the reference prints %q for the same loop, and this row's expectation is not the reference's", wantPy)
	}
}

// The refusal half, and the honesty half. A needle whose kind is a run-time fact the SOURCE never settled is
// still a refusal — **Gap R.83**'s two-armed answer, **Gap R.212**'s unprovable needle — but the sentence is
// the program's, not the compiler's: **Gap R.232**'s row was filed because the reader was handed the needle's
// Go representation and the compiler's temp name instead of the construct that was asked (**ADR 0249**).
func TestAMembershipNeedleTheCompilerCannotKindRefusesInProgramWords(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		says []string
	}{
		{
			"an answer whose two arms settle nothing",
			"def pick(v):\n    if v:\n        return v\n    return \"a\"\n\nxs = [1, \"a\"]\nprint(1 if pick(0) in xs else 0)",
			[]string{"pick(0)", "xs", "roadmap L11.1"},
		},
		{
			"a call whose answer carries no kind",
			"def f(x):\n    return x\n\nxs = [1, \"a\"]\nprint(f(xs[1]) in xs)",
			[]string{"f(xs[1])", "xs"},
		},
		{
			"the needle the record's own row asks",
			"def pick(v):\n    if v:\n        return v\n    return \"a\"\n\nxs = [1, \"a\"]\nprint(1 if pick(0) in xs else 0)",
			[]string{"needs a needle whose kind"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tt.src)
			if !refused {
				t.Fatalf("%q answered %q; a needle with no kind has no arm, and answering it is the coincidence this family refuses", tt.src, out)
			}
			for _, want := range tt.says {
				if !strings.Contains(out, want) {
					t.Fatalf("the refusal does not name %q (ADR 0249):\n%s", want, out)
				}
			}
			// The shape a refusal sentence must never have: the compiler's own vocabulary where the
			// program's belongs. This is the row's definition of done, and it is a claim about every
			// refusal the door can print, not about the one sample it was filed with.
			for _, forbidden := range []string{"%!s(", "*lang.", "__cans", "%!d(", "%v"} {
				if strings.Contains(out, forbidden) {
					t.Fatalf("the refusal leaks the compiler's own representation %q (roadmap Gap R.232, Gap R.249):\n%s", forbidden, out)
				}
			}
		})
	}
}

// ADR 0232's rule, asked of the two doors that read the same pair: anywhere the equality door answers with a
// slot read as an operand, the membership door may not decline the same operand. A door that answers one and
// refuses the other is how this row was filed, and this table is the guard against the next such pair.
func TestTheMembershipDoorNeverRefusesWhatTheEqualityDoorAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, eq, mem string }{
		{"a slot of a literal list", "xs = [1, \"a\"]\nprint(xs[1] == \"a\")", "xs = [1, \"a\"]\nprint(xs[1] in xs)"},
		{"a slot read through a computed position", "xs = [1, \"a\"]\ni = 1\nprint(xs[i] == \"a\")", "xs = [1, \"a\"]\ni = 1\nprint(xs[i] in xs)"},
		{"a slot of a call's container answer", "def words():\n    return [\"hi\", \"ho\"]\n\nw = words()\nprint(w[0] == \"hi\")", "def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words())"},
		{"a slot of a built container", "xs = []\nxs.append(\"q\")\nxs.append(1)\nprint(xs[0] == \"q\")", "xs = []\nxs.append(\"q\")\nxs.append(1)\nprint(xs[0] in xs)"},
		{"a container slot", "xs = [[7, 8], \"a\"]\nprint(xs[0] == [7, 8])", "xs = [[7, 8], \"a\"]\nprint(xs[0] in xs)"},
		{"a slot two levels down", "xs = [[7, 8], \"a\"]\nprint(xs[0][1] == 8)", "xs = [[7, 8], \"a\"]\nprint(xs[0][1] in xs[0])"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eqOut, eqRefused := compiledOutOrRefusal(t, tt.eq)
			if eqRefused {
				t.Skipf("the equality door itself refuses: %s", eqOut)
			}
			memOut, memRefused := compiledOutOrRefusal(t, tt.mem)
			if memRefused {
				t.Fatalf("the equality door answers %q and the membership door refuses %q: %s (one question, one door — ADR 0232)",
					strings.TrimSpace(eqOut), strings.TrimSpace(memOut), memOut)
			}
			want, _, err := PythonRun(tt.mem + "\n")
			if err != nil {
				t.Fatalf("the reference could not run the program: %v", err)
			}
			if got := strings.TrimSpace(memOut); got != strings.TrimSpace(want) {
				t.Fatalf("membership answered %q, the reference answers %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// What the door still cannot reach, pinned with today's answer beside the reference's so the row fails the
// day the door grows — and, just as loudly, the day one of these answers drifts without a row owning it.
// None of these is this row's missing half: each names the row that owns it.
func TestWhatTheMembershipNeedleDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today ("" = it refuses)
		oracle   string // what CPython answers
		gap      string
	}{
		{
			// **Gap R.582**, measured while closing Gap R.232: the needle arrives as a pair at the door
			// whose slots describe themselves; a DICT literal's slots do not, and the untagged road that
			// serves them reads the needle's payload under a kind the compiler asserted. The reference
			// looks the value up by what it IS.
			"a slot of a dict read back as a membership needle",
			"d = {\"a\": \"a\", \"b\": 2}\nprint(d[\"a\"] in d)",
			"False", "True", "roadmap Gap R.582 (a dict literal's slots describe nothing, so the needle keeps one word)",
		},
		{
			// **Gap R.582**'s second face: a container needle asked of a dict is not an absent key, it is a
			// key the language refuses to hash (**ADR 0328**'s rule, applied at the membership door for a
			// needle the compiler can only read through the object).
			"a container read out of a dict, asked of that dict",
			"d = {\"a\": 1, \"b\": [2]}\nprint(d[\"b\"] in d)",
			"False", "TypeError: unhashable type: 'list'", "roadmap Gap R.582 (the unhashable question is asked of a key the source spells, not of one read out of a slot)",
		},
		{
			// **Gap R.583**, the other direction: the HAYSTACK is a literal whose element is a slot read of
			// a call's answer — the container the builder fills with a pair, and the membership road that
			// reads it back compares payloads. The needle's own kind is the pair this door supplies; what
			// is missing is the haystack's claim that its slots describe themselves.
			"a slot of a list whose element is a call's slot read",
			"def g():\n    return [7, \"z\"]\n\nxs = [g()[1], 3]\nprint(xs[0] in xs)",
			"False", "True", "roadmap Gap R.583 (a literal built with a pair element is not claimed as a container whose slots describe themselves)",
		},
		{
			// **Gap R.212** beside **Gap R.83**: the needle's kind is a run-time fact the source never
			// settled. This is the row the capability table's sample now shows, and the honest direction
			// to be wrong in.
			"a needle whose kind only a run-time branch knows",
			"def pick(v):\n    if v:\n        return v\n    return \"a\"\n\nxs = [1, \"a\"]\nprint(1 if pick(0) in xs else 0)",
			"", "0", "roadmap Gap R.212 / Gap R.83 (the tag a call's answer carries is L11.7's and L11.1's)",
		},
		{
			// **Gap R.146**'s one-word position, seen from the membership door: the haystack is a literal the
			// fold builds, and the needle is a pair with nowhere to put its tag (**ADR 0456**'s road).
			"a needle that is a pair asked of a container the source spells",
			"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[1] in [\"hi\", \"ho\"])",
			"", "True", "roadmap Gap R.146 (the one-word position at the literal-membership door; ADR 0456's road)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if tc.compiled == "" {
				if !refused {
					t.Fatalf("%q answered %q where this row records a refusal and the reference answers %q — the door grew, so move the row into the tables above (%s)",
						tc.src, out, tc.oracle, tc.gap)
				}
				if strings.Contains(out, "llc") || strings.Contains(out, "verify") {
					t.Fatalf("%q failed as a toolchain rejection: %s", tc.src, out)
				}
				for _, forbidden := range []string{"%!s(", "*lang.", "__cans"} {
					if strings.Contains(out, forbidden) {
						t.Fatalf("the refusal leaks %q (%s):\n%s", forbidden, tc.gap, out)
					}
				}
				return
			}
			if refused {
				t.Fatalf("%q refuses (%s); this row records the answer %q for %s", tc.src, out, tc.compiled, tc.gap)
			}
			if got := strings.TrimRight(out, "\n"); got != tc.compiled {
				t.Fatalf("%q answered %q, the answer recorded for %s was %q (reference: %q)",
					tc.src, got, tc.gap, tc.compiled, tc.oracle)
			}
		})
	}
}
