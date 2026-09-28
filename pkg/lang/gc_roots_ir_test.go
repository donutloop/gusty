package lang

import (
	"strings"
	"testing"
)

// The compiled backend's collector is precise only if two structural properties hold
// in every module it emits. They are checked here rather than assumed, because each
// one failing turns into a wrong answer at run time, not a compile error — which is
// exactly how the two bugs found while writing ADR 0181 behaved (a class method that
// never popped its frame; a loop-body alloca whose address changed every iteration).

// irFunctions splits a module into (name, body) pairs, one per `define`.
func irFunctions(t *testing.T, ir string) map[string]string {
	t.Helper()
	funcs := map[string]string{}
	var name string
	var body strings.Builder
	for _, ln := range strings.Split(ir, "\n") {
		if strings.HasPrefix(ln, "define ") && strings.HasSuffix(strings.TrimSpace(ln), "{") {
			if name != "" {
				funcs[name] = body.String()
			}
			sig := strings.TrimSpace(strings.SplitN(ln, "(", 2)[0])
			fields := strings.Fields(sig)
			name = strings.TrimPrefix(fields[len(fields)-1], "@")
			body.Reset()
			continue
		}
		if name == "" {
			continue
		}
		if ln == "}" {
			funcs[name] = body.String()
			name = ""
			body.Reset()
			continue
		}
		// Comments are prose, not instructions: the runtime's own comments name these
		// helpers, and would read as calls to them.
		if strings.HasPrefix(strings.TrimSpace(ln), ";") {
			continue
		}
		body.WriteString(ln)
		body.WriteString("\n")
	}
	if name != "" {
		funcs[name] = body.String()
	}
	return funcs
}

// rootCorpus exercises every function-emitting path in codegen: plain functions,
// class methods, nested closures, decorated functions, generators.
var rootCorpus = []string{
	"def f(x):\n    xs = [x, x]\n    return xs[0] + 1\n\nprint(f(3))\n",
	"def total(xs):\n    t = 0\n    for x in xs:\n        t = t + x\n    return t\n\nprint(total([1, 2, 3]))\n",
	"class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\n\n    def norm(self):\n        return self.x * self.x + self.y * self.y\n\ns = 0\nfor i in range(6):\n    p = Point(i, i)\n    s = s + p.norm()\nprint(s)\n",
	// A method whose body binds a container: this is the case that leaked a root entry
	// per call until emitClassMethod opened a frame like every other function. Any
	// function-emitting path that forgets the frame shows up here, not as a heisenbug.
	"class Bag:\n    def __init__(self):\n        self.n = 0\n\n    def tally(self, k):\n        row = [k, k, k]\n        self.n = self.n + row[2]\n        return self.n\n\nb = Bag()\nt = 0\nfor i in range(6):\n    t = t + b.tally(i)\nprint(t)\n",
	"def outer(n):\n    def inner(k):\n        row = [k, k]\n        return row[1] + n\n    return inner(n) + inner(n + 1)\n\nprint(outer(2))\n",
	"def dec(g):\n    return g\n\n@dec\ndef f(x):\n    xs = [x]\n    return xs[0] + 1\n\nprint(f(3))\n",
	"def nums():\n    yield 1\n    yield 2\n\nxs = nums()\nprint(len(xs))\n",
	"def walk(d):\n    keep = [d, d * 2]\n    if d > 0:\n        walk(d - 1)\n    junk = [1, 2, 3]\n    return keep[0] + keep[1] + junk[2]\n\nprint(walk(4))\n",
	"xs = []\nfor i in range(20):\n    xs.append(i * 2)\nprint(len(xs))\n",
}

func TestRootedFunctionsOpenAndCloseTheirFrame(t *testing.T) {
	for _, src := range rootCorpus {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("Compile: %v\n%s", err, src)
		}
		for name, body := range irFunctions(t, res.IR) {
			if !strings.Contains(body, "rt_root_put") {
				continue
			}
			if name == "main" {
				// Module level has no caller to return to: its entries live for the
				// whole run by design.
				continue
			}
			if !strings.Contains(body, "call void @rt_frame_open") {
				t.Errorf("%s pushes roots but never opens a frame:\n%s", name, body)
			}
			// Every return must pop, or the caller's stack never shrinks: a loop
			// calling such a function ran the 4096-entry root stack out.
			lines := strings.Split(body, "\n")
			for i, ln := range lines {
				if !strings.HasPrefix(strings.TrimSpace(ln), "ret ") {
					continue
				}
				if i == 0 || !strings.Contains(lines[i-1], "rt_frame_close") {
					t.Errorf("%s returns without popping its root frame:\n%s", name, body)
				}
			}
			if n := strings.Count(body, "rt_frame_close"); n == 0 {
				t.Errorf("%s has no rt_frame_close at all:\n%s", name, body)
			}
		}
	}
}

func TestNoAllocaBelowTheFirstInstruction(t *testing.T) {
	// A slot whose allocation is re-executed (an alloca in a loop body) gets a new
	// address per iteration at -O0, so its root entry can never be matched and the
	// root stack grows forever. hoistAllocas is what prevents that.
	for _, src := range rootCorpus {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("Compile: %v\n%s", err, src)
		}
		for name, body := range irFunctions(t, res.IR) {
			seenInstruction := false
			for _, ln := range strings.Split(body, "\n") {
				tt := strings.TrimSpace(ln)
				if tt == "" || strings.HasSuffix(tt, ":") || strings.HasPrefix(tt, ";") {
					continue
				}
				if strings.Contains(tt, " = alloca ") {
					if seenInstruction {
						t.Errorf("%s allocates %q after other instructions: slot address is not stable for rooting", name, tt)
					}
					continue
				}
				seenInstruction = true
			}
		}
	}
}

func TestHoistAllocasIsIdempotentAndPreservesOrder(t *testing.T) {
	src := "def f(n):\n    xs = [n]\n    t = 0\n    for i in range(3):\n        ys = [i]\n        t = t + xs[0] + ys[0]\n    return t\n\nprint(f(2))\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	once := hoistAllocas(res.IR)
	if twice := hoistAllocas(once); twice != once {
		t.Errorf("hoistAllocas is not idempotent")
	}
	// Every instruction still appears exactly once, in the same relative order within
	// a block (only allocas moved, nothing was dropped or duplicated).
	for _, want := range []string{"%_xs = alloca i32", "%_ys = alloca i32", "call void @rt_root_put(i32* %_xs)"} {
		if !strings.Contains(once, want) {
			t.Errorf("hoisting lost %q", want)
		}
	}
	if n := strings.Count(once, "%_ys = alloca i32"); n != 1 {
		t.Errorf("alloca should appear once, got %d", n)
	}
}

func TestPureScalarModuleShipsNoRootRuntime(t *testing.T) {
	// A program that never touches the heap should not pay for the collector's
	// scaffolding: it also keeps the "no runtime in the module" assertions honest.
	res, err := Compile("x = 1 + 2\nprint(x)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{"define internal void @rt_root_put", "define internal void @rt_frame_close", "define internal void @rt_gc("} {
		if strings.Contains(res.IR, want) {
			t.Errorf("scalar-only module shipped %s", want)
		}
	}
}
