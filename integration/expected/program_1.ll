; ModuleID = 'main'
source_filename = "main"

@format_string = constant [3 x i8] c"%d\0A"

define void @main() {
entry:
  call void @add(i32 1, i32 2)
  ret void
}

declare i32 @printf(ptr, ...)

define void @add(i32 %0, i32 %1) {
entry:
  %donut = alloca i32, align 4
  store i32 43, ptr %donut, align 4
  %donutValue = load ptr, ptr %donut, align 8
  %2 = call i32 (ptr, ...) @printf(ptr @format_string, ptr %donutValue)
  %3 = call i32 (ptr, ...) @printf(ptr @format_string, i32 %0)
  ret void
}
