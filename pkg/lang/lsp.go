package lang

// Package lang exposes a Language Server (LSP) over stdio speaking JSON-RPC 2.0
// with the standard Content-Length framing. It gives editors live diagnostics
// (parse + semantic analysis), hover (inferred type + docstring), and
// completion (names in scope) for gusty source.
//
// Positions in the LSP protocol are 0-based, UTF-16 code-unit offsets; the
// language's Span is 1-based line/column in runes, so lsp.go converts between
// the two coordinate systems.

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// JSON-RPC wire types
// ---------------------------------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcOK(id json.RawMessage, result any) rpcResponse {
	b, err := json.Marshal(result)
	if err != nil {
		b = []byte(`null`)
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: b}
}

func rpcFail(id json.RawMessage, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// ---------------------------------------------------------------------------
// LSP wire types
// ---------------------------------------------------------------------------

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspDiag struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"` // 1 error, 2 warning, 3 info
	Message  string   `json:"message"`
	Source   string   `json:"source"`
	Code     string   `json:"code,omitempty"`
}

type textDocID struct {
	URI string `json:"uri"`
}

type textDocParam struct {
	TextDocument textDocID `json:"textDocument"`
}

type didOpenParams struct {
	TextDocument struct {
		URI  string `json:"uri"`
		Text string `json:"text"`
	} `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument    textDocID `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type hoverParams struct {
	TextDocument textDocID   `json:"textDocument"`
	Position     lspPosition `json:"position"`
}

type completionParams struct {
	TextDocument textDocID   `json:"textDocument"`
	Position     lspPosition `json:"position"`
}

type hoverResult struct {
	Contents []string  `json:"contents"`
	Range    *lspRange `json:"range,omitempty"`
}

type completionItem struct {
	Label      string `json:"label"`
	Kind       int    `json:"kind"` // 3 function, 5 class, 6 variable
	Detail     string `json:"detail,omitempty"`
	InsertText string `json:"insertText,omitempty"`
}

type completionResult struct {
	Items        []completionItem `json:"items"`
	IsIncomplete bool             `json:"isIncomplete"`
}

type publishParams struct {
	URI         string    `json:"uri"`
	Version     int       `json:"version,omitempty"`
	Diagnostics []lspDiag `json:"diagnostics"`
}

type initResult struct {
	Capabilities struct {
		TextDocumentSync   int `json:"textDocumentSync"` // 1 = full sync
		HoverProvider      bool `json:"hoverProvider"`
		CompletionProvider struct {
			TriggerCharacters []string `json:"triggerCharacters"`
		} `json:"completionProvider"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// ---------------------------------------------------------------------------
// Symbol index
// ---------------------------------------------------------------------------

type symbolKind int

const (
	symFunc symbolKind = iota
	symClass
	symVar
	symParam
	symLoopVar
)

// Symbol is one definition site (function, class, variable, parameter, loop
// variable) collected by buildIndex.
type lspSymbol struct {
	Name  string
	Kind  symbolKind
	Span  Span
	Type  string
	Doc   string
	Owner string // enclosing func/class name; "" = module level
	Line  int
}

// ref is an identifier occurrence (a Name node or an Attr attribute name)
// resolved against a definition.
type ref struct {
	name  string
	kind  symbolKind
	span  Span
	typ   string
	obj   string
	doc   string
	owner string
}

// ownerRange tracks the line span of a FuncDef/ClassDef so completion can
// decide which scope is active at a cursor position.
type ownerRange struct {
	name      string
	startLine int
	endLine   int
}

// docIndex is the symbol table for one document: definitions, references, and
// the function/class line ranges used for scope-aware completion.
type docIndex struct {
	defs   []lspSymbol
	refs   []ref
	owners []ownerRange
}

// ---------------------------------------------------------------------------
// Coordinate conversion helpers
// ---------------------------------------------------------------------------

// lineColFrom converts an LSP position (0-based line, UTF-16 character) into a
// 1-based rune line/column Span in the given document text.
func lineColFrom(text string, p lspPosition) (line, col int) {
	lines := strings.Split(text, "\n")
	line = p.Line + 1
	if line < 1 || line > len(lines) {
		return 1, 1
	}
	src := lines[p.Line]
	runes := []rune(src)
	u := 0
	runeIdx := 0
	for runeIdx < len(runes) {
		r := runes[runeIdx]
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if u+w > p.Character {
			break
		}
		u += w
		runeIdx++
	}
	return line, runeIdx + 1
}

// spanToRange converts a Span (1-based rune line/col) to a single-point LSP
// range at the identifier. The end is the start advanced by the name length in
// UTF-16 units, so editors highlight the word.
func spanToRange(text string, s Span, nameLen int) lspRange {
	lines := strings.Split(text, "\n")
	line := s.Line
	if line < 1 || line > len(lines) {
		return lspRange{Start: lspPosition{Line: 0, Character: 0}, End: lspPosition{Line: 0, Character: 0}}
	}
	runes := []rune(lines[line-1])
	u := 0
	runeIdx := 0
	for runeIdx < len(runes) && runeIdx+1 < s.Col {
		r := runes[runeIdx]
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		u += w
		runeIdx++
	}
	start := lspPosition{Line: line - 1, Character: u}
	end := lspPosition{Line: line - 1, Character: u + nameLen}
	return lspRange{Start: start, End: end}
}

// tyOf extracts the inferred type string from any Expr node (the semantic
// pass attaches Ty to concrete nodes; the Expr interface itself carries no Ty).
func tyOf(e Expr) string {
	if e == nil {
		return ""
	}
	switch n := e.(type) {
	case *Name:
		return n.Ty
	case *Attr:
		return n.Ty
	case *Call:
		return n.Ty
	case *BinOp:
		return n.Ty
	case *UnOp:
		return n.Ty
	case *Index:
		return n.Ty
	case *Slice:
		return n.Ty
	case *IntLit:
		return n.Ty
	case *FloatLit:
		return n.Ty
	case *BoolLit:
		return n.Ty
	case *NoneLit:
		return n.Ty
	case *StrLit:
		return n.Ty
	case *ListLit:
		return n.Ty
	case *DictLit:
		return n.Ty
	case *SetLit:
		return n.Ty
	case *Tuple:
		return n.Ty
	case *CondExpr:
		return n.Ty
	case *Lambda:
		return n.Ty
	case *Comp:
		return n.Ty
	case *Generator:
		return n.Ty
	case *FString:
		return n.Ty
	}
	return ""
}

// ---------------------------------------------------------------------------
// buildIndex: walk the AST to collect defs + refs
// ---------------------------------------------------------------------------

func buildIndex(prog *Program, src string) *docIndex {
	idx := &docIndex{}

	var walkStmts func(stmts []Stmt, owner string)
	var walkExpr func(e Expr, owner string)

	addDef := func(sym lspSymbol) {
		idx.defs = append(idx.defs, sym)
	}
	addRef := func(r ref) {
		idx.refs = append(idx.refs, r)
	}

	walkStmts = func(stmts []Stmt, owner string) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *FuncDef:
				addDef(lspSymbol{Name: s.Name, Kind: symFunc, Span: nameSpan(src, s.Src.Line, s.Name, "def "), Type: renderFnType(s), Doc: funcDoc(s), Owner: owner, Line: s.Src.Line})
				for _, p := range s.Params {
					addDef(lspSymbol{Name: p.Name, Kind: symParam, Span: p.Src, Type: typeName(p.Annot), Owner: s.Name, Line: p.Src.Line})
				}
				idx.owners = append(idx.owners, ownerRange{name: s.Name, startLine: s.Src.Line, endLine: maxStmtLine(s.Body)})
				walkStmts(s.Body, s.Name)
			case *ClassDef:
				addDef(lspSymbol{Name: s.Name, Kind: symClass, Span: nameSpan(src, s.Src.Line, s.Name, "class "), Type: "class", Doc: s.Doc, Owner: owner, Line: s.Src.Line})
				idx.owners = append(idx.owners, ownerRange{name: s.Name, startLine: s.Src.Line, endLine: maxStmtLine(s.Body)})
				walkStmts(s.Body, s.Name)
			case *AssignStmt:
				if n, ok := s.Target.(*Name); ok {
					addDef(lspSymbol{Name: n.Value, Kind: symVar, Span: n.Src, Type: n.Ty, Owner: owner, Line: n.Src.Line})
				} else {
					walkExpr(s.Target, owner)
				}
				walkExpr(s.Value, owner)
			case *AugAssignStmt:
				if n, ok := s.Target.(*Name); ok {
					addDef(lspSymbol{Name: n.Value, Kind: symVar, Span: n.Src, Type: n.Ty, Owner: owner, Line: n.Src.Line})
				} else {
					walkExpr(s.Target, owner)
				}
				walkExpr(s.Value, owner)
			case *ForStmt:
				for _, nm := range forVarNames(s.Var) {
					addDef(lspSymbol{Name: nm.Value, Kind: symLoopVar, Span: nm.Src, Type: nm.Ty, Owner: owner, Line: nm.Src.Line})
				}
				walkExpr(s.Iter, owner)
				walkStmts(s.Body, owner)
				walkStmts(s.Else, owner)
			case *WithStmt:
				if s.As != nil {
					addDef(lspSymbol{Name: s.As.Value, Kind: symVar, Span: s.As.Src, Type: s.As.Ty, Owner: owner, Line: s.As.Src.Line})
				}
				walkExpr(s.Expr, owner)
				walkStmts(s.Body, owner)
			case *IfStmt:
				walkExpr(s.Cond, owner)
				walkStmts(s.Then, owner)
				for _, e := range s.Elifs {
					walkExpr(e.Cond, owner)
					walkStmts(e.Then, owner)
				}
				walkStmts(s.Else, owner)
			case *WhileStmt:
				walkExpr(s.Cond, owner)
				walkStmts(s.Body, owner)
				walkStmts(s.Else, owner)
			case *TryStmt:
				walkStmts(s.Body, owner)
				for _, e := range s.Excepts {
					if e.Exn != nil {
						addDef(lspSymbol{Name: e.Exn.Value, Kind: symVar, Span: e.Exn.Src, Type: "exception", Owner: owner, Line: e.Exn.Src.Line})
					}
					walkStmts(e.Body, owner)
				}
				walkStmts(s.Finally, owner)
			case *MatchStmt:
				walkExpr(s.Subject, owner)
				for _, c := range s.Cases {
					walkExpr(c.Pattern, owner)
					if c.Guard != nil {
						walkExpr(c.Guard, owner)
					}
					walkStmts(c.Body, owner)
				}
			case *ReturnStmt:
				walkExpr(s.Expr, owner)
			case *YieldStmt:
				walkExpr(s.Expr, owner)
			case *YieldFromStmt:
				walkExpr(s.Expr, owner)
			case *ExprStmt:
				walkExpr(s.Expr, owner)
			case *ImportStmt:
				addDef(lspSymbol{Name: s.Module, Kind: symVar, Span: s.Src, Type: "module", Owner: owner, Line: s.Src.Line})
			case *BreakStmt, *PassStmt, *ContinueStmt:
				// no embedded refs
			default:
				// unknown statement kinds: ignore
			}
		}
	}

	walkExpr = func(e Expr, owner string) {
		if e == nil {
			return
		}
		switch n := e.(type) {
		case *Name:
			addRef(ref{name: n.Value, span: n.Src, typ: n.Ty, owner: owner})
		case *Attr:
			addRef(ref{name: n.Name.Value, span: n.Name.Src, typ: n.Ty, obj: tyOf(n.Obj), owner: owner})
			walkExpr(n.Obj, owner)
		case *Call:
			walkExpr(n.Fn, owner)
			for _, a := range n.Args {
				walkExpr(a, owner)
			}
		case *KeywordArg:
			walkExpr(n.Value, owner)
		case *BinOp:
			walkExpr(n.L, owner)
			walkExpr(n.R, owner)
		case *UnOp:
			walkExpr(n.X, owner)
		case *Index:
			walkExpr(n.Obj, owner)
			walkExpr(n.Idx, owner)
		case *Slice:
			walkExpr(n.Obj, owner)
			walkExpr(n.Low, owner)
			walkExpr(n.High, owner)
		case *CondExpr:
			walkExpr(n.Cond, owner)
			walkExpr(n.If, owner)
			walkExpr(n.Else, owner)
		case *Tuple:
			for _, el := range n.Elems {
				walkExpr(el, owner)
			}
		case *ListLit:
			for _, el := range n.Elems {
				walkExpr(el, owner)
			}
		case *SetLit:
			for _, el := range n.Elems {
				walkExpr(el, owner)
			}
		case *DictLit:
			for _, k := range n.Keys {
				walkExpr(k, owner)
			}
			for _, v := range n.Vals {
				walkExpr(v, owner)
			}
		case *Lambda:
			for _, p := range n.Params {
				addDef(lspSymbol{Name: p.Name, Kind: symParam, Span: p.Src, Type: typeName(p.Annot), Owner: owner, Line: p.Src.Line})
			}
			walkExpr(n.Body, owner)
		case *Comp:
			walkExpr(n.ForVar, owner)
			walkExpr(n.Iter, owner)
			if n.Cond != nil {
				walkExpr(n.Cond, owner)
			}
			for _, el := range n.Elems {
				walkExpr(el, owner)
			}
		case *Generator:
			walkExpr(n.ForVar, owner)
			walkExpr(n.Iter, owner)
			if n.Cond != nil {
				walkExpr(n.Cond, owner)
			}
			for _, el := range n.Elems {
				walkExpr(el, owner)
			}
		case *FString:
			// f-string parts are mostly literals; ignore nested exprs
		default:
			// literal nodes carry no references
		}
	}

	walkStmts(prog.Stmts, "")

	// Resolve refs to defs so hover shows documentation even at a use site.
	byName := map[string]lspSymbol{}
	for _, d := range idx.defs {
		if _, ok := byName[d.Name]; !ok {
			byName[d.Name] = d
		}
	}
	for i := range idx.refs {
		if d, ok := byName[idx.refs[i].name]; ok {
			idx.refs[i].doc = d.Doc
			if idx.refs[i].typ == "" {
				idx.refs[i].typ = d.Type
			}
		}
	}
	return idx
}

// forVarNames extracts the Name nodes bound by a for-loop variable (a single
// Name or a tuple of Names).
func forVarNames(v Expr) []*Name {
	var out []*Name
	switch n := v.(type) {
	case *Name:
		out = append(out, n)
	case *Tuple:
		for _, el := range n.Elems {
			if nm, ok := el.(*Name); ok {
				out = append(out, nm)
			}
		}
	}
	return out
}

// maxStmtLine returns the maximum source line across a statement block, used
// to bound the line span of a function/class for scope resolution.
func maxStmtLine(stmts []Stmt) int {
	max := 0
	for _, st := range stmts {
		if sp := st.Span(); sp.Line > max {
			max = sp.Line
		}
	}
	if max == 0 {
		return 1
	}
	return max
}


// nameSpan locates the column of a function/class name in its source line.
func nameSpan(src string, line int, name, prefix string) Span {
	lines := strings.Split(src, "\n")
	if line < 1 || line > len(lines) {
		return Span{Line: line, Col: 1}
	}
	idx := strings.Index(lines[line-1], name)
	if idx < 0 {
		return Span{Line: line, Col: 1}
	}
	return Span{Line: line, Col: idx + 1}
}

// funcDoc extracts the first docstring statement from a function body.
func funcDoc(f *FuncDef) string {
	if len(f.Body) == 0 {
		return ""
	}
	if es, ok := f.Body[0].(*ExprStmt); ok {
		if lit, ok := es.Expr.(*StrLit); ok {
			return lit.Value
		}
	}
	return ""
}

// renderFnType renders a function signature like `sq(x: int) -> int`.
func renderFnType(f *FuncDef) string {
	var b strings.Builder
	b.WriteString(f.Name)
	b.WriteString("(")
	for i, p := range f.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Name)
		if p.Annot != nil {
			b.WriteString(": ")
			b.WriteString(p.Annot.Name())
		}
	}
	b.WriteString(")")
	if f.ReturnAnno != nil {
		b.WriteString(" -> ")
		b.WriteString(f.ReturnAnno.Name())
	}
	return b.String()
}

// typeName renders a Type as a string, or "" when nil.
func typeName(t *Type) string {
	if t == nil {
		return ""
	}
	return t.Name()
}

// ---------------------------------------------------------------------------
// Document lifecycle
// ---------------------------------------------------------------------------

// Document is an open editor buffer: its parsed program, symbol index, and
// diagnostics.
type Document struct {
	URI     string
	Version int
	Text    string
	Prog    *Program
	Index   *docIndex
	Diags   []lspDiag
}

// analyze parses + semantically analyzes src and rebuilds the index.
func (d *Document) analyze() {
	d.Diags = nil
	prog, err := Parse(d.Text)
	if err != nil {
		line, col := 1, 1
		if pe, ok := err.(*ParseError); ok {
			line, col = pe.Span.Line, pe.Span.Col
		}
		d.Diags = []lspDiag{diagAt(line, col, 1, "parse error: "+err.Error())}
		d.Prog = nil
		d.Index = nil
		return
	}
	semDiags := Analyze(prog)
	d.Prog = prog
	d.Index = buildIndex(prog, d.Text)
	d.Diags = make([]lspDiag, 0, len(semDiags))
	for _, sd := range semDiags {
		sev := 3
		switch sd.Level {
		case LevelError:
			sev = 1
		case LevelWarning:
			sev = 2
		}
		d.Diags = append(d.Diags, diagAt(sd.Span.Line, sd.Span.Col, sev, sd.Msg))
	}
}

func diagAt(line, col, sev int, msg string) lspDiag {
	return lspDiag{
		Range:    lspRange{Start: lspPosition{Line: line - 1, Character: col - 1}, End: lspPosition{Line: line - 1, Character: col}},
		Severity: sev,
		Message:  msg,
		Source:   "gusty",
	}
}

// ---------------------------------------------------------------------------
// Hover
// ---------------------------------------------------------------------------

// hoverAt returns the hover contents for a position, or nil when no symbol is
// under the cursor.
func hoverAt(doc *Document, p lspPosition) *hoverResult {
	line, col := lineColFrom(doc.Text, p)
	// Search references first (identifier use sites), then definitions.
	for _, r := range doc.Index.refs {
		if r.span.Line == line && col >= r.span.Col && col < r.span.Col+len([]rune(r.name)) {
			contents := []string{}
			if r.typ != "" {
				contents = append(contents, "```\n"+r.typ+"\n```")
			} else {
				contents = append(contents, "```\n"+r.name+"\n```")
			}
			if r.doc != "" {
				contents = append(contents, r.doc)
			}
			rng := spanToRange(doc.Text, r.span, len([]rune(r.name)))
			return &hoverResult{Contents: contents, Range: &rng}
		}
	}
	for _, s := range doc.Index.defs {
		if s.Span.Line == line && col >= s.Span.Col && col < s.Span.Col+len([]rune(s.Name)) {
			contents := []string{}
			if s.Type != "" {
				contents = append(contents, "```\n"+s.Type+"\n```")
			} else {
				contents = append(contents, "```\n"+s.Name+"\n```")
			}
			if s.Doc != "" {
				contents = append(contents, s.Doc)
			}
			rng := spanToRange(doc.Text, s.Span, len([]rune(s.Name)))
			return &hoverResult{Contents: contents, Range: &rng}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Completion
// ---------------------------------------------------------------------------

// builtins are the language's predeclared names offered at every scope.
var builtins = []string{
	"print", "range", "min", "max", "abs", "len", "int", "float", "str",
	"list", "dict", "set", "sorted", "reversed", "ord", "chr", "input",
	"isinstance", "type", "super", "Exception", "True", "False", "None",
}

func kindOf(s lspSymbol) int {
	switch s.Kind {
	case symFunc:
		return 3
	case symClass:
		return 5
	default:
		return 6
	}
}

// completeAt returns completion items visible in scope at a position.
func completeAt(doc *Document, p lspPosition) *completionResult {
	line := p.Line + 1
	owner := activeOwner(doc.Index, line)
	seen := map[string]bool{}
	items := []completionItem{}

	add := func(name, detail string, kind int) {
		if seen[name] {
			return
		}
		seen[name] = true
		items = append(items, completionItem{Label: name, Kind: kind, Detail: detail})
	}

	// module-level definitions are always offered
	for _, s := range doc.Index.defs {
		if s.Owner == "" {
			add(s.Name, s.Type, kindOf(s))
		}
	}
	// names defined earlier in the active scope
	for _, s := range doc.Index.defs {
		if s.Owner == owner && s.Line <= line {
			add(s.Name, s.Type, kindOf(s))
		}
	}
	// builtins
	for _, b := range builtins {
		add(b, "", 3)
	}
	return &completionResult{Items: items, IsIncomplete: false}
}

// activeOwner returns the innermost function/class enclosing a line.
func activeOwner(idx *docIndex, line int) string {
	best := ""
	bestStart := -1
	for _, o := range idx.owners {
		if line >= o.startLine && line <= o.endLine && o.startLine > bestStart {
			best = o.name
			bestStart = o.startLine
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// stdio JSON-RPC loop
// ---------------------------------------------------------------------------

const lspServerName = "gustyc-lsp"
const lspServerVersion = "0.1.0"

// RunLSP runs the language server over the given reader/writer using the LSP
// stdio protocol (Content-Length framing). It blocks until the client sends
// exit.
func RunLSP(r io.Reader, w io.Writer) {
	docs := map[string]*Document{}
	write := func(msg any) {
		b, err := json.Marshal(msg)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(b))
		w.Write(b)
	}
	publish := func(d *Document) {
		write(map[string]any{
			"jsonrpc": "2.0",
			"method":  "textDocument/publishDiagnostics",
			"params":  publishParams{URI: d.URI, Version: d.Version, Diagnostics: d.Diags},
		})
	}

	for {
		body, err := readMessage(r)
		if err != nil {
			return
		}
		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		isNotification := req.ID == nil

		switch req.Method {
		case "initialize":
			var params struct {
				RootURI string `json:"rootUri"`
			}
			json.Unmarshal(req.Params, &params)
			var init initResult
			init.Capabilities.TextDocumentSync = 1
			init.Capabilities.HoverProvider = true
			init.Capabilities.CompletionProvider.TriggerCharacters = []string{".", "\"", "'"}
			init.ServerInfo.Name = lspServerName
			init.ServerInfo.Version = lspServerVersion
			write(rpcOK(req.ID, init))
		case "initialized":
			// notification; nothing to do
		case "shutdown":
			write(rpcOK(req.ID, nil))
		case "exit":
			return
		case "textDocument/didOpen":
			var p didOpenParams
			json.Unmarshal(req.Params, &p)
			d := &Document{URI: p.TextDocument.URI, Version: 1, Text: p.TextDocument.Text}
			d.analyze()
			docs[d.URI] = d
			publish(d)
		case "textDocument/didChange":
			var p didChangeParams
			json.Unmarshal(req.Params, &p)
			d := docs[p.TextDocument.URI]
			if d == nil {
				d = &Document{URI: p.TextDocument.URI}
				docs[d.URI] = d
			}
			d.Version++
			if len(p.ContentChanges) > 0 {
				d.Text = p.ContentChanges[len(p.ContentChanges)-1].Text
			}
			d.analyze()
			publish(d)
		case "textDocument/didClose":
			var p textDocParam
			json.Unmarshal(req.Params, &p)
			delete(docs, p.TextDocument.URI)
		case "textDocument/hover":
			var p hoverParams
			json.Unmarshal(req.Params, &p)
			d := docs[p.TextDocument.URI]
			if d == nil || d.Index == nil {
				if isNotification {
					continue
				}
				write(rpcOK(req.ID, &hoverResult{}))
				continue
			}
			res := hoverAt(d, p.Position)
			if res == nil {
				res = &hoverResult{}
			}
			write(rpcOK(req.ID, res))
		case "textDocument/completion":
			var p completionParams
			json.Unmarshal(req.Params, &p)
			d := docs[p.TextDocument.URI]
			if d == nil || d.Index == nil {
				if isNotification {
					continue
				}
				write(rpcOK(req.ID, &completionResult{Items: []completionItem{}, IsIncomplete: false}))
				continue
			}
			write(rpcOK(req.ID, completeAt(d, p.Position)))
		default:
			if !isNotification {
				write(rpcFail(req.ID, -32601, "method not found: "+req.Method))
			}
		}
	}
}

// readMessage reads one JSON-RPC message from an LSP stdio stream by parsing
// the Content-Length header and reading exactly that many body bytes.
func readMessage(r io.Reader) ([]byte, error) {
	headers := map[string]string{}
	for {
		line, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if len(line) == 0 {
			break
		}
		idx := strings.IndexByte(string(line), ':')
		if idx < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(string(line[:idx])))
		val := strings.TrimSpace(string(line[idx+1:]))
		headers[key] = val
	}
	lenStr := headers["content-length"]
	if lenStr == "" {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	n, err := strconv.Atoi(lenStr)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("invalid Content-Length: %q", lenStr)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// readLine reads a single \r\n (or \n) terminated header line.
func readLine(r io.Reader) ([]byte, error) {
	out := []byte{}
	buf := make([]byte, 1)
	for {
		_, err := r.Read(buf)
		if err != nil {
			return out, err
		}
		if buf[0] == '\n' {
			return out, nil
		}
		if buf[0] != '\r' {
			out = append(out, buf[0])
		}
	}
}
