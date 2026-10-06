package integration

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
)

// The reference-debt ledger — what this package does with a program the compiled backend answers
// *differently from CPython*, rather than refusing.
//
// With two engines, a table row could be corroborated engine-to-engine and checked against the
// reference for style. With one engine there is exactly one thing a row can claim: this program
// behaves like Python. When it does not, the disagreement has to live somewhere that is not a test's
// expectation column, because writing the wrong answer into an expectation is how a bug becomes
// documentation. The golden drift ledger does that for disagreements with the *retired engine's*
// recorded answers; this one does it for disagreements with the *reference itself*, which is the
// comparison that outlives every engine.
//
// It works like the drift ledger, including the part that makes ledgers safe:
//
//   - a program that differs from CPython and is not on the ledger fails the run;
//   - a program on the ledger that now agrees with CPython *also* fails, until the row is deleted,
//     because a paid debt left in the file is a lie about what the compiler still cannot do;
//   - a row without a roadmap reference — the row that owes the fix — fails, because a ledger nobody
//     owns is a wish list;
//   - and a row whose recorded expectation no longer matches what either side says fails too, so the
//     file cannot drift into describing a program nobody runs.
//
// `GUSTY_DEBT_UPDATE=1` rewrites the file from what the run measured. It is a recording tool for the
// cycle that measures a new surface, not a way to make a red suite green: what it writes is the
// divergence, with the reference's own answer beside it, and every row still needs its roadmap row
// filled in by hand before the suite will accept it.

// referenceDebtFile is the ledger's path, beside the golden drift ledger so the two are read together.
const referenceDebtFile = "testdata/cpython-debt.json"

// ReferenceDebt is one filed disagreement between the compiled backend and CPython.
type ReferenceDebt struct {
	Source string `json:"source"`
	// Python is what the reference does: its stdout, or "raises <Class>: <message>".
	Python string `json:"python"`
	// Compiled is what the binary does instead: its stdout, or "exit <n> <first line>".
	Compiled string `json:"compiled"`
	// Roadmap names the row that owes the fix (L11.1, Gap R.175, …). Required, and checked.
	Roadmap string `json:"roadmap"`
	// Why is one sentence of prose: what makes these two answers differ.
	Why string `json:"why,omitempty"`
}

var (
	debtMu      sync.Mutex
	debtSeen    = map[string]ReferenceDebt{}
	debtLoaded  map[string]ReferenceDebt
	debtOnce    sync.Once
	debtMissing []string
)

func loadReferenceDebt(t *testing.T) map[string]ReferenceDebt {
	t.Helper()
	debtOnce.Do(func() {
		debtLoaded = map[string]ReferenceDebt{}
		raw, err := os.ReadFile(referenceDebtFile)
		if err != nil {
			return // no ledger yet: every divergence is then "new", which is the honest default
		}
		var rows []ReferenceDebt
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Errorf("%s is not valid JSON: %v", referenceDebtFile, err)
			return
		}
		for _, r := range rows {
			debtLoaded[r.Source] = r
		}
	})
	return debtLoaded
}

// ReferenceAgreement is what a caller measured: the reference's answer, the compiled answer, and the
// exit status of the compiled run.
type ReferenceAgreement struct {
	Python     string
	PythonTrap string // "raises <Class>: <message>" when the reference raised
	Compiled   string
	Code       int
}

func (a ReferenceAgreement) pythonSide() string {
	if a.PythonTrap != "" {
		return a.PythonTrap
	}
	return "stdout " + quoteShort(a.Python)
}

func (a ReferenceAgreement) compiledSide() string {
	if a.Code == 0 {
		return "stdout " + quoteShort(a.Compiled)
	}
	return "exit " + itoa(a.Code) + " " + quoteShort(firstLineOf(a.Compiled))
}

func quoteShort(s string) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return `"` + s + `"`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// agreesWithReference is the claim a row makes when the compiled path ran the program: the two sides
// printed the same bytes and neither raised where the other did not.
func agreesWithReference(a ReferenceAgreement) bool {
	return a.Code == 0 && a.PythonTrap == "" && strings.TrimSuffix(a.Python, "\n") == strings.TrimSuffix(a.Compiled, "\n")
}

// requireReferenceAgreement is the ratchet. Call it with what a run measured and the roadmap row that
// owns the gap: it passes when the compiled answer equals the reference's, files (and passes) a
// documented divergence, and fails anything the ledger does not describe exactly.
func requireReferenceAgreement(t *testing.T, src string, a ReferenceAgreement, roadmap, why string) {
	t.Helper()
	debtMu.Lock()
	debtSeen[src] = ReferenceDebt{Source: src, Python: a.pythonSide(), Compiled: a.compiledSide(), Roadmap: roadmap, Why: why}
	debtMu.Unlock()
	if agreesWithReference(a) {
		// Recorded as exercised either way: a ledger row whose case never reached it is a row the suite
		// stopped testing, and this is the only place that can tell "agrees today" from "not asked".
		//
		// The debt may be paid. If it is on the ledger, that is a failure the row must notice.
		if row, ok := loadReferenceDebt(t)[src]; ok {
			t.Errorf("%s: this program now agrees with the reference, but %s still carries a debt row (%s → %s, owed by %s) — delete it",
				src, referenceDebtFile, row.Python, row.Compiled, row.Roadmap)
		}
		return
	}
	debtMu.Lock()
	debtSeen[src] = ReferenceDebt{Source: src, Python: a.pythonSide(), Compiled: a.compiledSide(), Roadmap: roadmap, Why: why}
	debtMu.Unlock()
	row, ok := loadReferenceDebt(t)[src]
	if !ok {
		if os.Getenv("GUSTY_DEBT_UPDATE") == "1" {
			// Recording mode: file it and let the run finish, so the cycle that measures new surface can
			// see the whole list at once. The rows still need their roadmap row, which is checked below.
			debtMu.Lock()
			if debtLoaded == nil {
				debtLoaded = map[string]ReferenceDebt{}
			}
			debtLoaded[src] = debtSeen[src]
			debtMu.Unlock()
			t.Logf("NEW reference debt recorded (needs a roadmap row before this passes): %s: %s vs %s", src, a.pythonSide(), a.compiledSide())
			return
		}
		t.Errorf("%s disagrees with the reference and is not on the ledger:\n reference: %s\n compiled:  %s\n file it in %s with the roadmap row that owns it (or fix the compiler, which is the better answer).",
			src, a.pythonSide(), a.compiledSide(), referenceDebtFile)
		return
	}
	if row.Python != a.pythonSide() || row.Compiled != a.compiledSide() {
		t.Errorf("%s: the ledger row no longer describes what either side does.\n ledger:   %s vs %s\n measured: %s vs %s\n One of the two changed under it — re-measure (GUSTY_DEBT_UPDATE=1) and re-own it.",
			src, row.Python, row.Compiled, a.pythonSide(), a.compiledSide())
		return
	}
	if strings.TrimSpace(row.Roadmap) == "" {
		t.Errorf("%s: a reference debt without a roadmap row is a wish, not a debt — who owes this fix?", src)
		return
	}
	t.Logf("reference debt (filed, owned by %s): %s vs %s", row.Roadmap, a.pythonSide(), a.compiledSide())
}

// markDebtExercised records that a case asked about this source, whatever the verdict was. The
// two-way ratchet needs it: a ledger row no case reached is a row the suite stopped testing, and
// "the compiler declines this shape" is not the same as "nobody asked".
func markDebtExercised(row ReferenceDebt) {
	debtMu.Lock()
	defer debtMu.Unlock()
	debtSeen[row.Source] = row
}

// requireReferenceTrapOrHonestRefusal is the same ratchet for a program the reference *raises* on:
// the compiled path raises with the reference's class and words, refuses at the compile door naming
// the half it is missing, or has a debt row saying which of the two it fails to do.
func requireReferenceTrapOrHonestRefusal(t *testing.T, src, classAndMessage string, out string, code int, roadmap, why string) {
	t.Helper()
	a := ReferenceAgreement{PythonTrap: "raises " + classAndMessage, Compiled: out, Code: code}
	switch {
	case code == 3 && strings.Contains(out, classAndMessage):
		requireReferenceAgreement(t, src, ReferenceAgreement{PythonTrap: "", Python: "", Compiled: out, Code: 3}, roadmap, why)
		return
	case code == 1 && refusesHonestly(out):
		// The case asked, which is what "exercised" means: it put the program to the reference and to
		// the compiler and compared them. An honest refusal satisfies the row, and still counts as
		// exercised — otherwise a filed divergence whose case happens to refuse gets reported to the
		// developer as untested, and the row looks stale when it is only declined.
		markDebtExercised(ReferenceDebt{
			Source:   src,
			Python:   "raises " + classAndMessage,
			Compiled: "refused: " + firstLine(out),
			Roadmap:  roadmap,
			Why:      why,
		})
		noteCompiledGap(t, src, out)
		return
	case code == exitIRVerify:
		t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
	}
	requireReferenceAgreement(t, src, a, roadmap, why)
}

// ReferenceDebtCount lets a run see how much of the suite is riding on filed reference divergences.
// The number going up while everything stays green is the suite going soft; the row in TestReferenceDebtLedger
// that prints it is what catches that.
func ReferenceDebtCount() int {
	debtMu.Lock()
	defer debtMu.Unlock()
	return len(debtSeen)
}

func writeReferenceDebt(t *testing.T, path string) {
	t.Helper()
	debtMu.Lock()
	rows := make([]ReferenceDebt, 0, len(debtSeen))
	for _, r := range debtSeen {
		rows = append(rows, r)
	}
	debtMu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Source < rows[j].Source })
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Errorf("encode the reference-debt ledger: %v", err)
		return
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		t.Errorf("write %s: %v", path, err)
	}
}

// TestReferenceDebtLedger is the ledger's own half of the contract, and the half that does not
// depend on when a case happened to run: the file parses, every row is shaped, and every row is
// owned by a roadmap row. The two-way ratchet — a row no case reached, and a debt its case says is
// paid — is checked once by TestReferenceDebtRatchet after the whole package has finished.
func TestReferenceDebtLedger(t *testing.T) {
	ledger := loadReferenceDebt(t)
	for src, row := range ledger {
		if strings.TrimSpace(row.Roadmap) == "" {
			t.Errorf("%s: reference debt with no roadmap row: %s", src, row.Why)
		}
		if strings.TrimSpace(row.Python) == "" || strings.TrimSpace(row.Compiled) == "" {
			t.Errorf("%s: reference debt with an empty side: the row has to say what BOTH sides do, or it is a shrug with a filename attached (%s)", src, row.Why)
		}
	}
	t.Logf("reference debts on file: %d", len(ledger))
}

// referenceDebtRatchet compares the ledger on disk with what the run reported. It returns the number
// of rows and the problems; a negative count means there is nothing to check.
//
// TestMain calls it — after m.Run, when every case in the package has had its turn — and not from a
// test function. That is deliberate: Go runs a package's tests in file order, so an assertion here
// would run before the cases in later files (scoping_test.go, slot_order_object_test.go, …) had
// reported anything, and would report their rows as untested. A check that depends on which file a
// case lives in is not a check; the post-run position is the only one where "no case reached this
// row" means something.
func referenceDebtRatchet() (int, []string) {
	return referenceDebtRatchetSince(isPartialRun())
}

// isPartialRun reports whether the package was asked to run a subset of its cases. The standard
// flag is the only place that knows, and the answer decides how much the ledger may claim: a
// `-run` subset never reaches most rows, and telling a developer their subset fixed 400 bugs would
// make the ratchet useless by lunchtime (the same rule the golden drift ledger runs by).
func isPartialRun() bool {
	f := flag.Lookup("test.run")
	return f != nil && strings.TrimSpace(f.Value.String()) != ""
}

func referenceDebtRatchetSince(partial bool) (int, []string) {
	debtMu.Lock()
	defer debtMu.Unlock()
	if debtLoaded == nil {
		return -1, nil
	}
	var msgs []string
	for src, row := range debtLoaded {
		if _, seen := debtSeen[src]; !seen && !partial {
			msgs = append(msgs, fmt.Sprintf("%s: on the reference-debt ledger but no case reached it this run — the case was deleted or stopped running this program; a ledger row nobody exercises is fiction", src))
		}
		if strings.TrimSpace(row.Roadmap) == "" {
			msgs = append(msgs, fmt.Sprintf("%s: reference debt with no roadmap row: %s", src, row.Why))
		}
	}
	return len(debtLoaded), msgs
}
