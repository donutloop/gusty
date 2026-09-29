# 0209. A runtime block travels with the module that references it

## Status

Accepted. Implemented this cycle in `pkg/lang/codegen.go` (`runtimeBlockReferenced`, the assembly
step, `iterableIsRuntimeString`); pinned by `pkg/lang/runtime_block_emit_test.go` and
`integration/runtime_block_emit_test.go`. Roadmap Gaps R.2 (closed) and R.16 (opened).

## Context

The roadmap entry blamed the async path:

```gusty
async def f(x):
    return x + 1
async def g():
    await f(1)
    while True:
        return "ok"
print(await g())
```

`--verify-llvm` caught it — *"LLVM rejected the module; this is a compiler bug, not a source error"* —
on a module containing `call i32 @rt_str_intern2(...)` with no definition of that helper, and the entry
concluded "this is the await path's intern accounting, not the loop's", because dropping either
ingredient made the program compile.

Both ingredients were red herrings. The same failure reproduces with no `async`, no `await`, and no
loop:

```gusty
def txt():
    return "hi"

for c in txt():
    print(c)
```

What the two programs share is a call site that emits `@rt_str_intern2` **without setting
`g.heapUsed`** — the "function known to return a string returns its interned index" path (correct
behaviour, ADR 0174) and one comparison fold. `heapRuntimeIR`, which *defines* the helper, is appended
to the module only when that flag is set. So the emission decision lived in a boolean that every one of
several codegen paths had to remember, and a path that forgot produced an invalid module — which the
exit-code contract (ADR 0006) classifies as the tool's failure, and which surfaced to users as an `llc`
error and a link failure.

Chasing R.15 (ADR 0208) is what surfaced this: the fix there routed unrolled-loop elements through the
same interning helper, and the neighbouring shape — iterating a *call* that returns a string — then
failed with the R.2 signature in a deterministic, await-free way.

## Decision

**Which runtime blocks a module carries is derived from the module, not from a flag.**

1. At assembly, the generated body is scanned: if the emitted code mentions any name a block *defines*
   — `define … @name(` or `@name = internal global/constant` — that block is emitted. Applied to
   `heapRuntimeIR`, `raiseRuntimeIR` and `floatRuntimeIR`. The flags stay (a container read raises with
   no explicit `raise`; printing a float wants the formatter) but they can now only *add*, never omit:
   no block is skipped while the module references one of its names.
2. **The name list is parsed from the blocks themselves**, not maintained by hand. A new helper cannot
   be referenced without being emitted, because there is nothing to remember.
3. **Data globals count as references.** `@str_tab`, `@gc.roots` and friends are found by the same
   scan, so a module that reaches for a block's table without its accessors is not left with a
   dangling global.
4. **A silent wrong answer becomes a refusal.** With the module now valid, the runtime-string loop
   compiled and printed *nothing*: `def txt(): return "hi"` hands back its `@str_tab` index, and the
   counted-loop fall-through reads that index as a repeat count. Where an AOT backend cannot do what the
   interpreter does, it says so with the house message — what is unsupported, which backend runs it, and
   what to write instead — rather than emitting a program that quietly does nothing
   (`iterableIsRuntimeString`, opened as **R.16**). String *literals* keep working (ADR 0208).

## Consequences

- The R.2 repro compiles and runs: no `llc` rejection, no link failure. The async program's compiled
  output is still wrong — it prints `0` where the interpreter and CPython print `ok` — which is Gap R.1
  (compiled coroutines run eagerly) and is asserted as a known-divergent-but-buildable state rather than
  papered over; `TestR2ReproCompilesAndRuns` skips loudly if the compiled leg ever starts agreeing, so
  the two gaps cannot be confused with each other.
- `def txt(): return "hi"` + `print(txt())` prints `hi` on the interpreter, in the compiled binary, and
  in CPython.
- The class is closed rather than the instance: the unit test asserts, over seven shapes reaching
  helpers through different paths (function body, container write, unrolled literal, raise, index read),
  that **every `@rt_*`/`@gc.*` call in a module has a matching `define` or `declare`**. That is the
  invariant `llc` enforces at build time, now checked where we can see it.
- `runtimeBlockReferenced` is tested as a predicate, including its two failure directions: a module that
  calls only libc must not gain the heap runtime (the flags exist to keep modules lean, and a blanket
  emission rule would throw that away), and a *mention* in a comment is not a reference.
- Measured during this cycle, unchanged: nothing in the corpus regressed, and the extra blocks are
  emitted only where referenced.

## Alternatives considered

- **Set `g.heapUsed = true` at the two call sites that forgot.** Rejected as the archetype of the wrong
  fix: the defect is that N call sites share one flag with the emitter, and each new call site is another
  chance to reproduce an invalid module. The flags stay for the cases where a program asks for a block
  without naming it, but correctness no longer depends on remembering.
- **Emit every runtime block into every module.** Rejected: those blocks are thousands of lines, and the
  existing per-block gating exists so a program that prints no float doesn't carry the snprintf/strtod
  machinery (and an agent reading `--emit-llvm` sees the module it earned). The derived rule keeps
  minimality *and* completeness.
- **Trust `--verify-llvm` to keep catching these.** Rejected: the gate works (it did), but it converts
  this tool's bug into the user's error message, and only on the paths someone runs. The exit-code
  contract's position is that a verifier rejection should be unreachable, not merely well-reported.
- **Leave `for c in txt()` compiling to a zero-iteration loop now that it "works".** Rejected: a program
  that prints nothing where every other engine prints `h i` is the worst outcome available — it looks
  like success. Refusing with an actionable message is the same stance as ADR 0166's, and R.16 tracks the
  real implementation (runtime string values, L11.5).
