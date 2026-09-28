@.fmt1 = private unnamed_addr constant [3 x i8] c"%d\00"
@.fmt2 = private unnamed_addr constant [2 x i8] c"\0A\00"
@exn_flag = internal global i32 0
@gc_roots_used = internal global i32 0
@gc.roots = internal global [4096 x i32*] zeroinitializer
declare i64 @write(i32, i8*, i64)
declare i32 @snprintf(i8*, i32, i8*, ...)
@exn_code = internal global i32 0
@env_store = internal global [4096 x i32] zeroinitializer
@env_count = internal global i32 0
declare i32 @printf(i8*, ...)
; gusty extern-fn ABI v1 — stable layout for tagged/union exports
%gusty_value = type {i32, i32}
%gusty_union = type {i32, i32, double, i8*}
@gusty_abi_version = internal constant i32 1
define i32 @main() {
entry:
  store i32 0, i32* @gc_roots_used
  %t1 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([3 x i8], [3 x i8]* @.fmt1, i32 0, i32 0), i32 42)
  %t2 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.fmt2, i32 0, i32 0))
  ret i32 0
main.raiseexit:
  ret i32 0
}
!llvm.module.flags = !{!0}
!0 = !{i32 2, !"PIC Level", i32 2}
