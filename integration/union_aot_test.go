package integration

import "testing"

func TestUnionAOTLinear(t *testing.T) {
	assertOutput(t, `x: int | float = 5
print(x)`, "5\n")
	assertOutput(t, `x: int | float = 5
x = 2.5
print(x)`, "2.5\n")
	assertOutput(t, `y: int | str = 42
y = "hi"
print(y)`, "hi\n")
}

func TestUnionAOTControlFlow(t *testing.T) {
	assertOutput(t, `i = 0
x: int | float = 5
while i < 1:
    x = 2.5
    i = i + 1
print(x)`, "2.5\n")
	assertOutput(t, `c = 0
y: int | str = 42
if c == 1:
    y = "hi"
print(y)`, "42\n")
	assertOutput(t, `c = 1
z: int | float = 5
if c == 1:
    z = 2.5
print(z)`, "2.5\n")
}

func TestUnionAOTReassign(t *testing.T) {
	// cross-member reassignment in linear code must dispatch correctly
	assertOutput(t, `x: int | float = 5
print(x)
x = 2.5
print(x)
x = 7
print(x)`, "5\n2.5\n7\n")
}
