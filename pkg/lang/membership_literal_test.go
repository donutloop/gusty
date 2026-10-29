package lang

// Membership asked of a container the SOURCE spells — `1 in [x]`, `"a" in [y]`, `5 in [f()]` — on the
// compiled leg and on the record leg.
//
// roadmap Gap R.523 · ADR 0456
//
// The program that filed this row printed three different things about one literal:
//
//	x = 5
//	print([x])        →  [5]        the container door builds it, slots carry a kind
//	print(len([x]))   →  1          the same door
//	print(1 in [x])   →  exit 1: codegen: list literal elements must be integers, not int
//
// The membership road lowered its haystack through the number road, and the number road's answer for a
// literal is a folded `@.lstN` global whose element slots are `i32` words: a name, a call, an arithmetic
// expression has no word to write into one, so the whole program was refused for asking an ordinary
// question — with a message that named an element kind the literal was not asking about (**ADR 0166**'s
// second clause). The door here asks the haystack the printer asks: built once by the container door, and
// the question goes to `rt_contains_tagged`, the object's own slots answering it.
//
// The second table is worth more than the first. The folded unroll compares payloads alone, and asks nothing
// else of the needle; so the void was a member of a list of whole numbers (`None in [0]` read the void's word
// as a 0), a double was a member of nothing at all (`1.0 in [1]` was refused outright, because the numeric
// cross-kind arm belongs to `rt_payload_eq` and not to `icmp eq i32`), and a container needle reached the
// untagged scan in a shape `llc` rejected — exit 2, the compiler's own mistake, for `print([1] in [1, 2])`.
// Those rows are answered here because the object answers them, not because this row went looking.
//
// `TestWhatTheLiteralHaystackDoorStillDeclines` is the residue, pinned with today's bytes and the row that
// owns each: an element whose answer is a container, and an element no door names an answer for. The door
// falls through to the refusal that already named its owner rather than minting a vaguer one.

import (
	"os"
	"strings"
	"testing"
)

// membershipLiteralHaystackRows is the family the door reaches: a haystack written as a list, a set or a
// dict, whose elements are names, calls, expressions, containers — anything but a constant the fold could
// have written into a global.
func membershipLiteralHaystackRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// A NAME: the shape that filed the row, in both verdicts and negated.
		{"a name, absent", "x = 5\nprint(1 in [x])\n", "False\n"},
		{"a name, present", "x = 5\nprint(5 in [x])\n", "True\n"},
		{"the negated question", "x = 5\nprint(1 not in [x])\n", "True\n"},
		{"a name the program wrote twice", "x = 5\nx = 5\nx += 1\nprint(6 in [x])\n", "True\n"},
		// A TEXT — the other element kind the refusal named ("not str"), and the reason the defect is the
		// whole literal road rather than one element kind.
		{"a text element", "y = \"a\"\nprint(\"a\" in [y])\n", "True\n"},
		{"a text element, absent", "y = \"a\"\nprint(\"b\" in [y])\n", "False\n"},
		// A CALL and an EXPRESSION: the element is an evaluation, and its effects are the program's.
		{"a call's answer", "def f():\n    return 5\n\nprint(5 in [f()])\n", "True\n"},
		{"an arithmetic element", "x = 4\nprint(5 in [x + 1])\n", "True\n"},
		{"the haystack's call still runs", "def probe():\n    print(\"needle\")\n\n    return 5\n\nprint(5 in [probe()])\n", "needle\nTrue\n"},
		// MIXED slots: one literal holding a number, a text and a name — no single element kind, so the
		// builder writes a kind beside every payload and the object decides.
		{"a mixed literal", "x = 5\nprint(5 in [1, \"a\", x])\n", "True\n"},
		{"a mixed literal, absent", "x = 5\nprint(2 in [1, \"a\", x])\n", "False\n"},
		{"two names, one literal", "a = 1\nb = 2\nprint(b in [a, b])\n", "True\n"},
		{"a double element", "x = 2.5\nprint(1 in [1, x])\n", "True\n"},
		{"a double element, absent", "x = 2.5\nprint(3 in [1, x])\n", "False\n"},
		// The SET and DICT spellings: the same door, the object's other two shapes. A dict answers its KEYS.
		{"a set built from a name", "x = 5\nprint(5 in {x})\n", "True\n"},
		{"a set built from a name, absent", "x = 5\nprint(1 in {x})\n", "False\n"},
		{"a set of two names", "a = 1\nb = 2\nprint(b in {a, b})\n", "True\n"},
		{"a dict keyed by a name", "x = 5\nprint(5 in {x: \"v\"})\n", "True\n"},
		{"a dict keyed by a name, absent", "x = 5\nprint(7 in {x: \"v\"})\n", "False\n"},
		{"a dict keyed by a text the program named", "k = \"key\"\nprint(\"key\" in {k: 1})\n", "True\n"},
		// A CONTAINER element, and a needle that is one.
		{"a container element, asked by name", "x = [1]\nprint(x in [x])\n", "True\n"},
		{"a nested literal", "print([1] in [[1], [2]])\n", "True\n"},
		// An ELEMENT WHOSE ANSWER IS A CONTAINER: the callee built the object, and the slot stores the handle
		// the callee's builders made with the callee's kind beside it (**ADR 0457**, roadmap **Gap R.530**).
		{"a container answer, asked as a container", "def f():\n    return [1, 2]\n\nprint([1, 2] in [f()])\n", "True\n"},
		{"a container answer, absent", "def f():\n    return [1, 2]\n\nprint(1 in [f()])\n", "False\n"},
		{"a dict answer, the number absent", "def g():\n    return {\"a\": 1}\n\nprint(1 in [g()])\n", "False\n"},
		{"a dict answer, the text absent — the LIST holds a dict, not a key", "def g():\n    return {\"a\": 1}\n\nprint(\"a\" in [g()])\n", "False\n"},
		{"a dict answer, asked by content", "def g():\n    return {\"a\": 1}\n\nprint({\"a\": 1} in [g()])\n", "True\n"},
		{"a set answer asked of its members", "def s():\n    return {1, 2}\n\nprint(1 in [s()])\n", "False\n"},
		{"a set answer, asked by content", "def s():\n    return {1, 2}\n\nprint({1, 2} in [s()])\n", "True\n"},
		{"an answer that nests", "def f():\n    return [1, [2]]\n\nprint([1, [2]] in [f()])\n", "True\n"},
		{"the answer is a member of its own literal", "def f():\n    return [1, 2]\n\nprint(f() in [f()])\n", "True\n"},
		{"a container answer beside a number", "def f():\n    return [1, 2]\n\nprint(7 in [f(), 7])\n", "True\n"},
		{"a container answer built by a comprehension", "def f():\n    return [1, 2]\n\nprint([f()] in [[f()] for x in range(1)])\n", "True\n"},
		{"the haystack one level above the answer", "def f():\n    return [1, 2]\n\nprint([1, 2] in [[f()] for x in range(1)])\n", "False\n"},
		// The positions a membership test stands in, each lowering through a different road.
		{"inside a body, the body's own name", "def g():\n    x = 5\n    print(1 in [x])\n\ng()\n", "False\n"},
		{"a module name read from a body", "x = 5\ndef g():\n    print(5 in [x])\n\ng()\n", "True\n"},
		{"standing in a condition", "x = 5\nif 1 in [x]:\n    print(\"yes\")\n\nelse:\n    print(\"no\")\n", "no\n"},
		{"standing in a while test", "n = 0\nwhile n in [0, 1]:\n    print(n)\n    n = n + 1\n", "0\n1\n"},
		{"unrolled over a literal the program built", "x = 5\nfor v in [x]:\n    print(v)\n", "5\n"},
		{"counted", "x = 5\nprint(len([x, 1]))\n", "2\n"},
	}
}

func TestMembershipOfASpelledLiteralHaystackAnswers(t *testing.T) {
	t.Parallel()
	for _, r := range membershipLiteralHaystackRows() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", r.want, err, r.src)
			}
			if got != r.want {
				t.Fatalf("printed %q, want %q\n%s", got, r.want, r.src)
			}
			onRecordIs(t, r.src, r.want)
		})
	}
}

// membershipNeedlesTheFoldCannotName is the load-bearing table, because every row in it is an answer the
// compiler used to get WRONG or refuse rather than one it never reached: the folded unroll compares payloads
// alone, so it read the void's word as a 0, could not name a double at all, and handed a container needle to
// the untagged scan in a shape `llc` rejected (exit 2 — the compiler's own mistake, for a program the
// reference prints in one line). The rows are kept separate from the table above so a future edit cannot
// quietly drop the claim that the OBJECT, not the fold, answered them.
func membershipNeedlesTheFoldCannotName() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"the void is not a member of a list of whole numbers", "print(None in [0])\n", "False\n"},
		{"the void, asked of a literal with a name beside it", "x = 0\nprint(None in [x])\n", "False\n"},
		{"a double equals the whole number it denotes", "print(1.0 in [1])\n", "True\n"},
		{"a double that does not", "print(1.5 in [1, 2])\n", "False\n"},
		{"a double named", "x = 2.5\nprint(x in [1, 3])\n", "False\n"},
		{"a text is not a member of a list of numbers", "print(\"a\" in [1, 2])\n", "False\n"},
		{"a container needle, absent — the shape that died in llc", "print([1] in [1, 2])\n", "False\n"},
		{"a container needle, present", "print([1] in [[1], [2]])\n", "True\n"},
		{"a verdict is a member of the number it denotes", "print(True in [1])\n", "True\n"},
		{"a verdict beside the number it is not", "print(True in [0])\n", "False\n"},
	}
}

func TestTheObjectAnswersTheNeedlesTheFoldCannotName(t *testing.T) {
	t.Parallel()
	for _, r := range membershipNeedlesTheFoldCannotName() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", r.want, err, r.src)
			}
			if got != r.want {
				t.Fatalf("printed %q, want %q — the payload-only fold answered, or the door answered wrongly\n%s", got, r.want, r.src)
			}
			onRecordIs(t, r.src, r.want)
		})
	}
}

// TestTheFoldedHaystackStillCostsNoObject is the door's own economy, and the claim that it did not replace a
// working road with an allocation: a haystack the fold can serve — constant whole numbers, a needle the fold
// can name — still compiles to an `icmp` chain with no container built and no runtime asked.
func TestTheFoldedHaystackStillCostsNoObject(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(1 in [1, 2, 3])\n",
		"print(4 not in [1, 2, 3])\n",
		"x = 2\nprint(x in [1, 2, 3])\n",
	} {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("compile: %v\n%s", err, src)
			}
			main := inMainBody(t, res.IR)
			if strings.Contains(main, "@rt_contains") {
				t.Errorf("a haystack the fold serves still asks a runtime scan:\n%s", main)
			}
			for _, build := range []string{"@rt_alloc(", "@rt_set_elem(", "@rt_tag_elem("} {
				if strings.Contains(main, build) {
					t.Errorf("a haystack the fold serves still builds an object (%s):\n%s", build, main)
				}
			}
		})
	}
}

// TestTheLiteralHaystackDoorAsksTheObjectThePrinterAsks is the one-question claim in the module's own bytes.
// The row exists because the SAME literal compiled at one door and refused at another; the answer is that
// there is now one door, which is only true if the two programs ask the builder the same way. So the module
// that PRINTS `[x]` and the module that asks MEMBERSHIP of `[x]` must emit the same build calls, and the
// membership module must ask the tagged scan rather than the payload-only one.
func TestTheLiteralHaystackDoorAsksTheObjectThePrinterAsks(t *testing.T) {
	t.Parallel()
	src := "x = 5\n"
	printed, err := Compile(src + "print([x])\n")
	if err != nil {
		t.Fatalf("compile (print): %v", err)
	}
	asked, err := Compile(src + "print(1 in [x])\n")
	if err != nil {
		t.Fatalf("compile (membership): %v", err)
	}
	pm, am := inMainBody(t, printed.IR), inMainBody(t, asked.IR)
	for _, build := range []string{"@rt_alloc(", "@rt_set_elem(", "@rt_tag_elem("} {
		if p, a := strings.Count(pm, build), strings.Count(am, build); p == 0 || p != a {
			t.Errorf("the two roads build the same literal %d and %d times (%s) — one literal, one builder", p, a, build)
		}
	}
	if n := strings.Count(am, "@rt_contains_tagged("); n != 1 {
		t.Errorf("the membership module asks the tagged scan %d times, want 1:\n%s", n, am)
	}
	// The payload-only scan is the answer the door replaces: it compares a needle's word with a slot's word
	// and asks nothing about either kind, which is how the void became a member of `[0]`.
	if strings.Contains(am, "@rt_contains(") {
		t.Errorf("the membership module still asks the untagged scan:\n%s", am)
	}
}

// TestWhatTheLiteralHaystackDoorStillDeclines is the residue, pinned with today's bytes and the row that owns
// each. Both are refusals the door walks past on purpose: `containerOperand` declines a shape no builder
// covers, and the ordinary literal road below answers with the sentence that already names its owner. A row
// here leaves the file — into the answered table above — when the row it names pays.
func TestWhatTheLiteralHaystackDoorStillDeclines(t *testing.T) {
	t.Parallel()
	rows := []struct{ name, src, contains, owner string }{
		{
			// A tuple answer in a slot: the object the callee built carries the tuple bit in its own
			// `@estr[h]` word (**ADR 0361**) and the OUTER list's printer has no road that reads that bit
			// through the slot, so the element door declines the shape rather than label a handle with a tag
			// nothing honours — `xs = [t()]` printed `[1]` for `[(1, 2)]` when it was tried (roadmap
			// **Gap R.533**; **ADR 0457**'s declined arm).
			"a tuple answer as an element",
			"def t():\n    return (1, 2)\n\nprint([t()])\n",
			"hands back a container",
			"Gap R.533",
		},
		{
			// An attribute call: no door names an answer for it, so the element's kind is `any` and the
			// folded road is the one that says so.
			"an element no door names an answer for",
			"import math\nprint(1 in [math.sqrt(4)])\n",
			"list literal elements must be integers",
			"Gap R.531",
		},
		{
			// The same door, the message's own defect: a lambda's `exprTyName` is the empty string, so the
			// refusal ends in `not ` and names neither the element nor what the slot needs (ADR 0166).
			"an element whose kind has no name at all",
			"print(1 in [lambda: 1])\n",
			"list literal elements must be integers",
			"Gap R.531",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			_, err := RunSnippet(r.src)
			if err == nil {
				t.Fatalf("the shape compiles today — the row that owns it (%s) has paid, so move this row to the answered table\n%s", r.owner, r.src)
			}
			if !strings.Contains(err.Error(), r.contains) {
				t.Fatalf("it failed for another reason (%v); the row pins the refusal %s owns\n%s", err, r.owner, r.src)
			}
		})
	}
}

// TestTheRecordSourcesTheLiteralHaystackRowAsksFor is the recorder, not a case: with
// GUSTY_RECORD_MEMBERSHIP_LITERAL set it writes every source the tables in this file ask about to the file
// that variable names, in the JSON shape `tools/recmerge` reads, and skips. Without it, it skips — a case
// that wrote a file every run would let two parallel runs overwrite each other's ledger, which is why the
// artifact-writing modes stay single-process.
//
// The list lives here rather than being retyped in a scratch program because these are the sources the cases
// actually ask: a recorder that drifts from its own table is what lets a row ship with no record under it
// (ADR 0302).
func TestTheRecordSourcesTheLiteralHaystackRowAsksFor(t *testing.T) {
	out := os.Getenv("GUSTY_RECORD_MEMBERSHIP_LITERAL")
	if out == "" {
		t.Skip("the recorder: set GUSTY_RECORD_MEMBERSHIP_LITERAL=<file> to dump these sources for tools/recmerge")
	}
	var srcs []string
	add := func(s string) {
		for _, have := range srcs {
			if have == s {
				return
			}
		}
		srcs = append(srcs, s)
	}
	for _, r := range membershipLiteralHaystackRows() {
		add(r.src)
	}
	for _, r := range membershipNeedlesTheFoldCannotName() {
		add(r.src)
	}
	for _, src := range []string{
		"print(1 in [1, 2, 3])\n",
		"print(4 not in [1, 2, 3])\n",
		"x = 2\nprint(x in [1, 2, 3])\n",
	} {
		add(src)
	}
	body, merr := marshalGoldenMissing(srcs)
	if merr != nil {
		t.Fatalf("rendering the source list: %v", merr)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("writing %s: %v", out, err)
	}
	t.Logf("wrote %d sources to %s", len(srcs), out)
}
