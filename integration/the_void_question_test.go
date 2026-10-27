package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/the_void_question_test.go — the REFERENCE leg of **ADR 0411** (roadmap Gap R.412): what the
// LINKED PROGRAM prints when `is`/`is not` is asked against the void of a value whose kind the container owns.
//
// The row's claim fits in a sentence: `xs.pop(0) is None` and `xs.pop(0) == None` ask one question of one
// value, and the answer is the tag the removal carried out of `@heap_tags` — not a decision about what the
// source wrote. What makes it a suite rather than two assertions is that the question is asked in eight
// positions (the printer, `str`, a condition, a short-circuit, a dict value, a method's return, a `while` head,
// a binding), that four other values carry the same kind of tag (a slot read, a tuple's slot, a loop element,
// a name the pair road bound), and that five shapes still differ and are pinned below with the row that owns
// each of them rather than dropped.
//
// Every expectation here is re-derived from a live `python3` inside the case, so a table whose premise drifted
// fails rather than certifying the drift; the traps are checked as traps (exit 3, never exit 1 for a refusal and
// never exit **2**, which ADR 0166 reserves for a module `llc` had to reject).
//
// The measurement, with a build of the parent commit `af8b847` beside a build of this one, over the programs
// these two files carry: **17 of them moved to the reference's answer and none moved away from it**; 11 controls
// held on both builds; 6 still differ and are rows — **Gap R.388** (a call that answers the void), **Gap R.415**
// (the same list under a second name, SINCE PAID by **ADR 0412** and moved to `theAliasAnswered()`'s family in
// `the_alias_shares_the_record_test.go`), **Gap R.416** (a loop item over a literal), **Gap R.417** (a callee's own
// parameter) and **Gap R.418** (identity against a container literal, which reaches the assembler and dies).

func voidQuestionFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		{"the row's own program", "xs = [None, 1]\nprint(xs.pop(0) is None)\n", "True\n"},
		{"its equality twin", "xs = [None, 1]\nprint(xs.pop(0) == None)\n", "True\n"},
		{"the negation", "xs = [None, 1]\nprint(xs.pop(0) is not None)\n", "False\n"},
		{"both spellings of one slot", "xs = [None, 1]\nprint(xs.pop(0) is None, xs.pop(0) is not None)\n", "True True\n"},
		{"the same slot asked twice", "xs = [None, 1]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is None)\n", "True\nFalse\n"},
		{"a text's slot", "xs = [\"a\", None]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is not None)\n", "False\nFalse\n"},
		{"a container's slot", "xs = [[1], None]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is None)\n", "False\nTrue\n"},
		{"a dict's slot", "xs = [{\"a\": 1}, None]\nprint(xs.pop(0) is None)\n", "False\n"},
		{"a number's slot", "xs = [1, 2]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is not None)\n", "False\nTrue\n"},
		{"the default index", "xs = [None, 1]\nprint(xs.pop() is None)\n", "False\n"},
		{"a negative index", "xs = [1, None]\nprint(xs.pop(-1) is None)\n", "True\n"},
		{"drained from the front", "xs = [None, 1, \"a\"]\nwhile xs:\n    print(xs.pop(0) is None)\n", "True\nFalse\nFalse\n"},
		{"the verdict through str()", "xs = [None, 1]\nprint(str(xs.pop(0) is None))\n", "True\n"},
		{"the verdict as a condition", "xs = [None, 1]\nif xs.pop(0) is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"the verdict in a short-circuit", "xs = [None, 1]\nprint(xs.pop(0) is None and \"t\")\n", "t\n"},
		{"the verdict as a dict value", "xs = [None, 1]\nd = {\"k\": xs.pop(0) is None}\nprint(d)\n", "{'k': True}\n"},
		{"the verdict out of a method", "class T:\n    def m(self):\n        xs = [None, 1]\n        return xs.pop(0) is None\nprint(T().m())\n", "True\n"},
		{"the verdict in a while head", "xs = [None, 1]\nwhile xs.pop(0) is None:\n    print(\"void\")\n    break\n", "void\n"},
		{"a slot read of a mixed list", "xs = [None, 1]\nprint(xs[0] is None)\nprint(xs[1] is None)\n", "True\nFalse\n"},
		{"a slot read of a tuple", "ys = (1, None)\nprint(ys[0] is None)\nprint(ys[1] is None)\n", "False\nTrue\n"},
		{"a loop element over a named list", "xs = [None, \"a\"]\nfor v in xs:\n    print(v is None)\n", "True\nFalse\n"},
		// The same three questions asked of the item of a loop over a LITERAL list, which the unroll now
		// answers from the kind it records per body copy (**Gap R.416**, **ADR 0426**). They stand here, and in
		// the record, because until this row they were the owed table's rows.
		{"a loop element over a literal list", "for v in [None, \"a\"]:\n    print(v is None)\n", "True\nFalse\n"},
		{"its equality twin", "for v in [None, \"a\"]:\n    print(v == None)\n", "True\nFalse\n"},
		{"the item in the literal it unrolls from", "for k in [None, 1.5]:\n    print(k in [None, 1.5])\n", "True\nTrue\n"},
		{"a name the pair road bound", "xs = [None, \"a\"]\nn = xs[0]\nprint(n is None)\nif n is None:\n    print(\"y\")\n", "True\ny\n"},
		{"a written void", "print(None is None)\nprint(None is not None)\n", "True\nFalse\n"},
		{"a void bound to a name", "x = None\nprint(x is None)\n", "True\n"},
		// The literal spellings (`print(1 is None)`) are the unit suite's fold controls; a live CPython
		// answers them with a SyntaxWarning on stderr, so the reference leg asks them of names instead.
		{"the void asked of literals", "x = 1\ns = \"a\"\nprint(x is None)\nprint(s is None)\nprint([1] == None)\n", "False\nFalse\nFalse\n"},
		{"identity between numbers", "n = 1\nm = 1\nprint(n is m)\nprint(n is not m)\n", "True\nFalse\n"},
		{"a single-kind list keeps its road", "xs = [7, 8, 9]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) == 8)\n", "False\nTrue\n"},
		{"the append neighbour", "xs = [None, 1]\nxs.append(\"a\")\nprint(xs)\n", "[None, 1, 'a']\n"},
	}
}

// TestTheVoidQuestionFamilyAnswersLikeTheReference runs every program of the family through the linked
// compiler and re-derives its expectation from a live CPython in the same case.
func TestTheVoidQuestionFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range voidQuestionFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_void_question.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: the module reached the assembler broken (ADR 0166): %s\n%s", engine, stderr, tc.src)
				}
				if code != 0 {
					t.Fatalf("%s: exit %d where the reference prints %q: %s\n%s", engine, code, want, stderr, tc.src)
				}
				if stdout != want {
					t.Fatalf("%s: printed %q, the reference printed %q\n%s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// TestTheVoidQuestionFamilyIsOnTheRecord makes the family's coverage auditable: every program the row pays for
// has the reference's answer on the record, so the answer cannot be deleted by deleting a test and cannot be
// certified by the backend that produced it (ADR 0302).
func TestTheVoidQuestionFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range voidQuestionFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d of the %d programs this row pays for are not on the record — write them from the reference "+
			"with tools/recmerge:\n  %s", len(missing), len(voidQuestionFamily()), strings.Join(missing, "\n  "))
	}
}

// TestTheVoidQuestionTrapAtTheCLIIsTheReferenceTrap asks the one program in the family that TRAPS rather than
// prints: a drained list, popped past its end. The compiled leg must exit 3 with the reference's class and
// sentence — a raise the compiler raises and a raise the program earns are the same raise or the second one is
// untestable — and must never reach exit 1 (a refusal) or exit 2 (ADR 0166's broken module).
func TestTheVoidQuestionTrapAtTheCLIIsTheReferenceTrap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, sentence string
	}{
		{"popped past the end", "xs = [None]\nprint(xs.pop(0) is None)\nprint(xs.pop(0) is None)\n", "pop from empty list"},
		{"a bad index", "xs = [None, 1]\nprint(xs.pop(7) is None)\n", "pop index out of range"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_void_trap.gy", tc.src)
			for _, engine := range cliEngines {
				_, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: exit 2 — the module was the compiler's mistake, not the program's (ADR 0166)\n%s", engine, stderr)
				}
				if code == 1 {
					t.Fatalf("%s: the compiler refused a program the reference runs to a trap: %s\n%s", engine, stderr, tc.src)
				}
				if code != 3 {
					t.Fatalf("%s: exit %d, want 3 for the reference's trap: %s\n%s", engine, code, stderr, tc.src)
				}
				if !strings.Contains(stderr, "IndexError: "+tc.sentence) {
					t.Fatalf("%s: the raise reads %q, which does not carry %q\n%s", engine, stderr, "IndexError: "+tc.sentence, tc.src)
				}
			}
		})
	}
}

// TestTheVoidQuestionIsCatchableByTheProgram keeps the trap inside the language: the program's own handler sees
// it, which is the only difference between a trap the runtime raises and a diagnostic the compiler prints.
func TestTheVoidQuestionIsCatchableByTheProgram(t *testing.T) {
	t.Parallel()
	src := "xs = []\ntry:\n    print(xs.pop(0) is None)\nexcept IndexError as e:\n    print(\"caught\", e)\n"
	path := writeSrc(t, t.TempDir(), "the_void_catch.gy", src)
	want, ok := cpythonOut(t, path)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, engine := range cliEngines {
		stdout, stderr, code := cliStreams(t, engine, path)
		if code != 0 {
			t.Fatalf("%s: exit %d where the reference prints %q: %s\n%s", engine, code, want, stderr, src)
		}
		if stdout != want {
			t.Fatalf("%s: printed %q, the reference printed %q\n%s", engine, stdout, want, src)
		}
	}
}

// TestTheVoidQuestionsRefusalsAreMachineReadable is the agentic face: what the identity road declines reaches an
// agent as a capability code, a suggestion and the roadmap rows that own it — never as prose alone (ADR 0006,
// ADR 0346).
func TestTheVoidQuestionsRefusalsAreMachineReadable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, code string }{
		{"a void asked of a tuple's method", "ys = (1, None)\nprint(ys.pop(0) is None)\n", "lower.unsupported.tuple-is-immutable"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_void_json.gy", tc.src)
			out, code := cliRunCode(t, "--json", path)
			if code != 1 {
				t.Fatalf("a refused program exited %d, want 1 (ADR 0006): %s", code, out)
			}
			for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"code":"` + tc.code + `"`, `"suggestion":`, `"rows":`} {
				if !strings.Contains(out, frag) {
					t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
				}
			}
			if strings.Contains(out, "%!s(") || strings.Contains(out, "*lang.") {
				t.Fatalf("the JSON refusal prints the compiler's own types at the agent:\n%s", out)
			}
		})
	}
}

// TestWhatTheVoidQuestionStillOwesAtTheCLI is the reference leg of the honest half: each row pins the
// reference's bytes beside this backend's and names the row that owns the fix. When a program starts printing
// the reference's bytes the row is DELETED, not edited.
func TestWhatTheVoidQuestionStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		{
			name: "a call that answers the void", gap: "Gap R.388",
			src:    "def f():\n    return None\nprint(f() is None)\nprint(f() == None)\n",
			python: "True\nTrue\n", compiled: "False\nFalse\n",
		},
		{
			name: "a callee's own parameter", gap: "Gap R.417",
			src:    "def f(v):\n    return v is None\nprint(f(None))\nprint(f(1))\n",
			python: "True\nFalse\n", compiled: "False\nFalse\n",
		},
		{
			name: "identity against a container literal", gap: "Gap R.418",
			src:    "xs = [[1], 2]\nprint(xs.pop(0) is [1])\n",
			python: "False\n", compiled: "",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "the_void_owed.gy", r.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != r.python {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", r.python, want, r.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if r.compiled == "" {
					// The shape the assembler refuses: ADR 0166's class, pinned so its removal is a decision.
					if code != 2 {
						t.Fatalf("%s: %s no longer reaches the assembler — it exits %d with %q; delete this row "+
							"and re-file what it became\n%s", engine, r.gap, code, stderr, r.src)
					}
					continue
				}
				if code == 2 {
					t.Fatalf("%s: %s reached the assembler with a broken module (ADR 0166): %s\n%s", engine, r.gap, stderr, r.src)
				}
				if code != 0 {
					t.Fatalf("%s: %s refused (exit %d) where the row pins an exit-0 wrong answer: %s\n%s",
						engine, r.gap, code, stderr, r.src)
				}
				if stdout == want {
					t.Fatalf("%s closed %s: the program prints the reference's bytes — delete this row and move it "+
						"into voidQuestionFamily()\n%s", engine, r.gap, r.src)
				}
				if stdout != r.compiled {
					t.Fatalf("%s: %s changed shape: printed %q, this row pins %q (the reference prints %q)\n%s",
						engine, r.gap, stdout, r.compiled, want, r.src)
				}
			}
		})
	}
}
