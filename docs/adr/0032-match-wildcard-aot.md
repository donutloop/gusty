# ADR 0032: `case _:` wildcard in the LLVM AOT `match` statement

## Decision

The interpreter's `match` treats a `case _:` pattern (a `*Name` with value
`"_"`) as a wildcard that matches any subject. The LLVM AOT codegen lowered
every case pattern through `g.value(b, c.Pattern)` and compared the subject
with `icmp eq`, so `case _:` became a normal comparison against an undefined
`_` global — the wildcard only matched when the subject happened to equal that
global (0), a silent wrong-output bug. Detect a `*Name` pattern with value
`"_"` in the codegen `match` case loop and lower it as `pat := sub`, making the
`icmp eq` compare the subject to itself (always true) — an unconditional
branch to the case body, exactly like the interpreter.

## Codegen/IR implications

- For every case, `pat` is initialized to the subject register `sub`.
- If the pattern is NOT a `_` wildcard (any non-`_` value node), `pat` is
  recomputed via `g.value(b, c.Pattern)` as before; if it is a `_` wildcard,
  `pat` stays `sub`, so `icmp eq i32 sub, sub` is always true.
- The existing `br i1 cmp, label bodyL, label fallL` is unchanged — the
  always-true `cmp` makes the wildcard branch unconditionally to the case body.
- Non-wildcard cases (`case 1:`, etc.) behave identically to before.
- The AOT path stays i32-only and allocation-free — no new runtime support.

## Alternatives rejected

- Emitting a separate unconditional `br label bodyL` for wildcard cases —
  rejected: reusing the existing `cmp`/`br` structure via `pat := sub` is one
  line of lowering, allocation-free, and needs no new control-flow plumbing.
- Documenting `case _:` as an AOT limitation — rejected: silently comparing
  against an undefined `_` global is incorrect, not a documented limitation;
  the AOT path should match the interpreter's wildcard semantics exactly.
