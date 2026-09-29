# 0205. A built-in name belongs to the program, from its definition onwards

## Status

Accepted. Implemented this cycle in `pkg/lang/semantic.go` (`predeclaredDefs`, the built-in call path
in `inferCall`); pinned by `pkg/lang/predeclared_shadow_test.go` and
`integration/predeclared_shadow_test.go`. Roadmap Gap R.12.

## Context

ADR 0199 established that a built-in name is a name the program may claim, and ADR 0203 made those
names spellable. What nobody had asked is *when* the claim takes effect:

```gusty
for i in range(2):          # interpreter: the built-in, 0 and 100
    print(i * 100)          # codegen:     the program's range(2) = 6, iterated as a count


def range(x):
    return x * 3


print(range(4))             # 12, both
```

The interpreter executes statements in order, so at the loop the `def range` has not run and the
built-in is still what `range` means. Codegen emits every function before the module body, so the
call resolves to the program's definition wherever it is written. Measured: the interpreter printed
`0 100 12`, the compiled binary printed `0 100 200 300 400 500 12`, CPython printed `0 100 12`. One
source file, two programs.

That is the worst shape a gap can have for a compiler whose main consumers are agents: no diagnostic,
no crash, just a different answer, with the two backends each defensible in isolation. The corpus
program written for R.9 was the thing that exposed it, and it had to be written to *avoid* this shape
to stay green.

## Decision

**A built-in name claimed by a module belongs to the program from its definition onwards; above that
line, the program is refused.**

1. `SemanticAnalyzer` keeps the positions of the module-level `def`s whose names are predeclared
   (`predeclaredDefs`, filtered from `an.moduleDefs` through the one `predeclaredNames` table the
   codegen guard and LSP already share). When a call to a predeclared name is analysed **at module
   level** — where statements execute in the order they are written — and its name is claimed by a
   `def` further down the file, the checker reports:

   ```
   error at 1:15: "range" is a built-in here, but this module defines it below, at line 5:
   the interpreter uses the built-in and the compiled backend uses that definition — move the
   definition above every use, or rename it
   ```

   The message names the built-in, the line of the competing definition, the two readings, and the
   two ways out. It is emitted at the call, which is where the programmer can still act.

2. **The rule is bounded to where order is real.** Inside a function body the question has no
   ordering: a body runs after every module-level `def` has executed, so both engines already resolve
   it the same way, and refusing it would be over-reach. Class bodies are eager, and methods are not
   module bindings at all — `Counter.range(self, n)` leaves the built-in loop working, which is a test
   rather than an assumption (it is the same separation ADR 0200 installed).

3. **Analysis continues down the built-in path after the error.** Returning dynamic there would make
   the loop variable dynamic too, and the one honest error would be followed by derived warnings about
   a perfectly typed call — the ADR 0201 rule, that one mistake produces one diagnostic.

4. **The refusal carries its own death clause.** `TestBackendsGenuinelyDifferOnTheRefusedProgram` runs
   the refused source through both engines *around the gate* and fails if they ever agree. A refusal
   that has outgrown its cause is a restriction, and the only way to know is to keep measuring the
   thing that motivated it.

## Consequences

- The R.12 repro is now a compile-time refusal instead of two different run results, so the two
  backends can no longer be observed disagreeing about a built-in name at module level; the corpus was
  scanned and no existing program is newly refused, because the programs that claim a built-in name
  (`programs/shadowed_builtins.gy`, `programs/builtin_names_as_defs.gy`) define it before using it.
- `docs/language.md` gains the ordering rule next to the shadowing rule, so the pair reads as one
  story: a built-in name is yours, and taking it means taking it everywhere below where you wrote the
  `def`.
- The faithful alternative — making codegen order-sensitive, or making built-in lookup dynamic in the
  compiled backend — is deferred, and deliberately so. Dynamic built-in resolution in an AOT pipeline
  (every built-in call becoming a table lookup that can be shadowed at run time) is a runtime-design
  decision, not a checker patch, and belongs with L11.x's value-model work. Meanwhile the language
  does not silently pick a side.
- The diagnostic is one more case where the *front end* is the place a divergence can be caught: this
  is the third such fix in six cycles (R.5 declaration order, R.8 name keying, R.12 built-in claims),
  all three invisible in the emitted IR and visible only in what the program means.

## Alternatives considered

- **Let both engines keep their reading and stay silent.** Rejected: the definition of a compiler bug
  in this project's exit-code contract is "the tool produces a wrong answer without saying so"
  (ADR 0006's reasoning), and this was exactly that, in the path agents use.
- **Make the compiled backend order-sensitive instead (resolve a built-in call by position).**
  Attractive, and the Python-faithful direction, but it only moves the problem: a call inside a
  function body depends on execution order the static emitter cannot know, so the emitter would end up
  emulating the interpreter's dynamic lookup — that is the runtime change described above, not a
  codegen tweak. Refusing until then keeps the language honest.
- **Make the interpreter hoist module-level `def`s to match codegen.** Rejected: it would silently
  change working programs (the R.12 repro would start iterating six times on *both* engines, diverging
  from CPython everywhere rather than in one backend), and it makes the interpreter less Python-like to
  match an artifact of how the emitter lays out a module.
- **Warn instead of error.** Rejected: ADR 0164's rule is errors mean "some path cannot execute this at
  all" — and here the two paths execute *different programs*, which is worse than not executing.
- **Prohibit claiming built-in names outright (reserve them).** Rejected: it reverses ADR 0199/0203,
  whose whole point is that a program owns the vocabulary of its own domain.
