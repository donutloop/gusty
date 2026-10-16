package lang

// Case mapping is the last place in this compiler where an answer came from Go instead of from the
// reference. `strings.ToUpper` implements Unicode's SIMPLE case mapping, and the reference does not
// answer with it: `"ß".upper()` is `SS` and not `ß`, `"ﬁ".upper()` is `FI`, `"İ".lower()` is `i` followed
// by COMBINING DOT ABOVE, and `"ΑΣ".lower()` is `ας` while `"ΑΣΒ".lower()` is `ασβ` — because the last
// sigma of a word is conditional on the characters on either side of it. A fold that borrowed Go's answer
// printed the wrong text at exit 0, and the runtime door that only knew A–Z printed the wrong text for
// every text the compiler could not read. Both are the answer the reference never produces, which ADR 0166
// counts as the worst thing a compiler can do.
//
// So this file holds the one engine. It reads `testdata/case-table.json` — generated from CPython by
// `tools/casetab`, never typed by hand, because a hand-typed case table is a claim about 1.1 million code
// points that nobody can review — and it is the only thing that decides a case answer:
//
//   - the compile-time fold calls caseMapString for a text it can read;
//   - the runtime door `@rt_str_case` (pkg/lang/case_runtime.go) walks the same table, emitted into the
//     module as the same ranges, for a text it cannot;
//   - `TestTheFoldAndTheDoorAnswerTheSameCase` fails the day the two disagree, and
//     `TestTheCaseDoorsAnswerTheReference` asks CPython over a corpus on top of that.
//
// The rules themselves are the reference's, and they are asymmetric in ways a paraphrase loses:
//
//   - upper: every character takes its (possibly multi-character) upper mapping; no context.
//   - lower: the same, except a sigma that has a cased character before it — scanning BACK over the
//     case-ignorable characters — and no cased character after it — scanning FORWARD over them — becomes
//     the final sigma ς.
//   - title: a word starts where the previous character is not cased; the character at a word start takes
//     its TITLE mapping and every other character takes its LOWER mapping (with the sigma rule).
//   - capitalize: the FIRST character takes the title mapping and every later character is lower-cased,
//     whether or not anything cased came before them — `'1A'.capitalize()` is `'1a'`, which is the answer
//     a "title the first cased character" reading gets wrong.
//   - swapcase: the reference's own per-character swapcase mapping, plus the same final-sigma rule.
//
// "Cased" and "case-ignorable" are not transcribed from a Unicode data file either: they are the sets the
// reference's own scans act on, probed for by the generator. That is what makes `ͅ` (GREEK PROSGEGRAMMENI,
// category Mn and titlecase) a member of BOTH, and therefore what makes `"ͅΣ".lower()` answer `ͅσ` — a
// cased character the sigma scan steps over, which a table copied from the wrong source gets wrong.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"unicode/utf8"
)

//go:embed testdata/case-table.json
var caseTableArtifact []byte

// The five case operations, in the order the runtime door's `mode` operand spells them. The door and this
// file share one vocabulary because one engine answers both.

// The four TABLES the operations read, in the order `caseTable.coded` keeps them. They are not the
// operations: `capitalize` reads the title table for one character and the lower table for the rest, and
// `swapcase` reads a table of its own because the reference's swap is not "upper if lower" for the
// titlecase characters. Keeping the two vocabularies apart is what stops a lookup asking for a table that
// does not exist.
const (
	caseUpper = iota
	caseLower
	caseTitle
	caseCapitalize
	caseSwapcase
	// The three trim roads ride the same artifact and the same gate word: `strip`/`lstrip`/`rstrip` trim the
	// reference's whitespace set, which is not the set Go's unicode.IsSpace knows (roadmap L11.5, ADR 0362).
	// They are operations on the same `g.caseOps` gate, which is why they continue this list rather than
	// starting a second numbering nobody keeps in step.
	caseTrimBoth
	caseTrimLeft
	caseTrimRight
)

// caseGateCase is the gate mask for the five case mappings, and caseGateTrim the mask for the three trims;
// the runtime block emits each half only when its own bit is set (ADR 0354's gate rule).
const (
	caseGateCase = 1<<caseUpper | 1<<caseLower | 1<<caseTitle | 1<<caseCapitalize | 1<<caseSwapcase
	caseGateTrim = 1<<caseTrimBoth | 1<<caseTrimLeft | 1<<caseTrimRight
)

// caseTrimMode names the trim road's operation, or -1 when the method is not a trim.
func caseTrimMode(name string) int {
	switch name {
	case "strip":
		return caseTrimBoth
	case "lstrip":
		return caseTrimLeft
	case "rstrip":
		return caseTrimRight
	}
	return -1
}

// The names of the operations, including the trims. The runtime block's gate and every refusal message
// read this, so no call site hand-spells a mode number.
var caseOpNames = [8]string{"upper", "lower", "title", "capitalize", "swapcase", "strip", "lstrip", "rstrip"}

func caseOpName(op int) string {
	if op < 0 || op >= len(caseOpNames) {
		return "case"
	}
	return caseOpNames[op]
}

// The two code points the sigma rule is about, named once.
const (
	caseSigmaUpper  = int32(0x3A3) // Σ
	caseSigmaFinal  = int32(0x3C2) // ς
	caseRawByteBase = int32(0x110000)
)

type caseMultiEntry struct {
	From int32   `json:"from"`
	To   []int32 `json:"to"`
}

type caseModeJSON struct {
	Ranges [][]int32        `json:"ranges"`
	Multi  []caseMultiEntry `json:"multi"`
}

type caseTableJSON struct {
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
	Cased     [][]int32    `json:"cased"`
	Ignorable [][]int32    `json:"ignorable"`
	WS        [][]int32    `json:"ws"`
	Upper     caseModeJSON `json:"upper"`
	Lower     caseModeJSON `json:"lower"`
	Title     caseModeJSON `json:"title"`
	Swapcase  caseModeJSON `json:"swapcase"`
}

// caseTable is the loaded, lookup-shaped form: sorted ranges for a binary search, and the one-to-many
// answers as their own sorted list (they are never folded into a range, because a range answers one code
// point per code point).
type caseTable struct {
	reference string
	coded     [4]caseModeTable // index is caseUpper..caseSwapcase, capitalize has no table of its own
	cased     [][2]int32
	ignorable [][2]int32
	// ws is the set `str.strip()` removes with no cutset, and `caseFoldTrim` and `rt_str_trim` both read it.
	ws [][2]int32
}

type caseModeTable struct {
	ranges [][3]int32 // lo, hi, delta: every code point in [lo,hi] maps to cp+delta
	multi  []caseMultiEntry
}

var (
	caseTableOnce   sync.Once
	caseTableLoaded *caseTable
	caseTableErr    error
)

// loadCaseTable decodes the artifact exactly once. A failure is returned and never papered over with
// Go's answer: a compiler that silently falls back to `strings.ToUpper` when its table fails to parse is
// the silent wrong answer this file exists to remove.
func loadCaseTable() (*caseTable, error) {
	caseTableOnce.Do(func() {
		var raw caseTableJSON
		if err := json.Unmarshal(caseTableArtifact, &raw); err != nil {
			caseTableErr = fmt.Errorf("case table: the generated artifact does not parse: %w", err)
			return
		}
		if raw.Meta.Schema != 1 {
			caseTableErr = fmt.Errorf("case table: artifact schema %d, this compiler reads schema 1", raw.Meta.Schema)
			return
		}
		t := &caseTable{reference: raw.Meta.Reference}
		for i, m := range []caseModeJSON{raw.Upper, raw.Lower, raw.Title, raw.Swapcase} {
			mt := caseModeTable{}
			for _, r := range m.Ranges {
				if len(r) != 3 {
					caseTableErr = fmt.Errorf("case table: a %s range is not (lo, hi, delta): %v", caseOpNames[i], r)
					return
				}
				mt.ranges = append(mt.ranges, [3]int32{r[0], r[1], r[2]})
			}
			sort.Slice(mt.ranges, func(a, b int) bool { return mt.ranges[a][0] < mt.ranges[b][0] })
			mt.multi = append(mt.multi, m.Multi...)
			sort.Slice(mt.multi, func(a, b int) bool { return mt.multi[a].From < mt.multi[b].From })
			t.coded[i] = mt
		}
		t.cased = asRangePairs(raw.Cased)
		t.ignorable = asRangePairs(raw.Ignorable)
		t.ws = asRangePairs(raw.WS)
		if len(t.ws) == 0 {
			caseTableErr = fmt.Errorf("case table: the artifact carries no whitespace ranges, which is not a table anyone probed")
			return
		}
		if len(t.coded[caseUpper].ranges) == 0 || len(t.cased) == 0 {
			caseTableErr = fmt.Errorf("case table: the artifact carries no %s ranges, which is not a table anyone generated", "upper")
			return
		}
		caseTableLoaded = t
	})
	return caseTableLoaded, caseTableErr
}

func asRangePairs(in [][]int32) [][2]int32 {
	out := make([][2]int32, 0, len(in))
	for _, r := range in {
		if len(r) != 2 {
			continue
		}
		out = append(out, [2]int32{r[0], r[1]})
	}
	sort.Slice(out, func(a, b int) bool { return out[a][0] < out[b][0] })
	return out
}

// caseLookup answers one code point under one operation. The multi table is asked FIRST because a
// one-to-many answer is never representable as a range, and the range table then covers the simple
// mappings; nil means the reference leaves the character alone.
func (t *caseTable) caseLookup(slot int, cp int32) []int32 {
	if slot < 0 || slot >= 4 {
		return nil
	}
	mt := &t.coded[slot]
	lo, hi := 0, len(mt.multi)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case mt.multi[mid].From < cp:
			lo = mid + 1
		case mt.multi[mid].From > cp:
			hi = mid - 1
		default:
			return mt.multi[mid].To
		}
	}
	lo, hi = 0, len(mt.ranges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		r := mt.ranges[mid]
		switch {
		case cp < r[0]:
			hi = mid - 1
		case cp > r[1]:
			lo = mid + 1
		default:
			return []int32{cp + r[2]}
		}
	}
	return nil
}

func inRanges(t [][2]int32, cp int32) bool {
	lo, hi := 0, len(t)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case cp < t[mid][0]:
			hi = mid - 1
		case cp > t[mid][1]:
			lo = mid + 1
		default:
			return true
		}
	}
	return false
}

func (t *caseTable) isCased(cp int32) bool     { return inRanges(t.cased, cp) }
func (t *caseTable) isIgnorable(cp int32) bool { return inRanges(t.ignorable, cp) }

// caseIsFinalSigma is the reference's sigma rule, read the way the reference reads it: the scan that
// looks for the cased character before the sigma steps OVER the case-ignorable ones, and so does the scan
// after it. Both directions have to agree before the sigma becomes ς.
func (t *caseTable) caseIsFinalSigma(cps []int32, i int) bool {
	j := i - 1
	for j >= 0 && t.isIgnorable(cps[j]) {
		j--
	}
	if j < 0 || !t.isCased(cps[j]) {
		return false
	}
	k := i + 1
	for k < len(cps) && t.isIgnorable(cps[k]) {
		k++
	}
	if k < len(cps) && t.isCased(cps[k]) {
		return false
	}
	return true
}

// caseMapRunes is the engine. `in` is code points, and the answer is code points, because a mapping can
// be one-to-many and a position in this language is a code point and never a byte (ADR 0225).
func caseMapRunes(op int, in []int32) ([]int32, error) {
	t, err := loadCaseTable()
	if err != nil {
		return nil, err
	}
	out := make([]int32, 0, len(in)+4)
	for i, cp := range in {
		switch op {
		case caseUpper:
			out = appendMapped(out, t.caseLookup(caseTableUpper, cp), cp)
			continue
		case caseSwapcase:
			if cp == caseSigmaUpper && t.caseIsFinalSigma(in, i) {
				out = append(out, caseSigmaFinal)
				continue
			}
			out = appendMapped(out, t.caseLookup(caseTableSwap, cp), cp)
			continue
		case caseLower:
			if cp == caseSigmaUpper && t.caseIsFinalSigma(in, i) {
				out = append(out, caseSigmaFinal)
				continue
			}
			out = appendMapped(out, t.caseLookup(caseTableLower, cp), cp)
			continue
		case caseCapitalize:
			if i == 0 {
				out = appendMapped(out, t.caseLookup(caseTableTitle, cp), cp)
				continue
			}
			if cp == caseSigmaUpper && t.caseIsFinalSigma(in, i) {
				out = append(out, caseSigmaFinal)
				continue
			}
			out = appendMapped(out, t.caseLookup(caseTableLower, cp), cp)
			continue
		case caseTitle:
			if i == 0 || !t.isCased(in[i-1]) {
				if t.isCased(cp) {
					out = appendMapped(out, t.caseLookup(caseTableTitle, cp), cp)
					continue
				}
			}
			if cp == caseSigmaUpper && t.caseIsFinalSigma(in, i) {
				out = append(out, caseSigmaFinal)
				continue
			}
			out = appendMapped(out, t.caseLookup(caseTableLower, cp), cp)
			continue
		default:
			return nil, fmt.Errorf("case table: no such operation %d", op)
		}
	}
	return out, nil
}

func appendMapped(dst []int32, mapped []int32, cp int32) []int32 {
	if mapped == nil {
		return append(dst, cp)
	}
	return append(dst, mapped...)
}

// caseMapString is the fold's door: the text the compiler can read answers the same way the runtime door
// answers the text it cannot.
func caseMapString(op int, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	r := []rune(s)
	in := make([]int32, len(r))
	for i, c := range r {
		in[i] = int32(c)
	}
	out, err := caseMapRunes(op, in)
	if err != nil {
		return "", err
	}
	buf := make([]rune, len(out))
	for i, c := range out {
		buf[i] = rune(c)
	}
	return string(buf), nil
}

// caseFoldFold is the fold roads' door: a text the compiler can read answers from the same table the
// runtime door walks. "false" is not a refusal — it is this fold declining, which sends the question to
// the door that asks the same artifact. A fold that answered Go instead would be the silent wrong answer
// this file exists to remove (roadmap L11.5, ADR 0362).
func caseFoldFold(op int, s string) (string, bool) {
	out, err := caseMapString(op, s)
	if err != nil {
		return "", false
	}
	return out, true
}

// caseTableReference names the reference the table was asked of, for the guard that re-asks it.
func caseTableReference() (string, error) {
	t, err := loadCaseTable()
	if err != nil {
		return "", err
	}
	return t.reference, nil
}

// The four table ids, in the order caseTable.coded keeps them.
const (
	caseTableUpper = iota
	caseTableLower
	caseTableTitle
	caseTableSwap
)

// caseTableSlotOf names the table an operation reads when it reads "its own" table. `capitalize` is not
// one of these: it reads two, and the caller says which for each character.
func caseTableSlotOf(op int) int {
	switch op {
	case caseUpper:
		return caseTableUpper
	case caseLower:
		return caseTableLower
	case caseTitle:
		return caseTableTitle
	case caseSwapcase:
		return caseTableSwap
	}
	return caseTableUpper
}

// caseOpOfMethod turns the method name the program wrote into the operation the door and the fold share.
// The name is the source's own word — nothing here maps a number to a meaning, and a method with no
// operation answers -1 so the call site says so out loud instead of quietly answering `upper`.
func caseOpOfMethod(name string) int {
	switch name {
	case "upper":
		return caseUpper
	case "lower":
		return caseLower
	case "title":
		return caseTitle
	case "capitalize":
		return caseCapitalize
	case "swapcase":
		return caseSwapcase
	}
	return -1
}

// ---------------------------------------------------------------------------
// The trim roads (roadmap L11.5, ADR 0362).
//
// `strip`/`lstrip`/`rstrip` were the other half of the same defect. The fold asked Go's `unicode.IsSpace`,
// which does not remove the four separators U+001C..U+001F that the reference removes, and the runtime door
// compared each byte with a SIGNED compare against 32, so every byte from 0x80 up read as "at or below
// space" — `print(f("ßﬁ"))` printed the empty string at exit 0, and a cutset argument was dropped on the
// floor so `"café x".strip("c")` kept the `c` the program asked it to remove. Both questions now go to one
// artifact: `ws` is what the probe got by asking the reference which characters `strip()` removes, and both
// roads walk it over CODE POINTS — a byte is never whitespace, because every UTF-8 lead and continuation
// byte is from 0x80 up (ADR 0225).
// ---------------------------------------------------------------------------

// caseFoldTrim answers one trim road at compile time. `constStr` is the caller's own way of reading a
// constant text, because the three fold roads each own one; ok=false means "the fold cannot answer this
// call", which is the road's cue to leave the question to the door that reads the same artifact.
func caseFoldTrim(mode int, s string, args []Expr, constStr func(Expr) (string, bool)) (string, bool) {
	if mode < caseTrimBoth || mode > caseTrimRight || len(args) > 1 {
		return "", false
	}
	cutset := ""
	hasCutset := false
	if len(args) == 1 {
		c, ok := constStr(args[0])
		if !ok {
			return "", false
		}
		cutset, hasCutset = c, true
	}
	t, err := loadCaseTable()
	if err != nil {
		return "", false
	}
	return caseTrimBytes(mode, s, cutset, hasCutset, t), true
}

// caseTrimBytes trims a text by code point. A byte that is not a well-formed sequence is not whitespace to
// the reference either — it is a character no cutset names — so it stops the scan and survives, which is the
// same rule the door's raw markers follow.
func caseTrimBytes(mode int, s string, cutset string, hasCutset bool, t *caseTable) string {
	inCut := func(r rune) bool {
		if hasCutset {
			for _, c := range cutset {
				if c == r {
					return true
				}
			}
			return false
		}
		return inRanges(t.ws, int32(r))
	}
	bs := []byte(s)
	lo, hi := 0, len(bs)
	if mode != caseTrimRight {
		for lo < hi {
			r, size := utf8.DecodeRune(bs[lo:])
			if r == utf8.RuneError && size <= 1 {
				break
			}
			if !inCut(r) {
				break
			}
			lo += size
		}
	}
	if mode != caseTrimLeft {
		for hi > lo {
			start := hi - 1
			for start > lo && bs[start]&0xC0 == 0x80 {
				start--
			}
			r, size := utf8.DecodeRune(bs[start:hi])
			if (r == utf8.RuneError && size <= 1) || start+size != hi {
				break
			}
			if !inCut(r) {
				break
			}
			hi = start
		}
	}
	return string(bs[lo:hi])
}
