package lang

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// newTestDoc parses + analyzes src and builds a Document with an index.
func newTestDoc(t *testing.T, src string) *Document {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	Analyze(prog)
	d := &Document{URI: "test://doc", Text: src}
	d.Prog = prog
	d.Index = buildIndex(prog, src)
	return d
}

func TestLineColFrom(t *testing.T) {
	text := "def f():\n    x = 1\n"
	line, col := lineColFrom(text, lspPosition{Line: 0, Character: 0})
	if line != 1 || col != 1 {
		t.Fatalf("expected (1,1), got (%d,%d)", line, col)
	}
	line, col = lineColFrom(text, lspPosition{Line: 1, Character: 4})
	if line != 2 || col != 5 {
		t.Fatalf("expected (2,5), got (%d,%d)", line, col)
	}
}

func TestSpanToRange(t *testing.T) {
	text := "def sq(x):\n    pass\n"
	r := spanToRange(text, Span{Line: 1, Col: 5}, 2)
	if r.Start.Line != 0 || r.Start.Character != 4 || r.End.Character != 6 {
		t.Fatalf("bad range: %+v", r)
	}
}

func TestBuildIndexDefs(t *testing.T) {
	src := `# comment
def sq(x: int) -> int:
    """square"""
    return x * x
class Foo:
    pass
y = 10
`
	d := newTestDoc(t, src)
	foundFn := false
	foundClass := false
	foundVar := false
	for _, s := range d.Index.defs {
		switch s.Name {
		case "sq":
			if s.Kind != symFunc || !strings.Contains(s.Type, "sq(x: int)") || s.Doc == "" {
				t.Fatalf("bad func symbol: %+v", s)
			}
			foundFn = true
		case "Foo":
			if s.Kind != symClass {
				t.Fatalf("bad class symbol: %+v", s)
			}
			foundClass = true
		case "y":
			if s.Kind != symVar {
				t.Fatalf("bad var symbol: %+v", s)
			}
			foundVar = true
		}
	}
	if !foundFn || !foundClass || !foundVar {
		t.Fatalf("missing defs: fn=%v class=%v var=%v", foundFn, foundClass, foundVar)
	}
}

func TestBuildIndexRefs(t *testing.T) {
	src := "def f():\n    pass\nf()\n"
	d := newTestDoc(t, src)
	// The call f() should produce a reference named "f".
	found := false
	for _, r := range d.Index.refs {
		if r.name == "f" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a reference for f(): %+v", d.Index.refs)
	}
}

func TestHoverOnFunc(t *testing.T) {
	src := "def sq(x: int) -> int:\n    \"\"\"square\"\"\"\n    return x * x\n"
	d := newTestDoc(t, src)
	// hover on line 0, character of "sq"
	res := hoverAt(d, lspPosition{Line: 0, Character: 4})
	if res == nil || len(res.Contents) == 0 {
		t.Fatalf("expected hover contents")
	}
	joined := strings.Join(res.Contents, "\n")
	if !strings.Contains(joined, "sq(x: int)") || !strings.Contains(joined, "square") {
		t.Fatalf("hover missing type/doc: %+v", res.Contents)
	}
}

func TestCompleteInScope(t *testing.T) {
	src := "def helper():\n    pass\nx = 1\n"
	d := newTestDoc(t, src)
	res := completeAt(d, lspPosition{Line: 2, Character: 0})
	labels := map[string]bool{}
	for _, it := range res.Items {
		labels[it.Label] = true
	}
	if !labels["helper"] || !labels["x"] || !labels["print"] {
		t.Fatalf("completion missing entries: %+v", res.Items)
	}
}

// TestRunLSPEndToEnd drives a full stdio session: initialize, didOpen, hover,
// completion, shutdown, exit.
func TestRunLSPEndToEnd(t *testing.T) {
	msgs := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"rootUri":"file:///root"}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///a.gy","text":"def sq(x: int) -> int:\n    \"\"\"square\"\"\"\n    return x*x\nsq(4)\n"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"textDocument/hover","params":{"textDocument":{"uri":"file:///a.gy"},"position":{"line":0,"character":4}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///a.gy"},"position":{"line":3,"character":3}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"shutdown","params":{}}`,
		`{"jsonrpc":"2.0","method":"exit"}`,
	}
	var in bytes.Buffer
	for _, m := range msgs {
		in.WriteString("Content-Length: " + itoaLSP(len(m)) + "\r\n\r\n")
		in.WriteString(m)
	}
	var out bytes.Buffer
	RunLSP(&in, &out)

	// Parse all framed responses from the output.
	frames := splitFrames(out.String())
	if len(frames) < 2 {
		t.Fatalf("expected at least initialize + hover responses, got %d", len(frames))
	}
	// frame 0: initialize response
	var initResp rpcResponse
	if err := json.Unmarshal(frames[0], &initResp); err != nil {
		t.Fatalf("bad initialize frame: %v", err)
	}
	if initResp.ID == nil || initResp.Error != nil {
		t.Fatalf("initialize failed: %+v", initResp.Error)
	}
	// find hover response (id 2)
	foundHover := false
	foundCompletion := false
	for _, f := range frames {
		var resp rpcResponse
		if err := json.Unmarshal(f, &resp); err != nil {
			continue
		}
		if resp.ID != nil {
			var id int
			json.Unmarshal(resp.ID, &id)
			if id == 2 && resp.Error == nil {
				var hr hoverResult
				if err := json.Unmarshal(resp.Result, &hr); err != nil {
					t.Fatalf("hover result bad: %v", err)
				}
				if len(hr.Contents) == 0 {
					t.Fatalf("hover returned no contents")
				}
				foundHover = true
			}
			if id == 3 && resp.Error == nil {
				var cr completionResult
				if err := json.Unmarshal(resp.Result, &cr); err != nil {
					t.Fatalf("completion result bad: %v", err)
				}
				if len(cr.Items) == 0 {
					t.Fatalf("completion returned no items")
				}
				foundCompletion = true
			}
		}
	}
	if !foundHover || !foundCompletion {
		t.Fatalf("missing hover/completion responses: hover=%v completion=%v", foundHover, foundCompletion)
	}
}

// splitFrames parses Content-Length framed messages from a raw stdio stream.
func splitFrames(raw string) [][]byte {
	frames := [][]byte{}
	rest := raw
	for {
		idx := strings.Index(rest, "\r\n\r\n")
		if idx < 0 {
			break
		}
		header := rest[:idx]
		lenStr := ""
		for _, line := range strings.Split(header, "\r\n") {
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				lenStr = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:"))
			}
		}
		n := 0
		for _, ch := range lenStr {
			n = n*10 + int(ch-'0')
		}
		body := rest[idx+4 : idx+4+n]
		frames = append(frames, []byte(body))
		rest = rest[idx+4+n:]
	}
	return frames
}

func itoaLSP(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// TestIncrementalDidChange verifies the incremental-sync integration: an
// edit to statement 2 re-parses only the affected tail, keeps the leading
// statement's AST identity, and yields a tree equal to a full re-parse.
func TestIncrementalDidChange(t *testing.T) {
	src := "a = 1\nb = 2\nc = 3\n"
	d := &Document{URI: "mem://doc", Version: 1, Text: src, cache: mustNewCache(t, src)}
	d.analyze()
	oldProg := d.Prog

	// Edit line 2 ("b = 2" -> "b = 100") via the incremental path.
	if err := d.cache.Update([]Edit{{Start: Span{Line: 2, Col: 1}, End: Span{Line: 2, Col: 100}, NewText: "b = 100"}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	d.analyze()

	if d.Text != "a = 1\nb = 100\nc = 3\n" {
		t.Fatalf("document text not updated: %q", d.Text)
	}
	if len(d.Prog.Stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(d.Prog.Stmts))
	}
	// statement 0 ("a = 1") is before the edit and must keep identity.
	if d.Prog.Stmts[0] != oldProg.Stmts[0] {
		t.Fatalf("unaffected statement lost identity")
	}
	// the edited statement must be freshly parsed.
	if d.Prog.Stmts[1] == oldProg.Stmts[1] {
		t.Fatalf("affected statement kept stale identity")
	}
	// "c = 3" is after a single in-line edit whose text is unchanged apart from a
	// shift, so it is reused as well: reuse covers both sides of the edit.
	if d.cache.Reused() != 2 {
		t.Fatalf("expected 2 reused statements (before + after the edit), got %d", d.cache.Reused())
	}
	if d.Prog.Stmts[2] != oldProg.Stmts[2] {
		t.Fatalf("statement after the edit lost identity")
	}
}

func mustNewCache(t *testing.T, src string) *ParseCache {
	t.Helper()
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatalf("NewParseCache: %v", err)
	}
	return c
}

// L5.8 — the diagnostics notification carries an incremental-parse self-report, so an
// editor (or an agent driving the server) can tell a one-statement re-parse from a
// whole-document re-parse without timing the server.
func TestLSPPublishesParseCacheStats(t *testing.T) {
	open := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///big.gy","text":"x = 1\ndef f(a):\n    return a + 1\n\ny = f(2)\nz = y * 3\n"}}}`
	change := `{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///big.gy","version":2},"contentChanges":[{"range":{"start":{"line":0,"character":4},"end":{"line":0,"character":5}},"text":"2"}]}}`
	shutdown := `{"jsonrpc":"2.0","method":"exit"}`
	var in bytes.Buffer
	for _, m := range []string{open, change, shutdown} {
		in.WriteString("Content-Length: " + itoaLSP(len(m)) + "\r\n\r\n")
		in.WriteString(m)
	}
	var out bytes.Buffer
	RunLSP(&in, &out)

	type stats struct {
		Statements  int  `json:"statements"`
		Reused      int  `json:"reusedStatements"`
		Incremental bool `json:"incremental"`
	}
	type pub struct {
		Method string `json:"method"`
		Params struct {
			URI         string `json:"uri"`
			Version     int    `json:"version"`
			Diagnostics []struct {
				Message string `json:"message"`
			} `json:"diagnostics"`
			ParseCache *stats `json:"parseCache"`
		} `json:"params"`
	}
	var got []pub
	for _, f := range splitFrames(out.String()) {
		var p pub
		if err := json.Unmarshal(f, &p); err != nil || p.Method != "textDocument/publishDiagnostics" {
			continue
		}
		got = append(got, p)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 publishDiagnostics (didOpen + didChange), got %d", len(got))
	}
	changed := got[1].Params
	if changed.Version != 2 {
		t.Fatalf("second publication is version %d, want 2", changed.Version)
	}
	if changed.ParseCache == nil {
		t.Fatalf("didChange publication has no parseCache report")
	}
	if changed.ParseCache.Statements != 4 {
		t.Errorf("statements = %d, want 4", changed.ParseCache.Statements)
	}
	if !changed.ParseCache.Incremental || changed.ParseCache.Reused < 3 {
		t.Errorf("in-line edit re-parsed %d of %d statements; expected tail reuse",
			changed.ParseCache.Statements-changed.ParseCache.Reused, changed.ParseCache.Statements)
	}
	// The whole point of the report: the diagnostics must be the same ones a full
	// parse produces, so reuse cannot be hiding work.
	full, err := Parse("x = 2\ndef f(a):\n    return a + 1\n\ny = f(2)\nz = y * 3\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(Analyze(full)) != len(changed.Diagnostics) {
		t.Errorf("incremental diagnostics = %d, full parse = %d",
			len(changed.Diagnostics), len(Analyze(full)))
	}
}

// A didChange whose spans are unusable must not clobber the buffer. An
// incremental change carries only the edited region, so "falling back" to that
// text as a whole-document set would shrink the file to a fragment; the server
// must keep the previous buffer and say the change was not applied.
func TestLSPFailedIncrementKeepsBuffer(t *testing.T) {
	open := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///fb.gy","text":"a = 1\nb = 2\n"}}}`
	// Line 99 does not exist: the incremental update must fail.
	change := `{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///fb.gy","version":2},"contentChanges":[{"range":{"start":{"line":98,"character":0},"end":{"line":98,"character":0}},"text":"c = 3\n"}]}}`
	exit := `{"jsonrpc":"2.0","method":"exit"}`
	var in bytes.Buffer
	for _, m := range []string{open, change, exit} {
		in.WriteString("Content-Length: " + itoaLSP(len(m)) + "\r\n\r\n")
		in.WriteString(m)
	}
	var out bytes.Buffer
	RunLSP(&in, &out)
	checked := false
	for _, f := range splitFrames(out.String()) {
		var p struct {
			Method string `json:"method"`
			Params struct {
				Version     int `json:"version"`
				Diagnostics []struct {
					Message  string `json:"message"`
					Severity int    `json:"severity"`
				} `json:"diagnostics"`
				ParseCache *struct {
					Statements int `json:"statements"`
				} `json:"parseCache"`
			} `json:"params"`
		}
		if json.Unmarshal(f, &p) != nil || p.Method != "textDocument/publishDiagnostics" || p.Params.Version != 2 {
			continue
		}
		checked = true
		if p.Params.ParseCache != nil && p.Params.ParseCache.Statements != 2 {
			t.Fatalf("after a failed incremental update the server reports %d statements; the buffer must keep the 2 statements it still holds (not the 1-statement fragment)",
				p.Params.ParseCache.Statements)
		}
		warned := false
		for _, d := range p.Params.Diagnostics {
			if strings.Contains(d.Message, "could not apply") {
				warned = true
			}
		}
		if !warned {
			t.Fatalf("the change was not applied but no diagnostic says so: %+v", p.Params.Diagnostics)
		}
	}
	if !checked {
		t.Fatal("no version-2 diagnostics notification was published")
	}
}
