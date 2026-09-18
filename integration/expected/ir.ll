@.fmt1 = private unnamed_addr constant [4 x i8] c"%d\0A\00"
@env_store = internal global [4096 x i32] zeroinitializer
@env_count = internal global i32 0
declare i32 @printf(i8*, ...)
define i32 @main() {
entry:
  %t1 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt1, i32 0, i32 0), i32 42)
  ret i32 0
}
!llvm.module.flags = !{!0}
!0 = !{i32 2, !"PIC Level", i32 2}
