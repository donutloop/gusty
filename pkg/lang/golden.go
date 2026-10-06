package lang

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// This file is the machinery behind the suite's second oracle. The compiler's own answers cannot
// check the compiler, and with the AST interpreter retired (ADR 0302) there is no second engine left
// to cross-check it with — so the suite checks the compiled backend against the record of what the
// engine that used to live here answered, taken at the last commit that had it.
//
// It lives in the package rather than in a _test.go file because both test binaries — pkg/lang and
// integration — put questions to the same record. The ledger path is an argument, so each package
// keeps its own list of disagreements while the answers themselves stay a single file.

// GoldenFile is the package-relative path of the record, and GoldenDriftFile the package-relative
// path of the disagreement ledger a package writes its own divergences to.
const (
	GoldenFile      = "testdata/interpreter-golden.json"
	GoldenDriftFile = "testdata/interpreter-golden-drift.json"
)

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
	// Note records a correction to the entry itself — where the recorder asked the retired engine a
	// question its users never saw answered, and the entry holds what the interface displayed instead.
	// It is data, not a comment, so `jq` shows it and a future re-recording cannot silently undo it.
	Note string `json:"note,omitempty"`
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

// goldenCandidates are where the record may be found. Each test binary runs from its own directory,
// and the record is one file that pkg/lang owns — integration reads it from next door rather than
// keeping a copy that could fall behind the answers it is supposed to hold.
var goldenCandidates = []string{
	filepath.Join("testdata", "interpreter-golden.json"),
	filepath.Join("..", "pkg", "lang", "testdata", "interpreter-golden.json"),
}

func loadGolden(t errorReporter) map[string]goldenEntry {
	goldenOnce.Do(func() {
		path := ""
		var raw []byte
		var statErr error
		for _, cand := range goldenCandidates {
			if _, e := os.Stat(cand); e == nil {
				raw, statErr = os.ReadFile(cand)
				if statErr == nil {
					path = cand
					break
				}
			}
		}
		if path == "" {
			// The runner started somewhere the fixture is not. That is an environment fault, and
			// naming it beats reporting 2900 sources as "no recorded expectation".
			goldenLoadE = fmt.Errorf("interpreter golden not found (looked in %s): %w", strings.Join(goldenCandidates, ", "), statErr)
			return
		}
		var f goldenFile
		if err := json.Unmarshal(raw, &f); err != nil {
			goldenLoadE = fmt.Errorf("parse interpreter golden at %s: %w", path, err)
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
	data := loadGolden(t)
	e, ok := data[src]
	if !ok {
		// The same program written two ways. A test that ends its snippet with a newline and one that
		// does not are asking about the same program — a trailing blank line is not a different piece
		// of source — and the record was written by whichever suite reached a source first. Refusing to
		// look across that difference would make coverage depend on how a test file happens to be
		// typed, which is the kind of brittleness that ends with someone deleting a case to make it
		// load. Leading/trailing blank lines are the only normalisation: the program's own text,
		// including its own final newline inside a string, is untouched.
		if trimmed := strings.Trim(src, "\n"); trimmed != src {
			e, ok = data[trimmed]
		}
	}
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
		s.Skipf("ADR 0302 divergence, recorded in the drift ledger: %s — owed %s, the compiled backend gave %s",
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

// checkDrift holds the run's divergences against the ledger. Both directions are failures: an
// unlisted divergence means new debt arrived unannounced, and a listed divergence that did not
// happen means debt was paid and the ledger still charges for it.
func checkDriftAgainst(ledger string) string {
	got := Divergences()
	want, err := readDriftLedgerFrom(ledger)
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
				ledger, d.Source, d.Reason, quoteOr(d.Expected), quoteOr(d.Actual)))
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
			src, w.Reason, ledger))
	}
	if len(problems) == 0 {
		return ""
	}
	sort.Strings(problems)
	return "\n=== interpreter golden drift (ADR 0302) ===\n" + strings.Join(problems, "\n") +
		fmt.Sprintf("\n%d divergence(s) reported by this run, %d on the ledger.\n", len(got), len(want))
}

func readDriftLedgerFrom(ledger string) (map[string]Divergence, error) {
	raw, err := os.ReadFile(ledger)
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

func writeDriftLedgerTo(ledger string, rows []Divergence) error {
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ledger, append(body, '\n'), 0o644)
}

// marshalGoldenMissing is the JSON face of the "which sources have no record" question.
func marshalGoldenMissing(missing []string) ([]byte, error) {
	return json.MarshalIndent(missing, "", "  ")
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

// --- the exported face, for the integration binary ------------------------------------------------

// The integration tests ask the same question pkg/lang asks — does the compiled answer match the one
// on record? — from a different package, so the check has one implementation rather than two that
// could disagree about what a match means. Each of these takes the source, runs it through the one
// backend, compares with the record, and fails the caller's test on a mismatch that is not already a
// ledger row.

// HasGoldenAnswer reports whether the record holds this source, without involving a *testing.T and
// without recording a divergence. A corpus case that wants to report ten unrecorded programs in one
// run — instead of one per run, which is how a recording cycle turns into an afternoon — asks this
// first and reports the misses itself.
func HasGoldenAnswer(src string) bool {
	data := loadGolden(silentReporter{})
	if data == nil {
		return false
	}
	if _, ok := data[src]; ok {
		return true
	}
	if _, ok := data[strings.Trim(src, "\n")]; ok {
		return true
	}
	// Not on record: put it in the harvest so one run can hand the whole list to the recorder.
	missingSources.Store(src, true)
	return false
}

// silentReporter absorbs the golden helpers' error reporting for callers that only want to know
// whether a record exists; those callers report the miss themselves, in their own words.
type silentReporter struct{}

func (silentReporter) Errorf(string, ...any) {}
func (silentReporter) Fatalf(string, ...any) {}
func (silentReporter) Skipf(string, ...any)  {}

// RecordedStdout runs src and returns the program's stdout, having checked it against the record.
func RecordedStdout(t errorReporter, src string) string {
	return goldenStdout(t, src)
}

// CompiledRefusal is the record's verdict on a program the compiler refused to build, for callers
// that ran a program through the CLI rather than through the in-process pipeline (the integration
// package's `--aot`/`--eval` runners, chiefly).
//
// It returns true when the caller's test should stop — because the record says this program has an
// answer, the compiled backend refused it, and that mismatch is now a row in the drift ledger, which
// is the artifact that owns the disagreement and fails the run when a new one appears or an old one
// quietly goes away. It returns false when the refusal is the *expected* outcome (the front end
// refused the same program, or no answer is on record), and the caller's own assertion about the
// refusal therefore still runs. A refusal is never simply passed: either it is filed here, or the
// caller is expected to check what it says.
func CompiledRefusal(t errorReporter, src, message string) bool {
	data := loadGolden(t)
	want, ok := data[src]
	if !ok {
		if trimmed := strings.Trim(src, "\n"); trimmed != src {
			want, ok = data[trimmed]
		}
	}
	if !ok {
		// Nothing on record: the caller's assertion stands, and the missing-source list gets the
		// program so the recording cycle can see it.
		missingSources.Store(src, true)
		return false
	}
	if want.FrontEnd {
		return false // the language refuses this program; the compiled refusal is the contract
	}
	if want.Err != "" {
		// The engine trapped here. A refusal that keeps the program from running is a different
		// failure class from the trap the record owes — a raise the compiler declines to build — so it
		// belongs on the ledger rather than in a test's expectation column.
		noteDivergence(t, src, "the compiled backend refuses a program the engine ran to a trap", wantSummary(want), message)
		return true
	}
	noteDivergence(t, src, "the compiled backend cannot build a program the engine ran", wantSummary(want), message)
	return true
}

// RecordedStdoutIs checks stdout against a literal the test wrote, with the record as the referee:
// if the two disagree with each other, that itself is the failure.
func RecordedStdoutIs(t errorReporter, src, want string) {
	goldenStdoutIs(t, src, want)
}

// RecordedPrints is RecordedStdoutIs for a program whose whole output is one line.
func RecordedPrints(t errorReporter, src, want string) {
	goldenPrints(t, src, want)
}

// RecordedRepr and RecordedType answer what the value of the source's last expression is, checked
// against the record.
func RecordedRepr(t errorReporter, src string) string {
	return goldenRepr(t, src)
}

func RecordedType(t errorReporter, src string) string {
	return goldenType(t, src)
}

// RecordedRunError runs a program expected to trap and returns the report the target wrote.
func RecordedRunError(t errorReporter, src string) error {
	return goldenRunError(t, src)
}

// RecordedProgramAgrees runs src through the compiled binary and checks its stdout against the
// record — the shape the parity tests used to check two engines against each other. With one backend
// the record is the second opinion, and CPython remains the one above both.
func RecordedProgramAgrees(t errorReporter, src string) {
	goldenStdoutIs(t, src, "")
}
