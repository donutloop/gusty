# gusty

> A statically-typed, Python-flavored programming language compiled ahead-of-time through LLVM.

gusty is a small, modern programming language that pairs Python's familiar
indentation-based syntax and ergonomic feel with the performance of native
compiled executables. Source goes through a clean, inspectable pipeline —
`lex → parse → semantic (type inference) → codegen` — down to LLVM IR, which
is verified, optimized, and lowered to a native binary, or JIT-executed in
the REPL.

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
- **Slicing** — `s[a:b]`, `s[::step]`, negative indices; supported in both the
  interpreter and the AOT backend (via the `rt_slice` runtime helper).
- **Generators** — `def g(): yield a; yield b` collects yielded values.
- **Async** — `async def` / `await` / `async for` / `async with`, with the await/return
  discipline checked in the shared front end: dropping a coroutine, awaiting one twice, or
  yielding inside an `async def` is a compile error, and each function's effect signature is
  readable with `gustyc --effects` (ADR 0195).
- **Context managers** — `with expr as name:` / `with expr:`, dispatching
  `__enter__` / `__exit__` (including exception suppression); supported in
  both backends.
- **Exceptions** — `try` / `except` / `finally` with typed built-in exception
  classes (`Exception`, `ValueError`, `TypeError`, `KeyError`, `IndexError`,
  `RuntimeError`, `StopIteration`, `ZeroDivisionError`) and `raise`.
- **Classes & inheritance** — `class Name:` with methods (`self`), instance
  attributes, `__init__`, `class Child(Base):` multi-level inheritance, and
  `super()` delegation.
- **Operator overloading** — binary operators dispatch to dunder methods
  (`__add__`, `__mul__`, `__lt__`, ...) with reflected fallbacks
  (`__radd__`, `__rmul__`, swapped comparisons), in both the interpreter and
  AOT codegen.
- **Decorators** — `@dec def f:` → `f = dec(f)` at definition time; wrapping
  (fnptr-valued) decorators compile in AOT via compile-time specialization.
- **Modules** — `import mod` loads `mod.gy` and binds `mod` as a namespace with
  `mod.name` / `mod.fn(args)` access.
- **Standard library** — data-only on-disk modules folded as AOT constants:
  - `import math` — `PI`, `E`, `TAU`, `PHI`, `SQRT2`, `LN2`, `LN10`.
  - `import string` — `DIGITS`, `LOWERCASE`, `UPPERCASE`, `HEXDIGITS`,
    `WHITESPACE`, `PUNCT`.
  - `import collections` — `EMPTY_DICT`, `EMPTY_LIST`, `ZERO`, `ONE`.
  - `import json` — `NULL` (`None`), `TRUE` (`True`), `FALSE` (`False`).

## The value model

Behind the annotations, a runtime value is one of fifteen kinds — `int`, `float`, `bool`,
`None`, `str`, `list`, `dict`, `set`, `tuple`, `class`, `instance`, `method`, `closure`,
`exn`, `module` — and one Go table says which. The interpreter's heap objects, the compiled
runtime's tagged values, the exported C ABI and the garbage collector's root tracing all
read those numbers, and the compiled heap's own object-header kind is a projection of them
(`list`, `dict`, `set`, `instance`, with 0 meaning "not allocated — an immediate or an
interned string"). `gustyc --lang` prints both tables and `--schema`'s `valueTag`
definition documents the numbering (ADR 0182).

A compiled container can mix numbers, strings and `None`: `print([1, "a", None])` prints
`[1, 'a', None]` on both backends, because each element slot carries its own tag (ADR 0184). Dicts
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
`y = xs[0][1]` all answer on both backends — and a slot the compiler can see holding a number *is* that
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
*interpreter* was the diverging engine and the compiled answer was CPython's; the two engines had disagreed
about an ordinary dictionary until a program wrote one.

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
`d[1]["k"]` came with it) — roadmap L11.1, ADR 0251. An **ordering** of those slots asks the object the same
question (ADR 0252, closing Gap R.93): `xs = []` + `xs.append(i)` in a loop + `print(1 if xs[0] > "a" else 0)`
used to print `1` compiled for a program CPython crashes, and now raises *'>' not supported between instances
of 'int' and 'str'* on both engines — as do the dict the program filled (`d["k"] = 5` / `d["k"] >= 5`) and the
slot one level below (`xs.append([3, "a"])` / `xs[0][0] > 1`). Two numbers become doubles, two texts go to
`strcmp`, and every other pair raises CPython's sentence with the kind the slot really holds in it: the raise
is one test per tag, because the sentence names *both* operand types, and it may end in an `else` only because
the tags ADR 0187's writers can store are a closed set. One arithmetic operator can ask the same question,
because it is the one whose **result** kind is settled before the slot is asked: **true division** (ADR 0253,
closing Gap R.96). `xs = []` / `xs.append(3)` / `print(xs[0] / 4)` printed `0.0` with exit 0 — ADR 0249's
empty-operand `fdiv` substituted into silence — and now prints `0.75` on both engines, because `/` is a float
whatever arrives: a float slot unboxes, an `int` or `bool` slot converts, and a text, `None`, a list, a dict or
a set raises CPython's own `unsupported operand type(s) for /` sentence naming the kind it really holds. The
zero trap is emitted inside each arm rather than after the merge, because which wording the pair earns is
itself a run-time question — `3 / 0` is `division by zero`, `1.5 / 0` is `float division by zero` — and a
program's `except ZeroDivisionError:` reads that sentence.
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

A function's **return word** is now read from what its body does rather than from the shape of its `return`
line (ADR 0254, closing Gap R.3c). `def addf(x): x = x + 1.5` / `return x` / `print(addf(1.0))` printed the
*argument* — `1`, with the exit code of success — because the answer's type came from `return x`, which says
nothing about a kind, while the body had already stored a double into the parameter's slot. The gate asks the
same question the emitted instructions ask (`isFloat`, over the body with the rebound names read as the doubles
they became), so the `ret` and the value it writes cannot disagree, and the family answers: the bare name,
`return x * 2`, `x % 3`, `abs(x)`, `abs(-x)`, `return -x` — which had been an `llc` rejection, `ret i32` under a
`define double`, ever since the body learned to negate a double — the local spelling `y = x + 0.5; return y`,
and any of them inside `if`, `while`, `try`, recursion or a default. What is not that double keeps its own word
(`int(x)`, `round(x)`, `x > 2`, a user callee), and where the one convention would have to hand a container
handle or an interned text the `double` word — a parameter the body also reads, or any method — the program is
refused in words, naming the variable whose double has nowhere to go. Three shapes came out of that sweep and
are filed rather than absorbed: the ternary arm (`return x if x > 2 else 0.0`, refused since ADR 0254 and a
truncated `1` before it, Gap R.102), a container **returned** from a function (`print(f(1.0))` printing `0` for
`{'k': 2.5}` — filed as Gap R.103 and re-measured as Gap R.67's, since the same dict written with no function around it prints
`{'k': 2.5}` on all three engines), and `min`/`max` with two arguments refusing in the interpreter (Gap R.104,
closed two cycles later by ADR 0256 — which found the compiled leg had its own half of the same question).

The element of a comprehension over a container the program built is free to **branch**, and the loop had to
be told (ADR 0255, closing Gap R.100). `xs = []` / `xs.append(6)` / `print([v / 2 for v in xs])` was **exit
2** — `llc` rejecting *PHI node entries do not match predecessors* — because the zero guard `/` carries
(ADR 0253) ends the body in a block the induction `phi` had never heard of, while the `phi` went on naming
the block the body *starts* in. The increment now lives in a latch block that every path the element can end
on branches to, the entry list says where the back edge really comes from, and the program prints `[3.0]` on
three engines: `[v / 0 for v in xs]` dies with `division by zero` (exit 3, behind a filter or in front of
one), `{v / 2 for v in xs}` prints `{3.0}`, and a `for` whose body divides came with it. Two shapes from the
same sweep are filed rather than absorbed, both silently-wrong answers with exit 0: the dict comprehension's
value loses the double's tag (`{6: 3}` for `{6: 3.0}`, Gap R.105), and a loop variable whose slot holds text
is divided by the static arm (`[0.0]` where every honest engine raises `TypeError`, Gap R.106).

A fold **returns the candidate it chose**, not the comparison that found it (ADR 0256, closing Gap R.104 and
the source-visible half of Gap R.73). `print(min(1.0, 2), max(1, 2.5))` printed `1.0 2.5` compiled and refused
outright interpreted; `print(min(2.5, 1))` printed `1.0`, because the compiled path promoted every candidate to
`double` and selected a `double` — while Python hands back the winning *element*, so the answer's kind is the
winner's own. `min`/`max` now take values side by side or one container on both engines, and the family
answers: ints (`1 5`), an int among doubles (`min(2.5, 1)` → `1`, `max(1, 2.5)` → `2.5`), three candidates, a
container written inline, and **text ordered by its content** through `rt_str_order` rather than by its
position in the intern table (ADR 0248's rule, met again where an interned text is also an `i32`) — so
`t = min("pear", "apple")` still has a working `.upper()`. Candidates with no ordering for the operator the
builtin asks (`min` asks `<`, `max` asks `>`) are **raised**, not refused and not answered: the old compiled
path compared heap and intern indices and exited 0 with a number, where all three engines now print CPython's
`TypeError: '<' not supported between instances of 'str' and 'int'`, catchable by `except TypeError`, at exit 3
— with the two kinds in the order the fold met them, because that is the order CPython's operands had. Four
shapes stay honest about the half they are missing and are filed rather than absorbed: a container the program
built (Gap R.107), a text container the program built (Gap R.108), two runtime candidates whose kinds straddle
`int` and `double` — comparable, but the winner's kind cannot leave the call until the tagged value word exists
(Gap R.109) — and a callee whose winner was settled by one call site (`def choose(a, b): return max(a, b)` /
`print(choose(2.0, 1))` printing `2` for `2.0`, Gap R.110).

A verdict **is a value, and it prints its own name** (ADR 0257, L11.1's bool step). `print(True)` is `True`,
`print(1 == 1)` is `True`, `print(0 == None)` is `False`, `str(True)` is `'True'` — an ordinary string with a
working `.upper()` — and an f-string writes `flag: True` rather than `flag: 1`. None of that changed what a
bool *is*: it is still the untagged `0`/`1` in the word it always occupied, still adds (`True + 1` → `2`),
multiplies, negates (`-True` → `-1`) and sums (`sum([True, True, False])` → `2`), and a condition still tests
it the way it always did. What changed is who answers "what is this?": the printer asks the **expression**,
through one predicate both backends share, so `and`/`or` of two verdicts are verdicts while `1 and 2` is still
`2` (Python yields the operand), a ternary is a verdict only when both arms are, `all`/`any` and a call whose
every `return` is a verdict (ADR 0254's rule, read from the body) are verdicts, and a name is a verdict only
until something that is not an expression rebinds it — `for flag in [1, 2]` prints `1` and `2`, because a loop
binds elements. A comparison that reached a class's own `__lt__` is not a verdict at all: the program's method
returned an int, and `print(a < 4)` prints `1` on all three engines. Two shapes stay where a tag has to travel
rather than be read off an expression. A bool in a container used to be one of them — `print([True, 1])`
printed `[1, 1]` on both backends, because the element tag vocabulary had no bool to read back — and is
paid: the slot now carries a bool tag, the container printer, the set dedup and the ordering sentence all
read `bool`, and `[True, 1]`, `{'k': True}` and `{True}` come out as CPython writes them while every
numeric question about the same slot still answers as its number (ADR 0259, closing Gap R.112). What stays
filed is the shape no tag can reach from the caller's side: a bool handed to a function prints the number it
is stored as (Gap R.111), and the comprehension and fold shapes that sweep measured are filed with
their per-engine answers — Gaps R.116, R.117 and R.119, since R.118 turned out to be a dict story and was
paid by ADR 0260 the same day. `--json` names the
type `bool`, and `--eval '1 == 1'` echoes `True`.

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
the same object; objects now describe their own slots on every assignment path. A value whose kind no
expression names is refused in words with exit 1 and the missing half named — never the number
underneath (the residual shapes are Gap R.115, a container returned from a function is Gap R.67's, a
tuple is L11.3's, and `print(f"{xs}")` is Gap R.114). `--json --eval 'repr("hi")'` reports
`{"result": "'hi'", "type": "str"}`, and `programs/probe_render_pair.gy` is `match` on all three legs.

A comprehension that folds **is** the literal it folds to: `sa = {x for x in [1, 2, 3]}` and
`sa = {1, 2, 3}` reach one lowering — a heap object, every slot written with its payload and its tag,
the variable's kind recorded — so print, `in`, subscript and `for` treat a bound set or dict
comprehension exactly like the literal (ADR 0234). `{x for x in xs if x > 1}` parses on both backends:
the `if` is the comprehension's, not a ternary's.

A class pattern is a question about a class, in both backends: `case Point(x, y):` matches an instance
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
  take the newline as a flag rather than baking it in). Interpreter and AOT agree
  byte-for-byte, including how an argument that prints interleaves with its line
  (ADR 0165).
- **Containers are references everywhere** — pass a `list`, `dict` or `set` to a
  function as a literal, variable, keyword argument, default, comprehension or
  generator result and the callee sees the same live object on both backends:
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
  `def range` was the built-in to the interpreter and the program's function to the compiled backend
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
  whether the interpreter or native code ran it, an `llc` rejection of our own module is the
  compiler-bug class 2 on the run path too, and `--json`'s `exit` field is derived from the
  process status rather than written down (ADR 0211)
- **A built-in trap is a typed exception everywhere** — `7 % 0` raises `ZeroDivisionError` with
  CPython's wording and `except ZeroDivisionError:` catches it on both backends; the compiled
  backend used to emit the instruction and keep walking, printing `inf` or a fresh garbage integer
  and exiting 0 (`programs/zero_division.gy`, ADR 0212)
- **Every `except` arm is a real arm** — arms are dispatched in source order on both backends, a
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
  found the interpreter's untagged values colliding with ordinary arithmetic: a bench loop computing
  `i * i` reached the heap's id range at `i = 1024` and read back the class's own method object, so the
  heap now starts at `1 << 48` and one predicate decides what an object is.
- **`//` and `%` floor, and they are one rule** (ADR 0216) — `print(-7 // 2)` printed `-3` compiled
  and `-4` interpreted, and `print(-7 % 2)` printed `-1` on *both*, because Go's `/` and `%` truncate
  toward zero while Python floors (the remainder carries the divisor's sign, so `-7 % 2` is `1`).
  Both backends now emit the correction (`sdiv`/`srem` plus a `select`; `frem` plus `fadd` and
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
  allowed to name a file precisely to report that it is missing. `%` formatting itself remains a gap
  (R.31), but now it has the artifact its entry always claimed.
- **An unwritten slot raises — it does not answer** (ADR 0228) — `def f(c): if c: x = 1; return x`
  called with `False` printed `0` and exited 0; `while 0: w = 1` in a function printed `8555776`; a `try`
  cut short before its second assignment printed `518208`; `if 0: x = 1` then `print(x)` at module level
  printed `64`. Those numbers were the frame's previous contents — leftover words and stale heap
  handles — read as values and reported as successes. Both backends now raise what CPython raises:
  `UnboundLocalError` when the frame owns the name, `NameError` when nothing does, catchable by class on
  either engine, exit 3 either way. The mechanism is one byte on each slot *the checker* cannot prove was
  written (codegen gets no dataflow rule of its own), cleared on entry, set by every write, tested at the
  read — and absent, with no instruction emitted, wherever assignment is provably definite. Three checker
  rules turned out to be the cause: a `for` body may run zero times, a `match` may match nothing, and a
  loop variable is certainly bound inside its own body. The probes also caught the linked binary exiting
  1 — the compile-error code — for a program that merely raised, which ADR 0211 does not permit.
- **A string is an index into a table the runtime can add to** (ADR 0229) — `s = get(); print(s[1])`
  refused as "index of a non-literal variable", `def f(s): return s[1]` printed `1`, and
  `get()[1].upper()` printed `2`, all with exit 0 while CPython and the interpreter printed `b` and `B`.
  Thirteen shapes were compile-time refusals for programs Python runs. A compiled string is an
  `@str_tab` index and the table is content-addressed and already grows at run time, so the fix was not
  a new representation but six runtime helpers that take indices and return them — and one question the
  compiler had been asking too narrowly: *is this a string?*, not *can the compiler read its text?*.
  Subscripts at run-time positions, `len`, `ord`, and `upper`/`lower` now answer on both backends, and
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
  implementations to each other. Both backends now name the operation — `math.RoundToEven`,
  `llvm.roundeven.f64` — and `programs/round_ties.gy` entered the corpus with no ledger row, which here
  means "print what CPython prints", so the old answer is a CI failure. What the same probe found and
  did not fix: `round(2.345, 2)` is `2.35` in CPython, `2` in the interpreter (the digit count is
  ignored) and an exit-1 refusal compiled — a compile-error exit code for a program CPython runs, which
  is Gap R.69 and the exit-code contract's own subject (ADR 0211).
- **A class pattern asked two backends the same question, and got two answers** (ADR 0235) —
  `case Point(a, b):` on an instance with `x` and `y` **matched** compiled and printed `pt 0 0`, while
  the interpreter and `docs/language.md` both say a missing attribute fails the case: the compiled arm
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
- **A subscript of a string is a one-character string** (ADR 0225) — both backends answered `s[1]`
  with `98`, and the missing type spread to everything the value touched: `s[0] + s[2]` did arithmetic
  and printed `196`, `s[1] == "b"` said false (compiled as well as interpreted — the two backends
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
  handler/deferred state, name their traceback frame `Class.method`, report their refusals as compile
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

  Eleven shapes measured against CPython, nine wrong, and **both backends wrong identically** — the
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
  and the interpreter was wrong on exactly 8. Equality between two numbers is now one question about
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
- **The corpus has a third opinion (L11.9)** — parity between the two backends can be satisfied
  by two implementations that share a bug, and for a hundred ADRs it was. The conformance matrix
  runs each program through the interpreter, the compiled binary **and CPython**, and each case
  declares its state in a ledger (`match` by default, `debt` with a reason, an owner and a pin of
  the wrong answer, or `not_applicable` for gusty-only surface). Drift fails the build in both
  directions. `gustyc --oracle '<src>'` exposes the same classifier interactively — `--json` for
  the leg-by-leg report, exit 6 when gusty disagrees with Python and 7 when the oracle could not
  judge the source (ADR 0186).
- **Benchmark suite + regression gate** — `gustyc --bench-suite` measures a
  corpus on both backends and prints (or `--json`-emits) a stable artifact;
  `--bench-baseline` gates a run against a saved baseline, so "the compiler got
  slower" is a number with its own exit code (5) instead of a hunch.
  `--bench-dir integration/programs` benchmarks the parity programs too.

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
- **async/await + effectful syntax (L5.6)** — `async def`, `async for`, `async with`, and `await expr` parse as first-class syntax; under the minimal synchronous-coroutine model (no suspension primitives yet) they lower identically to their sync counterparts in both the AST interpreter and the LLVM AOT/JIT backends, giving exact parity (see `async_basic.gy`). The cooperative event-loop runtime is Phase 7.
- **Pratt parser** — a precedence-climbing expression parser keyed off a
  precedence table (unary, `**` right-assoc, multiplicative, additive,
  comparison, `and`/`or`, ternary) with panic-mode recovery
  (`recoverStmt` — nest-aware `INDENT`/`DEDENT` skipping) producing a forest
  of `*ParseError`s and a partial AST.
- **Trailing commas** — `f(a, b,)`, `[1, 2,]`, `{1: 2,}` and `match` case arg
  lists tolerated, and normalized away by the canonical formatter.

## Two execution backends

Every feature ships in **both** paths:

- **Interpreter** — `pkg/lang/jit.go`, entry `EvalExpr`: the REPL / `--eval` /
  `--verify` path. Fast feedback, rich diagnostics; a two-generation
  (nursery + old) tracing GC (`ev.Collect()`) reclaims unreachable pure-data
  heap objects; roots are top-level bindings, walking container elements,
  dict values, closure envs, and attr tables. Classes/methods/closures/imports
  are never freed.
- **LLVM AOT codegen** — `pkg/lang/codegen.go` + `pkg/lang/closure.go`, entry
  `Compile`: the `--file` / `--emit-llvm` / link-and-run path. Emits
  deterministic opaque-pointer LLVM IR with real `double` float IR (float
  arithmetic via `sitofp` promotion, `%.17g` float print), tagged-union
  lowering, `__doc__` folding to string constants, string slicing via
  `rt_slice`, literal-container membership via `rt_contains`, and
  `with`/yield-from runtime protocols.

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
gustyc --effects <src> | effects file1.gy ...  # per-function effect signatures: effects performed, return shape, termination (--json for the document)
gustyc --jit "..."                           # in-process dlopen JIT path
gustyc --gc-stats --file prog.gy             # report what the collector did (stderr; --json adds a gc member)
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
  (`Fuzz*`) for the interpreter.
- **Integration suite** — `integration/` drives the full pipeline
  (lex → parse → typecheck → codegen → run) and asserts stdout matches
  expected output.
- **Conformance matrix** — `integration/conformance_cases.go` +
  `conformance-matrix.json`: **118 rows over three legs** — the AST interpreter, the LLVM AOT
  binary, and **CPython** — 96 rows asserting parity and 22 recorded without it (the probe and merged
  rows, which record an answer rather than assert one),
  the oracle verdict being 82 `match`, 18 pinned `debt` and 18 `not_applicable`. Parity (interpreter ==
  AOT) is necessary but not sufficient: two backends that share a bug agree, and for this
  project's history they did (`print(True)` printed `1` everywhere, `len("café")` printed `5`).
  A row is conformant when both backends print what CPython prints. Each case *declares* its
  state — `match` (the default), `debt` (with a reason, a roadmap owner, and a per-leg pin of
  the wrong answer), or `not_applicable` (gusty-only surface the oracle cannot run) — and drift
  fails the build in both directions, so a new divergence and an unrecorded fix are equally
  caught (roadmap L11.9, ADR 0186). Corpus growth follows a standing rule (ADR 0190): every feature
  ships its **least interesting** program — the tutorial one, `print([1, 2])`, `xs.sort()` — because
  a corpus grown from bug reports only tests what we already had reason to doubt. The oracle's first catch was not a refusal but a passing
  build: `xs[0] = "z"` on a mixed list answered `[1, 'a', None]`, the interned index printed
  through the slot's stale tag, and it is now ADR 0187 and two parity programs
  (`mixed_element_reads.gy`, `mixed_element_writes.gy`). Its third catch went the other way: `xs == ys`
  for two equal lists answered False on both backends (the comparison compared heap handles), while
  `[0] == ["zero"]` answered **True** — an interned index matching a number. Containers now compare
  by value, element by element, as `(payload, tag)` pairs (ADR 0189). The same sweep's second find
  is closed too: `xs.sort()`, `xs.reverse()` and `sorted(xs)` are now language surface on both
  backends, with one comparator that orders interned strings by their **text** rather than by the
  index they were interned at (ADR 0191). And `[f(x) for x in range(5)]` — a comprehension whose
  element is a call — now compiles: the AOT path had made the constant folder the *meaning* of a
  comprehension, and the ordinary list-building idiom refused with "comprehension element must be
  constant" while the interpreter ran it happily (ADR 0192). Its second catch was a crash in the most
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
  language into the states a construct is allowed to be in: **9** CPython-equal on both backends,
  **37** absent (both engines refuse, honestly), **16** refused by the compiled backend alone
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
go test -tags=llvm20 ./pkg/...        # unit tests
go test -tags=llvm20 ./integration/... # end-to-end pipeline
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
