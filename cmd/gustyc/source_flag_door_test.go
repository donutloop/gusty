package main

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"
)

// The CLI's wrong-subject door, at the altitude where its two rules can be asked on their own: is this
// value another flag's name, and does the flag that received it get refused? The end-to-end form — the real
// binary, the real exit code — is `integration/source_flag_door_test.go`; this file is the part that must be
// true whatever the argv looks like (roadmap **Gap R.549**, ADR **0463**).
func newDoorFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("gustyc-test", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the door's own verdict is what is under test, not the flag package's usage dump
	fs.String("eval", "", "")
	fs.String("file", "", "")
	fs.String("lambda-answers", "", "")
	fs.String("lambda_answers", "", "") // the underscore alias, which the FlagSet files under its own name
	fs.String("opt-level", "0", "")
	fs.String("target", "", "")
	fs.String("bench-dir", "", "")
	fs.String("build", "", "")
	fs.Bool("json", false, "")
	return fs
}

func doorTable(name, value string) []sourceFlag {
	return []sourceFlag{{name: name, value: value, says: "a source"}}
}

// TestAFlagShapedSourceIsNamedAsOne is the predicate half of the door: a value is refused exactly when it could
// only have come from the command line — an option shape, or a name the FlagSet recognises — and not merely
// when it begins with a dash, which is the test the two hand-written guards it replaces used to make.
func TestAFlagShapedSourceIsNamedAsOne(t *testing.T) {
	t.Parallel()
	fs := newDoorFlagSet()
	for _, tc := range []struct {
		value string
		want  bool
		why   string
	}{
		{"--file", true, "another defined flag, which is the mistake that filed the row"},
		{"--json", true, "a bool flag's name is still a flag's name"},
		{"-json", true, "the single-dash spelling the flag package also accepts"},
		{"--lambda_answers", true, "the alias spelling resolves too"},
		{"--not-a-flag", true, "an UNDEFINED long option is still an option: measured, `--lambda-answers " +
			"--not-a-flag` printed a well-formed account of that program at exit 0"},
		{"--eval=--file", true, "the name before the `=` is the token that was typed"},
		{"-1", false, "a negative literal is a program, and `gustyc --eval -1` has always meant it"},
		{"-O2", false, "a dash-prefixed value that names nothing is somebody's text"},
		{"-2.5", false, "so is a negative float"},
		{"-", false, "the stdin marker is the command line's own punctuation"},
		{"--", false, "so is the end-of-flags marker"},
		{"", false, "an unset flag is not a mis-typing"},
		{"print(1)", false, "an ordinary source"},
		{"--=", true, "an option-shaped token with nothing before the `=` is still shaped like one, and no program begins that way"},
	} {
		tc := tc
		t.Run(tc.value+" "+tc.why, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeAnOption(fs, tc.value); got != tc.want {
				t.Fatalf("looksLikeAnOption(%q) = %v, want %v (%s)", tc.value, got, tc.want, tc.why)
			}
		})
	}
	if got := looksLikeAnOption(nil, "--file"); !got {
		t.Fatal("a long option is an option whatever the FlagSet says")
	}
	if got := looksLikeAnOption(nil, "-json"); got {
		t.Fatal("a short option can only be recognised by a FlagSet, so a nil one must answer no rather than guess")
	}
}

// TestTheDoorRefusesTheMisTypedCommandLineAndPassesTheRest is the half that decides the exit code, and pins
// the sentence: it names the flag, the value it was handed, and the spelling that would have worked.
func TestTheDoorRefusesTheMisTypedCommandLineAndPassesTheRest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		args     []string
		attached map[string]bool
		given    []sourceFlag
		wantCode int
		want     bool
		says     string
		doesnt   string
	}{
		{
			name:     "the shape that measured the wrong program",
			args:     []string{"--lambda-answers", "--file", "prog.gy"},
			given:    doorTable("--lambda-answers", "--file"),
			wantCode: exitUsage,
			want:     true,
			says:     `--lambda-answers expects a source, not "--file"`,
			doesnt:   "prog.gy",
		},
		{
			name:     "the attached spelling is the escape",
			args:     []string{"--lambda-answers=--file"},
			given:    doorTable("--lambda-answers", "--file"),
			wantCode: 0,
			want:     false,
			says:     "",
			doesnt:   "expects",
		},
		{
			name:     "a dash that is not a flag name is the program",
			args:     []string{"--eval", "-1"},
			given:    doorTable("--eval", "-1"),
			wantCode: 0,
			want:     false,
			says:     "",
			doesnt:   "expects",
		},
		{
			name:     "an option-shaped value is refused even when nothing defines it",
			args:     []string{"--lambda-answers", "--not-a-flag"},
			given:    doorTable("--lambda-answers", "--not-a-flag"),
			wantCode: exitUsage,
			want:     true,
			says:     `--lambda-answers expects a source, not "--not-a-flag"`,
			doesnt:   "",
		},
		{
			name:     "a short option the FlagSet knows is refused too",
			args:     []string{"--eval", "-json"},
			given:    doorTable("--eval", "-json"),
			wantCode: exitUsage,
			want:     true,
			says:     `--eval expects a source, not "-json"`,
			doesnt:   "",
		},
		{
			name:     "a path flag says so",
			args:     []string{"--bench-dir", "--json"},
			given:    []sourceFlag{{name: "--bench-dir", value: "--json", says: "a path"}},
			wantCode: exitUsage,
			want:     true,
			says:     `--bench-dir expects a path, not "--json" — pass it last or use --bench-dir=<path>`,
			doesnt:   "",
		},
		{
			name:     "an empty value is not a mis-typing",
			args:     []string{"--eval", "print(1)"},
			given:    doorTable("--eval", ""),
			wantCode: 0,
			want:     false,
			says:     "",
			doesnt:   "expects",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			set := newDoorFlagSet()
			reordered, attached := reorderFlags(set, tc.args)
			if err := set.Parse(reordered); err != nil {
				t.Fatalf("the argument list the door is asked about must itself parse: %v", err)
			}
			if tc.attached != nil {
				attached = tc.attached
			}
			var out bytes.Buffer
			code, refused := refuseFlagShapedSource(&out, set, attached, tc.given)
			if refused != tc.want || code != tc.wantCode {
				t.Fatalf("refuseFlagShapedSource = (%d, %v), want (%d, %v)\nsaid: %s", code, refused, tc.wantCode, tc.want, out.String())
			}
			if tc.says != "" && !strings.Contains(out.String(), tc.says) {
				t.Fatalf("the refusal must say %q, said:\n%s", tc.says, out.String())
			}
			if tc.doesnt != "" && strings.Contains(out.String(), tc.doesnt) {
				t.Fatalf("the refusal must not contain %q, said:\n%s", tc.doesnt, out.String())
			}
		})
	}
}

// TestTheAttachedSpellingIsRecordedByTheReorder is the reason the refusal's promise is keepable: `--flag=<source>`
// is reported by reorderFlags, so the sentence can point at a form that actually compiles the text.
func TestTheAttachedSpellingIsRecordedByTheReorder(t *testing.T) {
	t.Parallel()
	fs := newDoorFlagSet()
	_, attached := reorderFlags(fs, []string{"--lambda_answers=--file", "--eval", "print(1)", "--json"})
	if !attached["lambda_answers"] {
		t.Fatalf("reorderFlags did not record the attached spelling: %v", attached)
	}
	if attached["eval"] {
		t.Fatalf("the space-separated form must not be recorded as attached: %v", attached)
	}
	if attached["json"] {
		t.Fatalf("a bool flag owns no value and must not be recorded: %v", attached)
	}
}
