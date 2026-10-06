package integration

// Gap R.172 / ADR 0289 at the CLI. `s.startswith(p)` and the seven other text predicates answer a verdict
// in the compiled path — and printed the WORD they hold, because a method call's callee is an attribute, not a
// name, and the print road's "is this a bool?" question gave up at its first line for anything that was not
// a plain name. So `print("abc".startswith("ab"))` exited 0 with `1` on the compiled path where Python prints
// `True`: the same wrong answer for the same reason ADR 0257 reported once for comparisons, still open for
// methods. Every row is cross-checked against a live `python3` before either engine runs.

import (
	"testing"
)

func TestTextPredicatesPrintAVerdictAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"startswith true", "print(\"abc\".startswith(\"ab\"))\n", "True\n"},
		{"startswith false", "print(\"abc\".startswith(\"z\"))\n", "False\n"},
		{"endswith true", "print(\"hello\".endswith(\"lo\"))\n", "True\n"},
		{"endswith false", "print(\"hello\".endswith(\"he\"))\n", "False\n"},
		{"isdigit true", "print(\"123\".isdigit())\n", "True\n"},
		{"isdigit false", "print(\"12a\".isdigit())\n", "False\n"},
		{"isalpha true", "print(\"abc\".isalpha())\n", "True\n"},
		{"isalnum true", "print(\"abc123\".isalnum())\n", "True\n"},
		{"isspace false", "print(\" a \".isspace())\n", "False\n"},
		{"islower true", "print(\"abc\".islower())\n", "True\n"},
		{"isupper true", "print(\"ABC\".isupper())\n", "True\n"},
		{"negated", "print(not \"abc\".isdigit())\n", "True\n"},
		{"conjoined", "print(\"abc\".startswith(\"ab\") and \"abc\".endswith(\"bc\"))\n", "True\n"},
		{"bound to a name", "flag = \"abc\".isdigit()\nprint(flag)\n", "False\n"},
		{"as a test", "if \"abc\".isalpha():\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "yes\n"},
		{"in a list", "print([\"abc\".isdigit(), \"12\".isdigit()])\n", "[False, True]\n"},
		// A value method must NOT be swept into the verdict road.
		{"upper is a text", "print(\"abc\".upper())\n", "ABC\n"},
		{"replace is a text", "print(\"a-b\".replace(\"-\", \"+\"))\n", "a+b\n"},
		{"count is a number", "print(\"banana\".count(\"a\"))\n", "3\n"},
		// A predicate still counts as the number it is (ADR 0259).
		{"verdict plus one", "print(\"1\".isdigit() + 1)\n", "2\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("row is stale: python3 prints %q, row pins %q", want, r.want)
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "pred", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 (ADR 0166's compiler-bug code): %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — a verdict printing its word is Gap R.172", engine, out, want)
				}
			}
		})
	}
}

// TestTheParamReceiverPredicateStillRefusesInWords pins the half the compiled leg declines. A receiver the
// compiler cannot see through — a parameter — has no compile-time text to fold, and the road says so. This
// is pre-existing behaviour, byte-identical on the pre-cycle binary; it is pinned so a verdict-rendering
// change cannot have quietly turned a refusal into a number, and so that it stays WORDS naming the method.
func TestTheParamReceiverPredicateStillRefusesInWords(t *testing.T) {
	dir := t.TempDir()
	src := "def check(s):\n    return s.startswith(\"x\")\n\n\nprint(check(\"xyz\"))\n"

	// The reference evaluates the receiver at run time and answers True. The compiled path owes the
	// same answer or a refusal that names the half it is missing (a *parameter* has no compile-time
	// text to fold — that is the L11.1/Gap I.2 debt); what it may not do is exit 0 with anything else.
	p := writeSrc(t, dir, "predanswer", src)
	if out, code := cliRunMerged(t, "--aot", "--file", p); code != 0 {
		if code == 1 && refusesHonestly(out) {
			noteCompiledGap(t, src, out)
		} else {
			t.Errorf("--aot exited %d with %q, want True or an honest refusal naming the missing half", code, out)
		}
	} else if out != "True\n" {
		t.Errorf("--aot printed %q, want True (the reference's answer)", out)
	}

	// The compiled leg folds a method over a constant it can see, and a PARAMETER has no compile-time
	// text to fold, so the road declines. That is the pre-existing half, owed to L11.1's tagged value
	// word; the contract here is that it stays a refusal IN WORDS and never becomes exit 2 or a number.
	p = writeSrc(t, dir, "predrefuse", src)
	out, code := cliRunMerged(t, "--aot", "--file", p)
	if code == 2 {
		t.Fatalf("--aot exited 2 (ADR 0166's compiler-bug code) rather than refusing: %s", out)
	}
	if code == 0 && out != "True\n" {
		t.Fatalf("--aot answered %q at exit 0 where the reference answers True — an answer must be the reference's, not the fold's word", out)
	}
	if code != 0 && out == "" {
		t.Errorf("--aot refused with no message (Gap R.38)")
	}
}
