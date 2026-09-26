package lang

import (
	"encoding/json"
	"strings"
)

// ABIVersion is the version of the gusty C ABI for extern fn exports. The
// generated IR carries @gusty_abi_version = ABIVersion so consumers can check
// compatibility before linking. Bump this whenever the struct layouts in
// %gusty_value / %gusty_union change incompatibly; keep the tag words fixed.
const ABIVersion = 1

// Stable tag words for the tagged-value ABI (%gusty_value = {i32, i32}).
// These mirror the interpreter tag table and MUST NOT be renumbered across
// releases: the ABI struct layout is keyed to them (see docs/abi.md).
const (
	ABITagInt      = 0
	ABITagFloat    = 1
	ABITagBool     = 2
	ABITagNone     = 3
	ABITagStr      = 4
	ABITagList     = 5
	ABITagDict     = 6
	ABITagSet      = 7
	ABITagTuple    = 8
	ABITagClass    = 9
	ABITagInstance = 10
	ABITagMethod   = 11
	ABITagClosure  = 12
	ABITagExn      = 13
	ABITagModule   = 14
)

// ABIValue mirrors `%gusty_value = type {i32, i32}` — a tagged value as a
// (tag word, payload word) pair. Layout is stable: offset 0 = tag, offset 1 =
// payload. This is the narrow ABI struct for integer/boolean/None exports.
type ABIValue struct {
	Tag     int32 `json:"tag"`
	Payload int32 `json:"payload"`
}

// ABIUnion mirrors `%gusty_union = type {i32, i32, double, i8*}` — the widened
// tagged union used by extern exports that pass floats (payload bit pattern)
// or string literals (i8* pointer). Layout is stable: offset 0 = tag, offset 1
// = i32 word, offset 2 = f64 word, offset 3 = i8* word.
type ABIUnion struct {
	Tag int32   `json:"tag"`
	I   int32   `json:"i"`
	F   float64 `json:"f"`
	S   string  `json:"s"`
}

// abiSchema is the machine-readable description of the extern-fn ABI.
type abiSchema struct {
	ABI     string            `json:"abi"`
	Version int               `json:"version"`
	Value   abiValueLayout    `json:"gusty_value"`
	Union   abiUnionLayout    `json:"gusty_union"`
	Tags    map[string]int    `json:"tags"`
	Extern  abiExternRules    `json:"extern_fn_marshalling"`
	IR      map[string]string `json:"ir_markers"`
	Stable  []string          `json:"stability_contract"`
}

type abiValueLayout struct {
	Type    string `json:"type"`
	Tag     int    `json:"tag_offset"`
	Payload int    `json:"payload_offset"`
	Words   int    `json:"words"`
}

type abiUnionLayout struct {
	Type string `json:"type"`
	Tag  int    `json:"tag_offset"`
	I    int    `json:"i32_offset"`
	F    int    `json:"f64_offset"`
	S    int    `json:"i8p_offset"`
}

type abiExternRules struct {
	Ints    string `json:"ints"`
	Strings string `json:"strings"`
	Tagged  string `json:"tagged_exports"`
	Version string `json:"version_check"`
}

// ABISchema returns a machine-readable JSON document describing the gusty ABI
// for extern fn exports: version, struct layouts, tag words, marshalling rules,
// and the emitted IR markers. Consumers can validate against it at build time.
func ABISchema() (string, error) {
	s := abiSchema{
		ABI:     "gusty-extern-abi",
		Version: ABIVersion,
		Value: abiValueLayout{
			Type:    "{i32, i32}",
			Tag:     0,
			Payload: 1,
			Words:   2,
		},
		Union: abiUnionLayout{
			Type: "{i32, i32, double, i8*}",
			Tag:  0,
			I:    1,
			F:    2,
			S:    3,
		},
		Tags: map[string]int{
			"int": ABITagInt, "float": ABITagFloat, "bool": ABITagBool,
			"none": ABITagNone, "str": ABITagStr, "list": ABITagList,
			"dict": ABITagDict, "set": ABITagSet, "tuple": ABITagTuple,
			"class": ABITagClass, "instance": ABITagInstance,
			"method": ABITagMethod, "closure": ABITagClosure,
			"exn": ABITagExn, "module": ABITagModule,
		},
		Extern: abiExternRules{
			Ints:    "plain i32",
			Strings: "i8* literal pointer",
			Tagged:  "%gusty_value (tag, payload)",
			Version: "@gusty_abi_version",
		},
		IR: map[string]string{
			"value":   "%gusty_value = type {i32, i32}",
			"union":   "%gusty_union = type {i32, i32, double, i8*}",
			"version": "@gusty_abi_version = internal constant i32 1",
		},
		Stable: []string{
			"tag words are fixed across releases",
			"gusty_value layout is {i32 tag, i32 payload}",
			"gusty_union layout is {i32 tag, i32 word, double f, i8* s}",
			"extern ints marshal as plain i32",
			"extern strings marshal as i8* literal pointers",
			"tagged exports marshal through %gusty_value",
		},
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// abiIR is the versioned, stable ABI prelude emitted into every generated
// module: the named struct types for the tagged value and widened union, plus
// the ABI version marker global. Consumers check @gusty_abi_version before
// linking against extern exports.
const abiIR = `; gusty extern-fn ABI v1 — stable layout for tagged/union exports
%gusty_value = type {i32, i32}
%gusty_union = type {i32, i32, double, i8*}
@gusty_abi_version = internal constant i32 1
`

// EmitABI writes the versioned ABI prelude into the given declarations
// builder. It is idempotent (call once per module).
func EmitABI(decls *string) {
	if strings.Contains(*decls, "@gusty_abi_version") {
		return
	}
	*decls += abiIR
}
