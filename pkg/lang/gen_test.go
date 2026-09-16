package lang

import "testing"

func TestGenSum(t *testing.T) {
	src := "def g():\n    yield 1\n    yield 2\n    yield 3\ns = 0\nfor x in g():\n    s = s + x\ns"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 6 {
		t.Fatalf("got %d, want 6", v)
	}
}

func TestListLit(t *testing.T) {
	src := "a = [1, 2, 3]\ns = 0\nfor x in a:\n    s = s + x\ns"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 6 {
		t.Fatalf("got %d, want 6", v)
	}
}

func TestLenList(t *testing.T) {
	src := "a = [10, 20, 30]\nlen(a)"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
}

func TestGenSetComprehensionLen(t *testing.T) {
	// {x * x} for x in [1, 2, 2] dedups to {1, 4}.
	if v, _, err := EvalExpr("len({x * x} for x in [1, 2, 2])"); err != nil {
		t.Fatalf("set comprehension: %v", err)
	} else if v != 2 {
		t.Fatalf("expected len 2, got %d", v)
	}
}

func TestGenDictComprehensionLen(t *testing.T) {
	// {x: x * 10} for x in [1, 2] builds a 2-entry dict.
	if v, _, err := EvalExpr("len({x: x * 10} for x in [1, 2])"); err != nil {
		t.Fatalf("dict comprehension: %v", err)
	} else if v != 2 {
		t.Fatalf("expected len 2, got %d", v)
	}
}

func TestGenDictComprehensionIndex(t *testing.T) {
	// d[1] over {x: x * 10} for x in [1, 2] maps key 1 -> value 10.
	if v, _, err := EvalExpr("({x: x * 10} for x in [1, 2])[1]"); err != nil {
		t.Fatalf("dict comprehension index: %v", err)
	} else if v != 10 {
		t.Fatalf("expected 10, got %d", v)
	}
	// missing key errors like a normal dict lookup.
	if _, _, err := EvalExpr("({x: x * 10} for x in [1, 2])[3]"); err == nil {
		t.Fatalf("expected key-not-found error")
	}
}

func TestGenSetComprehensionIndex(t *testing.T) {
	// s[2] over {x * x} for x in [1, 2] returns the element when present.
	if v, _, err := EvalExpr("({x * x} for x in [1, 2])[1]"); err != nil {
		t.Fatalf("set comprehension index: %v", err)
	} else if v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
	// absent element errors.
	if _, _, err := EvalExpr("({x * x} for x in [1, 2])[5]"); err == nil {
		t.Fatalf("expected not-in-set error")
	}
}

func TestGenMinSetLiteral(t *testing.T) {
	// min over a set literal folds its elements.
	if v, _, err := EvalExpr("min({1, 2, 3})"); err != nil {
		t.Fatalf("min set: %v", err)
	} else if v != 1 {
		t.Fatalf("expected 1, got %d", v)
	}
}

func TestGenMaxSetLiteral(t *testing.T) {
	// max over a set literal folds its elements.
	if v, _, err := EvalExpr("max({1, 2, 3})"); err != nil {
		t.Fatalf("max set: %v", err)
	} else if v != 3 {
		t.Fatalf("expected 3, got %d", v)
	}
}

func TestGenLambdaInline(t *testing.T) {
	// `(lambda x: int: x + 1)(5)` -> 6
	if v, _, err := EvalExpr("(lambda x: int: x + 1)(5)"); err != nil {
		t.Fatalf("inline lambda call: %v", err)
	} else if v != 6 {
		t.Fatalf("expected 6, got %d", v)
	}
	// multi-arg lambda: `(lambda x: int, y: int: x * y)(3, 4)` -> 12
	if v, _, err := EvalExpr("(lambda x: int, y: int: x * y)(3, 4)"); err != nil {
		t.Fatalf("multi-arg lambda call: %v", err)
	} else if v != 12 {
		t.Fatalf("expected 12, got %d", v)
	}
}

func TestGenLambdaNamed(t *testing.T) {
	// `f = lambda x: int: x * 2\nf(3)` -> 6
	if v, _, err := EvalExpr("f = lambda x: int: x * 2\nf(3)"); err != nil {
		t.Fatalf("named lambda: %v", err)
	} else if v != 6 {
		t.Fatalf("expected 6, got %d", v)
	}
}

func TestGenStrMethod(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"AbC".upper()`); got != "ABC" {
		t.Fatalf("expected ABC, got %q", got)
	}
	if got := evalStr(`" AbC ".strip()`); got != "AbC" {
		t.Fatalf("expected AbC, got %q", got)
	}
}

func TestGenDictKeysValues(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	// sum({1: 2, 3: 4}.keys()) -> 1 + 3 = 4
	if v := evalInt("sum({1: 2, 3: 4}.keys())"); v != 4 {
		t.Fatalf("keys sum: expected 4, got %d", v)
	}
	// sum({1: 2, 3: 4}.values()) -> 2 + 4 = 6
	if v := evalInt("sum({1: 2, 3: 4}.values())"); v != 6 {
		t.Fatalf("values sum: expected 6, got %d", v)
	}
}

func TestGenListAppend(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	// sum([1, 2, 3].append(4)) -> 1 + 2 + 3 + 4 = 10
	if v := evalInt("sum([1, 2, 3].append(4))"); v != 10 {
		t.Fatalf("append sum: expected 10, got %d", v)
	}
}

func TestGenDictItemsLen(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	// len({1: 2, 3: 4}.items()) -> 2 pairs
	if v := evalInt("len({1: 2, 3: 4}.items())"); v != 2 {
		t.Fatalf("items len: expected 2, got %d", v)
	}
}

func TestGenDictMinMaxMethods(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if v := evalInt("max({1: 2, 3: 4}.keys())"); v != 3 {
		t.Fatalf("max keys: expected 3, got %d", v)
	}
	if v := evalInt("min({1: 2, 3: 4}.values())"); v != 2 {
		t.Fatalf("min values: expected 2, got %d", v)
	}
}

func TestGenStrIndex(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	// "abc"[1] -> 98 ('b')
	if v := evalInt(`"abc"[1]`); v != 98 {
		t.Fatalf("string index: expected 98, got %d", v)
	}
}

func TestGenStrSplitLen(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	// len("a b c".split()) -> 3
	if v := evalInt("len(\"a b c\".split())"); v != 3 {
		t.Fatalf("split len: expected 3, got %d", v)
	}
}

func TestGenStrEq(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if v := evalInt(`"abc" == "abc"`); v != 1 {
		t.Fatalf("str eq: expected 1, got %d", v)
	}
	if v := evalInt(`"abc" == "abd"`); v != 0 {
		t.Fatalf("str neq: expected 0, got %d", v)
	}
}

func TestGenStrBuiltinLen(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	// len(str(42)) -> 2
	if v := evalInt("len(str(42))"); v != 2 {
		t.Fatalf("str len: expected 2, got %d", v)
	}
}

func TestGenStrReplace(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"aXbXc".replace("X", "-")`); got != "a-b-c" {
		t.Fatalf("replace: expected a-b-c, got %q", got)
	}
	if got := evalStr(`"hello".replace("l", "L")`); got != "heLLo" {
		t.Fatalf("replace: expected heLLo, got %q", got)
	}
}

func TestGenStrFind(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"abcabc".find("bc")`); got != 1 {
		t.Fatalf("find: expected 1, got %d", got)
	}
	if got := evalInt(`"hello".find("z")`); got != -1 {
		t.Fatalf("find absent: expected -1, got %d", got)
	}
	if got := evalInt(`"hello".find("he")`); got != 0 {
		t.Fatalf("find prefix: expected 0, got %d", got)
	}
}

func TestGenStrRfind(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"abcabc".rfind("bc")`); got != 4 {
		t.Fatalf("rfind: expected 4, got %d", got)
	}
	if got := evalInt(`"hello".rfind("z")`); got != -1 {
		t.Fatalf("rfind absent: expected -1, got %d", got)
	}
	if got := evalInt(`"hello".rfind("he")`); got != 0 {
		t.Fatalf("rfind prefix: expected 0, got %d", got)
	}
}

func TestGenStrStartswithEndswith(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"hello".startswith("he")`); got != 1 {
		t.Fatalf("startswith true: expected 1, got %d", got)
	}
	if got := evalInt(`"hello".startswith("lo")`); got != 0 {
		t.Fatalf("startswith false: expected 0, got %d", got)
	}
	if got := evalInt(`"hello".endswith("lo")`); got != 1 {
		t.Fatalf("endswith true: expected 1, got %d", got)
	}
	if got := evalInt(`"hello".endswith("he")`); got != 0 {
		t.Fatalf("endswith false: expected 0, got %d", got)
	}
}

func TestGenStrCount(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"ababab".count("ab")`); got != 3 {
		t.Fatalf("count: expected 3, got %d", got)
	}
	if got := evalInt(`"hello".count("z")`); got != 0 {
		t.Fatalf("count absent: expected 0, got %d", got)
	}
	if got := evalInt(`"aaaa".count("aa")`); got != 2 {
		t.Fatalf("count non-overlap: expected 2, got %d", got)
	}
}

func TestGenStrCountStartEnd(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"ababab".count("ab", 2)`); got != 2 {
		t.Fatalf("count start: expected 2, got %d", got)
	}
	if got := evalInt(`"ababab".count("ab", 2, 4)`); got != 1 {
		t.Fatalf("count start end: expected 1, got %d", got)
	}
}

func TestGenStrLstripRstrip(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"  hi  ".lstrip()`); got != "hi  " {
		t.Fatalf("lstrip: expected hi  , got %q", got)
	}
	if got := evalStr(`"  hi  ".rstrip()`); got != "  hi" {
		t.Fatalf("rstrip: expected   hi, got %q", got)
	}
	if got := evalStr(`"hello".lstrip()`); got != "hello" {
		t.Fatalf("lstrip none: expected hello, got %q", got)
	}
}

func TestGenStrCapitalize(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"hello".capitalize()`); got != "Hello" {
		t.Fatalf("capitalize: expected Hello, got %q", got)
	}
	if got := evalStr(`"hELLO".capitalize()`); got != "Hello" {
		t.Fatalf("capitalize lower rest: expected Hello, got %q", got)
	}
	if got := evalStr(`"".capitalize()`); got != "" {
		t.Fatalf("capitalize empty: expected empty, got %q", got)
	}
}

func TestGenStrTitle(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"hello world".title()`); got != "Hello World" {
		t.Fatalf("title: expected Hello World, got %q", got)
	}
	if got := evalStr(`"aB cD".title()`); got != "Ab Cd" {
		t.Fatalf("title mixed: expected Ab Cd, got %q", got)
	}
	if got := evalStr(`"".title()`); got != "" {
		t.Fatalf("title empty: expected empty, got %q", got)
	}
}

func TestGenStrSwapcase(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"HeLLo".swapcase()`); got != "hEllO" {
		t.Fatalf("swapcase: expected hEllO, got %q", got)
	}
	if got := evalStr(`"ABC".swapcase()`); got != "abc" {
		t.Fatalf("swapcase upper: expected abc, got %q", got)
	}
	if got := evalStr(`"".swapcase()`); got != "" {
		t.Fatalf("swapcase empty: expected empty, got %q", got)
	}
}

func TestGenListCount(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`["a", "b", "a"].count("a")`); got != 2 {
		t.Fatalf("count strings: expected 2, got %d", got)
	}
	if got := evalInt(`[1, 2, 1, 1].count(1)`); got != 3 {
		t.Fatalf("count ints: expected 3, got %d", got)
	}
	if got := evalInt(`["a", "b"].count("z")`); got != 0 {
		t.Fatalf("count absent: expected 0, got %d", got)
	}
}

func TestGenStrIsdigit(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"123".isdigit()`); got != 1 {
		t.Fatalf("isdigit digits: expected 1, got %d", got)
	}
	if got := evalInt(`"12a".isdigit()`); got != 0 {
		t.Fatalf("isdigit mixed: expected 0, got %d", got)
	}
	if got := evalInt(`"".isdigit()`); got != 0 {
		t.Fatalf("isdigit empty: expected 0, got %d", got)
	}
}

func TestGenStrIsalpha(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"abc".isalpha()`); got != 1 {
		t.Fatalf("isalpha letters: expected 1, got %d", got)
	}
	if got := evalInt(`"ab1".isalpha()`); got != 0 {
		t.Fatalf("isalpha mixed: expected 0, got %d", got)
	}
	if got := evalInt(`"".isalpha()`); got != 0 {
		t.Fatalf("isalpha empty: expected 0, got %d", got)
	}
}

func TestGenStrIslowerIsupper(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"abc".islower()`); got != 1 {
		t.Fatalf("islower lower: expected 1, got %d", got)
	}
	if got := evalInt(`"Abc".islower()`); got != 0 {
		t.Fatalf("islower mixed: expected 0, got %d", got)
	}
	if got := evalInt(`"ABC".isupper()`); got != 1 {
		t.Fatalf("isupper upper: expected 1, got %d", got)
	}
	if got := evalInt(`"AbC".isupper()`); got != 0 {
		t.Fatalf("isupper mixed: expected 0, got %d", got)
	}
	if got := evalInt(`"123".islower()`); got != 0 {
		t.Fatalf("islower no cased: expected 0, got %d", got)
	}
}

func TestGenStrPartition(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"a-b-c".partition("-")[0]`); got != "a" {
		t.Fatalf("partition head: expected a, got %q", got)
	}
	if got := evalStr(`"a-b-c".partition("-")[1]`); got != "-" {
		t.Fatalf("partition sep: expected -, got %q", got)
	}
	if got := evalStr(`"a-b-c".partition("-")[2]`); got != "b-c" {
		t.Fatalf("partition tail: expected b-c, got %q", got)
	}
	if got := evalStr(`"abc".partition("z")[0]`); got != "abc" {
		t.Fatalf("partition not found head: expected abc, got %q", got)
	}
}

func TestGenStrIsalnum(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"abc123".isalnum()`); got != 1 {
		t.Fatalf("isalnum alnum: expected 1, got %d", got)
	}
	if got := evalInt(`"abc!".isalnum()`); got != 0 {
		t.Fatalf("isalnum punctuation: expected 0, got %d", got)
	}
	if got := evalInt(`"".isalnum()`); got != 0 {
		t.Fatalf("isalnum empty: expected 0, got %d", got)
	}
}

func TestGenStrIsspace(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"   ".isspace()`); got != 1 {
		t.Fatalf("isspace spaces: expected 1, got %d", got)
	}
	if got := evalInt(`" a ".isspace()`); got != 0 {
		t.Fatalf("isspace mixed: expected 0, got %d", got)
	}
	if got := evalInt(`"".isspace()`); got != 0 {
		t.Fatalf("isspace empty: expected 0, got %d", got)
	}
}

func TestGenSorted(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`sorted([3, 1, 2])[0]`); got != 1 {
		t.Fatalf("sorted first: expected 1, got %d", got)
	}
	if got := evalInt(`sorted([3, 1, 2])[1]`); got != 2 {
		t.Fatalf("sorted second: expected 2, got %d", got)
	}
	if got := evalInt(`sorted([3, 1, 2])[2]`); got != 3 {
		t.Fatalf("sorted last: expected 3, got %d", got)
	}
}

func TestGenStrZfill(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"42".zfill(5)`); got != "00042" {
		t.Fatalf("zfill: expected 00042, got %q", got)
	}
	if got := evalStr(`"123".zfill(2)`); got != "123" {
		t.Fatalf("zfill no pad: expected 123, got %q", got)
	}
	if got := evalStr(`"".zfill(3)`); got != "000" {
		t.Fatalf("zfill empty: expected 000, got %q", got)
	}
}

func TestGenStrLjustRjust(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"ab".ljust(4)`); got != "ab  " {
		t.Fatalf("ljust: expected ab  2sp, got %q", got)
	}
	if got := evalStr(`"ab".rjust(4)`); got != "  ab" {
		t.Fatalf("rjust: expected  2sp ab, got %q", got)
	}
	if got := evalStr(`"abcd".ljust(2)`); got != "abcd" {
		t.Fatalf("ljust no pad: expected abcd, got %q", got)
	}
}

func TestGenStrMethodIndex(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`"abcabc".index("bc")`); got != 1 {
		t.Fatalf("index: expected 1, got %d", got)
	}
	if got := evalInt(`"hello".index("he")`); got != 0 {
		t.Fatalf("index prefix: expected 0, got %d", got)
	}
}

func TestGenStrRsplit(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"a-b-c".rsplit("-")[0]`); got != "a" {
		t.Fatalf("rsplit first: expected a, got %q", got)
	}
	if got := evalStr(`"a-b-c".rsplit("-")[1]`); got != "b" {
		t.Fatalf("rsplit second: expected b, got %q", got)
	}
	if got := evalStr(`"a-b-c".rsplit("-")[2]`); got != "c" {
		t.Fatalf("rsplit last: expected c, got %q", got)
	}
}

func TestGenStrRsplitMaxsplit(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"a-b-c-d".rsplit("-", 1)[0]`); got != "a-b-c" {
		t.Fatalf("rsplit maxsplit left: expected a-b-c, got %q", got)
	}
	if got := evalStr(`"a-b-c-d".rsplit("-", 1)[1]`); got != "d" {
		t.Fatalf("rsplit maxsplit last: expected d, got %q", got)
	}
}

func TestGenStrRemoveprefixSuffix(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"hello".removeprefix("he")`); got != "llo" {
		t.Fatalf("removeprefix: expected llo, got %q", got)
	}
	if got := evalStr(`"hello".removeprefix("x")`); got != "hello" {
		t.Fatalf("removeprefix absent: expected hello, got %q", got)
	}
	if got := evalStr(`"hello".removesuffix("lo")`); got != "hel" {
		t.Fatalf("removesuffix: expected hel, got %q", got)
	}
	if got := evalStr(`"hello".removesuffix("x")`); got != "hello" {
		t.Fatalf("removesuffix absent: expected hello, got %q", got)
	}
}

func TestGenStrExpandtabs(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"abc".expandtabs(4)`); got != "abc" {
		t.Fatalf("expandtabs no tab: expected abc, got %q", got)
	}
}

func TestGenStrStripChars(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"xxhi xx".strip("x")`); got != "hi " {
		t.Fatalf("strip chars: expected hi + space, got %q", got)
	}
	if got := evalStr(`"  hi  ".strip()`); got != "hi" {
		t.Fatalf("strip whitespace: expected hi, got %q", got)
	}
}

func TestGenStrLstripRstripChars(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"xxhi".lstrip("x")`); got != "hi" {
		t.Fatalf("lstrip chars: expected hi, got %q", got)
	}
	if got := evalStr(`"hi xx".rstrip("x")`); got != "hi " {
		t.Fatalf("rstrip chars: expected hi + space, got %q", got)
	}
	if got := evalStr(`"  hi".lstrip()`); got != "hi" {
		t.Fatalf("lstrip whitespace: expected hi, got %q", got)
	}
}

func TestGenStrSplitMaxsplit(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"a-b-c-d".split("-", 1)[0]`); got != "a" {
		t.Fatalf("split maxsplit first: expected a, got %q", got)
	}
	if got := evalStr(`"a-b-c-d".split("-", 1)[1]`); got != "b-c-d" {
		t.Fatalf("split maxsplit rest: expected b-c-d, got %q", got)
	}
	if got := evalStr(`"a-b-c".split("-")[2]`); got != "c" {
		t.Fatalf("split all: expected c, got %q", got)
	}
}

func TestGenStrJoin(t *testing.T) {
	evalStr := func(src string) string {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return e.Repr(v)
	}
	if got := evalStr(`"-".join(["a", "b", "c"])`); got != "a-b-c" {
		t.Fatalf("join: expected a-b-c, got %q", got)
	}
	if got := evalStr(`",".join(["x", "y"])`); got != "x,y" {
		t.Fatalf("join comma: expected x,y, got %q", got)
	}
	if got := evalStr(`"".join(["a", "b"])`); got != "ab" {
		t.Fatalf("join empty sep: expected ab, got %q", got)
	}
}

func TestGenDictGet(t *testing.T) {
	evalInt := func(src string) int64 {
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		e := NewEvaluator()
		v, err := e.EvalProgram(prog)
		if err != nil {
			t.Fatalf("eval %s: %v", src, err)
		}
		return v
	}
	if got := evalInt(`{"a": 1, "b": 2}.get("a", 9)`); got != 1 {
		t.Fatalf("get existing: expected 1, got %d", got)
	}
	if got := evalInt(`{"a": 1}.get("b", 9)`); got != 9 {
		t.Fatalf("get default: expected 9, got %d", got)
	}
	if got := evalInt(`{"a": 1}.get("b")`); got != 0 {
		t.Fatalf("get no-default absent: expected 0, got %d", got)
	}
}

