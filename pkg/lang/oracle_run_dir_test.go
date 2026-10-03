package lang

// The CPython leg's own diagnostics are quoted into a committed artifact. That is fine for the
// warning text and the line number, and not fine for the absolute path of a temporary directory:
// `/tmp/gusty-oracle3710782730/prog.py` is a path that exists during exactly one run of the
// suite, so quoting it verbatim means the artifact rewrites itself on every regeneration, and a
// diff that means nothing is how a diff that means something gets waved through. Roadmap L11.9
// owns the matrix artifact (ADR 0175); this is the half that keeps it diffable. ADR 0261.

import (
	"strings"
	"testing"
)

func TestTheOracleRunDirectoryIsScrubbedFromWhatWeQuote(t *testing.T) {
	const dir = "/tmp/gusty-oracle3710782730"
	in := "/tmp/gusty-oracle3710782730/prog.py:4: SyntaxWarning: 'set' object is not subscriptable\n" +
		"  File \"" + dir + "/prog.py\", line 5, in <module>\n" +
		"TypeError: 'int' object is not iterable\n"
	got := scrubOracleRunDir(in, dir)
	if strings.Contains(got, "gusty-oracle") {
		t.Errorf("scrubbed stderr still names the run directory: %q", got)
	}
	// The facts the row exists to record survive the scrub: file, line, error class.
	for _, want := range []string{"prog.py:4: SyntaxWarning", "File \"prog.py\", line 5", "TypeError"} {
		if !strings.Contains(got, want) {
			t.Errorf("scrub lost %q; got:\n%s", want, got)
		}
	}
	// A path the program itself names is its business, and stays.
	mine := scrubOracleRunDir("OSError: [Errno 2] /home/dev/data/prog.py: no such file", dir)
	if !strings.Contains(mine, "/home/dev/data/prog.py") {
		t.Errorf("scrub rewrote a path the program named itself: %q", mine)
	}
}

func TestTheOracleInterpretsItsOwnDiagnosisWithoutANominalRunDir(t *testing.T) {
	// A program that dies on line 1: the reference's traceback names the script, and the only
	// thing the matrix may record about it is `prog.py`.
	_, stderr, err := PythonRun("x = 1\nprint(x[0])\n")
	if err == nil {
		t.Fatal("the program was meant to fail")
	}
	if strings.Contains(stderr, "gusty-oracle") {
		t.Errorf("PythonRun leaked its run directory into the recorded stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "prog.py") {
		t.Errorf("the scrub should leave the script's own name; got %q", stderr)
	}
}
