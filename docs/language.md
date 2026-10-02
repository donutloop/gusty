# gusty language

## Gradual typing

`--verify` statically checks assignment annotations against the inferred type
of the value: `x: int = "hello"` reports `type mismatch: expected int, got
str`. The `any` annotation accepts any inferred type. Runtime enforcement
(`checkAnnot`) still applies for computed values. See ADR 0096.

## Comprehensions

List, dict, and set comprehensions are supported on both backends. Dict/set comprehensions use
`for` inside the braces, Python-style: `{x: x*2 for x in [1,2,3]}` builds a
dict, `{x for x in [1,2]}` a set. The `if` filter is the comprehension's on every kind —
`{x for x in xs if x > 1}` parses (ADR 0234). See ADR 0095.

**A `{…}` display ends at its brace.** A `for` after the closing brace finishes a display only when
the display is the whole expression — the call-argument form `len({x * x} for x in xs)`. Inside a list
display the `for` belongs to the *enclosing* comprehension, so the braces hold an element and not a
second comprehension (ADR 0244):

```gy
d = [{1, 2} for x in [1, 2]]   # a list of two sets — not a set
print(len(d))                   # 2
print(d[0])                     # {1, 2}
d = [{"k": x} for x in [1, 2]] # a list of two dicts, each with its own entry
print(d[1])                     # {'k': 2}
```

**A comprehension's element is a value, and it is stored as one** (ADR 0244). A container, a float,
`None` and a piece of text each go into the slot with the tag that says what they are — payload and
tag in one write, the same rule `xs.append(v)` follows — so the container prints them and a slot read
prints them back:

```gy
print([[1, 2] for x in [1]])     # [[1, 2]]   — the element is the list, not its address
print([1.5 for x in [1]])        # [1.5]      — not the float box's handle
print([None for x in [1]])       # [None]     — not the 0 that `if None:` folds to
print(["a" for x in [1]])        # ['a']      — the container prints, not the string printer
xs = [x + 1 for x in [1, 2]]
print(xs[0], xs[0] + 1)          # 2 3        — an integer element still does integer things
```

**What the loop variable knows** (ADR 0244, ADR 0245, Gap R.46, Gap R.76). An element that *is* the loop variable
carries its kind — and, over a container whose slots mix kinds, its **tag** — into the list it builds,
asked of the container being iterated, because the loop variable's own facts are gone once the loop
closes. That is what makes `print(out)` and `print(out[0])` agree on `['a']` and `a` instead of one
printing text and the other the interned index, and what lets `[x for x in {1, "a", None}]` print
`[1, 'a', None]` on the compiled backend instead of `[1, 0, 0]`. Iterating a dict walks its keys at the
stride its two-word entries need (ADR 0188), and a dict comprehension writes each entry as two
`(payload, tag)` pairs, so `{k: 1 for k in d}` produces an entry `out["a"]` can find. A slot of that
list answers an equality the way the printer already answers `print`: `out[1] == "a"` asks the slot's
own tag (ADR 0247, closing Gap R.79). What still refuses
by naming itself: a comprehension over a name the compiler kept as a compile-time list.

## Slicing (`s[a:b]`, `s[::step]`, negative indices)

Sequence slicing is supported on strings and lists in both the interpreter and
the AOT (native) backend:

```
l = [1, 2, 3, 4, 5, 6]
print(l[1:4])    # [2, 3, 4]
print(l[:])      # [1, 2, 3, 4, 5, 6]
print(l[::2])    # [1, 3, 5]
print(l[-3:])    # [4, 5, 6]
print(l[::-1])   # [6, 5, 4, 3, 2, 1]
```

- `s[a:b]` copies the slice from index `a` (inclusive) to `b` (exclusive).
- Omitted bounds default to the sequence start/end (`s[:b]`, `s[a:]`, `s[:]`).
- An explicit `step` selects every `step`-th element (`s[a:b:c]`, `s[::step]`);
  a negative step walks backwards (`l[::-1]`).
- Negative bounds count from the end (`s[-3:]` is the last three elements, `l[::-1]` walks
  backwards), and so do negative subscripts: `xs[-1]` is the last element, `xs[-1] = v`
  replaces it, `"abc"[-1]` is the last character. One rule — `i < 0 ⇒ i + len` — serves read,
  write, `pop`, `index` and slice bounds (ADR 0210). It is *not* applied to a dict or set
  subscript, where `i` is a key and `-1` is a key you can store (`d[-1] = v` works, as in
  Python). Out of range past either end is `IndexError`; a negative key in a dict is simply a
  key. See *Containers* below. A string held in a variable still cannot be subscripted in the
  AOT backend (roadmap L11.5) — the interpreter and CPython answer it, the compiled backend
  refuses with a message rather than answering zero.
- A zero step is an error.
- Slicing a string returns a string; slicing a list returns a list.

String slicing is supported in the interpreter; in the AOT backend string
variables are currently limited (string *literals* are supported inline), so
list slicing is the primary native path. See `pkg/lang/jit.go` (`pySliceIndices`)
and the `rt_slice` runtime helper in `pkg/lang/codegen.go`.

## Pattern matching

`match` supports list-destructuring patterns: `case [a, b]:` matches a list
subject element-wise, binding Name pattern elems to the subject's elements
(e.g. `match x: case [a, b]: a + b` unpacks to 30 for `x = [10, 20]`). A
non-Name element is compared element-wise; mismatched arity/kind falls through
to later cases. See ADR 0091.

A bare-name pattern (`case y:`) is *irrefutable*: it always matches and binds
the subject. `case _:` is a wildcard: it always matches and binds nothing.

**Exhaustiveness (ADR 0154).** A `match` is *exhaustive* iff at least one
case is irrefutable (a bare `Name`, including `_`). A `match` with only
refutable (literal) cases is non-exhaustive: `gusty check` emits a mypy-style
warning, so you know to add a `case _:` fallback. Warnings don't fail the
check (only errors do).

**Definite assignment (ADR 0154).** A name bound by an irrefutable case that
fires on *every* path is definitely assigned and readable after the match.
A name bound on only *some* paths (an earlier refutable case can skip it) is
not definitely assigned, and reading it after the match is an `undefined
name` error.

**AOT lowering.** Guards, or-patterns, and the `_` wildcard lower to native
code (`codegen.go`); dict-pattern and class-pattern lowering is
interpreter-only today.

## Standard library

Builtins include `len`, `print`, `range`, `min`, `max`, `zip`, `int`,
`float`, `str`, `sum`, `abs`, `sorted`, `reversed`, `enumerate`, `any`,
`all`, `chr`, `ord`, `round`, and the string methods `upper`/`lower`/
`capitalize`/`title`/`swapcase`. `any(iter)` is 1 if any element is truthy;
`all(iter)` is 1 if all are; `chr(n)` makes the single-char string for a
codepoint; `ord(s)` reads the first char's codepoint. In the AOT codegen, `chr(n)` folds a constant codepoint to a single-character string global and `ord(s)` folds a constant string to its first-byte codepoint (mirroring the interpreter's `sval[0]`), both as literal folds.

`round(x)` is the nearest-value rule with **ties to even** — IEEE `roundTiesToEven`, the rule CPython
uses: `round(0.5)` and `round(1.5)` are `0` and `2`, `round(2.5)` and `round(3.5)` are both `2`, and
`round(-2.5)` is `-2`. An integer argument is returned unchanged. Both backends ask for the named
IEEE operation rather than implementing a rounding rule — `math.RoundToEven` in the evaluator,
`llvm.roundeven.f64` (and the same fold for a constant) in the compiled backend — because two
independent implementations of "half away from zero" agreed with each other for as long as nobody
asked CPython (Gap R.50, ADR 0236).

`round(x, ndigits)` is **not** implemented, and the two backends currently fail differently (Gap R.69):
the interpreter ignores `ndigits` and returns an integer (`round(2.345, 2)` → `2`, where CPython gives
`2.35`), and the compiler refuses the call (`round expects one argument`). Write `round(x * 100) / 100`
or an explicit `int` conversion until that row closes.

`float(x)` folds a constant int to itself and a constant string to its parsed
then-truncated float value in the AOT codegen (the backend represents floats
as truncated ints, mirroring value()'s FloatLit handling).
floats. See ADR 0090 — and Phase 2's L11.6 for the float paths still open (a float reaching an
untyped function parameter, `/=` → float, `floor`/`ceil` → `int`).

## Memory model

The evaluator boxes values on a heap (`map[int64]*obj`). Unreachable pure-data
objects (list/dict/set/str/int/float) are reclaimed by a two-generation (nursery +
old) tracing collector with a **precise root set** (ADR 0181): the current
environment, every active call frame's locals, the root groups a construct declares
(a `for` loop's iterable, an executor's last statement value), and the permanent
roots (`None`, the generator accumulator, the `super()` receiver, class objects).
Marking walks container elems, dict values, closure envs, coroutine args, and attr
tables; the sweep frees unmarked pure-data objects. Class/method/closure/import
objects are never freed.

Collection happens at **safe points**: statement boundaries reached with no
expression evaluation in flight, from a construct that declared its roots — plus the
body of a call that *is* the whole statement (`work()`, `total = helper(x)`). The
watermark rule keeps it sound: anything allocated after the last safe point is
unconditionally live, because the interpreter may still hold it in a register.

Observable consequences, and the limits, in one place:

- A long loop or a long REPL session reclaims as it goes; the heap is bounded by live
  data rather than by everything the session has ever built.
- A value that lives only in a live frame's local survives collections — recursion
  that binds a list, recurses, allocates, and then reads the list back is correct
  (`integration/programs/gc_precise.gy`).
- Conservative by design: garbage created inside a call that is *nested in an
  expression* is not reclaimed until the enclosing statement completes. Only the
  value model of roadmap Phase 11 (a real value stack) removes that limit.
- `ev.Collect()` remains the explicit entry point (it lifts the watermark first, since
  nothing is mid-evaluation when it is called from outside a running program).
- The collector reports itself: `Evaluator.GCStats()` in Go, `gustyc --gc-stats` (and
  the `gc` member of an `--eval --json` payload) on the command line. See
  `docs/operations.md` § Collector self-report.

The **compiled backend** roots the same way, in its own runtime: `@gc.roots` is a stack
of slot addresses tagged by `@gc.kinds` as handle-or-not, a call opens a frame and pops
it at every return, every handle-assigning store pushes the slot it wrote, and storing a
non-handle tags the entry dead. Two properties this depends on are enforced by codegen
and checked by tests: a variable's slot is allocated **once per call** (an `alloca` left
in a loop body would change address every iteration and so could never be matched), and
**every** function-emitting path — plain functions, class methods, closure helpers,
decorated bodies — opens and closes its frame. A program whose root stack would overflow
(4096 entries) stops itself instead of running with an unrooted handle.

See ADR 0089 (the two-generation heap) and ADR 0181 (precise roots and safe points).

## Optimization (`--opt-level`)

`--opt-level` runs compiler-level optimization passes over emitted LLVM IR.
level 1 performs dead-global elimination (prunes `@.strN`/`@.lstN` globals
never referenced by the body). Escape analysis (always on, independent of
`--opt-level`) skips the runtime heap allocation for a top-level list literal
whose variable is never read — a dead-object elimination that proves the
allocation never escapes (see ADR 0134). Because `--emit-llvm` is a string
flag that consumes the next argument as its value, pass `--opt-level` first:

    gustyc --opt-level=1 --emit-llvm='"hi"'

See ADR 0088.

## Agentic interface

`--eval --json` emits a structured, typed result:
`{"result": "<repr>", "type": "<dynamic-type>", "exit": 0}` where `type` is the
evaluator's inferred dynamic type (`int`/`float`/`str`/`list`/`dict`/`set`/
`closure`). See ADR 0094.



`gustyc --schema` prints a machine-readable JSON Schema (draft-07) describing
the structured outputs: the `--emit-ast` AST dump (`{"stmts": [...]}`,
per-node required fields), the front end's diagnostics, the build report's
verification and optimization members, and the debug-line documents
(`definitions.debugInfo`, `definitions.dwarfReport`, ADR 0231). Agents can validate
any of these dumps against it without reading the compiler source; see
`docs/agentic/ast-ir-schema.md` and ADR 0087.

## Agentic interface

 prints a machine-readable JSON Schema (draft-07) describing
the structured outputs: the  AST dump (,
per-node required fields) and the  IR text dump
(, ). Agents can validate AST dumps against
it without reading the compiler source; see 
and ADR 0087. surface

Single source of truth for gusty's syntax and semantics. This document tracks
the language as it grows; each feature commit updates it.

gusty is a Python-like, indentation-based language compiled ahead-of-time
through LLVM. Values are gradually typed: optional annotations steer inference,
and untyped code falls back to dynamic dispatch at runtime.

## Lexical structure

- Indentation-sensitive: `INDENT`/`DEDENT` tokens drive block nesting.
- Statements are separated by newlines; a line ends a statement.
- Comments begin with `#` and run to end of line.

- Comments begin with `#` and run to end of line.

### Numeric literals

Integer and float literals accept several base prefixes and `_` digit
separators, mirroring Python's syntax:

- `0x` / `0X` — hexadecimal integer, e.g. `0xFF` == 255.
- `0b` / `0B` — binary integer, e.g. `0b101` == 5.
- `0o` / `0O` — octal integer, e.g. `0o17` == 15.
- `_` may appear between digits, or once right after a base prefix, as a
  digit separator (e.g. `1_000`, `0x_FF`, `0b1010_0000`). It is ignored for
  the numeric value.
- Floats accept separators in the integral and fractional parts
  (e.g. `1_0.5` == 10.5, `1.5_0` == 1.5).

Misplaced separators (`1__0`, `1_`, `0x_`) are a lexical error. Hex/binary/
octal literals are integer-only. Values are computed exactly at lex time, so
the interpreter and the AOT backend agree on `0xFF + 0b101 + 0o17 + 1_000`.

## Statements

### Statement separators (`;`)

A `;` separates simple statements written on one line — the on-line spelling of the newline that would
otherwise divide them (ADR 0242, closing roadmap Gap R.72):

```
x = 5; print(x + 1)        # 6
a = 1; b = 2; print(a + b) # 3
print("done");             # a trailing separator is fine; it is not an empty statement
```

An inline suite is a list of simple statements too, so the statements after a `;` belong to the suite and
not to the block that contains it:

```
for i in [1, 2]:
    print(i); print("step")   # 1, step, 2, step — both statements run per iteration
```

The empty statement is rejected, wherever a syntax rule lives (the parser, not a diagnostic one entry
point happens to enforce): `a = 1;;b = 2`, `a = 1; ;b = 2`, a line beginning with `;`, and `if x: pass;;`
are all reported as `empty statement: ';' separates two statements, and there is nothing between them`.
`a = (1; 2)` is rejected too — a separator is a statement-level token, not an operator. Every entry point
— `--interp`, `--eval`, `--repl`, `--jit`/`--aot`, `--emit-llvm` — gives the same verdict on the same
source, which is the point: `;` used to be a program to the interpreter, a refusal to the JIT, and a clean
compile to `--emit-llvm`.

### Assignment

```
x = 1
x = x + 1
```

The target may be a `Name`; the value is any expression. Assignments define or
rebind a variable in the current scope.

### Augmented assignment

`target op= expr` reads the target, applies an arithmetic operator to the RHS,
and writes the result back:

- `x += e`  (add), `x -= e`  (subtract), `x *= e`  (multiply),
  `x /= e`  (integer divide), `x //= e` (integer divide), `x %= e` (modulus).

The target must be a Name or an attribute (`self.x += 1`). Augmented assignment
is supported in both the tree-walking interpreter and the AOT code generator (int
and float Name targets, int Attr targets).

**`/` is true division (PEP 238), `//` is floor division.** `7 / 2` is `3.5` and
`84 / 2` is `42.0` — a float, even when both operands are integers. `7 // 2` is
`3`, and `-7 // 2` is `-4` (it floors, it does not truncate toward zero). An
earlier backend made `/` and `//` the same truncating operator, which turned the
language's most common operator into C's. Remaining compiled-backend gaps: `//`
on negative integers still truncates under AOT, `x /= 2` keeps the integer
representation, and a float passed to an untyped parameter truncates — each
pinned in `integration/division_test.go` (roadmap Gap P.1).

**Floats print the way Python renders them** (`str()`, `print`, and f-strings all
agree): the shortest text that round-trips, with a trailing `.0` when the value is
integral — `print(2.0)` is `2.0`, `print(0.123456789)` is `0.123456789`, `print(1e20)`
is `1e+20`. In the AOT backend this is `@rt_fmt_double` (a `snprintf` precision
ladder checked against `strtod`, then the `.0`), because `%g` truncates digits and
`%.17g` invents them.

### Expression statements

Any expression on its own line.

### `print(*args, sep=" ", end="\n")`

`print` is the builtin that writes to stdout, with Python's separator and
terminator semantics — identical in the interpreter and the AOT backend
(ADR 0165):

```python
print("n =", 42)              # n = 42
print(1, 2, 3)                # 1 2 3
print()                       # a blank line
print("csv", 1, 2, sep=", ")  # csv, 1, 2, 3
print("tick", end="!")        # tick! (line left open)
print("a", "b", sep="|", end="?")   # a|b?  — `end` replaces the newline entirely
```

- Arguments are rendered the way `str()`/`repr()` renders them and joined with
  `sep`; `end` is written once, after the last argument.
- **A container prints as a container, however it is written** (ADR 0188): a list, dict or set
  reaches `print` as a literal (`print([1, 2])`, `print([])`, `print({})`, `print({"a": 1})`) or
  as a constructor (`print(list())`, `print(dict())`, `print(set())` — the empty set has no
  literal spelling), and both backends call the same runtime printers that a container variable
  uses. What it must never print is its own representation: not the handle (`0`), not the static
  elements global, and not the interned indices a recycled heap slot happened to inherit — all
  of which happened here, in programs that compiled, verified and exited 0.
- **Known divergence (Gap L.5, pinned by `programs/probe_print_atomic.gy`):** an argument
  whose own evaluation prints — a call that prints — does *not* keep its place in the line.
  Both backends write each argument as they evaluate it, so
  `print("got", twice(21))` where `twice` prints `<< 21 >>` emits `got << 21 >>` then `42`,
  where Python emits `<< 21 >>` then `got 42`: Python evaluates every argument and only then
  writes one line. The two backends interleave it identically, which is exactly why parity
  could not see it; the CPython oracle leg can (ADR 0186).
- `sep` and `end` must be keyword arguments; any other keyword is an error
  (`print got an unexpected keyword argument "junk"`). In the AOT backend they
  must be compile-time string constants — the compiler says so
  (`print's sep must be a compile-time string constant`) rather than emitting IR
  that only LLVM's verifier would reject.
- A `%` inside `sep`/`end` is literal text, not a `printf` directive.
- Containers print through their runtime renderer inside the joined line:
  `print("xs =", [1, 2])` → `xs = [1, 2]`, and a set renders `{1, 2}` (the empty
  set as `set()`), matching Python rather than `<set>`.
- Under the hood an argument's `printf` format carries **no** newline; only the
  terminator does. That is what makes `end=""` mean "no line break" for every
  argument kind, including the runtime container printers
  (`rt_print_list`/`rt_dict_print`/`rt_set_print` take the newline as a flag).

### `pass` (no-op)

`pass` is a no-op statement: it does nothing and execution continues with the
next statement. Useful as a placeholder in function, loop, or branch bodies.

```
def wait():
    pass

for i in range(3):
    pass
    print(i)
```

Supported in both the interpreter and the LLVM AOT codegen path.

### Import / modules

### Lists (LLVM codegen)

The AOT LLVM path supports inline list literals with constant indexing and
`len` on an inline list: `[1, 2, 3][1]` and `len([1, 2, 3])`. Each list
literal is lowered to a dedicated global struct; indexing and `len` use
constant GEP indices (this llc build accepts only constant GEP indices), so
lists must be used inline (no assignment-to-variable indirection) in the
codegen path.

`for x in [1, 2, 3]:` iterates an inline list literal's constant elements in
the AOT path by unrolling one body block per element (`break`/`continue` and
the `else:` clause behave exactly like `range` loops).

### List comprehensions (LLVM codegen)

A list comprehension means *a loop that appends*, and the compiled backend now implements that
meaning rather than a subset of it (roadmap L11.7, ADR 0192). Three lowerings coexist, tried in
this order:

1. **Constant fold** — the iterable is an inline list literal or `range(...)` with constant bounds
   and the element and filter both fold. The result is a dedicated global struct, unrolled and
   folded at compile time, and indexable inline: `[x * 2 for x in [1, 2, 3]][1]` emits a
   `getelementptr` + `load` at the constant key. This path keeps priority because `sum`/`min`/`max`/
   `len` over a comprehension read the compile-time element set.
2. **Unrolled with runtime elements** — same iterables, but the element (or the filter) is not a
   constant, e.g. `[sq(x) for x in range(5)]` or `[abs(x) for x in [-1, 2, -3]]`. One straight-line
   block per item: store the item into the loop variable's slot, evaluate the element, append it to
   a heap list. The loop variable is a real binding, which is what makes a call in the element
   position possible at all.
3. **Runtime loop** — the iterable is a container whose length is only known at runtime (a list,
   set or dict variable that exists as a heap object): an index counter over
   `rt_list_len`/`rt_get_elem` with the loop variable bound per step, as a `for` statement binds it.
   The object the loop fills is the container the comprehension means — `rt_append_tagged` for a
   list, `rt_set_add_tagged` for a set (which dedups on the `(payload, tag)` pair, ADR 0232),
   `rt_dict_put_tagged` for a dict — and all three find their own slot, so an `if` filter that skips
   an item cannot leave a hole behind.
   An element is free to **branch**: `v / 2` carries its own zero guard (ADR 0253) and a slot whose kind
   the object reports branches on the tag (ADR 0251), so the counter's increment lives in a latch block of
   its own — `comp.merge`, or the filter's `comp.skip` — that every path the element can end on branches
   to, and the induction `phi` names *that* block instead of the body. The entry list has to say where the
   back edge really comes from, which is the check `llc` makes and the reason `xs.append(6)` followed by
   `print([v / 2 for v in xs])` was exit 2 until roadmap Gap R.100 (ADR 0255). A trapping element is
   therefore a real trap: `[v / 0 for v in xs]` dies with `ZeroDivisionError: division by zero` and exit 3
   inside the comprehension, behind a filter or in front of one.

The result of either runtime path is an ordinary container: it can be assigned (`sqrs = [sq(x) for
x in range(4)]`), printed, indexed, measured with `len`, iterated with `for`, and passed to a
function. The assignment registers it as a container *and* roots it, because an unrooted heap handle
is one collection away from a segfault (ADR 0181).

Three shapes refuse rather than answer wrongly, each naming its reason:

- `sum`/`min`/`max` over a comprehension whose elements are computed at runtime — there is no
  compile-time element set to fold, and folding the empty one answers **0 for a list that has
  elements** (`probe_comp_runtime_reduce`; a runtime reduction is the open item).
- printing an element of a comprehension whose **iterable** is a run-time-grown text list —
  `names = ["a", "b"]; names.append("c"); out = [n for n in names if n == "a"]; print(out[0])` answers
  the interned index (`0`) where CPython answers `a`: promoting the list to mixed clears the
  `listElemStr` fact the loop variable's kind came from, so the elements are tagged as numbers
  (roadmap Gap R.46). The other element kinds are answered — a container, a float, `None` or text
  written into a comprehension's slot carries its tag and prints back as itself (ADR 0244);
- `str(x)` of a value the compiler cannot fold, and indexing a `sorted(...)` result, refuse with a
  message (roadmap Gap R.22);
- any string question asked at *run* time on the compiled leg — `s[i]` with a variable index,
  `len(s[1])`, `s[1].upper()`, `ord(s[1])`, `for c in s` over a variable string — because a compiled
  string is a compile-time value and there is no runtime string object to ask (roadmap Gap R.47; the
  interpreter answers all of them).
- iterating a list the escape analysis kept as a compile-time constant (`xs = [1, 2, 3]` with no
  mutation and no `for` over it) — there is no runtime object to walk. Iterating it with `for`, or
  mutating it, materialises it (`probe_comp_folded_iter`; L11.2's tagged value word removes the
  category).

Dict and set comprehensions take the same three paths, and the same rule decides what a binding
means: **a comprehension that folds *is* the literal it folds to** (roadmap Gap J.2, ADR 0234).
`sa = {x for x in [1, 2, 3, 2, 1]}` and `sa = {1, 2, 3}` reach one lowering — a heap object of the
right kind, every slot written with its payload and its tag together, and the variable's kind
recorded so `print(sa)` asks `rt_set_print`, `2 in sa` asks `rt_contains`, `da[k]` asks the dict
lookup and `for k in da:` iterates the keys. Print dispatches on the comprehension's kind
(`rt_print_list_mixed` / `rt_set_print` / `rt_dict_print`), because a set comprehension rendered as a
list was a wrong answer hiding behind an invalid module.

The `if` belongs to the comprehension, on every kind:
`{x for x in xs if x > 1}` and `{k: k * 2 for k in ks if k > 1}` parse. They used not to — the
iterable was parsed as a full expression, and a full expression is a ternary, which read the
comprehension's `if` as its own and demanded an `else` (`expected keyword "else"`, on both backends,
while `[x for x in xs if x > 1]` worked: the brace branches had simply never been given the
`or`-precedence fix the list branch already had).

What still refuses in the compiled backend refuses *with the list spelling's words*, asserted in
`TestContainerComprehensionRefusalsStayHonest`: an iterable the escape analysis folded away
(`xs = [1, 2, 3]` with no mutation, then `{x for x in xs}` — `xs.append(...)` materialises it; L11.1's
tagged element retires the category), and a non-integer iterable, element or key (L11.5, L11.6).
A container **returned** from a function is a separate hole, literals included: `return [1, 2]` emits
`ret i32 @.lst1` and `la = [1, 2]; return la` prints the handle (roadmap Gap R.67).
The interpreter evaluates comprehensions at runtime and is the reference for all of this.


### Multi-argument range iterables (comprehensions)

List comprehensions over `range(start, stop)` and `range(start, stop, step)`
are supported in both the interpreter and the LLVM AOT codegen. The iterable
range accepts one, two, or three constant arguments:

- `range(stop)` → `0..stop-1`
- `range(start, stop)` → `start..stop-1`
- `range(start, stop, step)` → `start, start+step, ...` with a positive or
  negative step (a zero step is a compile error).

In the AOT path the comprehension body unrolls over the stepped range at
compile time, so e.g. `sum([x for x in range(1, 5, 2)])` folds to `1 + 3 = 4`
with no runtime loop.

### Aggregates over comprehensions (LLVM codegen)

The aggregate builtins `len`, `sum`, `min`, `max` accept a lowered
comprehension result in addition to an inline list literal. Because a
comprehension over a constant iterable unrolls to a `{i32 count, [n x i32]}`
global struct with the same shape as a list literal:

- `len([...])` loads the stored count field from the comprehension's global.
- `sum([...])`, `min([...])`, `max([...])` fold the folded comprehension
  elements (which are all constants) to a single constant at codegen time, so
  e.g. `sum([x for x in range(5)])` folds to `10` with no runtime loop.

### Generator expressions

`(elem for var in iter [if cond])` is a generator expression: it yields
`elem` for each element of `iter` bound to `var`, optionally filtered by
`cond`. In the interpreter it evaluates eagerly to a list of the yielded
values, so it can be consumed by a `for x in gen:` loop or a `list(gen)`
call. Example: `(x * 2 for x in [1, 2, 3] if x > 1)` → `[4, 6]`. Generator
expressions and generator functions (`yield`) evaluate eagerly to runtime
heap lists in both the interpreter and the AOT codegen; see ADR 0081.

## Every statement has a position (L8.5, ADR 0231)

A statement is not only what it does but where it is. Every statement in the grammar carries the
line and column it starts at, and the compiler is required to know that position wherever it
reports on, compiles, or can be asked about a program:

```gusty
class Counter:
    def bump(self, k):
        self.n = self.n + k      # an assignment to an attribute is still a statement,
        return self.n            # and it still knows it is on line 3
```

This is a language guarantee rather than an implementation detail, because three user-visible
behaviours are built on it and each of them fails silently without it:

- **diagnostics** point at the statement they mean (`--verify`, `--check`);
- **tracebacks** name the line of the `raise` and the function around it (Gap K.6, Gap K.8);
- **the compiled line table** — `--debug` puts `!dbg` records in the module, so `llc` writes a
  `.debug_line` table a real debugger can stop at, and `--debug-info` prints the table the
  compiler wrote (ADR 0231).

An assignment to an attribute (`self.n = …`) and a tuple assignment (`a, b = xs`) used to be built
without a position at all, so every instruction they produced inherited the previous statement's
line: the debugger, the traceback and the error message all blamed the `def` for the body. They now
carry the position of their leftmost target. Columns name where a statement *starts*: positions are
not kept for individual subexpressions, so a column is a statement column.

The compiled program also says what language it is: the artifact's `DW_AT_language` is
`DW_LANG_Python`, which is what lets a debugger print gusty frames as the language they are.

## Ternary conditional expressions

`then if cond else otherwise` (Python-style ternary) is supported in both the
interpreter and the AOT codegen. The condition is an or-level expression; the
`else` branch is a full expression, so nested ternaries bind right:
`1 if 0 else 2 if 1 else 3` is `1 if 0 else (2 if 1 else 3)`.

In the AOT path, a constant condition folds to the taken branch, and a runtime
condition (a comparison, or `and`/`or`) lowers to an LLVM `select i1 cond,
i32 then, i32 else`, so the ternary is allocation-free and needs no blocks.

## Truthiness

Every place that tests a value — `if`/`elif`/`else`, `while`, the ternary
condition, `and`/`or`/`not`, a comprehension's `if` clause, a `match` guard — uses
the same rule, and both backends implement it identically:

| Value | Truth |
|---|---|
| `0`, `0.0`, `-0.0` | false |
| `False`, `None` | false |
| `""` | false |
| `[]`, `{}`, an empty set | false |
| anything else (including any other number, string, container or object) | true |

## None

`None` is a real value — a singleton with its own dynamic type (`None`, value tag
`TagNone`) — and not the integer `0`. Both backends agree, and the AOT backend allocates it
once as a heap object of kind 4 rather than reserving an integer, because every `i32` is a
legal integer and no bit pattern is free to mean "no value" (ADR 0172).

```py
def emit():
    print("side")          # no return statement

print(None)                # None      (not 0)
print(emit())              # side, then None
print(emit() == None)      # 1         — a procedure returned None
print(0 == None)           # 0         — 0 is not None
if None:                   # None is falsy
    print("truthy")
else:
    print("falsy")         # → falsy
```

- A function that runs off the end, or `return` with no expression, yields `None` — never
  the value of its last statement.
- Generators are not procedures: `def gen(): yield 1` evaluates to the list of yields.
- `x == None`, `x != None`, `is`/`is not` against `None` are decided where the source says
  what each side is, and both operands are still evaluated, so a side effect in a comparison
  is never optimised away.
- `type(x) == "None"` is not how you ask: `type` is a reserved word here (annotations), so
  `type(x)` is not a call in this grammar. Use `x == None`.

```py
if count:                 # int truthiness — 0 is false
    print("have some")
while buffer:             # a container is true while it has elements
    print(len(buffer))
    buffer = []
if not (a > b) or flag:
    print("ok")
label = "yes" if text else "no"
```

Booleans are values, not just tests: `a and b`, `x in xs` and `not x` produce `1`
or `0`, so they can be printed, stored (`flag = a < b`) and re-tested.

How each backend gets there is an implementation detail, but a load-bearing one
(ADR 0167): in IR a value is either an `i32` or the `i1` result of a comparison, and
neither may be fed to the other's instruction, so conditions go through a
normalising helper (`truthOperand`) and containers/strings are tested by *length*
(`rt_list_len` / `rt_dict_len` / `rt_set_len`, or the compile-time length of a
literal) rather than by handle. In the interpreter a condition asks the heap object
behind a handle, never the handle itself.

### Dicts & sets (LLVM codegen)

The AOT path also lowers **inline dict and set literals** with constant-key
indexing and `len`: `{1: 10, 2: 20}[1]`, `{1, 2, 3}[2]`, `len({1: 10, 2: 20})`.
Each dict/set literal is emitted as a dedicated global struct (`{i32 count,
[n x i32] keys, [n x i32] vals}` for dicts; `{i32 count, [n x i32] elems}`
for sets). Constant-key lookup resolves at compile time; like lists, these
literals must be used inline (no assignment-to-variable indirection) in the
codegen path. The interpreter indexes dicts/sets at runtime and is unchanged.

> **Subscripting a set asks the set a membership question, and that is a gusty extension, not Python.**
> `{1, 2, 3}[2]` asks the set whether `2` is one of its members and answers `2`; asking for a member it
> does not have is `KeyError: not in set`. CPython rejects the shape outright with
> `TypeError: 'set' object is not subscriptable` (with a syntax warning calling out the missing comma),
> so programs that use it are recorded `oracle: not_applicable` in the conformance ledger — the CPython
> leg cannot reach the rest of the file — which makes the extension a deliberate difference rather than
> an unnoticed one (ADR 0186, ledger rows `programs/data_b` and `programs/features_b`). Both backends
> read a set the same way at every depth, including a set held in a container slot (ADR 0251); neither
> one reads it by position, whatever an older draft of this paragraph said.

### Iterating and mutating containers

A container variable behaves like the Python equivalent, identically on the
interpreter and in AOT:

```py
d = {}              # {} is an EMPTY DICT (the empty set is set(), see below)
m = {1: "a"}        # non-empty braces with colons are a dict
s = {1, 2}          # braces without colons are a set

for k in d:         # a dict yields its KEYS, in insertion order
    print(k, d[k])

for x in s:         # a set yields its members
    print(x)

d["k"] = 1          # item assignment inserts, or updates an existing key
d["k"] = 2          # …and updating replaces the value, it does not append
xs = [1, 2, 3]
xs[1] = 9           # a list replaces the element: [1, 9, 3]
xs[9] = 0           # IndexError — item assignment never grows a list
```

Rules that both backends implement:

- **`{}` is an empty dict.** The interpreter used to classify it as an empty set (so
  `d = {}` then `d[k] = v` failed with `not in set`) while the AOT treated the same
  token as a dict — one token, two kinds.
- **Iteration yields elements; a dict yields keys.** The checker binds the loop
  variable to the element/key type, so `for k in d: s = s + k` is not flagged as
  `int + dict[...]`.
- **Item assignment.** `d[k] = v` inserts or updates (Python's semantics: never raises
  for a missing key). `xs[i] = v` replaces an element within bounds and raises
  `IndexError` otherwise — in AOT the bounds test is emitted around the store, so a bad
  index takes the exception path instead of writing past the elements. Assigning to a
  set element or a string index is rejected (`TypeError` / `strings are immutable`).
- **A negative subscript counts from the end — for positions only.** `xs[-1]`, `xs[-1] = v`,
  `xs.pop(-1)`, `l[-2:]` and `"abc"[-1]` mean what they mean in Python, and both backends and
  CPython agree (`programs/negative_index.gy`, ADR 0210). Dict and set subscripts are keys, so
  they keep their sign: `d = {-1: "minus"}` then `d[-1]` is `"minus"`, not the last entry. A
  container *literal* with a negative constant subscript is folded at compile time; that shape
  used to be a Go panic inside the compiler rather than an answer or a diagnostic.
- A container produced by a call (`d = make(3)`) is iterated through the runtime
  length, like any other container variable.
- **A subscript of a string is a one-character string** (ADR 0225). `s[1]` is `b`, not `98`:
  the character is text in both backends, so it compares with text, concatenates, takes methods, and
  `s[1] == 98` is false the way CPython says it is. A string is counted in **code points** wherever
  position is asked about — `s[i]`, `s[a:b]`, `len(s)`, `ord(s)` — so `len("café")` is 4 and
  `"café"[3]` is `é`; a byte-wise slice could also cut a character in half. An out-of-range character
  subscript traps as `IndexError` on both backends.

- **A string is an index into a table the runtime can add to** (ADR 0229, ADR 0230). A compiled
  string value is an index into the program's string table, and the table grows while the program
  runs — so an operation asked about at run time has somewhere to live, and nothing about the value
  changes: it prints, compares, subscript

  ```py
  def word():
      return "abc"

  i = 1
  print(word()[i])           # b   — a subscript of a call result
  print("a" + word()[i])     # bc  — a literal joined to a computed character
  print(word()[i:i + 2])     # bc  — bounds that are values, not constants
  print(word()[-2:])         # bc  — positions count from the end
  print(len(word()), ord(word()[1]))   # 3 98
  print(word()[1].upper())   # B
  for c in word():           # a, b, c — code points, not bytes
      print(c)
  print(str(7), str(-7))     # 7 -7
  ```

  Interning is by content, so a string built while running equals the literal that spells it — no
  special case in equality. An absent slice bound means the default end. Iterating binds the loop
  variable to each character as the loop produces it, over the loop's own counter, so assigning to
  the variable inside the body does not move the iteration (ADR 0196).

  Three limits belong to the compiled backend and are stated rather than approximated: case folding
  covers ASCII and `strip` trims the ASCII whitespace set (the interpreter folds and trims the full
  Unicode sets); `str(<float>)` refuses rather than truncate a float it cannot yet hold (L11.6);
  and `%s` formatting does not exist at all (Gap R.31). A program that creates more distinct strings
  than the table holds (4096) raises `RuntimeError`, which it can catch — the alternative, which used
  to happen, was silently reusing an entry and printing a different string than the program built.
- **Containers hold strings.** `xs = ["a", "b"]`, `xs.append("s")`, `xs[0] = "s"`,
  `s.add("q")`, `d["k"] = 1`, `d[1] = "v"`, `"a" in xs`, `for x in xs`, `len`, indexing
  and printing all work in both backends. **A string value is an index into a runtime interned
  table** (ADR 0224): the address of a literal belongs only to the places that ask for bytes — a
  `printf` format, an argument to `rt_str_*`, a compile-time fold — so `x == "hi"`, `"a" in xs`,
  `self.w = "hi"` and a method's `-> str` result are all i32-to-i32 operations. Interning a
  constant emits `call i32 @rt_str_intern2(i8* getelementptr(...@.strN...), i8* null)`, and
  printing an index reads the text back with `rt_str_ptr`, which is why `print(f"hi {n}")` can
  print `hi world` rather than `hi 0`. Earlier the value path returned the global itself, and every
  one of those shapes reached `llc` as `icmp eq i32 @.str1, %t1` or `ret i32 @.str1`: exit 2, the
  compiler blamed for an ordinary program.
  Interning is content-addressed, so two spellings of `"k"` are the same dict key, and text the
  runtime makes up itself (a `+` concatenation, a slice) is interned by `rt_str_*` so an index
  always names an entry. Printing
  picks the slot by context — raw text for `print(x)`, the Python repr form for elements
  inside a container — which is why `print(names)` gives `['ada', 'brin']` and
  `print(["it's"])` gives `["it's"]`, exactly as CPython does. Dicts track their key and
  value kinds separately, so `{1: 'one'}` and `{'k': 1}` both render correctly.

  One rendering difference remains by convention: a bare `True`/`False` prints as `1`/`0`
  in both backends (bools are untagged i32 values today, so `print(True)` and `print(1)`
  are indistinguishable). Inside containers strings are quoted as Python does; giving bools
  their own spelling needs a tagged bool representation, not just a printer (roadmap L.2).
  Sets iterate in **insertion order** in both backends — deterministic, and identical between
  them, where CPython's order comes from hashing. `{"q", "r"}` prints as `{'q', 'r'}` here.

### Strings across a function boundary

A string can be passed to a function, and the parameter behaves like any other string value:

```gy
def greet(name):
    print("hello", name)      # hello ada

def size(s):
    return len(s)             # 4

def fill(out, v):
    out.append(v)             # a helper may fill a container its caller created

names = []
fill(names, "one")
```

Supported in both backends: passing a string positionally, by keyword, or as a default; a
`str` annotation; `print(p)`, `len(p)`, `p == "text"`, `p != "text"`, forwarding `p` to another
function, storing it in a list/dict/set, using it as a membership needle, and returning it
(`print(echo("yo"))` prints `yo`). Comparison is cheap because interning makes equal text the
same index.

Not yet supported in the compiled backend, each reported as a compile diagnostic that names
the interpreter rather than failing in the verifier:

- **concatenation of a runtime string** (`s + "!"`) — building a new string needs a buffer
  allocation the runtime does not have yet;
- **string methods on a parameter** (`s.upper()`) — same reason;
- **arithmetic on a string** (`s + 1`) — the interpreter raises `TypeError`; compiled code
  refuses rather than computing with a table index. **Ordering is not in this list**: an
  ordering of two texts is answered by `strcmp` on the bytes behind the index (ADR 0248), and
  ordering a text against a number is a separate open trap (Gap R.85), and an ordering whose
  operand is a slot whose kind the object carries asks the tag which pair it was — two numbers,
  two texts, or the `TypeError` CPython raises (ADR 0250);
- **a parameter used as both a string and a number** (`f("a")` and `f(7)`) — guessing would
  print `7` through the string table, so it stays a diagnostic.

Before this was implemented, `d[1] = 2` did not work at all: the parser accepted the
statement, consumed `= 2`, and threw it away, so the program ran as if the line were
absent — on both backends, with no diagnostic.

### Container methods

```py
xs = [1, 2, 3]
print(xs.pop())        # 3   — removes and returns the last element
print(xs.pop(0))       # 1   — removes and returns index i
print(xs.pop(-1))      # 2   — negative counts from the end

ys = [4, 5, 6]
while ys:              # draining a container: the idiom pop() exists for
    print(ys.pop())

s = set()              # the empty set has no literal; `{}` is an empty DICT
print(s)               # set()
s.add(1)
s.add(1)
print(len(s))          # 1
s.discard(1)           # silent if absent …
s.remove(1)            # … remove() raises KeyError instead

d = dict()             # same as {}
d[1] = 9
lst = list()           # []
lst.append(7)
```

- `xs.pop()` on an empty list raises `IndexError: pop from empty list`; `xs.pop(i)` with a
  bad index raises `IndexError: pop index out of range`. Both are real exceptions, so
  `except IndexError:` catches them on either backend. In AOT the bounds test is emitted
  around the removal (the new `rt_pop` shifts the tail left and shrinks the length).
- `set()` / `list()` / `dict()` construct empty containers. `s.add` / `s.discard` /
  `s.remove` / `s.clear` are the set methods; `remove` raises `KeyError` where `discard`
  is silent, matching Python.
- **AOT limitation:** the one-argument copy forms `list(xs)` / `set(xs)` / `dict(d)` are a
  compile-time diagnostic naming the interpreter as the working backend (ADR 0166). Build
  the container with the empty constructor and add elements.

`import mod` loads `mod.gy`, evaluates it, and binds `mod` to a module
namespace. Top-level variables and functions of the module are accessed as
`mod.name` and called as `mod.fn(args)`. A module can itself `import` other
modules. Imports are evaluated in the interpreter (REPL/--eval path). The AOT backend supports **data imports**: `import mod` loads `mod.gy`, parses + analyzes it, and constant-folds the module's top-level global variables, so `mod.var` reads compile statically to constants. Module function dispatch and non-constant globals are deferred with a clear compile error. Modules may themselves `import other` (nested imports): the nested module's globals are folded recursively and resolve via `other.var` references. Module globals may be strings and use `+` string concatenation. Printing an imported string module global (`print(mod.str)`) emits `printf("%s", i8*)` and outputs the folded string. `len(mod.str)` returns the folded string's length. `ord(mod.str)` returns the folded string's first byte value. `reversed(mod.str)` returns the folded string reversed. Module globals may also be lists (`mod.list`), folded element-wise; `mod.list[i]` indexes into the folded list; `len(mod.list)` returns its length. `sorted(mod.list)` folds the sorted list. `reversed(mod.list)` folds the reversed list. Module globals may also be dicts (`mod.d`), folded key/value-wise; `mod.d[k]` indexes the folded dict by integer key; `len(mod.d)` returns its entry count. Indexed imported dict elements fold in arithmetic (`mod.d[k] + mod.d[j]`). Indexed imported list elements fold in arithmetic (`mod.l[i] + mod.l[j]`).

### On-disk stdlib modules

`import mod` resolves `mod.gy` on disk: first in the working directory, then in the
standard-library root (`stdlib/` at the repository root, or `$GUSTY_STDLIB_DIR` /
`gustyc --stdlib <dir>`). The bundled stdlib ships data-only modules that both the
interpreter and the AOT compiler fold as top-level constants:

- `import math` — `PI`, `E`, `TAU`, `PHI`, `SQRT2`, `LN2`, `LN10`.
- `import string` — `DIGITS`, `LOWERCASE`, `UPPERCASE`, `HEXDIGITS`, `WHITESPACE`, `PUNCT`.
- `import collections` — `EMPTY_DICT`, `EMPTY_LIST`, `ZERO`, `ONE`.
- `import json` — `NULL` (`None`), `TRUE` (`True`), `FALSE` (`False`).

Reads like `math.PI` resolve to the folded constant in both backends. The interpreter
also supports importing function-bearing modules (module functions dispatch at runtime);
AOT module-function emission is supported: imported modules that define functions are lowered as mangled defines (`mod$fn`), and `mod.fn(args)` calls plus sibling-module calls and bare-name module-global capture are handled by the AOT compiler.
### Functions

Lambda is an anonymous single-expression function: `lambda x: int: x + 1`.
It can be called inline `(lambda x: int: x + 1)(5)` or bound to a name
`f = lambda x: int: x * 2` and called `f(3)`. A lambda is lowered to a
closure exactly like `def`, so the AOT codegen emits an anonymous FuncDef
(`lambda_N`) at module level and a call to it.


```
def add(a, b):
    return a + b
```

- `def` introduces a function; parameters are `Name`s (optionally annotated).
- `return` returns a value; a body without `return` returns void.
- Calls are `name(arg, ...)`. Argument count is checked against the parameter
  list at semantic analysis; return type is inferred by binding parameter
  types to the call argument types and analyzing the body.
- A `def` nested inside a function body is a **closure**: it captures the
  enclosing scope at definition time and can be returned, stored in a
  variable, and called later (`m = add(1); m(2)`). Closures are implemented in
  the interpreter (REPL / `--eval` path).
- **Decorators** apply before a `def`, one per line: `@dec def f: ...` is
  equivalent to `f = dec(f)` at definition time. Multiple decorators apply
  bottom-up (`@dec1 @dec2 def f` -> `f = dec2(dec1(f))`). A decorator is a
  function/closure that takes the function value and returns the (possibly
  transformed) value bound to `f`. Decorators are implemented in the interpreter and in AOT: identity decorators are fully supported, the canonical wrapping decorator (a decorator that returns a nested closure capturing the decorated function) is supported in AOT via compile-time specialization; other transform decorators are rejected (ADR 0157).

### Gradual typing

Optional annotations appear on variables (`x: int = 1`), parameters
(`def f(x: int)`), and returns (`def f() -> int`). Annotations are enforced
at runtime in the interpreter: a value whose runtime kind is not assignable
to its annotation is a `type mismatch` error. `any` (dynamic) accepts
everything. Because the interpreter stores booleans as plain integers, `int`
and `bool` annotations accept either kind.

### Control flow

```
if cond:
    ...
elif cond:
    ...
else:
    ...

while cond:
    ...

for i in range(n):
    ...
```

- `if`/`elif`/`else` branch on an integer condition (0 is false).
- `while` loops while the condition is non-zero.
- `for ... in range(n)` iterates `i` from `0` to `n-1`.
- `for ... in range(a, b)` iterates `i` from `a` to `b-1`.
- `for x in 5:` iterates `0, 1, 2, 3, 4` — an integer on the right-hand side is a **repeat count**
  (`programs/for_int_count.gy`). The count may be a literal, a variable or an expression; `0` and any
  negative value run the body zero times, exactly like `range(0)`. Both backends have always agreed on
  this, which is what makes it a feature rather than a bug — but CPython refuses it
  (`'int' object is not iterable`), so it is a declared extension with its own conformance row rather
  than something an agent should discover by accident (ADR 0207, roadmap R.14). It compiles to the same
  counter loop as `range(n)` and is handy precisely where `range` has been claimed by the program itself
  (ADR 0205). In a comprehension the integer form works in the interpreter
  (`[x for x in 3]` → `[0, 1, 2]`) while the compiled backend asks for `range(3)`.
- `for x in [1, 2, 3]:` iterates the elements of a list literal — in the
  interpreter over a boxed list, and in the AOT codegen over an inline list
  literal (unrolled per element).
- Classes are supported: `class Name:` bodies contain methods (the first param
  is `self`), `Point(0,0)` instantiates (calling `__init__` if present),
  `obj.attr` reads/writes instance attributes, and `obj.method(args)` / `Cls.method(self, args)`
  dispatch methods.
- **Inheritance**: `class Child(Base):` makes `Child` inherit `Base`'s methods
  and `__init__`; method/attribute lookup on instances and classes walks the
  whole base chain (multi-level). An overridden method can delegate to the base
  implementation with `super()` (valid only inside a method; it resolves methods
  on the base class of the currently-executing class, bound to the current
  instance). Class support is implemented in the interpreter (REPL/--eval path).
- **Dynamic dispatch**: a method call on an instance whose class is not statically
  known (e.g. an instance returned by a function or passed as a parameter) resolves
  the method by the runtime class of the receiver. The interpreter dispatches on the
  actual instance's class; the AOT/codegen path supports statement-level dynamic
  dispatch on instances and direct method calls.
- **Operator overloading (dunder dispatch)**: binary operators dispatch to
  dunder methods on class instances. For `a OP b`, the interpreter calls
  `__add__`/`__sub__`/`__mul__`/`__truediv__`/`__floordiv__`/`__mod__`/`__pow__`
  on `a` when `a` is an instance, falling back to the reflected method
  (`__radd__`, `__rsub__`, `__rmul__`, `__rtruediv__`, `__rfloordiv__`,
  `__rmod__`, `__rpow__`) on `b`. Comparisons dispatch to `__eq__`/`__ne__`/
  `__lt__`/`__le__`/`__gt__`/`__ge__` (with the swapped comparison as the
  reflected fallback). This is currently an interpreter-only feature in the
  AOT/codegen path; see ADR 0139.
- `raise ValueError("msg")` raises a typed exception carrying a class name and an
  optional message. Built-in exception classes: `Exception`, `ValueError`, `TypeError`,
  `KeyError`, `IndexError`, `RuntimeError`, `StopIteration`, `ZeroDivisionError`. A bare
  `raise` raises `Exception`. `raise IndexError` (the class itself, no call) raises that
  class with no message, and a class derived from an exception is raisable too:
  `class MyError(Exception):` then `raise MyError("x")` / `except MyError:`.
- **Runtime errors are typed exceptions, so they are catchable.** A list index out of
  range (read *or* write) raises `IndexError`, a missing dict key raises `KeyError`, and
  assigning to a string index or set element raises `TypeError` — on both backends:

  ```py
  xs = [1]
  try:
      xs[5] = 2
  except IndexError:
      print("caught")
  ```

  (The interpreter used to abort on these instead of unwinding, and the AOT used to read
  back whatever memory sat at that slot.)

  **Arithmetic traps are raised the same way** (ADR 0212): division, floor division and
  modulo by zero raise `ZeroDivisionError`, with CPython's wording for the operation, so the
  handler that reads like Python's reads like ours:

  ```py
  try:
      print(7 % 0)
  except ZeroDivisionError:
      print("caught")          # both backends, and CPython
  ```

  | operation | message |
  |-----------|---------|
  | `a / 0` (ints) | `division by zero` |
  | `a / 0.0`, `1.0 / 0` | `float division by zero` |
  | `a // 0` | `integer division or modulo by zero` |
  | `7.0 // 0` | `float floor division by zero` |
  | `a % 0` | `integer modulo by zero` |
  | `7.0 % 0` | `float modulo` |

  A trap never prints a value: the compiled backend used to emit the instruction and keep
  walking, which printed `inf` for `1 / 0` and a fresh garbage integer for `7 % 0` (an
  unguarded `srem` does not fault on AArch64). Where a trap is not implemented the backend
  refuses with a message — it does not answer with a substitute value.

  **Every built-in trap carries its class** (ADR 0214). A trap that reported a message with no
  class was invisible to the language: `except TypeError:` cannot match what has none, and the
  traceback printed a bare sentence. The wording below is the reference implementation's, so one
  search finds the same phrase in either language:

  | shape | raised |
  |-------|--------|
  | `p.nope` on an instance without it | `AttributeError: 'P' object has no attribute 'nope'` |
  | `int("abc")` | `ValueError: invalid literal for int() with base 10: 'abc'` |
  | `float("zzz")` | `ValueError: could not convert string to float: 'zzz'` |
  | `a, b = [1]` | `ValueError: not enough values to unpack (expected 2, got 1)` |
  | `a, b = [1, 2, 3]` | `ValueError: too many values to unpack (expected 2)` |
  | `x = 5` then `x()` | `TypeError: 'int' object is not callable` |
  | `len(5)` | `TypeError: object of type 'int' has no len()` |
  | `x = 5` then `x[0]` | `TypeError: 'int' object is not subscriptable` |

  Several are refused at compile time by the AOT backend rather than trapping (an honest refusal,
  class 1: `int on non-integer string`, `len requires an inline list/dict/set literal`, `index of a
  non-literal variable` — messages that still need stable codes, roadmap L11.8). The missing
  attribute is the one shape where the compiled backend still answers with a value (roadmap Gap
  R.19), and it is pinned as `programs/probe_builtin_traps_untyped.gy` rather than left to be
  discovered.
- **An uncaught exception is reported and fails the process, identically on both
  backends.** The report goes to **stderr**, so `prog 2>/dev/null` sees only what the
  program printed, and the exit status is non-zero, so a script cannot mistake a trapped
  program for a successful one:

  ```console
  $ gusty prog.gy; echo $?
  Traceback (most recent call last):
  IndexError: index out of range
  1
  ```

  Both backends agree on the frame line for the raise itself —
  `  File "prog", line 3, in boom` — where `boom` is the enclosing function
  (`<module>` at top level). The interpreter prints one such frame per stack level; the
  compiled report shows the raise site's own frame until the call-stack line tables of
  L8.5 land.
- One deliberate divergence: an assignment whose *target kind* is known statically to be
  impossible (`s[0] = "z"` on a string, `s[0] = 1` on a set) is a compile-time diagnostic
  in the AOT backend (ADR 0166) and a catchable `TypeError` in the interpreter.
- **The arms of a `try` are tried in the order they are written**, on both backends
  (ADR 0213). `except ValueError:` catches exactly that class; `except Exception:` and a bare
  `except:` catch anything, wherever they appear in the list — an arm after them is unreachable,
  as in Python. An exception no arm matches is **not** dropped: it propagates outward — to an
  enclosing `try`, or out of the function — and if nothing handles it, it is reported and the
  program fails with the runtime class (exit 3). In the interpreter it surfaces to the caller as
  an `*EvalError` carrying `ExnType` and `ExnMsg`.

  ```py
  try:
      xs = [1]
      print(xs[5])
  except KeyError:
      print("not this")        # the IndexError below is matched by the arm after it
  except IndexError:
      print("second arm")
  ```

  The compiled backend used to lower only the first arm and then clear the exception flag, so a
  later arm never ran, a nested `try` never reached its outer arm, and an unmatched exception was
  deleted: no report, exit 0.
- **Once an arm accepts an exception, the exception is over** (ADR 0218). On both backends, the code
  after the `try` runs as if nothing had happened — the pending state does not survive the arm, in
  any direction the arm leaves by: falling through, `return`, `break` or `continue`. The compiled
  backend keeps that state in one module-wide flag, so an arm that accepted an exception without
  clearing it handed the program a second copy: the next call to a user-defined function found the
  flag still set and reported the exception again, after the handler had already run and printed.

  ```py
  try:
      crash = 1 // 0
  except:
      recovered = 1

  def f() -> int:
      return 5

  print(recovered, f())      # 1 5 — this died with an uncaught ZeroDivisionError
  ```

  An arm's own `raise` is a new exception and keeps travelling outward; an exception no arm matches
  still propagates (above), and an uncaught one still reports and fails.
- **`finally:` is a deferred body: it runs on *every* exit from its `try`** (ADR 0222). Its body
  runs when the `try` completes, when an arm handled the error, when an exception propagates out
  of it, and when the block is left by a `return`, `break` or `continue` — once per exit,
  innermost first, on both backends:

  ```py
  def f() -> int:
      try:
          return 1
      finally:
          print("fin")        # fin, then 1 — on both backends, as in CPython
  ```

  Three consequences, each with a test:
  - The **return value is taken by the `return` statement**, before the deferred body runs, so
    `return n` hands back the old `n` even if the `finally` reassigns it.
  - A `return` or a `raise` **inside the `finally` replaces** whatever transfer was in flight —
    Python's last-transfer-wins.
  - **An `except` arm catches exceptions, never a transfer.** `try: return 1` / `except: ...`
    does not run the arm: a `return`, `break` or `continue` is not an exception, whatever the
    implementation uses to move it.

  Known limit: a `try` written inside a **method** body is still broken in the compiled backend
  (it emits a branch to an empty label — roadmap Gap R.41, pre-existing and pinned).
- **A compound statement is not a scope** (ADR 0217). Python has one flat scope per `def` and one
  per module, so a name assigned inside a `try` body, an `except` arm, a `finally` clause, a
  `while` body or a `match` arm belongs to the enclosing function or module and is readable after
  the statement — on both backends:

  ```py
  def pick() -> int:
      try:
          a = 7
      except:
          a = 0
      return a          # 7 — `a` is a local of pick(), not of the try
  ```

  Pattern captures (`case y:`) bind the same way. The checker keeps the two questions apart: a name
  is **visible** from wherever some path binds it, and **definite** only where every path reaching
  the use binds it. A use of a visible-but-not-definite local gets
  `possibly unbound: "y" is not definitely assigned on all paths` warning — warnings do not fail
  `--check` — rather than the `undefined name` error the front end used to raise for programs that
  run fine. The runtime is what enforces it: reading such a name where nothing assigned it is a
  `NameError`. A name no path binds at all is still an `undefined name` error.

  ```py
  def f(x):
      match x:
          case 1:
              pass
          case y:
              pass
      return y          # warning: possibly unbound — correct, the literal arm skips the binding
  ```

  Reading a name that this run never assigned is a `NameError` in the interpreter, matching
  CPython; the compiled backend currently loads the untouched slot and prints its contents (roadmap
  Gap R.36).
- **The module is a scope too** (ADR 0220). A name a function reads is looked for in its own frame,
  then in the closure environment captured where the function was written, then in the **module** the
  function was defined in — at call time, so the binding may sit below the `def`:

  ```py
  def twice() -> int:
      return MAX * 2      # MAX is a module name, looked up when twice() runs

  MAX = 40
  print(twice())          # 80
  ```

  A method reads module names the same way, and so does a `def` nested inside another function (its
  global scope is the module its enclosing function was written in, not that function's frame). An
  assignment inside a body makes the name *local* to that body — the module keeps its own value — and
  a name nothing binds anywhere is still a catchable `NameError`, with the checker reporting
  `undefined name` for it.

  The compiled backend reaches module names in two shapes, and refuses the rest (ADR 0227). A name the
  module binds exactly once, to an integer/string/bool/`None` literal, and never rebinds is a *value*:
  a body that reads it is given that value, because a thing that cannot change needs no place to be
  read from. A name the module does rebind is module *state*, and it lives in a module global
  (`@gy_mod_<name>`) that the top level writes and any function, method or closure loads when it runs —
  so the lookup really does happen at call time:

  ```py
  LATE = 0

  def read() -> int:
      return LATE          # whichever value the module held when read() was called

  LATE = 3
  print(read())            # 3
  ```

  What a body still cannot reach is module *containers* (`len(xs)`, `xs.append(v)` for a module-level
  list) and module names holding floats: those are refused, with a message naming module state and the
  missing machinery rather than misdescribing the value (roadmap Gap R.35's remaining half, Gap L11.6).

- **A name the body binds is the body's own, and an unwritten read raises** (ADR 0228). Whether a name
  is local is decided by what the body binds *anywhere inside itself*, not by how far the text has got,
  so a read above the write does not quietly find the module's value:

  ```py
  def f(c):
      if c:
          x = 1
      return x          # f(False): UnboundLocalError, not 0

  print(f(True))        # 1
  ```

  A local assigned on only one path is unbound on the others, and gusty raises where the value would
  otherwise be whatever the frame previously held. The class follows the frame, as CPython's does: a
  name this frame owns but has not bound is `UnboundLocalError`, a name no frame owns is `NameError`
  (so a module-level `if 0: x = 1` then `print(x)` is a `NameError`). Both are ordinary raises —
  `try: … except UnboundLocalError:` catches them on the interpreter and on the compiled backend — and
  both exit 3, the trap code, whichever engine ran the program. A `for` body that runs zero times and a
  `match` that matches nothing leave their captures unbound the same way; a loop variable is bound
  inside its own body, as it must be. Names the checker can prove were assigned cost nothing: no flag,
  no test, one fewer branch.
- `while`/`for` loops accept an optional `else:` clause that runs on normal
  completion and is skipped when the loop exits via `break`.
- `break` exits the innermost loop; `continue` skips to the next iteration.
  Both are valid inside nested `if`/loop bodies. Using either outside a loop
  is a semantic error.

### Match

```
match x:
    case 1:
        ...
    case 2:
        ...
```

- `match` selects a case whose pattern equals the subject (integer equality).
- A `case _:` pattern is a wildcard and always matches.
- The first matching case body runs; then control continues after the match.
- Patterns are tried in order; the first one that matches wins.

#### Class patterns

`case Point(x, y):` matches a value that is an instance of `Point` (or any
subclass of `Point`) and binds the capture variables `x` and `y` to the
instance's attributes of the same names:

```
class Point:
    def __init__(self, x, y):
        self.x = x
        self.y = y
p = Point(2, 3)
match p:
    case Point(x, y):
        x + y      # -> 5
    case _:
        0
```

- The class may be referenced by name or via a variable holding a class value
  (e.g. `Alias = Point`, including a chain of them, and including a function
  parameter that receives the class). The class is resolved by the front end from
  the program text, so the arm means the same thing in either backend; a name that
  holds a value which is *not* a class matches nothing — the pattern does not call it.
- Each argument is an attribute name; the pattern looks up that attribute on
  the instance and binds a same-named capture variable to its value. A name that is
  not an attribute of the instance fails the case, as does an argument that is not a
  name.
- A missing attribute, or a subject that is not an instance of the class (or a
  subclass), fails the pattern and the next case is tried.
- Both backends lower class patterns (ADR 0235). The compiled one asks the instance
  whether it has each attribute — `@inst_set`, written by every attribute store and
  cleared when a heap slot becomes a new instance — because its data words cannot
  tell an attribute that was never written from a stored `0`. ADR 0149 said this was
  interpreter-only; it was, and `case Point(a, b):` on an instance with `x` and `y`
  answered `pt 0 0` compiled against the interpreter's `no`.
- A case whose pattern is a call — `case f():` — is expression-equality: the call's
  result is compared to the subject. It is not a class pattern, and the compiled
  backend used to load `f` as if it were a variable (ADR 0235).

## Expressions

- Integer literals `1`, `2`, `-3`.
- Float literals, bool literals (`true`/`false`), `none`, string literals.
- `Name` reads a variable (fresh SSA load per read for dominance safety).
- `BinOp` arithmetic (`+`, `-`, `*`, `/`, `//`, `%`) and comparisons (`<`, `==`, ...).
  An operator is a question about two runtime **kinds**, and a pair it has no rule for is a
  `TypeError`, never a number (see *Operand kinds* below).
- `and` / `or` are boolean operators: both operands are evaluated and the
  result is a `0`/`1` integer (`and` is 1 iff both are non-zero, `or` is 1 iff
  either is non-zero). Lowered in the AOT codegen to i1 logic zero-extended to
  `i32`, mirroring the interpreter.

  **`//` and `%` floor; they are one rule, not two operators.** Go's `/` and `%` truncate
  toward zero, so with mixed signs the answers differ: Python's `-7 // 2` is `-4` and `-7 % 2`
  is `1`, because the remainder carries the *divisor's* sign. The two are only correct together
  — the invariant is `a == (a // b) * b + (a % b)`, which a truncating pair also satisfies, so
  testing each against its own table can certify a wrong pair. Both backends now emit the
  correction (`floorDiv`/`floorMod` in the interpreter, an `sdiv`/`srem` plus a `select`
  adjustment in the module, `frem` plus `fadd` for floats), constant folding uses the same pair,
  and the whole sign grid is checked against CPython on both backends (roadmap Gaps R.28, R.30,
  ADR 0216). An exact float remainder keeps the divisor's sign — `7.5 % -0.5` prints `-0.0`.
- `Call` to user functions or builtins (`print`, `range`).
- Attribute access (`obj.attr`) and indexing are parsed for future features.

### Comparison

Numbers compare by value, strings compare by content, and **containers compare by value too**
(ADR 0189):

```python
[1, 2] == [1, 2]                 # True
{1, 2} == {2, 1}                 # True   — sets are unordered
{"a": 1, "b": 2} == {"b": 2, "a": 1}   # True   — so are dicts
[0] == ["zero"]                  # False  — the tag separates a number from a word
[1] == 1                         # False  — unequal, not a trap
```

An element is the `(payload, tag)` pair, and *both* halves must match: a stored string is an
index into the interned table, so the container holding the number `0` and the container holding
the first interned string would otherwise be called equal. That is why every operation that writes
a container slot writes its tag with it (ADR 0187), including the builders a call argument uses.

- **A number equals the same number written the other way** (ADR 0221): `1 == 1.0`, `1.0 == 1`,
  `0 == -0.0` and `3 == 3.0` are all True, in either operand order, on both backends. An `int`
  compared with a `float` is one question about two values, not a comparison of representations;
  the interpreter used to answer `1 == 1.0` with False while answering `1.0 == 1` with True.
  This coercion reaches only numbers: `1 == [1]` and `1.0 == "a"` are False, not errors.
- Lists compare position by position; sets and dicts by containment — a positional walk would make
  `{1, 2} == {2, 1}` False. Because element equality is this same predicate, `[1] == [1.0]` and
  `{"a": 1} == {"a": 1.0}` are True too.
- A container compared with a scalar is unequal (`[1] == 1` is False, not an error), and the
  operands are still evaluated, so `f() == xs` keeps `f`'s side effects.
- Comparing a container with something whose kind the compiler cannot see — `xs == make()` —
  **reports** rather than answering: "comparing a container with X needs a tagged value". That is
  L11.2's missing value tag, named where it bites.
- `is` / `is not` are the identity operators and always were: `xs is xs` is True, `xs is ys` is
  False for two equal lists, and class instances, closures and methods compare by identity, as in
  Python without `__eq__`.
- `!=` is the negation of `==`. It used to be a separate, unreflective handle comparison, which
  made `[1] == [1]` and `[1] != [1]` agree — both False.


### Operand kinds

An operator is a question about **two runtime kinds**. If the language has no rule for the pair it
raises `TypeError`; it never answers with a value. Before this rule the interpreter consulted no
operand kind at all, so a heap handle that reached an arithmetic path was added or multiplied as an
integer, and each of these printed a number and exited 0:

```python
print("a" * "b")           # was 1099516870662
print(1 + None)            # was 1048578
print([1] + 1)             # was 2097157
print({"a": 1} + {"b": 2}) # was 2097157
print("a" < 1)             # was 0
```

Each is now a `TypeError`, and the wording is the reference implementation's character for character —
the tests run a real `python3` and compare the report line rather than trusting a remembered string
(ADR 0215). Four shapes of refusal exist, and which one you get follows what the reference says, not
which internal branch noticed:

| situation | message |
|-----------|---------|
| the operator does not apply to these kinds | `unsupported operand type(s) for -: 'int' and 'str'` |
| a sequence got a non-int on the other side of `*` | `can't multiply sequence by non-int of type 'float'` |
| a sequence got the wrong kind on the right of `+` | `can only concatenate list (not "int") to list` |
| an order operator on unorderable kinds | `'<' not supported between instances of 'str' and 'int'` |
| a `%` format string with nothing to convert | `not all arguments converted during string formatting` |

`==`, `!=`, `in`, `not in`, `is` and `is not` are total — they compare, they do not compute, so
`1 == "a"` is False rather than an error. Ordered comparison (`<`, `<=`, `>`, `>=`) accepts two
numbers, two strings or two lists, and a list whose elements are not mutually orderable raises the
same report a top-level mismatch would: `[1] < ["a"]` is `'<' not supported between instances of
'int' and 'str'`.

What the rule *enables* is the sequence half of the table. Those two lines are the reason the gate
exists — `[1] + [2]` and `"ab" * 2` were handle arithmetic as surely as `"a" * "b"` was:

```python
[1] + [2]         # [1, 2]        "ab" * 2      # abab      "ab" * -1   # (empty)
[1] + [2] + [3]   # [1, 2, 3]     2 * "ab"      # abab      [1] * 0     # []
[1] * 3           # [1, 1, 1]     3 * [1]       # [1, 1, 1] "a" + ""    # a
```

A repeat count may be zero or negative, which yields empty rather than an error, and the operand
order does not matter. `"a" + ""` is listed on purpose: the old concatenation path used "the string
is empty" as the test for "the operand is not a string", so the empty string was not a string.

**Why an `int` and an object were ever confusable.** Interpreter values are an untagged `int64`: a
heap handle and a program's own integer share one space, and the only test was "does this number name
a live object". The heap started at `1 << 20`, so an ordinary loop reached into object space — at
`i = 1024`, `self.x * self.x` is `1048576`, an accumulator reached the class's own *method object*,
and `s + p.norm()` became int-plus-method. `heapIDBase` is now `1 << 48`, far outside anything
arithmetic produces, and `isHandle` is the single predicate answering "is this value an object?" for
the collector, for operator dispatch and for every kind test, so those cannot disagree about what a
value is. Real tagged values are roadmap L11.1; until then this is the boundary, and a program would
have to compute an integer near 2.8×10^17 *and* land exactly on a live object id to cross it.

## FFI / C interop (`extern fn`)

Gusty can call C library functions by declaring them with `extern fn`:

```text
extern fn abs(x: int) -> int
extern fn getpid() -> int
extern fn strlen(s: str) -> int
print(abs(-5))        # 5
print(strlen("hello")) # 5
```

- An `extern` declaration has no body; it is a C prototype. The AOT codegen
  emits a `declare` for it in the LLVM IR and lowers calls: `int` arguments
  marshal to `i32`, string-literal arguments marshal to `i8*`, and the native
  `i32` return is used directly as the value. The existing `cc` link pipeline
  resolves the symbol (stdlib functions like `abs`/`getpid`/`strlen` need no
  extra libraries).
- The AST interpreter dispatches extern calls to a small Go registry mirroring
  the C stdlib (`abs`, `getpid`, `rand`, `strlen`); other externs raise a clear
  "not available in the interpreter" error.
- An `extern fn` keeps the **C name** in the emitted module — `declare i32 @strlen(i8*)`,
  called as `@strlen` — because that name is exactly what the declaration binds. Functions the
  program itself defines do not share that namespace: they are emitted as `gy_<name>` (ADR 0198),
  so declaring `extern fn abs` and defining `def abs` are two different declarations, and only
  the extern reaches the C library.
- Arity and argument types are checked at compile time.

## Types

- `int`, `float`, `bool`, `str`, `none`, `void`, and `any` (dynamic).
- Unannotated variables infer to `any`; annotated variables pin their type.

### The dynamic value model

Behind the annotations, a value at runtime is one of fifteen kinds, and there is exactly
one table that says which (ADR 0182): `int`, `float`, `bool`, `None`, `str`, `list`,
`dict`, `set`, `tuple`, `class`, `instance`, `method`, `closure`, `exn`, `module`. The
interpreter's heap objects, the compiled runtime's tagged values and the extern-fn ABI all
read those numbers; the compiled heap's own object-header kind is a projection of them
(`list`, `dict`, `set`, `instance`, with 0 meaning "the compiled backend does not allocate
this — it is an immediate or an interned string"). `gustyc --lang` prints both tables, and
`--schema`'s `valueTag` definition documents the numbering.

`str(x)` of a value the compiler can fold at compile time produces the same text on both
backends and the same text CPython prints: `str(None)` is `"None"` (not `"0"`), `str(1.5)` is
`"1.5"`, and `str("x")` is `x` — `str()` is the unquoted form, so it is not `repr()`. A folded
string may be printed but never stored as a global; where it is stored, the text is interned
and the handle kept (ADR 0183).

A **container slot is an i32 word**, so the element kinds a compiled container can hold are the kinds
a word can carry: integers, interned strings (`@str_tab` indices, ADR 0224) and `None` — each with its
own tag (ADR 0187/0189). A **float does not fit**, so `xs = [1.5]`, `print([1.5, 2])` and
`[1] == [1.0]` are refused with a message naming the element kind and the item that owns the fix
(roadmap L11.6), never emitted as an instruction LLVM has to reject (ADR 0166). The refusal replaces a
silence that was worse than either: the fold truncated floats to words, so `{1.5} == {1.6}` and
`{"a": 1.5} == {"a": 1.6}` compiled to **True** and `print(xs[0])` of `[1.5]` printed `1`
(roadmap Gap R.40, ADR 0226). The interpreter answers all of those correctly today.

An operator between **two operands of different runtime kinds** is not a numeric question:
`1.0 == [1]` is False, as CPython answers it, decided by kind rather than by coercing a container
through a float conversion (ADR 0215, with ADR 0221's numeric cross-kind pair as the deliberate
exception). Ordering a number against a container is a `TypeError` in CPython and is refused today,
because this backend cannot raise a runtime `TypeError` yet (roadmap Gap R.37).

A compiled list can hold numbers, interned strings and `None` together: each element slot
carries its own tag, so `print([1, "a", None])` gives `[1, 'a', None]` on both backends and on
CPython (ADR 0184). What a mixed list may hold is decided by what the tag can honestly describe
— integers, interned strings, `None` — so `[True, "a"]`, `[1.5, "a"]` and `[[1], "a"]` are still
reported rather than mis-printed.

A slot's tag is written by whatever writes its payload, never afterwards (ADR 0187). That covers
building a literal, `xs.append(v)`, and `xs[i] = v` — the last of which used to store the payload
and leave the tag alone, so `xs = [1, "a", None]; xs[0] = "z"; print(xs)` answered
`[1, 'a', None]`: the interned index of `"z"`, printed through the slot's stale `int` tag.

Reading one element out produces the pair as well, and three uses of it are open:
`print(xs[i])` dispatches on the tag, `xs[i] == [1, 2]` and `2 in xs[i]` compare through
`rt_container_eq`, and `v = xs[i]` binds a *tagged variable* — the same
`(value, tag)` binding a loop variable gets, so `print(v)` renders `str()` (`a`) while
`print(xs)` renders `repr()` (`'a'`), and rebinding `v = 5` retires the tag (ADR 0185, ADR 0187).
What still reports — rather than computing on a string-table index — is any context that needs
one static kind: `xs[i] + 1`, `xs[i] > 2`, passing `xs[i]` to a function, `xs[i]` in a format
spec.

**A container inside a container is read back the same way** (ADR 0239, ADR 0241). The slot holds
the inner object's *handle*, so the second subscript is a load from the object the first one named,
and every use of it works — on both backends, at CPython's answer:

```gy
xs = [[1, 2], [3, 4]]
print(len(xs[0]))        # 2
print(xs[0][1])          # 2
print(1 if xs[0] == [1, 2] else 0)   # 1
print(1 if 3 in xs[0] else 0)         # 1
for row in xs[0]:                     # 1, 2
    print(row)
d = {"a": [1, 2]}
print(d["a"][1])         # 2
y = xs[1][0]
print(y)                 # 3 — the tag travels with the binding, so print(y) needs no guess
```

**A slot the compiler can see holding a number is that number** (ADR 0243), so the same reads reach
arithmetic and comparison too:

```gy
xs = [1, "a"]
print(xs[0] + 1)                      # 2
print(xs[0] * 3, xs[0] - 1, -xs[0])   # 3 0 -1
print(1 if xs[0] > 2 else 0)          # 0
ys = [1.5, "a"]
print(ys[0] + 1, ys[0] * 2)           # 2.5 3.0 — the slot is read as a float, not an int
zs = [10, "a"]
print(zs[0] / 4, zs[0] // 3, zs[0] % 3)   # 2.5 3 1
def twice(v):
    return v * 2
print(twice(xs[0]))                   # 2 — an element is an argument
t = [[1.5, "x"], 2]
print(t[0][0] + 1)                    # 2.5 — one level down, same answer
```

A numeric use is the one use that does not need the tag, and where the container is one the program
spelled out and never changed, the compiler already knows what the slot holds — so the element itself is
compiled, and the int and float paths the language already has run on it. That is sound only for a
**literal** element: an element that is a *name* would be read at the point of use, and the name may have
been rebound since the list was built (`a = 1; xs = [a, "b"]; a = 5; print(xs[0] + 1)` is `2`, not `10`),
so a name-filled element is not folded — it keeps the refusal, which says which promise the fold needs.

The permission is the tag, remembered at compile time: a name qualifies while it is bound exactly once
to a container literal and nothing has changed that object. Rebinding it, writing `xs[0] = …`, calling
`append`/`sort`/`add`/`update`/`pop`, or handing the container to a function the compiler cannot see all
take the name out of that set — and the read then **refuses with a message that names the promise that
ran out** (`cannot reach into xs's slots: the name was rebound, mutated, or handed to code this pass
cannot see …`) instead of reading a payload as a handle. Reading a payload as a handle while it holds a
number is `[[5]]` where the program wrote `[[1, 2]]`, and that is the class of answer this language does
not ship (ADR 0233, ADR 0241).

A slot read through an **index the program computes** is answered too (ADR 0249): the read brings its
(payload, tag) pair to the arithmetic, and the tag decides whether to unbox a float, convert an int or
bool, or raise the `TypeError` CPython raises for that operator and that kind.

```gy
xs = [1.5, "a"]
i = 0
print(xs[i] + 1)        # 2.5  — the float slot unboxes; it is not its heap handle
print(xs[i] / 2)        # 0.75
print(-xs[i])           # -1.5 — negation is its own operator, with its own message
total = 0.0
for j in [0, 0]:
    total = total + xs[j]
print(total)            # 3.0
xs[1] + 1               # at run time: TypeError: can only concatenate str (not "int") to str
```

`*` and `%` are the two operators the door steps back from over a container that can hold **text**, and
the reason is that CPython does not raise there at all: `xs = [1.5, "a"]` / `i = 0` / `print(xs[i] * 2)`
repeats the text for one index and multiplies for another, and a door whose only vocabulary is `raise`
would answer the first wrongly. Over a container that cannot hold text both are answered — `xs = [1.5,
2.5]` / `print(xs[i] * 2)` is `3.0` — and what they are waiting for is repetition and `%`-formatting
themselves (Gap R.33, Gap R.31).

What the tag cannot settle is the **kind of the result**, which the compiler has to know before the program
runs. A container whose slots are ints here and floats there (`xs = [1, 2.5]`) is therefore refused rather
than widened: `xs[i] + 1` would print `2.0` where CPython prints `2`, and that is a different value, not a
near miss. The same honesty covers a container that was **built rather than spelled out** (`xs = [];
xs.append([7, 8]); print(xs[0][0])`), which no literal ever described.

The interpreter — boxed values, no static tag needed — answers all of these, which is what keeps the
refusals roadmap rows rather than mysteries (roadmap L11.1, `docs/roadmap-details.md`). One exception is
recorded rather than papered over: **unary minus does not consult a tag in either backend**, so `print(-"a")`
answers `-281474976710658` in the interpreter and `0` compiled, where CPython raises
`TypeError: bad operand type for unary -: 'str'` (roadmap Gap R.89).

**Dicts and sets take the same rule** (ADR 0232). A compiled dict may mix kinds on either side of
an entry and a compiled set may mix kinds among its members, because a slot is always the pair
(payload, tag):

```gy
d = {"a": 1, "b": "x", "c": None}   # values: number, string, None
print(d)            # {'a': 1, 'b': 'x', 'c': None}   — both backends, and CPython
print(d["b"])       # x        — the value's slot says it is a string
for k in d:         # a, b, c — keys carry tags too
    print(k)
s = {1, "a", None}
print(s)            # {1, 'a', None}
s.add("b")          # a member arrives with its tag; discard moves the tags down with the members
print(1 if 1 in s else 0)          # 1
```

A dict or set of a single kind is unaffected: it builds through the plain runtime calls, prints
through the static printers, and sets no "my slots describe themselves" bit on the object.

What a mixed container may hold is decided by what a tag can honestly describe — integers,
interned strings, `None`, floats, bools (stored as the number both backends store them as), and
another container. A needle whose kind the compiler cannot prove reports *when the container mixes*
— `1 in s` where `s` is `{1, 'a'}` and the needle is a call whose return kind nobody knows.

A **float** in a slot is the handle of a *float box* (ADR 0238): a slot is one `i32` word and a
double does not fit in one, so the bits live beside the heap and the tag says the payload is a box.
That is what lets the compiled path answer the whole family, byte for byte as the interpreter and
CPython do:

```gy
xs = [1.5, 2.5]
print(xs)                    # [1.5, 2.5]
print(1 if 1.5 in xs else 0) # 1
d = {1.5: "x", 2.5: "y"}
print(d[2.5])                # y — a float key is found by value, not by handle
print(1 if [1] == [1.0] else 0)   # 1: across the two numeric tags, equality is numeric
print([None, 1.5, "a"])           # [None, 1.5, 'a'] — used to be [0, 1, 1073]
ys = [1]
ys.append(1.5)
print(ys)                    # [1, 1.5] — the list stops claiming a kind when it grows out of one
```

Equality of two slots is one question with one answer (`rt_payload_eq`): same tag compares payloads,
int against float compares numbers — `-0.0` equals `0.0`, and NaN is unequal to itself. Container
equality, dict lookup and set dedup all ask it, so they cannot disagree about which entries are the
same. A `for` loop unrolled over a literal carries the element's tag with it, so `print(v)` of a float
or `None` element prints the value rather than the box (ADR 0238).

And the tag is not only for mixed containers, which is the part that was wrong before: **a lookup
compares the payload and the tag whenever the needle's kind is provable**, uniform container or
not. Strings are `@str_tab` indices, so an interned string and an integer of the same number are
the same bits, and this program used to have a compiled answer of `one`:

```gy
d = {1: "one"}
print(d["a"])   # KeyError, on both backends and on CPython — used to print `one` compiled
```

The same rule covers `"a" in s`, `1 in ["a"]`, and the `KeyError` a missing `d[k]` raises
(ADR 0189 wrote the tags; ADR 0232 made the runtime read them).

A container **inside** a container is the same trick one level down (ADR 0239): the slot holds the
inner object's handle and its tag says `TagList`/`TagDict`/`TagSet`, so the *object* — not the
builder that saw a literal — decides how it prints (`rt_print_container_value` asks the inner
object's kind and hands it to the printer that reads its own tags) and how it compares (a
container pair goes to `rt_container_eq`, which compares slots with the one payload rule above).
Nothing new is stored: a handle is still one `i32`, and the collector already marks every element
word of a marked object.

```gy
print([[1, 2], [3, 4]])                    # [[1, 2], [3, 4]] — used to report; a mixed one used to
                                           # print the interned indices of its inner strings
print([[1, "a"], [2, "b"]])                # text stays text inside a container
print([[1, 2]] == [[1, 2]])                # 1 — two literals build two objects; content decides
print(1 if [1, 2] in [[1, 2], 3] else 0)   # 1 — a container needle is matched by content
print({"a": [1, 2], 1: {2, 3}})            # {'a': [1, 2], 1: {2, 3}}
print([[1, 2], [3, 4]][0])                 # [1, 2]
for row in [[1, 2], [3, 4]]:
    print(row)                             # [1, 2] then [3, 4] — a loop variable carries its tag
xs = [[1], [2]]
xs.append([9])
xs[0] = [7]
print(xs)                                  # [[7], [2], [9]]
```

**A slot of a container the program *built* is read by asking the object** (ADR 0246). Appending to a
list, assigning into a dict, or rebinding the name takes the literal out of the compiler's hands — ADR
0241's read is a compile-time promise, and it runs out — but the object still knows: every writer went
through ADR 0187's payload-and-tag door, so `len` of a slot asks the slot's own tag and measures the text
in characters, the container in its own entries, and refuses to measure a number at all:

```gy
xs = []
xs.append([7, 8])
xs.append("abc")
print(len(xs[0]), len(xs[1]))              # 2 3   — used to be refused: "len cannot reach into xs's slots"
d = {}
d["a"] = [1, 2, 3]
print(len(d["a"]))                          # 3     — the dict was built by assignment, not spelled
xs.append(5)
print(len(xs[2]))                          # TypeError: object of type 'int' has no len() — as CPython
```

**…and one level below it the same door reads the slot** (ADR 0251). `len(xs[0])` asks the object what
the slot holds; `xs[0][0]` has to ask the same question and then *read* the answer back. The tag written
beside the outer slot names what kind of object its payload is, and the arm it selects is the read that
kind supports — a position, normalised and bounds-checked like any other; a key, with its `KeyError`; a
character of a text, with `IndexError: string index out of range`; or, for a set, the membership question
this language documents for a set subscript. What the tag reports as having no slots at all raises the
sentence CPython raises for that kind. The value that comes back is itself a `(payload, tag)` pair, so a
binding, an equality, a `len` and a further subscript all take it: no literal ever described these
objects, and nothing here is a guess about what the number happens to be.

```gy
xs = []
xs.append([7, 8])
xs.append({"k": 5})
xs.append("abc")
print(xs[0][0])                             # 7      — a list slot is read by position
print(xs[1]["k"])                           # 5      — a dict slot by its key
print(xs[2][1])                             # b      — a text slot gives a one-character string
d = {}
d["a"] = [1, 2]
print(d["a"][1])                            # 2      — the dict was built by assignment
y = xs[0][1]
print(1 if y == 8 else 0)                   # 1      — the pair travels with the value
i = 1
print(xs[0][i])                             # 8      — the position may be computed too
xs.append(5)
print(xs[3][0])                             # TypeError: 'int' object is not subscriptable — as CPython
```

**…and an ordering of such a slot asks the object the same question** (ADR 0252). `<`, `<=`, `>`, `>=`
have three answers — two numbers compared as numbers, two texts compared by their characters, and the
`TypeError` CPython raises for a pair that does not order at all — and for a container the program built
rather than spelled out, *which* of the three it is is a run-time question the tag answers. Two numbers are
lifted (`rt_float_of` out of a float slot, `sitofp` for an int or a bool) and compared; two texts go to the
`strcmp` helper the literal ordering already uses; anything else raises, and because the sentence names both
operand types (`'>' not supported between instances of 'list' and 'int'`) the raise is one test per kind the
writers can leave beside a payload, each with its own wording — `int`, `bool`, `float`, `NoneType`, `list`,
`dict`, `set`, text. It can end in an `else` only because that set of tags is closed: nothing outside it is
ever stored. The verdict is a bool whichever way the operands fall, which is exactly why an *ordering* can be
answered this way and a `+` cannot (below).

```gy
xs = []
xs.append(3)
xs.append("a")
xs.append([1, 2])
print(1 if xs[0] > 1 else 0)                # 1      — two numbers
print(1 if xs[1] > "A" else 0)              # 1      — two texts, by their characters
print(1 if xs[2] > 1 else 0)                # TypeError: '>' not supported between instances
                                            #          of 'list' and 'int' — the slot's own kind named
d = {}
d["k"] = 5
print(1 if d["k"] >= 5 else 0)              # 1      — the dict the program filled
xs2 = []
xs2.append([3, "a"])
print(1 if xs2[0][0] > 1 else 0)            # 1      — one level below, through the same door
print(1 if "a" < xs2[0][0] else 0)          # TypeError — CPython names the left operand's type first
```

**…and one arithmetic operator may ask it for a number** (ADR 0253). Everything else that needs a
number from a slot the literal never described still refuses, honestly, because the answer's kind is a
fact about the data: an `int` slot makes `xs[0] + 1` a `4` and a `float` slot makes it a `4.5`, and the
module has to be written before the slot is asked. **True division is the exception** — `/` is a float
whatever arrives, so the one thing the compiler must know in advance is settled, and the tag can be asked
for the rest. A float slot unboxes out of its `@float_box`, an `int` or `bool` slot converts, and every
other kind raises CPython's own sentence for this operator and this kind — the same closed set of tags the
ordering walks. Dividing by zero is trapped by the compiler rather than by `fdiv` (which answers ±inf), and
the trap lives *inside* each arm, because which `ZeroDivisionError` wording the pair earns is itself a
question about the operand kinds: `3 / 0` is `division by zero`, `1.5 / 0` is `float division by zero`.

```gy
xs = []
xs.append(3)
xs.append(1.5)
xs.append("b")
print(xs[0] / 4)                            # 0.75   — an int slot converts
print(xs[1] / 2)                            # 0.75   — a float slot unboxes
print(6 / xs[0])                            # 2.0    — the read may sit on either side
print(xs[0] / 4.0)                          # 0.75   — or the other operand may be a float
nested = []
nested.append([4])
print(nested[0][0] / 2)                     # 2.0    — one level below, through the same door
d = {}
d["a"] = 6
print(d["a"] / 3)                           # 2.0    — the dict the program filled
print(xs[2] / 2)                            # TypeError: unsupported operand type(s) for /: 'str' and 'int'
ys = []
ys.append(0)
try:
    print(5 / ys[0])                        # a zero slot traps, with CPython's own wording
except ZeroDivisionError:
    print("no")                             #          and the program catches it — exit 3, not 1
```

What the door still declines is written in words, never in a broken module: two sides the object would
have to describe (`xs[0] / ys[0]`, roadmap Gap R.101), a result the compiler cannot settle (`xs[0] ** 2`
where the slot may be a float), and a double handed to a context that stores an `i32` word — a call
argument, `str()`'s argument, a dict slot written by key, `+=` onto a variable that started life an `int`
(roadmap Gap R.98). Printing, a comparison, a condition, a float binding and a container element are in
the double domain, and take the answer directly.

**…and an equality asks it the same question** (ADR 0247). The tag was already carried to the printer,
so `print(out[1])` rendered `a` while `out[1] == "a"` was refused — one read, two doors. Both sides of
`==`/`!=` are now `(payload, tag)` pairs, and the one equality the container comparisons already use
(`rt_payload_eq`) answers: within a tag by payload, across the two numeric tags numerically, container
slots by content. Retired with this is the helper that compared the tags and then the words, which
called two float slots holding `1.5` unequal because they were two box handles:

```gy
xs = [1, "a"]
print(1 if xs[1] == "a" else 0)             # 1   — was refused: "needs a single static kind"
print(1 if xs[0] == xs[1] else 0)           # 0   — interned text and its index stay different values
i = 1
print(1 if xs[i] == "a" else 0)             # 1   — the position the program computes is a position
xs2 = []
xs2.append([1, 2])
print(1 if xs2[0] == [1, 2] else 0)         # 1   — a container slot equals an equal container
```

What still reports, with the mechanism it is missing named: a **dict keyed by a container** (Python
raises `unhashable type: 'list'`; a **set** does not even that yet — it admits the member and reports a
length, Gap R.81), and a tagged element whose kind only the run time can tell — a loop variable over a
mixed list used as a number, an element of a container the program built used **as a number**
(`xs[0][0] + 1`, `-xs[0][0]` — the result could be `4` or `4.5`, and the module has to be written before the
slot is asked; `xs[0] / 4` of such a slot answers `0.0` today instead of refusing, which is measured and owed
as Gap R.96), **two** such slots ordered against **each other** (`xs[0] > ys[0]` compares payloads where
CPython raises — one side whose kind comes from the object is a chain, two is a table the compiler would be
inventing, Gap R.97), a comparison against an expression whose kind cannot be proven (Gap R.83), a
**membership** test or a **loop** whose haystack is such a slot (`7 in xs[0]`, `for v in xs[0]` after
`xs.append([7, 8])`, which need the object's *kind* where the read asks only its tag), or a **set variable**
subscripted directly (`sa[0]`, which the interpreter answers by the documented extension and the compiler
declines, Gap R.94) — all of which need the value word that carries its own tag (roadmap L11.1). A fold (`sum`,
`min`, `max`) over container elements
reports too, rather than reaching for the elements' addresses the way CPython raises a `TypeError`.

The tag is what makes a value's kind a fact rather than a guess. What it does not buy yet is a
value that *is* a tag: a compiled float still has no word to hold it (L11.6), a bool still prints
as the number it is stored as — `print(True)` says `1` (L11.2) — and a container inside a
container is still a handle in a slot built for a word (L11.1 (5), the tagged value word, which
also collapses the parallel tag array into the value itself).

### Generics / structural protocols

Annotations are recursive generic type expressions:

- `list[T]`, `dict[K, V]`, `set[T]`, `tuple[...]`, and nested `list[list[int]]`.

### Union types

- A union annotation `int | str`, `int | float` accepts any value assignable
  to a member.
- A conditional whose branches carry different concrete types widens to their
  normalized union: `(1 if c else "hi")` infers `int | str`, which satisfies a
  union annotation but not either single member.
- Arithmetic over a union widens by membership: an all-numeric union
  (`int | float`) computes without warning; a mixed union (`int | str`) still
  warns `arithmetic on non-numeric`; `+` over a string-only union is
  concatenation.
- **AOT tagged-union lowering** — a union-annotated scalar variable
  (`int | float`, `int | str`) gets a tagged `%unionbox` slot: assignment stores
  the runtime member tag (0=int, 1=float, 2=string), and `print` dispatches on
  the live tag to emit `%d`/`%f`/`%s`. Cross-member reassignment under
  branches/loops prints the currently-stored member, matching the interpreter.
- `Sequence[T]` — a structural protocol bound accepting any list/set/iter/tuple/str
  whose element type is compatible with `T`.
- `Iterator[T]` / `Iterable[T]` — a lazy producer of `T` (same spelling as
  `Sequence[T]`'s read-only role; produced by `range`, generator expressions
  and `yield from`).
- `Callable[[A, B], R]` — a structural protocol bound accepting any function whose
  arity, parameter kinds, and return type match; a bare `fn` reference is accepted.
- `Animal` (a bare class name) — a NOMINAL class type. `a: Animal` accepts an
  instance of `Animal` or of any subclass, and the class hierarchy is visible
  even when the annotation appears before the `class` declaration.

### Variance (L6.6)

Every generic constructor declares a variance for each of its type parameters.
`gustyc --variance` prints the table as JSON; the checker implements exactly
that table.

| constructor | variance | why |
|---|---|---|
| `list[T]` | **invariant** in `T` | lists are writable through every alias |
| `set[T]` | **invariant** in `T` | sets support add/remove |
| `dict[K, V]` | **invariant** in `K` and `V` | dicts support insertion/overwrite |
| `tuple[...]` | **covariant** (arity fixed) | tuples are immutable |
| `Sequence[T]` | **covariant** in `T` | read-only: elements only flow out |
| `Iterator[T]` | **covariant** in `T` | a producer accepts nothing back |
| `Callable[[P...], R]` | **contravariant** in `P`, **covariant** in `R` | a handler is substitutable only if it accepts everything the destination will pass |
| `class C` | **nominal** | a value reaches a class position only as that class or a subclass |

In practice:

```python
class Animal:
    def speak(self):
        return 1
class Dog(Animal):
    def speak(self):
        return 2

def feed(a: Animal) -> int:      # nominal: any Animal or subclass
    return a.speak()

def count_animals(xs: Sequence[Animal]) -> int:   # covariant: read Dogs as Animals
    return len(xs)

def handle(f: Callable[[Animal], int]) -> int:    # contravariant: f must take ANY Animal
    return f(Animal())

dogs: list[Dog] = [Dog()]
print(count_animals(dogs))       # OK  — covariance widens
print(feed(Dog()))               # OK  — nominal subclass
print(handle(feed))              # OK  — feed accepts every Animal
```

and the unsound directions are rejected with a named rule, a stable code and a
suggestion:

```python
cats: list[Cat] = [Cat()]
animals: list[Animal] = cats     # type.variance.invariant — use Sequence[Animal]
handle(dog_only)                 # type.variance.contravariant — widen dog_only's parameter
dogs: list[Dog] = [Dog()]
count_dogs(dogs)                 # OK — covariance widens
all_animals: list[Animal] = [Animal()]
count_dogs(all_animals)          # type.variance.covariant — a base is not a Dog
```

A *freshly built* container literal is an exception to invariance: nothing
aliases the new object yet, so its element type may widen to the destination —
`x: list[int | str] = [1]` is accepted (mypy-style contextual inference), while a
genuinely wrong element (`x: list[int] = ["a"]`) is still rejected.

The type checker uses a structural `subType(got, want)` relation at assignment,
call-argument, and return checks (with `assignable` as its boolean front-end):
concrete kinds keep exact-kind equality, `any` tolerates anything in either
position, protocol kinds recurse on element/param/return shape, and class kinds
walk the declared base chain. Every rejection carries a `code` (see
`docs/operations.md`) and an actionable `suggestion`.

The interpreter enforces the same annotation rules at runtime for nominal class
parameters (`a: Animal` rejects a `Rock`), so a violation fails fast in the
REPL; the AOT backend treats annotations as static-only, exactly like the rest of
the annotation surface.
- Arithmetic on non-numeric operands reports a diagnostic (suppressed inside
  untyped function bodies, which fall back to dynamic dispatch).

## Builtins

Names are resolved by the checker before codegen runs. Every built-in call name comes from
one table (`pkg/lang/predeclared.go`), which the checker predeclares, the language server
offers in completions, and the AOT codegen consults before lowering a bare name — so a real
built-in is never reported as "undefined", and a name that is *not* bound is a front-end
error with a span rather than an invalid LLVM module:

    print(undefined_thing)
    → error at 1:7: undefined name "undefined_thing"        (exit 1, both backends)

If a built-in exists in the language but cannot be lowered yet, codegen refuses with an
actionable message naming the backend that does support it (ADR 0166), e.g.
`list(<container>) copies are not supported in the AOT backend yet; the interpreter supports
them — build the container with list() and add elements`.

`type` is a reserved word (annotations), so `type(x)` is not a call in this grammar and is
not offered in completions.

- `print(x, ...)` — writes each argument to stdout on its own line via
  `printf`. Multi-argument `print` mirrors the interpreter: one `printf` per
  argument. String arguments (literals, folded string calls like `str(7)`,
  and constant `+` concatenations) use a `%s\n` format; integer arguments
  use `%d\n`. Zero-argument `print()` writes nothing (no `printf`), matching
  the interpreter's no-op.
- `range(n)` — iteration bound for `for` loops.
- `str(x)` — converts a value to its string representation. In the
  interpreter, `str(x)` boxes `repr(x)` as a string; in codegen, `str(int)`
  folds to the decimal string constant and `str(float-constant)` folds to its
  `%g` decimal string (matching the interpreter's `repr`), so `print(str(3.5))`
  emits a valid `%s` printf with the string-global pointer rather than a `%d`
  printf fed an `i8*`. `len(str(...))` also folds.

## Generators & lists
- `def g(): yield a; yield b` is a generator: calling `g()` runs the body and
  returns a list of all yielded values.
- `[1, 2, 3]` is a list literal.
- `for x in g():` and `for x in [1,2,3]:` iterate the elements.

- `len(list)` returns the number of elements in a list.

## Strings

String literals (`"..."`) evaluate to boxed strings in the interpreter:

- `+` concatenates strings: `"a" + "b"` → `"ab"`
- `len(s)` returns the character count
- `print(s)` writes the string to stdout

Example:

    s = "hello"
    print(s)          # hello
    print(len(s))     # 5

String literals support three extra forms:

- **Raw strings** `r"..."` / `r'...'` (also `R` prefix): backslashes are kept literally, so `r"a\nb"` is the 4-byte value `a\nb`. A backslash immediately before the quote keeps the string open, so the quote appears in the value.
- **Triple-quoted strings** `"""..."""` / `'''...'''`: may span multiple lines; newlines are part of the value, and escapes are processed like ordinary strings.
- **Raw triple-quoted strings** `r"""..."""` / `r'''...'''`: a raw string that may span multiple lines with backslashes preserved.

Docstrings may be written in any string-literal form; a triple-quoted docstring preserves its multi-line content and is extracted into `__doc__`.
    print("a" + "b")  # ab

The AOT LLVM codegen also lowers **string-constant concatenation and `len`**
at compile time: `"a" + "b"` folds to a single string constant, and
`len("hello")` / `len("ab" + "cd")` fold to their character counts (`5`, `4`).
Semantically `+` on two strings is typed `str` (no arithmetic warning).

## Floats

Float literals (`1.5`, `2.0`) evaluate to boxed floats in the interpreter.

Arithmetic with floats (or float + int) produces a float:
`+`, `-`, `*`, `/`. Comparisons (`== < <= > >=`) work between floats and ints.
`print` renders floats with `%g`.

The interpreter applies `-`/`*` to the float64 payload of boxed floats
(not the raw heap handles), so `a = 1.5; b = 2.0` gives `a-b == -0.5`,
`a*b == 3.0`, `b-a == 0.5` — matching the AOT codegen's `fsub`/`fmul` IR.
Float `//` floors the quotient (`5.5 // 2.0 == 2.0`), `%` uses `math.Mod`/`frem`,
and unary `-` negates the payload (`-a == -5.5`), matching the AOT `llvm.floor.f64`
and `fsub double 0.0` paths.

    print(1.5 + 1)   # 2.5
    print(7.0 / 2.0) # 3.5
    print(1.5 > 1)   # 1

## Comprehensions

List comprehensions iterate a range or list, bind a loop variable, apply an
optional filter (`if`), and collect the element expressions.

    xs = [x * 2 for x in range(3)]   # [0, 2, 4]
    zs = [y for y in [1, 2, 3] if y > 1]
    d  = {k: k * 10 for k in range(2)}

Comprehensions over a `range(...)` work on both backends.

In the AOT LLVM codegen, a comprehension over an inline list literal or a constant `range(...)`
folds, and **a folded set/dict comprehension is the literal it denotes** (ADR 0234): the `@.setN` /
`@.dictN` global is emitted *from* the `SetLit` / `DictLit` the fold produced and never escapes into
a value position — a binding, a `print`, an `in` test or a call argument takes the literal's own
lowering, which allocates a heap object and writes each slot with its tag. Set comprehensions
unroll the iteration and deduplicate folded elements; dict comprehensions fold key/value pairs.
Both are usable with `len(...)` via the `compLen` map, mirroring the interpreter's semantics. A
comprehension whose iterable is a runtime container walks it with the same real loop `for` uses.

**A brace display ends at its brace, and a comprehension's element is one value**
**(both engines) (ADR 0244)** — Two rules, one row of output. *A `{…}` display* — a
set or dict literal — *ends at its `}`*: a `for` written after the closing brace
belongs to whatever encloses the display, not to the display itself. The parser
used to read `for` after `}` unconditionally, so `[{1,2} for x in [1]]` parsed as a
list holding one set comprehension, `[{x} for x in [1,2]]` collapsed to `[3, 3]`
(one shared object appended twice), and — the reason this was not merely cosmetic —
`{1,2} for x in y` was *accepted* where Python raises `SyntaxError: invalid
syntax`. A `{…}` display in an expression now only takes a trailing `for` when it
is not sitting in a `[…]` element position, which is exactly the call-argument form
`len({x*x} for x in xs)` that needs the display to stay open. (That form's *answer*
is still ours rather than CPython's — CPython raises `TypeError: object of type
'generator' has no len()` there, and matching it would mean rejecting the
`f(<expr> for x in it)` call form the conformance corpus uses; ADR 0244 records it
as a deliberately unpaid divergence rather than a silent one.)

*A comprehension's element is compiled as one value*, payload and tag written
together through the same door `xs.append(v)` uses, so an element that is itself a
container, a float, `None` or text arrives as one object rather than as whatever
the compiler's most convenient global happened to be. `[[1,2] for x in [1]]`
prints `[1, 2]` where it printed an interned string's characters, `[1.5 for x in
[1]]` prints `[1.5]` where it printed `[1]` (a box *handle* read as an int),
`[None for x in [1]]` prints `[None]` where it printed `[0]`, and `["a" for x in
[1]]` prints `['a']` where `print(xs)` wrote bare `a` — the fold had interned the
*variable*'s row as a string while its element was a list. The fold that turns a
constant-seeded comprehension into an inline literal may no longer answer a value
question with a truthiness answer: an element that folds to an integer it did not
literally contain (`None`, text, a float) is compiled through the runtime loop
instead, which tags it correctly. An element that *is* the loop variable registers
the list it builds with the kind its slots hold — asked of the container being
iterated, not of the loop variable, whose own facts die with the loop — so
`print(out)` and `print(out[0])` give one answer (this closes Gap R.46, whose pin
at the wrong output is now a parity case). What refuses, honestly and naming
itself: a comprehension over a name the
compiler kept as a compile-time list (there is no heap object to walk, and emitting the load is the
module `llc` rejects — ADR 0192), a comprehension whose *filter* reads a
container slot — the `cannot reach into …'s slots` refusal rather than a guess. Comparing a slot of a
run-time-built mixed list with text (`out[1] == "a"`) is answered: the pair the printer carries is the
operand the comparison needed (ADR 0247, Gap R.79).

**A comprehension's loop variable carries its element's tag** (ADR 0245, Gap R.76). Iterating a container
whose slots hold more than one kind binds the same `(payload, tag)` pair `for` binds (ADR 0185), so the
member arrives as what it is rather than as whatever its i32 happens to equal:

```gy
sa = {1, "a", None}
print([x for x in sa])            # [1, 'a', None]  — was [1, 0, 0]: an index and a fold's integer
d = {}
d["a"] = 1
d[2] = "b"
print([k for k in d])             # ['a', 2]         — a dict is walked by its entries, keys only (ADR 0188)
print({k: 1 for k in d})          # {'a': 1, 2: 1}   — each entry is two (payload, tag) pairs
```

An element that is a text loop variable keeps its text kind into the container it builds — the list is
registered from the iterated object, so `print(out)` and `print(out[0])` tell one story — and a dict
comprehension over a text dict writes keys tagged as text, which is what lets `out["a"]` find the entry
the printer already named (Gap R.46, Gap R.78).

Comprehension results are **indexable** exactly like their literal
counterparts, resolved at codegen time:
- `(d for ...)[key]` on a **dict comprehension** folds to the mapped constant
  value (keys recorded via `compKeys`), erroring `key not found` when absent.
- `(s for ...)[key]` on a **set comprehension** is a membership test returning
  the element when present, erroring `not in set` otherwise.
- `(l for ...)[i]` on a **list comprehension** is the positional GEP+load.
This mirrors the interpreter's `Index` handling for dict lookup and set
membership.

### min / max / abs

Standard-library numeric builtins:

    min([3, 1, 2])   # 1
    max([3, 1, 2])   # 3
    min(1.0, 2)      # 1.0
    max(1, 2.5)      # 2.5
    min("b", "a")    # a
    max([1, 2.5])    # 2.5
    abs(-5)          # 5

`min`/`max` accept one list or set, one scalar, or values written side by side; `abs` takes one number.
- The varargs form chooses the **candidate**, not the comparison that found it, so the answer's kind is
  the winner's own kind. `min(1.0, 2)` is the float `1.0`; `min(2.5, 1)` is the integer `1`, not `1.0`.
  A tie keeps the first candidate (`min(1, 1.0)` is `1`). The rule holds whether the candidates are literals
  or settled variables **of one kind**, and the same rule decides the one-container spelling (`max([1, 2.5])`
  is `2.5`, `min([1, 2.5])` is `1`). Text candidates are ordered by their content through `rt_str_order`,
  never by their position in the intern table (ADR 0248).
- A text candidate beside a number, and a `None` or container candidate beside a number, raises CPython's
  `TypeError` with the operator that actually failed: `min` names `'<'` and `max` names `'>'`. The raise is
  catchable on both engines. Two containers side by side are CPython's element-wise ordering, which this
  language does not implement yet (roadmap Gaps R.86, R.97): the interpreter raises, the compiler refuses.
  Two runtime candidates whose kinds only the object can reconcile still wait
  for the tagged value word (roadmap Gaps R.107–R.110), and so does a settled `float` beside a settled `int`;
  those refuse in words rather than promote the winner to a double or compare untagged payloads.
- The AOT codegen treats a single **numeric** scalar argument to `min`/`max` as a
  one-element collection: `min(5)` -> 5, `max(7)` -> 7 (see ADR 0110). A lone text or `None` is the
  interpreter's one-element collection too, and the compiled half refuses it (Gap R.107/R.108's family).
Implemented in both the interpreter (REPL/`--eval`) and the LLVM AOT codegen.
In the AOT path `min`/`max`/`sum` fold over an **inline list literal** (unrolled
`icmp`+`select` / `add` chains over the list's global struct). Each element is
lowered via `g.value`, so runtime-variable elements (`min([a, b])`) work
exactly like integer literals. `len([a, b])` returns the element count
directly, and `[a, b][i]` evaluates the indexed element via `g.value` —
runtime-variable elements also work in `len` and list index. `abs` accepts any
integer expression and is constant-folded when its argument is a literal.


`sum`/`min`/`max` also fold set literals, dict literal keys, and dict-method

`len`/`sum`/`min`/`max`/`any`/`all` also consume a **list-returning builtin call** over an inline list literal. `sorted`/`reversed` preserve the element set (only reorder), so `len(sorted([3, 1, 2]))` -> 3, `sum(sorted([3, 1, 2]))` -> 6, `min(sorted([3, 1, 2]))` -> 1, and `max(reversed([3, 1, 2]))` -> 3 — all folding identically over the underlying elements. The AOT codegen unwraps the underlying list literal for these consumers, and `any`/`all` widen their boolean accumulator to i32 so results are printable.

`len` also folds over further list-producing builtin calls:
`len(enumerate([a, b, c]))` -> 3, `len(zip(a, b))` ->
`min(len(a), len(b))`, `len(s.partition(sep))` -> 3, and
`len(s.split(sep))` / `len(s.rsplit(sep))` -> occurrences(sep in s) + 1 —
all matching the interpreter.
calls (`.keys()` / `.values()`), so `max({1: 2, 3: 4}.keys())` -> 3.
- The AOT codegen folds constant-index element access into list-producing
  call expressions: `keys()`, `values()`, `sorted(...)` (including
  `reverse=True`), `reversed(...)` and `split(sep)` emit the exact selected
  element constant (e.g. `{1: 10, 2: 20}.keys()[0]` -> 1,
  `sorted([3, 1, 2])[1]` -> 2). `partition()` and `items()` remain
  compile-time errors (their parts are unrepresentable nested/string
  elements in the integer-element list model).

### string methods

**Escape sequences** are decoded the way Python decodes them (ADR 0178): `\n`,
`\t`, `\r`, `\a`, `\b`, `\f`, `\v`, `\0`, `\\`, `\'`, `\"`, hex `\x41` → `A`,
`\u00e9` → `é`, `\U0001F600` → `😀`. An escape this list does not know (`\q`)
is kept **verbatim**, backslash included — Python does the same, and dropping the
backslash (which an earlier lexer did) silently rewrote every `"a\nb"` to `anb`.
A malformed numeric escape (`\xZZ`, a truncated `\u00`, a surrogate, a code
point past Unicode) is likewise kept as written rather than guessed at. Raw
strings `r"..."` keep backslashes; triple-quoted strings decode escapes and may
contain literal newlines. All three forms share one decoder, as do f-string
literal parts.

**String values are UTF-8 text, measured in bytes.** A literal's value is the
source's own bytes (`"café"` is 5 bytes, and `print` round-trips it), so
non-ASCII text survives lexing, containers, interning, and printing. Length,
indexing, and slicing measure **bytes**, not code points, in both backends:
`len("café")` is 5 where CPython says 4, and `"héllo"[1]` yields the byte 195.
That is a tracked divergence, not an accident — code-point semantics are a
representation decision for `len`, `s[i]`, `s[i:j]` and `for c in s` in both
backends at once, and a half-migration would break interpreter/AOT parity
(roadmap Gap N.2).

String equality `==` / `!=` compares contents in both paths (`"abc" == "abc"`
is 1, `"abc" == "abd"` is 0), constant-folded in the codegen.
String indexing `"abc"[1]` returns the char code (98 for 'b') in both the
interpreter and the AOT codegen (constant-folded).
Substring membership `"cat" in s` is a substring test, not container membership;
in the AOT backend both sides are interned table indices and the scan runs in
`@rt_str_contains`.
`.split()` (space-separated) folds to the substring count in the codegen
(`len("a b c".split())` -> 3). Boxed strings support Python-style methods:

    "heLLo".upper()      # HELLO
    "heLLo".lower()      # hello
    "  hi  ".strip()     # hi
    "a b c".split(" ")   # [a, b, c]
    "aXbXc".replace("X", "-")  # a-b-c

`split` takes an optional separator (default space). `.replace(old, new)`
replaces every occurrence of `old` with `new` in **both** paths: the
interpreter evaluates both arguments as strings and applies
`strings.ReplaceAll`; the codegen constant-folds it when the receiver and
both arguments are string constants (`print("aXbXc".replace("X", "-"))`
emits the folded global "a-b-c").

`.find(sub)` returns the index of the first occurrence of `sub`, or -1 if
absent, in **both** paths: the interpreter applies `strings.Index`; the
codegen constant-folds it to an `i32` literal when the receiver and argument
are string constants (`print("abcabc".find("bc"))` emits `i32 1`).
`"s".ljust(w)` pads the receiver on the right with spaces to width `w`;
`"s".rjust(w)` pads on the left; both are no-ops when `len(s) >= w`.
In the AOT codegen, both fold over a constant string receiver and constant
width to a padded string global, mirroring the interpreter.
`"s".index(sub)` folds to the byte index of `sub` in the receiver
`"s".expandtabs(w)` folds to a string global: each tab is replaced with the
`{k: v}.get(key, default)` folds to the matching value or the default in the
AOT codegen: an int/string key looks up the constant dict's key/value pairs
(not-found returns the default when given).
spaces to the next tab stop at width `w` (running-column algorithm). Source
string literals have no escape sequences, so literal receivers contain no
tabs (no-op); the interpreter can build tabs via `chr(9)`.
(`strings.Index`); the interpreter raises on not-found, but the AOT codegen
has no error channel, so it folds to `-1` on not-found (like `find`).
`"s".zfill(w)` pads the receiver on the left with `0` to width `w` (no-op
when `len(s) >= w`); the AOT codegen folds it over a constant receiver and
constant width to a zero-padded string global, mirroring the interpreter.
`"s".removeprefix(p)` strips the given prefix from the receiver and
`"s".removesuffix(s)` strips the suffix (no-op when unmatched, mirroring
`strings.TrimPrefix`/`TrimSuffix`); the AOT codegen folds both over a
constant receiver and constant prefix/suffix to a string global.

`.startswith(sub)` and `.endswith(sub)` return 1 or 0 in **both** paths: the
interpreter applies `strings.HasPrefix`/`strings.HasSuffix`; the codegen
constant-folds them to `i32 1`/`i32 0` when the receiver and argument are
string constants.

`.count(sub[, start[, end]])` returns the number of non-overlapping
occurrences of `sub` within `s[start:end]`, mirroring Python's
`str.count(sub, start, end)`. The interpreter applies `strings.Count` on the
sliced substring (`start`/`end` clamped to `[0, len(s)]`); the AOT codegen
constant-folds only the one-argument form to an `i32` literal when the
receiver and argument are string constants (`print("ababab".count("ab"))`
emits `i32 3`).

`.rfind(sub)` returns the index of the last occurrence of `sub`, or -1 if
absent, in **both** paths: the interpreter applies `strings.LastIndex`; the
codegen constant-folds it to an `i32` literal
(`print("abcabc".rfind("bc"))` emits `i32 4`).

`.capitalize()` uppercases the first rune and lowercases the rest in
**both** paths via a shared `capitalize` helper: the interpreter calls it
directly; the codegen folds it in `stringConst`/`stringVal` and the call
dispatch (`print(len("hello".capitalize()))` emits `i32 5`).

`.title()` capitalizes the first rune of each whitespace-separated word in
**both** paths via a shared `title` helper (`strings.Map` tracking the
previous rune), folded in `stringConst`/`stringVal` and the call dispatch
(`print(len("hello world".title()))` emits `i32 11`).

`.swapcase()` swaps the case of each rune in **both** paths via a shared
`swapcase` helper (`strings.Map` using `unicode.IsUpper`), folded in
`stringConst`/`stringVal` and the call dispatch
(`print(len("HeLLo".swapcase()))` emits `i32 5`).

List `.count(value)` returns the number of occurrences of `value` in the
list (interpreter path; elements compare by string content via
`dictKeyEq`, matching dict keys). `["a", "b", "a"].count("a")` is 2.

`.isdigit()` returns 1 if every rune is a digit (and the string is
non-empty), else 0, in **both** paths: the interpreter checks
`unicode.IsDigit`; the codegen folds it to `i32 1`/`i32 0`
(`print("123".isdigit())` emits `i32 1`).

`.isalpha()` returns 1 if every rune is alphabetic (and the string is
non-empty), else 0, in **both** paths: the interpreter checks
`unicode.IsLetter`; the codegen folds it to `i32 1`/`i32 0`
(`print("abc".isalpha())` emits `i32 1`).

`.islower()` / `.isupper()` return 1 if there is at least one cased rune
and all cased runes are lowercase / uppercase, else 0, in **both** paths:
the interpreter scans cased runes; the codegen folds to `i32 1`/`i32 0`
(`print("abc".islower())` emits `i32 1`).

`.partition(sep)` returns a list `[head, sep, tail]` split at the first
occurrence of `sep` (or `[s, "", ""]` when absent) in the interpreter
path. `"a-b-c".partition("-")[0]` is "a".

`.isalnum()` returns 1 if every rune is alphanumeric (and the string is
non-empty), else 0, in **both** paths: the interpreter checks
`unicode.IsLetter`/`unicode.IsDigit`; the codegen folds it to
`i32 1`/`i32 0` (`print("abc123".isalnum())` emits `i32 1`).

`.isspace()` returns 1 if every rune is whitespace (and the string is
non-empty), else 0, in **both** paths: the interpreter checks
`unicode.IsSpace`; the codegen folds it to `i32 1`/`i32 0`
(`print("   ".isspace())` emits `i32 1`).

The `sorted(list)` builtin returns a new sorted list; the argument keeps its own
order, because `sorted` sorts a copy. `sorted(iter, reverse=True)` (or a truthy
positional second arg) returns descending order; `reverse=False` keeps ascending
order. `list.sort()` sorts in place and returns `None`; `list.reverse()` reverses
in place and returns `None`. All four take no `key=`/`reverse=` argument on the
method yet — `sort(key=...)` needs first-class functions and refuses, naming that
reason (roadmap L11.7, ADR 0191).

Ordering is defined per element kind, and both backends implement the same rules:
numbers compare numerically (an int and a float mix fine, as Python's `<` does),
and **strings compare by their text** — never by the interned index the value is
stored as, which records the order the strings first appeared in the program
(ADR 0248: `print(1 if "b" > "a" else 0)` is `1` on both paths, and the same is true of
`a > b` on two text variables, of a slot read, and of a comparison through a parameter;
the sorter has compared by content since ADR 0173, and the operators were brought into
line with it — including through the tag, so a text in a slot of a container the program
*built* orders by its characters too, ADR 0252). Ordering a text against a number is a `TypeError` in Python, which the
interpreter raises and the compiled backend still answers with a verdict — that is
Gap R.85, not this rule. Equality of texts stays an index comparison, because interning
is content-addressed (ADR 0173).
A list whose elements are of more than one kind cannot be ordered: Python raises
`TypeError: '<' not supported between instances of 'str' and 'int'`, the
interpreter raises the same, and the compiled backend refuses with a message that
names Python's answer. Sorting is a stable insertion sort on both paths, which is
what a future `sorted(key=)` (decorate–sort–undecorate) will be built on.

In the AOT codegen, `sorted` of an all-integer inline list literal is still
constant-folded to a sorted list global; anything else — a variable, a list of
strings, an empty list — is built as a runtime list and sorted by `rt_sort`, with
`rt_list_copy` first when the argument is a variable. `sorted(xs)` assigned to a
name binds a container, so `print`, indexing, `len` and `for` all work on it.
`reversed(x)` returns a
reversed copy of a list or string: `reversed([1, 2, 3])` -> `[3, 2, 1]`,
`reversed("abc")` -> `"cba"`. `reversed` is interpreter-only on non-literal
arguments (the AOT codegen constant-folds it only on literal list/string
arguments); see ADR 0082.
`enumerate(x)` returns a list of `[index, value]` pairs for each element of a
list: `enumerate([10, 20, 30])` -> `[[0, 10], [1, 20], [2, 30]]`. It is
interpreter-only (the AOT codegen folds builtins only on literal args; nested
list construction is not yet lowered); see ADR 0083.
`zip(x, y)` combines two lists into a list of `[a, b]` pairs, stopping at the
shorter list: `zip([1, 2], [10, 20])` -> `[[1, 10], [2, 20]]`. It is
interpreter-only (the AOT codegen folds builtins only on literal args; nested
list construction is not yet lowered); see ADR 0084.
`for x in "abc"` iterates over each character of a string (yielding a
single-char string per rune), **in both backends** (`programs/for_string_chars.gy`).
The compiled loop unrolls one body copy per character and stores each one the way a
container slot does — as its interned index — so `for c in "ab"` and a literal list of
strings compile and print like the interpreter and CPython do (ADR 0208; it used to be
interpreter-only, ADR 0085, because the unrolled store handed a string global to an `i32`
slot and LLVM rejected the module).

A character is a one-character **string**, not a distinct char type, and strings are byte
sequences today: `len("café")` is `5` and `"héllo"[1]` is the byte `195` (roadmap L11.5,
`programs/unicode_text.gy`).
`int(x)` converts a value to an integer: `int("42")` -> 42, `int(3.9)` -> 3
(float truncation). `float(x)` converts to a float: `float("2.5")` -> 2.5,
`float(3)` -> 3.0. `int` ships in both backends: the AOT codegen folds `int` on
literal int/string args to a compile-time constant. `float` is
interpreter-only (the AOT codegen has no float representation); see ADR 0086.
## Runtime heap for mutable lists (AOT)

When a program assigns a list literal to a variable (`x = [1, 2]`), the AOT
backend emits a small runtime heap (`@heap`, `@heap_count`) and boxed list
objects. `x.append(v)` mutates the heap object via `rt_append`, `len(x)` on a
list-variable reads the mutated length via `rt_list_len`, `x[i]` reads a
mutated element via `rt_get_elem` (including a runtime index variable, e.g.
`x[a]`), and `print(x)` for a list-variable renders
the live contents via `rt_print_list`. Rebinding a list variable (`x = [...]`
again) frees its old heap slot via `rt_free` into a free-list that `rt_alloc`
recycles, so long-running programs don't leak heap slots. Rebinding a list var
to a non-list value (`x = [1,2]; x = 5`) also frees its slot. Inline list
literals (`len([1,2])`, `print([1,2])`) keep compile-time folding. Local dict
literals (`d = {1: 10}`) also allocate runtime heap dict objects: `d[k]` reads
via `rt_dict_get`, `len(d)` via `rt_dict_len`, and `print(d)` via
`rt_dict_print`. Inline dicts in expressions (`{1: 2}.keys()`) and module-
imported dicts stay compile-time globals. Local set
literals (`s = {1, 2}`) allocate runtime heap set objects: `len(s)` via
`rt_set_len` and `print(s)` via `rt_set_print`. Inline sets in expressions and
module-imported sets stay compile-time globals. Rebinding a var across collection
kinds (list -> dict -> set -> scalar) frees the old heap slot via `rt_free`,
so long-running programs don't leak slots on rebind. When the 1024-slot heap
is truly full (more live collections than slots), `rt_alloc` returns a -1
sentinel instead of writing out of bounds, so pathological programs can't
corrupt memory. See ADR 0009.

## Containers across function boundaries (AOT)

A list, dict or set is a *reference*: passing one to a function must give the
callee the same live object the interpreter would (see ADR 0161). Both backends
agree, so all of these print the same thing under `--eval` and `--file`:

```python
def total(xs) -> int:
    n = 0
    for x in xs:          # iterates the handle, not a range
        n = n + x
    return n

def head(xs) -> int:
    return xs[0]

def size(s: set[int]) -> int:
    return len(s)

nums: list[int] = [1, 2, 3]
print(total(nums))                     # variable argument
print(total([4, 5, 6]))                # literal argument
print(total(xs=[1]))                   # keyword argument
print(head(nums))                      # indexing a parameter
print(size({1, 2, 3}))                 # set parameter
print(total([x * 2 for x in [1, 2]]))  # comprehension argument

def with_default(xs=[7, 8]) -> int:
    return len(xs)

print(with_default())                  # default argument
```

How it works in the AOT backend (ADR 0161, ADR 0163):

- **At a binding** an assigned container is a live heap object, whatever its shape —
  a literal, a comprehension (`ys = [x * 2 for x in [1, 2]]`), a generator call, or a
  variable. A comprehension that the compiler constant-folded into a global is copied
  into the heap rather than stored as a global, so `print`, `len`, indexing, iteration
  and passing it to a function all see the same container the interpreter would. At
  module scope the definition also gives the variable its slot and its `gc.roots`
  entry, which is what a later `xs.append(i)` stores through.
- **At the call site** a container *literal* is materialised into the runtime
  heap (`rt_alloc` + `rt_set_elem` / `rt_set_add` / `rt_dict_put`) and passed by
  handle. A literal that the compiler constant-folded into a global is copied
  into the heap instead of being passed as a global.
- **In the callee** a parameter is typed as a container by a whole-module
  inference: its annotation (`list[T]`, `set[T]`, `dict[K, V]`, `Sequence[T]`,
  `Iterator[T]`), its default value, or *any* call site that hands it a
  container. Knowledge propagates along call chains, so a function that merely
  forwards its parameter (`def doubled(xs): return total(xs)`) is enough to make
  `total`'s parameter a container too. A container parameter is rooted for the
  GC like any local.
- Without that inference the old codegen silently treated the handle as an
  integer and compiled `for x in xs` into a `0..handle` range loop — the same
  program, different answers per backend.
- Strings cross a function boundary in both backends: `greet("ada")` interns the argument and
  the callee receives the index (ADR 0174). What the compiled backend still cannot do is report
  itself rather than miscompile — concatenating a runtime string (`s + "!"`), a string method on
  a parameter (`s.upper()`), arithmetic on a string, and a parameter used as both a
  string and a number. Each names the interpreter, which supports all four; see
  § Strings across a function boundary. Ordering two texts is no longer one of them: an
  interned index orders by `strcmp` on the text behind it (ADR 0248).
- Strings are ordinary container elements in both backends (ADR 0173, ADR 0175): lists,
  dictionaries and sets of strings, written as literals (`["a"]`, `{"a": 1}`, `{"a", "b"}`) or
  built with `append` / `add` / item assignment, and printed the way Python renders `repr`.
- **A container that grows a second kind is promoted, not refused.** A compiled container used to
  record one kind for everything it holds, so `xs = [1]; xs.append("a")` was reported rather than
  run, and — worse — `d = {"a": 1}; d["b"] = "x"` *relabelled* the whole dict and printed the number
  1 as the string whose interned index happens to be 1 (`{'a': 'x', 'b': 'x'}`, where the answer is
  `{'a': 1, 'b': 'x'}`). Writing one slot is a statement about that slot; it is not a statement about
  the others. A contradiction now promotes the container to describing its own slots, which is sound
  because every word that ever reached a slot arrived with its tag (ADR 0189, ADR 0232). So
  `xs = [1, 2]; xs[0] = "s"` prints `['s', 2]` — where it printed `['s', 'b']` — and item assignment
  stays allowed for exactly that reason.
- A container element whose kind the compiler cannot prove is reported, not guessed. A function that
  returns text on one path and a number on another is the case: `print` asks what the value is when
  it prints and gets it right, but a slot is labelled once, and the number labelled as text came out
  of the string table as `(null)` (ADR 0232). The tagged value word (roadmap L11.1 (5)) asks that
  question per element and retires the refusal.

## `with` context managers

- `with expr as name:` binds `name` to `expr.__enter__()` for the body; `with expr:`
  discards the entered value.
- On normal completion `__exit__(none, none, none)` is called; on an exception
  `__exit__(exc_type, exc_val, exc_tb)` is called and a truthy return suppresses it.
- Both backends: `with expr as name` runs the protocol and the body identically
  interpreted and compiled (`programs/async_effects.gy` and the `with` cases in the
  conformance corpus pin the output; ADR 0181 closed the generator-rooting defect that
  used to block the compiled leg).

## `yield from`

- `yield from expr` delegates yields to a sub-iterable (a generator call, a list
  literal, or `range(...)`), appending each element to the current generator.
- Full support in the interpreter; codegen emits a runtime loop over the sub-list.

## Async: `async def`, `await`, `async for` (L5.6, L7.1; the await/return discipline L7.6, ADR 0195)

`async def` is a **deferred** function. Calling it runs nothing: it builds a coroutine
object, and the body runs when that coroutine is `await`ed — exactly once.

```gusty
async def double(x):
    return x * 2

a = double(3)          # nothing has run yet
print(await a)         # 6 — the body runs here
print(await double(4)) # 8 — created and awaited in one expression
```

- `await e` consumes a coroutine and yields its result. Awaiting a value that is not a
  coroutine passes it through (the reference implementation raises `TypeError` there).
- `await` is legal at **module scope**: the top level of a file is the program's
  coroutine context. CPython rejects it, which is why every program that actually runs a
  coroutine is `not_applicable` on the oracle leg rather than a parity case.
- `async for v in [f(1), f(2)]` awaits each element as the loop produces it, so a list
  of coroutines is the expected iterable, not a bug. `async with m as x:` behaves as
  `with` does (`__enter__`/`__exit__`): the protocol is driven, but nothing suspends.
- Inside a plain `def`, `await` / `async for` / `async with` still *work* (the operand is
  evaluated, the loop runs) even though CPython calls them a `SyntaxError`. The checker
  reports them as warnings: they cannot suspend anything, so they are almost always what
  the author meant to write inside an `async def`.
- **Not implemented:** async generators (`async def` with `yield` — refused, because
  neither backend lowers one), and suspension in the middle of a body: an `await` runs its
  coroutine to completion, so there is no interleaving and no event loop to interleave on.
- The compiled backend still *lowers* a coroutine call as a call (`await e` evaluates
  `e`), so it performs a coroutine's effects at the call rather than at the await. That is
  invisible while every coroutine is awaited and nothing observable happens in between,
  and wrong when something does; the open item is named in `roadmap.md` as L7.6a and pinned as
  `programs/probe_async_eager`.

### What the checker proves about async code (L7.6, ADR 0195)

The discipline above is not enforceable by either backend — a dropped coroutine printed
`<coro>` in the interpreter and the awaited value in the compiled binary, and both
"worked". So it is checked in the shared front end, and every rule names the fix:

| Code | Level | Rule |
|------|-------|------|
| `async.coro.never_awaited` | error | a coroutine was created and nothing ever awaited it, so its body never runs — reported at the binding, the rebinding that drops it, or the operation that consumed it as a value |
| `async.coro.awaited_twice` | error | one coroutine object is awaited twice on a path: its body already ran (CPython raises `RuntimeError` here) |
| `async.generator.unsupported` | error | an `async def` whose body yields: an async generator that neither backend lowers |
| `async.await.outside_coroutine` | warning | `await` inside a plain `def` — legal here, a `SyntaxError` in Python, and never what a suspension-hungry program wants |
| `async.async_stmt.outside_coroutine` | warning | `async for` / `async with` inside a plain `def`: it behaves as the plain form |
| `async.await.not_coroutine` | warning | `await` pointed at a value that provably is not a coroutine (a literal, a built-in call, a plain `def` call): a no-op here, `TypeError` in Python |
| `async.missing_return` | warning | an `async def` path runs off the end while other paths — or its `-> T` annotation — promise a value, so awaiting it on that path is `None` |

The proof is flow-sensitive, not syntactic: `a = f(1)` followed five lines later by
`await a` is fine, coroutines stored in a container or handed to another function are
assumed to be awaited there, and an `async for` over a list of coroutines is exactly the
intended shape. What the pass cannot follow, it does not report.

Every fact the rules decide from is also published, per function: `gustyc --effects <src>`
(or `gusty effects <file>...`, `--json` for the document) prints the effect signature —
which effects the body performs (`await`, `yield`, `raise`), whether it returns a value,
and whether its control flow can run off the end. See `docs/operations.md`.

## Parameters and loop variables (ADR 0196)

A **parameter is a local variable** that starts out bound to an argument. Assigning to
one is ordinary and has no effect on the caller:

```gusty
def bump(n):
    n = n + 1        # a new value for this function's own n
    return n

bump(0)              # 1 — not 0

def clamp(x, lo, hi):
    if x < lo:
        x = lo
    if x > hi:
        x = hi
    return x
```

Rebinding is allowed for every value kind — numbers, strings, containers, instances —
and it is the same variable: what the body last stored is what the body reads, on every
path, including a path that reaches a read before any assignment textually.

A **`for` loop binds its variable to each element as the loop produces it**, the way
Python does, which has two visible consequences:

```gusty
for i in range(3):
    i = i * 100      # does not move the iteration
    print(i)         # 0, 100, 200
print(i)             # 200 — the last value the loop bound, not the range's end
```

The loop keeps its own counter, so writing to `i` changes only what `i` means inside the
body, and after the loop the variable still holds the last element (`for i in
range(3)` leaves `2`, never `3`). A `for` whose `else:` clause runs on normal completion
is unaffected.

## The word a function returns (L11.6, ADR 0254)

A function is emitted with a type for each parameter and a type for its answer. Those
words used to be chosen by two unrelated questions — the parameters from the call sites,
the answer from **the shape of the `return` expression** — and a shape is not evidence:

```gy
def addf(x):
    x = x + 1.5      # the body stores a double into x's slot
    return x         # `return x` says nothing about the answer's kind

print(addf(1.0))     # CPython 2.5, the interpreter 2.5, the compiled leg answered 1
```

The return word is now read from the same predicate the emitted body itself asks —
`isFloat`, the one the arithmetic, the negation and `print` consult — run over the body
with the names the body rebinds to a float taken as the doubles they became. So the `ret`
and the value it writes cannot disagree, and the program above answers `2.5` on both
backends. What the rule covers is the answer, not the last line: a bare name, an
arithmetic expression over it (`return x * 2`, `return x % 3`), its negation
(`return -x`, which used to reach no gate at all and produced a module `llc` rejected), a
kind-preserving numeric builtin of it (`return abs(x)`, `return abs(-x)`, `float(x)`), a
name bound inside the body (`y = x + 0.5; return y`), and any of those inside `if`, `while`,
`try` or recursion.

What the rule deliberately does **not** promote is an answer that is not that double, because
promoting it would put an `i32` in a `double`'s word — the same bug in the other direction:
`return int(x)` and `return round(x)` answer with an int whatever arrives, `return x > 2`
answers with the bool word, `return str(x)` with an interned index, and a user callee's return
word is that callee's own question. Those stay exactly as they were.

Two shapes cannot carry the answer and **refuse in words** (exit 1, never the module `llc`
rejects — ADR 0166), because the convention that picks the return word picks every parameter's
word with it:

```gy
def f(xs, y):
    y = y + 0.5
    print(xs[0])     # the body reads the container: its word is a heap handle
    return y         # refused: `xs` would have to arrive as a `double`

def g(x):
    x = x + 1.5
    return x if x > 2 else 0.0   # refused: no number-typed `select` (Gap R.102)
```

Each names the variable whose double has nowhere to go and says what to write instead
(`return x + 0.0`, bind it to a new name, or take the branch with `if`/`else`). A **method**
is emitted `i32`-returning whatever its body computes — the receiver's word and the answer's
are fixed together — so a method with this body is refused the same way; that is a method
limit, not a language one, and the interpreter answers all of these.

## Declaration order (ADR 0197)

A `def` is a binding of the scope that contains it — not only of the text below its line.
Python's rule is that a name has to exist when the call *runs*, and a function body does
not run when it is defined:

```gy
def is_even(n):
    if n == 0:
        return True
    return is_odd(n - 1)      # is_odd is declared five lines below


def is_odd(n):
    if n == 0:
        return False
    return is_even(n - 1)


print(1 if is_even(4) else 0)   # 1
```

Mutually recursive functions, and helpers declared below the code that uses them, are
ordinary programs and check clean — inside a function body, and inside a class body, a
name resolves to any `def` of that scope, wherever it is written. A `def` inside an
`if` / `while` / `for` / `try` / `match` arm belongs to the enclosing scope, since those
statements create no scope of their own; a nested `def` is visible to the whole body it is
written in, so sibling nested defs may call each other.

What is **not** hoisted is code that runs immediately. A call at module or class top level
and a decorator expression both evaluate where they are written, so their names must
already be bound:

```gy
print(later())          # error: undefined name "later"


def later():
    return 1


@identity              # error: undefined name "identity"
def target():
    return 3


def identity(f):
    return f
```

A forward call is still *checked*: the declared parameter annotations and `-> T` travel
with the hoisted name, so arity and argument-type errors are reported for a function the
walk has not reached. What it cannot know is a return type the checker would have
*inferred* from a body it has not analyzed — that stays dynamic, which is where gradual
typing already puts unannotated code.

## Your names are your own (ADR 0198)

A function name in a Gusty program is never a host symbol. These are all legal, and all mean
what the program says:

```gy
def sync(x):
    return x + 1


def main(x):
    return x + 7


print(sync(0))   # 1
print(main(0))   # 7
```

The compiled module defines them as `gy_sync` and `gy_main`. That is not decoration: an LLVM
function name is a **link name**, and a program emitted as `@sync` had its own call answered by
libc's `sync()` — the interpreter printed `1`, the binary printed the C library's answer, and the
build reported success (roadmap Gap R.4). `main` was worse, because the generated entry point is
`@main`: a program with a `def main` could not be built at all.

What keeps its own name is what the program does not define: the runtime helpers the compiler
emits (`rt_*`), the C library, the generated entry point `@main`, and every `extern fn`, whose
link name is the C name it declares. The distinction is visible to tools — `nm` on a built binary
shows `gy_sync` and never `sync` — and in the source map, where `name` is what you wrote and
`symbol` is what the linker sees.

Shadowing a *built-in* name (`def abs(x): ...`) is a separate, known defect: the compiled call is
resolved against the builtin table and answers with the builtin (roadmap R.6).

## Built-ins are shadowable (ADR 0199)

`str`, `float`, `len`, `abs`, `min`, `sum`, `round`, `sorted` are ordinary names. Defining one
shadows the built-in, and the definition wins — in both backends and in CPython:

```gy
def str(x):
    return x + 7


def len(x):
    return 3


print(str(1))          # 8
print(len([1, 2, 3]))  # 3
```

This matters because a compiled backend decides a lot from a call's *name*: whether its result is a
float, whether it folds to a constant, whether it may be printed with `%s`. Each of those readings
is the built-in's meaning, so each one is now guarded by a single question — does the program define
this name? — and a program that does gets its own function called:

```gy
def float(x):
    return x + 7


print(float(1) + 0.5)   # 9.5 — the call is the program's, its int result lifted to double
```

What a program does *not* get by shadowing is a slower program: when nobody shadows `str`, the
constant folding still runs (`print(str(42))` is still folded at compile time).

### Which words are reserved

Only words that change grammar are keywords: `def`, `return`, `if`/`elif`/`else`, `while`,
`for`/`in`, `class`, `import`/`from`/`as`, `match`/`case`, `try`/`except`/`finally`, `with`, `yield`,
`lambda`, `not`/`and`/`or`/`is`, `None`/`True`/`False`, and the declaration keywords `extern`/`type`.

Everything else — including every built-in — is an ordinary name a program may define, shadow, and
pass around (`programs/builtin_names_as_defs.gy`):

```gy
def range(x):                 # a helper of your own, not the built-in
    return x * 3


def use_both(print, range):   # parameters may be called print and range
    return print + range


class Counter:
    def range(self, n):       # and so may methods
        return n * 4


print(use_both(print=7, range=8))   # 15
print(range(4))                     # 12
```

`print` and `range` used to be keywords, so these definitions did not parse (`expected identifier`)
— see ADR 0203.

**A claimed built-in name must be defined above every use.** Taking a built-in's name means taking it
everywhere below the `def`; above it, the two backends would mean different things by the same call —
the interpreter, executing in order, still reaches the built-in, while the compiled backend resolves
the call to your definition — so the program is refused at the call instead (ADR 0205):

```gy
for i in range(2):        # error at 1:15: "range" is a built-in here, but this module
    print(i * 100)        #   defines it below, at line 5 — move the definition above
                          #   every use, or rename it


def range(x):
    return x * 3
```

Write it the other way round and it is simply a program with its own helper. The rule is bounded to
where order is real: inside a function body every module definition has already run, and a method
named `range` is a method, not a module binding — neither is affected.

### A call must fill every parameter without a default

```gy
def build(a, b):
    return a + b


print(build(1))     # error: function "build" expects 2 arguments, got 1
```

Too many arguments and too few are both refusals, at the call, and both name the function they are
complaining about (`method "m" ...` for an attribute call). A call that names its arguments and
leaves one out is told which name it skipped — `function "build" is missing argument "b"` — because
the count is not the useful fact there.

A **default is a way of being supplied**, so every shape that passes fewer arguments than the
definition lists stays legal (`programs/arity_defaults.gy`):

```gy
def offset(base, step=10, bonus):    # a default in the middle is not special
    return base + step + bonus


print(offset(1, 2, 3))               # 6   — positional binding fills left to right
print(offset(1, bonus=5))            # 16  — the default fills itself
print(offset(base=1, step=2, bonus=3))  # 6  — a keyword call names what it fills
```

**A default marks a parameter that may be omitted; it says nothing about the parameters around it**
(ADR 0206). CPython rejects `def offset(base, step=10, bonus)` at the `def`
(`parameter without a default follows parameter with a default`) because Python's positional binding
stops at the first default, so that last parameter genuinely cannot be filled. This language has no
such rule — positional binding fills left to right and a keyword call names what it fills — so every
parameter is reachable, and the shape is ordinary source instead of a syntax error. The consequence is
that `programs/param_default_order.gy` is a program CPython cannot run at all, recorded as such in the
conformance ledger rather than made to look like a match. What *is* enforced is the thing that is
actually broken in either language: a call that leaves a parameter with no default unfilled
(`offset(1)` → `function "offset" expects 3 arguments, got 1`).

```gy
def greet(name, punct="!"):
    return name + punct


print(greet("ada"))                # ada!
print(greet("ada", "?"))           # ada?
print(greet(name="bob", punct="."))  # bob.
```

A call that does not fit its definition is not analysed any further: the compiler reports the arity
mistake and stops, rather than also walking the callee's body with a parameter left unbound and
reporting the callee's own source as undefined (ADR 0201). One mistake, one diagnostic, at the call.

### A method is a call like any other

A method body has the same duties as a function body, and the same rights (ADR 0223): it owns its
unwind path, its `finally` runs on every exit, and an exception that leaves it reaches whoever called
it.

```gy
class Worker:
    def cleanup(self) -> int:
        try:
            return 3
        finally:
            print("fin")        # fin, then 3 — both backends

    def raiser(self) -> int:
        raise ValueError("boom")

try:
    print(Worker().raiser())    # the raise reaches the caller, which named the class
except ValueError:
    print("caught value error")
```

Three consequences, each with a test: a `raise` in a method (or in a method it calls, or in its
`__init__`) propagates to the call site rather than leaving the method to return a plausible value;
a construct the compiler cannot lower reports a compile error from inside a method exactly as it does
from inside a `def` — it is never quietly dropped, which used to produce half a function and a
toolchain rejection; and the traceback frame says `Class.method`. Known limit: a method whose result
is a `str` is still broken in the compiled backend (roadmap Gap R.42).

### A method and a helper may share a name

The words that describe what a class does are usually the words that describe a helper, so this
pair is ordinary rather than clever:

```gy
def time(x):
    return x + 5


class Timer:
    def time(self, x):
        return x * 3

    def run(self, x):
        return self.time(x) + 1


print(time(1))         # 6 — the module function
print(Timer().run(2))  # 7 — the method
```

`time(1)` means the module function and `self.time(x)` means the method: a `def` inside a class is
a definition *of that class*, not a definition of the surrounding file (ADR 0200). The same rule
holds across classes — `A.m` and `B.m` are two definitions — and it is checked, not assumed: a call
to the module function is checked against the module function's annotations.

Two neighbouring things stay refusals rather than wrong answers, both documented in the roadmap:
`def print` and `def range` do not parse (the parser reserves the tokens), and a module function
that shares a name with a *method* still confuses the checker (R.8).

## Docstrings and `__doc__` (Round 9, ADR 0141)

A leading bare string literal in a `def` or `class` body is captured as a
docstring at definition time (Python-style). It is removed from the body (not
re-evaluated as a no-op statement) and stored on the node.

- `def greet(): "returns a greeting"; return "hi"` — `greet.__doc__` is
  `"returns a greeting"`.
- `class Animal: "an animal class"; def speak(self): return self` —
  `Animal.__doc__` is `"an animal class"`.
- Nested `def` (closures) carry `__doc__` too.
- A function/class without a docstring yields `""`.
- A string literal that is NOT the first statement is a normal expression, not
  a docstring.

`__doc__` reads are **interpreter-only** in the AOT backend: AOT lowers
functions/classes to compile-time artifacts with no runtime introspection
objects (ADR 0141).

## Canonical formatter (`gusty fmt`, Round 8)

The compiler ships a deterministic source formatter. Docstrings are re-emitted
as the first statement of a `def`/`class` body so formatting round-trips
preserve them.

## Incremental parsing (L5.8)

The LSP and REPL avoid a full re-parse on every keystroke via a stable,
span-keyed parse tree:

- `NewParseCache(src)` lexes and parses a whole document, recording each
  top-level statement's token and byte boundaries.
- `ParseCache.Update(edits)` applies an edit batch, re-lexes the document,
  and re-parses only the top-level statements affected by the edits.
  Statements whose source region is untouched keep their AST node identity
  (keyed by their source span) across updates.
- `ParseCache.Program()` returns the current parse tree, `ParseErrors()`
  returns parse diagnostics, and `Reused()` reports how many statements were
  preserved by the most recent update.

The LSP uses incremental text sync (kind 2): `textDocument/didChange`
converts each change range into a `lang.Edit` and calls `Update`, falling
back to a full re-parse only for whole-document replacements.
