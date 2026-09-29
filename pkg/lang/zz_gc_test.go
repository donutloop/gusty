package lang

import (
	"os"
	"strconv"
	"testing"
)

func TestGCOrGateIsCulprit(t *testing.T) {
	src := func(n int) string {
		return "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\n\n    def norm(self) -> int:\n        return self.x * self.x + self.y * self.y\n\ns = 0\nfor i in range(" + strconv.Itoa(n) + "):\n    p = Point(i, i % 7)\n    s = s + p.norm()\nprint(s)\n"
	}
	run := func(t *testing.T, n int, noGC bool) (string, error) {
		prog, err := Parse(src(n))
		if err != nil {
			t.Fatal(err)
		}
		ev := NewEvaluator()
		if noGC {
			ev.gcThreshold = 1 << 60
		}
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		_, perr := ev.EvalProgram(prog)
		os.Stdout = old
		w.Close()
		buf := make([]byte, 128)
		n2, _ := r.Read(buf)
		return string(buf[:n2]), perr
	}
	out, err := run(t, 8000, false)
	t.Logf("with GC:      out=%q err=%v", out, err)
	out, err = run(t, 8000, true)
	t.Logf("without GC:   out=%q err=%v", out, err)
	// what does the value look like
	out, err = run(t, 2000, false)
	t.Logf("n=2000 GC:    out=%q err=%v", out, err)
}
