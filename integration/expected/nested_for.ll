; ModuleID = 'main'
source_filename = "main"

@format_string = constant [3 x i8] c"%d\0A"

define i32 @main() {
entry:
  %for_init_i = alloca i32, align 4
  store i32 0, ptr %for_init_i, align 4
  br label %loop

loop:                                             ; preds = %end2, %entry
  %for_init_j = alloca i32, align 4
  store i32 0, ptr %for_init_j, align 4
  br label %loop1

end:                                              ; preds = %end2
  ret i32 0

loop1:                                            ; preds = %loop1, %loop
  %jValue = load ptr, ptr %for_init_j, align 8
  %0 = call i32 (ptr, ...) @printf(ptr @format_string, ptr %jValue)
  %for_init_j_value = load i32, ptr %for_init_j, align 4
  %for_init_j_value_updated = add i32 %for_init_j_value, 1
  store i32 %for_init_j_value_updated, ptr %for_init_j, align 4
  %loopCond = icmp ule i32 %for_init_j_value_updated, 10
  br i1 %loopCond, label %loop1, label %end2

end2:                                             ; preds = %loop1
  %for_init_i_value = load i32, ptr %for_init_i, align 4
  %for_init_i_value_updated = add i32 %for_init_i_value, 1
  store i32 %for_init_i_value_updated, ptr %for_init_i, align 4
  %loopCond3 = icmp ule i32 %for_init_i_value_updated, 2
  br i1 %loopCond3, label %loop, label %end
}

declare i32 @printf(ptr, ...)
