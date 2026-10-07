package lang

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The witness-claim guard (Gap R.190, ADR 0308).
//
// ADR 0302 deleted the AST interpreter, and with it the second engine every parity claim in this
// repository used to be corroborated by. The claims did not become false — they changed kind, from
// engine-vs-engine to *compiled-vs-the-record* and *compiled-vs-CPython*. What did become false is any
// sentence a reader could only satisfy by running the interpreter: a test comment that says "checked on
// both engines", a flag description that advertises an interpreter leg, a tracker row that reports
// "three engines" as today's measurement. A reader — human or agent — that believes one of those goes
// looking for an engine that does not exist, and an agent that cannot run the thing a claim names
// cannot check the claim at all.
//
// So the retirement is only finished when the *language of the record* is enforced, and this file is
// that enforcement. It is a ratchet in the same family as the golden drift ledgers: the phrases in
// testdata/witness-banned-phrases.txt are banned anywhere a claim is made (test comments, CLI help
// text, the tracker, the docs), and a line is exempt only when it carries a marker from
// testdata/witness-history-markers.txt saying it reports history — because the answers the retired
// engine gave are still evidence about the language, and rewriting those measurements would trade a
// stale sentence for a fabricated one.
//
// The two lists are data, not code, for the same reason the drift ledgers are: a cycle is allowed to
// restate more of the corpus, and only the guard's own file may never be mass-edited (it is skipped
// below, and its phrases live outside the source so a mechanical restatement cannot eat them).

const witnessBannedFile = "testdata/witness-banned-phrases.txt"
const witnessHistoryFile = "testdata/witness-history-markers.txt"

// witnessListFile reads one entry per line, dropping blanks and `#` comments. A list that fails to
// parse is a fatal, not a skip: an empty list would silently make the guard pass.
func witnessListFile(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v — the witness guard has no list to enforce, and a guard that enforces nothing is the failure this file exists to catch", path, err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s is empty: the witness guard would pass without checking anything", path)
	}
	return out
}

func witnessPatterns(t *testing.T, path string) []*regexp.Regexp {
	t.Helper()
	var out []*regexp.Regexp
	for _, entry := range witnessListFile(t, path) {
		re, err := regexp.Compile("(?i)" + entry)
		if err != nil {
			t.Fatalf("%s: %q is not a usable pattern: %v", path, entry, err)
		}
		out = append(out, re)
	}
	return out
}

// repoRoot walks up from the package directory to the module root, so the guard reads the same tree
// `go test ./...` compiles rather than a copy of it.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("pwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("no go.mod above pkg/lang — the witness guard has nothing to scan")
	return ""
}

// witnessSurfaces are the files the guard reads: the tests that make behavioural claims, the CLI that
// makes interface claims, the conformance case registry that makes the matrix's claims, and the
// documents an agent reads instead of the source. Production comments are deliberately not scanned —
// they are the design narrative, written while both engines ran, and the history markers would
// licence almost all of them.
func witnessSurfaces(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var files []string
	scan := func(dir, suffix string) {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
				continue
			}
			if e.Name() == "witness_claim_test.go" {
				continue // the guard's own file, by name, never by content
			}
			files = append(files, filepath.Join(root, dir, e.Name()))
		}
	}
	scan("pkg/lang", "_test.go")
	scan("integration", "_test.go")
	scan("cmd/gustyc", ".go")
	// The conformance case registry is a claim surface, not a design narrative: its comments say what
	// each registered program asserts TODAY, and its `reason:` strings are copied verbatim into
	// `conformance-matrix.json`, which is machine payload an agent reads (ADR 0186's promotion rule
	// turns one of these strings into the ledger's wording).
	files = append(files, filepath.Join(root, "integration", "conformance_cases.go"))
	// The agent-facing documents: an agent reads these instead of the source, so a claim in one of
	// them is an interface claim. `docs/roadmap-details.md` is deliberately NOT here — it is the
	// measurement narrative, and the history it preserves is the point of it.
	docs := []string{
		"roadmap.md", "README.md", "AGENTS.md",
		filepath.Join("docs", "operations.md"),
		filepath.Join("docs", "language.md"),
		filepath.Join("docs", "shared-lowering-spec.md"),
		filepath.Join("docs", "benchmark.md"),
		filepath.Join("docs", "abi.md"),
		filepath.Join("docs", "agentic", "ast-ir-schema.md"),
	}
	for _, doc := range docs {
		files = append(files, filepath.Join(root, doc))
	}
	return files
}

func TestNoClaimNamesAnEngineNobodyRuns(t *testing.T) {
	root := repoRoot(t)
	patterns := witnessPatterns(t, filepath.Join(root, "pkg/lang", witnessBannedFile))
	phrases := witnessListFile(t, filepath.Join(root, "pkg/lang", witnessBannedFile))
	history := witnessPatterns(t, filepath.Join(root, "pkg/lang", witnessHistoryFile))

	var offenders []string
	for _, file := range witnessSurfaces(t) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		rel := strings.TrimPrefix(file, root+"/")
		for i, line := range strings.Split(string(data), "\n") {
			historical := false
			for _, re := range history {
				if re.MatchString(line) {
					historical = true
					break
				}
			}
			if historical {
				continue
			}
			for bi, re := range patterns {
				if re.MatchString(line) {
					trimmed := strings.TrimSpace(line)
					if len(trimmed) > 100 {
						trimmed = trimmed[:100] + "…"
					}
					offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+"  ["+phrases[bi]+"]  "+trimmed)
					break
				}
			}
		}
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("%d claims name an engine that no longer runs — restate each as the record leg, the reference leg, or both legs (AGENTS.md § One execution backend; ADR 0308):\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestTheCLINeverAdvertisesAnInterpreterLeg reads the flag set itself: an agent discovers this CLI by
// its help text, so a description that promises an interpreter run is the most expensive form of the
// stale claim. A description may mention the interpreter only to say it is gone.
func TestTheCLINeverAdvertisesAnInterpreterLeg(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "cmd", "gustyc", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	help := regexp.MustCompile(`fs\.(Bool|String|Int|Float64)\("[^"]+", [^,]+, ("(?:[^"\\]|\\.)*")`)
	for _, m := range help.FindAllStringSubmatch(string(data), -1) {
		desc := strings.Trim(m[2], `"`)
		low := strings.ToLower(desc)
		if !strings.Contains(low, "interpreter") && !strings.Contains(low, "--interp") {
			continue
		}
		if strings.Contains(low, "retir") {
			continue
		}
		t.Errorf("a flag description advertises the retired engine: %s", desc)
	}
}

// TestOneBackendIsWhatTheMachinePayloadNames closes the other half of the same contract: the deleted
// engine stays deleted, and the machine surfaces name exactly one backend.
func TestOneBackendIsWhatTheMachinePayloadNames(t *testing.T) {
	if BackendName != "aot" {
		t.Errorf("the one backend names itself %q; every machine payload says %q", BackendName, "aot")
	}
	if got := (GCStats{Backend: BackendName}); got.Backend != "aot" {
		t.Errorf("the collector report names backend %q", got.Backend)
	}
	if _, err := os.Stat(filepath.Join(repoRoot(t), "pkg", "lang", "jit.go")); !os.IsNotExist(err) {
		t.Errorf("pkg/lang/jit.go is back: the AST interpreter was deleted by ADR 0302 and stays deleted")
	}
}
