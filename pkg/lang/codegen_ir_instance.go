// An instance's attribute row: rt_inst_get/put/clear/has, the presence bits a class pattern and an attribute
// read ask about (ADR 0235). One block of heapRuntimeIRTmpl.
package lang

// heapIRInstance is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRInstance = `
define internal i32 @rt_inst_get(i32 %h, i32 %slot) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %p = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %slot
  %v = load i32, i32* %p
  ret i32 %v
}

define internal void @rt_inst_put(i32 %h, i32 %slot, i32 %v) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %p = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %slot
  store i32 %v, i32* %p
  ; writing the value writes its presence, the way a container write writes its tag (ADR 0175)
  %sp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @inst_set, i32 0, i32 %h, i32 %slot
  store i32 1, i32* %sp
  ret void
}

; rt_inst_clear zeroes an instance's attribute-presence row, and is emitted at each instantiation.
; Without it a heap slot inherited from a collected object would still claim its old attributes.
define internal void @rt_inst_clear(i32 %h, i32 %n) {
entry:
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %in, %body ]
  %more = icmp slt i32 %i, %n
  br i1 %more, label %body, label %done
body:
  %sp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @inst_set, i32 0, i32 %h, i32 %i
  store i32 0, i32* %sp
  %in = add i32 %i, 1
  br label %loop
done:
  ret void
}

; rt_inst_has answers "has this attribute been written?" — the question a class pattern asks of each
; capture name, and the question Gap R.19 will ask of a plain attribute read.
define internal i32 @rt_inst_has(i32 %h, i32 %slot) {
entry:
  %sp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @inst_set, i32 0, i32 %h, i32 %slot
  %v = load i32, i32* %sp
  ret i32 %v
}
`
