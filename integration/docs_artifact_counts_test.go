// integration/docs_artifact_counts_test.go — the same guard as `pkg/lang/golden_artifact_counts_test.go`,
// asked from the suite an agent actually runs.
//
// roadmap Gap R.204. Two claims live in docs/operations.md's artifact table about files THIS package owns:
// the CLI suite's divergence ledger (21 rows) and the CPython debt ledger (7 rows). Those files are written
// by this package's `TestMain` from the divergences the CLI cases register, and the table quotes their
// sizes — the numbers an agent uses to decide how much of the surface is known-divergent rather than
// unknown. Nothing recomputed either number: the table said 5623 sources for the record while the file held
// 5948, and the same freedom applied to these two.
//
// The duplication with `pkg/lang` is deliberate, not an oversight: a guard that lives only in the package the
// agent did not run is a guard that did not run (ADR 0317's lesson, learned about the test cache and about
// shard ledgers). Both suites read the same four files and the same table, and both can go red alone.
//
// Reads only, like every guard over a file outside the package holding the test — it depends on `make test`'s
// `-count=1` (ADR 0317) to be run at all after a doc edit, and it writes nothing.
package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// integrationCountedArtifacts are the rows of the table this package can certify: its own two ledgers, plus
// the record it reads through the shared loader and therefore also counts.
var integrationCountedArtifacts = []struct {
	path  string
	what  string
	slice bool
}{
	{path: "integration/testdata/interpreter-golden-drift.json", what: "divergence rows", slice: true},
	{path: "integration/testdata/cpython-debt.json", what: "reference-debt rows", slice: true},
	{path: "pkg/lang/testdata/interpreter-golden.json", what: "sources on record"},
}

func integrationArtifactCount(t *testing.T, path string, slice bool) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(recordRoot, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if slice {
		var rows []json.RawMessage
		if err := json.Unmarshal(data, &rows); err != nil {
			t.Fatalf("%s does not parse as the ledger array it is: %v", path, err)
		}
		return len(rows)
	}
	var file struct {
		Entries map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("%s does not parse: %v", path, err)
	}
	if len(file.Entries) == 0 {
		t.Fatalf("%s has no entries at all", path)
	}
	return len(file.Entries)
}

// TestTheCLISuitesArtifactCountsAreTheFilesOwn checks the table's rows for the files this package owns, from
// this package's directory.
func TestTheCLISuitesArtifactCountsAreTheFilesOwn(t *testing.T) {
	const doc = "docs/operations.md"
	text, err := os.ReadFile(filepath.Join(recordRoot, doc))
	if err != nil {
		t.Fatalf("read %s: %v", doc, err)
	}
	for _, a := range integrationCountedArtifacts {
		want := integrationArtifactCount(t, a.path, a.slice)
		row := ""
		for _, line := range strings.Split(string(text), "\n") {
			if strings.HasPrefix(line, "|") && strings.Contains(line, a.path) {
				row = line
				break
			}
		}
		if row == "" {
			t.Fatalf("%s has no artifact row for %s — the table is the interface an agent reads", doc, a.path)
		}
		cells := strings.Split(row, "|")
		if len(cells) < 3 {
			t.Fatalf("%s: the row for %s has no description cell:\n%s", doc, a.path, row)
		}
		digits := regexp.MustCompile(`[0-9][0-9,]*`).FindString(cells[2])
		if digits == "" {
			t.Fatalf("%s: the row for %s quotes no count:\n%s", doc, a.path, row)
		}
		got, err := strconv.Atoi(strings.ReplaceAll(digits, ",", ""))
		if err != nil {
			t.Fatalf("%s: %q is not a count: %v", doc, digits, err)
		}
		if got != want {
			t.Errorf("%s says %s %s for %s; the file holds %d — the count is the artifact's, not a number "+
				"someone carried over (roadmap Gap R.204)", doc, digits, a.what, a.path, want)
		}
	}
}

// TestTheRecordCountTheCLIKitQuotesIsTheFilesOwn covers the coverage claim in the two documents an agent
// reads before it trusts the record: AGENTS.md (the loop's contract) and docs/operations.md.
func TestTheRecordCountTheCLIKitQuotesIsTheFilesOwn(t *testing.T) {
	want := integrationArtifactCount(t, "pkg/lang/testdata/interpreter-golden.json", false)
	claim := regexp.MustCompile(`([0-9][0-9,]*)\s+sources\b`)
	for _, doc := range []string{"AGENTS.md", filepath.Join("docs", "operations.md")} {
		text, err := os.ReadFile(filepath.Join(recordRoot, doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		found := claim.FindAllStringSubmatch(string(text), -1)
		if len(found) == 0 {
			t.Fatalf("%s no longer states how many sources the record holds (%d)", doc, want)
		}
		for _, m := range found {
			if n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", "")); err != nil || n != want {
				t.Errorf("%s says %s sources; the record holds %d (roadmap Gap R.204)", doc, m[1], want)
			}
		}
	}
}
