// pkg/lang/roadmap_evidence_test.go — a DONE row whose evidence points at a file that is not there is a DONE
// row nobody can falsify.
//
// roadmap Gap R.205, measured just after Gap R.204. The tracker's Evidence cells are the whole reason a status
// cell means anything: `roadmap.md` says Gap R.189 is 🟨 `PARTIAL` *because* two named tests assert the
// literal-key door and the CLI leg. They were `pkg/lang/key_error_message_test.go` and
// `integration/key_error_names_the_key_test.go`. Neither file exists — the ADR 0301 cycle named them, a later
// refactor folded the cases into `container_methods_test.go`, and the evidence cell kept citing a pair of
// filenames from a tree that no longer has them. Nothing noticed, because nothing read them: the suite can
// fail for a missing TEST (a `go test` run with `-run` matching nothing is a green no-op, which is the same
// hole ADR 0317 closed for documents), and the tracker can cite a missing test only if someone opens the path.
//
// So the claim is checked the way the counts are: every backticked repository path in the tracker must exist,
// and every `path::TestName` must find `func TestName` in it. The exemption list is not prose — a citation is
// excused only if the path is a KNOWN-DELETED artifact, which is data the witness guard already keeps
// (`testdata/witness-banned-phrases.txt` names `pkg/lang/jit.go` and the retired entry points precisely so a
// line may say "that file must stay deleted"). A stale pointer to a renamed test is not history, and a marker
// word elsewhere on a 300-word row must not licence it.
//
// `docs/roadmap-details.md` and `docs/adr/*` are deliberately out of scope: they are the measurement narrative
// and the decision record, they legitimately name files that no longer exist in sentences about those files
// being gone, and rewriting an accepted ADR to chase a rename destroys the record of why the rename happened.
// `integration/docs_citations_test.go` already polices `.gy` citations in the same files.
//
// Reads only — like every guard over a file outside the package holding the test, it depends on `make test`'s
// `-count=1` (ADR 0317) to run at all after an edit, and it writes nothing.
package lang

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// roadmapCitation matches `some/dir/file.ext` in the tracker, with an optional `::TestName` suffix — the
// form an Evidence cell uses to point at one case rather than a whole file.
var roadmapCitation = regexp.MustCompile("`((?:pkg|integration|cmd|tools|docs)/[A-Za-z0-9_./-]+\\.(?:go|json|gy|md|txt))(?:::([A-Za-z0-9_]+))?`")

// funcDecl finds `func TestName` (method receivers allowed) for a `path::TestName` citation.
func funcDecl(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^func(\s+\([^)]*\))?\s+` + regexp.QuoteMeta(name) + `\b`)
}

// deletedArtifacts is the witness ledger's own list of names that no longer exist in the tree. A citation of
// one of these is a claim about a deletion, and the witness guard is the guard for that claim; this one does
// not duplicate it and does not demand the file reappear.
func deletedArtifacts(t *testing.T, root string) map[string]bool {
	t.Helper()
	deleted := map[string]bool{}
	for _, line := range witnessListFile(t, filepath.Join(root, "pkg/lang", witnessBannedFile)) {
		if s := strings.TrimSpace(line); s != "" && !strings.HasPrefix(s, "#") {
			deleted[s] = true
		}
	}
	if len(deleted) == 0 {
		t.Fatal("the witness ledger is empty — this exemption list has no source")
	}
	return deleted
}

// checkRoadmapCitations walks the tracker and returns every citation that does not resolve. `row` is the row's
// leading ID cell when there is one, so the failure says which claim is broken.
func checkRoadmapCitations(t *testing.T, root, text string) []string {
	t.Helper()
	deleted := deletedArtifacts(t, root)
	var bad []string
	for _, line := range strings.Split(text, "\n") {
		id := ""
		if cells := strings.Split(line, "|"); len(cells) >= 3 {
			for _, cand := range []string{strings.TrimSpace(cells[1]), strings.TrimSpace(cells[2])} {
				if strings.HasPrefix(cand, "Gap ") || strings.HasPrefix(cand, "L11") || strings.HasPrefix(cand, "L") {
					id = cand // the ledger spells the ID first, the queue second
					break
				}
			}
		}
		if strings.Contains(line, "(planned)") {
			continue // a row may cite the file it intends to add; docs_citations_test.go owns that marker
		}
		for _, m := range roadmapCitation.FindAllStringSubmatch(line, -1) {
			path, test := m[1], m[2]
			if deleted[path] {
				continue
			}
			full, err := os.Stat(filepath.Join(root, path))
			if err != nil || full.IsDir() {
				bad = append(bad, citize(id, path, test, "no such file"))
				continue
			}
			if test == "" {
				continue
			}
			src, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				bad = append(bad, citize(id, path, test, "unreadable: "+err.Error()))
				continue
			}
			if !funcDecl(test).Match(src) {
				bad = append(bad, citize(id, path, test, "the file holds no func "+test))
			}
		}
	}
	return bad
}

func citize(id, path, test, why string) string {
	if id != "" {
		id = id + ": "
	}
	if test != "" {
		path = path + "::" + test
	}
	return id + "`" + path + "` — " + why
}

// TestTheRoadmapsCitationsResolve is the guard: the tracker cites files and cases that exist.
func TestTheRoadmapsCitationsResolve(t *testing.T) {
	root := repoRoot(t)
	text := string(mustReadArtifact(t, filepath.Join(root, "roadmap.md")))
	if bad := checkRoadmapCitations(t, root, text); len(bad) > 0 {
		t.Errorf("%d citation(s) in roadmap.md do not resolve:\n\t%s\nAn Evidence cell is what makes a status "+
			"cell mean something — cite the file and case that answer the claim today, and when a refactor moves "+
			"a case into another file the refactor edits the cell that cited it (roadmap Gap R.205)",
			len(bad), strings.Join(bad, "\n\t"))
	}
}

// TestTheCitationGuardCanFail keeps the guard honest (the harness's own rule): a citation to a file that is
// not there, and a `::TestName` the file does not hold, must both be reported. Written as table cases rather
// than by breaking the tracker, because the tracker must stay green.
func TestTheCitationGuardCanFail(t *testing.T) {
	root := repoRoot(t)
	for _, tc := range []struct {
		name string
		row  string
		want string
	}{
		{
			name: "a file that is not there",
			row:  "| Gap R.999 | some item | ✅ `DONE` | codegen | `0001` | `pkg/lang/no_such_test_file_test.go` | free text |",
			want: "no such file",
		},
		{
			name: "a case the cited file does not hold",
			row:  "| Gap R.998 | some item | ✅ `DONE` | codegen | `0001` | `pkg/lang/golden.go::TestTheRecordDoesNotLoadItself` | free text |",
			want: "the file holds no func",
		},
		{
			name: "a retired artifact is exempt, because the witness guard owns that claim",
			row: "| Gap R.997 | some item | ✅ `DONE` | docs | `0001` | `pkg/lang/jit.go` must stay deleted — it was " +
				"the engine's own file and is no longer in the tree | free text |",
			want: "",
		},
		{
			name: "a real file and a real case resolve",
			row:  "| Gap R.996 | some item | ✅ `DONE` | codegen | `0001` | `pkg/lang/container_methods_test.go::TestKeyErrorNamesTheKey` | free text |",
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := checkRoadmapCitations(t, root, tc.row)
			if tc.want == "" {
				if len(bad) != 0 {
					t.Fatalf("a citation that should resolve was reported: %v", bad)
				}
				return
			}
			if len(bad) != 1 || !strings.Contains(bad[0], tc.want) {
				t.Fatalf("the citation guard accepted %q; want a report containing %q, got %v", tc.row, tc.want, bad)
			}
			if !strings.Contains(bad[0], "Gap R.9") {
				t.Errorf("the report does not name the row it came from: %s", bad[0])
			}
		})
	}
}
