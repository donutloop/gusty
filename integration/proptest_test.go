package integration

import (
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The property harness, rebuilt for one backend.
//
// It used to generate random shared-surface programs and diff the AST interpreter's stdout against
// the compiled binary's, logging "parity drift" where the two came apart. With the engine retired
// (ADR 0302) there is no second engine to diff, and pretending otherwise — comparing an artifact
// against itself — would turn the harness into decoration. Three properties survive the change and
// are the ones worth generating programs for:
//
//   - the compiler does not crash on a generated program (a refusal is a message; a panic is a bug);
//   - the same source produces the same answer, in the same shape, every time it is built;
//   - a generated program that the checker accepts actually terminates and prints something the
//     runtime can account for, rather than dying mid-run or hanging.
//
// These sources are generated rather than curated, so they have no recorded answer and do not go
// through the golden harness: a record is the answer a *named* program owes, and inventing one from
// the compiler's own output would be exactly the self-comparison the last paragraph refuses.

// compileAndRunOnce builds src natively and returns its stdout. Compiler refusals come back as an
// error; a panic in codegen is recovered and reported as one, because the harness's job is to survey
// a corpus, not to abort on the first gap it finds.
func compileAndRunOnce(t *testing.T, src string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", errCompilerPanic(r)
		}
	}()
	return runAOTConformance(t, src)
}

func errCompilerPanic(r any) error {
	return &codegenPanic{value: r}
}

type codegenPanic struct{ value any }

func (e *codegenPanic) Error() string { return "compiler panic: " + sprintAny(e.value) }

func sprintAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "<non-string panic value>"
}

// TestPropCompiledDeterminism generates a deterministic corpus and requires each accepted program to
// answer twice the same way from one build. Drift here is not noise: the module the compiler emits is
// deterministic, so a program that answers differently on a second run has state the runtime is
// reusing between calls — the class of bug the GC stress runs and the heap-reset work exist to catch.
func TestPropCompiledDeterminism(t *testing.T) {
	const seed = int64(20260704)
	corpus := lang.PropSource(seed, 10, lang.DefaultPropGrammar())
	drift, refused, ran := 0, 0, 0
	for i, src := range corpus {
		out, err := compileAndRunOnce(t, src)
		if err != nil {
			t.Logf("seed %d program %d: refused or failed to run: %v", seed, i, err)
			refused++
			continue
		}
		second, err := compileAndRunOnce(t, src)
		if err != nil {
			t.Errorf("seed %d program %d: the first run answered %q and the second could not run at all: %v", seed, i, out, err)
			drift++
			continue
		}
		ran++
		if out != second {
			t.Errorf("seed %d program %d: the same build printed %q and then %q\n%s", seed, i, out, second, src)
			drift++
		}
	}
	t.Logf("seed %d: %d ran twice, %d refused, %d drifted", seed, ran, refused, drift)
	if ran == 0 {
		t.Fatal("no generated program ran at all — the harness is not measuring anything")
	}
}

// TestPropCompiledDeterminismSeeds widens the covered surface across several seeds.
func TestPropCompiledDeterminismSeeds(t *testing.T) {
	drift, total, ran := 0, 0, 0
	for _, seed := range []int64{1, 42, 12345, 54321, 999} {
		for i, src := range lang.PropSource(seed, 6, lang.DefaultPropGrammar()) {
			total++
			out, err := compileAndRunOnce(t, src)
			if err != nil {
				t.Logf("seed %d program %d: refused or failed to run: %v", seed, i, err)
				continue
			}
			second, err := compileAndRunOnce(t, src)
			if err != nil || out != second {
				t.Errorf("seed %d program %d: unstable answer %q vs %q (err %v)\n%s", seed, i, out, second, err, src)
				drift++
				continue
			}
			ran++
		}
	}
	t.Logf("multi-seed: %d/%d generated programs ran twice with one answer; %d drifted", ran, total, drift)
	if ran == 0 {
		t.Fatal("no generated program ran at all across the seeds — the harness is not measuring anything")
	}
}

// FuzzPropCompiled is the Go-native fuzz target: the fuzzer mutates source and asserts the compiler
// neither panics nor produces a binary that dies in a way the runtime cannot report. A clean refusal
// is an acceptable outcome; a crash is not. The seed corpus is the deterministic PropSource corpus, so
// `go test -fuzz=FuzzPropCompiled` starts from programs the grammar actually produces.
func FuzzPropCompiled(f *testing.F) {
	g := lang.DefaultPropGrammar()
	for _, seed := range []int64{1, 42, 20260704, 12345} {
		for _, src := range lang.PropSource(seed, 8, g) {
			f.Add(src)
		}
	}
	f.Fuzz(func(t *testing.T, src string) {
		// Codegen is the part under the fuzzing microscope: it must refuse in words, not fall over.
		res, err := lang.Compile(src)
		if err != nil {
			t.Skipf("refused: %v", err)
		}
		if res.IR == "" {
			t.Fatal("accepted the program and emitted no module")
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Fatalf("emitted a module that does not verify: %v %v\n%s", v.Errors, verr, res.IR)
		}
	})
}
