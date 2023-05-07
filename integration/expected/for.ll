; ModuleID = 'main'
source_filename = "main"

@format_string = constant [3 x i8] c"%d\0A"

define i32 @main() {
entry:
  %i = alloca i32, align 4
  store i32 0, ptr %i, align 4
  br label %loop

loop:                                             ; preds = %loop, %entry
  %iValue = load ptr, ptr %i, align 8
  %0 = call i32 (ptr, ...) @printf(ptr @format_string, ptr %iValue)
  %iVal = load i32, ptr %i, align 4
  %updatedCounter = add i32 %iVal, 1
  store i32 %updatedCounter, ptr %i, align 4
  %loopCond = icmp ule i32 %updatedCounter, 5
  br i1 %loopCond, label %loop, label %end

end:                                              ; preds = %loop
  ret i32 0
}

declare i32 @printf(ptr, ...)
