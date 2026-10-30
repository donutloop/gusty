// pkg/lang/roadmap_open_row_test.go — the Open queue cannot advertise work that is already paid.
//
// roadmap L11.1/Gap R.144, measured 2026-11-02. The row said ⏳ `OPEN`, its `Blocked on` cell named two rows
// that are both closed, and its `Definition of done` said two things: `print(a)` is `8` on both legs, and
// `programs/probe_pair_from_a_tuple_unpack` leaves the debt ledger. Both have been true since the cycle that
// shipped **ADR 0367** (`0367-tuple-unpacking-takes-the-pair-road.md`): the program prints `8` at exit 0, CPython
// prints `8`, and the harness's own artifact lists the program as **asserted** with `oracle: match` — it is no
// longer a probe on a ledger at all. The row stayed open anyway, because nothing in the suite reads a row's
// definition of done and asks whether the artifact already satisfies it.
//
// That omission costs a cycle, not just a line. AGENTS.md's loop takes the **top row of the Open queue** as the
// next feature; a row that advertises paid work is a fork in the road where the tracker, which is supposed to
// be the single source of truth for what is next, tells the next agent to build something that exists. And a
// stale ⏳ is not inert: `countQueueRows` publishes it in the Snapshot's "Rows owed" cell, so the published
// count of owed work drifts upward by one every time a cycle forgets to move a status.
//
// The guards below read `roadmap.md` and the harness's own matrix artifact, and they are deliberately narrow:
// they check a row only when its own `Definition of done` cell states the requirement in the tracker's words —
// "<program> leaves the debt ledger" — because that is a claim about an artifact, phrased as one, and the
// artifact answers it. The opposite direction is already covered elsewhere (a closed row must not silently
// reopen; the Snapshot's numbers must be the files' own); what was missing is a row that never left.
//
// Reads only, like every guard over a file outside the package holding the test — it depends on `make test`'s
// `-count=1` (ADR 0317) to be run at all after an edit, and it writes nothing.
package lang

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// paidWorkPhrasing matches the definition-of-done clause that is a claim about the harness's artifacts: a row
// says a named program "leaves the debt ledger" when landing the row promotes it from a pinned probe to an
// asserted parity program. The program may be spelled `programs/<name>`, `<name>.gy`, or bare `<name>` — and it
// is spelled inside a markdown code span, so the closing backtick sits between the name and the clause and has
// to be allowed for (a reader that stops at the backtick matches nothing and passes every row).
var paidWorkPhrasing = regexp.MustCompile("(?i)([A-Za-z0-9_./]+)`?\\s+(?:is\\s+)?leaves the debt ledger")

// openQueueStatuses are the Status cells that mean "owed work" — everything the queue's own counter counts,
// which is every status except a DONE one (countQueueRows uses the same rule, and this is its twin).
func rowIsOwed(statusCell string) bool {
	return strings.HasPrefix(strings.TrimSpace(statusCell), "⏳") ||
		strings.HasPrefix(strings.TrimSpace(statusCell), "🟨") ||
		strings.HasPrefix(strings.TrimSpace(statusCell), "🟢")
}

// promotedConformancePrograms is the harness's answer to "has this program left the debt ledger": the matrix
// artifact is written by the conformance run, so it knows which programs are in the ASSERTED set and which are
// still pinned probes, and it knows whether the asserted one actually answers — a promoted program whose
// compiled leg fails is not a row's work being done, it is the row's work being wrong.
type promotedMatrix struct {
	Results []struct {
		Case struct {
			Name     string `json:"name"`
			Asserted bool   `json:"asserted"`
			Oracle   string `json:"oracle"`
		} `json:"case"`
		AOTOk *bool `json:"aot_ok"`
	} `json:"results"`
}

func promotedConformancePrograms(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "integration", "conformance-matrix.json"))
	if err != nil {
		t.Fatalf("read the conformance matrix: %v", err)
	}
	var m promotedMatrix
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse the conformance matrix: %v", err)
	}
	if len(m.Results) == 0 {
		t.Fatal("the conformance matrix holds no results — the guard would pass on an empty artifact")
	}
	out := map[string]bool{}
	for _, r := range m.Results {
		name := strings.TrimSuffix(strings.TrimSpace(r.Case.Name), ".gy")
		if name == "" || !r.Case.Asserted {
			continue
		}
		// A promoted probe whose compiled leg refuses is still a probe in every sense that matters; only a
		// program that ANSWERS is evidence that a row's work is done.
		if r.AOTOk != nil && !*r.AOTOk {
			continue
		}
		out[name] = true
	}
	return out
}

// advertisedPaidWork returns the programs a row's Definition-of-done cell claims are still on the ledger while
// the harness's artifact says they are promoted and answering.
//
// The rule is deliberately tied to the clause rather than to every program name in the cell. L11.1 — the
// umbrella row the queue sits on — names a dozen witnesses in its Definition of done, most of them paid, and
// none of them a claim that the row is owed work: `programs/slot_order_object.gy` is in that cell as a record of
// what the row has already bought. Only "<program> leaves the debt ledger" states the ROW's done-ness as a fact
// about the corpus, and only that reading keeps the umbrella row's own witness list from reading as stale.
func advertisedPaidWork(dodCell string, promoted map[string]bool) []string {
	var hits []string
	seen := map[string]bool{}
	for _, m := range paidWorkPhrasing.FindAllStringSubmatch(dodCell, -1) {
		name := strings.TrimSuffix(strings.TrimPrefix(m[1], "programs/"), ".gy")
		if promoted[name] && !seen[name] {
			seen[name] = true
			hits = append(hits, name)
		}
	}
	return hits
}

// TestAnOpenQueueRowDoesNotAdvertisePaidWork is the guard: a row the queue still counts as owed work must not
// state its definition of done as "some program leaves the debt ledger" while that program is already an
// asserted, answering member of the conformance corpus.
func TestAnOpenQueueRowDoesNotAdvertisePaidWork(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	text := string(mustReadArtifact(t, filepath.Join(root, "roadmap.md")))
	promoted := promotedConformancePrograms(t, root)
	if len(promoted) == 0 {
		t.Fatal("no program in the matrix is asserted — this guard cannot tell paid work from unpaid")
	}
	checked := 0
	for _, line := range strings.Split(text, "\n") {
		cells, ok := queueRowCells(line)
		if !ok {
			continue
		}
		id, status, dod := strings.TrimSpace(cells[2]), cells[4], cells[7]
		if !rowIsOwed(status) {
			continue
		}
		hits := advertisedPaidWork(dod, promoted)
		if len(hits) == 0 {
			continue
		}
		checked++
		t.Errorf("roadmap.md's Open-queue row %q is still owed work (%s) while its own definition of done is "+
			"satisfied: %s %s — the artifact says the program is promoted and answers, so the row's work is paid.\n"+
			"\tclose the row with what the artifact says (it must be re-measured, not remembered — roadmap Gap R.204) "+
			"or move the satisfied clause out of the Definition of done.\n\t%s",
			id, strings.TrimSpace(status), strings.Join(hits, ", "),
			pluralLeavesTheLedger(len(hits)), strings.TrimSpace(dod))
	}
	if checked == 0 {
		// Not a t.Fatal: the tree is legitimately clean today, and the guard must still be able to pass. The
		// negative control below is what proves it can fail; this only says the phrase has been reworded away.
		t.Log("no owed row states its done-ness as a program leaving the debt ledger — if that phrasing is gone " +
			"for good, delete this guard rather than let it silently stop checking anything")
	}
}

func pluralLeavesTheLedger(n int) string {
	if n == 1 {
		return "has already left the debt ledger"
	}
	return "have already left the debt ledger"
}

// TestThatGuardCanFail is the negative control the whole house runs on: a guard that cannot fail is a comment.
// Rather than remember a row id (which would be a second copy of the fact the guard reads, and the copy would
// rot exactly the way the stale row did), it DOCTORS the shipped file — takes a row that is CLOSED today,
// reopens its Status cell, and points its Definition of done at a program the artifact says is promoted and
// answering. The real file must pass and the doctored one must fail; if either half flips, the guard is not
// reading what it claims to read.
func TestThatGuardCanFail(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	text := string(mustReadArtifact(t, filepath.Join(root, "roadmap.md")))
	promoted := promotedConformancePrograms(t, root)
	var promotedName string
	for name := range promoted {
		if strings.HasPrefix(name, "probe_") || strings.HasPrefix(name, "programs/") {
			promotedName = name
			break
		}
	}
	if promotedName == "" {
		for name := range promoted {
			promotedName = name
			break
		}
	}
	if promotedName == "" {
		t.Fatal("no promoted program to point a doctored row at")
	}

	var lines []string
	reopened := false
	for _, line := range strings.Split(text, "\n") {
		cells, ok := queueRowCells(line)
		if ok && !rowIsOwed(cells[4]) && !reopened && strings.Contains(cells[2], "Gap R.") {
			// Reopen a closed row IN PLACE — Status cell and Definition-of-done cell both rewritten, the row
			// rebuilt from its own cells. Appending the clause to the line would have dropped it in the row's
			// EIGHTH cell, where the guard is right not to look, and the control would have "failed" for the
			// wrong reason.
			cells[4] = " ⏳ `OPEN` "
			cells[7] = " `programs/" + promotedName + "` leaves the debt ledger "
			line = "|" + strings.Join(cells[1:len(cells)-1], "|") + "|"
			reopened = true
		}
		lines = append(lines, line)
	}
	if !reopened {
		t.Fatal("could not find a closed Gap row to doctor — the guard has nothing to check")
	}

	doctored := strings.Join(lines, "\n")
	hits := 0
	for _, line := range strings.Split(doctored, "\n") {
		cells, ok := queueRowCells(line)
		if !ok || !rowIsOwed(cells[4]) {
			continue
		}
		hits += len(advertisedPaidWork(cells[7], promoted))
	}
	if hits == 0 {
		t.Errorf("reopening a closed row and pointing its Definition of done at promoted program %q was not caught "+
			"— the guard does not read the Status cell, the clause, or the matrix", promotedName)
	}

	// And the control that keeps it honest in the other direction: the shipped file, untouched, catches nothing.
	// (The main guard asserts the same fact across every row; this pins it for the doctored bytes specifically.)
	clean := 0
	for _, line := range strings.Split(text, "\n") {
		cells, ok := queueRowCells(line)
		if !ok || !rowIsOwed(cells[4]) {
			continue
		}
		clean += len(advertisedPaidWork(cells[7], promoted))
	}
	if clean != 0 {
		t.Errorf("the shipped roadmap.md already trips the guard %d time(s), so the doctored result proves nothing — "+
			"close the rows or fix the reading", clean)
	}
}
