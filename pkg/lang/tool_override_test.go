package lang

// The toolchain is a value a run carries, not a fact the process agrees on (ADR 0343, ToolOverride).
//
// Three package variables — `llcCmd`, `ccCmd`, `optCmd` — used to be the only way to name the binaries a
// compile runs, and the only way to point one build at a different `llc` was to write them. That is a race
// while two compiles overlap, and a last-writer-wins answer when they do not: a toolchain-error case that
// installed a stub was read by an unrelated program mid-compile, which is how `import math` came to report
// "invalid module: my favourite complaint" as its own error. So the questions this file asks are about
// *whose* toolchain a run is using, and the sharpest of them is two runs asking at the same time.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestAnOverrideReplacesOnlyTheToolItNames is the resolution rule. An empty field means "the default", which
// is what lets a case replace the one tool its question is about; a nil override is the whole set of defaults;
// and a name is taken literally, because a tool that does not exist is a fact about the run, not a typo to
// recover from.
func TestAnOverrideReplacesOnlyTheToolItNames(t *testing.T) {
	t.Parallel()
	base := defaultToolNames()
	if base.llc != llcCmd || base.cc != ccCmd || base.opt != optCmd || base.dwarfdump != dwarfdumpCmd {
		t.Fatalf("the default toolchain does not name the pinned tools: %+v", base)
	}
	if got := toolNamesFor(nil); got != base {
		t.Errorf("a run with no override resolved to %+v, want the defaults %+v", got, base)
	}
	one := toolNamesFor(&ToolOverride{LLC: "/tmp/stub-llc"})
	if one.llc != "/tmp/stub-llc" {
		t.Errorf("llc = %q, want the override", one.llc)
	}
	if one.cc != base.cc || one.opt != base.opt || one.dwarfdump != base.dwarfdump {
		t.Errorf("naming one tool moved the others: %+v vs %+v", one, base)
	}
	if got := toolNamesFor(&ToolOverride{CC: ""}); got.cc != base.cc {
		t.Errorf("an empty name replaced the default with nothing (%q) — absence is toolNamesWithoutOpt's job, not a blank field's", got.cc)
	}
	if got := toolNamesWithoutOpt(); got.opt != "" || got.llc != base.llc || got.cc != base.cc {
		t.Errorf("the no-optimizer toolchain is %+v, want the defaults with only opt taken away", got)
	}
}

// TestARunUnderAFailingStubDoesNotTakeTheNextRunDownWithIt is the case the old design could not pass. Two
// runs in one process: one names an `llc` that always fails, the other names nothing and expects the real
// toolchain. Overwriting the package switch to arrange the first situation put the second one inside it too —
// which is how a program unrelated to any stub came to report the stub's own sentence as its compile error,
// and how a golden-record run came to say a real program "could not be run".
//
// It is asked concurrently on purpose: the race is the visible half of the defect, and `-race` is what makes
// this a witness rather than a hope.
func TestARunUnderAFailingStubDoesNotTakeTheNextRunDownWithIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stub := writeStubLLC(t, dir, "invalid module: the stub is talking")

	const rounds = 6
	var wg sync.WaitGroup
	problems := make(chan string, rounds*2)
	for r := 0; r < rounds; r++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := JITWithOptions("print(1 + 1)\n", 0, &JITOptions{Tools: &ToolOverride{LLC: stub}})
			if err == nil {
				problems <- "the run under the stub llc succeeded"
				return
			}
			var rejection *ToolchainRejectionError
			if !errors.As(err, &rejection) {
				problems <- fmt.Sprintf("the stub's refusal was not classified as a rejection: %T %v", err, err)
				return
			}
			if !strings.Contains(rejection.Output, "the stub is talking") {
				problems <- fmt.Sprintf("the stub's own words did not reach the report: %q", rejection.Output)
			}
		}()
		go func() {
			defer wg.Done()
			res, err := JIT("print(1 + 1)\n", 0)
			if err != nil {
				problems <- fmt.Sprintf("the run on the real toolchain was answered by somebody else's llc: %v", err)
				return
			}
			if res.Output != "2\n" {
				problems <- fmt.Sprintf("the run on the real toolchain printed %q, want 2", res.Output)
			}
		}()
	}
	wg.Wait()
	close(problems)
	for p := range problems {
		t.Error(p)
	}
}

// TestTheToolchainNamedByOneRunIsNotTheOneAnotherRunReads asks the same fact of the link step, where the
// consequence of a shared name is a program linked by a toolchain its caller never asked for. The stub `cc`
// appends a line to one shared log before handing the work to the real compiler, and the run that did NOT ask
// for it runs beside it: one line in the log means the override reached the run that named it, and two means
// it reached the other one too — which is the shape the package variable had.
func TestTheToolchainNamedByOneRunIsNotTheOneAnotherRunReads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "cc-was-here.log")
	stub := filepath.Join(dir, "stub-cc")
	body := "#!/bin/sh\nprintf 'used\\n' >> '" + log + "'\nexec cc \"$@\"\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub cc: %v", err)
	}

	var wg sync.WaitGroup
	problems := make(chan string, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := JITWithOptions("print(2 * 3)\n", 0, &JITOptions{Tools: &ToolOverride{CC: stub}}); err != nil {
			problems <- fmt.Sprintf("the run under the stub cc failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		res, err := JIT("print(4 + 4)\n", 0)
		if err != nil {
			problems <- fmt.Sprintf("the run on the default toolchain failed: %v", err)
			return
		}
		if res.Output != "8\n" {
			problems <- fmt.Sprintf("the default run printed %q, want 8", res.Output)
		}
	}()
	wg.Wait()
	close(problems)
	for p := range problems {
		t.Error(p)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			t.Error("the run that named the stub cc never ran it — the override did not reach the link step")
		} else {
			t.Fatalf("read the stub log: %v", err)
		}
		return
	}
	if n := strings.Count(string(b), "used"); n != 1 {
		t.Errorf("the stub cc ran %d times for a run that named it once — the other run was linked through it too", n)
	}
}

// TestTheBuildCarriesItsOwnToolchain is the same rule at the `--build` entry, which is the path a person and a
// script take for an artifact: a build that names a `cc` that cannot link must fail its OWN build and leave the
// build beside it producing a binary.
func TestTheBuildCarriesItsOwnToolchain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "p.gy")
	if err := os.WriteFile(src, []byte("print(sorted([3, 1, 2]))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := BuildWithOptions([]string{src}, filepath.Join(dir, "broken.bin"), 0, &BuildOptions{Tools: &ToolOverride{CC: "gusty-no-such-cc"}})
	if err == nil {
		t.Error("the build under a cc that does not exist succeeded")
	}
	if _, err := BuildWithOptions([]string{src}, filepath.Join(dir, "fine.bin"), 0, nil); err != nil {
		t.Errorf("the build beside it was answered by the other build's cc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "fine.bin")); err != nil {
		t.Errorf("the build on the default toolchain produced no artifact: %v", err)
	}
}
