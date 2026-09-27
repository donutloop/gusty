# Strings cross a function boundary as interned indices

**Status:** accepted
**Date:** 2026-07-29
**Implements:** roadmap Gap J.5

## Context

`greet("ada")` compiled to `call i32 @greet(i32 @.str1)`: a global pointer in an i32 parameter.
LLVM rejected the module, and the exit-code contract reported a two-line program as a compiler
bug (exit 2). The interim fix (ADR 0166 style) was an actionable refusal naming the parameter —
correct while the shape was broken, and wrong as a permanent answer: passing a name, a message,
or a key to a helper is the single most common thing a program does with a string.

Gap I.2 had already solved the hard part. Strings live in a runtime interned table
(`@str_tab` for the text, `@str_repr_tab` for the Python repr, indices as the addressable
word), so a string is representable in an i32 slot — the question became how the compiler
*knows* that a given parameter is one.

## Decision

**Intern at the call site, pass the index, and infer which parameters are strings.**

`strArgKinds` (new, `pkg/lang/strargs.go`) answers "which parameters receive strings", mirroring
`heapArgKinds` for containers: a `str` annotation or a string default is authoritative, a call
site passing a string adds evidence, and classification runs to a fixed point in sorted order
because `def outer(s): inner(s)` must not classify by Go's map iteration order. At a call site
whose callee parameter is marked, the argument becomes
`rt_str_intern2(i8* text, i8* repr) -> i32` and the index is passed; a forwarded string (already
an index) passes straight through.

Inside the callee, a marked parameter joins `internedVars` — the same set that already covered
elements read out of a string container and loop variables over one — so `print`, `len`
(`rt_str_len`, a byte scan, not `strlen`, whose name is already taken with an `i64` signature in
the runtime), `==`/`!=` against a literal (intern makes equal text the same index, so there is
no character loop), membership needles, and container stores all reuse the existing paths.

**Three facts travel with the analysis, because one place is never enough:**

- `strReturningFuncs` — a function whose `return` yields a string. `print(echo("yo"))` must print
  text, and `xs.append(make_key())` must record an interned element; without this the caller
  printed the index.
- `stringFillingParams` — which positions of a container *parameter* the body fills with strings
  (`out.append(v)`, `s.add(v)`, `d[k] = v`). A helper mutates a container its caller created, and
  the caller's scope cannot otherwise know that `names[1]` is a string rather than an index.
- The object's own runtime flags, `@estr[h]` (bit 0 elements, bit 1 dict keys, bit 2 dict values),
  set by `rt_mark_estr` at every store site and read by the printers. This is the belt to the
  braces above: whether a container holds strings is a property of the *object*, since containers
  are mutable and get passed around, and no static map spanning a caller can be trusted for it.

**What stays refused, as a diagnostic naming the interpreter:** concatenation of a runtime string
(needs a buffer the runtime does not have), string methods on a parameter, and arithmetic or
ordering on a string — the last of which previously *compiled* and returned `index + 1`, where
the interpreter raises `TypeError`. A parameter demonstrably used as both a string and a number
(`f("a")` and `f(7)`) is refused rather than guessed: printing `7` through the string table
produced `(null)`, which is worse than a diagnostic.

## Codegen / IR implications

- New runtime IR: `@estr` + `rt_mark_estr`, `rt_str_len`, and the generic printers now consult
  the object's flags — `rt_print_list` reads them directly, `rt_set_print` renders members through
  `rt_print_value`, and `rt_dict_print` is a thin wrapper that derives the key/value flags and
  delegates to `rt_dict_print_s`. The static per-scope maps (`listElemStr`, `setElemStr`,
  `dictKeyStr`, `dictValStr`) remain for element *reads* and loop variables, where the decision is
  still needed at compile time.
- `heapASTWalker` gained a `scope` (the enclosing function name), threaded through its callbacks,
  and `heapArgKinds`' variable-kind table is keyed by `scope + name`. Until then `varKinds` was
  keyed by bare name, so `def ins(s, v): s.add(v)` marked every variable called `s` as a set
  program-wide — and `echo(s)` in an unrelated function was classified as passing a set, which
  silently vetoed `echo`'s string parameter. The same class of bug as the parameter-kind leak
  fixed in `5232142`: whole-program inference keyed by name is whole-program coupling.

## Alternatives rejected

- **Pass `i8*` parameters** and let the callee use pointers directly. Rejected in Gap I.2 for the
  same reason: container slots and parameter slots are i32 across the runtime, and mixing pointer
  and int representations is where the verifier errors came from. Interning also gives content
  equality for free.
- **Tag every value** so a string needs no analysis. A shift/extend on every access, for one type.
- **Decide stringness inside the callee by looking at its body.** Then `def f(s): print(s)` called
  with an integer would print through the string table. The call site is where the argument's type
  is known, so the call site is where the decision is recorded.
- **Refuse anything not statically provable, including helper-mutated containers.** That is what
  the static maps alone gave, and `fill(names, "one")` then `print(names[1])` printed `10`. The
  runtime object flags make the printing correct without a dataflow analysis across functions.
- **Use `strlen`** for `len(s)`: the runtime already declares `strlen` returning `i64`, and LLVM
  keys declarations by name — a second, incompatible declaration fails the module. A byte-counting
  loop keeps the runtime self-contained.

## Verification

`integration/string_args_test.go` runs 14 programs through both backends and asserts CPython's
output; `pkg/lang/string_args_test.go` asserts the IR invariants (interned call arguments, no
`i32 @.str`, unsupported uses refused before the verifier, mixed call sites not guessed at,
deterministic inference). The conformance corpus gained `string_params.gy` (20 prints). The
scoping fix is covered by `TestStrArgKindsIsDeterministic` and by `string_params.gy`, whose
`echo`/`wrap` pair is exactly the shape that exposed the name leak.
