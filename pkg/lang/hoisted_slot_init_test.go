package lang

import (
	"strings"
	"testing"
)

// A hoisted variable slot and a defined variable slot were two different promises, and only one was kept.
// `hoistAllocas` guaranteed a slot is allocated once per call (the root stack needs that); nothing
// guaranteed it ever had a value in it. The release-the-old-binding prologue reads the slot anyway —
// `load i32, i32* %_x` / `rt_free` — and the frame's previous contents are whatever the last call left
// there, so a garbage handle went into the allocator's free list and the next rt_alloc walked off the
// arena. The program died by SIGSEGV with the artifact at fault, not the source: no traceback, nothing on
// stdout, the runner's Gap R.203 sentence. Roadmap Gap R.258, found the moment cases began running beside
// each other (ADR 0343) — alone, the case answered [3, 2, 1]; beside a compile that had dirtied the same
// frame, it died.

// gapR258Programs bind a container the compiler builds at runtime to a name, which is the shape that
// created a slot without a value: the sorted/list-joining folds give the binding a handle no road had
// written a defined "no object yet" for.
var gapR258Programs = []struct {
	src      string
	wantOut  string
	wantEcho string
}{
	{"x = sorted([3, 1, 2], reverse=True)\nx\n", "", "[3, 2, 1]"},
	{"y = sorted([3, 1, 2])\nprint(y)\n", "[1, 2, 3]\n", ""},
	{"y = sorted([3, 1, 2])\nprint(y[0], len(y))\n", "1 3\n", ""},
	{"def build(n):\n    return [n, n + 1]\n\nz = build(4)\nprint(z)\n", "[4, 5]\n", ""},
	{"w = sorted([\"b\", \"a\"])\nprint(w)\n", "['a', 'b']\n", ""},
}

// TestAContainerBindingAnswersBesideEverythingElse runs the programs that used to die in the allocator.
// The assertion is the answer, and it is asked on both legs: the compiled program's own stdout, and the
// record the retired engine kept for the same source.
func TestAContainerBindingAnswersBesideEverythingElse(t *testing.T) {
	t.Parallel()
	for _, tc := range gapR258Programs {
		t.Run(strings.SplitN(tc.src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			res, err := RunSnippetIn(t.TempDir(), tc.src)
			if err != nil {
				t.Fatalf("%q: %v", tc.src, err)
			}
			if res.Signaled {
				t.Fatalf("%q was killed by signal %s — the artifact died, it did not report a trap (Gap R.258)",
					tc.src, res.Signal)
			}
			if res.Code != 0 {
				t.Fatalf("%q exited %d: %s", tc.src, res.Code, res.Stderr)
			}
			if res.Output != tc.wantOut {
				t.Errorf("%q printed %q, want %q", tc.src, res.Output, tc.wantOut)
			}
			if tc.wantEcho != "" {
				if res.Result == nil {
					t.Fatalf("%q echoed nothing; the snippet's last expression asked a question", tc.src)
				}
				if res.Result.Repr != tc.wantEcho {
					t.Errorf("%q answered %q, want %q", tc.src, res.Result.Repr, tc.wantEcho)
				}
			}
		})
	}
}

// TestEveryVariableSlotIsDefinedBeforeItIsReleased is the invariant, not the symptom: a stack slot that
// anything reads must have had its value written. The release-the-old-binding prologue is the reader that
// made this load-bearing. So: for every variable slot a function declares, if some line loads it, then
// some line must also have stored the defined "this variable holds no object yet" value into it first.
func TestEveryVariableSlotIsDefinedBeforeItIsReleased(t *testing.T) {
	t.Parallel()
	for _, src := range append(gapR258Corpus, gapR258Programs[0].src) {
		t.Run(strings.SplitN(src, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("Compile: %v\n%s", err, src)
			}
			for name, body := range irFunctions(t, res.IR) {
				lines := strings.Split(body, "\n")
				zeroed, read := map[string]bool{}, map[string]string{}
				for _, ln := range lines {
					tt := stripDbgSuffix(strings.TrimSpace(ln))
					if slot := operandAfter(tt, "store ", "i32* "); slot != "" && strings.HasPrefix(tt, "store i32 0, "+operandWithType(tt, "i32* ")) {
						zeroed[slot] = true
					}
					if slot := operandAfter(tt, "load ", "i32* "); slot != "" {
						if _, seen := read[slot]; !seen {
							read[slot] = tt
						}
					}
				}
				for _, ln := range lines {
					tt := stripDbgSuffix(strings.TrimSpace(ln))
					if !strings.HasSuffix(tt, " = alloca i32") {
						continue
					}
					slot := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(tt, "= alloca i32")), "="))
					if !strings.HasPrefix(slot, "%_") {
						continue // the runtime's own temporaries belong to hand-written code
					}
					use, isRead := read[slot]
					if !isRead {
						continue // never read: an unwritten slot costs nothing and means nothing
					}
					if !zeroed[slot] {
						t.Errorf("%s declares %s, reads it (%s), and never gives it a defined value (Gap R.258)",
							name, slot, use)
					}
				}
			}
		})
	}
}

// gapR258Corpus is the shapes that allocate slots from many different roads — the forty alloca sites are
// the reason the fix sits in the hoisting pass rather than in any one of them.
var gapR258Corpus = []string{
	"xs = []\nfor i in range(3):\n    xs.append(i)\nprint(xs)\n",
	"def f(n):\n    ys = [n]\n    t = 0\n    for i in range(3):\n        zs = [i]\n        t = t + ys[0] + zs[0]\n    return t\n\nprint(f(2))\n",
	"m = {}\nm[\"a\"] = 1\nprint(m)\n",
	"s = {1, 2}\nprint(len(s))\n",
	"def g():\n    return {\"k\": [1, 2]}\n\nd = g()\nprint(d[\"k\"])\n",
	"for i in range(2):\n    junk = [i]\n    keep = [i, i]\n    print(keep)\n",
}

// operandAfter returns the memory operand of an instruction of the given kind: the `%_x` that follows the
// type marker in `store i32 0, i32* %_x` / `load i32, i32* %_x`, or "" for anything else.
func operandAfter(tt, kind, typeMark string) string {
	if !strings.HasPrefix(tt, kind) {
		return ""
	}
	i := strings.Index(tt, typeMark)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(tt[i+len(typeMark):])
	if j := strings.IndexAny(rest, " ,"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// operandWithType renders the operand back with its type marker, for the prefix test below.
func operandWithType(tt, typeMark string) string {
	if operandAfter(tt, "store ", typeMark) == "" {
		return ""
	}
	return typeMark + operandAfter(tt, "store ", typeMark)
}
