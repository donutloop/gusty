package lang

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The interpreter golden: what the retired tree-walking engine answered, kept as an expectation.
//
// ADR 0302 removed the AST interpreter, and with it the oracle a large part of this suite had been
// checking the language against — not because its answers were wrong, but because *nothing else was
// measuring them*: `xs[0] + 1` over a built container, `str(3.5)`, `sorted`, a class's dunder, a
// comprehension's element, an exception that escaped a `try`. Deleting those cases would have traded
// measured coverage for a green build, which is the trade this repo exists to refuse.
//
// So the answers were recorded first. `testdata/interpreter-golden.json` holds, for every source the
// suite evaluated at the last commit that had the engine, what the program printed, what value it
// handed back, what dynamic type that value had, and — when the program failed — which exception
// escaped and with what message. A case that used to ask the interpreter now compiles the same
// source, runs it, and compares against that record.
//
// Three rules keep the golden honest rather than comfortable:
//
//   - The compiled backend has to *run* the source. A snippet the compiler refuses is not a pass;
//     it is a divergence, with the refusal recorded beside it.
//   - A source with no record fails its case. Coverage cannot be dropped by quietly deleting an
//     entry from the golden file: the case that asked it then has nothing to compare against and
//     says so, in red.
//   - Where a case states an expectation of its own, that expectation is checked against the record
//     too, so a test cannot drift by moving its `want` away from what was recorded.
//
// The golden is the reference-adjacent oracle this repo already uses (its conformance matrix checks
// against CPython for the same reason): a deleted engine's agreement proves nothing, and neither
// does the compiler agreeing with itself.

// goldenEntry is one recorded answer.
type goldenEntry struct {
	Repr    string `json:"repr,omitempty"`
	Type    string `json:"type,omitempty"`
	Int     int64  `json:"int"`
	HasInt  bool   `json:"hasInt"`
	Err     string `json:"err,omitempty"`
	ExnType string `json:"exnType,omitempty"`
	ExnMsg  string `json:"exnMsg,omitempty"`
	// FrontEnd marks a source the lexer, parser or checker refused before any engine could run it.
	// That front end is the language's, not the retired engine's, so the compiler inherits the
	// refusal — and the case pins that it still refuses.
	FrontEnd bool   `json:"frontEnd,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	HasStd   bool   `json:"hasStdout,omitempty"`
}

type goldenFile struct {
	Meta    map[string]any         `json:"meta"`
	Entries map[string]goldenEntry `json:"entries"`
}

var (
	goldenOnce  sync.Once
	goldenData  map[string]goldenEntry
	goldenLoadE error
)

func loadGolden(t errorReporter) map[string]goldenEntry {
	goldenOnce.Do(func() {
		path := filepath.Join("testdata", "interpreter-golden.json")
		if _, statErr := os.Stat(path); statErr != nil {
			// The runner started somewhere the fixture is not. That is an environment fault, and
			// naming it beats reporting 2900 sources as "no recorded expectation".
			goldenLoadE = fmt.Errorf("interpreter golden not found at %s (run the suite from the package directory): %w", path, statErr)
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			goldenLoadE = fmt.Errorf("read interpreter golden: %w", err)
			return
		}
		var f goldenFile
		if err := json.Unmarshal(raw, &f); err != nil {
			goldenLoadE = fmt.Errorf("parse interpreter golden: %w", err)
			return
		}
		if len(f.Entries) == 0 {
			goldenLoadE = fmt.Errorf("interpreter golden at %s has no entries", path)
			return
		}
		goldenData = f.Entries
	})
	if goldenLoadE != nil {
		fatalf(t, "%v", goldenLoadE)
	}
	return goldenData
}

// errorReporter lets the golden helpers serve *testing.T, *testing.B and plain helper code.
type errorReporter interface{}

func fatalf(t errorReporter, format string, args ...any) {
	if r, ok := t.(interface{ Fatalf(string, ...any) }); ok {
		r.Fatalf(format, args...)
	}
	panic(fmt.Sprintf(format, args...))
}

// missingSources collects the sources a case asked about that the record does not hold, so one run
// can list them all and they can be put through the recorder. It is a diagnostic aid for the
// recording cycle, not a way to make a gap pass: until they are in the file, their cases fail.
var missingSources sync.Map

// askedAbout records which sources this run put a question to. It is what lets the paid-debt half
// of the drift check work in a partial run: a ledger row whose case never ran is not a paid debt,
// it is simply absent, and telling developers their `-run` subset fixed 300 bugs would make the
// check useless by lunchtime. A full run sees every row, and then the ratchet bites.
var askedAbout sync.Map

func goldenLookup(t errorReporter, src string) goldenEntry {
	askedAbout.Store(src, true)
	e, ok := loadGolden(t)[src]
	if !ok {
		missingSources.Store(src, true)
		fatalf(t, "no recorded interpreter expectation for source:\n%s\n"+
			"The retired engine's answer is the expectation these cases check (ADR 0302). A source with no record "+
			"means the case never ran while the engine existed, or its record was deleted: re-record it, or write "+
			"the compiled-backend expectation out in full.", src)
	}
	return e
}

// --- the divergence ledger -------------------------------------------------------------------
//
// The record and the compiled backend do not agree about every source yet. Where they disagree —
// a value the compiler prints as `0` where the engine answered `cba`, a snippet the compiler
// cannot build at all — the case neither fails nor passes: it is *recorded* as a divergence and
// skipped with the reason in its message, and TestInterpreterGoldenDrift then holds that set
// against testdata/interpreter-golden-drift.json.
//
// That ledger is the point of the arrangement:
//
//   - A NEW divergence fails the drift test, so a regression cannot be smuggled in as a skip.
//   - A divergence that gets FIXED also fails it, with instructions to shrink the list — so the
//     debt cannot quietly become permanent either.
//   - Every row sits in one file, in one shape, with the owed answer and the delivered answer side
//     by side. That file is the compiler's work list, and it is machine-readable, which is what
//     the roadmap rows cite.
//
// It is not a pin. The record keeps the answer the language owes; a skipped case still carries it.

// Divergence is one source where the compiled backend's answer moved away from the record.
type Divergence struct {
	Source   string `json:"source"`
	Reason   string `json:"reason"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

var (
	divergenceMu  sync.Mutex
	divergenceSet = map[string]Divergence{}
)

func noteDivergence(t errorReporter, src, reason, expected, actual string) {
	divergenceMu.Lock()
	divergenceSet[src] = Divergence{Source: src, Reason: reason, Expected: expected, Actual: actual}
	divergenceMu.Unlock()
	if s, ok := t.(interface{ Skipf(string, ...any) }); ok {
		s.Skipf("ADR 0302 divergence, recorded in testdata/interpreter-golden-drift.json: %s — owed %s, the compiled backend gave %s",
			reason, quoteOr(expected), quoteOr(actual))
		return
	}
	panic("golden divergence outside a test: " + reason + " owed " + expected + " got " + actual)
}

// Divergences is the ledger's contents, sorted by source. TestMain reads it after every case has
// run; the ledger file it is compared against is the machine-readable surface an agent or a script
// consumes (`jq . testdata/interpreter-golden-drift.json`).
func Divergences() []Divergence {
	divergenceMu.Lock()
	defer divergenceMu.Unlock()
	out := make([]Divergence, 0, len(divergenceSet))
	for _, d := range divergenceSet {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// driftPath is where the ledger lives.
const driftPath = "testdata/interpreter-golden-drift.json"

// checkDrift holds the run's divergences against the ledger. Both directions are failures: an
// unlisted divergence means new debt arrived unannounced, and a listed divergence that did not
// happen means debt was paid and the ledger still charges for it.
func checkDrift() string {
	got := Divergences()
	want, err := readDriftLedger()
	if err != nil {
		return "interpreter golden drift ledger unreadable: " + err.Error()
	}
	var problems []string
	seen := map[string]bool{}
	for _, d := range got {
		seen[d.Source] = true
		w, ok := want[d.Source]
		if !ok {
			problems = append(problems, fmt.Sprintf("NEW divergence (not in %s): %q — %s; owed %s, got %s. Every divergence has to be a row in the ledger and a row in roadmap.md.",
				driftPath, d.Source, d.Reason, quoteOr(d.Expected), quoteOr(d.Actual)))
			continue
		}
		if w.Reason != d.Reason || w.Expected != d.Expected {
			problems = append(problems, fmt.Sprintf("divergence changed shape for %q: ledger says %s / owed %s, this run says %s / owed %s. Re-record the ledger if the record itself moved.",
				d.Source, w.Reason, quoteOr(w.Expected), d.Reason, quoteOr(d.Expected)))
		}
	}
	for src, w := range want {
		if seen[src] {
			continue
		}
		if _, asked := askedAbout.Load(src); !asked {
			// The case that asks about this source did not run (a -run subset, a skipped file). The
			// row stays as it is; a full run is where paying it gets noticed.
			continue
		}
		problems = append(problems, fmt.Sprintf("PAID debt still on the ledger: %q (%s) now agrees with the record. Remove the row from %s — run with GUSTY_GOLDEN_UPDATE=1 to rewrite it.",
			src, w.Reason, driftPath))
	}
	if len(problems) == 0 {
		return ""
	}
	sort.Strings(problems)
	return "\n=== interpreter golden drift (ADR 0302) ===\n" + strings.Join(problems, "\n") +
		fmt.Sprintf("\n%d divergence(s) reported by this run, %d on the ledger.\n", len(got), len(want))
}

func readDriftLedger() (map[string]Divergence, error) {
	raw, err := os.ReadFile(driftPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Divergence{}, nil
		}
		return nil, err
	}
	var rows []Divergence
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	m := make(map[string]Divergence, len(rows))
	for _, r := range rows {
		m[r.Source] = r
	}
	return m, nil
}

func writeDriftLedger(rows []Divergence) error {
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(driftPath, append(body, '\n'), 0o644)
}

func quoteOr(s string) string {
	if s == "" {
		return "(no answer)"
	}
	return fmt.Sprintf("%q", s)
}

// --- the checks ------------------------------------------------------------------------------

// runRecorded runs one source through the one backend and separates the two ways it can fail to
// produce an answer: the compiler could not build it (`hard`), or the program itself trapped
// (`trapped`, with the report the target wrote to its diagnostics).
func runRecorded(src string) (res *JITResult, trapped bool, hard error) {
	r, err := RunSnippet(src)
	if err != nil {
		return nil, false, err
	}
	return r, r.Code != 0, nil
}

// goldenOutcome is what checking one source against the record produced.
type goldenOutcome struct {
	// res is the compiled run, when there was one worth comparing.
	res *JITResult
	// refused says the record expected a refusal and the compiler refused too: the program never
	// ran, and the case passes on the strength of the shared front end's sentence.
	refused bool
}

// goldenRun is the one funnel: run the source, hold it against the record, and either hand back the
// run, or hand back the refusal the record asked for, or leave the disagreement on the ledger and
// skip the case that asked.
//
// A nil result with refused=false means the case was skipped as a divergence — by then the test has
// already been ended by Skipf, so a caller never sees that combination in a live run.
func goldenRun(t errorReporter, src string) goldenOutcome {
	want := goldenLookup(t, src)
	res, trapped, hard := runRecorded(src)
	if hard != nil {
		// The front end — lexer, parser, checker — is not the engine's or the compiler's, it is the
		// language's, and the record says these sources were refused there: `a = 1;;b = 2`, an
		// unawaited coroutine, a function called with the wrong number of arguments. The compiler
		// inheriting that refusal is the contract, so it is what this checks, sentence included.
		if want.FrontEnd {
			return goldenOutcome{refused: true}
		}
		noteDivergence(t, src, "the compiled backend cannot build a program the engine ran", wantSummary(want), hard.Error())
		return goldenOutcome{}
	}
	if want.FrontEnd {
		noteDivergence(t, src, "the compiler accepted a program the shared front end refuses", "refused: "+want.Err, "compiled and ran")
		return goldenOutcome{}
	}
	if trapped && want.ExnType != "" {
		_, c, m := ParseTrapReport(res.Stderr)
		if c != "" && (!strings.EqualFold(c, want.ExnType) || (want.ExnMsg != "" && m != "" && m != want.ExnMsg)) {
			noteDivergence(t, src, "the trap's class or message differs from the record", want.ExnType+": "+want.ExnMsg, c+": "+m)
			return goldenOutcome{}
		}
	}
	if trapped && want.Err == "" {
		noteDivergence(t, src, "the program trapped where the record has an answer", wantSummary(want), "trapped: "+trapLine(res))
		return goldenOutcome{}
	}
	if !trapped && want.Err != "" {
		noteDivergence(t, src, "the program answered where the record says it traps", "trap "+want.Err, "answered "+quoteOr(resultRepr(res)))
		return goldenOutcome{}
	}
	if reason, expected, actual, bad := goldenCompare(res, want); bad {
		noteDivergence(t, src, reason, expected, actual)
		return goldenOutcome{}
	}
	return goldenOutcome{res: res}
}

// goldenCompare checks one compiled run against one record. It returns the reason, the owed
// answer and the delivered answer when the record is not satisfied.
func goldenCompare(res *JITResult, want goldenEntry) (reason, expected, actual string, bad bool) {
	if want.HasStd && trimNL(res.Output) != trimNL(want.Stdout) {
		return "stdout differs from the record", want.Stdout, res.Output, true
	}
	if want.Repr != "" {
		got := resultRepr(res)
		// A snippet whose final expression is a call that hands back the void prints nothing and
		// echoes nothing — which is what the REPL did with it then, and does with it now.
		if got == "" && want.Repr == "None" {
			return "", "", "", false
		}
		if got != want.Repr {
			return "the echoed value differs from the record", want.Repr, got, true
		}
	}
	return "", "", "", false
}

// evalGolden is the golden-backed stand-in for the retired EvalExpr: it runs src through the one
// backend and checks the answer against the record. The signature is the old one on purpose — a
// case keeps its shape when the engine under it changes.
func evalGolden(t errorReporter, src string) (int64, []Diagnostic, error) {
	want := goldenLookup(t, src)
	out := goldenRun(t, src)
	if out.res == nil {
		if out.refused {
			return 0, nil, recordedTrap(want, out.res)
		}
		return 0, nil, errGoldenSkipped
	}
	res := out.res
	if res.Code != 0 {
		// Agreement about a failure: hand back the recorded class and sentence, so a case that asks
		// which exception escaped can still ask.
		return 0, nil, recordedTrap(want, out.res)
	}
	return want.Int, nil, nil
}

// errGoldenSkipped marks the path where the case was skipped as a divergence; a caller that only
// checks `err != nil` never sees it, because the skip already ended the case.
var errGoldenSkipped = fmt.Errorf("ADR 0302: this source is a recorded divergence (see testdata/interpreter-golden-drift.json)")

// runGoldenStdout is the golden-backed stand-in for the retired InterpreterRun: compile, run, and
// compare what the program printed against the record.
func runGoldenStdout(t errorReporter, src string) (string, error) {
	want := goldenLookup(t, src)
	out := goldenRun(t, src)
	if out.res == nil {
		if out.refused {
			return "", recordedTrap(want, out.res)
		}
		return "", errGoldenSkipped
	}
	res := out.res
	if res.Code != 0 {
		return res.Output, recordedTrap(want, res)
	}
	return res.Output, nil
}

// goldenStdout is a program's recorded stdout, for the cases that assert on the whole transcript.
func goldenStdout(t errorReporter, src string) string {
	return goldenRun(t, src).stdout()
}

// goldenRepr is the REPL's answer for one source: what the compiled backend reports the final
// expression's value is, checked against what the interpreter used to echo.
func goldenRepr(t errorReporter, src string) string {
	out := goldenRun(t, src)
	return resultRepr(out.res)
}

// goldenType is the dynamic type name the record gives for a source. The compiled backend's own
// answer is compared where it is willing to name a family; where the echo can only say `object`
// the record stands, and the gap is the echo's to close (roadmap L13.2).
func goldenType(t errorReporter, src string) string {
	want := goldenLookup(t, src)
	out := goldenRun(t, src)
	if out.res == nil {
		return ""
	}
	res := out.res
	if res.Result != nil && want.Type != "" && res.Result.Kind != "object" && !sameTypeName(want.Type, res.Result.Kind) {
		noteDivergence(t, src, "the reported type differs from the record", want.Type, res.Result.Kind)
		return ""
	}
	return want.Type
}

// goldenStdoutIs asserts what a program printed: the compiled run's stdout must match the record,
// and the record must say `want`. Programs that print are how a case asks about a value the
// compiler holds in a register rather than in a Go variable the test could reach into — which is
// every value, now.
func goldenStdoutIs(t errorReporter, src, want string) {
	rec := goldenLookup(t, src)
	if !rec.HasStd {
		fatalf(t, "the record for %s holds no stdout — the case asks what the program printed, so record it printed", src)
	}
	if trimNL(rec.Stdout) != trimNL(want) {
		fatalf(t, "the record says %s prints %q, but the case expects %q — the expectation moved away from what was recorded; if the reference agrees, re-record the source", src, rec.Stdout, want)
	}
	got, err := runGoldenStdout(t, src)
	if err != nil && err != errGoldenSkipped {
		fatalf(t, "%v", err)
	}
	if got != "" && trimNL(got) != trimNL(want) {
		fatalf(t, "compiled stdout %q, want %q", got, want)
	}
}

// goldenPrints is goldenStdoutIs for the common one-line case.
func goldenPrints(t errorReporter, src, want string) { goldenStdoutIs(t, src, want+"\n") }

// goldenReprIs asserts both halves of a case in one step: the compiled answer has to match the
// record, and the record has to say `want`. The second check is what stops a case from passing
// because its expectation drifted along with the implementation.
func goldenReprIs(t errorReporter, src, want string) {
	rec := goldenLookup(t, src)
	if rec.Repr != want {
		fatalf(t, "the record holds %q for %s, but the case expects %q — the expectation moved away from what was recorded; if the reference agrees, re-record the source", rec.Repr, src, want)
	}
	if got := goldenRepr(t, src); got != "" && got != want {
		fatalf(t, "compiled answer %q for %s, want %q", got, src, want)
	}
}

// goldenRunError is the record-checked failure of a snippet: the error the compiled backend gives
// for a source the record says must not answer.
func goldenRunError(t errorReporter, src string) error {
	want := goldenLookup(t, src)
	out := goldenRun(t, src)
	if out.res == nil {
		if out.refused {
			return recordedTrap(want, nil)
		}
		return errGoldenSkipped
	}
	res := out.res
	if res.Code != 0 {
		return recordedTrap(want, out.res)
	}
	if want.Err != "" {
		return fmt.Errorf("compiled backend answered %q where the record says the program traps with %q", resultRepr(res), want.Err)
	}
	return nil
}

// recordedTrap rebuilds the failure the record describes, so a case can still ask which class
// escaped and with what message.
//
// The class comes from the target's own report when the compiled run produced one: the runtime
// names CPython's class for every trap (ADR 0211), where the retired engine often reported only
// the sentence. Falling back to the record's class keeps the older entries honest, and a failure
// the shared front end produced stays a plain error — a refusal to compile never had a class, and
// inventing one to satisfy an assertion would be the wrong kind of helpful.
func recordedTrap(want goldenEntry, res *JITResult) error {
	if want.FrontEnd {
		return fmt.Errorf("%s", want.Err)
	}
	// The record's class and message win. The target's report fills them in where the retired
	// engine left them blank (it raised many failures with a sentence and no class, which is the
	// hole ADR 0211 filled), but it may not overwrite them: the message the program owes is the
	// reference's wording, and a runtime that says `KeyError: key not found` where the record has
	// `KeyError: 'a'` is the disagreement this ledger exists to catch — not a detail to smooth over.
	class, message := want.ExnType, want.ExnMsg
	if res != nil && class == "" {
		if _, c, m := ParseTrapReport(res.Stderr); c != "" {
			class, message = c, m
		}
	}
	// The sentence the engine wrote carried its own caption ("eval error: …", "verify: …"). That
	// caption is not part of the program's message: the last line of a traceback is
	// `ValueError: boom`, not `ValueError: eval error: boom`.
	msg := strings.TrimPrefix(strings.TrimPrefix(want.Err, "eval error: "), "verify: ")
	if msg == "" {
		msg = message
	}
	if msg == "" {
		msg = "the program trapped"
	}
	return &TrapError{Msg: msg, ExnType: class, ExnMsg: message}
}

// trapLine is the sentence the target wrote about the trap, for the ledger row that has to say why
// the compiled backend and the record disagree.
func trapLine(res *JITResult) string {
	if res == nil {
		return "(no run)"
	}
	if _, class, message := ParseTrapReport(res.Stderr); class != "" {
		return class + ": " + message
	}
	if strings.TrimSpace(res.Stderr) != "" {
		return strings.TrimSpace(res.Stderr)
	}
	return fmt.Sprintf("exit %d", res.Code)
}

func (o goldenOutcome) stdout() string {
	if o.res == nil {
		return ""
	}
	return o.res.Output
}

func resultRepr(res *JITResult) string {
	if res == nil || res.Result == nil {
		return ""
	}
	return res.Result.Repr
}

func trimNL(s string) string { return strings.TrimRight(s, "\n") }

// sameTypeName reconciles the interpreter's type vocabulary with the runtime's. The builtin names
// agree; the container spellings and the void do not.
func sameTypeName(recorded, reported string) bool {
	if recorded == reported {
		return true
	}
	switch recorded {
	case "NoneType", "none":
		return reported == "void" || reported == "NoneType"
	case "int", "integer":
		return reported == "int" || reported == "number"
	case "float":
		return reported == "float" || reported == "number"
	}
	if strings.HasPrefix(recorded, "list") {
		return reported == "list"
	}
	if strings.HasPrefix(recorded, "dict") {
		return reported == "dict"
	}
	if strings.HasPrefix(recorded, "set") {
		return reported == "set"
	}
	if strings.HasPrefix(recorded, "function") || recorded == "method" || recorded == "closure" {
		return reported == "function"
	}
	return false
}

func wantSummary(w goldenEntry) string {
	switch {
	case w.Repr != "":
		return "result " + fmt.Sprintf("%q", w.Repr)
	case w.HasStd:
		return fmt.Sprintf("stdout %q", w.Stdout)
	case w.Err != "":
		return "trap " + w.Err
	default:
		return "no recorded output"
	}
}

// TestMain holds the drift check after every case has run, because the ledger describes the whole
// run rather than one case: a divergence found by the 3,000th source has to be weighed against the
// list alongside the first.
func TestMain(m *testing.M) {
	code := m.Run()
	if p := os.Getenv("GUSTY_GOLDEN_MISSING"); p != "" {
		var missing []string
		missingSources.Range(func(k, _ any) bool {
			if src, ok := k.(string); ok {
				missing = append(missing, src)
			}
			return true
		})
		sort.Strings(missing)
		if body, err := json.MarshalIndent(missing, "", "  "); err == nil {
			_ = os.WriteFile(p, append(body, '\n'), 0o644)
		}
		fmt.Fprintf(os.Stderr, "%d source(s) with no recorded expectation written to %s\n", len(missing), p)
	}
	if os.Getenv("GUSTY_GOLDEN_UPDATE") != "" {
		if err := writeDriftLedger(Divergences()); err != nil {
			fmt.Fprintln(os.Stderr, "could not rewrite the drift ledger:", err)
			code = 1
		} else {
			fmt.Fprintf(os.Stderr, "interpreter golden drift ledger rewritten: %d divergence(s)\n", len(Divergences()))
		}
		os.Exit(code)
	}
	if report := checkDrift(); report != "" {
		fmt.Fprintln(os.Stderr, report)
		os.Exit(1)
	}
	os.Exit(code)
}
