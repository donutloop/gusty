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
  %t1 = icmp slt i32 %_i.ld1, 3
  br i1 %t1, label %for.body3, label %for.end6
for.body3:
  br label %for.init7
for.init7:
  %_j = alloca i32
  store i32 0, i32* %_j
  br label %for.cond8
for.cond8:
  %_j.ld2 = load i32, i32* %_j
  %t2 = icmp slt i32 %_j.ld2, 3
  br i1 %t2, label %for.body9, label %for.end12
for.body9:
  %_s.ld3 = load i32, i32* %_s
  %_i.ld4 = load i32, i32* %_i
  %t3 = add i32 %_s.ld3, %_i.ld4
  %_j.ld5 = load i32, i32* %_j
  %t4 = add i32 %t3, %_j.ld5
  store i32 %t4, i32* %_s
  br label %for.inc10
for.inc10:
  %_j.ld6 = load i32, i32* %_j
  %t5 = add i32 %_j.ld6, 1
  store i32 %t5, i32* %_j
  br label %for.cond8
for.end12:
  br label %for.inc4
for.inc4:
  %_i.ld7 = load i32, i32* %_i
  %t6 = add i32 %_i.ld7, 1
  store i32 %t6, i32* %_i
  br label %for.cond2
for.end6:
  %_s.ld8 = load i32, i32* %_s
  %t7 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt1, i32 0, i32 0), i32 %_s.ld8)
  ret i32 0
}
