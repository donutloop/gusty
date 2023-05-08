; ModuleID = 'main'
source_filename = "main"

@format_string = constant [3 x i8] c"%d\0A"

define i32 @main() {
entry:
  %for_init_i = alloca i32, align 4
  store i32 0, ptr %for_init_i, align 4
  br label %loop

loop:                                             ; preds = %loop, %entry
  %iValue = load ptr, ptr %for_init_i, align 8
  %0 = call i32 (ptr, ...) @printf(ptr @format_string, ptr %iValue)
  %for_init_i_value = load i32, ptr %for_init_i, align 4
  %for_init_i_value_updated = add i32 %for_init_i_value, 1
  store i32 %for_init_i_value_updated, ptr %for_init_i, align 4
  %loopCond = icmp ule i32 %for_init_i_value_updated, 10
  br i1 %loopCond, label %loop, label %end

end:                                              ; preds = %loop
  ret i32 0
}

declare i32 @printf(ptr, ...)
