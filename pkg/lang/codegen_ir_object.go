// The tagged %obj value and the heap-record readers (kind, len, element, dict key presence). One block of
// heapRuntimeIRTmpl.
package lang

// heapIRObject is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRObject = `
; Construct a tagged %obj value from a kind tag and a payload. The payload is
; a heap handle for reference kinds (str/list/dict/set/tuple/class/instance/
; method/closure/exn/module) or the raw immediate for int/bool/None.
define internal %obj @rt_mkobj(i32 %tag, i32 %payload) {
entry:
  %o = insertvalue %obj undef, i32 %tag, 0
  %o2 = insertvalue %obj %o, i32 %payload, 1
  ret %obj %o2
}

; Read the kind tag word of an %obj value.
define internal i32 @rt_obj_tag(%obj %o) {
entry:
  %t = extractvalue %obj %o, 0
  ret i32 %t
}

; Read the payload word of an %obj value.
define internal i32 @rt_obj_payload(%obj %o) {
entry:
  %p = extractvalue %obj %o, 1
  ret i32 %p
}

; Test whether an %obj value carries the given kind tag.
define internal i1 @rt_obj_is(%obj %o, i32 %tag) {
entry:
  %t = extractvalue %obj %o, 0
  %eq = icmp eq i32 %t, %tag
  ret i1 %eq
}


define internal i32 @rt_heap_kind(i32 %h) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %kf = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  %k = load i32, i32* %kf
  ret i32 %k
}

define internal i32 @rt_heap_len(i32 %h) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lf = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %l = load i32, i32* %lf
  ret i32 %l
}

define internal i32 @rt_heap_get(i32 %h, i32 %i) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %arr = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  %p = getelementptr [256 x i32], [256 x i32]* %arr, i32 0, i32 %i
  %v = load i32, i32* %p
  ret i32 %v
}

define internal i32 @rt_dict_has(i32 %h, i32 %k) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lf = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lf
  ; A dict stores [key, value] pairs, so scanning keys means walking the flat element
  ; array two words at a time up to 2*count. The bound used to be the entry count, which
  ; scanned only the first ceil(count/2) slots: "3 in {1: 2, 3: 4}" was false in the
  ; compiled binary, and a dict read for any later key raised KeyError.
  %limit = mul i32 %len, 2
  %arr = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %header
header:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %ok = icmp slt i32 %i, %limit
  br i1 %ok, label %loop, label %miss
loop:
  %p = getelementptr [256 x i32], [256 x i32]* %arr, i32 0, i32 %i
  %kv = load i32, i32* %p
  %eq = icmp eq i32 %kv, %k
  br i1 %eq, label %hit, label %cont
cont:
  %next = add i32 %i, 2
  br label %header
hit:
  ret i32 1
miss:
  ret i32 0
}

`
