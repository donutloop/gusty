# Strings in runtime containers are interned, not stored as pointers

**Status:** accepted
**Date:** 2026-07-29
**Implements:** roadmap Gap I.2

## Context

Containers in the AOT backend store i32 words, and strings in this compiler are compile-time
globals (`@.strN = private unnamed_addr constant [.. x i8] c"a\00"`). The first attempt at a
compiled list of strings therefore emitted

```
call void @rt_set_elem(i32 %h, i32 0, i32 @.str1)
```

— a global pointer where an i32 was required. `opt-20`/`llvm-as` rejected the module, and
because verification is a pipeline stage (ADR 0164) the failure surfaced as exit 2, "the
compiler produced an invalid module". Every one of those programs was valid source that the
interpreter ran correctly: `xs = ["a", "b"]` is not an exotic program.

Two refusals preceded this. First the shared `heapElem` choke point was made to reject string
elements with an actionable diagnostic (still the right rule for anything that cannot be
resolved to text). That converted a compiler bug into an honest refusal — and exposed how
large the hole was, because string lists are extremely common.

## Decision

**Intern at the store site; index in the container; two parallel tables.**

`rt_str_intern2(i8* raw, i8* repr) -> i32` appends the text to `@str_tab` if it is not already
present (compared with `strcmp`, so it is content-addressed: two separate `"k"` literals are
the same dict key) and returns its index. `@str_repr_tab[i]` holds the same string's Python
repr form. The container slot stores only the index.

Printing chooses the slot by context, not by guesswork: `rt_print_value(i32 v, i32 isStr, i32
quote)` reads `@str_tab` for a value printed raw (`print(x)` → `hello`) and `@str_repr_tab`
for an element inside a container (`print(xs)` → `['hello']`). One helper renders lists, sets
and dicts, so the three cannot drift, and the repr form is computed by the same
`pyReprString` the interpreter uses, which implements Python's quote choice — single quotes
unless the text contains `'` and no `"` (`["it's", 'plain']`).

**Element kinds are tracked per container, per slot.** `listElemStr`, `setElemStr`,
`dictKeyStr` and `dictValStr` say which positions hold interned strings, so a dict with string
keys and integer values (`{'ada': 3}`) renders correctly. They are saved and restored in
`beginScope` like every other per-scope fact, so a function's `xs` cannot describe another
function's `xs`.

**The refusal stays where the text is not statically known.** A container element whose string
value comes from a `str`-typed parameter still cannot be lowered — there is nothing to intern
at compile time — and reports it as a compile diagnostic with the pointer to the interpreter,
per ADR 0166.

## Codegen / IR implications

- `@str_count`, `@str_tab`, `@str_repr_tab`, `rt_str_intern2`, `rt_str_ptr`,
  `rt_str_repr_ptr`, `rt_print_value`, `rt_print_list_str`, `rt_set_print_str`,
  `rt_dict_print_s` are emitted with the runtime whenever a container touches a string.
- `heapElemKind(b, e) (string, bool, error)` is the one function that turns an element into
  the word to store: folded ints pass through, strings intern, anything else is the ADR 0166
  diagnostic. Every store site (append, add, setitem, list literal, dict literal, dict
  membership read, `in` needle) goes through it, so no site can reintroduce a global in an
  i32 slot.
- The interpreter's `Repr` was fixed at the same time: values *inside* a container route
  through `reprNested`, which quotes strings and renders dict keys with `Repr` too. Printing a
  dict key with `%v` used to print the heap handle — `{1048581: 1}` instead of `{'k': 1}`.
- The conformance corpus gained `string_containers.gy` (20 prints, 23 output lines) whose output was compared
  against CPython's, so a regression in either backend's rendering is caught by a number that
  is not ours.

## Alternatives rejected

- **Tag every container word** (pointer | small-int | other) so a string pointer fits directly.
  That is the general fix, and it costs a shift/extend on every container access and changes
  the representation of every value in the language. Interning buys the string case at the
  cost of one lookup and no representation change.
- **Store `@.strN` and bitcast it to i32.** A pointer-sized value in an i32 slot is not just
  unrepresentable on 64-bit targets; it also breaks when the string table is emitted in a
  different section. It makes the verifier's job harder, not the compiler's.
- **Emit the repr only** (skip the raw table). Then `print(x)` of a container element would
  print quotes where Python prints none.
- **Keep refusing and document the limit.** The interim diagnostic was right while the shape
  was broken, and stays right for values that are not statically known. It was wrong as a
  permanent answer for string literals, which are the common case in test code, benchmarks
  and CLI scripts — and this compiler is aimed at agents, for whom `["a", "b"]` is a normal
  thing to write.

## Verification

`integration/string_containers_test.go` runs 20 shapes through both backends and asserts
CPython's exact output; `pkg/lang/string_containers_test.go` checks the IR invariants (an
interned store, no `i32 @.str` anywhere in a module, the repr table emitted, the quote-choice
rule against Python's answers). `string_containers.gy` is in the conformance corpus, so the
shape is compiled and verified by `--verify` too.
