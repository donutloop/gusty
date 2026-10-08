// pkg/lang/roadmap_snapshot_test.go — a table that says "measured, not remembered" has to be measured.
//
// roadmap Gap R.204 again, one file further up the stack. `roadmap.md`'s Snapshot table is the first thing a
// reader (human or agent) consults for the state of the toolchain: how many ADRs, how many sources the record
// holds, how many divergences the ledgers carry, how many matrix rows are parity-asserted. Its heading says
// "(measured, not remembered)" and every one of those numbers was remembered: the record row said 5913 while
// the file held 5948, the ADR row said "306 records, highest `0314`" while `docs/adr/` held 311 files up to
// `0319`, the matrix row said 135 asserted + 39 divergent while the artifact says 136 + 38, the oracle row was
// one `match` and one `debt` out, the queue row said 117 rows where the table has 119 — and the row describing
// the test suite still spelled the gate without the `-count=1` ADR 0317 put on the shipped command.
//
// The staleness in the matrix and oracle rows is not neglect: closing Gap R.197 promoted a probe out of debt,
// which moved one row from divergent to asserted, and no step in that cycle's checklist could have noticed the
// snapshot disagreeing. That is the argument for computing these numbers instead of documenting them.
//
// Reads only, like every guard over a file outside the package holding the test — it depends on `make test`'s
// `-count=1` (ADR 0317) to be run at all after an edit, and it writes nothing.
package lang

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// snapshotRows are the Snapshot rows whose value cell is a claim about files in this repository. `want`
// returns the integers the cell must contain, in the order they appear there; the row is located by its label
// so a renamed row fails as "no such row" rather than as a pass.
var snapshotRows = []struct {
	label string
	want  func(t *testing.T, root string) []int
}{
	{"ADRs", snapshotADRs},
	{"Conformance programs", snapshotConformancePrograms},
	{"Matrix rows", snapshotMatrixRows},
	{"Parity failures / oracle drift", snapshotMatrixFailures},
	{"Oracle verdicts", snapshotOracleVerdicts},
	{"Retired-engine record", func(t *testing.T, root string) []int {
		return []int{countArtifact(t, root, countedArtifacts[0])}
	}},
	{"Ledgers (two-way ratchets)", snapshotLedgers},
	{"Rows owed", snapshotQueueRows},
}

func snapshotLedgers(t *testing.T, root string) []int {
	t.Helper()
	out := make([]int, 0, 3)
	for _, a := range countedArtifacts[1:] {
		out = append(out, countArtifact(t, root, a))
	}
	return out
}

func snapshotADRs(t *testing.T, root string) []int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "docs", "adr"))
	if err != nil {
		t.Fatalf("read docs/adr: %v", err)
	}
	adrFile := regexp.MustCompile(`^([0-9]{4})-.+\.md$`)
	n, high := 0, 0
	for _, e := range entries {
		m := adrFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n++
		if v, err := strconv.Atoi(m[1]); err == nil && v > high {
			high = v
		}
	}
	if n == 0 {
		t.Fatal("docs/adr holds no numbered ADRs")
	}
	return []int{n, high}
}

func snapshotConformancePrograms(t *testing.T, root string) []int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "integration", "programs"))
	if err != nil {
		t.Fatalf("read integration/programs: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".gy") {
			n++
		}
	}
	if n == 0 {
		t.Fatal("integration/programs holds no .gy program")
	}
	return []int{n}
}

// conformanceMatrix is the summary the harness writes; the row claims the total, the asserted half and the
// divergent half, so the halves are read from the per-case rows the same way the harness counts them.
type conformanceMatrix struct {
	Rows                int `json:"rows"`
	Skipped             int `json:"skipped"`
	Pass                int `json:"pass"`
	Fail                int `json:"fail"`
	OracleMatch         int `json:"oracle_match"`
	OracleDebt          int `json:"oracle_debt"`
	OracleNotApplicable int `json:"oracle_not_applicable"`
	OracleDrift         int `json:"oracle_drift"`
	Results             []struct {
		Conformant bool `json:"conformant"`
	} `json:"results"`
}

func readConformanceMatrix(t *testing.T, root string) conformanceMatrix {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "integration", "conformance-matrix.json"))
	if err != nil {
		t.Fatalf("read the conformance matrix (run `go test ./integration` to write it): %v", err)
	}
	var m conformanceMatrix
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("the conformance matrix does not parse: %v", err)
	}
	if len(m.Results) != m.Rows {
		t.Fatalf("the matrix says %d rows and carries %d: the artifact is half-written", m.Rows, len(m.Results))
	}
	return m
}

func snapshotMatrixRows(t *testing.T, root string) []int {
	t.Helper()
	m := readConformanceMatrix(t, root)
	asserted, divergent := 0, 0
	for _, r := range m.Results {
		if r.Conformant {
			asserted++
		} else {
			divergent++
		}
	}
	return []int{m.Rows, asserted, divergent}
}

func snapshotMatrixFailures(t *testing.T, root string) []int {
	t.Helper()
	m := readConformanceMatrix(t, root)
	return []int{m.Fail, m.OracleDrift}
}

func snapshotOracleVerdicts(t *testing.T, root string) []int {
	t.Helper()
	m := readConformanceMatrix(t, root)
	return []int{m.OracleMatch, m.OracleDebt, m.OracleNotApplicable}
}

// queueHeaderRow is the Open queue's header. The queue is the only list of owed work (AGENTS.md), so the tally
// is read out of this one table and nothing else.
const queueHeaderRow = "| Pri | ID | Item | Status |"

// queuePri matches the queue's own priority column: `1`, `111`, and the suffixed rows a cycle inserts beside an
// existing priority (`111a`, `111f`, `112b`). It is what separates a queue row from a Phase-table or Gap-ledger
// row, both of which put the ITEM ID in column 1 rather than a number.
var queuePri = regexp.MustCompile(`^[0-9]+[a-z]?$`)

// queueRowCells reports whether the line is an Open-queue data row, returning its cells.
func queueRowCells(line string) ([]string, bool) {
	if !strings.HasPrefix(line, "|") {
		return nil, false
	}
	cells := strings.Split(line, "|")
	if len(cells) < 8 { // seven columns: Pri, ID, Item, Status, Blocked on, Next action, Definition of done
		return nil, false
	}
	if !queuePri.MatchString(strings.TrimSpace(cells[1])) {
		return nil, false
	}
	return cells, true
}

// countQueueRows is the tally behind the Snapshot's "Rows owed" cell, and it is deliberately able to fail on
// the shape that hides work: a queue table SPLIT BY A BLANK LINE. The rows below the split still render, still
// read as owed work to a human, and are invisible to a scanner that stops at the first non-row line — which is
// exactly what this file did on 2026-10-08, when four filed rows (Gap R.202, R.203, R.206, R.207) sat below a
// stray blank line and the tracker reported 119 rows where it held 123. `hidden` counts them so the caller can
// fail the run and name the line to delete rather than quietly add them to the total.
func countQueueRows(text string) (total, owed, hidden int, err error) {
	at := strings.Index(text, queueHeaderRow)
	if at < 0 {
		return 0, 0, 0, fmt.Errorf(`roadmap.md has no %q table — the Open queue is the only list of owed work, do not rename it`, queueHeaderRow)
	}
	lines := strings.Split(text[at:], "\n")
	inTable := false
	for i := 0; i < len(lines); i++ {
		if cells, ok := queueRowCells(lines[i]); ok {
			inTable = true
			total++
			if !strings.Contains(cells[4], "DONE") {
				owed++
			}
			continue
		}
		if !inTable {
			continue
		}
		for j := i; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue // the split itself
			}
			cells, ok := queueRowCells(lines[j])
			if !ok {
				break // the Phase tables below are not owed work
			}
			hidden++
			total++
			if !strings.Contains(cells[4], "DONE") {
				owed++
			}
		}
		break
	}
	if total == 0 {
		return 0, 0, 0, fmt.Errorf("the Open queue holds no rows")
	}
	return total, owed, hidden, nil
}

func snapshotQueueRows(t *testing.T, root string) []int {
	t.Helper()
	text := string(mustReadArtifact(t, filepath.Join(root, "roadmap.md")))
	total, owed, hidden, err := countQueueRows(text)
	if err != nil {
		t.Fatal(err)
	}
	if hidden > 0 {
		t.Errorf("the Open queue table is SPLIT by a blank line: %d owed row(s) sit below it and render as a table "+
			"with no header, which is how four filed rows stayed out of the tally once (they are counted here so the "+
			"cell is honest; delete the blank line so the queue is one table)", hidden)
	}
	return []int{owed, total}
}

// TestTheRoadmapSnapshotIsMeasuredNotRemembered is the guard.
func TestTheRoadmapSnapshotIsMeasuredNotRemembered(t *testing.T) {
	root := repoRoot(t)
	text := string(mustReadArtifact(t, filepath.Join(root, "roadmap.md")))
	digits := regexp.MustCompile(`[0-9]+`)
	for _, row := range snapshotRows {
		want := row.want(t, root)
		cell := snapshotCell(t, text, row.label)
		var got []int
		for _, d := range digits.FindAllString(cell, -1) {
			n, err := strconv.Atoi(d)
			if err != nil {
				t.Fatalf("%s: %q is not a number: %v", row.label, d, err)
			}
			got = append(got, n)
		}
		if len(got) != len(want) {
			t.Errorf("the %q row quotes %d numbers (%v) and this guard expects %d (%v) — if the row's wording "+
				"changed, update the guard's reading of it, not the roadmap's claim about the file",
				row.label, len(got), got, len(want), want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("roadmap.md's Snapshot row %q says %d in position %d; the artifact says %d\n\t%s\n"+
					"this table is captioned \"measured, not remembered\" — the number is the file's (roadmap Gap R.204)",
					row.label, got[i], i, want[i], strings.TrimSpace(cell))
			}
		}
	}
}

// TestTheRoadmapSnapshotNamesTheSerialGateWithoutTheCache is ADR 0317's rule one file over: the makefile ships
// `-count=1`, and the roadmap's own description of the gate must not advertise the cached command an agent
// would otherwise copy out of the tracker.
func TestTheRoadmapSnapshotNamesTheSerialGateWithoutTheCache(t *testing.T) {
	root := repoRoot(t)
	text := string(mustReadArtifact(t, filepath.Join(root, "roadmap.md")))
	cell := snapshotCell(t, text, "Test suite")
	if !strings.Contains(cell, "go test -tags=llvm20 -count=1 ./...") {
		t.Errorf("roadmap.md's \"Test suite\" row does not name the shipped serial gate `go test -tags=llvm20 "+
			"-count=1 ./...` (ADR 0317) — the makefile is the rule and the tracker must not sell a cached command\n\t%s",
			strings.TrimSpace(cell))
	}
}

// TestTheQueueCounterSeesWhatASplitTableHides is this guard's own negative control (Gap R.204's rule aimed at
// the counter rather than at the cell). The counter stops at the first line that is not a queue row, which is
// correct for the Phase tables below the queue and WRONG for a blank line accidentally left inside the queue:
// the rows under it still render, still read as owed work, and used to leave the tally — four filed rows stayed
// out of the published count that way. Splitting the real file's table in a string must move `hidden` and must
// not move `owed`, which is the pair of facts that makes this a check rather than a comment.
func TestTheQueueCounterSeesWhatASplitTableHides(t *testing.T) {
	root := repoRoot(t)
	text := string(mustReadArtifact(t, filepath.Join(root, "roadmap.md")))
	total, owed, hidden, err := countQueueRows(text)
	if err != nil {
		t.Fatal(err)
	}
	if hidden != 0 {
		t.Errorf("the shipped roadmap.md's queue is split by a blank line: %d owed row(s) render below the split "+
			"and a plain scanner would not see them — delete the blank line", hidden)
	}
	if total <= owed {
		t.Errorf("total %d, owed %d: a queue with no closed rows is not what this file holds", total, owed)
	}

	// Split it: put a blank line in front of the queue row that is second from the end.
	lines := strings.Split(text, "\n")
	var queueLines []int
	for i, line := range lines {
		if _, ok := queueRowCells(line); ok {
			queueLines = append(queueLines, i)
		}
	}
	if len(queueLines) < 3 {
		t.Fatalf("the Open queue holds %d rows — the counter is reading the wrong table", len(queueLines))
	}
	at := queueLines[len(queueLines)-2]
	doctored := append(append([]string{}, lines[:at]...),
		append([]string{""}, lines[at:]...)...)
	splitTotal, splitOwed, splitHidden, err := countQueueRows(strings.Join(doctored, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if splitHidden == 0 {
		t.Errorf("splitting the queue table by hand hid %d rows from the counter — the counter cannot see the shape "+
			"that once took four rows out of the published tally, so it is not a guard against it", total-splitTotal)
	}
	if splitTotal != total || splitOwed != owed {
		t.Errorf("a split table changed the tally (%d→%d total, %d→%d owed): the counter is supposed to count the "+
			"rows below the split AND report them, not drop them", total, splitTotal, owed, splitOwed)
	}

	// A Phase-table row is not owed work: the Phase tables put the ITEM id in column 1, not a priority.
	if _, ok := queueRowCells("| L11.6 | numeric truth | ⏳ `PLANNED` | both | 0216 | probes | done | a | [→](x) |"); ok {
		t.Error("a Phase-table row counted as an Open-queue row — the tally would double-count every item")
	}
	if _, ok := queueRowCells("| Gap R.204 | the record's size | ✅ `DONE` | tooling | `0319` | x | y | [record](z) |"); ok {
		t.Error("a Gap-ledger row counted as an Open-queue row")
	}
	if _, ok := queueRowCells("| 111f | Gap R.148 | a pair-bound name | ⏳ `OPEN` | — | x | y |"); !ok {
		t.Error("a suffixed priority (`111f`) is not recognised as a queue row, and the rows that use it would " +
			"leave the tally — they exist in the file today")
	}
}

// snapshotCell returns the value cell (second) of the row whose label (first cell) is `label`.
func snapshotCell(t *testing.T, text, label string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) >= 3 && strings.TrimSpace(cells[1]) == label {
			return cells[2]
		}
	}
	t.Fatalf("roadmap.md's Snapshot table has no %q row — the row an agent reads for this measure has been "+
		"dropped or renamed; keep it and keep it computed (roadmap Gap R.204)", label)
	return ""
}
