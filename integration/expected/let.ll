@.fmt1 = private unnamed_addr constant [4 x i8] c"%d
\00"
declare i32 @printf(i8*, ...)
define i32 @main() {
entry:
  %_x = alloca i32
  store i32 5, i32* %_x
  %_x.ld1 = load i32, i32* %_x
  %t1 = add i32 %_x.ld1, 1
  %t2 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt1, i32 0, i32 0), i32 %t1)
  ret i32 0
}
