package integration

// The CLI half of the printf-style `%` family (roadmap L11.2, Gap R.165, Gap R.31, ADR 0282, ADR 0360).
// This family used to be a wrong number: `print("%.2f" % 3.5)` printed `0.0` at **exit 0**, the answer the
// reference gives as `3.50`, because a text's `@str_tab` index was widened into the numeric road where the
// operand belongs (ADR 0282 pinned the refusal; ADR 0360 built the door).
//
// What ships now is the reference's own text: each row below is compiled, run through `cc`, and compared to
// CPython's stdout byte for byte, so a row that stops being a format — or starts being a number — fails here
// rather than shipping a near-answer. The refusal rows beside them are the half the door still declines to
// render, and they are exit 1 with the capability code attached; exit 2 is forbidden throughout, because a
// module `llc` rejects for an ordinary program is the compiler's bug and the contract's own signal (ADR 0166).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPercentFormatAnswersAtTheCLI is the family's positive half: the compiled link-and-run leg — not the
// in-process JIT — printing what CPython prints.
func TestPercentFormatAnswersAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"the row: %.2f over a double", "print(\"%.2f\" % 3.5)\n"},
		{"%.1f over a double", "print(\"%.1f\" % 3.14159)\n"},
		{"%f over an int", "print(\"%f\" % 3)\n"},
		{"%d with a text left", "print(\"%d items\" % 3)\n"},
		{"%s with a text left", "print(\"%s!\" % \"hi\")\n"},
		{"in a function's return", "def f(v):\n    return \"%.2f\" % v\n\nprint(f(3.5))\n"},
		{"bound to a name first", "s = \"%.2f\" % 3.5\nprint(s)\n"},
		{"widths, flags and bases", "print(\"%06d|%+d|%#x|%5.2f|\" % (42, 7, 255, 3.14159))\n"},
		{"the mapping form", "print(\"%(who)s has %(n)d\" % {\"who\": \"ada\", \"n\": 2})\n"},
		{"a text answer reaching a text method", "print((\"[%s]\" % \"hi\").upper())\n"},
		{"the numeric remainder beside it", "print(7 % 3)\nprint(\"%d of 2\" % 1)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "percent.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference was expected to answer this program\nsrc: %s", tc.src)
			}
			out, code := cliRunMerged(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2, the compiler's own bug (ADR 0166):\n%s", out)
			}
			if code != 0 {
				t.Fatalf("--aot exited %d where the reference answers %q:\n%s\nsrc: %s", code, py, out, tc.src)
			}
			if out != py {
				t.Fatalf("--aot printed %q, the reference prints %q\nsrc: %s", out, py, tc.src)
			}
		})
	}
}

// TestPercentRefusalAtTheCLI is the family's honest half: the conversions and arguments the door has no
// renderer for leave as a front-end refusal (exit 1) carrying the capability code, never as a number.
func TestPercentRefusalAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, refusal string }{
		{"a conversion with no renderer", "print(\"%b\" % 3)\n", "%b"},
		{"the conversion the reference removed", "print(\"%u\" % 3)\n", "%u"},
		{"an argument with no value", "print(\"%s %s\" % (\"a\", (\"b\", \"c\")))\n", "kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "percent.gy", tc.src)
			out, code := cliRunMerged(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 where the front end should refuse (ADR 0166):\n%s", out)
			}
			if code == 0 {
				t.Fatalf("--aot answered a format it has no renderer for; a near-answer at exit 0 is the bug ADR 0282 closed\nsrc: %s", tc.src)
			}
			if code != 1 {
				t.Fatalf("--aot: exit %d, want the front-end refusal's 1\noutput: %s\nsrc: %s", code, out, tc.src)
			}
			if !strings.Contains(out, "codegen") {
				t.Errorf("the refusal must name the pass that declined, got: %s", out)
			}
			if !strings.Contains(out, tc.refusal) {
				t.Errorf("the refusal must name %q, got: %s", tc.refusal, out)
			}
		})
	}
}

func TestRemainderAtTheCLIIsUntouchedByThePercentGuard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"int remainder", "print(7 % 3)\nprint(-7 % 3)\n", "1\n2\n"},
		{"float remainder", "print(7.5 % 2)\nprint(-7.5 % 2)\n", "1.5\n0.5\n"},
		{"both double", "print(7.5 % 2.5)\n", "0.0\n"},
		{"through a parameter, both kinds", "def f(v):\n    return v % 2\n\nprint(f(7.5))\nprint(f(7))\n", "1.5\n1\n"},
		{"float equality with a text still answers", "print(1 if 1.0 == \"a\" else 0)\nprint(1 if \"a\" == 1.0 else 0)\n", "0\n0\n"},
		{"a float still prints", "print(1.5 + 2)\nprint(7 / 2)\n", "3.5\n3.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "percent_control.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference was expected to answer this program\nsrc: %s", tc.src)
			}
			if py != tc.want {
				t.Fatalf("the pinned expectation is not the reference's: cpython %q, table %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", gy)
				if code != 0 {
					t.Fatalf("%s exited %d:\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want %q (cpython agrees: %q)", engine, out, tc.want, py)
				}
			}
		})
	}
}

// TestPercentArgumentResidueAtTheCLI pins the arguments the formatting door is never handed, ONE program per
// shape. That one-per-program is not tidiness: a front-end refusal ends a program before its first line runs,
// so a program containing a refusal witnesses only that refusal — which is why the conformance probe
// (`integration/programs/probe_percent_arguments_beyond_the_door.gy`) carries the first of these and the rest
// live here as their own sources. Each row states the reference's answer beside this backend's, because the
// point of the family is which leg is missing what, and the missing door behind each answer belongs to another
// roadmap row: the bounded integer (Gap R.176, ADR 0166), the float literal's written forms (L11.2), a tuple
// standing where a value goes (L11.3), and the exception class the shared table does not hold (the surrogate).
//
// Exit 2 is forbidden in every row: an ordinary program whose module `llc` rejects is the compiler's bug
// whatever the reason it was reached (ADR 0166).
func TestPercentArgumentResidueAtTheCLI(t *testing.T) {
	t.Parallel()

	refuse := func(t *testing.T, name, src string, wants ...string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "percent_residue.gy", src)
			out, code := cliRunMerged(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 where the front end should refuse in words (ADR 0166)\nsrc: %s\n%s", src, out)
			}
			if code == 0 {
				t.Fatalf("--aot answered an argument it has no pair for; a near-answer at exit 0 is the bug ADR 0282 closed\nsrc: %s\noutput: %s", src, out)
			}
			if code != 1 {
				t.Fatalf("--aot: exit %d, want the front-end refusal's 1\nsrc: %s\noutput: %s", code, src, out)
			}
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("the refusal must name %q, got: %s", want, out)
				}
			}
		})
	}

	// The reference's answer for a source it runs, compared byte for byte.
	answers := func(t *testing.T, dir, src, want string) {
		t.Helper()
		py, ok := cpythonPlainOut(t, dir, src)
		if !ok || py != want {
			t.Fatalf("the reference was expected to print %q for\n%s\ngot %q (ok=%v)", want, src, py, ok)
		}
	}

	// CPython prints the 22 digits; the door is handed no (value, kind) pair for an integer outside the
	// bounded-word row, so it refuses naming the operand, the missing word and the row that owns the fix.
	refuse(t, "an integer beyond the bounded word", "print(\"%d\" % (2 ** 70))\n",
		"codegen", "2 ** 70", "cannot build", "Gap R.31", "ADR 0360")
	t.Run("and the reference prints those digits beside the refusal", func(t *testing.T) {
		t.Parallel()
		answers(t, t.TempDir(), "print(\"%d\" % (2 ** 70))\n", "1180591620717411303424\n")
	})

	// The float literal's written forms are not in this grammar yet, so the program never reaches the door:
	// the parser is the leg that refuses, and it refuses with an exit code rather than a crash.
	refuse(t, "the float literal's exponent", "print(\"%s\" % (1e100,))\n", "parse error")
	t.Run("and the reference renders that literal beside the parse error", func(t *testing.T) {
		t.Parallel()
		answers(t, t.TempDir(), "print(\"%s\" % (1e100,))\n", "1e+100\n")
	})

	// A tuple is an argument list here, not a value, so a tuple standing where one conversion's value goes
	// has no kind for the door to answer with. The refusal says that; it does not print the handle.
	refuse(t, "a tuple where a value goes", "print(\"%s %s\" % (\"a\", (\"b\", \"c\")))\n",
		"codegen", "cannot name", "Gap R.31", "ADR 0360")
	t.Run("and the reference renders the nested tuple beside the refusal", func(t *testing.T) {
		t.Parallel()
		answers(t, t.TempDir(), "print(\"%s %s\" % (\"a\", (\"b\", \"c\")))\n", "a ('b', 'c')\n")
	})

	// The surrogate is the debt, not the refusal: the LITERAL is refused by the same gate that guards the
	// rows above, but a code point the program COMPUTED reaches the encoder, which has no class to raise —
	// `UnicodeEncodeError` is not in the shared table — so the compiled leg writes the bytes it refused to
	// write at exit 0 while the reference refuses the program. Pinned as it is, not as anyone wishes it were.
	t.Run("a surrogate the program computed writes the bytes the encoder refused", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := "n = 0xD800\nprint(\"%c\" % n)\n"
		gy := writeSrc(t, dir, "percent_surrogate.gy", src)
		out, code := cliRunMerged(t, "--aot", "--file", gy)
		if code == 2 {
			t.Fatalf("--aot: exit 2 for an ordinary program (ADR 0166)\nsrc: %s\n%s", src, out)
		}
		if code != 0 {
			t.Fatalf("--aot: exit %d where the compiled leg runs the shape and prints; want 0\nsrc: %s\noutput: %s", code, src, out)
		}
		if out != "\xed\xa0\x80\n" {
			t.Errorf("the compiled leg's answer for a computed surrogate is the three bytes it cannot legally encode plus a newline, got %q", out)
		}
		py := writeSrc(t, dir, "percent_surrogate.py", src)
		cmd := oracleCommand(t, py)
		var ref bytes.Buffer
		cmd.Stdout = &ref
		cmd.Stderr = &ref
		if err := cmd.Run(); err == nil {
			t.Fatalf("the reference was expected to refuse this program, it printed %q", ref.String())
		}
		if !strings.Contains(ref.String(), "UnicodeEncodeError") {
			t.Errorf("the reference raises UnicodeEncodeError for a surrogate, got: %s", ref.String())
		}
	})
}

// TestTheLanguageDocsPercentSectionAnswersWhatItSays turns `docs/language.md`'s printf-style section into a
// test. The section is the language's authority for this operator (AGENTS.md), and it carries two kinds of
// claim a reader will act on: a fenced block whose comment is the answer the line prints, and a table whose
// rows name the class and sentence a program raises. Prose claims of that kind are exactly what this loop
// keeps having to retract when they live only in prose, so each line of both is compiled, linked, run and
// compared — the positive rows against CPython as well, because a documented answer that only the compiled
// leg gives is a documented bug.
func TestTheLanguageDocsPercentSectionAnswersWhatItSays(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile(filepath.Join("..", "docs", "language.md"))
	if err != nil {
		t.Fatalf("the section this case judges is in docs/language.md: %v", err)
	}
	text := string(body)
	anchor := strings.Index(text, "Printf-style `%` on a text **formats**")
	if anchor < 0 {
		t.Fatal("docs/language.md no longer has the Printf-style section this case reads")
	}
	tail := text[anchor:]

	// --- the fenced block: `print(...)` + a `#` comment that states the answer -------------------------
	fence := strings.Index(tail, "```python")
	if fence < 0 {
		t.Fatal("no fenced example block under the Printf-style heading")
	}
	block := tail[fence+len("```python"):]
	block = block[:strings.Index(block, "```")]
	rows := 0
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, "  #")
		if i < 0 {
			t.Errorf("the documented example %q carries no answer comment; every line of this block is run and compared, so it has to state one", line)
			continue
		}
		src, want := strings.TrimRight(line[:i], " ")+"\n", strings.TrimRight(afterOneHashSpace(line[i+len("  #"):]), " ")
		rows++
		t.Run("docs answer for "+firstN(src, 40), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "docs_percent.gy", src)
			out, code := cliRunMerged(t, "--aot", "--file", gy)
			if code != 0 {
				t.Fatalf("the documented program exited %d, not 0\nsrc: %s\noutput: %s", code, src, out)
			}
			if out != want+"\n" {
				t.Errorf("docs/language.md says this line prints %q; the compiled link-and-run leg printed %q\nsrc: %s", want, out, src)
			}
			if py, ok := cpythonPlainOut(t, dir, src); !ok || py != want+"\n" {
				t.Errorf("the reference was expected to print %q too\nsrc: %s\ngot %q (ok=%v)", want+"\n", src, py, ok)
			}
		})
	}
	if rows < 8 {
		t.Fatalf("the documented block shrank to %d rows; it is the surface this section promises, do not thin it here", rows)
	}

	// --- the trap table: `program` | `Class: message` -------------------------------------------------
	trapHead := strings.Index(tail, "catchable by the `except` arm the program wrote:")
	if trapHead < 0 {
		t.Fatal("the trap table's introduction changed wording; the case reads the table by its own sentence")
	}
	traps := 0
	for _, line := range strings.Split(tail[trapHead:], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") {
			if traps > 0 && line == "" {
				break
			}
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 2 {
			t.Errorf("trap row is not program | raise: %q", line)
			continue
		}
		prog := strings.Trim(strings.TrimSpace(cells[0]), "`")
		raise := strings.Trim(strings.TrimSpace(cells[1]), "`")
		traps++
		t.Run("docs trap "+firstN(prog, 40), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := "print(" + prog + ")\n"
			gy := writeSrc(t, dir, "docs_trap.gy", src)
			out, code := cliRunMerged(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 for a documented trap (ADR 0166)\nsrc: %s\n%s", src, out)
			}
			if code == 0 {
				t.Fatalf("the documented trap answered instead of raising\nsrc: %s\noutput: %s", src, out)
			}
			if !strings.Contains(out, raise) {
				t.Errorf("docs/language.md says %s raises %q; the run said:\n%s", prog, raise, out)
			}
		})
	}
	if traps < 13 {
		t.Fatalf("the documented trap table shrank to %d rows; the raise table is half the deliverable", traps)
	}
	t.Logf("docs/language.md printf-style section: %d documented answers and %d documented traps, all run", rows, traps)
}

// afterOneHashSpace keeps the answer's own leading spaces: `%5d` pads with them, and a comment separator
// that ate them would document a different string than the line prints. One space belongs to the `#`.
func afterOneHashSpace(s string) string {
	if strings.HasPrefix(s, " ") {
		return s[1:]
	}
	return s
}

// firstN keeps a subtest name readable when the case's source is the documented line itself.
func firstN(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
