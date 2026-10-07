package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// `for c in "ab":` was a compiler bug, not a language question: the unrolled loop stored each character
// with the scalar value path, producing `store i32 @.str1, i32* %_c` — a global in an i32 slot — which
// llc rejects. Under the exit-code contract an LLVM rejection is the tool's fault, never the source's
// (roadmap Gap R.15, ADR 0208).

func TestStringIterationAgreesOnEveryPath(t *testing.T) {
	src := readProgramSrc("for_string_chars")
	want := "a\nb\nc\nx\ny\n1\na\n2\n< h >\n< i >\nab\ncd\n"

	lang.RecordedStdoutIs(t, src, want)
	built, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
	pyOut, pyErr, perr := lang.PythonRun(src)
	if perr != nil {
		t.Skipf("no usable oracle: %v\n%s", perr, pyErr)
	}
	if pyOut != want {
		t.Errorf("CPython output =\n%q\nwant\n%q", pyOut, want)
	}
}

// TestStringIterationModuleVerifies is the assertion that actually failed before the fix: the emitted
// module has to be one llc accepts, not merely one the emitter believed in.
func TestStringIterationModuleVerifies(t *testing.T) {
	for _, src := range []string{
		"for c in \"ab\":\n    print(c)\n",
		"for w in [\"x\", \"y\"]:\n    print(w)\n",
		"for m in [1, \"a\", 2]:\n    print(m)\n",
		"for c in \"hi\":\n    print(\"<\", c, \">\")\n",
	} {
		name := strings.SplitN(src, "\n", 2)[0]
		res, err := lang.Compile(src)
		if err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		if strings.Contains(res.IR, "store i32 @.str") {
			t.Errorf("%s emitted a global stored into an i32 slot:\n%s", name, res.IR)
		}
		ver, verr := lang.VerifyModuleIR(res.IR, 0)
		if verr != nil {
			t.Fatalf("verify %s: %v", name, verr)
		}
		if ver.Skipped {
			t.Skipf("no LLVM verifier available: %s", ver.Note)
		}
		if !ver.OK {
			t.Errorf("%s: LLVM rejected the module — a compiler bug under the exit-code contract:\n%s", name, strings.Join(ver.Errors, "\n"))
		}
	}
}
