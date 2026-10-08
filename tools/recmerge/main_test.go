// tools/recmerge/main_test.go — the record-leg recorder is a guard over a reviewed artifact, so it is tested
// like one: an entry it does not add keeps its bytes, an entry it adds reads back, the two legs disagreeing is
// an error rather than a write, and a run that would leave the record unparseable fails instead of writing.
//
// The legs are arguments (see `legs` in main.go), so these cases run without building the compiler: the point
// here is what the recorder is willing to write, not what the backend answers.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleRecord = `{
 "entries": {
  "print(1)\n": {
   "stdout": "1\n",
   "hasStdout": true
  },
  "print(z)\n": {
   "err": "NameError: name 'z' is not defined",
   "exnType": "NameError",
   "exnMsg": "name 'z' is not defined"
  }
 },
 "meta": {
  "note": "the record"
 }
}`

func useRecord(t *testing.T, body string) *string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "interpreter-golden.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	old := recordPath
	recordPath = path
	t.Cleanup(func() { recordPath = old })
	return &recordPath
}

func sourcesFile(t *testing.T, srcs ...string) string {
	t.Helper()
	b, err := json.Marshal(srcs)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sources.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// agreeOn answers every source with the same stdout on both legs, or with the trap the map holds.
func agreeOn(answers map[string]string, traps map[string]string) legs {
	return legs{
		ref: func(src string) (string, string, error) {
			if out, ok := answers[src]; ok {
				return out, "", nil
			}
			if trap, ok := traps[src]; ok {
				return "", "Traceback (most recent call last):\n  File \"<string>\", line 1, in <module>\n" + trap, errReference
			}
			return "", "the reference declined", errReference
		},
		compiled: func(src string) (string, string, int, error) {
			if out, ok := answers[src]; ok {
				return out, "", 0, nil
			}
			if trap, ok := traps[src]; ok {
				return "", "Traceback (most recent call last):\n" + trap, 3, nil
			}
			return "", "refused", 1, errCompiled
		},
	}
}

type stubError string

func (s stubError) Error() string { return string(s) }

var (
	errReference = stubError("the reference declined")
	errCompiled  = stubError("the compiled backend refused")
)

func TestRunRecordsWhatBothLegsAgreeOn(t *testing.T) {
	useRecord(t, sampleRecord)
	srcs := sourcesFile(t, "print(min([1], [2]))\n", "print(2)\n")
	answers := map[string]string{"print(2)\n": "2\n"}
	traps := map[string]string{"print(min([1], [2]))\n": "TypeError: unorderable types"}
	if err := run(srcs, false, agreeOn(answers, traps)); err != nil {
		t.Fatalf("recording two programs both legs agree on: %v", err)
	}
	b, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatalf("the record is not valid JSON after the write:\n%s", b)
	}
	var f doc
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) != 4 {
		t.Fatalf("the record has %d entries, want 4", len(f.Entries))
	}
	// The entries the recorder did not add are byte-identical, in the order the file had them.
	for _, keep := range []string{`  "print(1)\n": {
   "stdout": "1\n",
   "hasStdout": true
  },`, `  "print(z)\n": {
   "err": "NameError: name 'z' is not defined",
   "exnType": "NameError",
   "exnMsg": "name 'z' is not defined"
  }`} {
		if !strings.Contains(string(b), keep) {
			t.Errorf("an entry the recorder did not add was rewritten:\n%s", b)
		}
	}
	var e entry
	if err := json.Unmarshal(f.Entries["print(2)\n"], &e); err != nil {
		t.Fatal(err)
	}
	if !e.HasStd || e.Stdout != "2\n" {
		t.Errorf("the answer entry reads back as %+v", e)
	}
	if strings.Contains(string(f.Entries["print(2)\n"]), `"int"`) {
		t.Errorf("a field the entry does not use was written anyway: %s", f.Entries["print(2)\n"])
	}
	if err := json.Unmarshal(f.Entries["print(min([1], [2]))\n"], &e); err != nil {
		t.Fatal(err)
	}
	if e.ExnType != "TypeError" || e.ExnMsg != "unorderable types" || e.Err != "TypeError: unorderable types" {
		t.Errorf("the trap entry reads back as %+v, want the reference's class, message and sentence", e)
	}
}

func TestRunWritesNothingWhenALegDisagrees(t *testing.T) {
	const trapSrc = "print(min([1], [2]))\n"
	const ansSrc = "print(2)\n"
	for _, tc := range []struct {
		name     string
		src      string
		ref      func(string) (string, string, error)
		compiled func(string) (string, string, int, error)
		want     string
	}{
		{
			name:     "the compiled leg prints other bytes than the reference",
			src:      ansSrc,
			ref:      func(string) (string, string, error) { return "2\n", "", nil },
			compiled: func(string) (string, string, int, error) { return "3\n", "", 0, nil },
			want:     "other bytes",
		},
		{
			name:     "the compiled leg refuses what the reference prints",
			src:      ansSrc,
			ref:      func(string) (string, string, error) { return "2\n", "", nil },
			compiled: func(string) (string, string, int, error) { return "", "refused", 1, errCompiled },
			want:     "refused a program the reference prints",
		},
		{
			name:     "the compiled leg answers at exit 0 what the reference traps",
			src:      trapSrc,
			ref:      trapRef(trapSrc),
			compiled: func(string) (string, string, int, error) { return "[1]\n", "", 0, nil },
			want:     "answered where the reference traps",
		},
		{
			name:     "the compiled leg spends exit 2 on a trap",
			src:      trapSrc,
			ref:      trapRef(trapSrc),
			compiled: func(string) (string, string, int, error) { return "", "boom", 2, nil },
			want:     "exit 2",
		},
		{
			name: "the compiled raise is not the reference's sentence",
			src:  trapSrc,
			ref:  trapRef(trapSrc),
			compiled: func(string) (string, string, int, error) {
				return "", "TypeError: something else entirely", 3, nil
			},
			want: "compiled raise is not the reference",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			useRecord(t, sampleRecord)
			before, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatal(err)
			}
			err = run(sourcesFile(t, tc.src), false, legs{ref: tc.ref, compiled: tc.compiled})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the recorder accepted a row it must refuse (%q); got %v", tc.want, err)
			}
			after, rerr := os.ReadFile(recordPath)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if string(after) != string(before) {
				t.Errorf("the record was written while a leg disagreed:\n%s", after)
			}
		})
	}
}

// trapRef answers the reference leg with a trap whose last line is the given sentence.
func trapRef(src string) func(string) (string, string, error) {
	return func(string) (string, string, error) {
		return "", "Traceback (most recent call last):\n  File \"<string>\", line 1, in <module>\n" +
			"TypeError: unorderable types", errReference
	}
}

func TestCheckModeRefusesARowTheReferenceAnswers(t *testing.T) {
	useRecord(t, sampleRecord)
	before, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	// The refusal rows (Gap R.198's shapes) are asked with -check: the reference RAISING is the row, and a
	// reference that ANSWERS means the row is no longer a refusal and must not be recorded as one.
	err = run(sourcesFile(t, "print(2)\n"), true, agreeOn(map[string]string{"print(2)\n": "2\n"}, nil))
	if err == nil || !strings.Contains(err.Error(), "the reference ANSWERS") {
		t.Fatalf("-check accepted a source the reference answers: %v", err)
	}
	if err := run(sourcesFile(t, "print(min([1], [2]))\n"), true,
		agreeOn(nil, map[string]string{"print(min([1], [2]))\n": "TypeError: unorderable types"})); err != nil {
		t.Fatalf("-check refused a row whose reference trap it should accept: %v", err)
	}
	after, rerr := os.ReadFile(recordPath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(after) != string(before) {
		t.Errorf("-check wrote the record: %s", after)
	}
}

func TestAppendEntriesRefusesARecordItCannotFind(t *testing.T) {
	if _, err := appendEntries([]byte(`{"meta": {"x": 1}}`), []string{"print(1)\n"},
		map[string]entry{"print(1)\n": {HasStd: true, Stdout: "1\n"}}); err == nil {
		t.Fatal("a record with no `entries` object was accepted")
	}
}
