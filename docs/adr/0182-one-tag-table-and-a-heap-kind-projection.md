# ADR 0182: One tag table, and the compiled heap's kind is a projection of it

Status: Accepted (L11.1, first step)
Date: 2026-08-04
Decides: the canonical dynamic-kind numbers (`ValueTag`), the compiled heap's object-header
`kind` word (`HeapKind*`), the extern-fn ABI's tag word, and the names/diagnostics derived
from them. Partially supersedes nothing; it is the foundation ADR 0183 (per-element tags,
retiring ADR 0175's refusal) will build on.

## Context

Three vocabularies claimed to answer "what kind of value is this":

| vocabulary | list | dict | set | instance | who reads it |
|---|---|---|---|---|---|
| `ValueTag` (`pkg/lang/value.go`) | 5 | 6 | 7 | 10 | the interpreter's heap objects (`obj.tag()`), the compiled `%obj` FFI values, `rt_obj_is` |
| ABI tags (`pkg/lang/abi.go`) | 5 | 6 | 7 | 10 | exported `extern "C"` functions, `--abi` |
| heap `kind` (`pkg/lang/heapargs.go`) | 1 | 2 | 3 | 4 | `rt_alloc`'s parameter, `rt_gc`'s dispatch, `rt_inst_get`, `HeapKindName` |

The first two agreed, and `abi_test.go` checked the list but not against `ValueTag`. The
third was unrelated on purpose (it numbers only what the compiled heap allocates) but
*unrelated by accident*: its numbers were written into IR as literals —
`%h = call i32 @rt_alloc(i32 1)` at a dozen codegen sites — and `HeapKindName` spelled the
names out a second time. Nothing tied `list = 1` to `TagList = 5`, so:

- a diagnostic could name a kind one way in `--lang` and another way in an error;
- the collector's `kinds=` field (L7.2) counted the heap numbering while everything else
  reported the tag numbering;
- and L11.1's actual work — one tag word per *element*, so `xs = [1, "a"]` stops needing
  ADR 0175's refusal — had no number to put in that word that both backends would read the
  same way.

`Roadmap L11.1` names the prerequisite exactly: "single source of truth for the tag set
(the `%obj` tags already in `pkg/lang/codegen.go` + the interpreter's obj kinds), mirror the
single-source-of-truth rule of `predeclared.go` / `exceptions.go`."

## Decision

**`ValueTag` is the one table.** The heap's `kind` word becomes a declared projection of it,
and every name and derived number reads from that table.

1. `heapKindOrder = []ValueTag{TagList, TagDict, TagSet, TagInstance}` is the projection:
   heap kind `i+1` is the tag `heapKindOrder[i]`. `HeapKindFor(tag) int32` and
   `HeapTagFor(kind) ValueTag` translate both ways; `HeapKindNone` (0) means "the compiled
   backend represents this without allocating" — ints, floats, bools, `None`, interned
   strings, closures, exceptions and modules.
2. The numbers themselves stay **constants** (`HeapKindList = 1` …), because they are
   emitted into IR and are therefore a wire format; a runtime function cannot be a `case`
   label either. The comment at the declaration says so, and `TestValueTagTableIsPinned`
   pins the canonical list (names *and* numbers) so renumbering has to be a decision.
3. **Names come from the table.** `HeapKindNameOf` (and `heapargs`' `int`-typed
   `HeapKindName` wrapper) delegate to `kindForTag`, so there is no second list of kind
   strings.
4. **Codegen uses the names, not the numbers.** The literal `rt_alloc(i32 1/2/3/4)` call
   sites now interpolate `HeapKindList/Dict/Set/Instance`. The one remaining literal is
   inside the runtime IR text, where it is a constant string — and
   `TestInstanceKindsInRuntimeAndCodegenAgree` asserts that text and the named constant
   agree, which is the check that made leaving it acceptable.
5. **Machine path.** `gustyc --lang` prints both tables, generated from the code
   (`values: int=0 float=1 … ` / `heap kinds …: list dict set instance`), and the JSON
   schema gains a `definitions.valueTag` describing the tag numbering and the projection.
   An agent can therefore target a tag without reading Go, and a test compares the schema's
   quoted list with the table.

## What this does *not* claim

- **No behaviour changes.** No program prints differently, no IR differs except that the
  kind numbers now arrive through named constants.
- **Bools are still not values.** `--json`'s `type` for `True` reports `int` on both
  backends, which is why `print(True)` prints `1` and why L11.2 (`str()` vs `repr()`) is
  gated on this item rather than being an independent fix: printing `True` needs a tag to
  print *from*.
- The per-element tag word, the retirement of ADR 0175's mixed-container refusal, and the
  `@estr[h]` per-object string flag are the next steps, listed in the roadmap.

## Consequences

- Two tests now fail if anyone renumbers a tag, reorders `heapKindOrder`, or introduces a
  new `rt_alloc` kind by hand — verified by stubbing: setting `HeapKindSet = 5` fails
  `TestEmittedHeapKindsComeFromTheTable` ("codegen allocated an unknown heap kind 5"), and
  swapping dict/set in the projection fails `TestHeapKindProjectionRoundTrips`.
- `HeapKindNone` is a real answer, not a zero value: `HeapKindFor(TagStr) == 0` is how the
  runtime says "strings are interned, not heap objects", which the ADR 0173/0174 string
  machinery relies on and which a future tagged-word step must keep true or explain.
- The projection has a deliberate asymmetry: it exists because the compiled heap allocates
  a subset. If a kind moves onto the compiled heap (tuples, L11.3, and generators already do
  in the AOT path), it is added to `heapKindOrder` in one place and the names, tags and
  reports follow.

## Alternatives rejected

- **Make the heap kind *be* the tag number** (`rt_alloc(i32 5)` for a list). Fewer numbers,
  but every existing module and the collector's `kinds=` history changes meaning for no
  runtime gain, and the heap's numbering is also used as a dense index by codegen paths
  (`kind %d` switches) that would become sparse.
- **A single table with both numbers as fields** (`{tag, heapKind}` per kind). Equivalent
  data, but it makes the *set of heap-allocated kinds* implicit again — the projection
  order is what encodes "the compiled heap allocates exactly these four today".
- **Derive the ABI tags from `ValueTag` by cast at each site.** They already match
  numerically; `TestABITagsAgreeWithCanonicalTags` now enforces it, which is the same
  guarantee without touching the exported ABI's definition.
- **Leave the literals in codegen and only add tests.** Tests alone leave the next reader
  to discover what `i32 3` means at 8,000 lines.
