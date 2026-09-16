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
  argument. String arguments (literals, folded string calls like `str(7)`,
  and constant `+` concatenations) use a `%s\n` format; integer arguments
  use `%d\n`. Zero-argument `print()` writes nothing (no `printf`), matching
  the interpreter's no-op.
- `range(n)` — iteration bound for `for` loops.
- `str(x)` — converts a value to its string representation. In the
  interpreter, `str(x)` boxes `repr(x)` as a string; in codegen, `str(int)`
  folds to the decimal string constant, so `len(str(42))` → `2` and
  `print(str(42))` prints `42` (and `"n=" + str(7)` folds to `"n=7"`).

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

In the AOT LLVM codegen, **set comprehensions** (`{x * x} for x in [1, 2, 3]`)
and **dict comprehensions** (`{x: x * 10} for x in [1, 2]`) are lowered to
dedicated `@.setN` / `@.dictN` globals: set comprehensions unroll the iteration
and deduplicate folded elements; dict comprehensions fold key/value pairs into
the dict global. Both are usable with `len(...)` via the `compLen` map,
mirroring the interpreter's semantics.

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
    abs(-5)          # 5

`min`/`max` accept a list or set (or a single value); `abs` takes one number.
Implemented in both the interpreter (REPL/`--eval`) and the LLVM AOT codegen.
In the AOT path `min`/`max`/`sum` fold over an **inline list literal** (unrolled
`icmp`+`select` / `add` chains over the list's global struct). Each element is
lowered via `g.value`, so runtime-variable elements (`min([a, b])`) work
exactly like integer literals. `len([a, b])` returns the element count
directly, and `[a, b][i]` evaluates the indexed element via `g.value` —
runtime-variable elements also work in `len` and list index. `abs` accepts any
integer expression and is constant-folded when its argument is a literal.


`sum`/`min`/`max` also fold set literals, dict literal keys, and dict-method
calls (`.keys()` / `.values()`), so `max({1: 2, 3: 4}.keys())` -> 3.

### string methods

String equality `==` / `!=` compares contents in both paths (`"abc" == "abc"`
is 1, `"abc" == "abd"` is 0), constant-folded in the codegen.
String indexing `"abc"[1]` returns the char code (98 for 'b') in both the
interpreter and the AOT codegen (constant-folded).
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

`.startswith(sub)` and `.endswith(sub)` return 1 or 0 in **both** paths: the
interpreter applies `strings.HasPrefix`/`strings.HasSuffix`; the codegen
constant-folds them to `i32 1`/`i32 0` when the receiver and argument are
string constants.

`.count(sub)` returns the number of non-overlapping occurrences of `sub` in
**both** paths: the interpreter applies `strings.Count`; the codegen
constant-folds it to an `i32` literal when the receiver and argument are
string constants (`print("ababab".count("ab"))` emits `i32 3`).

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

The `sorted(list)` builtin returns a copy of the list with elements sorted
(interpreter path; ints by value, strings by content via `lessVal`).
`sorted([3, 1, 2])[0]` is 1.

`.zfill(width)` pads with leading zeros to `width` in the interpreter
path: `"42".zfill(5)` is "00042", and a string already at/over width is
unchanged.

`.ljust(width)` / `.rjust(width)` pad with spaces on the right / left to
`width` in the interpreter path: `"ab".ljust(4)` is "ab  ", and a string
already at/over width is unchanged.

`.index(sub)` returns the index of the first occurrence of `sub`, raising
"substring not found" when absent (interpreter path; like `find` but
errors instead of returning -1).

`.rsplit(sep)` splits the string on `sep` from the right (interpreter
path; with no `maxsplit`, all splits — the same parts as `split`).
`"a-b-c".rsplit("-")[2]` is "c".

`.removeprefix(prefix)` / `.removesuffix(suffix)` return the string without
the prefix / suffix when it is present, else unchanged (interpreter path
via `strings.TrimPrefix`/`TrimSuffix`). `"hello".removeprefix("he")` is
"llo".

`.expandtabs(tabsize)` replaces each tab with spaces to the next tab stop
of size `tabsize` (interpreter path). A string with no tabs is unchanged.

`.strip([chars])` trims whitespace, or the given `chars` when provided
(interpreter path via `strings.TrimSpace`/`strings.Trim`).
`"xxhi xx".strip("x")` is "hi ".

`.lstrip()` and `.rstrip()` remove leading / trailing whitespace in **both**
paths: the interpreter applies `strings.TrimLeftFunc`/`TrimRightFunc` with
`unicode.IsSpace`; the codegen constant-folds them to a trimmed string
constant (`print(len("  hi  ".lstrip()))` emits `i32 4`).

`.join(list)` joins a list of string elements with the receiver as
separator in **both** paths: the interpreter evaluates the list arg and
applies `strings.Join`; the codegen constant-folds the receiver and a
constant list of string elements (`print("-".join(["a", "b", "c"]))`
emits the global "a-b-c").

Dict `.get(key[, default])` returns the value for `key`, or `default` if
absent (default is 0 when omitted). Dict keys compare by string content, so
`{"a": 1}.get("a", 9)` is 1 — this also fixed dict indexing `d[key]`,
which previously failed because string keys were compared by boxed id.

### list methods

Lists support `append(x)` (in-place, returns the updated list). On constant
list literals the AOT codegen folds `[1, 2, 3].append(4)` to `[1, 2, 3, 4]`
so `len`/`sum` work; mutating a bound variable stays interpreter-only.:

    xs = [1, 2]
    xs.append(3)     # xs == [1, 2, 3]

Constant dict literals: `.keys()` / `.values()` fold to lists in the AOT
codegen (`{1: 2, 3: 4}.keys()` -> `[1, 3]`), so `len`/`sum` work. `.items()`
now works in both paths on constant dict literals: the interpreter returns a
list of `[key, value]` pairs, and `len({1: 2, 3: 4}.items())` folds to 2 in
the codegen. Non-constant receivers remain interpreter-only.

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
