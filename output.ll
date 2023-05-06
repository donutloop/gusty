; ModuleID = 'example'
source_filename = "example"

@format_string = constant [3 x i8] c"%d\0A"

define void @main() {
entry:
  call void @add()
  ret void
}

declare i32 @printf(ptr, ...)

define void @add() {
entry:
  %donut = alloca i32, align 4
  store i32 43, ptr %donut, align 4
  %value = load ptr, ptr %donut, align 8
  %0 = call i32 (ptr, ...) @printf(ptr @format_string, ptr %value)
  ret void
}
