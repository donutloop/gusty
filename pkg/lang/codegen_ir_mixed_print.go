// The mixed-element printing doors, which ask a container's own record — not the builder — how its elements
// are stored. One block of heapRuntimeIRTmpl.
package lang

// heapIRMixedPrint is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRMixedPrint = `
; rt_print_container_value renders a container that some other container's slot holds. The slot's
; tag says list, dict or set; only the object's own record says how its elements are stored — plain
; numbers, interned text, or per-element tags — so that choice is made at run time from @heap's kind
; and @estr's element bits instead of from what the builder happened to know. Guessing it statically
; is how a nested list of text came to print its interned indexes (roadmap L11.1).
define internal void @rt_print_container_value(i32 %h, i32 %quote) {
entry:
  %o = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %o, i32 0, i32 0
  %kind = load i32, i32* %kp
  %isList = icmp eq i32 %kind, 1
  br i1 %isList, label %list, label %chkDict
list:
  ; Each container printer asks its own object how its elements are stored — plain numbers,
  ; interned text, or per-slot tags — so the delegate is chosen by kind here and by the object
  ; there, never by a guess in this scope (roadmap Gap J.5, L11.1). The list-layout branch asks the
  ; object one further question — the immutable parenthesised spelling shares this layout and differs
  ; in its brackets, its tag and its promise (roadmap L11.3, ADR 0361).
  call void @rt_print_list_or_tuple(i32 %h)
  ret void
chkDict:
  %isDict = icmp eq i32 %kind, 2
  br i1 %isDict, label %dict, label %chkSet
dict:
  call void @rt_dict_print(i32 %h, i32 0)
  ret void
chkSet:
  %isSet = icmp eq i32 %kind, 3
  br i1 %isSet, label %set, label %nothing
set:
  call void @rt_set_print(i32 %h, i32 0)
  ret void
nothing:
  ret void
}

; rt_print_mixed_value renders one element by its tag, using the same three texts the
; record's Repr produces: numbers with %d, interned strings through the repr slot
; (Python quotes elements inside a container), and the None singleton as "None".
define internal void @rt_print_mixed_value(i32 %v, i32 %t, i32 %quote) {
entry:
  %isStr = icmp eq i32 %t, 4
  br i1 %isStr, label %str, label %checkNone
str:
  ; Inside a container Python shows repr() -- quoted, with its chosen quotes -- and at top
  ; level str() -- the text itself. The caller knows which context it is in; only the tag
  ; cannot say, so the flag comes from the call site (ADR 0185).
  %q = icmp ne i32 %quote, 0
  br i1 %q, label %strrepr, label %strraw
strrepr:
  %rp = call i8* @rt_str_repr_ptr(i32 %v)
  call void @rt_out_txt(i8* %rp)
  ret void
strraw:
  %sp = call i8* @rt_str_ptr(i32 %v)
  call void @rt_out_txt(i8* %sp)
  ret void
checkNone:
  %isNone = icmp eq i32 %t, 3
  br i1 %isNone, label %none, label %checkFloat
none:
  call void @rt_print_none(i32 0)
  ret void
checkFloat:
  ; A float slot holds the handle of a @float_box entry, so the bits come from the box and the
  ; rendering comes from rt_fmt_double, the same helper that prints a bare float with Python's
  ; Python's 1.0 rather than printf's 1. Inside a container Python shows repr(), which for a
  ; float is its str() — the same call, not a second formatter (roadmap L11.1, ADR 0233).
  %isFloat = icmp eq i32 %t, 1
  br i1 %isFloat, label %flt, label %checkBool
flt:
  %d = call double @rt_float_of(i32 %v)
  %fp = call i8* @rt_fmt_double(double %d)
  call void @rt_out_txt(i8* %fp)
  ret void
checkBool:
  ; A bool slot is the 0/1 the verdict was made from, and the tag beside it is the only thing in
  ; the module that says so — which is exactly why [True] used to print [1] (Gap R.112). The
  ; rendering is rt_bool_text, the one answer ADR 0257 landed for print and ADR 0258 made the
  ; pair's: the name, through the same sink every other element goes through, and the quote flag
  ; does not apply because Python quotes a bool in no context.
  %isBool = icmp eq i32 %t, 2
  br i1 %isBool, label %bl, label %checkContainer
bl:
  %bp = call i8* @rt_bool_text(i32 %v)
  call void @rt_out_txt(i8* %bp)
  ret void
checkContainer:
  ; A slot that names another container is rendered by that container's own printer, chosen at
  ; run time from its record rather than from what the builder knew — the tag says list, only
  ; the object says whether its elements are numbers, text, or tagged (roadmap L11.1).
  ; "Is this tag a container?" is one door rather than three comparisons written out here, because
  ; the family gained a member: a tuple is a container, and an element printer that lists the kinds
  ; by hand is how the fourth one prints as its handle (roadmap L11.3, ADR 0361).
  %isCont = call i1 @rt_tag_is_container(i32 %t)
  br i1 %isCont, label %cont, label %checkInstance
checkInstance:
  ; An instance slot is a heap handle too, but it is not a container and must not be printed as
  ; its integer address. The class name is in instance slot 0, and the table beside it says what
  ; the reference puts inside the angle brackets of the identity form (roadmap Gap R.209, ADR 0325).
  %isInst = icmp eq i32 %t, 10
  br i1 %isInst, label %inst, label %num
inst:
  ; One call, because the class gets to answer first: whatever its own class binds for __str__ or
  ; __repr__ is what renders, and the identity form is what answers when it binds nothing
  ; (roadmap Gap R.209 + Gap R.55, ADR 0325).
  call void @rt_print_instance_value(i32 %v, i32 %quote)
  ret void
cont:
  call void @rt_print_container_value(i32 %v, i32 %quote)
  ret void
num:
  call void @rt_out_int(i32 %v)
  ret void
}

; rt_print_list_mixed walks a list whose elements carry per-element tags. It is rt_print_list
; with the container-wide @estr[h] flag replaced by a per-element tag lookup -- the difference
; between "this list is the string list" and "this slot holds a string".
define internal void @rt_print_list_mixed(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtlopen, i32 0, i32 0))
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %i1, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %done
body:
  %is0 = icmp eq i32 %i, 0
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %e = load i32, i32* %ep
  %tp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %t = load i32, i32* %tp
  br i1 %is0, label %first, label %sep
first:
  call void @rt_print_mixed_value(i32 %e, i32 %t, i32 1)
  ; A member's own class can RAISE on the way to its answer (its __repr__ handed back something the
  ; reference will not print). The raise outranks the rest of this container: the walk stops, the
  ; bracket that would close it is not written, and the module's own check reports the raise. Bytes
  ; already on stdout cannot be recalled — ADR 0258 says the same about the float door (Gap R.508).
  %ef1 = load i32, i32* @exn_flag
  %bf1 = icmp ne i32 %ef1, 0
  br i1 %bf1, label %bail, label %cont
sep:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  call void @rt_print_mixed_value(i32 %e, i32 %t, i32 1)
  %ef2 = load i32, i32* @exn_flag
  %bf2 = icmp ne i32 %ef2, 0
  br i1 %bf2, label %bail, label %cont
bail:
  ret void
cont:
  %i1 = add i32 %i, 1
  br label %loop
done:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtlclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

; rt_print_list_str renders a list whose elements are interned strings, with Python's
; quoting: ['a', 'b'].
define internal void @rt_print_list_str(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtlopen, i32 0, i32 0))
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %more = icmp slt i32 %i, %len
  br i1 %more, label %body, label %done
body:
  %ip = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %idx = load i32, i32* %ip
  %first = icmp eq i32 %i, 0
  br i1 %first, label %emit, label %sepd
sepd:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_value(i32 %idx, i32 1, i32 1)
  br label %cont
cont:
  %next = add i32 %i, 1
  br label %loop
done:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtlclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

; rt_none returns the None singleton handle, allocating it on first use.
define internal i32 @rt_none() {
entry:
  %cached = load i32, i32* @none_h
  %have = icmp ne i32 %cached, -1
  br i1 %have, label %hit, label %make
make:
  %h = call i32 @rt_alloc(i32 4)
  store i32 %h, i32* @none_h
  ret i32 %h
hit:
  ret i32 %cached
}

; rt_none_len is the container length of the None object (always 0), so a generic
; length query on a None handle reports 0 instead of reading an uninitialised word.
define internal i32 @rt_is_none(i32 %h) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  %k = load i32, i32* %kp
  %isnone = icmp eq i32 %k, 4
  %r = zext i1 %isnone to i32
  ret i32 %r
}

; rt_print_none writes "None", honouring the caller's newline flag like every other
; runtime printer (ADR 0165: the printer does not own the terminator).
define internal void @rt_print_none(i32 %nl) {
entry:
  call void @rt_out_txt(i8* getelementptr ([5 x i8], [5 x i8]* @.fmtnone, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

; rt_print_bool writes a bool as True/False. What a bool holds is the 0/1 the comparison
; produced — the value word carries no kind — so the compiler's front end decides which
; expressions are bools (pkg/lang/boolvalue.go) and this printer only renders them, which is
; the standing arrangement of rt_print_none (ADR 0172) and rt_print_str (ADR 0224): the front
; end reads the program, the runtime renders (roadmap L11.1 step 2, ADR 0257). The honour-the-
; caller's-newline rule of ADR 0165 applies: the printer does not own the terminator.
define internal void @rt_print_bool(i32 %v, i32 %nl) {
entry:
  %t = icmp ne i32 %v, 0
  %tp = select i1 %t, i8* getelementptr ([5 x i8], [5 x i8]* @.fmttrue, i32 0, i32 0), i8* getelementptr ([6 x i8], [6 x i8]* @.fmtfalse, i32 0, i32 0)
  call void @rt_out_txt(i8* %tp)
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

; rt_bool_text is the same answer as a string, for str(True) and for an interpolated
; {flag}: an f-string wants bytes, and a bool's bytes are its own literal.
define internal i8* @rt_bool_text(i32 %v) {
entry:
  %t = icmp ne i32 %v, 0
  %tp = select i1 %t, i8* getelementptr ([5 x i8], [5 x i8]* @.fmttrue, i32 0, i32 0), i8* getelementptr ([6 x i8], [6 x i8]* @.fmtfalse, i32 0, i32 0)
  ret i8* %tp
}

define internal void @rt_print_list(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ; Whether the elements are interned strings is read from the object, not from a static
  ; guess: a helper like "def fill(out, v): out.append(v)" can fill a list its caller created,
  ; and the caller's scope has no idea what the callee stored (Gap J.5).
  %fsp = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %h
  %flags = load i32, i32* %fsp
  ; bit 8 says the slots describe themselves: a list that mixes kinds, or whose elements are
  ; handles into other containers, has no single kind to report, so it prints element by element
  ; (roadmap L11.1 (1b), ADR 0232). Without this arm a nested list is a handle printed as a number
  ; when the printer was chosen by kind from outside — which is how an inner list reached through
  ; rt_print_container_value used to answer [5].
  %mixedBit = and i32 %flags, 8
  %isMixed = icmp ne i32 %mixedBit, 0
  br i1 %isMixed, label %mixed, label %notMixed
mixed:
  call void @rt_print_list_mixed(i32 %h, i32 %nl)
  ret void
notMixed:
  %eb = and i32 %flags, 1
  %isStr = icmp ne i32 %eb, 0
  %istr = zext i1 %isStr to i32
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtlopen, i32 0, i32 0))
  br label %loop
loop:
  ; The predecessor is notMixed, not entry: a phi names the blocks that actually branch to it,
  ; and the tag dispatch above sits in between (llc calls a wrong one a malformed PHI).
  %i = phi i32 [ 0, %notMixed ], [ %i1, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %done
body:
  %is0 = icmp eq i32 %i, 0
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %e = load i32, i32* %ep
  br i1 %is0, label %first, label %sep
first:
  call void @rt_print_value(i32 %e, i32 %istr, i32 1)
  br label %cont
sep:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  call void @rt_print_value(i32 %e, i32 %istr, i32 1)
  br label %cont
cont:
  %i1 = add i32 %i, 1
  br label %loop
done:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtlclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}
`
