package lang

// The trim roads (roadmap L11.5, ADR 0362).
//
// `strip`/`lstrip`/`rstrip` were the second text door that measured BYTES, and they failed in two opposite
// directions at once. The fold asked Go's `unicode.IsSpace`, which does not remove U+001C..U+001F — so
// `print("\x1cx".strip())` printed the separator the reference had removed. The runtime door compared every
// byte against 32 with a SIGNED `icmp`, so each byte from 0x80 up counted as "whitespace": a program that
// trimmed a Greek or Latin-accented text got the empty string, at exit 0, in a language whose reference
// never produces it. A cutset argument was dropped on the floor on both roads, which is how the record's
// `"xxhi xx".strip("x") == "hi "` became `"hi xx"`.
//
// Both roads now read one artifact. `ws` in `testdata/case-table.json` is what `tools/casetab` got by asking
// the reference which characters `strip()` removes — not `unicode.IsSpace`, and not this compiler's opinion —
// and both readers walk it over code points, because a byte is never whitespace: every UTF-8 lead and
// continuation byte is from 0x80 up. The rows below ask the three questions that shape can fail in: does the
// engine answer the reference, do the fold and the door answer each other, and is the emitted module honest.

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// trimOpsOnTable is the three trim roads, under the names the program writes them by.
var trimOpsOnTable = []struct {
	op   int
	name string
}{
	{caseTrimBoth, "strip"},
	{caseTrimLeft, "lstrip"},
	{caseTrimRight, "rstrip"},
}

// trimCut names a cutset the program wrote; nil means the call wrote none, which is the artifact's set.
func trimCut(s string) *string { return &s }

// cutRepr renders a cutset for a failure message. Printing the pointer would be a message nobody can read.
func cutRepr(p *string) string {
	if p == nil {
		return "no cutset"
	}
	return fmt.Sprintf("%q", *p)
}

type caseTrimRow struct {
	name   string
	in     string
	op     int
	cutset *string
	want   string
}

// caseTrimRows is the table the two hand-written roads could not answer. It is not a random sample: the
// U+001C row is where Go's set parts company with the reference's, the ß row is where the signed compare
// parted company with any text at all, and the empty-cutset row is where "trim nothing" and "trim spaces"
// are one character apart in a naive loop.
var caseTrimRows = []caseTrimRow{
	{"file separator", "\x1cx", caseTrimBoth, nil, "x"},
	{"group separator", "\x1d\x1ex", caseTrimBoth, nil, "x"},
	{"record separator", "x\x1f", caseTrimRight, nil, "x"},
	{"unit separator both ends", "\x1fx\x1f", caseTrimBoth, nil, "x"},
	{"no-break space", "\u00a0x\u00a0", caseTrimBoth, nil, "x"},
	{"ideographic space", "\u3000x", caseTrimBoth, nil, "x"},
	{"next line", "\u0085x\u0085", caseTrimBoth, nil, "x"},
	{"en quad", "\u2000x", caseTrimLeft, nil, "x"},
	{"fig space", "x\u2007", caseTrimRight, nil, "x"},
	{"line and paragraph separators", "\u2028x\u2029", caseTrimBoth, nil, "x"},
	{"tab and friends", "\t\n\v\f\r x ", caseTrimBoth, nil, "x"},
	{"sharp s and ligature are content", "ßﬁ", caseTrimBoth, nil, "ßﬁ"},
	{"accented text with spaces", "  café ", caseTrimBoth, nil, "café"},
	{"greek between spaces", "  ΑΣ  ", caseTrimLeft, nil, "ΑΣ  "},
	{"cjk full width space", "\u3000日本語", caseTrimBoth, nil, "日本語"},
	{"all whitespace is gone", "   ", caseTrimBoth, nil, ""},
	{"nothing to trim", "café", caseTrimBoth, nil, "café"},
	{"empty is empty", "", caseTrimRight, nil, ""},
	{"cutset both ends", "café x", caseTrimBoth, trimCut("c"), "afé x"},
	{"cutset right only", "hi xx", caseTrimRight, trimCut("x"), "hi "},
	{"cutset left only", "xxhi", caseTrimLeft, trimCut("x"), "hi"},
	{"cutset of spaces and a letter", "  x a  ", caseTrimBoth, trimCut(" xa"), ""},
	{"cutset that leaves something", "  x a b", caseTrimBoth, trimCut(" x"), "a b"},
	{"cutset past ascii", "ß café ß", caseTrimBoth, trimCut("ß "), "café"},
	{"cutset of one wide character", "ΣΣxΣ", caseTrimBoth, trimCut("Σ"), "x"},
	{"empty cutset trims nothing", "  x  ", caseTrimBoth, trimCut(""), "  x  "},
}

// trimReference answers each row — (text, method, cutset-or-nil) — in one python3 process.
func trimReference(t *testing.T, rows [][3]string) []string {
	t.Helper()
	payload := make([][]*string, 0, len(rows))
	for _, r := range rows {
		text, method, cut := r[0], r[1], r[2]
		var cutp *string
		if cut != "\x00none" {
			c := cut
			cutp = &c
		}
		payload = append(payload, []*string{&text, &method, cutp})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal the rows: %v", err)
	}
	probe := `import json,sys
rows = json.loads(sys.stdin.read())
out = []
for s, m, cut in rows:
    fn = getattr(s, m)
    out.append(fn() if cut is None else fn(cut))
sys.stdout.write(json.dumps(out))
`
	cmd := exec.Command("python3", "-c", probe)
	cmd.Stdin = strings.NewReader(string(encoded))
	var errb strings.Builder
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the reference could not answer the trim table: %v %s", err, errb.String())
	}
	var ans []string
	if err := json.Unmarshal(out, &ans); err != nil {
		t.Fatalf("the reference's answer is not the JSON the probe promises: %v", err)
	}
	if len(ans) != len(rows) {
		t.Fatalf("the reference answered %d rows for %d", len(ans), len(rows))
	}
	return ans
}

// trimAsks turns a row into the (text, method, cutset) triple the probe reads.
func trimAsks(tc caseTrimRow, opName string) [3]string {
	cut := "\x00none"
	if tc.cutset != nil {
		cut = *tc.cutset
	}
	return [3]string{tc.in, opName, cut}
}

// nameOfTrimOp is the method name the program wrote for an operation.
func nameOfTrimOp(op int) string {
	for _, o := range trimOpsOnTable {
		if o.op == op {
			return o.name
		}
	}
	return "?"
}

// TestTheTrimRoadsAnswerTheReferencesOwnSet is the row-by-row check. Each row states its own expectation and
// is then asked of the reference in the same case: a row that drifts from CPython fails as loudly as an
// engine that does, which is what keeps this file from becoming the source of truth it is describing.
func TestTheTrimRoadsAnswerTheReferencesOwnSet(t *testing.T) {
	t.Parallel()
	rows := make([][3]string, 0, len(caseTrimRows))
	for _, tc := range caseTrimRows {
		rows = append(rows, trimAsks(tc, nameOfTrimOp(tc.op)))
	}
	want := trimReference(t, rows)
	for i, tc := range caseTrimRows {
		tc, ref := tc, want[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opName := nameOfTrimOp(tc.op)
			if tc.want != ref {
				t.Fatalf("%q.%s(%s): this row states %q, the reference answers %q — the reference is the definition, fix the row",
					tc.in, opName, cutRepr(tc.cutset), tc.want, ref)
			}
			var args []Expr
			constStr := func(Expr) (string, bool) { return "", false }
			if tc.cutset != nil {
				lit := &StrLit{Value: *tc.cutset}
				args = []Expr{lit}
				constStr = func(e Expr) (string, bool) {
					s, ok := e.(*StrLit)
					if !ok {
						return "", false
					}
					return s.Value, true
				}
			}
			got, ok := caseFoldTrim(tc.op, tc.in, args, constStr)
			if !ok {
				t.Fatalf("the engine declined %q.%s(%s)", tc.in, opName, cutRepr(tc.cutset))
			}
			if got != ref {
				t.Fatalf("%q.%s(%s) = %q, the reference answers %q", tc.in, opName, cutRepr(tc.cutset), got, ref)
			}
		})
	}
}

// trimSweepTexts is the corpus the trim roads are swept over: every single code point the reference treats as
// whitespace, each of them in a context where a byte-wise or signed scan reads it differently, and the
// non-whitespace neighbours that must survive.
func trimSweepTexts() []string {
	out := []string{"", " ", "   ", "x", " x ", "\t", "\v", "\f", "\r", "\x1c", "\x1d", "\x1e", "\x1f",
		"\u0085", "\u00a0", "\u1680", "\u2000", "\u200a", "\u2028", "\u2029", "\u202f", "\u205f", "\u3000",
		"\x1cx", "x\x1c", " \x1cx ", "\u00a0café\u00a0", "ß", "ß ", " ß", " ß ", "Σ", " Σ ", " café ", "café \t",
		"日本 語", "0", " 0 ", "\n\t\x1f \x1c", "a", "\u00a0", "µ", "\ufeff x", "\u200b x"}
	for cp := 0x80; cp < 0x3400; cp++ {
		if cp >= 0xD800 && cp <= 0xDFFF {
			continue
		}
		if cp%7 != 0 {
			continue // the whitespace rows are dense; every seventh code point keeps the sweep honest
		}
		r := string(rune(cp))
		out = append(out, r, " "+r, r+" ", " "+r+" ", "\x1c"+r+"\x1c")
	}
	return out
}

// TestTheTrimRoadsSweepAgainstTheReference asks CPython all three trims, with no cutset and with two
// cutsets, over that corpus in one process. A hand-picked list can only say the rows somebody thought of
// still work; this is what says the artifact and the two readers agree.
func TestTheTrimRoadsSweepAgainstTheReference(t *testing.T) {
	t.Parallel()
	texts := trimSweepTexts()
	type ask struct {
		text   string
		op     int
		name   string
		cutset *string
	}
	var asks []ask
	for _, s := range texts {
		for _, o := range trimOpsOnTable {
			asks = append(asks, ask{s, o.op, o.name, nil})
		}
		cutA, cutB := " \tß", "x \t\u00a0"
		asks = append(asks, ask{s, caseTrimBoth, "strip", &cutA}, ask{s, caseTrimLeft, "lstrip", &cutB})
	}
	rows := make([][3]string, 0, len(asks))
	for _, a := range asks {
		rows = append(rows, trimAsks(caseTrimRow{in: a.text, cutset: a.cutset}, a.name))
	}
	want := trimReference(t, rows)
	bad := 0
	for i, a := range asks {
		var args []Expr
		constStr := func(Expr) (string, bool) { return "", false }
		if a.cutset != nil {
			lit := &StrLit{Value: *a.cutset}
			args = []Expr{lit}
			constStr = func(e Expr) (string, bool) {
				s, ok := e.(*StrLit)
				if !ok {
					return "", false
				}
				return s.Value, true
			}
		}
		got, ok := caseFoldTrim(a.op, a.text, args, constStr)
		if !ok {
			t.Fatalf("the engine declined %q.%s()", a.text, a.name)
		}
		if got != want[i] {
			bad++
			if bad <= 12 {
				t.Errorf("%q.%s(%s) = %q, the reference answers %q", a.text, a.name, cutRepr(a.cutset), got, want[i])
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d swept trim answers disagree with the reference (roadmap L11.5, ADR 0362)", bad, len(asks))
	}
}

// trimFoldProgram is a text and cutset the compiler CAN read: the answer comes from the fold.
func trimFoldProgram(text, method string, cutset *string) string {
	if cutset != nil {
		return fmt.Sprintf("print(%q.%s(%q))\n", text, method, *cutset)
	}
	return fmt.Sprintf("print(%q.%s())\n", text, method)
}

// trimDoorProgram is the same question about texts the compiler CANNOT read: both the receiver and the
// cutset arrive as parameters, so the runtime door answers.
func trimDoorProgram(text, method string, cutset *string) string {
	if cutset != nil {
		return fmt.Sprintf("def f(s, c):\n    return s.%s(c)\nprint(f(%q, %q))\n", method, text, *cutset)
	}
	return fmt.Sprintf("def f(s):\n    return s.%s()\nprint(f(%q))\n", method, text)
}

// caseTrimTexts is the corpus the COMPILED legs are asked about — the answers that need a decoder at both
// ends of the buffer, a cutset held as a value, and the scratch path that copies a tail unchanged.
var caseTrimTexts = []string{"  café ", "ßﬁ", " ß", "\x1cx", "\u00a0x\u00a0", "\u3000x", "   ", "café",
	"ΣΣxΣ", "hi xx", "\t\nx\v", "  x a  ", "日本 語", "µ µ"}

// TestTheFoldAndTheDoorAnswerTheSameTrim is the one-artifact claim, run instead of asserted: the same
// question is compiled twice, once where the compiler can read the text and once where it cannot, and both
// runs are compared with the reference. This is the case the old code could not pass in either direction —
// the fold kept U+001C and the door deleted U+00DF.
func TestTheFoldAndTheDoorAnswerTheSameTrim(t *testing.T) {
	t.Parallel()
	cutA, cutB := "c", " x "
	for _, text := range caseTrimTexts {
		for _, o := range trimOpsOnTable {
			for _, cutset := range []*string{nil, &cutA, &cutB} { // 14 x 3 x 3, all parallel
				text, o, cutset := text, o, cutset
				name := o.name
				t.Run(text+"/"+name, func(t *testing.T) {
					t.Parallel()
					foldSrc := trimFoldProgram(text, name, cutset)
					doorSrc := trimDoorProgram(text, name, cutset)
					want, err := caseReference(t, doorSrc)
					if err != nil {
						t.Fatalf("%v", err)
					}
					if foldSrc != doorSrc {
						fold := compiledOut(t, foldSrc)
						if fold != want {
							t.Fatalf("the FOLD printed %q, the reference prints %q (%s)", fold, want, foldSrc)
						}
					}
					door := compiledOut(t, doorSrc)
					if door != want {
						t.Fatalf("the DOOR printed %q, the reference prints %q (%s)", door, want, doorSrc)
					}
				})
			}
		}
	}
}

// TestTheTrimDoorModulesVerify pins the IR leg of the new door: the module has to survive `llc`, which is
// where a malformed loop, a phi with the wrong predecessor, or a walk-back that runs off the front of the
// buffer shows up.
func TestTheTrimDoorModulesVerify(t *testing.T) {
	t.Parallel()
	cut := " x"
	for _, text := range caseTrimTexts[:6] {
		for _, o := range trimOpsOnTable {
			for _, cutset := range []*string{nil, &cut} {
				ir := emittedModuleIR(t, trimDoorProgram(text, o.name, cutset))
				for _, want := range []string{"define internal i32 @rt_str_trim(", "define internal i32 @rt_ct_cp_at(", "define internal i1 @rt_ct_in_set("} {
					if !strings.Contains(ir, want) {
						t.Fatalf("the trim door asks for %q, which the module does not carry:\n%s", want, firstLines(ir, 20))
					}
				}
				mustVerifyWithLLC(t, ir)
			}
		}
	}
}

// TestTheTrimBlockIsGated asks ADR 0354's gate rule of the second half of this block: a program that trims
// must not carry the 1.1-million-code-point case tables, and a program that only case-folds must not carry
// the whitespace set or the trim door.
func TestTheTrimBlockIsGated(t *testing.T) {
	t.Parallel()
	trimOnly := "def f(s):\n    return s.strip()\nprint(f(\" x \"))\n"
	caseOnly := "def f(s):\n    return s.upper()\nprint(f(\"x\"))\n"
	neither := "print(1)\n"

	ir := emittedModuleIR(t, trimOnly)
	for _, want := range []string{"@rt_ct_ws_lo", "define internal i32 @rt_str_trim(", "define internal i1 @rt_ct_ws("} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the trim road asks for %q, which the module does not carry:\n%s", want, firstLines(ir, 20))
		}
	}
	for _, bad := range []string{"@rt_ct_upper_lo", "define internal i32 @rt_ct_map(", "define internal i32 @rt_str_case("} {
		if strings.Contains(ir, bad) {
			t.Fatalf("a program that only trims carries the case tables (%s)", bad)
		}
	}
	mustVerifyWithLLC(t, ir)

	ir = emittedModuleIR(t, caseOnly)
	for _, bad := range []string{"@rt_ct_ws_lo", "define internal i32 @rt_str_trim(", "define internal i32 @rt_ct_cp_at("} {
		if strings.Contains(ir, bad) {
			t.Fatalf("a program that only folds case carries the trim door (%s)", bad)
		}
	}
	for _, want := range []string{"@rt_ct_upper_lo", "define internal i32 @rt_str_case("} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the case door asks for %q, which the module does not carry:\n%s", want, firstLines(ir, 20))
		}
	}
	mustVerifyWithLLC(t, ir)

	for _, src := range []string{neither, "print(\" x \".strip())\n", "print(\"x\".upper())\n"} {
		ir := emittedModuleIR(t, src)
		for _, bad := range []string{"@rt_ct_ws_lo", "@rt_ct_upper_lo", "define internal i32 @rt_str_trim(", "define internal i32 @rt_str_case("} {
			if strings.Contains(ir, bad) {
				t.Fatalf("a program that answers its text at compile time carries the runtime tables (%s):\n%s", bad, firstLines(ir, 20))
			}
		}
	}
}

// TestTheAsciiTrimDoorStaysDead keeps the shape that caused this row from coming back: a loop over bytes
// compared signed, which is right for ASCII and throws away every text that is not.
func TestTheAsciiTrimDoorStaysDead(t *testing.T) {
	t.Parallel()
	if strings.Contains(heapRuntimeIRTmpl, "rt_str_strip") {
		t.Fatal("the byte-wise trim door returned to the runtime block (roadmap L11.5, ADR 0362)")
	}
	for _, src := range []string{"def f(s):\n    return s.strip()\nprint(f(\" x \"))\n"} {
		ir := emittedModuleIR(t, src)
		if strings.Contains(ir, "icmp sgt i8") {
			t.Fatalf("a SIGNED byte compare is back in the emitted module — every byte from 0x80 up is whitespace to it:\n%s", firstLines(ir, 30))
		}
	}
	cg := readCodegenSources(t)
	for _, bad := range []string{`strings.TrimSpace(v)`, "strings.TrimLeftFunc(v, unicode.IsSpace)", "strings.TrimRightFunc(v, unicode.IsSpace)"} {
		if strings.Contains(cg, bad) {
			t.Fatalf("a trim road returned to Go's answer (%s) — the fold and the door must read one artifact (roadmap L11.5, ADR 0362)", bad)
		}
	}
	if n := strings.Count(cg, "caseFoldTrim("); n < 3 {
		t.Fatalf("the fold roads ask the trim table %d times, want 3 — one road back on Go's answer is a fold and a door that disagree", n)
	}
}

// TestTheTrimRoadsKeepTheContainerRefusal is the boundary this row must not cross: `strip` on a container
// stays the refusal the container rows already own, and the new door must not turn `d.strip()` into text.
func TestTheTrimRoadsKeepTheContainerRefusal(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {\"a\": 1}\nprint(d.strip())\n",
		"xs = [1, 2]\nprint(xs.lstrip())\n",
		"xs = [1]\nprint(xs.rstrip(1))\n",
	} {
		out, refused := compiledOutOrRefusal(t, src)
		if !refused {
			t.Fatalf("%q answered %q where the container rows record a refusal", src, out)
		}
		if strings.Contains(out, "*lang.") {
			t.Fatalf("the refusal leaks an AST node name: %s", out)
		}
	}
}

// TestTheTrimArityIsSaidNotIgnored keeps the argument rule in one place: the old roads accepted any number
// of arguments and read none of them, which is a wrong answer dressed as a call.
func TestTheTrimArityIsSaidNotIgnored(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(\" x \".strip(\" \", \"x\"))\n",
		"def f(s):\n    return s.strip(\" \", \"x\")\nprint(f(\" y \"))\n",
	} {
		out, refused := compiledOutOrRefusal(t, src)
		if !refused {
			t.Fatalf("%q answered %q; two cutsets is a TypeError in the reference, not a silently ignored one", src, out)
		}
		if strings.Contains(out, "*lang.") {
			t.Fatalf("the refusal leaks an AST node name: %s", out)
		}
	}
}

// TestTheTrimTableRowsAreTheArtifactsOwn checks the whitespace set the door reads against the counts the
// artifact states and against the two properties the fold depends on: the separators Go's set leaves out are
// IN it, and no letter or digit is.
func TestTheTrimTableRowsAreTheArtifactsOwn(t *testing.T) {
	t.Parallel()
	var raw caseArtifactShape
	if err := json.Unmarshal(caseTableArtifact, &raw); err != nil {
		t.Fatalf("the artifact does not parse: %v", err)
	}
	if raw.Meta.WS == 0 {
		t.Fatal("meta.ws is 0 — the probe asks the reference which characters strip() removes, and it answers 29")
	}
	own := 0
	for _, r := range raw.WS {
		if len(r) != 2 || r[0] > r[1] {
			t.Fatalf("the whitespace table carries a range %v that is not (lo, hi) with lo <= hi", r)
		}
		own += int(r[1] - r[0] + 1)
	}
	if own != raw.Meta.WS {
		t.Errorf("the whitespace rows cover %d code points, meta.ws says %d — the count is the artifact's own (Gap R.204's rule)", own, raw.Meta.WS)
	}
	tbl, err := loadCaseTable()
	if err != nil {
		t.Fatalf("the table would not load: %v", err)
	}
	// The four separators, the NEL, NBSP, the ideographic space: every one of these is whitespace to the
	// reference and NOT to Go's unicode.IsSpace, which is the set the fold used to read.
	for _, cp := range []int32{0x1C, 0x1D, 0x1E, 0x1F, 0x85, 0xA0, 0x3000, 0x2028, 0x2029, 0x2000} {
		if !inRanges(tbl.ws, cp) {
			t.Errorf("U+%04X is not in the whitespace table; the reference strips it", cp)
		}
	}
	for _, cp := range []int32{'a', 'A', '0', 0x00DF, 0x03A3, 0x00E9, 0x4E2D} {
		if inRanges(tbl.ws, cp) {
			t.Errorf("U+%04X is in the whitespace table; it is a character, not a space", cp)
		}
	}
}
