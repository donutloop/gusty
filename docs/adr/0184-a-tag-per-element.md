# ADR 0184: A tag per element — heterogeneous lists compile, print, and refuse what they cannot read

Status: Accepted (roadmap L11.1, step 1)
Date: 2026-08-04
Decides: the compiled container's per-element tag storage (`@heap_tags`), which element kinds a
mixed list may hold, and which operations on a mixed list must still refuse.
Partially supersedes **ADR 0175** (mixed containers are a diagnostic, not a misprint): for list
*literals* the diagnostic retires; for growth (`append`/`add`), dicts, sets, and every element
*read* it stands.

## Context

`@estr[h]` is one i32 per heap object. When it says "string", `rt_print_list` renders *every*
element through the string table. That single bit is why the language had a rule instead of a
feature:

```python
xs = [1, "a"]      # ADR 0175: refused — "a compiled list holds either strings or numbers"
print(xs)          # before that: (null) for the integer, because %s was applied to 1
```

ADR 0175's own text said the refusal was "a property of the compiled representation, not the
language" and that the fix is "a tagged value word per element … the L11.1 change". L11.1
started with ADR 0182 (one tag table, the heap's `kind` as a projection of it); this is the
second step, where those tags get stored per element.

## Decision

1. **`@heap_tags : [1024 x [256 x i32]]`, parallel to the element slots**, indexed by
   `(handle, index)`. Parallel rather than a fourth struct field on purpose: the object's
   `{kind, len, elems}` layout is addressed by dozens of existing GEPs, and a parallel array
   adds per-element tags without touching one of them. The values are the canonical `ValueTag`
   numbers from ADR 0182 — `int=0`, `None=3`, `str=4` — so no new vocabulary appears; `0` is
   both the zero value and "integer", which is why no initialisation pass is needed.
2. **The tag decides rendering, per element.** `rt_print_list_mixed` is `rt_print_list` with the
   container-wide `@estr[h]` load replaced by a per-slot tag load, calling
   `rt_print_mixed_value`, which renders numbers with `%d`, interned strings through the
   repr slot (Python quotes elements inside a container), and `None` as `None`. The result is
   byte-identical to CPython for `[1, 'a', None, 2]`.
3. **Only kinds the tag can honestly describe are allowed in a mixed list.** `elemKindTag`
   refuses: **bools** (not values in either backend yet, so `[True, "a"]` would print `1`
   against Python's `True` — L11.1's step 2), **floats** (`rt_print_mixed_value` has no float
   rendering — Gap J.6's other half), **containers** (the collector does not mark elements, so a
   nested heap object could be freed under a list referencing it), and **any expression whose
   string-ness codegen cannot prove** — printing an interned string's *index* as a number is the
   exact wrong-output bug the original refusal existed to prevent. A string is proven by a
   compile-time fold, `printsAsInternedStr` (interned variable, string-returning call, element of
   a string container) or a known `strVals` entry; last assignment wins, so `y = 1; y = "s"`
   tags `y` as a string, matching Python.
4. **Everything that reads an element out still refuses, with a message that says what works.**
   `xs[0]`, `for x in xs`, and `xs.append(...)` over a mixed list report "printing it works, but
   reading one element out needs a tagged value at the use site". This is the load-bearing
   safety rule: the tag exists in the object, but the *use site* was compiled against one static
   kind, and silently reading an index as a number is what we refuse to do.

## Why the memory story is safe today

Mixed lists hold integers and interned-string indices, and both are immediate or permanent: no
GC-managed object is reachable only through a mixed list's elements, so the collector's inability
to scan elements is not a hazard. That argument is exactly why containers are excluded — the
moment `[[1], "a"]` is allowed, `rt_gc` needs an element worklist. A 150-iteration loop that
rebinds a mixed list is tested to actually collect (`freed=148`) while printing correctly.

## Verification

- `pkg/lang/mixed_list_test.go`: the element-kind decision table (including every excluded kind
  with its reason), the "must actually mix" rule, the emitted tag writes
  (`rt_tag_elem(..., i32 1, i32 4)` for slot 1 holding a string), and that each element-wise use
  refuses with a front-end diagnostic rather than an IR failure.
- `integration/mixed_list_test.go`: interpreter + JIT against CPython text for literals, loop
  bodies, string variables, rebound variables and string-returning calls; plus the collection
  test above.
- `integration/string_containers_test.go` was *changed in this commit*: its first two rows —
  `print([1, "a"])` and `xs = [1, "a"]` — asserted a refusal, which was correct under ADR 0175
  and is now the thing ADR 0184 fixed. They moved to a print-correctly test; the append/set/dict
  rows still assert refusal, since those are the paths where the tag is not written.
- Load-bearing, checked by stubbing: deleting the `rt_tag_elem` emission makes the compiled
  output `[1, 0, 0, 2]` and both suites fail; accepting `BoolLit` in `elemKindTag` fails the
  unit table. A test that only asserted "it compiles" would have passed through the first stub.

## Alternatives rejected

- **A fourth struct field** (`{kind, len, elems, tags}`): one object, better locality, but every
  existing GEP into `elems` is rewritten in the same commit that adds a feature — the riskier
  ordering for a change whose value is behavioural, not structural.
- **One byte tag in the element word** (NaN-boxing style): that *is* the endgame (L11.1's tagged
  value word) but it changes the representation of every scalar in the runtime at once; a
  parallel array is the incremental step that lets printing and reading be decided per site.
- **Allow heterogeneous containers and mark them in `rt_gc`**: rejected until the collector gains
  an element worklist; shipping it without would trade a wrong-output bug for a use-after-free.
- **Let `xs[0]` of a mixed list return the raw value**: rejected — it would print the *index* of
  a string in any context the codegen guessed was numeric, the original bug in a new place.
