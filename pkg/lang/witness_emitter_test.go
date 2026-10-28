package lang

import (
	"bufio"
	"encoding/json"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The emitter's own strings are a claim surface (roadmap Gap R.288, ADR 0437).
//
// The witness guard in witness_claim_test.go reads the files that make claims — the tests, the CLI's
// flag descriptions, the conformance registry, the agent-facing documents. It cannot see the half of
// this toolchain that actually talks to a reader: the refusal sentences `pkg/lang` emits. When ADR 0302
// retired the AST interpreter, the documents were restated and the flag help was restated, but
// `nestedContainerErr` went on offering to "run it interpreted" — five words pointing at an engine that
// no longer exists and a flag whose answer is a usage error — because nothing asked the emitter's
// strings the question the docs were asked (Gap R.193 named exactly this hole: the guard policed the
// docs, not the emitter).
//
// So this file asks it. A string literal a compiler hands to a user is a claim about what the toolchain
// can do, and an agent reading `gustyc --json` branches on it. The phrases live in
// testdata/witness-emitter-banned-phrases.txt — the offers to run a program the way this toolchain
// retired — and are read together with the documents' own list, because a false promise is false in
// either place. A literal is licensed only on the line of it that carries a history marker, which is
// what keeps a sentence about the retirement sayable while an offer to use it is not.
//
// Two more cases keep the guard honest:
//
//   - testdata/witness-emitter-ledger.json names, one row per string, what the guard is told to
//     tolerate and which roadmap row owns it — and every row must match a string that still exists, so
//     an exemption cannot outlive the sentence it was written for; and
//   - TestTheEmitterGuardGoesRedOnAnInjectedOfferToRun feeds the checker a `codegen:` error that
//     promises the retired way to run and fails if it is NOT reported, because a guard that reads an
//     empty corpus passes for the same reason a test that runs nothing does.

const witnessEmitterBannedFile = "testdata/witness-emitter-banned-phrases.txt"
const witnessEmitterLedgerFile = "testdata/witness-emitter-ledger.json"

// witnessEmitterString is one string literal the emitter can hand to a reader, with where it came from.
type witnessEmitterString struct {
	File string // repo-relative, so an offender names a file a reader can open
	Line int
	Text string
}

// witnessEmitterLedgerRow exempts one known string and says who owes it.
type witnessEmitterLedgerRow struct {
	Substring string `json:"substring"`
	File      string `json:"file,omitempty"`
	Owner     string `json:"owner"`
	Why       string `json:"why"`
}

// witnessEmitterStrings parses every production Go file of the emitter and returns the string literals
// in them. Parsing rather than a text scan is the point: a production COMMENT is the design narrative —
// the same reason witness_claim_test.go leaves comments out of the document surface — while a literal is
// what the program says.
func witnessEmitterStrings(t *testing.T) []witnessEmitterString {
	t.Helper()
	root := repoRoot(t)
	var out []witnessEmitterString
	add := func(dir, suffix string) {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
				continue
			}
			if strings.HasSuffix(e.Name(), "_test.go") {
				continue // a test's strings are the claim surface the other guard reads
			}
			path := filepath.Join(root, dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			fset := token.NewFileSet()
			pf, err := goparser.ParseFile(fset, path, string(data), goparser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v — the emitter-string guard cannot read a file it cannot parse", path, err)
			}
			rel := strings.TrimPrefix(path, root+"/")
			ast.Inspect(pf, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				out = append(out, witnessEmitterString{File: rel, Line: fset.Position(lit.Pos()).Line, Text: v})
				return true
			})
		}
	}
	add("pkg/lang", ".go")
	add("cmd/gustyc", ".go")
	if len(out) == 0 {
		t.Fatal("the emitter has no string literals — the guard would pass by reading nothing")
	}
	return out
}

// witnessEmitterLinesLicensable answers the marker question where it belongs: a long refusal sentence
// is one literal, and licensing all of it because one clause somewhere mentions the retirement would
// let an offer hide behind an honest neighbour. Each phrase is therefore licensed only by a marker on
// the line of the literal the phrase sits on.
func witnessEmitterLinesLicensable(text string, at int, history []*regexp.Regexp) bool {
	line := text[:at]
	if i := strings.LastIndexByte(line, '\n'); i >= 0 {
		line = line[i+1:]
	}
	if j := strings.IndexByte(text[at:], '\n'); j >= 0 {
		line = line + text[at:at+j]
	} else {
		line = line + text[at:]
	}
	for _, re := range history {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// witnessEmitterOffenders is the check, factored out of the file walk so the injection case can hand it
// a string no file contains.
func witnessEmitterOffenders(patterns []*regexp.Regexp, phrases []string, history []*regexp.Regexp, corpus []witnessEmitterString, ledger []witnessEmitterLedgerRow) []string {
	var offenders []string
next:
	for _, s := range corpus {
		for _, row := range ledger {
			if row.Substring == "" || !strings.Contains(s.Text, row.Substring) {
				continue
			}
			if row.File != "" && row.File != s.File {
				continue
			}
			continue next // exempt, and the row's own life is checked beside it
		}
		for bi, re := range patterns {
			loc := re.FindStringIndex(s.Text)
			if loc == nil {
				continue
			}
			if witnessEmitterLinesLicensable(s.Text, loc[0], history) {
				continue
			}
			trimmed := witnessEmitterFlatten(s.Text)
			if len(trimmed) > 110 {
				trimmed = trimmed[:110] + "…"
			}
			offenders = append(offenders, s.File+":"+strconv.Itoa(s.Line)+"  ["+phrases[bi]+"]  "+trimmed)
			break
		}
	}
	sort.Strings(offenders)
	return offenders
}

func witnessEmitterFlatten(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func witnessEmitterPatternList(t *testing.T, path string, reason string) ([]*regexp.Regexp, []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v — the emitter guard has no list to enforce, and a guard that enforces nothing is the failure this file exists to catch", path, err)
	}
	defer f.Close()
	var phrases []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		phrases = append(phrases, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(phrases) == 0 {
		t.Fatalf("%s is empty: the emitter guard would pass without checking anything (%s)", path, reason)
	}
	var out []*regexp.Regexp
	for _, p := range phrases {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			t.Fatalf("%s: %q is not a usable pattern: %v", path, p, err)
		}
		out = append(out, re)
	}
	return out, phrases
}

// witnessEmitterLists reads the two phrase lists the emitter is asked: the offers to run that only a
// message makes, and the engine claims the documents are policed for — one question, one reading
// (ADR 0232), asked of whoever says the sentence.
func witnessEmitterLists(t *testing.T) ([]*regexp.Regexp, []string, []*regexp.Regexp) {
	t.Helper()
	root := repoRoot(t)
	emitP, emitPh := witnessEmitterPatternList(t, filepath.Join(root, "pkg/lang", witnessEmitterBannedFile), "no offers to run")
	docP, docPh := witnessEmitterPatternList(t, filepath.Join(root, "pkg/lang", witnessBannedFile), "the document list came up empty")
	histP := witnessPatterns(t, filepath.Join(root, "pkg/lang", witnessHistoryFile))
	return append(emitP, docP...), append(emitPh, docPh...), histP
}

func witnessEmitterLedger(t *testing.T) []witnessEmitterLedgerRow {
	t.Helper()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "pkg/lang", witnessEmitterLedgerFile))
	if err != nil {
		t.Fatalf("read the emitter ledger: %v — a string the guard is told to tolerate must be named there, with the row that owns it", err)
	}
	var doc struct {
		Meta struct {
			AddedBy string `json:"added_by"`
			Rule    string `json:"rule"`
		} `json:"meta"`
		Rows []witnessEmitterLedgerRow `json:"rows"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the emitter ledger is not readable JSON: %v", err)
	}
	if strings.TrimSpace(doc.Meta.Rule) == "" || strings.TrimSpace(doc.Meta.AddedBy) == "" {
		t.Fatal("the emitter ledger's meta carries no rule — every row must be written under a stated rule and an ADR")
	}
	for _, row := range doc.Rows {
		if strings.TrimSpace(row.Substring) == "" || strings.TrimSpace(row.Owner) == "" || strings.TrimSpace(row.Why) == "" {
			t.Fatalf("the emitter ledger row %q is missing its substring, owning row or reason", row.Substring)
		}
		if !strings.HasPrefix(row.Owner, "Gap ") && !strings.HasPrefix(row.Owner, "L") {
			t.Fatalf("the emitter ledger row %q is owned by %q — a row is owned by a roadmap ID, not by a sentence", row.Substring, row.Owner)
		}
	}
	return doc.Rows
}

func TestTheEmitterNeverPromisesARetiredWayToRun(t *testing.T) {
	t.Parallel()
	patterns, phrases, history := witnessEmitterLists(t)
	offenders := witnessEmitterOffenders(patterns, phrases, history, witnessEmitterStrings(t), witnessEmitterLedger(t))
	if len(offenders) > 0 {
		t.Errorf("%d strings the compiled backend emits promise a way to run that ADR 0302 retired, or name an engine nobody runs — say which rule is missing instead (AGENTS.md § One execution backend; ADR 0437):\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// TestTheEmitterLedgerStillNamesStringsThatExist is the ratchet: an exemption that outlives its sentence
// is a hole somebody has to find again, so a row that matches nothing fails the way an unlisted offender
// does.
func TestTheEmitterLedgerStillNamesStringsThatExist(t *testing.T) {
	t.Parallel()
	strs := witnessEmitterStrings(t)
	for _, row := range witnessEmitterLedger(t) {
		found := false
		for _, s := range strs {
			if row.File != "" && row.File != s.File {
				continue
			}
			if strings.Contains(s.Text, row.Substring) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the emitter ledger exempts %q (%s, owned by %s) and no emitted string carries it any more — delete the row, and if the string came back, that is the offender this ledger exists to keep visible",
				row.Substring, row.File, row.Owner)
		}
	}
}

// TestTheEmitterGuardGoesRedOnAnInjectedOfferToRun is the guard's own witness. Every case above can pass
// by reading nothing, so this one writes the sentence the emitter must never say and fails if the check
// does not report it — the same shape of proof the drift ledgers use, applied to the guard itself.
func TestTheEmitterGuardGoesRedOnAnInjectedOfferToRun(t *testing.T) {
	t.Parallel()
	patterns, phrases, history := witnessEmitterLists(t)
	injected := []witnessEmitterString{
		{File: "pkg/lang/injected.go", Line: 1, Text: "codegen: a compiled container cannot hold a list literal yet. Build that inner value separately, or run it interpreted"},
	}
	offenders := witnessEmitterOffenders(patterns, phrases, history, injected, nil)
	if len(offenders) != 1 {
		t.Fatalf("an offer to run the program the retired way went unreported: %v — the guard reads strings but does not read them for this", offenders)
	}
	if !strings.Contains(offenders[0], "injected.go") {
		t.Fatalf("the offender does not name the injected string: %v", offenders)
	}
	// …and the honest sentence this row landed with stays green under the same check, so the guard is
	// not simply reporting every refusal.
	green := []witnessEmitterString{
		{File: "pkg/lang/injected.go", Line: 2, Text: "codegen: a compiled container cannot hold a list literal as a key yet: a dict finds an entry by hashing its key, and this backend has no hashing rule for a container (roadmap L11.1). Key the dict by a value that has a hash"},
	}
	if got := witnessEmitterOffenders(patterns, phrases, history, green, nil); len(got) != 0 {
		t.Fatalf("the honest refusal is reported: %v", got)
	}
	// A literal that talks ABOUT the retirement stays sayable, which is what the marker is for.
	historyCase := []witnessEmitterString{
		{File: "pkg/lang/injected.go", Line: 3, Text: "the interpreter was retired by ADR 0302; run it interpreted is not a thing you can ask for"},
	}
	if got := witnessEmitterOffenders(patterns, phrases, history, historyCase, nil); len(got) != 0 {
		t.Fatalf("a sentence reporting the retirement is reported as an offender: %v", got)
	}
}
