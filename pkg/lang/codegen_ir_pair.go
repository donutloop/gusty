// pairFoldRuntimeIRTmpl is the fold's pair door: the comparison and membership questions a list, dict or set
// of mixed-tagged slots has to be asked at run time.
package lang

// pairFoldRuntimeIR is the fold door's own block (roadmap L11.1, Gap R.146; ADR 0316): the two operator
// symbols a fold spells in its TypeError, the sentence, and the two helpers. It is emitted for a module
// that folds a value whose kind the run time chose and for no other — ADR 0309 asked the same question of
// the signless call ("carry the sentence per kind reached, carry none of it when nothing reaches it"), and
// a sum, which orders nothing, answers without ever naming an ordering. It rides with the arithmetic block,
// because rt_pair_fold asks that block's rt_lift_num and rt_kind_name and writes into its rt.num.msg
// buffer: a referenced internal function that was never emitted is the module llc rejects (ADR 0173).
const pairFoldRuntimeIRTmpl = `@rt.o.lt = private constant [2 x i8] c"<\00"
@rt.o.gt = private constant [2 x i8] c">\00"
; The sentence a fold raises when the two values it was asked to order have no ordering: CPython writes
; the OPERATOR the fold used and the two kinds in the order the comparison had them — the candidate first,
; the incumbent second — which is why one format serves min and max and why the caller passes the pairs in
; fold order rather than in source order (roadmap L11.1, Gap R.146; ADR 0271's "a trap says what the program
; wrote" rule, applied to a call whose comparison the program never wrote out).
@rt.fold.fmt = private constant [65 x i8] c"TypeError: '%s' not supported between instances of '%s' and '%s'\00"

; rt_pair_fold is the fold door: min() and max() choose one of their arguments and hand it back, so the
; answer's kind is the WINNER's kind — min(2.5, 3) is the float and max(2.5, 3) is the integer, which
; is why a fold cannot be lowered by comparing lifted doubles and returning one word. The door is handed
; two (payload, tag) pairs and writes back the winning PAIR: whichever arm the run time takes, the payload
; travels with the tag that says what it means. Two kinds are ordered here in the door itself — the
; numbers, in the one word that holds both families, and the texts, by content through rt_str_order rather
; than by the intern table's arrival order (ADR 0248's rule, one door later). Everything else — a
; container, a dict, a None, and every mismatched pair of those — is asked of rt_pair_order below, which
; walks a container's elements instead of reading a number out of its handle (roadmap Gap R.197) and
; answers CPython's own sentence for whatever has no ordering (ADR 0250/0252's per-kind raise, applied to
; a call; ADR 0316's fold door).
;
; op 0 is min, op 1 is max. %a is the CANDIDATE and %b the incumbent, which is the order the reference
; puts the two kinds in its TypeError, and the reason the fold runs left to right with the first argument
; as the starting incumbent: CPython keeps the incumbent on a tie, so max(True, 1) is True.
define internal i32 @rt_pair_fold(i32 %op, i32 %ap, i32 %at, i32 %bp, i32 %bt, i32* %outp, i32* %outt, i8** %outmsg) {
entry:
  %ismin = icmp eq i32 %op, 0
  ; The two words the raise will name. They start as the operands' own kinds and are overwritten by the
  ; door that fails, which is how a fold over [[1]] and [["a"]] names 'int' and 'str' — the kinds of the
  ; ELEMENTS that failed to order — rather than the two lists the program wrote (roadmap Gap R.197).
  %badlt = alloca i32
  %badrt = alloca i32
  %cmp = alloca i32
  store i32 %at, i32* %badlt
  store i32 %bt, i32* %badrt
  %an = icmp ult i32 %at, 3
  %bn = icmp ult i32 %bt, 3
  %bothnum = and i1 %an, %bn
  br i1 %bothnum, label %num, label %notnum
num:
  %af = call double @rt_lift_num(i32 %ap, i32 %at)
  %bf = call double @rt_lift_num(i32 %bp, i32 %bt)
  %nlt = fcmp olt double %af, %bf
  %ngt = fcmp ogt double %af, %bf
  %ntake = select i1 %ismin, i1 %nlt, i1 %ngt
  br label %win
notnum:
  %atxt = icmp eq i32 %at, 4
  %btxt = icmp eq i32 %bt, 4
  %bothtext = and i1 %atxt, %btxt
  br i1 %bothtext, label %order, label %walk
order:
  %o = call i32 @rt_str_order(i32 %ap, i32 %bp)
  %olt = icmp slt i32 %o, 0
  %ogt = icmp sgt i32 %o, 0
  %otake = select i1 %ismin, i1 %olt, i1 %ogt
  br label %win
walk:
  ; The ordering the fold did not have: lists lexicographically over their own elements, sets by the
  ; subset operator, and a raise that names the two kinds that actually failed for everything else.
  %wst = call i32 @rt_pair_order(i32 0, i32 %ap, i32 %at, i32 %bp, i32 %bt, i32* %cmp, i32* %badlt, i32* %badrt)
  %wraise = icmp eq i32 %wst, 1
  br i1 %wraise, label %bad, label %wcheck
wcheck:
  ; Status 2 is the set answer that is not an ordering: {1} < {2} and {1} > {2} are BOTH false in the
  ; reference, so neither operand is taken and the incumbent survives — which is what min({1}, {2}) is.
  %wneither = icmp eq i32 %wst, 2
  br i1 %wneither, label %keep, label %wcmp
wcmp:
  %wc = load i32, i32* %cmp
  %wlt = icmp slt i32 %wc, 0
  %wgt = icmp sgt i32 %wc, 0
  %wtake = select i1 %ismin, i1 %wlt, i1 %wgt
  br label %win
keep:
  br label %win
win:
  %take = phi i1 [ %ntake, %num ], [ %otake, %order ], [ %wtake, %wcmp ], [ false, %keep ]
  %wp = select i1 %take, i32 %ap, i32 %bp
  %wt = select i1 %take, i32 %at, i32 %bt
  store i32 %wp, i32* %outp
  store i32 %wt, i32* %outt
  ret i32 0
bad:
  %flt = load i32, i32* %badlt
  %frt = load i32, i32* %badrt
  call void @rt_fold_bad(i32 %op, i32 %flt, i32 %frt)
  %fm = getelementptr [192 x i8], [192 x i8]* @rt.num.msg, i32 0, i32 0
  store i8* %fm, i8** %outmsg
  ret i32 1
}

; rt_pair_order answers one question about two (payload, tag) pairs: does the first order before the
; second, after it, or neither way — or is there no ordering at all between these two kinds? It is the
; element-wise ordering the fold was measured as missing (roadmap Gap R.197, filed measuring ADR 0316),
; the same word Gap R.97 and Gap R.86 file for the relational operators, and the reason the fold used to
; read an object's ELEMENT COUNT as its value: min(la, 3) over a built la answered 2 at exit 0 where the reference raises.
;
; Three statuses, because the reference has three answers and a two-valued door would have to give one of
; them wrongly:
;
;   0  ordered — %outcmp holds -1, 0 or +1 (a < b, a == b, a > b) under the reference's own rules:
;      numbers in the one word that holds int, float and bool; texts by CONTENT, never by the @str_tab
;      arrival order (ADR 0248); a LIST lexicographically, element by element, and when every shared
;      element is equal the shorter list is the lesser one — CPython's own rule;
;   1  no ordering — the two kinds do not order against each other at all, and %outlt / %outrt hold the
;      kinds to name. A SET answers this for neither of its members, a dict always, and a list against a
;      number, a text, a None or a set always; the two slots keep the ELEMENTS' kinds when the failure is
;      inside a list, which is what makes [1] < ["a"] name 'int' and 'str' rather than 'list' and 'list';
;   2  neither order, no raise — two sets that are not each other's subset: the reference's < and > are
;      both simply False, so the fold keeps the incumbent instead of raising. Conflating this with 0
;      would make {1} <= {2} answer True the moment the relational door starts asking (Gap R.86).
;
; A pair that is the SAME OBJECT is equal — the comparison is reflexively false and the incumbent is kept,
; the answer this door already gave for a list and a set (and the cheap arm for two equal ints). A dict is
; excluded: it has no ordering even against itself, and CPython raises on it. The same shortcut is what
; makes a list that contains itself compare equal to itself without walking into its own slot.
;
; %depth is the recursion guard. A nested container recurses through this function once per level, and a
; cyclic structure two programs built independently — xs holding xs, ys holding ys — has no bottom: the
; reference raises RecursionError there, and this backend raises its TypeError at the 65th level, which is
; a different class and is filed as measured rather than presented as the answer (roadmap Gap R.203).
define internal i32 @rt_pair_order(i32 %depth, i32 %a, i32 %ta, i32 %b, i32 %tb, i32* %outcmp, i32* %outlt, i32* %outrt) {
entry:
  %deep = icmp ugt i32 %depth, 64
  br i1 %deep, label %bad, label %ident
ident:
  ; The one question that needs no walk over any elements: a container asked against the VERY SAME object
  ; is reflexively equal, so the fold keeps the incumbent — which is what the reference answers for a list
  ; and for a set. Only those two tags take the shortcut. None takes it NOT: min(None, None) raises, and a
  ; shortcut on identical payloads would have answered it (as would one on the two number tags, which the
  ; arms below answer correctly anyway). It is also what keeps a list that contains itself from walking
  ; into its own slot for ever — the reference's own identity rule for element comparison.
  %samep = icmp eq i32 %a, %b
  %samet = icmp eq i32 %ta, %tb
  %sameo = and i1 %samep, %samet
  %idlist = icmp eq i32 %ta, 5
  %idset = icmp eq i32 %ta, 7
  %idcont = or i1 %idlist, %idset
  %sameobj = and i1 %sameo, %idcont
  br i1 %sameobj, label %equal, label %nums
equal:
  store i32 0, i32* %outcmp
  ret i32 0
nums:
  %an = icmp ult i32 %ta, 3
  %bn = icmp ult i32 %tb, 3
  %bothnum = and i1 %an, %bn
  br i1 %bothnum, label %num, label %texts
num:
  %af = call double @rt_lift_num(i32 %a, i32 %ta)
  %bf = call double @rt_lift_num(i32 %b, i32 %tb)
  %nlt = fcmp olt double %af, %bf
  %ngt = fcmp ogt double %af, %bf
  %nlti = zext i1 %nlt to i32
  %ngti = zext i1 %ngt to i32
  %nneg = sub i32 0, %nlti
  %ncmp = add i32 %nneg, %ngti
  store i32 %ncmp, i32* %outcmp
  ret i32 0
texts:
  %at4 = icmp eq i32 %ta, 4
  %bt4 = icmp eq i32 %tb, 4
  %bothtext = and i1 %at4, %bt4
  br i1 %bothtext, label %txt, label %conts
txt:
  %so = call i32 @rt_str_order(i32 %a, i32 %b)
  store i32 %so, i32* %outcmp
  ret i32 0
conts:
  ; Anything that is not two numbers or two texts is ordered only when the two sides are containers of
  ; the SAME kind: a list against a set, or either against a number, is the reference's TypeError, not a
  ; comparison to be settled by which word happens to be in the payload (Gap R.197's first wrong answer).
  %ak = icmp uge i32 %ta, 5
  %bk = icmp uge i32 %tb, 5
  %twok = and i1 %ak, %bk
  %samek = icmp eq i32 %ta, %tb
  %bothcont = and i1 %twok, %samek
  br i1 %bothcont, label %which, label %bad
which:
  %islist = icmp eq i32 %ta, 5
  br i1 %islist, label %list, label %notlist
notlist:
  ; tag 6 is a dict, and a dict has no ordering against anything — including another copy of itself.
  %isset = icmp eq i32 %ta, 7
  br i1 %isset, label %set, label %bad
list:
  store i32 0, i32* %outcmp
  %oa = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %a
  %ob = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %b
  %lap = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %oa, i32 0, i32 1
  %lbp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ob, i32 0, i32 1
  %la = load i32, i32* %lap
  %lb = load i32, i32* %lbp
  %lmq = icmp slt i32 %la, %lb
  %lmin = select i1 %lmq, i32 %la, i32 %lb
  br label %lcheck
lcheck:
  %i = phi i32 [ 0, %list ], [ %inext, %lnext ]
  %lc = icmp slt i32 %i, %lmin
  br i1 %lc, label %lbody, label %lends
lbody:
  %eap = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %oa, i32 0, i32 2, i32 %i
  %ev1 = load i32, i32* %eap
  %tap = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %a, i32 %i
  %et1 = load i32, i32* %tap
  %ebp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ob, i32 0, i32 2, i32 %i
  %ev2 = load i32, i32* %ebp
  %tbp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %b, i32 %i
  %et2 = load i32, i32* %tbp
  ; The element pair is asked in three steps, and the ORDER of them is the reference's own.
  ;
  ; 1. the same object is equal — the identity rule that keeps [d] against [d] from walking into d, and
  ;    what makes xs against a copy of itself terminate;
  ; 2. equality, for anything that is not two containers: the reference's list comparison asks == FIRST and
  ;    only reaches for < when the two elements differ. That is what makes [None] against [None, 1] a
  ;    comparison of two lengths rather than a comparison of two Nones, which have no ordering and would
  ;    raise; and rt_payload_eq is the one equality the language has for a pair (ADR 0247), so an element
  ;    means here exactly what xs == ys means;
  ; 3. the ordering, which for two containers is THIS function again — asked with the depth argument, so a
  ;    container that contains itself earns the raise at the 65th level rather than a stack overflow. The
  ;    container elements skip step 2 for that reason: rt_payload_eq's container arm has no depth argument
  ;    (it is the helper == uses, and a cyclic pair reaches the same hole there today — roadmap Gap R.203).
  %eidp = icmp eq i32 %ev1, %ev2
  %eidt = icmp eq i32 %et1, %et2
  %eido = and i1 %eidp, %eidt
  br i1 %eido, label %lnext, label %leqgate
leqgate:
  %e1l = icmp eq i32 %et1, 5
  %e1s = icmp eq i32 %et1, 7
  %e1c = or i1 %e1l, %e1s
  %e2l = icmp eq i32 %et2, 5
  %e2s = icmp eq i32 %et2, 7
  %e2c = or i1 %e2l, %e2s
  %ebothc = and i1 %e1c, %e2c
  br i1 %ebothc, label %lorder, label %leq
leq:
  %eqr = call i32 @rt_payload_eq(i32 %ev1, i32 %et1, i32 %ev2, i32 %et2)
  %iseq = icmp ne i32 %eqr, 0
  br i1 %iseq, label %lnext, label %lorder
lorder:
  %d1 = add i32 %depth, 1
  %est = call i32 @rt_pair_order(i32 %d1, i32 %ev1, i32 %et1, i32 %ev2, i32 %et2, i32* %outcmp, i32* %outlt, i32* %outrt)
  %estbad = icmp eq i32 %est, 1
  br i1 %estbad, label %lret1, label %lcheck2
lcheck2:
  ; an element pair that simply does not order — two sets among the elements — makes the whole list
  ; neither-lesser-nor-greater, exactly as the reference's list comparison returns False there.
  %estneither = icmp eq i32 %est, 2
  br i1 %estneither, label %lret2, label %ldecide
ldecide:
  %cv = load i32, i32* %outcmp
  %diff = icmp ne i32 %cv, 0
  br i1 %diff, label %lret0, label %lnext
lnext:
  %inext = add i32 %i, 1
  br label %lcheck
lret1:
  ret i32 1
lret2:
  ret i32 2
lret0:
  ret i32 0
lends:
  ; Every shared element was equal, so the lengths decide it — [1, 2] < [1, 2, 3] and [1, 2] == [1, 2].
  %llt = icmp slt i32 %la, %lb
  %lgt = icmp sgt i32 %la, %lb
  %llti = zext i1 %llt to i32
  %lgti = zext i1 %lgt to i32
  %lneg = sub i32 0, %llti
  %lcmp = add i32 %lneg, %lgti
  store i32 %lcmp, i32* %outcmp
  ret i32 0
set:
  ; The subset operator, both directions: A ⊆ B and B ⊆ A is equality; one direction alone is the proper
  ; subset, which is the reference's <; neither is the pair that answers False to both < and >.
  store i32 0, i32* %outcmp
  %sa = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %a
  %sb = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %b
  %salp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %sa, i32 0, i32 1
  %sblp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %sb, i32 0, i32 1
  %sal = load i32, i32* %salp
  %sbl = load i32, i32* %sblp
  br label %sacheck
sacheck:
  %ai = phi i32 [ 0, %set ], [ %ainext, %sabody ]
  %ainb = phi i1 [ true, %set ], [ %aiand, %sabody ]
  %ac = icmp slt i32 %ai, %sal
  br i1 %ac, label %sabody, label %sbcheck
sabody:
  ; membership is rt_set_find's question, asked by (payload, tag) — so {True} and {1} are each other's
  ; subset, which is what {True} == {1} already answers elsewhere (ADR 0259's bool rule).
  %amp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %sa, i32 0, i32 2, i32 %ai
  %amv = load i32, i32* %amp
  %atp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %a, i32 %ai
  %amt = load i32, i32* %atp
  %ahit = call i32 @rt_set_contains_tagged(i32 %b, i32 %amv, i32 %amt)
  %ahit1 = trunc i32 %ahit to i1
  %aiand = and i1 %ainb, %ahit1
  %ainext = add i32 %ai, 1
  br label %sacheck
sbcheck:
  %bi = phi i32 [ 0, %sacheck ], [ %binext, %sbbody ]
  %binb = phi i1 [ true, %sacheck ], [ %biand, %sbbody ]
  %bc = icmp slt i32 %bi, %sbl
  br i1 %bc, label %sbbody, label %sverdict
sbbody:
  %bmp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %sb, i32 0, i32 2, i32 %bi
  %bmv = load i32, i32* %bmp
  %btp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %b, i32 %bi
  %bmt = load i32, i32* %btp
  %bhit = call i32 @rt_set_contains_tagged(i32 %a, i32 %bmv, i32 %bmt)
  %bhit1 = trunc i32 %bhit to i1
  %biand = and i1 %binb, %bhit1
  %binext = add i32 %bi, 1
  br label %sbcheck
sverdict:
  %bothsub = and i1 %ainb, %binb
  br i1 %bothsub, label %sequal, label %spick
sequal:
  store i32 0, i32* %outcmp
  ret i32 0
spick:
  %either = or i1 %ainb, %binb
  br i1 %either, label %ssub, label %sneither
ssub:
  %scmp = select i1 %ainb, i32 -1, i32 1
  store i32 %scmp, i32* %outcmp
  ret i32 0
sneither:
  ret i32 2
bad:
  store i32 %ta, i32* %outlt
  store i32 %tb, i32* %outrt
  ret i32 1
}

; rt_fold_bad writes the one sentence a fold cannot make: it is CPython's, character for character,
; because the fold is what a program wrote and the kinds are what the objects hold. The operator symbol
; comes from which CALL asked — min asks '<', max asks '>' — the same rule that keeps abs's sentence
; distinct from the unary minus's (ADR 0271).
define internal void @rt_fold_bad(i32 %op, i32 %lt, i32 %rt) {
entry:
  %ismin = icmp eq i32 %op, 0
  %sym = select i1 %ismin, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.o.lt, i32 0, i32 0), i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.o.gt, i32 0, i32 0)
  %ln = call i8* @rt_kind_name(i32 %lt)
  %rn = call i8* @rt_kind_name(i32 %rt)
  %buf = getelementptr [192 x i8], [192 x i8]* @rt.num.msg, i32 0, i32 0
  %w = call i32 (i8*, ...) @snprintf(i8* %buf, i32 192, i8* getelementptr inbounds ([65 x i8], [65 x i8]* @rt.fold.fmt, i32 0, i32 0), i8* %sym, i8* %ln, i8* %rn)
  ret void
}
`
