// pkg/lang/golden_artifact_counts_test.go — the numbers an agent plans with are the artifacts' own, or they
// are folklore.
//
// roadmap Gap R.204, measured while closing Gap R.197: the artifact table in docs/operations.md and the
// record sentence in AGENTS.md both said "5623 sources" while `pkg/lang/testdata/interpreter-golden.json`
// held 5948 entries. The figure was copied by the cycle that wrote the sentence, the record grew in every
// cycle after it — including the 35 the fold-ordering feature added — and nothing recomputed it. It is ADR
// 0317's defect class one layer up: a claim whose subject is a file, certified without opening the file.
// There the culprit was `go test`'s cache key; here it is that nobody ever asked. ADR 0302 made a MISSING
// record fail the case, so an entry cannot disappear; nothing protected the COUNT of them, which is the
// number a reader uses to judge what the record covers, and nothing protected a key spelled twice, which
// is an entry no case can ever ask.
//
// So: the four artifact counts, the two document counts and the record's key uniqueness are computed here,
// in the package that owns the record. `integration/docs_artifact_counts_test.go` asks the same question
// from the CLI suite's side, against the two ledgers that package writes. Reads only — like every guard
// over a file outside the package holding the test, this one depends on `make test`'s `-count=1` (ADR 0317)
// to run at all after a doc edit, and it writes nothing.
package lang

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// countedArtifact is one row of docs/operations.md's artifact table that quotes a count: the repo-relative
// file, what its count means in prose, and whether the file is a top-level JSON array (the ledgers) or the
// object with an `entries` map (the record).
type countedArtifact struct {
	path  string
	what  string
	slice bool
}

var countedArtifacts = []countedArtifact{
	{path: "pkg/lang/testdata/interpreter-golden.json", what: "sources on record"},
	{path: "pkg/lang/testdata/interpreter-golden-drift.json", what: "divergence rows", slice: true},
	{path: "integration/testdata/interpreter-golden-drift.json", what: "divergence rows", slice: true},
	{path: "integration/testdata/cpython-debt.json", what: "reference-debt rows", slice: true},
}

// countArtifact reads one artifact and answers how many entries it holds, through the same JSON decoding
// the suite reads it with: a file that does not parse fails the count rather than being counted as zero.
func countArtifact(t *testing.T, root string, a countedArtifact) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, a.path))
	if err != nil {
		t.Fatalf("read %s: %v", a.path, err)
	}
	return countArtifactBytes(t, a, data)
}

func countArtifactBytes(t *testing.T, a countedArtifact, data []byte) int {
	t.Helper()
	if a.slice {
		var rows []json.RawMessage
		if err := json.Unmarshal(data, &rows); err != nil {
			t.Fatalf("%s does not parse as the ledger array it is: %v", a.path, err)
		}
		return len(rows)
	}
	var file struct {
		Entries map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("%s does not parse: %v", a.path, err)
	}
	if len(file.Entries) == 0 {
		t.Fatalf("%s has no entries at all", a.path)
	}
	return len(file.Entries)
}

// firstNumberInRow finds the row of a markdown table that names `artifact` and returns the first integer in
// its description cell — the position the count sits in today ("| `path` | 5948 sources with the answer …").
// A row whose count was deleted, or pushed behind other digits, fails here naming the file and the row.
func firstNumberInRow(t *testing.T, text, artifact, file string) (int, string) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "|") || !strings.Contains(line, artifact) {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			t.Fatalf("%s: the row for %s has no description cell:\n%s", file, artifact, line)
		}
		digits := regexp.MustCompile(`[0-9][0-9,]*`).FindString(cells[2])
		if digits == "" {
			t.Fatalf("%s: the row for %s quotes no count at all:\n%s", file, artifact, line)
		}
		n, err := strconv.Atoi(strings.ReplaceAll(digits, ",", ""))
		if err != nil {
			t.Fatalf("%s: %q is not a count: %v", file, digits, err)
		}
		return n, cells[2]
	}
	t.Fatalf("%s: no table row names %s — the artifact table is an interface, do not drop a row", file, artifact)
	return 0, ""
}

// TestTheArtifactTableCountsAreTheFilesOwn is the guard: the four counts docs/operations.md quotes are the
// files' own, recomputed on every run that is not served from the test cache.
func TestTheArtifactTableCountsAreTheFilesOwn(t *testing.T) {
	root := repoRoot(t)
	const doc = "docs/operations.md"
	text := string(mustReadArtifact(t, filepath.Join(root, doc)))
	for _, a := range countedArtifacts {
		want := countArtifact(t, root, a)
		got, cell := firstNumberInRow(t, text, a.path, doc)
		if got != want {
			t.Errorf("%s says %s %s for %s; the file holds %d\n\t%s\n"+
				"the count is the artifact's, not anyone's memory of it — edit the cell to %d, and if it was "+
				"the FILE that changed, say so in the row it is recorded in (roadmap Gap R.204)",
				doc, strconv.Itoa(got), a.what, a.path, want, strings.TrimSpace(cell), want)
		}
	}
}

// sourcesClaim matches a documented record size: "5948 sources", "5,948 sources". The singular is
// deliberately not matched — docs/operations.md says "1 source" and "2765 source(s)" about other things.
var sourcesClaim = regexp.MustCompile(`([0-9][0-9,]*)\s+sources\b`)

// TestTheRecordedSourceCountInTheAgentFacingDocsIsTheFilesOwn is the same claim where it matters most:
// AGENTS.md is the loop's contract, and an agent reads the record's coverage out of it before it decides
// what a suite may certify. Every "N sources" in the two files is the file's own.
func TestTheRecordedSourceCountInTheAgentFacingDocsIsTheFilesOwn(t *testing.T) {
	root := repoRoot(t)
	want := countArtifact(t, root, countedArtifacts[0])
	for _, doc := range []string{"AGENTS.md", filepath.Join("docs", "operations.md")} {
		text := string(mustReadArtifact(t, filepath.Join(root, doc)))
		found := sourcesClaim.FindAllStringSubmatch(text, -1)
		if len(found) == 0 {
			t.Fatalf("%s no longer states how many sources the record holds — that coverage claim is an "+
				"interface, put the number back (the file holds %d)", doc, want)
		}
		for _, m := range found {
			n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
			if err != nil {
				t.Fatalf("%s: %q is not a count: %v", doc, m[1], err)
			}
			if n != want {
				t.Errorf("%s says the record holds %s sources; it holds %d (roadmap Gap R.204 — this test "+
					"recomputes it, so edit the sentence rather than the test)", doc, m[1], want)
			}
		}
	}
}

// TestTheRecordHasNoSourceSpelledTwice is the count's sibling: `map[string]any` decodes a file with a key
// written twice to the LAST of the pair and calls it a clean parse, so a duplicated entry is an answer no
// case can reach and a number the table over-reports by however many pairs there are. The keys are read
// through the token stream, which is the only place the repetition is still visible.
func TestTheRecordHasNoSourceSpelledTwice(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, countedArtifacts[0].path))
	if err != nil {
		t.Fatal(err)
	}
	keys := jsonKeysOf(t, data, "entries")
	seen := map[string]int{}
	for _, k := range keys {
		seen[k]++
	}
	var twice []string
	for k, n := range seen {
		if n > 1 {
			twice = append(twice, strconv.Quote(k)+" x"+strconv.Itoa(n))
		}
	}
	sort.Strings(twice)
	if len(twice) > 0 {
		t.Errorf("the record spells %d source(s) more than once, and only the last of each pair is ever "+
			"read: %s", len(twice), strings.Join(twice, ", "))
	}
	if len(keys) != len(seen) {
		t.Errorf("the record has %d keys and %d distinct ones", len(keys), len(seen))
	}
	// And the count the table quotes is the token-stream count, not the collapsed one.
	if got, want := len(keys), len(loadGolden(t)); got != want {
		t.Errorf("the token stream reads %d record entries and loadGolden %d", got, want)
	}
}

// jsonKeysOf walks the top-level object of `data` and returns the keys of the named child object, in file
// order, WITH repeats. A child that is not an object is a failure: the record's shape is its contract.
func jsonKeysOf(t *testing.T, data []byte, child string) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := expectToken(t, dec, '{'); err != nil {
		return nil
	}
	var raw json.RawMessage
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatalf("top-level key: %v", err)
		}
		key, ok := k.(string)
		if !ok {
			t.Fatalf("a top-level key is not a string: %v", k)
		}
		if key == child {
			if err := dec.Decode(&raw); err != nil {
				t.Fatalf("decode %s: %v", child, err)
			}
			break
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatalf("decode %s: %v", key, err)
		}
	}
	if raw == nil {
		t.Fatalf("the record has no %q object", child)
	}
	inner := json.NewDecoder(bytes.NewReader(raw))
	if err := expectToken(t, inner, '{'); err != nil {
		return nil
	}
	var keys []string
	for inner.More() {
		k, err := inner.Token()
		if err != nil {
			t.Fatalf("key after %s: %v", child, err)
		}
		key, ok := k.(string)
		if !ok {
			t.Fatalf("a key under %s is not a string: %v", child, k)
		}
		var skip json.RawMessage
		if err := inner.Decode(&skip); err != nil {
			t.Fatalf("decode the value of %q: %v", key, err)
		}
		keys = append(keys, key)
	}
	return keys
}

func expectToken(t *testing.T, dec *json.Decoder, want rune) error {
	t.Helper()
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("read the opening %c: %v", want, err)
	}
	if d, ok := tok.(json.Delim); !ok || rune(d) != want {
		t.Fatalf("expected the document to open with %c, got %v", want, tok)
	}
	return nil
}

// TestTheCountGuardCanFail is the guard tested against its own ability to fail (the harness's own rule, the
// conformance ledger's `a stubbed pin must produce drift`): the row locator is handed the real table with one
// digit of one count changed and must report the disagreement rather than nod.
func TestTheCountGuardCanFail(t *testing.T) {
	root := repoRoot(t)
	text := string(mustReadArtifact(t, filepath.Join(root, "docs", "operations.md")))
	number := regexp.MustCompile(`[0-9][0-9,]*`)
	for _, a := range countedArtifacts {
		want := countArtifact(t, root, a)
		_, cell := firstNumberInRow(t, text, a.path, "doctored")
		loc := number.FindStringIndex(cell)
		if loc == nil {
			t.Fatalf("no count found in the row for %s: %s", a.path, cell)
		}
		mutated := cell[:loc[0]] + strconv.Itoa(want+1) + cell[loc[1]:]
		if strings.Count(text, cell) != 1 {
			t.Fatalf("the row for %s is not a unique slice of the table, so the doctoring below proves nothing", a.path)
		}
		docctored := strings.Replace(text, cell, mutated, 1)
		got, _ := firstNumberInRow(t, docctored, a.path, "doctored")
		if got == want {
			t.Errorf("the count guard accepted a doctored row for %s: %s", a.path, strings.TrimSpace(mutated))
		}
		if got != want+1 {
			t.Errorf("the count guard read %d from a row that says %d for %s", got, want+1, a.path)
		}
	}
}

func mustReadArtifact(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
