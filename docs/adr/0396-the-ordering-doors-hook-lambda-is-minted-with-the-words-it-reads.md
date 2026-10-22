# The ordering door's hook lambda is minted with the words it reads

**Roadmap:** **Gap R.376** (closed here — the `key=` hook that reads the frame its call is written in) ·
**L11.7** (functions are values that compile; this is the hook side of its capture road) ·
**ADR 0340** (the key door: `sorted`/`min`/`max` call the key once per element through the ordinary call road) ·
**ADR 0345** (a `key=lambda …` hook is given a declaration so the door has a name to call — and the condition
that road carried, "the lambda must read nothing but its own parameters", is the half this ADR lifts) ·
**ADR 0370** (`callableLambdaCaptures`, the one door that answers what a body reads out of the frame it was
written in) · **ADR 0372** (a clone's parameter list is the union the body asks for) ·
**ADR 0395** (the ENTRY side of that road, paid one cycle before this one, whose residue this row is) ·
**ADR 0363** (the door's call is a call site the parameter scan must read, though the AST never wrote it) ·
**ADR 0232** (one question, one asking place — the reason this row asked the door instead of writing a third
spelling test) · **ADR 0166 / ADR 0249** (a refusal names the missing half, at exit 1, and never leaks the
compiler's own machinery) · **ADR 0273** (a gate is a pure function over the program) ·
**Gap R.332** (a capture whose value is not a number still has no word — the gate this row inherits at a new
position) · **Gap R.248** (float ELEMENTS under any key, the door's own refusal, reached for the first time by
a hook that got hoisted) · **Gap R.377 / Gap R.378** (the two refusals this row leaves standing, filed with
today's bytes: the `noteUnlowered` wrapper that leaks a Go type name into a program's diagnostic, and a hook
that CALLS a declared function)

## Status

Accepted. Landed 2026-10-22.

## Context

```python
def use():
    base = 1
    return sorted([3, 1, 2], key=lambda x: 0 - x + base)

print(use())           # CPython: [3, 2, 1]  — backend: exit 1
```

and the same program written at module level over a module-level `base`, and the same inside a method, and
`min`/`max` asking the one hook the same question. Each died with the key door's own sentence:

> `key=lambda ...` is refused: a lambda is a function VALUE, and this backend has none — `def` bindings are
> declared, not assigned, so the key has no word to carry to the call (roadmap L11.7)

Three roads had been built for this language's callables, and each had its own answer to one question:

| road | what it hoists | what it asks about a body's free names |
|---|---|---|
| the application road (ADR 0370/0372) | the `lambda` an application applies | `callableLambdaCaptures` — mints one parameter per name the frame owns |
| the entry road (ADR 0395) | the `lambda` a CLONE calls | the same door, asked on the entry side |
| the hook road (ADR 0345) | the `lambda` in a builtin's `key=` slot | `callableLambdaIsSelfContained` — a spelling test: free name ⇒ decline |

The third row is the defect. The sentence it refuses with is true about the representation (this backend has
no function values) and simply does not apply to the program in front of it: the hook is not used as a value
anywhere. It is called by the door, once per element, by the SYMBOL the pass minted for it, and the pass held
the host frame in its hand the whole time — `r.encs[rank]`, the same table the entry road reads, is the frame
that owns `base`. This is ADR 0232's disease for the third time in four cycles: ADR 0391 found "is this slot
self-describing?" written three times in the container roads, ADR 0392 and ADR 0394 found it twice more, and
here was a fourth copy, in a different family, holding a fourth answer.

## Decision

**The hook road asks the door the entry road asks.** In `planDefunctionalization`, the argument-hoist loop that
gives a builtin's lambda argument a declaration now accepts a non-self-contained lambda when, and only when,
the position is the one a builtin CALLS:

* the callee is one of `sorted`/`min`/`max` and the argument is the `key=` option — `keyDoorOptions`, the same
  census the door and the parameter scan read, so "the door calls this" is one fact with one definition;
* the body's free names are settled by `callableLambdaCaptures` (ADR 0370): a name the frame owns becomes a
  trailing parameter of the minted `def`, a name the module binds needs no word (a hoisted `define` reads a
  global as well as the lambda did — ADR 0395's rule, asked of this road now), and a name past the frame is
  declined, because that is ADR 0245's nested-`def` lift and the two passes still do not compose;
* a name that is a DECLARED function or class is declined too (`callableCapturesAreValueNames`): a `def` name
  is a symbol, not a value, and a capture filled from `%_helper` would be a call reading a symbol — the shape
  stays the door's refusal and is filed as **Gap R.378** rather than answered by an accident;
* the minted `FuncDef` carries `Captures`, which is the instruction `lambdaCallArgs` already obeys: fill each
  capture by NAME from the CALLER'S own reading. The caller of a hook is the frame the `sorted` call is written
  in, which is the frame that owns the name — so the existing fill road is the whole of the fix, with no second
  place that knows how to hand a value to a callable.

That is the opposite of the entry road's choice, and deliberately so. ADR 0395's hoisted ENTRY carries **no**
`Captures` field, because its caller is a clone the pass itself wrote and the word it holds was handed down
rather than reached for; here the caller is a frame of the program, so reaching for the name by name is the
honest instruction and `captureCarriesOneWord` (Gap R.332) is asked where the value lives.

**One arity census had to learn the difference between an argument and a word.** The minted hook has two
parameters and is called with one element; the key door counted every parameter as required and raised
`<lambda>() missing 1 required positional argument: 'base'` for a program that wrote no such argument, and
`keyDoorCallsOneArgument` — the census that decides whether a `key=` is a call at all — answered "never
called", which the pass reads as a function value left in the program and refuses the whole program for. Both
now count `callableSuppliedParams(fd)`: the parameters the SOURCE supplies, `fd.Captures` excluded. One
question ("how many arguments must a caller supply to this callable?"), one asking place, read by both doors —
because the failure mode of two copies here is not a wrong number, it is two doors disagreeing in public.

## Codegen and IR implications

The hook is an ordinary `define`, called by its symbol, with the element beside the words it reads:

```llvm
define i32 @gy_sorted$lambda$c4319c9a_0(i32 %p0, i32 %p1) {   ; the element, and the word it reads
  ...
  store i32 %p1, i32* %_param1
  %t59 = sub i32 0, %p0
  %t60 = add i32 %t59, %p1
  ...
}

define i32 @gy_use() {
  %_base = alloca i32
  store i32 1, i32* %_base
  %_base.ld1 = load i32, i32* %_base
  %t1 = call i32 @gy_sorted$lambda$c4319c9a_0(i32 3, i32 %_base.ld1)   ; the HOST'S own reading
```

ADR 0345's residue table is unchanged: no `fnptr`, no `ptrtoint`, no `inttoptr`, no indirect `call`, and the
minted name is a function of the program (two emits of one source are byte-identical, which the IR row
asserts). Nothing was added to the runtime: `@rt_elem_gt`, `@rt_append_tagged`, `@rt_put_elem` and the print
doors are the ones ADR 0340 already used, because the door only ever called a key — the missing part was a
declarable key with the right words in it.

The account keeps its two stages (`--callables`): `hoisted_lambdas` counts the declaration, `capture_parameters`
now counts the words the HOOK road wrote too, not only a clone's — including for a program whose capture the
emitter later refuses, because the account reports what the pass did and only the second verdict reaches
`--file` (ADR 0395's lesson, kept).

## Machine path

`gustyc --callables` reports the minting, which is how an agent tells a hoisted hook from a declined one without
reading IR:

```console
$ gustyc --callables "$(printf 'def use():\n    base = 1\n    return sorted([3,1,2], key=lambda x: 0 - x + base)\n\nprint(use())\n')"
{
  "tool": "callable-values",
  "pass": "defunctionalize-callables",
  "hash": "5132e298ef2e2b2e",
  "rewritten": true,
  "clones": 0,
  "hoisted_lambdas": 1,
  "rewritten_call_sites": 0,
  "recursive_sites": 0,
  "key_arguments": 0,
  "omitted_arguments": 0,
  "capture_parameters": 1,
  "higher_order_lambdas": 0,
  "higher_order_functions": [],
  "symbols": [
    "sorted$lambda$c4319c9a_0"
  ],
  "refused": []
}
```

`capture_parameters` is the field this row made load-bearing for a second road: it counted the words the ENTRY
road minted under ADR 0395 and now counts the hook road's as well. A declined hook is told apart from a hoisted
one by the same payload: for the nested-`def` shape (a name of a frame two steps out, which this row declines) the
pass answers `"rewritten": false, "hoisted_lambdas": 0, "capture_parameters": 0` with its own decline sentence in
`refused`, where the row's program answers `true / 1 / 1` with `refused: []` — the agent's path to "did my hook get
a declaration, and how many words went with it" without reading IR.

`--lang` still names the callable surface this row sits in, and every refusal listed below is asserted at exit 1
with exit 2 forbidden, on `--jit` and `--aot`, in `integration/key_hook_frame_test.go`.

## Alternatives rejected

* **Widen `callableLambdaIsSelfContained` with a fourth spelling test** (`is a name of the frame?`). It would
  have answered the same 23 programs and left four copies of one question in the tree, which is the exact
  condition ADR 0391/0392/0394/0395 were each found by. The decline is now impossible to write differently in
  one road than in another.
* **Let the hook road mint a word for a name the frame does not own.** Three probes turned that into a
  `define` reading a global slot the module rebinds, which is Gap R.35's answer with a plausible number in it.
  A name past the frame is ADR 0245's lift; the refusal says so and stays.
* **Fill the capture from the callee instead of the caller** (a copy of the value, or an address). Both would
  work for a `def`'s local and fail for a frame that changes under the call — the `while` row and the
  rebinding row exist to fail them; the host's own `load` is the only shape that stays right.
* **Give the minted capture parameters a `Default`** so the arity doors' `required` count came out right. One
  field's lie fixes two doors' arithmetic and makes "omittable" mean two things; `--emit-ast`/`gusty fmt` would
  print a default the source never wrote. `callableSuppliedParams` says the true thing instead.
* **Fix the two refusals in this commit too.** `noteUnlowered`'s `Stmt` arm leaking `*lang.ExprStmt` into a
  program's sentence is pre-existing and byte-identical on the parent build, and a hook that calls a module
  `def` needs its own shadowing analysis. Both are filed as **Gap R.377**/**Gap R.378** with today's bytes;
  adopting either would have let this commit claim a fix it did not make.

## Consequences

23 programs that were refused now print CPython's bytes, on the record leg, the compiled leg and the reference
leg; 17 of them were measured as newly agreeing over a 26-probe sweep with **0 regressed**, and the sweep's
reference leg was validated on a known-good program first because a reference that fails every probe degrades
to "both sides failed" (ADR 0394's lesson). `integration/programs/key_hook_frame.gy` is the matrix row, oracle
`match`, 25 lines byte-for-byte CPython; `tools/recmerge` wrote 22 entries and refused none.

What stays refused stays refused in words: a name of a frame two steps out (ADR 0245's lift), a capture that is
a container or a text (Gap R.332), a module name the module rebinds (Gap R.35 — the identical refusal the
applied-lambda road has always given, measured), a hook inside a `lambda` (the closure road's frame), a lambda
in the `reverse=` slot no door calls, float elements under a key (Gap R.248), and a hook that calls a declared
function (Gap R.378).
