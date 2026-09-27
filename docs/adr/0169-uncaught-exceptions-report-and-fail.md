# 0169. An uncaught exception is a report and a non-zero exit, on both backends

Status: Accepted
Date: 2026-09-27
Audience: compiler maintainers, agents/scripts consuming the CLI

## Context

```py
xs = [1, 2, 3]
xs[9] = 5
```

On the interpreter this printed a traceback and exited 1. Compiled, it printed **nothing
and exited 0**. Any script that ran the binary — CI, an agent, a Makefile — concluded the
program had succeeded. Four separate defects sat behind that one behaviour:

1. **The raise-exit block was `ret i32 0`.** And there was no message global beside
   `@exn_code`: the raise path stored a flag and a numeric code, so even a willing
   reporter could not say *what* had been raised.
2. **The checker's `exceptions` map was declared and never populated.** So
   `raise ValueError("boom")` is a valid interpreter program that fails AOT compilation
   with `undefined name "ValueError"` — the two front ends disagreed about whether a
   documented built-in class exists.
3. **Runtime errors were not exceptions in the interpreter.** An out-of-range read/write
   returned a plain error that aborted instead of unwinding, so `except IndexError:` did
   not fire — while the AOT did catch it. The inversion (the "slow" backend more correct
   than the fast one) is exactly what the conformance corpus had been missing. On the AOT
   side, *reads* were unchecked too: `xs[5]` and `d[missing]` loaded whatever sat in that
   slot and printed `0`.
4. **The interpreter wrote its traceback to stdout**, mixing diagnostics with program
   output, so `prog | sort` sorted tracebacks.

Found on the way: `rt_dict_has` walked the flat `[key, value]` array two words at a time
but bounded the walk by the *entry count*, so only the first ⌈count/2⌉ keys were ever
found. `3 in {1: 2, 3: 4}` was false in compiled binaries.

## Decision

**1. An escaped exception is reported on stderr and the process exits non-zero — on both
backends, with the same last line.**

```
Traceback (most recent call last):
IndexError: index out of range
```

The AOT emits `rt_die` (in the raise runtime block, gated on "this program can raise")
which writes the header, the message, and a newline to **fd 2 via `write(2)`** — not
`printf`, and not `fprintf(stderr, …)`, because `stderr` is a libc *symbol* on glibc but a
macro on macOS and a text macro on Windows; the fd is the only stream that is the same
idea everywhere. `main`'s raise-exit path then `ret i32 1`.

**2. `@exn_msg` travels with the flag.** Every raise site goes through one helper
(`setExn` → `raiseTo`) that stores the flag, the code, and a `"<Type>: <message>"` string,
so the report can never disagree with the code a handler matches on. Compiler-detected
raises carry their own text (`"IndexError: index out of range"`,
`"KeyError: key not found"`).

**3. Runtime errors are typed exceptions in both backends.** The interpreter raises
`exnError("IndexError", …)` / `KeyError` / `TypeError` for the operations that can fail,
and the AOT emits a test around the operation: `checkIndexRead` (bounds, `rt_list_len`)
and `checkKeyRead` (membership, `rt_dict_has`) branch to the innermost handler — or out to
the uncaught path — instead of loading a garbage word.

**4. One exception-class table.** `pkg/lang/exceptions.go` holds the class names and their
numeric codes; the interpreter's `isExnClass`, the codegen's `exnCode`, and the checker's
name set all come from it. A class derived from an exception is registered as raisable.
Before this, adding a class meant editing three files and remembering the checker.

**5. stdout is program output only.** The interpreter's traceback moved to stderr.

**Deliberate divergence:** an assignment whose target kind is *statically* impossible
(`s[0] = "z"` on a string, `s[0] = 1` on a set) is a compile-time diagnostic in AOT
(ADR 0166) and a catchable `TypeError` in the interpreter. A program can therefore be
interpretable and uncompilable there; the message names both the construct and the
backend that supports it.

## Rationale (agentic)

Exit codes and streams are the CLI's ABI for scripts. `exit 0` on a trapped program is
not a cosmetic bug: it silently converts a crash into a success in every pipeline that
consumes us. Keeping the report on stderr keeps `--json` on stdout meaningful and lets
`prog 2>/dev/null` isolate program output — the same rule the build path follows after
Gap K.7 (`docs/operations.md` § Runtime failures).

## Codegen / IR implications

```llvm
@exn_msg = internal global i8* null        ; in raiseRuntimeIR, gated on raiseUsed||heapUsed

define internal void @rt_die(i8* %msg) { … write(2, hdr) write(2, msg) write(2, "\n") … }

; d[k] read — membership test before the get, so a miss raises instead of yielding 0
  %ok  = call i32 @rt_dict_has(i32 %h, i32 %k)
  %z   = icmp eq i32 %ok, 0
  br i1 %z, label %rd.bad, label %rd.ok
rd.bad:
  store i32 1, i32* @exn_flag
  store i32 3, i32* @exn_code            ; KeyError
  store i8* @.strN, i8** @exn_msg         ; "KeyError: key not found"
  br label %main.raiseexit               ; or the innermost handler
rd.ok:
  %v = call i32 @rt_dict_get(i32 %h, i32 %k)

main.raiseexit:
  %m = load i8*, i8** @exn_msg
  call void @rt_die(i8* %m)
  ret i32 1
```

A program that cannot raise carries none of this: `print(42)` still emits no `rt_die`, no
`@exn_msg`, so the golden IR for clean programs is unchanged.

## Alternatives rejected

- **Keep silent `exit 0` after a raise** — indefensible for a compiler whose consumers are
  scripts and agents.
- **`printf`/`fprintf(stderr)` for the report** — stdout pollutes data streams, and the
  `stderr` symbol is not portable across the platforms this repo targets.
- **Set the flag inside `rt_get_elem`/`rt_dict_get`** (one check, all callers) — rejected:
  the iteration paths call those helpers with in-range indexes, and a flag set by a helper
  whose callers never test it leaks into the next `checkExn` after a user call. The test
  belongs at the read site, where control can branch.
- **Leave reads unchecked and document "undefined value for out-of-range index"** — this
  repo has already been bitten twice by unchecked container memory (`rt_free(0)`,
  out-of-range writes); a wrong *value* is worse than a raise, because it propagates.
- **Report on stdout when `--json` is off** — the stream split is the contract, not a
  verbosity setting.

## Consequences

- `docs/operations.md` § Runtime failures documents the stream + exit-code contract;
  `docs/language.md` § Exceptions documents typed runtime errors and the divergence above.
- Gap K.8 records the remaining half: the AOT report has no `File "prog", line N` frame
  until DWARF line tables land (L8.5).
- The raise runtime's `write`/`strlen` declarations are emitted only for programs that can
  raise, matching how the heap runtime is gated.
