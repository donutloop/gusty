package lang

// Node is any AST node carrying a source span.
type Node interface {
	Span() Span
}

// Program is the root AST: a list of statements.
type Program struct {
	Stmts []Stmt `json:"stmts"`
	Src    Span   `json:"span,omitempty"`
}

func (p *Program) Span() Span     { return p.Src }
func (p *Program) SetSpan(s Span) { p.Src = s }

// Stmt is a statement node.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is an expression node.
type Expr interface {
	Node
	exprNode()
}

// --- Statements ---

type ReturnStmt struct {
	Expr Expr `json:"expr"`
	Src   Span `json:"span,omitempty"`
}

func (n *ReturnStmt) Span() Span     { return n.Src }
func (n *ReturnStmt) stmtNode()      {}
func (n *ReturnStmt) SetSpan(s Span) { n.Src = s }

type ExprStmt struct {
	Expr Expr `json:"expr"`
	Src   Span `json:"span,omitempty"`
}

func (n *ExprStmt) Span() Span { return n.Src }
func (n *ExprStmt) stmtNode()  {}

type AssignStmt struct {
	Target Expr  `json:"target"`
	Value  Expr  `json:"value"`
	Annot  *Type `json:"annot,omitempty"`
	Src     Span  `json:"span,omitempty"`
}

type RaiseStmt struct {
	Expr Expr
	Src   Span
}

func (n *RaiseStmt) Span() Span { return n.Src }

func (n *RaiseStmt) stmtNode()   {}
func (n *AssignStmt) Span() Span { return n.Src }
func (n *AssignStmt) stmtNode()  {}

// AugAssignStmt is an augmented assignment `x op= e` (`x += e`, `x -= e`,
// `x *= e`, `x /= e`, `x //= e`, `x %= e`). Target must be a writable
// lvalue (Name or Attr); Op is one of "+", "-", "*", "/", "//", "%".
type AugAssignStmt struct {
	Target Expr   `json:"target"`
	Op     string `json:"op"`
	Value  Expr   `json:"value"`
	Src     Span   `json:"span,omitempty"`
}

func (n *AugAssignStmt) Span() Span { return n.Src }
func (n *AugAssignStmt) stmtNode()  {}

type IfStmt struct {
	Cond  Expr      `json:"cond"`
	Then  []Stmt    `json:"then"`
	Elifs []*IfStmt `json:"elifs,omitempty"`
	Else  []Stmt    `json:"else,omitempty"`
	Src    Span      `json:"span,omitempty"`
}

func (n *IfStmt) Span() Span { return n.Src }
func (n *IfStmt) stmtNode()  {}

type WhileStmt struct {
	Cond Expr   `json:"cond"`
	Body []Stmt `json:"body"`
	Else []Stmt `json:"else"`
	Src   Span   `json:"span,omitempty"`
}

func (n *WhileStmt) Span() Span { return n.Src }
func (n *WhileStmt) stmtNode()  {}

type ForStmt struct {
	Var  Expr   `json:"var"`
	Iter Expr   `json:"iter"`
	Body []Stmt `json:"body"`
	Else []Stmt `json:"else"`
	Src   Span   `json:"span,omitempty"`
}

func (n *ForStmt) Span() Span { return n.Src }
func (n *ForStmt) stmtNode()  {}

type Param struct {
	Name    string `json:"name"`
	Annot   *Type  `json:"annot,omitempty"`
	Default Expr   `json:"default,omitempty"`
	Src      Span   `json:"span,omitempty"`
}

func (n *Param) Span() Span { return n.Src }

type FuncDef struct {
	Name       string   `json:"name"`
	Params     []*Param `json:"params"`
	ReturnAnno *Type    `json:"return_annot,omitempty"`
	Doc        string   `json:"doc,omitempty"`
	Body       []Stmt   `json:"body"`
	Decorators []Expr   `json:"decorators,omitempty"`
	Src         Span     `json:"span,omitempty"`
}

func (n *FuncDef) Span() Span { return n.Src }
// ExternDecl declares a C function that can be called from gusty (FFI).
type ExternDecl struct {
	Name       string   `json:"name"`
	Params     []*Param `json:"params,omitempty"`
	ReturnAnno *Type    `json:"return_annot,omitempty"`
	Src         Span     `json:"span,omitempty"`
}

func (n *ExternDecl) Span() Span { return n.Src }
func (n *ExternDecl) stmtNode()  {}

func (n *FuncDef) stmtNode()  {}

type ClassDef struct {
	Name  string  `json:"name"`
	Bases []*Name `json:"bases,omitempty"`
	Doc   string  `json:"doc,omitempty"`
	Body  []Stmt  `json:"body"`
	Src    Span    `json:"span,omitempty"`
}

func (n *ClassDef) Span() Span { return n.Src }
func (n *ClassDef) stmtNode()  {}

type MatchCase struct {
	Pattern Expr   `json:"pattern"`
	Or      []Expr `json:"or,omitempty"`
	Guard   Expr   `json:"guard,omitempty"`
	Body    []Stmt `json:"body"`
	Src      Span   `json:"span,omitempty"`
}

type MatchStmt struct {
	Subject Expr         `json:"subject"`
	Cases   []*MatchCase `json:"cases"`
	Src      Span         `json:"span,omitempty"`
}

func (n *MatchStmt) Span() Span { return n.Src }
func (n *MatchStmt) stmtNode()  {}

type ExceptClause struct {
	Exn  *Name  `json:"exn,omitempty"`
	Body []Stmt `json:"body"`
	Src   Span   `json:"span,omitempty"`
}

type TryStmt struct {
	Body    []Stmt          `json:"body"`
	Excepts []*ExceptClause `json:"excepts,omitempty"`
	Finally []Stmt          `json:"finally,omitempty"`
	Src      Span            `json:"span,omitempty"`
}

func (n *TryStmt) Span() Span { return n.Src }
func (n *TryStmt) stmtNode()  {}

type ImportStmt struct {
	Module string `json:"module"`
	Src     Span   `json:"span,omitempty"`
}

func (n *ImportStmt) Span() Span { return n.Src }
func (n *ImportStmt) stmtNode()  {}

type YieldStmt struct {
	Expr Expr `json:"expr"`
	Src   Span `json:"span,omitempty"`
}

func (n *YieldStmt) Span() Span { return n.Src }
func (n *YieldStmt) stmtNode()  {}

// YieldFromStmt is `yield from expr`: delegate yields to a sub-iterable.
type YieldFromStmt struct {
	Expr Expr `json:"expr"`
	Src   Span `json:"span,omitempty"`
}

func (n *YieldFromStmt) Span() Span { return n.Src }
func (n *YieldFromStmt) stmtNode()  {}

// WithStmt is `with expr [as name]: body`. It drives a context manager's
// __enter__/__exit__ protocol.
type WithStmt struct {
	Expr Expr   `json:"expr"`
	As   *Name  `json:"as,omitempty"` // binding name, nil when no `as`
	Body []Stmt `json:"body"`
	Src   Span   `json:"span,omitempty"`
}

func (n *WithStmt) Span() Span { return n.Src }
func (n *WithStmt) stmtNode()  {}

// --- Expressions ---

type Name struct {
	Value string `json:"name"`
	Src    Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Name) Span() Span { return n.Src }
func (n *Name) exprNode()  {}

type IntLit struct {
	Value int64 `json:"value"`
	Src    Span  `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *IntLit) Span() Span { return n.Src }
func (n *IntLit) exprNode()  {}

type FloatLit struct {
	Value float64 `json:"value"`
	Src    Span    `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *FloatLit) Span() Span { return n.Src }
func (n *FloatLit) exprNode()  {}

type BoolLit struct {
	Value bool `json:"value"`
	Src    Span `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *BoolLit) Span() Span { return n.Src }
func (n *BoolLit) exprNode()  {}

type NoneLit struct {
	Src Span `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *NoneLit) Span() Span { return n.Src }
func (n *NoneLit) exprNode()  {}

// Tuple is a comma-separated expression list `a, b` or `(a, b)`.
type Tuple struct {
	Elems []Expr `json:"elems"`
	Src    Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Tuple) Span() Span { return n.Src }

func (n *Tuple) exprNode() {}

// loopVarNames returns the names bound by a for-loop variable expression
// (a Name, or each element of a Tuple of Names).
func loopVarNames(v Expr) []string {
	switch t := v.(type) {
	case *Name:
		return []string{t.Value}
	case *Tuple:
		var names []string
		for _, e := range t.Elems {
			if n, ok := e.(*Name); ok {
				names = append(names, n.Value)
			}
		}
		return names
	}
	return nil
}

type StrLit struct {
	Value string `json:"value"`
	Src    Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *StrLit) Span() Span { return n.Src }
func (n *StrLit) exprNode()  {}

type FString struct {
	Parts []FStringPart `json:"parts"`
	Src    Span          `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *FString) Span() Span { return n.Src }
func (n *FString) exprNode()  {}

// FStringPart is one segment of an interpolated format string: either a
// literal segment (Lit non-empty) or an interpolated expression (Expr set).
type FStringPart struct {
	Lit  string `json:"lit,omitempty"`
	Expr Expr   `json:"expr,omitempty"`
}

type ListLit struct {
	Elems []Expr `json:"elems"`
	Src    Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *ListLit) Span() Span { return n.Src }
func (n *ListLit) exprNode()  {}

type DictLit struct {
	Keys []Expr `json:"keys"`
	Vals []Expr `json:"vals"`
	Src   Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *DictLit) Span() Span { return n.Src }
func (n *DictLit) exprNode()  {}

type SetLit struct {
	Elems []Expr `json:"elems"`
	Src    Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *SetLit) Span() Span { return n.Src }
func (n *SetLit) exprNode()  {}

type BinOp struct {
	Op string `json:"op"`
	L  Expr   `json:"left"`
	R  Expr   `json:"right"`
	Src Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *BinOp) Span() Span { return n.Src }
func (n *BinOp) exprNode()  {}

type UnOp struct {
	Op string `json:"op"`
	X  Expr   `json:"x"`
	Src Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *UnOp) Span() Span { return n.Src }
func (n *UnOp) exprNode()  {}

// CondExpr is a ternary conditional expression `then if cond else otherwise`.
type CondExpr struct {
	If   Expr `json:"if"`   // value when cond is truthy
	Cond Expr `json:"cond"` // condition
	Else Expr `json:"else"` // value when cond is falsy
	Src   Span `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *CondExpr) Span() Span { return n.Src }
func (n *CondExpr) exprNode()  {}

type Call struct {
	Fn   Expr   `json:"fn"`
	Args []Expr `json:"args"`
	Src   Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Call) Span() Span { return n.Src }
func (n *Call) exprNode()  {}

// KeywordArg is a `name = value` argument inside a call: the function
// parameter `name` is bound to the evaluated `value`.
type KeywordArg struct {
	Name  string `json:"name"`
	Value Expr   `json:"value"`
	Src    Span   `json:"span,omitempty"`
}

func (n *KeywordArg) Span() Span { return n.Src }
func (n *KeywordArg) exprNode()  {}

type Index struct {
	Obj Expr `json:"obj"`
	Idx Expr `json:"index"`
	Src  Span `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Index) Span() Span { return n.Src }
func (n *Index) exprNode()  {}

// Slice is a sequence slice expression `s[a:b]`, `s[a:b:c]`, `s[::step]`.
// Low/High/Step are nil when the corresponding bound is absent (`s[:b]`).
type Slice struct {
	Obj  Expr `json:"obj"`
	Low  Expr `json:"low,omitempty"`
	High Expr `json:"high,omitempty"`
	Step Expr `json:"step,omitempty"`
	Src   Span `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Slice) Span() Span { return n.Src }
func (n *Slice) exprNode()  {}

type Attr struct {
	Obj  Expr  `json:"obj"`
	Name *Name `json:"name"`
	Src   Span  `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Attr) Span() Span { return n.Src }
func (n *Attr) exprNode()  {}

type Lambda struct {
	Params []*Param `json:"params"`
	Body   Expr     `json:"body"`
	Src     Span     `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Lambda) Span() Span { return n.Src }
func (n *Lambda) exprNode()  {}

// CompKind distinguishes list/dict/set/generator comprehensions.
type CompKind int

const (
	CompList CompKind = iota
	CompDict
	CompSet
	CompGenerator
)

type Comp struct {
	Kind   CompKind `json:"kind"`
	Elems  []Expr   `json:"elems"`          // for list/set
	Keys   []Expr   `json:"keys,omitempty"` // for dict
	Vals   []Expr   `json:"vals,omitempty"`
	ForVar *Name    `json:"for_var"`
	Iter   Expr     `json:"iter"`
	Cond   Expr     `json:"cond,omitempty"`
	Src     Span     `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Comp) Span() Span { return n.Src }
func (n *Comp) exprNode()  {}

// Generator is a generator expression `(x for x in iter)`.
type Generator struct {
	Elems  []Expr `json:"elems"`
	ForVar *Name  `json:"for_var"`
	Iter   Expr   `json:"iter"`
	Cond   Expr   `json:"cond,omitempty"`
	Src     Span   `json:"span,omitempty"`
	Ty    string `json:"inferred,omitempty"`
}

func (n *Generator) Span() Span { return n.Src }
func (n *Generator) exprNode()  {}

type BreakStmt struct {
	Src Span `json:"span,omitempty"`
}
type PassStmt struct {
	Src Span `json:"span,omitempty"`
}

func (n *BreakStmt) Span() Span     { return n.Src }
func (n *BreakStmt) stmtNode()      {}
func (n *BreakStmt) SetSpan(s Span) { n.Src = s }

type ContinueStmt struct {
	Src Span `json:"span,omitempty"`
}

func (n *PassStmt) Span() Span     { return n.Src }
func (n *PassStmt) SetSpan(s Span) { n.Src = s }

func (n *PassStmt) stmtNode() {}

func (n *ContinueStmt) Span() Span     { return n.Src }
func (n *ContinueStmt) stmtNode()      {}
func (n *ContinueStmt) SetSpan(s Span) { n.Src = s }

