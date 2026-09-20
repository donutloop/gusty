package lang

// Node is any AST node carrying a source span.
type Node interface {
	Span() Span
}

// Program is the root AST: a list of statements.
type Program struct {
	Stmts []Stmt `json:"stmts"`
	sp    Span   `json:"-"`
}

func (p *Program) Span() Span { return p.sp }
func (p *Program) SetSpan(s Span) { p.sp = s }

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

type ReturnStmt struct{ Expr Expr `json:"expr"`; sp Span `json:"-"` }
func (n *ReturnStmt) Span() Span { return n.sp }
func (n *ReturnStmt) stmtNode()  {}
func (n *ReturnStmt) SetSpan(s Span) { n.sp = s }

type ExprStmt struct{ Expr Expr `json:"expr"`; sp Span `json:"-"` }
func (n *ExprStmt) Span() Span { return n.sp }
func (n *ExprStmt) stmtNode()  {}

type AssignStmt struct {
	Target Expr  `json:"target"`
	Value  Expr  `json:"value"`
	Annot  *Type `json:"annot,omitempty"`
	sp     Span  `json:"-"`
}


type RaiseStmt struct {
	Expr Expr
	sp   Span
}

func (n *RaiseStmt) Span() Span { return n.sp }

func (n *RaiseStmt) stmtNode() {}
func (n *AssignStmt) Span() Span { return n.sp }
func (n *AssignStmt) stmtNode()  {}

// AugAssignStmt is an augmented assignment `x op= e` (`x += e`, `x -= e`,
// `x *= e`, `x /= e`, `x //= e`, `x %= e`). Target must be a writable
// lvalue (Name or Attr); Op is one of "+", "-", "*", "/", "//", "%".
type AugAssignStmt struct {
	Target Expr  `json:"target"`
	Op     string `json:"op"`
	Value  Expr  `json:"value"`
	sp     Span  `json:"-"`
}

func (n *AugAssignStmt) Span() Span { return n.sp }
func (n *AugAssignStmt) stmtNode()  {}

type IfStmt struct {
	Cond  Expr       `json:"cond"`
	Then  []Stmt     `json:"then"`
	Elifs []*IfStmt  `json:"elifs,omitempty"`
	Else  []Stmt     `json:"else,omitempty"`
	sp    Span       `json:"-"`
}
func (n *IfStmt) Span() Span { return n.sp }
func (n *IfStmt) stmtNode()  {}

type WhileStmt struct {
	Cond Expr   `json:"cond"`
	Body []Stmt `json:"body"`
	Else []Stmt `json:"else"`
	sp   Span   `json:"-"`
}
func (n *WhileStmt) Span() Span { return n.sp }
func (n *WhileStmt) stmtNode()  {}

type ForStmt struct {
	Var  *Name  `json:"var"`
	Iter Expr   `json:"iter"`
	Body []Stmt `json:"body"`
	Else []Stmt `json:"else"`
	sp   Span   `json:"-"`
}
func (n *ForStmt) Span() Span { return n.sp }
func (n *ForStmt) stmtNode()  {}

type Param struct {
	Name    string  `json:"name"`
	Annot   *Type   `json:"annot,omitempty"`
	Default Expr    `json:"default,omitempty"`
	sp      Span    `json:"-"`
}
func (n *Param) Span() Span { return n.sp }

type FuncDef struct {
	Name       string   `json:"name"`
	Params     []*Param `json:"params"`
	ReturnAnno *Type    `json:"return_annot,omitempty"`
	Body       []Stmt   `json:"body"`
	Decorators []Expr   `json:"decorators,omitempty"`
	sp         Span     `json:"-"`
}
func (n *FuncDef) Span() Span { return n.sp }
func (n *FuncDef) stmtNode()  {}

type ClassDef struct {
	Name string   `json:"name"`
	Bases []*Name `json:"bases,omitempty"`
	Body []Stmt   `json:"body"`
	sp   Span     `json:"-"`
}
func (n *ClassDef) Span() Span { return n.sp }
func (n *ClassDef) stmtNode()  {}

type MatchCase struct {
	Pattern Expr   `json:"pattern"`
	Body    []Stmt `json:"body"`
	sp      Span   `json:"-"`
}

type MatchStmt struct {
	Subject Expr        `json:"subject"`
	Cases   []*MatchCase `json:"cases"`
	sp      Span        `json:"-"`
}
func (n *MatchStmt) Span() Span { return n.sp }
func (n *MatchStmt) stmtNode()  {}

type ExceptClause struct {
	Exn  *Name  `json:"exn,omitempty"`
	Body []Stmt `json:"body"`
	sp   Span   `json:"-"`
}

type TryStmt struct {
	Body     []Stmt          `json:"body"`
	Excepts  []*ExceptClause `json:"excepts,omitempty"`
	Finally  []Stmt          `json:"finally,omitempty"`
	sp       Span            `json:"-"`
}
func (n *TryStmt) Span() Span { return n.sp }
func (n *TryStmt) stmtNode()  {}

type ImportStmt struct {
	Module string `json:"module"`
	sp     Span   `json:"-"`
}
func (n *ImportStmt) Span() Span { return n.sp }
func (n *ImportStmt) stmtNode()  {}

type YieldStmt struct {
	Expr Expr `json:"expr"`
	sp   Span `json:"-"`
}
func (n *YieldStmt) Span() Span { return n.sp }
func (n *YieldStmt) stmtNode()  {}

// --- Expressions ---

type Name struct {
	Value string `json:"name"`
	sp    Span   `json:"-"`
}
func (n *Name) Span() Span { return n.sp }
func (n *Name) exprNode()  {}

type IntLit struct{ Value int64 `json:"value"`; sp Span `json:"-"` }
func (n *IntLit) Span() Span { return n.sp }
func (n *IntLit) exprNode()  {}

type FloatLit struct{ Value float64 `json:"value"`; sp Span `json:"-"` }
func (n *FloatLit) Span() Span { return n.sp }
func (n *FloatLit) exprNode()  {}

type BoolLit struct{ Value bool `json:"value"`; sp Span `json:"-"` }
func (n *BoolLit) Span() Span { return n.sp }
func (n *BoolLit) exprNode()  {}

type NoneLit struct{ sp Span `json:"-"` }
func (n *NoneLit) Span() Span { return n.sp }
func (n *NoneLit) exprNode()  {}

type StrLit struct{ Value string `json:"value"`; sp Span `json:"-"` }
func (n *StrLit) Span() Span { return n.sp }
func (n *StrLit) exprNode()  {}
type FString struct{ Parts []FStringPart `json:"parts"`; sp Span `json:"-"` }
func (n *FString) Span() Span { return n.sp }
func (n *FString) exprNode()  {}

// FStringPart is one segment of an interpolated format string: either a
// literal segment (Lit non-empty) or an interpolated expression (Expr set).
type FStringPart struct {
	Lit  string `json:"lit,omitempty"`
	Expr Expr   `json:"expr,omitempty"`
}


type ListLit struct{ Elems []Expr `json:"elems"`; sp Span `json:"-"` }
func (n *ListLit) Span() Span { return n.sp }
func (n *ListLit) exprNode()  {}

type DictLit struct {
	Keys []Expr `json:"keys"`
	Vals []Expr `json:"vals"`
	sp   Span   `json:"-"`
}
func (n *DictLit) Span() Span { return n.sp }
func (n *DictLit) exprNode()  {}

type SetLit struct{ Elems []Expr `json:"elems"`; sp Span `json:"-"` }
func (n *SetLit) Span() Span { return n.sp }
func (n *SetLit) exprNode()  {}

type BinOp struct {
	Op string `json:"op"`
	L  Expr   `json:"left"`
	R  Expr   `json:"right"`
	sp Span   `json:"-"`
}
func (n *BinOp) Span() Span { return n.sp }
func (n *BinOp) exprNode()  {}

type UnOp struct {
	Op string `json:"op"`
	X  Expr   `json:"x"`
	sp Span   `json:"-"`
}
func (n *UnOp) Span() Span { return n.sp }
func (n *UnOp) exprNode()  {}

// CondExpr is a ternary conditional expression `then if cond else otherwise`.
type CondExpr struct {
	If   Expr `json:"if"`   // value when cond is truthy
	Cond Expr `json:"cond"` // condition
	Else Expr `json:"else"` // value when cond is falsy
	sp   Span `json:"-"`
}
func (n *CondExpr) Span() Span { return n.sp }
func (n *CondExpr) exprNode()  {}

type Call struct {
	Fn   Expr   `json:"fn"`
	Args []Expr `json:"args"`
	sp   Span   `json:"-"`
}
func (n *Call) Span() Span { return n.sp }
func (n *Call) exprNode()  {}

// KeywordArg is a `name = value` argument inside a call: the function
// parameter `name` is bound to the evaluated `value`.
type KeywordArg struct {
	Name  string `json:"name"`
	Value Expr   `json:"value"`
	sp    Span   `json:"-"`
}
func (n *KeywordArg) Span() Span { return n.sp }
func (n *KeywordArg) exprNode()  {}

type Index struct {
	Obj Expr `json:"obj"`
	Idx Expr `json:"index"`
	sp  Span `json:"-"`
}
func (n *Index) Span() Span { return n.sp }
func (n *Index) exprNode()  {}

// Slice is a sequence slice expression `s[a:b]`, `s[a:b:c]`, `s[::step]`.
// Low/High/Step are nil when the corresponding bound is absent (`s[:b]`).
type Slice struct {
	Obj  Expr `json:"obj"`
	Low  Expr `json:"low,omitempty"`
	High Expr `json:"high,omitempty"`
	Step Expr `json:"step,omitempty"`
	sp   Span `json:"-"`
}
func (n *Slice) Span() Span { return n.sp }
func (n *Slice) exprNode()  {}

type Attr struct {
	Obj  Expr  `json:"obj"`
	Name *Name `json:"name"`
	sp   Span  `json:"-"`
}
func (n *Attr) Span() Span { return n.sp }
func (n *Attr) exprNode()  {}

type Lambda struct {
	Params []*Param `json:"params"`
	Body   Expr     `json:"body"`
	sp     Span     `json:"-"`
}
func (n *Lambda) Span() Span { return n.sp }
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
	Elems  []Expr   `json:"elems"`  // for list/set
	Keys   []Expr   `json:"keys,omitempty"` // for dict
	Vals   []Expr   `json:"vals,omitempty"`
	ForVar *Name    `json:"for_var"`
	Iter   Expr     `json:"iter"`
	Cond   Expr     `json:"cond,omitempty"`
	sp     Span     `json:"-"`
}
func (n *Comp) Span() Span { return n.sp }
func (n *Comp) exprNode()  {}

// Generator is a generator expression `(x for x in iter)`.
type Generator struct {
	Elems  []Expr `json:"elems"`
	ForVar *Name  `json:"for_var"`
	Iter   Expr   `json:"iter"`
	Cond   Expr   `json:"cond,omitempty"`
	sp     Span   `json:"-"`
}
func (n *Generator) Span() Span { return n.sp }
func (n *Generator) exprNode()  {}

type BreakStmt struct{ sp Span `json:"-"` }
type PassStmt struct{ sp Span `json:"-"` }
func (n *BreakStmt) Span() Span { return n.sp }
func (n *BreakStmt) stmtNode()  {}
func (n *BreakStmt) SetSpan(s Span) { n.sp = s }

type ContinueStmt struct{ sp Span `json:"-"` }
func (n *PassStmt) Span() Span        { return n.sp }
func (n *PassStmt) SetSpan(s Span)    { n.sp = s }




func (n *PassStmt) stmtNode() {}

func (n *ContinueStmt) Span() Span { return n.sp }
func (n *ContinueStmt) stmtNode()  {}
func (n *ContinueStmt) SetSpan(s Span) { n.sp = s }

