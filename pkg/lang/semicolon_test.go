package lang

import (
	"strings"
	"testing"
)

// semicolon_test.go — `;` is a statement separator, not a diagnostic (roadmap Gap R.72, ADR 0242).
//
// The lexer had no case for `;`, so it fell through to the operator scan and came back as
// `unexpected character ";"` — a TokError that `filterLex` turned into an error diagnostic. The parser,
// which never looks at TokError, carried on parsing both statements anyway. The result was one source
// line with three verdicts: `--interp` printed CPython's answer, `--jit`/`--aot` refused the program
// because JITWithOptions fails on any error diagnostic, and `--emit-llvm` emitted and verified a module
// that ran. The same program, three engines, three stories — which is what an agent comparing backends
// reads as a backend bug and burns a round on.
//
// The answer (ADR 0242): `;` is the on-line spelling of the newline that would otherwise separate the
// statements, exactly as CPython defines it. It is a token the statement loops step over, it belongs to
// the inline suite that follows a `:` (`for i in xs: f(i); g(i)` runs both per iteration), and the empty
// statement `x = 1;;y = 2` is a parse error wherever a syntax rule lives — not a diagnostic that one
// entry point enforces and another ignores.

func TestSemicolonSeparatesStatementsOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"two_statements", "x = 5; print(x + 1)\n", "6\n"},
		{"three_statements", "a = 1; b = 2; print(a + b)\n", "3\n"},
		{"trailing_semicolon", "print(\"done\");\n", "done\n"},
		{"trailing_semicolon_and_comment", "a = 1; # comment\nprint(a);\n", "1\n"},
		{"calls_separated", "print(1); print(2)\n", "1\n2\n"},
		{"assignment_then_call", "q = 3; print(q * 7)\n", "21\n"},
		{"inline_if_body_keeps_both", "x = 1\nif x: print(\"in\"); print(\"body\")\nprint(\"after\")\n", "in\nbody\nafter\n"},
		{"inline_for_body_runs_per_iteration", "for i in [1, 2]:\n    print(i); print(\"step\")\n", "1\nstep\n2\nstep\n"},
		{"inline_def_body", "def f(n): return n * 2; return 0\nprint(f(3))\n", "6\n"},
		{"inline_while_body", "n = 0\nwhile n < 2: n = n + 1; print(n)\n", "1\n2\n"},
		{"indented_body_with_semicolon", "xs = [1, 2]\nfor x in xs:\n    y = x * 10; print(y)\n", "10\n20\n"},
		{"separator_then_blank_line", "a = 1;\n\nprint(a)\n", "1\n"},
		{"semicolon_does_not_end_the_block", "if True:\n    print(1); print(2)\nprint(3)\n", "1\n2\n3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%q refused: %v", tc.src, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("AOT ran %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want CPython's %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// TestSemicolonIsNotADiagnostic is the whole point of the change: a separator that records a diagnostic
// is a program that some entry points run and others refuse. Any error-level diagnostic here would put
// `--jit`/`--aot` back on the refusal side while `--interp` kept answering.
func TestSemicolonIsNotADiagnostic(t *testing.T) {
	for _, src := range []string{
		"x = 5; print(x + 1)\n",
		"a = 1; b = 2; print(a + b)\n",
		"print(\"done\");\n",
		"if True:\n    print(1); print(2)\n",
	} {
		prog, err := parseProgram(src)
		if err != nil {
			t.Fatalf("%q: parse: %v", src, err)
		}
		for _, d := range prog.Diags {
			if d.Level == LevelError {
				t.Errorf("%q recorded %q at %d:%d; a separator must record nothing", src, d.Msg, d.Span.Line, d.Span.Col)
			}
		}
		if _, _, err := evalGolden(t, src); err != nil {
			t.Errorf("%q: interpreter: %v", src, err)
		}
	}
}

// TestEmptyStatementIsAParseError pins the rule CPython shares: two separators with nothing between
// them are not two statements. It is a parse error rather than a lexer diagnostic so that every entry
// point — interpreter, JIT, AOT, --emit-llvm, the REPL — gives the same verdict.
func TestEmptyStatementIsAParseError(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"double_separator", "a = 1;;b = 2\n", "empty statement"},
		{"double_separator_with_spaces", "a = 1; ; b = 2\n", "empty statement"},
		{"double_separator_in_inline_suite", "x = 1\nif x: pass;;\n", "empty statement"},
		{"separator_where_a_statement_is_required", "a = 1;\n;b = 2\n", "empty statement"},
		{"separator_inside_brackets", "a = (1; 2)\n", "expected \")\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseProgram(tc.src)
			if err == nil {
				t.Fatalf("%q parsed; want a parse error", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%q refused with %q, want it to mention %q", tc.src, err.Error(), tc.want)
			}
			// The interpreter reads the same verdict: a program the parser rejects does not
			// run half of.
			if _, _, ierr := evalGolden(t, tc.src); ierr == nil {
				t.Errorf("%q: interpreter accepted a program the parser rejected", tc.src)
			}
		})
	}
}

// TestSemicolonDoesNotDisturbIndentation is the regression this change could have caused for free: the
// ';' is emitted mid-line, so the indent tracker must still see one INDENT/DEDENT pair per physical
// line and must not treat the separator as the start of a block.
func TestSemicolonDoesNotDisturbIndentation(t *testing.T) {
	src := "def f():\n    a = 1; b = 2\n    return a + b\n\nprint(f())\nif True:\n    x = 1; print(x)\nprint(0)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if out := runIR(t, res.IR); out != "3\n1\n0\n" {
		t.Errorf("AOT ran %q, want %q", out, "3\n1\n0\n")
	}
	if out := captureStdout(t, src); out != "3\n1\n0\n" {
		t.Errorf("interpreter printed %q, want %q", out, "3\n1\n0\n")
	}
}
