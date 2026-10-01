package lang

import (
	"regexp"
	"strings"
	"testing"
)

// Gap J.2 (ADR 0234): a set or dict comprehension was a compile-time global with no value.
//
// The fold in comp() emitted `@.setN` / `@.dictN` — a `{i32, [n x i32]}` whose layout is a length
// plus an array — and every consumer then treated that name as if it were an i32 value:
//
//	sa = {x for x in [3, 1, 2]}   ->  store i32 @.set1, i32* %_sa      (llc: global must have pointer type)
//	print({x for x in [3,1,2]})   ->  call void @rt_print_list_mixed(i32 @.set1, i32 0)
//	print(2 in {x for x in [1,2]}) -> call i32 @rt_contains(i32 @.set1, i32 2)
//
// Each is a module `llc` rejects, so the exit-code contract blamed the compiler for an ordinary
// program. And the `{x for x in xs if c}` spelling never even reached codegen: the parser read the
// iterable as a full expression, so the ternary ate the comprehension's own `if` filter and the
// file did not parse on either backend.
//
// The rule that closes it is ADR 0163's: a container binding allocates a heap object, writes every
// slot with its tag, and registers the variable's kind. A comprehension that means `{1, 2}` goes
// through the literal's lowering, so the two spellings cannot grow apart.

func firstAssignValue(t *testing.T, src string) Expr {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	as, ok := prog.Stmts[0].(*AssignStmt)
	if !ok {
		t.Fatalf("%q: first statement is %T, not an assignment", src, prog.Stmts[0])
	}
	return as.Value
}

func TestSetComprehensionFilterIsTheComprehensions(t *testing.T) {
	// The shape that did not parse at all: `if` after the iterable belongs to the comprehension.
	c, ok := firstAssignValue(t, "sa = {x for x in xs if x > 1}").(*Comp)
	if !ok {
		t.Fatalf("{x for x in xs if x > 1} did not parse as a comprehension")
	}
	if c.Kind != CompSet {
		t.Fatalf("kind = %d, want CompSet", c.Kind)
	}
	if c.Cond == nil {
		t.Fatalf("the filter was lost: %#v", c)
	}
	if _, ok := c.Iter.(*Name); !ok {
		t.Fatalf("the iterable must be the name xs, got %T — a ternary swallowed the filter", c.Iter)
	}
	if _, ok := c.Cond.(*BinOp); !ok {
		t.Fatalf("the filter must be the comparison x > 1, got %T", c.Cond)
	}
}

func TestDictComprehensionFilterIsTheComprehensions(t *testing.T) {
	c, ok := firstAssignValue(t, "da = {k: k * 2 for k in ks if k > 1}").(*Comp)
	if !ok {
		t.Fatalf("{k: k*2 for k in ks if k > 1} did not parse as a comprehension")
	}
	if c.Kind != CompDict {
		t.Fatalf("kind = %d, want CompDict", c.Kind)
	}
	if c.Cond == nil {
		t.Fatalf("the filter was lost: %#v", c)
	}
	if len(c.Keys) != 1 || len(c.Vals) != 1 {
		t.Fatalf("a dict comprehension keeps one key and one value, got %d/%d", len(c.Keys), len(c.Vals))
	}
	if _, ok := c.Iter.(*Name); !ok {
		t.Fatalf("the iterable must stay a name, got %T", c.Iter)
	}
}

func TestComprehensionIterableStillAdmitsATernaryFreeOr(t *testing.T) {
	// Narrowing the iterable parse must not lose what an iterable could always say: `a or b` is
	// still an iterable expression, and `x in xs` still reads as a membership test inside a filter.
	c, ok := firstAssignValue(t, "sa = {x for x in a or b if x in keep}").(*Comp)
	if !ok {
		t.Fatalf("{x for x in a or b if x in keep} did not parse as a comprehension")
	}
	if _, ok := c.Iter.(*BinOp); !ok {
		t.Fatalf("an `or` iterable must survive the parse, got %T", c.Iter)
	}
	if _, ok := c.Cond.(*BinOp); !ok {
		t.Fatalf("the filter must survive the parse, got %T", c.Cond)
	}
}

// The three positions a folded comprehension used to hand a global to a helper expecting an i32.
func TestFoldedContainerComprehensionNeverSitsInAValuePosition(t *testing.T) {
	// What is forbidden, spelled as a regex so the guard cannot be satisfied by luck: a folded
	// container global (@.lstN / @.setN / @.dictN) as an *operand* — of a store, a call argument or
	// a printf. Its definition (`private global`) is the only place the name may appear.
	badValuePosition := regexp.MustCompile(`(?:store .*i32 |call [^(\n]*\()@\.(?:set|dict|lst)[0-9]+`)
	for _, src := range []string{
		"sa = {x for x in [1, 2, 3]}\nprint(sa)\n",
		"da = {k: k * 2 for k in [1, 2]}\nprint(da)\n",
		"print({x for x in [1, 2, 3]})\n",
		"print({k: k * 2 for k in [1, 2]})\n",
		"print(2 in {x for x in [1, 2]})\n",
		"sa = {x for x in [1, 2, 3] if x > 1}\nprint(len(sa))\n",
		"da = {k: k * 2 for k in [1, 2]}\nprint(da[2])\n",
	} {
		mod := compileSrcIR(t, src)
		if bad := badValuePosition.FindString(mod); bad != "" {
			t.Errorf("%q puts a folded container global in a value position: %s", src, bad)
		}
	}
}

func TestSetComprehensionBindingBuildsAHeapSet(t *testing.T) {
	mod := compileSrcIR(t, "sa = {x for x in [1, 2, 3, 2, 1]}\nprint(sa)\n")
	for _, want := range []string{
		"call i32 @rt_alloc(i32 3)", // HeapKindSet
		"call void @rt_set_add(",
		"call void @rt_tag_elem(",
		"call void @rt_set_print(",
	} {
		if !strings.Contains(mod, want) {
			t.Errorf("set comprehension binding missing %q:\n%s", want, mod)
		}
	}
	if strings.Contains(mod, "store i32 @.set") {
		t.Fatalf("the binding stored the compile-time global instead of a handle:\n%s", mod)
	}
}

func TestDictComprehensionBindingBuildsAHeapDict(t *testing.T) {
	mod := compileSrcIR(t, "da = {k: k * 2 for k in [1, 2]}\nprint(da)\n")
	for _, want := range []string{
		"call i32 @rt_alloc(i32 2)", // HeapKindDict
		"call void @rt_dict_put(",
		"call void @rt_dict_print(",
	} {
		if !strings.Contains(mod, want) {
			t.Errorf("dict comprehension binding missing %q:\n%s", want, mod)
		}
	}
	if strings.Contains(mod, "store i32 @.dict") {
		t.Fatalf("the binding stored the compile-time global instead of a handle:\n%s", mod)
	}
}

func TestRuntimeContainerComprehensionRegistersItsKind(t *testing.T) {
	// The iterable has a runtime length, so this is a real loop; the handle it builds only prints
	// as a container if the variable's kind was recorded at the binding.
	setMod := compileSrcIR(t, "xs = [1, 2, 3]\nxs.append(4)\nsa = {x for x in xs if x > 2}\nprint(sa)\n")
	for _, want := range []string{
		"call i32 @rt_alloc(i32 3)",
		"call void @rt_set_add_tagged(",
		"call void @rt_set_print(",
		"call i32 @rt_get_elem(",
		"call void @rt_root_put(i32* %_sa)",
	} {
		if !strings.Contains(setMod, want) {
			t.Errorf("runtime set comprehension missing %q:\n%s", want, setMod)
		}
	}
	// The handle has to be in the root table before the collection the next statement may
	// trigger: an unrooted container is one collection away from answering nothing at all
	// (ADR 0181). Text order is the emission order here, which is what the collector sees.
	store := regexp.MustCompile(`store i32 %h\d+, i32\* %_sa`).FindStringIndex(setMod)
	if store == nil {
		t.Errorf("the runtime set comprehension's handle was never stored into the variable:\n%s", setMod)
	} else {
		rest := setMod[store[0]:]
		root := strings.Index(rest, "call void @rt_root_put(i32* %_sa)")
		gc := strings.Index(rest, "call void @rt_gc(")
		if root < 0 || gc < 0 || root > gc {
			t.Errorf("the handle must be rooted before the next collection (root at +%d, gc at +%d after the store):\n%s", root, gc, setMod)
		}
	}
	dictMod := compileSrcIR(t, "xs = [1, 2, 3]\nxs.append(4)\nda = {k: k * 3 for k in xs if k > 2}\nprint(da)\n")
	for _, want := range []string{
		"call i32 @rt_alloc(i32 2)",
		"call void @rt_dict_put_tagged(",
		"call void @rt_dict_print(",
		"call i32 @rt_get_elem(",
	} {
		if !strings.Contains(dictMod, want) {
			t.Errorf("runtime dict comprehension missing %q:\n%s", want, dictMod)
		}
	}
}

func TestMembershipInAFoldedComprehensionUsesAHandle(t *testing.T) {
	mod := compileSrcIR(t, "print(2 in {x for x in [1, 2]})\n")
	if strings.Contains(mod, "rt_contains(i32 @.") {
		t.Fatalf("`in` over a folded comprehension handed a global to rt_contains:\n%s", mod)
	}
	if !strings.Contains(mod, "call i32 @rt_contains(i32 %") {
		t.Fatalf("`in` over a comprehension must ask the runtime about a handle:\n%s", mod)
	}
}

func TestContainerComprehensionRefusalsStayHonest(t *testing.T) {
	// A refusal is only honest when the same program on the other side of the language refuses the
	// same way: a set comprehension over a non-integer iterable declines with the message its list
	// twin has always used, rather than a new one invented for sets.
	listMsg := compileErr(t, "print([x for x in [\"a\", \"b\"]])\n")
	setMsg := compileErr(t, "print({x for x in [\"a\", \"b\"]})\n")
	if listMsg == "" || setMsg == "" {
		t.Fatalf("both programs must be refused: list=%q set=%q", listMsg, setMsg)
	}
	if listMsg != setMsg {
		t.Errorf("the set twin refuses differently from the list:\n list: %s\n  set: %s", listMsg, setMsg)
	}
}

func compileErr(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err == nil && res != nil && len(res.Diagnostics) == 0 {
		t.Fatalf("%q must be refused, but it compiled", src)
	}
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	for _, d := range res.Diagnostics {
		b.WriteString(d.Msg)
	}
	return b.String()
}
