package lang

import (
	"strconv"
	"testing"
)

// Gap R.26 / ADR 0215: an operator applied to operands it cannot apply used to answer a number.
// The interpreter consulted no operand kind at all, so a heap handle that reached an arithmetic
// path was added or multiplied as an integer — `print("a" * "b")` printed 1099516870662 and exited
// 0, `print(1 + None)` printed a heap id. Every row below was measured against CPython's own
// output before it was written; the message is asserted exactly, because the wording is what a
// user searches for and what makes the two languages read the same.

var operandRefusals = []struct {
	name string
	src  string
	msg  string
}{
	{"str times str", `print("a" * "b")`, "can't multiply sequence by non-int of type 'str'"},
	{"str times list", `print("a" * [1])`, "can't multiply sequence by non-int of type 'list'"},
	{"str times float", `print("a" * 1.5)`, "can't multiply sequence by non-int of type 'float'"},
	{"list times str", `print([1] * "x")`, "can't multiply sequence by non-int of type 'str'"},
	{"int minus str", `print(1 - "a")`, "unsupported operand type(s) for -: 'int' and 'str'"},
	{"str minus int", `print("a" - 1)`, "unsupported operand type(s) for -: 'str' and 'int'"},
	{"str divided by int", `print("a" / 2)`, "unsupported operand type(s) for /: 'str' and 'int'"},
	{"int divided by str", `print(1 / "a")`, "unsupported operand type(s) for /: 'int' and 'str'"},
	{"int floordiv str", `print(1 // "a")`, "unsupported operand type(s) for //: 'int' and 'str'"},
	{"int modulo str", `print(7 % "a")`, "unsupported operand type(s) for %: 'int' and 'str'"},
	{"str modulo int", `print("a" % 2)`, "not all arguments converted during string formatting"},
	{"int pow str", `print(2 ** "a")`, "unsupported operand type(s) for ** or pow(): 'int' and 'str'"},
	{"str pow int", `print("a" ** 2)`, "unsupported operand type(s) for ** or pow(): 'str' and 'int'"},
	{"int plus None", `print(1 + None)`, "unsupported operand type(s) for +: 'int' and 'NoneType'"},
	{"None plus int", `print(None + 1)`, "unsupported operand type(s) for +: 'NoneType' and 'int'"},
	{"None times int", `print(None * 2)`, "unsupported operand type(s) for *: 'NoneType' and 'int'"},
	{"list plus int", `print([1] + 1)`, `can only concatenate list (not "int") to list`},
	{"str plus int", `print("a" + 1)`, `can only concatenate str (not "int") to str`},
	{"dict plus dict", `print({"a": 1} + {"b": 2})`, "unsupported operand type(s) for +: 'dict' and 'dict'"},
	{"int plus list", `print(1 + [2])`, "unsupported operand type(s) for +: 'int' and 'list'"},
	{"str lt int", `print("a" < 1)`, "'<' not supported between instances of 'str' and 'int'"},
	{"int lt str", `print(1 < "a")`, "'<' not supported between instances of 'int' and 'str'"},
	{"list of incomparable elements", `print([1] < ["a"])`, "'<' not supported between instances of 'int' and 'str'"},
	{"str floordiv str", `print("a" // "b")`, "unsupported operand type(s) for //: 'str' and 'str'"},
}

func TestOperatorRefusesOperandsItCannotApply(t *testing.T) {
	for _, tc := range operandRefusals {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != "TypeError" {
				t.Errorf("class = %q, want TypeError (msg %q)", ee.ExnType, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.msg {
				t.Errorf("message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.msg)
			}
		})
	}
}

// Equality and membership are total: they compare, they do not compute, so the gate must not
// refuse them. A rule that traps everything mistyped is wrong in the other direction.
func TestTotalOperatorsStillAcceptAnything(t *testing.T) {
	cases := []struct{ src, want string }{
		{`print(1 == "a")`, "0\n"},
		{`print("a" == 1)`, "0\n"},
		{`print(1 != None)`, "1\n"},
		{`print("a" in ["a", "b"])`, "1\n"},
		{`print(3 in [1, 2])`, "0\n"},
		{`print(None is None)`, "1\n"},
		{`print({"a": 1} == {"a": 1})`, "1\n"},
	}
	for _, tc := range cases {
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// The other half of the rule: the operand kinds the language *does* support must compute the
// right value, not merely be allowed through. These are the operations whose absence made the
// gate refuse legal programs — `[1] + [2]` and `"ab" * 2` were handle arithmetic.
func TestSequenceOperationsCompute(t *testing.T) {
	cases := []struct{ src, want string }{
		{`print([1] + [2])`, "[1, 2]\n"},
		{`print([1] + [2] + [3])`, "[1, 2, 3]\n"},
		{`print([] + [1])`, "[1]\n"},
		{`print([1] * 3)`, "[1, 1, 1]\n"},
		{`print(3 * [1])`, "[1, 1, 1]\n"},
		{`print([1] * 0)`, "[]\n"},
		{`print([1] * -2)`, "[]\n"},
		{`print("ab" * 2)`, "abab\n"},
		{`print(2 * "ab")`, "abab\n"},
		{`print("ab" * 0)`, "\n"},
		{`print("ab" * -1)`, "\n"},
		{`print("a" + "")`, "a\n"},
		{`print("" + "a")`, "a\n"},
		// Comparing by value: `"a" < "b"` compared heap handles before, so its answer
		// depended on the order the strings happened to be allocated.
		{"if \"a\" < \"b\":\n    print(\"yes\")", "yes\n"},
		{"if \"b\" < \"a\":\n    print(\"yes\")\nelse:\n    print(\"no\")", "no\n"},
		{"if [1, 2] < [2]:\n    print(\"yes\")", "yes\n"},
		{"if [1, 2] < [1]:\n    print(\"yes\")\nelse:\n    print(\"no\")", "no\n"},
		{"if [1] < [1, 2]:\n    print(\"yes\")", "yes\n"},
		{"if 1.5 > 1:\n    print(\"yes\")", "yes\n"},
		{"if 1 <= 1:\n    print(\"yes\")", "yes\n"},
	}
	for _, tc := range cases {
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// The representation regression that this whole rule uncovered.
//
// Interpreter values are an untagged int64: a heap handle and a program's own integer live in the
// same space, distinguished only by whether the number happens to name a live object. The heap
// used to start at 1<<20, so an ordinary loop computing i*i reached the object space — at i = 1024
// `self.x * self.x` evaluated to 1048576, the accumulator then equalled the class's own method
// object, and `s + p.norm()` read back as int plus method. The gate is what turned it from a wrong
// number into a diagnosable error; the fix is heapIDBase. The expected value is computed here in
// Go, independently of the language, so the test fails if a future change lets a computed integer
// be read as an object again.
func TestComputedIntegerIsNotMistakenForAHeapObject(t *testing.T) {
	const n = 2000
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\n\n" +
		"    def norm(self) -> int:\n        return self.x * self.x + self.y * self.y\n\n" +
		"s = 0\nfor i in range(" + strconv.Itoa(n) + "):\n    p = Point(i, i % 7)\n    s = s + p.norm()\nprint(s)\n"
	want := int64(0)
	for i := 0; i < n; i++ {
		want += int64(i*i) + int64((i%7)*(i%7))
	}
	if got := captureStdout(t, src); got != strconv.FormatInt(want, 10)+"\n" {
		t.Errorf("sum through the loop = %s, want %d — a computed integer is being read as a heap object", got, want)
	}
}

// The same collision, forced from the other side: an integer that equals a live object's id must
// still behave as the number the program wrote, and the object's own members must still work.
func TestIntegerEqualToALiveHandleIsStillAnInteger(t *testing.T) {
	src := "class P:\n    def f(self) -> int:\n        return 5\n\np = P()\nbig = 2 ** 40\n" +
		"s = big + p.f()\nprint(s)\nprint(big - 5)\n"
	handles := int64(1 << 40)
	want := strconv.FormatInt(handles+5, 10) + "\n" + strconv.FormatInt(handles-5, 10) + "\n"
	if got := captureStdout(t, src); got != want {
		t.Errorf("arithmetic on a handle-sized integer = %q, want %q", got, want)
	}
}
