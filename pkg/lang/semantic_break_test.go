package lang
import "testing"
func TestBreakOutsideLoop(t *testing.T) {
 for _, src := range []string{
   "break",
   "continue",
   "i = 0\nwhile i < 5:\n    i = i + 1\n    if i == 2:\n        break\ni",
 }{
  prog, err := parseProgram(src)
  if err != nil { t.Fatalf("parse: %v", err) }
  diags := Analyze(prog)
  t.Logf("src=%q diags=%v", src, diags)
 }
}
