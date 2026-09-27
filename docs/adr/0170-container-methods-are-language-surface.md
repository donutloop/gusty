# 0170. Container methods are language surface: pop, the empty-set constructor, set mutation

Status: Accepted
Date: 2026-09-27
Audience: compiler maintainers, agents consuming the language docs

## Context

Two of the most ordinary Python snippets failed in this language, in both backends:

```py
xs = [1, 2, 3]
print(xs.pop())        # interpreter: no such list method pop
                       # AOT:         string method pop on non-constant string
s = set()              # interpreter: unsupported call for eval
s.add(1)               # interpreter: attribute access on method
```

`xs.pop()` is how you drain a container, so the `while xs:` idiom had no spelling (the
truthiness tests had to rebind `xs = []` instead). And `{}` being the empty dict (ADR 0168)
left the language with **no way to write an empty set at all** — the `{1, 2}` literal needs
at least one element, so `set()` had to exist.

Notice how the two backends failed *differently*: the AOT routed `.pop` into the string
method path (`string method pop on non-constant string`) because method dispatch was a
chain of special cases per receiver kind, and heap sets had no branch at all.

## Decision

**1. `pop` is a language operation, not a library function.**

| Form | Result | Error |
|---|---|---|
| `xs.pop()` | removes and returns the last element | `IndexError: pop from empty list` |
| `xs.pop(i)` | removes and returns element `i` (negative counts from the end) | `IndexError: pop index out of range` |

The AOT lowers it to a new runtime helper `rt_pop(h, i)` — load the element, shift the tail
one slot left, decrement the length — with the bounds test emitted *around* the call, so a
bad index takes the exception path (ADR 0169) instead of reading past the element array.
The existing `rt_set_elem` could not express removal: it appends.

**2. `set()`, `list()`, `dict()` construct empty containers.** Each lowers to
`rt_alloc(kind)`, which zeroes the length on both the fresh and the recycled path, so the
returned handle is a genuinely empty container. They are also predeclared names in the
checker — `set`, `list` and `dict` were unbound there, which made the constructor a
*checker* error before codegen was ever reached.

**Rejected alternative:** lowering `set()` to an empty `SetLit`. The constant folder turns a
literal into a module-level global, and `s = set()` then emitted
`store i32 @.set1, i32* %_s` — the folded-global bug class of ADR 0163, caught by the module
verifier rather than by luck.

**3. Set mutation exists.** `s.add` / `s.discard` / `s.clear` on a heap set
(`rt_set_add`, the new `rt_set_discard` / `rt_set_clear`), and `remove` in the interpreter.
`discard` is silent about absence; `remove` raises `KeyError` — Python's distinction, chosen
because an interpreter-only pair of names would drift.

**4. The empty set prints as `set()` everywhere.** Python renders `set()`, not `{}`; the
interpreter's `Repr` already did and the AOT renderer did not, so `print(set())` disagreed
across backends. `rt_set_print` now branches on length 0.

**5. The copy forms are an honest diagnostic.** `list(xs)` / `set(xs)` / `dict(d)` need an
element loop; until codegen has one, the AOT refuses with a message naming the construct and
the backend that works (ADR 0166), and the interpreter keeps supporting them.

## Rationale (agentic)

`docs/language.md` is what an agent plans a program from. A built-in that the docs promise
and the compiler routes to a *string* method is worse than an absent one: the agent gets a
diagnostic about strings and optimises against the wrong problem. So each name above ships
in four places at once — interpreter, checker, codegen, docs — and `programs/container_methods.gy`
asserts both backends against Python's answer, not against each other.

## Codegen / IR implications

```llvm
; xs.pop(i) — bounds test around the removal, negative index normalised before the call
  %n    = call i32 @rt_list_len(i32 %h)
  %neg  = icmp slt i32 %i0, 0
  %adj  = add i32 %i0, %n
  %idx  = select i1 %neg, i32 %adj, i32 %i0
  %hi   = icmp sge i32 %idx, %n
  %lo   = icmp slt i32 %idx, 0
  %bad  = or i1 %lo, %hi
  br i1 %bad, label %pop.bad, label %pop.ok
pop.bad:
  store i32 1, i32* @exn_flag
  store i32 4, i32* @exn_code            ; IndexError
  store i8* @.strN, i8** @exn_msg         ; "IndexError: pop index out of range"
  br label %main.raiseexit
pop.ok:
  %v = call i32 @rt_pop(i32 %h, i32 %idx)

; s = set() — a real allocation, never a folded global
  %h = call i32 @rt_alloc(i32 3)          ; 1=list 2=dict 3=set
```

Two guard tests now protect the embedded runtime text (`pkg/lang/runtime_ir_test.go`):
LLVM comments must be `;` (a `//` line is a module parse error), the raw string must contain
no backtick (it would end the Go literal), `define`s must balance, and no helper may be
defined twice. Each of those four mistakes happened during this and the previous cycle and
each was found only by `opt -passes=verify`.

## Alternatives rejected

- **`xs.pop()` returning the list handle (like this repo's `append`)** — `append` returns the
  handle so the REPL can show the updated list; `pop` is an *expression* whose value the
  program uses. Returning the container would make `while xs: print(xs.pop())` print lists.
- **`set()` as `{}`** — `{}` is the empty dict (ADR 0168); overloading it reintroduces the
  one-token-two-kinds bug.
- **Silently accepting `list(xs)` copies** — there is no element loop for them in codegen;
  silently doing nothing would reproduce the lost-statement bug of ADR 0168.
- **Set methods only in the interpreter** — the divergence would be invisible to the parity
  harness whenever a case did not exercise it, which is exactly how `set`/`list`/`dict`
  stayed unbound in the checker for so long.

## Consequences

- `programs/container_methods.gy` joins the conformance corpus (37 cases, all at parity).
- `docs/language.md` § Container methods documents the surface, including the AOT copy
  limitation, so the docs cannot promise more than the compiler delivers.
- Remaining container gaps stay recorded rather than absorbed: `xs.extend`/`insert`/`remove`
  and `sort`/`sorted` on heap lists, dict `.keys()`/`.items()` on heap dicts, and the copy
  constructors.
