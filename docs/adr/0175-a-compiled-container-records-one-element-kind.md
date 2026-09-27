# A compiled container records one element kind

**Status:** accepted
**Date:** 2026-07-29
**Implements:** roadmap Gap J.6

## Context

Gaps I.2 and J.5 put strings inside runtime containers and across function boundaries, but three
shapes were still wrong, all discovered by probing rather than by tests:

- `d = {"a": 1}` — a *dict literal* with string contents — was rejected up front by
  `dictLiteralKeys`/`dictLiteralVals`, which only accept constant integers, even though the heap
  dict lowering (which can intern) existed right below it. `print({"a"})` hit the same wall in
  `setLiteralElems`.
- `print({"a": 1})` printed `0`: the print path lowered the literal to a heap handle and then
  printed the handle with `%d`.
- `print([1, "a"])` printed `[(null), 'a']`. This was the interesting one: not a crash, not a
  verifier rejection, but a **plausible-looking wrong answer**. A container's slots are i32 words
  and its element kind is recorded once per container (once per dict side), so the printer applied
  the string table to every element — the integer `1` became "index 1", out of range.

## Decision

**Literals intern; containers are homogeneous; growth that breaks homogeneity is a diagnostic.**

1. `literalNeedsHeap` decides between the two lowerings. A container literal whose elements are
   all constant integers keeps the compile-time global struct (`{i32 count, [n x i32] …}`) that
   the folding paths index directly. If any element is a string, the literal is built as a **heap
   object** by `heapListFrom` / `heapDictFrom` / `heapSetFrom`, which intern through
   `heapElemKind` and stamp the object's flags with `rt_mark_estr`. The static
   `dictLiteralKeys`/`dictLiteralVals`/`setLiteralElems` checks remain for the paths that really
   do need constant ints, and no longer stand in front of the ones that do not.
2. Printing a bare container literal goes to the runtime printers (`rt_print_list`,
   `rt_set_print`, `rt_dict_print`), never to `%d`. The printers consult the object's own flags —
   established in ADR 0174 — so a literal prints the same as the same contents in a variable.
3. **One element kind per position.** The per-scope maps gained their number-tracking twins
   (`listElemInt`, `setElemInt`, `dictKeyInt`, `dictValInt`, saved and restored in `beginScope`
   like every other per-scope fact), and `recordElemKind` refuses a container that has already
   held the other kind in that position:

   ```
   codegen: a compiled list holds either strings or numbers, not both; the interpreter allows
   mixing — a compiled container records one element kind, so heterogeneous contents need
   per-element tagging (roadmap Gap J.6)
   ```

4. **Replacement is not growth.** `xs[0] = "s"` *overwrites* a slot, so `replaceElemKind` moves
   the recorded kind to the new element's instead of colliding with the old one: `xs = [1]` then
   `xs[0] = "s"` leaves a list holding one string, and prints `['s']` — the interpreter's answer.
   `append`/`add` grow, and mixing there is refused.

## Codegen / IR implications

- New helpers in `pkg/lang/heapargs.go`: `heapDictFrom`, `heapSetFrom` (alongside
  `heapListFrom`), `literalNeedsHeap`, `literalMixedKinds`, `recordElemKind`,
  `replaceElemKind`, `mixedKindErr`.
- `heapListFrom` now stamps `rt_mark_estr` itself, so a list built by any caller carries its
  element kind on the object; the dict/set builders do the same for their two/one sides.
- `value()` for `*ListLit`/`*DictLit`/`*SetLit` branches on `literalNeedsHeap`, and the print
  dispatch gained the same branch, choosing the runtime printer.
- `len({"a": 1})` measures the heap object with `rt_dict_len`/`rt_set_len` rather than loading the
  static global's count field.
- No new runtime functions were needed: ADR 0174's `@estr` flags and `rt_print_value` already
  handle the rendering; this cycle only stopped the paths that bypassed them.

## Alternatives rejected

- **Tag every element** (pointer | small-int | other), making `[1, "a"]` work like Python. That is
  the general answer and the right one if heterogeneous containers prove important; it changes the
  representation of every container access for a case that is rare in real code and untestable
  against our own benchmarks. Refusing is the honest interim state, because the alternative on
  offer was not "works" but "prints `(null)`".
- **Intern integers into the same table** so every slot is an index. Numbers would then pay a table
  lookup and lose the arithmetic that reads container slots directly, for the sake of a printing
  rule.
- **Print elements as ints when their index is out of range.** A heuristic that would turn
  `[(null), 'a']` into `[1, 'a']` — and then silently misrender a real string whose index happens
  to be small. Guessing at print time is how the bug happened in the first place.
- **Refuse mixed containers including item assignment.** Rejected after the first test run:
  `xs = [1]; xs[0] = "s"` is Gap I.2's own conformance case, and the interpreter prints `['s']`.
  The distinction that matters is growth versus replacement.

## Verification

`integration/string_containers_test.go` gained `TestContainerLiteralsMatchPython` (10 literal
shapes × both backends × CPython's output), `TestMixedContainersAreADiagnosticNotAMisprint`
(6 shapes that must refuse, asserting the message names the interpreter), and
`TestItemAssignmentReplacesElementKind`. `pkg/lang/string_containers_test.go` gained
`TestStringLiteralsBuildHeapContainers` (interning + `rt_mark_estr` + no `i32 @.str` + verifier)
and `TestLiteralNeedsHeapAndMixedKinds`, which pins the two predicates themselves so a future
change is caught where the decision lives.
