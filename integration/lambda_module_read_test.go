package integration

// integration/lambda_module_read_test.go — an applied `lambda` that reads a module-level literal answers
// the VALUE, at the CLI, against a live reference (roadmap **Gap R.331**, **ADR 0461**).
//
// The program that filed the row is two lines:
//
//	t = "hey"
//	print((lambda: t)())      →  `0`   reference: `hey`
//
// and the digit is the intern table's index for `hey`: the module road carries an interned text through an
// `i32` correctly (**ADR 0227**), and the print door downstream of the call was never told the word it held
// was an index, because **ADR 0368**'s answer direction names a kind only where the source spells one and
// `return t` spells nothing. The same body written as a `def` answered `hey` all along.
//
// The fix is a FOLD, not a second answer road: an applied `lambda` whose whole body reads a name the module
// bound once, to a literal, and never rebound, has a body that IS that literal — and every door that
// answers a spelled kind then answers this program too, including the one that requests the runtime block
// **Gap R.333**'s program was calling `@rt_str_intern2` without.
//
// Every expectation below is what a live `python3` printed for the same source inside the case, never what
// this file remembers (**Gap N**'s standing rule), and exit 2 — **ADR 0166**'s code for a compiler that
// broke — fails every row here, including the owed ones.

import (
	"strings"
	"testing"
)

// lambdaModuleReadSources is the answered family at this altitude.
func lambdaModuleReadSources() []string {
	return []string{
		"t = \"hey\"\nprint((lambda: t)())\n",
		"t = \"a\\tb\"\nprint((lambda: t)())\n",
		"t = \"\"\nprint((lambda: t)())\n",
		"b = True\nprint((lambda: b)())\n",
		"b = False\nprint((lambda: b)())\n",
		"z = None\nprint((lambda: z)())\n",
		"n = 7\nprint((lambda: n)())\n",
		"n = -3\nprint((lambda: n)())\n",
		"t = \"hey\"\nprint((lambda x: t)(9))\n",
		"t = \"hey\"\nx = (lambda: t)()\nprint(x)\n",
		"t = \"hey\"\nx = (lambda: t)()\nprint(x == \"hey\", len(x), x.upper())\n",
		"t = \"hey\"\nprint(f\"[{(lambda: t)()}]\")\n",
		"t = \"hey\"\nfor i in range(2):\n    print((lambda: t)())\n",
		"t = \"hey\"\nprint([(lambda: t)(), 2])\n",
		"t = \"hey\"\nprint({\"k\": (lambda: t)()})\n",
		"t = \"hey\"\nif (lambda: t)() == \"hey\":\n    print(\"yes\")\n\nelse:\n    print(\"no\")\n",
		"t = \"hey\"\nprint(str((lambda: t)()))\n",
		"t = \"None\"\nprint((lambda: t)(), None)\n",
		"a = \"x\"\nb = True\nprint((lambda: a)(), (lambda: b)())\n",
		// the parameter gate, at this altitude too: the module binds `n` once and the body's own parameter owns
		// the name, so the argument answers and the fold does not fire.
		"n = 7\nprint((lambda n: n)(3))\n",
		// the controls: a body that already spells its kind, and the same read written as a `def`
		"print((lambda: \"hey\")())\n",
		"t = \"hey\"\ndef f():\n    return t\n\nprint(f())\n",
		"t = \"hey\"\ndef f():\n    return t\n\nprint(f(), (lambda: t)())\n",
	}
}

func TestTheFoldedModuleReadAnswersWhatCPythonAnswersAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range lambdaModuleReadSources() {
		src := src
		t.Run(strings.ReplaceAll(strings.SplitN(src, "\n", 2)[0], " ", "_"), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src)
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s\n%s", stderr, stdout)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if strings.TrimRight(stdout, "\n") != strings.TrimRight(want, "\n") {
				t.Fatalf("printed %q, the reference prints %q", stdout, want)
			}
		})
	}
}

// TestTheFoldedReadModuleDefinesWhatItCalls is the leg **Gap R.333** died on: the program reached the screen
// with `0` while the same source, compiled through the other leg, handed `llc-20` a call to
// `@rt_str_intern2` that no `define` or `declare` in the module answered for. A module that calls a runtime
// entry it never defined is **ADR 0166**'s exit 2, and a module this row answers must simply not do it.
func TestTheFoldedReadModuleDefinesWhatItCalls(t *testing.T) {
	t.Parallel()
	for _, src := range lambdaModuleReadSources() {
		src := src
		t.Run(strings.ReplaceAll(strings.SplitN(src, "\n", 2)[0], " ", "_"), func(t *testing.T) {
			t.Parallel()
			ir, stderr, code := cliStreams(t, "--emit-llvm", src)
			if code != 0 {
				t.Fatalf("--emit-llvm failed (%d): %s", code, stderr)
			}
			for _, line := range strings.Split(ir, "\n") {
				call, ok := runtimeCallOf(line)
				if !ok {
					continue
				}
				if definesRuntimeEntry(ir, call) {
					continue
				}
				t.Fatalf("the module calls @%s with no definition anywhere — `llc`'s `use of undefined value`, and the"+
					" program exited 0 with a wrong word instead (Gap R.333):\n%s\n%s", call, line, ir)
			}
			out, vstderr, vcode := cliStreams(t, "--verify-llvm", src)
			if vcode != 0 {
				t.Fatalf("the module verifier rejected a program this row answers (%d): %s\n%s", vcode, vstderr, out)
			}
		})
	}
}

// runtimeCallOf answers which `@rt_…` a line calls, if it calls one.
func runtimeCallOf(line string) (string, bool) {
	i := strings.Index(line, " @rt_")
	if i < 0 || !strings.Contains(line[:i], "call") {
		return "", false
	}
	rest := strings.TrimSpace(line[i+1:])
	if j := strings.IndexAny(rest, "( \t"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimPrefix(rest, "@"), true
}

// definesRuntimeEntry looks for the definition of one runtime helper in any of the shapes the runtime block
// writes it (`define internal i32 @rt_x(`, `define internal void @rt_x(`, `define i32 @rt_x(`).
func definesRuntimeEntry(ir, name string) bool {
	hay := "@" + name + "("
	for _, line := range strings.Split(ir, "\n") {
		t := strings.TrimSpace(line)
		// The runtime block writes `define internal i8* @`, `define internal void @`, `define internal i32 @`
		// and friends; an audit that recognised only two return shapes would report every program in the
		// suite as broken, which is the mistake this helper exists not to make.
		if (strings.HasPrefix(t, "define ") || strings.HasPrefix(t, "declare ")) && strings.Contains(t, hay) {
			return true
		}
	}
	return false
}

// TestWhatTheFoldedModuleReadLeavesStandingAtTheCLI pins what this row does NOT own, at the exit code the
// program actually gets. The text face of **Gap R.546** is the one worth reading at this altitude: the
// `pkg/lang` leg dies in `llc`, and the CLI — the leg a person and an agent actually run — prints the intern
// index at the exit code of success.
func TestWhatTheFoldedModuleReadLeavesStandingAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, today, want, gap string }{
		{
			src:   "t = \"hey\"\nt = t + \"!\"\nprint((lambda: t)())\n",
			today: "0",
			want:  "hey!",
			gap:   "Gap R.546",
		},
		{
			src:   "t = None\ndef f():\n    return t\n\nprint(f())\n",
			today: "0",
			want:  "None",
			gap:   "Gap R.547",
		},
		{
			src:   "print({\"k\": (lambda: 7)()}, (1,))\n",
			today: "{1: 7} (1,)",
			want:  "{'k': 7} (1,)",
			gap:   "Gap R.548",
		},
	} {
		tc := tc
		t.Run(tc.gap, func(t *testing.T) {
			t.Parallel()
			want := strings.TrimRight(pythonOutput(t, tc.src), "\n")
			if want != tc.want {
				t.Fatalf("the reference's answer is written wrong in this table: it prints %q, the row says %q", want, tc.want)
			}
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("%s: the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s\n%s", tc.gap, stderr, stdout)
			}
			got := strings.TrimRight(stdout, "\n")
			if got == want {
				t.Fatalf("%s: the program now prints the reference's %q — DELETE this case\n\t%s", tc.gap, want, tc.src)
			}
			if got != tc.today {
				t.Fatalf("%s: today's answer moved from %q to %q (the reference prints %q) — update or delete the row\n\t%s",
					tc.gap, tc.today, got, want, tc.src)
			}
		})
	}
}

// TestTheSourceStringAnswerFlagsRefuseToMeasureTheirOwnName is the CLI half of this row's machine path, and the
// shape that measured the wrong program for as long as it took to write this row: `--lambda-answers` takes a SOURCE
// STRING, so `--lambda-answers --file prog.gy` hands the flag the string "--file", which parses — as the expression
// `-(-file)` — and the toolchain answers a well-formed account of THAT program at exit 0. The `--verify-llvm` and
// `--debug-info` doors already refuse this shape (`expects a source, not … — pass it last or use --flag=<source>`,
// `cmd/gustyc/main.go`); the account flags do not, and an all-zero document is the answer an agent acts on. Pinned
// here at today's bytes beside the owning row (**Gap R.549**); it changes the day the door speaks.
func TestTheSourceStringAnswerFlagsRefuseToMeasureTheirOwnName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		flag, says string
		exit       int
		gap        string
	}{
		// Today: an account of the program `--file`, reporting that the pass did nothing, at the exit code of success.
		{"--lambda-answers", `"reads_folded": 0`, 0, "Gap R.549"},
		// The control that says the guard exists: the same mis-typing at the door that already has it, answered as the
		// usage error it is (ADR 0006) instead of as a run.
		{"--verify-llvm", "expects a source", 4, "Gap R.549"},
	} {
		tc := tc
		t.Run(tc.flag, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, tc.flag, "--file")
			if code == 2 {
				t.Fatalf("%s: the toolchain failed on its own program (exit 2 — ADR 0166's code):\n%s\n%s", tc.flag, stderr, stdout)
			}
			if code != tc.exit {
				t.Fatalf("%s with a flag as its value exits %d, not %d — the door changed, and the row changes with it\nstdout:%s\nstderr:%s",
					tc.flag, code, tc.exit, stdout, stderr)
			}
			if !strings.Contains(stdout+stderr, tc.says) {
				t.Fatalf("%s with a flag as its value no longer says %q — the door changed, and the row changes with it\nstdout:%s\nstderr:%s",
					tc.flag, tc.says, stdout, stderr)
			}
		})
	}
}

// TestWhatTheFoldedModuleReadStillRefusesAtTheCLI: the shapes the fold leaves to other doors must refuse in
// words at the source-error class, and never at ADR 0166's exit 2.
func TestWhatTheFoldedModuleReadStillRefusesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, says, gap string }{
		{"xs = [1, 2]\nprint((lambda: xs)())\n", "bound at module level", "Gap R.35"},
		{"t = 2.5\nprint((lambda: t)())\n", "roadmap L11.1", "L11.1"},
		{"t = \"hey\"\ndef go(f=(lambda: t)):\n    return f()\n\nprint(go())\n", "roadmap L11.1", "L11.1"},
	} {
		tc := tc
		t.Run(tc.gap+"_"+strings.ReplaceAll(strings.SplitN(tc.src, "\n", 2)[0], " ", "_"), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("%s: exit 2 — a refusal is allowed, a compiler bug is not:\n%s\n%s", tc.gap, stderr, stdout)
			}
			if code == 0 {
				t.Fatalf("%s: the program answered %q, and this row filed it as refused\n\t%s", tc.gap, stdout, tc.src)
			}
			if !strings.Contains(stderr, tc.says) {
				t.Fatalf("%s: the refusal does not name %q:\n%s", tc.gap, tc.says, stderr)
			}
		})
	}
}
