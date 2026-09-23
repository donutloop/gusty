package lang

import (
	"testing"
)

// TestPropSourceReproducible is the core determinism property: the same seed
// always yields the same corpus, so a failing seed is reproducible.
func TestPropSourceReproducible(t *testing.T) {
	g := DefaultPropGrammar()
	a := PropSource(12345, 20, g)
	b := PropSource(12345, 20, g)
	if len(a) != len(b) {
		t.Fatalf("corpus lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("seed 12345 program %d differs between runs:\nA:\n%s\nB:\n%s", i, a[i], b[i])
		}
	}
	// Different seeds should diverge (guards against accidental constant seed).
	c := PropSource(54321, 20, g)
	same := 0
	for i := range a {
		if a[i] == c[i] {
			same++
		}
	}
	if same == len(a) {
		t.Fatalf("two different seeds produced identical corpora")
	}
}

// TestPropSourceParses checks every generated source parses cleanly (so the
// corpus is runnable through both backends). It deliberately does not assert
// formatter idempotence (Format(Parse(src)) == src) — the formatter has known
// parenthesization quirks for unary-minus and tuple-assignment operands that
// are out of scope for the parity property this package guards.
func TestPropSourceParses(t *testing.T) {
	g := DefaultPropGrammar()
	srcs := PropSource(20260701, 40, g)
	for i, src := range srcs {
		if _, err := Parse(src); err != nil {
			t.Fatalf("seed corpus program %d failed to parse: %v\nsource:\n%s", i, err, src)
		}
	}
}

// TestPropInterpreterValid checks the generator emits only well-formed programs
// the shared surface accepts: every generated program must run cleanly in the
// interpreter (no undefined names, no runtime errors), so the parity test has
// observable, comparable output.
func TestPropInterpreterValid(t *testing.T) {
	g := DefaultPropGrammar()
	for _, seed := range []int64{1, 42, 20260702, 12345, 54321, 999} {
		srcs := PropSource(seed, 40, g)
		for i, src := range srcs {
			if _, err := InterpreterRun(src); err != nil {
				t.Fatalf("seed %d corpus program %d rejected: %v\nsource:\n%s", seed, i, err, src)
			}
		}
	}
}

// TestPropInterpreterDeterministic checks the interpreter determinism property:
// running the same generated program twice must produce byte-identical stdout.
func TestPropInterpreterDeterministic(t *testing.T) {
	g := DefaultPropGrammar()
	srcs := PropSource(20260702, 40, g)
	for i, src := range srcs {
		out1, err1 := InterpreterRun(src)
		out2, err2 := InterpreterRun(src)
		if err1 != nil || err2 != nil {
			t.Fatalf("seed corpus program %d errored: %v / %v\n%s", i, err1, err2, src)
		}
		if out1 != out2 {
			t.Fatalf("seed corpus program %d is non-deterministic:\n%s\nrun1=%q\nrun2=%q", i, src, out1, out2)
		}
	}
}
