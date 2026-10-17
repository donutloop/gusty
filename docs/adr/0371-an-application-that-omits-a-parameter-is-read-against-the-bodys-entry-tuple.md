# 0371 — An application that omits a parameter is read against the body's entry tuple

## Context

ADR 0370 taught the defunctionalisation pass to see an applied `lambda`. It did not teach the pass to read
an application that **writes fewer arguments than the body has parameters**. Two sources, one line apart in
what they ask, and the build answered one of them:

| source | reference | this backend, before this ADR |
|---|---|---|
| `print((lambda f, y=10: f(y))(lambda x: x * 2, 5))` | `10` | `10` |
| `print((lambda f, y=10: f(y))(lambda x: x + 1))` | `11` | exit 1 — `"f" is called but is not a function this backend can build a call to` |
| `def apply(f, y=10): return f(y)` / `print(apply(lambda x: x + 1))` | `11` | exit 1 — the same sentence |
| `print((lambda f, k=3, n=40: f(k) + n)(lambda x: x * 2))` | `46` | exit 1 — the same sentence |
| `print((lambda f, g=lambda v: v + 4: f(g(2)))((lambda n: n * 10)))` | `60` | exit 1 — the same sentence |

Nothing about the program changes between the first row and the second except how many words the
application wrote: same body, same call of the same parameter, same `+ 1`. This is not a corner of the
`lambda` road either — the identical shape on a `def`, the road ADR 0345 opened and ADR 0364 made
recursive, is refused too. The row is roadmap Gap R.329.

The build was not declining for one reason but for two, and they sat in different passes:

1. **The entry tuple was keyed by the call's argument list.** The site loop required
   `len(c.Args) == len(cf.def.Params)`; an application that left a defaulted parameter out failed that
   audit, so no clone was ever asked for. This is the audit a call must satisfy to be entered
   *positionally* — and it is the wrong question for a clone, whose body does not care whether the word at
   position 1 is an entry the source wrote or one the compiler filled.
2. **A called parameter could not carry a default at all.** `callableCalledParams` and
   `lambdaCalledParams` dropped any parameter whose `Default != nil` from `calledPos` — so a body that
   calls a defaulted parameter registered nothing, and the second audit could not even be reached. That
   rule existed because the pass had no way to name what such a slot holds; but "no way to name it" was
   only true of a default that is *not* a function.

What made the case decidable is that the pass already owns the two readings the omission needs. `resolve`
answers an expression with the **symbol** of the function it names — a `lambda` written inline, a name
bound to one, a `def`'d name, an alias — and the ordinary call road already fills an omitted argument with
its default. A call that omits a parameter therefore leaves the entry tuple's slot empty in exactly the
one way the compiler can still read.

## Decision

**The entry tuple is the list of functions the body asks for, not the list the call site writes.**

1. **The arity audit asks the body, not the call** (`callableSiteOmission`). A call is readable when, for
   every position at or beyond the arguments it wrote, the parameter has a default; a called one additionally
   has to have a default this pass can name as a function. A call that writes *more* arguments than the body
   has parameters, or names any argument by keyword, keeps the ordinary road's sentence: the first is a call
   this backend would trap on, and the second is a call whose positions this pass cannot read (ADR 0283's
   rule that an unanswered shape is refused, not guessed at).
2. **A called parameter may carry a default, when the default names a function**
   (`callableDefaultNamesAFunction`): a `lambda` literal that is self-contained and unblocked, or a plain
   name. Those are the two spellings `resolve` answers with a symbol, which is what an entry has to be. A
   default that is a number, a container, a call or a predeclared name still drops the parameter out of
   `calledPos`, and the call is then read against audit 1 and declines there rather than emitting a call
   whose callee is the word `10`.
3. **What the call did not write is read from the parameter, not from the argument**
   (`callableSiteOperand`): position `pos` resolves from the argument when the call wrote one and from the
   parameter's own `Default` when it did not. That is the same expression the body would have seen, which is
   the only expression whose symbol can stand in the body's call.
4. **The rewritten call gains the words the application did not write** (`callableSiteFill`), up to the last
   position the body calls: the placeholder word at a called position — the clone never reads the slot, it
   calls the entry by name — and a **copy** of the default's own expression at a position in between that the
   body reads, which is what the call road would have evaluated for an omitted argument anyway (ADR 0322).
   Positions at or above the last called one are left out entirely, so the ordinary road fills them by its
   own default rule and this pass does not duplicate that decision.
5. **The pass says so in its account**: `--callables` gained `omitted_arguments`, the count of parameters the
   applications left out and the pass read off the body's prototype. An agent reading the account can tell an
   application that named every slot from one the compiler completed, without reading the IR — and the two programs at
   the top of this record, which produce the same clone, are separated by that one field.

## Agentic rationale

`gustyc --callables '<src>'` answers the question "what did the pass decide" as one JSON document, and
`omitted_arguments` is the field that distinguishes the two programs above, which are otherwise
indistinguishable in the emitted module — both are one clone, one hoisted `lambda`, one retargeted call. An
agent that needs to know whether the source's own argument list survived can read that field instead of
diffing IR; the value with no such field was a guess, and the record leg is what a guess costs
(ADR 0302's rule that the record is evidence, not the definition).
The refusals stay in the same shape as every other refusal the pass makes: one sentence naming the
parameter, what the reference does with the program, and the roadmap row that owns the missing machinery.
`print((lambda f, y=10: f(y))(10))` — the omission that leaves the CALLED slot with a number — refuses with
the value-word sentence naming L11.1 and ADR 0166; `print((lambda f, y=[]: f(y))(lambda x: len(x)))` names
Gap R.277, the row that owns binding an omitted container default. Both are the reference's *answers* (a
`TypeError` and `0`), and both are declined: the backend has one shape of wrong answer, which is a module
(ADR 0166), and one shape of refusal, which is words (ADR 0273).

## Codegen / IR implications

No new instruction, no new runtime symbol, no new ABI. The rewritten `Call` node carries more positional
arguments and the callee name points at the clone; the emitted `define` is exactly the one ADR 0370 already
wrote for the same body, so the clone of

```python
print((lambda f, y=10: f(y))(lambda x: x + 1))
```

is the same `define` the twin at two arguments emits, called with `(ptrtoint %lambda$…, 10)` where the
twin is called with `(ptrtoint %lambda$…, 5)`. A called position the application omitted carries the
`ptrtoint`-form placeholder literal, which the clone reads never — its body names the entry directly. This
is why the change does not touch `pkg/lang/codegen*.go`: the arity that decides a call was already the
audit the pass reads against, and the pass is where an argument list is allowed to change.

## Alternatives rejected

- **Fill a left-out slot with the value the ordinary road binds for a default.** The clone's parameter is one
  word; a `None`, a list, a dict, a set or a tuple default arrives as a (payload, tag) pair or a heap
  handle, and reading that word as a count is the wrong-answer class. Gap R.277 owns binding an omitted
  container default; until it closes, the pass declines rather than routes a program through the gap
  (ADR 0273, restated for the pass by ADR 0345's identically-worded rejection).
- **Key the clone by the omitted parameter's name.** The clone's key is a tuple of *functions*, and the
  entry is chosen by which function the body is asked to call. A key that named a parameter would clone
  once per spelling and share a body across two different callees — the bug ADR 0345's `key=` road already
  rejected, where a hook called the payload word `0` because the symbol and the value were conflated.
- **Let a called slot keep a non-function default and build the call anyway.** The emitted `call` would name
  a symbol that is not a function; `llc` would reject the module (exit 2, ADR 0166) or, worse, accept a call
  to the wrong `define`. The refusal is the correct output for that shape until the value word arrives
  (L11.1).
- **Hoist the default's `lambda` into a `def` of its own** (ADR 0369's road for a `key=` name). Unnecessary,
  and it would change what the source wrote: `resolve` answers an inline `lambda` directly, and minting a
  second symbol for it would put two names on one body.
- **Support keyword arguments at such a site.** `(lambda f, y=10: f(y))(lambda x: x + 1, y=3)` is the
  reference's `4`, and reading it needs the argument list *reordered* into positions — a different rewrite,
  with its own arity audit and its own trap (`TypeError: got an unexpected keyword argument`) — so it keeps
  the ordinary road, and the row that owns keyword-call shape owns it too.

## Consequences

- The record leg covers six new sources, recorded from the reference by `tools/recmerge` and never from this
  backend (`pkg/lang/testdata/interpreter-golden.json`, 7827 → 7833).
- `integration/programs/lambda_applied_entry_tuple.gy` is on the conformance matrix: the omission, its twin,
  the `def` road, the method road, the comprehension and the `for` body, each line for line CPython.
- Two shapes the same sentence now separates are filed rather than fixed: an omitted CALLED slot whose
  default is not a function stays with L11.1 and the value word, and a body that calls its own parameter
  *while* reading a name from the enclosing frame is Gap R.334 — the capture road and the entry tuple do not
  yet meet in one clone.
