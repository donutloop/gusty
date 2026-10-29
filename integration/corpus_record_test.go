package integration

// Every program the conformance corpus ASSERTS must be ON the record leg.
//
// roadmap Gap R.532 · ADR 0456 (found while paying Gap R.523)
//
// The record (`pkg/lang/testdata/interpreter-golden.json`) is the second witness a compiled answer is judged
// against, and **ADR 0302**'s rule is that a missing entry FAILS the case — that is what keeps coverage from
// being deleted by deleting a record (**Gap R.204**). But the rule only bites where a case ASKS. For a source
// written inline in a test file, some table asks. For a whole corpus program under `integration/programs/`,
// exactly one case in the suite asked: `TestGCCorpusCollectsAndAgrees`, whose subject is the collector.
//
// That is how `integration/programs/unpack_container_claims.gy` sat in the corpus with NO record entry from
// the commit that added it — shipped in a documentation-only commit two commits after the feature it belongs
// to, whose gate therefore never asked. The GC case reports it as an error, so the package was red; nothing
// in the cycle that shipped it, and nothing in the matrix, named the missing half, because the matrix's own
// legs are the compiled binary and CPython and the record is not one of them.
//
// So the claim gets its own case, in the file that owns the corpus: an asserted program is a program the record
// holds an answer for. The message says what to run (`tools/recmerge`, from the reference — a compiled leg may
// never write its own answer), and the unrecorded programs are listed together rather than one per run, so a
// cycle that adds three learns about all three at once.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// corpusRecordedSources is the corpus's asserted programs — the ones the record leg owes an answer for. A row
// the ledger declares `not_applicable` is exempt, and exempt BY THE ARTIFACT rather than by this file's opinion:
// the reference cannot run that program at all (it is gusty-only surface — the set-subscript extension among
// them), so the record has no answer to hold for it and demanding one would ask the recorder to invent a
// CPython answer. `tools/recmerge`'s own `-check` mode exists for exactly this class.
func corpusRecordedSources() []lang.ConformanceCase {
	var out []lang.ConformanceCase
	for _, c := range conformanceCases() {
		if !c.Asserted || c.Source == "" {
			continue // a probe is a recorded divergence, not conformance surface
		}
		if c.Oracle == lang.OracleNA {
			continue // the reference has no answer for a program it cannot parse or run
		}
		out = append(out, c)
	}
	return out
}

func TestEveryAssertedCorpusProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []struct {
		id  string
		src string
	}
	for _, c := range corpusRecordedSources() {
		if !lang.HasGoldenAnswer(c.Source) {
			missing = append(missing, struct {
				id  string
				src string
			}{c.ID, c.Source})
		}
	}
	if len(missing) == 0 {
		return
	}
	t.Errorf("%d asserted conformance program(s) have no entry in `pkg/lang/testdata/interpreter-golden.json` — the record leg is the second "+
		"witness a compiled answer is judged against, and a missing entry means nothing asks about the program (roadmap Gap R.532, ADR 0302). "+
		"Record them FROM THE REFERENCE with `go run -tags=llvm20 ./tools/recmerge -sources <file>` (the compiled leg may never write its own answer):", len(missing))
	for _, m := range missing {
		t.Errorf("  %s:\n%s", m.id, m.src)
	}
}

// TestTheRecordSourcesTheCorpusRecordRowAsksFor is the recorder, not a case: with GUSTY_RECORD_CORPUS set it
// writes every asserted corpus program the record leg owes an answer for, in the JSON shape `tools/recmerge`
// reads, and skips. Same rules as the other recorders: without the variable it skips, the list is read from the
// registry rather than retyped, and the variable is scrubbed from every subprocess the suite starts
// (`integration/parallelism_guard_test.go`, **Gap R.410** / **ADR 0409**).
func TestTheRecordSourcesTheCorpusRecordRowAsksFor(t *testing.T) {
	out := os.Getenv("GUSTY_RECORD_CORPUS")
	if out == "" {
		t.Skip("the recorder: set GUSTY_RECORD_CORPUS=<file> to dump the corpus's asserted sources for tools/recmerge")
	}
	var srcs []string
	for _, c := range corpusRecordedSources() {
		srcs = append(srcs, c.Source)
	}
	body, merr := json.MarshalIndent(srcs, "", "  ")
	if merr != nil {
		t.Fatalf("rendering the source list: %v", merr)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("writing %s: %v", out, err)
	}
	t.Logf("wrote %d sources to %s", len(srcs), out)
}
