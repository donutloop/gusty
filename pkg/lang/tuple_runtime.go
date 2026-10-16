package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// The immutable container's runtime half (roadmap L11.3, ADR 0361).
//
// A tuple is a container whose OBJECT says it is a tuple. The compiled heap has one container
// layout — `{kind, length, [256 x i32]}` beside a per-element tag table — and the parentheses a
// tuple is spelled with are not a second layout, they are a fact about how the value prints, that
// it must not be mutated, and what tag it answers. That fact lives in the object's `@estr[h]`
// attribute word, in the bit below, for the same reason ADR 0251 made the element kinds an
// object's own: the expression that built a container is not the thing that knows what the
// container is. The bit is set when a literal written with parentheses is built, and read by the
// printer, the container dispatcher, and every door that would otherwise write into a slot the
// language promises never changes.
const (
	// estrBitTuple is the tuple flag inside @estr[h]. Bits 1/2/4 are "these positions hold interned
	// text" (elements, dict keys, dict values) and bit 8 is "the slots are self-describing" (ADR 0187);
	// rt_alloc clears the word, so a recycled slot cannot keep a dead tuple's flag.
	estrBitTuple = 16
)

// tupleKindTokens spells every kind constant the doors below compare against out of the canonical
// table, so no tag number is hand-written here (ADR 0171, and the rule ADR 0360 applied to the
// formatting door for the same reason).
func tupleKindTokens() []string {
	var rep []string
	for _, t := range ValueTagList() {
		rep = append(rep,
			"@TAG_"+strings.ToUpper(kindForTag(t))+"@", strconv.Itoa(int(t)))
	}
	return rep
}

// tupleRuntimeBlock is the immutable container's half of the runtime: the paren printer, the
// container dispatcher's tuple branch, and the container question the element printer asks.
//
// The printer is NOT a second renderer. Every element goes through `rt_print_mixed_value`, the one
// door ADR 0185 established and ADR 0303 made the module's single tag-reading printer; what the
// tuple adds is the two brackets and the single-element comma, which is the whole of the difference
// between `(1, 2)` and `[1, 2]` in the reference too.
func tupleRuntimeBlock() string {
	rep := []string{"@TUPLEBIT@", strconv.Itoa(estrBitTuple)}
	rep = append(rep, tupleKindTokens()...)
	body := tupleRuntimeGlobals() + tupleRuntimePrinters()
	// The block reads the arena's own tables (`@heap`, `@heap_tags`, `@estr`), so it spells their size
	// as the same @HEAPSLOTS@ placeholder every other arena-touching block spells it and is filled by
	// the same door — a module cannot print tuples out of one arena and allocate them from another
	// (roadmap Gap Q.2, ADR 0350).
	return fillRootTemplate(strings.NewReplacer(rep...).Replace(body))
}

// tupleRuntimeGlobals spells the two brackets and the lone-element comma. The lengths are computed
// from the texts, never typed: a hand-counted `[N x i8]` is the bug class ADR 0360 recorded for the
// formatting door's message constants. The separator and the end-of-line are the heap block's own
// `.fmtsep`/`.fmtnl`, borrowed here rather than spelled a second time.
func tupleRuntimeGlobals() string {
	var b strings.Builder
	for _, c := range []struct{ name, text string }{
		{".fmtpopen", "("},
		{".fmtpclose", ")"},
		{".fmtpsingle", ","},
	} {
		b.WriteString(fmt.Sprintf("@%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n",
			c.name, len(c.text)+1, c.text))
	}
	return b.String()
}

func tupleRuntimePrinters() string {
	return `
; rt_tag_is_container answers the question the element printer was hand-spelling three tag
; comparisons for: does this tag name a container whose own printer must be asked? Tuple is the
; fourth member of the family, and it is in the answer because a tuple inside a list is the same
; value it is anywhere else (roadmap L11.3, ADR 0361).
define internal i1 @rt_tag_is_container(i32 %t) {
entry:
  %isList = icmp eq i32 %t, @TAG_LIST@
  br i1 %isList, label %yes, label %chkDict
chkDict:
  %isDict = icmp eq i32 %t, @TAG_DICT@
  br i1 %isDict, label %yes, label %chkSet
chkSet:
  %isSet = icmp eq i32 %t, @TAG_SET@
  br i1 %isSet, label %yes, label %chkTuple
chkTuple:
  %isTuple = icmp eq i32 %t, @TAG_TUPLE@
  br i1 %isTuple, label %yes, label %no
yes:
  ret i1 true
no:
  ret i1 false
}

; rt_print_list_or_tuple is the branch the container dispatcher takes when an object's kind says
; "container of the list layout": the layout is shared, the presentation is not, and the object's
; own @estr[h] bit is what tells the two spellings apart. A name bound to a tuple, a tuple handed
; to a function and returned, and a tuple read out of a list all reach the same door, which is the
; point of putting the flag on the object rather than in the expression.
define internal void @rt_print_list_or_tuple(i32 %h) {
entry:
  %eslot = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %h
  %flags = load i32, i32* %eslot
  %isTuple = and i32 %flags, @TUPLEBIT@
  %hasTuple = icmp ne i32 %isTuple, 0
  br i1 %hasTuple, label %tup, label %lst
tup:
  call void @rt_print_tuple(i32 %h, i32 0)
  ret void
lst:
  call void @rt_print_list(i32 %h, i32 0)
  ret void
}

; rt_print_tuple renders the immutable container with Python's brackets and Python's lone-element
; comma: (), (1, 2), (1,). The elements are read the way the mixed list printer reads them — a
; payload beside its tag, from the object — and rendered by rt_print_mixed_value, the module's one
; tag-reading element printer, quoted because a container shows its elements by repr (ADR 0185).
define internal void @rt_print_tuple(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtpopen, i32 0, i32 0))
  %empty = icmp eq i32 %len, 0
  br i1 %empty, label %close, label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %i1, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %single
body:
  %is0 = icmp eq i32 %i, 0
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %e = load i32, i32* %ep
  %tp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %t = load i32, i32* %tp
  br i1 %is0, label %first, label %sep
first:
  call void @rt_print_mixed_value(i32 %e, i32 %t, i32 1)
  br label %cont
sep:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  call void @rt_print_mixed_value(i32 %e, i32 %t, i32 1)
  br label %cont
cont:
  %i1 = add i32 %i, 1
  br label %loop
single:
  ; (1,) — the comma is the syntax that keeps a one-element tuple a tuple, so the reference prints
  ; it and this printer has to print it; the empty tuple already skipped here.
  %one = icmp eq i32 %len, 1
  br i1 %one, label %comma, label %close
comma:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtpsingle, i32 0, i32 0))
  br label %close
close:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtpclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

; rt_tuple_flag answers the equality door with the object's own tuple bit, masked here rather than at
; the call site: the heap block must not spell a bit it does not own, and an equality that compared
; slots alone would answer True for a tuple beside a list with the same slots -- two values the language distinguishes
; (roadmap L11.3, ADR 0361).
define internal i32 @rt_tuple_flag(i32 %h) {
entry:
  %eslot = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %h
  %flags = load i32, i32* %eslot
  %isTuple = and i32 %flags, @TUPLEBIT@
  ret i32 %isTuple
}

; rt_print_tuple_value is how a tuple reaches the element printer: the tag says "tuple", the object
; says how to render it. The quote flag is accepted and ignored, because a tuple is bracketed in
; every context — the brackets are not quotes.
define internal void @rt_print_tuple_value(i32 %h, i32 %quote) {
entry:
  call void @rt_print_tuple(i32 %h, i32 0)
  ret void
}
`
}

// tupleRuntimeIR is the assembled block, ready to be spliced after the heap block it borrows from.
var tupleRuntimeIR = tupleRuntimeBlock()
