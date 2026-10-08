// Command testshards runs one package's tests in several processes at once.
//
// The CI failure it closes is worth spelling out, because the fix looks like a workaround until you
// see what broke. `go test ./pkg/...` runs a package's cases *serially*: one binary, one case at a
// time. Almost every case in pkg/lang spends its time inside a subprocess — `llc-20` lowering the
// module the case compiled, `cc` linking it, `lli-20` executing it, `python3` answering the reference
// leg — so on a four-core runner the suite used one core, needed more than the ten minutes `go test`
// allows by default, and died with a timeout panic naming whichever case happened to be executing when
// the alarm went off (`TestTextPredicatesPrintAVerdict/not_verdict_false` is the one on record). Nothing
// was wrong with that case. The suite did not fit, and the run could not say so.
//
// The obvious fix — `t.Parallel()` on the cases — is not available to this suite, and the reason is
// worth writing down. The in-process runner takes fd 1 and fd 2 with dup2 for the duration of a run
// (captureFD, pkg/lang/jit_llvm.go), so two programs cannot execute at once inside one process; and a
// case that needs process-wide state of its own — os.Chdir for the import-from-cwd cases, t.Setenv for
// the oracle override — cannot coexist with anything, because Go runs a parallel test *beside* the
// serial ones rather than in a phase of its own. Sharding sidesteps all of it: every shard is its own
// process, so descriptors, working directory, environment and the compiler's process-wide scan tables
// are each shard's alone, and N shards use N cores.
//
// What makes splitting legal is determinism: a case's answer depends on its source and the pinned
// toolchain, never on which cases ran before it, so a shard may be a subset. The partition is sort +
// round-robin over the names `go test -list` reports: the same tree always yields the same shards, and every
// name lands in exactly one shard, because `-list` is the authority on what exists.
//
// One thing in the suite is NOT case-local, and finding it cost a cycle: the golden drift ledger
// (pkg/lang/golden.go, ADR 0302) is adjudicated from process-global state — the divergences this process
// reported and the sources it put a question to. A ledger row is a claim about a RUN, and a sharded run is N
// processes, so a source whose two witnesses land in different shards is judged by a fraction of the run's
// evidence: the shard that merely ran it reported the row PAID and failed the build, while the shard holding
// the divergence reported it owed. The partition moves whenever a case is added, so this is not a corner case
// — it is what happens the first time the suite grows. Each shard now writes its evidence to a file and this
// runner merges them and adjudicates each ledger ONCE, in pkg/lang, so the rules stay a single
// implementation (roadmap Gap R.199, ADR 0315; the tests are in main_test.go beside this file).
//
// The artifact-writing modes (GUSTY_GOLDEN_UPDATE rewriting the drift ledger, GUSTY_GOLDEN_MISSING
// collecting sources the record does not cover) are per-run ledgers and belong on ONE process — N shards
// would each write their own subset over the file. Use plain `go test` for those, or -shards 1; when either
// is set this runner leaves adjudication inside the shards, exactly as before.
//
// Usage:
//
//	go run ./tools/testshards -tags llvm20 -shards 4 ./pkg/...
//	go run ./tools/testshards -tags llvm20 -json ./pkg/...   # machine-readable summary on stdout
//	go run ./tools/testshards -tags llvm20 -list ./pkg/...   # print the partition, run nothing
//	go run ./tools/testshards -shards 1 ./pkg/lang/          # what plain `go test` would have done
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

func main() {
	var (
		tags    = flag.String("tags", "", "build tags handed to every `go test` (e.g. llvm20)")
		shards  = flag.Int("shards", 0, "processes per package (default: GOMAXPROCS)")
		timeout = flag.String("timeout", "10m", "per-shard `go test` timeout: one shard's cases, not the whole suite")
		jsonOut = flag.Bool("json", false, "print a machine-readable summary of the run on stdout")
		list    = flag.Bool("list", false, "print the partition and exit — nothing is compiled or run")
		verbose = flag.Bool("v", false, "ask each shard for verbose `go test` output")
	)
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		fmt.Fprintln(os.Stderr, "usage: testshards [-tags T] [-shards N] [-timeout D] [-json] [-list] [-v] ./pkg/...")
		os.Exit(2)
	}
	if *shards <= 0 {
		*shards = runtime.NumCPU()
	}
	if _, err := time.ParseDuration(*timeout); err != nil {
		fmt.Fprintf(os.Stderr, "testshards: -timeout %q is not a duration: %v\n", *timeout, err)
		os.Exit(2)
	}

	pkgs, err := listPackages(*tags, patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testshards:", err)
		os.Exit(2)
	}
	plan := make([]planFor, 0, len(pkgs))
	for _, p := range pkgs {
		names, err := listTests(*tags, p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "testshards:", err)
			os.Exit(2)
		}
		if len(names) == 0 {
			// A package with no test files is not a failure — `./...` matches the tools and the
			// command alongside the packages. It is reported rather than silent, because a package
			// that contributes nothing to a green run should be visible in that run's output, and a
			// renamed one must not vanish quietly. `go test -list` *failing* is the error above.
			fmt.Fprintf(os.Stderr, "testshards: %s has no tests to shard\n", p)
			continue
		}
		n := *shards
		if n > len(names) {
			n = len(names)
		}
		plan = append(plan, planFor{Pkg: p, Shards: partition(names, n)})
	}
	if *list {
		// The partition is a claim about coverage, so it is printable on its own: an agent can check
		// that a name is in exactly one shard before trusting a green run to mean anything.
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"shards_per_package": *shards, "plan": plan})
		return
	}

	// Warm the build cache once. N shards starting cold would compile the same package N times over and
	// contend for the same cache entries; one serial build makes the shards pure executions, and it
	// fails here — where the message is the compiler's — instead of N times over as N indistinguishable
	// shard failures.
	for _, p := range pkgs {
		if out, err := goCmd(*tags, "test", "-run", "TestNothingMatchesThisNameXXX", p); err != nil {
			fmt.Fprintf(os.Stderr, "testshards: %s does not build or run: %v\n%s\n", p, err, out)
			os.Exit(1)
		}
	}

	// The ledger-writing modes are per-run artifacts and belong on ONE process (the makefile says so, and
	// ADR 0313's header says why). With either set, the shards keep their own adjudication and this runner
	// does not hand it over — a sharded `GUSTY_GOLDEN_UPDATE=1` would otherwise N times over the file.
	reportDir := ""
	if os.Getenv("GUSTY_GOLDEN_UPDATE") == "" && os.Getenv("GUSTY_GOLDEN_MISSING") == "" {
		d, err := os.MkdirTemp("", "gusty-shard-evidence")
		if err != nil {
			fmt.Fprintf(os.Stderr, "testshards: cannot make the evidence directory: %v\n", err)
			os.Exit(1)
		}
		defer os.RemoveAll(d)
		reportDir = d
	}

	sum := runPlan(*tags, *shards, *timeout, *verbose, *jsonOut, reportDir, plan)
	// The drift adjudication runs ONCE for the whole run, over the evidence every shard left behind. It is
	// per-process state by construction (`askedAbout`, `divergenceSet` in pkg/lang/golden.go), so a shard
	// that judges a ledger row at all judges it by a fraction of the run's evidence — which reads a row whose
	// cases are split across shards as paid off, and can keep a new divergence out of the only file that
	// would have caught it. The split moves whenever a test file is added, so this is not a corner case: it
	// is what happens the first time the suite grows a case (roadmap Gap R.199, ADR 0315).
	for _, line := range adjudicate(sum) {
		fmt.Fprintln(os.Stderr, line)
		sum.OK = false
	}
	if !*jsonOut {
		for _, r := range sum.Results {
			fmt.Printf("%s shard %d/%d: %d test(s), %s — %s\n", r.Pkg, r.Index+1, r.Of, r.Tests, r.Duration, verdict(r))
		}
		fmt.Printf("testshards: %d shard(s) over %d package(s) in %s — %s\n", len(sum.Results), len(plan), sum.Elapsed, passFail(sum.OK))
	} else {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sum)
	}
	if !sum.OK {
		os.Exit(1)
	}
}

// planFor is the work one package will do, before any of it has been run.
type planFor struct {
	Pkg    string     `json:"pkg"`
	Shards []shardFor `json:"shards"`
}

type shardFor struct {
	Index int      `json:"index"`
	Tests int      `json:"tests"`
	Names []string `json:"names,omitempty"`
}

// resultFor is what one shard did, in the shape an agent reads.
type resultFor struct {
	Pkg      string `json:"pkg"`
	Index    int    `json:"index"`
	Of       int    `json:"of"`
	Tests    int    `json:"tests"`
	OK       bool   `json:"ok"`
	Duration string `json:"duration"`
	Err      string `json:"error,omitempty"`
	// Evidence is the file this shard wrote its golden drift evidence to, empty when the run is in a
	// ledger-writing mode that keeps adjudication in the shard (roadmap Gap R.199, ADR 0315).
	Evidence string `json:"evidence,omitempty"`
}

// summary is the run: the plan it executed and what each shard said. `-json` prints exactly this.
type summary struct {
	OK      bool        `json:"ok"`
	Shards  int         `json:"shards_per_package"`
	Tags    string      `json:"tags,omitempty"`
	Timeout string      `json:"per_shard_timeout"`
	Elapsed string      `json:"elapsed"`
	Results []resultFor `json:"results"`
}

func runPlan(tags string, shards int, timeout string, verbose, jsonOut bool, reportDir string, plan []planFor) summary {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		rows []resultFor
	)
	start := time.Now()
	for _, p := range plan {
		for _, sh := range p.Shards {
			wg.Add(1)
			go func(p planFor, sh shardFor, n int) {
				defer wg.Done()
				r := runShard(tags, timeout, verbose, jsonOut, reportDir, p.Pkg, sh, n)
				mu.Lock()
				rows = append(rows, r)
				mu.Unlock()
			}(p, sh, len(p.Shards))
		}
	}
	wg.Wait()
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Pkg != rows[j].Pkg {
			return rows[i].Pkg < rows[j].Pkg
		}
		return rows[i].Index < rows[j].Index
	})
	ok := true
	for _, r := range rows {
		ok = ok && r.OK
	}
	return summary{OK: ok, Shards: shards, Tags: tags, Timeout: timeout, Elapsed: time.Since(start).Round(time.Millisecond).String(), Results: rows}
}

// runShard is one `go test -run '^(A|B|…)$'` for one package, with its output attributed to the shard
// that produced it. A run that dies at its timeout has to leave a log saying which shard was still
// talking when it stopped — that attribution is the piece the CI panic could not provide.
func runShard(tags, timeout string, verbose, quiet bool, reportDir, pkg string, sh shardFor, n int) resultFor {
	args := []string{"test", "-count=1", "-timeout", timeout}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	if verbose {
		args = append(args, "-v")
	}
	args = append(args, "-run", runRegex(sh.Names), pkg)
	var report string
	cmd := exec.Command("go", args...)
	cmd.Env = os.Environ()
	if reportDir != "" {
		cmd.Env = append(cmd.Env, "GUSTY_GOLDEN_REPORT="+filepath.Join(reportDir, evidenceName(pkg, sh.Index)))
		report = filepath.Join(reportDir, evidenceName(pkg, sh.Index))
	}
	t0 := time.Now()
	out, err := cmd.CombinedOutput()
	r := resultFor{Pkg: pkg, Index: sh.Index, Of: n, Tests: sh.Tests, Duration: time.Since(t0).Round(time.Millisecond).String(), Evidence: report}
	if err != nil {
		r.Err = strings.TrimSpace(string(out))
	} else {
		r.OK = true
	}
	if !quiet {
		prefix := fmt.Sprintf("[%s shard %d/%d] ", filepath.Base(pkg), sh.Index+1, n)
		os.Stdout.Write(labelled(out, prefix))
	}
	return r
}

// labelled prefixes every line a shard printed, keeping each shard identifiable in one interleaved log.
func labelled(out []byte, prefix string) []byte {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// evidenceName is the file one shard writes its golden evidence to. The package path is folded into the
// name so two packages' shards cannot collide on an index, and the index is zero-padded so a directory
// listing reads in shard order.
func evidenceName(pkg string, index int) string {
	safe := strings.NewReplacer("/", "-", "\\", "-", ".", "-", ":", "-").Replace(pkg)
	return fmt.Sprintf("%s-%03d.golden-evidence.json", safe, index)
}

// adjudicate merges the shards' golden evidence per ledger and applies the drift ratchet ONCE for the
// whole run, in the package that owns the rules (lang.CheckDriftAgainst) rather than in a copy of them
// here.
//
// Why this belongs to the runner and not to the shards: `askedAbout` and `divergenceSet` are process-local
// (pkg/lang/golden.go), and the round-robin partition moves whenever a test is added or renamed. A ledger
// row whose two witnesses land in different shards is then judged by one shard's evidence, which reads a
// still-owed row as "never asked" (new debt hides) and a row whose cases merely split as "paid off" (the
// run fails over nothing). Union first: a divergence any shard saw is the run's, and a source any shard
// asked counts as asked (roadmap Gap R.199, ADR 0315).
//
// A shard that failed reddens the run anyway, and its evidence may be truncated mid-run, so adjudication
// is skipped when anything failed — the shard's own failure is the message, and a second one assembled from
// partial evidence would send someone chasing the wrong thing.
func adjudicate(sum summary) []string {
	byShard := map[string][]lang.GoldenEvidence{}
	for _, r := range sum.Results {
		if !r.OK {
			return nil
		}
		if r.Evidence == "" {
			continue
		}
		raw, err := os.ReadFile(r.Evidence)
		if err != nil {
			if os.IsNotExist(err) {
				// A package with no ledger writes nothing; its shard still adjudicated its own way.
				continue
			}
			return []string{fmt.Sprintf("testshards: cannot read %s shard %d/%d golden evidence: %v", r.Pkg, r.Index+1, r.Of, err)}
		}
		var e lang.GoldenEvidence
		if err := json.Unmarshal(raw, &e); err != nil {
			return []string{fmt.Sprintf("testshards: %s shard %d/%d golden evidence is not JSON: %v", r.Pkg, r.Index+1, r.Of, err)}
		}
		byShard[e.Ledger] = append(byShard[e.Ledger], e)
	}
	ledgers := make([]string, 0, len(byShard))
	for l := range byShard {
		ledgers = append(ledgers, l)
	}
	sort.Strings(ledgers)
	var problems []string
	for _, l := range ledgers {
		sets := byShard[l]
		merged := lang.MergeGoldenEvidence(sets)
		if report := lang.CheckDriftAgainst(merged, l); report != "" {
			problems = append(problems, fmt.Sprintf("testshards: %s adjudicated over %d shard(s) of merged evidence:%s", l, len(sets), report))
			continue
		}
		fmt.Printf("testshards: %s adjudicated over %d shard(s) — %d divergence(s) over %d source(s) asked, all on the ledger\n",
			l, len(sets), len(merged.Divergences), len(merged.Asked))
	}
	return problems
}

// runRegex turns test names into the anchored alternation `-run` wants. Names are Go identifiers, so
// nothing needs escaping; the anchors are what keeps a shard from running a case whose name merely
// begins with one of its own — `TestFoo` matching `TestFooBar` would cover one case twice and another
// not at all, which is exactly the silent hole a harness must not be able to open.
func runRegex(names []string) string {
	return "^(" + strings.Join(names, "|") + ")$"
}

// partition deals sorted names round-robin into n shards. Sorting first makes the split reproducible:
// `-list` reports in file order, which moves whenever a test file is renamed, and a shard boundary that
// drifts between runs turns every bisect into a new experiment. Dealing rather than slicing keeps two
// expensive neighbours in one table from landing in the same shard and becoming the long pole.
func partition(names []string, n int) []shardFor {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	if n < 1 {
		n = 1
	}
	buckets := make([][]string, n)
	for i, nm := range sorted {
		buckets[i%n] = append(buckets[i%n], nm)
	}
	out := make([]shardFor, 0, n)
	for i, b := range buckets {
		if len(b) == 0 {
			continue
		}
		out = append(out, shardFor{Index: i, Tests: len(b), Names: b})
	}
	return out
}

func listPackages(tags string, patterns []string) ([]string, error) {
	out, err := goCmd(tags, "list", patterns...)
	if err != nil {
		return nil, fmt.Errorf("go list %s: %v\n%s", strings.Join(patterns, " "), err, out)
	}
	var pkgs []string
	for _, line := range strings.Split(string(out), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "_") {
			continue
		}
		pkgs = append(pkgs, s)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages match %s", strings.Join(patterns, " "))
	}
	return pkgs, nil
}

// listTests asks `go test -list` which cases the package holds. It builds the test binary, so this is
// also where a compile failure surfaces: once, in the compiler's own words, rather than N times over as
// N indistinguishable shard failures.
func listTests(tags, pkg string) ([]string, error) {
	out, err := goCmd(tags, "test", "-list", ".*", pkg)
	if err != nil {
		return nil, fmt.Errorf("go test -list %s: %v\n%s", pkg, err, out)
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		s := strings.TrimSpace(line)
		if !isTestName(s) {
			continue
		}
		names = append(names, s)
	}
	return names, nil
}

// isTestName says whether a line `go test -list` printed is the name of a Test function.
//
// Two things have to be rejected. The prose lines around the names ("ok  \tgithub.com/x/y", a build
// failure's "# pkg [setup failed]") would become cases that never run; and Benchmark/Example names,
// which live in the same files but are judged by -bench and by the example runner — putting one in a
// shard's -run means that shard ran nothing while reporting itself green, which is the hole a harness
// must not be able to open.
func isTestName(s string) bool {
	if !strings.HasPrefix(s, "Test") || s == "" {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// goCmd runs one `go` subcommand and hands back its combined output. The tags go through for every verb
// so a shard never compiles a package differently from the way its cases were written to build.
func goCmd(tags, verb string, args ...string) ([]byte, error) {
	argv := []string{verb}
	if tags != "" {
		argv = append(argv, "-tags", tags)
	}
	argv = append(argv, args...)
	cmd := exec.Command("go", argv...)
	cmd.Env = os.Environ()
	var b strings.Builder
	cmd.Stdout = &b
	cmd.Stderr = &b
	err := cmd.Run()
	return []byte(b.String()), err
}

func verdict(r resultFor) string {
	if r.OK {
		return "ok"
	}
	if r.Err == "" {
		return "FAILED"
	}
	return "FAILED: " + firstLine(r.Err)
}

func passFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
