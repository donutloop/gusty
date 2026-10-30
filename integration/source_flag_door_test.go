package integration

// integration/source_flag_door_test.go — the CLI refuses to measure its own flag names, at every door in
// the family (roadmap **Gap R.549**, **ADR 0463**).
//
// The command line that filed the row was three tokens long:
//
//	gustyc --lambda-answers --file prog.gy
//
// `--lambda-answers` takes a SOURCE STRING, and the token after it was `--file`, which PARSES — as the
// expression `-(-file)` — so the toolchain printed a well-formed `gusty.lambda-answers.v1` account of that
// two-character program and left through the exit code of success. Nothing in the document said it was about
// `--file`; an agent that asked a question about `prog.gy` received an answer about the command line's own
// punctuation and had no way to tell (ADR 0346, ADR 0379: a machine payload about the wrong subject is not a
// neutral answer).
//
// Two doors (`--verify-llvm`, `--debug-info`) already refused the shape, each with its own hand-written
// `strings.HasPrefix(v, "-")` loop. Those loops are gone; the family has one door now
// (`cmd/gustyc/main.go`'s `refuseFlagShapedSource`), and it is narrower than the two it replaces in one
// direction and wider in another: it refuses a value that NAMES A DEFINED FLAG rather than one that merely
// starts with a dash — which is what lets `--eval -1` keep meaning "run the program `-1`" — and it covers the
// flags that name a path (`--build`, `--bench-dir`, `--stdlib`) as well as the ones that name a source.
//
// Exit 4 is **ADR 0006**'s usage code: the mistake is in the command line, not in a program, and the tool must
// not run either to say so. Every row asserts **ADR 0166**'s rule too — no exit 2 anywhere, on the refused
// command line or on the control that proves the door did not swallow a real one.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sourceFlagDoorRows is the family: every flag the CLI reads a program, a path, or a document from, with the
// word the refusal uses for what it wanted.
func sourceFlagDoorRows() []struct{ flag, says string } {
	return []struct{ flag, says string }{
		{"--eval", "a source"},
		{"--file", "a path"},
		{"--verify", "a source"},
		{"--check", "a source"},
		{"--effects", "a source"},
		{"--oracle", "a source"},
		{"--oracle-file", "a path"},
		{"--emit-llvm", "a source"},
		{"--emit-ast", "a source"},
		{"--emit-source-map", "a path"},
		{"--verify-llvm", "a source"},
		{"--verify-llvm-file", "a path"},
		{"--debug-info", "a source"},
		{"--debug-info-file", "a path"},
		{"--callables", "a source"},
		{"--lambda-answers", "a source"},
		{"--bench", "a source"},
		{"--bench-file", "a path"},
		{"--fmt", "a source"},
		{"--fmt-file", "a path"},
		{"--bench-dir", "a path"},
		{"--bench-baseline", "a path"},
		{"--bench-baseline-update", "a path"},
		{"--build", "a path"},
		{"--source-map-out", "a path"},
		{"--stdlib", "a path"},
	}
}

// TestEveryFlagTheCLIGuardsIsAFlagTheCLIHas keeps the table above honest: a row for a flag that has since
// left the toolchain would certify a door nobody walks through, so each name is asked of the binary's own
// `--help` output.
func TestEveryFlagTheCLIGuardsIsAFlagTheCLIHas(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := cliStreams(t, "--help")
	if code != 0 {
		t.Fatalf("--help exits %d, not 0\n%s%s", code, stdout, stderr)
	}
	help := stdout + stderr
	for _, row := range sourceFlagDoorRows() {
		row := row
		t.Run(row.flag, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(help, strings.TrimPrefix(row.flag, "--")) {
				t.Fatalf("%s is guarded by the door but is not in --help — the table has rotted", row.flag)
			}
		})
	}
}

// TestTheSourceFlagFamilyRefusesAFlagAsItsValue is the row: the same mis-typing at each door prints the same
// sentence, leaves stdout empty (no report about the wrong subject escapes), and spends the usage code.
func TestTheSourceFlagFamilyRefusesAFlagAsItsValue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	prog := filepath.Join(dir, "prog.gy")
	if err := os.WriteFile(prog, []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, row := range sourceFlagDoorRows() {
		row := row
		t.Run(row.flag, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, row.flag, "--file", prog)
			if code == 2 {
				t.Fatalf("%s: the toolchain failed on its own program (exit 2 — ADR 0166's code)\n%s\n%s",
					row.flag, stderr, stdout)
			}
			if code != 4 {
				t.Fatalf("%s with a flag as its value exits %d, not 4 — the usage code is ADR 0006's answer to a "+
					"mis-typed command line, and anything else means the tool ran the wrong program\nstdout:%s\nstderr:%s",
					row.flag, code, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("%s refused, so it must print nothing on stdout; it printed a report about the wrong subject:\n%s",
					row.flag, stdout)
			}
			for _, want := range []string{row.flag, "expects " + row.says + ",", `not "--file"`, "pass it last"} {
				if !strings.Contains(stderr, want) {
					t.Fatalf("%s refusal must contain %q, said:\n%s", row.flag, want, stderr)
				}
			}
		})
	}
}

// TestTheDoorStillRunsWhatIsReallyASource is the control that keeps the refusal from being a wall: a value
// that merely LOOKS like an option is somebody's program, and the `--flag=<source>` spelling is the escape the
// refusal's own sentence promises. A door that refused all three would be a new bug filed under the same row.
func TestTheDoorStillRunsWhatIsReallyASource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want string // something stdout or stderr must contain
		deny string // something neither stream may contain
		code int
	}{
		{
			name: "a negative literal is a program, not a flag",
			args: []string{"--eval", "-1"},
			want: "-1",
			deny: "expects a source",
			code: 0,
		},
		{
			name: "the attached spelling carries the text the toolchain is asked to measure",
			args: []string{"--eval=--file"},
			// It compiles the TWO-CHARACTER program `--file`, which is `-(-file)`: the reference has no
			// `file`, and the refusal is the compiler's ordinary name error — about the program, not the flag.
			want: `undefined name "file"`,
			deny: "expects a source",
			code: 1,
		},
		{
			name: "a flag after the positional still reaches its flag",
			args: []string{"--opt-level", "2", "--eval", "print(1 + 1)"},
			want: "2",
			deny: "expects a source",
			code: 0,
		},
		{
			name: "an undefined option is refused by the door, and the flag package still owns it elsewhere",
			args: []string{"--eval", "--not-a-flag"},
			// Measured before the widening: `--lambda-answers --not-a-flag` printed a well-formed account of
			// the program `--not-a-flag` at exit 0 — the same wrong subject, one typo from the row's own shape.
			want: `not "--not-a-flag"`,
			deny: "",
			code: 4,
		},
		{
			name: "an option nobody defines, in a position that is not a value, is the flag package's to name",
			args: []string{"--not-a-flag", "--eval", "print(1)"},
			want: "flag provided but not defined",
			deny: "expects a source",
			code: 4,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, tc.args...)
			if code == 2 {
				t.Fatalf("exit 2 on `%s` — ADR 0166's code for a compiler that broke\n%s\n%s",
					strings.Join(tc.args, " "), stderr, stdout)
			}
			if code != tc.code {
				t.Fatalf("`%s` exits %d, not %d\nstdout:%s\nstderr:%s", strings.Join(tc.args, " "), code, tc.code, stdout, stderr)
			}
			if !strings.Contains(stdout+stderr, tc.want) {
				t.Fatalf("`%s` must contain %q\nstdout:%s\nstderr:%s", strings.Join(tc.args, " "), tc.want, stdout, stderr)
			}
			if tc.deny != "" && strings.Contains(stdout+stderr, tc.deny) {
				t.Fatalf("`%s` must NOT be refused — the door swallowed a real source\nstdout:%s\nstderr:%s",
					strings.Join(tc.args, " "), stdout, stderr)
			}
		})
	}
}

// TestTheAttachedAndSpacedSpellingsAnswerTheSameAccount is the second half of the row's Definition of done: a
// guard satisfied only by pushing the trap one keystroke away is not a guard. `--flag=<source>` is the form the
// refusal's own sentence points at, so for any source the two spellings must be the same question to the
// toolchain — byte for byte. The source that IS an option's name is the door's own case, and it is pinned from
// both sides in TestTheDoorStillRunsWhatIsReallyASource.
func TestTheAttachedAndSpacedSpellingsAnswerTheSameAccount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		flag, src string
	}{
		{"--lambda-answers", "t = \"hey\"\nprint((lambda: t)())\n"},
		{"--callables", "def apply(f, x):\n    return f(x)\nprint(apply(lambda v: v + 1, 1))\n"},
		{"--check", "print(1 + 2)\n"},
	} {
		tc := tc
		t.Run(tc.flag+" "+strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			spacedOut, spacedErr, spacedCode := cliStreams(t, tc.flag, tc.src)
			attachedOut, attachedErr, attachedCode := cliStreams(t, tc.flag+"="+tc.src)
			if spacedCode == 2 || attachedCode == 2 {
				t.Fatalf("exit 2 — ADR 0166's code for a compiler that broke\n%s\n%s", spacedErr+attachedErr, spacedOut+attachedOut)
			}
			if attachedCode != 0 {
				t.Fatalf("%s=<source> is the spelling the refusal points at, so it must run: exit %d\n%s\n%s",
					tc.flag, attachedCode, attachedOut, attachedErr)
			}
			if spacedCode != 0 {
				t.Fatalf("%s <source> must run the same program: exit %d\n%s\n%s", tc.flag, spacedCode, spacedOut, spacedErr)
			}
			if spacedOut != attachedOut {
				t.Fatalf("the two spellings answered differently:\nspaced:   %s\nattached: %s", spacedOut, attachedOut)
			}
		})
	}
}

// TestThePositionalProgramStillRuns is the cheapest possible control: the door reads the flags it is given, and
// a run with none of them must not notice it exists.
func TestThePositionalProgramStillRuns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	prog := filepath.Join(dir, "prog.gy")
	if err := os.WriteFile(prog, []byte("print(\"door-invisible\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := cliStreams(t, prog)
	if code != 0 || !strings.Contains(stdout, "door-invisible") {
		t.Fatalf("positional run broke: exit %d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
}
