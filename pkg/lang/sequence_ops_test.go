package lang

// Gap R.33 — the sequence operations the compiled backend builds.
//
// The row was: `[1] + [2]` emitted `%t1 = add i32 @.lst1, @.lst2`, `llc` rejected the module, and the
// CLI reported the compiler-bug class; `"ab" * 2` refused in words; CPython answered both. The fix is
// one door — `pkg/lang/sequence_ops.go` decides the shape and `pkg/lang/codegen_ir_sequence.go` owns
// the helpers it calls — placed where the operands can still be asked, and a refusal is reserved for a
// fact the compiler cannot see, never for a shape (ADR 0365).
//
// Each row is a ladder, read bottom-up (ADR 0273's shape for this family): the **record leg** — the
// answer on `testdata/interpreter-golden.json` for the source, or the reference where no record was
// ever taken — then the **compiled leg** through `lli`, then the **emitted IR**: that the module
// verifies, that the helper it calls is the one this door owns, and that the shape that used to be the
// bug is not in it. The reference leg is `integration/conformance_cases.go`'s `sequence_builds` row
// and `TestSequenceOperationsMatchCPython`.
//
// @reftest python3

import (
	"strings"
	"testing"
)

// TestTheCompiledSequenceDoorAnswersTheProgramTheReferenceAnswers is the ladder: every shape is one the
// reference answers, so the compiled leg must ANSWER it — a refusal on any of these is the failure this
// row was about, not an acceptable answer.
func TestTheCompiledSequenceDoorAnswersTheProgramTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name string
		src  string
		want string
	}{
		{"list concat", "print([1] + [2])\n", "[1, 2]\n"},
		{"list concat, three deep", "print([1] + [2] + [3])\n", "[1, 2, 3]\n"},
		{"empty concat left", "print([] + [1])\n", "[1]\n"},
		{"empty concat right", "print([1] + [])\n", "[1]\n"},
		{"list repeat", "print([1] * 3)\n", "[1, 1, 1]\n"},
		{"int times list", "print(3 * [1])\n", "[1, 1, 1]\n"},
		{"list repeat, zero", "print([1] * 0)\n", "[]\n"},
		{"list repeat, negative", "print([1] * -2)\n", "[]\n"},
		{"list repeat, foldable product", "print([1, 2] * (2 * 2 - 1))\n", "[1, 2, 1, 2, 1, 2]\n"},
		{"mixed payloads", "print([1, \"a\", None, True] + [2.5])\n", "[1, 'a', None, True, 2.5]\n"},
		{"tuple concat", "print((1, 2) + (3,))\n", "(1, 2, 3)\n"},
		{"tuple repeat", "print((1, 2) * 2)\n", "(1, 2, 1, 2)\n"},
		{"str repeat", "print(\"ab\" * 2)\n", "abab\n"},
		{"int times str", "print(2 * \"ab\")\n", "abab\n"},
		{"str repeat, zero", "print(\"ab\" * 0)\n", "\n"},
		{"str repeat, negative", "print(\"ab\" * -1)\n", "\n"},
		{"bound names", "xs = [1, 2]\nys = [3]\nprint(xs + ys)\n", "[1, 2, 3]\n"},
		{"bound list repeated", "xs = [7]\nprint(xs * 2)\n", "[7, 7]\n"},
		{"concat then length", "print(len([1] + [2, 3]))\n", "3\n"},
		{"concat then membership, absent", "print(2 in [1] + [3])\n", "False\n"},
		{"concat then membership, present", "print(3 in [1] + [3])\n", "True\n"},
		{"a built tuple's membership", "print(2 in (1,) + (2,))\n", "True\n"},
		{"concat then iterate", "for v in [1] + [2]:\n    print(v)\n", "1\n2\n"},
		{"text repeat then length", "print(len(\"ab\" * 3))\n", "6\n"},
		{"text repeat then index", "print((\"ab\" * 3)[1])\n", "b\n"},
		{"text repeat then membership", "print(\"ab\" in \"ab\" * 2)\n", "True\n"},
		{"a built list is printed by the object", "print(str([1] + [2]))\n", "[1, 2]\n"},
		{"a bound built list prints as the same text", "ys = [1] + [2]\nprint(str(ys))\n", "[1, 2]\n"},
		{"repr of a mixed built list", "print(repr([\"a\"] + [1]))\n", "['a', 1]\n"},
		{"an f-string field asks the object", "print(f\"{[1] + [2]}\")\n", "[1, 2]\n"},
		{"an f-string field of a text repeat", "print(f\"{'ab' * 2}|\")\n", "abab|\n"},
		{"a built list is equal to a literal", "print([1] + [2] == [1, 2])\n", "True\n"},
		{"two bound built lists are equal", "xs = [1] + [2]\nys = [1, 2]\nprint(xs == ys)\n", "True\n"},
		{"a repeat of a text repeats", "print(\"ab\" * 2 == \"abab\")\n", "True\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			// The record leg: the answer on record for this source, where one was ever taken.
			if _, known := loadGolden(t)[r.src]; known {
				if got := goldenStdout(t, r.src); got != r.want {
					t.Fatalf("the record reads %q for %q, want %q — the record and the reference must agree before the compiled leg is judged", got, r.src, r.want)
				}
			}
			// The compiled leg.
			if got := compiledOut(t, r.src); got != r.want {
				t.Fatalf("the compiled leg printed %q, want %q", got, r.want)
			}
			// The IR leg: the helper this door owns is called, and the shape that used to be the bug
			// (`add i32 @.lst1, @.lst2`, `mul i32 @.lst1, 3`, a container global in an arithmetic
			// instruction) is not in the module.
			ir := mustCompileIR(t, r.src)
			for _, banned := range []string{"add i32 @.lst", "mul i32 @.lst", "mul i32 @tuple", "global i32 @.lst"} {
				if strings.Contains(ir, banned) {
					t.Fatalf("%q is in the emitted module — a container went through the numeric road (Gap R.33): %s", banned, r.src)
				}
			}
		})
	}
}

// TestTheSequenceDoorRaisesWhereTheReferenceRaises is the other half of the same door. Every one of
// these is a program the reference *stops*, which in this backend means a raise that `except TypeError`
// reaches and the exit code of a raised error — not a refusal at compile time, and not an answer.
func TestTheSequenceDoorRaisesWhereTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name string
		src  string
		want string
	}{
		{"list times a float", "print([1] * 2.0)\n", "TypeError: can't multiply sequence by non-int of type 'float'"},
		{"float times a list", "print(2.0 * [1])\n", "TypeError: can't multiply sequence by non-int of type 'float'"},
		{"text times a float", "print(\"a\" * 1.5)\n", "TypeError: can't multiply sequence by non-int of type 'float'"},
		{"text times a text", "print(\"a\" * \"b\")\n", "TypeError: can't multiply sequence by non-int of type 'str'"},
		{"list plus a text", "print([1] + \"a\")\n", "TypeError: can only concatenate list (not \"str\") to list"},
		{"dict plus a dict", "print({1: 1} + {1: 1})\n", "TypeError: unsupported operand type(s) for +: 'dict' and 'dict'"},
		{"set plus a set", "print({1} + {2})\n", "TypeError: unsupported operand type(s) for +: 'set' and 'set'"},
		{"tuple plus a list", "print((1, 2) + [1])\n", "TypeError: can only concatenate tuple (not \"list\") to tuple"},
		{"list plus a tuple", "print([1] + (1, 2))\n", "TypeError: can only concatenate list (not \"tuple\") to list"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(r.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference raises for, in words that are not the reference's: %v", err)
			}
			if out := runIRMayTrap(t, res.IR); !strings.Contains(out, r.want) {
				t.Fatalf("the compiled leg did not raise the reference's %q: %s", r.want, out)
			}
		})
	}
}

// TestASequenceRaiseReachesAnExceptHandler is the ADR 0228 half the numeric door had to learn the hard
// way: a trap the door raises outside a function body still has to be catchable, because the handler
// table is per-function and the frame is the only thing tying a raise to it.
func TestASequenceRaiseReachesAnExceptHandler(t *testing.T) {
	t.Parallel()
	src := "try:\n    print(\"a\" * 1.5)\nexcept TypeError:\n    print(\"caught\")\n"
	want := "caught\n"
	if got := compiledOut(t, src); got != want {
		t.Fatalf("the compiled leg printed %q, want %q", got, want)
	}
}

// TestTheSequenceRuntimeIsEmittedOnlyWhenTheProgramAsksForIt is the module-size half of the arrangement:
// a helper the program never calls is never emitted, and a program that only repeats a text does not pay
// for the concatenate — or the reverse. `@rt_seed` is the always-emitted control.
func TestTheSequenceRuntimeIsEmittedOnlyWhenTheProgramAsksForIt(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name     string
		src      string
		wantUsed string
	}{
		{"no sequence at all", "print(1 + 1)\n", ""},
		{"a concat asks for one helper", "print([1] + [2])\n", "rt_seq_concat"},
		{"a list repeat asks for one helper", "print([1] * 2)\n", "rt_seq_repeat"},
		{"a text repeat asks for one helper", "print(\"a\" * 2)\n", "rt_str_repeat"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, r.src)
			if r.wantUsed == "" {
				if strings.Contains(ir, "define internal i32 @rt_seq_") || strings.Contains(ir, "define internal i32 @rt_str_repeat(") {
					t.Fatalf("a program with no sequence operation emitted the sequence runtime anyway: %s", r.src)
				}
				return
			}
			called := "call i32 @" + r.wantUsed + "("
			if !strings.Contains(ir, called) {
				t.Fatalf("the helper call %s is missing from the emitted module: %s", called, r.src)
			}
			if !strings.Contains(ir, "define internal i32 @"+r.wantUsed+"(") {
				t.Fatalf("the helper %s is called but never emitted (Gap R.173)", r.wantUsed)
			}
		})
	}
}

// TestTheSequenceDoorLeavesEveryOtherRoadAlone guards the boundary the door has to keep: `1 % 3` is still
// a remainder, `"%s" % 3` is still a formatting, `2 * 3` is still a multiplication, and a `+` the pass
// cannot prove two sequences on is still refused rather than guessed at.
func TestTheSequenceDoorLeavesEveryOtherRoadAlone(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		src  string
		want string
	}{
		{"print(7 % 3)\n", "1\n"},
		{"print(2 * 3)\n", "6\n"},
		{"print(2.5 * 2)\n", "5.0\n"},
		{"print(\"hello %s\" % \"you\")\n", "hello you\n"},
		{"print(\"a\" + \"b\")\n", "ab\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, r.src); got != r.want {
				t.Fatalf("printed %q, want %q", got, r.want)
			}
		})
	}
}

// TestTheConditionRoadRefusesABuiltOrderingInWordsRatherThanBreakTheModule is the half of the row the value door
// cannot see. An `if` condition is lowered by codegen_truth.go, not by valueBinOp, so the refusal the value
// road gives for `print([1, 2] < [2])` did NOT extend to `if [1, 2] < [2]:` — that pair fell through to the
// generic comparison arm, rendered both operands, and emitted `%t1 = icmp slt i32 @.lst1, @.lst2`, which
// `llc` rejects: the compiler-bug exit code (ADR 0166) for a program the reference answers, and the exact
// invalid IR this row's own bug was. Both roads now ask one predicate (ADR 0232), and the ladder is the
// same as everywhere else in this file: the condition position must produce the value position's WORDS at
// the refusal exit code, and the module must never contain the corruption.
func TestTheConditionRoadRefusesABuiltOrderingInWordsRatherThanBreakTheModule(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src string }{
		{"if", "if [1, 2] < [2]:\n    print(\"yes\")\n"},
		{"while", "n = 0\nwhile [1, 2] < [2]:\n    n = n + 1\nprint(n)\n"},
		{"a ternary test", "x = 1 if [1, 2] < [2] else 2\nprint(x)\n"},
		{"an `and` operand", "if [1, 2] < [2] and 1:\n    print(\"yes\")\n"},
		{"a `not` operand", "if not ([1, 2] < [2]):\n    print(\"yes\")\n"},
		{"a built side on the left", "if [1] + [2] < [1, 2]:\n    print(\"yes\")\n"},
		{"two built tuples", "if (1, 2) < (2,):\n    print(\"yes\")\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(r.src)
			if err == nil {
				t.Fatalf("the condition road compiled a shape Gap R.314 owns without a word — it must refuse like the value road does")
			}
			msg := err.Error()
			for _, needle := range []string{"has no compiled lowering", "roadmap Gap R.314", "tagged order door"} {
				if !strings.Contains(msg, needle) {
					t.Fatalf("the refusal %q does not carry %q — the condition road must say what is missing and which row owns it", msg, needle)
				}
			}
			if strings.Contains(msg, "llc") || strings.Contains(msg, "global variable reference") {
				t.Fatalf("the condition road reached `llc` with the corrupt module instead of refusing it: %s", msg)
			}
		})
	}
	// The ladder stays climbed: an ordering of two containers the program BOUND in names is answered by the
	// tagged order door, and a refusal there is a regression this backend has already had once (Gap R.175).
	if got := compiledOut(t, "a = [1, 2]\nb = [2]\nif a < b:\n    print(\"less\")\n"); got != "less\n" {
		t.Fatalf("the condition road refused an ordering of two bound containers, want \"less\\n\", got %q", got)
	}
	// And the corruption is banned by name, in the one condition shape the door answers.
	ir := mustCompileIR(t, "if [1] + [2]:\n    print(\"yes\")\n")
	for _, banned := range []string{"icmp slt i32 @.lst", "icmp eq i32 @.lst", "add i32 @.lst"} {
		if strings.Contains(ir, banned) {
			t.Fatalf("%q is in the emitted module — a built container went through the numeric comparison road (Gap R.33)", banned)
		}
	}
}

// TestTheTextRepeatIsOneValueAtEveryRoadThatReadsAText is the half of the door that the print leg alone
// does not prove. `print("ab" * 2)` answered `abab` while `print(["ab" * 2, 1])` answered `[1, 1]`,
// `str("ab" * 2)` answered the empty line, `{"ab" * 2: 1}` answered `{1: 1}` and `for c in "ab" * 2` walked
// the digits of the intern INDEX — every one of them exit 0, every one of them the same value read by a road
// that had been taught about the formatting (ADR 0360) and not about the repeat. One predicate now answers
// "does this operator's answer live in `@str_tab`?" for both doors, and every road asks it (ADR 0232;
// roadmap Gap R.33, ADR 0365). The reference leg is the same table run against `python3` by
// `TestSequenceOperationsMatchCPython`.
func TestTheTextRepeatIsOneValueAtEveryRoadThatReadsAText(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, want string }{
		{"printed", "print(\"ab\" * 2)\n", "abab\n"},
		{"an element of a list literal", "print([\"ab\" * 2, 1])\n", "['abab', 1]\n"},
		{"an element of a list literal, bound text", "s = \"ab\"\nprint([s * 2, 1])\n", "['abab', 1]\n"},
		{"a dict key", "print({\"ab\" * 2: 1})\n", "{'abab': 1}\n"},
		{"a dict value", "print({\"k\": \"ab\" * 2})\n", "{'k': 'abab'}\n"},
		{"a set element", "print({\"ab\" * 2})\n", "{'abab'}\n"},
		{"read by str()", "print(str(\"ab\" * 2))\n", "abab\n"},
		{"read by repr()", "print(repr(\"ab\" * 2))\n", "'abab'\n"},
		{"an f-string field", "print(f\"{'ab' * 2}|\")\n", "abab|\n"},
		{"walked by a for", "for c in \"ab\" * 2:\n    print(c)\n", "a\nb\na\nb\n"},
		{"returned by a function", "def f():\n    return \"ab\" * 2\n\nprint(f())\n", "abab\n"},
		{"a text parameter repeated", "def f(s):\n    return s * 2\n\nprint(f(\"hi\"))\n", "hihi\n"},
		{"a text parameter, measured", "def f(s):\n    return s * 2\n\nprint(f(\"hi\"), len(f(\"hi\")))\n", "hihi 4\n"},
		{"an interpolation repeated", "n = 3\nprint(f\"x{n}\" * 2)\n", "x3x3\n"},
		{"bound and compared", "ys = \"ab\" * 2\nprint(ys == \"abab\", ys + \"!\")\n", "True abab!\n"},
		{"a slot read repeated", "xs = [[1, 2], [3]]\nprint(xs[0] * 2)\n", "[1, 2, 1, 2]\n"},
		{"a container answer concatenated", "def b():\n    return [1, 2]\n\nprint(b() + b())\n", "[1, 2, 1, 2]\n"},
		{"a container answer repeated", "def b():\n    return [1, 2]\n\nprint(b() * 2)\n", "[1, 2, 1, 2]\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if _, known := loadGolden(t)[r.src]; known {
				if got := goldenStdout(t, r.src); got != r.want {
					t.Fatalf("the record reads %q for %q, want %q", got, r.src, r.want)
				}
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Fatalf("the compiled leg printed %q, want %q", got, r.want)
			}
			// The number underneath a text is never printed as the text (Gap R.42, ADR 0224).
			ir := mustCompileIR(t, r.src)
			for _, banned := range []string{"add i32 @.str", "mul i32 @.str", "icmp eq i32 @.str"} {
				if strings.Contains(ir, banned) {
					t.Fatalf("%q is in the emitted module — a text went through the numeric road (Gap R.42): %s", banned, r.src)
				}
			}
		})
	}
}

// TestTheSequenceDoorStillRefusesWhatItCannotBuild pins the three shapes Gap R.314 owns, so the door
// cannot quietly answer one of them with a number while the row is open: each must refuse IN WORDS, at
// the refusal exit code, with the roadmap row in the sentence — never a wrong value, never the
// compiler-bug class.
func TestTheSequenceDoorStillRefusesWhatItCannotBuild(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, needle string }{
		{"the subscript of a built sequence", "print(([1] + [2])[1])\n", "Gap R.314"},
		{"the order of two containers no name holds", "print([1, 2] < [2])\n", "Gap R.175"},
		{"a build past one object's row", "print([1] * 300)\n", "roadmap Gap R.271"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(r.src)
			if err == nil {
				out := runIRMayTrap(t, res.IR)
				t.Fatalf("the door answered %q for a shape Gap R.314 owns; it must refuse in words instead", out)
			}
			if !strings.Contains(err.Error(), r.needle) || !strings.Contains(err.Error(), "roadmap") {
				t.Fatalf("the refusal %q does not name what is missing and the row that owns it", err.Error())
			}
		})
	}
}
