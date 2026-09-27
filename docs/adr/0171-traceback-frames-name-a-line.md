# 0171. A traceback names a line, and the parser is where that starts

Status: Accepted
Date: 2026-09-27
Audience: compiler maintainers, users reading a report after a crash

## Context

The uncaught-exception report (ADR 0169) printed the exception line and nothing else:

```
Traceback (most recent call last):
IndexError: index out of range
```

Two things were wrong, and the second one predated the AOT report completely:

1. **Compiled programs printed no frame at all.** The raise path had a flag, a code and a
   message — no position. The interpreter, by contrast, printed
   `  File "prog", line N, in fn` lines.
2. **`raise` statements reported line 0 — on the interpreter too.** The parser built
   `&RaiseStmt{Expr: ex}` and never filled `Src`, so the interpreter's own traceback for
   the most common failure in the language said:

   ```
     File "prog", line 0, in boom
   ```

   Nobody noticed because the try/except tests caught their exceptions before printing.
   A report that answers "where" with `0` is worse than no report: it looks authoritative.

While wiring the frame through the AOT, a third bug surfaced from the module verifier:
`strConst` escaped `\` and `\n` but not `"`. A frame string contains quotes
(`  File "prog", line 3, in boom`), so the emitted LLVM literal ended early and the module
died with a nonsense error — `constant expression type mismatch: got type '[7 x i8]' but
expected '[31 x i8]'`. Any *program* string containing a double quote had the same bug; the
frame was simply the first string in this repo that needed one.

## Decision

**1. Statements carry the position they were written at, or the parser has lied.**
`RaiseStmt.Src` is set from the `raise` keyword's token span. Every AST node that can fail
at runtime must be able to answer "which line", because that is what turns a stack trace
into a bug report.

**2. Raise sites emit a pre-rendered frame into `@exn_frame`.** `setExn` now takes the
site's `Span` and, when it is non-zero, stores a compile-time string

```llvm
@.strN = private unnamed_addr constant [31 x i8] c"  File \22prog\22, line 3, in boom\00"
```

into `@exn_frame`; `rt_die` prints header, frame (when non-null), then the exception line.
Pre-rendering keeps `rt_die` format-free — no printf, no varargs in the raise path, and the
verifier checks the literal's length.

**3. An unknown position prints nothing rather than `line 0`.** `raiseFrame` returns "" for
a zero span and the store writes `null`; a fabricated line is a lie in a report whose whole
job is accuracy.

**4. The compiled report shows the raise site's own frame.** The interpreter prints one
frame per stack level. Matching that needs the call stack at the raise point — either the
DWARF line tables of L8.5 or an explicit frame stack pushed at each call site. Recorded as
the remaining half of Gap K.8, not silently claimed.

**5. `strConst` escapes the full set that LLVM's lexer needs**: `\` → `\\`, `"` → `\22`,
newline → `\0A`, tab → `\09`, CR → `\0D`. The length stays the *decoded* byte count.

## Rationale (agentic)

An agent that gets `IndexError: index out of range` cannot localise the fault and will
retry blind; one that gets `line 3, in boom` can patch the file. And a diagnostic whose
line number is fabricated is worse than none, because it is trusted. The frame is part of
the machine-visible contract now: `docs/operations.md` § Runtime failures documents its
exact shape, and `TestTracebackFramesNameTheRaiseSite` asserts both backends print it.

## Codegen / IR implications

```llvm
@exn_msg   = internal global i8* null
@exn_frame = internal global i8* null

define internal void @rt_die(i8* %msg) {
entry:
  %nlpre = getelementptr ... @.rt_die.nl ...
  %hdr   = getelementptr ... @.rt_die.hdr ...
  call i64 @write(i32 2, i8* %hdr, i64 %hlen)          ; "Traceback (most recent call last):\n"
  %fp = load i8*, i8** @exn_frame
  %hasframe = icmp ne i8* %fp, null
  br i1 %hasframe, label %withframe, label %withoutframe
withframe:
  call i64 @write(i32 2, i8* %fp, i64 %flen)
  call i64 @write(i32 2, i8* %nlpre, i64 1)
  br label %withoutframe
withoutframe:
  ... %m = select (%msg == null ? "gusty: uncaught exception" : %msg) ...
  call i64 @write(i32 2, i8* %m, i64 %len)
  call i64 @write(i32 2, i8* %nlpre, i64 1)
  ret void
}
```

`g.curFnSrc` tracks the source-level name of the function being lowered (empty at module
level ⇒ `<module>`), so the frame says `in boom` rather than the mangled IR symbol name.

## Alternatives rejected

- **Print `line 0` when the span is unknown** — a fabricated location in a report whose
  value is location. Rejected.
- **Use the mangled IR symbol (`Class_method`, `mod$fn`) in the frame** — accurate about the
  module, useless about the program. The frame names the *source* function.
- **Run `addr2line`/DWARF at raise time** — L8.5's job, and it needs a symbolized runtime;
  pre-rendered constants get correct locations today for the one frame that matters most.
- **Escape quotes at the call sites instead of in `strConst`** — the bug belongs to the
  emitter, so the fix does too; every current and future constant string gets it for free.

## Consequences

- `pkg/lang/exceptions_test.go` pins: the parser's raise span, the frame's rendering, and
  the quote escaping (`\22` + module verification).
- `integration/uncaught_exception_test.go` asserts both backends print the same frame line
  and the same final line.
- Gap K.8 stays open (🟨 PARTIAL) for the missing caller frames; L8.5 is its remaining work.
- Program strings containing `"` now compile at all — previously such a program failed the
  module verifier with a confusing array-length error.
