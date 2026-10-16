package lang

// The case doors (roadmap L11.5, ADR 0362).
//
// `upper`/`lower`/`title`/`capitalize`/`swapcase` used to have two answers and both were wrong in a
// different way: the fold borrowed Go's SIMPLE case mapping, so `"ß".upper()` stayed `ß`, `"ﬁ".upper()`
// stayed `ﬁ`, `"ΑΣ".lower()` printed `ασ` where the reference prints `ας`, and `"é.é".title()` capitalised
// only after a space; the runtime door recognised A–Z and copied every other byte, so the same program
// written through a parameter printed `CAFé`. Neither was refused, and both exited 0 — a value the
// reference never produces, which ADR 0166 counts as the worst class of answer this compiler can give.
//
// The rows below therefore ask three different questions, because a case engine can fail three different
// ways:
//
//   - does the ENGINE answer the reference? (the hand-picked special-casing rows, then a sweep against
//     CPython over thousands of strings, in the shape ADR 0263 used for the rounding rule);
//   - do the FOLD and the DOOR answer the same thing? (each text is compiled twice, once where the compiler
//     can read it and once where it cannot, and the two runs must agree with each other and with python3);
//   - is the door's MODULE honest? (the emitted module must verify, and the tables must be gated — an
//     ordinary program that never upper-cases anything must not carry the reference's 1.1-million-code-point
//     answer in it).

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// caseReference asks python3 the question, in the same shape the other reference-leg rows use: the
// program is written once and asked of both sides inside the case, so an expectation cannot be quietly
// rewritten to match whatever the compiler happened to do.
func caseReference(t *testing.T, src string) (string, error) {
	t.Helper()
	cmd := exec.Command("python3", "-c", src)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("the reference refused the program (%v): %s", err, src)
	}
	return string(out), nil
}

// caseOpsOnTable is the five operations with the names the program writes them under.
var caseOpsOnTable = []struct {
	op   int
	name string
}{
	{caseUpper, "upper"},
	{caseLower, "lower"},
	{caseTitle, "title"},
	{caseCapitalize, "capitalize"},
	{caseSwapcase, "swapcase"},
}

// caseSpecialRows are the answers that no simple mapping produces, each written the way the reference
// wrote it. This is the table a hand-written implementation loses, which is why it is the first row.
var caseSpecialRows = []struct {
	name string
	in   string
	op   int
	want string
}{
	// one-to-many special casings, from the reference's own per-character answer
	{"sharp s upper", "ß", caseUpper, "SS"},
	{"straße upper", "straße", caseUpper, "STRASSE"},
	{"ligature fi upper", "ﬁ", caseUpper, "FI"},
	{"ligature st upper", "ﬅ", caseUpper, "ST"},
	{"dotted capital I lower", "İ", caseLower, "i\u0307"},
	{"titlecase digraph title", "ǅ", caseTitle, "ǅ"},
	{"titlecase digraph lower", "Ǆ", caseLower, "ǆ"},
	{"small ligature upper", "ǳ", caseUpper, "Ǳ"},

	// the conditional sigma: the same characters answer differently depending on their neighbours
	{"lone sigma lower", "Σ", caseLower, "σ"},
	{"sigma after a cased letter", "ΑΣ", caseLower, "ας"},
	{"sigma with a cased letter after it", "ΑΣΒ", caseLower, "ασβ"},
	{"three sigmas", "ΣΣΣ", caseLower, "σσς"},
	{"sigma after an ignorable prosgegrammeni", "ͅΣ", caseLower, "ͅσ"},
	// the apostrophe AFTER the sigma does not stop the sigma being final — the forward scan steps over it
	// and finds nothing cased.
	{"sigma before an apostrophe", "ΑΣ’", caseLower, "ας’"},
	{"sigma after an apostrophe", "Α’Σ", caseLower, "α’ς"},

	// title's word rule is "the previous character is not cased", not "the previous character is a space"
	{"title after a dot", "é.é", caseTitle, "É.É"},
	{"title after a digit", "a1b", caseTitle, "A1B"},
	{"title of a greek word", "ἀἄ", caseTitle, "Ἀἄ"},

	// capitalize is the reference's, which is not "title the first cased character"
	// `'1a'.capitalize()` is `'1a'` and not `'1A'`: the reference upper-cases the FIRST character, which
	// here is a digit, and lower-cases the rest — a reading of "title the first cased character" gets this
	// row wrong, which is why it is in the table.
	{"capitalize of a digit then a letter", "1a", caseCapitalize, "1a"},
	{"capitalize lower-cases the tail", "aBC", caseCapitalize, "Abc"},
	{"capitalize of sharp s", "ßx", caseCapitalize, "Ssx"},

	// swapcase is its own table: title-case characters go down, not up
	// a titlecase character's swapcase is itself: the reference's table answers `'ǅ'` for `'ǅ'`.
	{"swapcase of a titlecase digraph", "ǅ", caseSwapcase, "ǅ"},
	// swapcase reaches the sigma rule too: `'ΑΣ'.swapcase()` is `'ας'`.
	{"swapcase of greek", "ΑΣ", caseSwapcase, "ας"},
	{"swapcase of latin", "aÉ", caseSwapcase, "Aé"},

	// and everything that has no mapping must come back untouched
	{"chinese is unchanged", "日本語", caseUpper, "日本語"},
	{"emoji is unchanged", "🎉🎉", caseSwapcase, "🎉🎉"},
	{"empty is empty", "", caseTitle, ""},
}

// TestTheCaseEngineAnswersTheReferencesSpecialCasings is the row-by-row check for the answers that no
// simple mapping produces. The expectation each row states is itself checked against the reference in the
// same case — one process answers the whole table — so this file cannot become a second source of truth: a
// row that drifts from CPython fails as loudly as an engine that does.
func TestTheCaseEngineAnswersTheReferencesSpecialCasings(t *testing.T) {
	t.Parallel()
	pairs := make([][2]string, 0, len(caseSpecialRows))
	for _, tc := range caseSpecialRows {
		pairs = append(pairs, [2]string{tc.in, caseOpName(tc.op)})
	}
	want := caseReferenceForPairs(t, pairs)
	for i, tc := range caseSpecialRows {
		tc, ref := tc, want[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.want != ref {
				t.Fatalf("%q.%s(): this row states %q, the reference answers %q — the reference is the definition, fix the row",
					tc.in, caseOpName(tc.op), tc.want, ref)
			}
			got, err := caseMapString(tc.op, tc.in)
			if err != nil {
				t.Fatalf("the engine would not answer (%v): %s.%s()", err, tc.in, caseOpName(tc.op))
			}
			if got != ref {
				t.Fatalf("%q.%s() = %q, the reference answers %q", tc.in, caseOpName(tc.op), got, ref)
			}
		})
	}
}

// caseReferenceForPairs asks python3 each (text, method) pair, in one process.
func caseReferenceForPairs(t *testing.T, pairs [][2]string) []string {
	t.Helper()
	encoded, err := json.Marshal(pairs)
	if err != nil {
		t.Fatalf("marshal the pairs: %v", err)
	}
	probe := `import json,sys
pairs = json.loads(sys.stdin.read())
sys.stdout.write(json.dumps([getattr(s, m)() for s, m in pairs]))
`
	cmd := exec.Command("python3", "-c", probe)
	cmd.Stdin = strings.NewReader(string(encoded))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the reference could not answer the table: %v", err)
	}
	var ans []string
	if err := json.Unmarshal(out, &ans); err != nil {
		t.Fatalf("the reference's answer is not the JSON the probe promises: %v", err)
	}
	if len(ans) != len(pairs) {
		t.Fatalf("the reference answered %d rows for %d pairs", len(ans), len(pairs))
	}
	return ans
}

// caseSweepStrings is the corpus the engine is swept against. It is not only the interesting characters:
// the rules that broke in the past were the ones about NEIGHBOURS (the sigma's two scans, title's word
// boundary), so most rows are two or three characters long and mix cased, ignorable and uncased ones.
func caseSweepStrings() []string {
	alph := []rune{'a', 'A', 'b', 'B', 'ß', 'Σ', 'σ', 'ς', 'ͅ', 'Α', 'Β', 'Ω', 'ω', 'é', 'É', 'İ', 'ı',
		'ǅ', 'Ǆ', 'ǆ', 'ﬁ', 'ﬅ', 'ﬁ', 'ᾈ', '̓', '’', '\'', '-', '1', ' ', '.', '日', '🎉', 'ǳ', 'ῆ'}
	out := []string{"", "a", "Σ", "ΑΣ", "ΣΣΣ", "straße", "İ", "ǅ", "1a", "a1b", "é.é", "ﬁﬅ",
		"ἈΣ", "Α᾿Η", "ΣͅΣ", "ῌΣ", "ᾈΨ", "µµ", "ǅǅ", "ßßß", "᾿Ἠ", "ͅΣ", "ᾼ", "Σ’Α"}
	for _, a := range alph {
		for _, b := range alph {
			out = append(out, string([]rune{a, b}))
			for _, c := range alph {
				out = append(out, string([]rune{a, b, c}))
			}
		}
	}
	// every cased code point on its own and in the sigma's two contexts, which is where a table that
	// disagrees with the sets beside it shows up
	for cp := 0x80; cp < 0x2FFF; cp++ {
		r := rune(cp)
		if cp >= 0xD800 && cp <= 0xDFFF {
			continue
		}
		out = append(out, string(r), "Α"+string(r)+"Σ", "a"+string(r), string(r)+"Σ")
	}
	return out
}

// TestTheCaseEngineSweepsAgainstTheReference asks CPython for the five operations over the whole corpus in
// one process and compares every answer to the engine's. ADR 0263's rounding row is the precedent: the
// sweep is the only thing that can say a table and two sets agree over 1.1 million code points, and a
// hand-picked list can only say the thirty cases somebody thought of still work.
func TestTheCaseEngineSweepsAgainstTheReference(t *testing.T) {
	t.Parallel()
	corpus := caseSweepStrings()
	// The reference is asked for the whole corpus at once: one process, and the answer is the data.
	encoded, err := json.Marshal(corpus)
	if err != nil {
		t.Fatalf("marshal the corpus: %v", err)
	}
	probe := `import json,sys
cases = json.loads(sys.stdin.read())
ans = []
for s in cases:
    ans.append([s.upper(), s.lower(), s.title(), s.capitalize(), s.swapcase()])
sys.stdout.write(json.dumps(ans))
`
	cmd := exec.Command("python3", "-c", probe)
	cmd.Stdin = strings.NewReader(string(encoded))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the reference could not answer the sweep: %v", err)
	}
	var want [][]string
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatalf("the reference's sweep answer is not JSON: %v", err)
	}
	if len(want) != len(corpus) {
		t.Fatalf("the reference answered %d rows for %d strings", len(want), len(corpus))
	}
	bad := 0
	for i, s := range corpus {
		for _, o := range caseOpsOnTable {
			got, err := caseMapString(o.op, s)
			if err != nil {
				t.Fatalf("the engine would not answer %q (%v)", s, err)
			}
			if got != want[i][int(o.op)] {
				bad++
				if bad <= 10 {
					t.Errorf("%q.%s(): the engine answers %q, the reference answers %q", s, o.name, got, want[i][int(o.op)])
				}
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d swept answers disagree with the reference (roadmap L11.5, ADR 0362)", bad, len(corpus)*5)
	}
}

// caseFoldProgram is a text the compiler CAN read: every answer comes from the fold.
func caseFoldProgram(text, method string) string {
	return fmt.Sprintf("print(%q.%s())\n", text, method)
}

// caseDoorProgram is the same question about a text the compiler CANNOT read: the receiver is a parameter,
// so the answer is the runtime door's.
func caseDoorProgram(text, method string) string {
	return fmt.Sprintf("def f(s):\n    return s.%s()\nprint(f(%q))\n", method, text)
}

// caseTexts are the answers the compiled legs are asked for. They cover each operation's hard case, both
// of the door's scratch-buffer paths (a mapping that grows the text and one that does not), and the
// multi-byte encodings, including the four-byte one.
var caseTexts = []string{
	"straße", "STRASSE", "ǅ", "İ", "ΑΣ", "ΣΣΣ", "ͅΣ", "é.é", "1a", "aÉ", "ǳ", "日本語", "🎉🎉", "µ",
	"a", "ß", "ﬁﬅ", "ᾈ", "  á", "ı", "Ω", "123", "'Σ",
}

// TestTheFoldAndTheDoorAnswerTheSameCase is the one-table claim, run instead of asserted. Each text is
// compiled twice — once where the compiler can read it, once where it cannot — and the two runs are
// compared to each other and to the reference. A fold and a door that read different tables are the shape
// that hid here for as long as the two had different implementations.
func TestTheFoldAndTheDoorAnswerTheSameCase(t *testing.T) {
	t.Parallel()
	for _, text := range caseTexts {
		text := text
		for _, o := range caseOpsOnTable {
			o := o
			t.Run(text+"/"+o.name, func(t *testing.T) {
				t.Parallel()
				foldSrc := caseFoldProgram(text, o.name)
				doorSrc := caseDoorProgram(text, o.name)
				want, err := caseReference(t, doorSrc)
				if err != nil {
					t.Fatalf("%v", err)
				}
				fold := compiledOut(t, foldSrc)
				door := compiledOut(t, doorSrc)
				if fold != want {
					t.Fatalf("the FOLD printed %q, the reference prints %q (%s)", fold, want, foldSrc)
				}
				if door != want {
					t.Fatalf("the DOOR printed %q, the reference prints %q (%s)", door, want, doorSrc)
				}
			})
		}
	}
}

// TestTheCaseDoorModulesVerify pins the IR leg: the door's module has to survive `llc`, which is where a
// malformed loop, a misplaced phi or a buffer size spelled in the wrong unit shows up.
func TestTheCaseDoorModulesVerify(t *testing.T) {
	t.Parallel()
	for _, text := range caseTexts {
		for _, o := range caseOpsOnTable {
			ir := emittedModuleIR(t, caseDoorProgram(text, o.name))
			if !strings.Contains(ir, "define internal i32 @rt_str_case(") {
				t.Fatalf("the case door is missing from the module:\n%s", firstLines(ir, 20))
			}
			mustVerifyWithLLC(t, ir)
		}
	}
}

// TestTheCaseBlockIsGated is ADR 0354's gate rule asked of the largest block this cycle adds. The tables
// are the reference's answer for 1.1 million code points; a program that never asks a case question has no
// business carrying them, and a program whose text the compiler can read answers without ever needing the
// door.
func TestTheCaseBlockIsGated(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(1)\n",
		"print(\"straße\")\n",
		"print(\"straße\".upper())\n",                 // the fold answers: no door, no tables
		"xs = [1, 2]\nprint(len(xs))\n",               // nothing case-shaped at all
		"print(\"%s\" % \"straße\")\n",                // the formatting door borrows the printers, not the tables
		"s = \"a\"\nprint(s + \"ß\")\n",               // concatenation, not a case question
		"def f(s):\n    return s.strip()\nf(\"a\")\n", // a text method that is not a case question
	} {
		ir := emittedModuleIR(t, src)
		if strings.Contains(ir, "@rt_ct_upper_lo") || strings.Contains(ir, "define internal i32 @rt_ct_map(") {
			t.Fatalf("the case tables were emitted for a program that asks no case question:\n%s", firstLines(ir, 20))
		}
	}
	for _, src := range []string{
		"def f(s):\n    return s.upper()\nprint(f(\"a\"))\n",
		"def f(s):\n    return s.title()\nprint(f(\"a\"))\n",
		"def f(s):\n    return s.capitalize()\nprint(f(\"a\"))\n",
		"def f(s):\n    return s.swapcase()\nprint(f(\"a\"))\n",
		"def f(s):\n    return s.lower()\nprint(f(\"a\"))\n",
	} {
		ir := emittedModuleIR(t, src)
		for _, want := range []string{
			"define internal i32 @rt_str_case(", // the door
			"@rt_ct_cased_lo",                   // the two sets the rules read
			"@rt_ct_lower_lo",                   // the table the rules search
			"define internal i1 @rt_ct_final_sigma(",
			"define internal i32 @rt_ct_apply(",
		} {
			if !strings.Contains(ir, want) {
				t.Fatalf("the door asks for %q, which the module does not carry:\n%s", want, firstLines(ir, 20))
			}
		}
	}
}

// TestTheAsciiCaseDoorStaysDead keeps the shape that caused this row from coming back. The old runtime
// door was a loop over ASCII that copied every other byte unchanged — right for `CAFé`-style ASCII and
// wrong for everything else, which is the answer the reference never produces.
func TestTheAsciiCaseDoorStaysDead(t *testing.T) {
	t.Parallel()
	if strings.Contains(heapRuntimeIRTmpl, "define internal i32 @rt_str_case(") {
		t.Fatal("the ASCII case door's implementation returned to the runtime block (roadmap L11.5, ADR 0362)")
	}
	if strings.Contains(heapRuntimeIRTmpl, "folds ASCII letters") {
		t.Fatal("the ASCII case door's comment is back, which means its body is too")
	}
}

// TestTheFoldRoadsAskTheTableAndNotGo is the shape guard: the three fold roads must read the generated
// table, because a road that reaches for Go's `strings.ToUpper` again answers a text the compiler can read
// differently from the same text arriving as a parameter (ADR 0343's shape guard is the precedent).
func TestTheFoldRoadsAskTheTableAndNotGo(t *testing.T) {
	t.Parallel()
	src := readCodegenSources(t)
	for _, o := range caseOpsOnTable {
		if !strings.Contains(src, "caseFoldFold("+caseConstForFold(o.op)) &&
			!strings.Contains(src, "caseMapString("+caseConstForFold(o.op)) {
			t.Errorf("no fold road asks the table for %s — one road back on Go's answer is a fold and a door that disagree", o.name)
		}
	}
	// The five operations must not borrow Go\'s simple mapping anywhere in codegen.
	for _, bad := range []string{`case "upper":` + "\n" + "\t\t\treturn strings.ToUpper(v), true", `case "lower":` + "\n" + "\t\t\treturn strings.ToLower(v), true"} {
		if strings.Contains(src, bad) {
			t.Fatal("a fold road returned the Go case answer again (roadmap L11.5, ADR 0362)")
		}
	}
}

// caseConstForFold spells the operation constant the fold roads are expected to name.
func caseConstForFold(op int) string {
	switch op {
	case caseUpper:
		return "caseUpper"
	case caseLower:
		return "caseLower"
	case caseTitle:
		return "caseTitle"
	case caseCapitalize:
		return "caseCapitalize"
	case caseSwapcase:
		return "caseSwapcase"
	}
	return "?"
}

// TestTheCaseTableIsTheArtifactItClaims is the artifact-count rule asked of the new generated file: the
// provenance block states counts, and the arrays it sits beside are the thing that has to match them.
func TestTheCaseTableIsTheArtifactItClaims(t *testing.T) {
	t.Parallel()
	var raw caseArtifactShape
	if err := json.Unmarshal(caseTableArtifact, &raw); err != nil {
		t.Fatalf("the artifact does not parse: %v", err)
	}
	if raw.Meta.Schema != 1 {
		t.Errorf("meta.schema = %d, want 1", raw.Meta.Schema)
	}
	if raw.Meta.GeneratedBy != "tools/casetab" {
		t.Errorf("meta.generated_by = %q — the table is generated, and by that tool", raw.Meta.GeneratedBy)
	}
	if !strings.HasPrefix(raw.Meta.Reference, "CPython") {
		t.Errorf("meta.reference = %q, want the reference the probe asked", raw.Meta.Reference)
	}
	// The surrogate range is not a character a well-formed string can hold, so the probe asked the rest.
	if raw.Meta.Codepoints != 0x110000-0x800 {
		t.Errorf("meta.codepoints = %d, want %d (the whole range minus the surrogates)", raw.Meta.Codepoints, 0x110000-0x800)
	}
	for name, m := range map[string]caseArtifactMode{
		"upper": raw.Upper, "lower": raw.Lower, "title": raw.Title, "swapcase": raw.Swapcase,
	} {
		if got := m.ownEntryCount(); got != raw.Meta.Counts[name] {
			t.Errorf("the %s table carries %d entries, meta.counts says %d — the count is the artifact's own (Gap R.204's rule)", name, got, raw.Meta.Counts[name])
		}
		if len(m.Ranges) == 0 {
			t.Errorf("the %s table has no ranges", name)
		}
		for _, r := range m.Ranges {
			if len(r) != 3 || r[0] > r[1] {
				t.Fatalf("the %s table carries a range %v that is not (lo, hi, delta) with lo <= hi", name, r)
			}
		}
	}
	if len(raw.Cased) == 0 || len(raw.Ignorable) == 0 {
		t.Fatal("one of the two behavioural sets is empty, which no probe produced")
	}
}

// caseArtifactMode is one operation\'s tables as the artifact spells them.
type caseArtifactMode struct {
	Ranges [][]int32           `json:"ranges"`
	Multi  []caseArtifactMulti `json:"multi"`
}

type caseArtifactMulti struct {
	From int32   `json:"from"`
	To   []int32 `json:"to"`
}

// ownEntryCount is how many code points the mode answers for: every row of every range plus every
// one-to-many entry.
func (m caseArtifactMode) ownEntryCount() int {
	n := 0
	for _, r := range m.Ranges {
		if len(r) == 2 || len(r) == 3 {
			n += int(r[1] - r[0] + 1)
		}
	}
	return n + len(m.Multi)
}

type caseArtifactShape struct {
	Meta struct {
		Schema      int            `json:"schema"`
		GeneratedBy string         `json:"generated_by"`
		Reference   string         `json:"reference"`
		Codepoints  int            `json:"codepoints"`
		Counts      map[string]int `json:"counts"`
		Cased       int            `json:"cased"`
		Ignorable   int            `json:"ignorable"`
		WS          int            `json:"ws"`
	} `json:"meta"`
	Cased     [][]int32        `json:"cased"`
	Ignorable [][]int32        `json:"ignorable"`
	WS        [][]int32        `json:"ws"`
	Upper     caseArtifactMode `json:"upper"`
	Lower     caseArtifactMode `json:"lower"`
	Title     caseArtifactMode `json:"title"`
	Swapcase  caseArtifactMode `json:"swapcase"`
}

// TestTheCaseRulesAreAskedOfTheTableAndNotOfAGuess keeps the engine honest about where its answers come
// from: the two behavioural sets are the reference\'s own scans, and ͅ is the character that proves it — it
// is cased AND case-ignorable at once, which a paraphrase copied from either side gets wrong, and it is
// what makes "ͅΣ".lower() answer ͅσ and not ͅς.
func TestTheCaseRulesAreAskedOfTheTableAndNotOfAGuess(t *testing.T) {
	t.Parallel()
	tbl, err := loadCaseTable()
	if err != nil {
		t.Fatalf("the table would not load: %v", err)
	}
	if !tbl.isCased(0x345) {
		t.Error("prosgegrammeni is not cased in the table; the reference title() treats it as cased")
	}
	if !tbl.isIgnorable(0x345) {
		t.Error("prosgegrammeni is not case-ignorable in the table; the reference sigma scan steps over it")
	}
	if tbl.isIgnorable('a') {
		t.Error("'a' is case-ignorable in the table; it is not to the reference")
	}
	if !tbl.isCased('a') || !tbl.isCased('A') {
		t.Error("a latin letter is not cased in the table")
	}
	if tbl.isCased('1') || tbl.isCased('.') {
		t.Error("a digit or a dot is cased in the table; title() would not start a word after it")
	}
	for _, o := range caseOpsOnTable {
		if caseOpName(o.op) == "" {
			t.Errorf("operation %d has no name", o.op)
		}
	}
}

// TestTheCaseDoorRefusesWhatNoDoorAnswers: a receiver that is not a text keeps the refusal the container
// rows already own — the new door must not turn `d.upper()` into an answer.
func TestTheCaseDoorRefusesWhatNoDoorAnswers(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {\"a\": 1}\nprint(d.upper())\n",
		"xs = [1, 2]\nprint(xs.title())\n",
	} {
		out, refused := compiledOutOrRefusal(t, src)
		if !refused {
			t.Fatalf("%q answered %q where the row records a refusal for a container receiver", src, out)
		}
		if strings.Contains(out, "*lang.") {
			t.Fatalf("the refusal leaks an AST node name: %s", out)
		}
	}
}

// adr0362MeasuredRows is the measured table of ADR 0362 written as Go, so the ADR's claim is an
// executable one rather than a paragraph. Each row was measured against the pre-ADR binary — every one
// of them answered WRONG at exit 0 or refused a program the reference runs — and each row is asked here
// of the compiler and of CPython inside the same case, so the sentence "the compiled leg answers the
// reference" cannot be quietly reinterpreted when the compiler drifts.
var adr0362MeasuredRows = []string{
	// The case doors over a text the compiler can read.
	"print(\"stra\\u00dfe\".upper())\n",    // STRASSE, not STRAßE
	"print(\"\\ufb01\".upper())\n",         // FI — one character answers two
	"print(\"\\u0130\".lower())\n",         // i + U+0307 — one character answers two
	"print(\"\\u0391\\u03a3\".lower())\n",  // ας — the final sigma
	"print(\"\\u00e9.\\u00e9\".title())\n", // É.É — a word begins after any uncased character
	"print(\"ab-cd\".title())\n",           // Ab-Cd
	"print(\"\\u01c5\".title())\n",         // ǅ — a titlecase letter's title form is itself
	// The same doors over a text the compiler cannot read.
	"def f(s):\n    return s.upper()\n\nprint(f(\"stra\\u00dfe\"))\n",
	"def f(s):\n    return s.lower()\n\nprint(f(\"\\u00c4S\"))\n",
	"def f(s):\n    return s.title()\n\nprint(f(\"\\u00e9.\\u00e9\"))\n",
	"def f(s):\n    return s.swapcase()\n\nprint(f(\"\\u0130\"))\n",
	// The trim doors, whose whitespace set is the reference's and not Go's.
	"print(\" \\x1cx \".strip())\n",          // x — U+001C is whitespace to the reference
	"print(\"caf\\u00e9 x\".strip(\"c\"))\n", // afé x — a cutset is read as code points
	"def f(s):\n    return s.strip()\n\nprint(f(\"  caf\\u00e9  \"))\n",
	"def f(s):\n    return s.strip()\n\nprint(f(\"\\u00df\\ufb01\"))\n",
	// The predicate that decides whether a value is a text at all.
	"def g():\n    return \"caf\\u00e9\"\n\ndef f(s):\n    return \"\\u00e9\" in s\n\nprint(f(g()))\n",
	"def f(s):\n    x = s[1:3]\n    return x\n\nprint(f(\"caf\\u00e9\"))\n",
	"def g():\n    return \"caf\\u00e9\"\n\ndef f(s):\n    return s[1:3]\n\nprint(f(g()))\n",
}

// TestTheAdrsMeasuredRowsAnswerTheReference is that table: compiled output byte for byte the
// reference's, at exit 0, with no refusal hiding behind an empty stdout.
func TestTheAdrsMeasuredRowsAnswerTheReference(t *testing.T) {
	t.Parallel()
	for _, src := range adr0362MeasuredRows {
		src := src
		t.Run(adr0362RowName(src), func(t *testing.T) {
			t.Parallel()
			want, _, refErr := PythonRun(src)
			if refErr != nil {
				t.Fatalf("the reference could not answer the program the ADR measured: %v", refErr)
			}
			out, refused := compiledOutOrRefusal(t, src)
			if refused {
				t.Fatalf("the compiled leg refuses a program the reference runs (exit 0, %q): %s\n%s",
					want, out, src)
			}
			if out != want {
				t.Fatalf("the compiled leg answers %q, the reference answers %q\n%s", out, want, src)
			}
		})
	}
}

// adr0362RowName turns a program into a subtest name: the first line, with the whitespace flattened, so
// a failure names the program rather than a number.
func adr0362RowName(src string) string {
	line := strings.SplitN(strings.TrimSpace(src), "\n", 2)[0]
	line = strings.ReplaceAll(line, "\t", " ")
	if len(line) > 48 {
		line = line[:48] + "…"
	}
	return line
}
