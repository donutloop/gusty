; ModuleID = 'main'
source_filename = "main"

@format_string = constant [3 x i8] c"%d\0A"

define void @main() {
entry:
  %donutloop = alloca i32, align 4
  store i32 42, ptr %donutloop, align 4
  ret void
}

declare i32 @printf(ptr, ...)
