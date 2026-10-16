package lang

// dict_key_pair.go — which keys a dict read may be asked *with*, now that the answer carries its kind
// (roadmap Gap R.221, ADR 0353).
//
// A dict whose values describe themselves — the builder wrote a tag beside every value, ADR 0232's
// `rt_mark_estr(…, 8)` — is read by one door: `rt_dict_get_tagged` hands back the entry's payload and
// `rt_dict_value_tag` the kind the writer left beside it, and the printer, the binder and the
// `and`/`or` doors dispatch on that tag. ADR 0327 opened this road for a dict *literal* and ADR 0233
// had already opened it for a float *key*; what stayed shut was the key. `mixedDictIndexRead` kept a
// list of key spellings, and a `None`, `True` or `False` was not on it, so those three reads fell out
// of the pair door and back onto the one-word road — which hands a payload to the number printer.
// `d = {None: 3.5}` / `print(d[None])` printed `3` (a `@float_box` handle), `d = {None: "hi"}` printed
// `0` (a text's `@str_tab` index) and `d = {None: [2]}` printed the slot number, all of them at exit 0
// with nothing raised. The key was never the hard part: `None`, `True` and `False` each have a tag
// (`TagNone`, `TagBool`) and a payload (`0`, `1`, `0`), which is exactly what the door already asks
// `taggedOperand` for. The gate had simply never been told.
//
// One rule keeps this a door and not a broom: the gate asks only whether **the key** can travel as a
// (payload, tag) pair. Whether the *value* can is decided by `mixedDicts` — the notebook the writers
// keep — and whether a **position** can carry the pair is decided by the door that asks (print, a
// binding, `and`/`or`). A key this pass cannot tag (a tuple, a container) stays where it was: refused
// by the tag rules, naming the key it could not type, rather than hashed by a guessed tag — which is
// the claim ADR 0327's untypable-key row pins and this file does not touch.
//
// The gate is a pure function of the AST, asked at emit time and asserted as a table in
// `dict_none_bool_key_test.go`: a door that only ever answers `true` to its own test is not a door.

// dictKeyCarriesPair reports whether a dict subscript's key reaches the dict doors as a
// (payload, tag) pair — the question `mixedDictIndexRead` asks before it hands a read to
// `mixedDictPair`. Every kind below has a tag in the runtime's table and a payload that means
// something only beside it; a key whose kind has no tag has no arm here.
func dictKeyCarriesPair(e Expr) bool {
	switch e.(type) {
	case *Name:
		// A name is the pair its last binding left (ADR 0172's latest-binding rule, ADR 0267's
		// tagged variable). A name whose binding *was* a None or a bool is the half still owed —
		// roadmap Gap R.286 measures what it answers today.
		return true
	case *IntLit, *StrLit:
		return true
	case *FloatLit:
		// A float key's payload is a box handle, so the untagged read compares handles and answers
		// whichever entry happened to hold the same little integer. The tagged read is the only one
		// that can find a float key at all (roadmap L11.1, ADR 0233).
		return true
	case *NoneLit, *BoolLit:
		// The row this file exists for. A None and a bool both have payload 0/1 and a tag of their
		// own; the one-word road read the payload alone, so `d[None]` printed the allocator's slot
		// number or the interned text's index whatever the entry held (roadmap Gap R.221, ADR 0353).
		// A bool is also the number the reference says it is — `{0: "a"}[False]` is `a` — and that
		// is the runtime's key-equality rule, not this gate's: it lives in the payload-and-tag
		// comparison behind `rt_dict_get_tagged`, which ADR 0328 left correct.
		return true
	case *ListLit:
		// The immutable spelling is a key the language hashes, and a dict holding one has its slots
		// self-describing (a container key is a container slot: ADR 0232's promotion), which is exactly
		// the state in which the one-word read prints the entry's @str_tab index as a number. The pair
		// road is the only sound read of such an entry, so the gate must say yes here — and only for a
		// tuple the door can build: every element a kind the pair table labels, no container inside
		// (roadmap L11.3, ADR 0361).
		if ln, isList := e.(*ListLit); isList && ln.IsTuple() {
			return tupleKeyCarriesPair(ln)
		}
	}
	return false
}

// tupleKeyCarriesPair is the walk that decides whether the door can build this key's pair: a tuple of
// scalars and tuples-of-scalars, which is what `rt_dict_put_tagged` wrote the entry with and what
// `rt_container_eq` compares it back against (roadmap L11.3, ADR 0361).
func tupleKeyCarriesPair(ln *ListLit) bool {
	if !ln.IsTuple() {
		return false
	}
	for _, el := range ln.Elems {
		if inner, isList := el.(*ListLit); isList {
			if !tupleKeyCarriesPair(inner) {
				return false
			}
			continue
		}
		if !dictKeyCarriesPair(el) {
			return false
		}
	}
	return true
}
