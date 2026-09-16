# gusty language surface

Single source of truth for gusty's syntax and semantics. This document tracks
the language as it grows; each feature commit updates it.

gusty is a Python-like, indentation-based language compiled ahead-of-time
through LLVM. Values are gradually typed: optional annotations steer inference,
and untyped code falls back to dynamic dispatch at runtime.

## Lexical structure

- Indentation-sensitive: `INDENT`/`DEDENT` tokens drive block nesting.
- Statements are separated by newlines; a line ends a statement.
- Comments begin with `#` and run to end of line.

## Statements

### Assignment

```
x = 1
x = x + 1
```

The target may be a `Name`; the value is any expression. Assignments define or
rebind a variable in the current scope.

### Expression statements

Any expression on its own line. `print(...)` is a builtin call that writes to
stdout.

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

The AOT LLVM path lowers **list comprehensions** over a constant iterable
(inline list literal or `range(n)`) into a dedicated global struct, unrolled
and constant-folded at compile time. A constant `if` condition filters elements
at compile time. The lowered result can be indexed inline exactly like a list
literal: `[x * 2 for x in [1, 2, 3]][1]` folds to the elements `2, 4, 6` and
emits a `getelementptr` + `load` at the constant key. Like list/dict/set
literals, the comprehension must be used inline (no assignment-to-variable
indirection) in the codegen path; the interpreter evaluates comprehensions at
runtime and is unchanged.


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

### Ternary conditional expressions

`then if cond else otherwise` (Python-style ternary) is supported in both the
interpreter and the AOT codegen. The condition is an or-level expression; the
`else` branch is a full expression, so nested ternaries bind right:
`1 if 0 else 2 if 1 else 3` is `1 if 0 else (2 if 1 else 3)`.

In the AOT path, a constant condition folds to the taken branch, and a runtime
condition (a comparison, or `and`/`or`) lowers to an LLVM `select i1 cond,
i32 then, i32 else`, so the ternary is allocation-free and needs no blocks.

### Dicts & sets (LLVM codegen)

The AOT path also lowers **inline dict and set literals** with constant-key
indexing and `len`: `{1: 10, 2: 20}[1]`, `{1, 2, 3}[2]`, `len({1: 10, 2: 20})`.
Each dict/set literal is emitted as a dedicated global struct (`{i32 count,
[n x i32] keys, [n x i32] vals}` for dicts; `{i32 count, [n x i32] elems}`
for sets). Constant-key lookup resolves at compile time; like lists, these
literals must be used inline (no assignment-to-variable indirection) in the
codegen path. The interpreter indexes dicts/sets at runtime and is unchanged.

`import mod` loads `mod.gy`, evaluates it, and binds `mod` to a module
namespace. Top-level variables and functions of the module are accessed as
`mod.name` and called as `mod.fn(args)`. A module can itself `import` other
modules. Imports are evaluated in the interpreter (REPL/--eval path).

### Functions

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
  transformed) value bound to `f`. Decorators are implemented in the
  interpreter (REPL / `--eval` path).

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
- `raise Exception` raises a runtime error; `try:`/`except Exception:`/`finally:` catch
  it (the catch-all `Exception` clause matches any raised or builtin error), and
  `finally:` always runs. `Exception` is a builtin exception type name.
- `try:` / `except Exception:` / `finally:` are supported: the try body runs; on a
  runtime error a matching except clause runs (catch-all `Exception` matches any);
  the `finally:` body always runs. Implemented in the interpreter path.
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
- Lowered to a chain of integer comparisons.

## Expressions

- Integer literals `1`, `2`, `-3`.
- Float literals, bool literals (`true`/`false`), `none`, string literals.
- `Name` reads a variable (fresh SSA load per read for dominance safety).
- `BinOp` arithmetic (`+`, `-`, `*`, `/`, `//`, `%`) and comparisons (`<`, `==`, ...).
- `and` / `or` are boolean operators: both operands are evaluated and the
  result is a `0`/`1` integer (`and` is 1 iff both are non-zero, `or` is 1 iff
  either is non-zero). Lowered in the AOT codegen to i1 logic zero-extended to
  `i32`, mirroring the interpreter. `//` floor division lowers to `sdiv`, and
  `%` modulo lowers to `srem` (signed remainder) — both in the interpreter and
  the AOT runtime path; modulo on constant operands folds at compile time.
- `Call` to user functions or builtins (`print`, `range`).
- Attribute access (`obj.attr`) and indexing are parsed for future features.

## Types

- `int`, `float`, `bool`, `str`, `none`, `void`, and `any` (dynamic).
- Unannotated variables infer to `any`; annotated variables pin their type.
- Arithmetic on non-numeric operands reports a diagnostic (suppressed inside
  untyped function bodies, which fall back to dynamic dispatch).

## Builtins

- `print(x, ...)` — writes each argument to stdout on its own line via
  `printf`. Multi-argument `print` mirrors the interpreter: one `printf` per
  argument. String-literal arguments use a `%s\n` format (the interpreter
  prints strings via `Repr`); integer arguments use `%d\n`. Zero-argument
  `print()` writes nothing (no `printf`), matching the interpreter's no-op.
- `range(n)` — iteration bound for `for` loops.

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

    print(1.5 + 1)   # 2.5
    print(7.0 / 2.0) # 3.5
    print(1.5 > 1)   # 1

## Comprehensions

List comprehensions iterate a range or list, bind a loop variable, apply an
optional filter (`if`), and collect the element expressions.

    xs = [x * 2 for x in range(3)]   # [0, 2, 4]
    zs = [y for y in [1, 2, 3] if y > 1]
    d  = {k: k * 10 for k in range(2)}

Comprehensions over a `range(...)` are supported in the interpreter.

### min / max / abs

Standard-library numeric builtins:

    min([3, 1, 2])   # 1
    max([3, 1, 2])   # 3
    abs(-5)          # 5

`min`/`max` accept a list or set (or a single value); `abs` takes one number.
Implemented in both the interpreter (REPL/`--eval`) and the LLVM AOT codegen.
In the AOT path `min`/`max`/`sum` fold over an **inline list literal** (unrolled
`icmp`+`select` / `add` chains over the list's global struct), so the argument
must be an inline list literal; `abs` accepts any integer expression and is
constant-folded when its argument is a literal.

### string methods

Boxed strings support Python-style methods:

    "heLLo".upper()      # HELLO
    "heLLo".lower()      # hello
    "  hi  ".strip()     # hi
    "a b c".split(" ")   # [a, b, c]

`split` takes an optional separator (default space). Interpreter path only.

### list methods

Lists support `append(x)` (in-place, returns the updated list):

    xs = [1, 2]
    xs.append(3)     # xs == [1, 2, 3]

Interpreter path only.

### dict methods

Dicts support `keys()` and `values()` (insertion order, returned as lists):

    {"a": 1, "b": 2}.keys()     # [a, b]
    {"a": 1, "b": 2}.values()   # [1, 2]

Interpreter path only.

### sum

Sums a list or set of numbers:

    sum([1, 2, 3])   # 6

Implemented in both the interpreter and the LLVM AOT codegen. In the AOT path
`sum` unrolls `add` chains over an inline list literal's global struct (the
argument must be an inline list literal).
