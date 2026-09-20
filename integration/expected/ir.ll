@exn_flag = internal global i32 0
@gc_roots_used = internal global i32 0
@exn_code = internal global i32 0
@.fmt1 = private unnamed_addr constant [4 x i8] c"%d\0A\00"
@env_store = internal global [4096 x i32] zeroinitializer
@env_count = internal global i32 0
declare i32 @printf(i8*, ...)
define i32 @main() {
entry:
  %gc.roots = alloca [1024 x i32*]
  store i32 0, i32* @gc_roots_used
  %t1 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt1, i32 0, i32 0), i32 42)
  ret i32 0
main.raiseexit:
  ret i32 0
}
!llvm.module.flags = !{!0}
!0 = !{i32 2, !"PIC Level", i32 2}
