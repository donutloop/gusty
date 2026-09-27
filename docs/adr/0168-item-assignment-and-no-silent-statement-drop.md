# 0168. Item assignment is real syntax, and the parser may never drop a statement

Status: Accepted
Date: 2026-09-27
Audience: compiler maintainers, agents consuming the CLI

## Context

Writing the container-iteration corpus ran straight into the most ordinary Python
statement in the language:

```py
d = {}
d[1] = 2
print(d[1])      # printed nothing happened — on BOTH backends
```

Three defects were stacked on top of each other:

1. **The parser dropped the statement.** `parseExprOrAssign` handles `target = value`,
   but after consuming `=` and the value it only built an `AssignStmt` for an `*Attr`
   target; anything else fell through to `ExprStmt`, so `d[1] = 2` became a *read* of
   `d[1]` and the value was discarded. `1 = 2` and `f() = 1` produced **zero statements**
   and zero diagnostics, because the statement-recovery loop in `parseTopLevel` recorded
   only `*ParseError` values and silently discarded every other error the parser raised.
2. **Neither backend implemented the operation.** The interpreter's assignment statement
   matched `Name`, `Tuple` and `Attr` targets (an `Index` target matched nothing and did
   nothing); codegen had `Name`/`Tuple`/`Attr` and rejected `Index`.
3. **`{}` disagreed with itself.** The parser classified an empty brace pair as a
   `SetLit`, so the interpreter called it a set (`d[k] = v` → `not in set`) while codegen
   emitted dict code for the same token. Iterating a dict was broken too (Gap K.2), and
   the checker typed the loop variable as the *iterable*, so `for k in d: s = s + k` was
   rejected as `int + dict[any, any]`.

This is the class of bug the parity harness cannot catch: both backends agreed — on doing
nothing.

## Decision

**1. Item assignment is supported syntax with Python's semantics, in both backends.**

| Form | Behaviour | Out of bounds / wrong kind |
|---|---|---|
| `d[k] = v` | inserts, or updates in place | never raises for a missing key |
| `xs[i] = v` | replaces element `i` | `IndexError` |
| `s[k] = v` (set) | not allowed | error (`TypeError`-like) |
| `s[i] = v` (string) | not allowed | error (`strings are immutable`) |

The AOT list store goes through a new runtime helper `rt_put_elem(h, i, v)` — `rt_set_elem`
could not be reused because it *appends* (it bumps the length); the bounds test is emitted
around the store and routes to the existing `raise` path with `IndexError`'s code, so a bad
index takes the exception path instead of writing past the elements. Dict stores use the
existing `rt_dict_put` (update-or-insert, matching the interpreter).

**2. The parser must never drop a statement without saying so.** Two rules:

- the assignment path accepts `Name`, `Attr`, `Tuple` and `Index` targets and returns a
  `*ParseError` for anything else (a clear message with the statement's span);
- `parseTopLevel` converts **any** non-`ParseError` failure from `parseStmt` into a
  `ParseError` at the current token instead of discarding it. Parser errors are part of
  the diagnostic contract; a failure that vanishes during error recovery is how a program
  silently loses code.

**3. `{}` is an empty dict.** Braces mean mapping; `{1, 2}` (no colons, non-empty) is a
set; the empty set needs `set()`, which does not exist yet and is recorded as Gap K.3
rather than being quietly served by `{}`.

**4. Iteration binds what the container yields.** The checker maps a `for` iterable to its
element type (`list[T]`/`set[T]` → `T`, `dict[K, V]` → `K`, iterator → element), and codegen
iterates runtime containers by length (`rt_list_len`/`rt_dict_len`/`rt_set_len`), reading a
dict's keys at `2*i` from the `[key, value]` pair layout. A container bound from a call
(`d = make(3)`) takes its kind from the checker's inferred type, and the "release the
previous binding" step keeps the registration when the new binding *is* a container.

**5. Slot allocation belongs in the block that branches into the loop.** The loop
variable's `alloca` was emitted inside the body block; that does not dominate the code
after the loop, so `for k in d:` followed by `for k in m:` (which reuses the already
"allocated" name) produced `Instruction does not dominate all uses`.

## Rationale (agentic)

An agent that writes `d[k] = v` and gets a clean compile with no effect learns the wrong
lesson about the language, and a compiler that loses statements while reporting success
breaks the core promise of the front end: *what you wrote is what was compiled*. That is
why the parser rule is general — not just "support `Index` targets" but "no statement may
disappear" — and why the semantics are asserted against Python's answer on both backends
rather than against each other.

## Codegen / IR implications

```llvm
; d[k] = v
  call void @rt_dict_put(i32 %_d.ld, i32 %k.ld, i32 %v)

; xs[i] = v — bounds-checked, then an in-place store that must not grow the container
  %n = call i32 @rt_list_len(i32 %_xs.ld)
  %hi = icmp sge i32 %i.ld, %n
  %lo = icmp slt i32 %i.ld, 0
  %bad = or i1 %lo, %hi
  br i1 %bad, label %item.bad, label %item.ok
item.bad:
  store i32 1, i32* @exn_flag
  store i32 4, i32* @exn_code        ; IndexError
  br label %main.raiseexit           ; or the innermost handler
item.ok:
  call void @rt_put_elem(i32 %_xs.ld, i32 %i.ld, i32 %v)
  br label %item.end
item.end:

; for k in d:            (keys from the pair layout; loop-var slot allocated in the pre-header)
  %_k = alloca i32                    ; dominates the whole loop and the code after it
  %len = call i32 @rt_dict_len(i32 %_d.ld)
  ...
  %pos = mul i32 %idx, 2
  %key = call i32 @rt_get_elem(i32 %_d.ld, i32 %pos)
  store i32 %key, i32* %_k
```

## Alternatives rejected

- **Silently ignore an unsupported assignment target (previous behaviour)** — compiles a
  hole in the program. Rejected outright.
- **Lower `xs[i] = v` with `rt_set_elem`** — it appends, so `xs[1] = 9` would make the list
  one element longer. Hence `rt_put_elem`.
- **Skip the bounds check because `rt_get_elem` is unchecked too** — a read past the end
  reads a neighbour's words; a *write* past the end corrupts another container, which is
  the failure mode this repo already learned to fear (Gap K.1's `rt_free(0)`).
- **`{}` as an empty set** — keeps `{}` ambiguous between the two backends and is not
  Python; the empty set gets its own constructor instead (Gap K.3).
- **Keep the loop-variable alloca in the body block** — works until a second loop reuses the
  name, which is exactly what real programs do.

## Consequences

- `programs/subscript_assign.gy` joins the conformance corpus (36 cases, all at parity).
- `pkg/lang/subscript_assign_test.go` + `integration/dict_iteration_test.go` pin the AST
  shape, runtime semantics, IR shape and module verification.
- Two follow-on gaps recorded in `roadmap.md`: K.6 (an unhandled raise exits 0 silently in
  AOT; `raise IndexError("…")` does not compile) and K.7 (`--build` can fail without
  printing the reason when diagnostics are present).
- `beginScope` now governs the per-scope maps (slots, GC roots, container kinds); adding a
  new per-scope map means adding it there, or the next function body inherits it.
