# ADR 0160 — Variance model for generics + nominal class annotations (L6.6)

## Status
Accepted

## Context
Before this round the checker had a single structural `assignable(got, want)`
relation that compared only *kinds*: `list[str]` was assignable to `list[int]`
(both are `KindList`), a callable that demanded a `Dog` could be handed to a
`Callable[[Animal], int]` bound, and a user class name could not be written in
an annotation at all (`a: Animal` was a parse error). Every one of those is a
soundness hole or a missing ergonomic, and "L6.6 Variance + generics —
`list[T]` invariance, protocol structural subtyping; `gusty check` reports
contravariant misuse" is the roadmap item that names them.

The difficulty is that the obvious rule ("allow when the kinds match") is
unsound for mutable containers, while the textbook-strict rule ("require
identical type arguments") rejects code everybody writes, e.g.
`x: list[int | str] = [1]`. And the moment variance becomes observable, the
diagnostic has to *explain itself*: a user (or an agent) who sees `type
mismatch: expected Callable[[Animal], int], got fn` learns nothing.

## Decision
1. **One relation, one place.** `pkg/lang/variance.go` owns `subType(ci, got,
   want) *Violation`, the single structural subtyping relation. `assignable`
   (and `assignableIn`) are its boolean front-ends; the semantic checker calls
   `flowCheck`/`flowCheckAt`, which wrap `subType` and turn a `*Violation` into
   a `Diagnostic`. The old kind-only `assignable`/`seqAssignable`/
   `callableAssignable` trio is deleted.
2. **The variance table** (also machine-readable, see 6):
   - `list[T]`, `set[T]`, `dict[K, V]` — **invariant**. All three are writable
     through every alias, so the type arguments must match exactly. Dynamic type
     arguments stay tolerated (gradual typing).
   - `Sequence[T]`, `iter[T]`/`Iterator[T]`, `tuple[...]` — **covariant**.
     Read-only producers may widen their element type; tuples keep fixed arity.
   - `Callable[[P...], R]` (and `fn` types) — **contravariant** in the
     parameters, **covariant** in the return. A bare `fn` reference with no
     parameter information stays permissive.
   - class types — **nominal**, walking the declared base chain via
     `ClassIndex` (a pre-pass over the whole statement tree records
     `class Sub(Base)` links, so an annotation may name a class before its
     declaration).
3. **Fresh literals are covariant.** A container *literal* being assigned or
   passed has no other aliases yet, so its element type may widen to the
   destination's element type (`x: list[int | str] = [1]` is accepted, mypy's
   contextual inference); any other expression is a pre-existing object and gets
   the invariant rule. This is what keeps the strict rules usable.
4. **Diagnostics are rule-addressable.** `Diagnostic` gains a `Code` field
   carrying a stable rule id (`type.variance.invariant`,
   `type.variance.covariant`, `type.variance.contravariant`,
   `type.variance.nominal`, `type.callable.arity`, `type.union.members`,
   `type.mismatch`) plus a `Suggestion`. Message wording may improve; codes may
   not change. Human wording is unchanged for the pre-existing cases
   (`type mismatch: expected X, got Y`, `argument "f": …`, `return type
   mismatch: …`) with the rule clause appended, so existing consumers keep
   working.
5. **Runtime agrees where it can.** The interpreter enforces a nominal class
   annotation for the real class *and* subclasses (it walks the same base chain,
   recorded on the `Evaluator` when a class is defined), and treats the
   read-only protocols structurally (`Sequence[T]` accepts any container,
   `tuple[T...]` accepts the runtime list representation). Signature/element
   rules remain static-only, matching the existing annotation surface.
6. **Machine path first-class.** `gustyc --variance` prints the variance table
   (`VarianceDocument`: constructor, params, per-parameter variance, mutability,
   the read-only alternative, rationale, and the code the checker emits);
   `gustyc --schema` declares the `diagnostic` (with its code enum) and
   `varianceRule` shapes. The checker's rules and the printed table are one
   table, so an agent can plan a compilation without guessing variance.
7. **Function symbols carry their declared signature.** `analyzeFunc` now
   defines the function's own name as `TFunc(declaredParamAnnots, returnAnno)`
   instead of `TFunc(nil, …)`. Without this, passing a named function to a
   `Callable` bound would always hit the permissive "no parameter information"
   path and contravariance could never be reported.

## Codegen / IR implications
None. Variance is a *front-end* rule: no new runtime helper, no new IR shape, no
change to the module prelude or the golden IR file. The conformance program
`integration/programs/variance.gy` (nominal class annotations, covariant
`Sequence`, invariant `list`/`dict`) runs byte-identically on the interpreter and
the LLVM AOT backend, and is registered in the conformance matrix.

## Alternatives rejected
- **Bivariant (kind-only) assignability — the previous behaviour.** Rejected:
  it accepts `list[Dog]` where `list[Animal]` is written, which is the classic
  mutation bug (`animals.append(Cat())` on a `list[Dog]`).
- **Full covariant containers (`list[Dog] <: list[Animal]`).** Rejected: unsound
  for a mutable list. It is exactly the case the diagnostic now names.
- **Requiring literal element types to match exactly.** Rejected: it breaks
  idiomatic code (`x: list[int | str] = [1]`) for no soundness gain — a fresh
  literal has no aliasing.
- **Runtime-enforcing every variance rule (mypy does not even do this).**
  Rejected for element/signature rules: element types and callable signatures
  are erased, so runtime checks would need boxed element metadata that AOT does
  not carry — a parity risk for no benefit. Nominal class checks were kept
  because the instance already records its class name, so the check is free.
- **Duck-typed (structural) classes as the only class rule.** Rejected: two
  unrelated classes with the same method set are not interchangeable here
  (`super()`, `__init__`, class identity), so class annotations are nominal;
  structural protocols stay available through `Sequence`/`Callable`.
- **Putting the codes in the message only.** Rejected: the AGENTS contract
  requires a machine consumption path; codes are the only stable handle an agent
  gets once message wording improves.
