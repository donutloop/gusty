package integration

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The two container doors that refuse in words, at the command line (roadmap Gap R.288, ADR 0437).
//
//	d = {[1]: "x"}
//	print(d[[1]])
//	  reference  TypeError: unhashable type: 'list'
//	  this build exit 1, and the sentence ended "…Build that inner value separately, or run it
//	             interpreted" — a way to run that ADR 0302 retired and a flag whose answer is a usage
//	             error, which is the half of a refusal a reader can neither use nor verify
//
// An agent reading `--json` branches on the `error` string, so the machine path is asserted beside the
// human one: the same sentence, once, with the rule that is missing named — the hashing rule when the
// position is a dict KEY, the per-slot tag rule when the position is an element. The phrases a refusal may
// not say are read from `pkg/lang/testdata/witness-emitter-banned-phrases.txt`, the same data
// `pkg/lang/witness_emitter_test.go` asks of every string literal in the emitter, so the CLI cannot pass a
// test the emitter's own guard would fail (ADR 0232's one question, asked wherever the sentence surfaces).
//
// Exit 2 is forbidden on every row: a refusal is exit 1, and the code the exit-code contract reserves for a
// compiler bug is a different defect (ADR 0166).

// cliRefusalPhraseList reads the emitter's banned phrases. They are data in the language package's
// testdata because the guard that polices the emitter's literals reads the same file; a CLI test that
// typed its own copy would be a second list to drift.
func cliRefusalPhraseList(t *testing.T) []*regexp.Regexp {
	t.Helper()
	path := filepath.Join("..", "pkg", "lang", "testdata", "witness-emitter-banned-phrases.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v — the CLI guard has no list to enforce", path, err)
	}
	defer f.Close()
	var out []*regexp.Regexp
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile("(?i)" + line)
		if err != nil {
			t.Fatalf("%s: %q is not a usable pattern: %v", path, line, err)
		}
		out = append(out, re)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s is empty: the CLI refusal guard would pass without checking anything", path)
	}
	return out
}

// cliDiag is the part of `--json` this row is about.
type cliDiag struct {
	OK      bool   `json:"ok"`
	Backend string `json:"backend"`
	Phase   string `json:"phase"`
	Exit    int    `json:"exit"`
	Error   string `json:"error"`
}

func TestTheContainerDoorsNameTheRuleTheyMissThroughTheCLI(t *testing.T) {
	t.Parallel()
	banned := cliRefusalPhraseList(t)
	for _, tc := range []struct {
		name string
		src  string
		says string
	}{
		{"the row's own program, an untaggable key", "d = {[1]: \"x\"}\nprint(d[[1]])\n", "as a key yet: a dict finds an entry by hashing its key"},
		{"a list key on a literal dict", "print({[1, 2]: 3})\n", "no hashing rule for a container"},
		{"the empty container spelled as a call", "print({set(): 1})\n", "an empty set as a key yet"},
		{"an unhashable tuple key", "print({(1, [2]): 3})\n", "a tuple literal as a key yet"},
		{"a container key inside a value", "d = {\"a\": {[1]: 2}}\nprint(d)\n", "a list literal as a key yet"},
		{"an element that nests deeper than any tag", "d = {1: [2]}\nprint([[d[1]] for k in d])\n", "a slot of a compiled container carries one word"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "refusal.gy", tc.src)
			stdout, stderr, code := cliStreams(t, cliEngines[0], path)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 is ADR 0166's own-bug code):\n%s", stderr)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 — a refusal is the front end declining, not the program trapping: %s / %s", code, stdout, stderr)
			}
			if stdout != "" {
				t.Errorf("a refused program printed %q on stdout", stdout)
			}
			human := stdout + stderr
			if !strings.Contains(human, tc.says) {
				t.Errorf("the refusal does not name %q:\n%s", tc.says, human)
			}
			if !strings.Contains(human, "roadmap") {
				t.Errorf("the refusal names no roadmap row:\n%s", human)
			}
			for _, re := range banned {
				if re.MatchString(human) {
					t.Errorf("the refusal promises %s — the pattern it matched is %q:\n%s", "a way to run this toolchain retired", re.String(), human)
				}
			}
			// The machine path: the same sentence, not a paraphrase, not a second copy that can drift.
			jsonOut, jsonErr, jsonCode := cliStreams(t, "--json", cliEngines[0], path)
			if jsonCode != 1 {
				t.Fatalf("--json exited %d, want 1:\n%s / %s", jsonCode, jsonOut, jsonErr)
			}
			var diag cliDiag
			if err := json.Unmarshal([]byte(strings.TrimSpace(jsonOut)), &diag); err != nil {
				t.Fatalf("--json is not readable: %v\n%s", err, jsonOut)
			}
			if diag.OK || diag.Exit != 1 || diag.Backend != "aot" {
				t.Errorf("--json reported ok=%v exit=%d backend=%q, want false/1/aot:\n%s", diag.OK, diag.Exit, diag.Backend, jsonOut)
			}
			if diag.Phase != "codegen" {
				t.Errorf("--json named phase %q, want \"codegen\" — this is the compiled backend declining to lower a shape, not the front end or the run time", diag.Phase)
			}
			for _, want := range []string{tc.says, "roadmap"} {
				if !strings.Contains(diag.Error, want) {
					t.Errorf("--json's error does not carry %q:\n%s", want, diag.Error)
				}
			}
			for _, re := range banned {
				if re.MatchString(diag.Error) {
					t.Errorf("--json's error promises a way to run this toolchain retired (%q):\n%s", re.String(), diag.Error)
				}
			}
			if strings.TrimSpace(human) != "gustyc: "+diag.Error {
				t.Errorf("the human sentence and the machine sentence are not one sentence:\n  human: %s\n  json:  %s", strings.TrimSpace(human), diag.Error)
			}
		})
	}
}

// TestTheHashableKeysStillAnswerAtTheCLI is the control that the doors were not simply widened: every key
// that has a hash, and every container element that printed, prints the reference's bytes at exit 0. Each
// row is asked of a live CPython in the case, and each answered source is on the record.
func TestTheHashableKeysStillAnswerAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"a tuple key answers", "d = {(1, 2): \"x\"}\nprint(d[(1, 2)])\n", "x\n"},
		{"a tuple key prints in a dict", "print({(1, 2): 3})\n", "{(1, 2): 3}\n"},
		{"a text key answers", "d = {\"a\": 1}\nprint(d[\"a\"])\n", "1\n"},
		{"a container element one level deep prints", "print([[1, [2]], 1])\n", "[[1, [2]], 1]\n"},
		{"two levels of containers print", "print([[[1, 2], [3]], 1])\n", "[[[1, 2], [3]], 1]\n"},
		{"a dict in a list prints", "print([{\"a\": 1}, 2])\n", "[{'a': 1}, 2]\n"},
		{"a dict whose value is a container prints", "print([[{\"a\": [1]}], 1])\n", "[[{'a': [1]}], 1]\n"},
		{"a comprehension over containers", "xs = [[1, 2], [3]]\nprint([y for y in xs])\n", "[[1, 2], [3]]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "answered.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode != 0 {
				t.Fatalf("the reference exited %d on a program meant to answer:\n%s", refCode, refOut)
			}
			if refOut != tc.want {
				t.Fatalf("the reference prints %q, want %q", refOut, tc.want)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 {
					t.Errorf("%s exited %d on a program the reference answers: %s / %s", engine, code, stdout, stderr)
				}
				if stdout != tc.want {
					t.Errorf("%s printed %q, want %q — the reference's bytes (the refusal door was widened): %s", engine, stdout, tc.want, tc.name)
				}
			}
			if !lang.HasGoldenAnswer(tc.src) {
				t.Errorf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
		})
	}
}

// TestTheUntaggableKeyRefusalsHaveNoAnswerToRecord is the record's half of the row: a refusal has no answer
// to record, and `tools/recmerge` will not write one — it asks the reference and refuses a source whose
// compiled leg disagrees. So the four shapes this row's doors decline must be absent from the record, not
// present with a fabricated sentence beside them: the reference raises for three and answers the fourth for
// a different reason than this backend has, which is a debt row's shape, not a record's.
func TestTheUntaggableKeyRefusalsHaveNoAnswerToRecord(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {[1]: \"x\"}\nprint(d[[1]])\n",
		"print({set(): 1})\n",
		"print({(1, [2]): 3})\n",
		"d = {1: [2]}\nprint([[d[1]] for k in d])\n",
	} {
		src := src
		t.Run(strings.ReplaceAll(src, "\n", " / "), func(t *testing.T) {
			t.Parallel()
			if lang.HasGoldenAnswer(src) {
				t.Errorf("%q is on the record with an ANSWER, although this backend refuses it — a refusal cannot have an answer beside it", src)
			}
			if _, err := lang.Compile(src); err == nil {
				t.Errorf("%q compiled where the refusal is the honest answer", src)
			}
		})
	}
}
