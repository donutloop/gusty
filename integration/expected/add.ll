@.fmt1 = private unnamed_addr constant [4 x i8] c"%d
\00"
declare i32 @printf(i8*, ...)
define i32 @main() {
entry:
  %t1 = add i32 40, 2
  %t2 = call i32 @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt1, i32 0, i32 0), i32 %t1)
  ret i32 0
}
