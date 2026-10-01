# The line table lives in the module, and the report is read back from the artifact

Status: accepted. Closes roadmap L8.5 (Debug line tables in IR), and unblocks Gap K.8 (full
tracebacks in the AOT path), which needs frames to resolve to source. Cites: ADR 0227 (a module
binding is a value or a global, never a default — the first lesson in this file is its twin),
ADR 0166 (a refusal, never an invalid module), ADR 0192/0226 (the tagged word will make every
container a runtime object: the runtime blocks exempted below are the ones that go away with it),
ADR 0184 (the pinned LLVM 20 toolchain), Gap K.6 (a statement the parser never gave a position
cannot be reported on).

## What `--debug` had been

The flag existed and did almost nothing. It added `-g` to the `cc` command that links the object
file — and DWARF is not produced there. `llc` writes `.debug_line` from `!dbg` metadata on
instructions, metadata the codegen never emitted. So `gustyc --build prog.bin prog.gy --debug`
printed a confident `built …` and produced an object whose `.debug_line` section was empty, and
nothing in the output said so. The same gap sat in `--emit-source-map`, whose own note admitted the
module "carries no !dbg records".

That is the shape of bug this ADR is mostly about: **a claim about an artifact that nobody read back
from the artifact.** Every number in this feature is now produced by reading the finished module,
and the DWARF numbers by running `llvm-dwarfdump` over the linked object.

## The decision

1. **Codegen records positions; a post-pass turns them into debug metadata.** Codegen does not
   build metadata node by node while emitting — it records, per emitted byte offset, the statement
   it was writing and the function it sits in (`dbgMark`/`dbgDefine` in `pkg/lang/codegen.go`).
   After the module is assembled and its allocas hoisted, `attachDebugInfo`
   (`pkg/lang/debug.go`) lays those marks over the finished text and writes the records. Offsets
   make the two halves agree without either one having to mirror the other's line counting.
2. **Only program functions get records.** A define belongs to the program when the module itself
   says so: some `DISubprogram` names it as `linkageName`. That single rule is what keeps the
   compiler's own blocks — the GC frame, the exception landing pads, the runtime helpers — out of
   the line table. A debugger must not stop in code the program did not write, and a traceback that
   blames `rt_frame_open` for a user statement is worse than no traceback. Tagging a runtime block
   is also invalid IR: a `DILocation` whose scope chain has no `DISubprogram` is rejected by LLVM's
   verifier, so the rule is enforced by the toolchain as well as by intent.
3. **The language is named.** `DW_AT_language` is `DW_LANG_Python`. A debugger reads it to decide
   how to print frames and where a prologue ends; claiming "Meta1" because gusty is a new language
   would be the same lie as a language field that says C. `DW_LANG_Python` is what the LLVM 20
   enumerator accepts, and the test pins it rather than trusting the enum's spelling.
4. **The account is read back, then cross-checked.** `readBackDebugInfo` scans the shipped module:
   which defines are described, how many of their instructions carry a `!dbg`, what each `!dbg`
   resolves to in the metadata, and whether the location's scope names the function it sits in. The
   emitter's own positions are kept only to *disagree* with the module: if the two do not match,
   `DebugInfo.Defect` says so in words. A report that can only say "yes" is not a report.
5. **The artifact gets the last word.** `--build --debug` runs `llvm-dwarfdump --debug-line` over
   the object it just linked and reports `line_rows`, the source lines the table covers, and the
   file names the table names. `skipped` marks a missing toolchain; it is never `ok`. The
   integration case goes one step further and asks `llvm-addr2line` where `gy_total` is, because
   that is the question a debugger will ask.
6. **Machine path first-class.** `DebugInfo` and `DWARFReport` are schema documents
   (`debugInfo`, `dwarfReport` in `gustyc --schema`), reachable as `--debug-info --json` and as the
   `debug` / `dwarf` members of `--build --json`. A drift test reflects over the Go structs and
   fails if a field is missing from the schema, so an agent's view cannot quietly fall behind the
   compiler's.
7. **The source map grows up.** Source map v2 carries the same IR-line-to-source-line table, so the
   tool that already promised "which IR line came from which source line" no longer has to say it
   cannot.

## The position half of the feature

None of this works on a statement the parser never located. Two statement shapes — assignment to
an attribute (`self.n = self.n + k`) and tuple assignment (`a, b = xs`) — were built without a
`Src`, so every one of their instructions inherited the *previous* statement's line: a line table
that blamed the `def` for the body, and an error message that would point at the wrong line today if
anything complained about such an assignment. Gap K.6 learned this with `raise` ("every traceback
said line 0"); the same hole was still open elsewhere. They now carry the position of their
leftmost target, and `TestLineRowsNameTheStatementTheyCameFrom` asserts a row for each statement of
a program that has a class, a method, a loop and prints — a statement with no row is a statement the
compiler cannot find in a debugger.

## What LLVM taught us here

Each of these was found by reading the artifact rather than the code:

* A `DISubprogram` whose `type:` points straight at a bare `!{…}` list makes `llc` print **`invalid
  subroutine type`** and then write **no line table at all** — exit code 0, no diagnostics beyond
  that line, empty `.debug_line`. The record needs two nodes: the type list, and a
  `DISubroutineType(types: !N)` naming it.
* `DILocation` nodes are *not* `distinct`; `DISubprogram` and `DICompileUnit` are. The reader
  accepts either spelling, because the reader's job is to describe a module, not to test whether it
  wrote it.
* The module is ignored entirely unless `!llvm.module.flags` carries `Dwarf Version` (5) and `Debug
  Info Version` (3).
* `DW_LANG_PYTHON`, `DW_LANG_python`, `DW_LANG_BASIC`, `DW_LANG_Carbon` are not LLVM 20
  enumerator names; `DW_LANG_Python` is.
* An instruction's `!dbg` suffix must be split off before the textual optimizer parses it, and
  re-attached when it serializes; the metadata block itself must survive as the module's *tail*, or
  the fallback optimizer (no `opt` on PATH) deletes the whole line table.
* Allocas are hoisted *after* the records are attached, which moves lines; the read-back therefore
  runs on the shipped text, so reported IR line numbers are the ones in the file an agent will open.

## What is deliberately not tagged

Global initializers run before `main`, so they carry the module's opening position; the compiler's
runtime blocks carry none; blocks with no marks inherit their function's `scopeLine` rather than
inventing a line. `ret` and `br`/`unreachable` are tagged, `invoke`/`landingpad`/`catchswitch` and
the `personality` clause are not, and a terminator inside a parenthesised operand (`unreachable, !dbg !5`)
is not an instruction — the gate is a table of opcode prefixes, and the tests assert both ways so
neither an over-broad gate (which corrupts terminator positions) nor an under-broad one (which
loses coverage) goes unnoticed.

## Alternatives rejected

* **Emit metadata inline during codegen, like clang does.** Rejected: codegen already has twelve
  emit sites across four files, and each would need to thread a builder, an insert-block and a scope
  through itself. Recording offsets and laying the records over the finished text keeps one place
  where debug info is decided, and it is the place that can be tested against the artifact.
* **`DW_LANG_META1` (or any generic language).** Rejected: it makes a debugger guess, and guesses
  badly — frame printing and prologue heuristics are language-specific. If gusty's semantics stop
  being Python-like, the right move is a real DWARF language enumerator, not "unknown".
* **Trust the emitter's bookkeeping for the report.** Rejected on evidence: the emitter was
  "successful" while `llc` wrote no table. The artifact is the only witness that matters.
* **Debug info behind an environment variable, or always on.** Rejected: `--debug` is the ask, it is
  discoverable in `--help`/`--lang`, and always-on costs metadata bytes and verification time in
  every build for a feature most builds do not use.
* **Rely on `llc -g`.** There is no such flag in LLVM 20's `llc`; the metadata is the interface.

## Consequences, and the honest limits

`--build --debug` prints two lines: what the module claims, and what the object carries. They are
different statements and both are printed. Columns are the statement's own column — the parser
records where a statement starts, not where each subexpression sits, so a column-accurate
stepping model needs positions on expressions (a follow-up, and separate from this). The line table
is capped (`DebugOptions.LineTableCap`) so a large module cannot blow up a JSON document; when it
cuts, `lines_truncated` says so while the counts stay exact. Debug info survives `opt -O2` — the
rows change, and the count goes up as code is specialised, which is what the report shows rather
than what anyone hopes.
