; ModuleID = 'main'
source_filename = "main"

@format_string = constant [3 x i8] c"%d\0A"

define void @main() {
entry:
  %"9b3c24fa-f1d5-4d41-9fd1-0637244ce4f3" = alloca i32, align 4
  store i32 84, ptr %"9b3c24fa-f1d5-4d41-9fd1-0637244ce4f3", align 4
  %0 = load ptr, ptr %"9b3c24fa-f1d5-4d41-9fd1-0637244ce4f3", align 8
  %1 = call i32 (ptr, ...) @printf(ptr @format_string, ptr %0)
  ret void
}

declare i32 @printf(ptr, ...)
