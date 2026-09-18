@.fmt1 = private unnamed_addr constant [4 x i8] c"%d
\00"
@.fmt2 = private unnamed_addr constant [4 x i8] c"%d
\00"
declare i32 @printf(i8*, ...)
define i32 @main() {
entry:
  %_x = alloca i32
  store i32 2, i32* %_x
  %_x.ld1 = load i32, i32* %_x
  %t1 = icmp eq i32 %_x.ld1, 1
  br i1 %t1, label %match.case2, label %match.next3
match.case2:
  %t2 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt1, i32 0, i32 0), i32 1)
  br label %match.end1
match.next3:
  %t3 = icmp eq i32 %_x.ld1, 2
  br i1 %t3, label %match.case4, label %match.end1
match.case4:
  %t4 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt2, i32 0, i32 0), i32 2)
  br label %match.end1
match.end1:
  ret i32 0
}
