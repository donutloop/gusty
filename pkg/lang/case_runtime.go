package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// The case door's runtime half (roadmap L11.5, ADR 0362).
//
// A text the compiler cannot read — a parameter, an element read out of a container, the answer of a call
// — has to answer `upper()`/`lower()`/`title()`/`capitalize()`/`swapcase()` as well, and the door that was
// supposed to answer lived in the runtime as a loop over ASCII: `print(f("café").upper())` printed `CAFé`,
// and the three operations whose rules did not fit in that loop were refused outright. The rules are the
// ones `pkg/lang/case_table.go` states, and they are far too large to type here — so the table this door
// walks is emitted out of the SAME artifact the fold reads (`testdata/case-table.json`, generated from the
// reference by `tools/casetab`). One question, one table, two readers (ADR 0232's rule), and
// `TestTheFoldAndTheDoorAnswerTheSameCase` is what makes it a claim instead of a comment.
//
// The shape of the emitted module:
//
//   - one pair of sorted range arrays per operation plus a delta array, plus a one-to-many list whose
//     answers are flattened into one text array, plus the cased and case-ignorable sets. Every count is
//     computed from the artifact — a hand-counted `[N x i32]` is the bug class ADR 0360 recorded for the
//     formatting door's message constants;
//   - two binary searches, which is what makes the cost per character a handful of loads instead of a scan
//     over 1.1 million rows;
//   - the five rules, reading the code points and their neighbours; the sigma question looks BACK and then
//     FORWARD over the case-ignorable characters, exactly as the reference's own scan does;
//   - decode, map, encode — three loops. Mapping is done on code points because a character's answer can be
//     several characters (`ß`→`SS`, `İ`→`i`+U+0307), and the sigma rule indexes the ORIGINAL code points,
//     because that is what the reference's scans read. A byte that is not valid UTF-8 is carried as a RAW
//     marker (0x110000 + the byte) and written back unchanged, so the door cannot corrupt what it copies.
//
// The block is gated on `caseOps`: a module that never asks a case question ships the module it always
// shipped, byte for byte (ADR 0354's gate rule).

// caseRuntimeTableIR spells the emitted tables. The element lists are printed by one helper per shape, so a
// mode cannot be emitted with a length that disagrees with its own data. The gates say which half the module
// asked for: the case tables ride the case bits, the whitespace set rides the trim bits, and a module that
// asks neither question carries none of it.
func caseRuntimeTableIR(t *caseTable, gates int) string {
	var b strings.Builder
	// The two behavioural sets are one pair of arrays each, spelled the same way the operation tables are:
	// one length, printed from the rows the artifact actually carries.
	set := func(name string, rows [][2]int32) {
		los := make([]int32, len(rows))
		his := make([]int32, len(rows))
		for j, r := range rows {
			los[j], his[j] = r[0], r[1]
		}
		b.WriteString(renderI32Global("rt_"+name+"_lo", los))
		b.WriteString(renderI32Global("rt_"+name+"_hi", his))
	}
	if gates&caseGateCase != 0 {
		set("ct_cased", t.cased)
		set("ct_ign", t.ignorable)
	}
	if gates&caseGateTrim != 0 {
		set("ct_ws", t.ws)
	}
	if gates&caseGateCase == 0 {
		return b.String()
	}
	for i, name := range []string{"upper", "lower", "title", "swapcase"} {
		m := &t.coded[i]
		los := make([]int32, len(m.ranges))
		his := make([]int32, len(m.ranges))
		ds := make([]int32, len(m.ranges))
		for j, r := range m.ranges {
			los[j], his[j], ds[j] = r[0], r[1], r[2]
		}
		b.WriteString(renderI32Global("rt_ct_"+name+"_lo", los))
		b.WriteString(renderI32Global("rt_ct_"+name+"_hi", his))
		b.WriteString(renderI32Global("rt_ct_"+name+"_delta", ds))
		froms := make([]int32, len(m.multi))
		lens := make([]int32, len(m.multi))
		offs := make([]int32, len(m.multi))
		var text []int32
		for j, e := range m.multi {
			froms[j] = e.From
			lens[j] = int32(len(e.To))
			offs[j] = int32(len(text))
			text = append(text, e.To...)
		}
		b.WriteString(renderI32Global("rt_ct_"+name+"_mfrom", froms))
		b.WriteString(renderI32Global("rt_ct_"+name+"_mlen", lens))
		b.WriteString(renderI32Global("rt_ct_"+name+"_moff", offs))
		b.WriteString(renderI32Global("rt_ct_"+name+"_text", text))
	}
	return b.String()
}

// renderI32Global prints one `@name = private global [N x i32] [...]` line, with the length the element
// list actually has.
func renderI32Global(name string, vals []int32) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = "i32 " + strconv.FormatInt(int64(v), 10)
	}
	body := "zeroinitializer"
	if len(parts) > 0 {
		body = "[" + strings.Join(parts, ", ") + "]"
	}
	return "@" + name + " = private global [" + strconv.Itoa(len(vals)) + " x i32] " + body + "\n"
}

// caseMaxExpansion is the widest answer any single character can give, which is what sizes the door's
// scratch buffers. Measured off the artifact, so widening the table can never outrun the allocation.
func caseMaxExpansion(t *caseTable) int32 {
	max := int32(1)
	for i := 0; i < 4; i++ {
		for _, e := range t.coded[i].multi {
			if int32(len(e.To)) > max {
				max = int32(len(e.To))
			}
		}
	}
	return max
}

// caseRuntimeBlock is the whole runtime half. The tables and the numbers the rules compare against arrive
// as placeholders filled from the artifact, because none of them may be typed here. `gates` is the module's
// own list of questions (the case operations it folded over a text it could not read, and the trim roads it
// called): a part the module never asked is not emitted at all (ADR 0354's gate rule).
func caseRuntimeBlock(t *caseTable, gates int) string {
	if t == nil || gates == 0 {
		return ""
	}
	if gates&caseGateTrim != 0 && len(t.ws) == 0 {
		return ""
	}
	count := func(name string) string {
		switch name {
		case "upper":
			return strconv.Itoa(len(t.coded[caseTableUpper].ranges))
		case "lower":
			return strconv.Itoa(len(t.coded[caseTableLower].ranges))
		case "title":
			return strconv.Itoa(len(t.coded[caseTableTitle].ranges))
		default:
			return strconv.Itoa(len(t.coded[caseTableSwap].ranges))
		}
	}
	mcount := func(name string) string {
		switch name {
		case "upper":
			return strconv.Itoa(len(t.coded[caseTableUpper].multi))
		case "lower":
			return strconv.Itoa(len(t.coded[caseTableLower].multi))
		case "title":
			return strconv.Itoa(len(t.coded[caseTableTitle].multi))
		default:
			return strconv.Itoa(len(t.coded[caseTableSwap].multi))
		}
	}
	tcount := func(name string) string {
		total := 0
		switch name {
		case "upper":
			for _, e := range t.coded[caseTableUpper].multi {
				total += len(e.To)
			}
		case "lower":
			for _, e := range t.coded[caseTableLower].multi {
				total += len(e.To)
			}
		case "title":
			for _, e := range t.coded[caseTableTitle].multi {
				total += len(e.To)
			}
		default:
			for _, e := range t.coded[caseTableSwap].multi {
				total += len(e.To)
			}
		}
		return strconv.Itoa(total)
	}
	rep := []string{
		"@TABLES@", caseRuntimeTableIR(t, gates),
		"@NCASED@", strconv.Itoa(len(t.cased)),
		"@NIGN@", strconv.Itoa(len(t.ignorable)),
		"@NWS@", strconv.Itoa(len(t.ws)),
		"@NUPPER@", count("upper"), "@MUPPER@", mcount("upper"), "@TUPPER@", tcount("upper"),
		"@NLOWER@", count("lower"), "@MLOWER@", mcount("lower"), "@TLOWER@", tcount("lower"),
		"@NTITLE@", count("title"), "@MTITLE@", mcount("title"), "@TTITLE@", tcount("title"),
		"@NSWAP@", count("swapcase"), "@MSWAP@", mcount("swapcase"), "@TSWAP@", tcount("swapcase"),
		"@MODEUPPER@", strconv.Itoa(caseUpper),
		"@MODELOWER@", strconv.Itoa(caseLower),
		"@MODETITLE@", strconv.Itoa(caseTitle),
		"@MODECAP@", strconv.Itoa(caseCapitalize),
		"@MODESWAP@", strconv.Itoa(caseSwapcase),
		"@MODETRIMB@", strconv.Itoa(caseTrimBoth),
		"@MODETRIML@", strconv.Itoa(caseTrimLeft),
		"@MODETRIMR@", strconv.Itoa(caseTrimRight),
		"@SIGMAU@", strconv.Itoa(int(caseSigmaUpper)),
		"@SIGMAF@", strconv.Itoa(int(caseSigmaFinal)),
		"@RAWBASE@", strconv.Itoa(int(caseRawByteBase)),
		"@OUTCAP@", strconv.Itoa(int(caseMaxExpansion(t))),
		// The map buffer is one i32 per character the answers can occupy, so its byte size is the
		// expansion times the word — the two are different numbers and the allocator takes bytes.
		"@OUTCAPBYTES@", strconv.Itoa(int(caseMaxExpansion(t)) * 4),
	}
	// The two gated parts are rendered first: a strings.Replacer reads its input once, so a part substituted
	// into the head would never get its own placeholders filled.
	rc := strings.NewReplacer(rep...)
	casePart := ""
	if gates&caseGateCase != 0 {
		casePart = rc.Replace(caseRuntimeCasePart)
	}
	trimPart := ""
	if gates&caseGateTrim != 0 {
		trimPart = rc.Replace(caseRuntimeTrimPart)
	}
	return strings.NewReplacer(append(rep, "@CASEPART@", casePart, "@TRIMPART@", trimPart)...).Replace(caseRuntimeTemplate)
}

// caseRuntimeTemplate is the door's code, in the three gated parts the block is assembled from: the head
// every reader needs (the range search), the case rules and the trim door. `caseRuntimeBlock` emits a part
// only when the module asked a question that part answers (ADR 0354's gate rule).
const caseRuntimeTemplate = `
; ---------------------------------------------------------------------------
; Case mapping (ADR 0362). The tables below are generated from the reference by tools/casetab and emitted
; by pkg/lang/case_runtime.go; the compile-time fold in pkg/lang/case_table.go reads the same artifact, so
; a text the compiler can read and a text it cannot are answered by one engine (ADR 0232). Mapping is by
; code point and never by byte (ADR 0225); a byte that is not valid UTF-8 is carried through unchanged.
; ---------------------------------------------------------------------------
@TABLES@

; rt_ct_find_range is the range search: the index of the row whose [lo,hi] holds cp, or -1.
define internal i32 @rt_ct_find_range(i32* %lo, i32* %hi, i32 %n, i32 %cp) {
entry:
  br label %bs
bs:
  %a = phi i32 [ 0, %entry ], [ %a2, %step ]
  %b = phi i32 [ %n, %entry ], [ %b2, %step ]
  %done = icmp sge i32 %a, %b
  br i1 %done, label %miss, label %body
body:
  %sum = add i32 %a, %b
  %mid = sdiv i32 %sum, 2
  %lap = getelementptr i32, i32* %lo, i32 %mid
  %l = load i32, i32* %lap
  %below = icmp slt i32 %cp, %l
  %hap = getelementptr i32, i32* %hi, i32 %mid
  %h = load i32, i32* %hap
  %after = icmp sgt i32 %cp, %h
  %notbelow = icmp eq i1 %below, false
  %notafter = icmp eq i1 %after, false
  %hit = and i1 %notbelow, %notafter
  br i1 %hit, label %found, label %step
step:
  %midp1 = add i32 %mid, 1
  %a2 = select i1 %after, i32 %midp1, i32 %a
  %b2 = select i1 %below, i32 %mid, i32 %b
  br label %bs
found:
  ret i32 %mid
miss:
  ret i32 -1
}

; ---------------------------------------------------------------------------
; The case rules (ADR 0362). Emitted only for a module that asks a case question: they carry the reference's
; answer for 1.1 million code points, which an ordinary program has no business shipping.
; ---------------------------------------------------------------------------
@CASEPART@
@TRIMPART@
`

// caseRuntimeCasePart is the case half: the one-to-many search, the two behavioural sets the rules read, the
// sigma question, the five rules, and the decode/map/encode door.
const caseRuntimeCasePart = `
; rt_ct_find_from is the same search over the one-to-many list's sorted keys, where a row answers only its
; own code point.
define internal i32 @rt_ct_find_from(i32* %from, i32 %n, i32 %cp) {
entry:
  br label %bs
bs:
  %a = phi i32 [ 0, %entry ], [ %a2, %step ]
  %b = phi i32 [ %n, %entry ], [ %b2, %step ]
  %done = icmp sge i32 %a, %b
  br i1 %done, label %miss, label %body
body:
  %sum = add i32 %a, %b
  %mid = sdiv i32 %sum, 2
  %fat = getelementptr i32, i32* %from, i32 %mid
  %f = load i32, i32* %fat
  %before = icmp slt i32 %f, %cp
  %after = icmp sgt i32 %f, %cp
  %either = or i1 %before, %after
  %isHit = icmp eq i1 %either, false
  br i1 %isHit, label %found, label %step
step:
  %midp1 = add i32 %mid, 1
  %a2 = select i1 %before, i32 %midp1, i32 %a
  %b2 = select i1 %after, i32 %mid, i32 %b
  br label %bs
found:
  ret i32 %mid
miss:
  ret i32 -1
}

define internal i1 @rt_ct_cased(i32 %cp) {
entry:
  %p0 = bitcast [@NCASED@ x i32]* @rt_ct_cased_lo to i32*
  %p1 = bitcast [@NCASED@ x i32]* @rt_ct_cased_hi to i32*
  %i = call i32 @rt_ct_find_range(i32* %p0, i32* %p1, i32 @NCASED@, i32 %cp)
  %hit = icmp sge i32 %i, 0
  ret i1 %hit
}

define internal i1 @rt_ct_ignorable(i32 %cp) {
entry:
  %p0 = bitcast [@NIGN@ x i32]* @rt_ct_ign_lo to i32*
  %p1 = bitcast [@NIGN@ x i32]* @rt_ct_ign_hi to i32*
  %i = call i32 @rt_ct_find_range(i32* %p0, i32* %p1, i32 @NIGN@, i32 %cp)
  %hit = icmp sge i32 %i, 0
  ret i1 %hit
}

; rt_ct_final_sigma is the reference's sigma rule: ς when a cased character sits before the sigma and no
; cased character sits after it, both scans stepping OVER the case-ignorable characters. That is what makes
; "ΑΣΒ" answer ασβ and "ΑΣ" answer ας — and what makes ͅ, which is cased and ignorable at once, answer ͅσ.
define internal i1 @rt_ct_final_sigma(i32* %cps, i32 %n, i32 %i) {
entry:
  %bs = sub i32 %i, 1
  br label %back
back:
  %j = phi i32 [ %bs, %entry ], [ %jnext, %back-step ]
  %hasj = icmp sge i32 %j, 0
  br i1 %hasj, label %back-check, label %no
back-check:
  %jat = getelementptr i32, ptr %cps, i32 %j
  %jc = load i32, i32* %jat
  %jign = call i1 @rt_ct_ignorable(i32 %jc)
  br i1 %jign, label %back-step, label %back-ask
back-step:
  %jnext = sub i32 %j, 1
  br label %back
back-ask:
  %jcased = call i1 @rt_ct_cased(i32 %jc)
  %nothing-before = icmp eq i1 %jcased, false
  br i1 %nothing-before, label %no, label %fwd-start
fwd-start:
  %fs = add i32 %i, 1
  br label %fwd
fwd:
  %k = phi i32 [ %fs, %fwd-start ], [ %knext, %fwd-step ]
  %hask = icmp slt i32 %k, %n
  br i1 %hask, label %fwd-check, label %yes
fwd-check:
  %kat = getelementptr i32, ptr %cps, i32 %k
  %kc = load i32, i32* %kat
  %kign = call i1 @rt_ct_ignorable(i32 %kc)
  br i1 %kign, label %fwd-step, label %fwd-ask
fwd-step:
  %knext = add i32 %k, 1
  br label %fwd
fwd-ask:
  %kcased = call i1 @rt_ct_cased(i32 %kc)
  br i1 %kcased, label %no, label %yes
yes:
  ret i1 true
no:
  ret i1 false
}

; rt_ct_apply asks the one-to-many list first — a range cannot carry a multi-character answer — and falls
; back to the range table, whose answer is the code point plus the row's delta. Returns how many code
; points it wrote into %out.
define internal i32 @rt_ct_apply(i32* %mfrom, i32* %mlen, i32* %moff, i32* %text, i32 %mn, i32* %lo, i32* %hi, i32* %delta, i32 %n, i32 %cp, i32* %out) {
entry:
  %mi = call i32 @rt_ct_find_from(i32* %mfrom, i32 %mn, i32 %cp)
  %multi = icmp sge i32 %mi, 0
  br i1 %multi, label %copy, label %ranges
copy:
  %lenv = getelementptr i32, i32* %mlen, i32 %mi
  %len = load i32, i32* %lenv
  %offv = getelementptr i32, i32* %moff, i32 %mi
  %off = load i32, i32* %offv
  br label %copyloop
copyloop:
  %k = phi i32 [ 0, %copy ], [ %knext, %copybody ]
  %kdone = icmp slt i32 %k, %len
  br i1 %kdone, label %copybody, label %copyend
copybody:
  %offk = add i32 %off, %k
  %tat = getelementptr i32, i32* %text, i32 %offk
  %tv = load i32, i32* %tat
  %oat = getelementptr i32, ptr %out, i32 %k
  store i32 %tv, i32* %oat
  %knext = add i32 %k, 1
  br label %copyloop
copyend:
  ret i32 %len
ranges:
  %ri = call i32 @rt_ct_find_range(i32* %lo, i32* %hi, i32 %n, i32 %cp)
  %hit = icmp sge i32 %ri, 0
  br i1 %hit, label %mapped, label %unchanged
mapped:
  %dat = getelementptr i32, i32* %delta, i32 %ri
  %dv = load i32, i32* %dat
  %mappedcp = add i32 %cp, %dv
  store i32 %mappedcp, i32* %out
  ret i32 1
unchanged:
  store i32 %cp, i32* %out
  ret i32 1
}

; rt_ct_map is the five rules, in the order the fold's switch states them. '%cps'/'%n' are the ORIGINAL
; code points, because that is what the sigma rule reads, and '%out' is where the answer's code points go.
define internal i32 @rt_ct_map(i32 %mode, i32 %cp, i32 %i, i32* %cps, i32 %n, i32* %out) {
entry:
  %is-upper = icmp eq i32 %mode, @MODEUPPER@
  br i1 %is-upper, label %do-upper, label %ask-lower
ask-lower:
  %is-lower = icmp eq i32 %mode, @MODELOWER@
  br i1 %is-lower, label %do-lower, label %ask-title
ask-title:
  %is-title = icmp eq i32 %mode, @MODETITLE@
  br i1 %is-title, label %do-title, label %ask-cap
ask-cap:
  %is-cap = icmp eq i32 %mode, @MODECAP@
  br i1 %is-cap, label %do-cap, label %do-swap

do-upper:
  %u0 = bitcast [@NUPPER@ x i32]* @rt_ct_upper_lo to i32*
  %u1 = bitcast [@NUPPER@ x i32]* @rt_ct_upper_hi to i32*
  %u2 = bitcast [@NUPPER@ x i32]* @rt_ct_upper_delta to i32*
  %um0 = bitcast [@MUPPER@ x i32]* @rt_ct_upper_mfrom to i32*
  %um1 = bitcast [@MUPPER@ x i32]* @rt_ct_upper_mlen to i32*
  %um2 = bitcast [@MUPPER@ x i32]* @rt_ct_upper_moff to i32*
  %um3 = bitcast [@TUPPER@ x i32]* @rt_ct_upper_text to i32*
  %urn = call i32 @rt_ct_apply(i32* %um0, i32* %um1, i32* %um2, i32* %um3, i32 @MUPPER@, i32* %u0, i32* %u1, i32* %u2, i32 @NUPPER@, i32 %cp, i32* %out)
  ret i32 %urn

do-lower:
  %s1 = icmp eq i32 %cp, @SIGMAU@
  br i1 %s1, label %sigma1, label %low1
sigma1:
  %f1 = call i1 @rt_ct_final_sigma(i32* %cps, i32 %n, i32 %i)
  br i1 %f1, label %final1, label %low1
final1:
  store i32 @SIGMAF@, i32* %out
  ret i32 1
low1:
  %l0 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_lo to i32*
  %l1 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_hi to i32*
  %l2 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_delta to i32*
  %lm0 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_mfrom to i32*
  %lm1 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_mlen to i32*
  %lm2 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_moff to i32*
  %lm3 = bitcast [@TLOWER@ x i32]* @rt_ct_lower_text to i32*
  %lrn = call i32 @rt_ct_apply(i32* %lm0, i32* %lm1, i32* %lm2, i32* %lm3, i32 @MLOWER@, i32* %l0, i32* %l1, i32* %l2, i32 @NLOWER@, i32 %cp, i32* %out)
  ret i32 %lrn

; title: a word starts where the previous character is not cased; the character at a word start takes its
; TITLE mapping and every other character takes its LOWER mapping, sigma rule included.
do-title:
  %first1 = icmp eq i32 %i, 0
  %prev = sub i32 %i, 1
  %pat = getelementptr i32, ptr %cps, i32 %prev
  %pv = load i32, i32* %pat
  %pc = call i1 @rt_ct_cased(i32 %pv)
  %not-before = icmp eq i1 %pc, false
  %word-start = or i1 %first1, %not-before
  %isc = call i1 @rt_ct_cased(i32 %cp)
  %title-this = and i1 %word-start, %isc
  br i1 %title-this, label %tit, label %titrest
tit:
  %t0 = bitcast [@NTITLE@ x i32]* @rt_ct_title_lo to i32*
  %t1 = bitcast [@NTITLE@ x i32]* @rt_ct_title_hi to i32*
  %t2 = bitcast [@NTITLE@ x i32]* @rt_ct_title_delta to i32*
  %tm0 = bitcast [@MTITLE@ x i32]* @rt_ct_title_mfrom to i32*
  %tm1 = bitcast [@MTITLE@ x i32]* @rt_ct_title_mlen to i32*
  %tm2 = bitcast [@MTITLE@ x i32]* @rt_ct_title_moff to i32*
  %tm3 = bitcast [@TTITLE@ x i32]* @rt_ct_title_text to i32*
  %trn = call i32 @rt_ct_apply(i32* %tm0, i32* %tm1, i32* %tm2, i32* %tm3, i32 @MTITLE@, i32* %t0, i32* %t1, i32* %t2, i32 @NTITLE@, i32 %cp, i32* %out)
  ret i32 %trn
titrest:
  %s2 = icmp eq i32 %cp, @SIGMAU@
  br i1 %s2, label %sigma2, label %low2
sigma2:
  %f2 = call i1 @rt_ct_final_sigma(i32* %cps, i32 %n, i32 %i)
  br i1 %f2, label %final2, label %low2
final2:
  store i32 @SIGMAF@, i32* %out
  ret i32 1
low2:
  %b0 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_lo to i32*
  %b1 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_hi to i32*
  %b2 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_delta to i32*
  %bm0 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_mfrom to i32*
  %bm1 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_mlen to i32*
  %bm2 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_moff to i32*
  %bm3 = bitcast [@TLOWER@ x i32]* @rt_ct_lower_text to i32*
  %brn = call i32 @rt_ct_apply(i32* %bm0, i32* %bm1, i32* %bm2, i32* %bm3, i32 @MLOWER@, i32* %b0, i32* %b1, i32* %b2, i32 @NLOWER@, i32 %cp, i32* %out)
  ret i32 %brn

; capitalize titles the FIRST character and lower-cases every later one, whatever sits in between — the
; reading that makes '1A'.capitalize() answer 1a and not 1A.
do-cap:
  %at-start = icmp eq i32 %i, 0
  br i1 %at-start, label %capt, label %caprest
capt:
  %c0 = bitcast [@NTITLE@ x i32]* @rt_ct_title_lo to i32*
  %c1 = bitcast [@NTITLE@ x i32]* @rt_ct_title_hi to i32*
  %c2 = bitcast [@NTITLE@ x i32]* @rt_ct_title_delta to i32*
  %cm0 = bitcast [@MTITLE@ x i32]* @rt_ct_title_mfrom to i32*
  %cm1 = bitcast [@MTITLE@ x i32]* @rt_ct_title_mlen to i32*
  %cm2 = bitcast [@MTITLE@ x i32]* @rt_ct_title_moff to i32*
  %cm3 = bitcast [@TTITLE@ x i32]* @rt_ct_title_text to i32*
  %crn = call i32 @rt_ct_apply(i32* %cm0, i32* %cm1, i32* %cm2, i32* %cm3, i32 @MTITLE@, i32* %c0, i32* %c1, i32* %c2, i32 @NTITLE@, i32 %cp, i32* %out)
  ret i32 %crn
caprest:
  %s3 = icmp eq i32 %cp, @SIGMAU@
  br i1 %s3, label %sigma3, label %low3
sigma3:
  %f3 = call i1 @rt_ct_final_sigma(i32* %cps, i32 %n, i32 %i)
  br i1 %f3, label %final3, label %low3
final3:
  store i32 @SIGMAF@, i32* %out
  ret i32 1
low3:
  %d0 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_lo to i32*
  %d1 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_hi to i32*
  %d2 = bitcast [@NLOWER@ x i32]* @rt_ct_lower_delta to i32*
  %dm0 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_mfrom to i32*
  %dm1 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_mlen to i32*
  %dm2 = bitcast [@MLOWER@ x i32]* @rt_ct_lower_moff to i32*
  %dm3 = bitcast [@TLOWER@ x i32]* @rt_ct_lower_text to i32*
  %drn = call i32 @rt_ct_apply(i32* %dm0, i32* %dm1, i32* %dm2, i32* %dm3, i32 @MLOWER@, i32* %d0, i32* %d1, i32* %d2, i32 @NLOWER@, i32 %cp, i32* %out)
  ret i32 %drn

do-swap:
  %s4 = icmp eq i32 %cp, @SIGMAU@
  br i1 %s4, label %sigma4, label %swappp
sigma4:
  %f4 = call i1 @rt_ct_final_sigma(i32* %cps, i32 %n, i32 %i)
  br i1 %f4, label %final4, label %swappp
final4:
  store i32 @SIGMAF@, i32* %out
  ret i32 1
swappp:
  %w0 = bitcast [@NSWAP@ x i32]* @rt_ct_swapcase_lo to i32*
  %w1 = bitcast [@NSWAP@ x i32]* @rt_ct_swapcase_hi to i32*
  %w2 = bitcast [@NSWAP@ x i32]* @rt_ct_swapcase_delta to i32*
  %wm0 = bitcast [@MSWAP@ x i32]* @rt_ct_swapcase_mfrom to i32*
  %wm1 = bitcast [@MSWAP@ x i32]* @rt_ct_swapcase_mlen to i32*
  %wm2 = bitcast [@MSWAP@ x i32]* @rt_ct_swapcase_moff to i32*
  %wm3 = bitcast [@TSWAP@ x i32]* @rt_ct_swapcase_text to i32*
  %wrn = call i32 @rt_ct_apply(i32* %wm0, i32* %wm1, i32* %wm2, i32* %wm3, i32 @MSWAP@, i32* %w0, i32* %w1, i32* %w2, i32 @NSWAP@, i32 %cp, i32* %out)
  ret i32 %wrn
}

; rt_str_case is the door the compiler calls: decode, map with the neighbours visible, encode, intern. The
; mode operand is generated from the fold's own operation order, so no call site spells a number.
define internal i32 @rt_str_case(i32 %s, i32 %mode) {
entry:
  %p = call i8* @rt_str_ptr(i32 %s)
  %n = call i32 @rt_str_nchars(i32 %s)
  %n1 = add i32 %n, 1
  %cpsz = mul i32 %n1, 4
  %cpsz64 = zext i32 %cpsz to i64
  %cps = call ptr @malloc(i64 %cpsz64)
  ; One i32 per possible answer character, so the answer's WIDTH has to be multiplied by the word size as
  ; well — sizing this in code points instead of bytes walked off the end of the allocation at seven
  ; characters and left glibc to notice.
  %outsz = mul i32 %n1, @OUTCAPBYTES@
  %outsz64 = zext i32 %outsz to i64
  %out = call ptr @malloc(i64 %outsz64)
  br label %dec
dec:
  %bo = phi i32 [ 0, %entry ], [ %bonext1, %advance-one ], [ %bonext2, %advance-seq ]
  %ci = phi i32 [ 0, %entry ], [ %cinext1, %advance-one ], [ %cinext2, %advance-seq ]
  %more = icmp slt i32 %ci, %n
  br i1 %more, label %dec-ascii, label %decoded
dec-ascii:
  %bpat = getelementptr i8, i8* %p, i32 %bo
  %b0 = load i8, i8* %bpat
  %u0 = zext i8 %b0 to i32
  %ascii = icmp ult i32 %u0, 128
  %iso = getelementptr i32, ptr %cps, i32 %ci
  br i1 %ascii, label %dec-one, label %dec-lead
dec-one:
  store i32 %u0, i32* %iso
  br label %advance-one
dec-lead:
  %m2 = and i32 %u0, 224
  %is2 = icmp eq i32 %m2, 192
  %m3 = and i32 %u0, 240
  %is3 = icmp eq i32 %m3, 224
  %m4 = and i32 %u0, 248
  %is4 = icmp eq i32 %m4, 240
  %len4 = select i1 %is4, i32 4, i32 1
  %len3 = select i1 %is3, i32 3, i32 %len4
  %len = select i1 %is2, i32 2, i32 %len3
  %base2 = and i32 %u0, 31
  %base3 = and i32 %u0, 15
  %base4 = and i32 %u0, 7
  %b4 = select i1 %is4, i32 %base4, i32 0
  %b3 = select i1 %is3, i32 %base3, i32 %b4
  %b2 = select i1 %is2, i32 %base2, i32 %b3
  br label %dec-cont
dec-cont:
  %k = phi i32 [ 1, %dec-lead ], [ %knext, %dec-cont-acc ]
  %acc = phi i32 [ %b2, %dec-lead ], [ %accn, %dec-cont-acc ]
  %kmore = icmp slt i32 %k, %len
  br i1 %kmore, label %dec-cont-body, label %dec-cont-done
dec-cont-body:
  %coff = add i32 %bo, %k
  %cbpat = getelementptr i8, i8* %p, i32 %coff
  %cb = load i8, i8* %cbpat
  %cu = zext i8 %cb to i32
  %cm = and i32 %cu, 192
  %iscont = icmp eq i32 %cm, 128
  %notcont = icmp eq i1 %iscont, false
  br i1 %notcont, label %dec-raw, label %dec-cont-acc
dec-cont-acc:
  %clow = and i32 %cu, 63
  %shift = shl i32 %acc, 6
  %accn = or i32 %shift, %clow
  %knext = add i32 %k, 1
  br label %dec-cont
dec-cont-done:
  %cat = getelementptr i32, ptr %cps, i32 %ci
  store i32 %acc, i32* %cat
  br label %advance-seq
dec-raw:
  ; Not a well-formed sequence: keep the byte, do not reinterpret it. @RAWBASE@ is above every code point,
  ; so the encoder writes exactly that byte back — the door cannot corrupt what it copies.
  %raw = add i32 %u0, @RAWBASE@
  %rat = getelementptr i32, ptr %cps, i32 %ci
  store i32 %raw, i32* %rat
  br label %advance-one
advance-one:
  ; A plain byte, an ASCII byte, or a malformed lead: one byte consumed either way.
  %cinext1 = add i32 %ci, 1
  %bonext1 = add i32 %bo, 1
  br label %dec
advance-seq:
  ; A well-formed sequence: the lead byte said how long it was.
  %cinext2 = add i32 %ci, 1
  %bonext2 = add i32 %bo, %len
  br label %dec
decoded:
  ; The map loop writes one run of answers, compacted, so the encoder does not have to know how wide any
  ; single answer was. %w is how many code points the answers occupy.
  br label %map
map:
  %mi = phi i32 [ 0, %decoded ], [ %minext, %map-body ]
  %w = phi i32 [ 0, %decoded ], [ %wnext, %map-body ]
  %mdone = icmp sge i32 %mi, %n
  br i1 %mdone, label %mapped, label %map-body
map-body:
  %mat = getelementptr i32, ptr %cps, i32 %mi
  %mc = load i32, i32* %mat
  %oat = getelementptr i32, ptr %out, i32 %w
  %written = call i32 @rt_ct_map(i32 %mode, i32 %mc, i32 %mi, i32* %cps, i32 %n, i32* %oat)
  %wnext = add i32 %w, %written
  %minext = add i32 %mi, 1
  br label %map
mapped:
  %bufsz = mul i32 %w, 4
  %bufsz1 = add i32 %bufsz, 1
  %bufsz64 = zext i32 %bufsz1 to i64
  %buf = call ptr @malloc(i64 %bufsz64)
  br label %enc
enc:
  %ei = phi i32 [ 0, %mapped ], [ %enext, %enc-write ]
  %ob = phi i32 [ 0, %mapped ], [ %obnext, %enc-write ]
  %edone = icmp slt i32 %ei, %w
  br i1 %edone, label %enc-body, label %enc-end
enc-body:
  %eat = getelementptr i32, ptr %out, i32 %ei
  %cp = load i32, i32* %eat
  %israw = icmp sge i32 %cp, @RAWBASE@
  br i1 %israw, label %enc-raw, label %enc-size
enc-raw:
  %rawb = sub i32 %cp, @RAWBASE@
  %raw8 = trunc i32 %rawb to i8
  %rw = getelementptr i8, ptr %buf, i32 %ob
  store i8 %raw8, i8* %rw
  br label %enc-write
enc-size:
  %width1 = icmp ult i32 %cp, 128
  %width2 = icmp ult i32 %cp, 2048
  %width3 = icmp ult i32 %cp, 65536
  ; The width and its lead mask are built the same way, widest first and narrowest last, so the narrowest
  ; test that holds is the one that answers: an ASCII character is also "under 2048", and a chain that
  ; asked the questions in the other order gave every byte a continuation mask.
  %nb4 = select i1 %width3, i32 3, i32 4
  %nb3 = select i1 %width2, i32 2, i32 %nb4
  %nb = select i1 %width1, i32 1, i32 %nb3
  %lead4 = select i1 %width3, i32 224, i32 240
  %lead3 = select i1 %width2, i32 192, i32 %lead4
  %leadmask = select i1 %width1, i32 0, i32 %lead3
  br label %enc-bytes
enc-bytes:
  %k2 = phi i32 [ 0, %enc-size ], [ %k2next, %enc-bytes ]
  %six = mul i32 %nb, 6
  %top = sub i32 %six, 6
  %k6 = mul i32 %k2, 6
  %sh = sub i32 %top, %k6
  %part = lshr i32 %cp, %sh
  %low6 = and i32 %part, 63
  %firstbyte = or i32 %part, %leadmask
  %contbyte = or i32 %low6, 128
  %isfirst = icmp eq i32 %k2, 0
  %byte32 = select i1 %isfirst, i32 %firstbyte, i32 %contbyte
  %byte8 = trunc i32 %byte32 to i8
  %bw = getelementptr i8, ptr %buf, i32 %ob
  %bwoff = getelementptr i8, i8* %bw, i32 %k2
  store i8 %byte8, i8* %bwoff
  %k2next = add i32 %k2, 1
  %k2more = icmp slt i32 %k2next, %nb
  br i1 %k2more, label %enc-bytes, label %enc-write
enc-write:
  ; How many bytes the character just written took: one for a raw byte, the sequence width otherwise.
  %stepbytes = phi i32 [ 1, %enc-raw ], [ %nb, %enc-bytes ]
  %enext = add i32 %ei, 1
  %obnext = add i32 %ob, %stepbytes
  br label %enc
enc-end:
  %nul = getelementptr i8, ptr %buf, i32 %ob
  store i8 0, i8* %nul
  %idx = call i32 @rt_str_intern(i8* %buf)
  ret i32 %idx
}
`

// caseRuntimeTrimPart is the trim half (ADR 0362): the reference's whitespace set, the code-point reader the
// two scans share, and the door strip/lstrip/rstrip call. It is emitted only for a module that asked one of
// those three questions.
const caseRuntimeTrimPart = `
; ---------------------------------------------------------------------------
; Trimming (ADR 0362). The set below is the reference's own answer to "which characters does strip() remove"
; — probed by tools/casetab, never typed — and the scan walks CODE POINTS, because a byte is never
; whitespace: every UTF-8 lead and continuation byte is from 0x80 up, and a door that compared bytes signed
; threw away the non-ASCII text it was handed (roadmap L11.5).
; ---------------------------------------------------------------------------

define internal i1 @rt_ct_ws(i32 %cp) {
entry:
  %p0 = bitcast [@NWS@ x i32]* @rt_ct_ws_lo to i32*
  %p1 = bitcast [@NWS@ x i32]* @rt_ct_ws_hi to i32*
  %i = call i32 @rt_ct_find_range(i32* %p0, i32* %p1, i32 @NWS@, i32 %cp)
  %hit = icmp sge i32 %i, 0
  ret i1 %hit
}

; rt_ct_cp_at decodes the one code point that STARTS at %off, by the rules every reader in this block uses: a
; plain ASCII byte, a well-formed 2/3/4-byte sequence, or a raw marker for a byte that is not valid UTF-8.
define internal i32 @rt_ct_cp_at(i8* %p, i32 %off) {
entry:
  %bp = getelementptr i8, i8* %p, i32 %off
  %b0 = load i8, i8* %bp
  %u0 = zext i8 %b0 to i32
  %ascii = icmp ult i32 %u0, 128
  br i1 %ascii, label %one, label %lead
one:
  ret i32 %u0
lead:
  %m2 = and i32 %u0, 224
  %is2 = icmp eq i32 %m2, 192
  %m3 = and i32 %u0, 240
  %is3 = icmp eq i32 %m3, 224
  %m4 = and i32 %u0, 248
  %is4 = icmp eq i32 %m4, 240
  %seqlen = select i1 %is4, i32 4, i32 1
  %seqlen3 = select i1 %is3, i32 3, i32 %seqlen
  %seqlen2 = select i1 %is2, i32 2, i32 %seqlen3
  %b2 = and i32 %u0, 31
  %b3 = and i32 %u0, 15
  %b4 = and i32 %u0, 7
  %s4 = select i1 %is4, i32 %b4, i32 0
  %s3 = select i1 %is3, i32 %b3, i32 %s4
  %s2 = select i1 %is2, i32 %b2, i32 %s3
  br label %cont
cont:
  %k = phi i32 [ 1, %lead ], [ %kn, %acc ]
  %acc0 = phi i32 [ %s2, %lead ], [ %an, %acc ]
  %more = icmp slt i32 %k, %seqlen2
  br i1 %more, label %body, label %done
body:
  %co = add i32 %off, %k
  %cbp = getelementptr i8, i8* %p, i32 %co
  %cb = load i8, i8* %cbp
  %cu = zext i8 %cb to i32
  %cm = and i32 %cu, 192
  %okc = icmp eq i32 %cm, 128
  br i1 %okc, label %acc, label %raw
acc:
  %low = and i32 %cu, 63
  %sh = shl i32 %acc0, 6
  %an = or i32 %sh, %low
  %kn = add i32 %k, 1
  br label %cont
done:
  ret i32 %acc0
raw:
  ; Not a well-formed sequence: the byte stands for itself, and no cutset names it.
  %rawcp = add i32 %u0, @RAWBASE@
  ret i32 %rawcp
}

; rt_ct_cp_bytes is how many bytes the code point occupies on the way out: one for a raw marker, and the
; width its value says otherwise — widest test last, so the narrowest claim that holds is the one that answers.
define internal i32 @rt_ct_cp_bytes(i32 %cp) {
  %israw = icmp sge i32 %cp, @RAWBASE@
  %w1 = icmp ult i32 %cp, 128
  %w2 = icmp ult i32 %cp, 2048
  %w3 = icmp ult i32 %cp, 65536
  %nb4 = select i1 %w3, i32 3, i32 4
  %nb3 = select i1 %w2, i32 2, i32 %nb4
  %nb2 = select i1 %w1, i32 1, i32 %nb3
  %nb = select i1 %israw, i32 1, i32 %nb2
  ret i32 %nb
}

; rt_ct_in_set asks whether cp is a character of the cutset text %set — or, when %set is -1, one of the
; reference's whitespace characters. The cutset is read as code points for the same reason the receiver is.
define internal i1 @rt_ct_in_set(i32 %cp, i32 %set) {
entry:
  %isws = icmp eq i32 %set, -1
  br i1 %isws, label %ws, label %walk
ws:
  %hit = call i1 @rt_ct_ws(i32 %cp)
  ret i1 %hit
walk:
  %p = call i8* @rt_str_ptr(i32 %set)
  %nb = call i32 @rt_str_len(i32 %set)
  br label %loop
loop:
  %o = phi i32 [ 0, %walk ], [ %on, %step ]
  %more = icmp slt i32 %o, %nb
  br i1 %more, label %each, label %no
each:
  %c = call i32 @rt_ct_cp_at(i8* %p, i32 %o)
  %same = icmp eq i32 %c, %cp
  br i1 %same, label %yes, label %step
step:
  %cb = call i32 @rt_ct_cp_bytes(i32 %c)
  %on = add i32 %o, %cb
  br label %loop
yes:
  ret i1 true
no:
  ret i1 false
}

; rt_str_trim is the door the three trim roads call. %cut is the cutset's @str_tab index, or -1 for the
; reference's whitespace set, and %mode is generated from the fold's own operation order — no call site
; spells a number.
define internal i32 @rt_str_trim(i32 %s, i32 %mode, i32 %cut) {
entry:
  %p = call i8* @rt_str_ptr(i32 %s)
  %nb = call i32 @rt_str_len(i32 %s)
  %dohead = icmp ne i32 %mode, @MODETRIMR@
  %dotail = icmp ne i32 %mode, @MODETRIML@
  br label %head
head:
  %lo = phi i32 [ 0, %entry ], [ %lon, %headstep ]
  %inr = icmp slt i32 %lo, %nb
  %gohead = and i1 %dohead, %inr
  br i1 %gohead, label %headdec, label %headend
headdec:
  %c1 = call i32 @rt_ct_cp_at(i8* %p, i32 %lo)
  %inh = call i1 @rt_ct_in_set(i32 %c1, i32 %cut)
  br i1 %inh, label %headstep, label %headend
headstep:
  %b1 = call i32 @rt_ct_cp_bytes(i32 %c1)
  %lon = add i32 %lo, %b1
  br label %head
headend:
  %lofin = phi i32 [ %lo, %head ], [ %lo, %headdec ]
  br label %tail
tail:
  %hi = phi i32 [ %nb, %headend ], [ %st, %tailbody ]
  %above = icmp sgt i32 %hi, %lofin
  %gotail = and i1 %dotail, %above
  br i1 %gotail, label %tailfind, label %tailend
tailfind:
  ; Step back to where the last code point starts: a continuation byte is never a character's first byte.
  %s0 = sub i32 %hi, 1
  br label %walk
walk:
  %st = phi i32 [ %s0, %tailfind ], [ %stn, %walkback ]
  %bpat = getelementptr i8, i8* %p, i32 %st
  %bv = load i8, i8* %bpat
  %bu = zext i8 %bv to i32
  %cm = and i32 %bu, 192
  %iscont = icmp eq i32 %cm, 128
  %over = icmp sgt i32 %st, %lofin
  %keep = and i1 %iscont, %over
  br i1 %keep, label %walkback, label %taildec
walkback:
  %stn = sub i32 %st, 1
  br label %walk
taildec:
  %c2 = call i32 @rt_ct_cp_at(i8* %p, i32 %st)
  %intail = call i1 @rt_ct_in_set(i32 %c2, i32 %cut)
  br i1 %intail, label %tailbody, label %tailend
tailbody:
  br label %tail
tailend:
  %hifin = phi i32 [ %hi, %tail ], [ %hi, %taildec ]
  %len = sub i32 %hifin, %lofin
  %base = getelementptr i8, i8* %p, i32 %lofin
  %idx = call i32 @rt_str_from_bytes(i8* %base, i32 %len)
  ret i32 %idx
}
`

// emitTrimRoad is the trim door's call site: it answers strip/lstrip/rstrip over a receiver the caller
// already holds as a @str_tab index, with the cutset the program named — or the artifact's whitespace set
// when the call named none. Both roads come here (the fold's when it can read the text, the door's when it
// cannot), so the arity rule, the gate bit and the interned-table sentinel live in one place
// (ADR 0229's rule that print and the operation ask one predicate).
func (g *irGen) emitTrimRoad(b *strings.Builder, c *Call, name, recvReg string) (string, error) {
	mode := caseTrimMode(name)
	if mode < 0 {
		return "", fmt.Errorf("codegen: %s() is not a trim road (roadmap L11.5, ADR 0362)", name)
	}
	if len(c.Args) > 1 {
		return "", fmt.Errorf("%s() takes one cutset at most; the reference raises TypeError for %d arguments (roadmap L11.5, ADR 0362)", name, len(c.Args))
	}
	cut := "-1"
	if len(c.Args) == 1 {
		cr, isText, err := g.strReg(b, c.Args[0])
		if err != nil {
			return "", err
		}
		if !isText {
			return "", fmt.Errorf("%s() was given a cutset that is not a text: %s — CPython raises TypeError for it (roadmap L11.5, ADR 0166)", name, g.exprSummary(c.Args[0]))
		}
		cut = cr
	}
	g.caseOps |= 1 << uint(mode)
	r := g.rtStrCall(b, "rt_str_trim", "i32 "+recvReg, "i32 "+strconv.Itoa(mode), "i32 "+cut)
	g.checkStrSentinels(b, r, "RuntimeError", strFullMessage, c.Span(), "sttrim")
	return r, nil
}
