package lang

// Kind enumerates the language type kinds.
type Kind int

const (
	KindDynamic Kind = iota // any / untyped (gradual typing fallback)
	KindInt
	KindFloat
	KindBool
	KindString
	KindNone
	KindList
	KindDict
	KindSet
	KindTuple
	KindFunc
	KindClass
	KindIterator
	KindVoid
	// structural protocol kinds (generics / protocols)
	KindSequence // Sequence[T] — accepts any indexable sequence of T
	KindCallable // Callable[[...], R] — accepts any function with matching signature
)

// Type is a value type. Gradual typing: KindDynamic means "any".
type Type struct {
	Kind   Kind   `json:"kind"`
	Elem   *Type  `json:"elem,omitempty"`   // list/set element
	Key    *Type  `json:"key,omitempty"`    // dict key
	Val    *Type  `json:"val,omitempty"`    // dict value
	Params []*Type `json:"params,omitempty"` // function params
	Ret    *Type  `json:"ret,omitempty"`    // function return
	ClassName string `json:"class_name,omitempty"` // class/type name
	Elems []*Type `json:"elems,omitempty"` // tuple element types
}

// singleton constructors
func TInt() *Type      { return &Type{Kind: KindInt} }
func TFlt() *Type      { return &Type{Kind: KindFloat} }
func TBool() *Type     { return &Type{Kind: KindBool} }
func TStr() *Type      { return &Type{Kind: KindString} }
func TNone() *Type     { return &Type{Kind: KindNone} }
func TDyn() *Type      { return &Type{Kind: KindDynamic} }
func TSequence(e *Type) *Type { return &Type{Kind: KindSequence, Elem: e} }
func TCallable(params []*Type, ret *Type) *Type {
	return &Type{Kind: KindCallable, Params: params, Ret: ret}
}

func TTuple(elems ...*Type) *Type { return &Type{Kind: KindTuple, Elems: elems} }
func TVoid() *Type     { return &Type{Kind: KindVoid} }
func TList(e *Type) *Type  { return &Type{Kind: KindList, Elem: e} }
func TDict(k, v *Type) *Type { return &Type{Kind: KindDict, Key: k, Val: v} }
func TSet(e *Type) *Type    { return &Type{Kind: KindSet, Elem: e} }
func TFunc(params []*Type, ret *Type) *Type {
	return &Type{Kind: KindFunc, Params: params, Ret: ret}
}
func TIter(e *Type) *Type { return &Type{Kind: KindIterator, Elem: e} }

func (t *Type) IsInt() bool     { return t != nil && t.Kind == KindInt }
func (t *Type) IsFloat() bool   { return t != nil && t.Kind == KindFloat }
func (t *Type) IsBool() bool    { return t != nil && t.Kind == KindBool }
func (t *Type) IsString() bool  { return t != nil && t.Kind == KindString }
func (t *Type) IsNone() bool    { return t != nil && t.Kind == KindNone }
func (t *Type) IsDyn() bool     { return t == nil || t.Kind == KindDynamic }
func (t *Type) IsNum() bool     { return t != nil && (t.Kind == KindInt || t.Kind == KindFloat) }

// Name renders a human-readable type name.
func (t *Type) Name() string {
	if t == nil {
		return "any"
	}
	switch t.Kind {
	case KindDynamic:
		return "any"
	case KindInt:
		return "int"
	case KindFloat:
		return "float"
	case KindBool:
		return "bool"
	case KindString:
		return "str"
	case KindNone:
		return "None"
	case KindList:
		return "list[" + t.Elem.Name() + "]"
	case KindTuple:
		return "tuple[" + tupleElemNames(t.Elems) + "]"
	case KindDict:
		return "dict[" + t.Key.Name() + ", " + t.Val.Name() + "]"
	case KindSet:
		return "set[" + t.Elem.Name() + "]"
	case KindFunc:
		return "fn"
	case KindClass:
		return "class:" + t.ClassName
	case KindIterator:
		return "iter[" + t.Elem.Name() + "]"
	case KindVoid:
		return "void"
	case KindSequence:
		return "Sequence[" + t.Elem.Name() + "]"
	case KindCallable:
		return "Callable[[" + callableParamNames(t.Params) + "], " + t.Ret.Name() + "]"
	}
	return "any"
}

// callableParamNames renders a Callable's parameter list as comma-separated
// type names for Name().
func callableParamNames(params []*Type) string {
	out := ""
	for i, p := range params {
		if i > 0 { out += ", " }
		if p == nil {
			out += "any"
			continue
		}
		out += p.Name()
	}
	return out
}

// Same reports whether two types are structurally identical.
func tupleElemNames(elems []*Type) string {
	out := ""
	for i, e := range elems {
		if i > 0 { out += ", " }
		out += e.Name()
	}
	return out
}

func (t *Type) Same(o *Type) bool {
	if t == nil || o == nil {
		return t == o
	}
	if t.Kind != o.Kind {
		return false
	}
	switch t.Kind {
	case KindList:
		return t.Elem.Same(o.Elem)
	case KindTuple:
		if len(t.Elems) != len(o.Elems) { return false }
		for i := range t.Elems { if !t.Elems[i].Same(o.Elems[i]) { return false } }
		return true
	case KindDict:
		return t.Key.Same(o.Key) && t.Val.Same(o.Val)
	case KindIterator:
		return t.Elem.Same(o.Elem)
	case KindSequence:
		return o.Kind == KindSequence && (t.Elem == nil || o.Elem == nil || t.Elem.Same(o.Elem))
	case KindCallable:
		if o.Kind != KindCallable || len(t.Params) != len(o.Params) {
			return false
		}
		for i := range t.Params {
			if t.Params[i] == nil || o.Params[i] == nil {
				continue
			}
			if !t.Params[i].Same(o.Params[i]) {
				return false
			}
		}
		return t.Ret == nil || o.Ret == nil || t.Ret.Same(o.Ret)
	default:
		return true
	}
}
