# ADR 0235 — a class pattern is one question about a class, and the instance has to be asked whether it has the attribute

Status: accepted. Closes roadmap **Gap B** (AOT `match`: class-pattern lowering, aliases, binding
edges) and the compiled half of **P2.9**. Supersedes the limitation recorded in ADR 0149
("interpreter-side; AOT/codegen limitation documented"). Cites: ADR 0161 (one question, one table —
the rule this restores), ADR 0211 (an `llc` rejection is exit 2, a compiler bug, never a refusal),
ADR 0175/0189 (a container writes its tag with its payload, never later and never by guessing),
ADR 0227 (what a function body may know about its module), ADR 0228 (which bindings a body owns),
ADR 0186 (the CPython leg, and what it cannot adjudicate here).

## What had been measured

Three shapes, run on both engines, from `docs/roadmap-details.md` § Gap B and re-measured on 2026-10-02:

```gy
class Point:
    def __init__(self, x, y):
        self.x = x
        self.y = y
Alias = Point
p = Point(2, 3)
```

| program | `--interp` | `--aot` |
| --- | --- | --- |
| `match p: case Point(x, y): print(x, y)` | `2 3` | `2 3` |
| `match p: case Point(a, b): print(a, b)` | `no` (next arm) | **`0 0`** — the arm matched |
| `match p: case Alias(x, y): …` at module scope | `alias 2 3` | `alias 2 3` |
| the same `match` **inside a function** | **exit 3** `TypeError: 'type' object is not callable` | **exit 2**, `llc` rejected the module |
| `match 3: case call_it(): …` | `call` | **exit 2**, `use of undefined value '%_f'` |

The row in the tracker said the two backends disagreed by one digit. They disagreed about whether
the arm had matched at all.

## Root causes, three of them, one of which was hiding inside another

**The instance never got asked.** The compiled instance is a row of `i32`s in `@heap`; a class pattern
bound every capture name with `rt_inst_get(sub, slot)` and ANDed in only the class-chain test. An
attribute that was never written and an attribute holding `0` are the same words. So `case Point(a, b):`
matched an instance that has `x` and `y` and bound `0 0`. The documented rule (`docs/language.md` §
Class patterns) says a missing attribute fails the case; the interpreter implemented that rule (it
has an attribute map with an existence question in it) and the compiled backend could not ask.

**One question, asked twice, answered differently.** "Which class does this name denote?" was
implemented in the evaluator (`resolveClassID`, reading the scope it happened to be executing) and
again in codegen (`g.classIDs`/`g.classInfos`, then a fallback branch for "a runtime alias"). Neither
was wrong in isolation; together they meant `Alias = Point` worked at module scope in both and died
inside a function in both — in the evaluator because the function's frame does not hold the module's
binding, so the pattern fell through to *calling the class*; in codegen because the fallback branch
called `g.value(b, fn)` and wrote:

```go
alias, _ := g.value(b, fn)                                    // the error is discarded
b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cc, cid, alias))   // …and lands here
```

with `alias == ""`. The module reached `llc` as `%t6 = icmp eq i32 %t5, ` — nothing after the comma.
That third row (`case f():`) is the same line: `f` is a function, so `g.value` refused, and the
emitted load was `%_f.ld1 = load i32, i32* %_f` for a variable that does not exist.

So the "runtime alias" branch was not a feature with a bug in it. It was a hole: **any** class-pattern
callee that was not a declared class went through it and produced either a rejection or an
operand-less compare.

## The decision

**One table for the class.** A new front-end pass (`pkg/lang/classpat.go`) answers, once per program,
what a class-pattern callee denotes: `Alias = Point` (and chains of them, cycle-bounded) resolves to
the declared class at the end; a name that is no class is absent. `matchPattern` (codegen) and
`matchPattern`/`resolveClassID` (evaluator) read that answer. The compiled arm for an alias is now
the same lowering as the named arm — the class-chain walk, not a value compare.

**The module scope is the evaluator's answer to a bare name.** `resolveClassID` now consults
`e.curModule` after the frame, the way any other bare name in a body does (ADR 0227). A function body
that reads `Alias` means the module's `Alias`; that is the whole fix for the interpreter's half.

**The instance is asked, per capture, whether it has the attribute.** A new parallel array —

```llvm
@inst_set = internal global [1024 x [256 x i32]] zeroinitializer
```

— is written by `rt_inst_put` alongside the value ("the operation that writes the payload writes its
presence", ADR 0175's rule), read by `rt_inst_has`, and cleared by `rt_inst_clear` **at each
instantiation**, before the class id is written. Clearing is not optional: `rt_alloc` recycles slots,
so without it a fresh instance would inherit which attributes the previous tenant of that slot had —
the same stale-word class ADR 0189 hit with element tags. The clear's length is the complete set of
attribute names the program can write, collected by the same pre-pass, because interning slots
lazily during emission would leave the count short for a slot first named later.

**A pattern answer is an `i1`, not an integer.** The old code returned the strings `"1"`/`"0"` for
always/never-matching patterns and then `and`ed/`or`ed and `br`-anched them; `and i1 %t, 0` is not
IR. Patterns now answer `true`/`false` and `andCond`/`orCond` fold the constants.

**A call pattern is a call.** `case f():` where `f` is a user function or a builtin goes to
expression-equality, which is what the interpreter always did. A name that holds a value which is
not a class matches nothing — it is not called. (CPython raises `TypeError` there; gusty's documented
rule is that the case fails and the next is tried, and the two backends now both follow it.)

## Why this shape, and not the alternatives

- *Give the interpreter the compiled backend's answer* (bind missing attributes to 0): rejected. It
  would make a wrong answer agree with another wrong answer, and the documented rule and the
  instance's own attribute map both say the case fails.
- *Answer "does it have the attribute" statically* (scan the class body for `self.x = …`): rejected.
  It is right for the common program and silently wrong for `c.z = 5`, for a conditional write, and
  for a subclass that adds the attribute — and those are exactly the programs a pattern is written for.
- *Give compiled instances a real attribute dict*: rejected for now. It is the honest long-term model
  and it is what **Gap R.19** will need (a missing attribute should raise `AttributeError`, not read
  0); this cycle adds the minimum the pattern needs, in the representation the runtime already has,
  and R.19's row now records that the presence machinery exists and what remains is the raise.
- *Leave the alias branch and check its error*: insufficient. It fixes the crash and leaves the two
  backends resolving aliases by different rules — which is how they diverged in the first place.

## Agentic interface

`gustyc --lang` gained a `patterns:` line naming every pattern form, including the class pattern and
its aliasing — until now `match` appeared only as a statement keyword and no machine could discover
that `Point(x, y)`, `1 | 2` or a guard exist, let alone that a missing attribute fails the case.

The conformance ledger is where this feature is *pinned*. `integration/programs/match_classpat.gy` is
row 103 of the matrix (80 shared, all at parity) and covers: the named arm, the aliased arm at module
scope and inside a function, the missing-attribute failure, a non-instance subject, a subclass
instance, a call pattern, an or-pattern, a class passed in as a parameter, a name holding a non-class,
and an attribute that appears only after a method writes it. CPython cannot run the file — a positional
class sub-pattern needs `__match_args__`, which this language does not have — so the row is
`not_applicable` with per-leg pins rather than a `match`, and the pins are what would catch a
regression if someone "fixed" the divergence by making both backends print `0`.

## Codegen/IR notes

- `case Point(x, y):` emits, per capture: `call i32 @rt_inst_has` → `icmp ne …, 0` → `and i1`, then
  `rt_inst_get` and the capture store. The conjuncts are ANDed, so an early failure still costs the
  loads; sinking them behind a branch would need per-capture blocks, which the match lowering does not
  have and this does not need.
- Instantiation is `rt_alloc` → `rt_inst_clear(h, n)` → `rt_inst_put(h, 0, classId)`. Order matters:
  clearing after the class id would erase the instance's own identity.
- `andCond`/`orCond` folding means a case that can never match contributes no instructions at all.

## What is still owed

- **Gap R.19** — a plain attribute read of a missing name should raise `AttributeError` in the
  compiled backend. `rt_inst_has` is now in the module and used by one caller; the raise is the rest.
- `match` binding edges (Gap B's second half): a capture bound only in an arm that did not run is
  still readable in compiled code (Gap R.18's family). The conformance program avoids the shape
  rather than pinning it.
- Nested `match` inside a class body, and `default`-pattern (`case _ if …`) chains deeper than the
  current lowering, are untested on the compiled leg.
