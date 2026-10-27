# gusty language

## One backend

There is one execution path in this language: check → LLVM codegen → `llc`/`cc` → the artifact. The AST
interpreter that used to run `--eval`, the REPL and `--interp` is retired (ADR 0302), so a passage below
that says "the record answers X" is one of two things, and the sentence's own tense tells you which:

- **history** ("the record answered `[]` here, which is why the shape is a defect"): keep it — that is
  the measurement the row was filed from, and the answer itself is preserved, per source, in
  `pkg/lang/testdata/interpreter-golden.json`;
- **a live claim** ("supported in both the record and the compiled backend"), left over from the two-engine
  wording: read it as *the compiled
  backend is what runs, and the record is what the language owes there*. Where the two differ, the case is
  a row in `testdata/interpreter-golden-drift.json`, not a passing test, and where the compiled answer also
  differs from CPython it is a row in `integration/testdata/cpython-debt.json` naming the roadmap row that
  owes the fix. `docs/operations.md` §"The suite's own interface" is the contract for both files.

A capability the retired engine had and the compiler does not is a **gap with an owner**, never a
fallback: `docs/operations.md` §"One backend, and what it does not lower" lists them (nested closures,
a function *read* as a value rather than called, run-time iteration and `sorted` over a computed container,
`dict(<container>)` copies) with the row that owes each. A function *called* as a value answers — a higher-order
body is cloned per tuple of functions its call sites ask for (**ADR 0345**, roadmap L11.7).

Refusing is therefore part of the language's surface, and it is documented as such (**ADR 0346**, roadmap
L11.8). Each shape the compiled backend declines has a stable **code** — `lower.unsupported.<shape>` — beside
the **suggestion** that writes the same intent in what the backend does answer and the **roadmap rows** that
own the missing half. `gustyc --capabilities` prints that inventory, `gustyc --schema` declares it
(`definitions.capabilityRefusalCode` is the closed enum), and a `--json` failure carries the entry as its
`capability` member. A refusal is not a diagnosis of the program: a program the reference runs and this
backend refuses is a divergence with a row, which is why the code names the rows rather than only the shape.
Where the program is the one that is wrong — an undefined name, a non-exhaustive `match`, a text used as a
number — the payload's `capability` is `null` and the `phase` field says who spoke. The prose a refusal is
written in is in `docs/operations.md` §"Codegen capability messages", and the table there is printed from
the same registry that answers `--capabilities`.

## Gradual typing

`--verify` statically checks assignment annotations against the inferred type
of the value: `x: int = "hello"` reports `type mismatch: expected int, got
str`. The `any` annotation accepts any inferred type. Runtime enforcement
(`checkAnnot`) still applies for computed values. See ADR 0096.

## Comprehensions

List, dict, and set comprehensions are supported on the compiled backend. Dict/set comprehensions use
`for` inside the braces, Python-style: `{x: x*2 for x in [1,2,3]}` builds a
dict, `{x for x in [1,2]}` a set. The `if` filter belongs to its clause. Constant integer operands may fold
into a module-global container; anything else is built at run time through tagged slots (ADR 0384). See ADR 0095.

**A comprehension carries a clause list** (ADR 0383, closing Gap R.352). A `for` clause may be repeated —
`for NAME in ITER [if COND]` each time, read left to right, each clause running inside the ones before it and able to see the
targets they bound. That is the whole of the flattening idiom and the whole of the product:

```gy
print([y for x in [[1, 2], [3, 4]] for y in x])   # [1, 2, 3, 4]   — rows of rows, walked one level in
print([[x, y] for x in [1, 2] for y in [3, 4]])   # [[1, 3], [1, 4], [2, 3], [2, 4]]
print([x*10 + y for x in range(2) for y in range(2)])   # [0, 1, 10, 11]
print([x + y + z for x in [1] for y in [2] for z in [3]])   # [6]
```

**A filter belongs to the clause it follows**, and it decides whether the whole rest of the nest runs — which is the difference
between `[2, 3]` and `[2, 3, 2, 3]`:

```gy
print([x for x in [1, 2, 3] if x > 1 for y in [10, 20] if y > 10])   # [2, 3]
print([y for x in [1, 2] if x > 1 for y in [7, 8]])                  # [7, 8] — the outer filter skips the inner loop
```

Each clause's target is a binding of the frame that hosts the comprehension (ADR 0381), installed once per clause, so an element
reads every one of them the way it reads a loop variable; a later clause may rebind an earlier spelling, and then the element sees
the later binding (`[x for x in [1] for x in [2]]` is `[2]`). A generator expression carries the same list, and looping over one
walks the product: `for v in (x + y for x in [1, 2] for y in [3]):` prints `4`, `5`.

What the compiled backend does not yet walk refuses by naming itself (exit 1, one capability code each, `llc` never reached): a
**clause over a container the run time built** — `rows = [[1, 2], [3, 4]]` / `[y for x in rows for y in x]`, where one clause over
the same name answers, because that loop is a real loop and the nest is unrolled (roadmap **Gap R.356**); an **element that is
itself a container storing a clause's non-number binding** — `[[x, y] for x in [1] for y in [[3]]]`, whose static table keeps one
word per slot and the tag that gives the word meaning is not in it (**Gap R.355**); a **set or dict nest whose element is not a
constant** now uses ADR 0384's tagged builder; and a **clause over a text or a float**, which is refused one clause in too (`L11.1`).

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

**A set or dict comprehension asks the fold first, then builds what the fold cannot name** (ADR 0384,
closing Gap R.353). `{x * 2 for x in [1, 2, 2]}` and `{x: x * 2 for x in [1, 2]}` still fold into static containers.
A call, lambda, method result, float, `None`, verdict, text, or container cannot be named by that fold, so it
enters the same runtime loop as a list comprehension. Each member, key, and value is written as a payload plus
its tag; only the member/key is hash-checked, while a dict value may itself be a container:

```gy
print({(lambda z: z + x)(1) for x in [10, 20]})       # {11, 21}
print({x: (lambda z: z + x)(1) for x in [10, 20]})    # {10: 11, 20: 21}
print({None for x in [1]})                           # {None}
print({1: None for x in [1]})                         # {1: None}
print({True for x in [1]})                            # {True}
xs = []
xs.append(6)
print({v: v / 2 for v in xs})                         # {6: 3.0}
xs = ["a", "b"]
print({x: x.upper() for x in xs})                     # {'a': 'A', 'b': 'B'}
print({x: [x] for x in [1, 2]})                       # {1: [1], 2: [2]}
print({[1] for x in [1]})                             # TypeError: unhashable type: 'list'
```

The textual repr of a set is not an ordered fact: CPython randomizes string hashing. The backend pins the
members and tags rather than whether `{'A', 'B'}` or `{'B', 'A'}` comes out. A dict's insertion order remains
visible and pinned.

**A literal comprehension iterable is a container, not a demand for integers** (**ADR 0386**, roadmap **Gap R.358**).
An all-integer literal such as `[1, 2]` still folds to its static list. Any other literal display — texts, floats,
`None`, verdicts, tuples, containers, or items computed by calls — is walked item by item, and each item binds the
loop variable with the tag the unrolled `for` road records:

```gy
print([x for x in ["a", "b"]])                        # ['a', 'b']
print({k: 1 for k in ["a", "b"]})                     # {'a': 1, 'b': 1}
print([x for x in [1, "a", 2.5, None, True]])          # [1, 'a', 2.5, None, True]
print([x for x in [None, True, False]])                # [None, True, False]
print([[x] for x in [None, 1.5, True]])                 # [[None], [1.5], [True]]
print({x: x for x in ["a", 1, 2.5]})                   # {'a': 'a', 1: 1, 2.5: 2.5}
print([x for x in [[1], [2]]])                         # [[1], [2]]
print([x for x in [1, 2]])                              # [1, 2] — still the integer fold
```

The boundary is the position that consumes the value, not the iterable. Container writes, dict entries, membership,
printing, text methods and filters ask the tag. A one-word arithmetic position does not yet carry it, so
`[x + 1 for x in ["a", "b"]]` is refused rather than turning the text's string-table index into a plausible number.

**A `{…}` display ends at its brace.** A `for` after a closing brace belongs to the display whose brace is
still open (ADR 0384). Thus `{{1} for x in [1]}` is a set comprehension over an inner set literal, not a
literal set containing a set literal; its member asks the hashable question and CPython's `TypeError` reaches
the program's handler. The parenthesized spelling `len({x * x} for x in xs)` remains a generator expression.

**What the loop variable knows** (ADR 0244, ADR 0245, Gap R.46, Gap R.76). An element that *is* the loop variable
carries its kind — and, over a container whose slots mix kinds, its **tag** — into the list it builds,
asked of the container being iterated, because the loop variable's own facts are gone once the loop
closes. That is what makes `print(out)` and `print(out[0])` agree on `['a']` and `a` instead of one
printing text and the other the interned index, and what lets `[x for x in {1, "a", None}]` print
`[1, 'a', None]` on the compiled backend instead of `[1, 0, 0]`. A verdict is asked the same way
(ADR 0259): `[x for x in [True, 1, 1]]` builds `[True, 1, 1]`, because `x` says True only if the item
it copied does, and the constant fold declines to write such a list into a global that has no tag
table. Iterating a dict walks its keys at the
stride its two-word entries need (ADR 0188), and a dict comprehension writes each entry as two
`(payload, tag)` pairs, so `{k: 1 for k in d}` produces an entry `out["a"]` can find. A slot of that
list answers an equality the way the printer already answers `print`: `out[1] == "a"` asks the slot's
own tag (ADR 0247, closing Gap R.79). What still refuses by naming itself is a comprehension over a name the
compiler kept as a compile-time list. The set and dict verdict shapes (`{True for x in [1]}`,
`{1: True for x in [1]}`) are answered by ADR 0384's tagged runtime builders, not refused.

## A comprehension has a scope, and its target belongs to it

A comprehension is a binding construct, not a loop written at the use site. Its target is a binding
**of the comprehension**: the module never learns the spelling, and the two programs below are the same
program to this back end and to the reference — the second is refused, in the reference by
`NameError`, here by the shared front end (ADR 0381):

```
print([x for x in [1, 2]])   # [1, 2]
print(x)                     # error: undefined name "x"
```

What that binding *is*, inside the comprehension, is the part a program can see. An element position
reads the target the way any name is read, and a `lambda` body written inside the comprehension reads
it too — the body is emitted as a function of its own, and the target reaches it as one more parameter
that the application fills from the loop's own slot, which is how a `for` variable and a `def` local
have always travelled (ADR 0370, ADR 0380, and roadmap L11.7 · Gap R.348 for this case):

```
print([(lambda z: z + x)(1) for x in [10, 20]])                     # [11, 21]
print([(lambda y: (lambda z: z + x + y)(1))(2) for x in [10, 20]])  # [13, 23]
print([(lambda z: z + x)(1) for x in [10, 20] if x > 15])           # [21]  — the filter is one scope too
```

Three rules come with it, and each is observable:

- **The nearer binding answers.** If the module also spells the name, the target wins —
  `x = 5` then `print([(lambda z: z + x)(1) for x in [10, 20]])` is `[11, 21]`, not `[6, 6]`. This is
  the same precedence a `def` local has over a module global of the same spelling.
- **A body owns only what is written inside it.** A `def` whose body hosts a comprehension binds that
  comprehension's target; the comprehension the module bound is a different name, and reaches the body
  as a parameter if the body reads it at all.
- **A target still has to fit in the word a capture travels in.** With `xs = [[1], [2]]`, the program
  `print([(lambda z: x)(1) for x in xs])` is refused in words (it names roadmap L11.1): a capture
  crosses as one `i32` and cannot say what the value is, which is the same gate that refuses a captured
  container, text or float anywhere else, reached by a loop variable instead of a local. Reading the
  target *as an argument* — `[(lambda fn: fn(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]]`, which is
  `[4, 6, 8]` — is a different door, and answers for values this one refuses.

The mirror image is worth stating because the two roads look identical in the source: a **`for`
statement's** target at the module level *is* module state, because the reference leaks it
(`for x in [1, 2]: pass` then `print(x)` prints `2` there and here). A compiled body that reads such a
name meets the module-global rule (roadmap Gap R.35) instead, in different words, on purpose.

## Slicing (`s[a:b]`, `s[::step]`, negative indices)

Sequence slicing is supported on strings and lists:

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
  AOT backend (roadmap L11.5) — CPython and the retired engine's record answer it, and the compiled
  backend refuses with a message naming that gap rather than answering zero.
- A zero step is an error.
- Slicing a string returns a string; slicing a list returns a list.

String slicing works on a value the compiler can see; a string held in a *variable* is still limited
(string *literals* are supported inline), so list slicing is the primary native path. The index arithmetic
lives in `pkg/lang/fold.go` (`pySliceIndices`, `normPosIndex` — shared code the retired engine and the
codegen both used, which is why it survived the engine), and the lowering in the `rt_slice` runtime helper
in `pkg/lang/codegen_ir_slice.go`.

## Pattern matching

`match` supports list-destructuring patterns: `case [a, b]:` matches a list
subject element-wise, binding Name pattern elems to the subject's elements
(e.g. `match x: case [a, b]: a + b` unpacks to 30 for `x = [10, 20]`). A
non-Name element is compared element-wise; mismatched arity or kind falls through
to later cases — the subject has to *be* a sequence for a sequence pattern to
apply (ADR 0358). See ADR 0091.

A bare-name pattern (`case y:`) is *irrefutable*: it always matches and binds
the subject. `case _:` is a wildcard: it always matches and binds nothing.

**Exhaustiveness (ADR 0154).** A `match` is *exhaustive* iff at least one
case is irrefutable (a bare `Name`, including `_`). A `match` with only
refutable (literal) cases is non-exhaustive: `gusty check` emits a mypy-style
warning, so you know to add a `case _:` fallback. Warnings don't fail the
check (only errors do).

**Definite assignment (ADR 0154, ADR 0228, ADR 0358).** A name bound by an irrefutable case that
fires on *every* path is definitely assigned and readable after the match. A name bound on only
*some* paths is not, and reading it where no arm bound it is a **run-time trap**, not a compile-time
error and not a value: `NameError: name 'y' is not defined` in the module's own statements,
`UnboundLocalError: cannot access local variable 'y' where it is not associated with a value` inside
a function, each catchable by its own class (`ADR 0212`'s rule for a built-in trap, and the
reference's own split of the two events — which frame owns the name decides the class).

**A capture is a binding, so it happens where the arm that made it runs (ADR 0358).** `matchPattern`
is called to decide *which* arm runs — every arm's pattern is lowered, in order, before any arm's
body — so the store that binds a capture lives in the block the branch enters, not where the pattern
was lowered. `v = 5` / `case [y]: pass` / `case 5: pass` / `print(y)` therefore raises; until ADR
0358 it printed `0` and exited 0, because the arm that lost had stored the capture on its way past.
Two placements the reference decides rather than the implementation:

* a pattern that **matched** whose `if` guard then failed **keeps** its binding — `match 5:` /
  `case z if False: pass` / `case _: pass` / `print(z)` prints `5` — so the binding lands in front
  of the guard, and a guard may read the capture its own pattern bound (`case z if z == 5`);
* an arm written `case p0 | p1 | …` is decided one alternative at a time, and the alternative that
  matched binds the captures — `case [a] | {"k": a}:` over `[3]` binds `3` and over `{"k": 8}` binds
  `8`. One arm block cannot hold both stores, which is why each alternative ends in its own entry
  block.

**A pattern asks what the object *is* before it asks the object anything (ADR 0358).** A sequence
pattern requires a list or a set, and a mapping pattern requires a dict. `d = {"k": 1}` /
`case [a]:` is a match that **failed** — the reference falls through to the next arm — and not a
one-element list whose element is the dict's first slot.

**What the pattern road does not answer.** A nested sub-pattern binds nothing (`case [[a]]:` is
refused — `Gap R.301`), a parenthesised tuple pattern is not a pattern at all (`case (a,):` —
`Gap R.302`, blocked on L11.3), a class pattern's keyword attribute does not parse
(`case Point(x=1):` — `Gap R.303`), and a positional class-pattern argument names an attribute
instead of asking the class for its `__match_args__` (`Gap R.304`). A capture of a container slot
whose slot carries its own tag binds the payload word and not the `(payload, tag)` pair, so a captured
text prints as a number (`Gap R.305`). Each of these is refused in words or pinned with today's
answer beside the reference's — none of them answers silently.

**AOT lowering.** Guards, or-patterns, the `_` wildcard, list-destructuring patterns, dict patterns
and class patterns (subclass walk, per-capture presence test, aliases resolved by the front end) all
lower to native code (`codegen_match.go`, ADR 0235). What a pattern still cannot ask a value is a question
about L11.1's tagged value word, which is the shape the four refused forms above are waiting for.

## Standard library

Builtins include `len`, `print`, `range`, `min`, `max`, `zip`, `int`,
`float`, `str`, `sum`, `abs`, `sorted`, `reversed`, `enumerate`, `any`,
`all`, `chr`, `ord`, `round`, `floor`, `ceil`, `sqrt`, and the string methods
`upper`/`lower`/`capitalize`/`title`/`swapcase`. `any(iter)` is 1 if any element is truthy;
`all(iter)` is 1 if all are; `chr(n)` makes the single-char string for a
codepoint; `ord(s)` reads the first char's codepoint. In the AOT codegen, `chr(n)` folds a constant codepoint to a single-character string global and `ord(s)` folds a constant string to its first-byte codepoint (mirroring the record's `sval[0]`), both as literal folds.

`round(x)` is the nearest-value rule with **ties to even** — IEEE `roundTiesToEven`, the rule CPython
uses: `round(0.5)` and `round(1.5)` are `0` and `2`, `round(2.5)` and `round(3.5)` are both `2`, and
`round(-2.5)` is `-2`. An integer argument is returned unchanged. The compiled backend ask for the named
IEEE operation rather than implementing a rounding rule — `math.RoundToEven` in the evaluator,
`llvm.roundeven.f64` (and the same fold for a constant) in the compiled backend — because two
independent implementations of "half away from zero" agreed with each other for as long as nobody
asked CPython (Gap R.50, ADR 0236).

`round(x, ndigits)` **moves the decimal point instead of asking for a whole number, and answers with
the kind the value arrived as** (Gap R.69, ADR 0263): `round(2.345, 2)` is `2.35`, `round(3.5, 0)` is
`4.0` — the trailing zero of a float answer, not `4` — `round(2.675, 2)` is `2.67`, `round(2500, -2)`
is `2500`, `round(1234.5678, -2)` is `1200.0`, and an integer or a verdict keeps being an integer
(`round(5, 2)` is `5`, `round(True, 2)` is `1`, `round(2.345, True)` is `2.3`). What rounds is the
*exact decimal value of the double*, ties to even — **not** the value scaled by `10**n` in binary,
which manufactures ties the value never had: `0.005` is `0.00500000000000000010408…` and so rounds up
to `0.01`, but times 100 it is exactly `0.5`, and a nearest-even rule answers that `0`. The same
family gives `0.025`→`2.5`→`0.02` (CPython `0.03`) and `0.075`→`7.5`→`0.08` (CPython `0.07`). The
answer is the nearest double to the rounded decimal. So this is a second named operation, not
`roundeven` again: `strconv.FormatFloat`/`ParseFloat` in the evaluator, and `snprintf("%.*f")`/`strtod`
in the compiled runtime's `rt_round_digits`, both correctly rounded, both reached from the one Go rule
in `pkg/lang/round_digits.go`. The two were swept against CPython over 531,272 `(value, ndigits)` pairs
compared bit for bit; the residual class — 143 cases, all between |x| = 4.117e18 and 1e300 with a
negative digit count, all one ULP, where the scale itself is the inexact step — is roadmap's, not hidden
under a green test.

Two clamp facts come with it: every double is exactly a decimal with at most 324 digits after the
point, so a digit count of `324` or more is the identity; and `10^309` is beyond the largest finite
double, so a digit count of `-309` or below answers zero with the sign kept — which is what CPython
answers there too. Infinity and NaN are handed back (`round(inf, 2)` is `inf`).

A digit count that is not an integer is CPython's `TypeError`, **raised** on both legs and
catchable on both — `round(2.345, 1.5)` gives `'float' object cannot be interpreted as an integer`
under `--interp` and `--aot`, and a `try`/`except TypeError` takes the branch on each (exit 3, not a
refusal: ADR 0166 keeps exit 1 for programs the reference itself rejects). `round()` with no argument
and `round(x, 1, 2)` with three answer an arity error in one shared sentence on both legs;
four other builtins (`int`, `float`, `ord`, `chr`) still panic the evaluator on that shape and are
roadmap Gap R.131's.

What the digit count cannot follow is a value whose kind the module cannot see: `round(v, 2)` of a
parameter the call filled with a double answers `2` rather than `2.35` on the compiled leg, and a
loop variable off a literal list of doubles is worse (Gap R.129, Gap R.130 — the tagged value word's,
L11.1, as with Gaps R.107–R.110). Bound the value in the body instead (`v = v * 1.0`) and the answer
arrives.

`float(x)` folds a constant int to itself and a constant string to its parsed
then-truncated float value in the AOT codegen (the backend represents floats
as truncated ints, mirroring value()'s FloatLit handling).
floats. See ADR 0090 — and Phase 2's L11.6 for the float paths still open (a float reaching an
untyped function parameter, `/=` → float, stdlib constants keeping their type). The `floor`/`ceil` →
`int` half of that list is closed: see the two paragraphs below (Gap R.51, ADR 0264).

### `floor` and `ceil` answer a whole number; `sqrt` answers a float

`floor(3.7)` is `2`-style: **a whole number, printed without a decimal point** — `floor(2.7)` is `2`,
`floor(-2.7)` is `-3`, `ceil(2.2)` is `3`, `ceil(-2.2)` is `-2`, `ceil(-0.5)` is `0`, `floor(7)` is `7`,
`floor(True)` is `1`. They are the reference's `math.floor` / `math.ceil`, which return an `int`, and a
backend that prints `3.0` for `floor(3.7)` has shown the program a number it never wrote (Gap R.51,
ADR 0264). `sqrt` is the other kind of question and keeps its fractional part: `sqrt(9)` is `3.0`,
`sqrt(2.25)` is `1.5`, `sqrt(2)` is `1.4142135623730951`. The checker types the three the same way
(`floor`/`ceil` → `TInt()`, `sqrt` → `TFlt()`), because a name the checker types one way and the codegen
lowers another is exactly how this stayed invisible.

A whole-number answer beside a float is **widened, never truncated**: `floor(2.7) + 1.5` is `3.5` and
`floor(2.7) * 3` is `6` (an exact product of whole numbers stays a whole number), and the answer carries
into a container (`[floor(2.7), ceil(2.2)]` is `[2, 3]`), into `str` (`str(floor(2.7))` is `2`), into an
f-string (`f"{floor(2.7)}"` is `2`), into a comparison (`floor(3.7) == 3`, `sqrt(4) == 2`) and across a
call boundary. Compiled, the pair is `llvm.floor.f64`/`llvm.ceil.f64` carried in the int word, and the
float road lifts it back with `sitofp` — ADR 0243's (payload, widen) step for an element, for the same
reason.

**Every bad argument is a raise the reference wrote, on the compiled backend** (exit 3, catchable by class):

| program | both legs and CPython |
|---|---|
| `floor("a")`, `sqrt("a")` | `TypeError: must be real number, not str` |
| `ceil(None)` | `TypeError: must be real number, not NoneType` |
| `floor([1])`, `sqrt({"a": 1})`, `ceil({1})` | `TypeError: must be real number, not list` / `dict` / `set` |
| `sqrt(-1)`, `sqrt(-1.0)`, `sqrt(-inf)` | `ValueError: math domain error` |
| `floor(float("nan"))` | `ValueError: cannot convert float NaN to integer` |
| `ceil(float("inf"))` | `OverflowError: cannot convert float infinity to integer` |

The kind in `must be real number, not str` is **not** quoted, unlike this language's other `TypeError`
family (`'str' object cannot be interpreted as an integer`, ADR 0263): the two sentences are CPython's own
pair, and a program's `except` matches on the text a user reads. `sqrt` of a negative is a *run-time*
raise, not a compile-time refusal — `try: sqrt(n) except ValueError:` has to stay writable — and
`OverflowError` is in the exception table (tag 9) so `except OverflowError:` means the same thing on all
both legs. `floor()`, `floor(1, 2)` and the like answer one shared arity sentence per name, exit 1
compiled and exit 3 interpreted, and exit 2 nowhere (ADR 0211).

Two walls stay open, both filed with a pin per leg rather than smoothed over. **Past the compiled int
word** the engines disagree: `floor(3000000000.0)` is `3000000000` in the evaluator (whose ints are
`int64`) and on the compiled leg an `OverflowError: floor: the whole number is beyond the word this
backend's int holds (roadmap L12.12)` — catchable, and raised rather than letting `fptosi` answer poison,
because a silently-wrong number is Gap R.64's existing complaint (Gap R.133, owned by L12.12). And a
**loop variable off a literal list of doubles** has no kind for the call to follow: `for v in [3.7]:
print(floor(v))` is `3` interpreted and `0` compiled (Gap R.130, the tagged-value word's, L11.1).
Neither is asserted as correct anywhere; both are pinned where they are filed.

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
unconditionally live, because the record may still hold it in a register.

Observable consequences, and the limits, in one place:

- A long loop or a long REPL session reclaims as it goes; the heap is bounded by live
  data rather than by everything the session has ever built.
- A value that lives only in a live frame's local survives collections — recursion
  that binds a list, recurses, allocates, and then reads the list back is correct
  (`integration/programs/gc_precise.gy`).
- Conservative by design: garbage created inside a call that is *nested in an
  expression* is not reclaimed until the enclosing statement completes. Only the
  value model of roadmap Phase 11 (a real value stack) removes that limit.
- The collector's own report (`--gc-stats`, and `lang.SetGCReport` for a caller in-process) is the way a
  program says what the collector did: `collections`, `roots`, `marked`, `freed`, `live`, and the heap's
  `top`. The retired engine had a `Collect()` entry point on its own heap; the precise-root collector is
  now the compiled runtime's, entered by the allocation watermark crossing its threshold.
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
`closure`, and `bool` for an expression that answers a question — `True`, a
comparison, `not x`, `all(...)` — see ADR 0257). A verdict echoes as `True`/
`False` under plain `--eval` too, so `gustyc --eval '1 == 1'` says what
`python -c 'print(1 == 1)'` says. See ADR 0094.



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
the record and the AOT backend agree on `0xFF + 0b101 + 0o17 + 1_000`.

**The exponent form is not in the number lexer yet**: `1e18`, `1.5e-3` and `2E8` are a parse error on both
engines (`parse error at 1:8: expected ")"`, exit 1) where CPython parses them as `1e+18`, `0.0015` and
`2e+08` — the `e18` is read as a name after the number `1`. Write `1000000000000000000.0`, or
`float("1e18")`, whose *string* is parsed. Filed as roadmap Gap R.135 with a probe
(`programs/probe_float_literal_with_exponent`, an `oracle: debt` row) so the row cannot be forgotten while
scientific-looking values are being typed into a program.

**The bare-dot float is not in the number lexer either**: `print(.5)` is `0.5` in the reference and
`parse error at 1:7: unexpected token` (exit 1) here, and with it `print(1+.5)`, `print(-.5)`, `print(.5+.5)` and
`x = .5` / `print(x)`. Write the leading zero — `0.5`, `1_0.5`, `1.5` — and the literal lexes, folds and prints
today; `lexNumber` is entered on a digit and on the base prefixes, and there is no case for a leading `.`, so the dot
reaches the operator path and the parser meets a `.` where a primary belongs. Growing that case has the same
constraint ADR 0338 met with `-`: it may be entered only where the `.` cannot be the member operator, so `obj.5`
stays the attribute error the reference reports instead of becoming a float. Filed as roadmap **Gap R.245**, beside
**Gap R.135**'s exponent marker — the neighbouring half of the same function.

**The two values with no decimal spelling are `float("inf")` and `float("nan")`** (with `-float("inf")`
for the third). They print `inf`, `-inf` and `nan` on every engine, and they survive a binding, an
arithmetic step and a `sqrt` (`sqrt(inf)` is `inf`, `sqrt(nan)` is `nan`, `sqrt(-inf)` raises
`ValueError: math domain error`). A folded one used to reach the module as `inf.0e+00` — Go's spelling
plus this emitter's exponent suffix — which LLVM 20's parser has no token for, so `llc` stopped and the
program died with exit 2; the emitter now writes the IEEE bit pattern (`0x7FF0000000000000`,
`0xFFF0000000000000`, `0x7FF8000000000000`), and both bad spellings are on the shared `forbiddenIR`
blacklist every codegen test runs (Gap R.134, closed alongside `floor`/`ceil`/`sqrt` by ADR 0264).

### A `-` before a digit is asked what it follows (Gap R.242, ADR 0338)

A minus has two jobs in this language, and the lexer used to decide between them by looking at
the character **after** it: `-` followed by a digit was read as one negative integer, wherever
it appeared. That is right for `x = -1`, `xs[-1]`, `f(-1)` and `{-1: "neg"}`, and wrong
everywhere else. `y = x-1` was `y = x` followed by an expression statement `-1`, so the program
printed the value the subtraction never produced — `5` where CPython prints `4`, at **exit 0**,
with no diagnostic anywhere to read. The other face met an expression the parser could not
swallow: `print(1-1)`, `print(x-1)`, `print(len(xs)-1)`, `print(xs[2-1])`, `print(3.5-1)`,
`for i in range(3-1):` and `def g(n): return n-1` all died with `parse error: expected ")"`.
Write the same expression with a space (`x - 1`) and it was right, which is the only reason a
corpus written in this house's style never saw it.

The rule now asks the token **before** the minus, which is already in hand where the token is
emitted:

- after anything that can **end** an expression — a number, a name, a text, a closing `)` `]`
  `}`, or one of `True` / `False` / `None` — the `-` is the subtraction operator and the parser's
  existing unary-minus rule takes whatever comes next, which is why `5--1` and `5- -1` keep
  answering `6` and `x -= 1` still decrements; and
- everywhere else — the start of a line, after an operator, after `,` `:` `(` `[` `{` `=`, after
  a keyword like `return`, `in` or `else` — `-N` stays **one literal**, so no index, call
  argument, dict key, parameter default or `print(-1)` changed answer.

`x -= 1` is still the longest match, and a `-` inside a text or a comment is not a token at all.
The token stream is asserted both ways in `pkg/lang/lexer_test.go` (`INT:-1` beside
`OP:- INT:1` is the whole difference), the parse is asserted as *one* statement rather than two,
and `integration/minus_operator_test.go` asserts the arithmetic is in the module (`sub i32` in
`@main` or a `@gy_*` function) — before the row, the module for `y = x-1` contained no
subtraction at all.

Two shapes stand outside the row, both filed rather than smuggled in: **`+` before an operand is
refused** (`print(+1)` is `parse error: unexpected token` where CPython prints `1`) because the
grammar has no unary-plus rule at all — roadmap **Gap R.244** — and a comprehension whose
iterable is a list the compiler kept as a compile-time constant still refuses (roadmap
**L11.1**, the tagged value word, and ADR 0192 is that refusal's own record).

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
rebind a variable in the current scope. A subscript target (`xs[0] = 9`, `d["k"] = v`) and an attribute target
(`c.v = 7`) are supported too, and a tuple target unpacks (`a, b = [1, 2]`). What a **store** needs is a container it
holds in a variable: `f()[0] = 3`, `[1, 2][0] = 3`, `d["x"][0] = 1` and `c.a[0] = 3` are refused with
``item assignment needs a container variable on the left (`d[k] = v`), and the program writes into the call `f(…)``` —
CPython performs those writes into a temporary, so the narrowing is **Gap R.381** (the read side of the same position
answered years ago, ADR 0334). An attribute store needs an instance the compiler typed: `(1).x = 2`, `"ab".x = 2`,
`xs.pop = 3` are refused, and the reference raises `AttributeError` for those programs too (**Gap R.272** owns the
attribute road's remaining half).

### Augmented assignment

`target op= expr` reads the target, applies an arithmetic operator to the RHS,
and writes the result back:

- `x += e`  (add), `x -= e`  (subtract), `x *= e`  (multiply),
  `x /= e`  (integer divide), `x //= e` (integer divide), `x %= e` (modulus).

The target must be a Name or an attribute (`self.x += 1`). Anything else is refused where it is parsed, in the
program's own words — ``augmented assignment (``+=``) needs a name or an attribute on the left, and the program puts the
subscript `xs[0]` there`` — and the code generator already lowers an `*ir.InstIndex` target, so that one sentence is
narrower than the compiler behind it: **Gap R.380** owns widening the rule (a number, a call or a list literal on the
left stays a refusal, because CPython rejects those with a `SyntaxError` too). Augmented assignment is answered by the
compiled backend (int and float Name targets, int Attr targets).

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
terminator semantics — identical on the record and the AOT backend
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
  literal spelling), and the compiled backend call the same runtime printers that a container variable
  uses. What it must never print is its own representation: not the handle (`0`), not the static
  elements global, and not the interned indices a recycled heap slot happened to inherit — all
  of which happened here, in programs that compiled, verified and exited 0.
- **A `print` statement writes one line, and writes it after every argument has been evaluated**
  (Gap L.5, ADR 0355). An argument whose own evaluation prints — a call whose body prints — writes
  **its own line first**, exactly as Python does: with `def twice(v): print("<<", v, ">>")` /
  `return v + v`,

  ```gusty
  print("got", twice(21))
  ```

  prints `<< 21 >>` then `got 42`. The same rule gives `print("a", boom())` a line that **never
  appears** when `boom()` raises: the statement's writes are held until the whole argument list has
  been evaluated, so a trap in a later argument leaves no half-written line ahead of the traceback.
- **One shape still interleaves, and says so.** When an argument's *rendering* is itself conditional
  — a union-annotated value printed from its tagged arm, as in `x: int | str = 1` and
  `print("a", x, p())` where `p` prints — the write belongs to the arm that ran, and moving it to the
  end of the statement would print it on the arm that did not. That statement keeps the ordering it
  always had (`a 1 S` / `2` where Python writes `S` / `a 1 2`), the gate names its reason
  (`an argument writes inside a block its own lowering opened`), and the shape is pinned
  filed-not-fixed in `TestWhatTheRewriteStillCannotReachIsFiledNotFixed`. What it owes is the union
  box's printer writing its line once from the merge block, which is L11.1's tagged value word.
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

Supported in the compiled backend, and recorded against it.

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
comprehension's `if` as its own and demanded an `else` (`expected keyword "else"`, on the compiled backend,
while `[x for x in xs if x > 1]` worked: the brace branches had simply never been given the
`or`-precedence fix the list branch already had).

What still refuses in the compiled backend refuses *with the list spelling's words*, asserted in
`TestContainerComprehensionRefusalsStayHonest`: an iterable the escape analysis folded away
(`xs = [1, 2, 3]` with no mutation, then `{x for x in xs}` — `xs.append(...)` materialises it; L11.1's
tagged element retires the category), and a non-integer iterable, element or key (L11.5, L11.6).
A container **returned** from a function is a value, as of ADR 0333: `return [1, 2]`, `return {1, 2}`,
`return {"a": 1}`, `return set()`, a container the body built (`xs = []` / `xs.append(1)` / `return xs`), a
loop-filled list, a comprehension and a forwarded answer (`return build()`) are all materialised at the
`return` and read back as the object they are — `print(f())`, `str(f())`, `len(f())`, `2 in f()`,
`not f()`, `if f():`, `for v in f():` and `f() == [1, 2]` answer the way the bound form `ys = f()` always
did, because they are the same read of the same handle. The function is treated as a container only when
**every** `return` in its body answers one and the same kind and the body cannot fall off its end; a body
whose arms disagree keeps the one-word road it always had. A subscript of the answer is a value too, as of
ADR 0334: `print(f()[0])` prints `1`, `print(f()[-1])` counts from the end, `print(f()[7])` raises the
reference's catchable `IndexError: list index out of range`, `print(f()["b"])` hashes a key out of a dict
answer, and `f()[0][1]` reads a slot of a slot — the call is lowered to the same rooted name a binding
records, the subscript node is copied with that name as its base, and the roads that read `ys[i]` for
`ys = f()` answer it, in the same order, with the same bounds check (roadmap **Gap R.214**, paid). What still
has no arm — the answer as another literal's element or as a method receiver (**Gap R.229**), a slice of the
answer (**Gap R.229** beside **Gap R.230**, whose road answers words for every base), the answer through a
parameter (Gap R.154), `f() + [9]` (Gap R.175), a membership test whose needle is one of its slots (Gap R.232)
— refuses and names the function and the door it is waiting for; none of them prints the handle. `None` handed
back by a function is the one value in this family still answered as a number (**Gap R.228**).
A container the answer table *declines* is still a container, as of **ADR 0344** (roadmap **Gap R.261**):
when a slot is filled by a value the scan cannot follow — a call, a comprehension, a format — the function is
recorded with its kind and without a description of its slots, and which positions may still read the answer is
asked of that record, not of each door. A position whose question the **object** answers prints it: `print(f())`
is `[5]` over `out.append(g())`, `{5}` over `out.add(g())` and `{'k': 5}` over `out["k"] = g()` (and `{'k': 2.5}`
when the callee answers a float) — each of which printed `0` at exit 0 before the row; the same for
`str(f())`, `repr(f())`, `if f():`, `not f()` and `ys = f()` / `print(ys)`, and a membership test keeps asking
the object, so `5 in f()` is `True`. A position that reads a slot back through the compiler's own records — an
index, a slice, a comparison, a loop's iterable — refuses naming the missing half and the way around it
(`bind it to a name and print that`), because a notebook nobody filled is a fabricated number: `print(f()[0])`
and `for v in f(): print(v)` are **Gap R.262**'s, the second of which printed nothing at exit 0 before the row.
A comprehension over a **text** is refused naming its missing runtime door (`Gap I.2`), where the retired engine
evaluated it and the record still holds its answer.

### What a comprehension's iterable may be (LLVM codegen)

A comprehension has two routes, and which one runs is a fact about the iterable (ADR 0192, ADR 0357):

| the iterable | the route | example |
|---|---|---|
| an inline list literal of constants, or `range(start, stop[, step])` with constant bounds | **unrolled** at compile time | `[x * 2 for x in range(3)]` → `[0, 2, 4]` |
| a container variable, a container a function answered, a `sorted`/`reversed` over a walkable list | **walked** by a runtime loop | `xs = [1, 2, 3]` / `[x for x in xs]` → `[1, 2, 3]` |
| anything else a call returns that no scan can call a container | **refused**, naming the missing proof | `[x for x in list([1, 2])]` |

Only `range(...)` — the name, with at least one argument — has a compile-time length. Every other call is a value
the program computes, so the loop that walks it asks the object the questions a `for` statement asks it, and what
it cannot prove is a container it refuses rather than measures:

```
def g():
    return [1, 2, 3]
print([x for x in g()])            # [1, 2, 3]   (this panicked the toolchain before ADR 0357)

def z():
    return {"a": 1}
print([k for k in z()])            # ['a']       walking a dict walks its keys

def h(n):
    return [n * n]
print([x for x in h(3)])           # [9]

print([x for x in sorted([3, 1, 2])])   # [1, 2, 3]

print({x for x in [1, 2, 3, 2]})        # {1, 2, 3}   the set drops the fourth element in the object
print({x: x * 2 for x in [1, 2]})       # {1: 2, 2: 4}
```

A set or a dict comprehension walks the same road, and the set's answer is not a list with braces: the dedup
happens in the object, through the tagged add that finds a slot by (payload, tag) together (ADR 0234). What the
printer then shows is the order the slots were filled, which for small ascending ints is the order CPython prints
too — and which is **Gap R.300** for `[3, 1, 2]`, where the reference prints `{1, 2, 3}` and this backend prints
`{3, 1, 2}`. A set has no order the language promises, so the rows that are asserted are the ones whose two
orders agree, and the disagreement is a roadmap row rather than an expectation written down wrong.

Reading a call's own argument as the loop bound is the defect this route exists to end: `[x for x in h(3)]` printed
`[0, 1, 2]` for *any* one-argument `h` — where the reference prints `[9]` for the body above and raises
`TypeError: 'int' object is not iterable` for `return n + 1` (roadmap **Gap R.291**, paid). A call the container
scan does not vouch for is refused with
`does not name a container the compiled backend can prove … a call answers a container only where the scan can see
every arm of the callee answer one` (`Gap R.67`, ADR 0333) rather than walked as an empty object — walking one is
how `[x for x in h(3)]` over `return range(n)` came to print `[]` at exit 0 during this row's own work. A `range`
is still not a value anywhere else in the language (`print(h(3))` writes `3`, not `range(0, 3)`: **Gap R.293**),
the `for` road still reads a call's argument as a bound (**Gap R.299**), and a generator passed to a builtin is
refused by the grammar in its bare spelling and by the door in its parenthesised one (**Gap R.296**).


### Multi-argument range iterables (comprehensions)

List comprehensions over `range(start, stop)` and `range(start, stop, step)`
are supported in the compiled backend. The iterable
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
`cond`. on the record it evaluates eagerly to a list of the yielded
values, so it can be consumed by a `for x in gen:` loop or a `list(gen)`
call. Example: `(x * 2 for x in [1, 2, 3] if x > 1)` → `[4, 6]`. Generator
expressions and generator functions (`yield`) evaluate eagerly to runtime
heap lists in the compiled backend and in the record of what the retired engine answered; see ADR 0081.

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
- **tracebacks** name every frame the raise travelled through, each with the line that frame was
  on and the function the program wrote (Gap K.6, Gap K.8, ADR 0354);
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

`then if cond else otherwise` (Python-style ternary) is a value chosen by a test, and the compiled
backend runs exactly one of its two arms. The condition is an or-level expression; the
`else` branch is a full expression, so nested ternaries bind right:
`1 if 0 else 2 if 1 else 3` is `1 if 0 else (2 if 1 else 3)`.

**The arm the test did not choose is not evaluated** (roadmap Gap R.340, ADR 0374 — the ternary half
of ADR 0275's `and`/`or`). Nothing in a rejected arm reaches the run time: not a call, so not the line
that call prints; not a division, so not its `ZeroDivisionError`; not a subscript, so not its
`IndexError`/`KeyError`; not a read of a slot that is unwritten on that path, so not its
`UnboundLocalError`. Four programs say the shape:

```python
def boom():
    print("boom")
    return 1


x = 5
print(1 if x > 0 else boom())        # 1     — `boom` is never called
print(1 if x > 0 else 1 // 0)        # 1     — no ZeroDivisionError from the rejected arm


def fact(n):                          # 120   — the base case is a stop, not merely a value
    return 1 if n <= 1 else n * fact(n - 1)


print(fact(5))
```

A pair of arms that can provably do nothing but answer a value — literals, arithmetic and orderings
over literals of one domain, `not`/unary minus of a literal, a read of a slot the checker proves was
written — is still folded into one LLVM `select`, because for that pair the two roads disagree about
nothing except how many instructions the module carries. Anything else is lowered behind a branch:
the test once in the block the caller stands in, a `br i1` to `tern.then` / `tern.else`, each arm
emitted **only inside the block the branch enters**, and one block-parameter `phi` per answer meeting
them at `tern.merge` (`pkg/lang/ternary_arms.go`, the same skeleton ADR 0275 gave `and`/`or`). Which
side a program is on is the gate `ternaryArmsNeedBranch`, and the gate's default is the direction that
costs an instruction and never the direction that runs a program's effects.

In the AOT path, a constant condition folds to the taken branch, and a runtime condition that chooses
between two pure arms lowers to an LLVM `select`. The select's word is the
arms' word, asked once — `ternaryKind` in `pkg/lang/ternary_number.go` — and read by the renderer,
the arithmetic and the gate that picks a function's return word, so the four cannot disagree about
what the same expression is (roadmap Gap R.102, ADR 0262):

| arms | the answer |
|---|---|
| both doubles | `select i1 cond, double then, double else` — `def f(x): x = x + 1.5` / `return x if x > 2 else 0.0` answers `2.5` |
| neither a double | `select i1 cond, i32 then, i32 else` — the ternary stays allocation-free and needs no blocks |
| the test is a value the source wrote | the arm that runs answers, in its own kind, and the other arm is not emitted: `print(1 if 1 else 2.5)` is `1` and `print(1 if 0 else 2.5)` is `2.5` |
| exactly one arm is a double, and the test must be evaluated | refused in words, not truncated: `print(1 if c else 0.0)` — neither word prints both arms the way the reference does, and the tagged value word (roadmap L11.1) is what makes the run-time choice answerable |

An arm that is not already a double converts inside a context that asks for one, because that is
what the reference's own promotion means: `(1 if c else 2.5) * 2` multiplies a `1.0`. A container
holding such an arm says so on the object, so `print([1.5 if c > 0 else 2.5])` is `[2.5]` (ADR 0259's
`boolSlotExpr` has a float sibling, `floatSlotExpr`, for the same reason).

An arm that is a **container** is answered too, on the road every other effectful arm already takes: an arm is a
program the test runs, so each arm's container is BUILT inside the block the branch enters — with the builders every
container road has used since **ADR 0189** — and one `phi` carries the two handles to a printer chosen from the kind
the arms AGREE on (roadmap Gap R.385 and Gap R.128, both closed by **ADR 0405**):

| arms | the answer |
|---|---|
| two lists | `print([1, 2] if 1 else [9])` is `[1, 2]`, and `print([1, 2] if (lambda v: v)(1) else [9])` — the arm the reference has to evaluate — is `[1, 2]` |
| two dicts / two sets | `print({"a": 1} if 1 else {"b": 2})` is `{'a': 1}`; `print({1, 2} if 1 else {3})` is `{1, 2}` |
| two tuples | `print((1, 2) if 1 else (3, 4, 5))` is `(1, 2)` — the brackets come from the object, not from the road that built it |
| a comprehension | `print([v for v in [1, 2, 3] if v > 1] if 1 else [9])` is `[2, 3]`, and the dict and set spellings answer theirs |
| a container arm and an arm that is a word, the test written | the arm that runs answers: `print({"a": 1} if 1 else 7)` is `{'a': 1}`, `print(7 if 1 else [1, 2])` is `7` and emits no list |
| a container arm and an arm that is a word, the test evaluated | **refused in words**, not printed as a number: the door picks its printer from a kind the compiler can see, and the reference's answer needs the tag printer roadmap Gap R.67 / **ADR 0333** built for the answer of a call (roadmap **Gap R.394**) |

A container handed to a function, asked a membership question, ordered by `sorted`, bound to a name or compared to
another container takes the same handle, so `head([1, 2] if 1 else [9])`, `1 in ([1, 2] if 1 else [9])` and
`([1, 2] if 1 else [9]) == [1, 2]` answer `1`, `True` and `True`. What the family still owes is said out loud rather
than hidden: the doors that ASK a container ternary a question — `str(...)`, a loop's iterable, a `def`'s `return`, a
`%` format, an interpolation's field, `len`, a position — still read the arm as one word, five of them reaching the
assembler at all, and that is roadmap **Gap R.393**. A mixed pair — one arm a double, the other an `i32`, the test a
run-time fact — is still refused in words rather than truncated, and that refusal is roadmap L11.1's tagged value word
to earn: a gate that puts an arm behind a branch cannot promote an arm into a word it does not have.

## Truthiness

Every place that tests a value — `if`/`elif`/`else`, `while`, the ternary
condition, `and`/`or`/`not`, a comprehension's `if` clause, a `match` guard — uses
the same rule, and the compiled backend implement it identically:

| Value | Truth |
|---|---|
| `0`, `0.0`, `-0.0` | false |
| `False`, `None` | false |
| `""` | false |
| `[]`, `{}`, an empty set | false |
| anything else (including any other number, string, container or object) | true |

Where the value's kind is the run time's fact and not the compiler's — an entry read out of a dict whose
values mix kinds, or the item of a comprehension or `for` unrolled over a literal display — the test is asked
of the **tag** beside the payload, in the module's one truth table (`@rt_pair_truth`: a number in either
family by its value, a text by its length, a container by its element count). Testing the payload instead is
what once made `d = {None: ""}` / `if d[None]:` take the `then` arm, because the word there is an interned
string's *index*, and `bool({None: []}[None])` answer `True`, because an empty list's handle is never zero
(**Gap R.284**, **Gap R.359**, **ADR 0388**), and the same is true of a **parameter whose kind the caller
supplied**: an argument that crossed as a pair arrives with a tag word the call site wrote, and the callee's
own `if v:`, `elif v`, `while v:`, ternary head, `not v` and `bool(v)` ask that tag rather than testing the
payload (**Gap R.361**, **ADR 0393**) — a text's payload is its interned index and an empty list's is a heap
handle, so `def f(v):` / `    if v:` over `f(d["e"])` once answered `1` for CPython's `0`. Two shapes are not
served by that door and are filed, not hidden: a body that **rebinds** the parameter never rewrites the tag
word, so the ask is declined there and the rebinding family stays wrong (and, for one spelling, runs
forever — **Gap R.366**), and a body emitted under the text- or double-returning convention never receives
the tag at all (**Gap R.368**). An `and`/`or` whose operand is such a parameter, and the arm a ternary
chooses, are neither of this door nor the operator road's (**Gap R.367**).

A provable integer entry or item keeps the plain compare, and the entry read handed to a call — a position
that keeps one word for the whole value — is still **Gap R.146**/**Gap R.220**'s. A **set** entry now carries
its tag where the container was spelled as a literal or written by a binding, and where the empty container
was spelled as a call (**Gap R.360**, **Gap R.364**, **ADR 0391**, **ADR 0392**, **ADR 0394**): the read of a
slot the program spelled `list()` / `dict()` / `set()` — a dict value, a list or set member, at any depth of
nesting — crosses a call argument with the kind of the container that spelling names, so
`d = {"k": list()}` / `f(d["k"])` prints `[]` and takes the `else` arm like the reference and not `3` and the
`then` arm (**Gap R.369**, closed). The door reaches a **value** and a **member**, not a **key**: a key the tag
table cannot label is a key that cannot be hashed, and *when* that raises is **Gap R.81**'s decision — so
`s = {list()}` still adds the member (**Gap R.81**), and the reference raises `TypeError`. The scan asks the
same shadowing question the emitter asks, so a program that writes `def set(): return 1` keeps its own value at
that spelling; a **method** named `set` taking the built-in's spelling from this road is **Gap R.373**, and the
truth of an empty container spelled as a call where there is no call to cross — `print(bool(set()))` at module
level — is **Gap R.370**.

A text whose value **arrives from a call** asks the same length question the tag table takes for its kind
(**Gap R.407**, **ADR 0409**). `def f(): return "abc"` / `if f():`, `s = f()` / `if s:`, `while f(i):`,
`not f()`, `"y" if f() else "n"`, `bool(f())`, `class T: def m(self): return "abc"` / `if T().m():`, a slice of a
built text (`if s[1:]:`), a concatenation of two of them, and the name an `except … as err:` clause bound all branch
the way the reference branches — because the truth of a text is its **length**, asked of `@rt_str_len`, the same door
`@rt_pair_truth` takes for the text tag and the one an unrolled loop item already asked. What it is *not* is a test of
the word: a text's value is an index into the interned string table, so testing it against zero asks which table
entry the intern table handed out — and the first text a program interns is entry 0, which is why every one of these
programs looked like a toy and failed. Two neighbours stay refused rather than answered: a parameter one call fills
with a text and another fills with a number (`w(0)` and `w("a")` — **Gap R.409**, and the pair road's **Gap R.324**
beside it), and a container's truth spelled as a *call* (`print(bool([1]))` — **Gap R.131**, **Gap R.370**), which is
why `if xs:` is the spelling this backend answers. Both refusals carry codes an agent can branch on
(`lower.unsupported.argument-kind-undecided`, `lower.unsupported.bool-of-a-container` — **Gap R.408**).

### `and` and `or` answer an **operand**, not a verdict, and skip what the test rejected (Gap R.147, ADR 0269; Gap R.149, ADR 0275)

The two operators test one operand and hand back whichever operand the test chose, unconverted — the
operand keeps its own representation, its own rendering and its own type:

```gy
print(2 and 3)          # 3, not 1
print(0 or 5)           # 5
print("" or "d")        # d
print([1] and [2])      # [2]
print(True or 1)        # True   — the operand the test chose is the verdict
print(1 or True)        # 1      — and the other choice is the number
```

A **condition** asks only the verdict, and `truth(a and b)` is `truth(a) and truth(b)`, so an `if`, an
`elif`, a `while` head, a ternary test or a `match` guard branches on any two operands whatever they are:
`if x or "d":` works even though the *value* of `x or "d"` has nowhere to live. A **value** position — a
binding, an argument, an element, an arithmetic operand — needs the answer to fit in a word, and three
roads carry it: a test the source wrote picks its operand at compile time; two operands that share a word
are merged from the left operand's block and the right operand's block, in the `i32` door or the `double`
door; and the print door merges the payload *and* its tag, because the module has one printer that reads a
kind. A shape none of the three roads can state is refused by naming both operands and the missing tag
(`roadmap L11.1 … Gap R.147`) rather than answered with the `1` that stood here before — which is Gap
R.146's one-word position and L11.1's tagged value word, not a syntax problem.

**The test decides whether the other operand runs at all** — on both legs, and this is a promise a
program may rely on:

```gy
def boom():
    print("boom")
    return 9
x = 0
y = 1
print(x and boom())   # 0        — boom is never called
print(y or boom())    # 1        — boom is never called
print(x and (1 // 0)) # 0        — the division the test excluded never traps
print(y and (1 // 0)) # ZeroDivisionError — the operand it reached still raises, catchably
if x and boom():      # the body's test is never evaluated
    print("then")
print(boom() and 2)   # boom, then 2 — the operand the test reached runs exactly once
```

The compiled lowering is a branch and a merge, not a `select`: the left operand is evaluated once, its truth
is the only thing the branch reads, the operand the test excluded has no instructions on that path, and the
two arms meet in a `phi` (one for the payload, and a second one beside it when the answer is a pair). The
truth of an operand whose kind only the object carries — a container slot, a loop variable over a mixed list
— is asked of `@rt_pair_truth`, the same tag table the printer and the comparison read, so an empty text, an
empty container and `None` are false in a branch exactly as they are in a print. A test the source wrote
never emits the other operand at all (`constantLogicArm`). What the operators do *not* promise is that an
operand the test reached is free: it runs, it prints, and a trap inside it raises the error the program's
`except` catches.

The answer's type follows the same rule: the checker types `a and b` as the operand's type when both
operands agree and leaves it dynamic when they do not, so `x: int = 2 and 3` checks and `x: bool = 2 and 3`
is the mismatch it always should have been. `--json --eval` reports the same pair the printer does:
`"type": "bool"` with `True or 1`, `"type": "int"` with `1 or True`.

### A name answers with its **latest binding** (ADR 0172's rule, Gap R.145, ADR 0270)

What a name *is* — a text, `None`, a verdict, a container, an instance, a tagged pair — is decided by the
last statement that bound it, and every earlier record is retired at that binding:

```gy
x = "text"
x = [1, 2]
print(x)        # [1, 2]
x = 5
print(x, x * 2) # 5 10
x = "abc"
print(x.upper())# ABC
```

The compiled backend keeps a notebook of these statuses because an untagged `i32` cannot carry a kind, and
the notebook used to be retired one status at a time — the verdict, `None` and the tag each had their own
clear-on-rebind, and the interned-text record had none. That is why `x = "text"` followed by `x = [1, 2]`
printed `text` at exit 0, why the same program refused `x * 2`, and why `-x` raised
`TypeError: bad operand type for unary -: 'str'` for a program CPython answers with `-5`. The door is now
one function called at the binding, and it asks the print dispatch's own predicate about the right-hand
side, so the notebook and the printer cannot disagree about what the name holds.

### An unpacking binds each name by the **element's own tag** (Gap R.144, ADR 0367)

`a, b = <values>` is not one statement about a container; it is a row of binding positions, and a
binding position asks the pair doors. So each right-hand element is lowered through the same ordered
list an ordinary assignment asks — a pair someone else already carried, the tagged read of a slot,
the entry of a dict whose values mix kinds, the answer of a call, the arithmetic the print position
answers, the fold that hands back one of its arguments, the slot of a container the program built,
and (for the unpacking alone) a name the pair road bound before — and only then is it stored:

```python
xs = []
xs.append([7, 8])
a, b = xs[0][0] + 1, xs[0][1] + 2
print(a)          # 8 — and the same three lines with `=` instead of `,` answer 8 too
a = 5
print(a, b)       # 5 8 — the rebinding retypes the pair's own slot, it does not open an int beside it
```

Before this rule the unpacking lowered every element through the one-word road and recorded nothing,
so `a, b = 1, None` printed `1 0`, `a, b = "a", "b"` printed `0 1` (the `@str_tab` index), `a, b = 1,
2.5` printed `1 2`, and a slot read in an element was refused outright with `index cannot reach into
xs's slots` while the plain binding of the same expression answered it. Values are now decided by the
value, in one place: `pkg/lang/assign_pair.go`.

The values are still read **before any of them is named**, so `a, b = b, a` swaps and
`a, b, c = c, a, b` rotates; a pair door answers into registers, which is what lets every load read
the old binding. And what the pair road cannot carry is still refused rather than answered from the
payload alone: `print(a + b)` over two pair-bound names, `if a:`, `print(a[0])` each say so, naming
where the tag they cannot hold came from (roadmap Gap R.323, and Gap R.321 for binding a pair-bound
name to a second name or unpacking one collection — `a, b = xs[0]`).

## None

`None` is a real value — a singleton with its own dynamic type (`None`, value tag
`TagNone`) — and not the integer `0`. The compiled backend agree, and the AOT backend allocates it
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
  is never optimised away. That is the answer for an operand whose kind the compiler can state;
  it is not an answer for one whose kind lives in a container the program built, and asking it
  there is how `xs = [None, 1]` / `print(xs.pop(0) is None)` came to write `False` beside the
  `None` its own printer wrote (`Gap R.412`). A void is a singleton, so for it identity and
  equality coincide, and the question against `None` goes to the same pair door `==` asks:
  `@rt_payload_eq(payload, tag, 0, TagNone)` — the object's own **tag**, never the word `0` the
  void leaves behind, which is also the word an `int` slot carries (**ADR 0172**). The condition
  road (`if x is None:`, `while x is None:`, an `and`/`or` operand) asks that one door too, so a
  value and a test cannot be given different answers about one slot (**ADR 0232**). Folded where
  the source decides: `print(1 is None)` emits no call at all. A CALL is foldable too, once the
  compiler can state what its callee answers: a body whose every `return` is the void — spelled
  `return None` or simply falling off the end — makes `print(f() is None)`, `print(f() == None)`
  and `print((lambda: None)() is None)` print `True`, the way `print(f())` always printed `None`,
  because both roads consult the ONE predicate `isNoneExpr` and it reads the answer plan's void
  verdict (**Gap R.388**, **ADR 0427**). A body that FORWARDS such an answer answers the void
  itself: `def g():` / `    return f()` above `print(g() is None)` prints `True`, however many
  frames deep the chain goes, because the `return other(...)` arm asks whether `other` answers only
  the void and then answers the constant void word rather than forwarding one (**Gap R.451**,
  **ADR 0428**). The same verdict reaches a body that lives in a **class**: `class C():` /
  `    def m(self):` / `        return None` above `c = C()` answers `print(c.m())` → `None`,
  `print(c.m() is None)` → `True`, `print(c.m() == 0)` → `False` and `print(str(c.m()), repr(c.m()))`
  → `None None`, because the answer plan reads a method's body with the ONE function
  (`methodAnswersOnlyTheVoid`, asking `answerBodyPlan`) that reads a module `def`'s, and the body is
  resolved the way the dispatch resolves it — by the receiver's class and the name, walking bases in
  the order the method table walks them (**Gap R.452**, **ADR 0429**). A method that answers a number,
  a text or a verdict keeps that answer; a body with any arm the plan cannot name (`return self.x`) is
  not read as the void; a receiver whose class the compiler cannot place is no receiver at all.
  A method whose arms answer MORE THAN ONE immediate kind — `        if x:` / `            return 7` /
  `        return None` — is not answered from the source at all: which arm ran is a run-time fact, so the
  method is carried, stores the kind beside its own `ret`, and the doors that read the answer read that word,
  so `print(c.m(1), c.m(0), c.m(0) is None)` prints `7 None True` (**Gap R.453**, **ADR 0430**). A method that
  answers a CONTAINER is carried for the same reason and builds it before it answers — `print(c.m())` of
  `        return [1, 2]` prints `[1, 2]`, and `len(c.m())`, `c.m()[0]`, `c.m()["k"]`, `1 in c.m()`,
  `c.m() == [1, 2]` and `for v in c.m():` all ask the object rather than refusing (**Gap R.67**, **ADR 0333**,
  taken off a class's return for the first time). A method that FORWARDS a call to another method of its class
  carries that answer too — `    def n(self):` / `        return self.m()` above `print(c.n())` prints `[1, 2]`,
  `len(c.n())` prints `2` and `for v in c.n():` walks, because the forwarded arm brings the callee's container kind
  with it (**Gap R.454**, **ADR 0431**). The shapes that stay declined, at exit 1 and in words:
  `sum(c.m())`/`max(c.m())`/`min(c.m())` (`sum requires an inline list/set/dict literal` — the module `def`
  twin refuses the same sentence, **Gap R.458**), a BOUND answer measured or subscripted
  (`ys = c.m()` / `print(len(ys))` / `print(ys[0])`, where `ys = build()` answers — **Gap R.459**), a mixed
  container arm compared with a container literal (`print(c.m(1) == [1, 2])`, **Gap R.461**), and TWO calls of one
  carried body inside one expression — the kind word is one per body, not per call, so `print(f(1) == f(0))` reads
  whichever arm ran LAST and prints `True` where the reference prints `False` (**Gap R.460**; two different
  bodies, `f(1) == g(1)`, and two bound names are answered correctly). The fold keeps the call: `r = f() == None` still runs `f`, and `@gy_g` still calls
  `@gy_f`, so a body that prints prints (**ADR 0172**); and a body that answers `None` in one arm
  and a number in another is NOT read as the void — which arm ran is a run-time word — nor may a
  body forwarding a number, a text, a verdict or a container borrow the answer.
- **What the identity question still gets wrong**: `is` asked of a *container* literal is not
  answered by the content door — content equality is the wrong answer for two containers holding
  the same elements — and the shape dies in the assembler instead (`print([1] is [1])`,
  `Gap R.418`). Of a callee's own parameter (`Gap R.417`) the question is still answered by the
  fold, and prints `False` where the reference prints `True`. Of a call that ANSWERS the void it
  is not: `def f():` / `    return None` / `print(f() is None)` and its `==` and applied-`lambda`
  twins print the reference's `True`, because the fold now asks the one predicate `print`, `str()`
  and the REPL ask (`isNoneExpr`, which reads `answerKindPlan`'s void verdict — `Gap R.388`,
  **ADR 0427**), through as many forwarding bodies as the program writes (**Gap R.451**, **ADR 0428**).
  A METHOD's body now answers the same way (`class C():` / `    def m(self):` / `        return None` above
  `print(c.m() is None)` prints `True`, **Gap R.452**, **ADR 0429**) — and so does the answer's
  KIND: a method whose arms answer `7` and `None` prints `7`, `None` and answers `c.m(0) is None` → `True`,
  because a method whose arms do not all answer one kind is CARRIED — it keeps the arm that ran in a word of
  its own (`@gy_C_m.anst`, written by the same `answerReturn` a module `def` takes, read by the caller that just
  called it), exactly as a module `def` has since **ADR 0368** (**Gap R.453**, **ADR 0430**). A method that
  FORWARDS a call to another METHOD of its own class answers that method's own kind too — `class C():` /
  `    def m(self):` / `        return None` / `    def n(self):` / `        return self.m()` above
  `print(c.n() is None, c.n())` prints `True None`, through a base class and through a shadow as well, and a
  forwarded container prints, measures (`len(c.n())`), walks (`for v in c.n():`) and tests (`1 in c.n()`),
  because the arm asks the callee's verdict out of the same two tables and never walks into the callee — two
  methods that forward to each other are a reading that would never end (**Gap R.454**, **ADR 0431**). What is
  still owed there: forwarding a call to a method that answers ONE text (`print(c.greet())` of `return self.hi()`
  prints `0` where `print(c.hi())` prints `hi` — **Gap R.462**), forwarding through a PARAMETER
  (`def f(o):` / `    return o.m()`, **Gap R.463**), and subscripting a forwarded container
  (`print(c.n()[0])`, **Gap R.464**); and a method answering ONE float prints the double's low word on the direct
  call as well (**Gap R.466**). Asked of a CONTAINER POP — the same
  list under a **second name** — the fold's answer no longer holds either: `ys = xs` /
  `print(ys.pop(0) is None)` asks the tag, because the record of an object's slots now travels with
  the handle the two names share (`Gap R.415`, **ADR 0412**). Nor is it answered by the fold for the
  item of a `for` **unrolled over a literal list**: `for v in [None, "a"]:` / `print(v is None)` /
  `print(v == None)` / `print(v in [None, "a"])` each ask the item's own `(payload, tag)` pair — the
  payload loaded out of the loop variable's slot, the tag the unroll records per body copy
  (`Gap R.416`, **ADR 0426**; the same record the printer reads, `Gap R.358`, and the reason
  `print(v)` and `v == None` cannot disagree inside one body copy). Ordering is the one compare
  family that has not joined: `print(v < 2)` over `[1.5, 2.5]` still compares the box handle
  (`Gap R.448`).
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

Booleans are values, not just tests: `a and b`, `x in xs` and `not x` produce a verdict that can be
printed, stored (`flag = a < b`) and re-tested. A verdict writes its own name — `print(1 == 1)` is
`True`, `print(not True)` is `False` — and is still the `0`/`1` every numeric path reads, so
`True + 1` is `2`, `-True` is `-1` and `sum([True, True])` is `2` (ADR 0257). A verdict in a container
carries its own tag, so `print([True, 1])` is `[True, 1]`, `print({"k": True})` is `{'k': True}` and
`print({True, 1})` is `{True}` — while every numeric, ordering, equality and membership question about
that same slot keeps answering as the number, which is why `[True] == [1]`, `1 in {True}`, `d[True]`
and `sum([True, 1])` are CPython's answers too (ADR 0259, closing roadmap Gap R.112). A verdict handed
to a function writes its name as well — `def show(f): print(f)` / `show(1 == 1)` prints `True`, and so do a
bound name, a keyword argument, a default, a method's parameter and a verdict the callee computed — because
the front end reads the parameter's callers before the body asks what the name means (ADR 0348, closing
roadmap Gap R.111). What that reading declines stays a number: a position two call sites fill differently
(`show(True)` and `show(1)`), or a method name two classes both define, prints the number the value is
stored as until a tag rides with the argument (roadmap Gap R.270).

How each backend gets there is an implementation detail, but a load-bearing one
(ADR 0167): in IR a value is either an `i32` or the `i1` result of a comparison, and
neither may be fed to the other's instruction, so conditions go through a
normalising helper (`truthOperand`) and containers/strings are tested by *length*
(`rt_list_len` / `rt_dict_len` / `rt_set_len`, or the compile-time length of a
literal) rather than by handle. on the record a condition asks the heap object
behind a handle, never the handle itself.

### Dicts & sets (LLVM codegen)

The AOT path also lowers **inline dict and set literals** with constant-key
indexing and `len`: `{1: 10, 2: 20}[1]`, `{1, 2, 3}[2]`, `len({1: 10, 2: 20})`.
Each dict/set literal is emitted as a dedicated global struct (`{i32 count,
[n x i32] keys, [n x i32] vals}` for dicts; `{i32 count, [n x i32] elems}`
for sets). Constant-key lookup resolves at compile time; like lists, these
literals must be used inline (no assignment-to-variable indirection) in the
codegen path. the record indexes dicts/sets at runtime and is unchanged.

**An entry the compiler cannot read is written by the runtime, with its tag.** A dict whose key, value or
member is something this pass cannot classify — a name the pair road bound (`n = xs[0]` over a container the
program built), a slot read, the answer of arithmetic — goes to `rt_dict_put_tagged(h, k, v, keyTag, valTag)`
and `rt_set_add_tagged(h, v, tag)`, which take the payload **and** the tag as five and three `i32`s, and branch
per kind inside (`ADR 0232`, ADR 0187's rule that a payload is never written without its tag). That is the door
a pair-bound name enters by, and it is why `print({"k": n})` is `{'k': 'a'}` rather than the interned index the
slot actually holds, and `print({n})` over a float slot is `{2.5}` rather than the box handle (ADR 0310, ADR 0306
before it for the list element). A key or a member also asks a question an element never does — *can this value
be hashed* — and only the tag can answer it: a container-valued key or member raises CPython's
`TypeError: unhashable type: 'list'` / `'dict'` / `'set'`, catchable by the program's `except TypeError:`, where
reading the payload alone would have put a heap handle in the bucket and printed a plausible container at the
exit code of success (`Gap R.81` owns the general hashing rule).

The two container families divide the language's key question, and each gets its own sentence. A dict
or a set **hashes** its key, so the question there is *can this be hashed* — a list, dict or set cannot,
and the raise names the type (ADR 0328, `Gap R.217`, below). A list or a string **counts** positions, so
the question there is *is this a position*, and the answer is CPython's own pair of sentences
(ADR 0329, `Gap R.222`):

```python
xs = [10, 20, 30]
print(xs["a"])    # TypeError: list indices must be integers or slices, not str
print(xs[1.5])    # TypeError: list indices must be integers or slices, not float
print(xs[None])   # TypeError: list indices must be integers or slices, not NoneType
print(xs[[0]])    # TypeError: list indices must be integers or slices, not list
print(xs[True])   # 20 — a bool IS an integer, so this is position 1
s = "abc"
print(s[1])       # b
print(s["a"])     # TypeError: string indices must be integers, not 'str'   — quoted, for a string
print(xs[0:2])    # [10, 20] — a slice is a position-shaped thing
print(xs[5])      # IndexError: list index out of range     — the container is named (ADR 0330)
xs[5] = 1         # IndexError: list assignment index out of range  — reading and writing differ
```

A text index is an index into `@str_tab`, so before this rule the door counted to the slot the interned
number named and printed whatever was there — `xs["a"]` answered `20` at exit 0. The store road
(`xs["a"] = 99`) stored into that slot; the reference refuses to treat a text as a position at either
door, and so does this. A name the program last filled with a float or a text is the same index
(`t = "a"` / `xs[t]`), read from the same kind maps the print door uses; where the kind is only a
run-time fact the door keeps the bounds check it always had rather than accuse something it cannot see.

The question is asked at **every door that consults a dict**, not only the deep ones (ADR 0328,
`Gap R.217`). All three of these raise the reference's sentence, on both dict roads — a literal written in
place and a variable bound to one:

```python
print({"a": 1}[[1]])      # TypeError: unhashable type: 'list'
print([1] in {"a": 1})    # TypeError: unhashable type: 'list'   (not `False` — the question is not allowed)
d = {}
d[[1]] = 2                # TypeError: unhashable type: 'list'   (the entry is never stored)
print({"a": 1}[(1, [2])]) # TypeError: unhashable type: 'list'   — a tuple is hashable, its element is not
```

A tuple key names the *inner* type, because the reference hashes a tuple by hashing its elements left to
right and reports the first one it cannot. A name bound to a container is a container key
(`x = [1]` / `d[x]`), and the kind is read from what the source wrote. **A list and a tuple are the
containers that walk rather than hash**, so membership in one takes an unhashable needle legally:
`[1] in [[1], [2]]` is `True` and `{1} in [{1}]` is `True` — the gate that refuses a dict key must not
follow the needle into a list haystack, where it would turn a working program into a raise. What still
refuses, rather than answering, is a key whose kind is only a run-time fact: a name the compiler cannot
pin to one container keeps its lookup honest by declining (L11.1's tagged word, `Gap R.212`).

> **Subscripting a set asks the set a membership question, and that is a gusty extension, not Python.**
> `{1, 2, 3}[2]` asks the set whether `2` is one of its members and answers `2`; asking for a member it
> does not have is `KeyError: not in set`. CPython rejects the shape outright with
> `TypeError: 'set' object is not subscriptable` (with a syntax warning calling out the missing comma),
> so programs that use it are recorded `oracle: not_applicable` in the conformance ledger — the CPython
> leg cannot reach the rest of the file — which makes the extension a deliberate difference rather than
> an unnoticed one (ADR 0186, ledger rows `programs/data_b` and `programs/features_b`). The compiled backend
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

**A dict is a key → value mapping however it is built** (ADR 0260, closing roadmap Gaps R.118 and
R.120). Item assignment has always put an entry; so do the literal, the dict comprehension and the
`dict(d)` copy, through one door (`dictPut`) rather than four rules:

```py
print({"a": 1, "a": 2})              # {'a': 2}   — one entry, the last value
print({"a": 1, "b": 2, "a": 3})      # {'a': 3, 'b': 2} — position is first insertion, not last write
print({1: "a", True: "b"})           # {1: 'b'}   — one key, and the first spelling is what prints
d = {1: 2 for x in [1, 2]}
print(d, len(d))                     # {1: 2} 1   — the loop put the entry, it did not append one
```

The keys compare the way CPython's do, so `1`, `True` and `1.0` are one key (ADR 0259's key equality asks
the numbers, not the handles) and the entry that survives keeps the key that was written first. The
interpreter grew its dicts by appending instead, so `{"a": 1, "a": 2}` held two entries that both printed
and both counted while the compiled backend — whose fold deduplicates keys — answered CPython's line: the
two engines disagreed about an ordinary dictionary, and no program in the corpus had written one.

Rules that the compiled backend implement:

- **A key is asked, not spelled** (ADR 0353, closing roadmap Gap R.221). The value of an entry is one word and
  what that word means lives in the tag the writer left beside it (ADR 0232), so a subscript that needs the
  entry's kind — the `print` door, a binding, `and`/`or` — asks the *pair* door, `rt_dict_get_tagged` beside
  `rt_dict_value_tag` (ADR 0327). Which keys that door can be asked **with** is one question with one answer,
  `dictKeyCarriesPair` (`pkg/lang/dict_key_pair.go`): a name, an `int`, a text, a float, `None`, `True`,
  `False` — every key kind that has a tag in the runtime's table. A `None` or bool key was not on the gate's
  list, so those three reads fell back to the one-word road and printed the **payload** at the exit code of
  success: a `@float_box` handle, a text's `@str_tab` index, the allocator's slot.

  ```py
  d = {None: 3.5}
  print(d[None])              # 3.5   (was 3 — the box handle)
  d = {None: "hi"}
  print(d[None])              # hi    (was 0 — the interned text's index)
  d = {False: [2]}
  print(d[False])             # [2]   (was 3 — the allocator's slot)
  print({1: 1.5, True: "a", False: [2]}[True])   # a
  print({0: "a"}[False])      # a     — a bool is the number the reference says it is,
                              #        and that is the runtime's key-equality rule, not the gate's
  ```

  A key this backend cannot tag — a container — is refused **before** the door: `d = {[1]: "x"}` says a
  compiled container cannot hold a list literal (ADR 0327's untypable-key row; the class a container key raises
  is **Gap R.217**'s, and the sentence's own wording is **Gap R.288**'s). A **tuple key answers**: `d = {(1, 2): "x"}` /
  `print(d[(1, 2)])` prints `x`, `print((1, 2) in d)` prints `1`, a tuple key added at run time (`e[(5, 6)] = 7`) reads
  back, and a missing one raises `KeyError: (3, 4)` with the key's own repr — because since **ADR 0361** a tuple is an
  object the dict road can hash, element by element, and an unhashable element names the inner type (`(1, [2])` says
  `'list'`, the reference's left-to-right hashability walk). What the same read could not reach was
  two rows, each pinned with today's answer beside the reference's: a **name** whose last binding was `None`,
  which arrived as an untagged word and raised `KeyError` where the reference prints the entry (paid below by
  **ADR 0420**), and a **set** entry, whose tag the builders never write, so it prints its handle and is
  truthy (**R.360**).
  `+`/`*` over the entry are **paid**: `d = {None: "a"}` answers `print(d[None] + "b")` with `ab` and
  `print(d[None] * 2)` with `aa`, as do the method road (`d[None].upper()`), the length road, the
  interpolation field, `str`/`repr`, the equality door, the condition door, a container element, a call
  argument, a character of the entry and a slice of it (**R.285**, **ADR 0419**). The rule that pays them is
  one question per operand: the dict's static key door reads a `None` and a verdict key — and the two roads
  that read an entry's KIND in order to decide *what question to ask the entry at all*, the membership test
  and the `for` loop, ask the **object** instead, whose tag says which arm runs and raises the reference's
  sentence for a kind with no elements:

  ```gusty
  d = {None: "a"}
  print(d[None] + "b")           # ab    (was refused: "CPython raises TypeError for it" — it does not)
  print(d[None] * 2)             # aa    (was 0 — the text's @str_tab index, at exit 0)
  print(d[None].upper())         # A
  t = {None: "ab"}
  for ch in t[None]:
      print(ch)                  # a, b   (was: nothing printed, exit 0)
  print(1 in {None: 5}[None])    # TypeError: argument of type 'int' is not iterable (was False)
  xs = [5]
  print(1 in xs[0])              # the same raise — no `None` in the program; the label road caused both
  print({0: "a"}[None])          # KeyError: None — the void is a key of its own kind, not the number 0
  ```

- **A dict key that arrives through a binding carries the kind its binding wrote.** The builder stores every
  entry of a self-describing dict as a `(payload, tag)` pair, so the void's key is `(0, TagNone)` and the number
  `0`'s is `(0, TagInt)`; a read that hands the object only the payload is comparing the void as an integer, and
  `d = {None: 3.5}` / `k = None` / `print(d[k])` died `KeyError` where the reference prints `3.5` (**R.286**,
  **ADR 0420**). The tag now comes from the same latest-binding records print, truthiness, `str` and equality live
  by, so the key door cannot know less about a name than the printer does:

  ```gusty
  d = {None: 3.5}
  k = None
  print(d[k])                    # 3.5    (was: KeyError: key not found, exit 3)
  print(d[k] + 1)                # 4.5
  s = {None: "a"}
  print(s[k] + "b")              # ab     (the label road asks the door, the door reads the binding)
  t = {True: "one"}
  flag = True
  print(t[flag], t[True], t[1])  # one one one
  print({None: 1, 0: 2}[k])      # 1      — and the number 0 is still a different key
  z = {0: "zero"}
  print(z[k])                    # KeyError: None — a miss names the void, and a bound text names itself
  ```

  What it does not reach, each pinned with today's answer beside the reference's: an operator road over an entry
  addressed by a bound **number** key refuses, because the door can
  retire a void and has no number's value to retire (**R.436**, whose other half is **R.189**'s `KeyError: 5`); and a
  bound **verdict** key that misses says `key not found`, because the record says a name holds a verdict and not which
  one. The blanket "it is an integer" that `elemKindTag` falls back to for a name no arm recognises is **L11.1**.

  **The container literal asks the same question the print road asks** (**R.435**, **ADR 0421**). A slot whose value
  arrived through a binding, rather than being spelled, used to be built by the cheap road — one recorded element
  kind for the whole literal, no tag beside the payload — and the screen printed the void's payload, which is the
  number `0`:

  ```gusty
  k = None
  print([k])            # None  (was [0], at the exit code of success; print([None]) had always been right)
  print([k, 1])         # [None, 1]
  print({k})            # {None}          print((k,)) → (None,)
  print({1: k})         # {1: None}       print({k: 1}) → {None: 1}
  print([k][0])         # None            print(None in [k]) → True     print(len([k])) → 1
  def nothing():
      pass
  print([nothing()])    # [None]          — the void a BODY answers, and every in-place mutator's answer
  xs = [1]
  print([xs.append(2)]) # [None]          — the list still GREW; only the element is the void
  print([None])         # [None]          — the spelled singleton, and the mixed sibling, untouched
  k = 5
  print([k])            # [5]             — the LAST binding is the truth (ADR 0172)
  ```

  The gate that chooses the cheap road and the door that labels the slot now ask **one** predicate, the ADR 0172
  record print, `str`, truthiness and equality already live by, and they carry the same two exemptions (a name the
  unroll tagged is THIS item; a name the container records claim holds a handle the object describes). A `None` that
  reaches a slot is a `TagNone` slot: the builder forces its payload to `0`, the object says its slots are the truth,
  and the mixed printer walks the pair — the static `int` array the numeric literal still folds from is left alone,
  so `sum`, `min` and `max` kept **ADR 0239**'s fold. What the arm still cannot see is a void that arrives through a
  frame's front door and a container the program NAMED sitting beside a number — **R.437** and **R.439**, each
  pinned with today's bytes beside the reference's; and the fold that adds a void's
  payload instead of raising is **R.438**.

  **A loop item's own kind is the item's, not the name's** (**R.440**, **ADR 0424**). The unrolled `for` road
  records what each item is once per iteration (`loopElemTag`, beside the interned-text record **ADR 0233**
  grew), and the print door has read that record since **R.358**; what did not read it were the four gates that
  decide whether a container literal needs its slots labelled, so one value was answered twice:

  ```gusty
  for k in [None, 1.5]:
      print(k)          # None    1.5     — the read road, which asked the record
      print([k])        # [None]  [1.5]   — the literal (was [0], [1] at the exit code of success)
      print([k, k])     # [None, None]    [1.5, 1.5]
      print({v: "x"})   # {None: 'x'}     {1.5: 'x'}       (v the item, in a dict key)
  for k in [[1], [2, 3]]:
      print([k])        # [[1]]   [[2, 3]]                 — an item that is a container
  for k in [True, False]:
      print([k])        # [True]  [False]
  for k in [1, 2]:
      print(sum([k]))   # 1       2       — the fold's static array, unchanged (ADR 0239)
  k = None
  for k in [1, 2]:
      print([k])        # [1]     [2]     — the item is a number; the older binding is not the truth
  ```

  One predicate answers the question — `loopItemTag`, `loopItemIsThis` and `loopItemKindTag` in
  `pkg/lang/heapargs.go` — and both sides ask it: the four gate arms (`floatSlotExpr`, `boolSlotExpr`,
  `noneSlotExpr`, and the container arm `loopItemIsContainer`) before they ask the NAME, and the builders'
  own `elemKindTag`/`elemTagFor`, which had carried a private copy of that reading since **R.358**
  (**ADR 0249**'s rule, one map, one reading). The answer carries whether the record SPEAKS for the name at
  all, not only whether the item has a kind a compiled word cannot render: an item that is a plain number is
  still an item, and a gate that hears "no kind" goes on to read the stale void record and refuses the literal
  as mixed. An item whose kind the fold can spell — a number, an interned text — answers "no kind", which is
  what keeps a numeric loop folding from the static array. What the same sweep found and did not fix is named
  rather than absorbed: the item's **identity and membership** (**R.416**), its **interpolation field**
  (**R.445**), the **fold over a fractional item** (**R.446**), and the **slot the `for` road stores an `i32`
  into when the float road declared that slot a `double`** (**R.447**).

  The identity and membership half has since been paid (**R.416**, **ADR 0426**): `isTaggedCompareSide` admits
  the item to the pair door and the literal-haystack membership scan asks `@rt_contains_tagged`, both reading
  this same record, so `for v in [None, "a"]:` answers `print(v)`, `v is None`, `v == None` and `v in [...]`
  from one `(payload, tag)` pair. The payload is always the LOAD of the item's own slot — never re-derived by
  asking the float or container road for the name, which would mint a second object out of whichever number the
  slot holds (**ADR 0233**). What that probe found and left owed is filed the same way: the **ordering** door
  still never asks the item (**R.448**), the **condition** road folds `x == None` by the payload words while the
  value road folds it by the source kinds (**R.449**), and an item whose kind is a container, compared with a
  container literal, is **refused** where the reference answers (**R.450**).

  A `None` key is therefore not the number `0` to this backend even though the constant fold reads a
  `NoneLit` as `0`: the key door answers the void FIRST, before any number arm can reach it. `1`, `True` and
  `1.0` remain one key, which is the reference's rule, and `print({0: "a"}[False])` still answers `a`. What it does reach: the f-string field of the entry
  (**R.283**, **ADR 0356**), `abs` and the `min`/`max`/`sum` folds of it (**R.287**, **ADR 0387**), and its
  **truth** — `if d[None]:`, `while d[None]:`, `"y" if d[None] else "n"` and `bool(d[None])` all branch the
  reference's way (**R.284**, **ADR 0388**), because the condition asks the entry the same `(payload, tag)` pair
  print asks and decides it in `@rt_pair_truth`: an empty text, an empty container, `0.0`, `-0.0`, `False` and
  `None` are false, and a text's interned index or a float box's handle is not evidence either way. The item of
  an unrolled comprehension asks the same table (**R.359**): `[bool(x) for x in [0.0, [0]]]` is `[False, False]`.
  **A call argument that is such an entry carries the pair too** (**R.220**, **ADR 0389**): `def id(v): return v`
  with `print(id(d["a"]))` prints `x`, not the text's `@str_tab` index, and a float, `None`, a verdict, a list or a
  dict entry likewise arrives with its kind beside its payload — the emission asks the one subscript door print
  asks, so the two spellings of a read cannot disagree. An entry the literal already describes as an integer keeps
  the one word it always had. What the row does not reach is pinned with today's answer beside the reference's: a
  callee that TESTS its own parameter — `if v:` over the pair — still asks the payload (**R.361**), a recursive
  callee that hands the parameter back and a ternary whose chosen arm is the parameter keep one word for the
  answer (**R.146**, **R.147**), and an entry bound to a name first (`x = d["a"]` then `id(x)`) is still the
  one-word position of **Gap R.146**. Two more shapes are pinned the same way because this row's own measurement
  walked into them: a **tuple** that lives in a slot is repeated and concatenated as a **list** — `d = {None: (1,)}`
  with `print(d[None] * 2)` writes `[1, 1]` where the reference writes `(1, 1)`, and `print(d[None] + (2,))` raises
  `TypeError: can only concatenate list (not "tuple") to list` where it writes `(1, 2)` — because the sequence door
  asks its operand a KIND and a slot read has no static kind, so the list arm answers for the tuple (**R.434**);
  and a FRACTION in a slot that goes through `min`/`max` into a call loses its fraction (`3` for `3.5`), because the
  fold that would keep it hands a bare `double` to a position one word wide and `llc` rejects the module (**R.433**).
  `print(d[None])`, `len(d[None])` and `for v in d[None]` all answer correctly, which is what makes those two doors
  rather than a general illness — and the same program with an `int` key behaves identically, so it is the SLOT, not
  the void key, that costs the tuple its brackets.

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
  `xs.pop(-1)`, `l[-2:]` and `"abc"[-1]` mean what they mean in Python, and the compiled backend and
  CPython agree (`programs/negative_index.gy`, ADR 0210). Dict and set subscripts are keys, so
  they keep their sign: `d = {-1: "minus"}` then `d[-1]` is `"minus"`, not the last entry. A
  container *literal* with a negative constant subscript is folded at compile time; that shape
  used to be a Go panic inside the compiler rather than an answer or a diagnostic.
- A container produced by a call (`d = make(3)`) is iterated through the runtime
  length, like any other container variable.
- **A subscript of a string is a one-character string** (ADR 0225). `s[1]` is `b`, not `98`:
  the character is text in the compiled backend, so it compares with text, concatenates, takes methods, and
  `s[1] == 98` is false the way CPython says it is. A string is counted in **code points** wherever
  position is asked about — `s[i]`, `s[a:b]`, `len(s)`, `ord(s)` — so `len("café")` is 4 and
  `"café"[3]` is `é`; a byte-wise slice could also cut a character in half. An out-of-range character
  subscript traps as `IndexError` on the compiled backend.

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

  Three limits belong to the compiled backend and are stated rather than approximated: the text methods
  that take an argument (`count`, `find`, `split`, `replace`, `startswith`, `encode`, `zfill`, `ljust`,
  `rjust`) answer on the roads that can read their receiver and refuse on the ones that cannot
  (Gap I.2); `str(<float>)` refuses rather than truncate a float it cannot yet hold (L11.6);
  and `%`-formatting of a text is answered by the run-time door described in § *Printf-style `%`*
  (Gap R.31, ADR 0360). Case mapping and trimming are NOT among them: `upper`/`lower`/`title`/
  `capitalize`/`swapcase` and `strip`/`lstrip`/`rstrip` answer the reference's answers on both roads,
  from one generated table (ADR 0362, § *Case and trim* below). A program that creates more distinct strings
  than the table holds (4096) raises `RuntimeError`, which it can catch — the alternative, which used
  to happen, was silently reusing an entry and printing a different string than the program built.
- **`sep.join([...])` is the concatenation, slots and all** (**ADR 0408**, roadmap **Gap R.403**). A list written
  at the call is answered whether its slots are literals or not:

  ```gusty
  n = 3
  print("-".join(["a", "b"]))          # a-b    — folded at compile time
  print("-".join([f"a{n}", "z"]))      # a3-z   — one @rt_str_cat per seam, the separator interned once
  print("-".join(("a", f"b{n}")))      # a-b3   — a tuple reads as a list here, as the reference reads it
  print("".join([f"a{n}"]))            # a3     — an empty separator, and a one-element list that never meets it
  ```

  A slot holds a text or the program hears about it: an element the compiler can see is **not** a text raises the
  reference's own catchable `TypeError: sequence item N: expected str instance, int found` — class, sentence and the
  position the reference counted, with `bool` spelled `bool` and `None` spelled `NoneType` — at exit **3**, catchable
  by the program's `except TypeError:` (**ADR 0166**'s trap). Two shapes are refused in words rather than answered:
  the list the program BUILT and named (`xs = [f"a{n}", "z"]` / `"-".join(xs)`, **Gap R.404** — the road has no slot
  to ask, the list is a run-time object) and the separator the program BUILT (`s = f"-{n}"` / `s.join(["a", "z"])`,
  **Gap R.405** — the receiver door above answers `upper`/`strip` on a runtime text and does not yet answer `join`).
  A join's answer is one interned text like any other, so it prints, compares, concatenates, crosses a parameter
  and leaves a `return` as text (**ADR 0368**'s answer direction reads its tag the same way).
- **Containers hold strings.** `xs = ["a", "b"]`, `xs.append("s")`, `xs[0] = "s"`,
  `s.add("q")`, `d["k"] = 1`, `d[1] = "v"`, `"a" in xs`, `for x in xs`, `len`, indexing
  and printing all work in the compiled backend. **A string value is an index into a runtime interned
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

 A verdict writes its own name wherever the front end can see the expression that made it —
  `print(True)` is `True`, `print(1 == 1)` is `True`, `str(True)` is `'True'` (a real string, so
  `.lower()` works on it), and an f-string interpolates `True`/`False` — while the value behind it
  stays the untagged `0`/`1` the compiled backend has always used (ADR 0257). Where the front end cannot
  see the expression — inside a container, where only the slot is left — the slot carries the tag that
  says so, so `print([True, 1])` is `[True, 1]`, `print({"k": True})` is `{'k': True}`,
  `print({True, 1})` is `{True}` and `str([True, 1])` is the same text, because print and the pair are
  one table (ADR 0258, ADR 0259; Gap R.112). A verdict handed to a function writes its name too: the
  call sites answer whether the parameter holds one, so `show(True)` prints `True` (ADR 0348; Gap R.111),
  and only a position the call sites contradict prints the number it was stored as (Gap R.270).
  Inside containers strings are quoted as Python does.
  A set keeps its members in the order the reference's table gives them, and the order is decided once, at
  the door that inserts: a number sits at `payload mod 8` of the first table, a verdict at `0`/`1`, and the
  void ahead of every one of them (its hash lands it in bucket 0), so `s = {2}` / `s.add(1)` /
  `s.add(None)` prints `{None, 1, 2}` — and `for x in s` walks the very same sequence, because `print` and
  the loop read the one array the container owns (`Gap R.421`, ADR 0415). A member the payload cannot
  answer for — a text — keeps insertion order after the numbers, which is the only order that can be
  promised for a kind whose reference hash is salted per process: `{"q", "r"}` prints as `{'q', 'r'}`
  here. Two members that collide in a bucket, and a set big enough to outgrow the first table, are the
  reference's probe sequence and rehash this backend does not yet simulate (`Gap R.424`), and a TEXT member
  sits after the numbers because the reference's hash of it is salted per process and no function of the payload
  can reproduce it (`Gap R.425`) — the reference legs here pin that salt (`PYTHONHASHSEED=0`), which makes the
  reference's answer reproducible without making it knowable.

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

Supported in the compiled backend: passing a string positionally, by keyword, or as a default; a
`str` annotation; `print(p)`, `len(p)`, `p == "text"`, `p != "text"`, forwarding `p` to another
function, storing it in a list/dict/set, using it as a membership needle, and returning it
(`print(echo("yo"))` prints `yo`). Comparison is cheap because interning makes equal text the
same index.

Not yet supported in the compiled backend — each is refused with a diagnostic that names the missing door,
says what the reference does there, and cites the roadmap row that owes it (ADR 0166), rather than failing
in the verifier or answering a table index:

<!-- The list below is the refusals that still hold. Two of its original entries — "concatenation of a
     runtime string" and "string methods on a parameter" — were falsified while the printf-style door was
     built and have been removed: `def f(s): return s + "!"` answers `hi!` and `def f(s): return s.upper()`
     answers `HI` on the compiled leg (ADR 0224's interned handle made them reachable, `L11.1`), and a list
     kept here that no longer refuses would send a reader looking for a door that is open. What replaces
     them is checked, not remembered: each line below was compiled and run before it was written.
-->

- **arithmetic on a string** (`s + 1`) — CPython raises `TypeError`; the compiled backend refuses rather
  than computing with a table index, and the record holds the answer the retired engine gave. **Ordering is not in this list**: an
  ordering of two texts is answered by `strcmp` on the bytes behind the index (ADR 0248), and
  ordering a text against a number is a separate open trap (Gap R.85), and an ordering whose
  operand is a slot whose kind the object carries asks the tag which pair it was — two numbers,
  two texts, or the `TypeError` CPython raises (ADR 0250);
- **a parameter used as both a string and a number** (`f("a")` and `f(7)`) — guessing would
  print `7` through the string table, so it stays a diagnostic.


Before this was implemented, `d[1] = 2` did not work at all: the parser accepted the
statement, consumed `= 2`, and threw it away, so the program ran as if the line were
absent — on the compiled backend, with no diagnostic.

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
  bad index raises `IndexError: pop index out of range` — the two sentences the reference has
  always had, and the empty list has always had its own. Both are real exceptions, so
  `except IndexError:` catches them. The bounds test is emitted
  around the removal, and `rt_pop` shifts the tail left *with its tag*: a list slot is a payload
  and the tag that says what the payload means, so the removal moves both words and hands the
  removed slot's tag to the position that asked (`Gap R.411`, **ADR 0410**). The value a `pop`
  returns is therefore one value to every reader: `print(xs.pop(0))`, `str(xs.pop(0))`,
  `if xs.pop(0):`, `xs.pop(0) == 1`, `xs.pop(0) > 1`, a popped element of a list literal and a
  popped value passed to a function all agree, and an ordering that the reference answers with a
  `TypeError` answers with that `TypeError` here too.
- **What the removal still gets wrong** (filed, not hidden): a popped TEXT in an arithmetic position is answered by the untagged
  doors, so `xs.pop(0) * 2` raises where the reference prints `aa` and `xs.pop(0) + "?"` refuses
  where it prints `a?` (`Gap R.413`); a function body whose `return` IS the popped slot hands
  back one word instead of a pair (`Gap R.414`); a function handed the popped slot prints the payload,
  so `def w(v): print(v)` above `w(xs.pop(0))` writes `0` for `None` (`Gap R.417`); and the same program
  over an aliased list answers no differently — the alias is the notebook's business, not the callee's
  (`Gap R.415`, **ADR 0412**, pays the alias and leaves that one to its own row). A `tuple.pop()` is refused — a tuple is immutable
  (**L11.3**, **ADR 0361**) — and `dict.pop()` over a name is refused with
  `lower.unsupported.container-method-over-a-name` (`Gap R.174`).
- **A second name for a container is ONE container, and answers the same at every door** (**Gap R.415**,
  **ADR 0412**; **Gap R.419**, **ADR 0413**).
  `ys = xs` shares the object — it is not a copy — and what the object's slots hold is a fact about the OBJECT, not
  about whichever name the read happens to be written against: `print(ys.pop(0))`, `print(ys[0])`,
  `for v in ys: print(v)`, `if ys.pop(0):` and `ys.pop(0) is None` all answer what the same line written against `xs`
  answers, and a mutation is one event on one object wherever it is written — `ys.append("a")` is told to `xs`, and
  `xs.append("a")` is told to `ys`. The same rule holds for the other two containers: a value written through an
  aliased dict arrives with its tag and a record to live in (`print(d)` is the reference's `{'a': 1, 'b': 'x'}`, not
  the intern index the stale claim rendered), and a member added through an aliased set is stored with its tag rather
  than reaching the text table unlabelled. A name keeps the facts of the object it HOLDS, so after `xs = [2, None]` the
  alias `ys` still describes the first list (`print(ys)` is the reference's `[None, 1]`), and `s = {2, 3}` leaves `t`
  printing `{1}`. What this leaves owed is filed rather than smoothed over: the dict rebound under its own alias is
  `Gap R.420` (the rebinding releases the object and the allocator hands that slot to the next dict), the set built
  from a LITERAL writes its members with no tag and in the order the source named them (`Gap R.423`) — each of
  those answers the same way with NO alias in the program — and a container METHOD over an aliased container
  (`for k, v in e.items():`) is refused exactly as it is over the source name, because an alias is a
  name and that is `Gap R.146`/`Gap R.174`'s standing, not this one's. What was owed here and is paid:
  `t.discard("a")` and `t.remove(1)` over the aliased set now answer the reference (`Gap R.422`, `ADR 0425`)
  — an alias is a name, and a name is what the removal door asks its member's pair of.
- `set()` / `list()` / `dict()` construct empty containers. `s.add` / `s.discard` /
  `s.remove` / `s.clear` are the set methods; `remove` raises `KeyError` where `discard`
  is silent, matching Python.
- **Removing a member asks the ARGUMENT, not the set** (`Gap R.422`, `ADR 0425`). A member is a pair — a
  payload and the tag that says what the payload means, because a stored text is an index into the string
  table and the text `"0"` and the number `0` are the same word — and `s.discard(v)` / `s.remove(v)` compare
  BOTH words, exactly as `s.add(v)` writes them. So the value a set has never held removes nothing:

  ```
  s = {1, 2}
  s.discard("a")      # nothing — the set prints {1, 2}
  s.discard(9)        # nothing
  s.discard(None)     # nothing
  s.discard(1.0)      # the 1 goes: 1.0 == 1 and hash(1.0) == hash(1)
  s.remove(9)         # KeyError: 9 — the member is named in the sentence
  ```

  A member the language refuses to hash raises `TypeError` BEFORE the set is consulted, worded for the type
  the source wrote: `s.discard([1])`, `s.discard({"a": 1})`, `s.remove((1, [2]))` and `s.discard((1, {2}))`
  are `TypeError: unhashable type: 'list'` / `'dict'` / `'list'` / `'set'`, the last two because a tuple walks
  its elements and names the first it cannot hash. One exemption is the reference's own and not this
  backend's convenience: a BARE set is a missing member, never an unhashable one — `s.discard({1})` raises
  nothing at all and `s.remove({1})` says `KeyError: {1}` — while the same value inside a tuple still raises.
  A tuple spelled as a tuple is hashable here even though it shares a list's storage (`ADR 0361`), so
  `s.discard((1, 2))` prints the set unchanged.
- **A set prints in the order its members arrived** (`Gap R.170`, `Gap R.423`): `{1, 2}` prints `{1, 2}`.
  The reference prints hash-slot order, so a mixed-kind set can print its members in a different sequence —
  `s = {"0", "1"}` / `s.discard(0)` / `print(s)` is `{'1', '0'}` there and `{'0', '1'}` here. Which members
  are in the set, and what `len` and `in` say about them, agree with the reference; only the sequence differs.
- **An empty container written as a call is the object its literal spells** (Gap R.360, ADR 0391),
  wherever it lands: `xs = [set()]` / `print(xs[0])` is `set()`, `d = {None: list()}` /
  `print(d[None])` is `[]`, `print([set(), list(), dict()])` is `[set(), [], {}]`, and
  `print([bool(x) for x in [set()]])` is `[False]`. The slot keeps two words — the handle and the
  kind beside it — so the printer, the truth test, `len` and `==` all ask the object rather than
  the address. The deciding doors ask the same question the storing roads do (Gap R.364, ADR 0392),
  so `print(len(set()))` is `0`, `print(list() == [])` and `print(set() == set())` are `True`,
  `print(sorted(set()))` is `[]`, `print(list() + [1])` is `[1]`, and `print(abs(list()))` raises
  CPython's `TypeError: bad operand type for abs(): 'list'` — catchable by `except TypeError:`, which
  a refusal never is. A program that defines `set` itself keeps its own answer (`def set(): return 1`
  then `print([set()])` is `[1]`). The spelling reaches a call argument too, at either side of a container and
  at any depth: `def f(v):` / `    print(v)` / `    if v:` with `d = {"k": list()}` / `f(d["k"])` prints `[]`
  and takes the `else` arm, and `xs = [set(), [1]]` / `f(xs[0])` prints `set()` — the read of such a slot
  crosses as the `(payload, kind)` pair the spelling describes, because the pair scan asks this same door and
  no longer re-derives the answer from a list of node types (**Gap R.369**, closed by **ADR 0394**). What the spelling still does not reach is filed and named:
  `print(set() < set())` answers `True` where the reference answers `False` — the set-ordering helper the
  literal spellings lack too (Gap R.175) — `sum(list())` refuses with the same sentence a container
  variable refuses with, `{set()}` prints where the reference raises unhashable (Gap R.81), and a
  container key written as a call refuses the way a container key written as a literal refuses.
  Three faces of the same missing word are filed beside it: the **truth** of an empty container spelled as a
  call where no call crosses — `print(bool(set()))` at module level answers from the handle, so the first one
  in a program is false by luck and the next is true (**Gap R.370**); the **value** of a parameter the caller
  supplied, as opposed to its truth — `repr(v)`, an f-string field, a container slot written from the
  parameter, `len(v)` and `v == []` all still read the payload or refuse it (**Gap R.371**); and the **key**
  of the read when a `for` variable holds it — `for k in ["a", "b"]: f(d[k])` crosses four handles where
  `print(d[k])` in the same loop prints right (**Gap R.372**).
- **Not lowered:** the one-argument copy forms `list(xs)` / `set(xs)` / `dict(d)` are a compile-time
  refusal that names the runtime door they need (`rt_list_clone` and its dict/set siblings, which were
  never emitted — the retired engine did the copy in Go), what the reference answers, and the rows that
  own the work (`Gap I.2`, `L11.1`, `L12.11`). Until then: build the container with the empty constructor
  and add elements.

`import mod` loads `mod.gy`, evaluates it, and binds `mod` to a module
namespace. Top-level variables and functions of the module are accessed as
`mod.name` and called as `mod.fn(args)`. A module can itself `import` other
modules. Imports are compiled by the one backend (the REPL and `--eval` compile too, since ADR 0302). The AOT backend supports **data imports**: `import mod` loads `mod.gy`, parses + analyzes it, and constant-folds the module's top-level global variables, so `mod.var` reads compile statically to constants. Module function dispatch and non-constant globals are deferred with a clear compile error. Modules may themselves `import other` (nested imports): the nested module's globals are folded recursively and resolve via `other.var` references. Module globals may be strings and use `+` string concatenation. Printing an imported string module global (`print(mod.str)`) emits `printf("%s", i8*)` and outputs the folded string. `len(mod.str)` returns the folded string's length. `ord(mod.str)` returns the folded string's first byte value. `reversed(mod.str)` returns the folded string reversed. Module globals may also be lists (`mod.list`), folded element-wise; `mod.list[i]` indexes into the folded list; `len(mod.list)` returns its length. `sorted(mod.list)` folds the sorted list. `reversed(mod.list)` folds the reversed list. Module globals may also be dicts (`mod.d`), folded key/value-wise; `mod.d[k]` indexes the folded dict by integer key; `len(mod.d)` returns its entry count. Indexed imported dict elements fold in arithmetic (`mod.d[k] + mod.d[j]`). Indexed imported list elements fold in arithmetic (`mod.l[i] + mod.l[j]`).

### On-disk stdlib modules

`import mod` resolves `mod.gy` on disk: first in the working directory, then in the
standard-library root (`stdlib/` at the repository root, or `$GUSTY_STDLIB_DIR` /
`gustyc --stdlib <dir>`). The bundled stdlib ships data-only modules that both the
interpreter and the AOT compiler fold as top-level constants:

- `import math` — `pi`, `e`, `tau` (the reference's names and the literals the module declares) and `PI`, `E`,
  `TAU` (this language's own names, aliased to the same literal), plus `PHI`, `SQRT2`, `LN2`, `LN10` and their
  lower-case spellings — constants the reference's `math` module does not carry.
- `import string` — `DIGITS`, `LOWERCASE`, `UPPERCASE`, `HEXDIGITS`, `WHITESPACE`, `PUNCT`.
- `import collections` — `EMPTY_DICT`, `EMPTY_LIST`, `ZERO`, `ONE`.
- `import json` — `NULL` (`None`), `TRUE` (`True`), `FALSE` (`False`).

**A constant the reference has, the module has under the reference's name** (roadmap L11.6, ADR 0321). `PI` is
this language's spelling and the record holds it, but the reference spells its constant `math.pi`, and a name the
reference cannot spell is a name the reference leg cannot adjudicate: `probe_math_const.gy` — the corpus program
written with the upper-case names — is recorded `not_applicable` for its spelling alone, and the comparison lived
in a hand-written twin. So `stdlib/math.gy` declares `pi = 3.141592653589793` and `PI = pi`: one literal, two
names, and `integration/programs/probe_math_constant_answers_the_references_spelling.gy` is a program CPython
runs. The four names the reference does not have (`PHI`, `SQRT2`, `LN2`, `LN10`) stay asserted against their
values, and are answered under both spellings.

What the module does not answer is still a refusal in words at exit 1, named by what is missing:
`math.inf`, `math.nan` and the reference's module functions (`math.floor`, `math.sqrt`) are roadmap
Gap R.207's row, and `string.digits` — the reference's spelling of the data module's own name — is Gap R.206's.

Reads like `math.PI` resolve to the folded constant in the compiled backend. the record
also supports importing function-bearing modules (module functions dispatch at runtime);
AOT module-function emission is supported: imported modules that define functions are lowered as mangled defines (`mod$fn`), and `mod.fn(args)` calls plus sibling-module calls and bare-name module-global capture are handled by the AOT compiler.

**A data module's constant keeps the type the module declares** (roadmap L11.6, ADR 0272). The value behind
`mod.NAME` is a literal the importing program never wrote, so every question about it — is this a double,
does it have a sign, is it a text — is asked of that literal, through the one read in `pkg/lang/module_const.go`:

```python
import math
print(math.PI)         # 3.141592653589793   — every engine
print(math.PI * 2)     # 6.283185307179586
print(-math.PI)        # -3.141592653589793
x = math.PI
print(x > 3.14)        # True
print(abs(""))         # 0                   — and a text the module declares raises, naming 'str'
```

Before that read the compiled backend printed `3`, `6`, `-3` and `False` — the double poured into the integer
word — while the same numbers written by hand were right, because `value()` resolved the name through the fold
and the predicates that decide *how* to write the value saw an attribute they could not read. A name the
program binds itself (`math = 3`) is the program's, not the module's; an attribute the module does not declare
is answered by neither backend's imagination.
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
  the compiled backend — the REPL and `--eval` compile too, since ADR 0302.
- **Decorators** apply before a `def`, one per line: `@dec def f: ...` is
  equivalent to `f = dec(f)` at definition time. Multiple decorators apply
  bottom-up (`@dec1 @dec2 def f` -> `f = dec2(dec1(f))`). A decorator is a
  function/closure that takes the function value and returns the (possibly
  transformed) value bound to `f`. Decorators are implemented on the record and in AOT: identity decorators are fully supported, the canonical wrapping decorator (a decorator that returns a nested closure capturing the decorated function) is supported in AOT via compile-time specialization; other transform decorators are rejected (ADR 0157).

### A function that answers more than one kind carries its kind (Gap R.219, Gap R.228, ADR 0368)

A body may hand back values of different kinds on different roads, and the kind is part of the answer:

```python
def f(x):
    if x:
        return "x"
    return 0

print(f(1))          # x
print(f(0))          # 0
print(str(f(0)))     # 0
print(repr(f(0)))    # 0        (repr of the int, not of a text)
print([f(1), f(0)])  # ['x', 0]
print(f"{f(0)}")     # 0
if f(1):
    print("t1")      # t1

def g(x):
    if x:
        return 7

print(g(1))          # 7
print(g(0))          # None — reaching the end of a body answers None, not 0
```

Each `return` road stores the tag table's word for the kind it produced beside the payload it has always
produced — `0` int, `2` bool, `3` None, `4` str, `5` list, `6` dict, `7` set — and a body that runs off its
end stores the void's. The position that reads the answer then asks the **tag** rather than guessing the kind
from the payload: `print`, `str`/`repr`, a condition, a container element, a binding and an f-string field all
ask that one pair, which is why `print(f(0))`, `str(f(0))` and `f"{f(0)}"` cannot answer three different
things. A body whose arms all answer **one** kind is compiled exactly as it always was — the tables keyed by
the callee's name are correct for it, and this pass leaves them alone.

What the compiled backend **still gets wrong** is Gap R.324, and the shapes are named here rather than
smoothed over. An answer handed to an ordinary function keeps one word: `def g(v): return v` with
`print(g(f(1)))` writes `0` where the reference writes `x`. A **method** whose arms mix kinds answers the text
twice (`print(C().m(0))` writes `x` for `0`), because a method's answer travels `emitClassMethod` rather than
the `return` roads above. An arm that answers a **float** beside any other arm is left on the old roads —
`return 1.5` / `return 2` prints `1.5` and `2.0`, and `return "a"` / `return 1.5` refuses with *a string cannot
be lifted to a double* — because the answer direction names a kind only where the source spells one, and a
double crosses this backend's call boundary as a `double` rather than a payload. And a **conditional** answer,
`return "x" if x else 0`, prints `0`/`0`: one road whose two arms answer different kinds needs the word the
branch took, which is a run-time fact this pass does not emit. Arithmetic and comparison, measured, are not on
that list.

The machine path: `gustyc --emit-llvm` shows the decision. A carried body declares one internal global and
every `return` road stores into it, so a caller — human or agent — can tell a carried answer from a one-word
answer without running the program:

```
@gy_f.anst = internal global i32 0
  store i32 4, i32* @gy_f.anst      ; return "x"  — TagStr
  store i32 0, i32* @gy_f.anst      ; return 0    — TagInt
  store i32 3, i32* @gy_f.anst      ; fall off    — TagNone
  %t11 = load i32, i32* @gy_f.anst  ; the caller asks
```

A function that answers one kind declares no such global.

### Gradual typing

Optional annotations appear on variables (`x: int = 1`), parameters
(`def f(x: int)`), and returns (`def f() -> int`). Annotations are enforced
at runtime on the record: a value whose runtime kind is not assignable
to its annotation is a `type mismatch` error. `any` (dynamic) accepts
everything. Because the record stores booleans as plain integers, `int`
and `bool` annotations accept either kind.

### Ternaries (`a if test else b`)

A ternary hands back **one of its arms**, so its kind is that arm's kind (ADR 0262's rule, applied to texts
by `Gap R.173` / ADR 0290):

```
print("big" if x > 2 else "small")   # big or small — the word, not an intern table position
print(1 if 0 else 2.5)               # 2.5
print(True if x else False)          # True
```

When the test is a value the source wrote, only the arm that **runs** is evaluated — `print("a" if 1 else
shout())` does not call `shout`. When the test is a run-time fact, the arms must agree on a kind: a function
whose returns are `"yes"` and `0` is not a text-returning function, and a pair of arms that disagree on a
word is refused rather than guessed (ADR 0262). A **container** arm answers on the same
road: `print([1, 2] if 1 else [9])` is `[1, 2]`, `print({"a": 1} if 1 else {"b": 2})` is `{'a': 1}`,
`print((1, 2) if 1 else (3, 4, 5))` is `(1, 2)` and a comprehension arm — `print([v for v in [1, 2, 3] if v > 1] if 1
else [9])` — is `[2, 3]`, each arm built inside the block the branch enters and phis its handle to a printer chosen
from the kind the arms agree on (**ADR 0405**, closing `Gap R.385` and `Gap R.128`). What still refuses in words is the
pair whose arms agree on no kind (`[1, 2] if c else {"a": 1}`), whose answer is the tag printer of `Gap R.67` / ADR 0333
(`Gap R.394`).

### `None` from a lookup

`d.get(key)` with the key **absent** and no default hands back `None` — not the number `0`
(`Gap R.174`, ADR 0291):

```
print({"a": 1}.get("z"))        # None
print({"a": 1}.get("z", 42))    # 42   — an explicit default is returned as written
v = {"a": 1}.get("z")
print(v == None)                 # True
print(v == 0)                    # False
```

The compiled backend folds a container method over a **literal written at the call**. A container bound to a
**name** (`d = {"a": 1}` / `d.get("a")`) is answered by the record and declined by the compiled leg, which
names the receiver and the representation it waits for; a dict whose **answer is a text**
(`print({1: "x"}.get(1))`) still prints `0` there. Both are owed to L11.1's tagged value word and are pinned as
still-owed rather than claimed.

### What kind a power answers with

`x ** y` answers an **int** or a **float**, and which one is a rule about the operands, not about how they
are written (`Gap R.176`, ADR 0293):

```
print(2 ** 3)        # 8          int ** int, exponent >= 0
print(2 ** -1)       # 0.5        int ** int, NEGATIVE exponent -> a float, not 0
print(2 ** -2)       # 0.25
print(4 ** 0.5)      # 2.0        anything float -> a float
print(2.0 ** 10)     # 1024.0     note the .0 — the reference keeps it
print(2 ** 3 ** 2)   # 512        right associative, and still an int
print(0 ** 0)        # 1
```

Both legs agree with the reference on every line. Two shapes are **not** answered, because there is
nothing correct to answer with:

```
print(0 ** -1)          # ZeroDivisionError: 0.0 cannot be raised to a negative power
print((-8) ** (1/3))    # refused: the reference answers a COMPLEX number, which this language has no value for
print(2 ** 100)         # refused: the reference has arbitrary-precision integers; this backend is bounded
```

The `ZeroDivisionError` is a real trap — catchable with `except ZeroDivisionError:`, on both legs, with
the reference's own wording — and it fires whether the base is a literal or a name bound to `0`, because
the check is emitted at run time. The refusals name what is missing rather than printing the `inf`, `nan`
or wrapped `0` the machine would hand back.

The compiled integer is 32 bits, so `2 ** 31` refuses there while the record (64-bit) answers it; the
interpreter refuses at `2 ** 63`. Neither wraps in silence. A separate parser row covers `-2 ** 2`, which
the reference reads as `-(2 ** 2)` = `-4` and this front end currently reads as `4` (`Gap R.177`).

### A list and a dict have their methods

The container methods a Python reader writes without thinking exist, and answer what the reference
answers (`Gap R.188` / `Gap R.63`, ADR 0301). Until this cycle all eight answered `no such list method`
/
`no such dict method` on **both** engines:

```
xs = [1]
xs.extend([2, 3])     # xs is [1, 2, 3]     accepts a list or a set
xs.insert(0, 0)       # xs is [0, 1, 2, 3]  an index past either end CLAMPS, it does not fail
print(xs.index(2))   # 2                   found by VALUE, so [1].index(True) is 0
xs.remove(0)          # deletes the first match
xs.clear()            # xs is []

d = {"a": 1}
d.update({"b": 2})    # {'a': 1, 'b': 2}    an existing key keeps its position, its value is replaced
print(d.pop("a"))     # 1                   an EXPRESSION: answers with what it removed
print(d.pop("z", 9)) # 9                   the second argument makes absence a default, not an error
print(d.setdefault("c", 3))  # 3            AND WRITES it in -- what makes d.setdefault(k, []).append(v) work
d.clear()             # {}
```

The methods that **mutate and return nothing** (`extend`, `insert`, `remove`, `clear`, `update`) answer
`None` like every other in-place mutator; `pop` and `setdefault` are expressions. `list.index` raises
`ValueError: 5 is not in list` rather than answering `-1` — a `-1` is a legal index, and a program would
quietly read from the end of the list. `list.remove` raises `ValueError: list.remove(x): x not in list`,
`dict.pop` raises `KeyError: 'z'` (a `KeyError` carries the **key's repr**: `Gap R.189`).

`d.popitem()` **exists and refuses**, naming the reason: it is a dict method whose answer the compiled
doors do not build (the same `Gap I.2` receiver table as `.items()` over a literal), and a two-element list
would not be an answer — `[(k, v)]` prints where the reference prints `(k, v)`. A method that exists is not
obliged to answer. A tuple itself is a value since **ADR 0361**; what `popitem` still needs is the door that
answers it from a dict's own slots.

The compiled leg folds a container method only over a literal written at the call, so these over a
*variable* still refuse at exit 1 — owed to `L12.11`, whose receiver table is the fix (and, per ADR
0298's rule, not matched by making the record refuse a correct answer).

### A method that changes a container in place answers None

`append`, `extend`, `insert`, `sort`, `reverse`, `add`, `discard`, `remove`, `update` and `clear` change
their receiver and hand back **nothing** — the same void as a builtin with no answer (`Gap R.187`, ADR
0300):

```
xs = [1]
print(xs.append(2))     # None    the list still GREW; only the answer is the void
print(xs)               # [1, 2]
print(xs.sort())        # None
print(xs.reverse())     # None
s = {1}
print(s.add(2))         # None
print(s.discard(2))     # None    silent about absence, and silent about success
print(s.remove(1))      # None
print({"a": 1}.get("z"))  # None   ADR 0291's sibling rule: a missing key is a void too
```

Because the answer is the void, the call is **not a value**: `sum([1, 2, 3].append(4))` is the
reference's `TypeError: 'NoneType' object is not iterable`, not `10`.

`pop` and `popitem` are the mutators that *are* expressions — they answer with what they removed, which
is what makes `while xs: x = xs.pop()` work:

```
xs = [1, 2]
print(xs.pop())         # 2
print(xs.pop(0))        # 1
```

A method of your own named `append` keeps its own answer: the void belongs to the language's container
methods, recognised by the *shape* of the call, not by the spelling of a name. The compiled leg still
refuses, at exit 1, the mutators it cannot lower over a variable (`extend`, `insert`, `update`, `clear`,
dict `pop`, `popitem`, `s.remove(1)` over a name) — those methods do not exist there yet and are owed to
`L12.11` / `Gap R.63`; what changed here is that the ones which do exist answer correctly.

### An f-string field reads a value the way `print` does

A field is a `print` in disguise: `f"{d[1]}"` renders the slot `print(d[1])` renders, and the two cannot answer
differently, because they ask the same subscript door (`subscriptPairOf`, ADR 0356). A slot holds a *value*, and
the kind of that value is a fact about the object, so reading it yields a payload **and the tag that says what it
is** (ADR 0187):

```
d = {1: 3.5, 2: "x", 3: None, 4: True, 5: [2], 6: {"k": 1}}
print(f"{d[1]}|{d[2]}|{d[3]}|{d[4]}|{d[5]}|{d[6]}")   # 3.5|x|None|True|[2]|{'k': 1}
print(f"{d[1]!r}|{d[2]!r}")                            # 3.5|'x'      the repr of the same read
xs = [1.5, "a", None, True, [7]]
print(f"v={xs[1]} and {xs[0]}!")                       # v=a and 1.5!
print(f"{ {'a': 1.5}['a'] }")                          # 1.5        a literal read by its key, in a field
print(f"{xs[0] + 1}")                                  # 2.5        arithmetic over a slot
```

Before ADR 0356 the field road asked none of the pair door's arms and read the payload word alone, so
`f"{d[1]}"` printed the `@float_box` handle (`5`), `f"{d[2]}"` printed the text's index into `@str_tab` (`0`) and
`f"{d[5]}"` printed a heap slot — all at exit 0, with `print(d[1])` on the line above printing `3.5`
(roadmap `Gap R.283`). A field that renders a value goes to the module's ONE tag-reading printer
(`@rt_str_of_value` → `@rt_print_mixed_value`, ADR 0303); a field the compiler can fold at compile time still
emits a constant and pays for no object at all.

**A field may be padded.** `f"{ x }"`, `f"{ x}"` and `f"{x }"` are the same field, and so is a field that opens
with a brace (`f"{ {'a': 1.5}['a'] }"`, `f"{ {1, 2} }"`). An interpolation is a *fragment* of a line, and the
fragment lexer drops the indentation tokens its own padding invents while keeping every column, so a diagnostic
still points at the token the author wrote (`Gap R.294`, ADR 0356). Until that row was paid, every padded field
— and with them every dict- or set-literal field — was `parse error at 1:2: unexpected token`.

**What a field still cannot hold.** A field that *is* a container (`f"{[1, 2]}"`, `f"{ {1,2} }"`) refuses at exit
1: this road builds one `printf` format string, and neither of the two words it can hand `printf` — an `i32` or an
`i8*` — renders `[1, 2]`. Emitting the handle as `%d` is the invalid-module shape ADR 0166 put out of reach, so the
refusal is the answer, and the container's own printing is owed to `L11.1`.

### An f-string's format spec and conversion

A field may carry a **format spec** after a `:` and a **conversion** `!r` / `!s` before it. Both are part
of the program (`Gap R.185`'s sibling `Gap R.186`, ADR 0299) — they used to be cut off at parse time and
discarded, which is how `f"{3.5:.2f}"` came to print `3.5` on both legs at exit 0:

```
print(f"{3.5:.2f}")        # 3.50
print(f"{3.5:.0f}")        # 4         rounding, and 2.5 below answers 2 (the reference's rule)
print(f"{7:05d}")          # 00007
print(f"{-4:05d}")         # -0004     padding goes after the sign; the width counts the sign
print(f"{255:x}")          # ff        also :X :b :o
print(f"{1234:,.2f}")      # 1,234.00
print(f"{3.5:>6}|")        #     3.5|  also <, ^, and a fill: f"{3.5:*^7}|" is **3.5**|
print(f"{0.25:.2%}")       # 25.00%
print(f"{3.14159:.3e}")    # 3.142e+00
print(f"{2}")              # 2
print(f"{2.0}")            # 2.0       an int's empty spec is its digits, a float's is its repr
print(f"{7!r}")            # 7         the conversion, which the parser also used to drop
print(f"{3.5!r:>8}|")      #      3.5| the spec applies to the conversion's text
n = 7
print(f"{n:05d}")          # 00007     a name is formatted too
```

The digits always come from one shared formatter, never from `printf`'s own `%05d` / `%.2f`: the two
diverge on exactly the cases that matter — `%.0f` rounding, `%g`'s exponent rules, and `%d` on a value
the language calls `float`.

**A spec is honoured or the field refuses — there is no third answer.** A spec this language cannot
honour (`!a`, `#`, `;`-grouping, `n`, a `.q`, a spec on a value that is not a number) raises with the
reference's own sentence; `format([1,2], ">8")` answers
`TypeError: unsupported format string passed to list.__format__`, because a list has no `__format__`
beyond the object default. On the compiled leg, a field whose value it cannot read at compile time
(`f"{n:05d}"` over a variable, `f"{3.5 + 1:.2f}"`, `f"{xs[0]:>6}"` over a slot, `f"{max(1,2):>6}"`) and a field
that is a **container** both refuse at
exit 1 naming the spec — owed to `L12.8` with `L11.1`, whose tagged word will make a field's kind
readable. Neither ever emits the unformatted number, and neither reaches exit 2: `f"{[1,2]}"` used to
hand a list's *global address* to `printf`'s `%d`, which `llc` rejected as an invalid module. The spec engine
also never formats a value it did not read: a field it recognised as neither number nor text used to fall
through as an empty **text** and get aligned, so `x = 5` / `print(f"{x:>6}")` wrote six spaces at exit 0 where
the reference writes `     5` (`Gap R.292`, ADR 0356).

### A text iterates one character at a time

A text is an **iterable**, so the roads that iterate one answer the same thing (`Gap R.185`, ADR 0298):

```
print([c for c in "abc"])              # ['a', 'b', 'c']
print([c for c in "abc" if c != "b"])  # ['a', 'c']
print([c + c for c in "ab"])           # ['aa', 'bb']
print([c for c in ""])                 # []
print(sorted([c for c in "cba"]))      # ['a', 'b', 'c']
print(max("abc"))                      # c       the CHARACTERS are compared
print(min("abc"))                      # a
for c in "abc":                        # a, b, c — `for`, a comprehension and max/min cannot disagree
    print(c)
```

Iteration is per **code point**, so `max("aé")` is `é` and not a stray byte — and so is every other
question a text answers: `len`, `s[i]`, `s[i:j]` and the doors that transform a text all count characters
(roadmap L11.5 closed Gap N.2; ADR 0225, ADR 0362).

The compiled leg **refuses** a comprehension whose iterable is a text, and refuses `max`/`min` of one, at
exit 1 with a sentence naming what it cannot lower. That is owed work (`L11.1`), not a limit: before this
row the *interpreter* answered `[]` and `abc` at exit 0, which is a wrong number rather than a refusal.

### Truth of a text, and what a string method answers with

`not` asks the same question an `if` asks, for every kind, and a text-returning string method prints its
text rather than the number the compiler stores for it (`Gap R.183`, ADR 0297):

```
print(not "x")        # False      a NON-EMPTY text is truthy — even "0" and "False"
print(not "0")        # False      the text "0" is not the number 0
print(not "")         # True
print(not None)       # True
print(not [1])        # False
if "x":               # truthy     the `if` and the `not` cannot disagree about one value
    print("truthy")
print("ab".zfill(5))          # 000ab
print("abc".zfill(1))         # abc
print(repr("ab".ljust(4)))    # 'ab  '
print(repr("ab".rjust(4)))    # '  ab'
print("abc".capitalize())     # Abc
print("a b".title())          # A B
print("ab".swapcase())        # AB
print("abc".count("b"))       # 1     a method that answers a NUMBER still answers a number
print("abc".startswith("a"))  # True  and one that answers a verdict answers a verdict
```

`zfill` pads on the side of the digits CPython does, after a leading sign (`Gap R.184`):

```
print("-42".zfill(5))   # -0042    the zeros go BETWEEN the sign and the digits
print("+42".zfill(5))   # +0042
print("42".zfill(5))    # 00042
```

### A dict view is not a list

`d.keys()`, `d.values()` and `d.items()` answer a **dict view**, and a view says so when it prints
(`Gap R.182`, ADR 0296):

```
print({"a": 1}.keys())            # dict_keys(['a'])
print({"a": 1, "b": 2}.keys())    # dict_keys(['a', 'b'])
print({"a": 1}.values())          # dict_values([1])
print({1: "a"}.values())          # dict_values(['a'])
print({}.keys())                  # dict_keys([])
print(sum({1: 2, 3: 4}.keys()))   # 4      — a view is list-SHAPED: sum/min/max/sorted/for/in all work
print([1, 2])                     # [1, 2] — and a list still prints as a list
```

Both legs print all of this identically, at exit 0. A view is a list-shaped value that carries its own
name: everything you can do to a list you can do to a view, and only its rendering differs.

`d.items()` answers `dict_items([('a', 1), ('b', 2)])` on the record; the compiled leg **refuses** it,
because its elements are key/value pairs the compiled doors do not build (`Gap R.126`, beside the `Gap I.2`
method table). Refusing is the honest answer — inventing a bracket-shaped value would be a fabricated
representation. The tuple the pair would print is itself a value since **ADR 0361** (`print(("a", 1))`
prints `('a', 1)`); what is owed is the door that builds these from a dict's slots, not the value.

### What a set counts, and what a `dict.get` answers with

A set has one member per distinct value, and a literal with duplicates collapses — in the compiled backend, in the
same insertion order (`Gap R.181`, ADR 0295):

```
print(len({1, 2, 2, 3}))         # 3        not 4
print(len({1, 1, 1}))            # 1        not 3
print({1, 2, 2, 3})              # {1, 2, 3}
print({1, 1} == {1})             # True
print(2 in {1, 2, 2, 3})         # True
```

`dict.get` answers with the **kind of the slot it read**, or of the default it fell back to (`Gap R.180`):

```
print({1: "a"}.get(1))            # a        a text, printed as text — not its interned index
print({1: "a", 2: "b"}.get(9, "z"))  # z      the default, with the default's kind
print({"k": None}.get("k"))        # None     a void is rendered, not printed as a word
print({1: True}.get(1))           # True     a verdict is rendered, not printed as 1
print({1: "a"}.get(9))            # None     a missing key with no default
print({1: 2}.get(1))              # 2        a number, as it always did
```

One limitation stays open and is pinned as such: a **float** in a dict slot still prints as a whole number on
the compiled leg (`{1: 1.5}.get(1)` gives `1`, where the reference and the record give `1.5`) — that is
`Gap R.105`'s missing tag, and it is covered by a test that reports it rather than by a refusal that would
hide it.

### A slice of a container

`xs[1:]`, `xs[:2]`, `xs[::2]` answer a new container (`Gap R.179`, ADR 0294) — a list, with the reference's
rendering, and not the machine's handle for it:

```
print([1, 2, 3][1:])       # [2, 3]
print([1, 2, 3][:2])       # [1, 2]
print([1, 2, 3][::2])      # [1, 3]
print([1, 2, 3][-1:])      # [3]
print([][:])               # []
print(["a", "b"][1:])      # ['b']        — the text keeps its tag, so it prints as text
print([1, "a", None][1:])  # ['a', None]
print("abcdef"[2:])        # cdef         — a text slice belongs to the text road
print([1, 2, 3][0])        # 1            — a subscript belongs to the element road
```

Slice parts are all optional and negative bounds count from the end, as they do in the reference. The compiled
form allocates a fresh heap object and copies **payload and tag together** (**ADR 0335**): an element whose value
is an interned text, a float or a nested container prints the same way in the copy as in the original. That
sentence was a claim rather than a measurement until this row — `xs = [1.5, "a", [7]]` with `print(xs[0:2])`
printed `[1, 0]` at exit 0, the text's table index and the float's box handle read as the numbers they are not —
and paying it took three roads, because a copy is a container and each road asked about a different one:
the printer has to be told to read a tag, the object being copied has to be built tagged in the first place,
and the **binding** of a copy has to record that the name received a list:

```
xs = [1.5, "a", [7]]
print(xs[0:2])           # [1.5, 'a']
ys = xs[0:2]
print(ys)                # [1.5, 'a']      — and len(ys) is 2, `for v in ys` walks it, ys[0] is 1.5
print(not xs[0:0])       # True            — an empty copy is false, a full one is not
print(2 in xs[0:2])      # False
print(xs[0:2] == [1.5, "a"])   # True

def build():
    return [1.5, "a", [7]]
print(build()[0:2])      # [1.5, 'a']      — the answer is sliced by the door that slices a variable (ADR 0334)
print(len(build()[0:2])) # 2               — Gap R.33's slice half
```

A list whose slots are all one kind keeps the plain copy and the plain printer; asserting both halves is the
point, because a slice road that tags every copy sends a plain `[1, 2, 3]` to a tag array nobody wrote.

`len(...)` of a slice answers the count — of a literal's slice, of a variable's, of a call's answer, and of a
name bound to a copy.

**A bound is a position or an absence, never a value of another kind** (**ADR 0336**). An integer — a `bool`
included, because `True` is `1` — is a position; `None` means the same thing an omitted bound means; anything else
the compiler can name the kind of raises the reference's
`TypeError: slice indices must be integers or None or have an __index__ method`, catchable by the program's own
`except`:

```
xs = [1, 2, 3]
print(xs[None:None])   # [1, 2]   — the same slice as xs[:], because None is an absent bound, not a 0
print(xs[::None])      # [1, 2]
print(xs[True:3])      # [2, 3]   — a bool is an int
print(xs[-9:9])        # [1, 2, 3] — an out-of-range bound clamps, it does not raise
s = "abcdef"
print(s[None:2])       # ab       — the same rule over the text road

# and these raise, where the toolchain once answered a plausible list:
#   xs["a":"b"]        TypeError: slice indices must be integers or None or have an __index__ method
#   xs[1.5:2]          the same
#   xs[[0]:2]          the same (this one used to reach `llc` as a global in an i32 argument: exit 2)
```

**A step is asked what it holds, and zero is the program's `ValueError`** (**ADR 0337**). `0` is a perfectly good
integer, so it passes the kind gate above — and `rt_slice` walks its own loop forever on it, which used to end with
the run killed and **exit 2**, the code ADR 0166 reserves for a compiler bug, reported for a program whose only fault
was writing `xs[::0]`. The reference answers `ValueError: slice step cannot be zero`, and so does this backend, on
the road that must be caught:

```
xs = [1, 2, 3]
print(xs[::2])         # [1, 3]   — a stride answers; nothing is asked of it at run time
print(xs[::-1])        # [3, 2, 1]
k = 2
print(xs[::k])         # [1, 3]   — a stride the compiler cannot read is guarded beside the copy, and strides

# and these raise, catchable by the program's own `except ValueError:`:
#   xs[::0], xs[0:2:0], xs[::-0]        ValueError: slice step cannot be zero
#   k = 0 with xs[::k]                  the same (the guard is where the value is)
#   s[::0] over s = "abcd"              the same — over a text as well
```

A stride the compiler can read as anything but zero asks nothing at all: no compare, no raise block, no interned
message in the module. What the door still declines, and why, is filed rather than glossed:

- **the copy's own slots, read directly** — `print(xs[0:2][0])` is `1.5` in the reference and
  `index requires an inline list/dict/set literal` here, while the bound form (`ys = xs[0:2]`, `print(ys[0])`)
  answers: **Gap R.235**;
- **a base whose slots are reachable only through another object** — `print(m()["k"][0:1])` where `m()` returns
  `{"k": [1.5, "a"]}`, and `print([x for x in [1, 2, 3]][0:2])`: the gate in front of the copy refuses with a
  sentence naming the missing record instead of copying whatever word the base lowered to (**Gap R.235**; the
  comprehension shape reached `llc` and had the module rejected before ADR 0335 — exit 2);
- **a base that is not a list at all** — `print({1, 2}[0:1])` and `print(d[0:1])` over a dict refuse here, where
  the reference raises `TypeError: 'set' object is not subscriptable` for the set and `KeyError: slice(0, 1, None)`
  for the dict (a slice of integers is hashable, so the dict hashes it and names the key it missed):
  **Gap R.234**; both were exit 2 or a fabricated `[1]` before the gate;
- **a stepped slice of a text whose step is a value** — `k = 2` with `print(s[::k])` is `ac` in the reference and
  refuses with `cannot fold string slice`, while the same stride written as a literal answers (`s[::2]`, `s[0:4:2]`,
  `s[::-1]`) and the *unstepped* road with value bounds answers too (`s[i:i+2]`, ADR 0229): `rt_str_slice` takes a
  low and a high and **no stride**, so only the fold can honour a step and a fold needs a constant — **Gap R.243**
  (it is the one slice shape in this family that cannot hang: there is no loop here to walk forever);
- **a bound whose kind is a tag, not a record** — `ys = ["a"]` with `print(xs[ys[0]:2])`, or a call handing back a
  text, answer `[1, 2]` at exit 0 where the reference raises the `TypeError` above: **Gap R.237**, the same missing
  question one layer in, where the kind travels with the payload and the gate asks only where a record names it;
- **a comprehension over a copy** — `print([x * 2 for x in xs[0:2]])` is `[2, 4]` in the reference and refuses
  here with a sentence about texts, while the same copy *named* (`zs = xs[0:2]`) iterates and answers: **Gap R.239**,
  because the iterable door asks none of what the binding door records;
- **the length of a slice of a text** — `print(len(s[1:3]))` over `s = "abcd"` is `2` in the reference and refuses
  with `len requires an inline list/dict/set literal`, although `len(s)` answers `4`, `len(xs[0:2])` of a list copy
  answers `2`, and the fold that prints `s[1:3]` holds the characters it would count: **Gap R.240**;
- **a slice handed back by a function** — `def n(): return xs[1:3]` with `print(n())` answers `1` (and `3` over a
  mixed base, the copy's handle) where the reference prints `[2, 3]`, and `for v in n():` prints a single `0`:
  **Gap R.238**, because the scan that decides whether a function hands back a container has no arm for a
  `return` that is a slice — ADR 0333's decision one construct further out. `ys = n()` prints the list correctly,
  which is what makes it a missing record rather than a missing object.

### An ordering between kinds that do not order

`<`, `<=`, `>` and `>=` are defined between two numbers, between two texts, and between two containers of the same
kind. Everywhere else the reference refuses, and so does the compiled leg, with its own sentence:

```
print(1 < None)        # TypeError: '<' not supported between instances of 'int' and 'NoneType'
print(1.5 < "a")       # TypeError: '<' not supported between instances of 'float' and 'str'
print(None <= None)    # TypeError: '<=' not supported between instances of 'NoneType' and 'NoneType'
print(True >= "a")     # TypeError: '>=' not supported between instances of 'bool' and 'str'
print(True < [1])      # TypeError: '<' not supported between instances of 'bool' and 'list'
```

The void orders against **nothing**, including itself — `None <= None` is a raise, not a verdict. The raise is a real
trap: `except TypeError:` catches it, `if 1 < None:` takes the same raise as `print(1 < None)` does, and printing the
exception prints what `python3` prints. The two type names are named in source order, the way CPython names the operand
whose `__lt__` was tried first, and a verdict, the void and a text are named for what they are — `'bool'`, `'NoneType'`,
`'str'` — not as the number their representation happens to be (`roadmap Gap R.85` and `Gap R.202`, closed by
**ADR 0382**).

The question is asked of the **operands**, before the lowering turns the void into the word `0` and a text into an index
into `@str_tab`, because afterwards there is nothing left to ask. It is asked on both roads a comparison travels down:
the value road, and the road an `if`/`while`/ternary condition takes.

Two things the door deliberately does not claim:

- Two **containers** — `[1] < [2]`, `{1} < {2}`, `(1,) < (2,)` — are the container-order door's beside it, which
  answers the pair it can build and files the rest (`roadmap Gaps R.201`, `R.86`). A mixed pair like `[1] < {1}` is a
  raise either way, and says so.
- A side whose kind is a **run-time fact** — a parameter, a slot the module tagged, a call whose branches answer two
  kinds — leaves the door silent rather than guessed at. `x = None` / `y = 1` / `print(x < y)` therefore still answers
  `True`, because an int is what a name holds when nothing was recorded about it and the door raises only where the
  program states the pair (`roadmap Gap R.354`).

### A container in a numeric operand

A list, dict, set or tuple has no number to do arithmetic **with**, so it never reaches an LLVM arithmetic
instruction (`Gap R.175`, ADR 0292). What happens instead is decided by the reference, line by line:

```
print([1] - [2])    # TypeError: unsupported operand type(s) for -: 'list' and 'list'
print([1] / 2)      # TypeError: unsupported operand type(s) for /: 'list' and 'int'
print([1] + {})     # TypeError: can only concatenate list (not "dict") to list
print({} + [])      # TypeError: unsupported operand type(s) for +: 'dict' and 'list'
print([] < {})      # TypeError: '<' not supported between instances of 'list' and 'dict'
```

Every one of those is a real trap on the compiled leg and a real trap in the reference: `except TypeError:`
catches it, and the message is
CPython's own words, so printing the exception prints what `python3` prints. Note that `+` follows the
**left** operand's type — a list defines concatenation and refuses the other type, a dict does not define
concatenation at all — so rows three and four are not mirrors of each other.

The sequence operations themselves are **built** (roadmap Gap R.33, closed by ADR 0365): the answer is a heap
object the program never wrote, with a tag beside every slot, and the object prints, compares, measures,
iterates and tests membership like the one a literal made.

```
print([0] * 3)        # [0, 0, 0]      print([1, 2] + [3])   # [1, 2, 3]
print(3 * [0])        # [0, 0, 0]      print("ab" * 2)       # abab
print((1, 2) + (3,))  # (1, 2, 3)      print(str([1] + [2])) # [1, 2]
```

Three shapes are still owed, and each refuses in words naming the row (Gap R.314) rather than guessing — in a
value position *and* in a condition, because the value door and the condition door ask one predicate
(ADR 0232): `%t1 = icmp slt i32 @.lst1, @.lst2` is not a module, and exit 2 is not the answer to a program
the reference runs.

```
a = [1, 2]
print(a < [2])        # True — the order of two CONTAINERS the program named: the tagged order road
if [1, 2] < [2]: …    # refuses in words: neither side is a name, so no name holds the pair (Gap R.314)
print(([1] + [2])[1]) # 2 in the reference — the built subscript is Gap R.314's
print([1] * 300)      # 300 slots, past the 256 one object owns: Gap R.314 / Gap R.271
```

These are owed to L11.1's tagged value word and are pinned as still-owed. Two things are deliberately
**not** included, because their kind belongs to someone else and each was measured breaking an answer:

- a **container passed as an argument** — `def half(xs): return xs[0] / 2` with `half([1.5])` = `0.75`;
  a container is a legal argument, only an arithmetic *operand* is not;
- a **comparison over two names** of the same kind — `a = [1]` / `b = [2]` / `print(a < b)` = `True`, the
  tagged order road's question.

A **text** repeated by an int (`"ab" * 2` = `abab`) is answered on both witness legs: the repeat is built by the
same door and answers an `@str_tab` index, which is the word every text road already reads (ADR 0224; the
refusal this row retired — Gap R.82's — is gone).

One shape is answered wrongly rather than refused, and is written here rather than hidden: a **method that takes
a parameter and returns a text its body built** prints the intern table's position under the text —
`def fmt(self, s): return "x%s" % s` prints `x0` where the reference prints `xhi`, and
`def rep(self, s): return s * 2` prints `0` where it prints `hihi`, both at exit 0 (**Gap R.315**, measured while
widening the one predicate that decides what a text is). The neighbouring positions are right — module level, a
plain function, a method with no parameter, a method returning a text literal — and the probe that keeps the
wrong lines on the record is `integration/programs/probe_method_text_answer.gy`. Until that row is paid, spell
such a body as a **function**, or build the text in the caller and pass it in.

### Text predicates

`s.startswith(p)`, `s.endswith(p)`, `s.isdigit()`, `s.isalpha()`, `s.isalnum()`, `s.isspace()`,
`s.islower()` and `s.isupper()` answer a **verdict**, so they print `True` / `False` — not the `1` / `0`
they are stored as (ADR 0257's rule; `Gap R.172`, ADR 0289). A verdict is still a number where a number is
asked: `"1".isdigit() + 1` is `2`, and `[True] == [1]` is `True`.

A predicate works on a text the compiler can see, including one bound to a name
(`s = "abc"` / `print(s.isalpha())`). A **parameter** as the receiver is refused by the compiled leg — it
has no compile-time text to fold — while the record answers it normally; the row stays open with
L11.1's tagged value word.

A class the program defines may use one of these eight names for its own attribute; what its method returns
is then what prints, exactly as with an overloaded comparison.

### Comparison chains

`a < b < c` is one construct, not two nested comparisons: it asks `a < b` **and** `b < c` and reads the
middle operand **once**. So does `1 <= x < 10`, `a == b != c`, `1 < 2 < 3 < 4`, and a chain may mix the
operators (`2 == 2 <= 3`, `1 is 1 < 2`). The answer is a verdict, so `print(1 < 2 < 3)` prints `True`.

A chain's operands may be anything a comparison accepts, including something computed: with
`print(1 < g() < 10)` the reference calls `g` exactly once, and both legs match it. A chain whose middle
operand is a **container** answers on the record and refuses on the compiled leg, where a container is a
global rather than a word — `Gap R.53` keeps that half open until L11.1's tagged value word lands.

An operand whose kind the **object** carries is not a refusal either (ADR 0423, paying `Gap R.442`). The chain
asks each operand what it carries before lowering anything, and binds its one evaluation slot with the answer
— a `(payload, tag)` pair for a slot the container's own census describes, a `double` for a floating value:

```
xs = [1, 2]
xs[0] = 2.5
print(0 < xs[0] < 3)      # True    — and False for 5.5, not for the number the store never wrote
d = {"a": 2.5}
print(0 < d["a"] < 3)     # True    — the entry's tag decides, not the handle the entry is stored in
d = {"a": "x"}
print(0 < d["a"] < 3)     # TypeError: '<' not supported between instances of 'int' and 'str'
print(0 < abs(-2.5) < 3)  # True    — this reached `llc` as store i32 over a double, which is exit 2
```

The links themselves are the ordinary comparison roads, so `0 < v < 3` over a text raises the reference's
sentence at the trap's exit code rather than answering `False`. What is still owed at this position is the
**loop item**: `for v in [None, 2.5]:` `print(0 < v < 3)` answers `False` `False`, because the loop keeps its
per-item tag in a record this gate does not read yet (`Gap R.443`).

`a < b and b < c` is **not** a chain: it is two comparisons joined by `and`, and `and` short-circuits. Before
ADR 0288 the grammar parsed chains left-associatively as `(a < b) < c`, which compares an int against a
boolean — a question this front end answers rather than refusing, so `print(1 > 2 < 3)` printed `True` where
Python prints `False`.

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
  negative value run the body zero times, exactly like `range(0)`. The compiled backend have always agreed on
  this, which is what makes it a feature rather than a bug — but CPython refuses it
  (`'int' object is not iterable`), so it is a declared extension with its own conformance row rather
  than something an agent should discover by accident (ADR 0207, roadmap R.14). It compiles to the same
  counter loop as `range(n)` and is handy precisely where `range` has been claimed by the program itself
  (ADR 0205). In a comprehension the integer form works on the record
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
  instance). Class support is implemented in the compiled backend (the REPL and `--eval` compile too, ADR 0302).
- **Dynamic dispatch**: a method call on an instance whose class is not statically
  known (e.g. an instance returned by a function or passed as a parameter) resolves
  the method by the runtime class of the receiver. the record dispatches on the
  actual instance's class; the AOT/codegen path supports statement-level dynamic
  dispatch on instances and direct method calls.
- **Operator overloading (dunder dispatch)**: binary operators dispatch to
  dunder methods on class instances. For `a OP b`, the record calls
  `__add__`/`__sub__`/`__mul__`/`__truediv__`/`__floordiv__`/`__mod__`/`__pow__`
  on `a` when `a` is an instance, falling back to the reflected method
  (`__radd__`, `__rsub__`, `__rmul__`, `__rtruediv__`, `__rfloordiv__`,
  `__rmod__`, `__rpow__`) on `b`. Comparisons dispatch to `__eq__`/`__ne__`/
  `__lt__`/`__le__`/`__gt__`/`__ge__` (with the swapped comparison as the
  reflected fallback). This is currently an interpreter-only feature in the
  AOT/codegen path; see ADR 0139.
- `raise ValueError("msg")` raises a typed exception carrying a class name and an
  optional message. Built-in exception classes: `Exception`, `ValueError`, `TypeError`,
  `KeyError`, `IndexError`, `RuntimeError`, `StopIteration`, `ZeroDivisionError`,
  `UnboundLocalError` (ADR 0228) and `OverflowError` (ADR 0264 — the class CPython uses for
  `int(inf)`, which is what makes `except OverflowError:` around a `floor`/`ceil` of a huge double mean
  the same thing on both legs). `MemoryError` (ADR 0347) is the compiled arena saying no: the
  backend's heap is one arena that **grows** (ADR 0350) from `lang.HeapInitialCapacity` (1024) containers to
  the reservation `lang.HeapCapacity` (8192), and the container-taking door that finds even the reservation
  full raises this class rather than handing the program a slot number that does not exist — see
  § The compiled arena. A bare
  `raise` raises `Exception`. `raise IndexError` (the class itself, no call) raises that
  class with no message, and a class derived from an exception is raisable too:
  `class MyError(Exception):` then `raise MyError("x")` / `except MyError:`.
- **Runtime errors are typed exceptions, so they are catchable.** A list index out of
  range (read *or* write) raises `IndexError`, a missing dict key raises `KeyError`, and
  assigning to a string index or set element raises `TypeError` — on the compiled backend:

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
      print("caught")          # the compiled backend, and CPython
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
  | `[][0]`, `[1,2][5]`, `[1,2][-3]` | `IndexError: index out of range` |
  | `"abc"[7]`, `"abc"[-4]` | `IndexError: string index out of range` |
  | `{1: 2}[3]` | `KeyError: 3` |
  | `float(None)`, `float([1,2])`, `float({"a":1})` | `TypeError: float() argument must be a string or a real number, not 'NoneType'` / `'list'` / `'dict'` |
  | `int([1])` | `TypeError: int() argument must be a string, a bytes-like object or a real number, not 'list'` |
  | `ord("")`, `ord("ab")` | `TypeError: ord() expected a character, but string of length 0 found` / `… length 2 found` |
  | `chr(-1)`, `chr(1114112)` | `ValueError: chr() arg not in range(0x110000)` |

  **A trap is decided where the value is reached, not where it is written** (ADR 0326). A read the compiler
  can fold — a literal list, a literal text, a literal dict, a comprehension's object, a method body that
  returns one — raises the same class from the same table as a read it cannot see into, so `except
  IndexError:` and `except ValueError:` work whichever road the read took. A literal that *does* spell a
  number answers rather than trapping, including the spellings a naive parser drops: `int(" 12 ")` is `12`,
  `int("1_0")` is `10`, `int(True)` is `1`, `float("1_0.5")` is `10.5`, and `int()` and `float()` are `0`
  and `0.0`. The one conversion still refused is a literal too wide for the word — `int("99999999999999999999999999")`
  — where the reference has an unbounded `int` and this language has a 64-bit slot (roadmap L12.12 is the
  decision that row is waiting for).

  **A dict literal is a dict** (ADR 0327). `{"a": 1}["a"]` is `1`, `{1: 1.5}[1]` is `1.5`, `{1: None}[1]` is
  `None`, `{1: "a"}[True]` is `a` (the reference's key identity: `1`, `1.0` and `True` are one key, and a text
  never is), `{"a": [1, 2]}["a"]` is `[1, 2]`, and a key the literal does not hold raises the `KeyError` the
  program can catch — `{"a": 1}["b"]`, `{"a": 1}[1]`, `{1: 2}["a"]` all die at exit 3 with the key's own repr
  as the message, exactly as the same read through a variable does. The read goes to the object the same door
  a dict *variable*'s read uses (`rt_dict_has_tagged` / `rt_dict_get_tagged` / `rt_dict_value_tag` on a dict
  built by the binding road's builder), so the answer carries its kind instead of having one guessed for it;
  the compile-time lookup is kept for the one shape it can read — an integer key into a literal whose keys and
  values are all integers — and a repeated key in a literal keeps the value the *last* entry gave it
  (`{1: 2, 1: 3}[1]` is `3`). Two positions still refuse, because they keep one word for a whole value and the
  answer's kind lives in the tag beside the payload: handing such an entry back from a `return`, or to a call,
  is `Gap R.146`'s to pay and says so (roadmap Gap R.215).

  **A list literal is a list** (ADR 0331), and the same decision taken one container over.
  `["a", "b"][0]` is `a`, `[1.5, 2][0]` is `1.5`, `[None, 2][0]` is `None`, `[True, 2][0]` is `True`,
  `["a", "b"][-1]` is `b`, `[[1, 2], [3, 4]][0][1]` is `2`, `[True, False][False]` is `True` (a bool is an
  int, so it is a legal position), and a position the literal does not hold raises the catchable
  `IndexError: list index out of range`. Before this the four non-integer elements printed their payload —
  `0`, `1`, `0`, `1` — while the *same element read off a variable* answered correctly, because the pair door
  had a dict arm and no list one. The read now goes to the object through the same call a list variable's read
  emits (`rt_get_elem` beside `rt_tag_of`, on a list built by the binding road's builder), the compile-time
  lookup is kept for the one shape it can read — an integer position into a literal whose elements are all
  integers — and a condition asks the same pair: `if [0.0, 1][0]:`, `if [[]][0]:` and `if [""][0]:` are all
  false in the reference and all were non-zero words, so truth comes from `@rt_pair_truth` rather than from a
  payload compared to zero. The one-word positions — a `return`, a call argument, an operand of arithmetic —
  still refuse rather than print a handle (roadmap Gap R.218, and Gap R.146 for the position).

  Several are refused at compile time by the AOT backend rather than trapping (an honest refusal,
  class 1: `len requires an inline list/dict/set literal`, `index of a non-literal variable` for a
  subscript into a **set** — the raise that shape owes is `TypeError: 'set' object is not subscriptable`,
  roadmap Gap R.217 — and
  `reading a dict literal by a key the compiler cannot type` for a tuple key, the same residue as
  Gap R.212; messages that still need stable codes, roadmap L11.8). A subscript on a **call's container
  answer** is no longer one of them (ADR 0334, roadmap Gap R.214): `def build(): return [1, 2]` /
  `print(build()[0])` prints `1` and `print(build()[7])` raises the catchable
  `IndexError: list index out of range`, because the subscript asks its base one question — *is this a call
  that hands back a container?* — and, when it is, is answered by the door that reads `ys[i]` for
  `ys = build()` — and the **slice** beside the index, so `print(build()[0:2])` prints the copy the same door
  reads off a variable (**ADR 0335**, roadmap Gap R.230). What those doors still decline is filed where it
  belongs: a **slot of a copy** read directly (`print(xs[0:2][0])`, while the bound copy answers — Gap R.235), a
  slice whose base is a slot or a comprehension's answer (Gap R.235), a slice of a set or dict, which the
  reference raises rather than declines (Gap R.234), text bounds, which this backend answers instead of raising
  (Gap R.233), an answer as an item-assignment target or as a method receiver (Gap R.229), and a membership test
  whose needle is a slot (Gap R.232). The missing
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

  The report names **every frame the raise travelled through**, outermost first, in the shape the
  reference prints — which ADR 0302 makes the definition (Gap K.8, ADR 0354). The compiled program
  keeps a shadow frame stack of its own, the raise copies it, and the runtime prints the copy:

  ```console
  $ gusty prog.gy; echo $?
  Traceback (most recent call last):
    File "prog", line 7, in <module>
    File "prog", line 5, in outer
    File "prog", line 2, in inner
  ZeroDivisionError: integer division or modulo by zero
  3
  ```

  Each frame is the function **as the program wrote it**, which is not the symbol the module holds:
  a method's frame says `in bump` and not `in Counter.bump` (the reference's shape — the qualified
  name is what `--debug-info` reports, for a debugger), a lifted closure's frame says `inner` and
  not `inner_impl`, and the program's own top level is `<module>`. The line beside a frame is the
  line that frame was executing: the line the call it is waiting on is written on, and the raise's
  own line in the innermost frame.

  Two limits, both stated rather than discovered by a reader who needed more:

  - the stack is a **window of 64 frames**. A recursion deeper than that reports its newest 64 —
    the frames a traceback is read for — and the module frame falls out of the top of the window;
  - a run of identical frames is printed **three times and then counted**, in the reference's own
    words: `[Previous line repeated 61 more times]`. A recursion is one fact, and a report that
    states it 200 times is not more true for that.

  A program that can never report a trap pays for none of this: the instrumentation is laid over a
  module only when something in it can raise, and every other module ships byte for byte as it
  did before.
- One deliberate divergence: an assignment whose *target kind* is known statically to be
  impossible (`s[0] = "z"` on a string, `s[0] = 1` on a set) is a compile-time diagnostic
  in the AOT backend (ADR 0166) and a catchable `TypeError` on the record.
- **The arms of a `try` are tried in the order they are written**, on the compiled backend
  (ADR 0213). `except ValueError:` catches exactly that class; `except Exception:` and a bare
  `except:` catch anything, wherever they appear in the list — an arm after them is unreachable,
  as in Python. An exception no arm matches is **not** dropped: it propagates outward — to an
  enclosing `try`, or out of the function — and if nothing handles it, it is reported and the
  program fails with the runtime class (exit 3). on the record it surfaces to the caller as
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
- **An arm can name what it caught**: `except <Type> as name:` parses, the arm binds `name`, and the name holds
  what the exception **writes** — the message the raise carried (ADR 0406, roadmap Gap R.113). It is interned by the
  same door every other text goes through, so the four ways of reading it are one fact:

  ```py
  try:
      1/0
  except ZeroDivisionError as err:
      print(err)              # division by zero
      print(f"got: {err}")    # got: division by zero
      print(str(err))         # division by zero
  ```

  The machine's own raises carry the reference's message too — `xs[9]` binds `list index out of range`, a missing
  key binds `'k'` with the key's own quotes — and `raise ValueError("a: b")` binds `a: b`, colon and all, because
  the raise writes the report string (`"ValueError: a: b"`, what an uncaught trace prints) and the bare message
  into two separate places rather than one being cut out of the other. A raise that carried nothing binds the empty
  text, exactly as `str(ValueError())` is empty there. An arm that never runs binds nothing, and an arm that names
  nothing (`except IndexError:`) is unaffected: it binds no slot and interns nothing.

  What the name is NOT is the exception object. `len(err)`, `err.upper()` and `err == "…"` are **refused** — the
  reference raises `TypeError`/`AttributeError` at those three doors, and answering them with the message wearing
  the object's clothes is a wrong answer, not an answer. `repr(err)`, `err.args`, `type(err)`, the name read after
  the arm (which the reference deletes there) and an explicit `raise KeyError("a: b")`, whose message the reference
  quotes, diverge today and are pinned as owed in `pkg/lang/except_as_name_test.go` under roadmap **Gap R.395**;
  `print(type(err))` cannot even be written, because `type` is a reserved word here (**Gap R.396**), and
  `except (ValueError, TypeError):` — the tuple spelling — does not parse either (**Gap R.397**).

- **Once an arm accepts an exception, the exception is over** (ADR 0218). On both backends, the code
  after the `try` runs as if nothing had happened — the pending state does not survive the arm, in
  any direction the arm leaves by: falling through, `return`, `break` or `continue`. The compiled
  backend keeps that state in one module-wide flag, so an arm that accepted an exception without
  clearing it handed the program a second copy: the next call to a user-defined function found the
  flag still set and reported the exception again, after the handler had already run and printed. ADR 0218 put that
  clear on every **edge out** of the arm — falling through, `return`, `break`, `continue`. One shape is still open,
  and it is the other direction: a call to a user-defined function **inside** the arm's body, whose statements run
  with the flag still set, so the call's own post-call check takes the exception back before any edge-out clear is
  reached. The arm prints, the traceback prints, the statement after the `try` never runs, and the run exits 3 —
  while the same arm with a *builtin* in it (`print("caught")`) exits 0, because a builtin has no such check
  (**Gap R.241**, measured while landing ADR 0336, reproduced with a plain out-of-range subscript):

  ```py
  def h():
      print("caught")

  xs = [1, 2, 3]
  try:
      print(xs[9])
  except IndexError:
      h()                    # the arm runs…
  print("after")             # …CPython prints both lines at exit 0; this backend prints `caught`,
                             # reports the handled IndexError again, and exits 3
  ```

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
  innermost first, on the compiled backend:

  ```py
  def f() -> int:
      try:
          return 1
      finally:
          print("fin")        # fin, then 1 — on the compiled backend, as in CPython
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
  the statement — on the compiled backend:

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

  Reading a name that this run never assigned is a `NameError` on the record, matching
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

  Two rules decide that sentence, and both are the reference's (ADR 0351). *Which* names are state is
  asked per scope: a `def`'s parameters, a body's own assignments, its loop variables and its `with … as`
  names bind the name **inside that body**, so `def g(y): return y` never makes the module's `y` into
  state — a body has to read the name without binding it. And *every* statement that binds such a name
  at module level writes the global, whether it is a plain assignment, an augmented assignment (`y += 1`)
  or a loop variable (`for y in […]`), because a name with two slots is a program that reads a value no
  statement produced:

  ```py
  LATE = 0

  def read() -> int:
      return LATE          # whichever value the module held when read() was called

  def show(LATE):          # a parameter named LATE is show()'s OWN binding…
      return LATE

  LATE = 3
  print(read(), show(9))   # 3 9   — the parameter never became module state

  for LATE in [7, 8]:      # …and a loop's variable IS the module's, so a body sees it
      print(LATE)          # 7, 8
  print(read(), LATE)      # 8 8
  ```

  A module name holding a **float** is state too, and reaches a body through a global of its own
  (`@gy_mod_<name> = global double`) that the top level writes and the body loads when it runs (ADR 0376).
  The gate admits a name only where the module's own bindings are numbers the compiler can see — a float
  literal, a negated one, or arithmetic over literals (`RATE = 7 / 2`, `AREA = 2.0 * 3`, `RATE = RATE * 2`)
  — and nothing else ever binds it. A name whose value comes from a call (`RATE = float(x)`, `RATE = xs[0]`)
  is refused rather than answered with a number nobody wrote, and a name a body also *assigns* stays that
  body's local, exactly as the rule above:

  ```py
  RATE = 2.5

  def scale(v):
      return v * RATE      # 7.5 — the module's own double, read at call time

  def shadow():
      RATE = 1.0           # shadow()'s OWN binding; the module keeps 2.5
      return RATE

  print(scale(3), shadow(), RATE)    # 7.5 1.0 2.5
  ```

  Two positions still refuse the module's float by name, both because the *word* is missing rather than the
  value: a **method** that returns it (a method has an `i32` return word whatever its body computes — read
  it into a local, or print it from the body), and a plain read in a position the compiler kept `i32`. A
  nested `def` returning it is refused the same way. Both are L11.6 · Gap R.3c's missing return word.

  A module name holding a **container** reaches a body too, since **ADR 0407**, for the containers the program
  never writes through: a name whose ONE module binding is a plain assignment of a literal container — every slot
  itself a literal — and which nothing anywhere in the program appends to, assigns a slot of, augments, aliases
  and then writes, hands out of a `return`, or asks an identity question, is handed to each body that reads it as
  a **binding of that literal**. The frame then owns the slot, the GC root and every notebook the container doors
  read by name, so the print, `len`, position, membership, loop, equality, ordering, `str` and interpolation-of-a-
  slot doors all answer it, in a `def` and in a method alike:

  ```py
  xs = [1, 2, 3]
  d = {"a": 1, "b": 2}

  def n() -> int:
      return len(xs)         # 3 — the module's own list, read at call time

  def first():
      return xs[0]           # 1 — a slot the literal says is a number, which travels

  def keys():
      for k in d:            # a, b — the module's dict, walked by its keys
          print(k)

  print(n(), first(), n() + len(d))   # 1 1 5
  print(xs)                           # [1, 2, 3] — the module keeps its own object
  ```

  A copy is honest only while nothing writes through the object, so the write half is refused with a message
  naming module state — and every one of those refusals is a program CPython runs (roadmap **Gap R.398**; the
  answer is the tagged value word, **L11.1**, carrying a rooted container as module state):

  ```py
  xs = [1, 2, 3]

  def show():
      print(xs)

  xs.append(4)      # writing through the module's object: refused, naming module state
  show()
  ```

  Also refused, each with its own row: a body that hands the container (or a slot of it that is itself a
  container) back to its caller, a name the module binds twice, a name any other binder takes (`for`, `with … as`,
  `match`, unpacking, `import`), a slot that is a name or a call (`xs = [one()]`), the identity question
  (`ys is xs`), a nested `def` or a `lambda` reading the name (**Gap R.401** — those two roads take no such
  prologue), and a `return` of a slot the literal does not read as an integer — a text, a float, a verdict or the
  singleton — because the road out of a `return` carries an integer honestly and no other kind (**Gap R.400**;
  it prints the payload today, at the exit code of success, for a list written inside the body too). A module-level
  **text** read by a body prints the intern position today (`Gap R.343`).

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
  `try: … except UnboundLocalError:` catches them on the record and on the compiled backend — and
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
- The compiled backend lower class patterns (ADR 0235). The compiled one asks the instance
  whether it has each attribute — `@inst_set`, written by every attribute store and
  cleared when a heap slot becomes a new instance — because its data words cannot
  tell an attribute that was never written from a stored `0`. ADR 0149 said this was
  interpreter-only; it was, and `case Point(a, b):` on an instance with `x` and `y`
  answered `pt 0 0` compiled against the record's `no`.
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
- `and` / `or` are the two operators that hand back an **operand**: the test picks one of them and that
  one answers, in its own representation — `print(2 and 3)` is `3`, `print("" or "d")` is `d`
  (see *Truthiness*, ADR 0269). They are not a `0`/`1` verdict; only a *condition* that uses them asks for
  a verdict, and that road composes the two operands' truths.

  **`//` and `%` floor; they are one rule, not two operators.** Go's `/` and `%` truncate
  toward zero, so with mixed signs the answers differ: Python's `-7 // 2` is `-4` and `-7 % 2`
  is `1`, because the remainder carries the *divisor's* sign. The two are only correct together
  — the invariant is `a == (a // b) * b + (a % b)`, which a truncating pair also satisfies, so
  testing each against its own table can certify a wrong pair. The compiled backend now emit the
  correction (`floorDiv`/`floorMod` on the record, an `sdiv`/`srem` plus a `select`
  adjustment in the module, `frem` plus `fadd` for floats), constant folding uses the same pair,
  and the whole sign grid is checked against CPython on the compiled backend (roadmap Gaps R.28, R.30,
  ADR 0216). An exact float remainder keeps the divisor's sign — `7.5 % -0.5` prints `-0.0`.

  **`/=` is true division, so the operator — not the operands — decides the domain** (roadmap Gap P.1,
  ADR 0274). `x = 8` then `x /= 2` leaves `x` holding `4.0`, and `x = 1` then `x /= 3` leaves
  `0.3333333333333333`; `//=` and `%=` keep answering an `int` for two `int`s, because those operators do.
  The same is true of a double handed to the variable by any other assignment: the reference keeps a value
  and its kind together, so a variable's *newest* binding is what says whether it is an int or a float,
  and this backend now follows it across the change of state:

  ```python
  y = 12345
  x = 8
  x = 2.5
  print(x, y)      # 2.5 12345 — the neighbour keeps the number the program wrote

  a = 7
  a /= 2
  print(a)         # 3.5
  print(a + 1)     # 4.5   an ordering, a condition, str(a) and an f-string field ask the same tag
  ```

  What carries the change of mind is the `(payload, tag)` pair the rest of the family already uses: the
  double goes into a float box, the name is bound to the pair with the float's tag, and every later read
  asks the tag — the print dispatch, an operation, a comparison, a condition, `str`, an f-string field and
  a `while` head (ADR 0166, ADR 0265, ADR 0267). Storing the double into the four-byte slot the variable's
  first binding chose was the shape before this: it verified, `llc` accepted it, and the variable beside it
  answered a different number (`y` printed `1074003968`, exit 0), which is what roadmap **Gap R.155** files
  and why the pair — not a wider store — is the cure. A variable that never changes state is untouched: it
  keeps the plain slot and the plain load it had.

  What still refuses is the neighbour that needs the state to travel further: returning it from a function
  (`Gap R.156`), changing it twice in the same variable — the `t += i / 2` accumulator (`Gap R.157`),
  handing it to a function (`Gap R.158`), using it as a container element (`Gap R.159`), or ordering it
  against a float literal (`Gap R.160`). Each says which position keeps one word, and none of them answers
  a number the program did not write.
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
  `0 == -0.0` and `3 == 3.0` are all True, in either operand order, on the compiled backend. An `int`
  compared with a `float` is one question about two values, not a comparison of representations;
  the interpreter used to answer `1 == 1.0` with False while answering `1.0 == 1` with True.
  This coercion reaches only numbers: `1 == [1]` and `1.0 == "a"` are False, not errors.
- **A zero carries the sign the program gave it, and the sign-removing operations remove it** (ADR 0349,
  closing roadmap Gap R.132): `print(-0.0)` writes `-0.0`, and so does `x = -0.0` / `print(x)`, a container
  slot, `str(-0.0)`, a loop binding and a function's parameter — on the compiled backend, where a folded
  constant used to arrive as `+0.0` because the emitter materialised it by adding zero to it and IEEE answers
  −0.0 + +0.0 with +0.0. Meanwhile `abs(-0.0)`, `-(-0.0)` and `abs(n)` over a slot the run time owns all write
  `0.0`, exactly as CPython does: the sign is one value's property, stated by its IEEE bits on the way out of
  the compiler and cleared bit-by-bit by the runtime's signless `abs` — not asked about with a comparison
  against zero, which is the question that is false for `-0.0`.
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

### Unary minus asks its operand's kind (`-x`)

`-` is an operator, and an operator is a question about a **kind**. A value that has no sign stops the
program with the reference's own sentence — on the compiled backend, catchably, at the runtime-error exit class
(roadmap Gaps R.89, R.137, ADR 0266):

```python
print(-7)          # -7        — an int negates
print(-1.5)        # -1.5      — a float negates
print(-True)       # -1        — a bool is a number; the answer is an int
xs = [3, 4]
print(-xs[1])      # -4        — a slot read is a number if the slot holds one

print(-"hi")       # TypeError: bad operand type for unary -: 'str'
print(-None)       # TypeError: bad operand type for unary -: 'NoneType'
print(-[1, 2])     # TypeError: bad operand type for unary -: 'list'
print(-{"a": 1})   # TypeError: bad operand type for unary -: 'dict'
print(-{1, 2})     # TypeError: bad operand type for unary -: 'set'
print(-Token())    # TypeError: bad operand type for unary -: 'Token'   — an instance names its class
```

The kind inside the quotes is the operand's real kind, read from the same table the printer, the equality
and the binary operators read, and the raise is a language event: `try: print(-"hi")
except TypeError: …` runs the arm on both legs. The name is not a decoration — the reference writes a
*different* sentence for a negation than for a binary minus, so `-x` is its own door rather than `0 - x`
in a mask (ADR 0265 owns the sentence table).

Two rules keep the door honest rather than merely loud:

- **The constant folders do not answer it.** `-None`, `-[1]` and friends are not folded to a number, so a
  shape the reference stops on is never printed (Gap R.37's rule, extended to the last operator that lacked
  it).
- **A slot is only raised on when the program can be shown to hold no number there.** `xs = ["a"]` /
  `-xs[i]` raises `'str'` on both legs, because the literal says every slot is a text and the program
  never stores anything else into `xs`. A container that can also hold a number is left to the tag door
  (ADR 0265) or to the refusal that names the missing half — raising there would answer `TypeError` for
  `xs = [1, "a"]` / `-xs[0]`, which the reference answers `-1`.

What the compiler cannot see it does not claim: a parameter the caller fills with a text on one path and a
number on another keeps the road it had, which is the tagged-value-word clause of roadmap L11.1.

### `abs` asks the same question (`abs(x)`)

The absolute value is the other operation that cannot be written without knowing what arrived: a sign is a
property of a number, and the reference stops on everything else. One predicate answers the kind for both
operators — `-{1}` and `abs({1})` read the same slot through the same door, so the two sentences can never
disagree about what the operand is (roadmap Gap R.140, ADR 0271):

```python
print(abs(-5))        # 5         — an int
print(abs(-2.5))      # 2.5       — a float keeps its kind
print(abs(True))      # 1         — a verdict is a number
print(abs(3 - 10))    # 7         — whatever the expression answers
print(abs("hi"))      # TypeError: bad operand type for abs(): 'str'
print(abs(None))      # TypeError: bad operand type for abs(): 'NoneType'
print(abs([1, 2]))    # TypeError: bad operand type for abs(): 'list'
print(abs({"a": 1}))  # TypeError: bad operand type for abs(): 'dict'
print(abs({1, 2}))    # TypeError: bad operand type for abs(): 'set'
print(abs(Token()))   # TypeError: bad operand type for abs(): 'Token'  — an instance names its class

# A self-describing dict entry brings its kind to the signless call, not just to print.
d = {None: -3.5}
print(abs(d[None]))   # 3.5 — ADR 0387 pays the read half of this door
```

Three things the first draft of this door had to get right:

- **The wording is the reference's own, and it is not the negation's.** CPython writes
  `bad operand type for abs(): 'str'` — the *call* named, with nothing between the colon and the operand —
  where the same value under `-` earns `bad operand type for unary -: 'str'`. One sentence table owns both
  (ADR 0265), with `abs` spelled as its own operation rather than borrowed from `unary -`.
- **A raise, not a refusal.** A program the reference stops on must reach the runtime error exit class, so
  `try: print(abs("hi")) except TypeError:` runs the handler on both legs. A front-end refusal would be
  a compile-time verdict that escapes the arm (ADR 0166's exit-code contract).
- **Both roads lower the call.** The `i32` road and the `double` road each reach `abs`; the second one
  produces the operand through the float lift, which yields nothing for a text, so `print(abs("hi") * 2.5)`
  had been answering `0.0` at exit 0. Each road asks the door and falls back to the raise the other one uses.

What a *name* in the operand holds is the latest binding's answer, not the first one's: `x = "text"` then
`x = [1, 2]` retires the earlier kind, so `abs(x)` raises `'list'` and `print(x)` prints the container
(roadmap Gap R.145, ADR 0270).

A slot whose kind only the run time can describe also asks the door, because the kind it needs is the tag the
object carries: a name bound from a container the program built is two words — a payload and a tag — and
`abs` takes the magnitude of the **lifted** payload inside the same arithmetic helper the operators use, so
the answer's kind follows the operand's (roadmap L11.1, Gap R.146; ADR 0309):

```python
xs = []
xs.append(7)
n = xs[0]
print(abs(n))            # 7          — the pair-bound name, the shape this door exists for
print(abs(n - 1))        # 6          — the arithmetic road (ADR 0304)
y = abs(n)
print(y)                 # 7          — the answer is itself a pair, so it prints right
neg = []
neg.append(-2.5)
print(abs(neg[0]))       # 2.5        — a float slot keeps its kind
flag = []
flag.append(True)
print(abs(flag[0]))      # 1          — a verdict is the number it is
txt = []
txt.append("a")
print(abs(txt[0]))       # TypeError: bad operand type for abs(): 'str'  — the call's own sentence
d = {None: -3.5}
print(abs(d[None]))      # 3.5    — the dict entry's tag travels with its handle (ADR 0387)
```

What is **not** answered stays a refusal that names the missing half, and the boundary is deliberate: the
positions that keep one word for a whole value — `abs(n) + 1`, `abs(n) * 2`, `round(abs(n) / 2)`,
`abs(-n)`, and `abs(n) + abs(m)` — still refuse at exit 1. Taking one word out of a pair is not a partial
answer, it is a different
value: a float slot's payload is a box handle and a text slot's is an interned index, and a magnitude of
either is a plausible number at the exit code of success.

## The fold builtins over a pair — `sum`, `min`, `max`

The three folds take a sequence and answer one value out of it, so they are the builtins that most often get
handed the shape this row exists for: an element read out of a container the program built. `min(n, 3)` asks
for the smaller of a slot and a literal; `sum([n])` adds a slot into a literal list. The argument position is
where the pair is read (roadmap L11.1, Gap R.146's fold clause; ADR 0316):

```python
xs = []
xs.append(4)
n = xs[0]
print(min(n, 3))          # 3      — the slot's 4 lost to the literal's 3
print(max(n, 3))          # 4
ys = []
ys.append(True)
b = ys[0]
print(min(b, 3))          # True   — the verdict wins, and prints as the verdict it is
print(sum([n, 1]))        # 5      — a fold over a literal list, one element of which is a pair
m = min(n, 3)
print(m)                  # 3      — the answer is itself pair-bound, so it prints right
print(min(n, "b"))        # TypeError: '<' not supported between instances of 'str' and 'int'
d = {None: 3.5}
print(max(d[None], 2))   # 3.5    — the selected dict entry carries the tag the fold returns (ADR 0387)
```

Three things make that work, and each is why something nearby still refuses:

* The **argument** the fold receives is a `(payload, tag)` pair, and the tag comes from the element path the
  name was bound from — not from the expression the argument *looks* like. Asking the print helper or the pair
  accessors about the argument instead of the fold's answer is what used to make `min(n, 3)` answer the
  container's length (`min([n], 3)` → `1`) and `print(min(n, 3))` print `3` whatever the slot held.
* The **comparison is a runtime call, not an IR instruction**. `<` inside a fold has to be able to raise: the
  answer of `min(n, "b")` is a `TypeError` naming both classes — the same sentence CPython writes, and the
  same one `max` writes. `sum` of an empty literal answers `0`; `min`/`max` over an empty container are
  refused in words for now, and the comparison helpers return a sentinel, never a `-1` that could be mistaken
  for a winner.
* The **answer** is pair-bound and print-correct, and it is *not* container-bound: `print(m)` answers, and
  `m + 1` refuses in words. That boundary is deliberate — reading one word of the answer back into an
  arithmetic door is the truncation ADR 0309 and Gap R.161 measure — and `min(n, 3) + 1`, `abs(min(n, 3))`,
  `m + 1` and `m > 1` stay refusals that name the origin, the missing half and the row.

What the door will not label stays a refusal in words, and the list of those shapes is the measure of where
it stops:

* a slot whose kind is **text, None, a list, a set or a dict** — the comparison is answered by the tag at run
  time, not by a guess at compile time: two numbers and two texts compare, and a pair with no ordering raises
  CPython's sentence naming the kinds the tag says (`min(n, "a")` over a text slot raises, and since ADR 0318 so
  does a container slot against a number). What is *refused* is the answer used where one word is kept — see the
  `m + 1` rows above;
* a **set literal** — its element set and order are the objects' hash fact and not the source's, and folding
  the written order would answer `sum({n, 3})` as `6` where the reference answers `3`;
* a **container written among the folded values** (`sum([n, [1]])`) — refused by the operand lowering in the
  ordinary `sum` road's own sentence, `sum adds numbers, and a list literal is a container`, which names what
  the element is rather than the tag it lacks;
* a **keyword argument** (`sum(xs, start=1)`) — and a **shadowed** `min`, `max` or
  `sum`, where the program's own function is asked first; the rest of the call surface is roadmap L11.7's row.
  `min(a, b, key=f)` over literal candidates is no longer in this refusal list: the ordering door calls the key
  through the ordinary call road (ADR 0340), while unsupported key shapes remain named refusals (Gaps R.254/R.255).

### Containers order element by element, and a set that is not a subset orders neither way

A container as a fold operand is answered by the same door as everything else, walking the container's
elements rather than reading anything out of the object it is stored in (roadmap Gap R.197, ADR 0318):

```python
xs = []
xs.append([1, 2])
xs.append([3])
a = xs[0]
b = xs[1]
print(min(a, b))                     # [1, 2]  — a list compares element by element
print(max(a, b))                     # [3]
print(min(a, a))                     # [1, 2]  — one object is reflexively equal; the first is kept

ss = []
ss.append({1, 2})
ss.append({1, 2, 3})
print(min(ss[0], ss[1]))             # {1, 2}  — a set compares by the subset operator
ta = {1}
tb = {2}
print(min(ta, tb))                   # {1}     — neither is the other's subset: '<' and '>' are BOTH False,
                                     #           so the first survives, and nothing raises

la = [1, 2]
print(min(la, 3))                    # TypeError: '<' not supported between instances of 'int' and 'list'
print(min(list(), list()))           # []
print(min(set(), list()))            # TypeError: '<' not supported between instances of 'list' and 'set'
d = {}
print(min(d, d))                     # TypeError: '<' not supported between instances of 'dict' and 'dict'
```

Four rules, all of them the reference's rather than this backend's:

* **A list is lexicographic.** Elements are compared in order, and when every shared element is equal the
  **shorter list is the lesser one** — the fold of `[1]` and `[1, 2]` keeps `[1]`. Each element pair is asked for
  **equality first** and only then for ordering, which is the order CPython's own list comparison asks; that is
  why `[None]` against `[None, 1]` compares two lengths rather than two `None`s (which have no ordering, and
  would raise).
* **A set is the subset operator**, which is a partial order: two sets that are not each other's subset answer
  False to both `<` and `>`, so `min({1}, {2})` keeps `{1}` **without raising**. A fold that raised there would
  disagree with the reference, and a fold that answered `False` for `>` would be wrong in the other direction.
* **A dict has no ordering against anything** — including a copy of itself. `min(d, d)` raises, because the
  identity shortcut that answers a list and a set is deliberately not taken for a dict or a `None`.
* **The sentence names the two kinds that failed**, in the order the failing comparison had them: a list
  against a number says `'int'` and `'list'`, and a failure *inside* two lists names the **elements**, so
  `min(["a"], [1])` says `'int'` and `'str'`, not `'list'` and `'list'`. Every one of these raises is catchable
  by the program's own `except TypeError:` and leaves the program running.

The walk is one runtime helper, `@rt_pair_order`, asked by the fold — which is also where the honest limit of
this row is: the **relational operators did not change**. `a < b` over the two built lists above still raises
(`TypeError: '<' not supported between instances of 'list' and 'str'` where the reference answers `True`) — that is
roadmap Gap R.201, measured at the same time and left open on purpose: the operators order through a
chain of their own, and rewiring it is the pair table Gap R.97 owns. The other half of what that row filed is since
been paid: `True < [1]`, `None < [1]` and `"a" < [1]` name `'bool'`, `'NoneType'` and `'str'` for the operand that is
not a number, because the ordering question is now asked of the operands before the lowering (Gap R.202, closed by
ADR 0382 — see “An ordering between kinds that do not order” above). A container that contains itself is filed
the same way (Gap R.203): the fold chooses the right winner and the printer cannot render it, so
`print(min(xs, xs))` after `xs.append(xs)` leaves the contract's exit 2 where the reference prints `[[...]]`.

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
  "not available on the record" error.
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
record's heap objects, the compiled runtime's tagged values and the extern-fn ABI all
read those numbers; the compiled heap's own object-header kind is a projection of them
(`list`, `dict`, `set`, `instance`, with 0 meaning "the compiled backend does not allocate
this — it is an immediate or an interned string"). `gustyc --lang` prints both tables, and
`--schema`'s `valueTag` definition documents the numbering.

### `str(x)` and `repr(x)` — one pair, one renderer each (ADR 0258)

`str()` and `repr()` are a **pair**, not two renderers: for every value they agree, and the one
value they disagree on is a **text**, which writes its characters under `str` and its quoted source
form under `repr` — `'hi'`, and `"it's"` when the text contains a single quote and no double one.
Inside a container both halves quote, which is why `print(xs)` and `str(xs)` write the same line:
a list element is always rendered by the `repr` half (ADR 0185's `quote` flag, now carrying the pair).

There is **one renderer per backend**, and `print`, `str()` and a container element are its three
callers. on the record that renderer is the evaluator's value printer, and `repr()` asks it the
quoting question a text alone answers differently. In the compiled backend the value printers no
longer call `printf` at all: every write goes through `rt_out_txt` / `rt_out_int`, which are pointed
either at stdout or — while `str()`/`repr()` render — at a capture buffer whose bytes come back as an
interned string (`rt_str_of_container` asks the object to render itself; `rt_str_of_value` renders one
value by its tag). A form added for `print` is therefore a form the pair has; the module fails its own
test (`TestRenderPairIsOneTableNotTwo`) the day a second value renderer appears.

A function whose `return` renders a value **is a string-returning function**, and its callers are told
so: `def g(): return str(42)` / `print(g())` writes `42` on the compiled backend, and so do `str(x)` of a name,
`repr(42)`, `str([1, 2])` and `str(None)`, with the answer usable as a binding, an argument to `len`, a
method receiver and a concat operand (`Gap R.163`, ADR 0281). Before it was, the callee returned the
interned **index** and the caller printed it with `%d` — `0`, at exit 0, for every one of those shapes.
The rule is ADR 0224's (a method's string answer is known to its callers) one door earlier, and it
respects Gap R.6's: a program that defines `str` itself gets its own function, so
`def str(x): return x + 7` beside that same body answers `49`.

A builtin called with **no argument** is first asked which kind of call it is (ADR 0287). `int()`, `float()`,
`bool()` and `str()` are constructors and answer `0`, `0.0`, `False` and the empty text on both legs;
`ord()`, `chr()`, `abs()` and `repr()` convert a required value and raise the reference's own
`TypeError: <name>() takes exactly one argument (0 given)` (exit 3). They are two rules, not one: the eight
names share a dispatch, and the reference splits them. `bool(x)` asks the truthiness question — a number by
its word, a text by its length, and a container declines until the object can be asked its size.

A text the body **rendered into a name** is a text-returning function (ADR 0286). `def f(v): s = str(v);
return s` answers `3` for `f(3)` and `hi` for `f("hi")` on both engines; before this the compiled leg printed
the interned index through `%d` — `0`, and `2` for `s = "x" + str(v)` where the reference prints `x3` — at
exit 0. ADR 0281 taught the compiler that `return str(v)` renders; a body that binds the rendering one
statement earlier was still a number-returning function to its callers. The verdict now follows the
assignment, exactly as ADR 0285's number verdict does. The `str()` digits road is separately gated: it may
only be handed what the compiler can *see* is a number, because it writes the decimal digits of the word it
receives — right for an int, a fabrication for a handle. A `None` handed to a function is still the number
`0` there (`Gap R.171`), which is L11.1's tagged value word arriving by call.

A number the body **computed out of its own parameter** keeps the kind the argument arrived with (ADR 0285).
`def f(x): y = x + 1; return y` answers `1.1` for `f(0.1)` and `3` for `f(2)` on both engines; before this
the compiled leg printed the truncated `1`, and `y = x * 2`, `y = x - 1` and a float default printed `0`,
`-1` and `5` at exit 0. A parameter is written by the caller, so no assignment in the body records it — the
scan now asks that question knowing the body's own parameter names. Three shapes still refuse rather than
guess (`y = x` / `return y`, a binding under a condition beside an int-returning arm, and two bindings deep),
each naming what the arithmetic was over (`a parameter the call site handed a double`, not a container) —
`Gap R.169` keeps them open until the return can carry a tag.

A call is **asked how many arguments there were**, once, on the road every caller shares (ADR 0284). A
callable read out of a variable used to be called without being asked: `g = lambda x: x * 2` then
`print(g(1, 2))` printed `2` and `print(g())` printed `0`, both at exit 0 — arithmetic on arguments that
were never passed — where the reference raises `takes 1 positional argument but 2 were given` /
`missing 1 required positional argument: 'x'`. Both now stop at exit 3 naming the callee, the accepted count
and the received count (`too many arguments for <lambda>: it accepts 1 argument, got 2`). A `def` keeps the
name the program gave it (`too many arguments for f`); a lambda is `<lambda>`, never the compiler's
generated `lambda_0`. Defaults and keywords are unaffected — `def g(a, b=2, c=3)` answers `6`/`9`/`12` for
one, two and three arguments, and `g(c=9, a=1)` answers `12` — because a missing parameter is only an error
when nothing can fill it.

A `def`'d name is a **declaration, not a variable**, and says so (ADR 0283). Reading one where a value
is asked — `print(f)`, `f + 1`, `xs = [f]`, `str(f)` — is a front-end refusal at exit 1, because a `def`
allocates no slot to read; an earlier build emitted `load i32, i32* %_f` for that nonexistent slot and left
through exit 2. A function in a **numeric** position raises instead of refusing, in the reference's words
on the compiled backend: `abs(f)` → `TypeError: bad operand type for abs(): 'function'`, `-f` →
`bad operand type for unary -: 'function'`, and an imported module answers `'module'`; each is catchable by
`except TypeError:`. A name the program *assigned* is a different matter and keeps working — `g = f` then
`g(21)` answers `42`, `g = lambda x: x * 3` then `g(4)` answers `12` — and a body that calls a **parameter**
answers too: **ADR 0345** specialises a function that is used as a value into names before the doors that decide
a call are asked, so `print(f)` and `xs = [f]` still refuse in words while `apply(inc, 41)` compiles.

A **function used as a value** is a value this backend answers, as long as the program *calls* it (L11.7,
ADR 0345). A higher-order function is cloned once per tuple of functions its call sites ask its parameters to be,
a `lambda` that needs a name is given a declaration of its own, a name that only ever aliases a function is
pointed at the calls it makes, and **a higher-order body that calls itself is answered by the clone calling
itself** (**ADR 0364**, roadmap **Gap R.267**) — the recursion hands the callable back to its own position, which
is the one mention of a called parameter that is not a use as a value. **ADR 0385** closes the recursive half of the
omission rule: a self-call may leave a defaulted callable parameter out, because the enclosing clone already owns that
entry, and may repeat the entry's own spelling rather than name the parameter (**Gap R.335**). A self-call that supplies
a *different* function still refuses; so does one that omits a required parameter, which is the reference's arity
`TypeError`, not a placeholder the compiler may invent.

```python
def twice(fn, v):
    return fn(v) + fn(v)

def d2(x: int) -> int:
    return x * 2

print(twice(d2, 3))                              # 12
print(twice(lambda x: x * 3, 2))                 # 12

def fact(n, f):
    if n <= 1:
        return 1

    return f(n) * fact(n - 1, f)

print(fact(4, lambda x: x))                      # 24 — the clone calls itself
print(fact(3, d2), fact(3, lambda v: v))         # 24 6 — two clones, each its own recursion

def run(n, f=lambda x: x):
    if n <= 0:
        return 0

    return f(n) + run(n - 1)

print(run(3))                                    # 6 — the recursive slot inherits the clone's entry (ADR 0385)
print(run(3, lambda x: x))                       # 6 — repeating the entry is still the same entry

def run(v, fn):
    return fn(v)

print(run(21, d2))                               # 42 — the callable need not be the first parameter (Gap R.313)

g = d2
print(g(4))                                      # 8 — the alias is called as d2
print(sorted([3, 1, 2], key=lambda x: 0 - x))    # [3, 2, 1] — the hook lambda gets a declaration
print(max([1, 3, 2], key=lambda x: 0 - x))       # 1
neg = lambda v: 0 - v
print(sorted([3, 1, 2], key=neg))                # [3, 2, 1] — the same hook read out of a NAME (Gap R.281)
print(sorted([3, 1, 2], key=neg, reverse=True))   # [1, 2, 3]
```

The boundary is *called* versus *read*. A function that flows to a position which keeps one word for a value
still refuses, in words, at exit 1: `print(f)`, `xs = [f]`, `print(g)` after `g = f`, a `lambda` returned from a
function and then called, a `lambda` that reads a variable of an enclosing function (outside the `key=` hook the
ordering door calls, which is a position the compiler calls rather than one it keeps a word for — see
§ A hook lambda reads the frame its keyed call is written in), and a `lambda` that calls
its own parameter. A recursion the clone cannot follow is refused by **this** pass, in a sentence that names the
parameter and the line: the recursion hands the callable to something else (`run(n - 1, other)`), it omits a
parameter, or it is written inside a `lambda`. What the rewrite did not change is what ADR 0283 promises about
reading a `def`'d name; what it
changed is that a function the program *calls* no longer needs a word to travel in. A clone reports the name the
program wrote — a traceback and `--debug-info` say `twice`, and a hoisted `lambda` says `<lambda>`, the
reference's own word for it. `gustyc --callables <src>` prints the pass's own account of a source (what it
specialised, how many self-calls it answered, and the shape it declined) — and when the call that account declined
over is the call the emitter cannot name, the refusal QUOTES that sentence, under a label that says who is speaking
(**ADR 0379**, roadmap Gap R.337). A program the pass never spoke about — `t = 1` / `print(t())`, whose only problem
is a name that was never a function — gets the plain sentence, with nothing appended to it. A **`lambda`'s parameter list is spelled the same way a `def`'s
is** — `f = lambda x, y=1: x + y` / `print(f(1))` is `2`, and the call may omit `y`, supply it positionally, or
name it (`f(1, y=9)` is `10`) — since **ADR 0352** closed Gap R.263, which had been a parse error. The default
is an ordinary expression, so a call (`y=two()`), a module name (`k=STEP`), a conditional and a value with a
`:` inside its own brackets (`d={"a": 1}`, `s="a:b"`) all work, and the default resolves against the
*definition's* scope, not the caller's. What still refuses is Gaps R.255, R.264, R.265, R.266, R.268
and R.269, plus the shapes ADR 0352 measured and filed rather than absorbed: a `lambda` that reads a variable
of the function it is written in (**Gap R.276**), `*args`/`**kwargs` and a trailing comma in any parameter
list (**Gap R.278**, **Gap R.279**) and a container
default beside a higher-order call (**Gap R.282**). A hook read out of a name a `lambda` was bound to is no
longer among them: `neg = lambda v: 0 - v` with `sorted([3, 1, 2], key=neg)` answers `[3, 2, 1]` since
**ADR 0369** closed **Gap R.281**, because the pass now retargets the `key=` argument to the declaration it
minted for the lambda (see § A hook reads its key from the name a lambda was bound to).

One default shape does not merely refuse — it **answers wrongly**, and is called out here rather than
discovered by chance: a default whose value is not a number or a text is never bound to the parameter, so the
parameter reads the payload word `0`. `def d(y=None): return y` prints `0` where the reference prints `None`,
`def d(xs=[]): return xs` prints `0` for `[]` and `def d(w={}): return w` prints `0` for `{}` — at exit 0, with
no diagnostic. Every *scalar* default answers (`x=5` → `5`, `s="hi"` → `hi`, `b=True` → `True`), and a container
default the *caller supplies* answers, which is what localises it to the omitted-argument road (**Gap R.277**;
the same on both `def` and `lambda`). Bewise `len(xs)` on an `xs=[]` default: it prints `0`, which is what
CPython prints, for entirely the wrong reason.

Printf-style `%` on a text **formats**, on the compiled path, at the answers the reference gives
(`Gap R.31`, `L11.1`, ADR 0360):

```python
print("%s" % 2)                                  # 2
print("hi %s, you are %d" % ("Ada", 36))         # hi Ada, you are 36
print("%05d|%-5d|%+d|% d" % (42, 42, 7, 7))      # 00042|42   |+7| 7
print("%#x %#X %#o" % (255, 255, 8))             # 0xff 0XFF 0o10
print("%5.2f|%08.3f|%.0f" % (3.14159, 3.14159, 2.5))   #  3.14|0003.142|2
print("%r" % "text", "%a" % "it's")              # 'text' "it's"
print("%c %c" % (65, 8364))                      # A €
print("%*d|%.2f|" % (5, 42, 3.14159))            #    42|3.14|
print("%(who)s scored %(score)d" % {"who": "Ada", "score": 7})   # Ada scored 7
print("100%% of %d" % 100)                       # 100% of 100
```

The conversions answered are `%s %r %a %d %i %o %x %X %e %E %f %F %g %G %c`, with the reference's flags
(`-`, `+`, space, `#`, `0`), its field width and precision — including the `*` and `.*` forms that take
them from the arguments — its positional numbering `%2$s`, and its mapping form `%(key)s`. A field is
measured in **characters**, not bytes: `%10s` beside a two-byte accent pads to ten of them. The traps are
the reference's own classes and sentences, catchable by the `except` arm the program wrote:

| program | raise |
|---|---|
| `"%d" % "text"` | `TypeError: %d format: a real number is required, not str` |
| `"%x" % "text"` | `TypeError: %x format: an integer is required, not str` |
| `"%f" % "text"` | `TypeError: must be real number, not str` |
| `"%s %s" % ("one",)` | `TypeError: not enough arguments for format string` |
| `"%s" % ("a", "b")` | `TypeError: not all arguments converted during string formatting` |
| `"%(a)s" % 5` | `TypeError: format requires a mapping` |
| `"%(a)s" % [1, 2]` | `TypeError: list indices must be integers or slices, not str` |
| `"%(missing)s" % {"a": 1}` | `KeyError: 'missing'` |
| `"%c" % "ab"` | `TypeError: %c requires int or char` |
| `"%c" % 3000000` | `OverflowError: %c arg not in range(0x110000)` |
| `"%*d" % ("x", 3)` | `TypeError: * wants int` |
| `"%Q" % 1` | `ValueError: unsupported format character 'Q' (0x51) at index 1` |
| `"%s %" % ("a",)` | `ValueError: incomplete format` |
| `"%d" % float("nan")` | `ValueError: cannot convert float NaN to integer` |

The answer **is a text** — the same `@str_tab` index a literal answers — so it binds (`s = "%s!" % "hi"`),
measures (`len(s)`), concatenates, compares, reaches a text method (`s.upper()`), enters a container slot
with its kind beside it (`print(["%d" % 7])` is `['7']`), lands in an instance slot and crosses a function
boundary, printing its text and never the index under it (ADR 0359's rule, asked of the one predicate
`percentIsTextExpr`).

What the door declines it says in words, at compile time and in the source it was written in: a conversion
with no renderer (`%b`, `%B`, `%u` — bytes conversions and the obsolete `%u` the reference removed) and an
argument this backend has no value for (a nested tuple, a lambda, a generator, an integer beyond what the
bounded-integer row covers such as `"%d" % (2 ** 70)`), which would print the number underneath it
(`roadmap L11.1`, ADR 0360). `%` between two numbers is untouched by all of this: it
is the remainder and answers on both legs — `7 % 3` is `1`, `-7 % 3` is `2`, `7.5 % 2` is `1.5`,
`-7.5 % 2` is `0.5`, `7.5 % 2.5` is `0.0` — and a comparison is not a numeric use, so `1.0 == "a"` still
answers `False`/`0`. The bug that made the operator unsafe to allow, a format string's `@str_tab` index
widened into the double domain and used as a remainder's dividend (`print("%.2f" % 3.5)` printing `0.0` at
exit 0, `Gap R.165`, ADR 0282), is closed by the door having the only text-`%` road in the module.

A `%c` whose code point the source **spells** is refused when that code point is a surrogate (`0xD800` through
`0xDFFF`): a surrogate has no UTF-8 encoding, and the reference refuses it with `UnicodeEncodeError` — a class
this backend's shared exception table does not hold — so a front-end refusal naming the code point is honest
and a three-byte answer is not. A code point the program **computed** reaches the encoder rather than the
refusal, and is written as the bytes the encoder refused (`0xED 0xA0 0x80` for `0xD800`) at exit 0 where the
reference raises: that divergence is the debt, and the missing door behind it is the exception class, not the
formatter (pinned as it is by `TestPercentArgumentResidueAtTheCLI`; `roadmap Gap R.31`, ADR 0360).

A value is rendered by the form its **expression names**, not by the slot that happens to hold it:

```python
print(str([1, 2]))      # [1, 2]        · was `0`, exit 0
print(str(set()))       # set()         · was exit 2, llc rejecting the module
print(str(None))        # None          · was `0`, exit 0
print(str(1.5))         # 1.5           · was refused, `str on non-integer`
print(repr("hi"))       # 'hi'
print(str(True))        # True          · ADR 0257: the verdict's name, not the 1 stored for it
xs = []
xs.append("v" + str(7))
print(xs)               # ['v7']        · a text built at run time had no repr: it printed (null)
```

`str(x)` of a value the compiler can fold produces the same text on the compiled backend and the same text
CPython prints: `str(None)` is `"None"` (not `"0"`), `str(1.5)` is `"1.5"`, and `str("x")` is `x`. A
folded string may be printed but never stored as a global; where it is stored, the text is interned
and the handle kept (ADR 0183). A value whose kind **no expression names** — an untagged word the
compiler cannot read — is refused in words, exit 1, naming which half is missing, rather than
answering with the number underneath it; the remaining shapes of that kind are roadmap Gap R.115. A
**tuple** is no longer one of them: since **ADR 0361** it is the list layout's object with one attribute bit
set, so it renders, indexes, unpacks, compares and hashes (see § Tuples are values).

A **container slot is an i32 word**, so the element kinds a compiled container can hold are the kinds
a word can carry: integers, interned strings (`@str_tab` indices, ADR 0224) and `None` — each with its
own tag (ADR 0187/0189). A **float does not fit**, so `xs = [1.5]`, `print([1.5, 2])` and
`[1] == [1.0]` are refused with a message naming the element kind and the item that owns the fix
(roadmap L11.6), never emitted as an instruction LLVM has to reject (ADR 0166). The refusal replaces a
silence that was worse than either: the fold truncated floats to words, so `{1.5} == {1.6}` and
`{"a": 1.5} == {"a": 1.6}` compiled to **True** and `print(xs[0])` of `[1.5]` printed `1`
(roadmap Gap R.40, ADR 0226). the record answers all of those correctly today.

An operator between **two operands of different runtime kinds** is not a numeric question:
`1.0 == [1]` is False, as CPython answers it, decided by kind rather than by coercing a container
through a float conversion (ADR 0215, with ADR 0221's numeric cross-kind pair as the deliberate
exception). Ordering a number against a container is a `TypeError` in CPython and is refused today,
because this backend cannot raise a runtime `TypeError` yet (roadmap Gap R.37).

A compiled list can hold numbers, interned strings and `None` together: each element slot
carries its own tag, so `print([1, "a", None])` gives `[1, 'a', None]` on the compiled backend and on
CPython (ADR 0184). What a mixed list may hold is decided by what the tag can honestly describe
— integers, interned strings, `None` — so `[True, "a"]`, `[1.5, "a"]` and `[[1], "a"]` are still
reported rather than mis-printed.

A slot's tag is written by whatever writes its payload, never afterwards (ADR 0187). That covers
building a literal, `xs.append(v)`, and `xs[i] = v` — the last of which used to store the payload
and leave the tag alone, so `xs = [1, "a", None]; xs[0] = "z"; print(xs)` answered
`[1, 'a', None]`: the interned index of `"z"`, printed through the slot's stale `int` tag.

The tag is only half of what a store owes. The container keeps a census of its own — the bit that
tells the emitter to print each slot by asking it (`rt_mark_estr …, 8`) rather than by assuming one
kind for the whole list — and a store that writes a value the container's kind cannot render has to
raise it. Since **ADR 0422** all three writers ask the same question of the value, `elemKindTag`, and
promote the container when the answer is a kind whose payload only means something through its tag: a
float, `None`, a verdict, another container, an instance. So the store, `append` (ADR 0233) and the
dict's entry store (ADR 0232) cannot disagree about what one slot holds:

```gy
xs = [1]
xs[0] = 2.5
print(xs)             # [2.5]   — was [1]: the float box's handle, printed as a number
xs[0] = None
print(xs)             # [None]  — was [0]: the void's payload, printed as a number
xs[0] = [3]
print(xs)             # [[3]]
ys = xs               # the promotion is an event on the object, not on the name written (ADR 0412)
ys[0] = True
print(xs)             # [True]
```

A store of a plain number into a numeric list is untouched by this: the container keeps its single
kind, the plain printer, and the static array `sum`/`min`/`max` fold from (ADR 0239). What the promotion
used to cost — a chained comparison over a promoted slot, or a numeric use of a slot a run-time index
wrote, each refusing where the reference answers — is paid: the chain asks its operand what it carries and
binds the pair (ADR 0423, paying Gap R.442), and `+` beside a number the source spells asks the two tags
rather than the notebook. What still reports rather than answering from a payload is the **comparator**
over a promoted list (`sorted(xs)`, Gap R.438 / ADR 0191) and the **loop item** at a chain
(Gap R.443).

Reading one element out produces the pair as well, and three uses of it are open:
`print(xs[i])` dispatches on the tag, `xs[i] == [1, 2]` and `2 in xs[i]` compare through
`rt_container_eq`, and `v = xs[i]` binds a *tagged variable* — the same
`(value, tag)` binding a loop variable gets, so `print(v)` renders `str()` (`a`) while
`print(xs)` renders `repr()` (`'a'`), and rebinding `v = 5` retires the tag (ADR 0185, ADR 0187).
What still reports — rather than computing on a string-table index — is a context that needs one static
kind and was given a **text, a container or the void** in the slot: `xs[i] + 1`, `xs[i] > 2`, passing
`xs[i]` to a function, `xs[i]` in a format spec. A *number* in the slot answers at all four of them
(ADR 0249, ADR 0423).

**A container inside a container is read back the same way** (ADR 0239, ADR 0241). The slot holds
the inner object's *handle*, so the second subscript is a load from the object the first one named,
and every use of it works — on the compiled backend, at CPython's answer:

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
2.5]` / `print(xs[i] * 2)` is `3.0` — and what `*` is waiting for is repetition itself (Gap R.33); the
`%`-formatting half of that pair is paid (Gap R.31, ADR 0360), including a slot's value rendered by the
printer: `xs = [3]` / `print("%d items" % xs[0])` is `3 items`.

What the tag cannot settle is the **kind of the result**, which the compiler has to know before the program
runs. A container whose slots are ints here and floats there (`xs = [1, 2.5]`) is therefore refused rather
than widened: `xs[i] + 1` would print `2.0` where CPython prints `2`, and that is a different value, not a
near miss. The same honesty covers a container that was **built rather than spelled out** (`xs = [];
xs.append([7, 8]); print(xs[0][0])`), which no literal ever described.

the record — boxed values, no static tag needed — answers all of these, which is what keeps the
refusals roadmap rows rather than mysteries (roadmap L11.1, `docs/roadmap-details.md`). One exception is
recorded rather than papered over: **unary minus does not consult a tag in either backend**, so `print(-"a")`
answers `-281474976710658` on the record and `0` compiled, where CPython raises
`TypeError: bad operand type for unary -: 'str'` (roadmap Gap R.89).

**Dicts and sets take the same rule** (ADR 0232). A compiled dict may mix kinds on either side of
an entry and a compiled set may mix kinds among its members, because a slot is always the pair
(payload, tag):

```gy
d = {"a": 1, "b": "x", "c": None}   # values: number, string, None
print(d)            # {'a': 1, 'b': 'x', 'c': None}   — the compiled backend, and CPython
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
interned strings, `None`, floats, bools (stored as the number the compiled backend stores them as), and
another container. A needle whose kind the compiler cannot prove reports *when the container mixes*
— `1 in s` where `s` is `{1, 'a'}` and the needle is a call whose return kind nobody knows.

A **float** in a slot is the handle of a *float box* (ADR 0238): a slot is one `i32` word and a
double does not fit in one, so the bits live beside the heap and the tag says the payload is a box.
That is what lets the compiled path answer the whole family, byte for byte as the record and
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

**…and `in` and `for` over such a slot ask it too** (ADR 0323, `Gap R.95`). A membership test and a loop
both pick a helper — which container's elements to walk, and how many there are — and for a container the
program spelled out that choice is a compile-time fact. For a container it *built*, the fact lives beside
the payload, so the question goes to the object: a list slot is walked by its elements, a dict slot by its
**keys** (which is what `in` tests a dict by, and what a loop over a dict steps over), a set slot by its
members, and a text slot as a substring test or as a run of one-character strings. Each arm is one test on
the tag, and the loop's three questions — how many, which word, which kind — are asked of the object, so a
list, a dict, a set and a text can be the haystacks of one program and each get the question that fits.

```gy
xs = []
xs.append([7, 8])
xs.append({"k": 1})
xs.append({5})
xs.append("abc")
print(7 in xs[0])                            # True   — used to be refused: "needs a single static kind"
print("k" in xs[1], "z" in xs[1])             # True False — a dict is tested by its keys
print(5 in xs[2], 6 in xs[2])                 # True False — a set by its members
print("b" in xs[3], "z" in xs[3])             # True False — a text by its substrings
for v in xs[0]:
    print(v)                                 # 7, 8   — the loop variable carries its tag (ADR 0185)
d = {}
d["a"] = [1, 2]
for v in d["a"]:
    print(v)                                 # 1, 2   — printed a single 0 before this door existed
```

A value with nothing to walk raises, and the two questions phrase it differently because the reference
phrases them differently: `1 in xs[0]` over a number slot is `TypeError: argument of type 'int' is not
iterable` and `for v in xs[0]` over the same slot is `TypeError: 'int' object is not iterable`. Asking a
text whether it contains a number is neither of those — it is
`TypeError: 'in <string>' requires string as left operand, not int`, which names the *needle*. All three are
raises, not refusals, so the program's `except TypeError:` reaches them (`ADR 0228`).

**…and the chain, and the needle, are asked the same way** (ADR 0324, paying `Gap R.210` and `Gap R.211`). The
haystack does not have to be one slot deep: `7 in xs[0][0]` is `True`, `for v in d["a"]["k"]` steps the list under
two dict keys, and `for v in xs[0][0][0][0]` works for the same reason — the four arms never asked how long the
chain was, only what the object at its end is, so the read ADR 0251 already did recursively hands them the pair at
any depth. The **needle** is asked the same question, because a value the program put into a container has its kind
beside its payload and nowhere else: two containers with the same entries are equal, whatever objects hold them.

```gy
xs = []
xs.append([[7, 8], 5])
xs.append([{"k": 1}])
d = {}
d["z"] = {"k": 1}
print(7 in xs[0][0])                             # True   — used to be refused: "index cannot reach into xs's slots"
print(d["z"] in xs[1])                           # True   — a dict found by its contents, not its address
```

Two more sentences come out of looking the needle up rather than comparing it loosely. A dict is tested by its keys
and a set by its members, and a container can be neither: `[1] in xs[0]` over a dict or a set slot is
`TypeError: unhashable type: 'list'`, again a raise the program's `except` reaches, not a reported absence — the read
side of the rule `ADR 0311` enforces when an unhashable value is *written*. A set haystack keeps the one exception
the reference itself makes: `{1} in {1, 2}` is answered `False`, not raised, because a set needle is looked up as
the frozenset it converts to. What still refuses is a needle whose kind no road can prove — a call that returns a
number on one path and a text on another (`Gap R.212`), which says so in words rather than guessing.

**…and an instance slot names its own class** (ADR 0325, paying `Gap R.209`). A slot written by
`xs.append(C())`, a literal `[c]`, `{c}`, `"k": c`, or any other container position whose element is a user-class
instance carries `TagInstance`, not `TagInt`. The payload is still the heap handle; the object's own slot 0 carries
the class id, and the module's class table reads it when a renderer or raise has to name the class. Printing an
instance therefore gives the default identity form rather than a number, and a tag-keyed question raises the
reference's sentence with the class the program stored:

```gy
class C:
    pass

xs = []
xs.append(C())
print(xs[0])                 # <__main__.C object at 0x…> — the address is the heap slot, not a process pointer
try:
    print(len(xs[0]))        # TypeError: object of type 'C' has no len()
except TypeError:
    pass
try:
    print(1 in xs[0])        # TypeError: argument of type 'C' is not iterable
except TypeError:
    pass
try:
    for v in xs[0]:
        print(v)
except TypeError:
    pass

ys = []
ys.append(C())
xs2 = []
xs2.append("ab")
print(ys[0] in xs2[0])       # TypeError: 'in <string>' requires string as left operand, not C
```

The same rule reaches the ordinary binary road: `c in "abc"` is the `'in <string>'` raise, while `"abc" in c` is
the membership raise, so the sentence names the side CPython names — the needle for a text haystack, the haystack
for iterability. Identity equality remains identity: two separate `C()` objects compare `False`, and a slot
compares equal to itself.

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

Such a slot can also be **used as a number**, and the answer's kind is the answer's own
(`xs[0][0] + 1` is `8` where the slot holds `7` and `15.0` where it holds `7.5`). The compiler hands the
pair — the payload and the tag its writers left — to the target, which does the arithmetic in the one word
that holds both families exactly and answers a pair back; the printer reads that tag, so no instruction had
to guess (`roadmap L11.1`, `ADR 0265`):

```python
xs = []
xs.append([7, 8])
print(xs[0][0] + 1)         # 8    — the slot holds an int, the answer is an int
print(-xs[0][0])            # -7
xs.append([7.5, 8])
print(xs[1][0] * 2)         # 15.0 — the same source, and the answer is a float
d = {}
d["k"] = 40
print(d["k"] + 2)           # 42
```

`+` and `*` take that road only where the program can be *shown* to keep numbers in the slots the read
reaches: a text, or a value the compiler cannot see through, written into the container the read walks
closes those two operators, because CPython answers `"a" + "b"` and `[1] * 2` with a value this backend
cannot build from a slot (`Gap R.82`) and a raise where the reference returns a number is the one answer
this compiler does not ship. The block is about **that container**, and about everything that can denote
it — an alias (`ys = xs`), a loop variable (`for v in xs`), a parameter beside the argument that reached it
(`f(xs)`) — and about nothing else, so a text in a container the arithmetic never walks no longer closes
the door for the rest of the module (`Gap R.191`, `ADR 0322`):

```python
w = []
w.append("a text")            # a container the arithmetic never walks
nested = []
nested.append([7, 8])
print(nested[0][0] * 2)      # 14   — the door is keyed by the container the store writes
d = {}
d["k"] = 40
print(d["k"] + 2)            # 42
```

What still closes it is a write this pass cannot attribute to any name — `xs[0].append("a")`,
`self.items.add(b)`, a loop variable over `rows()` — because there the question "which container?" has no
answer to key a block by. `-` and the unary `-` answer a number or raise whatever the slot holds, so they take the door
either way; a text under the negation raises CPython's own
`TypeError: bad operand type for unary -: 'str'`, naming the kind the object carries, and the raise leaves
through the same door `except TypeError:` and `except OverflowError:` reach. A whole-number answer past the
compiled 32-bit `int` word raises a catchable `OverflowError` naming `roadmap L12.12` rather than truncate
through the conversion into a silent negative; the interpreted leg, whose ints are 64-bit, answers the
number, and the split is recorded rather than averaged.

The answer of that arithmetic is a value, so it survives being **bound** before it is read
(`programs/probe_arith_result_bound_to_a_name.gy`):

```python
xs = []
xs.append([7, 8])
n = xs[0][0] * 2
print(n)              # 14
n = [1, 2]
print(n)              # [1, 2] — the rebinding retires the tag the arithmetic left
```

A binding of that shape stores the pair the printer reads — the value beside the tag the objects decided —
and a binding that is *not* a pair retires it, so the name is read by the road its newest value deserves
(`Gap R.142`). Every position that asks for a **number** then reads the pair too (`Gap R.143`, ADR 0268):
`print(n)` and `print("n", n)`, `n + 1` and `-n`, an ordering (`print(n > 13)`, `13 > n`), a condition's head
(`if n:`, `while n > 0:`, `if n > 1 and n < 20:`), a format field (`f"{n}"`, `str(n)`, `repr(n)`), and the
target of an augmented assignment (`n += 1`). A name the arithmetic door bound is provably a whole number or a
float — the door raised on everything else — so those positions lift the pair into the one word that holds both
families, and a float keeps its digits where an int keeps its `True`/`False`/`14`.

The same value reaches a **function**, in both directions (`Gap R.139`, ADR 0273). The argument arrives as
the pair — a parameter whose argument the arithmetic door answers is declared with a second `i32` beside its
payload and bound inside the callee through the door every tagged value uses — and the answer's kind comes
back in a word the callee stores beside its own `return`, read by the caller immediately after the call:

```python
def twice(v):
    return v * 2

xs = []
xs.append([7, 8])
xs.append([1.5, 2])
print(twice(xs[0][0]))        # 14
print(twice(xs[1][0]))        # 3.0 — the slot's own kind, chosen by the objects, not by the compiler
print(twice(v=xs[0][1]))      # 16 — the keyword form lands on the same position
print(twice(3))               # 6  — one `define`, both call sites
n = twice(xs[0][0])
print(n)                      # 14 — the answer is a value, so it survives being bound
```

A body that hands its parameter to `print`, to a comparison, or to arithmetic reads the tag it was handed; a
body that can reach its end without a `return` answers `None` and says so in that same word, and so does the
road where its arithmetic raises — `except TypeError:` in the *caller* reaches a trap the *callee's* operator
threw. What the pair crosses is settled by one scan of the whole program, before any IR exists, so the
`define` and every `call` agree about the arity; and a parameter is opened only where every call site can
supply a pair from its own spelling and the body reads it back somewhere a pair door answers.

The argument does not have to be a slot read for the pair to be the right shape: **a number handed to a
function keeps the kind its argument had** (`Gap P.1`, `Gap R.154`, `Gap R.157`, `Gap R.158`, ADR 0276). A
parameter whose argument can end on a double — a float literal, a variable the program rebound to one, a
quotient, a slot of a literal list that holds one, a default no call site mentions — arrives as the same two
words, because the ordinary road would answer it with one and print a truncated number at exit 0:

```python
def twice(v):
    return v * 2

print(twice(2.5))               # 5.0 — where the compiled leg printed 4
print(twice(2))                 # 4   — the same `define`, the answer's kind follows the argument
x = 8
x = 2.5
print(twice(x))                 # 5.0 — the state the pair already carries crosses too

ys = [1, 2.5]
print(twice(ys[1]))             # 5.0 — a slot of a literal the scan can read

def area(w, h):
    return w * h

print(area(2.5, 2))             # 5.0 — `w` carries the tag; `h` is proven an integer by its call sites

def greet(name, times=1.5):
    return times

print(greet("a"))               # 1.5 — a default is an argument the caller did not have to write
```

The answer travels back the same way when it is a pair: `def g(y): return y * 2` under
`def f(x): x = x + 1.5; return g(x)` hands the caller the pair the callee stored, and where the caller's own
convention is a `double` the pair is lifted at `@rt_lift_num` — `f(1.0)` is `5.0`, where the module used to read
the double as an `i32` and answer `2`. The argument does not stop at the first frame: **a body that hands its parameter on keeps the kind too**
(`Gap R.161`, ADR 0277). `def outer(x): return twice(x)` contains nothing that says `x` can be a float — a
parameter is written by the caller and never by the body — so the evidence lives one frame up, in the
argument `outer`'s own call site was written with:

```python
def twice(v):
    return v * 2

def outer(x):
    return twice(x)

print(outer(2.5))               # 5.0 — where the compiled leg printed 4
print(outer(3))                 # 6   — one `define`, the answer's kind still follows the argument

def outermost(y):
    return outer(y)

print(outermost(2.5))           # 5.0 — two frames hand the same pair on

def add(a, b):
    return a + b

def shift_it(z):
    return add(z, 1)

print(shift_it(2.5))            # 3.5 — a forwarded parameter beside a literal the body writes
```

Where a pair is handed on, both frames carry the two words, and a function that returns a pair-returning
callee's result directly is pair-returning itself **whichever order the file is written in**: `def f(v):
return other(v)` above `def other(w): return w * 2` and the same two `def`s swapped compile identically,
because the answer direction is a scan answer and the tag word is declared where the callee's own `define` is.
A chain the doors cannot carry is closed from either end: if the callee takes a power of its parameter, renders
it a text, or indexes with it, the caller is not given a tag word either — the pair would be half-read one frame
down, which is a wrong number rather than a refusal (`Gap R.164` names what those chains still owe an answer
for; the flooring operators stopped being one of them with ADR 0278, below).

Two rules keep the answers that already worked, both of them answered by
the same scan rather than by the order the module happens to be emitted in. A function whose body is written
under a convention that owns its return word — ADR 0274's `double`, ADR 0174's string index — keeps its
parameter words too, so `def f(x): x = x + 1.5; return x` still compiles to `define double @gy_f(double %p0)`
with no tag word beside it and `def fmt(v): return str(v)` keeps the refusal rather than printing an interned
index. And a **condition** is a different position from a **value**: `if v > 10:` asks an operand's truth, which
the truth doors answer a pair for, while `return v > 1.5` asks for a value in a road that takes one word — so the
first is served and the second leaves the parameter on the ordinary road, printing the `True`/`False` it always
printed instead of refusing one it never needed (`Gap R.161`'s comparison half names what is owed).

The same two words carry the kind that decides **what floors a number and what remains** (`Gap R.162`,
ADR 0278). `//` and `%` choose their answer's kind by their operands, which is ADR 0216's rule for a literal;
over a parameter the kind is the tag, so the pair door asks the same tagged arithmetic door that `+ - *` ask —
the floor is `@llvm.floor.f64` and the remainder is `frem` corrected to the divisor's sign, both from one
helper the statement road shares, so the two roads cannot grow two definitions of `//`:

```python
def floorit(v):
    return v // 2

def modit(v):
    return v % 2

print(floorit(5.0))              # 2.0 — where the compiled leg printed 2
print(floorit(5))                # 2   — the same `define`, the answer's kind follows the argument
print(modit(5.0))                # 1.0 — where the compiled leg printed 1
print(floorit(-7.5))             # -4.0 — the floor goes down, not toward zero
print(modit(-7.5))               # 0.5  — and the remainder takes the divisor's sign

def modop(a, b):
    return a % b

print(modop(7.5, 2))             # 1.5 — either parameter may carry the double
print(modop(-7, 2))              # 1   — the integer control row, still one word

def outer(x):
    return floorit(x)

print(outer(5.0))                # 2.0 — the pair forwarded, and floored a frame down
```

The divide-by-zero sentence travels with the tag, and the reference has **four** of them, not two: over a
parameter `f(v) = v // 0` says "integer division or modulo by zero" for `f(5)` and "float floor division by
zero" for `f(5.0)`, and `f(v) = v % 0` says "integer modulo by zero" and "float modulo" — one source line,
named by the argument. Each is raised catchably (`except ZeroDivisionError:` reaches it) and exits non-zero,
because a trap that prints a number is not a trap.

What the door answers is the whole expression, not only its top: the arm question walks an operand's leaves,
so the identity ADR 0216 pinned holds over a parameter as it does over a literal — `(v // 2) * 2 + (v % 2)`,
`v * 2 + 1`, `v + 1 + 1`, `2 * (v + 1)`, `(v + v) * (v - 1)` and `(x + 1) * 2` over a name that became a double
all answer a pair (`Gap R.166`, ADR 0279). A *call*'s pair answer is an arm too, and a name that holds one is
read as a pair wherever the program reads it: `def outer(x): y = twice(x); return y` answers CPython's `5.0`,
`(v // 2) + other(v)` answers `4.5`, and an identity built from two call answers — `floorit(v) * 2 + modit(v)` —
answers `7.5` for `7.5` (`Gap R.164`, ADR 0280); a name written back with an ordinary value retires the pair, so
`y = twice(x); y = 3; return y` still answers `3`. What stays refused is a **position** that keeps one word, in
words and at exit 1: a pair answer stored in a container's element or handed to another function as its argument
(`Gap R.146`), and a pair answer read back by an augmented assignment (`Gap R.143`) — each of them measured
first as a digit at exit 0 (`8` for `10.0`, `5` for `6.0`) and turned into a refusal rather than shipped. The
text-`%` that row owed is paid beside them: `"%.2f" % 3.5` is `3.50` (Gap R.31, ADR 0360), and the argument it
refuses is the one with no kind to render, not the format.


What still refuses, in words: a **value** position — a builtin's argument, a container's element, an `and`'s
operand, or a second function's parameter — keeps one word for what it stores and has nowhere to put the tag
(`Gap R.146`); an argument that is *itself* such an answer (`twice(twice(xs[0][0]))`) is refused rather than
half-read; a pair-bound name cannot enter the float road (`n / 4`, `n > d` with `d = 2.5`,
`Gap R.148`), and a tuple unpacking has not taken the pair at all (`a, b = xs[0][0] + 1, xs[0][1] + 2`,
`Gap R.144`). A function that takes a pair-carrying parameter *and* an ordinary one (`def shift(a, b=100):
return a + b`) keeps the convention it has always had, because the shared arithmetic door has no kind to name
for a parameter no caller tagged (`Gap R.154`).

What still reports, with the mechanism it is missing named: a **dict keyed by a container** (Python
raises `unhashable type: 'list'`; a **set** does not even that yet — it admits the member and reports a
length, Gap R.81), a tagged element whose kind only the run time can tell used as a number by a road this
backend does not open there — a loop variable over a mixed list, or a position that stores one word for it
(`min(n, 3)`, `sum([n])`, Gap R.146 — a dict entry, a set member, a list element and the `and`'s operand are
paid: ADR 0310, ADR 0306 and ADR 0269), or a **tuple unpacking**, which has not taken the pair at all:
`a, b = xs[0][0] + 1, xs[0][1] + 2` refuses where the plain assignment answers (Gap R.144), **two** such slots ordered against **each other** (`xs[0] > ys[0]` compares payloads where
CPython raises — one side whose kind comes from the object is a chain, two is a table the compiler would be
inventing, Gap R.97), a comparison against an expression whose kind cannot be proven (Gap R.83), a
**membership** test or a **loop** whose haystack is such a slot (`7 in xs[0]`, `for v in xs[0]` after
`xs.append([7, 8])`, which need the object's *kind* where the read asks only its tag), or a **set variable**
subscripted directly (`sa[0]`, which the record answers by the documented extension and the compiler
declines, Gap R.94) — all of which need the value word that carries its own tag (roadmap L11.1). A fold (`sum`,
`min`, `max`) over container elements
reports too, rather than reaching for the elements' addresses the way CPython raises a `TypeError`.

The tag is what makes a value's kind a fact rather than a guess. What it does not buy yet is a
value that *is* a tag: a compiled float still has no word to hold it (L11.6), and a verdict handed to a
function prints `True` only because the front end read its call sites and told the print road what the
parameter holds (ADR 0348, closing Gap R.111) — a position the call sites contradict still prints the
number it is stored as (Gap R.270; the print *rule* landed in ADR 0257, the container tag in ADR 0259, and
what neither can reach is a tag that would have to ride with the argument) — while a bool in a container
slot says `True` on the compiled backend and
still answers every numeric question as its number. A container inside a
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
  branches/loops prints the currently-stored member, matching the record.
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

the record enforces the same annotation rules at runtime for nominal class
parameters (`a: Animal` rejects a `Rock`), so a violation fails fast in the
REPL; the AOT backend treats annotations as static-only, exactly like the rest of
the annotation surface.
- Arithmetic on non-numeric operands reports a diagnostic (suppressed inside
  untyped function bodies, which fall back to dynamic dispatch).

## `str(v)` and `repr(v)` ask the same door `print` asks

A value's kind lives beside its payload, in the object — and for a slot the program built at run time, only
the tag can say what it holds. `print(x)` has always asked the tag. `str(x)` and `repr(x)` ask **the same
printer**, pointed at a buffer instead of at stdout, with one flag deciding whether a text writes its
characters or its quoted repr (ADR 0185, ADR 0258, ADR 0303):

```python
xs = []
xs.append(3)
xs.append("a")
xs.append([1, 2])
xs.append(None)
print(str(xs[0]), str(xs[1]), str(xs[2]), str(xs[3]))   # 3 a [1, 2] None
print(repr(xs[1]))                                      # 'a'
n = xs[1]
print(str(n))                                           # a
for v in xs:
    print(repr(v))                                      # 3, 'a', [1, 2], None
```

So the answer to `str(v)` never disagrees with what `print(v)` wrote, and a container's slots answer for
themselves — only the object knows how its own slots are stored. What still refuses is a position that keeps
**one word** for a whole value (`n + 1`, `abs(n)`, `[n]`, `min(n, 3)`): the refusal names what the value is and
where it came from, and roadmap L11.1 owns the missing word. A *name* bound from a slot carries its tag with
it, and a refusal never claims a value "comes from a loop" unless a loop bound it (Gap R.38).

### A container variable asks its last binding what it is (L11.1, Gap Q.1, ADR 0341)

A name is not an object. `d = {1: 10}` then `d = [3, 4]` frees the dict, builds a list and stores the new
handle — and a read of `d` then asks the compiler's own notebook, kept by *name*, about a kind that statement
just replaced. One door decides all of a name's container facts at the binding that takes the name over: the
kind, whether its slots describe themselves, and what those slots hold.

```python
d = {1: 10}
d = [3, 4]
print(d)          # [3, 4]        (it printed {3: 4, 0: 0} before ADR 0341)
print(d[0])       # 3             (it printed 0)
print(d[-1])      # 4             (it died KeyError: 1)
d.append(9)
print(d)          # [3, 4, 9]
print(d[9])       # IndexError: list index out of range — the LAST binding's container names the trap

s = {1, 2}
s = [5, 6]
print(s)          # [5, 6]

xs = ["a"]
xs = [1, 2]
print(xs)         # [1, 2]  — a literal writes every slot again, so the freed object's element kinds are gone
```

The rule is the one ADR 0172 wrote for `None`, ADR 0257 for `bool`, ADR 0267 for a pair's tag and ADR 0270 for
an interned text: the variable's **latest** assignment decides how a read lowers, and a binding retires what
contradicts it — never more than that. A name bound *once* to one literal keeps the record that lets its slots
be read statically (ADR 0241), and a rebinding of a different kind takes that record away with the object it
described. Two alias names for one object are a separate matter (a rebinding frees what shares its handle) and
belong to roadmap **Gap R.257**; a kind that only the run time can describe — a parameter, a value that
crosses a call — belongs to L11.1's tagged roads.

### A name bound from a container slot can be used as a number

A slot the program built at run time carries its kind beside its payload, so a name bound from one keeps that
kind and can be asked for a number (ADR 0303's binding, ADR 0304's door):

```python
xs = []
xs.append(7)
n = xs[0]
print(n - 1)     # 6
print(-n)        # -7
print(n // 2)    # 3
xs = []
xs.append(2.5)
n = xs[0]
print(n - 1)     # 1.5 — a float slot keeps its float
xs = []
xs.append("a")
n = xs[0]
print(n - 1)     # TypeError: unsupported operand type(s) for -: 'str' and 'int'
```

The operators that answer a number or raise (`-`, unary `-`, `//`) ask the tag whatever the slot holds, and the
raise is the reference's own sentence naming the kind the slot really holds — catchable by the `except` arm the
program wrote. `+`, `*` and `%` are the exception, and an honest one: CPython answers `"a" + "b"`, `[1] * 2` and
`"%d" % 3`, and this backend builds the first two of those from a slot only where a pass proves the slots hold
numbers (roadmap Gap R.82), so those positions refuse in words rather than raise where the reference answered a
value (roadmap L11.1, Gap R.146). `%` on a **text** is a different operator and does not ask this question at
all: it is the formatting, and it renders a slot's value through the printer, whose answer carries the slot's
kind (Gap R.31, ADR 0360).

The name reaches the **double** domain the same way — by its tag, not by a cast (ADR 0305, closing roadmap
Gap R.148):

```python
xs = []
xs.append(7)
n = xs[0]
print(n / 4)      # 1.75
print(n >= 7)     # True
print(n + 2.5)    # 9.5
xs = []
xs.append(7.5)
n = xs[0]
print(n / 2)      # 3.75 — a float slot unboxes, it does not read its handle as a number
xs = []
xs.append("a")
n = xs[0]
print(n > 7)      # TypeError: '>' not supported between instances of 'str' and 'int'
print(7 > n)      # TypeError: '>' not supported between instances of 'int' and 'str'
```

An ordering names **both** operand types in source order, which is one of the reasons a pair enters through
the tag-reading doors rather than through a widening: a lift would compare the interned index of `"a"` as a
number and answer `True` where the reference stops the program.

A name the pair road bound is also a **container element** now (ADR 0306). A list literal whose element is such
a name is built on the heap, and that element writes both of its words — the payload and the tag the objects
made — so the slot says what it holds:

```python
xs = []
xs.append(7)
n = xs[0]
print([n])                    # [7]
print([n, "x", 2, None])      # [7, 'x', 2, None]
xs = []
xs.append("a")
n = xs[0]
print([n])                    # ['a'] — the tag, not the interned index
y = [n]
print(len(y), 7 in y, str(y)) # 1 True [7]
```

The same rule covers a float, `None`, a `bool` and a nested container in that element position, and the list
bound to a name keeps working through every read — `len`, subscript, `in`, `append`, `for`, `str`, `repr`, `==`.

**An element that is itself a read is stored as the pair that read answers** (ADR 0332). The print door, the
binding door, the membership door and the length door have always asked a subscript for two words — a payload
and the tag that says what it means; the container builder alone asked for one, and answered `[1, 2]` for
`['x', 2]`. It asks for the pair now, taken from the same call `print(xs[i])` emits, so a slot the builder
wrote and the same slot the printer reads cannot mean different things:

```python
print([{"a": "x"}["a"], 2])          # ['x', 2] — was [1, 2], the text's @str_tab position
print([["a"][0], 2])                 # ['a', 2]
print([[1.5, 2][0], 3])              # [1.5, 3]
xs = [1, "a"]
print([xs[1], 2])                    # ['a', 2]
print([xs[0], xs[1]])                # [1, 'a']
xs = {"a": "x"}
print([xs["a"], 2])                  # ['x', 2]
print({1: {"a": "x"}["a"]})          # {1: 'x'} — a dict entry, a dict key and a set member ride the same door
print({{"a": "x"}["a"]: 1})          # {'x': 1}
print({{"a": "x"}["a"]})             # {'x'}
xs = []
xs.append([7, 8])
print([xs[0][1], 0])                 # [8, 0] — a slot of a container the program built
deep = {"a": {"b": "z"}}
print([deep["a"]["b"], 1])           # ['z', 1] — the chain is one pair per level
```

The compile-time lookup keeps exactly the shape it could read — `[[1, 2][0], 3]` is `[1, 3]` and emits a
constant, allocating nothing and asking none of the object doors — and a raise inside an element is the
program's exception rather than the compiler's: `[({"a": 1})["b"], 2]` raises the catchable `KeyError: 'b'`,
`[xs[5], 1]` the catchable `IndexError: list index out of range`, `[xs["a"], 1]` the `TypeError: list indices
must be integers or slices, not str` (Gap R.222's gate, reached through the new door). What stays refused is
the position that keeps one word for a whole value — `[id({"a": "x"}["a"]), 1]` still refuses with Gap R.146's
sentence — and what stays wrong, filed rather than hidden, is an element that *contains* a read rather than
being one (Gap R.225: `[str({"a": "x"}["a"]), 1]` prints `[1, 1]`), the loop that unrolls a literal directly
(Gap R.226: `for v in [{"a": "x"}["a"], 2]:` prints `1` where the bound form prints `x`) and a subscript of a
subscript of a literal (Gap R.227). The fourth row ADR 0332 filed — this same list crossing a `return`,
recorded as answering `0` — answers `['x', 2]` as of ADR 0333, and is asserted beside the element door that
always got it right.

An **f-string field** asks the same question and gets the same answer (ADR 0307): the field goes through the
module's one tag-reading printer — the door `print()`, `str()` and `repr()` use — so the field, `str(n)` and
`print(n)` cannot disagree about `6` versus `6.0`, and a text field prints its text rather than its interned
index:

```python
xs = []
xs.append(7)
n = xs[0]
print(f"{n}")          # 7
print(f"[{n}]")        # [7]
print(f"{n - 1}")      # 6
xs = []
xs.append("a")
n = xs[0]
print(f"x{n}y")        # xay
print(f"{n!r}")        # 'a' — `!r` asks the same printer with the quote flag
x = 2.5
print(f"{x!r}")        # 2.5 — a conversion of a variable used to print nothing at all
```

What still refuses is a position that keeps one word for the value — `float(n)`, `min(n, 3)`, a literal
`sum`/`min`/`max` folds into a static array (`sum([n])`), a pair handed across a call into a body whose
parameter is not a pair (`Gap R.154`) — all owned by Gap R.146, each refusing in a sentence that names the
missing half. An f-string used as a **value** is no longer in that list: since **ADR 0359** an interpolation is
an ordinary text (see *An interpolation is a text, not only a printing instruction* below). A **dict entry and a set
member** are not in that list any more (ADR 0310): the dict and the set each have one builder that takes the tag
with the payload, so `print({"k": n})` is `{'k': 'a'}`, `print({n: 1})` is `{'a': 1}`, `print({n})` is `{'a'}`
and everything that reads the bound object back asks the same tag — `d = {"k": n}` answers `d["k"]`, `len(d)`,
`k in d`, `d[k]`, `str(d)`, `d == {"k": "a"}` and a `for k in d`, and `s = {n}` answers `7 in s`, `len(s)` and a
`for v in s`. A key or a member that cannot be hashed raises CPython's `TypeError: unhashable type: 'list'`, catchable
by the program's own `except TypeError:` (`Gap R.81`, ADR 0228).

### An interpolation is a text, not only a printing instruction

`print(f"x{n}")` has answered since this backend had one. The same source standing where a **value** goes now
answers too (**ADR 0359**, roadmap L11.1 / Gap R.146's value half): an f-string builds its parts in the runtime
string table and hands back an `@str_tab` index — the very word a text literal answers — so every road that
takes a text takes it, without knowing what built it.

```python
n = 3
s = f"x{n}"
print(s)                    # x3
print(f"a{n}" + "!")        # a3!
print(f"a{n}" + f"b{n}")    # a3b3
print(f"v={n}".upper())     # V=3
print(len(f"a{n}b"))        # 3
print(f"a{n}" == "a3")      # True
print(f"a{n}b"[1:])         # 3b
def greet():
    return f"hi {n}"        # a return, a method's return, a text parameter…
print([f"a{n}", 1])         # ['a3', 1]      — a mixed container, each slot its own kind
print({f"k{n}": 1})         # {'k3': 1}      — and a key, and a member, and a `for c in f"ab{n}"`
```

A field is rendered by the module's **one** tag-reading printer — the door `str()`, `repr()` and `print()`
share (ADR 0258, ADR 0303) — so a field, `str(x)` and `print(x)` cannot disagree about `7` versus `7.0`, and
nothing interpolates a `@str_tab` index, a float box handle or a heap slot. Two fields used to do exactly that,
because the print road builds one printf and can hand it an integer or a byte pointer: `print(f"v={None}")`
wrote `v=0` and `print(f"{xs}")` wrote `0` for `[1, 2]`. Both write the reference's bytes now, and they write
them whether the interpolation is printed directly or bound first — `print(f"{xs}")` and
`t = f"{xs}"; print(t)` are one answer.

An interpolation the AST already states is constant is **folded** — `f"a{1}b"` *is* `"a1b"`, and the fold is one
function asked by the three roads that would otherwise disagree about it (`value`, `stringVal`, and a container
literal's element-kind census). Quoting is not folded: `f"{'a'!r}"` goes to the printer, because choosing the
quote character is the printer's job (`Gap R.192`).

A **loop** vouches for the numbers it binds itself, and stops at the loop's end:

```python
for i in range(2):
    print(f"x{i}")           # x0, x1 — the counter's digits, written by the same helper `str()` uses
for i in range(2):
    i = i * 100
    print(f"x{i}")           # refused: the value is no longer the loop's alone
print(f"x{i}")             # refused: no claim survives the loop that earned it
```

What still refuses, in words, with a row apiece: a **format spec** over a value the compiler cannot read
(`f"{n:>6}"`, `f"{v:.2f}"`, `f"{10:#x}"` — ADR 0299's engine reads literals, and the runtime half is **Gap
R.308**; a spec over a literal answers, `print(f"{3.5:.2f}")` is `3.50`), the string methods whose run-time road
has not been built (`f"a{n}b".replace(...)`, `.split(...)`, `.center(...)`) and repeating a text,
`f"x{n}" * 2` (**Gap R.33**).

The same two words travel through the **mutation roads** — the statements that change a container the program
already built (ADR 0311):

```python
xs = []
xs.append(2.5)
n = xs[0]
ys = []
ys.append(n)          # [2.5] — the append takes the payload and the tag together
ys[0] = 1
print(ys)             # [1]
st = set()
st.add(n)
print(7 in st, len(st))   # False 1 — dedup reads both words
d = {}
d["k"] = n
d[n] = "v"            # the pair as key and the pair as value, in one program
print(d["k"], d[2.5], len(d))   # 2.5 v 2
xs2 = []
xs2.append([1, 2])
c = xs2[0]
st2 = set()
st2.add(c)            # TypeError: unhashable type: 'list'
d2 = {}
d2["k"] = xs[0] / 2     # refused: the value is not a pair, and the ordinary road is asked first
```



## Tuples are values (L11.3, ADR 0361)

A tuple is a **value**: an object that prints itself, measures, indexes, unpacks, compares, hashes, and
refuses — in words, at build time — every write the reference also refuses. It is not a spelling the parser
hands to a list road, and the compiled backend does not render one as a list.

```gusty
t = (1, 2, 3)
print(t)            # (1, 2, 3)
print((1,))         # (1,)      — the comma is what keeps a one-element tuple a tuple
print(())           # ()
print((1, "a", None, True, 2.5))
print(t[0], t[-1], len(t))          # 1 3 3
a, b = (4, 5)
print(a, b)                         # 4 5
p = (1, ("a", "b"), [1, 2])
print(p[1][0], p[2][1])             # a 2
print(2 in (1, 2, 3))               # True
print((1, 2) == (1, 2))             # True
print((1, 2) == [1, 2])             # False — a tuple is not a list, in either direction
print((1, 2) == (1, 2.0))           # True  — an int and a float are the same number
print(str((1, "a")))                # (1, 'a')   — str() and repr() ask print's door
def pair():
    return (1, 2)
print(pair())                       # (1, 2)
d = {(1, 2): "a"}
print(d[(1, 2)])                    # a
print((1, 2) in d)                  # True
```

**One layout, one bit.** The compiled heap has one container layout (ADR 0232), and a tuple is that object
with the immutable-container bit set in its `@estr[h]` attribute word, beside the bits that claim interned
text and self-describing slots (ADR 0187/0251). The printer, the equality door, the slice door and the dict
key road all ask **the object**, because the expression that built a container is not the thing that knows
what the container is. That is why `p = mk(1, "x")` followed by `print(p)` prints `(1, 'x')`: the name holds
a handle, and the handle answers the question. A tuple literal is therefore always heap materialised — the
compile-time global shape has no attribute word to carry the fact.

**What a slice keeps.** A copy carries the object's own attributes, so `t[1:]`, `t[::-1]` and `t[0:0]` are
tuples, print with parentheses, and compare as tuples (`t[1:] == (2, 3)` is `True`). Roads that build a copy
*without* an attribute word — `sorted(...)`, `reversed(...)`, the dict views — keep their specific printers
rather than asking the object dispatcher: a dispatcher that trusts a missing word prints the wrong brackets.

**What refuses, in words.** Immutability is a build-time refusal, not a silent write, because the shared
layout *could* carry the write and the answer would then be one the reference never produces:

| program | the compiled backend says | the reference raises |
|---|---|---|
| `t.append(3)` | refuses, quoting | `AttributeError: 'tuple' object has no attribute 'append'` |
| `t[0] = 9` | refuses, quoting | `TypeError: 'tuple' object does not support item assignment` |
| `t.sort()`, `t.pop()`, `t.clear()`, `t.insert(0,1)`, `t.remove(1)`, `t.extend(x)`, `t.reverse()` | refuses, quoting | the same `AttributeError` family |

…where a tuple literal used to stop the build with `unsupported expression *lang.Tuple` for every one of
those lines (the sentence **Gap R.288** files), and `()` did not parse at all.

**Traps carry the reference's class and sentence.** `t[5]`, `(1, 2)[5]`, `t[-3]` and a subscript under a
nested read all raise `IndexError: tuple index out of range` — the tuple's own sentence, taken from the
object when the road holds only a handle and from the node when the road can see the source. A missing key
raises `KeyError: (3, 4)` with the key's repr, and an unhashable element inside a key names the inner type:
`{(1, [2]): 1}` says `'list'`.

**What is still owed.** Sorting a container whose *elements* are containers refuses rather than printing the
order they arrived in (`Gap R.309`, with the ordering operators between containers); `count`/`index` are the
list-method table (`Gap I.2`) for a tuple exactly as for a list; `() is ()` prints `False` where CPython prints
`True`, because the reference keeps one empty tuple and this backend mints one per literal (`Gap R.310`);
and reading **one element** out of a container the compiler cannot kind — `def mk(a,b): return (a,b)` then
`print(mk(1,"x")[1])` — is the one-word-position family (`Gap R.146`), wrong for a list in the same way, and
parity-guarded so a fix cannot land on one leg alone.

## Builtins

Names are resolved by the checker before codegen runs. Every built-in call name comes from
one table (`pkg/lang/predeclared.go`), which the checker predeclares, the language server
offers in completions, and the AOT codegen consults before lowering a bare name — so a real
built-in is never reported as "undefined", and a name that is *not* bound is a front-end
error with a span rather than an invalid LLVM module:

    print(undefined_thing)
    → error at 1:7: undefined name "undefined_thing"        (exit 1, the compiled backend)

If a built-in exists in the language but cannot be lowered yet, codegen refuses with an
actionable message naming the backend that does support it (ADR 0166), e.g.
`list(<container>) copies are not supported in the AOT backend yet; the record supports
them — build the container with list() and add elements`.

`type` is a reserved word (annotations), so `type(x)` is not a call in this grammar and is
not offered in completions.

- `print(x, ...)` — writes each argument to stdout on its own line via
  `printf`. Multi-argument `print` mirrors the record: one `printf` per
  argument. String arguments (literals, folded string calls like `str(7)`,
  and constant `+` concatenations) use a `%s\n` format; integer arguments
  use `%d\n`. Zero-argument `print()` writes nothing (no `printf`), matching
  the record's no-op.
- `range(n)` — iteration bound for `for` loops.
- `str(x)` — the value's own text, returned as a string value: printable, comparable,
  subscriptable, case-foldable (`s = str(xs)` then `print(s.upper())` works, ADR 0224/0257).
  In codegen a readable number folds to its decimal constant, a float to Python's
  round-tripping text, a container to its own printer's output, and `len(str(...))` folds.
- `repr(x)` — the value's written form, from the **same renderer**: identical to `str(x)` except
  for a text, which comes back quoted (`repr("hi")` is `'hi'`). Registered as a string-returning
  builtin, so `print(repr(x))` writes the text and not the intern table's position (roadmap L11.2,
  ADR 0258, closing Gap L.2).

## Generators & lists
- `def g(): yield a; yield b` is a generator: calling `g()` runs the body and
  returns a list of all yielded values.
- `[1, 2, 3]` is a list literal.
- `for x in g():` and `for x in [1,2,3]:` iterate the elements.

- `len(list)` returns the number of elements in a list.

## Strings

String literals (`"..."`) evaluate to boxed strings on the record:

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

Float literals (`1.5`, `2.0`) evaluate to boxed floats on the record.

Arithmetic with floats (or float + int) produces a float:
`+`, `-`, `*`, `/`. Comparisons (`== < <= > >=`) work between floats and ints.
`print` renders floats with `%g`.

the record applies `-`/`*` to the float64 payload of boxed floats
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

Comprehensions over a `range(...)` work on the compiled backend.

In the AOT LLVM codegen, a comprehension over an inline list literal or a constant `range(...)`
folds, and **a folded set/dict comprehension is the literal it denotes** (ADR 0234): the `@.setN` /
`@.dictN` global is emitted *from* the `SetLit` / `DictLit` the fold produced and never escapes into
a value position — a binding, a `print`, an `in` test or a call argument takes the literal's own
lowering, which allocates a heap object and writes each slot with its tag. Set comprehensions
unroll the iteration and deduplicate folded elements; dict comprehensions fold key/value pairs.
Both are usable with `len(...)` via the `compLen` map, mirroring the record's semantics. A
comprehension whose iterable is a runtime container walks it with the same real loop `for` uses.

**A brace display ends at its brace, and a comprehension's element is one value**
**(both legs) (ADR 0244)** — Two rules, one row of output. *A `{…}` display* — a
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
This mirrors the record's `Index` handling for dict lookup and set
membership.

### min / max / abs

Standard-library numeric builtins:

    min([3, 1, 2])   # 1
    max([3, 1, 2])   # 3
    min(1.0, 2)      # 1.0
    max(1, 2.5)      # 2.5
    min("b", "a")    # a
    max([1, 2.5])    # 2.5
    max([True, 0])   # True   — the chosen candidate decides what prints (ADR 0261)
    max([True, 1])   # True   — the tie keeps the first candidate
    max([1, True])   # 1      — …so this one keeps the number
    abs(-5)          # 5
    abs(-2.5)        # 2.5    — a float keeps its kind
    abs(True)        # 1      — a verdict is a number
    abs("hi")        # TypeError: bad operand type for abs(): 'str'   — no sign to take (ADR 0271)

`min`/`max` accept one list or set, one scalar, or values written side by side; `abs` takes one number, and
names its operand's kind when it is not one. 
- The call chooses the **candidate**, not the comparison that found it, so the answer's kind is the
  winner's own kind — the rule ADR 0256 wrote for numbers and ADR 0261 finished for verdicts.
  `min(1.0, 2)` is the float `1.0`; `min(2.5, 1)` is the integer `1`, not `1.0`.
  A tie keeps the first candidate (`min(1, 1.0)` is `1`, `max([True, 1])` is `True`, `max([1, True])`
  is `1`). The rule holds whether the candidates are literals or settled variables **of one kind**, and
  the same rule decides the one-container spelling (`max([1, 2.5])` is `2.5`, `min([1, 2.5])` is `1`).
  Text candidates are ordered by their content through `rt_str_order`, never by their position in the
  intern table (ADR 0248).
- **What the winner is, everything follows.** `print`, `str()`, `repr()`, an f-string, the tag a container
  slot carries and `--json`'s `"type"` all ask the same question about the chosen candidate, so
  `str(max([True, 0]))` is `True` and `print([max([True, 0])])` is `[True]` on both legs — while the
  number stays underneath, because a verdict is the number every numeric path reads from it:
  `max([True, 0]) + 1` is `2` and `max([True, 0]) == 1` is `True` (ADR 0259's int/bool/float family).
  A candidate the compiler cannot read — a name it has not folded — leaves the winner to a run-time
  `select` with nothing beside it to name its kind, and that line prints the number (roadmap Gap R.124).
- **A ternary follows the same rule where the test is a value the source wrote**: `print(False if 1 else
  2)` is `False`, `print(1 if 0 else False)` is `False`, `print(True if "" else 2)` is `2`. When the test
  must be evaluated, neither arm is known to run and a ternary is a verdict only if *both* arms are
  (ADR 0257) — the shape still printing the number underneath is roadmap Gap R.125.
- A text candidate beside a number, and a `None` or container candidate beside a number, raises CPython's
  `TypeError` with the operator that actually failed: `min` names `'<'` and `max` names `'>'`. A verdict
  candidate names `'bool'`, not `'int'` — `max(["a", True])` raises `'>' not supported between instances
  of 'bool' and 'str'` on both legs (ADR 0261, matching ADR 0259's tag). The raise is
  catchable on both legs. Two containers side by side are CPython's element-wise ordering, which this
  language does not implement yet (roadmap Gaps R.86, R.97): the record raises, the compiler refuses.
  Two runtime candidates whose kinds only the object can reconcile still wait
  for the tagged value word (roadmap Gaps R.107–R.110), and so does a settled `float` beside a settled `int`;
  those refuse in words rather than promote the winner to a double or compare untagged payloads.
- The AOT codegen treats a single **numeric** scalar argument to `min`/`max` as a
  one-element collection: `min(5)` -> 5, `max(7)` -> 7 (see ADR 0110). A lone text or `None` is the
  record's one-element collection too, and the compiled half refuses it (Gap R.107/R.108's family).
Implemented in the compiled backend — the LLVM AOT codegen that every path, the REPL and `--eval` included, goes through (ADR 0302).
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
all matching the record.
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

**A text is a sequence of code points** (roadmap L11.5, which closed Gap N.2; ADR 0225, ADR 0229, ADR 0362).
Its value is the source's own UTF-8 bytes, so a text survives lexing, containers, interning and printing
byte for byte; what is measured in *characters* is every question the program asks of it.
`len("café")` is `4`, `"héllo"[1]` is `"é"`, `"héllo"[-1]` is `"o"`, `"héllo"[1:3]` is `"él"`, and
`for c in "aé"` binds `"a"` then `"é"` — each of those on a text the compiler can read AND on one it cannot
(a parameter, an element read out of a container, the answer of a call). An out-of-range index raises
`IndexError`; `s[i]` answers a one-character text, so `s[1][0]`, `len(s[1])` and `ord(s[1])` all work on it.

### Case and trim

`upper`, `lower`, `title`, `capitalize`, `swapcase` and `strip`, `lstrip`, `rstrip` answer the reference's
answers, on both roads — the compile-time fold over a text the compiler can read, and the runtime door over
a text it cannot (roadmap L11.5, ADR 0362). They are not this compiler's opinions of case:

    "straße".upper()   # STRASSE      not straße: one character answers two
    "ﬁ".upper()        # FI           a ligature is two characters to the reference
    "İ".lower()        # i + U+0307   one character answers two
    "ΑΣ".lower()       # ας           the last sigma of a word is ς, and ασ when a cased letter follows
    "é.é".title()      # É.É          a word begins after any uncased character, not only after a space
    "ǅ".title()        # ǅ            a titlecase character's title form is itself
    "1a".capitalize()  # 1a           the FIRST character is title-cased, and here it is a digit
    "\x1cx".strip()    # x            the reference strips U+001C..U+001F, which Go's set does not
    "ßﬁ".strip()       # ßﬁ           non-ASCII text is content, not whitespace
    "café x".strip("c")  # afé x        a cutset is a set of characters, read as code points

The answers come from `pkg/lang/testdata/case-table.json`, which `tools/casetab` generates by asking
CPython every code point (`go run ./tools/casetab -check` re-asks it and fails if the file on disk is not
what the reference says). One artifact, two readers: the fold and the emitted IR door both walk it, so the
same text cannot answer one way in a literal and another way in a parameter. A byte that is not a
well-formed UTF-8 sequence is carried through unchanged rather than reinterpreted, and the tables a module
never asks are not emitted into it (ADR 0354).

String equality `==` / `!=` compares contents in both paths (`"abc" == "abc"`
is 1, `"abc" == "abd"` is 0), constant-folded in the codegen.
String indexing `"abc"[1]` returns the one-character text `"b"` (ADR 0225), and `ord("b")` is where the
number 98 comes from.
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
absent, in **both** paths: the record applies `strings.Index`; the
codegen constant-folds it to an `i32` literal when the receiver and argument
are string constants (`print("abcabc".find("bc"))` emits `i32 1`).
`"s".ljust(w)` pads the receiver on the right with spaces to width `w`;
`"s".rjust(w)` pads on the left; both are no-ops when `len(s) >= w`.
In the AOT codegen, both fold over a constant string receiver and constant
width to a padded string global, mirroring the record.
`"s".index(sub)` folds to the byte index of `sub` in the receiver
`"s".expandtabs(w)` folds to a string global: each tab is replaced with the
`{k: v}.get(key, default)` folds to the matching value or the default in the
AOT codegen: an int/string key looks up the constant dict's key/value pairs
(not-found returns the default when given).
spaces to the next tab stop at width `w` (running-column algorithm). Source
string literals have no escape sequences, so literal receivers contain no
tabs (no-op); the record can build tabs via `chr(9)`.
(`strings.Index`); the record raises on not-found, but the AOT codegen
has no error channel, so it folds to `-1` on not-found (like `find`).
`"s".zfill(w)` pads the receiver on the left with `0` to width `w` (no-op
when `len(s) >= w`); the AOT codegen folds it over a constant receiver and
constant width to a zero-padded string global, mirroring the record.
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
`str.count(sub, start, end)`. the record applies `strings.Count` on the
sliced substring (`start`/`end` clamped to `[0, len(s)]`); the AOT codegen
constant-folds only the one-argument form to an `i32` literal when the
receiver and argument are string constants (`print("ababab".count("ab"))`
emits `i32 3`).

`.rfind(sub)` returns the index of the last occurrence of `sub`, or -1 if
absent, in **both** paths: the record applies `strings.LastIndex`; the
codegen constant-folds it to an `i32` literal
(`print("abcabc".rfind("bc"))` emits `i32 4`).

`.capitalize()` answers the reference's answer — the FIRST cased character title-cased, the rest
lower-cased — on both roads: the compile-time fold and the runtime door read the same generated
table (ADR 0362), so `print(len("hello".capitalize()))` emits `i32 5` and
`"straße".capitalize()` is `"Straße"` whichever road the receiver came in on.

`.title()` title-cases every word — a word boundary is "the previous code point was not cased", not
"the previous character was a space", so `"ab-cd".title()` is `"Ab-Cd"` — on both roads, from the
same table (ADR 0362); `print(len("hello world".title()))` emits `i32 11`.

`.swapcase()` swaps the case of every code point on both roads — the Greek final sigma included, so
`"ΑΣ».swapcase()` becomes `"ασ»` while `"ΑΣ".swapcase()` is `"ας"` — from the same table
(ADR 0362); `print(len("HeLLo".swapcase()))` emits `i32 5`.

None of the five is `strings.ToUpper`, `strings.ToTitle`, `strings.Map(unicode.ToUpper, …)` or any
other Go case routine, and the reason is measured rather than asserted (ADR 0362): Go answers
`strings.ToUpper("straße")` as `STRAßE` where the reference answers `STRASSE`, `strings.ToLower("ΑΣ")`
as `ασ` where the reference answers `ας` (it has no final-sigma rule), and `strings.ToLower("İ")` as a
bare `i` where the reference answers `i` + U+0307. `strip`, `lstrip` and `rstrip` read the same
artifact's whitespace set, also probed from the reference (`c.strip() == ""`) rather than imported from
`unicode.IsSpace` — which differs: Go does not count U+001C..U+001F as whitespace and the reference does.

List `.count(value)` returns the number of occurrences of `value` in the
list (interpreter path; elements compare by string content via
`dictKeyEq`, matching dict keys). `["a", "b", "a"].count("a")` is 2.

`.isdigit()` returns 1 if every rune is a digit (and the string is
non-empty), else 0, in **both** paths: the record checks
`unicode.IsDigit`; the codegen folds it to `i32 1`/`i32 0`
(`print("123".isdigit())` emits `i32 1`).

`.isalpha()` returns 1 if every rune is alphabetic (and the string is
non-empty), else 0, in **both** paths: the record checks
`unicode.IsLetter`; the codegen folds it to `i32 1`/`i32 0`
(`print("abc".isalpha())` emits `i32 1`).

`.islower()` / `.isupper()` return 1 if there is at least one cased rune
and all cased runes are lowercase / uppercase, else 0, in **both** paths:
the record scans cased runes; the codegen folds to `i32 1`/`i32 0`
(`print("abc".islower())` emits `i32 1`).

`.partition(sep)` returns a list `[head, sep, tail]` split at the first
occurrence of `sep` (or `[s, "", ""]` when absent) on the record
path. `"a-b-c".partition("-")[0]` is "a".

`.isalnum()` returns 1 if every rune is alphanumeric (and the string is
non-empty), else 0, in **both** paths: the record checks
`unicode.IsLetter`/`unicode.IsDigit`; the codegen folds it to
`i32 1`/`i32 0` (`print("abc123".isalnum())` emits `i32 1`).

`.isspace()` returns 1 if every rune is whitespace (and the string is
non-empty), else 0, in **both** paths: the record checks
`unicode.IsSpace`; the codegen folds it to `i32 1`/`i32 0`
(`print("   ".isspace())` emits `i32 1`).

The `sorted(list)` builtin returns a new sorted list; the argument keeps its own
order, because `sorted` sorts a copy. `sorted(iter, reverse=True)` returns descending
order; `reverse=False` keeps ascending order. (With no `key=`, this language also
accepts a documented constant truthy second positional argument as its repeat flag.)
`list.sort()` sorts in place and returns `None`; `list.reverse()` reverses
in place and returns `None`. The two **methods** still take no `key=`/`reverse=`
argument — `sort(key=...)` has not been given the key road the **builtin** was given in
ADR 0340, and refuses naming that reason (roadmap L11.7, **Gap R.269**; ADR 0191). The **builtin** `sorted` now reads
`key=` and `reverse=` by name and calls an allowed key through the ordinary call road
(roadmap L11.7; Gap R.249; ADR 0340), including a `key=lambda …` hook, which **ADR 0345** gives a
declaration of its own so the door that calls a named callable has a name to call, and which **ADR 0363**
registers with the scan that decides a parameter's kind, so the hook's body may ask the element doors.

**A class's own members are read through the class name** (roadmap **Gap R.297**, **ADR 0418**). A class body runs, and
what it bound becomes an attribute of the class — the reference reads it, and so does this backend:

```gusty
class C:
    first = 1
    label = "hi"

print(C.first)            # 1
print(C.label)            # hi
print(C.first + 1)        # 2
print(C.label + "!")      # hi!
C.first = 5
print(C.first)            # 5
```

That is a different road from an instance attribute (`self.x`, `inst.x`), which lives in the object's slot array; the two
are pinned as neighbours so opening one cannot move the other. What the road will not answer is refused in words naming
the member's kind rather than printed as a payload: a fractional member, a verdict member, a void member, a container
member, a member bound by a call, and a member the body never bound are roadmap **Gap R.431**.

**A dict sorts its keys** (roadmap **Gap R.426**, **ADR 0417**). `sorted(d)` is the reference's `sorted(d)`: it answers
a list of the dict's KEYS, in the reference's order, and leaves `d` and its entry order alone —

```gusty
d = {"pear": 1, "apple": 2}
print(sorted(d))                  # ['apple', 'pear']
print(sorted(d, reverse=True))    # ['pear', 'apple']
for k in d: print(k)               # pear, apple    — the dict is untouched by the sort
xs = sorted({"b": 2, "a": 1})
print(xs, len(xs), xs[0])          # ['a', 'b'] 2 a
```

The answer comes from the dict, not from the text that built it: the door asks the record of the keys, which is what lets
a dict the program grew by item stores (`g = {}` then `g["q"] = 1`) answer too. The keys' kinds also decide how they are
ordered and how they print, from one record, so a verdict key orders as a number and prints `True`. What the record cannot
name is refused rather than ordered by a rule the language does not have — keys of more than one kind (the reference raises
`TypeError: '<' not supported between instances of 'str' and 'int'`), a fractional key, and a key the compiler holds no kind
for; each names the missing half and roadmap **Gap R.429**. A SET is a separate case that is still owed: `sorted(s)` echoes
the set's own order today, and that is roadmap **Gap R.430** — as is `for k in sorted(d)`, which the loop road still refuses
(**Gap R.247**).

**What used to be wrong with the shape, and what it took to see it** (roadmap **Gap R.258**, paid by
**ADR 0343**): `x = sorted([3, 1, 2], reverse=True)` followed by `x` died by `SIGSEGV` inside `@rt_alloc` while
`print(sorted([3, 1, 2]))` answered — because the variable's stack slot, hoisted to the top of the function for
the root stack, had no value in it before the release-the-old-binding prologue read it, and the garbage handle it
read went into the allocator's free list. It reproduced only when other cases were running beside it: alone, the
frame word happened to be 0, and a release of 0 is skipped. The hoisting pass now gives every variable slot it
moves a defined value, so a binding reads "no object yet" rather than the last call's leftovers.

Ordering is defined per element kind, and the compiled backend implement the same rules:
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
names Python's answer. Sorting is stable: the unkeyed runtime sort is a stable insertion
sort, and the keyed ordering ranks each element by the keys that come before it, with earlier
equal keys held ahead. The keyed door answers the reference's stable order, so `sorted([2, 1, 2, 1], key=abs)`
is `[1, 1, 2, 2]` (roadmap L11.7; Gap R.249; ADR 0340).

In the AOT codegen, `sorted` of an all-integer inline list literal is still
constant-folded to a sorted list global; anything else — a variable, a list of
strings, an empty list — is built as a runtime list and sorted by `rt_sort`, with
`rt_list_copy` first when the argument is a variable. `sorted(xs)` assigned to a
name binds a container, so `print`, indexing, `len` and `for` all work on it.
`reversed(x)` returns a
reversed copy of a list or string: `reversed([1, 2, 3])` -> `[3, 2, 1]`,
`reversed("abc")` -> `"cba"`. On a non-literal argument the compiled backend copies the list
with `rt_list_copy` and flips it with `rt_reverse` — `for v in reversed(xs)` and
`list(reversed(xs))` both answer `3, 2, 1` — while `print(reversed(xs))` still renders the
argument untouched against the reference's `<list_reverseiterator …>` (roadmap Gap R.251);
see ADR 0082, widened by ADR 0339.
`enumerate(x)` returns a list of `[index, value]` pairs for each element of a
list: `enumerate([10, 20, 30])` -> `[[0, 10], [1, 20], [2, 30]]`. It is
interpreter-only (the AOT codegen folds builtins only on literal args; nested
list construction is not yet lowered); see ADR 0083.
`zip(x, y)` combines two lists into a list of `[a, b]` pairs, stopping at the
shorter list: `zip([1, 2], [10, 20])` -> `[[1, 10], [2, 20]]`. It is
interpreter-only (the AOT codegen folds builtins only on literal args; nested
list construction is not yet lowered); see ADR 0084.
`for x in "abc"` iterates over each character of a string (yielding a
single-char string per rune), **in the compiled backend** (`programs/for_string_chars.gy`).
The compiled loop unrolls one body copy per character and stores each one the way a
container slot does — as its interned index — so `for c in "ab"` and a literal list of
strings compile and print like the interpreter and CPython do (ADR 0208; it used to be
interpreter-only, ADR 0085, because the unrolled store handed a string global to an `i32`
slot and LLVM rejected the module).

A character is a one-character **string**, not a distinct char type, and a text measures in characters:
`len("café")` is `4` and `"héllo"[1]` is `"é"` on both roads (roadmap L11.5 closing Gap N.2,
`programs/unicode_text.gy`, ADR 0225, ADR 0362).

### A `for` asks its iterable what it is (Gap R.246, ADR 0339)

A `for` loop asks its iterable **before** choosing a road, and a container the compiler can
write down or name is never read as a repeat count:

```python
for v in sorted([3, 1, 2]):        # 1, 2, 3      — the loop folds the same ordering print(sorted(...)) renders
for v in reversed([1, 2, 3]):      # 3, 2, 1
for v in sorted([3, 1, 2], reverse=True):   # 3, 2, 1
xs = [3, 1, 2]
for v in sorted(xs):               # 1, 2, 3      — a heap copy built by @rt_sort, walked as a list variable
for v in reversed(xs[0:3]):        # 2, 1, 3      — @rt_list_copy + @rt_reverse over the slice's copy
def b(): return [3, 1, 2]
for v in sorted(b()):              # 1, 2, 3      — a user call's container answer, sorted by the runtime
for c in reversed("café"):         # é, f, a, c   — reversed by code point, not by byte
```

Both doors take their ordering from the `sorted` builtin's own comparator (`sortedLiteralElems`
for what the compiler can write down, `@rt_sort` for what only the runtime can measure), so a
program that prints a sorted list and a program that steps over one cannot see two orders
(ADR 0318). `reverse=True` / `reverse=False` are the builtin's own spelling; duplicates come
out stable; an empty iterable steps over nothing. `sorted` and `reversed` are still ordinary
names — a program that defines `sorted` itself shadows the builtin and the loop folds nothing.

What neither door reaches is **refused in words**, naming the half that is missing rather than
falling through to the counter (`for v in sorted([3,1,2])` used to print `3 1 2` at exit 0,
`for v in sorted(xs)` used to emit `icmp slt i32 %_ctr1.ld1, @.lst1` and die in `llc` at exit
2): a dict (`sorted(d)` would order its **keys**, stored as `[key, value]` pairs), a text
through `sorted`, a `range`, a float or mixed list, a heap-list `key=` whose elements only the run
time can hand to the call (**Gap R.254**), a `key=` this backend cannot call (**Gap R.255**), a
reference-style positional key, and a non-builtin call nested inside the argument. The loop's sentence always
carries `roadmap L11.1, ADR 0339` and the words `repeat count`, and names the row that owns the missing half
(`roadmap Gap R.247`–`R.251`, `R.254`, `R.255`), so an agent can tell the loop's refusal from the builtin's own
(`roadmap L11.7` for `key=`/`reverse=`). A callable key over literal elements is no longer one of these refusals
— it is answered by the builtin's own door, and the loop walks what that door builds (ADR 0340).

A loop variable's kind belongs to the loop that bound it: `for v in words` over a text list
claims `v` as interned text **for that loop only**, and the numeric and counter roads take the
claim back, so a second loop over `[1, 2]` with the same variable prints `1`, `2` and not the
string table (the untagged half of **Gap R.252**; the tagged half stays open).

This does not change the language's own reading of `for x in <integer>` as a repeat count
(ADR 0207), nor any loop that already answered: a literal, a string, `range`, a list name, a
slice, a dict's keys, a set's elements, a user function's container answer and a generator are
asserted unchanged, on both witness legs, in `pkg/lang/for_iterable_test.go` and
`integration/programs/for_iterables.gy`.

### The ordering builtins ask their options by name (L11.7, Gap R.249, ADR 0340)

`sorted`, `min` and `max` now read `key=` and `reverse=` as **named options**, not as another candidate
or as the old positional reverse flag:

```python
print(sorted([3, 1, 2], key=abs))                  # [1, 2, 3]
print(sorted([3, 1, -2], key=abs, reverse=True))   # [3, -2, 1]
print(sorted(["bbb", "a", "cc"], key=len))         # ['a', 'cc', 'bbb']
print(min([3, 1, -2], key=abs))                    # 1
print(max([3, 1, -2], key=abs))                    # 3

def flip(x):
    return 0 - x
print(sorted([3, 1, 2], key=flip))                 # [3, 2, 1]
```

A callable key is called by the **ordinary call road**: the door synthesises `key(element)` and lowers it
exactly as it lowers that call anywhere else. A declared `def` that can receive one positional argument is
allowed, including optional parameters (`def bump(x, bias=0): return x + bias`), and allowed builtin keys
include `abs`, `len`, `ord`, `int`, `str` and `repr`. A non-callable key raises CPython's own TypeError, not
a compile refusal, so `except TypeError:` catches it:

```python
try:
    print(sorted([3, 1, 2], key=3))
except TypeError:
    print("caught")       # caught
```

`key=None` is the reference's identity key — the option is removed and the unkeyed road answers. An unknown
keyword is also the reference's raise: `sorted([1], cmp=3)` says `'cmp' is an invalid keyword argument for
sort()`, `min([3, 1], reverse=True)` says `'reverse' is an invalid keyword argument for min()`, and
`max([3, 1], key=abs, reverse=True)` says the same for `max()`. Empty keyed `min`/`max` raise `ValueError:
min() iterable argument is empty` / `max() iterable argument is empty`.

The keyed order is stable because each element's rank counts the keys that come before it and the **earlier**
equal keys, using the same comparator (`@rt_elem_gt`) the unkeyed sorter already had. Placement uses
`@rt_put_elem`; `@rt_set_elem` would bump the length again and print phantom zeros.

What still refuses is named, not guessed: `sorted(xs, key=...)` over a heap list built by the program needs
the value word to carry each element to the call (**Gap R.254**; the same call without `key=` is answered by
`@rt_sort`), a call-expression key still needs the function-*answer* the value word will carry
(**Gap R.255**, roadmap L11.7 — a `key=lambda …` hook answers, since **ADR 0345** declares the lambda and the
ordinary call road answers the call), and a key returning a float needs the comparator the tagged value word
owns (**Gap R.248**). A **builtin** key is called through the ordinary call road too: `key=abs`, `key=len`,
`key=str`, `key=repr`, `key=int`, `key=ord` answered under **ADR 0340**, and **ADR 0390** added the builtins
whose one-argument answer the comparator already orders — `key=round` (the whole number the reference's
one-argument `round` answers: `sorted([3, 1, 2], key=round)` is `[1, 2, 3]`, `max([3, 1, 2], key=round)` is `3`),
`key=bool` (`sorted([0, 3, 0, 1], key=bool)` is `[0, 0, 3, 1]`, and all-truthy elements keep the stable order the
reference keeps) and `key=chr` (`sorted([3, 1, 2], key=chr)` is `[1, 2, 3]`). The door asks the road which
builtins are callable at all, so a key that is not a callable builtin (`key=hash`, `key=bytes`) is refused by the
road's own sentence rather than by one written here, and the builtins that answer a double or a container
(`key=float`, `key=sum`, `key=min`, `key=max`, `key=sorted`, `key=reversed`) keep the **Gap R.248** refusal.
That row also owns the key door's other half, now measured: the door returns the element it picks in **one**
word, so `sorted([2.5, 1.5], key=f)` over float elements is refused — it had printed `[0.0, 0.0]` at exit 0, and
`key=abs` over them reached `llc` at exit 2. The ordinary call road's existing parameter-kind refusals are not
widened by this row.

### A hook key asks the element, and the door answers for it (L11.7, Gap R.264, ADR 0363)

The key is called by a call the program never wrote: the door synthesises `key(element)` at lowering time.
The scan that decides which parameters receive texts reads call sites out of the tree, so the door registers
that hidden call with it — which candidates the key will be handed, and in what shape. A hook's parameter
therefore arrives **claimed**, and every door its body asks has the fact it needs:

```python
print(sorted(["bbb", "a", "cc"], key=lambda s: len(s)))       # ['a', 'cc', 'bbb']
print(sorted(["Z", "a", "Mb"], key=lambda s: s.lower()))      # ['a', 'Mb', 'Z']
print(sorted(["ab", "ba"], key=lambda s: s[0]))               # ['ab', 'ba']
print(sorted(["bbb", "a", "cc"], key=lambda s: s + "!"))      # ['a', 'cc', 'bbb']
print(sorted(["bbb", "a", "cc"], key=lambda s: 0 - len(s)))   # ['bbb', 'cc', 'a']
print(min(["bbb", "a", "cc"], key=lambda s: len(s)))          # a
print(max("bbb", "a", "cc", key=lambda s: len(s)))            # bbb

def first(s):
    return s[0]
print(sorted(["zz", "am"], key=first))                         # ['am', 'zz']
```

A `def` used as a key is claimed the same way — the door's call and a call the program wrote are one claim
road — so a parameter the door hands a text and a call site hands a number **cancels**, and that program is
refused rather than half-answered (ADR 0348). `sorted` answers a *container* of its candidates and `min` and
`max` answer *one* of them, which is why the bound forms read as the reference prints them:

```python
xs = sorted(["bbb", "a"], key=lambda s: len(s))
print(xs, len(xs), xs[0])                                      # ['a', 'bbb'] 2 a
for v in xs:
    print(v)                                                   # a, then bbb

best = max(["bbb", "a", "cc"], key=lambda s: len(s))
print(best, best.upper(), len(best))                           # bbb BBB 3
```

Before this row every body above was refused with a sentence about a variable the program never left
unproven (`len of a non-string variable`, `string-method lower on a receiver that is not a text the compiler
can read`, `index of a non-literal variable`), and the bound container reached the screen as the intern
table's numbering: `print(xs, xs[0])` answered `['a', 'bbb'] 1` at exit 0.

What still refuses beside a key, each named with the row that owns it: the elements of a container the program
built (`ys = ["bbb", "a"]` / `sorted(ys, key=…)`) exist only where the runtime puts them (**Gap R.254**,
L11.1); candidates of two kinds (`max("bbb", 1, key=…)`) need the same word (**L11.1**); a key the ordinary
call road has no answer for raises CPython's `TypeError` (**Gap R.255**, **Gap R.248**); and the door's answer
indexed **inline** (`print(sorted(…, key=…)[0])`) asks the index road for a name to read (**L11.1**) — bind it
to a name, as above, and it answers.

### A hook reads its key from the name a lambda was bound to (L11.7, Gap R.281, ADR 0369)

The key may be a **name**, and the name may hold a `lambda`:

```python
neg = lambda v: 0 - v
print(sorted([3, 1, 2], key=neg))                  # [3, 2, 1]
print(min([3, 1, 2], key=neg))                     # 3
print(max([3, 1, 2], key=neg))                     # 1
print(sorted([3, 1, 2], key=neg, reverse=True))    # [1, 2, 3]

two = neg                                          # the same callable, a second name
print(sorted([3, 1, 2], key=two))                  # [3, 2, 1]

shout = lambda s: s.upper()
print(sorted(["b", "A", "a"], key=shout))           # ['A', 'a', 'b']
par = lambda v: v % 2
print(sorted([3, 2, 1], key=par))                  # [2, 3, 1] — equal keys keep the written order

keyed = lambda v, w=1: 0 - v - w                   # ADR 0352's defaults, beside this row
print(sorted([3, 1, 2], key=keyed))                # [3, 2, 1]
```

Before **ADR 0369** every line above died with `TypeError: 'int' object is not callable` at exit 3, while the
same hook written inline (`key=lambda v: 0 - v`) or declared (`def neg(v): return 0 - v`) answered. The reason
is that a `key=` is a call the source never wrote: the defunctionalisation pass retargets every *call* of a
name it gave a declaration to, and this position is not a call, so the name kept holding the payload word `0`
and the door called that. The row is the alias road performed one argument outward — the `key=` argument is
rebuilt to name the declaration the pass minted for the lambda, the `key=` keyword itself surviving so the door
still reads the call the reference reads, and a name the pass cannot resolve leaves the program exactly as it
was. `gustyc --callables` reports the count as `key_arguments`.

The diagnostics keep the program's words and the reference's: an arity trap says
`TypeError: <lambda>() takes 0 positional arguments but 1 was given` — the reference's line, not the generated
symbol — and a `def`'d key keeps its own name in the same sentence (**Gap R.325**, closed beside this row). What
still refuses is the container, not the key: `xs = [3, 1, 2]` with `sorted(xs, key=neg)` needs the value word to
carry each element to the call, and it refuses in the same words whatever spelling the key wears — name, `def`
or inline (**Gap R.254**, roadmap L11.1).

### A hook lambda reads the frame its keyed call is written in (L11.7, Gap R.376, ADR 0396)

The hook may read a name of the frame the `sorted`/`min`/`max` call sits in, not only its own parameter:

```python
def use():
    base = 1
    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))          # [3, 2, 1]

def frame(step):                                                 # the frame's PARAMETER is a word too
    print(sorted([3, 1, 2], key=lambda x: 0 - x + step))

print(sorted([3, 1, 2], key=lambda x: 0 - x + LIMIT))             # a module name: no word is minted
for v in sorted([3, 1, 2], key=lambda x: 0 - x + 1):              # the keyed answer is a list like any other
    print(v)                                                     # 3, 2, 1
```

ADR 0345 gave a `key=lambda …` a declaration on the condition that the body "read nothing but its own
parameters"; the condition is what this row lifts. The hook is hoisted with **one capture parameter per name
the frame owns**, exactly as an entry is under **ADR 0395** and an applied body under **ADR 0370** — all three
ask the one door (`callableLambdaCaptures`), which is why the three cannot disagree about what a body is free
in (**ADR 0232**). The words are filled by the road that already fills them: the minted `def` carries
`Captures`, `lambdaCallArgs` appends each capture as the CALLER'S own reading of that name, and the caller of a
hook is the frame the call is written in — so `sorted`'s hidden call becomes
`call i32 @gy_sorted$lambda$…(i32 3, i32 %_base.ld1)`, the host's load beside the element. A name the MODULE
binds mints nothing: a hoisted `def` reads a global as well as the lambda did. A hook inside a method body is a
hook inside a frame, and answers.

One arity census had to learn the difference between an argument and a word: the minted hook has two parameters
and the door supplies one, so both the key door's arity verdict and `keyDoorCallsOneArgument` now count
`callableSuppliedParams` — the parameters the SOURCE supplies, capture words excluded — rather than every
parameter the declaration has. Counting the minted word answers `<lambda>() missing 1 required positional
argument: 'base'` for a program that wrote no such argument, and "that key is never called" to the pass, which
reads that as a function value left in the program and refuses the whole thing.

What still refuses, each in words and at exit 1: a name of a frame TWO steps out (`def outer(): base = 2` /
`def use(): sorted(…, key=lambda x: x + base)`) is **ADR 0245**'s nested-`def` lift, which this pass still does
not compose with; a capture whose value is a container or a text is **Gap R.332**'s missing tagged word; a
module-level name the module rebinds (a module `for` target) is **Gap R.35**; a hook written inside a `lambda`
body is the closure road's frame; a `lambda` in the `reverse=` slot is an operand no door calls; float ELEMENTS
under any key are **Gap R.248**; and a hook that CALLS a `def`'d name (`key=lambda x: helper(x)`) is
**Gap R.378**, because a function name is a symbol rather than a value and the pass declines to read `%_helper`
as if it were one.

### An applied `lambda` is a function this backend can build a call to (L11.7, Gap R.265, Gap R.276, ADR 0370)

A `lambda` that is **applied where it is written** is a function declaration, not a value — and the compiler now
reads it that way, so its body may call its own parameters and may read the frame that encloses it:

```python
print((lambda f: f(3))(lambda x: x * 2))                        # 6
print((lambda f, g, x: f(g(x)) + g(f(x)))(lambda v: v + 1, lambda v: v * 2, 3))   # 15
print((lambda f: f(1) + f(2))(lambda x: x * 3))                 # 9
print((lambda x: x * 2.5)(3))                                   # 7.5
print((lambda m: m / 2)(3))                                     # 1.5

def apply1(fn, v):
    return fn(v)
print(apply1(lambda x: (lambda y: y * 2)(x), 4))                # 8 — an inner lambda keeps the outer parameter

def f(x):
    return (lambda y: y * 2)(x)                                 # a lambda applied inside a `def`
print(f(4))                                                     # 8

def outer(x):
    y = x + 1
    return (lambda z: z + y)(2)                                 # the capture comes from the caller's frame
print(outer(3))                                                 # 6

class Foo:
    def bar(self, x):
        return (lambda y: y + 1)(x)                             # a lambda applied inside a method
print(Foo().bar(3))                                             # 4

print([(lambda k: k * 2)(x) for x in [1, 2, 3]])                # [2, 4, 6]

# The application may write FEWER arguments than the body has parameters: the entry tuple is the list of
# functions the BODY asks for, so a slot the call left out arrives with its default, and a called slot the
# call left out arrives with the function that default names (ADR 0371).
print((lambda f, y=10: f(y))(lambda x: x + 1))                  # 11 — the same body as the row above
print((lambda f, y=10: f(y))(lambda x: x * 2, 5))               # 10 — the twin that names the slot
print((lambda f, k=3, n=40: f(k) + n)(lambda x: x * 2))         # 46 — the read slot left out below the called one
print((lambda f, g=lambda v: v + 4: f(g(2)))((lambda n: n * 10)))   # 60 — the called slot defaults to a lambda

def apply(f, y=10):
    return f(y)
print(apply(lambda x: x + 1))                                   # 11 — the same omission on the `def` road

def use(f, g=lambda v: v + 4):
    return f(g(2))
print(use(lambda n: n * 10))                                    # 60

# A body may BOTH call one of its own parameters AND read a name the frame around the application owns: the
# clone carries the entries its calls name beside one capture parameter per free name (ADR 0372).
def f(x):
    return (lambda h: h(1) + x)(lambda q: q * 10)
print(f(2))                                                     # 12

def g(v):
    return (lambda fn, n=3: fn(n) + v)(lambda a: a * 2)
print(g(5))                                                     # 11 — entry, capture AND an omitted default

def h(a, b):
    return (lambda fn: fn(a) + b)(lambda z: z * 2)
print(h(3, 4))                                                  # 10 — two captures beside one entry

def k():
    t = 7
    return (lambda fn: fn(1) + t)(lambda z: z * 2)
print(k())                                                      # 9 — the capture is a local, not a parameter

print([(lambda fn: fn(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]])   # [4, 6, 8] — the target is the capture
```

Two mechanisms, one per half of the title. An applied `lambda` whose body calls one of its own parameters is
registered as a **higher-order body**: it is given a declaration, cloned once per distinct tuple of functions its
called parameters are asked to be, and its own application is pointed at the clone — so the callable never has to
exist as a value, and no `fnptr` word or indirect `call` is emitted anywhere (**ADR 0345**'s rejected alternative
stays rejected). A `lambda` that reads a name its own parameters do not bind gains one **capture parameter** per such
name; the list is the set of names the body is *free in*, asked with the binding rule, and the application fills each
one with the caller's own reading of that name. `gustyc --callables` reports the clones under the reader-facing name
`<lambda>`, with `higher_order_lambdas` counting them.

An application does not have to name every parameter. **The entry tuple is read off the body, not off the call**
(**ADR 0371**, Gap R.329): for every position the call left out, the parameter must carry a default, and a default on
a parameter the body *calls* must itself name a function — a `lambda` written inline or a plain name — because that is
the symbol the clone builds its call to. The retargeted call is then entered positionally up to the last parameter the
body calls: a called position the application omitted carries a placeholder the clone never reads, a position in
between that the body reads carries a copy of the default's own expression, and positions at or above the last called
one are left for the ordinary call road to fill, which keeps sole ownership of the default rule (**ADR 0322**).
`--callables` reports how many parameters your applications left out, read off the body, as `omitted_arguments`. What the rule will
not do is guess: a call with more arguments than the body has parameters, one that names any argument by keyword, an
omitted CALLED slot whose default is a number or a container, and an omitted READ slot whose default is a container or
`None` all keep the ordinary road's refusal, each naming the row that owns the missing machinery.

The two lists a clone needs are a **union**, not a choice (**ADR 0372**, Gap R.334). A body that calls one of its own
parameters AND reads a name the frame around its application owns gets a declaration carrying both: the entry
parameters, in the order the body calls them, and one capture parameter per name the body is free in. The capture list
is asked with the closure road's own reader, so the two cannot disagree about what a body is free in; a name the
MODULE binds is not in it (the module-global road owns those reads), a predeclared name is a door rather than a value,
and a name the enclosing frame does not itself bind is declined rather than reached — that one lives two frames up,
which is the nested-`def` lift's territory (**ADR 0245**, **Gap R.347**). The application then fills each capture with the
caller's OWN reading of that name — `call i32 @gy_…$callable$…(i32 0, i32 %p0, i32 %p1)` for `def h(a, b): return
(lambda fn: fn(a) + b)(lambda z: z * 2)` — never a copy of the value, which is what keeps a captured local live across
the call and keeps two applications of one body two clones rather than two constants. A capture parameter is not a
position an application may name: the arity rules above read the parameters the SOURCE wrote. `--callables` counts the
union as `capture_parameters`, which is how a reader tells ADR 0370's self-contained clone from this one.

The same words exist on the **entry** side (**ADR 0395**, Gap R.336). The lambda that reads the frame is not always
the body an application applies — it can be the ARGUMENT a higher-order `def` calls, and an entry is a bare `define`
symbol called by name, so the read has no word to ride in unless one is minted: `apply1(lambda x: x + base, seed)`
inside a frame that binds `base`. The entry lambda is now hoisted with a capture parameter per name it reads, the clone
that dispatches on it grows the union of its entries' words as trailing parameters, the application fills them by name
from the HOST frame's own reading (the same rule as above, asked of a different callee), and the clone hands each word to
the entry positionally — `define i32 @gy_apply1$callable$…(i32 %p0, i32 %p1, i32 %p2)` calling
`@gy_use$lambda$…(i32 %p1, i32 %p2)`, the host writing `@gy_apply1$callable$…(i32 0, i32 %p0, i32 %_base.ld1)`. So
`def apply1(fn, v): return fn(v)` with `return apply1(lambda x: x + base, seed)` prints **`4`**, a callee that calls its
parameter twice prints **`11`**, and a body reading a frame local while its entry reads the loop target prints **`72`**.
`--callables` counts these in the same `capture_parameters` field: `1` for the row program, `2` when the body also reads
a frame local. A name the MODULE binds is not a capture and mints no word — a hoisted `def` reads a global as well as the
lambda did.

The clone's own parameter is **minted**, not spelled with the captured name (**ADR 0397**, **Gap R.375**). Three names
that one spelling used to do the work of are kept apart: the name the PROGRAM wrote (`base`) is what the host frame reads
and the only name its value tables can be asked about; the name of the CLONE'S PARAMETER is generated (`gy$cap$…`, under
the lexer rule `[A-Za-z0-9_]` no source can write) so a higher-order body that has already bound `base` for itself cannot
have its own read answer the word it is being handed; and the ENTRY's parameter keeps the program's spelling, because the
entry's body is the lambda that reads that name. So the collision prints the reference's answer rather than a refusal:

```python
def apply1(fn, v):
    base = 9                            # the body owns the name the entry reads
    return fn(v)

def use(seed):
    base = 1
    return apply1(lambda x: x + base, seed)

print(use(3))                            # 4
```

and so does every shape around it: the name owned as one of the callee's own parameters (`4`), as a `for` target (`4`),
or through a comprehension target (`4`); the entry reading two names while the body owns one of them (`13`); two entries
reading the ONE word the body owns (`11`, one parameter written for both calls); the callee calling its parameter twice
(`11`); a higher-order recursion handing the word down with the entry tuple (`9`); an omitted argument beside a forwarded
word (`4`); and the body reading its OWN `base` while the entry reads the host's (`13`). The clone's recursive call writes
its forwarded keyword itself — the one call a frame cannot reach for, because the frame the recursion lives in owns nothing
(**ADR 0385**'s rule, one word taller). Two shapes still decline, each naming itself: a read the host frame does not own
(two frames up — **ADR 0245**'s nested-`def` lift), and a capture whose VALUE is not one word (a text, a float, a container,
the void or a verdict — **Gap R.332**), which the minting did not widen: the one-word question is still asked of the frame
that OWNS the name, so a body holding `base = 9` beside a host holding `base = "hey"` is refused, not answered with the
number. The `key=` hook, which
hoists a lambda by a second road that had never asked this question, was the other residue and is paid by
**ADR 0396** (**Gap R.376**; see § A hook lambda reads the frame its keyed call is written in).

A `lambda` applied inside a `lambda` that is itself applied reads a frame the program never names, and a capture
travels **one step per frame** (**ADR 0380**, Gap R.330):

```python
print((lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1))     # 111
print((lambda a: (lambda b: (lambda c: (lambda d: a + b + c + d)(4))(3))(2))(1))   # 10

def threeDeep():
    x = 1
    return (lambda y: (lambda z: x + y + z)(100))(10)                # 111 — the owner is a `def` local
```

Each generated `define` carries its OWN forwarded tail — the middle frame spells `y` and gains `x`, the deepest spells
`z` and gains `x` and `y` — and each inner application is filled from the registers of the `define` it is written in:
`call i32 @gy_lambda_2(i32 100, i32 %p1, i32 %p0)` inside `define i32 @gy_lambda_1(i32 %p0, i32 %p1)`. Two rules make
that work, and both are questions about a frame rather than about a call site. A **free name** is what a body reads
after the binders OF THAT BODY are subtracted, so a nested `lambda`'s reads are filtered against the parameter list of
the frame being asked: the middle frame above gains `x` and NOT `y`, which it already spells, and a scan that asked
the nested body in isolation would mint a second parameter of a name the frame binds itself (the reference's own
`multiple values for argument 'y'`, which this backend refuses rather than answers). And a capture **asked of a
forwarded name** is answered by the frame that SUPPLIES it — the verdict the supplying frame gave when it handed the
value over, kept for the frame that received it — because a capture parameter is a position no application in the
program spells, so the call-site scan that types an ordinary parameter has no argument to read there. The verdict is
the supplying frame's own answer, which is why the deep road carries a refusal as faithfully as it carries an answer:
the same chain whose outermost body hands a text, a float or a container to the frames below is still refused at
exit 1, naming the kind and **Gap R.332** (**ADR 0273**: a door that cannot answer says so).

What still refuses, in words that name the missing half and the row that owns it:

```python
print((lambda f: f(3))(abs))                     # a called parameter asked to be a builtin — the value word, L11.1
print((lambda f, y=10: f(y))(10))                # the CALLED slot omitted, and the default is a number — L11.1
print((lambda f, y=[]: f(y))(lambda x: len(x)))  # an omitted READ slot whose default is a list — Gap R.277
print((lambda f, y: f(y))(lambda x: x))           # a required parameter after a defaulted one — the reference's TypeError

def outer(a):
    def inner(b):
        return (lambda fn: fn(1) + a + b)(lambda z: z * 3)   # a capture TWO frames up, by a body that calls its parameter — Gap R.347, ADR 0245's lift
    return inner(2)
print(outer(4))

def apply1(fn, v):
    base = 9                                                 # the body owns the name the entry reads
    return fn(v)

def use(seed):
    base = 1
    return apply1(lambda x: x + base, seed)                  # the word it would forward collides — Gap R.375
print(use(3))

def sorted_by():
    base = 1
    return sorted([3, 1, 2], key=lambda x: 0 - x + base)     # the hook door never asks the capture road — Gap R.376
print(sorted_by())

def f():
    xs = [1, 2]
    return (lambda: len(xs) + 1)()               # a capture whose value is not a number — Gap R.332
```

A capture crosses as one `i32` word, which can say a number and nothing else; the gate asks the value tables the print
door and the arithmetic roads already read **before** the body is emitted, so these are refusals at exit 1 rather than
a handle, a string-table index or a truncated number printed at exit 0 (**ADR 0273**). A `lambda` that *answers* a
float is refused inside a closure or a method for the same reason turned inside out: those bodies answer one `i32`
whatever they do, so the module that tried would be rejected by `llc` — an **exit 2**, which ADR 0166 reserves for a
compiler bug. A `lambda` that is neither applied nor called — used as a *value*, returned from a function, stored in a
container — remains **Gap R.266**'s refusal.

### An applied `lambda` answers a container the way a `def` does (L11.7, Gap R.382, ADR 0401)

```python
print((lambda: [1, 2])())                         # [1, 2]
print((lambda: {1: 2})())                         # {1: 2}
print((lambda: {1, 2})())                         # {1, 2}
print((lambda: (1, 2))())                         # (1, 2)
print((lambda: [])())                             # []
print((lambda: list())())                         # []
print((lambda: [q for q in [3, 1]])())            # [3, 1]
print((lambda v: [v, 1])(9))                      # [9, 1] — a slot the parameter filled
print((lambda x=3: [x])())                        # [3]     — a slot a default filled
print(str((lambda: [1, 2])()))                    # [1, 2]   — and repr, and len, and `in`, and `for`
print(len((lambda: [1, 2])()))                    # 2
print(1 in (lambda: [1, 2])())                    # True
print(sorted((lambda: [3, 1, 2])()))               # [1, 2, 3]
print([v for v in (lambda: [1, 2])()])             # [1, 2]
print(f"{(lambda: [1, 2])()}")                     # [1, 2]   — an interpolation's field
x = (lambda: [1, 2])()                            # a binding, under ADR 0333's container root
print(x[0], x[0:2], x == [1, 2])                   # 1 [1, 2] True
if (lambda: [1, 2])():                             # a condition
    print("yes")
for v in (lambda: [7, 8])():                        # a loop's iterable
    print(v)

def g():
    return (lambda: [1, 2])()                      # a `def` forwarding the answer

print(g())                                          # [1, 2]
print((lambda: [1, 2])()[9])                        # IndexError: list index out of range, and catchable
```

An answer is a value with a kind, and the compiled backend decides the kind by reading a **declaration** — which is
why this section exists. A `def` that hands back a container is answered by ADR 0333's door: `containerAnswerScan`
reads its `return`, the `ret` builds the object for that one call and returns its handle, and each of the eleven
positions above asks the door that knows the kind (**ADR 0344**). A `lambda` applied where it is written had no
declaration for that scan to read — the emitter minted one in the middle of lowering the statement around it, by
which time every whole-program table was already filled — so its answer fell through to the road that keeps one
`i32` per value. `print((lambda: [1, 2])())` ended the hoisted `define` with `ret i32 @.lst1`, a module constant in
an integer position, and `llc-20` refused the module at **exit 2** (**ADR 0166**'s compiler-bug class); the same body
as a comprehension reached the run time and printed the allocator's slot `0` at **exit 0** (**Gap R.331**).

`pkg/lang/lambda_answer.go` gives the `lambda` the declaration the scan needs, and nothing else: an application whose
body the container scan PROVES answers a container becomes a module-level `FuncDef` (minted `lambda_%d`, above every
name the program declared, carrying the application's own span and ADR 0345's `<lambda>` display name) and the call
names it. There is no second answer road for a `lambda`, so there is nothing to fall out of step with: the same scan
registers it, the same `ret` builds it, the same eleven doors read it, and the doors that refuse refuse with the same
sentence the `def` twin gets. Two rules keep the pass honest. A hoist is kept only if the scan still proves the
answer after the rewrite — asked of the rewritten tree, so it runs in bounded rounds, and a round only ever removes a
hoist, so it settles; a program that does not settle is handed back untouched, emitter numbering unmoved (**ADR 0273**).
And the body may read nothing but its own parameters and the builtins, asked with `lambdaFreeNames` — the same door the
capture road asks (**ADR 0232**) — because a hoisted `lambda` reads the module, not the frame its application is written
in. `--emit-ast` and `gusty fmt` still see the `lambda` the program typed: the rewrite happens on a copy (**ADR 0320**).

What this does NOT buy, each with the row that owns it:

```python
def inner():
    return "hi"
print((lambda: inner())())     # prints 0 where the reference prints hi — Gap R.387 (the `def` twin prints hi;
                               # the answer is in the CALLEE's registration, one node past this gate)
def f(x):
    return (lambda: [x])()     # refused: the body reads the frame, so it keeps the capture road, which has
                               # no container answer — Gap R.386 (the `def` twin prints [2])
print([1, 2] if 1 else [9])    # llc refuses `i32 @.lst1` at exit 2 — no lambda involved: Gap R.385
```

and the positions that keep one word for a value keep refusing, in words, at exit 1 — a slot of a container literal
(`print([1, (lambda: [1, 2])()])`), a call argument (`g((lambda: [1, 2])())`), a dict value, `bool(...)`, arithmetic,
and a ternary arm. Each says `"<lambda>" hands back a container, whose word is a heap object and whose kind only this
table knows … (roadmap Gap R.67, ADR 0333)`, the sentence its `def` twin gets, because a handle printed as a number is
**Gap R.261** and a guess about a kind is **ADR 0273**'s wrong answer. A `lambda` in a `match` arm, an `except` body or
an f-string field is also still invisible to the *clone* road's reader — the applied-answer road reaches those carriers
and the defunctionalisation pass does not, which is **Gap R.383**'s shared walk.

### An applied `lambda` that spells its answer is a value with a kind (L11.7, Gap R.384, ADR 0402)

```python
print((lambda: True)())                          # True     — was `1` at exit 0
print((lambda: False)())                         # False    — was `0`
print((lambda: None)())                          # None     — was `0`
print((lambda: "hi")())                          # hi       — was `0`; the compiling leg's llc refused the module
print((lambda: "hi")() == "hi")                   # True
t = (lambda: "hi")()                             # a binding
print(t, len(t), t.upper(), t * 2)               # hi 2 HI hihi
print(str((lambda: "hi")()), repr((lambda: "hi")()))   # hi 'hi'
print((lambda: "a")() + "!")                      # a!
print((lambda: "hi")() in ["hi", "x"])            # True
for c in (lambda: "ab")():                         # a loop over the answer
    print(c)                                     # a / b
if (lambda: True)():                              # a condition
    print("yes")
print([(lambda: True)(), (lambda: None)()])       # [True, None]
print({"k": (lambda: "hi")()})                    # {'k': 'hi'}
print({(lambda: "k")(): 1}, {(lambda: "k")()})    # {'k': 1} {'k'}
print(f"{(lambda: True)()}")                       # True     — an interpolation's field
print((lambda: False)() or "fallback")            # fallback
print(bool((lambda: "")()))                       # False
print((lambda x: None)(1))                        # None     — a parameter, or a default, filled
def echo(v):
    return v
print(echo((lambda: "hi")()))                     # hi       — across a call boundary
```

The section above this one is the rule; this one is the same rule read by the three doors that had no arm for a
callee they could not see. A `def` whose body is `return True` has printed `True` since **ADR 0257**, and a `def`
whose body is `return None` has printed `None` since **ADR 0368**, because the print door never decides a call's kind
from the payload — it asks `textReturningCallables` (**ADR 0366**), `IsBoolExpr`'s call arm and `fdReturnsValue`, and
every one of those reads a module-level **declaration**. An applied `lambda` had none, so its answer travelled the
one-word road and arrived as the number it was stored in.

**ADR 0402** adds a second question to the pass's answer half beside the container scan's: `appliedLambdaBodyRegisters`
reads the body's own spelling — `*StrLit`, `*BoolLit`, `*NoneLit` — and a body that spells one of the three becomes a
declaration exactly as a container answer does. Nothing else was written: `rt_print_str`, `rt_print_bool` and
`rt_print_mixed_value` (with the `answerKindPlan` tag the `return` road stores beside it) are the printers a `def`
has always used, and the family they cover is the same family the `def` twin of every row prints.

Two boundaries, both asserted rather than assumed. A body that spells a NUMBER or a FLOAT is left on the one-word
road on purpose — `print((lambda: 5)())` is `5`, `print((lambda: 2.5)())` is `2.5`, and a hoist there buys nothing
and risks the pair road and the double-domain `define`; the pass's own account lists those bodies as declined, under
`answers_nothing_provable`, with the reason saying which shapes the road renders correctly. And the gate will not take
an answer the source does not spell:

```python
def inner():
    return "hi"
print((lambda: inner())())     # 0 where the reference prints hi — Gap R.387: the kind is in the CALLEE's table
def ib():
    return True
print((lambda: ib())())        # 1 where the reference prints True — the same row, another spelling
print((lambda: f"hi{2}")())    # 0 (and llc refuses the module on the verifying leg) — Gap R.387, whose arm stays
                               # shut because scanFreeIn cannot see a name inside an interpolation's field
                               # (the Gap R.383 blindfold): hoisting `f"{x}"` would read the MODULE's x
def f(v):
    return v
print(f((lambda: None)()))     # 0 — Gap R.171: the void crosses a call boundary as the bare word, `def` twins too
```

The fifth program that stood in that block — `print((lambda: None)() is None)`, pinned at `False` — answers
`True` now (**Gap R.388**, **ADR 0427**: the comparison fold asks the same `isNoneExpr` the print road asks) and
stands in the answered tables of `pkg/lang/void_answer_test.go` and `integration/void_answer_test.go`.

Each of those four is pinned with today's byte beside the reference's in both `pkg/lang/lambda_answer_test.go` and
`integration/lambda_answer_kind_test.go`, so the day one of them answers it stops being a footnote and fails a case.
The same question asked of a slot the program built is answered, and is pinned in
`pkg/lang/the_void_question_test.go` and `integration/the_void_question_test.go`:

```python
xs = [None, 1]
print(xs.pop(0) is None)          # True — Gap R.412, ADR 0411: the TAG says the void, not the word 0 left behind
print(xs.pop(0) is None)          # False — the slot that is left holds 1, and its own tag says so

ys = [None, "a"]
zs = ys                           # Gap R.415, ADR 0412: the handle is shared, and the record of it is shared with it
print(zs.pop(0))                  # None — before this row: 0, the void's payload printed as an int, at exit 0
print(zs.pop(0) is None)          # True — the same door, asked through the other name
for v in ys:
    print(v is None)              # True, False — one object, two names, one answer at every door
```
`integration/programs/lambda_answer_kind.gy` is the line-for-line reference witness for the answered family.

### A callable written in a `match` arm, an f-string field or an `except` handler is read where it is written (L11.7, Gap R.383, ADR 0403)

Three constructs keep their expressions in AST values that are not nodes — they carry code and answer no `Span()`:
`MatchCase` (an arm: its pattern, its guard, its body), `FStringPart` (an interpolation's field) and `ExceptClause`
(a handler: the exception it catches and its body). The whole-program reader the function-value pass asks its
questions of followed a field only when that field's type could hold a node, so for three cycles every question —
which calls exist, which names a body reads, which hook a `key=` argument names — was answered from a program with
those three regions cut out. The visible symptom was one statement written twice:

```python
base = 1
print(sorted([3, 1, 2], key=lambda v: v + base))    # [1, 2, 3]   — answered

base = 1
match 1:
    case 1:
        print(sorted([3, 1, 2], key=lambda v: v + base))   # refused at exit 1 — before ADR 0403
```

The language has no rule that says so. The pass had simply never met the arm's `lambda`, and the door below it
refused with the sentence for a callable that has no word to travel in.

**ADR 0403** descends the carriers. A non-node struct is walked — fields, slice elements, interface values — while
the enclosing `FuncDef`, the lambda-depth counter and the per-path cycle set keep meaning what they did, and a
carrier is **descended rather than visited** because it has no node identity to report to a visitor that switches
on node kinds. Whether a type may be entered at all is one memoised question (`callableCarrierCarriesNodes`), so
annotations, spans and strings stay outside the walk without a list of exceptions.

```python
def use(f):
    return f(2)

match 2:
    case 1:
        print("never")
    case _:
        print(use(lambda v: v - 1))      # 1   — an arm's call is a call site

match 4:
    case v if (lambda w: w > 0)(v):      # 8   — a guard is read too
        print(f"got {use(lambda w: w * v)}")   # got 8 — and so is a field
```

The reader is one question and the rewrite another, and for one cycle the third carrier split them: an `except`
handler's call was READ by **ADR 0403** and deliberately not MOVED, because cloning it out of the handler left the
frame's own trap unhandled at exit 3. What stood there was never the pass. The exception the arm had accepted stayed
flagged until the arm's END, and every call the arm writes ends with a check of that module-wide flag — so the call's
own check took the run to the handler OUTSIDE this one: the arm ran, printed, and then the program died with the
exception it had already handled, and the statement after the `try` never ran (roadmap **Gap R.241**, closed by
**ADR 0404**). An accepting arm clears the flag before its first statement now, which is what the reference's
semantics say it should have been doing all along, and the handler answers like its un-carried twin:

```python
def apply(f, v):
    return f(v)

try:
    print([1, 2][5])
except IndexError:
    print(apply(lambda x: x * 2, 3))   # 6   — a handler's call is a call site too
print("after")                          # after — the run continues
```

`gustyc --callables <src>` reports what the reader reached as `carriers_read` (`{"except_handler": 2}` for the program
above — the two calls the arm writes, `print` and `apply`, counted once each however many routes the walk reaches
them by), so "this pass never met that call" is a claim a script can falsify rather than one it has to trust. The guard
on the AST (`TestTheASTCarriesExpressionsInExactlyTheseThreeNonNodeTypes`, reading `ast.go` itself) keeps the carrier
set closed at exactly these three: a fourth carrier becomes *readable* the day it is added and fails that case,
because whether a construct is worth reading is a decision to take against a witness, not a default a reflection
inherits.

What this does not claim, pinned in `pkg/lang/callable_carrier_test.go` and `integration/lambda_carrier_test.go`
with today's byte beside the reference's: an interpolation's field over a *folded* container answers the container's
handle (**Gap R.390**, beside **Gap R.114** and **Gap R.327**), a `def` that hands a folded container back as its
answer prints the handle from a handler or an arm as it does beside one (**Gap R.328**), and a `match` whose subject
is a text takes no arm at all (**Gap R.391**).

`int(x)` converts a value to an integer: `int("42")` -> 42, `int(3.9)` -> 3

(float truncation). `float(x)` converts to a float: `float("2.5")` -> 2.5,
`float(3)` -> 3.0. `int` ships in the compiled backend: the AOT codegen folds `int` on
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
again) releases its old heap slot through `rt_release`, which pushes it on the
free list that `rt_alloc` recycles — and declines to, while another rooted
variable still names it (**ADR 0414**) — so long-running programs don't leak heap slots. Rebinding a list var
to a non-list value (`x = [1,2]; x = 5`) also releases its slot. Inline list
literals (`len([1,2])`, `print([1,2])`) keep compile-time folding. Local dict
literals (`d = {1: 10}`) also allocate runtime heap dict objects: `d[k]` reads
via `rt_dict_get`, `len(d)` via `rt_dict_len`, and `print(d)` via
`rt_dict_print`. Inline dicts in expressions (`{1: 2}.keys()`) and module-
imported dicts stay compile-time globals. Local set
literals (`s = {1, 2}`) allocate runtime heap set objects: `len(s)` via
`rt_set_len` and `print(s)` via `rt_set_print`. Inline sets in expressions and
module-imported sets stay compile-time globals. Rebinding a var across collection
kinds (list -> dict -> set -> scalar) releases the old heap slot through `rt_release`,
so long-running programs don't leak slots on rebind.

A container is a value with several names, and the release respects that (**ADR 0414**, roadmap
**Gap R.420**/**Gap R.257**): `e = d` copies the handle, so `d` and `e` name ONE object, and rebinding
one of them may not put that object's slot back on the free list while the other still reads it.
`rt_release` therefore asks the collector's own root table before it pushes, and a slot is only recycled
when no rooted variable names it — `d = {"a": 1}` / `e = d` / `d = {"b": 2}` prints `{'b': 2}` then
`{'a': 1}`, and `e["a"]` is still `1`. What a name does not hold, the table cannot see: a value that
survives only inside another container's slot is still released eagerly by a rebinding, which is roadmap
**Gap R.427**. The arena the slots come out of grows
(ADR 0350): `rt_alloc` doubles the window it has committed until it reaches the reservation,
and only when even that is full does it return a -1 sentinel instead of writing out of bounds —
which the caller turns into `MemoryError` (§ The compiled arena, ADR 0347), so pathological
programs can't corrupt memory and can name why they stopped. See ADR 0009.

## Containers across function boundaries (AOT)

A list, dict or set is a *reference*: passing one to a function must give the
callee the same live object the record would (see ADR 0161). The compiled backend
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
  and passing it to a function all see the same container the record would. At
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
- Strings cross a function boundary in the compiled backend: `greet("ada")` interns the argument and
  the callee receives the index (ADR 0174). What the compiled backend still cannot do is report
  itself rather than miscompile — concatenating a runtime string (`s + "!"`), a string method on
  a parameter (`s.upper()`), arithmetic on a string, and a parameter used as both a
  string and a number. Each names the record, which supports all four; see
  § Strings across a function boundary. Ordering two texts is no longer one of them: an
  interned index orders by `strcmp` on the text behind it (ADR 0248).
- Strings are ordinary container elements in the compiled backend (ADR 0173, ADR 0175): lists,
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
- The compiled backend: `with expr as name` runs the protocol and the body identically
  interpreted and compiled (`programs/async_effects.gy` and the `with` cases in the
  conformance corpus pin the output; ADR 0181 closed the generator-rooting defect that
  used to block the compiled leg).

## `yield from`

- `yield from expr` delegates yields to a sub-iterable (a generator call, a list
  literal, or `range(...)`), appending each element to the current generator.
- Full support on the record; codegen emits a runtime loop over the sub-list.

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
`<coro>` on the record and the awaited value in the compiled binary, and both
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

print(addf(1.0))     # CPython 2.5, the record 2.5, the compiled leg answered 1
```

The return word is now read from the same predicate the emitted body itself asks —
`isFloat`, the one the arithmetic, the negation and `print` consult — run over the body
with the names the body rebinds to a float taken as the doubles they became. So the `ret`
and the value it writes cannot disagree, and the program above answers `2.5` on both
backends. What the rule covers is the answer, not the last line: a bare name, an
arithmetic expression over it (`return x * 2`, `return x % 3`), its negation
(`return -x`, which used to reach no gate at all and produced a module `llc` rejected), a
kind-preserving numeric builtin of it (`return abs(x)`, `return abs(-x)`, `float(x)`, and — in its
two-argument form only — `return round(x, 2)`, whose answer is the kind its *first* argument arrived
as and nothing else: the digit count is an integer whoever wrote it, and walking it would promote a
function because a name in that count happens to hold a double, ADR 0263), a
name bound inside the body (`y = x + 0.5; return y`), and any of those inside `if`, `while`,
`try` or recursion.

The rule is about the **name the return reads back**, not about where that name came from, so a
body with no parameters at all is inside it — and that body used to be answered *before the
question was asked*, because the scan began with `if len(fd.Params) == 0 { return nil }`:

```gy
def f():
    u = 9.5          # the body binds a double to its own name
    return u         # `return u` says nothing about the answer's kind

print(f())           # CPython 9.5, and the compiled leg printed 0 at exit 0
```

`define i32 @gy_f()`, a `double` slot the body had just stored into, and the read answered the low
word of that double. The second shape the same scan could not answer is the **derived** name, and
it is the reason the scan now settles its float table by repetition instead of reading each binding
once:

```gy
def f():
    u = 2.5
    v = u * 2        # a double because `u` is one
    return v         # CPython 5.0; a single-pass scan said no and emitted the truncated word
```

Each round asks its questions inside `withFloatNames` — the same table the emitted body will act
on — so the answer the scan gives and the answer the instructions give are one answer (ADR 0273's
rule that a scan which decides must not keep a second opinion), and the round is snapshotted before
it starts so the result cannot depend on the order a map happens to iterate in. **The last write
settles it**, because the read answers the last write's word: `u = 2.5` / `u = 3` / `return u`
answers `3`, not `3.0` (ADR 0373, roadmap Gap R.3c and Gap R.338).

**Whose writes that table holds is the same question one level up** (ADR 0375, roadmap Gap R.339). The
table 「whose last write produced a double」 belongs to ONE body: two functions may use the same spelling
for different kinds, and neither one's answer is decided by which `define` the compiler happened to
lower first.

```gy
def bound():
    u = 9.5
    return u         # 9.5

def lastWrite():
    u = 2.5
    u = 3
    return u         # 3 — not 3.0, and not 3.0 because `bound` exists

def seven():
    v = 7
    return v

print(seven(), bound(), seven())   # 7 9.5 7 — one function, called twice, answering once
```

The body's table is its own assignments plus the parameters its own float convention promotes; the
enclosing frame's table comes back intact when a nested `def`, a method or an applied `lambda` finishes,
so a body in the middle of a statement does not take its frame's float facts with it. What a body may
read from the **module** is a separate question — see the module-scope section, roadmap Gap R.35 — and a
module float a body reads still refuses: `RATE = 2.5` above `def scale(v): return v * RATE` is refused in
words rather than answered by borrowing a table from wherever it was written.

What the rule deliberately does **not** promote is an answer that is not that double, because
promoting it would put an `i32` in a `double`'s word — the same bug in the other direction:
`return int(x)` and `return round(x)` answer with an int whatever arrives, `return x > 2`
answers with the bool word, `return str(x)` with an interned index, and a user callee's return
word is that callee's own question. Those stay exactly as they were. (`round` is on both lists, and
the split is the language's: one argument answers a whole number, two answer in the kind that came
in.)

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
limit, not a language one, and the record answers all of these.

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

**One name, one stack slot — per frame** (ADR 0351, per-frame ownership settled by ADR 0378). A name bound
by a nested `def` and by the body that encloses it is TWO slots, not one shared cell, and the two answers
cannot see each other:

```gy
def outer():
    def inner():
        w = 3          # inner's own slot
        return w
    w = 1.5             # outer's own slot, a different word in a different frame
    return inner()

print(outer())          # 3, what the reference answers
```

A nested body reads the names its frame owns through the closure environment, never through the enclosing
frame's slots — which are dead by the time a closure runs — so a name the two frames both bind is two
allocations, two written-flags where one is possibly unwritten (ADR 0228), and two root registrations
(ADR 0181: it is the slot's address that identifies a variable to the collector). Before ADR 0378 the two
roads that emit a body without passing through the plain `def` road — the closure lift and a class's method
emitter — asked the enclosing frame which slots existed, and the program left at exit 2 with `llc`'s
`use of undefined value` rather than an answer. Reading a name a frame does *not* own is unchanged: the
lookup order (local, then the closure environment, then the module) is the one the scoping section above
states, and a module-level name a body reads is still roadmap Gap R.35's row.

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
libc's `sync()` — the record printed `1`, the binary printed the C library's answer, and the
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
shadows the built-in, and the definition wins — in the compiled backend and in CPython:

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
everywhere below the `def`; above it, the compiled backend would mean different things by the same call —
the record, executing in order, still reaches the built-in, while the compiled backend resolves
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
            print("fin")        # fin, then 3 — the compiled backend

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
toolchain rejection; and a method's frame in a traceback says what the program called, `bump`, the
way the reference prints it — the qualified `Counter.bump` is what the line table reports instead
(ADR 0231, ADR 0354). Known limit: a method whose result
is a `str` is still broken in the compiled backend (roadmap Gap R.42).

### A method's parameter and its answer carry their kind

A method is a callable, and the compiler decides what a callable's slot holds, and what its answer is,
the same way for both spellings (ADR 0366). A parameter every call site fills with a text is an
interned text inside the body, so every door a function's text parameter meets is a door a method's
meets too; a body that hands back a verdict is a verdict, whoever called it:

```gy
class C:
    def __init__(self, flag):
        self.flag = flag          # every call site passes a verdict, so the field holds one

    def fmt(self, s):             return "x%s" % s   # xhi
    def rep(self, s):             return s * 2        # hihi
    def yes(self):                return 1 == 1       # True, not 1
    def held(self):               return self.flag    # True, or False
    def via_self(self):           return self.yes()   # True
    def upper(self, s):           return s.upper()    # HI — the case door, not a recursive call
```

Three rules keep the reading honest rather than lucky. A name more than one callable owns — two classes
declaring `area`, or a `def` and a method sharing a word — is classified as **neither**, because the
table is keyed by the name and one call site would dress the other's number in the string table. A
position a call leaves to its **default** is evidence against being a text: the dispatch passes only the
arguments the source wrote, so a defaulted slot is a word nothing wrote at the call (**Gap R.320**,
which is also why `C().greet()` still refuses). And a text handed to a parameter the call sites never
agreed on **refuses**, naming the missing half, rather than printing the intern position under the text.

The receiver's kind, not the method's name, picks the table: `s.upper()` is the text operation even in a
class that declares `upper` (**Gap R.317**). What a *field* hands back is read for a verdict (`self.flag`
above) and is still owed for a text — `return self.v` of a text field prints the intern position
(**Gap R.319**).

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

## The compiled arena (Gap Q.2, ADR 0347, ADR 0350)

The compiled backend allocates containers out of one arena, `@heap`. A list, a dict, a set, a tuple, an
instance, a class, a closure, a runtime-built string and a boxed float each take a slot, and a slot number —
a *handle* — is the only thing a program holds. The arena has two sizes, and the difference between them is
the whole design:

- **`lang.HeapCapacity` (8192) is the reservation** — how many of them the module reserves. The tables are
  zero-filled globals, so this is `.bss`: a program that never needs the room never touches those pages.
- **`lang.HeapInitialCapacity` (1024) is the window a run starts in**, held in `@heap_cap`. The allocator
  hands out slots inside the window, and when a program asks for one the window has not committed, `rt_alloc`
  doubles the window (up to the reservation) and carries on. Because a handle is an index and not an address,
  widening commits nothing and relocates nothing.

So a program with a large live set is answered rather than refused:

```py
groups = []
fill(groups, 40, 200)   # 8000 containers, all of them reachable
print(len(groups), len(groups[0]))   # 40 200
```

What still runs out is the reservation, and the backend answers that with a trap rather than an invention:

```py
def push_n(grown, n):
    i = 0
    while i < n:
        grown.append([1, 2, 3])   # one container per trip, all of them retained
        i = i + 1
    return i

bucket = []
push_n(bucket, 9000)              # MemoryError, exit 3
```

`MemoryError` is a class like the others (`docs/operations.md` § Exit codes): `except MemoryError:`
reaches it, `except Exception:` covers it, an uncaught one prints a traceback and exits 3, and
`--gc-stats` reports `capacity=`, `headroom=`, `grown=` and `exhausted=` so the condition is visible to a
caller who was not looking for it. `capacity=` is the window this run committed — not a constant — and
`grown=` is how many doublings it took to get there, which is the only way a caller can tell "this arena
widened" from "this arena was always that big".

Four things about it are worth knowing before you write against it:

- **The trap fires where the memory was asked for, not where the wrong answer appeared.** The
  allocation door that found the arena full is the statement that raises, so the traceback points at
  the `append`, not at the later `print` that would have rendered nonsense. Before this rule existed
  the arena's answer was the handle `-1`, which the readers painted as whatever lived at that index:
  a churn loop printed another iteration's list and exited 0.
- **A loop body is an allocation point even when its source contains no literal.** `while` and `for`
  bodies run a collection at their top, and that safe point is also where a full arena is noticed.
  A loop whose body cannot take a slot — `while i < n: i = i + 1` — gets neither the collection nor
  the guard, so the promise costs nothing where it cannot be needed.
- **Growth is a run fact, so the collector follows it.** The mark-clear walk sweeps the committed window,
  not the reservation, and every handle-range question asks the window too: a slot above the window is one
  the allocator never handed out. A program that fits in the window it started in is measured against 1024
  in every direction and reports `grown=0`.
- **Handling it does not create capacity.** The containers you hold are still held, so the next
  container raises again; recover by dropping data, not by retrying. This matches CPython closely
  enough to be the same habit and is the reason `integration/programs/gc_heap_exhaustion.gy` ends at
  its two caught arms.

The arena's two sizes are spelled once each (`HeapCapacity`, `HeapInitialCapacity`, `HeapGrowthFactor` in
`pkg/lang/heap_guard.go`), and the array types, the allocator's bound and doubling, the report's
`capacity=`/`grown=` and the trap's sentence are all generated from them — a module cannot allocate from one
arena and report another. The ceiling that remains is a *container* count: an object owns 256 element words,
so eight thousand small containers is a program and one eight-thousand-element list is a different question
(`docs/roadmap-details.md` § Gap Q.2).
