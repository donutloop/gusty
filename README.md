# gusty

> A statically-typed, Python-flavored programming language compiled ahead-of-time through LLVM.

gusty is a small, modern programming language that pairs Python's familiar
indentation-based syntax and ergonomic feel with the performance of native
compiled executables. Source goes through a clean, inspectable pipeline —
`lex → parse → semantic (type inference) → codegen` — down to LLVM IR, which
is verified, optimized, and lowered to a native binary, or JIT-executed in
the REPL.

There is **one backend**. Everything `gusty` runs — `--file`, `--eval`, the REPL — goes through LLVM
codegen and the resulting artifact; the AST interpreter that used to answer those paths is retired
(ADR 0302), and its recorded answers, per source, are what the test suite checks the compiler against
(`docs/operations.md` §"The suite's own interface"). `--aot`/`--jit` are accepted and ignored; `--interp`
is a usage error that says so.

The toolchain is built for **both humans and agents**: a friendly REPL/CLI for
people, plus structured, machine-readable output (JSON diagnostics, JSON AST/IR
dumps, a JSON Schema, stable flags, deterministic exit codes) so scripts and AI
workflows can discover and consume the language without guessing.

---

## A taste of the language

```python
print(40 + 2)                # -> 42

x = 5
print(x + 1)                 # -> 6

if x < 2:
    print(10)
else:
    print(20)

s = 0
for i in range(5):           # range(n) and range(a, b)
    s = s + i
print(s)                     # -> 10

def double(x):
    return x * 2
print(double(5))             # -> 10

x = 2
match x:
    case 1:
        print(1)
    case 2:
        print(2)             # -> 2

i = 0
while i < 100:
    i = i + 1
    if i == 3:
        break
print(i)                     # -> 3
```

## Language surface

gusty ships an indentation-based syntax covering:

- **Functions** — `def` with default/keyword args, inferred return types, and
  anonymous `lambda` functions (`lambda x: int: x + 1`) lowered to closures
  exactly like `def`.
- **Functions as values** — a function the program *calls* is a value this backend answers: a higher-order body
  is cloned once per tuple of functions its call sites ask for, a `lambda` that needs a name is given a
  declaration of its own, and `g = f` is called as `f` — so `twice(d2, 3)` is `12`, `apply1(inc, 41)` is `42`,
  `g = d2` / `g(4)` is `8`, and `sorted([3, 1, 2], key=lambda x: 0 - x)` is `[3, 2, 1]` (**ADR 0345**,
  roadmap L11.7). The body may call **itself**: `def fact(n, f): … return f(n) * fact(n - 1, f)` with
  `print(fact(4, lambda x: x))` is `24`, because a clone answers the recursion inside its own body by calling
  **itself**, and the callable need not come first — `def run(v, fn): return fn(v)` calls `fn` by name too
  (**ADR 0364**, roadmap **Gap R.267** and the entry-tuple ordinal **Gap R.313**). The recursive call may also omit
  a defaulted callable parameter: `def run(n, f=lambda x: x): … return f(n) + run(n - 1)` is `6`, because the call
  inherits the entry of the clone that owns the recursion; repeating the same lambda is accepted too
  (**ADR 0385**, roadmap **Gap R.335**). Supplying a *different* callable, or omitting a required parameter, remains
  an honest refusal. A hook key may ask its **element**: `sorted(["bbb", "a", "cc"], key=lambda s: len(s))` is
  `['a', 'cc', 'bbb']`, `key=lambda s: s.lower()` orders by the text, `key=lambda s: s[0]` by a subscript, and
  `xs = sorted(...)` then prints, indexes and walks as the list of texts it is — because the door that calls
  the key registers that call with the scan which decides a parameter's kind (**ADR 0363**, roadmap Gap R.264).
  The elements of a container the program built at run time still refuse, and say so (**Gap R.254**). A function that is *read* rather than called — `print(f)`, `xs = [f]`, a function a call
  returns — still refuses in words, because no `fnptr` word exists to hold it; `gustyc --callables <src>` prints
  what the pass decided about a program, including how many self-calls it answered (`recursive_sites`).
- **An applied `lambda`** — a `lambda` applied where it is written is read as the function declaration it is, so its
  body may call its own parameters and may read the frame that encloses it:
  `(lambda f: f(3))(lambda x: x * 2)` is `6`, `(lambda f, g, x: f(g(x)) + g(f(x)))(…)` is `15`,
  `apply1(lambda x: (lambda y: y * 2)(x), 4)` is `8`, `def f(x): return (lambda y: y * 2)(x)` applied is `8`, a lambda
  applied inside a method or a comprehension answers, and `(lambda m: m / 2)(3)` prints `1.5` — because each lambda is
  emitted as a real `define`, cloned once per tuple its called parameters are asked to be, with one capture parameter
  per name its body is free in and the application filling each from the caller's own reading (**ADR 0370**), and an
  application may leave a parameter out because the entry tuple is the list of functions the BODY asks for — an omitted
  slot arrives with its default, and a called omitted slot with the function that default names, so
  `(lambda f, y=10: f(y))(lambda x: x + 1)` is `11` and `def apply(f, y=10): return f(y)` applied to one argument is
  `11` too (**ADR 0371**, roadmap L11.7 · **Gap R.265**, **Gap R.276**, **Gap R.329**). The two lists a clone needs are
  a union: a body that calls one of its own parameters AND reads the frame around its application gets the entries beside
  one capture parameter per free name, so `def f(x): return (lambda h: h(1) + x)(lambda q: q * 10)` applied is `12`,
  `def g(v): return (lambda fn, n=3: fn(n) + v)(lambda a: a * 2)` is `11`, and
  `[(lambda fn: fn(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]]` is `[4, 6, 8]` (**ADR 0372**, **Gap R.334**). A capture
  travels ONE STEP PER FRAME, so a `lambda` applied inside an applied `lambda` reads a name two lambdas up: each
  generated `define` gains its own forwarded tail and each inner application is filled from the registers of the `define`
  it sits in — `(lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1)` is `111`, four binding forms deep is `10`, a
  `def` local owned three frames up is `111`, and a name the middle frame shadows is not forwarded at all (**ADR 0380**,
  **Gap R.330**). A comprehension's own target is a binding of the frame that hosts it, so a body written inside the
  comprehension reads it the same way: `[(lambda z: z + x)(1) for x in [10, 20]]` is `[11, 21]`,
  `[(lambda y: (lambda z: z + x + y)(1))(2) for x in [10, 20]]` is `[13, 23]`, and where the module spells the same name
  the target answers, not the module — `x = 5` then that first program is `[11, 21]`, the `[6, 6]` it printed before is
  on the record as the wrong answer it was (**ADR 0381**, **Gap R.348**). A capture whose value is not a number, the
  ENTRY of a higher-order `def` reading the caller's frame,
  and an omitted slot whose default is not a function the compiler can name refuse in words instead of printing a word
  whose kind nobody asked for (**Gap R.332**, **Gap R.336**). The one name an applied `lambda`'s body may be spelled
  for is a module literal: `TEXT = "hey"` above `print((lambda: TEXT)())` prints `hey`, because the pass rewrites the
  read into the literal the module bound once and never rebound, and the doors that answer a spelled answer answer
  it (**Gap R.331**, **ADR 0461**).
- **Control flow** — `if` / `elif` / `else`, `while`, `for ... in range(n)`
  / `range(a, b)` / `range(a, b, step)`, `for x in [...]`, optional loop
  `else:` clauses, `break` / `continue`, and `pass`. A parameter is a local
  variable (assigning to one is ordinary and local), and a `for` loop binds its
  variable per element — it keeps the last value bound, and writing to it does not
  move the iteration (ADR 0196).
- **Pattern matching** — `match` with integer-literal equality, `_` wildcards,
  and list-destructuring patterns (`case [a, b]:`). Matches also support
  guards (`case x if cond:`), or-patterns (`case 1 | 2:`), dict patterns
  (`case {"k": v}:`), and class patterns (`case Point(x, y):` with subclass
  walk + attribute binding).
- **Data structures** — inline `list` / `dict` / `set` literals, indexing, and
  list / dict / set comprehensions.

- **Tuples are values** — `(1, 2)`, `(1,)` and `()` are objects, not a spelling the parser hands to a list road:
  they print `(1, 2)`, index (including `t[-1]`), measure with `len`, unpack with `a, b = (4, 5)`, test with `in`,
  compare for equality (a tuple is never equal to a list, in either direction), enter a container, come back from a
  function, key a dict (`d[(1, 2)]`, and a missing key raises `KeyError: (3, 4)` with the key's repr), and survive a
  slice (`t[1:]` is a tuple, because a copy carries the object's attributes). They are **immutable**, and the write
  doors refuse rather than mutate: `t.append(3)`, `t[0] = 9`, `t.sort()` stop the build quoting the reference's own
  `AttributeError`/`TypeError`. The mechanism is one object, not a second layout — the list layout's heap object with
  the immutable-container bit set in its own `@estr[h]` attribute word — so the printer, the equality door and the
  dict-key road all ask **the object**, which is why `p = mk(1, "x")` prints `(1, 'x')` (**ADR 0361**, roadmap L11.3).
  What is owed is named by the rows: an order (`Gap R.309` — `sorted` refuses container elements rather than printing
  arrival order), `count`/`index` (`Gap I.2`), and the empty tuple's identity (`Gap R.310`).
- **Text is measured in code points, and one generated table decides its case** — a text's value is its source's
  own UTF-8 bytes, and every question about it is asked of characters: `len("café")` is `4`, `"héllo"[1]` is `"é"`,
  `"héllo"[1:3]` is `"él"`, `for c in "aé"` binds `a` then `é` — each of them on a text the compiler can read AND on
  one it cannot (a parameter, a container element, the answer of a call). `upper`/`lower`/`title`/`capitalize`/
  `swapcase` and `strip`/`lstrip`/`rstrip` answer what CPython answers — `"straße".upper()` is `STRASSE`, `"ﬁ".upper()`
  is `FI`, `"İ".lower()` is two characters, `"ΑΣ".lower()` is `ας` and `"ΑΣΒ".lower()` is `ασβ`, `"ǅ".title()` is `ǅ`,
  `"\x1cx".strip()` is `x` — because the answers come from `pkg/lang/testdata/case-table.json`, which
  `tools/casetab` generates by asking the reference every code point (1114112 asked; `go run ./tools/casetab -check`
  re-asks it), and because the compile-time fold and the emitted IR door read that ONE artifact rather than each
  carrying a rule. A module that never asks a case or trim question carries neither table. This is not Go's case
  engine: `strings.ToUpper("straße")` answers `STRAßE`, `strings.ToLower("ΑΣ")` answers `ασ`, and `unicode.IsSpace`
  does not count U+001C as whitespace (**ADR 0362**, roadmap L11.5, closing Gap N.2; `docs/language.md` § *A text is a
  sequence of code points* and § *Case and trim* are the authority). What is owed is named by the rows: a
  comprehension over a text (`[c for c in "abc"]`, `L11.1`/`Gap I.2`), and the search/split/pad methods over a
  receiver the compiler cannot read (`Gap I.2`).
- **Text formatting** — f-strings (`f"x{n}"`, an interpolation is a text value: ADR 0359) and printf-style
  `%` on a text: `"hi %s, you are %d" % ("Ada", 36)` is `hi Ada, you are 36`. The door renders
  `%s %r %a %d %i %o %x %X %e %E %f %F %g %G %c` with the reference's flags, widths, precisions, the `*`/`.*`
  forms, positional `%2$s` and the mapping form `%(key)s`; a field is measured in characters, and the traps
  are the reference's own classes and sentences, catchable by the program's `except` — `"%d" % "text"` raises
  `TypeError: %d format: a real number is required, not str`. The answer is an ordinary text, so it binds,
  measures, concatenates, compares, enters a container slot and crosses a function boundary
  (**ADR 0360**, roadmap Gap R.31; `docs/language.md` § *Printf-style `%`* is the authority).
- **Slicing** — `s[a:b]`, `s[::step]`, negative indices, lowered by the `rt_slice` runtime helper; the
  index arithmetic (`pySliceIndices`, `normPosIndex`) is shared code in `pkg/lang/fold.go`.
- **Generators** — `def g(): yield a; yield b` collects yielded values, and **each yielded value keeps its kind**:
  a buffer that yields a text, an integer, the void and a float walks as `a` / `2` / `None` / `1.5` and prints
  `['a', 2, None, 1.5]`, at the module door and through a generator METHOD alike (**ADR 0433**, roadmap Gap R.468).
- **Method calls fill the callee's parameter vector** — `c.m(5)` into `def m(self, a, b=2)` answers `2`, and
  `c.m(b=7, a=3)` into `def m(self, a=1, b=2)` binds by name and answers `37`: every method road (static receiver,
  `super()`, the run-time class-id switch, `mod.fn(...)`, `C(...)`'s constructor) emits one word per parameter the
  METHOD declared, filled from its own default and read from the class that declares it (**ADR 0434**, roadmap
  Gap R.472). A call that cannot be laid over the signature is refused in words at exit 1, not answered. And the word
  the default wrote carries its KIND: `C().m()` into `def m(self, s="hi"): return s` answers `hi`, not the `@str_tab`
  position, because the text census reads the default that filled the slot (**ADR 0435**, roadmap Gap R.479).
- **Async** — `async def` / `await` / `async for` / `async with`, with the await/return
  discipline checked in the shared front end: dropping a coroutine, awaiting one twice, or
  yielding inside an `async def` is a compile error, and each function's effect signature is
  readable with `gustyc --effects` (ADR 0195).
- **Context managers** — `with expr as name:` / `with expr:`, dispatching
  `__enter__` / `__exit__` (including exception suppression); supported in
  the compiled backend.
- **Exceptions** — `try` / `except` / `finally` with typed built-in exception
  classes (`Exception`, `ValueError`, `TypeError`, `KeyError`, `IndexError`,
  `RuntimeError`, `StopIteration`, `ZeroDivisionError`) and `raise`.
- **Classes & inheritance** — `class Name:` with methods (`self`), instance
  attributes, `__init__`, `class Child(Base):` multi-level inheritance, and
  `super()` delegation.
- **Operator overloading** — binary operators dispatch to dunder methods
  (`__add__`, `__mul__`, `__lt__`, ...) with reflected fallbacks
  (`__radd__`, `__rmul__`, swapped comparisons), resolved from one table
  (`dunderForBinOp`/`reflectedDunder` in `pkg/lang/fold.go`) so the name a program defines and the name
  the runtime dispatches can never disagree.
- **Decorators** — `@dec def f:` → `f = dec(f)` at definition time; wrapping
  (fnptr-valued) decorators compile in AOT via compile-time specialization.
- **Modules** — `import mod` loads `mod.gy` and binds `mod` as a namespace with
  `mod.name` / `mod.fn(args)` access.
- **Standard library** — data-only on-disk modules folded as AOT constants:
  - `import math` — `pi`, `e`, `tau` (the reference's names, and the literals the module declares) plus
    `PI`, `E`, `TAU` (this language's own, aliased to the same literal), and `PHI`, `SQRT2`, `LN2`,
    `LN10` (each with a lower-case spelling too) — constants the reference's module does not carry.
  - `import string` — `DIGITS`, `LOWERCASE`, `UPPERCASE`, `HEXDIGITS`,
    `WHITESPACE`, `PUNCT`.
  - `import collections` — `EMPTY_DICT`, `EMPTY_LIST`, `ZERO`, `ONE`.
  - `import json` — `NULL` (`None`), `TRUE` (`True`), `FALSE` (`False`).

## The value model

Behind the annotations, a runtime value is one of fifteen kinds — `int`, `float`, `bool`,
`None`, `str`, `list`, `dict`, `set`, `tuple`, `class`, `instance`, `method`, `closure`,
`exn`, `module` — and one Go table says which. The compiled runtime's tagged values, the exported C ABI and
the garbage collector's root tracing all read those numbers, and the heap's own object-header kind is a
projection of them
(`list`, `dict`, `set`, `instance`, with 0 meaning "not allocated — an immediate or an
interned string"). `gustyc --lang` prints both tables and `--schema`'s `valueTag`
definition documents the numbering (ADR 0182).

A container lives in one arena, and a handle is an **index** into its parallel tables rather than an address —
which is why the arena can **grow**: the allocator commits a window of slots when the run asks for one past
the window it has (`@heap_cap`), doubling it from `HeapInitialCapacity` (1024) to the reservation
`HeapCapacity` (8192) with no reference fixed up anywhere, and reports the run's size as `capacity=` and the
number of widenings as `grown=` (`--gc-stats`, the `--json` `gc` member, `--schema`'s `gcStats`). Past the
reservation the answer is a trap the program can name — `MemoryError`, catchable, exit 3 uncaught — and never
again the handle `-1` painted as an object (**Gap Q.2**, paid by **ADR 0347** for the diagnostic and
**ADR 0350** for the growth; `docs/language.md` § The compiled arena).

A float keeps the sign the program gave it, whoever wrote it: `print(-0.0)` writes `-0.0` — through a binding,
a container slot, `str()` and a function's parameter too — while the operations that remove a sign remove it,
so `abs(-0.0)`, `-(-0.0)` and `abs(n)` over a slot the run time owns all write `0.0`. A signed constant is
stated by its IEEE bits on its way out of the compiler (the old idiom added `+0.0` to it, which IEEE answers
for `−0.0` with `+0.0`), and the runtime's signless `abs` clears the bit rather than asking whether the value
is below zero — the question that is false for `-0.0` (ADR 0349, closing roadmap Gap R.132).

A compiled container can mix numbers, strings and `None`: `print([1, "a", None])` prints
`[1, 'a', None]` on the compiled backend, because each element slot carries its own tag (ADR 0184). Dicts
and sets take the same rule, so `print({"a": 1, "b": "x", "c": None})` and `print({1, "a", None})`
print correctly compiled, `for k in d` / `for x in s` bind the tag beside the value, and a container
that grows a second kind is promoted rather than refused (ADR 0232). The tag is not only for mixed
containers: a lookup compares payload *and* tag, because an interned string and an integer of the
same number are the same bits — `{1: "one"}` asked for `"a"` raises `KeyError` compiled, where it
used to answer `one` (ADR 0189 wrote the tags; ADR 0232 made the runtime read them).

Where the tag does not decide yet, one rule does: `str(x)` folds to the same text on both
backends and on CPython — `str(None)` is `"None"`, `str(1.5)` is `"1.5"`, `str("x")` is `x`
(ADR 0183).

A slot is read back by the tag its builder wrote, remembered at compile time (ADR 0241): while a name is
bound exactly once to a container literal and nothing mutates it or takes it past an unseen callee,
`len(xs[0])`, `xs[0][1]`, `d["a"][1]`, `t[0][0][0]`, `xs[0] == [1, 2]`, `2 in xs[0]`, `for v in xs[0]` and
`y = xs[0][1]` all answer on the compiled backend — and a slot the compiler can see holding a number *is* that
number for arithmetic, so `xs = [1, "a"]; print(xs[0] + 1)` prints `2` and `ys = [1.5, "a"]; print(ys[0] * 2)`
prints `3.0` (ADR 0243). Where the promise runs out — including rebinding the very name the read goes
through — the answer is a refusal naming the promise, not a payload read back as a handle.

A `{…}` display ends at its brace: `[{1, 2} for x in xs]` is a list of two sets, not a set, because the
`for` after the brace belongs to the enclosing list comprehension — only a display that *is* the whole
expression (`len({x*x} for x in xs)`) finishes itself into one. And a comprehension's element is a value:
a container, a float, `None` or text enters the slot with the tag that says what it is, payload and tag in
one write — the rule `xs.append(v)` already followed — so `[[1, 2] for x in [1]]` prints `[[1, 2]]` rather
than the inner list's address, `[1.5 …]` prints `[1.5]` rather than a box handle, and `[None …]` prints
`[None]` rather than the `0` that `if None:` folds to (ADR 0244).

**A literal iterable need not be a list of integers** (ADR 0386, closing **Gap R.358**). `[1, 2]` still takes the
integer fold. Any other literal display — text, float, `None`, verdicts, tuples, containers, or calls — now walks
item by item and binds the comprehension's loop variable with the same per-item tag an unrolled `for` records. So
`[x for x in ["a", "b"]]` is `['a', 'b']`, `{k: 1 for k in ["a", "b"]}` is `{'a': 1, 'b': 1}`, heterogeneous
`[1, "a", 2.5, None, True]` prints every kind, and `[[x] for x in [None, 1.5, True]]` is `[[None], [1.5], [True]]`
rather than the handles those payloads happen to be. Positions that read one word still refuse to guess a tagged
loop variable's kind rather than turn a string index or float box handle into an integer.

What the loop variable knows is part of the same rule: an element that *is* the loop variable carries its
kind — and, over a container whose slots mix kinds, its **tag** — into the list it builds, and it asks the
**container being iterated**, not the loop variable, whose facts are gone once the loop closes. So
`print(out)` and `print(out[0])` tell one story (`['a']` and `a`) instead of one printing text and the
other the interned index (Gap R.46, closed), and `[x for x in {1, "a", None}]` prints `[1, 'a', None]`
where the compiled backend printed `[1, 0, 0]` (Gap R.76). Iterating a dict walks its **keys** at the
stride its two-word entries need, so `[k for k in d]` over `{1: "x", 2: "y"}` is `[1, 2]` and not the
`[1, 0]` — a key and a value — the compiler used to hand back (Gap R.77); and a dict comprehension writes
each entry as two `(payload, tag)` pairs with the tag its key really has, so `{k: 1 for k in d}` over a
text-keyed dict prints `{'a': 1}` and `out["a"]` finds it instead of dying with `KeyError` (Gap R.78).

A **dict is a key → value mapping however it is built** (ADR 0260, closing Gaps R.118 and R.120) — which is
another way of saying the interpreter used to *append* entries. `print({"a": 1, "a": 2})` printed
`{'a': 1, 'a': 2}` and `len` printed `2`; `{1: 2 for x in [1, 2]}` printed `{1: 2, 1: 2}` — the literal, the
comprehension and the `dict(d)` copy each grew the entry arrays, and only item assignment asked the dict
whether it already held the key. All four now walk one door, so the entry keeps its **position from first
insertion** (`{"a": 1, "b": 2, "a": 3}` is `{'a': 3, 'b': 2}`, because `for k in d` walks insertion order)
and takes its **value from the last write**, while the **key that survives is the first one written** —
`{1: 'a', True: 'b'}` prints `{1: 'b'}` and `{True: 1, 1: 2}` prints `{True: 2}`, agreeing with CPython
because `1`, `True` and `1.0` are one key (ADR 0259's key equality). This is the rare row where the
*interpreter* was the diverging engine and the compiled answer was CPython's; both legs had disagreed
about an ordinary dictionary until a program wrote one.

A **dict literal** is a dict, so it is read by the door a dict *variable*'s entries are read by (ADR 0327).
`{"a": 1}["a"]` is `1`, `{"a": "x"}["a"]` prints `x` rather than the `@str_tab` index it is interned by,
`{1: 1.5}[1]` is `1.5`, `{1: None}[1]` is `None`, `{1: "a"}[True]` is `a` — the reference's key identity, where
`1`, `1.0` and `True` are one key and a text never is — and `{"a": [1, 2]}["a"]` prints `[1, 2]`. A key the
literal does not hold is the reference's exception, with the key's own repr as its message and a handler that
catches it: `{"a": 1}["b"]`, `{"a": 1}[1]` and `{1: 2}["a"]` each die at exit 3 under `KeyError`, and
`except KeyError:` runs. The compile-time lookup stays for the one shape it can read — an integer key into a
literal whose keys and values are all integers — and it keeps the reference's repeated-key rule, so
`{1: 2, 1: 3}[1]` is `3` rather than the `2` the fold used to answer (Gap R.216).

A **list literal** is a list, and ADR 0331 takes the same decision one container over:
`["a", "b"][0]` is `a`, `[1.5, 2][0]` is `1.5`, `[None, 2][0]` is `None`, `[True, 2][0]` is `True`,
`["a", "b"][-1]` is `b` and `[[1, 2], [3, 4]][0][1]` is `2`. Until now those four printed `0`, `1`, `0` and
`1` — the element's *payload*, an index into `@str_tab` or a `@float_box` handle — at exit 0, while the very
same element read off a variable (`xs = ["a", "b"]` / `print(xs[0])`) answered correctly, because the door that
answers a subscript as a `(payload, tag)` pair had a dict arm and never grew the list arm beside it. The
literal is now built the way the binding road builds a list and read by the same call `xs[i]` emits; the
compile-time lookup stays for the shape it genuinely reads (`[1, 2, 3][0]` is still a constant, asserted by an
IR test that fails if it starts allocating), a position past the end is the catchable
`IndexError: list index out of range`, an index that is not a position is `TypeError: list indices must be
integers or slices, not str`, and a condition over such an element asks the runtime's truth table rather than
comparing a handle to zero — `if [0.0, 1][0]:`, `if [[]][0]:` and `if [""][0]:` are all false (Gap R.218).

A **literal's element that is itself a read** is stored as the pair that read answers, and ADR 0332 closes the
layer ADR 0331 named: `print([{"a": "x"}["a"], 2])` is `['x', 2]` where it printed `[1, 2]` at exit 0, and
`print([["a"][0], 2])`, `print([[1.5, 2][0], 3])` and `xs = [1, "a"]` / `print([xs[1], 2])` — which refused —
are `['a', 2]`, `[1.5, 3]` and `['a', 2]`. The print, binding, membership and length doors had always asked a
subscript for a `(payload, tag)` pair; the container builder alone asked for one word and then labelled the
slot from the element's spelling. It takes the register the objects wrote now — from the same call
`print(xs[i])` emits — and a dict entry, a dict key and a set member ride that one door with the list element:
`{1: {"a": "x"}["a"]}` was `{1: 1}`, `{{"a": "x"}["a"]: 1}` was `{1: 1}`, `{{"a": "x"}["a"]}` was `{1}`.
The fold keeps the shapes it can read (`[[1, 2][0], 3]` is `[1, 3]`, still a constant with nothing allocated),
a raise inside an element is the program's own catchable exception, and the position that keeps one word for a
whole value still refuses rather than printing a payload (Gap R.146). What the door still cannot reach is
filed with both answers written into the row: an element that *contains* a read (Gap R.225), the loop that
unrolls a literal directly (Gap R.226) and a subscript of a subscript of a literal (Gap R.227). The fourth
row it carried — a container crossing a `return`, recorded as answering `0` — answers `['x', 2]` as of ADR
0333 below, and sits in that row's answering table rather than this one.

A **container returned from a call is a value**, and ADR 0333 closes the oldest container row in the queue.
`def build(): return [1, 2]` used to emit `ret i32 @.lst1` — a global in a value position, `llc` refusing the
module and `gustyc` exiting **2**, the code the contract reserves for a compiler bug, on a two-line program
CPython prints `[1, 2]` for. The two shapes that did build were worse for being quiet: `xs = []` /
`xs.append(1)` / `return xs` printed `0`, `for v in build():` printed nothing at all, `if build():` was false
for a two-element list, and `str(build())` wrote the handle's digits. The `return` now materialises through
the builders a binding already uses, so the word crossing the `ret` is a heap handle in a register — **no
emitted module under this row contains `ret i32 @.`**, by an IR-shape assertion put through `llc` — and
`containerReturningFuncs` records the one kind a function's arms all answer, so the call site (which holds
one word and a name, and cannot see a body) reads the answer as the container it is. `print`, `str`/`repr`/
f-string, `len`, `in`, `not in`, `not`, truthiness, a `for` iterable, `==` and a binding then answer the way
the bound form `ys = build()` always did — because they are the same read of the same handle. A function is
claimed only when **every** road through its body agrees (Gap R.219 is the standing lesson: a function
registered as text-returning on the evidence of one arm prints the other arm's number as a byte string), and a
function the scan declines keeps the road it had. A call site that hands a **double** into a parameter is left
to the pair road with its honest `(payload, tag)` sentence: `print(b(2))` is claimed and answers `[2, 1]`,
`print(b(2.5))` is declined — the same body, the caller's word as the difference. `programs/container_returns.gy`
is `match` on both legs; what still refuses — the
answer as another literal's element or as a method receiver (**Gap R.229**), a slot of a copy read directly
(**Gap R.235**), the answer through a parameter (Gap R.154), `build() + [9]` (Gap R.175), `None` returned from
a function (**Gap R.228**) — names the function and the door, and never exits 2.

**Sequences are built, not refused** (roadmap Gap R.33, closed by ADR 0365). `[1] + [2]`, `[1] + [2] + [3]`,
`[1] * 3`, `3 * [1]`, `(1, 2) + (3,)`, `"ab" * 2` and `2 * "ab"` compile to an object the program never wrote —
a heap block whose slots each carry their tag, or, for a text, an `@str_tab` index, which is the word every
text road already reads — and the object answers `print`, `str`/`repr`, an f-string field, `len`, `in`, `==` and
a `for` the way one a literal made does. `programs/sequence_builds.gy` is a parity row, line-for-line the
reference. What the reference **raises**, raises here too: `[1] * 2.0` and `"a" * 1.5` say
`can't multiply sequence by non-int of type 'float'`, `"a" * "b"` names `'str'`, `[1] + "a"` names `'str'` on the
other side, and `{"a": 1} + {"b": 2}` says `unsupported operand type(s) for +: 'dict' and 'dict'` — at exit 3,
catchable by the program's own `except TypeError`. Three shapes stay refused in words, each naming the row that
owes them (**Gap R.314**): the subscript of a built sequence, the order of two containers no name holds, and a
repeat the compiler can see needs more slots than one object owns.

**A method is a callable, so its parameter and its answer carry their kind** (roadmap Gap R.315, Gap R.316,
Gap R.317, Gap R.318 — closed by ADR 0366). The censuses that decide what a callable's slot holds, and what its
answer is, read `def`s; a method never entered them, so four ordinary programs answered themselves wrongly:
`def fmt(self, s): return "x%s" % s` printed `x0`, `def rep(self, s): return s * 2` printed `0`, `def yes(self):
return 1 == 1` printed `1` where its own function twin printed `True`, and `def upper(self, s): return
s.upper()` called itself until the process died by SIGSEGV. All four answer the reference now: the method road
imports the parameter census the function road grew over three cycles, the print road asks an *attribute*
callee whether its body returns only verdicts (and what every write of a field hands it, for `return
self.flag`), and a receiver the compiler can see is a text belongs to the case door whatever the enclosing
class calls itself. `programs/probe_method_text_answer.gy` and `programs/probe_method_verdict_answer.gy` are
parity rows, byte-for-byte the reference. Two shapes stay pinned where they are: a text held in a *field*
still prints the intern position under it (**Gap R.319**, `programs/probe_method_text_field.gy`), and a
defaulted method parameter or a `self.other()` call still refuses in words (**Gap R.320**) — never a crash,
never exit 2.

**An unpacking binds each name the way an assignment binds its value** (roadmap Gap R.144 — closed
by ADR 0367). The doors that decide whether a value travels as a `(payload, tag)` pair belonged to
the plain binding: an element of `a, b = …` went through the one-word road and recorded nothing, so
`a, b = 1, None` printed `1 0`, `a, b = "a", "b"` printed the intern position `0 1`, `a, b = 1, 2.5`
printed `1 2`, `def f(): a, b = 1, "z"; return b` returned `0`, and `a, b = xs[0][0] + 1,
xs[0][1] + 2` — the arithmetic the plain binding answers with `8` — spent exit 1 on `index cannot
reach into xs's slots`. The doors are one ordered list now (`pkg/lang/assign_pair.go`), asked per
element by both binding roads and bound through `bindAssignPair`, which is ADR 0187's binding door
plus the tag's origin and the rooting; every value is still read before any name is written, so the
swap keeps swapping. `programs/probe_pair_from_a_tuple_unpack.gy` is that shape as an answered row
and `programs/unpack_pair_binds_by_its_tag.gy` walks every element kind through the door, both
byte-for-byte the reference. What the pair still cannot carry says so — the operand of a binary `+`,
a condition, `a[0]` (**Gap R.323**), and binding a pair-bound name again, a non-name target or
unpacking one collection (**Gap R.321**); an interned text name that answers from the fold, and a
`None` returned out of a function, are the two wrong answers this row found and filed (**Gap
R.322**).

**A call that answers more than one kind carries its kind beside its payload** (roadmap Gap R.219, Gap
R.228 — closed by ADR 0368). `def f(x):` / `    if x:` / `        return "x"` / `    return 0` with
`print(f(1))` / `print(f(0))` printed `x` and `x`, because the first text seen anywhere in that body
registered `f` as text-returning and the caller then handed `printf` an interned `@str_tab` index with `%d`
for BOTH arms — the number being the answer nobody wrote. The same missing word printed `0` for CPython's
`None` where a body fell off its end, `1` for its `True`, `0` for its `{'k': 1}`, and handed `llc` a
`ret i32 @.lst1` for a container arm: exit 2, the exit ADR 0166 forbids. One program-wide scan
(`pkg/lang/answer_kind.go`) now answers, before any IR exists, which bodies hand back more than one kind and
what word each `return` road stores; the one-kind tables stop being the authority for such a body, and print,
`str`/`repr`, a condition, a container element, a binding and an f-string field ask the tag instead of guessing
the kind from the payload. Of a 25-shape probe measured against a build of the parent commit, **12 answers
changed and none regressed**. What it still gets wrong is named rather than smoothed over — an answer into an
ordinary parameter, a method's mixed arms, a float arm beside anything else, and `return "x" if x else 0`
(**Gap R.324**) — and the word is visible to a tool without running the program: `--emit-llvm` shows
`@gy_f.anst` declared, stored on each road and loaded by each caller (**ADR 0368**'s machine path).

**A subscript of that answer is read by the door that reads a variable's slot**, and ADR 0334 pays the row ADR 0326
named and left open. `print(build()[7])` is `IndexError: list index out of range` in the reference; here it was
`codegen: index requires an inline list/dict/set literal` at exit 1 — while `ys = build()` / `print(ys[7])` had
raised the same trap since ADR 0326 and `print([1, 2][7])` had raised it in the fold. One question, three doors, and
the missing one chose its road from the **spelling of its base** (a call is not a literal) rather than from what the
compiler holds once the answer is lowered. The fix is a rewrite, not a second reader: the call is lowered to the
rooted name ADR 0333 records its notebooks under, the subscript node is **copied** with its base set to that name,
and `ys[i]`'s roads answer — the kind roads first, then the mixed reads, then the tagged read, with ADR 0330's bounds
check behind them. `print(build()[0])` is `1`, `print(grid()[1][0])` is `3`, `print(cfg()["b"])` hashes a key out of
a dict answer, and the out-of-range position raises the reference's class, message and exit code — catchable by the
program's own `except`. Two ordering facts came out of it: the rewrite must run **before** the print door's kind
roads, or a dict built from a literal prints its `@str_tab` index as a number; and a tagged answer must **clear** its
element notebooks the way `heapDictFrom` clears them for the variable it builds into — leaving `listElemStr` set over
a tagged list was why `def words(): return ["hi","ho"]` refused `print(words()[1])` with Gap R.82's sentence while
printing the whole list correctly. The slice was left refused for exactly that reason — its road answered
`[1, 0]` for a mixed list and `1` for a bound slice whatever the base is (**Gap R.230**) — and **ADR 0335**
closes it, below. `programs/container_subscript.gy` is `match` on both legs, and the neighbours that keep
their own sentences — a set answer (Gap R.217), an answer as an item-assignment target (Gap R.229), a numeric use of
two slots (Gap R.82) — are asserted to stay refusals without exit 2, beside three rows measured on the way and filed
rather than bundled: a number subscripting a text-keyed dict answers the slot at that position (**Gap R.231**),
a slice that drops its slots' tags and a slice bound to a name that answers the handle (**Gap R.230**, paid by
**ADR 0335**), and a
membership test whose needle is a slot, refusing with a sentence that prints the needle's Go representation
(**Gap R.232**).

**A refusal is part of the model, so it is part of the machine document too** — ADR 0346 pays the diagnostic
half of L11.8. The exit-code half landed with ADR 0211 (refusal 1, `llc` rejection 2, trap 3, on every path);
what stayed open is the sentence. A capability refusal reached a human as prose and a script as
`"error": "codegen: …"`, so an agent planning a compilation had to match on the wording to learn what it was
refused — and prose is not a contract: the sentence gets edited, the branch silently stops firing, and the
failure is a script that quietly stops covering the shape. `pkg/lang/capability.go` is now the single source for
the code (`lower.unsupported.<shape>` — one per refused shape, and `gustyc --capabilities` is the inventory the
registry prints rather than a number this document remembers), the suggestion that writes the same intent in what the
backend does answer, and the roadmap rows that own the missing half. It reaches three readers: the `capability`
member of every `--json` failure payload, `gustyc --capabilities` as one self-describing document, and
`--schema`'s `definitions.capabilityRefusalCode` — where the enum is *spliced out of the registry at init*,
because a second hand-typed list of the same fact is a second thing to drift. Two rules keep it honest, and both
fail loudly instead of silently: **nothing is invented** (the classifier answers the longest marker its sentence
contains, and one that matches nothing gets `null` rather than a guessed code — a typo stays a typo), and
**coverage is measured rather than claimed** (`TestEveryCapabilityRefusalInTheCorpusIsClassified` compiles the
whole conformance corpus and fails on any refusal without a code, except an explicit allowlist where each entry
carries the reason it is the program's own error; `TestTheDocumentedCapabilitySentencesStillExist` greps every
sentence the docs tell an agent to match out of the compiler's own string literals, and caught three rows that
advertised sentences the backend had stopped emitting; `go run ./tools/captable -check` holds the doc table to
the registry). No `codegen:` site was touched — a typed error at 142 refusal sites would compile with a forgotten
type and rot quietly, where a registry whose sample no longer classifies is a failing test the same afternoon.

**A function used as a value is specialised into names before the doors that decide a call are asked** — ADR
0345 pays the called half of L11.7. One sentence had stood between this backend and every higher-order program
since it existed — `"fn" is called but is not a function this backend can build a call to … (roadmap L11.1,
ADR 0166)` — and seven doors each carried their own version of it, because a callee the compiler cannot name is
one question each door answered in its own words. The pass in `pkg/lang/callable_value.go` asks it once, first:
`defunctionalizeCallables` reads the whole program (calls, name occurrences, whether a name is a callee, every
binding), plans a rewrite or declines, and only then deep-copies the tree — so the container-answer scan, the
float and text return scans and the pair road's arity all read the tree the module is built from, and a rewrite
that arrived after them would be a second opinion about a program they had already answered. A higher-order
function is cloned once per tuple of functions its call sites ask its parameters to be (the original retires with
`SkipEmit`, so no unspecialised body survives to be called), a `lambda` that must be named gets a declaration of
its own when its body reads nothing but its own parameters, and a name whose whole history is one binding plus
calls is retargeted at the function it means. What it cannot prove it hands back **untouched** — the pass's only
safe answer, and the reason it can run before the front end's own tools are asked — so the refusal a reader sees
is the honest one that was already written. `twice(d2, 3)` is `12`, `apply1(inc, 41)` is `42`, `g = d2` / `g(4)`
is `8`, `sorted([3, 1, 2], key=lambda x: 0 - x)` is `[3, 2, 1]`, and `max`/`min` with a `key=lambda` likewise —
and the two oracle probes that recorded this debt, `probe_fn_value` and `probe_fn_name`, are conformance rows
now, recorded from the reference through `tools/recmerge` (the record grew 6877 → 6879, to 6880 when
Gap R.111's promoted program was recorded the same way, to 6882 with Gap R.132's signed-zero rows, to 6888 with Gap Q.2's
growth programs and to 6931 with Gap R.49's twenty module-scope crossings, its corpus program and Gap R.263's twenty-one defaulted-`lambda` shapes, to 7009 with Gap R.221's seventy-seven key×value readings, its corpus program and Gap R.291's promoted comprehension-over-a-folded-list program, and to 7061 with Gap R.68's fifty-two capture readings and its corpus program — the count is the file's
own and not this sentence's, and `recmerge` refuses to write an entry whose compiled leg disagrees with the reference). There is no `fnptr` word and no indirect `call`: the pass never puts a handle in
a value slot, two test tables forbid `ptrtoint`/`inttoptr` across every probe, and `print(f)` still refuses —
the boundary is *called* versus *read*, and the read half is L11.1's. Three things the first draft got wrong and
the tests now hold shut: names were minted from `%p`, which Go randomises per process, so one program compiled to
two different modules until `nodeSeed` seeded from a node's ordinal in the plan (the determinism the record and
every lit test read — Gap R.162's shape again); the reference-leg test used the package's `compiledPrints`, which
answers from the *record* when the record holds the source, so four probes passed without the compiler being asked
anything; and a hoisted lambda reported its binding as its name until a CLI row that had been passing by accident
caught `too many arguments for g` where CPython says `<lambda>`. `gustyc --callables <src>` prints the pass's own
account — clones, hoisted lambdas, minted symbols, and the shape it declined, with the row that owns it — and the
terminal now quotes that account where it matters: when the pass withdrew a rewrite and the emitter is left refusing
the call the rewrite would have built, the refusal carries the pass's sentence verbatim, under a label that says who
is speaking, so `def use(seed):` / `return apply1(lambda x: x + base, seed)` tells the reader `an entry lambda reads
"base" from the frame its application is written in … Gap R.336` rather than pointing them at the memory-model row
that owns nothing in the program — while a program the pass never spoke about keeps the old sentence byte for byte,
and `--json` still classifies the longer message as `lower.unsupported.indirect-call` (**ADR 0379**, **Gap R.337**).
The seven shapes still refused each got a row: **Gap R.263** (a `lambda` with a default parameter never reaches the
pass because the parser refuses it), **R.264** (a hook lambda whose body asks a door the call site cannot answer),
**R.265** (a `lambda` that calls its own parameter, and a `lambda` inside a `lambda` — closed since by **ADR 0370**),
**R.266** (a function a
call produces), **R.267** (a higher-order body that calls itself — closed since by **ADR 0364**), **R.268** (a bound method or a `__call__`
object as a callable), **R.269** (`list.sort` still refusing the options `sorted` was taught to read).

**A container the answer table declines is still a container, and the doors are told who they ask** — ADR 0344
pays Gap R.261, the wrong answer the higher-order door turned up. `containerAnswerScan` had always asked two
questions — does this function hand back a container, and can the table describe what its slots hold — and the
toolchain read only the first, so a function declined for the second was simply absent, and absence meant "not a
container" to the ordinary call road. `def g(): return 5` / `def f(): out = []` / `out.append(g())` /
`return out` / `print(f())` printed **`0`** at **exit 0** — the heap handle's address digits — and the same over
`out.add(g())` (`{5}`), over `out["k"] = g()` (`{'k': 5}`), over a float-returning callee (`{'k': 2.5}`), while
`for v in f(): print(v)` printed *nothing at all* where CPython prints `5`. The scan now keeps the decline as a
fact (`(described, undescribed)`), and which positions may read such an answer is a method on the record —
`containerAnswer.takesRead` — not a choice each door makes for itself: the positions whose question the **object**
answers (`print`, `str`, `repr`, an f-string field, a condition, `not`, `in`, a binding) take the handle and ask
it, and the positions that read a slot back through the compiler's own notebooks (an index, a slice, a
comparison, a loop's iterable) refuse at exit 1 naming the missing half and the rewrite that works — `bind it to
a name and print that`. Every door that does not name its read asks the strictest question, so the change is
monotone: a door only ever gets wider. The two readers left shut are filed as **Gap R.262** with CPython's answer
written into the row, because opening them means recording what a declined slot holds — and a slot filled by a
call may be the `(payload, tag)` pair ADR 0273 exists to keep out of this table.

**A slice hands back a copy of the base's slots, tags included, and the copy is a list the records know** — ADR
0335 pays Gap R.230, the row ADR 0334 filed rather than widen. A slice was the one container operation that
built a **new** container out of words it read one at a time, and it dropped the tag on the way through:
`xs = [1.5, "a", [7]]` / `print(xs[0:2])` printed `[1, 0]` — the text's `@str_tab` index and the float's box
handle, read as the numbers they are not — and `ys = xs[0:2]` / `print(ys)` printed `1`, the copy's heap handle,
both at **exit 0**. Three roads each held a third of the truth, and each now asks the same question of the site
that wrote the bytes: the printer asks `mixedLists` before the single-kind notebooks, the way the name arm
beside it always did; the road that builds the sliced literal asks **its own** predicate
(`literalNeedsHeap || literalNeedsTags || literalHasPairElement` → `heapListFromTagged`), so `rt_slice` copies
tags somebody actually wrote; and `recordContainerSlice` records the binding — kind, tag fact, and the element
notebooks cleared when the copy is tagged, because a tagged object has no single kind. From that one record come
`print(ys)`, `len(ys)`, `for v in ys`, `ys[0]`, `not ys`, `2 in xs[0:2]`, `xs[0:2] == [1.5, "a"]`, a slice of a
slice, and — with ADR 0334's rewrite grown to the `*Slice` case — `print(build()[0:2])`. A list whose slots are
all one kind keeps the plain word copy and the plain printer, asserted both ways: a slice road that tagged every
copy would send a plain `[1, 2, 3]` to a tag array nobody wrote (**ADR 0249**'s gate and door, growing together).
In front of the copy stands `sliceBaseIsDescribed`, and it is the load-bearing part: handed a base the records
cannot describe, the road used to emit `rt_slice` over whatever word the base lowered to and let the caller print
it — `print(m()["k"][0:1])` answered `3` — and three shapes handed a compile-time global's address to a helper
whose parameter is an i32 register, which `llc` rejected: **exit 2**, the code ADR 0166 reserves for a compiler
bug. The gate now answers the bases it can describe — a literal, a recorded variable, a scan-proved call, a copy,
a **brace** the program wrote and a **slot** whose object the door can pin to the expression that wrote it
(**ADR 0459**, **Gap R.235**) — and refuses the rest with a sentence naming the missing record: a base that is a
set or a dict, which the reference *raises*, is **Gap R.234**, a copy taken from a name whose last binding was a
slot read is **Gap R.540**, a copy of a brace whose loop copies a mixed container is **Gap R.539**; the copy's
bounds are a third question, still unanswered, filed as **Gap R.233**
(`print(xs["a":"b"])` answers `[1]` where the reference raises `TypeError: slice indices must be integers or None
or have an __index__ method`). `programs/slice_answer.gy` is `match` on both legs.

**A slice bound is asked what it is**, and ADR 0336 pays Gap R.233 — the question ADR 0335 left outside its own
gate. `rt_slice` takes seven i32s and the road handed every bound to the generic expression door, so a text bound
was its `@str_tab` index read as a position: `print(xs["a":"b"])` answered `[1]` and `print(s["a":"b"])` over
`"abcd"` answered `b`, all at exit 0, while the reference raises `TypeError: slice indices must be integers or
None or have an __index__ method`; `print(xs[[0]:2])` handed a list literal's global address to an i32 argument and
`llc` rejected the module (exit 2). The other half was the bound the reference *excuses*: `None` lowers to the word
0 and the presence flag was set beside it as though a bound had been written, so `print(xs[None:None])` answered
`[]`, `print(s[None:None])` printed nothing, and `print(xs[::None])` was killed with exit 2. A bound now has two
legal shapes and one illegal one — an integer (a `bool` included, since `True` is `1`) is a position, `None` is the
absence of one, and anything else the compiler can name the kind of raises the reference's sentence through the
non-local road, so `except TypeError:` takes it — and the ask happens **before any bound is lowered**, which is
what keeps a container-valued bound out of an i32 slot. It runs after ADR 0335's base question, in the reference's
order (`{1,2}["a":"b"]` complains about the set first), and every position that reads a copy asks it — print, `len`,
a `for`, a condition, `==`, `in`, a binding — because they all lower through one door. Where the kind is a
run-time fact the gate stays silent rather than guess (ADR 0232's rule: a claim about a kind is a claim about
bytes some writer wrote), and the silence is priced: a bound hidden in a slot, in a call's answer or in a
float-carrying parameter still answers `[1, 2]` (**Gap R.237**), a slice handed back by a function still travels as
one word, so `print(n())` over `return xs[1:3]` answers `1` while `ys = n()` prints the list (**Gap R.238**), a
comprehension over a copy refuses although the named copy iterates (**Gap R.239**), and `len` of a slice of a text
refuses although `len` of the text and `len` of a list copy both answer (**Gap R.240**).

**The step of zero is asked what it holds, and ADR 0337 pays Gap R.236** — the worst row of that family, and the one
that could not be witnessed: `rt_slice` walked its loop forever on a zero step, the run was killed, and **exit 2**
reported the compiler's own bug (ADR 0166) for `print(xs[::0])`; `print(xs[0:2:0])` answered `[]` at exit 0 and
`print("abcd"[::0])` was refused. All three now raise the reference's `ValueError: slice step cannot be zero`,
catchable by `except ValueError:`. The fix is three-way where the value is: a step the compiler reads as zero raises
with **no copy built and no counter advanced** (ADR 0273's purity — `call i32 @rt_slice(` is absent from a raising
module), a step it cannot read carries a guard beside the call, and a step it reads as anything else asks nothing, so
the ordinary `xs[::2]` module is no fatter than it was. The text road keeps what it cannot answer: `rt_str_slice` has
no stride argument, so `s[::k]` still refuses (**Gap R.243**). Measuring this row also found a defect that is not the
slice road's and is the queue's worst class: `y = x-1` — no space before the minus — **answers `x` at exit 0**, and
`print(1-1)` is a parse error, because the lexer emits `-1` as one token whenever a minus is followed by a digit and
never asks what came before it (**Gap R.242**, priority 1, the next row this loop takes). Also filed from the same session, and not a slice defect
either: an `except` arm that **calls** a user function runs its handler and then takes back the exception the handler
already handled — traceback, exit 3, the statement after the `try` skipped (**Gap R.241**).
`programs/slice_bounds.gy` is `match` on both legs.

**The minus between two expressions is the operator, and ADR 0338 pays Gap R.242** — the priority-1 defect that
measuring the slice door turned up. The lexer read `-` followed by a digit as one negative integer wherever it
appeared, never asking what came *before* it, so `x = 5` / `y = x-1` / `print(y)` wrote **`5`** at exit 0 where
CPython writes `4`: the program never contained a subtraction — it was `y = x` and an expression statement `-1`
whose value nothing prints — and no diagnostic could have said so. The same rule met an expression the parser
could not swallow, and `print(1-1)`, `print(x-1)`, `print(len(xs)-1)`, `print(xs[2-1])`, `print(3.5-1)`,
`for i in range(3-1):` and `def g(n): return n-1` all died with `expected ")"`; the spaced spelling (`x - 1`) was
always right, which is the only reason the corpus never saw the tight one. The rule now asks the previous token,
which is already in hand: after a number, name, text, closing `)` `]` `}` or one of `True`/`False`/`None` the
minus is the operator and the parser's existing unary minus takes the rest; everywhere else — start of line,
after an operator, after `,` `:` `(` `[` `{` `=`, after `return`/`in`/`else` — `-N` stays one literal, so `print(-1)`,
`xs[-1]`, `{-1: "neg"}`, `f(-1)`, `2 ** -1` and `def f(a=-1)` answer exactly as they did, and `x -= 1` is still the
longest match. The token stream is pinned both ways, the parse is pinned as *one* statement and not two, and the
module is pinned to contain the `sub i32` the program asked for (`programs/minus_operator.gy`, `match` on both
legs, 22 lines of CPython beside it). What the row leaves standing is filed, not folded in: **`+` before an
operand is refused** (`print(+1)` is `unexpected token` where CPython prints `1`) — the grammar has no unary-plus
rule at all (**Gap R.244**, filed by this row) — and a comprehension whose iterable is a list the compiler kept as
a compile-time constant still refuses (roadmap **L11.1**, the tagged value word, whose refusal carries ADR 0192's own sentence).

**A `for` loop asks its iterable what it is, and ADR 0339 pays Gap R.246** — the loop had three roads and no
question in front of them, so anything the compiler could not name fell through to the numeric counter road and
was read as a *repeat count*. `for v in sorted(xs)` over a heap list emitted `icmp slt i32 %_ctr1.ld1, @.lst1`
— the counter told to count up to the **address of a list global** — and `llc` refused the module: exit 2, the
code the contract reserves for a compiler bug, on the most ordinary program in the language (the same five
programs CPython answers `1 2 3`). The shapes whose comparison *succeeded* were worse, because they ran:
`for v in sorted([3, 1, 2][0:2])` printed `0` where CPython prints `1` and `3`, and `for v in sorted(["b",
"a"])`, `for v in reversed("abc")` and `for v in sorted(b())` printed **nothing at exit 0**. The builtin itself
was never the problem — `print(sorted([3, 1, 2]))` has always answered `[1, 2, 3]` — the loop never asked what
it was about to step over. It asks now: a container the compiler can write down is folded to the literal it
denotes (`reversed(sorted([1, 3, 2]))` is `3, 2, 1`, the order the reference applies them, using
`sortedLiteralElems` — the same ordering the print road renders, so the two cannot diverge, ADR 0318), and a
container only the runtime can measure is copied by `@rt_sort` / `@rt_list_copy` + `@rt_reverse` into a named,
rooted slot and walked down the list-variable road ADR 0323 made correct. Neither door claims an element kind it
cannot see — a dict, a text through `sorted`, a `range`, a float or mixed list, a `key=` callable and a call the
container scan cannot see are refused by name, with the missing half in the sentence, and each refusal is a
row: **Gap R.247–R.251**. One measurement narrowed the row: the first version opened the second door through the
container *pair*, which made `print(1 in sorted(xs))` — a program that answers `True` today — become a refusal,
because the membership road asks `isContainerExpr` and then reaches for `containerOperand`, whose arm those
builtins do not have. The arms came out (ADR 0233 forbids trading an answer for a refusal), the pair is
untouched, and the widening is filed as **Gap R.253** with that measurement as its evidence. Asking the iterable
per loop also meant un-asking the previous loop: the container road claimed its loop variable was interned text
and nothing took the claim back, so a text loop followed by `for v in xs` printed `b`, `a`, `a`, `(null)` for
CPython's `b`, `a`, `1`, `2` — each road now claims from its own iterable and deletes otherwise, the untagged
half of **Gap R.252**. 198 cases, `programs/for_iterables.gy` `match` on both legs, and the counter-vs-container-global
instruction is now banned *by shape* from every module the suite compiles.

**`sorted`, `min` and `max` now read their ordering options by name, and ADR 0340 pays Gap R.249** — the
builtin's second positional argument used to mean "reverse", whatever it spelled. That made `key=` a wrong
answer at exit 0, a refusal blaming `reverse=`, or a crash: `print(sorted([3,1,-2], key=abs, reverse=True))`
printed ascending nonsense, `print(sorted([3,1,2], key=3))` printed a descending list where CPython raises
`TypeError: 'int' object is not callable`, and `print(min(3,1,-2,key=abs))` made `llc` reject a `%_abs` load
that should never have existed. The door now reads `key=` and `reverse=` by name, treats `key=None` as the
reference's identity key, and lets the ordinary call road answer `key(element)` for declared functions and
allowed builtins. Stable keyed order comes from ranks over `@rt_elem_gt` and placement through `@rt_put_elem`;
keyed `min`/`max` use the same comparator. Non-callable keys and invalid keywords are real catchable Python
`TypeError`s, while lambda keys, call-expression keys and heap-list element keys remain honest refusals with
their own rows (**Gap R.254**, **Gap R.255**). The conformance program `programs/key_argument.gy` is `match`
on both legs.

**A container variable asks its last binding what it is, and ADR 0341 pays Gap Q.1** — the assignment freed
the old object and the notebook kept answering for it, because six binding roads had been deciding a name's
container kind six ways and the list-literal road cleared nothing at all. `d = {1: 10}` then `d = [3, 4]`
printed `{3: 4, 0: 0}` and `d[0]` printed `0`, both at the exit code that means the program ran; `d.append(9)`
grew a *dict*; and `d = {1: "x"}` then `d = [2, 3]` made `llc` reject the module. One door in
`pkg/lang/rebind_kind.go` now writes all fourteen of a name's container claims at the binding that takes the
name over — kind, whether the slots describe themselves, what they hold — and it clears only what disagrees
with that binding, so a name bound once keeps the record that lets its slots be read statically (ADR 0241).
All six transitions between the container kinds answer, a rebound list loops and sorts as the list it is, and
`xs = ["a"]` / `xs = [1, 2]` prints `[1, 2]` instead of refusing as a mixed list. The structural witness is
the one an answer-level test cannot write: for seven pairs, the reader calls a rebound name makes are
*identical* to the calls the same program makes with the name bound once. `programs/rebound_container_kind.gy`
is `match` on both legs; the alias that shares a handle (Gap R.257) and the set asked for a position (Gap
R.256) are filed, not hidden.

A container the program *built* rather than spelled out — appended to, assigned into, rebound — no longer
has a literal behind it, so ADR 0241's compile-time promise has run out, and the read used to refuse. It
asks the object instead, because every writer wrote payload and tag together (ADR 0187): `len(xs[0])` of a
list built by `xs.append([7, 8])` is `2`, a text slot is measured in characters, and a slot holding `5`
raises CPython's `TypeError: object of type 'int' has no len()` rather than being measured as if it were a
container (ADR 0246). An equality asks the slot the same question the printer asks it: both sides of
`==`/`!=` are `(payload, tag)` pairs and the one equality the container comparisons already use answers,
so `xs[1] == "a"`, `xs[0] == xs[1]`, `xs[i] == "a"` over a position the program computes, and
`d["k"] == [1, 2]` of a dict filled by assignment all answer CPython's answer instead of being refused,
interned text stops equaling the number it is indexed by, and two float slots holding `1.5` finally
compare equal rather than by box handle (ADR 0247, closing Gap R.79). An ordering reads the value
too: `<`, `<=`, `>`, `>=` between two texts compares the bytes behind their interned indices, so
`print(1 if "b" > "a" else 0)` is `1` compiled, interpreted and run under CPython — the index records
which spelling the program mentioned first, and reading it as an ordering was the answer
(ADR 0248, closing Gap R.84; equality stays an index comparison, because interning is
content-addressed). A slot used **as a number** asks the object the same question, index included: a read
through a position the program computes arrives at the arithmetic as a `(payload, tag)` pair, and the tag
decides whether to unbox a float, convert an int or bool, or raise the `TypeError` CPython raises for that
operator and that kind — so `xs = [1.5, "a"]` / `i = 0` / `print(xs[i] + 1)` is `2.5` compiled where it used
to be refused, `print(xs[i] / 4)` over `[10, 4]` is `2.5` where it used to reach `llc` as `fdiv double , %t1`
and exit 2, `print(-xs[i])` carries negation's own sentence, and a text slot says
`can only concatenate str (not "int") to str` at run time like the oracle does (ADR 0249, closing
Gap R.88). An ordering between slots asks the same object which pair it was: two numbers are compared as
numbers, two texts by their characters, and a number against a text raises
`'>' not supported between instances of 'int' and 'str'` with the left operand's type named first, so
`xs = [1, "a"]` / `print(1 if xs[1] > "a" else 0)` prints `0` and `print(1 if xs[i] > "z" else 0)`
raises, compiled as interpreted (ADR 0250, closing Gap R.82). The arms nobody can reach are not emitted,
because a merge that names an unreachable predecessor is a module `llc` rejects — exit 2, the compiler's
own bug — and a pair whose types the tags cannot name is refused in words instead of answered. One level
below, the same object is asked twice: `xs[0][0]` reads the outer slot's tag to learn what kind of object
its payload names and then reads that object — a position with the bounds check and the `IndexError` an
explicit subscript raises, a key with its `KeyError`, a character of a text, a member of a set — so
`xs = []` / `xs.append([7, 8])` / `print(xs[0][0])` prints `7`, `d["a"] = [1, 2]` / `print(d["a"][1])`
prints `2`, `xs.append("abc")` / `print(xs[2][1])` prints `b`, and a slot that holds `5` raises
`TypeError: 'int' object is not subscriptable` instead of being refused at compile time. Nothing behind it
needs a literal, and the answer is a `(payload, tag)` pair again, so a binding, an equality, a `len` and a
further subscript all take it (`probe_nested_list.gy` is parity surface now, and a comprehension-built
`d[1]["k"]` came with it) — roadmap L11.1, ADR 0251. The two callers that had never been handed that door took
it in ADR 0323 (`Gap R.95`): **`in` and `for` over a slot the program built** ask the object which question they
are asking — `xs = []` / `xs.append([7, 8])` / `print(7 in xs[0])` printed a refusal for CPython's `True`, and
`d = {}` / `d["a"] = [1, 2]` / `for v in d["a"]: print(v)` was worse than a refusal, printing a single `0` at the
exit code of success because the loop walked the *dict's* words while the iterable was the value under its key,
with a text slot printing nothing at all. A list slot is walked by its elements, a dict slot by its keys, a set
slot by its members and a text slot as characters or as a substring; a slot holding `5` raises
`TypeError: argument of type 'int' is not iterable` to the test and `TypeError: 'int' object is not iterable` to
the loop, because those are two sentences in the reference and not one, and both reach the program's own
`except TypeError:` (`runtime_slot_haystack.gy` is corpus surface, identical on both legs). ADR 0324 opened the
same door one level deeper and on the other side of the comparison: `7 in xs[0][0]`, `for v in d["a"]["k"]`, and a
chain four slots long ask the object at the *end* of the chain what it is, because the four arms never had a depth
parameter; and a needle that is itself a slot read is read as a `(payload, tag)` pair rather than from the notebook,
so two equal dicts in two different containers compare `True`, while a container needle into a dict or set slot
raises `TypeError: unhashable type: 'list'` rather than quietly answering “not present” (`runtime_slot_haystack_deep.gy`).
ADR 0325 then fixed the word the *writer* had been leaving beside an instance: `xs.append(C())` used to tag the
slot `int`, so `print(xs[0])` printed the heap handle and `len`, membership and the loop named `'int'` for an object
the program never stored. The slot now carries `TagInstance`; the constructor's dispatch id in object slot 0 selects
the module's class table, `print(xs[0])` answers the identity form `<__main__.C object at 0x…>` (with only the class
compared byte-for-byte by the CLI oracle), and the raises name `C` — including the two ordinary binary shapes
`c in "abc"` and `"abc" in c`, which now raise the reference's text and iterable sentences instead of inventing
`True` or `False` (`runtime_slot_instance_tag.gy`).
A **trap the compiler could read** used to be a compile error: `[][0]`, `"abc"[7]`, `{1: 2}[3]`, a position
below a comprehension and `return [][0]` inside a method each stopped the build with `codegen: index of a
literal … is out of range` at exit 1 — the class the exit-code contract reserves for a program that is wrong,
issued before the program ran an instruction, with the program's own `except IndexError:` out of reach — and
the conversions that had been widened instead of refused answered instead: `float("x")` and `float(None)`
printed `0.0`, `ord("ab")` printed `97`. ADR 0326 gave the fold the one door it had never had to ADR 0181's
raise, so a fold may compute a value only where the reference would have produced one: those programs now
raise `IndexError`/`KeyError`/`ValueError`/`TypeError` at exit 3 with a traceback, catchably, from the same
sentence table the dynamic road reads, while the literals that do spell numbers answer — `int(" 12 ")`,
`int("1_0")`, `int(True)`, `float("1_0.5")`, `int()` and `float()` (`constant_trap.gy` is corpus surface,
byte-identical to CPython; the number too wide for the word is still refused, because there the slot rather
than the program is wrong).
An **ordering** of
those slots asks the object the same
question (ADR 0252, closing Gap R.93): `xs = []` + `xs.append(i)` in a loop + `print(1 if xs[0] > "a" else 0)`
used to print `1` compiled for a program CPython crashes, and now raises *'>' not supported between instances
of 'int' and 'str'* on both legs — as do the dict the program filled (`d["k"] = 5` / `d["k"] >= 5`) and the
slot one level below (`xs.append([3, "a"])` / `xs[0][0] > 1`). Two numbers become doubles, two texts go to
`strcmp`, and every other pair raises CPython's sentence with the kind the slot really holds in it: the raise
is one test per tag, because the sentence names *both* operand types, and it may end in an `else` only because
the tags ADR 0187's writers can store are a closed set. One arithmetic operator can ask the same question,
because it is the one whose **result** kind is settled before the slot is asked: **true division** (ADR 0253,
closing Gap R.96). `xs = []` / `xs.append(3)` / `print(xs[0] / 4)` printed `0.0` with exit 0 — ADR 0249's
empty-operand `fdiv` substituted into silence — and now prints `0.75` on both legs, because `/` is a float
whatever arrives: a float slot unboxes, an `int` or `bool` slot converts, and a text, `None`, a list, a dict or
a set raises CPython's own `unsupported operand type(s) for /` sentence naming the kind it really holds. The
zero trap is emitted inside each arm rather than after the merge, because which wording the pair earns is
itself a run-time question — `3 / 0` is `division by zero`, `1.5 / 0` is `float division by zero` — and a
program's `except ZeroDivisionError:` reads that sentence. The rest of arithmetic follows the same road
(ADR 0265, roadmap L11.1's last clause): `xs = []` / `xs.append([7, 8])` / `print(xs[0][0] + 1)` is `8` and
`xs.append([7.5, 8])` / `print(xs[0][0] * 2)` is `15.0` compiled, where the answer's kind used to be the
reason the module could not be written at all — the pair goes to the target, which does the sum in a `double`
and answers a pair back, so the tag that decides `8` from `8.0` is the one the object carries. `+` and `*`
take that road only under a proof that the slots the read walks hold numbers, and the proof is about *that*
container — and everything that can denote it, an alias (`ys = xs`), a loop variable (`for v in xs`), a
parameter beside the argument that reached it (`f(xs)`) — and about nothing else, because CPython *answers*
`"a" + "b"` and `[1] * 2` and this backend builds neither from a slot (`Gap R.82`). A text in a container the
arithmetic never walks therefore closes nothing (`Gap R.191`, ADR 0322): `w.append("a")` beside
`nested.append([7, 8])` prints `[14]` where it used to print a refusal that blamed `nested`. Where the proof is
absent the old refusal stands, and the positions the door does not reach are filed (`Gaps R.139`, `R.143`, `R.144`).
The answer is a value, so it survives being named before it is read: `n = xs[0][0] * 2` / `print(n)` is `14`
compiled too, and the rebinding `n = [1, 2]` behind it prints `[1, 2]`, because a binding that is not a pair
retires the tag the arithmetic left behind (`Gap R.142`, ADR 0267) — and a binding retires *every* other
status the same way now (`Gap R.145`, ADR 0270): `x = "text"` then `x = [1, 2]` used to print `text`
compiled at exit 0, because the interned-text record of the first binding was still where the print dispatch
reads it, and the compiled leg still refused `x * 2` and *raised* `bad operand type for unary -: 'str'` for a
program whose answer is `-5`. A name answers with what its latest binding gave it, on both legs. Every position that asks that name for a
*number* now asks the pair: `n + 1`, `-n`, `n > 13`, `if n:`, a `while` head, `f"{n}"`, `str(n)`, `n += 1`
(ADR 0268) — a name the arithmetic door bound is provably a whole number or a float, so the position lifts it
into the one word that holds both families rather than guessing. What still refuses is a position that keeps
**one word** for a whole value — a builtin's argument the fold door does not reach, a pair handed across a
call (`Gap R.146`) — while the list
element (ADR 0306), the f-string field (ADR 0307), the signless call (ADR 0309), the **dict entry and set
member** (ADR 0310), the **mutation roads** — `xs.append(n)`, `s.add(n)`, `xs[i] = n`, `d[k] = n` (ADR 0311) —
and **a fold's argument** (with `n` the `14` above, `print(min(n, 3))` is `3`, `print(max([n, 2.5]))` is `14`,
`print(sum([n, 1]))` is `15`, and `m = min(n, 3)` / `print(m)` prints the winner by the winner's own kind,
because the fold is handed two (payload, tag) pairs and writes back the winning pair — ADR 0316), and since ADR 0318 the fold that gets an
**element-wise ordering** rather than a handle: over two containers the program built, `min(a, b)` is `[1, 2]` and
`max(a, b)` is `[3]` (a list compares element by element, the shorter list the lesser when the shared elements are
equal), a set compares by the subset operator and the pair that is neither one’s subset orders **neither** way
without raising, a dict has no ordering against anything including a copy of itself, and a container folded against a
number raises CPython’s `TypeError` naming the two kinds that failed instead of answering the container’s element
count (`roadmap Gap R.197`)
all take the pair now, and so does the float road (`n / 4` is `1.75`, ADR 0305). An `and`'s operand is not one of those
positions any more: the operator that chooses an operand can ask the pair directly, so `print(n and 3)` prints
CPython's `3` and `print(n or 3)` its `14`, on all three legs (`Gap R.147`, ADR 0269).
What still refuses by naming itself: a numeric use whose **result** kind is only knowable while
the program runs (`xs = [1, 2.5]`, ints here and floats there — answering it would print `2.0` for `2`), a
slot used as a number on a container this pass cannot see (`xs.append(1.5)`, or a container handed to a
function), the numeric, membership and loop uses of a slot only the run time can describe
(`xs[0][0] + 1`, `-xs[0][0]`, `7 in xs[0]`, `for v in xs[0]` after `xs.append([7, 8])`) — the first two because
a use whose kind only the object knows has no untagged lowering, the last two because they need the object's
**kind** where the read asks only its tag. The numeric shapes that still refuse are the operators whose answer
is a fact about the slot rather than about the operator (`xs[0] + 1` or `xs[0] ** 2` on a container that may be
holding a float; an all-`int` slot answers both today), and the division of two such slots at once
(`xs[0] / ys[0]`, filed as Gap R.101 beside the ordering's Gap R.97). Two more come out of the sweep that
opened the division: a float element of a comprehension over a container the program built is appended by the
static path (`[xs[0] / 2]` prints `[2]`, Gap R.99), and when such an element raises, the guard's blocks move
the comprehension's loop back edge and `llc` rejects the module (Gap R.100). So does the door's `double` handed
to a context that stores an `i32` word — a call argument, `str()`'s argument, a dict slot written by key, `+=`
onto a variable that started life an `int` (Gap R.98; printing, a comparison, a condition, a float binding and
a container element are all in the double domain and take it) — and an **ordering of two such slots against
each other** (`xs[0] > ys[0]`, filed as
Gap R.97: one side whose kind comes from the object is a chain, two is a table the compiler would be
inventing), and a comparison against an expression whose kind cannot be proven — which until ADR 0247 answered
`1` where CPython answers `0` (Gap R.83, whose ordering side is measured by the same table).

**Unary minus is an operator, so it asks the same question** (ADR 0266, closing `Gap R.89` and `Gap R.137`).
`print(-"hi")` printed `-281474976710658` interpreted — the negation of the text's interned *index*, which is
why the digits looked like an address — and `0` compiled, both at exit 0; `print(-[1, 2])` reached `llc` as
`%t1 = sub i32 0, @.lst1`, spending the contract's exit 2 on a program CPython merely stops on. Both legs
now stop too, with the reference's own `TypeError: bad operand type for unary -: 'str'` at exit 3 and
reachable by `except TypeError:`: a text, `None`, a list/dict/set/tuple literal, a container variable, an
instance (which names its own class), a character read out of a text, and a slot the literal says holds no
number. the record asks the **value** (`operandKind`, the table the binary operators and `len` already
read); the compiled backend asks the **expression**, and for the text question it asks *the printer's own*
predicate, so `print(x)` and `print(-x)` cannot disagree about what `x` holds (ADR 0229's rule, one operator
further out). The constant folders were closed on the shape — a folded `-None` is a silent `0` with no
instruction that could have disagreed (Gap R.37's rule, at its last operator) — and a slot is raised only
where the literal says nothing it holds is a number *and* the program never stores anything else into it,
because a raise where CPython answers `-1` would be the same bug wearing a class. the record still
calls a tuple `'list'`, which is pinned with its own row (`Gap R.141`) rather than hidden in this one; the **compiled**
leg says `'tuple'` — the reference's word, since **ADR 0361** gave the tuple an object to ask — which makes that
record row a divergence *from the record*, and the drift ledger is where such a row lives (ADR 0302: where the
record and the reference disagree, the reference is right and the record is the debt).

**A slot read reaches a function, in both directions** (ADR 0273, closing `Gap R.139`).
`def twice(v): return v * 2` with `xs = []` / `xs.append([7, 8])` / `print(twice(xs[0][0]))` is CPython's
`14` and the interpreted leg's `14`; the compiled leg spent **exit 1** on `index cannot reach into xs's
slots`, because the value has two words and the parameter had one — and the code that consumes it lives in
the callee, which has never seen `xs`. The argument now arrives as payload **and** tag (a second `i32` on
the `define`, bound through the door every tagged value uses) and the answer's kind comes back in a word the
callee stores beside its own `return`, read by the caller immediately after the call — so the same file
prints a float slot's `3.0`, the keyword form's `16`, an argument that is itself arithmetic's `16`, an answer
bound to a name's `14`, and `None` for a callee that falls off its end. Three return roads store the tag, and
the two that give no value — fall-off-the-end and the raise — store `None`, because a stale kind read by the
next caller is a number-shaped lie; `except TypeError:` in the caller reaches a trap the callee's operator
threw. Which parameters carry the pair is decided by one pure scan of the AST, before any IR exists, so the
`define` and every `call` agree about the arity (**exit 2** was the first version's answer to a `def` written
below its call), and a parameter is opened only where every call site can supply a pair from its own spelling
and the body reads it back somewhere a pair door answers — the rule the `function_calls` benchmark taught the
day this landed, when `(a * 31 + b * 17) % 100003` was handed a tag it had nothing to read. Refusals moved
none of their words: `twice(twice(xs[0][0]))`, `twice(xs[0][0]) + 1`, `[twice(xs[0][0])]` and
`show(twice(xs[0][0]))` still exit 1 naming the one-word position (`Gap R.146`).

**A number handed to a function keeps the kind its argument had** (ADR 0276, closing `Gap P.1`'s remaining
half, `Gap R.154`, `Gap R.157`, `Gap R.158`).
`def twice(v): return v * 2` / `print(twice(2.5))` is CPython's `5.0` and the interpreted leg's `5.0`; the
compiled leg printed `4`, and `print(twice(1.5))` printed `2`, `print(area(2.5, 2))` printed `4`, a
`greet(name, times=1.5)` called without `times` printed `1` — every one at **exit 0**, because the parameter had
one word and the argument had two. One `double` convention cannot express the function: CPython's answer kind
follows the argument, so `twice(2)` must stay `4` while `twice(2.5)` is `5.0`. The pair crosses now, and the
scan asks the question ADR 0273 never asked — not *would the ordinary road refuse this argument* but *would it
refuse it, or answer a double it cannot carry* — so a float literal, a variable the program rebound to a double,
a quotient, a slot of a literal list holding one, and a default no call site mentions all take the two words,
while the parameters every call site hands integers are named (`knownIntParams`) so `w * h` has a kind for both
operands. The answer direction came with it: `def g(y): return y * 2` under
`def f(x): x = x + 1.5; return g(x)` used to print `2` for CPython's `5.0` and now hands the callee's pair on,
lifted by `@rt_lift_num` where the caller's own word is a `double`. Two rules keep the ladder honest, both of
them scan answers rather than emission-order answers: a body written under ADR 0274's `double` return or
ADR 0174's string index keeps its parameter words (`define double @gy_f(double %p0)` stays exactly that spelling,
and `def fmt(v): return str(v)` keeps its refusal instead of printing an interned index), and a **condition** is
served where a **value** is not — `if v > 10:` asks an operand's truth and takes the pair, `return v > 1.5` asks
a road that has one word and keeps the `True`/`False` it always printed. What still owes an answer is filed
rather than guessed: a pair-call answer bound to a name inside a forwarding frame prints `4` (`Gap R.164`),
and `def g(): return str(42)` prints the interned index (`Gap R.163`,
pre-existing, found by this cycle's whole-corpus sweep).

**A number handed on by a body keeps the kind its caller's argument had** (ADR 0277, closing `Gap R.161`).
`def twice(v): return v * 2` / `def outer(x): return twice(x)` / `print(outer(2.5))` is CPython's `5.0` and
the interpreted leg's `5.0`; the compiled leg printed `4`, at **exit 0**, and `print(shift_it(2.5))` over
`def shift_it(z): return add(z, 1)` printed `3` for `3.5`. Nothing inside `outer` says `x` can be a float: a
parameter is written by the caller and never by the body, so no assignment records a binding for it and the
only evidence is the argument `outer`'s own call site was written with. The scan now brackets a `def`'s body,
which is how a call site learns whose parameter it hands over, and marks propagate along those edges in both
directions — the caller holds a pair, so the callee position must carry it; another call site proved the
callee's position, so every caller must supply the tag — in bounded rounds, growing only. A mark that rests on
an edge is closed when the edge is: close the callee (it takes a power of the value, renders it a text, indexes
with it)
and the caller loses its tag word too, because a pair half-read one frame down is the wrong number this file
exists to remove. The answer direction moved to the same scan, and the tag word is declared where the callee's
`define` is emitted: `def f(v): return other(v)` now compiles identically written above or below
`def other(w): return w * 2`, where before it refused for a reason that had nothing to do with the program.
The tag definition and the flag that records "this body carries a tag" cannot share a guard, because the read
can precede the write. `programs/probe_forward_a_pair_through_a_function.gy` is the closing event — eight
lines, three legs, one answer — and `integration/forwarded_pair_test.go` pins the chain that must **not** take
the pair by its exit class rather than by a number.

**What floors a number, and what remains, ask the tag the number arrived with** (ADR 0278, closing
`Gap R.162`). `def floorit(v): return v // 2` / `print(floorit(5.0))` is CPython's `2.0` and the interpreted
leg's `2.0`; the compiled leg printed `2`, at **exit 0** — and `print(modit(-7.5))` printed `1` for `0.5`,
`print(modop(7.5, 2))` printed `1` for `1.5`, `print(outer(5.0))` over `def outer(x): return floorit(x)` printed
`2` for `2.0`. `+ - *` were served over the pair; `//` and `%` were in no served list at all, so the parameter
never took the tag and the double was truncated before the floor ran. They now go through the same tagged door,
as operator codes `4` and `5` of `@rt_num_arith`, with ADR 0216's rules kept in one helper the statement road
shares — `@llvm.floor.f64` for the floor (down, not toward zero) and `frem` corrected to the divisor's sign for
the remainder, so `floor(x/y)*y + (x%y) == x` holds of the doubles. The tag decides which of the reference's
**four** divide-by-zero sentences a line gets: one `def f(v): return v % 0` says "integer modulo by zero" for
`f(5)` and "float modulo" for `f(5.0)`, and `//` has its own pair of them — the first draft of the table shared
the floor's wording with the remainder, which is the drift a whole-corpus sweep exists to catch.
The combination landed the same day, one ADR later: see ADR 0279 below, and ADR 0280 below that, which pays
`Gap R.164`. What the pair door still refuses is a **position** that keeps one word — a pair answer stored in a
container's element or handed to a call (`Gap R.146`) and a pair answer read back by an augmented assignment
(`Gap R.143`) — and the reference's text-`%`, `print("%.2f" % 3.5)` printing `0.0` for CPython's `3.50`
(`Gap R.165`): refusals and one measured wrong number, filed instead of answered by payload.

**The arms of a pair expression are asked the same question as the whole** (ADR 0279, closing `Gap R.166`).
`def identity(v): return (v // 2) * 2 + (v % 2)` / `print(identity(7.5))` is CPython's `7.5` and the interpreted
leg's; the compiled leg exited 1 on it, as it did on `v * 2 + 1` (since ADR 0276), `v + 1 + 1`, `2 * (v + 1)`
and `(x + 1) * 2` over a name that had become a double. Three of the four questions the pair door asks of an
operand walk it recursively — may this be a text, would the ordinary road have refused this, is this a shape the
door serves. The fourth, "is this a slot the program is shown to hold only numbers?", looked at the node it was
handed: a bare `v` passed, `(v - 1)` was not a name and not a chain, so the arm was declined, the expression fell
back to the road with one word for a parameter, and that road refused. Walking the leaves is the whole fix.
What was rejected is the version that prints something: valuing an arm by its payload with `tag = 0` makes
`identity(7.5)` print `7` — a number, at exit 0, and the wrong one — which is why ADR 0278 pinned these shapes
as refusals first, and the pinned refusals are what failed when the honest question was finally asked.
`programs/probe_combine_a_floored_pair.gy` is the closing event: twelve lines, three legs, one answer.

**A name holding a pair answer is read as a pair wherever it is read** (ADR 0280, closing `Gap R.164`).
`def outer(x): y = twice(x); return y` / `print(outer(2.5))` is CPython's `5.0` and the interpreted leg's; the
compiled leg printed `4` at **exit 0**, and the same was true of `return y + 0`, of `h = floorit(x); return h`
printing `2` for `2.0`, and of that body three frames deep. Nothing about the call changed — `return twice(x)`
already answered (ADR 0277) and `print(y)` already answered (ADR 0273) — the pair simply was not bound when the
answer went through a *name*. Three questions had to be asked of the name instead of the expression: does this
body hold a pair at all (`pairBoundCallNames`, which retires a name the moment the program writes anything else
to it — `y = twice(x); y = 3` still answers `3`), is a call's answer a number (`exprNumberish` had no answer for
a call and said "unknown", which hands the whole function to the float-return road), and can an arm be a call
(it can, through the same door `print` uses). Two silent wrong numbers surfaced on the way and both are pinned:
`half(7.5) + twice(7.5)` printed `30.0` for `18.0` **in one arm order only**, because a double answer leaves the
arithmetic door as a heap box that no root held and the next allocation recycled its slot; the same cause one
door deeper printed `3.0` for `floorit(v) * 2 + modit(v)`. What the cycle could not answer it refuses instead of
truncating — `z = twice(y)` and `y += 1` are exit 1 naming the parameter and the missing kind, where both had
printed `8` and `5` — and the two rows are pinned as refusals. `programs/probe_bind_a_pair_call_answer.gy` is the
closing event: eleven lines, three legs, one answer.

**An int that meets `/=` becomes a float** (ADR 0274, closing `Gap P.1`'s `/=` half and `Gap R.155`).
`x = 7` / `x /= 2` / `print(x)` is CPython's `3.5` and the interpreted leg's `3.5`; the compiled leg printed
`3`, and `x = 1` / `x /= 3` printed `0` where the reference prints `0.3333333333333333` — both at **exit 0**,
because the operator asked the integer road. The operator now names the domain, the way `/` always did, and
where that leaves a double standing in a slot the variable's first binding made for an `i32` the rebinding
takes the statement: the double goes into a float box, the name is bound to the **`(payload, tag)` pair** with
the float's tag, and every later read asks the tag — the same pair print, a number position, an ordering, a
condition, `str` and a `while` head already ask (ADR 0166, ADR 0265, ADR 0267), so no read road needed a new
case and a variable that never changes state still emits the plain slot and the plain load it always had.
The corruption the row was hiding is the reason a wider store was not the answer: `y = 12345` / `x = 8` /
`x = 2.5` / `print(x, y)` printed **`1074003968`** for `y`, a `store double` reaching four bytes past the
allocation into its neighbour, and the module verified because an opaque pointer hides the width of what a
`ptr` points at. Measured, not predicted: with the variable's GC root missing, `h = 1` / `h /= 3` answered
`print(h + 1)` correctly and `print(h * 2)` with the first print's answer — the collector had recycled the
box under a slot it was told held no handle. What still exits 1 naming the position that keeps one word is
the state travelling further: returned from a function (`Gap R.156`), stored as a container element
(`Gap R.159`), ordered against a float literal (`Gap R.160`). The two rows this line used to carry — the
`t += i / 2` accumulator (`Gap R.157`) and a float-state name handed to a call (`Gap R.158`) — answer `6.5`
and `5.0` now, and `Gap P.1`'s other half, an untyped parameter that receives a double, closed with ADR 0276
above.

**A data module's constant keeps the type the module declares** (ADR 0272, closing L11.6's typed stdlib
constants). `import math` / `print(math.PI)` printed `3` compiled at exit 0 — the census table's own row —
while the record beside it printed `3.141592653589793` and a hand-written `pi = 3.141592653589793` was
right on both legs. The value behind `mod.NAME` is a literal the importing program never wrote, and only
`value()` knew it: every predicate that decides *how* to write the value saw an attribute with no kind and
took the integer word, so `math.PI * 2` was `6`, `-math.PI` was `-3`, and `x = math.PI` / `print(x > 3.14)`
was `False`. One read (`pkg/lang/module_const.go`) now answers the kind questions the print formatter, the
arithmetic, the binding, the double road and ADR 0271's signless door ask — and the same read that turned
`print(abs(NAME))` on a module text from `0` into CPython's sentence is what proves it is a kind rule and not
a stdlib patch. Half-fixing it is a trap the tests now hold: formatter-only printed `3.0`, because the value
still came from `sitofp` of the truncated word. What the probe found and this did not fix is filed: a float
written into a container slot the program recorded as ints (`Gap R.152`), and a verdict the module declares
printing `1` on both legs (`Gap R.153`).

**`abs` is the other operator that needs the answer** (ADR 0271, closing `Gap R.140`). CPython stops on the
absolute value of anything without a sign — `TypeError: bad operand type for abs(): 'str'` — and this
compiler answered every one of them: `print(abs("hi"))` printed `hi` (the text reached the print door holding
the interned *index* it is stored as), `abs(None)` and an instance printed the number `0`, and `abs([1])`,
`abs({"a": 1})`, `abs({1})` each handed `llc` a `sub i32 0, <heap global>` and came back **exit 2**. It is
paid through the door above rather than a new one — one predicate names the operand for `-x` and for `abs(x)`,
so the two sentences can never disagree about what the operand holds — with the wording coming from the one
sentence table, `abs` spelled as its own operation because CPython names the *call* there and the *operator*
under the minus. Both lowering roads ask it: the `double` road builds its operand by lifting to a float, which
yields nothing for a text, and had been printing `0.0` for `print(abs("hi") * 2.5)` at exit 0. What a *name*
asks is its latest binding, which is why this row needed `Gap R.145` (ADR 0270) fixed in the same commit —
`x = "text"` / `x = [1, 2]` printed `text`, and `x = "text"` / `x = 5` / `abs(x)` raised on a program whose
answer is `5`. The three-engine program is `programs/abs_names_its_kind.gy`; what the sweep around it found
broken is filed, not folded in: a builtin or an imported module used as a value is exit 2 compiled and
`NameError` interpreted (`Gap R.150`), and `-(lambda x: x)` reaches `sub i32 0, lambda_0` — also exit 2
(`Gap R.151`).

A function's **return word** is now read from what its body does rather than from the shape of its `return`
line (ADR 0254, closing Gap R.3c). `def addf(x): x = x + 1.5` / `return x` / `print(addf(1.0))` printed the
*argument* — `1`, with the exit code of success — because the answer's type came from `return x`, which says
nothing about a kind, while the body had already stored a double into the parameter's slot. The gate asks the
same question the emitted instructions ask (`isFloat`, over the body with the rebound names read as the doubles
they became), so the `ret` and the value it writes cannot disagree, and the family answers: the bare name,
`return x * 2`, `x % 3`, `abs(x)`, `abs(-x)`, `return -x` — which had been an `llc` rejection, `ret i32` under a
`define double`, ever since the body learned to negate a double — the local spelling `y = x + 0.5; return y`,
and any of them inside `if`, `while`, `try`, recursion or a default. **The rule is about the name the return
reads back, not about where that name came from** (ADR 0373, closing Gap R.338): the gate began with
`if len(fd.Params) == 0 { return nil }`, so `def f(): u = 9.5` / `return u` / `print(f())` — the first float
function a person writes — emitted `define i32 @gy_f()` over a `double` slot and printed the low word of the
double, `0`, at the exit code of success; and `v = u * 2` / `return v` printed `0` even **with** a parameter,
because the scan asked each name's float question once, in map order, with no notion that `v` is a double
only once `u` is. Both are answered now: the scan keeps the **last** binding of every name (which is why
`u = 2.5` / `u = 3` / `return u` says `3` and not `3.0`) and settles its float table by repetition, each round
asked inside `withFloatNames` — the one table the emitted body acts on, so the `ret`, `isFloat` and the
assignment road cannot hold three opinions about one value. What is not that double keeps its own word
(`int(x)`, `round(x)`, `x > 2`, a user callee), and where the one convention would have to hand a container
handle or an interned text the `double` word — a parameter the body also reads, or any method — the program is
refused in words, naming the variable whose double has nowhere to go. Three shapes came out of that sweep and
are filed rather than absorbed: the ternary arm (`return x if x > 2 else 0.0`, refused since ADR 0254 and a
truncated `1` before it, Gap R.102), a container **returned** from a function (`print(f(1.0))` printing `0` for
`{'k': 2.5}` — filed as Gap R.103 and re-measured as Gap R.67's, since the same dict written with no function around it prints
`{'k': 2.5}` on both legs), and `min`/`max` with two arguments refusing on the record (Gap R.104,
closed two cycles later by ADR 0256 — which found the compiled leg had its own half of the same question).
ADR 0333 paid the half of that row which is not a double: `def f(): return {"k": 1}` / `print(f())` answers
`{'k': 1}`. The half where the **caller's double** becomes a slot of the answer (`def f(x): return {"k": x}` /
`print(f(1.0))`, still `0`) is left to the pair road on purpose — see the carve-out under ADR 0333 below.

**The float table a body reads is that body\u2019s own writes** (ADR 0375, closing Gap R.339). ADR 0373 had
settled which write a read answers \u2014 the last one \u2014 and left whose writes the table holds at all. The map
was allocated once per module and nothing cleared it between `define`s, so a body read whatever the
previously-emitted body had written: `def bound(): u = 9.5 / return u` above `def lastWrite(): u = 2.5 /
u = 3 / return u` printed the reference\u2019s `9.5` and **`3.0`** where `lastWrite` alone printed `3`, and
`print(b(), a(), b())` answered `7.0 1.5 7.0` for one function called three times in one line. The IR was
wrong, not just the rendering: the return word is chosen by asking `isFloat` through that same table, so
the int-returning body opened `define double @gy_b()` \u2014 a function returning a double it never computed.
The cure is the mechanism the module already trusts: `beginScope` installs the container, bool, None and
text tables per scope, and `floatVars`/`doubleSlot` were the per-frame kind tables it never installed
(ADR 0163 closed this disease for the tables that were on the list). `enterBodyFloatTable` is now the one
place that installs them, called by `beginScope` and \u2014 for the roads that emit a body without it, the method
emitter and the closure lift \u2014 by `enterBody`; the frame\u2019s own map comes back on the way out, so a `lambda`
or nested `def` minted mid-statement neither takes its frame\u2019s float facts nor leaves its own behind. What a
body may read from the *module* is a different row and is unchanged: `RATE = 2.5` above
`def scale(v): return v * RATE` it is closed below by ADR 0376. Two shapes came
out of the same measurement and are filed rather than absorbed, both identical before and after: a
`for`-driven float accumulator inside a body, which prints `0` where the reference prints `6.0` while the
same loop at module level prints `6.0` (Gap R.341, closed below by ADR 0377), and a nested `def` that shares
a name with its enclosing body's float local, whose store went through the enclosing frame's alloca so `llc`
rejected the module and the program left **exit 2** (Gap R.342, closed below by ADR 0378, which found that the
slot table had the same hole the float table had).


**A body's stack slots belong to the frame that emits the body** (ADR 0378, closing Gap R.342). `g.allocd`
answers 「does this name already have a stack slot in the frame being emitted『, and one map answered it for a
whole module. The `def` road reset it (`beginScope`); the two roads that emit a body without passing through
`funcDef` -- the closure lift and the class road's method emitter -- never did, so both read the ENCLOSING
body's table:

```py
def outer():
    def inner():
        w = 3              # asks outer's table: "already allocated" -- no alloca for inner
        return w
    w = 1.5                # store i32 %t7, i32* %_w -- in a define that never allocated %_w
    return inner()
print(outer())             # CPython 3 -- this toolchain before ADR 0378: exit 2, `use of undefined value '%_w'`
```

Renaming either binding compiled, which is the signature of a name-keyed table that one road resets and
another does not. The written-flag table (ADR 0228) had the same hole one level down: both roads asked
`emitBoundAllocas` before installing their own flags, so a nested `def` or a method with a local on a path that
may not write it set `%bnd_z` in a frame that had none. Five shapes reached exit 2 that way, three lines of
ordinary source each, and two of them were container and text collisions rather than float ones -- the tell
that the fault was the frame entry and not the float convention. The cure is one rule in one new file:
`enterFrameSlots` installs a body's own `allocd`/`gcRootSeen`/`freshSlots` and hands the enclosing frame's back,
taken by `enterBody` -- the helper the method and lift roads share with `beginScope`, which now delegates to it
so there is no second copy of the table to forget -- before a single line of that frame is emitted.
`frame_audit.go` is the guard that keeps the rule true: the compiler walks the module it produced and refuses a
`define` that reads a slot-shaped operand its own body never allocated, naming the function, the slot and the
instruction, and failing as `*lang.IRInvariantError` -- exit **2**, the same class an `llc` rejection gets and
the same `--json` shape, because a compiler bug must never reach the reader as a defect in their program
(ADR 0211). Three neighbours the probe reached stay filed rather than absorbed, all identical before and after:
a lifted `def` whose body answers a double prints `0` at exit 0 (Gap R.345, ADR 0377's fold unwritten on the
lift road), two nested `def`s of the same spelling in two frames collide on the global the lift mints (Gap
R.346, exit 2 again), and a `def` nested inside a method is not lifted at all (Gap R.332).

**A comprehension's target is a binding of the frame that hosts it** (ADR 0381, closing Gap R.348). The
comprehension road had always allocated the loop variable's slot and stored each item into it — and had
always recorded the name nowhere, so the one road that asks a frame what that frame owns came back saying
nothing:

```py
print([(lambda z: z + x)(1) for x in [10, 20]])   # CPython [11, 21] — this toolchain before ADR 0381:
                                                  # `undefined name "x"`, blamed on the program
x = 5
print([(lambda z: z + x)(1) for x in [10, 20]])   # CPython [11, 21] — here: [6, 6], at exit 0
```

The body of an applied `lambda` is emitted as a `define` of its own, and a `define` reaches neither another
`define`'s registers nor the module's storage for a name the module never bound — so the read of the target
inside the body died at the read, while the SAME target handed to that call as an argument answered all
along (`[(lambda fn: fn(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]]` is `[4, 6, 8]`), which is what located
the hole in the read and not in the loop. ADR 0380 had measured the shape, filed it, and written the rule
down in `pkg/lang/frame_targets.go` with no caller on either side — a law with no police, and a distinct
thing from a half-implementation: the code that would have refused differently existed, only the wiring was
absent. Two calls in the two comprehension roads (`noteFrameTarget` beside the slot, `forgetFrameTarget`
when the element and the filter have lowered — the binding's span), and one ask in the capture road,
ordered: a name the frame owns as its own target is a capture *whatever the module also binds*, and only
then does the module table get to answer. That ordering is the `[6, 6]` fix, and it is the reason the
recorder could not record that source before the change — `tools/recmerge` asks CPython first and turns away
an answer the compiled leg disagrees with. The gate ADR 0370 installed was not widened: a target bound to a
container, a text or a float is refused in that gate's own words at exit 1. The `for` road keeps its
mirror-image rule, because its scoping is the mirror image — a module-level `for` target IS module state,
and a body reading one still meets Gap R.35's different sentence — and both are pinned in one file so the
two roads cannot quietly merge. Five neighbours came out of the probe and are filed, not absorbed: a capture
of a name the module also spells answers the module at exit 0 (Gap R.349), a comprehension inside a
comprehension answers one flat list and in another face hands `llc` a global as a payload at **exit 2** (Gap
R.350), a subscript on a runtime-built comprehension traps with `IndexError` (Gap R.351), a comprehension's
second `for` clause does not parse (Gap R.352), and a set or dict comprehension whose element is a call
refused while the list twin answered (Gap R.353, paid by ADR 0384).

**A comprehension's clause list is the nested loop its clauses spell** (ADR 0383, closing Gap R.352). The grammar carried
ONE `for` clause, so the idiom Python programmers write without thinking was not a half-supported feature but a syntax error pointing at a
bracket — and the record leg had been holding the right answers to 46 of those sources the whole time, invisible to a drift ledger that only
compares answers:

```py
print([y for x in [[1, 2], [3, 4]] for y in x])    # CPython [1, 2, 3, 4]
                                                   # this toolchain before ADR 0383: parse error at 1:28: expected "]"
print([[x, y] for x in [1, 2] for y in [3, 4]])    # CPython [[1, 3], [1, 4], [2, 3], [2, 4]]  · before: parse error at 1:31
```

`parseCompClauses` reads `for NAME in ITER [if COND]` repeated, left to right, each clause seeing the targets before it; `Comp.Clauses` carries
it and `ForVar`/`Iter`/`Cond` stay as the first clause so every road that ever read a comprehension still reads one. `pkg/lang/codegen_comp_nested.go`
is the door, with two arms: a fold that evaluates the element under every binding (which keeps `sum([x+y for x in [1,2] for y in [3,4]])` at
`20`), and a runtime builder that appends one element per combination through `rt_append_tagged`, giving each clause its own slot — ADR 0381's
rule, applied to a whole clause list instead of one target. A filter belongs to the clause it follows and skips the whole rest of the nest, which
is the difference between `[2, 3]` and `[2, 3, 2, 3]`. Two answers that were **wrong at exit 0** were found and raised out of the language on the
way: a fold that bound a container item as its lowered table index (`[[x,y] for x in [1] for y in [[3]]]` as `[[1, 1]]`, now **Gap R.355**'s
refusal), and a dict fold that kept the first value a key was written with (`{1: 10, 2: 10}` for `{1: 20, 2: 20}`). What the nest cannot walk says
so, with a capability code and the row that owns it — a clause over a container the run time built is **Gap R.356** (`rows = [[1,2],[3,4]]` /
`[y for x in rows for y in x]`), and a clause over a text or a float is refused one clause in too (`L11.1`). The formatter writes every clause it
read: a `gustyc --fmt` that printed the first one only would delete half of a program on save.

**A set or dict comprehension builds what its fold cannot name** (ADR 0384, closing Gap R.353 and Gap R.357).
The list road had a fold and a runtime loop; the set and dict roads had only the fold. The same expression that
list slots carried as payload-plus-tag was refused, or — worse — became an integer pretending to be `None`, a
verdict, text, or a float:

```py
print({(lambda z: z + x)(1) for x in [10, 20]})      # CPython {11, 21} · before: element must be constant
print({x: (lambda z: z + x)(1) for x in [10, 20]})   # CPython {10: 11, 20: 21} · before: value must be constant
print({None for x in [1]})                           # CPython {None} · before: {0}
print({1: None for x in [1]})                        # CPython {1: None} · before: {1: 0}
xs = []
xs.append(6)
print({v: v / 2 for v in xs})                        # CPython {6: 3.0} · before: {6: 3}
print({{1} for x in [1]})                            # TypeError: unhashable type: 'set' · before: llc exit 2
```

`runtimeCompSetDict` now mirrors the list loop. If `setCompFolds` / `dictCompFolds` can prove every operand, the
static container is still emitted; otherwise each iteration writes through `rt_set_add_tagged` or
`rt_dict_put_tagged`. A dict key and value are independently `(payload, tag)` pairs. The hashable guard is applied
only at member/key positions — a dict may hold a list — and it raises CPython's exact unhashable sentence. The
fold question and executor were brought into one contract: `None`, verdicts, floats, texts, and containers no
longer count as integers merely because their lowered payloads happen to be ints.

The parser now tracks brace-display depth, so `{{1} for x in [1]}` is parsed as a set comprehension over a set
member rather than a display whose element stole the outer `for`. That distinction is what turns `llc` exit 2 into
the program's own catchable `TypeError`. Text set repr order is not pinned: CPython's string-hash randomization
makes it arbitrary; members and tags are the facts under test.

**An ordering between two kinds that do not order raises what the reference raises** (ADR 0382, closing Gap R.85 and
Gap R.202). A comparison never sees the kinds: by the time the ordering road runs, `None` has lowered to the word `0`
and a text to an index into `@str_tab`, so `1 < None` is an `icmp slt i32 1, 0` — a true thing to say about two
integers and a meaningless thing to say about the program that produced them.

```py
print(1 < None)       # CPython TypeError: '<' not supported between instances of 'int' and 'NoneType'
                      # this toolchain before ADR 0382: False, at exit 0
print("a" < 1.5)      # CPython TypeError … 'str' and 'float' · before: True, at exit 0
print(True < [1])     # CPython TypeError … 'bool' and 'list' · before: the same raise naming 'int'
```

There was already a door of this exact shape in the codebase — ADR 0293's arithmetic door, which stops `1 + "a"` before
the addition runs — and comparisons simply had no counterpart. `pkg/lang/order_kind.go` is that counterpart: it names
each side's kind from the AST, puts it in a family (number, text, the void, a container, unknown), and where the two
families have no ordering it raises `TypeError` with CPython's own sentence, naming the operand whose `__lt__` was
tried first. The void orders against nothing, `None <= None` included. The question is asked **before either operand is
lowered**, and on both roads a comparison travels — the value road and the condition road an `if`/`while`/ternary takes,
so `if 1 < x:` raises what `print(1 < x)` raises. The two raises the road used to invent (`TypeError: unorderable types:`
and a 256-character truncated `unable to compare …`) are deleted rather than improved: a raise the reference does not
have is a wrong answer too. Thirty-seven sources moved, every one of them a trap the record already carried, so the
record leg and the compiled leg now agree on all of them. What the door does not claim it does not raise over: two
containers stay the container door's question (Gaps R.201, R.86), and a side whose kind is a run-time fact — a
parameter, a tagged slot, a call whose branches answer two kinds — keeps the door silent, which is why `x = None` /
`y = 1` / `print(x < y)` still answers `True` (**Gap R.354**): the front end records a name whose latest binding was the
void, a verdict, a float, a text or a container, and an int is what a slot holds when nothing was recorded.

**A body's return word is chosen by folding its writes, not by asking only its newest one** (ADR 0377,
closing Gap R.341). ADR 0373 settled that a read answers a name's LAST write, and the scan that picks a
`define`'s return word was built on exactly that — which is right when the newest write states a kind and
helpless when it merely reads the name back:

```py
def loopAcc():
    t = 0.0
    for i in [1, 2, 3]:
        t = t + i      # the LAST write of t; isFloat(t + i) asks about t, whose kind is what is being decided
    return t
print(loopAcc())       # CPython 6.0 · this toolchain before ADR 0377: 0, at exit 0
```

`isFloat` consulted a table the scan had not filled and found an `int` loop variable on the other side, so
the body was emitted `define i32` and the emitted module said the whole thing in two instructions —
`store double \u2026, double* %_t` beside `%_t.ld = load i32, i32* %_t`. The identical loop at module level
answers `6.0`, because `@main` has no return word to choose: the control that localised the wrong `0` to
the scan rather than to the arithmetic. The scan now **folds** the body's writes in source order, asking
each against the kinds the earlier ones produced, so a newest write that states a kind still decides and a
newest write that inherits one is asked with its seed's kind. ADR 0373's rule is not merely unfixed but
pinned inside the new table: `u = 2.5` / `u = 3` / `return u` still answers `3`, an `int` accumulator still
answers `6`, and a float accumulator finally rebound to `3` still answers `3` — which is precisely why the
tempting one-line alternative, seeding the scan from “any write was ever a float”, was rejected: it
reopens Gap R.338 and prints `3.0`.

**A compiled body reads the module's float out of the module's own global** (ADR 0376, closing the float
half of Gap R.35). ADR 0227 built the mechanism and left a hole in it deliberately: `moduleSlots` maps a
name to an `i32` global, a double does not fit an `i32`, and so every module-level float a body named was
answered with the module-state refusal, at exit 1, for a program CPython runs. `RATE = 2.5` above
`def scale(v): return v * RATE` printed `7.5` in the reference and nothing here. The double already had a
word, a slot type, a printer and a return convention after ADR 0373 and ADR 0375; what it had never had was
the module's half of the pair. `@gy_mod_<name> = global double` is that half: `@main` writes it in source
order and any body that reads the name without binding it loads it, at call time, so a module that rebinds
the number between two calls is answered twice and differently. The gate is the safety of the whole thing,
written as a rejection list because a global can only promise a word for a number the compiler can see:
every module binding must be a float literal or arithmetic over literals (`RATE = 7 / 2`, `AREA = 2.0 * 3`,
and `RATE = RATE * 2`, the module doubling what it already holds), no `for` target, `with ... as`, `match`
capture, unpacking or `import` may touch the name, and no body that reads it may also write it. A name
whose value comes from a call is refused rather than answered with a number nobody wrote, and the i32 road
refuses the name outright rather than handing `llc` a `mul i32` over a `double` — exit 2, which the exit-code
contract reserves for a compiler bug. A method that returns the module's double is refused before its
header is written, by ADR 0196's rule for a rebound parameter: a method is `define i32` whatever its body
computes. The module **container** is still refused, and still owed — a heap handle has to be rooted against
the collector and its element operations come from the scope a binding settles (ADR 0341) — and it sits in the
same test table as the shapes that now answer, so a half-closed row cannot be read as a closed one. Two
neighbours came out of writing those tables and are filed rather than absorbed, both identical on the
pre-ADR build: a module-level **text** read by a body prints the intern position (`0` for `world`, Gap
R.343), and printing a float-returning call whose argument arrives as a double emits an `i32` store of a
`double` so `llc` rejects the module at **exit 2** (Gap R.344).

The element of a comprehension over a container the program built is free to **branch**, and the loop had to
be told (ADR 0255, closing Gap R.100). `xs = []` / `xs.append(6)` / `print([v / 2 for v in xs])` was **exit
2** — `llc` rejecting *PHI node entries do not match predecessors* — because the zero guard `/` carries
(ADR 0253) ends the body in a block the induction `phi` had never heard of, while the `phi` went on naming
the block the body *starts* in. The increment now lives in a latch block that every path the element can end
on branches to, the entry list says where the back edge really comes from, and the program prints `[3.0]` on
both legs: `[v / 0 for v in xs]` dies with `division by zero` (exit 3, behind a filter or in front of
one), `{v / 2 for v in xs}` prints `{3.0}`, and a `for` whose body divides came with it. Two shapes from the
same sweep are filed rather than absorbed, both silently-wrong answers with exit 0: the dict comprehension's
value loses the double's tag (`{6: 3}` for `{6: 3.0}`, Gap R.105), and a loop variable whose slot holds text
is divided by the static arm (`[0.0]` where every honest engine raises `TypeError`, Gap R.106).

A fold **returns the candidate it chose**, not the comparison that found it (ADR 0256, closing Gap R.104 and
the source-visible half of Gap R.73). `print(min(1.0, 2), max(1, 2.5))` printed `1.0 2.5` compiled and refused
outright interpreted; `print(min(2.5, 1))` printed `1.0`, because the compiled path promoted every candidate to
`double` and selected a `double` — while Python hands back the winning *element*, so the answer's kind is the
winner's own. `min`/`max` now take values side by side or one container on both legs, and the family
answers: ints (`1 5`), an int among doubles (`min(2.5, 1)` → `1`, `max(1, 2.5)` → `2.5`), three candidates, a
container written inline, and **text ordered by its content** through `rt_str_order` rather than by its
position in the intern table (ADR 0248's rule, met again where an interned text is also an `i32`) — so
`t = min("pear", "apple")` still has a working `.upper()`. Candidates with no ordering for the operator the
builtin asks (`min` asks `<`, `max` asks `>`) are **raised**, not refused and not answered: the old compiled
path compared heap and intern indices and exited 0 with a number, where both legs now print CPython's
`TypeError: '<' not supported between instances of 'str' and 'int'`, catchable by `except TypeError`, at exit 3
— with the two kinds in the order the fold met them, because that is the order CPython's operands had. Four
shapes stay honest about the half they are missing and are filed rather than absorbed: a container the program
built (Gap R.107), a text container the program built (Gap R.108), two runtime candidates whose kinds straddle
`int` and `double` — comparable, but the winner's kind cannot leave the call until the tagged value word exists
(Gap R.109) — and a callee whose winner was settled by one call site (`def choose(a, b): return max(a, b)` /
`print(choose(2.0, 1))` printing `2` for `2.0`, Gap R.110).

**The candidate the comparison chose decides what the answer is — and it is asked once** (ADR 0261,
closing Gap R.117). `print(max([True, 0]))` printed `1` compiled where CPython and the record print
`True`; `print(max(True, 0))` printed `1` on **both** engines; `print(max([True, 1.5]))` printed `1`, which
was not a rendering bug at all but the double underneath being truncated. One rule now covers all three:
a verdict is a candidate like any other number (1 or 0 — ADR 0259's numeric family applied where it had
not been), one strict comparison picks the winner, and the renderer asks **that candidate** what it is.
So `max([True, 0])` prints `True` through `print`, `str()`, an f-string and a container slot
(`[True]`), while the number stays underneath (`max([True, 0]) + 1` → `2`, `max([True, 1.5])` → `1.5`).
The tie rows are what no element-level rule can produce, and both spellings are pinned:
`max([True, 1])` is `True` and `max([1, True])` is `1`, because the comparison is strict and the first
candidate stays — the same reason `min(1, 1.0)` is `1`. The fold's `TypeError` names the candidate's kind
too: `max(["a", True])` raises `'>' not supported between instances of 'bool' and 'str'` on both legs.
A ternary takes the same rule where the test is a value the source wrote (`print(False if 1 else 2)` →
`False`). Two shapes are filed instead of guessed: a candidate the compiler cannot read — `i = 0` /
`max([True, i])`, whose `select` keeps the payload with nothing beside it to name its kind (Gap R.124) —
and a ternary whose test it cannot read, where the record agrees with the compiled backend and
neither agrees with CPython (Gap R.125).

A verdict **is a value, and it prints its own name** (ADR 0257, L11.1's bool step). `print(True)` is `True`,
`print(1 == 1)` is `True`, `print(0 == None)` is `False`, `str(True)` is `'True'` — an ordinary string with a
working `.upper()` — and an f-string writes `flag: True` rather than `flag: 1`. None of that changed what a
bool *is*: it is still the untagged `0`/`1` in the word it always occupied, still adds (`True + 1` → `2`),
multiplies, negates (`-True` → `-1`) and sums (`sum([True, True, False])` → `2`), and a condition still tests
it the way it always did. What changed is who answers "what is this?": the printer asks the **expression**,
through one predicate the compiled backend share, so `and`/`or` of two verdicts are verdicts while `1 and 2` is still
`2` (Python yields the operand), a ternary is a verdict only when both arms are — unless its test is a value
the source wrote, in which case the arm that runs decides (ADR 0261), `all`/`any` and a call whose
every `return` is a verdict (ADR 0254's rule, read from the body) are verdicts, and a name is a verdict only
until something that is not an expression rebinds it — `for flag in [1, 2]` prints `1` and `2`, because a loop
binds elements. A comparison that reached a class's own `__lt__` is not a verdict at all: the program's method
returned an int, and `print(a < 4)` prints `1` on both legs. Two shapes stay where a tag has to travel
rather than be read off an expression. A bool in a container used to be one of them — `print([True, 1])`
printed `[1, 1]` on the compiled backend, because the element tag vocabulary had no bool to read back — and is
paid: the slot now carries a bool tag, the container printer, the set dedup and the ordering sentence all
read `bool`, and `[True, 1]`, `{'k': True}` and `{True}` come out as CPython writes them while every
numeric question about the same slot still answers as its number (ADR 0259, closing Gap R.112). A verdict
handed to a function was the next one, and is paid the same way the print rule was: the front end reads the
call sites and tells the print road what the parameter holds, so `show(1 == 1)` says `True` instead of the `1`
the comparison produced (ADR 0348, closing Gap R.111). What stays filed is what that reading will not guess
about — a position two call sites fill differently, or a method name two classes share, prints the number it
is stored as until a tag rides with the argument (Gap R.270) — and the comprehension and fold shapes that
sweep measured are filed with
their per-engine answers — Gaps R.116, R.117 and R.119, since R.118 turned out to be a dict story and was
paid by ADR 0260 the same day. `--json` names the
type `bool`, and `--eval '1 == 1'` echoes `True`.

**`and` and `or` hand back the operand the test chose** (ADR 0269, closing Gap R.147). They are the two
operators that are not operators: `print(2 and 3)` is `3`, `print(0 or 5)` is `5`, `print("" or "d")` is the
text `d`, `print([1] and [2])` is `[2]`, and the pair that proves the rule is `print(True or 1)` → `True`
beside `print(1 or True)` → `1`. Both engines printed `1` for all four before ADR 0269 — exit 0, digits wrong, no
refusal, on an operator every Python program uses — because the lowering composed two predicates and
zero-extended the verdict, and the truthiness table asked whether the *test* passed rather than what the
*expression* is. A condition still wants only a verdict, and `truth(a and b)` is `truth(a) and truth(b)`,
so `if x or "d":` branches on operands that could never share a word; a value position needs one, and the
compiled backend merges it from the two arms — in the `i32` word, in the `double` word, or as the
(payload, tag) pair the module's one tag-reading printer takes, which is what lets `print(x or "d")`
print a word instead of the interned index underneath it. A shape none of those roads can state names both
operands and the missing tag at the capability exit instead of answering `1`, and the checker types the
expression by its operand, not by a bool it never returns. The operators also **short-circuit, on both
engines** (ADR 0275, closing Gap R.149): the test decides whether the other operand is in the program at
all, so `x and boom()` with x false is silent, `x and (1 // 0)` answers `0` without trapping, and the operand
the test does reach runs exactly once — which the compiled leg had been getting wrong twice over, once by
running the excluded operand and once by evaluating the tested one twice, because a `select` between two
operands evaluates both and the truth and the value were each lowered on their own.

A **conditional expression runs the arm its test chose** (ADR 0374, closing `Gap R.340` — the second half
of ADR 0275). `a if c else b` used to be one LLVM `select` with both arms lowered beside the test, and a
`select` has no unreachable operand: both of its operands are arguments, so the run time had already done
whatever the rejected arm does before the instruction picks. `print(1 if x > 0 else boom())` printed
`boom`; `print(1 if x > 0 else 1 // 0)` died of a `ZeroDivisionError` the program never performed; and
`def fact(n): return 1 if n <= 1 else n * fact(n - 1)` never returned, because the recursive call was
unconditional and a base case that is a value rather than a stop fills the root stack — exit 2, the code
this contract reserves for a broken compiler. Now a pair the compiler can prove answers a value and nothing
else (literals, arithmetic and orderings over literals of one domain, a read of a slot the checker proves
was written) still folds to the `select` ADR 0262 pinned, and every other arm is lowered **inside the
block the branch enters**, the two answers meeting at a block-parameter `phi` — so the rejected arm's call,
division, subscript and unwritten-slot test are not in the module on that path at all, while the arm that
DOES run still raises its own class at exit 3 with its own frame in the traceback.

A ternary with **text arms** prints the text, not the intern table's position (ADR 0290, closing `Gap R.173`
and paying `Gap R.127`'s text half). `print("y" if 1 else "n")` exited 0 with `0`, and
`print("big" if x > 2 else "small")` with `0` or `1` depending on which way the test went — those digits are
the slots `y`, `big` and `small` hold in `@str_tab`, and they were printed because **four** different
"what kind is this expression?" predicates in the compiled backend (`stringVal`, `exprIsString`,
`printsAsInternedStr`, `methodReturnsStr`) had no ternary arm. ADR 0262 had already established the rule — a
ternary hands back one of its arms, so the answer's kind is the arm's kind — but applied it only to numbers.
Fixing three of the four changed nothing: `print` folds through `stringVal` first. That is the "one road" rule
with the quantifier made explicit. The run-time-test pair is now a real `select` over interned indices (legal
because interning is idempotent), while a constant test still evaluates only the arm that runs. A container
arm still refuses — that half belongs to L11.1's tagged value word, and the test says so rather than claiming
it.

A list and a dict have **the methods their reference's containers have** (ADR 0301, closing `Gap R.188`
and advancing `Gap R.63` / `L12.11`). `xs.extend([2, 3])`, `xs.insert(0, 9)`, `xs.index(2)`,
`xs.remove(x)`, `xs.clear()`, `d.update(o)`, `d.pop(k)`, `d.setdefault(k, v)` and `d.clear()` all answered
`no such list method` / `no such dict method` on **both** engines. That is a *missing* answer rather than
a wrong one — the class no pin can catch, because there was no output to compare — and the exit-1 sentence
blamed the program for a feature the language lacked. `d.setdefault(k, []).append(v)`, which is how
anybody groups rows, now works; `index` finds by value equality (so `[1].index(True)` is `0`) and raises
`ValueError: 5 is not in list` rather than answering `-1`, which is a legal index a program would obey
into reading from the end of the list; `insert` clamps rather than validating. `d.popitem()` **exists and
refuses**, naming why: it is a dict method the compiled doors do not build over a literal (the `Gap I.2`
receiver table, beside `Gap R.126`'s pair), and a two-element list is not an answer — it prints `[1, 2]`
where the reference prints `(1, 2)`. The tuple itself is a value since **ADR 0361**; the door is what is owed. Along the way a `KeyError` learned to name the
**key** (`KeyError: 'a'`) on the leg that can: the compiled leg's raise is a module constant, and that
half is filed as `Gap R.189` PARTIAL rather than left looking closed.

A method that **changes a container in place answers the void** (ADR 0300, closing `Gap R.187`).
`print(xs.append(2))` printed `[1, 2]` on the record at **exit 0** and made the compiled leg emit
`printf(i8* @.fmt1, i32 )` — a call with a *missing operand*, which `llc` rejects — so the same one-line
program spent **exit 2**, the forbidden class. `add`, `discard`, `remove` and `clear` were the same, and
`sum([1, 2, 3].append(4))` answered **10** where the reference raises `TypeError: 'NoneType' object is not
iterable`. The mutation was never wrong; only the answer the statement throws away was, which is exactly
why nobody hit it — a program writes `xs.append(2)`, not `print(xs.append(2))`. The fix is one table both
roads read, keyed on the *shape* of the call: the record's `return recv, nil` ("so the REPL can show
the resulting list") went, and the print door asks `isNoneExpr`, which already knew a folded `dict.get`
miss was a void (ADR 0291). `pop` and `popitem` still answer with what they removed — `while xs:
x = xs.pop()` is why — and your own method named `append` keeps its own answer.

An f-string's **format spec is part of the program** (ADR 0299, advancing `L12.8` / `Gap R.60`).
`stripFormatSpec` used to cut the `:` suffix off and drop it on the floor — no AST field ever held it —
so `f"{3.5:.2f}"` printed `3.5`, `f"{7:05d}"` printed `7`, `f"{255:x}"` printed `255` and `f"{3.5:>6}"`
padded nothing, on **both** engines, at **exit 0**. Eleven shapes, engines in perfect agreement, which is
precisely why nothing noticed: parity compares the engines to each other, and only the oracle leg compares
either to CPython. The spec and the `!r`/`!s` conversion now live in the AST and both legs format
through one shared engine, whose digits never come from `printf` — the two diverge on exactly the cases
that matter (`%.0f` rounding, `%g`'s exponent rules, `%d` on what the language calls a `float`). A spec
this language cannot honour **refuses with a sentence naming it**; there is no "print the plain value"
fallback, because that is the bug with a parser bolted on. `f"{[1,2]}"` used to hand a list's *global
address* to `printf`'s `%d` and die in `llc` — exit 2, the forbidden class — and refuses now.

A text iterates **one character at a time** (ADR 0298, closing `Gap R.185`). `[c for c in "abc"]` answered
`[]` on the record at **exit 0** — not a refusal, not a trap, just an empty list a program would happily
iterate and never enter — and `max("abc")` answered `abc`, the whole string, because a text fell through to
the arm that treats anything unrecognised as a one-element collection. `for c in "abc"` had always been right
beside them: three roads asked "iterate this text" and read two different stores. All three now range over
the text per code point, so `max("aé")` is `é`. The compiled leg still refuses at exit 1 with a sentence
naming what it cannot lower — owed to `L11.1` — and was deliberately *not* brought into agreement by taking
the record's new answer away.

A text answers **truth** like any other value, and a string method answers with its own kind (ADR 0297,
closing `Gap R.183` and `Gap R.184`). `print(not "x")` said `True` on the compiled leg while `if "x":` —
asking the same question about the same value two lines away — said truthy: `not` used a helper that compares
the lowered word against zero, which is right for a number and, for a text, reads the intern slot. Nine text-
returning methods printed the intern **INDEX** (`0` for `"ab".zfill(5)`) because the print road knew three
methods where the fold implements fourteen, in a list that existed twice. And `"-42".zfill(5)` answered
`00-42` on **both** engines — the reference says `-0042` — so the parity matrix reported nothing and only the
oracle leg saw it; the rule now lives in one function the compiled backend call.

A dict view prints as a **view** (ADR 0296, closing `Gap R.182`). `print({"a": 1}.keys())` printed `0` on
the compiled leg — the heap handle through `%d` — and `print({1: 2}.values())` handed the address of a
compile-time global to a heap walker, which is **exit 2**, the compiler's own bug on a two-line program. Both
engines also printed a bare `['a']` where the reference prints `dict_keys(['a'])`, so the compiled backend agreed
and only the oracle leg could see the third defect. A view is now a list-shaped value that carries its own
name: `sum`, `max`, `min`, `for` and `in` keep working on it, and only the rendering knows it is not a list.
`items()` still refuses on the compiled leg, because a key/value pair has no value representation until
L11.3 — refusing is honest where a plausible-looking `[(…)]` would be fabricated.

A set counts **distinct** members, and a `dict.get` answers with the kind its **slot** has (ADR 0295,
closing `Gap R.180` and `Gap R.181`). Both were wrong numbers at exit 0 that no amount of correct-looking
test data would catch: `len({1, 2})` answers `2` on any implementation, so only a *duplicate* reveals that the
static set global reserved one slot per source element and `len({1, 1, 1})` said `3`. Same for `get` — an
int-valued dict always looked fine, while `{1: "a"}.get(1)` printed the text's interned INDEX (`0`),
`{"k": None}.get("k")` printed the void word (`0`) and `{1: True}.get(1)` printed `1`. One of them
(`{1: True}.get(9, True)`) was wrong on the **interpreter** too, so the compiled backend agreed on the wrong
answer and only the oracle leg saw it. The float case (`{1: 1.5}.get(1)` → `1`) stays open as `Gap R.105`
and is pinned by a test that reports it — not by a refusal, because the record answers it and taking
that away would be trading an answer for silence.

A slice of a container answers the **list**, not the machine's handle for it (ADR 0294, closing `Gap
R.179`). `print([1, 2, 3][1:])` was three bugs at once, none of them on the record: the container
literal lowered to the **address of a compile-time global** and went into `rt_slice` where a heap handle
belongs, so llc rejected our own module (exit 2); once that was fixed, `print` took the numeric road and
printf'd the handle — `1` at exit 0; and `rt_slice` copied element payloads **without their tags**, so
`print(["a", "b"][1:])` printed the interned text's INDEX — `[1]` where the reference prints `['b']`. The
last one is ADR 0187's pairing rule ("the operation that writes a slot's payload writes its tag") violated by
a runtime builder that had simply never been asked to follow it. Deferring a wrong number because a redesign
will eventually make it impossible is how wrong numbers survive.

`x ** y` answers the kind the **reference** answers with (ADR 0293, closing `Gap R.176`). `print(2 ** -1)`
said `0` on both legs and `print(4 ** 0.5)` said `1` — both at exit 0, both legs agreeing, which is
the configuration a parity matrix is blind to. The rule is not "either side has a dot": `int ** int` is an
int only when the exponent is non-negative, so `2 ** -1` is the float `0.5`; a float anywhere makes the
answer a float, so `2.0 ** 10` is `1024.0` and not `1024`. The compiled leg's root cause was **one missing
token** — `**` was absent from the operator list that tells `print` what kind an expression answers with —
so `llvm.pow.f64` computed the right double and the caller truncated it back through `fptosi` into `%d`.
`0 ** -1` now raises the reference's own `ZeroDivisionError` from a **run-time** guard (so a base bound to a
name traps exactly as a literal does), and `(-8) ** (1/3)` and `2 ** 100` are refused in words — a complex
value and an unbounded integer are things this language does not have, and `inf`, `nan` or a wrapped `0`
are not substitutes for either.

A container in a numeric operand raises or refuses — it never spends the contract's **exit 2** (ADR 0292,
closing `Gap R.175`). `print([0] * 3)`, `print([1, 2] + [3])`, `print([1] / 2)`, `print([1] - [2])`,
`print({1: 2} * 2)`, `print([] < {})` and ten siblings emitted a module `llc-20` rejected —
`mul i32 @.lst1, 3`, `add i32 @.lst1, @.lst2`, `sitofp i32 @.lst1 to double` — because `value()` renders a
container literal as the **address** of a compile-time global rather than a value. Exit 2 is reserved for a
bug of **ours**, so a harness could not distinguish "the compiler is broken" from "my program has a type
error", and `--interp` answered every one of them correctly. The split follows the **reference**, not what
this backend lacks: where CPython raises, both legs raise CPython's own sentence (exit 3, catchable, so
`print(e)` prints what `python3` prints); where CPython answers and no sequence helper exists, the compiled
leg declines in words (exit 1) and the record answers. Two things were deliberately left alone after
each was measured breaking an answer: a container as a **call argument** (`half([1.5])` = `0.75`) and a
**comparison over two names** (`a < b` = `True`, the tagged order road's question).

A dict lookup with nothing to find hands back **`None`** (ADR 0291, closing `Gap R.174`).
`print({"a": 1}.get("z"))` answered `0` on the record, so `print(v == 0)` answered `True` where the
reference answers `False` — a program could not even *test* for the missing key — while the compiled leg
refused the whole program with `get: key not found and no default`, although its own fold already knew the key
was absent. Both are the same representation `Gap R.171` caught leaving a function body: a void written as the
word `0`, reached this time from a builtin. The third defect was only visible in the refusal —
`d = {"a": 1}` / `d.keys()` was answered with *"string method keys on non-constant string"*, calling a dict a
string in a message quoted back to the author. That road now asks the same records the iteration and subscript
roads read before it speaks, so a diagnostic cannot contradict the codegen beside it.

A text predicate prints a **verdict** (ADR 0289, closing `Gap R.172`). `print("abc".startswith("ab"))` said
`1` and `print("abc".isdigit())` said `0` on **both** engines at exit 0. The eight methods — `startswith`,
`endswith`, `isdigit`, `isalpha`, `isalnum`, `isspace`, `islower`, `isupper` — had been answering correctly
all along, with the same 0/1 a comparison answers with; only the printer never learned, because a method
call's callee is an *attribute* (`Call{Fn: Attr{…}}`) and the "is this a bool?" question gave up on its very
first line for anything that was not a plain name. So the answer was right and its rendering was wrong — the
one thing every check misses: exit 0, a valid module, a plausible digit. Thirteen tests in
`integration/lang_test.go` had pinned `1`/`0` as the *expected* output, with a comment explaining the fold as
though it were the contract; those pins moved to the reference's answer in the same commit, because a pinned
wrong answer is the wrong answer with a green check. The predicate table asks the method's own name, and asks
the **receiver** first: `class Box: def isdigit(self): return 1` prints `1`, since what a class's own method
returns is a fact about that class body (ADR 0257's dunder rule).

Comparison chains answer what Python answers (ADR 0288, closing `L12.1` / `Gap R.53`). `a < b < c` is one
construct — `a < b` **and** `b < c`, with the middle operand evaluated **once** — and the grammar had been
folding it left-associatively into `(a < b) < c`, comparing an int against a boolean. Because this front end
*answers* that question instead of refusing it, both legs printed the opposite verdict at exit 0:
`print(1 > 2 < 3)` said `True` where Python says `False`, `print(1 < 2 > 1)` said `False` where Python says
`True`, and `if 1 < 5 < 3:` took the branch. `print(1 < 2 < 3)` said `True` too — the accidental pass, and the
reason a chain table must contain chains the nested reading gets *wrong*. The fix is L12.1's own design: a
comparison node carrying n operands and n−1 operators, **not** a desugaring to `a < b and b < c`, which would
run a call in the middle twice and let `and` skip the tail comparisons. The compiled leg gets "evaluated once"
by storing each operand into a local slot and reading the slot — and my first version of that handed the links
the *original* expressions, printing a middle call's output three times at exit 0: the bug re-imported by its
own fix. A chain over a **container** operand refuses on the compiled leg rather than emitting
`store i32 @.lst1, i32* %_chain1` (ADR 0234's compiler bug); the record answers it, and L11.1's tagged
value word owns the rest.

A builtin called with **no argument** is asked which kind of call it is, before its operand is reached
(ADR 0287, closing `Gap R.131`). `print(int())` died with a Go stack trace and **exit 2** — the code the
exit-code contract reserves for a compiler bug — and so did `float()`, `bool()`, `str()`, `ord()`, `chr()`,
`abs()` and `repr()`; `chr()` panicked the **compiler** too. They are two rules the single dispatch had
never separated: `int()`/`float()`/`bool()`/`str()` are **constructors** the reference answers (`0`, `0.0`,
`False`, the empty text), while `ord()`/`chr()`/`abs()`/`repr()` convert a required value and the reference
raises `TypeError: <name>() takes exactly one argument (0 given)` — so the first four now answer and the
other four raise that sentence, at exit 3, in the reference's own words rather than ours (ADR 0215 makes trap
wording observable). Two wrong answers were produced on the way there and both are pinned as tests:
selecting between the interned `"True"`/`"False"` strings made `rt_print_bool` print **inverted** verdicts
(it asks only whether its word is zero), and answering `float()` with a textual double made the print road
widen a value that already was one — `sitofp i32 0.0 to double`, which llc-20 rejects. `bool("")`/`bool("x")`
measure a text by its length rather than its interned index, and `bool([1])` **refuses** on the compiled leg
rather than reporting "non-empty" for every allocated handle — which is also what `bool([])` would have
gotten wrong. `repr()` is not `str()`: it has no default, and the asymmetry is pinned because the pair shares
one renderer and one table.

A text the body rendered **into a name** is a text-returning function (ADR 0286, closing `Gap R.170`).
`def f(v): s = str(v); return s` printed `0` for `f(3)` on the compiled leg at exit 0 — and `2` for
`s = "x" + str(v)` where the reference prints `x3` — because ADR 0281 taught the compiler that
`return str(v)` renders, and a body that bound the rendering one statement earlier still looked
number-returning to its callers: `print` handed `printf` an index into `@str_tab` with `%d`. The verdict
now follows the assignment, the same two-hop question ADR 0285 asked of numbers. Underneath it sat the
reason a wrong number was possible: `str()` of anything "not a text and not a float" fell to
`rt_str_of_int`, which writes the **decimal digits of the word it was handed** — right for an int, a
fabrication for a handle — so that road is now asked what it was given. Narrowing it nearly cost more than
it bought: refusing every parameter broke four programs that had always worked (`x = str(v); print(x)`
printing `3`), because a text argument is already refused at the call site by ADR 0174, and "gate the road"
had to become "ask the road" — with `x = "abc"` / `x = 5` / `print(str(x))` pinned at CPython's `5` so the
latest binding, not "was it ever a number", is what the gate reads. A `None` handed to a function is still
the number `0` (`Gap R.171`, verified pre-existing, filed not absorbed).

A number the body computed **out of its own parameter** keeps the kind the argument arrived with (ADR 0285,
opening `Gap R.169`). `def f(x): y = x + 1; return y` printed `1` for `f(0.1)` on the compiled leg at exit 0 —
and `0` for `y = x * 2`, `-1` for `y = x - 1`, `3` for `y = (v - 1) * 2`, `5` for a float default — four
believable truncated digits, all from the AOT engine while the record and the reference answered `1.1`,
`0.2`, `-0.9`, `3.0`, `5.0`. The scan's own comment named the hole: a parameter is written by the **caller**,
so no assignment in the body records it, `exprNumberish` answered the returned leaf "not a number", the
ordinary return road claimed the body, and `define i32 @gy_f(i32 %p0)` truncated the double the tagged door
had computed. The question is now asked knowing the function's own parameter names — scoped to the one body
being asked, because cycles 0280 and 0281 both measured what a module-wide widening costs. Three shapes
refuse rather than guess (`y = x` / `return y`, a binding under a condition beside an int arm, two bindings
deep), and two of their refusals were themselves **false about the program** — blaming "arithmetic over a
slot the program built at run time" for a value that came in through a call site, and claiming an integer arm
in a function that had none. Both now say what the arithmetic actually read (`Gap R.38`: a refusal the reader
cannot check against their own source is a defect of its own), and a test keeps the original container
sentence alive for the container that earned it.

A wrong-arity call **stops the program on both legs** (ADR 0284, closing `Gap R.168`). A callable read
out of a variable was called without being asked how many arguments there were: `g = lambda x: x * 2` then
`print(g(1, 2))` printed `2`, `print(g())` printed `0`, and `g = lambda x, y: x - y` / `print(g(3))` printed
`3` — all at **exit 0**, all arithmetic on values that were never passed, where CPython raises
`<lambda>() takes 1 positional argument but 2 were given`. `callClosure` handed `callFunc` a slice of however
many arguments there were, and the bind loop walks *parameters*, so extras were never indexed and a missing
parameter with no default kept its zero value. The count moved into `callFunc`, the road every caller shares,
before the bind loop: a fifth road added later inherits the question instead of having to remember it. A
`def` keeps the name the program gave it (`too many arguments for f`), a lambda is `<lambda>`, and the
compiler's generated `lambda_0` is never shown to a reader who never wrote it. Nothing about a *correct* call
tightened — defaults fill (`6`/`9`/`12`), keywords work out of order (`12`), recursion, methods and a lambda
through a parameter are CPython-exact on both legs, which is what the eleven-line probe exists to prove.

A declared name is **not a variable**, and a function is **not a number** (ADR 0283, closing the exit-2
half of `Gap R.150` and `Gap R.151`). `print(f)`, `f + 1`, `xs = [f]`, `str(f)`, `print(math + 1)` and
`print(lambda x: x)` all left `--aot` through **exit 2** — `llc-20` rejecting a module that emitted
`load i32, i32* %_f` for a slot no `def` ever allocated (a `def` writes a global `@gy_f` and allocates
nothing), or writing the closure's function global into an operand: `printf(…, i32 lambda_0)`,
`sub i32 0, lambda_0`. The guard sits at the **read**, in `value()`'s `*Name` case, because the first draft
put it in the arithmetic roads and `f + 1` exited 2 anyway — the float road lowers its operands before the
operator asks anything, so by the time the guard ran, the load was already in the module. In the numeric
door the answer is a **raise**, not a refusal: `abs(f)`, `-f`, `abs(math)`, `-(lambda x: x)` print CPython's
`bad operand type for abs(): 'function'` / `'module'` byte-for-byte on both legs and are catchable.
A *binding* is a different thing and still works: `g = f` / `g(21)` is `42` interpreted, a lambda through a
parameter is `12` on both legs — `nameIsAValueWithNoSign` asks `params` first so the fix is not a ban on
functions as arguments. Binding the `def`'d name on the record is what made reading it stop being a
`NameError` for a name the program had just declared, and it briefly routed `f(1, 2)` into the closure road,
which pads and drops arguments without asking: it printed `2` at exit 0. That trade is the one the ladder
forbids, so the declared road is consulted **before** the closure value, and the pin fails on silence
(`Gap R.168` records the road's own remaining defect).

A text at an arithmetic operator **refuses instead of answering** (ADR 0282, closing `Gap R.165`'s wrong
number). The case that opened the row — `print("%.2f" % 3.5)` — is CPython's `3.50`, a `TypeError` on the
record, and `0.0` on the compiled leg at **exit 0**: the format string was interned, and its `@str_tab` index
was widened with `sitofp` and used as a remainder's dividend. The sibling spellings already refused
(`"%d items" % 3`, `"%s!" % "hi"`, `"%x" % 255`) because with no float operand the program stays on the
integer road, whose text guard was the refusal; a `%.2f` conversion is what moved an expression onto the
double road, and that road's lift asked no question about kind. `floatValue` now refuses a text the way ADR
0249 refuses an unlift-able operand — record, return empty, let the caller name the missing feature. What the
refusal was *standing in for* has since been built: the shape is answered today by the formatting door
(**ADR 0360**, roadmap Gap R.31), and what this guard still owns is the text with no `%` behind it —
`"a" * 2.5`, `"a" + 1.5` — whose reference answer is a `TypeError`, not a number. `%` between two numbers is
still the remainder, and that is the other half of the tests: `7 % 3` is `1`, `-7 % 3` is `2`, `7.5 % 2` is
`1.5`, `-7.5 % 2` is `0.5`, `7.5 % 2.5` is `0.0`, `v % 2` over a pair-marked parameter is `1.5` and `1`, and
`1.0 == "a"` still answers `0` — the last of those is a narrowing the first draft broke, because the
equality road lifts both of its operands too.

A function that **renders** its answer is a string-returning function, and its callers are told (ADR
0281, closing `Gap R.163`). `def g(): return str(42)` / `print(g())` was `0` at **exit 0** on the
compiled leg — and `0` for `str(x)` of a name, `str(2.5)`, `repr(42)`, `str([1, 2])` and `str(None)`
too. The body was never wrong: it asked the rendering door and returned the interned index. The
caller printed that index with `%d`, because the program-wide predicate that decides which functions
yield text counted literals, f-strings, string parameters and concats, and had never been told about
a `str()` call — ADR 0224's bug class (`print(Dog().sound())` printed `0`) one door earlier. One case
in that predicate, with Gap R.6's guard still standing: `def str(x): return x + 7` beside that body
answers `49`, because a program that took the name owns it. `programs/probe_return_str.gy` is ten
lines, three legs, one answer.

`str()` and `repr()` are **one pair over one renderer** (ADR 0258, closing Gap L.2). `print`, `str()`
and a container element ask the same table: in the compiled backend the value printers no longer call
`printf` — every write goes through a sink that is either stdout or, while the pair renders, a capture
buffer whose bytes come back interned — so a form that exists for `print` exists for `str()` and
`repr()`, and the module fails its own test the day a second value renderer appears. A text is the
only value the two halves disagree on, and it disagrees the way CPython does: `str("hi")` is `hi`,
`repr("hi")` is `'hi'`, and inside a container both quote, which is why `print(xs)` and `str(xs)`
write one line. What that closed is a class of answers rather than one bug: `str([1, 2])` compiled
answered `0` and `str(None)` answered `0`, both with exit 0 — a missing rendering returning the number
underneath the value — while `str({1})`, `str(set())` and `str(1.5)` refused or reached `llc` with a
module it rejected, and a text built at run time printed `(null)` inside a container because only the
compiler had ever been able to produce a repr. Three container builders wrote per-slot tags without
saying so on the object, so `print(["a", 1])` was right while `str(["a", 1])` answered `[0, 1]` from
the same object; objects now describe their own slots on every assignment path. A value whose kind is a **run-time** fact asks
the same door: `str()` and `repr()` of a slot read out of a container the program built, of a name bound
from such a slot, and of a loop variable stepping over a container that mixes kinds all route through the
module's one tag-reading printer (`rt_str_of_value` → `rt_print_mixed_value`), so `str(v)` can never
disagree with `print(v)` (ADR 0303). The same pair answers arithmetic where the operator cannot
answer a non-number: `n - 1`, `-n` and `n // 2` for `n = xs[0]` sum the number the tag says and raise
CPython's own sentence, per kind, when the slot holds a text or a container (ADR 0304) — and it reaches the
double domain the same way, by its tag: `n / 4` is `1.75`, `n >= 7` is `True`, `n + 2.5` is `9.5`, and a text
slot in an ordering raises `'>' not supported between instances of 'str' and 'int'` naming both types in source
order (ADR 0305). It is a **container element** too: `print([n])` is `[7]`, `print([n, "x", 2, None])` is
`[7, 'x', 2, None]`, and a text slot prints `['a']` rather than the interned index it stores, because the
literal is heap-built and the element writes its payload *and* its tag while the object is marked
self-describing (ADR 0306). It is an **f-string field** as well: `print(f"{n}")` is `7`, `print(f"{n - 1}")` is
`6`, a text field prints `xay` rather than the interned index, and `!r` asks the same printer with the quote
flag — the field, `str(n)` and `print(n)` are one renderer pointed at three sinks, which also ended a
conversion that printed nothing at exit 0 and a quoted literal that `llc` rejected outright (ADR 0307,
Gap R.192). It is the **signless call** as well: `print(abs(n))` is `7`, `abs(neg[0])` of a slot holding `-8`
is `8`, `abs(half[0])` of `-2.5` is `2.5`, `abs(flag[0])` of `True` is `1`, `abs(table["k"])` of `-9` is `9`,
and the answer is itself a pair so `y = abs(n); print(y)` is `7` — `abs` is the arithmetic door's own operand
code, taking the magnitude of the *lifted* value, so the answer's kind follows the operand's and a text slot
raises the call's own sentence, `bad operand type for abs(): 'str'`, never the unary minus's (ADR 0309,
ADR 0271). It is a **dict entry and a set member** as well: `print({"k": n})` is `{'k': 7}`, `print({n: 1})` is
`{7: 1}`, `print({n})` is `{7}`, and a text slot gives `{'k': 'a'}` and `{'a'}` rather than the interned index
it stores, because a dict and a set each have one builder that takes the tag *with* the payload
(`rt_dict_put_tagged`, `rt_set_add_tagged`), a register is an `i32`, and the pair's tag is a register the
objects wrote — the same shape ADR 0306 paid for a list element, asked of two more builders, with nothing added
to the runtime. The binding keeps its tags too, so `d = {"k": n}` answers `d["k"]`, `len(d)`, `k in d`, `d[k]`,
`str(d)`, `d == {"k": "a"}` and a `for k in d`, and `s = {n}` answers `7 in s`, `len(s)` and a `for v in s`. A
key or a member asks one question an element never asks — **can this value be hashed** — and the tag is what
knows: `{c: 1}` over a container slot raises `TypeError: unhashable type: 'list'`, and so does a container used
as a dict key or a set needle at any of the three dict doors — `d[[1]] = 2`, `print({"a": 1}[[1]])`,
`[1] in {"a": 1}` — catchable by the program's
own `except TypeError:`, where reading the payload alone would have put an address in the bucket and printed a
plausible container at the exit code of success (ADR 0310, ADR 0232, Gap R.81). The other container family
asks the other question — a list and a string **count** positions, so there the question is *is this a
position*: `print(xs["a"])`, `print(xs[1.5])`, `print(xs[None])` and `s["a"]` each printed an element at exit 0
(a text index is an index into `@str_tab`, so the door counted to the slot the interned number named) and now
raise `TypeError: list indices must be integers or slices, not str` — quoted for a string, bare for a list —
while `xs[True]` stays position 1, a slice stays a slice, and `xs[[0]]` compiles instead of lowering a list
literal into the bounds check's `icmp slt` and making `llc` reject the module at exit 2, the code the contract
reserves for a compiler bug (ADR 0329, Gap R.222; `unhashable_key.gy` and `list_index_kind.gy` are corpus
surface, byte-identical to CPython). What still refuses is a
position that keeps **one word** for a whole value — `n + 1`, `abs(n) + 1`, `round(abs(n) / 2)`, `min(n, 3)`,
a literal `sum`/`min`/`max` folds into a static array, a pair handed across a call — and the refusal names
what the value is and where it came from, never a loop that the program does not contain (Gap R.38, Gap R.146; the residual shapes are Gap
R.115 and an unrelated container's nested slot read refusing, Gap R.191's — the tuple that used to be in
this list is a value since **ADR 0361**, see `docs/language.md` § *Tuples are values*). An
**interpolation** is no longer in that list: since **ADR 0359** `s = f"x{n}"`, `f"a{n}" + "!"`,
`f"v={n}".upper()`, `[f"a{n}", 1]`, `{f"k{n}": 1}` and `for c in f"ab{n}"` answer, `print(f"{xs}")` writes
`[1, 2]` instead of the handle (Gap R.114 paid with the same change), and what the interpolation still owes is a
format spec over a value the compiler cannot read (**Gap R.308**). `--json --eval 'repr("hi")'` reports
`{"result": "'hi'", "type": "str"}`, and `programs/probe_render_pair.gy` is `match` on both legs.

A comprehension that folds **is** the literal it folds to: `sa = {x for x in [1, 2, 3]}` and
`sa = {1, 2, 3}` reach one lowering — a heap object, every slot written with its payload and its tag,
the variable's kind recorded — so print, `in`, subscript and `for` treat a bound set or dict
comprehension exactly like the literal (ADR 0234). `{x for x in xs if x > 1}` parses on the compiled backend:
the `if` is the comprehension's, not a ternary's.

A class pattern is a question about a class, in the compiled backend: `case Point(x, y):` matches an instance
of `Point` or of any subclass of it and binds `x` and `y` to the instance's attributes **of those
names** — and an attribute the instance does not have **fails the case**, which the compiled backend
could not ask until `@inst_set` started recording which slots have been written (ADR 0235). The class
may be named directly or reached through a binding (`Alias = Point`, in a function body too); a case
whose pattern is a call — `case f():` — compares the call's result to the subject.

## Gradual typing & the type system

Optional annotations on variables, parameters, and returns are checked
statically by `--verify`, with `any` as the dynamic escape hatch; untyped code
falls back to dynamic dispatch.

- **Union types** — `int | str`, `int | float`, and `None | int` sugar for
  `Optional`; inferred and checked across assignments, call boundaries, and
  returns. In AOT, a union-annotated scalar variable gets a tagged `%unionbox`
  slot (runtime member tag 0=int, 1=float, 2=str) so `print` dispatches on the
  live member — an `int` member prints as `%d`, a `float` as `%f`, a `str` as
  `%s`, even after cross-member reassignment under branches/loops.
- **Literal types** — `Literal[1, 2]` annotations feed `match`
  exhaustiveness + narrowing on constants.
- **Type narrowing** — after `if isinstance(x, int):`, the checker narrows `x`
  from `any` to `int` in the then branch and away from it in the else branch;
  `not isinstance(x, T)` flips those; union complement narrowing uses
  `dropType`.
- **Walrus operator** — assignment expressions `name := expr` usable inside
  `if` conditions and comprehensions (`if (n := len(x)) > 0:`), scoped per
  Python 3.8+.
- **Variance + generics (L6.6)** — one subtyping relation implements a declared
  variance table: `list[T]` / `set[T]` / `dict[K, V]` are **invariant** (they are
  writable), `Sequence[T]` / `iter[T]` / `tuple[...]` are **covariant** (read-only,
  so an element type may widen), `Callable[[P...], R]` is **contravariant** in its
  parameters and covariant in its return, and user classes are **nominal** —
  `a: Animal` accepts a `Dog` because the declared base chain says so. A freshly
  built container literal may widen its element type to the destination
  (`x: list[int | str] = [1]`). Every rejection names its rule and carries a
  stable `code` (`type.variance.invariant`, `type.variance.contravariant`, …) plus
  an actionable `suggestion`; the whole model is machine-readable via
  `gustyc --variance`.
- **`print` behaves like Python's** — `print("n =", 42)` writes `n = 42`, not two
  lines: arguments are joined with `sep=" "` and terminated by `end="\n"` (both
  honoured for every argument kind, including runtime containers, whose printers
  take the newline as a flag rather than baking it in). And the line is **one unit**:
  every argument is evaluated first, so an argument whose own body prints writes its
  line before the caller's — `print("got", twice(21))` writes `<< 21 >>` then `got 42`,
  exactly as CPython does — and a statement whose later argument raises writes nothing
  at all, so no half-line precedes the traceback (Gap L.5, ADR 0355; the one shape still
  interleaved, a union-annotated argument printed from its tagged arm, names its own reason
  in `docs/language.md`).
- **Containers are references everywhere** — pass a `list`, `dict` or `set` to a
  function as a literal, variable, keyword argument, default, comprehension or
  generator result and the callee sees the same live object on the compiled backend:
  the AOT backend materialises container literals into the runtime heap and
  infers each parameter's container kind from annotations, defaults and call
  sites (forwarding included), so `for x in xs`, `len(xs)`, `xs[i]` and
  `xs.append(v)` work on parameters exactly as on variables. Binding one is the
  same story (ADR 0163): `ys = [x * 2 for x in [1, 2]]` — even constant-folded,
  even at module scope — yields a rooted heap handle, so `print`, `len`, indexing,
  iteration and calls all see the container, not a folded global's address.
- **The verifier is a pipeline stage (L8.2)** — the AOT backend emits textual IR, so
  `Build` runs LLVM's own module verifier (`opt -passes=verify`, `llc -filetype=null`
  fallback) over the module it is about to link and reports the verdict in
  `BuildResult.verification`; `gustyc --verify-llvm <src>` exposes it as a
  machine-readable record (`ok`/`tool`/`skipped`/`pipeline`/`errors`/`note`) so an
  agent can tell "the compiler emitted bad IR" apart from "my program is wrong" —
  without scraping `llc` output. A missing toolchain is reported as `skipped`, never
  as a pass. Turning it on is how Gap I.3 was found.
- **The line table lives in the module, and the report is read back from the artifact (L8.5, ADR 0231)** —
  `--debug` used to add `-g` to a link step that had nothing to stringify: DWARF is written by `llc`
  from `!dbg` metadata, and the module had none. Codegen now records which statement each stretch of
  emitted code was written for, and a post-pass lays the LLVM debug metadata over the finished module:
  a `DICompileUnit` that names the language (`DW_LANG_Python`, not a generic guess), one
  `DISubprogram` per *program* function — never the compiler's own GC, exception or printer blocks,
  which are not code the program wrote and must not be blamed for it — and a `DILocation` per
  instruction. Then the toolchain reads it back: `--debug-info` reports the table out of the emitted
  IR's own metadata (`definitions.debugInfo`, one entry per function, an IR-line-to-source-line row
  table, and a `defect` field for when the module disagrees with the emitter), and `--build --debug`
  runs `llvm-dwarfdump` over the object it just linked (`definitions.dwarfReport`) so a claim about
  DWARF is a claim about the artifact. That is how a `DISubprogram` with a malformed `type:` was
  caught: `llc` printed `invalid subroutine type`, exited 0, and wrote an empty `.debug_line` while
  every internal count looked perfect. `--emit-source-map` v2 carries the same table, and
  `integration/debug_info_test.go` ends by asking `llvm-addr2line` where a function lives.
  Fixing it required closing a Gap-K.6-class hole first: assignment to an attribute and tuple
  assignment were built with no source position at all, so their instructions inherited the previous
  statement's line (§ Every statement has a position, `docs/language.md`).
- **Declaration order that matches the language** — mutually recursive functions, and helpers
  declared below the code that calls them, check clean and compile; a call at module level and a
  decorator still require the name above them, because that code runs where it is written
  (`programs/forward_defs.gy`, ADR 0197)
- **Your function names are your own** — `def sync`, `def main`, `def exit` are emitted as `gy_sync`,
  `gy_main`, `gy_exit`, so the linker can never answer the program's own call from libc, while
  `extern fn` keeps the C name it binds; `nm` on the built binary and the source map's `symbol`
  field both show the link name (`programs/host_symbol_names.gy`, ADR 0198)
- **Built-ins are shadowable, on both paths** — `def str`, `def float`, `def len` mean what the
  program says they mean, exactly as in CPython, instead of being answered by the compiler's own
  reading of the name (`float(1)` printed `1.0` for a function returning `x + 7`); the constant
  folding still runs whenever nothing shadows the name (`programs/shadowed_builtins.gy`, ADR 0199)
- **A method and a helper may share a name** — `def time` beside `class Timer: def time(self, x)` is
  ordinary vocabulary, and the two definitions are keyed apart instead of the method overwriting the
  module function (a call then measured against `self`-inclusive arity, refusing a program every
  other layer ran) (`programs/method_function_name_clash.gy`, ADR 0200)
- **A dropped argument is refused at the call** — too many arguments was already an error, too few
  was not, so an unbound parameter came back later as an `undefined name` blamed on the callee's
  correct source while the interpreter had been refusing it all along (`programs/arity_defaults.gy`,
  ADR 0201)
- **A diagnostic is said once** — per-call-site return inference used to re-report everything inside
  a callee, so one warning appeared two or three times and the length of the JSON `diagnostics` array
  was not a count of findings (ADR 0202)
- **Only grammar words are reserved** — `print` and `range` were keywords, so `def print`, a parameter
  named `range`, a keyword argument named `print` and a method named `range` all failed to parse;
  built-ins are ordinary names, and the keyword table is now exactly the words that change grammar
  (`programs/builtin_names_as_defs.gy`, ADR 0203)
- **A program's stdout is only what it printed** — the interpreter used to echo a file's final bare
  expression (`f(5)` last printed `10`) while the compiled backend and CPython printed nothing, so the
  same source had two stdouts depending on the engine (ADR 0204)
- **A built-in name you claim must be defined above your uses** — `for i in range(2)` above a
  `def range` was the built-in to the record and the program's function to the compiled backend
  (two different outputs, no diagnostic); it is refused at the call instead (`ADR 0205`)
- **A default may sit anywhere in a signature** — `def f(a, b=1, c)` is a `SyntaxError` in CPython and
  ordinary source here, because positional binding fills left to right and a keyword call names what it
  fills, so every parameter is reachable (`programs/param_default_order.gy`, ADR 0206)
- **A module never calls a runtime helper it does not define** — which runtime blocks a module carries is
  derived from the code it emits, not from flags each codegen path had to remember, so the failure that
  surfaced as an `llc` "undefined value" error is gone; and where the compiled backend cannot act
  (iterating a run-time string) it refuses with a message instead of compiling a loop that silently does
  nothing (ADR 0209)
- **A negative subscript means what it means** — `xs[-1]`, `xs[-1] = v`, `"abc"[-1]` and the folded
  `[1, 2, 3][-1]` agree with CPython on both backends (the literal used to crash the compiler), while a
  dict's `-1` stays a key, because a subscript is either a position or a key and only positions count
  from the end (`programs/negative_index.gy`, ADR 0210)
- **A failure class has one code, whichever path produced it** — a program that trapped exits 3
  whether the record or native code ran it, an `llc` rejection of our own module is the
  compiler-bug class 2 on the run path too, and `--json`'s `exit` field is derived from the
  process status rather than written down (ADR 0211)
- **A built-in trap is a typed exception everywhere** — `7 % 0` raises `ZeroDivisionError` with
  CPython's wording and `except ZeroDivisionError:` catches it on the compiled backend; the compiled
  backend used to emit the instruction and keep walking, printing `inf` or a fresh garbage integer
  and exiting 0 (`programs/zero_division.gy`, ADR 0212)
- **Every `except` arm is a real arm** — arms are dispatched in source order on the compiled backend, a
  bare `except:` works in any position, a nested `try` reaches its outer arm, and an exception no
  arm matches propagates instead of being deleted (the compiled backend used to lower only the
  first arm and clear the flag, exiting 0 on a program whose error nobody handled, ADR 0213)
- **A trap the program cannot name is not a trap it can handle** (ADR 0214) — nine interpreter
  shapes (a missing attribute, `int("abc")`, a bad unpack, `x()` on an int, `len(5)`, `5[0]`) raised
  errors with a message and *no exception class*, so every `except` clause written for them was dead
  code. They raise what CPython raises, in CPython's words, and the tests pin class *and* wording:
  `'P' object has no attribute 'nope'`, `invalid literal for int() with base 10: 'abc'`, `not enough
  values to unpack (expected 2, got 1)`. One of them had reported `cannot index null` about an
  integer — not untyped, just false, and the kind of wrong that sends someone hunting a null.
- **An operator is a question about two runtime kinds** (ADR 0215) — `print("a" * "b")` used to
  print `1099516870662` and exit 0, because an operand that wasn't a known container went into the
  arithmetic path holding a heap handle. So did `1 + None`, `[1] + 1`, `"a" < 1`. Mistyped pairs now
  raise `TypeError` in the reference implementation's words (22 shapes verified by running `python3`
  and diffing the report line), and the *legal* pairs the same path was silently eating — `[1] + [2]`,
  `[1] * 3`, `"ab" * 2`, `"a" < "b"` — compute values instead of numbers-no-one-wrote. Chasing it also
  found the record's untagged values colliding with ordinary arithmetic: a bench loop computing
  `i * i` reached the heap's id range at `i = 1024` and read back the class's own method object, so the
  heap now starts at `1 << 48` and one predicate decides what an object is.
- **`//` and `%` floor, and they are one rule** (ADR 0216) — `print(-7 // 2)` printed `-3` compiled
  and `-4` interpreted, and `print(-7 % 2)` printed `-1` on *both*, because Go's `/` and `%` truncate
  toward zero while Python floors (the remainder carries the divisor's sign, so `-7 % 2` is `1`).
  The compiled backend now emit the correction (`sdiv`/`srem` plus a `select`; `frem` plus `fadd` and
  `copysign` for floats), and 312 integer and 392 float sign combinations are checked against CPython
  on both paths. Two integration tests had pinned `-3.5 % 2.0 == -1.5` as correct — with a comment
  naming `frem` — because they had been written from the emitted IR rather than from the language; the
  new tests assert `a == (a // b) * b + (a % b)` instead, which a consistently truncating pair can
  never satisfy.
- **A compound statement is not a scope** (ADR 0217) — this refused to compile:

  ```py
  def f() -> int:
      try:
          a = 7
      except:
          a = 0
      return a
  ```

  `--check` said `undefined name "a"` and `--aot` exited 1, while `--interp` printed `7` and CPython
  printed `7`. The analyser put a `try` body, each arm, the `finally` clause, a `while` body and each
  `match` arm in a child scope it then threw away — and never walked `finally` at all, so nothing
  inside a `finally` was ever checked, a call to a nonexistent function included. Visibility and
  definiteness are now separate questions: bindings join the enclosing function or module, and a name
  only some paths assign is read with a `possibly unbound` warning instead of the old error, which is
  what the program actually does. Two compiled-backend defects surfaced on the way and are recorded
  with their measurements rather than bundled: a handled exception that the next call re-raises
  (Gap R.21's compiled half), and an untouched slot being loaded and printed as a value (Gap R.36).
- **A handled exception is over** (ADR 0218) — the compiled half of that first finding, fixed the
  cycle after it was measured. The compiled backend holds the exception in one module-wide bit, and
  nothing was ever told the search had ended: the arm ran, the program continued, and the next call to
  a user-defined function found the bit still set and reported the exception a second time — after the
  handler had already handled it. `print(5)` after the `try` was safe, `print(f())` was not, and
  `print("handled")` inside the arm printed `handled` before dying, which is what proved the arm had
  run. Every edge that leaves an accepting arm now clears the flag, including the `return`, `break` and
  `continue` that stepped past the one edge the clear was on; an arm's own `raise` and the unmatched
  re-raise deliberately do not, and a program whose only arm always raises must emit no clear at all.
- **Citations in the record must resolve (ADR 0219)** — the roadmap said two `%`-formatting shapes
  "stay pinned as `programs/probe_percent_format.gy`"; that program did not exist, had no ledger row,
  and its sentence had long since gone stale (the interpreter no longer returns `0` there, it raises).
  It was one of seven dangling citations found in one pass — renamed files still cited under their old
  names, a historical name left in prose after the file was promoted, a claim of measured debt with no
  measurement. `TestRecordCitationsResolveToRealPrograms` now scans roadmap, README, `docs/` and every
  ADR: `programs/NAME.gy` must exist unless marked `(planned)` (the notation for a program a roadmap
  item still owes), a bare `probe_*.gy` must exist because the prefix is a claim about the corpus, and a
  near-miss fails with "did you mean programs/X.gy". The session-learnings file is exempt — it is
  allowed to name a file precisely to report that it is missing. `%` formatting was a gap then (R.31 — it
  landed later as ADR 0360), but the entry has had the artifact it always claimed since the day it was filed.
- **An unwritten slot raises — it does not answer** (ADR 0228) — `def f(c): if c: x = 1; return x`
  called with `False` printed `0` and exited 0; `while 0: w = 1` in a function printed `8555776`; a `try`
  cut short before its second assignment printed `518208`; `if 0: x = 1` then `print(x)` at module level
  printed `64`. Those numbers were the frame's previous contents — leftover words and stale heap
  handles — read as values and reported as successes. The compiled backend now raise what CPython raises:
  `UnboundLocalError` when the frame owns the name, `NameError` when nothing does, catchable by class on
  either engine, exit 3 either way. The mechanism is one byte on each slot *the checker* cannot prove was
  written (codegen gets no dataflow rule of its own), cleared on entry, set by every write, tested at the
  read — and absent, with no instruction emitted, wherever assignment is provably definite. Three checker
  rules turned out to be the cause: a `for` body may run zero times, a `match` may match nothing, and a
  loop variable is certainly bound inside its own body. The probes also caught the linked binary exiting
  1 — the compile-error code — for a program that merely raised, which ADR 0211 does not permit.
- **A string is an index into a table the runtime can add to** (ADR 0229) — `s = get(); print(s[1])`
  refused as "index of a non-literal variable", `def f(s): return s[1]` printed `1`, and
  `get()[1].upper()` printed `2`, all with exit 0 while CPython and the record printed `b` and `B`.
  Thirteen shapes were compile-time refusals for programs Python runs. A compiled string is an
  `@str_tab` index and the table is content-addressed and already grows at run time, so the fix was not
  a new representation but six runtime helpers that take indices and return them — and one question the
  compiler had been asking too narrowly: *is this a string?*, not *can the compiler read its text?*.
  Subscripts at run-time positions, `len`, `ord`, and `upper`/`lower` now answer on the compiled backend, and
  because equality is by content, a string built while running compares equal to the literal that spells
  it. The table's overflow path used to reuse its last entry — printing a different string than the
  program had built — and now raises a catchable `RuntimeError`.
- **A string built at run time is a buffer, an intern, and the same index** (ADR 0230) — the write half
  of the same gap: `"a" + word()`, `s[i:i+2]` where the bounds are values, `str(get())`, `.strip()`, and
  `for c in <runtime string>` were compile-time refusals for programs Python runs. Iteration had been
  worse than a refusal before it was refused: the string's table index was read as a repeat count, so
  the loop printed *nothing* and exited 0 (Gap R.16). Five more runtime helpers close it, and since
  interning dedups by content, a built `"ab"` and the literal `"ab"` are one value with no special case.
  The suite lesson: `for c in txt()` had quietly become the canonical "the backend refuses" fixture in
  four tests — a pinned refusal is a claim about the future, and when the gap closes its fixtures have
  to move or those tests go green while saying nothing.
- **A comprehension that folds is the literal it folds to** (ADR 0234) —
  `sa = {x for x in [3, 1, 2]}`, `print({x for x in [3, 1, 2]})` and `2 in {x for x in [1, 2]}` each
  reached `llc` as a folded container global in a value slot (`store i32 @.set1, i32* %_sa`,
  `rt_print_list_mixed(i32 @.set1, 0)`, `rt_contains(i32 @.set1, i32 2)`) and came back as exit 2, the
  compiler blamed for ordinary Python. The list spelling had been fixed twice (print builds the
  object, the binding copies the fold into the heap); the set and dict spellings had been left behind,
  and their print branch asked one printer for all three kinds — a valid module would still have
  rendered `{1, 2}` as `[1, 2]`. The `{x for x in xs if x > 1}` form never reached codegen at all: the
  iterable was parsed as a full expression, the ternary inside it ate the comprehension's `if`, and
  the file died on `expected keyword "else"` while the list twin parsed. One fold now produces the
  literal, and one binding rule binds it — plus the module-wide guard that a folded container global
  never appears in an operand position.
- **A numeric rule is an IEEE operation, not a habit** (ADR 0236) — `round(2.5)` answered `3` on both
  backends where CPython answers `2`: `math.Round` in the evaluator, `@llvm.round.f64` in the compiled
  runtime, `math.Round` again in the compiled constant fold, and four tests — two of them stating
  "half-away-from-zero" in a comment, one pinning `i32 3` in the IR. Four authorities agreeing is what
  makes a wrong answer survive review, and parity cannot see this class at all: it compares the two
  implementations to each other. The compiled backend now name the operation — `math.RoundToEven`,
  `llvm.roundeven.f64` — and `programs/round_ties.gy` entered the corpus with no ledger row, which here
  means "print what CPython prints", so the old answer is a CI failure. What the same probe found and
  did not fix, until ADR 0263: `round(2.345, 2)` is `2.35` in CPython, `2` in the interpreter (the digit
  count is ignored) and an exit-1 refusal compiled — a compile-error exit code for a program CPython runs,
  which was Gap R.69 and the exit-code contract's own subject (ADR 0211).
- **The digit count rounds the decimal, so the compiled backend borrow the conversion** (ADR 0263, closing
  Gap R.69) — `round(x)` had been taught to name the IEEE operation; `round(x, ndigits)` arrived with
  three answers, one per engine, and the third was an exit code. The row's own suggested fix — scale by
  10ⁿ, ask `roundeven`, unscale — was measured and thrown away, on 1,077 of 375,224 fractional pairs and
  74,838 of 156,048 negative-digit ones. It fails in the worst available way: it prints CPython's `2.35`
  for the row's own example, because that double happens to sit above its tie, and disagrees elsewhere.
  The mechanism is that **scaling manufactures ties the value never had** — `0.005` is
  0.00500000000000000010408… and rounds up to `0.01`, but times 100 it is exactly `0.5`, and a
  nearest-even rule answers that `0`; `0.075` scales to `7.5` and comes back `0.08` where the reference
  says `0.07`. What the reference rounds is the **exact
  decimal value of the double**, which is not a question the binary domain can answer at any level of
  cleverness about ties. It is a correctly-rounded double→decimal conversion, an operation neither
  backend owns: the evaluator asks Go's `strconv.FormatFloat`/`ParseFloat`, the compiled runtime asks the
  C library's `snprintf("%.*f")`/`strtod` through one `rt_round_digits`, and both are reached from one
  Go rule, `roundToDigits`. Neither is trusted on sight — 531,272 `(value, ndigits)` pairs were swept
  against CPython and compared bit for bit, the residual 143 (all between |x| = 4.117e18 and 1e300 with a
  negative digit count, all one ULP, where the scale itself is the inexact step) got a roadmap row instead
  of a passing test, and the same scale-and-`roundeven` shape at the negative branch sat 74,838 cases from
  the reference where the `%.0f`+`strtod` shape shipped sits 143. The answer is the kind the value arrived as (`round(3.5, 0)` is `4.0`, `round(5, 2)` is
  `5`), a digit count that is not an integer is **raised** as CPython's `TypeError` rather than refused
  into exit 1, and the two clamps are arithmetic facts with reasons: 324 digits is past every double's
  expansion, and 10³⁰⁹ is past the largest finite one.
- **Three names, both legs, three behaviours — and the docs agreed with the wrong one** (ADR 0264,
  closing Gap R.51 and Gap P.2) — `floor`, `ceil` and `sqrt` were in the checker's predeclared table since
  it existed, so `gustyc check` called any program that used them well-typed. Behind that table:
  `print(floor(3.7))` was CPython's `3`, an interpreted `NameError` at **exit 3**, and compiled `3.0`;
  `print(ceil(-0.5))` was `0`, `NameError`, `-0.0`; `print(sqrt(-1))` was a `ValueError`, `NameError`, and
  **`nan` with exit 0**; `print(floor("a"))` was a `TypeError`, `NameError`, and `0.0`. The test said so too
  — `TestExecFloorCeil` pinned `2.0\n-3.0\n3.0\n-2.0` under a comment quoting the language's own "the
  largest double <=", and roadmap's Gap P.2 quoted the same phrase back as a *decision*. Three voices
  agreeing with each other and with nothing outside, which is why every row here is asserted against
  `from math import floor, ceil, sqrt` on the first line of the same bytes.
  What shipped is one rule per name in `pkg/lang/math_names.go`, read by the compiled backend: `floor`/`ceil`
  answer a **whole number** (the reference's `math.floor`/`math.ceil` return an `int`) and `sqrt` a float,
  the checker types them that way too, and a whole number beside a float is **widened, not truncated** —
  `floor(2.7) + 1.5` is `3.5`, `[floor(2.7), ceil(2.2)]` is `[2, 3]`, `str(floor(2.7))` is `2`. A text, a
  `None` or a container argument, a negative under `sqrt`, a NaN or an infinity under `floor` are all
  **raises with the reference's sentence**, exit 3 and catchable by class on both legs — `must be real
  number, not str` (unquoted, unlike this language's other `TypeError` family, because CPython writes two
  different sentences and a program's `except` reads them), `math domain error`, `cannot convert float NaN
  to integer`, `cannot convert float infinity to integer` — the last of which needed `OverflowError` in the
  exception table. Beyond the compiled `int` word the legs disagree on purpose: `floor(3000000000.0)` is
  answered by the `int64` evaluator and **raised** by the compiled leg rather than letting `fptosi` return
  poison, and the split is a ledger row with a pin per leg (Gap R.133, owned by L12.12). Walking that
  boundary found the bug the middle would have hidden: the lower guard was `-2147483649.0`, which lets
  `-2147483649.0` through into the poison truncation, where it printed `2147483647`. And two findings the
  row did not own got IDs of their own: a folded `inf` could not be emitted at all — `x = float("inf")`
  died in `llc` with `inf.0e+00`, **exit 2**, and now writes its IEEE bit pattern with both bad spellings on
  the `forbiddenIR` blacklist (Gap R.134, closed here) — and `1e18` does not lex, filed with the ten other
  predeclared names no engine can call (`pow`, `divmod`, `hash`, …), `print(pow(2, 3))` being a program
  CPython evaluates and the compiled leg answers with exit 1 (Gaps R.135, R.136).
- **A class pattern asked two backends the same question, and got two answers** (ADR 0235) —
  `case Point(a, b):` on an instance with `x` and `y` **matched** compiled and printed `pt 0 0`, while
  the record and `docs/language.md` both say a missing attribute fails the case: the compiled arm
  checked the class chain and never asked the instance, whose data words cannot tell an attribute that
  was never written from a stored `0`. The alias form was worse in both directions — `case Alias(x, y):`
  inside a function was exit 2 (`%t6 = icmp eq i32 %t5, `, an `icmp` with nothing after the comma, from
  `alias, _ := g.value(...)` throwing away the error) and exit 3 interpreted (`TypeError: 'type' object
  is not callable` — the pattern fell through to *calling* the class) — and `case f():` loaded `%_f`, a
  variable that does not exist, because the same branch had decided any non-class name must be one. One
  front-end table now says what a pattern-position name denotes and what attributes exist; the body's
  scope reaches the module for a bare class name (ADR 0227); and `rt_inst_put` writes presence with the
  value, cleared per instantiation because heap slots are recycled — 300 instantiations under GC stress
  and a `ghost` attribute nobody wrote stays absent. `--lang` never mentioned patterns; it does now.
- **A container slot is a word — ask what fits before writing it** (ADR 0226) — `[1] == [1.0]`,
  `print([1.5, 2])` and `1.0 == [1]` reached `llc` as invented operands (`[1 x i32] [@env_store = ...`,
  `%t1 = sitofp i32  to double`, `%t2 = sitofp i32 @.lst1 to double`) and came back as exit 2, while
  three neighbouring shapes were *green* on truncation: `{1.5} == {1.6}` compiled to True. A container
  now holds what a word can carry — ints, interned strings, `None` — and a float element is refused with
  a message naming the missing representation. Two mechanisms were behind it: a global written into the
  module before its elements were validated (leaving an unterminated definition that downstream paths
  shipped), and `valueText` discarding a lowering error and returning `""`. A number compared with a
  container is also answered by kind now, the way CPython answers it, not by coercing the container.
- **A subscript of a string is a one-character string** (ADR 0225) — the compiled backend answered `s[1]`
  with `98`, and the missing type spread to everything the value touched: `s[0] + s[2]` did arithmetic
  and printed `196`, `s[1] == "b"` said false (compiled as well as interpreted — the compiled backend
  agreeing is what hid it from a green suite), and `len(s[1])`, `s[1].upper()`, `ord(s[1])` trapped. A
  string is counted in code points everywhere position is asked about (`s[i]`, `s[a:b]`, `len`, `ord`),
  so `len("café")` is 4 and `"café"[3]` is `é`. Measuring it also found a condition emitter that
  replaced an un-lowerable condition with a false branch and printed `0` for `1 if s[1] == "b" else 0`:
  a part that cannot be lowered is now a compile error, never a default value.
- **A string value is an index, not a pointer** (ADR 0224) — `x == "hi"`, `"a" in xs`,
  `self.w = "hi"; print(C().w)` and a method's `-> str` result reached `llc` as an `i32` holding the
  address of a string global (`icmp eq i32 @.str1, %t1`, `ret i32 @.str1`) and came back as exit 2,
  the compiler blamed for an ordinary program. A string value is an index into the runtime interned
  table everywhere a *value* is asked for — `rt_str_intern2` on the way in, `rt_str_ptr` on the way
  out to `printf` — and the address of a literal stays only where bytes are the question. Printing an
  index with `%d` was the silent twin of the same bug: `print(f"hi {n}")` answered `hi 0`.
  Removing the refusal that covered this also exposed a filtered comprehension loop whose `phi` named
  a predecessor that never branches to it.
- **A method is a call like any other** (ADR 0223) — three different wrong interfaces came out of one
  emitter that had never been brought back to parity with functions:

  ```py
  class C:
      def m(self) -> int:
          try:
              return 3
          finally:
              print("fin")      # compiled: exit 2, `br label %` — an empty target

      def raiser(self) -> int:
          raise ValueError("boom")

  print(C().raiser())          # compiled: prints 0 and exits 0 · CPython: traceback
  ```

  A `try` in a method emitted a branch to an empty label because only `funcDef` set a raise-exit; a
  `raise` out of a method was invisible because no call site checked the exception flag after a method
  call — the program printed a value and carried on; and `emitClassMethod` threw away the error
  `g.stmt` returned, so any construct the compiler refuses inside a method became half a function and
  an `llc` rejection: exit 2, blaming the compiler for a source error (ADR 0166's rule). Methods now
  own their unwind path (which closes the GC frame they opened), clear the enclosing statement's
  handler/deferred state, name their traceback frame after the method the program wrote (ADR 0354
  prints `bump`, the way the reference does, and leaves the class-qualified `Counter.bump` to the
  line table, ADR 0231), report their refusals as compile
  errors, and every call site into program code — static dispatch, `super()`, the class-id `switch`,
  and a constructor's `__init__` — checks the flag. Inside the `switch` the check had to finish the
  arm and the join's `phi` name the check's continuation: a call site that can raise cannot also be a
  value producer for the join. What is still wrong is recorded with a minimal repro and a
  pre-existingness check against two older binaries: a method returning a `str` returns the raw string
  global (roadmap Gap R.42).
- **A deferred body belongs to every exit** (ADR 0222) — this ran the cleanup on the boring path only:

  ```py
  def f() -> int:
      try:
          return 1
      finally:
          print("fin")      # was: prints nothing, returns 1 · CPython: fin, then 1
  ```

  Eleven shapes measured against CPython, nine wrong, and **the compiled backend wrong identically** — the
  deferred body ran on fall-through and after a handled exception and was skipped for `return`,
  `break`, `continue`, and for an exception no arm matched. Parity could not see it because the two
  implementations agreed. The same statement hid a second bug: transfers travel as Go errors in the
  interpreter, exactly like raised exceptions, and the arms asked "did something come out?" instead of
  "did an *exception* come out?", so a bare `except:` **caught a `return`** and dropped the value.
  Now a `finally` runs once on every exit in both paths, innermost first, with Python's ordering —
  the return value is taken by the `return`, so `return n` hands back the old `n` even if the `finally`
  reassigns it — and a `return`/`raise` inside the `finally` replaces what was in flight. In codegen an
  escaping exception runs only the *innermost* pending body (the outer ones run on their own way out,
  and running the whole stack printed `outer fin` twice), and whether a body already left the block is
  judged by where control went, not by opcode, because an `if` also ends its block with a `br`.
- **Two numbers are one question** (ADR 0221) — this printed two different answers depending on
  which flag you used:

  ```py
  print(1 == 1.0)     # CPython True · interpreter 0 · compiled 1
  print(1.0 == 1)     # CPython True · interpreter 1 · compiled 1
  ```

  An integer was compared as a word against a float object's handle, so an int never equalled the
  float with the same value — and only when the **integer was on the left**, which is why it survived:
  a test written from the direction that worked never saw the one that didn't. Measured as 300
  comparisons (5 ints × 5 floats × 6 operators × both orders) the compiled leg was right on all of them
  and the record was wrong on exactly 8. Equality between two numbers is now one question about
  their values in either order, gated by the same `isHandle` predicate operators use, so `1 == [1]` and
  `1.0 == "a"` remain False rather than becoming errors — and container equality inherited it for free
  (`[1] == [1.0]`, `{"a": 1} == {"a": 1.0}`). What the tests turned up on the way is recorded rather
  than bundled: a *literal* `[1] == [1.0]`, and `1.0 == "a"`, emit modules `llc` rejects, so those
  programs exit 2 with a temp-file path where they should refuse (roadmap Gap R.40).
- **The module is a scope too** (ADR 0220) — this program did not exist:

  ```py
  def twice() -> int:
      return MAX * 2

  MAX = 40
  print(twice())      # CPython 80 · interpreter: NameError · compiled: refusal
  ```

  The scope chain reached an enclosing function but stopped before the module, so a script could not
  read a constant from a function — the most ordinary shape there is. A function's name is now
  resolved in its frame, then the captured closure environment, then the module it was *defined* in
  (a nested def gets its enclosing function's module; a function in an imported module gets that
  module), and because the lookup happens at call time the assignment may sit below the `def`. Each
  module scope became a permanent GC root for the same reason. The checker pre-collects top-level
  binding names and consults them *only* inside function bodies — module code still runs line by line,
  and two existing tests caught my first attempt doing it globally. The compiled leg still cannot
  reach a module binding: a name bound to a literal the module never rebinds is read as the value it
  is, and one the module rebinds lives in a `@gy_mod_*` global the callee can read (ADR 0227). Both
  legs of `programs/module_scope_in_functions.gy` and `programs/module_calltime_lookup.gy` print
  CPython's line now; a body reading a module *container* is still refused, with the reason that names
  module state rather than blaming a string.
- **A module name has one slot, whichever statement binds it (Gap R.49, ADR 0351)** — the row was filed as
  `llc` rejecting a module (`y += 3` storing to a `%_y` nothing had allocated: an ordinary program leaving
  as exit **2**), and it had grown a silent face nobody measured: an unrolled `for` bound its variable to a
  main-frame alloca while every read consulted the name's `@gy_mod_y`, so `y = 2` / `for y in [10, 20]: print(y)`
  printed `2`, `2`, `2` at exit 0 where CPython prints `10`, `20`, `20`. One name had two slots and three
  writers because two questions each had one answer: *which names need a global* (the walk over bodies
  counted a `def`'s **parameter** as a module read — now shadowed away, per scope, as the reference scopes
  it) and *which roads bind a name that has one* (only the plain assignment asked; `+=` and both `for` roads
  now call the same helper). A loop keeps its rooted alloca beside the global, because an element can be a
  handle and the collector traces root entries, not globals. `programs/module_slot_scope.gy` is `match` on
  the matrix, reusing one name across the module, a parameter and a loop target on purpose. Three neighbours
  the same probe session measured stay **open** and are pinned with today's answer rather than quietly
  skipped: a module name rebound over a literal text keeps printing the literal (Gap R.273), two nested
  `def`s sharing a name collide on one arena slot and `llc` rejects the module (Gap R.274), and a module name
  defined twice collides in the function table (Gap R.275) — the last two leaving an ordinary program at exit
  **2**, which is ADR 0166's code for a compiler bug.
- **A `lambda`'s parameter list is spelled the way a `def`'s is (Gap R.263, ADR 0352)** — `f = lambda x, y=1:
  x + y` / `print(f(1))` was a *parse error* (`expected ":"`), so the program did not exist and nothing looked
  broken. `def` read its defaults from `parseParam()` and `lambda` had a hand-written loop that stopped at `=`,
  with a comment explaining that sharing the routine would read the body separator as a type annotation — true,
  and no reason at all to refuse an `=`: `:` is no operator, so a default's expression stops at the separator by
  itself, even when the default contains one (`d={"a": 1}`, `s="a:b"`). One parser arm, **no codegen change** —
  ADR 0322's rule that a callable's options live at the call, the hook specialisation and the `to N` arity check
  already answered for a `def`. The probe session that built the tables found seven shapes the reference answers
  and this build does not, and filed each instead of absorbing it: the parameter that reads `0` because a
  non-scalar default was never bound (**Gap R.277** — a wrong number at the exit code of success, `def` included),
  the `lambda` that cannot read the function it is written in while the nested `def` beside it can (**R.276** —
  answered since **ADR 0370**: a `lambda`'s capture list is the names its body is free in, and each crosses as a
  parameter the application fills), a
  hook lambda bound to a name and called as an `int` (**R.281** — answered since **ADR 0369**: the `key=` argument
  is retargeted to the declaration the pass minted for the lambda, so `sorted([3, 1, 2], key=neg)` prints
  `[3, 2, 1]`), `*args`/`**kwargs` (**R.278**), a trailing comma
  (**R.279**), the arity door a `lambda` reaches that a `def` does not (**R.280**), and a container default that
  stops the pass for an unrelated higher-order call (**R.282**).
- **A dict key is asked, not spelled (Gap R.221, ADR 0353)** — `d = {None: 3.5}` / `print(d[None])` printed
  `3`, `d = {None: "hi"}` printed `0`, `d = {None: [2]}` printed `3` again, and `d = {False: True}` printed
  `1`: four programs, four **exit 0**s, and not one number in them that the program wrote. `3` is a
  `@float_box` handle, `0` is a text's index in `@str_tab`, the other `3` is the allocator's slot — each the
  **payload** of an entry whose **kind** stayed in the slot beside it. The door that reads a dict slot's tag
  (`rt_dict_get_tagged` + `rt_dict_value_tag`, ADR 0327) was already built and already correct, and the bound
  form of the same read already answered, which is what named the defect: the gate in front of the door.
  `mixedDictIndexRead` kept a `switch` of key *spellings* — name, int, text, float — and a `None`, `True` or
  `False` fell out of it onto the one-word road. The list became one function, `dictKeyCarriesPair`
  (`pkg/lang/dict_key_pair.go`), asked by the four doors that read an entry rather than by each of them, and
  **nothing in the runtime, the builder or the printers moved**. 36 of a 301-shape probe changed answer and
  nothing else did. What the probe session found beside the fix stayed as rows, each pinned with today's answer
  in a filed table: the operator roads that label it disagree with the pair road in both directions
  (**R.285**), and a name whose last binding was `None` is a key that arrives untagged (**R.286**). The truth of
  the entry used to sit in that list too, and is paid: `if`/`while`/ternary and `bool()` over a self-describing
  entry now ask it the same `(payload, tag)` pair print asks and decide it in `@rt_pair_truth`, and the item of
  an unrolled comprehension asks the same table, so `[bool(x) for x in [0.0, [0]]]` is `[False, False]`
  (**R.284**, **R.359**, **ADR 0388**); the set entry that table is never asked about is **R.360**. The signless
  and fold calls that used to sit in that list have since been paid, and are told below
  (**R.287**, **ADR 0387**): `abs`, `min`, `max` and `sum` now ask the entry's tag beside its payload. So does a
  **call argument**: `def id(v): return v` with `print(id(d["a"]))` prints `x` where it printed the text's
  `@str_tab` index, and a float entry, `None`, a verdict or a container entry arrives with its kind beside its
  payload too — the emission asks the one subscript door print, `bool` and the folds already ask, so the read
  written in place and the read bound to a container variable cannot answer differently, and an entry the
  literal already describes as an integer keeps the one word it always had (**R.220**, **ADR 0389**). That
  boundary now reaches the callee's OWN condition too — `def f(v):` / `    if v:` asks the tag the call site
  wrote, not the payload (**R.361**, **ADR 0393**, told below). What it still does not reach is filed rather
  than smoothed over: a ternary arm or an `and`/`or` operand that IS the parameter (**R.367**, beside
  **R.146**, **R.147**), a body that rebinds its own parameter (**R.366**), a body emitted under the
  text-returning convention (**R.368**), and an entry bound to a name first, which is still the one-word
  position **R.146** owns. The f-string field that was in that
  list has since been paid, and is told below (**R.283**, ADR 0356). The tempting `NoneLit` arm on the *static*
  key door was built, measured, and
  reverted: it turns one refusal into an answer and four correct answers into wrong ones, and the measurement is
  left in the comment above `constKeysMatch` for whoever writes that arm next.
- **A condition asks a parameter the kind its caller supplied (Gap R.361, Gap R.365, ADR 0393)** — the
  argument crossed as a pair and the body went back to guessing. `def f(v):` / `    if v:` / `        return 1`
  / `    return 0` with `d = {"e": [], "t": "", "l": [1], "n": None}` /
  `print(f(d["e"]), f(d["t"]), f(d["l"]), f(d["n"]))` printed `1 1 1 0` where CPython prints `0 0 1 0`: the
  entry's tag had crossed with it, `bindPairParams` had stored both words, and `emitCond` still took the plain
  local slot and tested it against zero — an empty list's payload is a heap handle and an empty text's is an
  interned index, both non-zero, so both are true at the exit code of success. The same program written with
  `elif`, with `while`, with `print(not v)`, with `print(bool(v))` and with `"y" if v else "n"` was wrong the
  same five ways, and `print(not boom())` printed `boom` **twice** because the operand was lowered for a value
  `not` throws away and then lowered again by the road that decided (**R.365**). One helper now answers all
  six positions: it loads the parameter's two words through the same door every pair-bound read uses — the one
  place ADR 0228's written-flag is tested — and hands them to `@rt_pair_truth`, the table ADR 0275 built. Two
  gates keep it honest, and both are what make it a measurement rather than a widening: the **origin** (only a
  name the argument road bound; a name the arithmetic door keeps ADR 0268's lift for, whose tag can only ever
  name int, float or bool) and the **body** (the tag word has one writer, the entry store, so a body that can
  rebind its parameter is served the ordinary road instead — three probes went from a wrong answer to a run
  that never ends before that half was in place, and one spelling still does not terminate, which
  **R.366** owns). Six positions × fifteen entry kinds is 91 programs on the record and compiled-vs-record, 20
  of the family through the CLI on both paths with the reference asked inside the case, the callee's own IR
  asserted to carry `call i32 @rt_pair_truth` beside a load of `%_v_tag` and, in three negative controls,
  asserted NOT to; `programs/pair_param_truth.gy` is line-for-line CPython. The residue is four rows with
  today's bytes pinned beside the reference's: the stale tag under a rebinding and the loop that never ends
  (**R.366**), the operand `and`/`or` and the ternary choose, which still answers a heap handle for `z`
  (**R.367**), the text-returning convention whose parameters never get a tag to ask (**R.368**), and the empty
  container spelled as a `list()` stored in a dict literal — one row that was filed against the store and
  turned out to live in the scan, told below and paid by **ADR 0394** (**R.369**).

- **An entry lambda is minted with the words it reads; the clone supplies them (Gap R.336, ADR 0395)** —
  three programs, one question, and the answer was one door away that the pass had already built for the
  other side of the same construct. `def apply1(fn, v): return fn(v)` with `def use(seed):` / `base = 1` /
  `return apply1(lambda x: x + base, seed)` is CPython's `4` and this backend's exit 1, while the twin with
  the read on the other lambda — `apply1(lambda x: x + seed, seed)`, a parameter of the same frame — had
  answered since **ADR 0370**. The distinction the refusal drew ("an entry is a bare symbol with no
  environment to carry") was true and beside the point: an entry is not called from far away, it is called
  by the CLONE the pass writes, in a body the pass writes, and the pass already had the host frame in hand
  — `entryHost` is the table that decides whose locals an entry's reads resolve through, and ADR 0370 reads
  that very table to fill an applied lambda's captures. It had simply never asked it the second question.
  It asks it now: `resolveEntry` consults the same door the applied road consults
  (`callableLambdaCaptures`), the hoisted `define` gains a capture parameter per name the entry reads, the
  clone grows the UNION of its entries' words as trailing parameters, the application fills them from the
  HOST's own reading — `%_base.ld1`, a local read, not an address and not a copy — and the clone hands each
  word to the entry **positionally**. Positionally and with no `Captures` field on the entry, which is the
  safety story rather than a detail: `Captures` means "the caller reaches for this name", and the gate that
  asks whether a name carries one word (`captureCarriesOneWord`, **Gap R.332**) has to be asked where the
  value lives. ADR 0345's rule holds untouched — the entry is still a named `define`, and the module has no
  `fnptr`, no indirect `call`, no `ptrtoint`. Over 21 probes of the family, asked of CPython first: **4
  agreeing before, 14 after, 10 newly, 0 regressed** — the entry reading a local (`4`), a parameter (`12`),
  the loop target of a `for` whose word changes under one minted symbol (`36`), two entries of one callee
  reading two names (`35`), a capturing entry beside a closed one (`8`), a callee that calls its parameter
  twice (`11`), and a module-level name, which mints no word at all (`10`). `programs/entry_lambda_capture.gy`
  is the matrix row (the 225th, oracle `match`), seven lines that are byte-for-byte what CPython prints, and
  `--callables` counts the forwarding in the field it already had: `capture_parameters` 1 for the row
  program, 2 where the body reads a frame local too. Two shapes still decline, and both say which name is
  in the way: the forwarded word's name is already owned by the higher-order body, which would leave the
  clone with two slots under one spelling (**R.375**), and the `key=` hook, which hoists its lambda by a
  second road that never asks this question (**R.376**). The `sorted(..., key=…)` answer that prints `0` once
  a function RETURNS it is neither — measured identical on the parent, and **R.328**'s.

- **An applied `lambda` answers a container, because it finally has a declaration (Gap R.382, Gap R.331, ADR 0401)** —
  `print((lambda: [1, 2])())` is the reference's `[1, 2]` and here never reached the run time: the hoisted `define` ended
  `ret i32 @.lst1`, a module constant in an integer position, and `llc-20` refused the module at **exit 2**, the code
  **ADR 0166** reserves for a compiler bug. Put a comprehension in the same body and it DID build, and printed the
  allocator's slot `0` at **exit 0** (**Gap R.331**). One defect, two witnesses: a `def` that hands back a container is
  answered by **ADR 0333**'s door — a whole-program scan registers the kind, the `ret` builds the object, eleven positions
  ask the door that knows it — and an applied `lambda` had no declaration for that scan to read, because the emitter minted
  one mid-statement, after every table had been filled. The fix is not a twelfth door: `pkg/lang/lambda_answer.go`'s
  `hoistAppliedLambdaAnswers` gives the application a module-level `FuncDef` and points the call at it, so the SAME scan
  registers the answer, the same `ret` builds it, and the same eleven doors read it — the refusals included, in the same
  sentence naming `<lambda>`. The gate asks the scans their own question (described or proved-and-undescribed, the latter
  being what turns a printed handle into a refusal for **Gap R.261**) plus `lambdaFreeNames`' frame question, so a body that
  reads `x` stays on the capture road; names are minted once and the rounds only ever remove a hoist, so a program that does
  not settle is handed back with the emitter's numbering unmoved (**ADR 0273**), and `--emit-ast` still sees the `lambda` the
  human typed (**ADR 0320**). Asked of CPython first, 38 programs, parent build beside this one: **29 newly answering, 2
  moved from a miscompile to a refusal in words, 7 unchanged** — `print`, `str`, `repr`, `len`, `in`, `for`, a binding, a
  condition, a slice, a subscript, `==`, `sorted`, a comprehension over the answer, an f-string field, a `match` arm, a
  `while` body, a `finally` body and a method body all answer the bytes the reference prints, and
  `print((lambda: [1, 2])()[9])` raises `IndexError: list index out of range` where before it was refused and
  `print((lambda: {"a": 1})()["b"])` raises `KeyError: 'b'`, both catchable by the program's own `except`. The pass speaks
  for itself — `gustyc --lambda-answers <src>` prints `rewritten`, `settled`, `declarations[]` (symbol, answer kind,
  params, defaults, line), `declined[]` over the closed pair `captures_frame` / `answers_nothing_provable`, and
  `names_consumed`, described by three new `--schema` definitions, with a test that fails if the account ever names a symbol
  the module does not define. Sixteen answered programs are on the record leg as well as the reference leg, and the record
  went 8568 → **8621** sources. Four rows came out from under the row and are pinned beside the reference's answer rather than
  reworded away: **Gap R.383** (the whole-program reader that decides what a `lambda` is cannot see a `match` arm, an `except`
  body or an f-string field — the clone road's refusal, where the same statement beside the arm compiles), **Gap R.384** (an
  answer that is a text, a verdict or `None` still prints its slot), **Gap R.385** (`print([1, 2] if 1 else [9])` emits
  `i32 @.lst1` into a `printf` slot, no `lambda` involved), **Gap R.386** (an applied `lambda` that reads a name and answers a
  container is refused by `list literal elements must be integers, not ` — a sentence naming nothing).

- **An applied `lambda` that SPELLS its answer answers with that kind, not with its slot (Gap R.384, ADR 0402)** —
  `print((lambda: True)())` printed `1`, `print((lambda: None)())` printed `0`, and `print((lambda: "hi")())` printed
  `0` on the CLI while the leg that verifies never reached the run time at all, because the hoist's `return` interned
  the text on a road that only asks for the heap-text runtime when something in the original statement asks for it.
  A `def` whose body is `return True` has printed `True` since **ADR 0257** and one whose body is `return None` since
  **ADR 0368**, because the print door never decides a call's kind from its payload — it asks `textReturningCallables`,
  `IsBoolExpr`'s call arm and `fdReturnsValue`, and all three read a module-level **declaration**. The fix is the same
  shape as **ADR 0401**'s and reuses it: the pass's answer half gained `appliedLambdaBodyRegisters`, a second question
  beside the container scan's that reads the body's own spelling (`*StrLit`, `*BoolLit`, `*NoneLit`), and a body that
  spells one of the three becomes a declaration — then walks the `def` roads it always should have, where the
  registration, the `ret` and the printer are decisions someone else already made. Nothing new was written: `rt_print_str`,
  `rt_print_bool` and `rt_print_mixed_value` (with the `answerKindPlan` tag beside it) are the printers a `def` uses,
  and the interning moved onto the road that requests the runtime, so both legs compile the same module. Asked of CPython
  first over the 44-program family — each body in a binding, a print, `str`/`repr`, an equality, a concatenation, a
  multiplication, a method receiver, `len`, a list element, a dict key and value, a set member, a call argument, a
  condition, a comprehension, a `def`, a method, a `match` arm, a `try` body, a `finally` body, an f-string field, a
  parameter and a default: **43 newly answered, 1 divergence** kept as evidence rather than reworded — the three carriers
  whose answer is a CALL or an interpolation (**Gap R.387**, including the f-string arm this cycle deliberately did NOT
  take, because `scanFreeIn` cannot see a name inside an interpolation's field and hoisting `f"{x}"` would read the
  MODULE's `x`) and the `is None` verdict over an explicit `return None` (**Gap R.388**, which the `def` twin answers
  wrongly too). Number and float bodies stay on the one-word road ON PURPOSE — `print((lambda: 5)())` is `5` and
  `print((lambda: 2.5)())` is `2.5` today, and a hoist there buys nothing while risking the pair road and the
  double-domain `define`. The record leg gained the 47 programs of that family the retired engine never answered (**8621 →
  8668** sources), `gustyc --lambda-answers` reports the new kinds (`text`/`bool`/`none`) through the same
  schema-declared document, and `integration/programs/lambda_answer_kind.gy` is the line-for-line reference witness.

- **The whole-program reader reads the three places the AST hides an expression (Gap R.383, ADR 0403)** — one
  statement, written twice: `print(sorted([3, 1, 2], key=lambda v: v + base))` beside a `match` compiled, and inside
  its arm was refused at exit 1 with the sentence for a callable that has no word to travel in. No language rule
  says so, and no gate was chosen: `callableWalk` followed a struct field only when the field's type could hold a
  node, and three constructs keep their expressions in values that are not nodes — `MatchCase` (an arm's pattern,
  guard and body), `FStringPart` (an interpolation's field) and `ExceptClause` (a handler) — so every question the
  function-value pass asks was answered from a program with those three regions cut out. The reader now DESCENDS a
  non-node carrier (fields, slice elements, interface values) while the enclosing `FuncDef`, the lambda-depth counter
  and the per-path cycle set keep meaning what they did, and descends it WITHOUT visiting it: a carrier has no node
  identity to report to a visitor that switches on node kinds. One memoised question (`callableCarrierCarriesNodes`)
  decides whether a type may be entered at all, so `*Type` reaches `*Type` and stops rather than wandering into the
  annotation grammar. **A reader and a rewrite are two questions**: `match_arm` and `interpolation_field` are taken
  and measured answering — eleven programs moved from the parent commit's exit-1 refusal to CPython's bytes (eight in
  an arm, three in a field), each beside an un-carried twin printing the same bytes — while `except_handler` was
  DECLINED for one cycle, on measurement rather than on hunch (**ADR 0404** took the veto back; see the next bullet):
  with the gate emptied, `try: print([1, 2][5])` /
  `except IndexError: print(apply(lambda x: x * 2, 3))` stops being refused and dies at **exit 3** with its own
  `IndexError` unhandled, because the clone moves the call out of the frame whose trap catch the handler is
  registered on (**ADR 0166** ranks a refusal above that). `gustyc --callables` therefore gained `carriers_declined`
  (since **ADR 0404**, `carriers_read`),
  so a pass that left a call out cannot be mistaken for one that never saw it, and its `refused` sentence names the
  position, the line and the row. The carrier set is closed by reading `ast.go` itself: a fourth carrier becomes
  readable the day it is added and FAILS `TestTheASTCarriesExpressionsInExactlyTheseThreeNonNodeTypes`, because
  whether a clone may leave it is a decision to take awake. Four rows came out of the measurement and are filed, not
  reworded: **Gap R.389** (a call in a handler loses the frame's trap catch — reproduces on the parent commit with
  no `lambda` in the program, which is what makes it **ADR 0259**'s table and not a callable pass's), **Gap R.390**
  (an interpolation's field over a folded container answers the handle, or hands `llc` a global in a `printf` slot),
  **Gap R.391** (a `match` whose subject is a text takes no arm at all) and **Gap R.392** (the applied-`lambda` pass
  still keeps its own second reader of the same AST, which is how this hole came to be fixed in one walker and left
  standing in the other). The record leg gained 27 sources the reference answered and the compiled leg agreed to
  (**8668 → 8695**), and `integration/programs/lambda_carrier.gy` is the line-for-line reference witness.

- **An accepted exception is cleared when the arm accepts it (Gap R.241, Gap R.389, ADR 0404)** —
  `def answer(): return 7` with `try:` / `    z = 1 / 0` / `except ZeroDivisionError:` / `    print(answer())` prints
  `7` in the reference; here it printed NOTHING and died at **exit 3** with the `ZeroDivisionError` the handler existed
  for — on the parent commit too, and on a program with no `lambda` anywhere in it. The arm had run: the flag saying
  "an exception is in flight" was cleared only on the edge OUT of the arm, and every call the arm writes ends with a
  check of that module-wide flag, so the arm's own call took the run to the handler OUTSIDE this one — the function's
  raise-exit printed a traceback and the statement after the `try` never ran. Clearing on the accepting edge, before the
  arm's first statement, is both the reference's semantics and one `store` in `tryStmt`; the edge-out clear stays as the
  belt-and-braces **ADR 0218** paid for. That closes the handler half of last round's carrier story: the position
  **ADR 0403** read and refused to clone out of a handler — a restraint **ADR 0166** endorsed while the fault was
  unexplained — is taken like any other, `callableRewriteDeclinedCarriers` is DELETED rather than left empty, and
  twelve programs came over from the parent commit's answer to CPython's bytes — five from an exit-1 refusal (a
  `sorted(key=lambda …)` hook, two `apply(f, v)` shapes, two `use(lambda v: …)` shapes) and seven from an exit-3
  uncaught trap, five of the twelve carrying no `lambda` at all (`try: print([1, 2][5])` /
  `except IndexError: print(apply(lambda x: x * 2, 3))` is `6`). The pass's account field became `carriers_read` — the
  same count, honestly a reading rather than a permission, taken over distinct call nodes — and the three rows that
  pinned the escape are DELETED, not edited, with the handler programs moved into the answered tables of both suites
  and 14 new sources on the record (**8695 → 8709**).

- **The module's literal container reaches a compiled body (Gap R.35's container half, ADR 0407)** —
  `xs = [1, 2, 3]` / `def n(): return len(xs)` / `print(n())` is `3` in the reference, and here it was the row's own
  refusal at exit 1: **ADR 0227** gave the compiled backend a module global for the `i32` word and **ADR 0376** one
  for the `double`, and a container was left out on purpose — its word is a heap handle the collector also has to be
  told about, and every door that reads one asks a name-keyed notebook that the *binding* road writes and no read can
  invent. It closes on a **binding, not a global**: the body's own entry prologue allocates `%_xs`, roots it with
  `@rt_root_put`, builds the literal with `@rt_alloc` / `@rt_set_elem` / `@rt_dict_put` and stores the handle — the
  same three instructions `xs = [1, 2, 3]` emits on the first line of a function — so the print, `len`, position,
  membership, loop, equality, ordering, `str`, interpolation and slot doors answer the module's container through the
  code that has been right about a local one all along, in a `def` and in a method alike. Grepping `@gy_mod_` for a
  container finds nothing, deliberately: one name with two owners is the bug, and a shared handle would answer the
  identity questions wrongly instead of refusing them. The gate is the feature — a name reaches this road only when
  its ONE binding is an assignment of a self-describing literal container and nothing anywhere appends to it,
  assigns a slot of it, augments it, writes through an alias, hands it out of a `return`, or asks which object two
  names hold; those are **Gap R.398**, pinned at exit 1 with the reference's bytes beside each. A second door was
  built first and deleted: a per-node interception that answered reads from the container-answer table, which reached
  49 of 80 probes where the binding reached 111 of 178 — an interception answers the positions it recognises and
  nothing else, and every position it declines falls to a road that refuses. It also refused to inherit a wrongness it
  found on the road it was reusing: `def f(): xs = ["a", "b"]; return xs[1]` prints **`1`**, the slot's index, because
  a constant-index fold names a word and that word carries no tag (**Gap R.324** · **Gap R.219** · **Gap R.331**, byte-
  identical on the parent build) — so a body whose `return` reaches a slot the emitter cannot name is not handed the
  container at all. `tools/recmerge` wrote **60 sources** from CPython (**8791 → 8851**) with no compiled-leg refusal,
  one compiled-vs-record drift row went paid (`def n() -> int: return len(xs)`, ledger 319 → 318), and the corpus gained
  `integration/programs/module_container.gy`, asserted line-for-line (matrix **232 → 233**, parity-asserted **203 → 204**).
  Named rather than absorbed: the concatenation frame that still reaches the frame-slot audit at **exit 2**
  (**Gap R.399**), a slot whose payload the roads cannot name (**Gap R.400**), the nested `def` and `lambda` bodies that
  take no prologue (**Gap R.401**), and the doors that ask the module's records rather than the frame's
  (**Gap R.402**).

- **The truth of a text is its length, not its table entry (Gap R.407, ADR 0409)** — `def f():` / `    return "abc"` /
  `if f():` / `    print("t")` runs its body in the reference and printed **`f`** here at **exit 0**, and one wrong question
  answered the whole family beside it: `while f(i):` never entered its body, `not f()` said `True`, `"y" if f() else "n"`
  chose `n`, a class method's `return "abc"` tested false, and `s = f()` / `if s:` took the else arm. The condition road's
  fallback tests its operand against zero — and a text's value is an **index into the interned string table**, so that test
  asks **which table entry the intern table handed out**, not whether the text has any characters. The first text a program
  interns is entry 0, which is why every one of these programs looks like a toy and fails. The fix is the fact the print
  door, the operation door and the sequence door each had to learn in their own cycle (**ADR 0232**): the condition road now
  asks the one text question and measures with `@rt_str_len` — the arm `@rt_pair_truth` already takes for the text tag, and
  the door the unrolled **loop item** has asked for two cycles (**ADR 0386**/**0393**) — so there is **no new runtime entry
  point**, and what the fold can read still folds (asserted from the emitted module in both directions: the shapes that must
  emit `@rt_str_len` do, the ones that must not, don't). The roads that own other objects keep their roads — a container
  still calls its own length helper, a float still `fcmp`s, a mixed-kind body still reads the tag **ADR 0368** gave it, a
  pair-supplied parameter still reads the caller's word (**ADR 0393**) — pinned beside every answered program because the
  danger of a truth door learning one more shape is that it starts answering an object that already had a better answer.
  Measurement over the 28 programs the two suites carry, parent build `b552b8d` beside this one, each asked of the pinned
  CPython: **16 agreed before, 28 after — 12 newly answering, none regressed**; `tools/recmerge` wrote all **28** from
  CPython (**8884 → 8912**) and one compiled-vs-record row **arrived** (`print(bool([1]))`, ledger **18 → 19**), which is the
  ratchet's other face. Two neighbours stay refused with codes attached for the first time — a parameter one call fills with a
  text and another with a number (**Gap R.409**, beside the pair road's **Gap R.324**) and a container's truth spelled as a
  *call* (**Gap R.131**) — because the sentences reached agents with no code at all (**Gap R.408**). And the harness stopped
  writing artifacts nobody asked it to write: the parallelism guard shelled out to a `-run`-filtered `go test ./pkg/lang/`
  **inheriting** `GUSTY_GOLDEN_UPDATE`, and that subset rewrote the other package's drift ledger with 1 of its 318 rows —
  twice in one cycle, until **Gap R.410** stripped the artifact-writing variables from every subprocess the guard starts and
  a case asserted the scrubbing holds.

- **`str.join` builds its seam the way `+` does (Gap R.403, ADR 0408)** — `n = 3` /
  `print("-".join([f"a{n}", "z"]))` is `a3-z` in the reference, and here it was `join() list elements must be
  constant strings` at exit 1: the method was a **fold** over `*StringLit`s, so a list with one interpolation in it
  fell out of the road and the sentence blamed the program for a limitation the backend did not have. `@rt_str_cat`
  has built a text while the program ran since **ADR 0229** — it is the door `s + t` walks, interning into the same
  `@str_tab` — and a join is nothing but that concatenation with the separator between the neighbours. So `join` now
  asks concatenation's own question of each slot (`strReg`) and emits one `@rt_str_cat` per neighbour, left-leaning,
  the separator interned once, **no new runtime entry point**: `TestTheJoinSeamIsBuiltByTheDoorTheConcatenationRoadAlreadyUses`
  reads the module to pin that, and pins the other half too — the all-constant list still folds and still emits no
  concatenation at all. An element the compiler can see is **not** a text is a **raise** and not a refusal: exit **3**
  with the reference's catchable `TypeError: sequence item 1: expected str instance, int found`, index counted,
  `bool` spelled `bool` and `None` spelled `NoneType`; an element whose kind it cannot read is refused in words,
  because interning somebody's `repr` where the reference raises is a wrong answer at the exit code of success.
  What the refusal hid was three wrong answers behind it, all of them pre-scan verdicts rather than codegen ones:
  `def f(): return "-".join([...])` printed **`6`** — the `@str_tab` **index** of `a3-z`, at exit 0 — the same body
  with a frame-local `s` printed `6` and `10`, and a join arm beside a number arm printed `a-b` and `(null)`. Text
  has to be one question across the compiler, so `join` joined the shape predicates the pre-passes ask
  (`isStringExpr`, `isStrUnder`) beside the formatting door and the repeat door (**ADR 0360**, **ADR 0365**), and
  `scanStringBindings` now walks a `def`'s body *before* judging what it hands back — a caller holds one word and a
  name, and never sees the body (**Gap R.219**'s lesson, **ADR 0368**'s gate). Family measurement over the 36
  programs these two suites carry, parent build beside this one, each asked of the pinned CPython: **3 agreed
  before, 31 after — 28 newly answering, none regressed.** `tools/recmerge` wrote **33 sources** from CPython
  (**8851 → 8884**) and REFUSED five, which is how the three rows beside this one exist: the list the program built
  (**Gap R.404**), the separator the program built (**Gap R.405**), and the body that hands back a NAME the pre-scan
  holds as text beside an arm of another kind (**Gap R.406** — pinned at `(null)`, in both suites, because the same
  body spelled `s = "ab".upper()` prints `AB` / `(null)` on the parent build too, which is what files it against the
  census and not against `join`).

- **An `except … as name:` arm binds what the exception writes (Gap R.113, ADR 0406)** —
  `try: 1/0` / `except ZeroDivisionError as err:` / `print(err)` is `division by zero` in the reference, and here it
  was a **parse error** at exit 1 on every path: `parseTry` read the type name and then asked for a colon, so the
  single most common exception idiom in the language had no grammar, no AST field, no IR and no doc. The clause now
  parses and the arm binds — to what the reference's exception **writes**, since this backend's value is one word
  wide and there is no object to hand over: `setExn` stores the bare message in `@exn_arg` beside the
  `"<Type>: <message>"` report `@exn_msg` already carried (a message containing `": "` keeps its colon), and the arm
  interns it through `@rt_str_intern2`, the door every literal, `str()` and formatted text already uses. So
  `print(err)`, `str(err)`, `f"{err}"`, `return err` and the print of that return are one record read five ways. The
  record it writes is a new one — `caughtVars`, deliberately **not** `internedVars` — because the reference splits
  the name in two: an exception prints as its message and is not a string to `len`, to `.upper()` or to `==`, and
  those three doors refuse in words (each registered as its own capability code) instead of answering the message
  dressed as the object. `repr(err)`, `err.args`, `type(err)`, the name's life after the arm and the KeyError the
  reference quotes are pinned as **Gap R.395**; asking the arm that question found `type` is a reserved word
  (**Gap R.396**) and `except (A, B):` does not parse (**Gap R.397**). 38 sources recorded from CPython
  (**8753 → 8791**), corpus `integration/programs/except_as_name.gy` asserted line for line.

- **A container in a ternary arm is built where the branch enters (Gap R.385, Gap R.128, ADR 0405)** —
  `print([1, 2] if 1 else [9])` is `[1, 2]` in the reference, and here `llc-20` refused the module on
  `%t1 = call i32 (i8*, ...) @printf(i8* getelementptr…, i32 @.lst1)` at **exit 2**, the code that says the compiler
  is broken rather than that the program is wrong. The backend had two roads for a container — the module-level one
  that names a literal with a global, and the call-argument one that hands a function a word — and neither had the
  **arm** as a shape it could name, so the arm fell to the number road, whose `value()` renders a container literal as
  the address of its global. Two of the family were quieter than the crash and worse for it: the dict pair and the
  tuple pair printed `0`, the heap handle as the number the value is not, at the code of success. An arm is a program
  the test runs, not an operand of the instruction that picks (**ADR 0374**), so each arm's container is now built
  INSIDE the block the branch enters and the merge phis the two handles; `isContainerExpr` and `staticContainerKind`
  ask the pair through one new pair of functions, so the road that builds and the door that prints cannot disagree about
  which programs are containers (**ADR 0232**), and the print door chooses `rt_print_list` / `rt_set_print` /
  `rt_dict_print` / the tuple's `rt_print_container_value` from the kind the arms AGREE on. Comprehension arms came with
  it — `print([v for v in [1, "a", True]] if 1 else [9])` printed `0` and prints `[1, 'a', True]` — because an arm is
  allowed to be a whole loop. A pair of arms that agrees on no kind is refused in words at exit 1 rather than emitted
  (**ADR 0166**), which moved four programs off the assembler's floor. Measured with a build of the parent commit beside
  this one, over the same 39 files: **25 programs moved to CPython's bytes and none left answering wrongly at exit 0** —
  12 off exit 2, 5 off a silent `0`, 3 off a refusal in words, plus the 4 refusals — and **Gap R.128**, the same defect
  read at the other door eight weeks ago, paid with it: `programs/probe_ternary_container_arms.gy` prints its header's
  two lines, its `Missing` pin left the ledger and the program became an asserted parity row. The sweep's two finds were
  filed rather than fixed sideways — **Gap R.393** (the doors that ASK a container ternary a question: `str`, a loop's
  iterable, a `def`'s `return`, a `%` format, an interpolation, `len`, a position) and **Gap R.394** (the arm pair that
  needs the tag printer) — each pinned with today's bytes beside the reference's on both legs, and 44 sources went onto
  the record (**8709 → 8753**).

- **A refusal about a target names the node the PROGRAM wrote, not the node the parser built (Gap R.379, ADR
  0400)** — ADR 0398 cured the leak at the statement door and its own filing named three more, which sat untouched
  because none of them goes through `noteUnlowered`: `[1, 2][0] = 3` printed `… got *lang.ListLit; CPython accepts
  more forms`, `(1).x = 2` printed `unsupported assignment target *lang.Attr`, and `3 += x` printed the parser's
  `(got *lang.IntLit)`. All three were already the right verdict at the right exit code — the reference agrees these
  programs fail, with a `SyntaxError` and an `AttributeError` for two of them — so the wording was the entire defect,
  and a reader has no AST to look a node type up in (`--json` shows the same string to a machine). One door now writes
  all three, `nodeSurface`, beside `stmtSurface`: it names the node's KIND in the language's words and its TEXT as
  written — ``the program writes into the list literal `[1, 2]``, ``the attribute `x` of a number``, ``the number
  `3```. It shares `exprSurface`'s rendering and refuses its fallback: `exprSurface` answers an unformattable node
  with `a %T`, which is right for the messages ADR 0386 built it for and would have smuggled the type name back, so
  this door says ``an expression the compiler cannot name`` and the arm gets a witness (`oddExpr`). Kind comes from
  the node, so an attribute is named by what it is attached to rather than by the rendering `1.x` nobody typed, and a
  tuple — a `*ListLit` with ADR 0361's flag, decided by the flag, with the parentheses re-rendered because the
  formatter prints the shared `[…]` shape. The row's most instructive decision is what it did NOT touch:
  `unsupported attr expression`, one door along and one line away, is quoted verbatim by four rows of the drift
  ledger plus two tests, and ADR 0398's rule is that a drift row whose wording moves is a divergence silently
  disappearing — four at once, in a commit about three other sentences. It stays, under **Gap R.272**, whose
  definition of done already asks for the better wording and which must move those ledger rows with it; a test pins
  today's string and says so in its failure text. Measured with a build of the parent beside this one, over the shapes the tests themselves declare (the probe list is
  extracted from the Go tables, so a count cannot drift from what is asserted): 23 programs — **16 door refusals, 16
  carrying a Go type name on the parent and 0 doing so here, 0 naming a roadmap row before and all 16 doing so now, all
  16 at exit 1 on both builds, the 5 working target forms printing the reference's bytes on both, exit 2 nowhere, and 0
  programs where the compiled leg prints what CPython does not.** The family's own table also caught the door disagreeing
  with itself — one tuple path said ``the tuple `(1, 2)``, the other ``the tuple literal `(1, 2)` `` — which is the drift
  a shared door exists to turn into a test failure. And the family found something the roadmap had never recorded: of the
  16 refusals, 11 are ones CPython agrees the program fails, and **5 are programs the reference answers** — `xs[0] += 5`
  → `6`, and four item stores through a container that is not a variable (`f()[0] = 3`, `[1, 2][0] = 3`, `d["x"][0] = 1`,
  `c.a[0] = 3`) → `1`, `1`, `1`, `[3]`. Those are not wording bugs, so the row stopped at the sentence and filed them:
  **Gap R.380** (the parser's `op=` rule refuses a subscript target in front of a lowering `genItemStoreFromExpr` has had
  since Gap R.98) and **Gap R.381** (`assignIndex`'s `*Name` rule, the mirror image of what ADR 0334 closed for reads) —
  both cited from inside the new sentences, both pinned against live CPython in both legs so that closing one is a
  failing build and not a re-discovery. `tools/recmerge` wrote **5** entries — one per control, each
  recorded from CPython — taking the record to its own 8568, and filed none of the 16 refusals, which the tests assert are
  **absent**: ADR 0302's recorder will not enter a shape the compiled leg will not answer. One more divergence surfaced on the control leg and had no pin anywhere (`def bump(c): c.v += 3`
  is the reference's `3`, ours is the attr door); it is pinned now, beside the ledger case.

- **A hook that CALLS a function the module declares gets that call by SYMBOL (Gap R.378, ADR 0399)** — three
  roads hoist a `lambda` in this compiler, and only the hook road had grown a second opinion: after the shared
  capture door (`callableLambdaCaptures`, ADR 0370) had answered what a body reads out of its frame,
  `callableCapturesAreValueNames` looked the name up in the pass's own census of `def`s and `class`es and
  refused the whole hoist. So `def helper(v): return 0 - v` above `print(sorted([3, 1, 2], key=lambda x:
  helper(x)))` — two lines, the reference's `[3, 2, 1]` — sat at exit 1 behind a sentence about words,
  while `(lambda x: helper(x) + 5)(3)` had been printing `2` for cycles because the applied and entry roads
  never asked that second question. ADR 0345 §1 says no WORD can carry a function; the gate had concluded that
  no hoisted BODY may name one, and a hoisted hook is a `define` like every other, calling `helper` the way the
  rest of the module does. The gate is deleted. The door now asks three questions about the PROGRAM: does the
  module's own statement list DECLARE the name (so its definition dominates the hoisted `define`); does the body
  read it OUTSIDE a call position — `callableFreeNameSet(l, callableCalleeNameNodes(l))` marks out the callee
  `*Name` NODES, not the spellings, because `key=lambda x: helper(x) + len(str(helper))` spells one name both
  ways and only the reading wants a value (**Gap R.297**'s half, still refused); and does the frame the call is
  written in OWN the name (`callableFrameOwns`, lifted out of this door's own body so the question is asked once)
  — because `def use(): helper = 3` owns `helper`, and the reference's `TypeError: 'int' object is not callable`
  is the answer that shape has, never a fabricated `[3, 2, 1]`. Two rules stayed conservative for exactly that
  reason: the `def` must sit above the hook, and a module that rebinds the name anywhere — before the call, after
  it, inside a module-level `if` or `for` — keeps the shape a capture, because a hoisted `define` reaches a symbol
  and can never see a slot. Measured with a build of the parent beside this one over **55 programs: 13 agreeing
  with CPython before, 36 after, 23 newly, 0 regressed, and 0 programs where this backend prints what the
  reference does not** — 16 of those programs written specifically to make the new gate fabricate an answer, and
  11 of them still refusals, three with the reference answering a value (`helper = 3` after the call, an alias,
  a hand-off). `--callables` says the distinction out loud now: `capture_parameters` is `0` for a hook that only
  calls and `1` for one that calls and reads, both `rewritten: true` where the parent reported a refusal. The IR
  is read for the claim a table of numbers cannot make: the hoisted hook is a one-parameter `define`, the callee
  is `call i32 @gy_helper(i32 %p0)`, and no `fnptr`/`ptrtoint`/`inttoptr`/indirect `call` appears anywhere in the
  family. `programs/hook_named_call.gy` is the matrix row (the 242nd, oracle `match`), 29 record entries came from
  `tools/recmerge`, and three emitter sentences the row exposes rather than causes are **Gap R.379**'s, pinned
  with the reference's answer beside each.

- **The ordering door's hook lambda reads the frame its call is written in (Gap R.376, ADR 0396)** — the same
  door, one cycle later, at the third road that hoists a `lambda`. `def use():` / `base = 1` /
  `print(sorted([3, 1, 2], key=lambda x: 0 - x + base))` is CPython's `[3, 2, 1]` and this backend's exit 1
  with the key door's own sentence — “a lambda is a function VALUE, and this backend has none” — true about
  the representation and beside the point, because the hook is not used as a value anywhere: the DOOR calls
  it, once per element, by the symbol the pass minted for it. ADR 0345 had hoisted a hook only under the
  condition “the lambda must read nothing but its own parameters”, while the application road (ADR 0370) and
  the entry road (ADR 0395) each asked one shared door — `callableLambdaCaptures` — for what a body reads out
  of the frame it was written in. The hook road had its own spelling test, and that is the pattern this house
  now hunts on sight: three copies of one question produced three answers, and the fourth patient in the
  container family (ADR 0391/0392/0394) is the precedent. The hook now asks the shared door, is minted with
  one capture parameter per name its frame owns, and carries `Captures` — the instruction the existing call
  road already obeys, filling the word by name from the **caller's** own reading, and the caller of a hook is
  the frame the call is written in:
  `call i32 @gy_sorted$lambda$…(i32 3, i32 %_base.ld1)`. That is the opposite of the entry road's choice about
  `Captures`, on purpose: an entry's caller is a clone the pass wrote, whose word was handed down; a hook's
  caller is a frame of the program. A name the module binds mints no word (ADR 0395's rule), a name past the
  frame is declined (ADR 0245's lift), and a declared function name is declined rather than read as
  `%_helper` (**R.378**). One arity census had to learn the difference between an argument and a word: the
  minted hook has two parameters and the door supplies one, so the key door's arity verdict and
  `keyDoorCallsOneArgument` now count `callableSuppliedParams` — counting the minted word raised
  `<lambda>() missing 1 required positional argument: 'base'` for a program that wrote no such argument, and
  told the pass a function value was left behind, which refuses the whole program. Over 26 probes, each asked
  of CPython first and the harness validated before any count was believed: **5 agreeing before, 22 after, 17
  newly, 0 regressed** — a frame local, the frame's parameter, a module constant from inside a `def`, `min`
  and `max` sharing one hook, `reverse=True` beside a capture, the answer bound/measured/indexed and walked,
  two hooks of one frame, a hook in a `while` body, a rebinding between two hooks, two names of one frame, a
  **method's** frame, and the hook whose own parameter shadows the frame's name (which mints no word and
  prints the reference's `[3, 1, 2]`). `programs/key_hook_frame.gy` is the matrix row (the 226th, oracle
  `match`); three refusals moved to the sentence of the row that actually owns the missing half — a text or
  container capture to **R.332**, float elements to **R.248**, a module-rebound name to **R.35** — and the
  rest stay the door's own, each at exit 1 with exit 2 forbidden. Both of this row's test files also carried a
  carve-out that cut the pre-existing `a *lang.ExprStmt statement cannot be compiled:` wrapper off the front of a
  refusal before asserting it leaked nothing — a leak the suite had adapted to rather than the compiler owning.
  That is **Gap R.377**, closed one cycle later by **ADR 0398** (the next entry), and the carve-outs are gone.

- **A statement's refusal is the door's own sentence, at the frame and line the program wrote it (Gap R.377, ADR
  0398)** — the probe that filed this row was the nested-`def` hook above, and what it printed was two sentences
  welded: `gustyc: jit: codegen: a *lang.ExprStmt statement cannot be compiled:` in front of “`key=lambda ...` is
  refused: a lambda is a function VALUE … (roadmap L11.7)”. The half after the colon is what a reader acts on; the
  half before it is the emitter reading its own Go type table at someone who has never seen it — and it was on the
  record leg's sibling too, where a `%T` met an unformattable payload and produced
  `(*lang.ReturnStmt)(return outside function)`. `noteUnlowered` is the helper (the closure road funnels every body
  statement's error through it, so a body that was not lowered cannot also be executed — ADR 0227, Gap R.35), and
  Gap R.38 had already fixed its `*FuncDef` arm to say `function "m"` while the `Stmt` arm stayed `a %T statement`.
  One question now decides the arm: `refusalIsSentence(err)` — the capability registry's own classifier
  (`ClassifyCapabilityRefusal`, the one `--capabilities` and `--json` read, ADR 0346) or ADR 0166's citation, because
  a refusal this backend wrote on purpose carries the row that owns the missing half. A sentence is handed over
  **verbatim** with the only two facts the wrapper knew appended — ``(in `use`, line 4)``, the frame printed by
  `callableDisplayName` so a clone never shows its IR symbol (ADR 0345 §3) — and `codegen: ` is added exactly once,
  which the old arm doubled. A raw helper error still gets a wrapper, and the wrapper's subject comes from
  `stmtSurface`: ``the `return` at line 6``, ``the `with` at line 3``, ``the `print(…)` call at line 7``, the 22
  statement kinds the language can write, and a fallback that says a line and never a type; `codegen_stmt.go`'s last
  `%T` (`unsupported statement %T`) went through the same door. `*FuncDef` is excluded **in code and in a test** — it
  implements `Stmt`, and folding it in would have reworded Gap R.38's sentence, which two drift-ledger rows quote
  verbatim: a divergence silently disappearing is the failure this suite fears most. Measured with a build of the
  parent beside this one over 23 probes: **the same 18 refusals before and after, 6 carrying a Go type name and 0
  doing so now, 6 newly clean, 0 regressed**, the `*FuncDef` sentence byte for byte, and the five answered controls
  re-derived from `python3` inside the case on `--jit` and `--aot`. The two ADR 0396 carve-outs are deleted, not
  edited, so those tests fail if the wrapper returns; `--json` carries the same sentence and the same class
  (`ok:false`/`exit:1`/`phase:"codegen"`, plus a `capability` code whenever the registry knows it), which is the
  point — one fix, both readers. What the same survey found at the *other* doors — `… got *lang.ListLit`,
  `unsupported assignment target *lang.Attr`, the parser's `(got *lang.IntLit)` — is **Gap R.379**, closed two cycles
  later by **ADR 0400** (the next-but-one entry), with all three pinned until then in a table that failed when
  somebody closed them.

- **The word a clone forwards to its entry is MINTED, not spelled (Gap R.375, ADR 0397)** — ADR 0395 landed the
  entry-side capture road and left one shape refused out loud: `def apply1(fn, v):` / `base = 9` / `return fn(v)`,
  applied from a frame that binds `base` itself, is the reference's `4` and this backend's exit 1 with “the word
  the clone would forward is a name the body reads for itself”. Honest, and the whole family behind it — fourteen
  programs the reference answers, all refused because of one spelling. The reason was one string doing three
  jobs in three frames: the name the PROGRAM wrote (what the host reads, and the only name its value tables
  know), the name of the CLONE'S PARAMETER (a word handed down, which nobody's tables describe), and the name of
  the ENTRY's parameter (the lambda's own source text reads it). Two of the three had to keep the program's
  spelling; the middle one was wearing it. So the parameter is minted now — `freshForwardWord`, carrying
  `callableForwardInfix`, checked through the same doors as a clone's symbol (`nameIsFree`, `callableLexable`) plus
  the one a parameter has to answer that a symbol does not: no name the higher-order body owns. `FuncDef` gained
  `CaptureReads`, parallel to `Captures`, so the call road fills the minted parameter with the host's reading of
  the OTHER name (`call … @gy_apply1$callable$…(i32 0, i32 %p0, i32 %_base.ld1)`), the gate still asks the frame
  that OWNS the value, and the verdict is recorded against the parameter that receives it. The filing had said
  “fill it positionally”; that was measured and rejected — an application may leave parameters out (Gap R.329), so
  an appended word lands in a default's position, and the fill is where the one-word gate and the verdict live.
  The pass writes a fill in one place only: the recursion, the single call of a clone whose caller is not a frame
  of the program (`…(i32 %t15, i32 0, i32 %p2)` — ADR 0385's rule, one word taller). **2 agreeing before (both of them the
  controls), 16 after, 14 newly, 0 regressed** over 23 probes — the 20 of the family, the two controls that had to stay
  answering, and one sibling row of another gap, the before-column a build of `HEAD` run beside this one: the body binding the name, owning it as its own parameter, as a
  `for` target, through a comprehension target, two entries reading one owned word, three entries over two words,
  the parameter called twice, a recursion and two hosts of it, an omitted argument, the body reading its own word
  beside the entry reading the host's. The three pins that held it refused are deleted, not rewritten; the minted
  spelling is forbidden from diagnostics and from the module's text; the two declines it does not reach — a name
  two frames out (**ADR 0245**'s lift) and a value that is not one word (**R.332**) — are asserted at exit 1, the
  gate asked of the OWNER, which is why a body holding `9` beside a host holding `"hey"` is refused rather than
  answered with the number. `programs/forwarded_word.gy` is the matrix row (the 227th, oracle `match`).

- **The pair scan asks the same door: the container spelled as a call is a slot it can read (Gap R.369, ADR
  0394)** — three rows in a row were the same program with one word changed, and the filing of the third one
  blamed the wrong road. `d = {"k": []}` / `f(d["k"])` prints `[]` and takes the `else`; `d = {"k": list()}` /
  `f(d["k"])` printed `3` and took the `then`, at the exit code of success, for the print and the truth alike.
  The store was not the hole: `--emit-llvm` shows the entry being written by
  `@rt_dict_put_tagged(i32 %…, i32 4, i32 5)` — the tag of a list, beside the handle, exactly as ADR 0391 left
  it. The hole was `pkg/lang/paircall.go`. The scan decides once per program which `(function, parameter)`
  positions take the pair, and one of its predicates is a hand-written list of AST node types —
  `pairSlotIsSpelling`, "can the builder write this slot from its spelling alone?" — on which a `*Call` does
  not appear. So `readOfLiteralSlots(d["k"])` answered *no*, the position was recorded closed, and the callee
  got the ordinary road's single word. That list is the **third** hand-written copy of one question: ADR 0391
  put the door in the road that LABELS a slot, ADR 0392 in the roads that DECIDE a kind, and this row in the
  road that decides whether the read of a slot crosses a call at all (ADR 0232's disease, third patient).
  `pairScan.valueSlotIsSpelling` is now the value/member half and asks the emitter's own door; the KEY keeps
  the strict list, because a key the tag table cannot label is a key that cannot be hashed and **R.81** owns
  when that raises. The door arrives with one fact the scan cannot borrow — `(*irGen).builtinShadowed` is a
  method on a generator `pairCallScan` has no handle on — so the scan carries `programDefinesName`, a census
  that asks exactly one question and no more: does this program introduce the name with a `def` or a `class`.
  A census wider than that was built, measured and narrowed: counting a `for` variable or another function's
  parameter as taking the name printed `3` for `def g(list):` above `d = {"k": list()}` / `f(d["k"])` while the
  emitter went on labelling the entry a container, and a gate stricter than the door it copies is not safe, it
  is a second door that disagrees with the first. Over 46 probes of the family, asked of CPython first: **7
  agreeing before, 34 after, 27 newly, 0 regressed**. 27 answer rows and `programs/empty_container_argument.gy`
  are on the record and print CPython's bytes on `--jit` and `--aot`; the store is asserted to write the tag of
  the container the CALL spells (`i32 5` / `6` / `7`), the callee to carry `define i32 @gy_f(i32 %p0, i32 %q0)`
  and spend `%_v_tag`, and the caller's `@main` — not the callee — to ask `@rt_dict_get_tagged` /
  `@rt_dict_value_tag`; `d = {"a": 7}` is asserted NOT to grow a tag word at all. What is left is five rows
  with today's bytes beside the reference's, and two of them are neighbours rather than casualties: the truth
  of a call-spelled empty container with no call to cross it (**R.370**), the VALUE of a caller-supplied
  parameter where its truth is now two — `repr`, an f-string field, a container slot, `len`, `==` (**R.371**),
  the read whose KEY a `for` variable holds (**R.372**), a method named `set` taking the built-in's spelling
  from this road (**R.373**), and — measurable on the build before this row, so filed and not absorbed — a
  callee whose pair layout was decided by a different call site, which hands the second call the first call's
  payload word (**R.374**).

- **An empty container written as a call carries the kind the object already has (Gap R.360, ADR 0391)** —
  `xs = [set()]` / `print(xs[0])` printed `1`, `d = {None: set()}` / `print(d[None])` printed `3`,
  `print([set(), list(), dict()])` printed `[1, 2, 3]`, and `print([bool(x) for x in [set()]])` printed
  `[True]`: all four at the exit code that means the program was answered, and every one of them a heap
  handle. The tag table has had a set in it since the first commit, the pair truth table has had a container
  arm since ADR 0275 and the container printer has read slot tags since ADR 0232 — what was missing was the
  line that says what a slot holds when the value in it is spelled `set()`, `list()` or `dict()`. Those are
  `*Call`s, and the predicate that labels a slot reads spellings, so they fell through to "an integer" and
  everything downstream believed the label: `len` of such a slot named `'int'`, which is the tell that the
  road was misinformed rather than confused. The gates that DECIDE a value is a container already asked
  `emptyContainerLiteral`; the roads that LABEL it did not — one question in two places (ADR 0232), exactly
  how ADR 0259's bool got left behind. One door now answers both, and it is the shape plus `builtinShadowed`,
  so `def set(): return 1` keeps the program its own answer. No runtime helper was added and no tag invented;
  the doors the label did not reach were filed as **R.364** and paid by **ADR 0392**, which gave the *deciding*
  pair — `staticContainerKind` and `containerHandleOf`, plus `isContainerExpr` and the `abs`/unary-minus kind
  door — the same arm: `len(set())` is `0`, `list() == []` and `set() == set()` are `True`, `sorted(set())` is
  `[]`, `list() + [1]` is `[1]` (it had been raising a `TypeError` the reference does not make), and
  `abs(list())` raises CPython's `TypeError: bad operand type for abs(): 'list'` at the program's own exit 3
  instead of answering `0` at exit 0. What is still filed stays named: `set() < set()` answers `True` where the
  reference answers `False` — the ordering helper the literal spellings lack too (**R.175**) — `{set()}` prints
  where CPython raises unhashable (**R.81**), and `{set(): 1}` refuses with the sentence `{[1]: 1}` refuses
  with, naming "an empty set" rather than a Go type.
- **A traceback names the calls the program made (Gap K.8, ADR 0354)** — a raise inside two calls
  reported one frame. `def inner(x): return x // 0` / `def outer(x): return inner(x)` / `outer(7)`
  printed the header, `File "prog", line 2, in inner` and the class, and the reader had to guess who
  called `inner`; the reference prints three frames, and a report that blames the innermost line for
  a bug living in the interaction of three statements is not a report. The record has no frames at
  all — a scan of `testdata/interpreter-golden.json` for `File "prog"` returns nothing, it stores a
  trap's class, message and stdout — so this is a reference-leg feature and CPython is its
  definition. What shipped is a **shadow stack the program keeps itself**: `pkg/lang/frame_stack.go`
  carries the runtime (two parallel 64-slot tables, a depth, a snapshot the report prints from) and
  the post-pass that instruments the module, putting a push before every call to a program function
  and its line beside it, and a pop after it. A pass rather than a hunt through the emitter's
  eleven call roads — a `def`, a method, `super()`, a constructor, the dispatch `switch`, a hoisted
  hook lambda, a closure `_impl` — because a feature that depends on remembering all eleven has a
  missing frame in it. The frame keeps the name the *program* wrote, not the symbol the module holds:
  `bump` for a method (the reference's shape; the qualified `Counter.bump` is what the line table
  says, ADR 0231), `inner` for a lifted closure, `<module>` for the top level. A run of identical
  frames is printed three times and counted (`[Previous line repeated 61 more times]`), the way the
  reference does it, and the table is a window on the newest 64 frames — both stated in
  `docs/language.md`. The whole thing is gated on `raiseUsed`, so a program that can never report a
  trap ships the module it always shipped, byte for byte. The agent path came with it rather than
  after it: `--json` grows a `frames` member beside `traceback`, read back off the target's own
  bytes by `ParseTrapFrames` so the prose and the array cannot disagree, shaped by
  `definitions.trapFrames` in `gustyc --schema`. Eight programs — nested calls, a method, an index
  through a helper, a recursion shallow and a recursion deep enough to collapse, an escape after a
  handled exception, a comprehension's helper, the module's own statement — are compared to CPython
  frame by frame, by name **and** line, in `integration/traceback_test.go`.
- **An f-string field is a print in disguise (Gap R.283, R.289, R.292, R.294; ADR 0356)** — one value, two lines,
  two answers: `print(d[1])` wrote `3.5` and `print(f"{d[1]}")` wrote `5`, at **exit 0**, over `d = {1: 3.5}`; the
  text entry wrote `0` (its index into `@str_tab`) and the list entry wrote a heap slot. The pair door that fixes
  this has existed for cycles and `pkg/lang/list_literal_read.go` even named the field as one of the positions
  that asks it — the field just never asked, and no test noticed, because the claim was written where a reader
  would trust it. The five arms `print` walks are now one function (`subscriptPairOf`) that both roads call, so the
  field, `print(x)` and `str(x)` cannot answer a slot differently, and the field renders through the module's ONE
  printer while a foldable field still folds to a constant. Three defects came out of the same road and are each
  pinned: a **padded field was a syntax error** (`f"{ x }"` → `parse error at 1:2: unexpected token`, and with it
  every field that opens with a brace, because the lexer reads a fragment's leading blanks as a block — `lexFieldSrc`
  now trims the ends and keeps the columns); a **format spec over a field the spec engine could not read aligned an
  empty text**, so `x = 5` / `print(f"{x:>6}")` wrote six spaces at exit 0 where the reference writes `     5`; and
  the deepest one, **a read the escape analysis could not see deleted the container it reads** — `deadListAssignments`
  issues a permission, not a report, and a list whose only read sat in an f-string field, a keyword argument, a
  tuple element, a chain comparison, a comprehension's iterable, a `match` guard, a `raise`, a class body, a
  decorator or a parameter default had its allocation taken away, then got refused with a sentence about a
  construct the source did not contain (`show(k=xs)` → `undefined name "xs"`, two lines after assigning it). The walk
  is now driven from the AST's own **field types** by reflection, so a node type cannot be forgotten, and a guard
  reads `ast.go`, censuses the types carrying `exprNode()`, and fails when the read-set table has no row for one. The
  two policy nodes stay enumerated and stated: a `lambda` body **is** a read (a closure is exactly that), a `def`
  body is **not** (a compiled body cannot read module state at all — `Gap R.290` owns the day both move). The record
  leg deserves one honest sentence: of the record's 7008 engine-era sources, exactly **one** interpolated a subscript into a field,
  which is why no record ever caught this; so the field is witnessed by its `print(E)` twin's record plus CPython, and
  a guard fails if anyone mints a second record for it.
- **A comprehension walks what a call answers (Gap R.291; ADR 0357)** — `def g(): return [1, 2, 3]` /
  `print([x for x in g()])` did not refuse and did not answer: it died `panic: runtime error: index out of range [0]
  with length 0`, **exit 2**. The dispatcher asked whether a comprehension's iterable *was a call* and handed it to
  the compile-time unroller, whose only notion of a call is `range` — so it opened `Args[0]` (nothing to read for a
  zero-argument call) and, for any one-argument call, read **the call's own argument as the loop bound**. `def h(n):
  return [n*n]` / `[x for x in h(3)]` printed `[0, 1, 2]` where CPython prints `[9]`, and `return n + 1` printed
  `[0, 1, 2]` where it raises `TypeError`. So the row could not be closed by a nil check: only `range(...)` with at
  least one argument has a compile-time length, every other call goes to the runtime loop, and the loop asks the
  `for` road's three iterable questions before lowering — fold it, materialise a `sorted`/`reversed`, or read a
  proven container answer — and **refuses** what nothing can prove rather than asking `rt_list_len` of a word and
  walking nothing (that is how `[x for x in h(3)]` over `return range(n)` printed `[]` at exit 0 mid-row). Four
  shapes that panicked or refused now answer (`[1, 2, 3]`, `['a']`, `[9]`, `[1, 2, 3]` for `sorted`), the refusal
  quotes the container door's own sentence, and the leftovers are rows rather than surprises: the `for` road still
  reads a call's argument as a bound (**R.299**), a `range` is still not a value anywhere else (**R.293**), the two
  roads keep two copies of the iterable sequence (**R.298**), and a generator handed to a builtin is refused twice
  over (**R.296**). The set and dict spellings walk the same road and answer the reference's bytes — `{x for x in
  [1, 2, 3, 2]}` is `{1, 2, 3}`, the fourth element dropped in the object rather than in the printer — and seven of
  the sources the retired engine answered with a *name* as the iterable are now the route's record leg. One shape is
  filed instead of pinned: a set the program builds prints the order the elements arrived in, so `{3, 1, 2}` comes
  out `{3, 1, 2}` where the reference prints `{1, 2, 3}` — unordered in the language, visible in a file, and owned by
  **R.300** rather than by a test expectation copied from the wrong leg.
- **A `match` capture binds in the arm the branch entered (Gap R.68; ADR 0358)** — `v = 5` / `match v:` /
  `case [y]: pass` / `case 5: pass` / `print(y)` printed `0` and **exited 0**, where CPython raises `NameError: name
  'y' is not defined` (`UnboundLocalError` inside a function, catchable by either class). ADR 0228's written-flag
  machinery was present, correct and untouched: the flag was being set by `matchPattern`, which is asked **which arm
  runs** and is therefore called for the arms that lose too, so the capture's store and its `store i8 1` ran on the
  way to a decision that is not a binding. A capture is now queued where the pattern computes it and stored first
  thing in the block the `br i1 <pattern>` branches to — in front of the guard, because a pattern that matched and
  whose guard then failed *keeps* its binding (measured, both legs). Three causes went together or each kept a silent
  zero: the store sat in the decision; the checker's capture census, the written-flag eligibility rule and the
  emitter disagreed about the class and mapping positions; and a module-level capture was never a local of anything,
  so no flag was attached to it at all. An or-pattern is now decided one alternative at a time and each alternative
  carries only its own captures (`case [a] | {"k": a}:` over `[3]`, `{"k": 8}` prints `3`, `8`, not `0`, `8`), and a
  pattern asks the object's **kind** before it asks the object anything — `case [a]` over a one-entry dict is a match
  that failed, not a one-element list (**R.306**, paid beside the row, because which arm runs *is* this row's
  question). What the row declines to bundle is filed with its measurement rather than fixed quietly: a capture binds
  one word where the assignment road binds a `(payload, tag)` pair, so a captured text prints as a number
  (**R.305**); nested sub-patterns, the parenthesised tuple pattern and the class pattern's keyword and positional
  spellings are refused in words (**R.301**–**R.304**); and two forms this backend accepts where the reference refuses
  the program are pinned as parity failures in the accepting direction (**R.307**). `programs/match_binding_edges.gy`
  is the whole-program witness — both legs print `caught / 4 / 30 / 0 / -1 / 7 / 3 / 8` and then raise
  `UnboundLocalError` from the same line — and `gustyc --lang`'s `patterns:` line now states the rule, because a
  binding rule an agent cannot discover is one it cannot rely on.
- **A clone answers the recursion inside its own body by calling itself (Gap R.267, Gap R.313; ADR 0364)** —
  `def fact(n, f): if n <= 1: return 1` / `return f(n) * fact(n - 1, f)` / `print(fact(4, lambda x: x))` is
  CPython's `24` and exited 1 here with `"f" is called but is not a function this backend can build a call to` —
  the same sentence, eight shapes. ADR 0345 clones a higher-order body once per tuple of functions its call sites
  hand its parameters, and the plan builds that tuple out of *call sites*; a call written inside the body is not a
  call site, so the recursion's callable slot had no entry, the plan declined, the tree came back untouched, and
  codegen met `f(n)` in a body no clone had ever specialised. The fix is one coordinate: a self-call is a **site of
  the clone it lives inside**, recorded by walking the body the same way the clone's own calls are walked, and the
  rewrite points it at the clone's `define` with the callable's slot filled the way an outside site fills it. Two
  decisions the row's prescription did not anticipate had to be made: `fact(n - 1, f)` mentions `f` outside callee
  position, and ADR 0345's rule is that a called parameter which is *read* is not substitutable — so the recursion
  handing a parameter back to **its own position** is credited as not-a-read, while `print(f)` and `xs = [f]` still
  make the function unrewritten and `callableNoFunctionValuesLeft` is what says so afterwards; and once the recursion
  is the pass's, refusing it is the pass's — four shapes (`run(n - 1, other)`, a recursion that omits a parameter, a
  recursion inside a `lambda`, two higher-order bodies recursing into each other) each got their own sentence naming
  the parameter and the line, instead of one codegen sentence standing for all of them. Found on the way, and fixed in
  the same commit because the recursion cannot be written without it: a call of a parameter records **which parameter
  it evaluates** and **which entry of the clone's tuple that parameter is** — two facts, one field, read as the wrong
  one. They agree whenever the callable is written first, which is how every program in the suite was written;
  `def run(v, fn): return fn(v)` / `print(run(21, inc))` is CPython's `22` and read entry 1 of a one-entry tuple
  (**Gap R.313**). The pass's account grew `recursive_sites`, because a clone that recursed through the
  un-specialised entry could not leave that field at a plausible value by accident; and the ban holds — every clone
  reaches its recursion with `call i32 @gy_fact$callable$…(i32 %t9, i32 0)`, a call to a `define`, and no
  `ptrtoint`/`inttoptr` anywhere in the module.
- **A `list.pop()` moves the payload AND its tag (Gap R.411, ADR 0410)** — `xs = [1, 2, "a"]` / `xs.pop(0)` /
  `print(xs)` wrote **`[2, 0]`** where the reference writes `[2, 'a']`, at **exit 0**; `xs = ["a", 1]` /
  `xs.pop(0)` / `if xs[0]:` read `@str_tab[1]` — a pointer the intern table never wrote — and the process died in
  the printer with nothing on stdout (**exit 139**, **ADR 0166**'s artifact-at-fault class); and the same removal
  asked of the printer answered `0` for a text (the position of the first text the program interns), `1` for a
  double (its `@float_box` handle) and the arena slot for a nested list. A slot in this backend is **two words** —
  a payload, and the tag that says what the payload means (**ADR 0232**) — and `@rt_pop` shifted one of them. The
  tag array is not cleared on free, so the tail wore the kind of the slot it landed on and the popped answer wore
  whatever kind the removed slot's *last tenant* carried: the answer depended on the program's history and not on
  its source. The fix is the one the file already taught: `rt_reverse`'s swap (**ADR 0187**) and
  `rt_set_discard_tagged`'s shift both move the tag with the payload, and `pop` was the third positional door that
  had not been told — `@rt_pop(i32 %h, i32 %i, i32* %outt)` moves the tag through the shift and reports the removed
  one, with **no new runtime entry point**, because `@heap_tags` is the array the printer already reads. One road
  (`emitPopRoad`) lowers the removal for the position that discards the answer and the position that prints it, and
  the answer travels as a (payload, tag) pair through the doors those positions ALREADY ask — the print/`str` door,
  the truth road, `==`, the ordering door, the tagged arithmetic door behind `arithWouldRefuse`, a container
  literal's element, a function's parameter — so no position got a road beside a door and `print`, `str`, `if` and
  `==` cannot disagree about one value (**ADR 0258**, **ADR 0232**). Which tag travels is the container's fact: the
  object's own for a list whose slots describe themselves (**ADR 0184**/**ADR 0232**), the notebook's claim for a
  list that still claims one kind, and **DECLINE** for a kind no notebook describes — guessing is how a payload gets
  rendered under a kind the object never gave it, which is the class of bug this row exists to remove
  (**ADR 0166**), and `TestThePopAnswerShapeHasThreeAnswers` asks the gate and the door the same question so they
  cannot disagree. The ordering arm is where a wrong answer became the reference's *raise*: `xs = [1, "a"]` /
  `print(xs.pop(0) > 1)` now emits `TypeError: '>' not supported between instances of 'str' and 'int'` where it had
  printed `False`, because a pair arm that bypasses `taggedArithPair` beats the door's refuse arms (**Gap R.296**).
  The bounds raise is the reference's too, and the two sentences are two: `pop from empty list` for the empty list,
  `pop index out of range` for the rest, both catchable by the program's own `except IndexError:`. Measurement over
  the **63** programs the two suites carry, a build of the parent (`0f798e8`) beside this one, each asked of the
  pinned CPython: **35 moved to the reference's answer, 17 controls held, 11 still differ and each is a row** —
  **Gap R.412** (a popped slot compared with `None`), **Gap R.413** (a popped text in an arithmetic position, three
  shapes), **Gap R.414** (a body whose answer IS the popped slot) beside the refusals **Gap R.35**, **Gap R.174**
  and **L11.3** already own. `tools/recmerge` wrote **79** entries from the reference (**8912 → 8991**) and refused
  the programs the compiled leg has no answer for, and one compiled-vs-record row **arrived** — a dict's `pop` over
  a name, which the record answers and this backend refuses with a code (ledger **19 → 20**), the ratchet's other face.

- **`is None` asked of a slot asks the TAG, not the word `0` (Gap R.412, ADR 0411)** — `xs = [None, 1]` /
  `print(xs.pop(0))` wrote `None`, `print(xs.pop(0) == None)` wrote `True`, and `print(xs.pop(0) is None)` wrote
  **`False`**, at **exit 0**: one value, one question, three readers, two answers. The printer and `==` had been
  given (payload, tag) pairs by **ADR 0410** and reach `@rt_payload_eq`; `is` was never let through that door and
  fell to the None-equality fold, whose own comment says it "decides the result statically, from what the source
  says each side is" — sound for `print(1 is None)`, and a guess for a slot, because the payload a void leaves
  behind is `0` and `0` is also what an `int` slot carries (**ADR 0172**'s collision in the identity road's
  clothes). So `is`/`is not` join `==`/`!=` at `mixedTaggedCompare` for ONE shape — one side statically the void,
  which is a singleton, so identity and equality coincide — and the answer is
  `@rt_payload_eq(payload, tag, 0, TagNone)`: the object's own tag, with the tag a **register** the removal
  produced. Only the void is admitted, and the reason is the row's best finding: `@rt_payload_eq` answers
  containers **by content** (**ADR 0189**), content equality is the wrong answer to `is` for two containers that
  hold the same elements, and routing containers through it would have traded an impossible-to-miss crash for a
  plausible lie — `print([1] is [1])` dies in the assembler on `icmp eq i32 …, @.lst1` at **exit 2** instead, and
  is filed as **Gap R.418** with the assembler's line beside it. The door is hung in **both** roads that ask an
  identity question — the value road and the condition road, where **ADR 0232** already hangs the tagged equality
  — because `print(x is None)` and `if x is None:` are one question in two positions. The fold still folds,
  asserted in both directions from the module: the popped slot's `@main` carries exactly one
  `call i32 @rt_payload_eq(i32 %popped, i32 %tag, i32 0, i32 3)` and the literal programs carry none. Four shapes
  measured beside the family and not bought, each byte-identical on the parent build, are filed rather than
  buried: **Gap R.415** (the same list under a second name — `ys = xs` / `print(ys.pop(0))` prints `0`, the alias
  carrying no kind claim to the door), **Gap R.416** (a loop item over a *literal* list, `==` half already wrong),
  **Gap R.417** (a callee's own parameter — `pairCallScan` opens the pair for a popped argument, the tag crosses
  the frame, and the body throws it away) beside **Gap R.388**'s standing call-answer row. Measurement over the
  **34** programs the two suites carry, a build of the parent (`af8b847`) beside this one, each asked of the pinned
  CPython: **17 moved to the reference's answer — 4 of them off a flat REFUSAL — 11 controls held, 6 still differ
  and each is a row, none moved away from the reference**; `tools/recmerge` wrote **34** entries from the reference
  (**8991 → 9025**) and refused none, and the two pins **Gap R.412** left in the pop row's owed tables are
  DELETED rather than edited, which is the only way a paid debt is distinguishable from a rewritten one.

- **The alias of a list answers what the name that built it answers (Gap R.415, ADR 0412)** —
  `ys = xs` shares an OBJECT and, until this row, shared nothing else. The handle travels; the notebook
  does not, and `mixedLists`/`listElemStr`/`listElemInt` are keyed by NAME, so every door that asks the
  notebook before it asks the object asked a record that had never been written and answered from the
  payload — the void's `0`, the intern table's index, the float box's handle (**ADR 0172**'s collision,
  reached by writing the most ordinary statement in the language beside a door that had only ever been
  asked about one name):

  ```gusty
  xs = [None, 1]            print(xs.pop(0))       #  None   None      — the name answered
  ys = xs                   print(ys.pop(0))       #  None   ← this backend, before: 0   — at exit 0
  print(ys.pop(0) is None)  #  True   ← this backend, before: False
  ```

  The fix is the notebook's, not the printer's, and its finding is that the record travels with the
  handle in **three** places and any two of them is a wrong answer at the exit code of success or worse:
  the claims are **copied** at the binding (without the copy the mutation doors read an empty record for
  the alias, store an untagged payload into a tagged container, and the read-back is a **SIGSEGV**), the
  **link is kept** for the read (a claim frozen at the binding is a claim about the moment of the binding,
  and `xs.append("a")` is an event after it), and the **promotion** is told to every name that holds the
  object (`markListMixed` over `containerSharers`, cycle-guarded), because one `append` is one event on one
  object wherever the source writes it. A link resolves at read time and dies with the binding that hands
  either name another object, which is why `xs = [2, None]` under an alias leaves `print(ys[0])` answering
  for the FIRST list. The roads gained a lookup, not a road: the pop's gate and its door ask ONE
  `listElementClaims` table (**ADR 0249**), the slot read and the loop's tagged arm ask the same lookup, and
  the removal still goes through the one `@rt_pop(i32, i32, i32*)` **ADR 0410** hung — the IR guard counts one
  call, checks the tag's out-parameter and the alias's own handle, and forbids `@rt_pop_aliased` by name. The
  third answer survives: an alias of a list no record describes is DECLINED, not guessed (**ADR 0166**).
  Measurement over the **42** programs the two suites carry as answered, a build of the parent (`8aa3029`)
  beside this one, each asked of the pinned CPython: **26 moved to the reference's answer, 16 controls held,
  0 regressions, 0 of the family still differing**; `tools/recmerge` wrote **36** entries from the reference
  (**9025 → 9061**) and refused none, and the pin **Gap R.415** left in the void row's owed table is DELETED
  rather than edited. What the row left standing is filed with its bytes beside it: an alias of a DICT or a
  SET is the same missing record one container over — **Gap R.419**, two wrong answers and two crashes — the
  callee's printer through the alias stays **Gap R.417**, and the slot bound and asked as a condition stays
  **Gap R.146**'s one-word position, refused exactly as its non-alias twin is.

- **The alias door is a CONTAINER door — the dict's and the set's record travels with the handle too
  (Gap R.419, ADR 0413)** — **ADR 0412** gave `ys = xs` a record and stopped at the list's three claims; the
  dict's six and the set's three were shared just as little, and the answer was the same class of wrong one
  container over:

  ```gusty
  d = {"a": 1}   e = d   e["b"] = "x"
  print(d)   #  {'a': 1, 'b': 'x'}   reference
             #  {'a': 1, 'b': 1}     ← this backend, before: the text's intern index, at exit 0
  s = {1, 2}   t = s   t.add("a")
  print(s)   #  SIGSEGV              ← an untagged member reaching a table nobody wrote
  ```

  The fix is the same door, widened: `containerKindHeld` asks which container the notebook says a name
  holds, `recordContainerAlias` copies THAT container's claims at the shared-handle binding, and
  `markDictMixed`/`markSetMixed` join `markListMixed` in telling **ADR 0232**'s promotion to every name in
  the cycle-guarded `containerSharers` walk — because `d["b"] = "x"` written against one name and
  `t.add("a")` written against another are one event on one object. The row's sharpest finding is about
  ignorance itself: the dict store's contradiction check asks `dictValInt`/`dictValStr` of the name it is
  written against, and a name with NO record does not abstain — it concludes *nothing contradicts*, so the
  entry is stored unlabelled. An empty notebook is a confident wrong answer, not a lack of one (**ADR
  0172**'s collision at the store door). No new road and no new helper: the store still lowers to
  `rt_dict_put_tagged`, the member to `rt_set_add_tagged`, and an alias is a NAME — which is also why
  `for k, v in e.items():` and `t.remove(1)` stay refused exactly as they are over the source name
  (**Gap R.174**). Measurement over the **37** programs the dict/set battery carries, a build of the parent
  (`8aa3029`) beside this one, each asked of the pinned CPython: **15 agreeing became 31, and none moved
  away from the reference**; `tools/recmerge` wrote **23** entries from the reference (**9061 → 9084**) and
  refused none, and the three pins **Gap R.419** left in the owed tables are DELETED rather than edited.
  What the row declined to answer is filed with its bytes beside it — **Gap R.420** (a rebinding releases a
  container another name still holds and the allocator hands that slot to the next object), **Gap R.421**
  (a set's print order puts the void last) and **Gap R.422** (`discard` compares the payload and not the
  pair) — each of the three answering identically with NO alias in the program and on the parent build.

- **A set keeps its members in the reference's bucket order, and the add door decides it (Gap R.421, ADR
  0415)** — `s = {1}` / `s.add(None)` / `print(s)` printed `{1, None}` where CPython prints `{None, 1}`,
  at **exit 0**: both members were in the object and both carried their tag (**ADR 0232**), so the absence
  was the ORDER, and an order is not cosmetic here because `print(s)` and `for x in s` are two doors over
  one array and the reference answers them with one sequence — the record's `repr` is that sequence too.
  The row's first route was the printer, and that is the route it rejected: sorting at print time fixes the
  first door and strands the second, and every later reader (`sorted`, a join, a comparison) inherits the
  same choice to re-make. The order went into the container instead. `@rt_member_bucket(i32 %v, i32 %t)`
  answers where a pair belongs — `0` for the void, whose hash lands it in the table's first bucket; the
  payload's floor-mod 8 for a number or a verdict, an int hashing to itself with CPython's documented remap
  of `-1` to `-2`; and `8` as the *not participating* answer for any kind the payload cannot speak for, a
  text's salted hash included, which is why `t.add("a")` after `s = {1, 2}` still prints `{1, 2, 'a'}` —
  the honest answer to an unanswerable question. `@rt_set_add_tagged` then **inserts** at the first member
  whose bucket is strictly greater and shifts the tail up one, payload AND tag together (**ADR 0410**'s
  pair rule: a shift that moved the payload alone leaves every shifted member wearing the kind of the slot
  it landed on), equal buckets keeping insertion order as the reference's probe leaves them. Nothing sorts
  at print time; dedupe is untouched and still asks the pair, so `s.add(1)` on `{1, 2}` is one member. The
  finding the row did not buy is filed with its bytes: the set literal has TWO fill roads — its mixed arm goes
  through the ordered door, so `s = {1, None, 3}` prints `{None, 1, 3}`, while its single-kind arm fills positionally
  with `rt_set_elem` beside `rt_tag_elem` — the tags ARE written (**ADR 0187**, **ADR 0189**), what that arm withholds
  is the PLACE — so `s = {3, 1, 2}` keeps the source's order and `s = {7, 2}` / `s.add(9)` lands the new member ahead
  of two that were never placed (**Gap R.423**), and a collision or a
  table that outgrows its first 8 buckets wants the reference's perturb probe and its rehash (**Gap R.424**)
  — and a TEXT member, whose bucket is a per-process salted hash the payload cannot answer for, is parked after
  the numbers, which costs it the reference's place for eight of fifteen words the pinned oracle orders the other
  way and is filed as **Gap R.425**. Measurement with a build of the parent beside this one, over the **89**
  programs the battery and the two alias files carry: **9 moved to the reference's answer, 65 controls held, none
  moved away from the reference**; the leftover programs are exactly those rows. `tools/recmerge` wrote **10**
  entries from the reference (**9084 → 9094**) and refused none. The commit also corrected a pin that had
  `"want CPython's"` above `{1, 'a', None}` for `s = {1, "a", None}` — bytes eight unpinned `python3` runs do not
  produce — and lifted that file's ban on oracle set-print cases, three of which now compare the linked binary to
  a live reference over one order (**ADR 0166**: a wrong answer's pin is a debt, not a witness). **ADR 0414** is left unissued on purpose: the in-flight
  release-door change owns that number, and this row is not it.

- **A released container is asked who still holds it, at the door in front of the free list (Gap R.420 +
  Gap R.257, ADR 0414)** — `d = {"a": 1}` / `e = d` / `d = {"b": 2}` / `print(d)` / `print(e)` printed
  `{'b': 2}` **twice** at **exit 0** where CPython prints `{'b': 2}` then `{'a': 1}`, and `print(e["a"])`
  raised `KeyError: 'a'` over a dict that had never lost its `a` — the same lie told twice. `e = d` copies the
  handle (**ADR 0412**), and the rebinding's release pushed that one object's slot on the allocator's free list,
  so the very next `rt_alloc` took the slot back and wrote the SECOND dict into it: the alias's handle, being a
  slot number, denoted the new object. The collector could not undo it and the reason is the design: `rt_gc`
  marks the whole free list dead (`2`) BEFORE it marks from the roots, and `rt_gc_mark` marks only a slot whose
  mark is `0`, so a rooted object once pushed stayed pushable — while letting the root mark override that dead
  mark, the first alternative, leaves a LIVE object on the free list for `rt_alloc` to hand out. The question
  therefore moved to the door in front of the push. `emitFree` calls `@rt_release`, which walks the collector's
  own table — `gc.roots` / `gc.kinds` to `gc_roots_used`, the same kind byte, so twelve `int` roots whose words
  are the indices `0..12` veto nothing — and declines to push while any entry holds the handle;
  `emitFreeOld` tags the REBINDING's own entry dead first, so the question is “does any OTHER name hold this?”
  and not a permanent veto by the slot being written over (**ADR 0181**'s precision, asked at the door as well as
  at the sweep). A declined release costs one occupancy until the sweep — which is the road allowed to decide
  liveness (**ADR 0350**) — and that is measured, not promised: a 500-iteration churn reports the same
  `total_freed=1998` as its parent and a 2000-iteration one has to answer with `exhausted=0`. `@rt_free` now has
  exactly ONE caller in a module and it stands inside `@rt_release` (the sweep pushes `free_next` inline), and the
  six positions of a rebinding — `load` → `rt_root_clear` → `icmp ne` → `rt_release` → `rt_alloc` → `store` — are
  pinned from the emitted `define` blocks, because an order the codegen merely happens to have is a hope and not a
  claim. **Gap R.257**'s pinned program came with it: `a = {1: 2}` / `b = a` / `b = [7, 8]` / `print(a[1])`, dead
  with `KeyError: 1` at exit 3 on the tagged parent, prints the reference's `7 / 7 / 2` and was promoted — loop form
  and both negative controls included — into the answered table, the record and the corpus. What the per-name
  question cannot see is filed rather than smoothed: a value that survives only in another container's SLOT is
  still released eagerly, because a tag is data and not an entry in `gc.roots` (**Gap R.427**), and `sorted(d)` over
  a dict sorts the entry array's first `len` words instead of its keys — `['a', 1]` for `['a', 'b']` (**Gap R.426**,
  both byte-identical on the parent `f7723b2`). `tools/recmerge` wrote **30** entries from the reference (record
  **9094 → 9124**) and refused none; the two owed-table pins this row paid are DELETED, not edited.
- **A class keeps the members its body bound, and the read asks the class (Gap R.297, ADR 0418)** —
  `xs = [1, 2, 3]` / `class C:` / `first = xs[0]` / `print(C.first)` stopped the build with
  **`unsupported attr expression`** where CPython prints **`1`**: the class walk registered the methods and dropped every
  other statement of the body, so the read fell through the value road's cases onto a sentence naming an AST node at the
  program (**Gap R.116**'s rule). Python builds a class by executing its body, and what the body bound becomes an
  attribute of the CLASS — a different road from an instance attribute, which lives in the object's slot array behind
  `@rt_inst_get`, and the temptation was to reach for the machinery that already existed. `classInfo` now keeps a member
  table, and the read lowers the bound **expression** where it is READ: a body runs once and the read may stand anywhere
  after it, which is also what makes `C.first = 5` then `print(C.first)` answer `5` without a runtime attribute store.
  Only values whose re-lowering is a read, not an event, are kept that way, and the print-side kind questions ask the
  same table — `print(C.label)` first came out `0`, the intern table's index, until the text question was taught to read
  it too. What cannot be lowered faithfully is refused with its kind named rather than answered as the payload word: the
  first version printed `1.5` as `1.0`, `True` as `1` and `None` as `0`, and the row was only shipped once those became
  refusals (**Gap R.431**, alongside the container- and call-bound members and `C.new = 1`). Whole numbers, texts, the
  arithmetic and concatenation over them, a member in a condition, the rebinding, and `__doc__` answer the reference's
  bytes on the compiled leg, the record leg and the CLI leg; the instance road is pinned as the untouched neighbour, and
  `tools/recmerge` wrote **16** entries from the reference (**9156 → 9172**), refusing none.
- **A dict reads its void and verdict keys — and the roads that LABEL an operand ask the object first
  (Gap R.285, ADR 0419)** — `d = {None: "a"}` / `print(d[None] + "b")` stopped the build with
  **`concatenating a string with a value that is not a string … CPython raises TypeError for it`**, a sentence
  that is **false of the program** (CPython answers `ab`), and `print(d[None] * 2)` printed **`0`** — the interned
  text's index — at the exit code of success, while the `1`-keyed twin answered both all along. One construct, two
  answers, decided by which KIND the key was spelled with: the door that answers *what kind is in this entry*
  (`constKeysMatch`, which walks the dict's own literal) had arms for an integer, a text and a float and none for
  the void or a verdict. The arm was not simply missing — **ADR 0353** built it, measured it and reverted it,
  because two *callers* of that door then answered wrong: the membership test and the `for` loop each read an
  entry's kind to decide what question to ask at all (walk its slots, its keys, its members, or test a substring),
  and neither can walk a text, a number, a verdict or the void, so each read the payload as a heap handle and
  answered at exit 0 — `1 in d[None]` said `False` where the reference raises `argument of type 'int' is not
  iterable`, and `for x in d[None]` over `5` printed `0 1 2`. So the row went in the order the record prescribed:
  **the two label roads first** (they ask the object for the slot's `(payload, tag)` pair through
  `slotPairWhereStaticLabelCannotWalk`, whose tag chooses the arm and raises for a kind with no elements), **then
  the door** — with the void answered before any number arm can reach it, because the constant fold reads a
  `NoneLit` as the integer `0` and `{0: "a"}` read by `None` had been finding an entry the program never wrote
  (harmless only by luck: the object road caught it. A gate that is only correct because another road exists is
  not a gate). `print({0: "a"}[None])` raises `KeyError: None`, pinned both as a decision of the door asked **as a
  function** over 21 key/index pairs and as a program. Two predicates, two questions, one measurement: `exprIsString`
  (*what kind is this value*) asks the literal, and the concatenation's gate asks it too — otherwise
  `{None: "a", 1: "b"}` / `print(d[None] + d[1])` emits `add i32` over two `@str_tab` indices and prints `b`;
  `printsAsInternedStr` (*is the WORD an index a road may print directly*) is NOT widened, because measured, it
  took `print(["a", "b"][0])` and two pinned neighbours from answers to exit-1 refusals. Sixteen shapes that were
  wrong answers at exit 0 are the reference's now, and two never had a `None` in them: `xs = [5]` /
  `print(1 in xs[0])` answered `False` and `xs = ["ab"]` / `for x in xs[0]` printed nothing.
  The door behind the door was in the read road: an **integer** key was the one key spelled without a tag, so it
  took the untagged `rt_dict_has`/`rt_dict_get` pair — which compares payloads alone, and the void's payload IS 0.
  That is where `d = {"a": 1, "b": 2}` / `print(d[0], d[1])` answered `1 2` at exit 0; the integer key now carries
  `TagInt` to the tagged doors every other key has asked since **ADR 0232**, the program raises the reference's
  `KeyError: 0` catchably, `{0: "zero"}` still answers `zero`, and **Gap R.231** closes as the side-effect its own
  Definition of Done described. `tools/recmerge` kept **95** entries recorded from the reference (**9172 → 9267**)
  and refused four sources rather than let an answer be self-certified; **twelve more were withdrawn by hand** —
  they were interpolated slot reads (`print(f"{d[None]}")`), and the record leg is kept permanently silent about
  that field, which is witnessed by its `print` twin and by the live-`python3` CLI row instead. The two Gap R.285
  pins in `integration/dict_none_bool_key_test.go` are DELETED, not edited, and `integration/programs/void_key_label_roads.gy`
  joins the corpus as an asserted `match` row. Two defects the change made visible are FILED, not fixed:
  **Gap R.434** (`d = {None: (1,)}` / `print(d[None] * 2)` writes `[1, 1]` and `print(d[None] + (2,))` raises a
  sentence about a list the program never wrote, because the sequence door asks a KIND where the pair door handed it
  a TAG — byte-identical with an `int` key on this commit's parent) and **Gap R.433** (a fraction in a slot through
  `max` into a call answers `3` for `3.5`, because the fold that would keep it hands a bare `double` to a one-word
  position and `llc` rejects the module).

- **A name that held the void is still the void: the tag a BINDING leaves behind (Gap R.286, ADR 0420)** —
  `d = {None: 3.5}` / `k = None` / `print(d[k])` died **`KeyError: key not found`** at exit 3 where CPython prints
  **`3.5`**. The read was fine; the door had two halves, and the second is why the first hid for eight days. The
  read road had an arm for a key arriving through a binding and it was **unreachable** — it sat in the `else` of a
  test on `n.Idx.(*IntLit)`, so it could only run for an index that was BOTH an `*IntLit` and a `*Name`. Every name
  key fell through to `elemKindTag`, the one door that answers *which kind is this value*, and that door has arms
  for the container, the text, the float and the instance a name holds, literal arms for `*NoneLit`/`*BoolLit`, and
  **no arm for the name whose binding wrote a singleton** — so its foot answered `TagInt` and the void reached
  `rt_dict_has_tagged` as `(0, TagInt)` beside an entry the builder had stored as `(0, TagNone)`. The two arms went
  into **that** door, read from the **ADR 0172** latest-binding records `print`, truthiness, `str` and equality
  already live by (the key door may not know less about a name at the same point than the printer does), with a
  loop variable the unroll tagged and a name the container records claim both standing aside. Binding the singletons
  through the pair road instead was built and **rejected**: `taggedVars` is a fact four other roads consult. The
  static door got `constKeyView`, which retires a void-named key to its singleton — that is what makes
  `d = {None: "a"}` / `k = None` / `print(d[k] + "b")` answer `ab` and not merely the plain read — and it retires
  **only the void**, because `boolVars` records that a name holds a verdict and not *which*; a key that cannot be
  quoted stays unreadable, which is a refusal rather than a guess. The traceback names what the records can quote
  (`KeyError: None`, and `KeyError: 'zz'` for a bound text read from `strVals`, the printer's own record). **15**
  unit rows over two cases and **11** CLI rows against a live `python3`; `tools/recmerge` wrote **13** entries
  (**9267 → 9280**) and **refused two** whose compiled raise is not the reference's; **four pins deleted**;
  `integration/programs/bound_dict_keys.gy` joins the corpus as an asserted `match` row; and a **4878**-source sweep
  of the record corpus against a `git worktree` build of this commit's parent reported **0 regressions / 16
  improvements**. Two shapes the measurement surfaced are FILED with today's bytes pinned beside the reference's:
  **Gap R.435** (`k = None` / `print([k])` writes `[0]`, because the list literal records one element kind for all
  its slots) and **Gap R.436** (the operator road over an entry addressed by a bound **number** key refuses where
  the const-key twin answers, because the door has no number-in-a-name record to retire).


- **The void a slot holds is the void its binding wrote: the container literal asks the ELEMENT (Gap R.435,
  **ADR 0421**)** — `k = None` / `print([k])` wrote **`[0]`** at **exit 0** where CPython writes **`[None]`**,
  while `print([None])` had written `[None]` all along and the mixed sibling `ys = [None, 1, "a"]` / `print(ys)`
  wrote `[None, 1, 'a']`. Two lines of one program, two answers, and the difference is whether the void was
  **WRITTEN** or **NAMED** — the signature of a gate that reads the source text next to a road that reads the
  object. A container literal goes one of two ways: an `i32`-sized slot folds the whole literal into a static
  module array (**ADR 0239**, what `sum`/`min`/`max` reduce from), and a slot that cannot live in a bare word is
  built on the heap with a **tag beside each payload** (`rt_print_list_mixed` walks the pair). The gate that
  chooses is `literalNeedsTags`, and its float arm (`floatSlotExpr`) and verdict arm (`boolSlotExpr`) have asked
  the ELEMENT for years — **ADR 0259** added `|| g.printsAsBool(e)` for exactly this shape — while its void arm
  was the bare `isNoneLitExpr`. So a slot holding the void through a binding was not a void to the gate, the
  literal stayed on the static array, and the array's element reader folds a void-bound name to the payload the
  void happens to carry. The fix is one arm, but the **pair** is the unit: the first cut widened the gate alone
  and `def f(): pass` / `print([f()])` went from a wrong answer at exit 0 to a REFUSAL at exit 1, because
  `elemsTaggable` did not know the void a CALL answers — **ADR 0249**'s gate-and-door rule, run in the direction
  that matters. So `elemKindTag` grew the arm the gate needed in the same commit, and `noneSlotExpr` restates the
  door's two exemptions (a name the unroll tagged is THIS item; a name the container records claim holds a handle
  the object describes) from the SAME maps the door reads. The payload is neither trusted nor printed: a
  `TagNone` slot is forced to `0`, `rt_mark_estr …, i32 8` says the slots are the truth, and the mixed printer
  renders the pair, with the static array absent — asserted in both directions, and the numeric control still
  keeps `rt_print_list` and its fold. **36** answered rows (23 through a binding, 13 through a CALL) on the
  compiled leg AND the record leg, **9** control rows the gate must not touch, a **gate/door property case** that
  fails if the gate sends a literal the door declines, an IR row that pins the tagged build and denies the static
  array, **31** programs through the CLI against a live `python3`; `tools/recmerge` wrote **45** entries from the
  reference (**9280 → 9325**) and **refused** `bool([k])`, whose compiled leg still owes **Gap R.131**; the drift
  ledger row `print([{"a": 1}.get("z")])` went **PAID, not edited** (**318 → 317**); and
  `integration/programs/void_slots_say_themselves.gy` joins the corpus as an asserted `match` row. A sweep of all
  **9325** record sources through a `git worktree` build of this commit's parent reports **0 regressions over 20
  changed sources, 18 of them this family**. Four shapes the measurement surfaced are FILED with today's bytes
  pinned beside the reference's: **Gap R.437** (the void through a frame's front door — a parameter, or the
  module's name read from inside a body: the gate asks the FRAME's records), **Gap R.438** (`sum([None])` is `0`
  and `sorted([1, None])` produces an order the reference refuses to compute — the fold and the comparator ask the
  payload, never the kind), **Gap R.439** (`xs = [1]` / `print([xs, 2])` wrote `[0, 2]` — the gate's container arm
  was a spelling test while the door has had the container-NAME arm since **ADR 0189**; **paid by ADR 0438
  below**) and **Gap R.440** (a loop
  item's own kind, which `print(k)` reads from the unroll's record and the literal beside it does not).


- **A loop item's kind is kept once per iteration, so ONE predicate answers it — the print door and the
  four gates beside it (Gap R.440, **ADR 0424**)** — `for k in [None, 1.5]:` printed **`None`**, **`1.5`** for
  `print(k)` and **`[0]`**, **`[1]`** for `print([k])` on the next line, both at the **exit code of success**:
  the unrolled `for` road records what each item is (`loopElemTag`, since **ADR 0233**), the print door reads
  that record (**Gap R.358**), and the four gates that decide whether a container literal needs its slots
  labelled asked the NAME and its older bindings instead — so one value had two doors, and only one of them
  had been told. `floatSlotExpr`, `boolSlotExpr`, `noneSlotExpr` and the container arm of `literalNeedsTags`
  now ask the record before they ask the name, and `elemKindTag`/`elemTagFor` — which had carried a private
  copy of that reading since **Gap R.358** — ask the same function, so gate and door cannot keep two readings
  of one map (**ADR 0249**). The half that decides the row is that the answer carries **whether the record
  speaks for this name at all**, not only whether the item has a kind a word cannot render: for
  `k = None` / `for k in [1, 2]:` the item's kind IS a plain number, and a gate that hears only "no kind"
  reads the stale void claim and refuses the literal as mixed — the shape a **9421**-source sweep of the
  record corpus caught and no probe had thought to try. A **46-shape probe** beside a rebuilt parent:
  **16 newly answering / 0 regressed**, twelve of them wrong answers at exit 0; the record sweep reports
  **9421 unchanged / 0 improved / 0 regressed**. `pkg/lang/loop_item_literal_test.go` NEW (**28** answering
  rows on the compiled leg AND the record leg, **12** controls with **ADR 0239**'s fold asserted intact in the
  IR, **3** IR rows reading TagNone `3` / TagFloat `1` / TagList `5` off `mainRegion`, **9 × 6** gate-and-door
  property rows, **7** filed-not-fixed) and `integration/loop_item_literal_test.go` NEW (**34** CLI rows
  against a live `python3`, **6** pinned wrong answers, **1** refusal row); `tools/recmerge` wrote **33**
  entries from the reference with the compiled leg cross-checked (**9421 → 9454**) and REFUSED three whose
  compiled leg disagrees. Three rows FILED from the same probe: **Gap R.445** (the interpolation field of an
  item — `print(k)` answers `None`, `print(f"{k}")` answers `0`), **Gap R.446** (the fold over a fractional
  item — `sum([k])` answers `0`, `1`), **Gap R.447** (the `for` road's `store i32` into the `double` slot the
  float road declared, which `--verify` calls `ok` because opaque pointers make `i32*` and `double*` one type).

- **A comparison chain asks each of its operands what it CARRIES (Gap R.442, **ADR 0423**)** —
  `xs = [1, 2]` / `xs[0] = 2.5` / `print(0 < xs[0] < 3)` said *"this context needs a single static kind"*
  where CPython prints **`True`**; `d = {"a": 2.5}` / `print(0 < d["a"] < 3)` printed **`False`** and
  `d = {"a": "x"}` printed **`True`**, both at the **exit code of success**; and
  `print(0 < abs(-2.5) < 3)` reached `llc` as `store i32 %t1, i32* %_chain1` over a `double` — the
  exit-code contract's **exit 2** (ADR 0166), spent on the compiler's own module. One cause: the chain stores
  each operand once so the middle is evaluated once (**ADR 0288**), and the store it wrote was ONE WORD, while
  a value in this language is a `(payload, tag)` pair (**ADR 0331**) — the dict entry's payload is a
  `@float_box` handle, a `@str_tab` index, the void's `0`: numbers for reasons that have nothing to do with
  the value. The fix is one pure question asked BEFORE lowering (`chainOperandCarriesMoreThanAWord`) and the
  THREE doors the neighbouring positions already ask, in the binding road's order — `comparePair` (the door
  the single-comparison form of the SAME program already answers with, **ADR 0232**), `assignPairWords`, then
  the float road as the net between "a double" and `store i32` over a double — each arm building into scratch
  and committing only once it answered, because a door that changes its mind halfway has already evaluated the
  operand twice. The same sweep paid two neighbours: **`+` beside a number the SOURCE spells** (the two shapes
  **ADR 0265**'s proof exists for — `"a" + "b"`, `[1] * 2` — are unreachable with an integer literal on the
  other side of `+`, so `*` and `%` keep the proof and `+` asks the two tags; a class, an instance and a
  function stay behind the door, because an object's int half is a pointer and `print(C() + 1)` answering `1`
  is the trap that would have shipped), and **the sentence the trap wore**: `text + number` and
  `list + number` are not "unsupported operand" to CPython, they are the concatenation method reporting what
  it was handed, so `@rt_num_arith` writes `can only concatenate %s (not "%s") to %s` for the three kinds that
  have one, LEFT tag only, which is what CPython does. A **25-program sweep** through the rebuilt parent:
  **10 improved / 0 regressed / 13 already correct / 2 still owed** — 7 refusals became answers, 3 wrong
  answers at exit 0 became right, 2 were exit-2 module rejections. `pkg/lang/chain_operand_test.go` NEW
  (**19** answering rows on the compiled leg AND the record leg, **3** trap rows, **5**+**2** IR rows asserting
  the pair-bound chain slot and the untouched cheap one, **5** gate-purity rows, **1** white-box row asking the
  double door itself, **2** pins); `tools/recmerge` wrote **27** entries from the reference with the compiled
  leg cross-checked (**9394 → 9421**), refused none; the pins **ADR 0422** left MOVED rather than disappeared.
  Two rows the sweep surfaced are FILED with today's bytes: **Gap R.443** (the loop item is still a one-word
  chain operand — the `for` road keeps its own per-item tag and the gate does not read it, so
  `for v in [None, 2.5]:` `print(0 < v < 3)` answers `False` `False`) and **Gap R.444** (the concatenation
  sentence blames the **list** when the operand is a **tuple** — right trap, wrong class, **ADR 0251**'s tuple
  being a list to the payload).

- **Removing a set member asks the ARGUMENT what it carries (Gap R.422, **ADR 0425**)** — `s = {1, 2}` /
  `s.discard("a")` / `print(s)` wrote **`{1}`**, `s.discard(9)` wrote **`{1}`**, `s = {0, 1}` /
  `s.discard(None)` wrote **`{1}`**, `s.discard([1])` wrote **`{2}`** where CPython raises
  **`TypeError: unhashable type: 'list'`**, and `s.add(1)` / `s.discard(1)` / `s.discard(9)` /
  `print(len(s))` wrote **`-1`** — all five at the **exit code of success**. `s.remove(9)` was refused at
  **exit 1** by the string-method road's name-holds-container gate, where the reference answers
  **`KeyError: 9`**. One cause: `add` asks its argument what it carries (a stored text is an index into
  `@str_tab`, so the text `"0"` and the number `0` are one `i32` — **ADR 0187**, **ADR 0189**) and `discard`
  asked the SET, then handed the removal door ONE WORD, so the scan deleted the first member whose payload
  happened to equal whatever the argument's payload happened to be. Both verbs now resolve the member the way
  `add` does — `pairElemPair`, then `taggedOperand`, refusal rather than accident if neither answers — and go
  to the same two doors in the same order: `rt_set_find` (payload AND tag, through `rt_slot_matches`) for the
  position, `rt_set_remove_at` for the shift of BOTH arrays, with `rt_set_discard_tagged` holding the two and
  the absent test between them. Hashability answers before the set is consulted, and the exemptions are
  measured one program per cell rather than copied from the dict door: `s.discard({1})` raises NOTHING and
  `s.remove({1})` says `KeyError: {1}` — a bare set is a MISSING member here, never an unhashable one — while
  `s.discard((1, {2}))` still names the set, and a tuple is exempt from the tag test because this backend
  gives it a LIST's pair word (**ADR 0361**). The raise names its member too (`KeyError: 9`, `'z'`, `None`,
  `False`, `1.5`, `{1}`, `set()`), which needed the unroll to keep WHICH element this body copy walks beside
  **ADR 0424**'s record of what it is. A **45-program battery** beside a rebuilt parent: **30 moved to the
  reference, 0 regressed, 1 still owed** (**Gap R.447**'s slot word, not this door's); a **9526**-source sweep
  of the record corpus through both builds reports **9472 unchanged / 54 changed** — 52 of them set-verb
  programs, **49 moving TO the record, 0 moving away**. `pkg/lang/set_member_removal_test.go` NEW (**26**
  answering rows on the compiled leg AND the record leg, **3** IR rows denying the payload-only scan, **11**
  trap rows on both legs, **9** sentence rows, the two doors asked directly of the AST, **3** filed-not-fixed)
  and `integration/set_member_removal_test.go` NEW (**29** CLI rows against a live `python3`, **15** raise rows
  whose sentence the reference re-derives inside the case, **5** exit-code rows, **3** pinned wrong answers);
  `tools/recmerge` wrote **72** entries from the reference with the compiled leg cross-checked
  (**9454 → 9526**) and refused the sources whose compiled leg disagrees, which are the pinned rows and not
  records.

- **The comparison asked of an unrolled loop ITEM asks the ITEM (Gap R.416, **ADR 0426**)** —
  `for v in [None, "a"]:` / `print(v)` printed **`None`**, **`a`** and the line beside it printed
  **`False`**, **`False`** for `v is None` and the same for `v == None` and `v != None` said `True` twice,
  while `for k in [None, 1.5]:` / `print(k in [None, 1.5])` said **`False`** twice and `print(v == 1.5)` over
  `[1.5, 2.5]` said `False` twice — every one of them at the **exit code of success**, where the reference
  prints `True`/`False`, `True`/`True` and `True`/`False`. The same item over a NAMED list answered correctly,
  which is what localised it: the unrolled `for` records each element's KIND per body copy (**ADR 0233**) and
  the print door (**Gap R.358**), the truth door (**ADR 0388**) and the container-literal gates (**ADR 0424**)
  read that record, while the compare family reads the notebook the pair road writes — which a literal unroll
  never visits. So the fold compared payload WORDS, and a text's payload is an index into `@str_tab` (`0` for
  the first string a program interns), a float's is a `@float_box` handle, the void's is `0`, and an integer
  item's slot can carry `0` too (**ADR 0187**): three kinds and a number all compared equal to `None`, and the
  void turned out to be a member of a text list. One predicate read in three doors — `taggedOperand` answers the
  item with a LOAD of its own slot plus the recorded tag (**it re-derives nothing**: asking `floatValue` or
  `containerOperand` of the name would mint a second object out of whichever number the slot happens to hold,
  which is **ADR 0233**'s rule read from the other side), `isTaggedCompareSide` admits it to the pair door
  (**ADR 0410**'s `comparePair`, in the value road AND the condition road), and the literal-haystack membership
  scan takes `@rt_contains_tagged` because the builder already writes a kind beside every slot. An integer or
  interned-text item compiles byte-for-byte as before. `pkg/lang/loop_item_compare_test.go` NEW (**23** answered
  programs on the compiled leg AND the record leg, **5** rows putting `print(v)` and `v == …` on ONE line, **8**
  rows moving the item through every position a compare stands in, **12** door rows asked directly over
  hand-built generator states, **6** IR rows in both directions, **1** row reading back which register the pair
  call is handed, **5** filed-not-fixed) and `integration/loop_item_compare_test.go` NEW (**28** CLI rows against
  a live `python3`, **8** exit-code rows, **5** owed rows pinned beside the reference). A **9526**-source sweep of
  the record corpus through a `git worktree` build of the parent and this commit reports **0 changed answers** —
  the corpus had no case for this family at all, which is why `tools/recmerge` wrote **48** entries from the
  reference (**9526 → 9574**) and refused the one source whose compiled leg disagrees (a PARAMETER's kind —
  **Gap R.417**'s standing). Four pins this row owned MOVED from the owed tables to the answered ones, in both
  suites, and a fifth that belonged to **ADR 0425** went paid on the way through: `s = {1, 2}` / `for k in [1.5]:`
  / `s.discard(k)` / `print(len(s))`, pinned at **`1`** where the reference prints **`2`**, answers `2` once the
  removal asks the same arm instead of the number a box handle looks like. Three rows FILED from the same probe:
  **Gap R.448** (the ordering door never asks the item — `print(v < 2)` over `[1.5, 2.5]` says `True` `True`
  and `print(v < 3)` over `[None, 2.5]` answers where CPython raises), **Gap R.449** (the condition road folds
  `s == None` from the payload words while the value road folds it from the source kinds — `s = "a"` /
  `if s == None:` prints `void`), and **Gap R.450** (an item whose kind is a container, compared with a
  container literal, is refused where the reference answers).

- **A body that answers the void answers it at every door (Gap R.388, **ADR 0427**)** — `def f():` /
  `    return None` / `print(f())` wrote **`None`**, and the line beside it wrote **`False`** for
  `print(f() is None)` and **`False`** for `print(f() == None)`, both at the **exit code of success**,
  where the reference prints `True` twice — while the very same body spelled `def f():` / `    pass`
  printed `True`. One value, two spellings, three answers. The print road, `str()`/`repr()` and the REPL
  all consult ONE predicate, `isNoneExpr`; the `is`/`==` road consulted a DIFFERENT one, asking only
  whether the callee's body has a `return` statement at all (`fdReturnsValue`). A body that falls off its
  end has none and was read as the void; a body that WRITES `return None` has one, so the fold called it
  a value and compared its payload — the word `0`, which is also the word an `int` slot legally carries
  (**ADR 0187**) — against nothing. The fix is one line of permission in the plan rather than a new door:
  `answerKindPlanOf` had been skipping a body whole the moment the pair road claimed it
  (`if p.carry[name] || pairReturns[name] { continue }`), and a body whose every arm answers `None` is
  exactly such a body — it now records the VOID verdict while still taking no arms from it, and
  `isNoneExpr`'s Call arm asks `answersOnlyTheVoid` before the older answer (**ADR 0368**, **ADR 0232**).
  No comparison road was widened: the pair-door route was tried, answered `False` a second time from a
  tag slot this program never reaches, and was deleted. The fold stays a fold and keeps the CALL —
  `r = f() == None` still emits `call i32 @gy_f()` exactly once, so `def f():` / `    print("side")` /
  `    return None` still prints `side` (**ADR 0172**), and a body answering `None` in one arm and `1` in
  another is never read as the void. `pkg/lang/void_answer_test.go` NEW (**23** answered programs judged
  on the compiled leg AND the record leg, **13** position rows — `if`, a negated `if`, `while`, both
  short-circuits, a ternary, a loop element, a folded answer that prints its side effect, the answer used
  twice, plus the controls that must NOT be read as the void — **4** print-and-compare parity rows,
  **7** rows asking `isNoneExpr` and the plan DIRECTLY over a parsed program in both directions, **3** IR
  rows, **4** filed-not-fixed) and `integration/void_answer_test.go` NEW (**35** CLI rows whose expectation
  is read off a live `python3` in the same case, **7** exit-code rows, **1** machine-readable refusal row,
  **3** owed rows pinned beside the reference). Four pins MOVED rather than vanished: the two
  `Gap R.388` rows in `pkg/lang/the_void_question_test.go` and `integration/the_void_question_test.go`, and
  the applied-`lambda` pair in `pkg/lang/lambda_answer_test.go` and `integration/lambda_answer_kind_test.go`
  — each DELETED from its owed table and standing in the answered family with the reference's bytes.
  A **40-program battery** beside a `git worktree` build of the parent (`324f5c3`, **ADR 0426**) reports
  **17 moved to the reference, 19 held byte-for-byte, none moved away**, the 4 that still differ being the
  pinned rows; a **9618**-source sweep of the record corpus through the same two builds changes
  **nothing among the 9574 sources already on the record** — the **22** whose answer moves are all sources this
  row newly recorded, which is the sweep saying the blast radius was one shape and no more. `tools/recmerge` wrote **44** entries from the reference with the compiled leg
  cross-checked (**9574 → 9618**) and refused the four whose compiled leg disagrees, which are rows and not
  records: **Gap R.451** (a body that FORWARDS the void answer — `def g(): return f()`), **Gap R.452** (a
  METHOD whose body answers the void — `c.m() is None`), the arm of a body that also answers a number
  (standing **Gap R.324**) and a container `pop` over a name, refused exactly as it always was
  (**Gap R.174**).

- **A METHOD that answers the void answers it at the class's door: one body reading, asked of a body in a
  class too (Gap R.452, **ADR 0429**)** — `class C():` / `    def m(self):` / `        return None` /
  `c = C()` / `print(c.m() is None)` printed **`False`** at **exit 0**, `print(c.m())` printed **`0`**, and
  `print(c.m() == 0)` printed **`True`**, where the reference prints `True`, `None` and `False` — while the
  identical body in a module-level `def` had answered all three correctly since **ADR 0427**. One predicate
  owns this question (`isNoneExpr`: `print`, `str()`/`repr()`, the REPL, the binding road, the container-element
  door and the fold all ask it) and its `*Call` arm asked two tables, `g.fds` and the answer plan, both keyed by
  a MODULE-level `def` name; a method's body lives in the class's table, so nobody had ever read it (**ADR
  0366**'s one-question-two-tables split, one door later). The fix is the row's own prescription and it is a
  round, not a door: `methodAnswersOnlyTheVoid` asks the SAME `answerBodyPlan` the module road asks — the walk
  that stops at a nested `def`, the arm reader that declines a body with any un-nameable arm — and
  `methodBodyOf` resolves the callee the way the DISPATCH does, by the **receiver's class and the name**, walking
  bases in the order `resolveMethod` walks them (**Gap R.66**: the compiled backend already walks every base, so
  an inherited method is not an ambiguous one). A name-keyed method table was written, measured and rejected: it
  declined `class A():` and `class B():` each with their own `__init__`, which is two bodies the compiler can
  each read (**ADR 0299**'s rule is about one class's body standing in for another's, and a receiver that names
  its class makes that substitution impossible). Four doors moved on one arm — `None` from the print door,
  `True` from the fold, `False` from the `== 0` collision (**ADR 0187**), and `print(str(c.m()), repr(c.m()))`,
  which the parent REFUSED at exit 1, prints `None None`. `pkg/lang/void_method_test.go` NEW (**33** answered
  programs on the compiled leg AND the record leg, **13** position rows with their own controls, **20** direct
  predicate rows over a parsed program in both directions plus 3 sub-runs, **4** IR rows, **7** filed-not-fixed
  pins) and `integration/void_method_test.go` NEW (**47** CLI programs read off a live `python3`, a **13**-row
  exit-code table, **1** machine-readable refusal, **2** owed pins, **2** pins holding the module `llc` refuses).
  A **50-program battery** beside a `git worktree` build of the parent (`66c20b2`, **ADR 0428**): **28 moved to
  the reference, 22 held byte-for-byte, none moved away**; a **9652**-source sweep of the record corpus through
  both builds reports **0 changed answers**. `tools/recmerge` wrote **54** entries from the reference with the
  compiled leg cross-checked (**9652 → 9706**, none refused; the parent was right for **15** of them, wrong for
  **38** and refused **1**). The four `Gap R.452` pins MOVED from the `void_answer`/`void_forward` owed tables
  into the answered families rather than vanishing, and what the battery left owed is filed with its own bytes:
  **Gap R.453** (the answer direction has no method round — a mixed-arm method carries no word for which arm ran,
  and the arm that answers a container hands `llc` the literal's GLOBAL at **exit 2**, the shape **ADR 0333**
  took off a `def`'s `ret` and never off a class's — **closed by ADR 0430 below**), **Gap R.454** (a method forwarding a call to another METHOD
  of its class: `answerArmOf`'s arm reads a `*Name` callee — **closed by ADR 0431 below**), **Gap R.455** (a generator METHOD refused as
  `yield outside a generator function`, though the same body in a `def` compiles), **Gap R.456** (a method
  mutating `self.items` refused by the string-method door) and **Gap R.457** (a nested `def` inside a method
  re-emits the GC frame prologue and `llc` rejects the module).

- **The answer direction gets its METHOD round: a carried method keeps the arm that ran, and a container answer
  is built before it answers (Gap R.453, **ADR 0430**)** — `class C():` / `    def m(self, x):` /
  `        if x:` / `            return 7` / `        return None` / `c = C()` /
  `print(c.m(1), c.m(0), c.m(0) is None)` printed **`7 0 False`** at **exit 0** where the reference prints
  `7 None True`: which arm ran is a run-time fact, and the word that says it is stored by `answerReturn`
  (**ADR 0368**) on the return roads a module `def` takes, which `emitClassMethod` does not share. The louder
  face was a body answering a container — `    def m(self):` / `        return [1, 2]` above `print(c.m())`
  stopped `llc` with *"expected instruction opcode"* at **exit 2** (**ADR 0166**'s compiler-bug code): a module
  `def` that answers a container is handed to `containerRet` and its call site lowers a HANDLE (**Gap R.67**,
  **ADR 0333**), and a method has no such table, so the literal's GLOBAL went into an `i32` return slot — the
  shape **ADR 0188**/**ADR 0189** forbid. The plan now walks a `ClassDef` beside its `def`s and keys each carried
  method by the **emitted symbol** (`gy_C_m` — the name `resolveMethod` mints, so the call site and the `define`
  read ONE word, and two classes sharing a method name are keyed apart); `methodAnswerBodyPlan` asks the ONE body
  reading (`answerBodyArms`, split out so neither caller re-implements the walk that stops at a nested `def`), and
  a carried method returns through the same `answerReturn` a `def` takes — stores `(payload, kind)` beside its
  `ret`, and builds a container arm through `containerHandleOf`/`containerOperand` so the `ret` carries a handle.
  The reading roads then ask the PLAN, not a name: `pairCallPair`, `containerHandleOf`, `containerOperand`,
  `isContainerExpr`, the print/`str`/`repr`/f-string/condition/loop doors, the binding road's
  `recordContainerAnswer` and a literal element's `carriedCallIsPairElem` each grew the method arm beside the
  name arm, so `print(c.m())` prints `[1, 2]` and `{'a': 1}`, `len(c.m())` prints `2`, `c.m()[0]` and
  `c.m()["k"]` index, `1 in c.m()` answers, `c.m() == [1, 2]` compares and `for v in c.m():` walks — all of which
  the parent refused or answered with a bare handle. The None/number comparison asks the WORD rather than the
  payload, which also closed the module `def` twin `print(f(True) == 0)` (**Gap R.324**'s void half, standing
  since **ADR 0427**). Three shapes are declined rather than widened: a void-only body stays **ADR 0429**'s
  static fold, a one-kind method declares no word and keeps every road it had (asserted from the IR), and an
  answer compared against ANOTHER answer is declined by the new door and left to `comparePair` — which answers
  `f(1) == g(1)` across two bodies with the reference's bytes, and gets wrong the one shape this round's
  word-sharing exposes: TWO calls of the SAME carried body in one expression, where one kind word says whichever
  ran last (**Gap R.460**). `pkg/lang/method_answer_test.go` NEW (**31** answered programs on the compiled leg AND
  the record leg, **9** direct plan rows in both directions, **5** IR rows, **6** owed rows) and
  `integration/method_answer_test.go` NEW (**30** CLI programs re-derived from a live `python3` in the case,
  **1** machine-readable JSON refusal, **6** owed rows). A **31**-program battery beside a `git worktree` build of
  the parent (`d493ce8`, **ADR 0429**) moved **26** to the reference and held **5** byte-for-byte; across both
  suites' **46** deduplicated sources — the answered families AND both owed tables — **34** moved, **6** held, the
  **6** owed rows were wrong or refused identically on both builds, and NOTHING moved away.
  `tools/recmerge` wrote **40** entries from the reference with the compiled leg cross-checked
  (**9706 → 9747**, none refused). Three pins that stood in the way are **DELETED rather than edited** and stand
  in the answered families, and what the round leaves owed is filed with its bytes: **Gap R.458**
  (`sum`/`max`/`min` over a container the answer hands back — refused for a module `def` too, so one door and one
  row), **Gap R.459** (a BOUND method answer, `ys = c.m()`, cannot be measured or subscripted the way
  `ys = build()` can), **Gap R.460** (the kind word is ONE per carried body, not per call — `print(f(1) == f(0))`
  prints `True` where the reference prints `False`), and **Gap R.461** (a mixed container arm compared with a
  container literal refuses, in a sentence that also names `*lang.Call` to the program — **Gap R.116**'s class).
- **A method that FORWARDS a call to another METHOD answers the callee's own word (Gap R.454, **ADR 0431**)** —
  `class C():` / `    def m(self):` / `        return None` / `    def n(self):` / `        return self.m()` /
  `c = C()` / `print(c.n() is None, c.n())` printed **`False 0`** at **exit 0** where the reference prints
  `True None`, while the identical body forwarding a module `def` (`return f()`) had printed `True` since
  **ADR 0428**: `answerArmOf`'s `*Call` arm read `v.Fn.(*Name)` and returned `ok=false` for an attribute callee,
  ONE un-nameable arm retires a WHOLE body, and the body's `int` slot then legally carries the void's payload —
  the word `0` (**ADR 0187**), which the number printer legally prints. The arm grew the attribute road beside its
  name one, and it answers the CALLEE'S OWN WORD out of the two tables the METHOD round keeps
  (`methodVoid`, `methodCarry`) rather than out of the callee's body — reading the body would follow
  `def n(self):` / `    return self.m()` over `def m(self):` / `    return self.n()` around forever, and a round
  reads a table. The callee is resolved through the plan's new `classTable` (own declarations, then every base in
  the order the dispatch walks them, with **ADR 0299**'s rule applied by the walk that fills it: a duplicated class
  answers nothing, and a name a class declares twice resolves to no body), and the forwarding body carries the
  callee's CONTAINER kind with it in the new `answerArm.fwdKind`, which is what moved `print(c.n())` from `1` to
  `[1, 2]`, `len(c.n())` from a refusal to `2`, and `1 in c.n()` / `for v in c.n():` from declines to answers.
  The round had to become TWO verdicts first: a void-only body is never carried, so an arm reading `methodCarry`
  alone would have named the carried callee and declined the identical call over a void one — **ADR 0232**'s two
  answers to one question, and it would have stranded the exact program **Gap R.452** was paid for; and
  `methodAnswersOnlyTheVoid` and the new arm now ask ONE predicate (`bodyAnswersTheVoid`), which is what
  `answersOnlyTheVoid` already answered a module `def` by. Three declines, each pinned: a receiver the body's own
  spelling cannot name, a call forwarded to ITSELF (**ADR 0273**'s exclusion), and a class the plan never
  registered. `pkg/lang/method_forward_test.go` (**39** answered programs on the compiled and record legs, **20**
  direct predicate rows, **4** IR rows, **7** pins) and `integration/method_forward_test.go` (**39** CLI programs
  re-derived from a live `python3`, **1** machine-readable JSON refusal, **8** pins) are NEW; across both suites'
  **68** deduplicated sources, measured beside a `git worktree` build of the parent (`68a327d`, **ADR 0430**),
  **30** moved to the reference, **38** held and NOTHING moved away; `tools/recmerge` wrote **39** entries from the
  reference with the compiled leg cross-checked (**9747 → 9786**). What the arm leaves owed is filed with its
  bytes: **Gap R.462** (forwarding a call to a method answering ONE text prints the payload, though the direct
  call answers it), **Gap R.463** (a module `def` forwarding a call through a PARAMETER), **Gap R.464** (a
  forwarded container answer subscripted or read by key, where the same object prints, measures and walks),
  **Gap R.465** (a base chain that names itself overflows the COMPILER's own stack in `resolveMethod` — byte-identical
  on the parent, and **ADR 0166**'s exit 2 rather than a refusal), and **Gap R.466** (a method answering ONE float
  answers the double's low word — found by going to pin the forwarded spelling and finding the DIRECT call already
  wrong, which is the measurement that split it out of **Gap R.462**).
- **A generator's buffer carries the kind of every element it yielded: one door writes payload and tag, and the
  doors that receive the buffer ask the slot (Gap R.468, **ADR 0433**)** — `class C():` /
  `    def m(self):` / `        yield "a"` / `        yield 2` / `        yield None` / `        yield 1.5` walked by a
  `for` printed **`0` `2` `0` `1`** at the exit code of success where the reference prints `a` `2` `None` `1.5`, and the
  module `def` twin printed those SAME four bytes — the measurement that moved the row out of **ADR 0432**'s class round
  and onto the append door. A generator's buffer was filled by `rt_append`, which takes a payload and no kind: the
  mutation road has owned the door that takes both since **ADR 0311** (`rt_append_tagged`, what `xs.append(...)` asks),
  and a slot whose tag was never written reads whatever tag the previous tenant left (**ADR 0187**/**ADR 0189**), so a
  text answered its `@str_tab` index and the void and a float answered the low half of a heap handle. `yield` now asks
  that door through ONE writer (`genBufferAppend`, `pkg/lang/codegen_genbuf.go`) that `stmtYield`, `stmtYieldFrom`'s
  literal arm and the class road all call, and the claim that a buffer must be WALKED by asking each slot is earned
  from the callee's BODY — `registerGenBuffer` records, under the mangled symbol **ADR 0224** minted, whether a body
  yielded a value whose kind the untagged road cannot render — so a `yield k` or a `yield k * 2` claims nothing, a
  nested `def`'s yields are not this buffer's, and a dynamically dispatched call claims the kinds only when EVERY arm
  earned them. The claim is deliberately narrower than the list claim it sits beside, and that narrowness is what keeps
  the row from widening anyone's refusal: a buffer of nothing but integers is still walked and summed by the roads it
  always used (`for v in c.m(): s = s + v` answers the number at exit **0**), while a buffer that yielded floats
  reaches the standing **Gap R.146** pair refusal instead of the parent's truncated **`3`**. Measured beside a `git
  worktree` build of the parent (`bb4abc9`, **ADR 0432**): across the answered family's **23** deduplicated sources
  **17** moved onto the reference's bytes and **6** held byte-for-byte — the membership row that was already right, the
  forwarded integer sum the record holds `104` for, the two integer-buffer controls, the parameter-fed buffer and the
  expression-fed buffer — which is the control that
  says the tag door opens for a KIND and not for a generator; one of the 17 moved off **exit 2** (`yield [1, 2]` /
  `yield 3` handed `llc` the container literal's `@.lst1` GLOBAL inside the `define` — **Gap R.474**'s shape — and the
  tagged door, which takes the literal's WORD, answers `[1, 2]` `3`), and the
  only program that answered on the parent and refuses here is the mixed-buffer sum, which now spends the mixed-LIST
  road's own sentence at exit **1** (the same sum over `xs = [1.5, 2.5]`, no generator in it, refuses with those bytes
  today). `pkg/lang/generator_buffer_kind_test.go` NEW (**23** answered programs on the compiled leg AND the record leg
  with a MISSING record failing the case, **9** method-vs-module rows that may differ only by a deliberate refusal
  sentence, **4** eager-print rows pinned as this backend's model rather than the reference's, **20** reading rows
  asking `generatorBodyWritesKinds`/`methodIsGenerator` directly with their own controls, **3** IR rows plus a negative
  control that fails if a method which never yields carries `%gh` or `rt_append`, **8** owed rows) and
  `integration/generator_buffer_kind_test.go` NEW (**23** CLI programs re-derived from a live `python3`, **1** record
  audit, **4** eager-print rows, **3** exit-code rows with exit **2** forbidden, **5** owed rows, **1** corpus check)
  with `integration/programs/generator_buffer_kind.gy` NEW, byte-identical to CPython on both legs; `tools/recmerge`
  wrote **21** entries from the reference with the compiled leg cross-checked (**9814 → 9835**, none refused); the
  owed, eager and exit-code tables were swept too — **5** moved (three eager-print rows off `[0, 2, 0, 1]` and `[1, 0]`, the
  exit-code table's own row program, and the mixed-buffer sum) and **10** held byte-for-byte, which is what keeps every filed-not-fixed pin another row's lesion.
  What the
  round leaves owed is filed with its bytes: **Gap R.477** (a comprehension over a bound buffer, `str()` of one and an
  interpolation field of one still read `mixedLists`/`listOperands` and never the buffer claim — `[0, 2, 0]` and `1`
  where the same values as a list literal print `['a', 2, None]`), **Gap R.478** (the membership test compares
  payloads, so `0 in c.m()` is `True` where the reference says `False` — and `0 in ["a", 2]` says the same thing, which
  is what makes the door the owner and not the generator), plus the rows **ADR 0432** filed and this one did not reach:
  **Gap R.467**/**Gap R.471** (a dynamic join whose arms disagree about kinds), **Gap R.475** (a `yield from` of a
  parameter's list copies words) and **Gap R.474** (a container literal in a method's operand list, `llc` at exit 2).

- **A method call fills the parameter vector the METHOD declared: one word per parameter, defaults and names read
  from the declaration (Gap R.472, **ADR 0434**)** — `class C():` / `    def m(self, a, b=2):` / `        return b` /
  `print(c.m(5))` printed **`0`** and `class D():` / `    def m(self, a=1, b=2):` / `        return a * 10 + b` called
  as `D().m(b=7, a=3)` printed **`73`**, both at the exit code of success, where the reference prints `2` and `37`. The
  callee reads its parameters by position (`emitClassMethod` declares `i32 %self` plus one word per `fd.Params` entry
  and binds `fd.Params[i].Name` to `%p<i>`), and five roads built their operand list from `for i, arg := range c.Args`
  — the static-receiver dispatch, `super()`, the run-time class-id switch, `mod.fn(...)` and `C(...)`'s constructor
  call — so an omitted default read whatever the allocator left in the slot and a keyword was a positional with a
  label on it. `llc-20` and `opt -passes=verify` both ACCEPT the under-filled call (`--verify-llvm` says `module
  verified` over the whole family), which is why the only witness any of these had was the program's own output. The
  module-`def` road had done the walk over the declared parameters since **ADR 0166**; the method roads are separate
  roads and had never been asked. One binder (`pkg/lang/codegen_args.go`) now answers the one question — positionals
  after the caller's own receiver slot, `name=` bound by name against the callee's parameters, every omitted slot
  filled from `fd.Params[i].Default` through the SAME `g.value` road a written argument takes, so a default can only
  ever travel a word the road was already able to emit — and it is asked at all five doors. The refusal sentences are
  the `def` road's reused verbatim (`too many arguments for C.m`, `missing argument "b" for C.m`, `unknown keyword
  argument "k" for C.m`, `multiple values for argument "b" of C.m`, `positional argument after keyword argument for
  C.m`; `self=` is refused for free, slot 0 being the caller's), so one mis-shaped call says the same thing about
  itself whichever door it came to, at exit **1** beside the reference's own `TypeError`. The declaration is read by
  the walk that minted the symbol (`classInfo.methodDefs` + `resolveMethodDef`, the same base chain `resolveMethod`
  walks per **ADR 0224**), so an inherited method keeps its base's defaults and an override brings its own; the
  run-time class-id switch builds EVERY arm's vector before it writes the `switch`, from one binder, which is how
  `4`/`6` from two arms with two different defaults became an answered row instead of a register defined in the block
  that did not run (**ADR 0227**, **Gap R.318**). Measured beside a `git worktree` build of the parent (`bf03c37`,
  **ADR 0432**): across **25** deduplicated sources **20 moved** and **5 held** byte-for-byte, and **17** of the moves
  landed on the reference's exact bytes — eleven value programs, the corpus program, and five shapes that moved off a
  wrong answer at exit **0** onto a refusal at exit **1**. Three moved and did not arrive, each named for its row:
  **Gap R.479** (the defaulted text: `def m(self, s="hi"): return s` prints `0` where the SAME body as a module `def`
  prints `hi` — the kind censuses are keyed on the arguments a call WROTE, which is the second door behind this
  first one), **Gap R.35** (the defaulted float, which is the method's missing double return word — the written
  spelling prints the same `4`), and **Gap R.474** (the container default, whose `i32 @.lst1` in an operand position
  is that row's `llc` rejection reached a second way). One pin that belonged to another row went paid and MOVED rather
  than vanished: `pkg/lang/method_text_test.go` claimed the reference prints `hi` for a constructor whose required
  parameter the program never passes — CPython raises `TypeError: C.__init__() missing 1 required positional
  argument: 'v'` and exits **1**; that `hi` was the retired engine's record, and the row moved to the refusal table
  (**ADR 0302**'s rule, asked of a column that had never been asked). `pkg/lang/method_args_test.go` NEW (**11**
  answered programs on the compiled leg AND a live `python3` in the same case, **1** record-leg row for the shape
  CPython's own parser refuses, **9** reading rows asking `callArgs` DIRECTLY over hand-built `[]*Param`/`[]Expr`, **5**
  owed rows, **5** refusal rows) and `integration/method_args_test.go` NEW (**15** CLI programs re-derived from a live
  `python3`, **1** record audit, **6** exit-code rows with exit **2** forbidden, **1** assembler-rejection pin, **5**
  owed rows, **1** corpus check), with `integration/programs/method_arg_defaults.gy` NEW (matrix `match`, 20 lines
  byte-identical to CPython); `tools/recmerge` wrote **16** entries from the reference with the compiled leg
  cross-checked (**9835 → 9851**, none refused), and the two **ADR 0432** pins that held this row open are DELETED
  rather than edited.

- **A parameter a method call OMITS arrives with the KIND its default states: the text census reads the word the
  vector wrote (Gap R.479, **ADR 0435**)** — `class C():` / `    def m(self, s="hi"):` / `        return s` /
  `print(C().m())` printed **`0`**, `print(f"hi {who}")` over `def greet(self, who="world")` printed **`hi 0`**, and
  `return "hi " + name` refused as arithmetic on a number, where the reference — and the SAME body written as a module
  `def`, on the same build — print `hi`, `hi world` and `hi world`. `0` and `hi 0` are the `@str_tab` POSITION, the
  artifact **ADR 0366** was written to remove from the WRITTEN-argument spelling, surviving at the one door **ADR 0434**
  had just opened: `strArgKinds` collected its evidence by walking call sites and reading the arguments a call WROTE,
  and it carried two deliberate suppressions for a defaulted method slot — `seed()`'s `byDefault` skip and
  `w.attrCall`'s defaulted-tail `negOf` — both written when that slot really was never written, which was **Gap R.315**'s
  SIGSEGV. Once the vector fills it, the default's word IS in the slot, so the parameter earns the claim its shape
  gives it, seeded exactly as a module `def`'s defaulted parameter has been since **ADR 0166**; the negative-evidence
  roads were left untouched, which is what keeps the claim evidence rather than a guess about spelling (a `1` default
  earns nothing, an undefaulted parameter no call names earns nothing, and `C().m(5)` still argues the slot is not a
  text, printing `5`). Measured beside a `git worktree` build of the parent (`88d7da6`, **ADR 0434**): across **63**
  deduplicated sources **7 moved, 56 held byte-identical, and all 7 arrived at the reference's bytes exactly**. The
  row's other two halves turned out not to be defaults questions at all, which is what their controls measured: the
  container pair (`len(xs)` refusing, `return xs` reaching `llc`) answers the same whether the argument is WRITTEN or
  defaulted, because `emitClassMethod` has no `declareHeapParams` door — the module `def` road's private privilege —
  filed **Gap R.480**; and the defaulted float's `4` for `5.0` is the method's missing double return word
  (**Gap R.35** / **L11.6**). Two more shapes came out of the sweep and are filed, not absorbed: **Gap R.481** (one
  slot carries ONE claim, so the number one call writes un-marks the text the call that omits needs — `5` then `0` for
  the reference's `5` then `hi`, and its sibling refusing with **ADR 0366**'s sentence) and **Gap R.482** (a method name
  two classes share drops the whole claim under **ADR 0366**'s ambiguity rule, printing the intern position at exit 0 —
  the source `tools/recmerge` REFUSED to record, which is the cross-check working). `pkg/lang/method_args_test.go` grows
  to **19** answered programs on both legs, **7** rows asking `strArgKinds` DIRECTLY and **5** owed rows;
  `pkg/lang/method_text_test.go`'s `class C:` concatenation moves from the refusal table to the answered one; the CLI
  family grows to **22** programs each re-derived from a live `python3` inside the case, with **4** owed rows;
  `integration/programs/method_default_kinds.gy` NEW answers **14** lines byte-identical to CPython; and
  `tools/recmerge` wrote **27** entries with the compiled leg cross-checked (**9851 → 9878**, two sources refused).
- **A generator METHOD is a generator: the reading is the body's own, taken at the class's door (Gap R.455, **ADR 0432**)** —
  `class C():` / `    def m(self):` / `        yield 1` / `c = C()` / `print(c.m() is None)` died at **exit 1** with
  **`yield outside a generator function`**, while `def g():` / `    yield 1` / `for v in g():` / `    print(v)` printed
  `1` on the same build: the generator reading was a whole-program scan over `prog.Stmts` that never entered a
  `ClassDef`, so a `def` written inside a class body was a method by POSITION only, and the refusal blamed the program
  for the position its body was written in — the one thing an honest refusal may not do (**ADR 0166**). The reading now
  lives where a class's methods are registered: `registerClass` asks `containsYield` — the ONE question `funcDef` asks
  a module `def` (**ADR 0232**) — and records the verdict under the MANGLED symbol `resolveMethod` hands a call site
  (**Gap R.42**/**ADR 0224**'s key), and `emitClassMethod` allocates the buffer in the callee's frame, rooted like the
  `def` road's (**ADR 0181**), answering it at the ordinary exit AND the unwind edge. The widening that actually cost
  something was the set of positions that RECEIVE the answer: the print door, the `in` door, a binding and the `for`
  road each asked "is this register a list?" through a road that knew only the module spelling (`call.Fn.(*Name)`), so
  a method generator fell through the `for` road's dispatch to the numeric-range arm and walked a counter while
  printing the loop variable — `for v in c.m():` printed `0 1` for `1 2` at the exit code of success. One predicate
  (`callIsGeneratorCall`) now answers both spellings, and the doors ask it. Two shapes REFUSE rather than answer, each
  a registered capability code: a dynamically dispatched SPLIT yield census (**Gap R.467**) and a generator body that
  walks its own parameter (**Gap R.475**, where the module twin answers `[1, 2]` and the method arm would otherwise
  have answered the empty buffer). `pkg/lang/generator_method_test.go` (**26** answered programs on the compiled and
  record legs, **10** direct reading rows beside `containsYield` with their own controls, **4** IR rows asserting the
  buffer allocated, appended to twice and answered at both exits — and asserted ABSENT from a method that does not
  yield, **13** owed rows) and `integration/generator_method_test.go` (**24** CLI programs re-derived from a live
  `python3`, **1** record audit, **2** machine-readable JSON refusals, **3** exit-2 pins, **8** owed rows) are NEW, and
  so is `integration/programs/generator_method.gy`, `match` on all three legs across 23 lines byte-identical to
  CPython. Two void-method pins stood in the way and are **DELETED rather than edited** into the answered families.
  Measured beside a `git worktree` build of the parent (`d729564`, **ADR 0431**): across **22** deduplicated sources
  **18** moved and every one of them moved OFF the `yield outside a generator function` refusal, **4** held (the two
  programs **ADR 0429**/**ADR 0431** answered print byte-identical bytes; `len` of a generator and `bool` of a
  container refuse with the same sentences), and NOTHING that ran on the parent now refuses; `tools/recmerge` wrote
  **28** entries from the reference with the compiled leg cross-checked (**9786 → 9814**, none refused). What the round
  leaves owed is filed with its bytes: **Gap R.467** (a split yield census at the dynamic door), **Gap R.468** (a
  yielded text, float and `None` keep no kind — `rt_append` where the mutation road asks `rt_append_tagged`, and the
  module twin prints the same four bytes), **Gap R.469** (`return` after the yields answers the void, and the walk runs
  nothing), **Gap R.470** (a `yield` that lives ONLY in a `try`/`except`/`finally` handler is not read — while the same
  handler `yield` lowers fine once any other statement makes the body a generator), **Gap R.471** (a dynamically
  dispatched container-returning method walks a counter, no `yield` anywhere, byte-identical on the parent),
  **Gap R.472** (a method parameter the call omits answers `0`, not its default), **Gap R.473** (the truth of a
  generator's answer is the heap word the buffer arrived in, so the module door answers `no` and the instance door
  `True` for the same program), **Gap R.474** (a container literal handed to a method — or stored in an `__init__` —
  has its global emitted inside the `define`, `llc` rejects it, **exit 2**, byte-identical on the parent's yieldless
  twin), and **Gap R.476** (a method that FORWARDS a generator call answers a word that is not the buffer, because
  **ADR 0431**'s forwarding arm reads two tables that have no generator row).

- **A body that FORWARDS the void answers the void (Gap R.451, **ADR 0428**)** — `def f():` /
  `    return None` / `def g():` / `    return f()` / `print(g() is None)` printed **`False`** at **exit 0**
  where the reference prints `True`, and the line that makes it a defect rather than a cosmetic gap printed
  **`True`** for `print(g() == 0)`: the fold was comparing the void's payload — the word `0`, which an `int`
  slot legally carries (**ADR 0187**) — while the printer beside it wrote `None` (**ADR 0232**'s two doors,
  one value). **ADR 0427** had connected the fold to the answer plan, and the verdict stopped at the callee's
  own name: `answerArmOf` admits a `return other(...)` arm only once `other` is CARRIED (**ADR 0368**'s
  fixed-point rule, so an inner frame writes its kind word before an outer caller reads it), and a void-only
  callee is served by the PAIR road and never carried — so `g`'s arm declined, `answerBodyPlan` declines a body
  with any un-nameable arm, and `g` kept the roads **Gap R.388** was filed for. The fix is the row's own
  prescription, in the arm: ask `answersOnlyTheVoid` BEFORE the carried-only rule and answer the CONSTANT
  `answerWordNone` instead of forwarding a word, because a body whose every road answers `None` has no word for
  the outer frame to wait on. The call survives — `@gy_g` carries exactly one `call i32 @gy_f()`, a folded
  comparison still calls `@gy_g` once, so `def g():` / `    print("side")` / `    return f()` prints `side`
  (**ADR 0172**) — the self-call exclusion holds (**ADR 0273**), and a body forwarding a number, a text, a
  verdict or a container is a control, asserted from its module to store no answer word at all. Two shapes the
  parent REFUSED with *"answers …(), whose own answer carries a kind word this door declined"* now print the
  reference's `True`: `f = lambda: None` above `def g(): return f()`, and the mutual pair `def a():` /
  `    return b()` / `def b():` / `    return None`. `pkg/lang/void_forward_test.go` NEW (**24** answered
  programs on the compiled leg AND the record leg, **10** position rows with their own controls, **8** rows
  asking `answerKindPlanOf` directly in both directions, **3** IR rows, **4** filed-not-fixed) and
  `integration/void_forward_test.go` NEW (**34** CLI programs read off a live `python3`, **6** exit-code rows,
  **1** machine-readable refusal, **3** owed pins). A **32-program battery** beside a `git worktree` build of
  the parent (`93f3404`, **ADR 0427**): **15 moved to the reference, 13 held byte-for-byte, none moved away**;
  a **9618**-source sweep of the record corpus through both builds reports **0 changed answers** — the corpus
  had no case that forwards a void answer, which is why `tools/recmerge` wrote **34** entries from the reference
  (**9618 → 9652**, none refused; the parent was right for 15 of them, wrong for **17** and refused **2**). The
  two `Gap R.451` pins MOVED from the `void_answer` owed tables into the answered families rather than vanishing,
  and what the same battery left owed is pinned with bytes: a mixed-arm callee (**Gap R.324**, standing), a
  forwarded PARAMETER (**Gap R.417**, standing) and a METHOD's body (**Gap R.452**).

- **The element a slot is WRITTEN with answers what it is: the third road that writes a container slot asks
  the ELEMENT (Gap R.441, **ADR 0422**)** — `xs = [1]` / `xs[0] = 2.5` / `print(xs)` wrote **`[1]`** at
  **exit 0** where CPython writes **`[2.5]`**, while `xs.append(2.5)` beside it wrote **`[1, 2.5]`** all
  along. Three roads write one kind of slot, and two of them had learned to ask the VALUE its kind —
  `append` since **ADR 0233**, the dict's entry store since **ADR 0232** — while `xs[i] = v` asked only
  whether the new word was text or a number. The store was NOT broken where it looked: `%t3 = call i32
  @rt_float_new(double 2.5)` boxed the float, `rt_put_elem` wrote the handle and `rt_tag_elem(…, i32 1)`
  labelled the slot — payload AND tag, **ADR 0189** kept. The missing word was the container's: 
  `rt_mark_estr(…, i32 8)`, *"my slots are the truth, ask them"*, the bit that chooses
  `rt_print_list_mixed` over `rt_print_list` — without it the printer read slot 0 as the kind the container
  still claimed and printed the float box's **handle** as the number it is. Ten shapes answered wrong on the
  parent (a fraction, `None`, `True`, `False`, a list, a dict, a set, the tuple spelling, a container under a
  NAME, a body's answer) plus the loop over a promoted list, `print(xs[0])`, `str`/`repr`, `xs[0] == None`,
  `pop`, `reverse`, a slice and `ys = xs` / `ys[0] = 2.5` / `print(xs)`. The arm is the neighbours' predicate,
  not a fourth list of kinds — `elemKindTag` + `slotTagSelfDescribing` (**ADR 0238**: float, void, verdict,
  container, instance; never `TagInt`, whose payload *is* the value, never `TagStr`, which the census asks
  about) — placed in the AGREEMENT branch, where **ADR 0232**'s str/int contradiction had nothing to say, and
  deliberately NARROWER than its door, with **ADR 0249**'s implication asserted over hand-built elements.
  **34** answered rows on the compiled leg AND the record leg, **13** controls (`append`, the dict store, the
  same-kind store, **ADR 0239**'s fold), **4** bounds rows (the out-of-range write still raises the reference's
  `IndexError: list assignment index out of range`), **14** gate/door property rows, **4** IR rows asserting
  bit+tag+mixed-printer for the promoted stores and DENYING them for the numeric control, **42** CLI rows
  against a live `python3`, and `integration/programs/item_stores_say_themselves.gy` joins the corpus as an
  asserted `match` row (**253 → 254** programs, matrix **239 → 240**, `match` **196 → 197**, fail 0 / drift 0).
  `tools/recmerge` wrote **69** entries from the reference (**9325 → 9394**) and **refused four** whose compiled
  leg disagrees, each pinned with the row that owns it. A **9394**-source sweep of the record corpus through a
  rebuilt parent reports **9352 unchanged / 40 improvements / 0 regressions**. One shape the same measurement
  surfaced is FILED with today's bytes — and the parent's wrong bytes — pinned beside the reference's:
  **Gap R.442** (a chained comparison over a promoted slot, and a numeric use of a slot a run-time index
  wrote, both refuse where the parent answered from the payload the store had left behind — `xs[0] = 5.5` /
  `print(0 < xs[0] < 3)` said `True` where CPython says `False`, which is why a refusal here is a hole being
  shown and not a promise broken).

- **A dict is sorted by its keys, and the door that copies it is where the stride is decided (Gap R.426,
  ADR 0417)** — `d = {"a": 1, "b": 2}` / `print(sorted(d))` wrote **`['a', 1]`** and
  `print(sorted(d, reverse=True))` wrote **`[1, 'a']`**, both at **exit 0**, where CPython writes `['a', 'b']` and
  `['b', 'a']`: one key and one value, dressed up as the two items of a sort over a two-entry dict. `sorted` had one
  runtime road — copy the container, sort the copy — which is right for a list (**ADR 0191**'s `sorted(xs)` leaves `xs`
  alone) and wrong for a dict, because a dict's payload array holds TWO words per entry while its length word counts
  ENTRIES; the copy read `len` words positionally. In the same program `for k in d` walked the keys correctly, and
  `list(d)` / `max(d)` / `sum(d)` all refused by their own sentences, so the lie was this door's alone. The road now asks
  its argument what it is before it copies anything, and a dict is walked by the module's one new door, `@rt_dict_keys`
  — `%w = mul i32 %i, 2`, stopped at `@rt_dict_len` — the same stride the collector asks in `rt_gc` (**ADR 0350**) and the
  printer asks in `rt_dict_print_mixed`; `@rt_sort` is handed the key copy and nothing else, asserted by reading the
  block. The comparator and the printed kind of a key come from ONE record of the keys (`dictKeyStr` / `dictKeyInt` /
  `mixedDicts`), which is what makes a dict the program grew by item stores answerable, and it took two measurements to
  get right: a single-kind dict is stored through the UNTAGGED put, so a text key's arena tag word is whatever the last
  tenant left (`xs = sorted(d)` printed the `@str_tab` indexes, `[1, 0]`), and the assignment road that gives
  `xs = sorted(...)` its element kinds looked only at list sources, so it now reads them off the dict's KEYS. Text keys,
  whole-number keys and verdict keys answer the reference's bytes; keys of more than one kind, a fractional key and a key
  no record describes are refused in words that name the missing half (**Gap R.429**), including the mixed pair where the
  reference *raises* — an ordering across kinds is not this backend's to invent (**ADR 0191**). The control family is
  where the next row came from: `sorted(<set>)` echoes the set's own insertion order, `reverse=` and all, byte-identical
  on this commit's parent, and is filed as **Gap R.430** with a pin that fails if the door starts answering by accident.
  `tools/recmerge` wrote **32** entries from the reference (**9124 → 9156**) and refused none; the corpus gains
  `integration/programs/sorted_dict_keys.gy` (matrix **235 → 236** rows, **206 → 207** passing, oracle `match`
  **192 → 193**, drift **0**). **ADR 0416** is left unissued here on purpose — the in-flight set-member change owns it.

- **The corpus has a third opinion (L11.9)** — parity between the compiled backend can be satisfied
  by two implementations that share a bug, and for a hundred ADRs it was. The conformance matrix
  runs each program on the record, the compiled binary **and CPython**, and each case
  declares its state in a ledger (`match` by default, `debt` with a reason, an owner and a pin of
  the wrong answer, or `not_applicable` for gusty-only surface). Drift fails the build in both
  directions. `gustyc --oracle '<src>'` exposes the same classifier interactively — `--json` for
  the leg-by-leg report, exit 6 when gusty disagrees with Python and 7 when the oracle could not
  judge the source (ADR 0186).
- **Benchmark suite + regression gate** — `gustyc --bench-suite` measures a
  corpus on the compiled backend and prints (or `--json`-emits) a stable artifact;
  `--bench-baseline` gates a run against a saved baseline, so "the compiler got
  slower" is a number with its own exit code (5) instead of a hunch.
  `--bench-dir integration/programs` benchmarks the parity programs too.

- **A refusal names the rule it is missing, and never offers a way to run that is gone (Gap R.288, **ADR 0437**)** —
  `d = {[1]: "x"}` / `print(d[[1]])` was refused honestly and described dishonestly: `a compiled container cannot
  hold a list literal yet; a dict keyed by a container has no hashing rule, **and** an element nested deeper than
  every inner element can be tagged would be printed by handle … Build that inner value separately, **or run it
  interpreted**`. One sentence recited both rulings whatever the position asked, and the last five words pointed at
  the engine **ADR 0302** deleted — a flag (`--interp`) whose answer is a usage error. The door is now two: the KEY
  door says `cannot hold %s as a key yet: a dict finds an entry by hashing its key, and this backend has no hashing
  rule for a container, so the entry would live under whichever handle the allocator handed out … Key the dict by a
  value that has a hash — a text, an integer, a float, a verdict or the void — and keep the container in the value`,
  the ELEMENT door says `cannot hold %s yet: a slot of a compiled container carries one word and the tag that says
  what that word is, and this element nests deeper than any tag the compiler writes …`, and the element door walks
  into what it declines, so `print([{{[1]: 2}}])` blames `[1]` rather than the tag that was never the problem. The
  comprehension road that reached the same door used to wrap its error in a second `codegen: ` — `gustyc: jit:
  codegen: codegen: a compiled container …` at `4fdb87c` — and that is fixed too, because a doubled prefix is a
  sentence about the toolchain that is not true. The machine path moved with the prose: **two codes** where there
  were none, `lower.unsupported.container-key` and `lower.unsupported.container-element`, so a script branches on
  the shape and not on the wording. What makes the promise checkable is `pkg/lang/witness_emitter_test.go`, which
  reads the string literals of every non-test file in `pkg/lang` and `cmd/gustyc` — the corpus the document-side
  witness guard never opened (**Gap R.193**) — against the phrase lists in `pkg/lang/testdata/`, with exemptions in
  a ledger that fails if a row matches no live string, and the ledger is EMPTY. Grepping that corpus for the first
  time found two more sentences still describing two engines: `semantic.go`'s builtin-shadow diagnostic, and
  `schema.go`'s `valueTag` description, which used to credit its tag table to the retired engine's heap objects —
  the JSON an agent reads to learn the ABI. Eight control sources (hashable keys at every depth, three depths of nested
  literal elements, a dict in a list, a comprehension over a literal of containers) went to the record — **9878 →
  9886** — and `tools/recmerge` refused the refusals, which have no answer to cross-check; the four shapes the same
  sweep found wrong are **Gaps R.485**–**R.488**, filed with today's bytes at both legs and unfixed.

- **A literal element's kind is asked ONCE, from the tag road: a container the program NAMED travels tagged, and a
  loop item is its item, not its name's history (Gaps R.486, R.439, R.489, **ADR 0438**)** —
  `xs = [1]` / `print([xs, 2])` printed **`[0, 2]`** and `sa = {1: "a", "b": 2}` / `print([sa, 1])` printed
  **`[1, 1]`**, both at the exit code that means the program ran, because a container slot is a pair (a word and the
  tag that says what the word is) and the two halves were asked of two different things. The word and the tag were
  right — `heapElemKind` stored the handle and `elemKindTag` wrote `TagList` beside it — while the object's header,
  written by `literalNeedsTags`, asked the element's SPELLING and stopped at `isContainerLiteral`; a name has no
  spelling, so `rt_print_list` (the single-kind printer) printed the handle as the integer the allocator handed out.
  The census now asks `elemKindTag`, the same question the tag asks — **ADR 0232**'s "the slots the builder wrote are
  the truth", and **ADR 0359**'s fix for the interpolation element applied to the container element — so the object
  says its slots are the truth (`rt_mark_estr(o, 8)`) and `rt_print_list_mixed` walks the pair: `[[1], 2]`,
  `[{1: 'a', 'b': 2}, 1]`, `[{1: 'a'}, {2: 'b'}]`, and the roads that READ the slot (`[xs, 1][0]`, `[xs] ==
  [[1, 2]]`, `for v in [xs, [3]]`, `{"a": xs}`) share the answer, which is why a print-time fix was rejected.
  Asking the tag road once also exposed the defect that had been hiding behind it: the tag's *Name arm consulted the
  container TABLES — cumulative, never unenrolled — after the iteration's record, and the iteration's record
  answers only for a kind it can name, which an integer is not. So `xs = [7]` / `for xs in [1, 2]:` /
  `print([xs, 1.5])` stored the integer 1 tagged `TagList` and printed **`[, 1.5]`**, with `print([xs] == [1])`
  answering `False` (**Gap R.489**, found by landing the census fix and fixed in the same commit, because the fix is
  unsafe without it): the tag road asks the ITEM first now, **ADR 0233**/**Gap R.358**'s per-iteration record asked
  of the name the unroll replaced. Two roads still owed, and pinned rather than papered over: a COMPREHENSION
  written as an element answers the slot (**Gap R.487**) and the same shape one level deeper hands `llc` an
  `i32 @.lst1` operand and exits **2** (**Gap R.488**). Eighteen answered sources — 12 unit (compiled leg + record
  leg + four IR rows) and the same family plus 7 through the CLI beside a live CPython — with `tools/recmerge`
  writing 19 record entries from the reference and the compiled leg cross-checked (**9886 → 9905**, none refused);
  the numeric-literal IR row asserts `print([1, 2, 3])` still takes `rt_print_list` with no census bit, which is
  what keeps **ADR 0239**'s static array, and therefore `sum`/`min`/`max`, from being paid for this.

- **A subscript standing on a comprehension the RUN TIME BUILDS asks the OBJECT for the slot (**Gap R.501**, **ADR 0444**)** —
  `def g(v):` / `    return v * 2` / `print([g(x) for x in [1, 2, 3]][1])` raised `IndexError: list index out of range`
  for the reference's `4`, and `print({g(x) for x in [1, 2]}[0])` stopped the build instead of raising the set's
  `TypeError: 'set' object is not subscriptable`. **ADR 0442** gave `len`, **ADR 0443** gave the loop, condition and
  `bool()` — the subscript was the sibling door still consulting the fold's empty table for the object a builder
  filled. The fix is the same two-comprehensions-one-door rule, with a side-effect-free deciding predicate
  (`compTakesTheRuntimeRoad`) mirroring `comp`'s own choice before the pair road may emit an instruction. Built lists
  use `rt_list_len` + `rt_get_elem` + `rt_tag_of`; built dicts use the keyed doors; negative positions, missing keys
  and the wrong key kind raise the program's own accidents; built sets raise the set's TypeError. The fold's control
  still loads `getelementptr({i32, [3 x i32]}, …)` and is asserted NOT to acquire the built road's `rt_get_elem`.
  Fourteen unit answer rows, five trap rows with both compiled/record legs, nineteen CLI rows against live `python3`, and
  `integration/programs/built_comp_subscript.gy` joined the matrix as a `match` row. The same door paid the filed
  **Gap R.491** container-of-containers row: `print([[x for x in [[1]]][0], 2])` now prints `[[1], 2]` instead of raising
  the reference’s class against a position the fold invented.

- **Membership asked of a container the SOURCE SPELLS asks the OBJECT (**Gap R.523**, **ADR 0456**)** — one literal,
  three roads, two answers: `x = 5` / `print([x])` printed `[5]`, `print(len([x]))` printed `1`, and
  `print(1 in [x])` stopped the build with `codegen: list literal elements must be integers, not int` — a sentence
  about an element kind the literal was not asking about, for a program the reference answers with `False`. The
  membership road lowered its haystack through the **number** road, whose answer for a literal is a folded `@.lstN`
  global with `i32` element slots, so a name, a call or an expression had no word to write into one. The door now
  asks the **container** door the printer asks — `containerOperand` builds the object once, `rt_contains_tagged`
  asks it, the needle arriving as a `(payload, kind)` pair — and it is hoisted above every operand lowering, the
  position **ADR 0365** fixed for exactly this reason: the ordinary lowering of the operand IS the thing that
  refuses. The folded road kept its fast path and its zero allocation, but only where it can name **both** sides,
  and that second half had never been asked of it: the unroll compares payloads alone, so `None in [0]` answered
  **`True`** (the void's word is `0`), `1 in [1.0]` answered `False`, and `1.0 in [1]`, `1.5 in [1, 2]` and
  `[1] in [1, 2]` were **exit 2** — the last in `llc`, with `global variable reference must have pointer type`
  (`%t3 = icmp eq i32 @.lst1, 1`). All five answer what the reference answers, because `rt_payload_eq` (**ADR
  0233**) is asked the pair rather than the word, and `foldNamesNeedle` reads the slot builder's own kind table so
  the fold and the door cannot disagree about which needles are which (**ADR 0232**). `x in [1, 2, 3]` still
  compiles to an `icmp` chain that allocates nothing, asserted by a row that fails if `@main` grows a builder for a
  literal the fold serves; and the unity claim is asserted too — printing `[x]` and asking membership of `[x]`
  emit the SAME `@rt_alloc(`/`@rt_set_elem(`/`@rt_tag_elem(` counts. Thirty-eight answered rows on the compiled leg
  AND the record leg (`print`, a condition, a `while` test, a `for` iterable, `len`, inside a body, a module name
  read from a body), the same rows at the CLI with every expectation read out of a live `python3` inside the case,
  two `--emit-llvm` rows, and `integration/programs/membership_literal_haystack.gy` as a `match` row (corpus 282 →
  **283**, matrix 268 → **269**, parity-asserted 232 → **233**, oracle `match` 218 → **219**); **43 record entries
  (10386 → 10459)** from `tools/recmerge`, reference-answered with the compiled leg cross-checked, eight of them the
  control rows `pkg/lang/membership_name_test.go` had pinned as this row's refusals — that table is written to fail
  when a pinned refusal starts compiling, so a paid row moves rather than vanishing — and a recorder
  (`GUSTY_RECORD_MEMBERSHIP_LITERAL=<path>`) that dumps this row's own sources so a case cannot ship without a
  record under it (**ADR 0302**). The door also took **Gap R.478**'s literal half on the way out:
  `print(0 in ["a", 2])` printed **`True`** — the integer `0` matching the interned index of `"a"` with nobody asked
  the tag — at exit 0, pinned as a known-bad answer in `pkg/lang/generator_buffer_kind_test.go` and its CLI twin; it
  prints the reference's `False` now and those rows moved into the answered family, leaving the GENERATOR's own arm
  (`print(0 in c.m())`, still `True`) as that row's remaining half — one question, two doors, and only one of them
  still wrong. The same cycle found a hole in the record leg itself (**Gap R.532**, paid here): **ADR 0302**'s rule
  that a missing record FAILS a case only bites where a case asks, and the only case that ever asked a whole
  `integration/programs/` file for its record was the garbage-collector case — so **24** asserted corpus programs, one
  of them shipped by **ADR 0455** in a docs-only commit, had no entry at all. A guard over the registry now asks the
  question directly (`integration/corpus_record_test.go::TestEveryAssertedCorpusProgramIsOnTheRecord`), exempting
  only what the ledger itself declares `not_applicable`, and 32 corpus entries went into the record with the compiled
  leg cross-checked. What the door declines it does not answer: an element whose kind no table records
  (`math.sqrt(4)`, `lambda: 1` — the latter refused with a message that ends in `not ` and names nothing) still
  refuses, filed as **Gap R.531** and pinned with its own words in the tables that fail when a shape starts
  compiling.

- **A container literal's ELEMENT that answers with a container asks the door that builds the answer (**Gap R.530**,
  **ADR 0457**)** — the element one spelling further in from the row above, and the last place a literal was decided
  five ways at once: `def f(): return [1, 2]` / `print([1, 2] in [f()])` died with
  `codegen: list literal elements must be integers, not list[int]` while `print(len([f()]))` — the same literal, one
  door away — had always compiled. The five DECIDING doors (`literalNeedsHeap`, `literalNeedsTags`,
  `nestedContainerElem`, `elemKindTag`, `taggableNestedElem`) had arms for a comprehension (**ADR 0192**), a pair
  (**ADR 0226**) and a bound container (**ADR 0438**), and none for a call; the BUILD door had had that arm since
  **ADR 0333**. All five now take their permission from one predicate, which reads the callee's kind off the
  whole-program answer scan, asks the carriable-argument question the build asks, and refuses to label a slot whose
  contents it cannot see — so `print([f()])` is `[[1, 2]]`, `print([f(), 3])` is `[[1, 2], 3]`,
  `print([1, 2] in [f()])` is `True`, `print([f()] == [[1, 2]])` is `True`, and a method's answer
  (`print([m.f()])`) is `[[1, 2]]`, on the compiled leg, the record leg and against a live `python3`. Three shapes
  were implemented, measured against the reference, and pulled back by name rather than left in: a tuple answer
  printed **`[1]`** for `[(1, 2)]` (**Gap R.533**), a forwarded answer and an applied `lambda`'s answer each printed
  `[` until the process died (**Gap R.533**, **Gap R.536**), and a `def` nested in a `def` still reaches `llc` with
  `ret i32 @.lst1` (**Gap R.535**, re-measured on the parent commit's binary so it is filed, not confessed). The
  cheap case stayed cheap — `x in [1, 2, 3]` emits no allocation, no `@rt_set_elem`, no `@rt_tag_elem` and no
  `@rt_contains`, asserted by a row that fails if `@main` grows a builder — and the unity claim is a count, not a
  reading: `[f()]` printed and `[f()]` asked for membership emit the same builder counts and ask the tagged scan
  once. Eight rows that four files had pinned as **Gap R.67**'s refusals moved into the answered family instead of
  being deleted, and **23 record entries (10482 → 10505)** went in from the reference with the compiled leg
  cross-checked, through the row's own recorder (`GUSTY_RECORD_CONTAINER_ELEMENT=<path>`).

- **A container the run time BUILDS and no statement named is given a NAME, so the roads that read a container variable read it too (**Gap R.235**, **ADR 0459**)** — `xs = [1.5, "a", [7]]` / `print(xs[0:2][0])` stopped the build with `index requires an inline list/dict/set literal` while `ys = xs[0:2]` / `print(ys[0])` answered `1.5`: one object, two doors, and the road chosen by whether the program happened to spell a name. `print(m()["k"][0:1])`, `print(d[0][0:1])` and `print([x for x in [1, 2, 3]][0:2])` were refused for want of a record. `pkg/lang/container_copy_read.go` asks the object's own builder for the handle, roots it and writes the notebooks the binding writes, and the caller lowers the rewritten node — the bounds raise, the tagged read, the pop pair and the container printer answer the direct shape **without being told anything exists**. Claims are imported from whoever wrote the object and **declined by name** where no builder made one (**Gap R.539**: a copy of a brace whose loop copies a mixed container; **Gap R.540**: a copy taken from a name whose last binding was a slot read). Two measurements the row carries rather than asserts: an untagged copy entering a one-word position printed the SUM of two `@str_tab` indices (`print(xs[0:2][0] + xs[0:2][1])` answered `1` at **exit 0**), so that shape is taken only on a proof that every slot's one word is the value; and the copy is live in a **bare register** while the statement goes on to CALL, so `print(m()["k"][0:1], m()["k"][1:2])` printed `['k'] ['a']` where the reference prints `[1.5] ['a']` — **Gap R.541**, paid by `gcRootSliceCopy` and asserted of the emitted module, because a GC window is not a fact a run can witness. **24** sources came in from the reference with the compiled leg cross-checked (record **10569 → 10594**), `programs/slice_answer.gy` carries the direct shapes line-for-line, and the capability table's `lower.unsupported.slice-base-not-described` sample moved onto a program the toolchain still refuses.
- **An applied `lambda` that reads a module literal IS that literal, and the kind travels by folding the read (**Gap R.331**, **Gap R.333**, **ADR 0461**)** — `t = "hey"` / `print((lambda: t)())` wrote the intern table's index — the digit **`0`** — at the **exit code of success**, where the reference writes `hey`, and `b = True` / `print((lambda: b)())` wrote **`1`** where it writes `True`; the same two programs written as a `def` (`def f(): return t`) had answered correctly all along, and that control is the whole row. The read was never the defect: the module road carries an interned text through one `i32` exactly as **ADR 0227** designed it, and **ADR 0368**'s answer direction names a kind only out of a SPELLING — `return "hey"` spells one, `return t` spells nothing. So the fix is a **fold and not a registry**: `foldAppliedLambdaModuleReads` rewrites the body of `(lambda: NAME)()` into the literal the module bound, once, to a name it never rebinding and never declared `global`, at the head of **ADR 0401**'s pass and before the candidate walk, the container-answer scan and the text-return scan (**ADR 0345**'s rule — a rewrite that arrives after a whole-program read is a second opinion about a program already answered). It asks **one** table, `moduleEnvFor`, the same one `codegen_value.go` asks when a compiled body reads a module name, so fold and read cannot disagree (**ADR 0294**), and it installs a fresh node with the READ's span, because two emitters walking one AST node is how a per-node cache starts answering two programs at once. Once the body spells its kind the answer goes where a spelled answer already goes — **ADR 0402**'s arm hoists text, verdict and void bodies to a declaration and the eleven doors that read a `def`'s answer read this one, with no door taught anything new. The row's own `Next concrete action` said to *register* the missing kinds; that road was considered and refused on **ADR 0368**'s terms, because a kind table keyed by a name the program never spelled is **Gap R.219** waiting to happen. Containers are excluded by the table itself — a fold there would mint a fresh object per read where the reference shares one — so a module FLOAT and a module CONTAINER keep their honest refusals, and **Gap R.333**'s missing `@rt_str_intern2` definition went with the fold as a consequence (the road that emits it is now the `def` road, which asks for the runtime block; its exit-code half stays owed). The machine path grew beside the language: `gustyc --lambda-answers` reports `reads_folded`/`folded_reads[]` under `definitions.lambdaAnswerFold`, a claim separate from `rewritten` and `declarations`, because a fold that buys no hoist still rewrote the program — `NUM = 7` folds and declares nothing. **23 record entries** came in from the reference with the compiled leg cross-checked (record **10626 → 10649**), `programs/applied_lambda_module_read.gy` is a new `match` row (corpus 284 → 285, matrix 270 → 271, parity-asserted 234 → 235, oracle `match` 220 → 221), and three defects the fold surfaced are filed rather than folded in: **Gap R.546** (a name the module rebinds — still `0` at exit 0 on the CLI, `llc`'s `use of undefined value '@rt_str_intern2'` on the other leg), **Gap R.547** (a `def` whose `return` READS a module `None` prints `0`, where `return None` has printed `None` since **Gap R.228**), and **Gap R.548** (`print({"k": (lambda: 7)()}, (1,))` writes `{1: 7}` — byte-identical on this row's parent, and the reason the corpus row does not contain it).
- **The COUNT door performs the element whose answer IS a container, and drops the handle it cannot keep (**Gap R.534**, **ADR 0460**)** — `def probe(): print("built")` / `return [1]` / `print(len([probe()]))` wrote `1` where the reference writes `built` then `1`, and the same door answered `1` at the **exit code of success** to a program whose counted element raised `ValueError` — the raise, the print and the element itself were all skipped, quietly, and the program's own `except ValueError` printed `no trap`. The gate was right and the door under it was wrong: `literalElementAsksToBeRun` answers every non-spelling with `default: return true`, so a call element was already `true` and the pass was already entering the literal — but `lowerLiteralElementForEffect` had arms for the four container spellings and none for a call, so the element fell to the ONE-WORD value road, which refuses a container answer (**Gap R.67**), and the caller, keeping its own promise to commit nothing of a literal whose elements did not all lower, answered the count from the spelling and left the program around it unperformed. The arm now asks the compiler's own answer scan, and a callee the scan names is performed by the print road's door (**ADR 0333**) into a buffer of its own that is copied into the pass's builder only on an answer, with the handle dropped where it dies — a position that keeps no word has no reason to refuse a pair, and no root either, because `rt_gc` frees what nothing holds. The exactly-once claim is read out of the MODULE and not only the stdout, because an element lowered by both the trial and the real road prints twice while still answering `2`; the fold's free answers (`len([1, 2, 3])`, `len([])`) are pinned as emitting no builder, no allocation and no call. **The pair road was built for the two shapes that stay wrong and refused by measurement**: `len([f(k())])` still loses `k`'s line (**Gap R.542**), and pushing its argument through `pairCallPair` spent **ADR 0166**'s exit 2 on `def g(): return 2.5` / `print(len([f(g())]))` — `call i32 @gy_f(i32 %t10)` over `%t10 = fadd double 0.0, 2.5` (**Gap R.543**) — so the gate keeps the pair road out and the shapes stay pinned with their bytes, beside **Gap R.544** (the `str`/`+` roads lower their argument twice, so a counted element is performed twice or four times) and **Gap R.545** (the element door that leaves a slot holding the object being built, so `def f(): return [1]` / `def g(): return [1]` / `print([g()])` writes `[` until the run is killed where CPython prints `[[1]]` — pre-existing, and identical on the parent commit's own binary). **32 record entries** came in from the reference with the compiled leg cross-checked (record **10594 → 10626**), and `programs/count_answer_effect.gy` is a new `match` row of the conformance matrix (corpus 283 → 284, matrix 269 → 270, parity-asserted 233 → 234, oracle `match` 219 → 220).
- **A method run on the container a CALL hands back is dispatched on the name the answer was recorded under
  (**Gap R.229**, **ADR 0458**)** — the last of the three doors **ADR 0333**'s answer owed: `print(b())`,
  `str(b())`, `if b():`, `for v in b():` and `ys = b()` had all read the object for eight rows, and
  `print(b().append(3))` still stopped the build with **Gap I.2**'s sentence
  `string-method append on a receiver that is not a text the compiler can read: b(...)`, while the bound
  spelling `ys = b()` / `ys.append(3)` printed `None`. The road was being chosen from the *spelling* of a
  receiver that has no shape the method roads recognise. The fix is **ADR 0334**'s judgement, not a second
  reader: materialise the answer through the door a print already uses, copy the call with its receiver
  replaced by that rooted name, and let the position lower the copy — so the mutator road, the bounds raise,
  the set's hash, the pop door and the container printer answer both spellings at once. The ROAD decides
  which methods it serves (asked in a scratch buffer, committed only on an answer), which is what keeps a
  refusal the road's own and keeps it naming `d(...)` rather than the door's invented `__cans9`; a method
  that reads a slot back is taken only on the pop door's own shape proof, because the first build of this
  door printed `1` — a text's `@str_tab` index — where the reference prints `b`. `b().append(3)`,
  `b().pop()`, `b().pop(i)`, `b().sort()`, `b().reverse()`, `s().add()`, `s().discard()`, `s().remove()` and
  `s().clear()` now answer the reference's bytes, their raises are catchable, the tuple's mutator keeps its
  `AttributeError`, and each call answers a fresh object. Two neighbours are filed rather than hidden:
  **Gap R.537** (the void a mutator answers reaches `llc` at a condition or a binding — `if xs.append(3):`
  dies identically for a plain variable) and **Gap R.538** (the reader methods, which the container roads
  refuse for a *variable* too). **64** record entries (10505 → 10569) came in from the reference with the
  compiled leg cross-checked and none refused.

- **A container that TRAVELS through an unpacking keeps its claims (**Gap R.527**, **ADR 0455**)** — `xs = [1, 2]` /
  `a, b = xs, 5` / `print(a)` printed **`0`** — the handle, as a number — at the exit code that means the program ran,
  and `len(a)` of the same binding refused the build with `len of a non-string variable`; `s = {1, 2}` /
  `a, b = s, 2` printed `0`, `d = {"k": 1}` / `a, b = d, 2` printed `1`, and the mixed swap `a = [1]` / `b = 2` /
  `a, b = b, a` printed **`[]` `0`** where CPython prints `2` and `[1]`. A container is not its handle: what makes
  `print(xs)` print a list is a dozen compile-time claims keyed by the NAME (**Gap Q.1**'s twelve), and the plain
  binding road has always carried them across a copy — "the handle travels and the notebook claims do not" is
  **ADR 0412**'s own finding — while the unpacking's bind loop wrote the payload word and asked none of them, so the
  receiving name was a number wearing another object's bits and the name it displaced kept claiming a container it no
  longer held (which is the half that made the swap print `[]`: the number `2` rendered through the list door). The
  fix asks the SAME door per element: `containerClaimsOf` / `writeContainerClaims` in `container_alias.go`, with
  `recordContainerAlias` refactored onto them so the two binding roads cannot spell fifteen map fields differently
  (**ADR 0232**), and `settleUnpackedClaims` on top — settle the kind (**Gap Q.1** owns the retirement too: the float
  face, `a = [1]` / `a, b = 1.5, 2` / `print(a)` printing `[1]`, goes through the same door), copy the record, keep
  the shared-handle link, root the handle (**ADR 0163**). Two rules make it a door rather than a broom. The record is
  read **before any of the statement's own bindings land**: `a, b = b, a` reads `a` while settling `b` and settles
  `a` with what `b` was, so **ADR 0367**'s "an unpacking reads every value before naming any of them" had to be
  carried from the payload into the notebook. And the live link is kept **only when the source is not also a target**
  — `a → b` beside `b → a` describes two objects that have already exchanged places, and the existing cycle guards
  prevent a hang, not a lie. The witness is the DOOR multiset, not the stdout: `b = xs` and `a, b = xs, 0` must ask
  the same `@rt_print*` doors in `@main`, and the swapped program must ask the doors the plain bindings ask — the
  stdout could not carry that claim, because the buggy build's `[]` was a plausible list rendered through a door the
  value had no business reaching. Eighteen answer rows on the compiled leg AND the record leg (**18 record entries,
  10368 → 10386**, from `tools/recmerge` with the compiled leg cross-checked), the same family at the CLI against a
  live `python3`, and `integration/programs/unpack_container_claims.gy` as a `match` row (corpus 281 → **282**,
  matrix 267 → **268**, parity-asserted 231 → **232**, oracle `match` 217 → **218**); the pair-bound residue stays
  pinned with its owner (**L11.1** / **Gap R.146**) rather than being guessed from a tag the compiler does not have
  (**ADR 0299**). Measured while writing this row's own table: **Gap R.529** — an arm the condition rejects is
  checked for undefined names as if it ran (`n = 0` / `if n:` / `    print(zzz)` / `print("ok")` is refused
  `undefined name "zzz"` at exit 1 where the reference prints `ok`), and **Gap R.528** (`global` honoured by the
  plain binding, ignored by the unpacking) stays open beside it.

- **A container the source SPELLS, unpacked, binds the object (**Gap R.524**, **ADR 0454**)** — `x = [1]` compiles and
  prints `[1]`, and still does; `x, y = [1], 2` — the same literal one binding position over — emitted
  `store i32 @.lst1, i32* %_x`, and `llc-20` refused the module with `global variable reference must have pointer
  type`. That is not a wrong answer or a refusal but an **invalid module**: an ordinary program the reference runs,
  dying in the toolchain at the exit class the contract reserves for a compiler bug (**ADR 0166**). It arrived from
  **ADR 0453**'s *control* table — the rows that were supposed to be a formality. The fix is a delegation, not a
  second builder: the unpacking hands every spelled `ListLit`/`SetLit`/`DictLit` element to the **same plain binding
  road** `x = [1]` walks — the same `rt_alloc`, the same per-slot tags, the same twelve container claims
  (**Gap Q.1**), the same rooting store (**ADR 0163**, **Gap J.2** / **ADR 0234**) — and the claim that there is one
  feature and not two is asserted as equal `@rt_alloc(` / `@rt_set_elem(` / `@rt_tag_elem(` call counts between the
  two bindings, because two roads that "should" agree are the drift this suite exists to catch (**ADR 0232**). Where
  the bind runs is correctness, not layout: it runs **after every element has been read out of its slot and before
  any element is stored**, which is what keeps an unpacking simultaneous (**ADR 0367**) while a container may still
  mention a name the statement has not overwritten. Outside that window the backend refuses in words instead of
  pretending — `b, a = [b], [a]` used to build a self-referential literal and print `[[[[[[…` until the process died
  with a segfault, and now names the name, the binding and **Gap R.526**, the row that owns the snapshot. The IR is
  asked at the altitude the defect lived on: no `store i32 @`/`ret i32 @` anywhere, the heap door called inside
  `@main` for each of the three kinds, and `--verify-llvm-file` at the CLI, because reading printed bytes cannot
  tell "fixed" from "the failure moved" when the broken build printed nothing at all. Fourteen answer rows (list,
  set, dict, both empty and then written to, nested three deep, a text element, membership, a `for`, an entry write,
  a tuple element, containers swapped through names, a container and a scalar rebound together) on the compiled leg
  AND the record leg, the same fourteen at the CLI beside a live `python3`, two refusal rows that fail when the shape
  starts compiling, and `integration/programs/unpack_container.gy` as a `match` row (corpus 280 → **281**, matrix
  266 → **267**, parity-asserted 230 → **231**, oracle `match` 216 → **217**); **16 record entries (10352 → 10368)**
  from `tools/recmerge`, reference-answered with the compiled leg cross-checked, and a recorder
  (`GUSTY_RECORD_UNPACK=<path>`) that dumps this row's own sources so a case cannot ship without a record under it
  (**ADR 0302**). Checking this row's swap rows against the reference found two defects the delegation does not pay,
  filed rather than absorbed: **Gap R.527** (an element that is a NAME holding a container binds the handle as a word
  — `a, b = xs, 5` prints `0`, and the mixed swap `a = [1]` / `b = 2` / `a, b = b, a` prints `[]` `0` where the
  reference prints `2` `[1]`, because the container's claims travel with the object on the plain road and not here,
  cf. **ADR 0412**) and **Gap R.528** (`global` is honoured by the plain binding — **ADR 0448** — and ignored by the
  unpacking, which binds the module's name locally and prints the old value at exit 0).

- **Membership asked of a kind the SOURCE PROMISES raises (**Gap R.522**, **ADR 0453**)** — ADR 0452 paid the
  haystack the source spells; this pays the haystack the source only NAMES. `x = 5` / `print(1 in x)` printed
  **`False`** at the exit code that means the program ran, where CPython ends it with
  `TypeError: argument of type 'int' is not iterable` — and so did `x = None`, `x = 1.5`, `x = True`,
  `print(1 not in x)`, the chain link `print(1 == 1 in x)`, the same question inside a loop body, a builtin's answer
  (`print(1 in len(xs))`), a conversion's (`print(1 in int("5"))`), a module name read from inside a function, the same
  name two frames down, and `def f(): return 5` / `print(1 in f())`. The `return 1.5` form was worse than wrong: a
  `double` reached `call i32 @rt_contains(i32 %t6, …)`, `llc` rejected the module, and the compiler paid **exit 2** —
  its own bug code — on a program the reference stops with a sentence. What ADR 0452 refused to read is what this row
  had to make sound first: a name's kind out of the compiler's fourteen name-keyed notebooks is a SUMMARY of the
  assignments a pass happened to see, not a PROMISE about the value a read receives, and raising at
  `x = [1]` / `if 1 > 2: x = 5` / `print(1 in x)` — where the reference prints `True` — would trade a wrong answer for
  a broken language (**ADR 0299**'s cliff, one step to the left). So `pkg/lang/membership_kind.go` asks a different
  question, about the PROGRAM: *did every road that could ever have written this name write THIS LITERAL?* One binder in
  the frame that owns the name, and that binder is a plain assignment of a no-member literal — then the value standing
  at the read IS that literal whatever happens next, and the raise is sound. Every other road retires the name: a second
  assignment, a write inside any arm of any compound statement, an augmented write, a `for`/`while` target, a
  comprehension target hiding inside an expression, a `match` capture, an unpack, `except … as`, a name another frame
  writes through `global` (**ADR 0448**'s road is the newest in the tree and the one a name-keyed summary would never
  have noticed). A call answers the same question of a BODY: every `return` road through `if`, `for`, `while`, `try` and
  `match` — every arm required, a missing `return` spelling the void — must spell one such literal, asked of the answer
  direction's own walk and the one kind table (**ADR 0232**), with a guard in front for a program that re-points the
  name at something else, which has taken its own promise back. Names are read in the frame that owns them, and the two
  program-wide readings are taken in `GenerateIRReport` before any body is lowered, because whether a promise reached a
  body may not depend on which loop the emitter was standing in (**ADR 0343**). The raise is the same non-local road and
  the same one sentence as every other membership raise, so `except TypeError:` reaches all 17 shapes and the haystack's
  own call still runs — exactly once — before it. Fifteen control programs (a name written twice, an arm that may not
  run, a loop-bound name, a body that binds its own `x`, a `global` written from another frame, an unpack, a text, a
  dict, a set, a tuple, a run-time-built container) keep the reference's bytes, and the residue is a defect list rather
  than a claim: a parameter's kind, a copy binding, a forwarding call, two arms of two kinds, and any value arriving as
  a `(payload, tag)` pair stay pinned with the row that owns each (**L11.1**, **Gap R.146**). Checking the control table
  against the reference also turned up three defects this row does not own and filed instead of absorbing —
  **Gap R.523** (`print(1 in [x])` refuses with a self-contradicting list-literal message while `print([x])` compiles),
  **Gap R.524** (`x, y = [1], 2` stores a container global into an `i32` and `llc` rejects the module) and **Gap R.525**
  (`except … as x` leaves `x` bound where the reference unbinds it). Corpus 279 → **280** programs with
  `membership_promised_kind.gy` as a `match` row (matrix 265 → **266**, parity-asserted 229 → **230**, oracle `match`
  215 → **216**), and **35 record entries (10317 → 10352)** from `tools/recmerge`, reference-answered and compiled-leg
  cross-checked — three candidate control rows were REMOVED rather than recorded, because the reference does not print
  what the table had claimed for them.

- **Membership asked of a value that has NO members raises (**Gap R.521**, **ADR 0452**)** — `print(1 in 5)` printed
  **`False`** at the exit code that means the program ran, where CPython ends it with
  `TypeError: argument of type 'int' is not iterable`; and so did `print(1 in None)` (`'NoneType'`),
  `print("a" in 1.5)` (`'float'`), `print(1 in True)` (`'bool'`), `print(1 not in 5)` (which printed **`True`**) and the
  same question as a comparison-chain link, `print(1 == 1 in 5)`. The embarrassment is where the answer already lives:
  `xs = [1]` / `print(1 in xs[0])` has raised the reference's exact sentence all along, because there the haystack's kind
  is a run-time fact and the object is ASKED — it is only the operand the front end can FOLD that reached a containment
  fold which does not know what a member is and answered "not present". A fold may substitute a verdict only when the
  verdict IS the answer (**ADR 0266**, **ADR 0329**), so the fold raises now: same non-local road as every other
  compiler-detected trap (`exnCode`/`raiseTo`), so `except TypeError:` reaches it and the program carries on, and asked
  AFTER both operands are lowered so `print(probe() in 5)` prints `needle` and then raises — the order the reference
  uses. The four kinds stopped being spelled twice: the runtime road's inline `{TagInt, "int"}, {TagFloat, "float"},
  {TagBool, "bool"}, {TagNone, "NoneType"}` list is the package-level `notIterableKinds`, read by the tag walk AND the
  literal fold (**ADR 0232**'s one question, now about a SET of values), and the text is the one door `notIterableSentence`
  — the loop's `'int' object is not iterable` stays the loop's (**ADR 0271**), with a row that fails if the membership door
  speaks it. `@main` for `print(1 in 5)` now carries no `@rt_contains` at all, which is the difference between replacing a
  runtime question and answering beside it. Eleven haystacks that DO have members — list, tuple, set, dict keys, text
  characters, container elements — keep their bytes on both legs, and what the fold cannot reach is named rather than
  guessed: a NAME's or a call's answer still answers `False`, because a name's kind is a summary of the assignments a pass
  saw and not a promise the value carries (**Gap R.522**, **L11.1**'s tagged value word), pinned in the row that files it.
  Corpus 278 → **279** programs with `membership_not_iterable.gy` as a `match` row, and **21 record entries
  (10296 → 10317)** from `tools/recmerge`, reference-answered and compiled-leg cross-checked.

- **A comparison chain STOPS at the first link its verdict rejects (**Gap R.520**, **ADR 0451**)** — the chain had
  been evaluated as if a link were a pure expression, and a link is not: `x = None` / `print(1 == x < 3)` printed
  nothing and died with `TypeError: '<' not supported between instances of 'NoneType' and 'int'` at **exit 3** where
  CPython prints `False`; `x = 5` / `print(x > 9 < 1 // 0)` raised `ZeroDivisionError` where CPython prints `False`; and
  the third is the one with no failing assertion for a test that only reads a verdict — `print(3 < 2 < boom())`
  printed **`called`** and then the right answer, for a call the reference never makes, at the exit code that means
  the program ran. `chainValue` did the two things **ADR 0288** taught it (store each operand once, lower each link
  through the ORDINARY comparison road) and one thing it invented: a flat `%tN = and i32 %acc, %link` over links that
  had ALL been lowered, under a comment arguing that nothing was left for short-circuiting to skip — true of the
  OPERANDS, false of the LINKS. The fix reuses the one door that already knows how to skip an operand a test rejected:
  **ADR 0275**'s skeleton, `br i1` on the previous verdict, the later operand AND the later link emitted in the block
  the verdict admits, one `phi` whose settled arm answers `0` without lowering anything. ADR 0288's rule survives the
  move rather than being traded back — operand binding became a closure taking its builder, with a `bound[]` memo, so a
  slot is written by exactly one store, in the one block its link lives in, which is asserted at IR level beside the
  stdout level where `calls` reaches `1` instead of `2`. A raise that a skipped link would have spent is now a raise in
  a block the run never enters, which is why the first two programs print `False`, and a link the chain DOES reach
  still raises into the program's own `except TypeError:` — both directions are rows, not adjectives. **13 record
  entries (10283 → 10296)** came from the reference through `tools/recmerge` with the compiled leg cross-checked, the
  conformance corpus grows to 278 programs with `chain_short_circuit.gy` as a `match` row, and **ADR 0450**'s checker
  rows passed unchanged: the verdicts did not move, only the effects did.

- **A comparison chain is CHECKED like the comparisons it holds, and typed the verdict it answers (**L12.1**,
  **ADR 0450**)** — `a < b < c` has been one node since **ADR 0288**, with its own lowering, its own operand-carriage
  door and its own conformance probe. What it never had was an arm in the analyzer's expression switch, and a node the
  walk does not know is a node whose contents do not exist: `gustyc --check 'y = 1 < zzz < 3'` answered **`ok`** at exit
  0 for a name nothing binds, `--check 'y = 1 < f() < 3'` answered `ok` for a callee that asks for an argument, and
  `--check 'y: int = 1 < 2 < 3'` answered `ok` for a verdict poured into an `int` annotation that the SAME program with
  one operator removed reports as `type mismatch: expected int, got bool`. A false `ok` is the failure mode an agent
  driving the compiler cannot see — the JSON says the program is sound, and codegen disagrees forty seconds later with a
  sentence that has no span, no level and no code. The fix is two arms and nothing else: infer every operand through the
  ordinary road (so the diagnostics a chain swallowed are the operand's own, at the operand's own span) and type the
  node **`bool`** (so the annotation, argument and arithmetic rules stop stepping aside for a `any` the node never had).
  The row's owed half — `--check` and a link the reference can never answer — ships as a NAMED warning, code
  `type.compare.uncomparable`, worded in the reference's own sentence (`'<' not supported between instances of 'int' and
  'str'`, `argument of type 'int' is not iterable`) and asked out of the ONE table the compiled leg already reads to
  decide when to raise (`order_kind.go`, **ADR 0232**), because refusing it would stop the build on a program the
  reference runs by raising — **ADR 0166**'s rule, quoted at the lexer and now at the chain. Every test is a PAIR (a
  chain and the same source with one operator removed, diagnostic lists asserted EQUAL), the five CLI rows read their
  expected sentence out of a live `python3` traceback and re-ask the compiled trap for the same bytes, a 12-row
  stay-silent table is the false-positive guard, and `integration/programs/compare_chains.gy` joined the corpus as a
  `match` row — 21 lines byte-identical to CPython, its first two lines the `out` then `True` the row names as its own
  definition of done, its source **1 record entry (10282 → 10283)** through `tools/recmerge`. What the sweep found in
  the LOWERING is filed rather than papered over: a chain evaluates every
  link, so an unreachable link still raises (**Gap R.520**, the **ADR 0275** skeleton is the fix — the next cycle paid
  it, **ADR 0451**), and `print(1 in 5)`
  answers `False` at exit 0 where the runtime door one step away raises (**Gap R.521**).

- **A `for` asks the OBJECT for its iterator (**L12.2**, **Gap R.54**, **ADR 0449**)** — the loop had one question for
  an iterable, *can I count its slots?*, and an object that answered a different question fell through it: the textbook
  `class Count:` / `    def __iter__(self): return self` / `    def __next__(self): … raise StopIteration` counted out
  NOTHING, printed only the trailer, and exited **0** — the code that means the program ran. Zero iterations is the worst
  answer this backend can give, because it is indistinguishable from an empty iterable to a caller, a CI job or an agent,
  so the row's DoD is not "the loop runs" but "a protocol loop either runs or refuses". It now asks the object once for
  `__iter__` (outside the loop, rooted for the loop's whole life so a body that allocates cannot collect the iterator
  mid-loop, **ADR 0181**), emits one `it.__next__()` per turn through the ORDINARY binding road — which is why the element
  keeps the kind the method answered with and a text still prints as a text instead of the intern table's index (**Gap
  R.22**) — and treats the `StopIteration` the iterator raises as the loop's own end signal, tested against the reference's
  exception CODE and eaten by the loop, so `except StopIteration:` outside never sees it while a `ValueError` from `__next__`
  walks out to the program's handler and dies at exit **3**. Not being iterable is a RUN-TIME `TypeError: 'Plain' object is
  not iterable` — catchable, fatal at 3 — not a compile-time refusal, because the reference's test is a question about the
  object in hand; `__next__` alone does not make an iterable, and a subclass inherits whichever answer its base has
  (**ADR 0446**). Six shapes refuse in words, each naming the missing rule and its row, because for every one of them the
  alternative was the parent's silent exit 0: `__getitem__` sequence iteration (**Gap R.514**), an `__iter__` whose hand-off
  has no class the source names (**Gap R.515**), the iterator test the reference runs at run time (**Gap R.516**), two call
  sites disagreeing about one frame parameter (**Gap R.517**), a container the loop fills and the frame hands back — the
  answer table reading an absent kind as "not a container" and printing the allocator's slot (**Gap R.518**, **ADR 0344**'s
  fault form), and a `__next__` answering a fractional number, which has no float word to travel in because the class road
  emits every method as an `i32` function whatever its body answers (**Gap R.519**, the same missing word as
  `print(o.m())`). `integration/programs/iterator_protocol.gy` printed only its trailer on the parent and now prints the
  counted-out values byte-identically to CPython, beside six `probe_iterator_*.gy`; 17 answer rows run on the compiled leg
  AND the record leg, 3 pin the raise, 8 pin the refusals, one asks the emitted module for `__iter__`/`__next__`/`@exn_code`
  and forbids the protocol leaking into a container loop, and a watchdog row keeps the third failure mode — a loop that
  never ends — out by construction, the retired engine having printed 7.3 M lines in 15 s. **21 record entries (10261 →
  10282)** went in through `tools/recmerge`, the 276-program corpus moved only where this row says it should, and the corpus
  classifier files the six refusals under their six `lower.unsupported.iterator-*` capability codes.

- **A body that DECLARES a name writes the MODULE's binding (**Gap R.48**, **ADR 0448**)** — `global` was an ordinary
  name, so `global count` was an expression mentioning a name called `global` and the resolver answered `undefined name
  "global"`: the counter a helper bumps, the accumulator a loop body fills, the flag a setup function sets and the name
  a METHOD writes could not be written at all, and the only way to share state between a body and the module was to
  shadow it and read `0`. The storage already existed — **ADR 0227** mints `@gy_mod_<name>` for a module-bound name a
  body reads — and the write half did not, which is the whole decision: **a declaration is not a new storage, it is the
  permission to write the storage the reads already use**. `enterBody` records what a body declares and DELETES those
  names from `funcLocals`, so the read road and the store road both fall through to the module's slot and neither
  needed a new arm; a body that never declares a name keeps its frame slot (asserted from `--emit-llvm` in both
  directions, **ADR 0351**'s defect asked from the declaration side). The checker is a whole-program walk at compile
  time because the reference's refusals are `SyntaxError`s that do not wait for the call, and its "used prior" rule is
  a DOCUMENT-ORDER walk because a first draft that compared line numbers refused four programs the reference answers
  while reporting each at "line 0" (**Gap R.116**). `integration/programs/global_statement.gy` joined the corpus
  byte-identical (ten prints; the parent refuses it at exit 1), beside 17 answer rows on the compiled leg and the
  record leg, 9 refusal rows, 11 CLI rows and a `cc` link-and-run row that re-ask CPython inside the case; **18 record
  entries (10243 → 10261)** went in through `tools/recmerge` and the 268-program corpus moved exactly twice, both times
  this cycle's own program. What the module's one int-sized word cannot carry is refused in words rather than printed as
  a handle — two kinds, a text, a fractional number, a container (**L11.1**'s tagged word, filed as **Gap R.512**), a
  name declared in a class body (**Gap R.511**, beside **ADR 0418**), and an unwritten global the reference would rather
  RAISE than refuse (**Gap R.513**).

- **An instance prints the way its own CLASS says, and there are TWO questions, not one (**L12.3**, **Gap R.55**,
  **ADR 0447**)** — `class C:` / `    def __str__(self): return "S"` / `    def __repr__(self): return "R"` /
  `print(C())` printed `0` at the exit code of success (the intern table's index going to `%d`, **Gap R.22**'s
  signature at the printer), `str(C())` and `repr(C())` were refused in words, `print([C()])` printed
  `<__main__.C object at 0x…>` and `print(f"v={x}")` printed `v=0`. Four spellings of one question, four wrong answers,
  because each had its own renderer. The reference has two questions and only one fallthrough, so the backend now has
  two doors: the *str* question (`print`, `str()`, an f-string field, `!s`) asks `__str__`, then `__repr__` the way
  `object.__str__` hands it down, then **ADR 0325**'s identity form; the *repr* question (`repr()`, every container
  element, `!r`) asks `__repr__` and NOTHING else — a class binding only `__str__` prints the identity form inside a
  list, which is the row that punishes a door that consulted `__str__` for elements. Both are keyed by the class id the
  instance CARRIES (`@rt_inst_get(h, 0)`), not by the expression's static spelling, and both walk **ADR 0446**'s C3
  order, so a dunder a base owns is found on the base that owns it. A row is installed only on a PROOF
  (`renderAnswerKind`): a body the compiler reads as returning a text gets a live row; a body it reads as returning a
  number gets a row that RAISES the reference's own `TypeError: __str__ returned non-string (type int)` and prints
  nothing; a body it cannot read keeps the honest identity form rather than inventing an answer (**Gap R.503**'s
  lesson, asked of a renderer). Rendering is a CALL — it pushes a frame, so a raise out of a dunder reports
  `File "prog", line N, in __str__`, and `try`/`except` around a `print` catches it — and a container walk that sees a
  raise STOPS rather than printing a container the program never finished building. `programAsksToBeRendered` is the
  file-then-pay gate: 266 corpus programs byte-identical parent to child (`0 moved`), and a module-shape row asserts a
  program whose classes bind no dunder carries no table, no indirect call and no frame push. 25 answer rows + 7
  identity rows + 6 trap rows + 2 caught rows on both witness legs, 4 module-shape rows, 8 filed rows, the two doors
  asked directly, 21 CLI programs on both execution legs, and `integration/programs/class_render.gy` joined the corpus
  byte-identical (the parent refuses it at exit 1); **46 record entries (10197 → 10243)** were written from
  reference/compiled agreements, and 8 shapes whose reference answer carries an unstable address were left off the
  record on purpose — the address is an allocation accident, and a record that stored it would be storing the allocator.
  Filed beside it: **Gap R.508** (a raise out of a dunder deep in a container leaves the bytes the walk already wrote —
  `rt_out_snapshot` does not exist, and **ADR 0258** says the same of the float door), **Gap R.509** (an instance
  reaching `print` through a parameter or loop variable prints the handle it was stored as — reproduced byte-identical
  on the parent, so it is inherited and not this cycle's damage), **Gap R.510** (a dunder body that CONCATENATES a
  literal with a method call is not provable text, while the f-string spelling of the SAME body answers).

- **An attribute read asks the class OBJECT, and the walk goes through EVERY base in the reference's resolution order
  (**L12.4**, **Gap R.56** + **Gap R.66**, **ADR 0446**)** — `class C:` / `    X = 1` / `print(C().X)` printed `0` at the
  exit code of success, `print(Sub.NAME)` for a member only a base binds was refused in words (`the class names no member
  of that name`), a method handing back `self.NAME` printed the intern table's index, and two objects of one class — one of
  them written to — were read with one word (`9 0 0` for the reference's `9 1 1`). The class body's bindings were already
  kept (**ADR 0418**); what the tree had never had was the walk. `classLinearization` writes C3's merge rather than
  approximating it — `class D(B, C)` over a shared `A`, and `class C(A, B)` where both bind the member, are answered by no
  cheaper order — `classChainMember` asks it what the first class binds, keeping **ADR 0418**'s owed/stable split so
  `C().ratio` still refuses instead of printing `1.0` (**Gap R.431**), and `instanceAttrWrites` decides whether the object's
  own word wins: with no write anywhere the read is a compile-time fact emitting neither `@rt_inst_get` nor the presence row
  (asserted), and with a write plus a class binding the read asks `@rt_inst_has` — **ADR 0235**'s row, asked by a plain
  attribute read for the first time. The residual lesson is **ADR 0445**'s arriving from the class side: a value road and a
  kind road are two roads, and widening one alone printed `0 1` for `print(c.NAME, C().NAME)` — so
  `methodReturnsClassText`, the silence in `stringVal` and `classMemberReadIsText` ask the same walk. 23 answer rows + 5
  shadowing rows on both witness legs, 4 module-shape rows, 7 filed rows, 26 CLI programs on both execution legs, and
  `integration/programs/class_object.gy` joined the corpus byte-identical (the parent refuses it at exit 1);
  **29 record entries (10168 → 10197)** were written from reference/compiled agreements, none refused. Filed beside it:
  **Gap R.505** (a class body may not read its own earlier bindings), **Gap R.506** (a class attribute may not be rebound
  outside the body), **Gap R.507** (`__doc__` on a class that has none prints the empty text, not `None`).

- **A hand-off call answers with the CALLER'S kind word, so a container slot stops wearing a guessed one (**Gap R.502**,
  **ADR 0445**)** — `def g(v):` / `    return v` / `print([g(x) for x in [1, "a", 2.5, None]])` printed
  `[1, 0, 1, 0]` for the reference's `[1, 'a', 2.5, None]`, `print({g(x): 1 for x in ["a", "b"]})` printed
  `{0: 1, 1: 1}` for `{'a': 1, 'b': 1}`, and `print([g(None), 1])` printed `[0, 1]` — all at the exit code of success.
  The builder asked the element EXPRESSION what kind to write beside the payload, and a call whose body hands back one
  of its own parameters spells no kind: the table fell through to Int and the tag array held a lie. A hand-off has no
  answer of its own — its answer IS the caller's argument — so the kind is read from the ARGUMENT at the call site
  (positional, keyword, or the declared default) and carried to the store through the tagged door that already existed:
  `rt_append_tagged`, `rt_set_add_tagged`, `rt_dict_put_tagged`, with `rt_mark_estr(…, 8)` marking the built slot
  self-describing. The gate is narrow and measured: only a body whose every `return` names one parameter, never rebound,
  is a hand-off, and `def g(v): return v + 0` keeps Gap R.146's refusal because an element built from a tag the pass
  cannot read prints a float box's address as a number. Asking the tag made the hashability guard askable, which closed
  two neighbouring rows on the way: `{[1], [2]}`, `{sa}`, `{{1}}` and `{[x for x in [1]]}` printed the member the
  reference refuses to hash and now raise `TypeError: unhashable type: …` catchably (**Gap R.485**, **Gap R.492**), while
  `{(1, 2)}` keeps building because a tuple's hashability is the AST's verdict, not the heap's (**ADR 0361**). 27 unit
  answer rows (22 container positions + 9 built-comprehension positions, each judged on the compiled leg AND the record
  leg), 6 module-shape rows, 12 hashability rows checked against both the compiled trap and the record's, 5 filed rows,
  and **44** CLI rows beside a live `python3` (33 answers, 5 traps at exit **3**, 3 catchable raises at exit **0**, 3
  refusals that keep exit **1**), plus `integration/programs/forwarded_answer.gy` joined byte-identical; **59 record
  entries (10109 → 10168)** were written from reference/compiled agreements and the merge tool refused the one program
  whose compiled bytes diverge — **Gap R.504**, filed rather than recorded.

- **A comprehension standing where the LOOP, the CONDITION or `bool()` asks for a container is walked, tested and
  measured as the OBJECT (Gap R.490 + Gap R.500, **ADR 0443**)** — three programs, one missing question.
  `for v in [x for x in [1]]:` and `if [g(x) for x in [1, 2, 3]]:` died with `llc-20: global variable reference must
  have pointer type` on `%t3 = icmp slt i32 %_ctr1.ld1, @.lst2` — **exit 2**, **ADR 0166**'s own code — and
  `for v in [g(x) for x in [1, 2, 3]]:` printed **`0`** for CPython's `2`, `4`, `6` at the exit code that means the
  program ran. The loop road already asked the door that knows how to make a comprehension's object
  (`containerHandleOf`'s `*Comp` arm, **ADR 0439**, materialising a fold through `containerOperand` the way
  **ADR 0188** taught) — but that door answers `""` for the KIND, correctly for a slot read where only the tag knows,
  and the loop arm stored the empty kind, skipped the walk, and fell through to the numeric counter road. The
  condition and `bool()` had no arm at all: they probed the word the comprehension arrived in, which is `@.lstN`
  (exit 2) or an allocation index (every comprehension "true", the empty one included). One predicate now answers
  the kind from the brace the program wrote — `[` → a list, `{a}` → a set, `{a: b}` → a dict, the kind being a fact
  of the **spelling** and not a run-time question — and all three doors ask it once: the loop walks the handle with
  `rt_list_len`/`rt_set_len`/`rt_dict_len` + `rt_get_elem`/`rt_tag_of`, the road a container VARIABLE takes (a dict
  comprehension yields its KEYS, in insertion order), the condition and `bool()` ask the object its length through
  the `rt_heap_len` **ADR 0442**'s count door asks. Deliberately NOT by widening `containerHandleOf`'s kind answer or
  `isContainerExpr`: **ADR 0339** measured that a deciding door which says "container" without a handle to hand turns
  programs that answer into refusals. Fifteen loops and thirteen condition/`bool` programs on the compiled leg AND
  the record leg (asked with **no** tolerance for a missing entry — `tools/recmerge` wrote **35 entries, 10051 →
  10086**, none refused), four module rows naming the length helper per brace plus both-altitude assertions that
  `icmp slt i32 %_ctr … @.` is absent, 43 programs through `--eval` beside a live `python3` with exit **2**
  forbidden on every row, six asks of the predicate directly, and `integration/programs/comp_loop_truth.gy` joined
  the corpus as a line-for-line `match` (33 lines). The walk was not the whole fix: with the kind asked and the walk
  in place the built loop printed **`4`, `6`, `4`, `6`** for `2`, `4`, `6`, because the object a builder allocates
  lives in a bare register while the road that FILLS it is free to CALL, and a call's body collects — **ADR 0181**'s
  forbidden shape, one construct earlier than the loop **ADR 0347** rooted. `gcRootDuringBuild`/`gcEndBuildRoot`
  open and close that window in the four builders (`rt_root_put` beside the allocation, `rt_root_clear` after the
  last element), attributed by control build and pinned by two more tests; a loop that mints a runtime comprehension
  3000 times still runs at `total_freed=2999`, `capacity=1024`, `exhausted=0`. What the two fixes do NOT settle is
  FILED with today's bytes, not
  folded in: **Gap R.502** — the comprehension's ELEMENT carries no kind word, so
  `for v in [g(x) for x in [1, "a", 2.5, None]]:` prints `1`, `0`, `1`, `0` and `print([g(x) for x in [1, "a", 2.5,
  None]])` prints `[1, 0, 1, 0]`, byte-identical on `290aa59` and on this commit, which is what keeps it a row and
  not damage; and a loop item handed to a call is now refused in **Gap R.146**'s words where it used to reach `llc`
  at exit **2** — a refusal that names the missing word beats a module LLVM rejected. The three **Gap R.500** rows
  left the filed tables for the answering tables (they fail on the fix) and the emptied slot names where they went.

- **A comprehension the run time BUILDS is an OBJECT, and `len` asks it (Gap R.496, **ADR 0442**)** —
  `def g(v):` / `    return v * 2` / `print(len([g(x) for x in [1, 2, 3]]))` never became a program at all:
  `%t15 = load i32, i32* getelementptr({i32, [0 x i32]}, {i32, [0 x i32]}* %h1, i32 0, i32 0)`,
  `llc-20: error: invalid use of function-local name`, **exit 2** — the code **ADR 0166** reserves for the toolchain's
  own failure, spent by a program CPython answers with a digit. There are two comprehensions in this backend and the
  length door knew only one of them: **ADR 0192**'s fold writes an unrolled global and records the count in `g.compLen`,
  while the four runtime builders write a heap object into a register and record themselves in `g.rtComps`; the door read
  the FOLD's table for both, so for a built comprehension it emitted the fold's load with the fold's type — `[0 x i32]`,
  because the table was empty — against the object's register. The road now asks who built it: `g.rtComps` is set →
  `call i32 @rt_heap_len`, the same helper a container VARIABLE's length asks (one helper for a list, a dict and a set,
  because the count is one word and the object's own kind word decides the rest); the fold answered → the compile-time
  load, unchanged, with no builder and no allocation, because **ADR 0192**'s rule that a comprehension which folds IS
  the literal it folds to is what still lets `min`/`max`/`sum` read folded elements. Two rows prove the number came from
  the object and not from the iterable the comprehension walked:
  `print(len({g(x) for x in [1, 1, 2]}))` answers **`2`** and `print(len({g(x) // 4: 1 for x in [1, 2, 3]}))` answers
  **`2`**, and any number the compiler could have stated is `3`. Measured against a build of `3b13229` in a detached
  worktree, **thirteen** of the fifteen programs of the family moved from exit **2** to the reference's bytes at exit
  **0**, and two (`xs = [g(x) for x in [1, 2, 3]]` / `print(len(xs))`, and the count taken inside a function) already
  answered — the control that this commit closed one door rather than three by luck. Fifteen answered sources on the
  compiled leg AND the record leg (asked with **no** tolerance for a missing record — `tools/recmerge` wrote **16
  entries, 10035 → 10051**, none refused), three module rows that pin which instruction the door emits (`rt_heap_len`
  present for the built one and ABSENT for the folded one, and the `[0 x i32]` load forbidden outright), the same fifteen
  through `--eval` beside a live `python3` with exit **2** forbidden on every row, and
  `integration/programs/built_comp_len.gy` joined the corpus as a line-for-line `match`. What the door still does not
  reach is FILED with today's bytes, not folded in: **Gap R.500** — the loop head, the truth test and `bool()` standing
  over the same built object answer the numeric counter, so `for v in [g(x) for x in [1, 2, 3]]:` prints **`0`**, `if
  [g(x) for x in [1, 2, 3]]:` prints nothing and `print(bool([g(x) for x in [1, 2, 3]]))` prints **`False`**, all three
  at the exit code of success (**Gap R.490**'s three doors over the other comprehension) — and **Gap R.501** —
  `print([g(x) for x in [1, 2, 3]][1])` dies of `IndexError: list index out of range` where the reference prints `4`,
  because `runtimeSlotPairDeep` has an arm for a container a NAME and a CALL answer and none for one a BUILT
  COMPREHENSION answers (**Gap R.495**'s hand-off at the second base). Both are byte-identical on the parent build and
  on this one, which is what keeps them rows.

- **The count door PERFORMS the elements whose count it takes: `len` of a spelled-out container evaluates them
  first (Gap R.494, **ADR 0441**)** — `print(len([f()]))` printed **`1`** and never called `f`, and
  `print(len([[1, 2][0][0], 1]))` printed **`2`** at the exit code that means the program ran, where the reference
  raises `TypeError: 'int' object is not subscriptable` — the same element the subscript door beside it has raised
  catchably since **ADR 0440**, so one program told two stories depending on whether a `len` was reading the
  container. `len` answers a spelled-out literal from its element list and never builds the object, and that is an
  optimisation, not the meaning: the reference evaluates every element, builds the container, then asks it how many
  slots it holds. `(*irGen).lowerLiteralElementsForEffect` now asks each element one pure question — *is this an
  operation, or a spelling?* — performs the operations in the order the reference evaluates them, and throws their
  values away. Inert is a number, a text, a verdict, the void, a name or a literal of inert parts, so
  `len([1, 2, 3])` still emits no instruction and no builder (the row's own negative control, asserted in the
  module at both altitudes); a call, a read, a division or a comprehension is an operation, so
  `print(len([1, f(), 3]))` prints `side` then `3`, `print(len([[1, 2][5]]))` dies with `IndexError: list index out
  of range`, and `print(len([f(), 1 // 0]))` prints `side` and THEN takes the `ZeroDivisionError` — the first
  element's accident wins, which is the row that proves the walk is in order and not just in place. Two rules kept
  it a door and not a broom: a container element is WALKED to its parts rather than handed to `value` (the element
  road a container takes is chosen from what it HOLDS — `[f(), 2]` reads as an int list, the static road refuses
  anything but an `IntLit`, and the door was silently keeping the count), and everything is built into a scratch
  buffer and committed only when every element lowered, because an element that raises ends a block and an
  instruction after a terminator is **ADR 0166**'s exit **2** paid by the compiler. The rule is **Gap R.149**'s
  (**ADR 0275**) arriving at the length door from the literal side. Eleven answered shapes and five traps on the
  compiled leg AND the record leg, the trap's ORDER asserted on the JIT's split streams (stdout is exactly `side`,
  the raise on the other channel), every raise caught by the program's own `except … as e:`, seven controls plus
  five module rows that fail if a builder returns or the call disappears, 21 asks of the gate directly; through the
  CLI 12 answers, 6 raises, 5 catches, 9 controls and the `--emit-llvm` builder row, every one beside a live
  `python3`. `tools/recmerge` wrote 30 entries (**10005 → 10035**), none refused — among them the two programs
  **ADR 0440**'s run refused, which record now because the legs agree — and
  `integration/programs/length_element_effects.gy` joined the corpus as a line-for-line `match`. The residue is
  FILED, not folded in, and each row sits beside the door that owes it: **Gap R.496** (`len` of a comprehension the
  run time builds reads its count out of a global that is not there — `llc` rejects the module at exit **2**),
  **Gap R.497** (a `def` inside a `try:` emits its whole `define` into the enclosing block — exit **2**, no `len` in
  the program at all) and **Gap R.498** (the same statement in an `if`/`for`/`while` refuses its own call) are the
  sweep's neighbours, and **Gap R.499** is the one face of this family the count door cannot reach —
  `print(len([[True][0][0]]))` prints `1`, and `x = [[True][0][0]]` / `print("ok")` prints `ok` too, which is what
  puts it on the subscript door. The two filed rows this commit paid MOVED to the answering tables, and the emptied
  tables carry a comment naming where every row went (**ADR 0440**'s rule: a row that starts answering moves, and a
  filed table that empties itself proves nothing).

- **A subscript stands on what the FOLD already answered, and the door raises instead of refusing (Gap R.227,
  **ADR 0440**)** — `print([[1, 2][0][0], 1])` and `print([[1][0][0], 1])` stopped the build at exit **1** with
  `codegen: index requires an inline list/dict/set literal`, where the reference raises
  `TypeError: 'int' object is not subscriptable` and the program can catch it. The fold was never the bug —
  `[1, 2][0]` IS the integer 2 — but what it leaves behind is an AST the next read cannot use: the outer
  subscript still sees the inner `Index`, its gates (`numericElemUse`, the pair doors) each correctly decline,
  and the tail of the road blamed a literal the program did write. `(*irGen).constantSubscriptKind` now walks
  that one fold level at the door (`foldedLiteralElemOf` asks the SAME `listLiteralFoldAnswers` question
  `valueIndex` asks, and declines a literal it cannot read, a non-constant position and a position out of range,
  which the pair door and the bounds check own), and the trap goes out through the emitter **ADR 0326**'s
  `KeyError` and **ADR 0330**'s `IndexError` use, so the enclosing `try:`'s landing pad is reached.
  `notSubscriptableSentence` is the one spelling of `'int'/'float'/'bool'/'NoneType' object is not subscriptable`,
  quoted by the run-time tag door too, so the fold and the object cannot report one accident twice. The order is
  the reference's — `print([[[1, 2][5]][0]])` raises the INNER `IndexError: list index out of range` — and a text
  stays what it is: `print(["ab"[0][0]])` answers `['a']`, because raising there would be a raise where the
  reference prints. Twenty-one sources raise on the compiled leg AND against the record (`jitTrap` +
  `recordTrapOf`), twenty are caught by the program's own `except … as e:` with the sentence compared byte for
  byte, fifteen controls answer on both legs, five IR rows pin the module (the sentence present, the refusal
  absent, no `i32 @.` in an operand — **ADR 0188**), and a twelve-row table asks the predicate directly so a
  widening fails a decision row; through the CLI 21 raises at exit **3**, 6 catches at exit **0** beside a live
  CPython, 16 controls line-for-line, exit **2** forbidden throughout. `tools/recmerge` wrote 72 entries (9932 →
  10004, none refused) and `integration/programs/folded_subscript_trap.gy` joined the corpus (260 programs) as a
  line-for-line `match`. The residue is FILED, not folded in: **Gap R.494** (`len` of a literal counts without
  evaluating its elements, so `print(len([[1, 2][0][0], 1]))` prints `2` at the exit code that means the program
  ran, and `len([f()])` never performs the call inside the literal — **Gap R.149**'s rule at the length door) and
  **Gap R.495** (a subscript of a read out of a COMPREHENSION still refuses — the pair the first read computed
  never reaches the second). The stale filed row left `pair_element_read_test.go` for the answering tables, and
  the residue table stayed populated with **Gap R.495**: a row that stops refusing moves, and a filed table that
  empties itself proves nothing.

- **A comprehension is a container at the doors that DECIDE, too: its element answers the brace (Gaps R.487,
  R.488, **ADR 0439**)** — `print([[{"a": 1} for x in range(1)], 1])` printed **`[1, 1]`** and
  `def f(): return 1` / `print([[f() for x in range(1)], 2])` printed **`[1, 2]`**, both at the exit code that means
  the program ran, while `print([[[x for x in [1]]]])` died in `llc` on
  `call void @rt_set_elem(i32 %t3, i32 0, i32 @.lst1)` at exit **2** — **ADR 0166**'s compiler-bug code, for a
  program the reference answers with `[[[1]]]`. One construct, two faces, one missing arm: the BUILD door has known
  a comprehension is a container since **ADR 0192** (`containerOperand` and `containerHandleOf` each carry the
  `*Comp` case, and the former materialises a folded `@.lst1`/`@.set1`/`@.dict1` because **ADR 0188** forbids a
  global in an `i32` operand), while the doors that DECIDE — `nestedContainerElem` for the word, `elemKindTag` for
  the label, `literalNeedsTags`/`literalNeedsHeap` for the header — stopped at the three literal spellings and a
  name, exactly the shape **ADR 0249** forbids. All three now ask one predicate, `compSlotIsContainer`, which reads
  the brace: `TagList` (5), `TagDict` (6), `TagSet` (7) beside the handle in a register, `rt_mark_estr(o, 8)` on the
  object, `rt_print_list_mixed` chosen — so `[[1], 1]`, `[{'k': [1]}]`, `[{1}]`, `[{1: 1}]`, `([1], 2)`,
  `[[[1]]]`, `[[{'a': 0}, {'a': 1}], 1]` come out at every door that WRITES the slot (a list element, a dict value,
  an `append`, a `d[k] =`, a tuple element) and every door that READS it back (`[0][0]`, `len`, `in`, a `for`, `==`,
  `str`, a repeat by a number). A dict keyed by one is declined by the hashing rule the literal spelling has always
  met, and — **Gap R.232**/**Gap R.249**'s rule applied to a node nobody had asked — the sentence says "a list
  comprehension", not `*lang.Comp` (the `repr` door had been saying `repr of ` for exactly that reason). Twenty-five
  sources answer on the compiled leg and the record leg, 28 more through the CLI beside a live CPython with the exit
  code asserted, ten with exit **2** forbidden; `tools/recmerge` wrote 27 entries from the reference with the
  compiled leg cross-checked (**9905 → 9932**, none refused — the family's 26 sources plus the corpus program's own
  27-line text, which the GC corpus asks about by its own spelling) and `integration/programs/
  comprehension_container_element.gy` joined the corpus as a line-for-line `match`. The numeric-literal IR row still
  asserts `print([1, 2, 3])` takes `rt_print_list` with no census bit, which is what keeps **ADR 0239**'s static
  array — and `sum`/`min`/`max` — from being paid for this. Four shapes the sweep surfaced are FILED with today's
  bytes pinned, not papered over: **Gap R.490** (the same global at the LOOP head, the truth test and `bool()`,
  still exit 2 — and the rejected alternative, recorded with its measurement: the deciding `*Comp` arms were built
  and the three programs still die, so that door owes the handle before the arm), **Gap R.491** (a comprehension
  over a container of containers raises `IndexError` where the reference prints), **Gap R.492** (a set literal
  accepts the unhashable member a comprehension builds, beside **Gap R.485**), and **Gap R.493** (a builtin's name
  as a statement reads a frame slot no `define` allocated — found by the recorder, not by a program).

## Modern front-end (lexer & parser)

- **Error-recovering lexer** — on an unexpected character, emits a `TokError`
  token carrying the span + message and *resumes* instead of aborting the file,
  so the parser/semantic pass can report multiple diagnostics per run.
- **Rich token spans** — each token carries `start` AND `end` (byte + rune
  offsets) plus an optional multi-line flag, giving f-strings, slices, and
  `match` patterns exact ranges for hover/diagnostics/formatting.
- **Unicode identifiers** — identifiers scan by Unicode `ID_Start`/`ID_Continue`
  (not just ASCII), NFC-normalized via `golang.org/x/text` so decomposed and
  precomposed spellings are one symbol, and a `TokWarning` diagnostic flags
  Greek/Cyrillic homoglyph lookalikes (e.g. `Ο` U+039F vs Latin `O`).
- **Numeric-literal modernization** — hex (`0xFF`), binary (`0b101`), octal
  (`0o17`), and `_` digit separators (`1_000`, `0x_FF`), with exact integer
  semantics and rejection of misplaced separators.
- **Raw & triple-quoted strings** — `r"..."` / `R'...'` raw strings and
  `"""..."""` / `'''...'''` multi-line strings; docstring extraction reuses
  both forms.
- **Line continuation** — a trailing `\` joins the next physical line into one
  logical line (Python-compatible), skipping the continued line's leading
  indentation and blank/comment-only continuation lines.
- **async/await + effectful syntax (L5.6)** — `async def`, `async for`, `async with`, and `await expr` parse as first-class syntax; under the minimal synchronous-coroutine model (no suspension primitives yet) they lower identically to their sync counterparts in the compiled backend, matching CPython (see `async_basic.gy`). The cooperative event-loop runtime is Phase 7.
- **Pratt parser** — a precedence-climbing expression parser keyed off a
  precedence table (unary, `**` right-assoc, multiplicative, additive,
  comparison, `and`/`or`, ternary) with panic-mode recovery
  (`recoverStmt` — nest-aware `INDENT`/`DEDENT` skipping) producing a forest
  of `*ParseError`s and a partial AST.
- **Trailing commas** — `f(a, b,)`, `[1, 2,]`, `{1: 2,}` and `match` case arg
  lists tolerated, and normalized away by the canonical formatter.

## One execution backend — and the two legs that witness it

Every feature ships once, in the one backend, and is judged against two witnesses (ADR 0302 retired
the AST interpreter that used to be the second engine):

- **LLVM AOT codegen** — `pkg/lang/codegen.go` (the front door) + the `pkg/lang/codegen_*.go` files the
  roads and the runtime IR blocks live in + `pkg/lang/closure.go`, entry
  `Compile`: every path that runs a program — `--eval`, `--file`, `--repl`, `--emit-llvm`,
  `--verify`, `--build`, the REPL included, which compiles in-process through the LLVM JIT
  (`pkg/lang/jit_llvm.go`, entry `JIT`). Emits
  deterministic opaque-pointer LLVM IR with real `double` float IR (float
  arithmetic via `sitofp` promotion, `%.17g` float print), tagged-union
  lowering, `__doc__` folding to string constants, string slicing via
  `rt_slice`, literal-container membership via `rt_contains`, and
  `with`/yield-from runtime protocols. The runtime it links carries the
  two-generation (nursery + old) tracing GC that reclaims unreachable pure-data
  heap objects; roots are top-level bindings, walking container elements,
  dict values, closure envs, and attr tables.
- **the record leg** — `pkg/lang/testdata/interpreter-golden.json`: the answer the retired
  interpreter gave for each of its 5.5k+ sources (value repr, type name, stdout, trap class and
  message, front-end refusal), read by `pkg/lang/golden.go`. A missing record fails the case, and a
  compiled answer that disagrees registers a drift row that fails the build in both directions.
- **the reference leg** — CPython, via `gustyc --oracle` and the conformance matrix; disagreements
  are rows in `integration/testdata/cpython-debt.json`, each naming the roadmap item that owns it.

## Optimization pipeline

- **Constant folding** — integer-literal binops fold at codegen time
  (`x = 1 + 2` emits `store i32 3`, no `add`).
- **Escape-analysis heap elision** — never-read top-level list literals skip
  their runtime heap allocation (`[x * 2 for x in ...]`,
  `{k: v for ...}`, `{x for ...}`).
- **Dead-global / dead-object elimination** — unused `@.strN` / `@.lstN`
  globals and dead heap objects are pruned.
- **Real LLVM `opt` pipeline** — `pkg/lang/opt_llvm.go` drives the external
  `opt-20` tool over the raw module IR (instcombine, gvn, licm, sroa,
  simplifycfg, ...), so AOT emits verified, optimized IR — while preserving
  GC-correctness by rooting heap slots through module-global arrays
  (`@gc.roots` / `@gc_roots_used`).

## CLI

`gustyc` is the command-line interface and REPL:

```
gustyc --eval "x = 2 + 3\nx"                 # evaluate source, print result
gustyc --file prog.gy                        # compile & run a source file
gustyc --build out a.gy b.gy                 # compile a set of files into a binary
gustyc --verify "def f(x): return x * 2"     # static analysis only
gustyc --emit-llvm "x = 1 + 2"               # print emitted LLVM IR
gustyc --emit-ast "x = 1"                    # print the AST as JSON
gustyc --emit-source-map "x = 1"             # JSON source map (fn -> IR symbol+line)
gustyc --emit-source-map-file src.gy         # the same, reading the program from a file
gustyc --build out prog.gy --debug            # DWARF: !dbg records, .debug_line, reported back
gustyc --debug-info "x = 1"                  # the compiled line table as JSON (--json)
gustyc --check <src> | check file1.gy ...    # mypy-style type-check without executing
gustyc --oracle '<src>' | --oracle-file prog.gy  # interpreter + compiled backend + CPython, one verdict
gustyc --json ...                            # machine-readable JSON output
gustyc --schema                              # print the JSON Schema for AST/IR dumps
gustyc --lang                                 # self-describing feature list
gustyc --variance                             # JSON variance table (list/dict invariant, Sequence covariant, Callable params contravariant)
gustyc --capabilities                         # JSON refusal registry: every lower.unsupported.<shape> code, its suggestion, and the roadmap rows that own it (L11.8, ADR 0346)
gustyc --effects <src> | effects file1.gy ...  # per-function effect signatures: effects performed, return shape, termination (--json for the document)
gustyc --jit "..."                           # in-process dlopen JIT path
gustyc --gc-stats --file prog.gy             # report what the collector did, and the compiled arena: capacity, headroom, grown, exhausted — how big the run's window got and how many times it doubled (stderr; --json adds a gc member) — Gap Q.2, ADR 0347, ADR 0350
gustyc --bench '<src>' --bench-runs N --bench-opt L   # wall-clock benchmark
gustyc --fmt <src> | --fmt-check <src> | --fmt-file <path>  # canonical formatter
gustyc --lsp                                  # stdio language server (hover, completion, diagnostics)
gustyc --stdlib <dir>                        # set stdlib root (default: ./stdlib)
gustyc --version                             # compiler version
gustyc                                      # start the interactive REPL
```

Exit codes are deterministic (full contract in `docs/operations.md` § Exit codes):

| Code | Meaning |
|------|---------|
| 0    | success / clean (check, fmt-check, an `--oracle` run that matches CPython) |
| 1    | compile error — parse, analysis, a codegen refusal, or `llc`/`cc` failed; the program never ran |
| 2    | LLVM rejected the module *we* emitted (a compiler bug, not a source error) |
| 3    | runtime error — the program compiled, ran, then trapped |
| 4    | CLI usage error (bad/unknown flags, no source, unreadable file) |
| 5    | benchmark regression (the `--bench-baseline` gate fired) |
| 6    | the oracle leg: the program ran and printed something other than what CPython prints |
| 7    | the oracle leg: CPython could not run the source, so there is no verdict |

## Testing & verification

- **Unit tests** — lexer, parser, semantic, codegen, and runtime tests in
  `pkg/lang/`; benchmarks (`Benchmark*`) and Go-native fuzz targets
  (`Fuzz*`) for the record.
- **Integration suite** — `integration/` drives the full pipeline
  (lex → parse → typecheck → codegen → run) and asserts stdout matches
  expected output.
- **The cases run beside each other** — `make testparallel` runs the whole suite with `t.Parallel()` honoured:
  **2,246 cases and 646 subtests in about 1 m 40 s on 20 cores, against 11 m 43 s serial** (ADR 0343). It is
  safe because a compiled program is no longer run inside the test process — the module is linked to an
  executable and run as a **child** with its own pipes, address space and heap — and because a run's directory,
  environment and standard-library root travel in `JITOptions`/`CompileRun` instead of in process globals.
  Parallelism is a checked property, not a style: `go run ./tools/parify -check ./pkg/... ./cmd/... ./integration/... ./tools/...`
  fails on a case that calls `t.Parallel()` while its body — or any chain of calls it reaches, production files
  included — touches process state, and on a case left serial without a reason. Exactly **one** case carries a
  `//parify:serial <reason>` marker — the parity case that enters the program through `JITOptions{InProcess: true}`
  and so owns fd 1 and fd 2 — and everything that used to need a marker was refactored out of needing one: the
  toolchain became a per-run `ToolOverride` value (Gap R.260) and the environment knobs became functions of an
  environment they are handed (`tools/envprobe` is how a case asks the process-wide ones without `t.Setenv`).
  The guard runs inside the suite, so the command a person can type is the command that breaks the build. What the change found: two table rows sharing one source file, a ledger row
  pinned to unstable bytes, a data race on a package-level reader, a run's toolchain kept as package state, and
  **Gap R.258** — a `sorted(...)` binding that died by `SIGSEGV` in `@rt_alloc` because the variable's hoisted
  slot was read before its binding had run. All three are paid; none was visible while one case at a time was
  allowed to run.
- **The suite runs sharded** — `make testshards` (and CI) runs the cases in **one process per core**
  through `tools/testshards`. The reason is arithmetic rather than taste: nearly every case ends inside a
  subprocess — `llc-20` lowering the module (58 ms), `cc` linking it (13 ms), `lli-20` running it (57 ms),
  `python3` answering the reference leg — against 1 ms of Go, so a serial suite is a single-core suite and
  `pkg/lang` did not fit a four-core runner in `go test`'s ten minutes (it died with a timeout panic naming
  the 70 ms case that happened to be running, not the slow suite). The split comes from `go test -list`,
  every case runs exactly once, and each case asserts what it always asserted; `-list` prints the partition
  and `-json` reports what ran, how long and what failed. `make test` remains the plain serial command, and
  is the one for the artifact-writing modes (`GUSTY_GOLDEN_UPDATE`, `GUSTY_GOLDEN_MISSING`), which write
  per-run ledgers. ADR 0313; ADR 0312 gives every toolchain call a budget (`GUSTY_TOOL_TIMEOUT`,
  `GUSTY_ORACLE_TIMEOUT`) so a tool that stops answering fails as **exit 8** instead of hanging the run.
- **Conformance matrix** — `integration/conformance_cases.go` +
  `conformance-matrix.json`: **170 rows over two legs** — the LLVM AOT
  binary and **CPython** — 131 asserting parity and 39 recorded without it (the probe and merged
  rows, which record an answer rather than assert one),
  the oracle verdict being 116 `match`, 33 `debt` and 21 `not_applicable`. Witness-leg parity was
  necessary but not sufficient: two witnesses that share a bug agree, and this project's history is the proof (`print(True)` printed `1` everywhere, `len("café")` printed `5`).
  A row is conformant when the compiled backend prints what CPython prints. Each case *declares* its
  state — `match` (the default), `debt` (with a reason, a roadmap owner, and a per-leg pin of
  the wrong answer), or `not_applicable` (gusty-only surface the oracle cannot run) — and drift
  fails the build in both directions, so a new divergence and an unrecorded fix are equally
  caught (roadmap L11.9, ADR 0186). Corpus growth follows a standing rule (ADR 0190): every feature
  ships its **least interesting** program — the tutorial one, `print([1, 2])`, `xs.sort()` — because
  a corpus grown from bug reports only tests what we already had reason to doubt. The oracle's first catch was not a refusal but a passing
  build: `xs[0] = "z"` on a mixed list answered `[1, 'a', None]`, the interned index printed
  through the slot's stale tag, and it is now ADR 0187 and two parity programs
  (`mixed_element_reads.gy`, `mixed_element_writes.gy`). Its third catch went the other way: `xs == ys`
  for two equal lists answered False on the compiled backend (the comparison compared heap handles), while
  `[0] == ["zero"]` answered **True** — an interned index matching a number. Containers now compare
  by value, element by element, as `(payload, tag)` pairs (ADR 0189). The same sweep's second find
  is closed too: `xs.sort()`, `xs.reverse()` and `sorted(xs)` are now language surface on both
  backends, with one comparator that orders interned strings by their **text** rather than by the
  index they were interned at (ADR 0191). And `[f(x) for x in range(5)]` — a comprehension whose
  element is a call — now compiles: the AOT path had made the constant folder the *meaning* of a
  comprehension, and the ordinary list-building idiom refused with "comprehension element must be
  constant" while the record ran it happily (ADR 0192). Its second catch was a crash in the most
  ordinary program in the corpus — `print([1, 2])` handed the static elements global to `printf`
  as an `i32` and `llc` refused the module, while `print(set())` printed the handle `0` and
  `print([["a"], ["b"]])` printed `[1, 2]` (ADR 0188). Parity cases include
  `programs/truthiness.gy` (Python's truthiness rules),
  `programs/subscript_assign.gy` (container iteration and `d[k] = v` /
  `xs[i] = v` item assignment), `programs/container_methods.gy`
  (`xs.pop()`, `set()`/`list()`/`dict()`, `s.add`/`s.discard`) and
  `programs/none_values.gy` (`None` as a singleton, void functions returning `None`,
  `f() == None`) — see `docs/language.md`
  § Truthiness / § None / § Iterating and mutating containers / § Container methods.
  The probes are the roadmap's measured TODO list: nested and heterogeneous containers, tuples,
  negative indexing (including the one that panics the compiler), code-point strings, stdlib
  constant types, floored `//`/`%`, `sorted`/`enumerate`, calling a function through a
  parameter, `print(set())`, and print atomicity. `tools/oracleprobe` prints the three legs for
  any program, which is how a ledger row is written from data.
- **The Python-visible surface survey (Phase 12, 2026-10-01)** — 76 programs written the way
  someone writes Python, each run through `--interp`, `--aot` and CPython 3.12.3, classify the
  language into the states a construct is allowed to be in: **9** CPython-equal on the compiled backend,
  **37** absent (both legs refuse, honestly), **16** refused by the compiled backend alone
  (5 of those emit a module `llc` rejects, which is exit 2 rather than a refusal), **8** that run
  everywhere and answer wrong, **2** that hang or iterate nothing. The last two groups are what
  Phase 12 exists to delete: its rule is that every construct is implemented, refused by a stable
  code, or absent from `--lang` — never "parses, runs, prints something". Rows `L12.1`–`L12.13`
  in `roadmap.md`, method and per-class examples in `docs/roadmap-details.md` § Phase 12; making
  the census a standing artifact instead of a dated measurement is `L12.13`.
- **Property testing** — seeded deterministic whole-program generation.

## Requirements & build

- **LLVM 20** — `llc-20` for lowering and `opt-20` for the optimization
  pipeline (the AOT backend emits textual opaque-pointer IR verified by these
  external tools).
- **Go** — build with the LLVM 20 tag:

```sh
go build -tags=llvm20 ./...
go test -tags=llvm20 ./pkg/...        # unit tests, serial — the form for the ledger-writing modes
make testparallel                     # the whole suite with its cases overlapping (ADR 0343, ~1m40s on 20 cores)
make parallelism                      # the guard: tools/parify -check over the tree
make testshards                       # the same cases, one test process per core (what CI runs)
go run ./tools/testshards -tags llvm20 -json ./pkg/...   # the same, as data
```

- **C linker** — `cc` / `gcc` to link the emitted native object into a binary.

## Repository layout

```
pkg/lang/          compiler: lexer, parser, semantic (types), codegen, jit runtime
cmd/gustyc/        the CLI + REPL
integration/       end-to-end pipeline + conformance suite
docs/              language spec (language.md) + operations guide (operations.md)
stdlib/            on-disk modules: math.gy, string.gy, collections.gy, json.gy
```

---

*The language spec lives in `docs/language.md`; the toolchain & CLI reference
in `docs/operations.md`; the status of every item, gap and queued next step in
`roadmap.md`, with the reasoning behind each row in `docs/roadmap-details.md`.*
