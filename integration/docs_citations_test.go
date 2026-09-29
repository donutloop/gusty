package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The record has to point at things that exist.
//
// roadmap.md and docs/ are how a later cycle reconstructs why the language is shaped as it is, and
// they cite programs as evidence: "stays pinned as programs/probe_x.gy", "byte-identical across
// backends (programs/y.gy)". Those citations rot — files get renamed, and a name written from memory
// can describe a program nobody ever created. Three claims of that kind were found in one pass, each
// describing "measured debt" with no file and no ledger row behind it. A claim about an artifact that
// does not exist is worse than no claim: it stops the next reader from looking.
//
// Two rules, both mechanical:
//
//  1. `programs/NAME.gy` must be a file in integration/programs, unless the citation is immediately
//     followed by `(planned)` — the marker for a Definition-of-Done program a roadmap item intends to
//     write, which is a promise about the future rather than a statement about the corpus.
//  2. Any other `NAME.gy` in the same documents must exist, or be plainly a prose file (a name with
//     no relative in the corpus, like `math.gy` in an import example). A *near-miss* — the name minus
//     or plus the `probe_` prefix, or an `s` at the end — is a citation that drifted from a rename and
//     fails with the real file suggested.
//
// _001_session_learnings.md is deliberately out of scope: it is narrative, and it is allowed to
// discuss a filename precisely in order to say it does not exist.

// recordRoot is the repository root as seen from this package's directory.
const recordRoot = ".."

var (
	programsCiteRe = regexp.MustCompile(`programs/([a-z0-9_]+\.gy)(\s*\(planned\))?`)
	bareCiteRe     = regexp.MustCompile(`(?:^|[^/\w.])([a-z0-9_]+\.gy)`)
	recordDocs     = []string{"roadmap.md", "README.md", "docs/language.md", "docs/operations.md"}
)

func recordFiles(t *testing.T) []string {
	t.Helper()
	dirs := []string{recordRoot, filepath.Join(recordRoot, "docs"), filepath.Join(recordRoot, "docs", "adr")}
	var out []string
	for _, d := range dirs {
		if d == recordRoot {
			for _, f := range recordDocs {
				out = append(out, filepath.Join(recordRoot, f))
			}
			continue
		}
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatalf("read %s: %v", d, err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				out = append(out, filepath.Join(d, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}

func corpusNames(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("programs"))
	if err != nil {
		t.Fatalf("read integration/programs: %v", err)
	}
	have := map[string]bool{}
	for _, e := range entries {
		have[e.Name()] = true
	}
	return have
}

// nearMiss returns the corpus file a drifted citation most likely meant to name.
func nearMiss(name string, have map[string]bool) string {
	cands := []string{
		"probe_" + strings.TrimPrefix(name, "probe_"),
		strings.TrimPrefix(name, "probe_"),
		strings.TrimSuffix(name, ".gy") + "s.gy",
		strings.TrimSuffix(strings.TrimPrefix(name, "probe_"), ".gy") + "s.gy",
		strings.TrimSuffix(name, ".gy") + "_1.gy",
	}
	seen := map[string]bool{}
	for _, c := range cands {
		if c == "" || c == name || seen[c] {
			continue
		}
		seen[c] = true
		if have[c] {
			return c
		}
	}
	return ""
}

func TestRecordCitationsResolveToRealPrograms(t *testing.T) {
	have := corpusNames(t)
	checked, problems := 0, []string{}
	for _, doc := range recordFiles(t) {
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		text := string(raw)
		for _, m := range programsCiteRe.FindAllStringSubmatch(text, -1) {
			name, planned := m[1], m[2] != ""
			checked++
			if planned || have[name] {
				continue
			}
			if alt := nearMiss(name, have); alt != "" {
				problems = append(problems, doc+": programs/"+name+" does not exist; the corpus has programs/"+alt+" (rename the citation, or mark it (planned) if it is a program yet to be written)")
				continue
			}
			problems = append(problems, doc+": programs/"+name+" is cited as evidence but is not in integration/programs (write it, or mark it (planned) if a roadmap item still owes it)")
		}
		for _, m := range bareCiteRe.FindAllStringSubmatch(text, -1) {
			name := m[1]
			if have[name] {
				continue
			}
			if strings.HasPrefix(name, "probe_") {
				// The `probe_` prefix is a claim about this corpus whatever the sentence around it.
				checked++
				if alt := nearMiss(name, have); alt != "" {
					problems = append(problems, doc+": "+name+" is not in the corpus; did you mean programs/"+alt+"?")
				} else {
					problems = append(problems, doc+": "+name+" is not in the corpus and has no relative in it (cite a program that exists, or write it)")
				}
				continue
			}
			// A bare name with no relative is prose about some other file — `import math` talks about
			// math.gy on disk, not about a corpus program — so only a near-miss counts as drift.
			if alt := nearMiss(name, have); alt != "" {
				checked++
				problems = append(problems, doc+": "+name+" is not in the corpus; did you mean programs/"+alt+"?")
			}
		}
	}
	if len(problems) > 0 {
		t.Fatalf("%d citation(s) in the record do not resolve (%d checked):\n  %s", len(problems), checked, strings.Join(problems, "\n  "))
	}
	if checked < 20 {
		t.Fatalf("only %d citations checked — the scanners stopped matching the corpus citations, so this test can no longer fail", checked)
	}
	t.Logf("%d corpus citations resolve", checked)
}
