package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// mustParseForCheck parses source for a checker-only assertion.
func mustParseForCheck(t *testing.T, src string) *lang.Program {
	t.Helper()
	prog, err := lang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

// TestForwardReferencedProgramCompilesAndRuns is the regression for Gap R.6: a program
// whose functions call each other, or call helpers declared below them, is refused by no
// part of the toolchain and runs the same way on both backends.
//
// Before the fix the interpreter ran it and the checker refused it: `undefined name
// "is_odd"`, which — because the compiled path refuses to emit IR for a program the front
// end rejected — meant the canonical Python shape could not be compiled at all.
func TestForwardReferencedProgramCompilesAndRuns(t *testing.T) {
	res, err := lang.Compile(`def is_even(n):
    if n == 0:
        return True
    return is_odd(n - 1)

def is_odd(n):
    if n == 0:
        return False
    return is_even(n - 1)

print(1 if is_even(4) else 0)
print(1 if is_odd(3) else 0)
`)
	if err != nil {
		for _, d := range res.Diagnostics {
			t.Logf("diag: %v", d)
		}
		t.Fatalf("a mutually recursive pair must compile, got %v", err)
	}
	for _, want := range []string{"define i32 @gy_is_even(", "define i32 @gy_is_odd(", "call i32 @gy_is_odd("} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("emitted module missing %q:\n%s", want, res.IR)
		}
	}
	out, err := runAOTWithTimeout(t, `def is_even(n):
    if n == 0:
        return True
    return is_odd(n - 1)

def is_odd(n):
    if n == 0:
        return False
    return is_even(n - 1)

print(1 if is_even(4) else 0)
print(1 if is_odd(3) else 0)
`, 60*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if got := strings.TrimSpace(out); got != "1\n1" {
		t.Errorf("compiled output = %q, want %q", got, "1\n1")
	}

	// The interpreted leg of the same program: the fix is in the shared front end, so
	// the path that always ran this must keep running it identically.
	interp := strings.TrimSpace(runInterp(t, `def is_even(n):
    if n == 0:
        return True
    return is_odd(n - 1)

def is_odd(n):
    if n == 0:
        return False
    return is_even(n - 1)

print(1 if is_even(4) else 0)
print(1 if is_odd(3) else 0)
`))
	if interp != "1\n1" {
		t.Errorf("interpreted output = %q, want %q", interp, "1\n1")
	}
}

// TestForwardReferenceIsNotARefusal pins the checker verdict itself: no diagnostic at all
// for the shapes the language allows, and the errors that belong to it still reported.
func TestForwardReferenceIsNotARefusal(t *testing.T) {
	mustBeClean := map[string]string{
		"mutual recursion": `def a(n):
    return b(n)

def b(n):
    return n * 2

print(a(3))
`,
		"helper below its caller": `def use():
    return helper() + 1

def helper():
    return 41

print(use())
`,
		"sibling nested defs": `def outer(n):
    def a(m):
        return b(m)

    def b(m):
        return m * 2

    return a(n)

print(outer(3))
`,
	}
	for name, src := range mustBeClean {
		for _, d := range lang.Analyze(mustParseForCheck(t, src)) {
			// Warnings about arithmetic on unannotated parameters are a separate, known
			// rough edge (they fire on the base compiler too); what this test pins is
			// that declaration order no longer produces a refusal.
			if d.Level == lang.LevelError || strings.Contains(d.Msg, "undefined name") {
				t.Errorf("%s: declaration order was refused: %v", name, d)
			}
		}
	}

	mustStillReport := map[string]string{
		"a call at module level to a def below it": `print(later())

def later():
    return 1
`,
		"a decorator naming a def below it": `@identity
def target():
    return 3

def identity(f):
    return f

print(target())
`,
		"a genuinely missing name": `print(nowhere())
`,
		"a variable read above its assignment": `print(total)

total = 3
`,
	}
	for name, src := range mustStillReport {
		found := false
		for _, d := range lang.Analyze(mustParseForCheck(t, src)) {
			if strings.Contains(d.Msg, "undefined name") {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: this one must stay an `undefined name` error, got no such diagnostic", name)
		}
	}
}
