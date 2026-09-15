# CHANGELOG

Single clean list of features, newest first.

## Current

- `feat(aot)`: **`for` loops over inline list literals in the LLVM AOT
  codegen** — `for x in [1, 2, 3]:` now ships in **both** backends (previously
  range-only in AOT). Lowered by unrolling one body block per constant element
  (`break`/`continue` and the `else:` clause behave exactly like `range`
  loops), mirroring the interpreter's boxed-list iteration. Adds IR checks
  (`TestIRForListCompilesWithLLC` / `TestIRForListUnrolls`), interpreter unit
  tests (`TestEvalForOverList`), and end-to-end exec tests
  (`TestExecForListLiteral`). ADR 0021.

- `feat(aot)`: **`sum`/`min`/`max`/`abs` builtins in the LLVM AOT codegen** —
  these numeric builtins now ship in **both** backends (interpreter + AOT),
  not just the interpreter. `sum`/`min`/`max` fold over an inline list
  literal's global struct (`add` / `icmp`+`select` chains, constant GEP only);
  `abs` emits `select` and constant-folds literal arguments. Adds unit IR
  checks (`TestIRSumMinMaxAbs`) and end-to-end exec tests
  (`TestExecSumMinMaxAbs`). ADR 0020.

- `feat(tools)`: **pi-loop CLI** — pi-loop now connects using **only** the
  embedded provider config (`local-vllm` @ `http://localhost:8000/v1`,
  `openai-completions`, apiKey `dummy`, model `deepseek-v4-flash`). It writes
  the provider+model into `<agentDir>/models.json` on startup and passes
  `model: "deepseek-v4-flash"` to `createAgentSession`. — `tools/pi-loop/pi-loop.mjs` connects to a
  local pi agent via the pi SDK (`createAgentSession`) and drives the AGENTS.md
  loop round by round. It persists `{round, lastCommit}` to
  `.pi-loop-state.json` on any stop (SIGINT/error/target). On restart it resumes
  at the next round if committed progress matches HEAD, or re-executes AGENTS.md
  from round 1 if no progress exists.

- `feat(interp)`: **sum builtin** — `sum([1,2,3])` → 6 over a boxed list/set. Adds TestSumBuiltin.

- `feat(interp)`: **dict keys/values methods** — `d.keys()` / `d.values()` return boxed lists in insertion order. Adds TestDictMethods.

- `feat(interp)`: **list append method** — `xs.append(x)` mutates a boxed list in place and returns it. Adds TestListAppend.

- `feat(interp)`: **string methods** — `upper()`, `lower()`, `strip()`, `split(sep?)` dispatch on boxed strings in `evalCall` (interpreter path). Adds TestStrMethods.

- `feat(interp)`: **min / max / abs builtins** — standard-library numeric builtins (`min([3,1,2])` → 1, `max` → 3, `abs(-5)` → 5), registered in the semantic analyzer. Fixes a heap-handle collision: boxed ids now start at `1 << 20` so `Repr` of a raw small int never misformats an object handle (stack overflow on `min([3,1,2])`).

- `feat(codegen)`: **constant folding** — integer-literal binary expressions (`+ - * / %` and comparisons) fold to constants at compile time (e.g. `x = 1 + 2` emits `i32 3`, no `add`). Adds TestIRConstantFolding.

- `feat(cli)`: **`--json` agent output** — `gustyc --json` emits `{"result"/"ok"/"error"/"diagnostics", "exit"}` JSON for `--eval`/`--verify`, with stable exit codes. Adds a CLI JSON test.

- `feat(interp)`: **comprehensions over range** — `[... for x in range(n)]` now evaluates (previously only list iteration worked); guards `o` nil so range dict/list comps don't panic. Adds a comprehension interpreter test.

- `feat(interp)`: **floats** — float literals evaluate to boxed floats; `+ - * /` between floats and ints produce floats; comparisons (`== < <= > >=`) support floats; `print`/`Repr` render with `%g`. Adds `allocFloat`/`floatOf` and a float interpreter test.

- `feat(interp)`: **strings** — string literals evaluate to boxed strings in the interpreter; `+` concatenates, `len(s)` counts chars, `print(s)` writes output (previously `print` discarded it). Adds `Repr` rendering for values.

- `feat(cli)`: **`gustyc` CLI/REPL** — new `cmd/gustyc` with `--eval`, `--file`, `--verify`, `--emit-llvm`, `--emit-ast`, `--target`, `--opt-level`, `--lang` (self-describing), `--version`, `--repl`, `--help`; stable exit codes (0 ok, 1 runtime, 2 parse); stateful REPL. Adds exported `lang.Parse`.

- `feat(interp+llvm)`: **`pass` (no-op statement)** — `pass` parses as a
  dedicated `PassStmt` (previously fell through to an undefined `Name`), is a
  pure no-op in both the interpreter and the LLVM AOT codegen, and is a valid
  placeholder in function/loop/branch bodies. Adds unit + integration tests.

- `feat(interp+llvm)`: **`elif` chains** — the parser already built
  `IfStmt.Elifs`, but neither the interpreter nor the LLVM codegen executed
  them, and the semantic analyzer skipped elif bodies entirely (and scoped
  if/else bodies to child scopes, hiding assignments from the enclosing
  scope). Both backends now evaluate elif branches; the analyzer analyzes
  elif bodies and analyzes if/while/for bodies in the enclosing scope so
  assignments flow outward (matching the runtime's shared-vars model).

- `feat(llvm)`: **inline list literals with constant indexing + len**
  in the AOT codegen — `[1,2,3][1]` and `len([1,2,3])` lower to a
  dedicated global struct with constant-GEP loads.

- `feat(interp)`: **modules / imports** — `import mod` loads `mod.gy`,
  evaluates it, and binds `mod` as a module namespace; top-level variables and
  functions are accessed via `mod.name` and `mod.fn(args)`. A module can
  import other modules. Parser/AST already had `ImportStmt`; the evaluator now
  runs it.
- `feat(interp)`: **class inheritance** — `class Child(Base):` inherits
  `Base`'s methods and `__init__`; instance/class method and attribute lookup
  walks the whole base chain (multi-level). An overridden method can delegate
  to the base implementation with `super()` (resolves methods on the base
  class of the currently-executing class, bound to the current instance).
- `feat(interp)`: **decorators** — `@dec` lines before a `def` apply at
  definition time (`@dec def f` -> `f = dec(f)`), bottom-up for multiple
  decorators. Parser accepts `@`, analysis infers decorator expressions, and
  the evaluator applies them to the function value.
- `feat(interp)`: **closures** — a `def` nested in a function body captures the
  enclosing scope, can be returned/stored/called (`m = add(1); m(2)`).
  Semantic analysis treats calls to function-typed variables with unknown
  return type as dynamic so assigned names aren't flagged undefined.
- `feat(interp)`: `match` `case _:` wildcard always matches (interpreter).
- `feat(lang)`: `for i in range(a, b)` iterates `a`..`b-1` end-to-end (interpreter + LLVM codegen, llc-clean).
- `feat(semantic)`: reject `break`/`continue` outside a loop with a diagnostic;
  track loop depth through `while`/`for` bodies.
- `feat(lang)`: add `break` and `continue` loop control in the parser,
  interpreter, and IR emitter (loop-label tracking; llc-clean). Loop bodies now
  go through the full statement dispatcher, fixing nested `if`/control flow
  inside `while`/`for` bodies.
- `feat(codegen)`: emit `match` as a chain of integer comparisons with
  terminators on every basic block (llc-clean IR).
- `feat(interp)`: evaluate `match` statements with literal patterns in the
  interpreter.
- `feat(interp)`: evaluate `if`/`elif`/`else`, `while`, and `for ... in
  range(n)` in the interpreter.
- `feat(interp)`: register and call user-defined functions in the
  interpreter; bind params in a fresh scope and return via `evalBody`.
- `feat(semantic)`: infer user function signatures by binding call argument
  types to parameters and analyzing the body; check argument counts.
- `feat(semantic)`: indentation-aware lexer producing `INDENT`/`DEDENT`.
- `feat(codegen)`: deterministic textual LLVM IR emitter (opaque pointers)
  replacing the crashing go-llvm `CreateCall` path; verified by `llc
  -opaque-pointers`.
- `feat(lang)`: Python-like indentation-based language with functions, control
  flow, match, print, and range.
