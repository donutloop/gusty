@.fmt1 = private unnamed_addr constant [4 x i8] c"%d
\00"
declare i32 @printf(i8*, ...)
define i32 @double(i32 %p0) {
entry:
  %t1 = mul i32 %p0, 2
  ret i32 %t1
}

define i32 @main() {
entry:
  %t2 = call i32 @double(i32 5)
  %t3 = call i32 @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt1, i32 0, i32 0), i32 %t2)
  ret i32 0
}
