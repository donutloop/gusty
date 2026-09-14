@.fmt1 = private unnamed_addr constant [4 x i8] c"%d
\00"
declare i32 @printf(i8*, ...)
define i32 @main() {
entry:
  %_s = alloca i32
  store i32 0, i32* %_s
  br label %for.init1
for.init1:
  %_i = alloca i32
  store i32 0, i32* %_i
  br label %for.cond2
for.cond2:
  %_i.ld1 = load i32, i32* %_i
  %t1 = icmp slt i32 %_i.ld1, 5
  br i1 %t1, label %for.body3, label %for.end6
for.body3:
  %_s.ld2 = load i32, i32* %_s
  %_i.ld3 = load i32, i32* %_i
  %t2 = add i32 %_s.ld2, %_i.ld3
  store i32 %t2, i32* %_s
  br label %for.inc4
for.inc4:
  %_i.ld4 = load i32, i32* %_i
  %t3 = add i32 %_i.ld4, 1
  store i32 %t3, i32* %_i
  br label %for.cond2
for.end6:
  %_s.ld5 = load i32, i32* %_s
  %t4 = call i32 @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt1, i32 0, i32 0), i32 %_s.ld5)
  ret i32 0
}
