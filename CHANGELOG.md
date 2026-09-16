# CHANGELOG

Single clean list of features, newest first.

## Current
- `feat(sorted-builtin)`: **`sorted(list)`** returns a copy of the list
  with elements sorted (interpreter path; ints by value, strings by content
  via `lessVal`). `sorted([3, 1, 2])[0]` is 1. Adds an Eval unit test.
  ADR 0068.
- `feat(str-isspace)`: **`.isspace()`** returns 1 if every rune is
  whitespace (and the string is non-empty), else 0, in **both** paths: the
  interpreter checks `unicode.IsSpace`; the codegen folds it to
  `i32 1`/`i32 0` (`print("   ".isspace())` emits `i32 1`). Adds
  Eval/IR/integration tests. ADR 0067.
- `feat(str-isalnum)`: **`.isalnum()`** returns 1 if every rune is
  alphanumeric (and the string is non-empty), else 0, in **both** paths:
  the interpreter checks `unicode.IsLetter`/`unicode.IsDigit`; the codegen
  folds it to `i32 1`/`i32 0` (`print("abc123".isalnum())` emits
  `i32 1`). Adds Eval/IR/integration tests. ADR 0066.
- `feat(str-partition)`: **`.partition(sep)`** returns a list
  `[head, sep, tail]` split at the first occurrence of `sep` (or
  `[s, "", ""]` when absent) in the interpreter path.
  `"a-b-c".partition("-")[0]` is "a". Adds an Eval unit test. ADR 0065.
- `feat(str-islower-isupper)`: **`.islower()`** / **`.isupper()`** return
  1 if there is at least one cased rune and all cased runes are lowercase /
  uppercase, else 0, in **both** paths: the interpreter scans cased runes;
  the codegen folds to `i32 1`/`i32 0` (`print("abc".islower())` emits
  `i32 1`). Adds Eval/IR/integration tests. ADR 0064.
- `feat(str-isalpha)`: **`.isalpha()`** returns 1 if every rune is
  alphabetic (and the string is non-empty), else 0, in **both** paths: the
  interpreter checks `unicode.IsLetter`; the codegen folds it to
  `i32 1`/`i32 0` (`print("abc".isalpha())` emits `i32 1`). Adds
  Eval/IR/integration tests. ADR 0063.
- `feat(str-isdigit)`: **`.isdigit()`** returns 1 if every rune is a digit
  (and the string is non-empty), else 0, in **both** paths: the interpreter
  checks `unicode.IsDigit`; the codegen folds it to `i32 1`/`i32 0`
  (`print("123".isdigit())` emits `i32 1`). Adds Eval/IR/integration
  tests. ADR 0062.
- `feat(list-count)`: **`.count(value)`** returns the number of
  occurrences of `value` in a list (interpreter path; elements compare by
  string content via `dictKeyEq`). `["a", "b", "a"].count("a")` is 2.
  Adds an Eval unit test. ADR 0061.
- `feat(str-swapcase)`: **`.swapcase()`** swaps the case of each rune in
  **both** paths via a shared `swapcase` helper (`strings.Map` using
  `unicode.IsUpper`), folded in `stringConst`/`stringVal` and the call
  dispatch (`print(len("HeLLo".swapcase()))` emits `i32 5`). Adds
  Eval/IR/integration tests. ADR 0060.
- `feat(str-title)`: **`.title()`** capitalizes the first rune of each
  whitespace-separated word in **both** paths via a shared `title` helper
  (`strings.Map` tracking the previous rune), folded in `stringConst`/
  `stringVal` and the call dispatch
  (`print(len("hello world".title()))` emits `i32 11`). Adds Eval/IR/
  integration tests. ADR 0059.
- `feat(str-capitalize)`: **`.capitalize()`** uppercases the first rune and
  lowercases the rest in **both** paths via a shared `capitalize` helper:
  the interpreter calls it directly; the codegen folds it in
  `stringConst`/`stringVal` and the call dispatch
  (`print(len("hello".capitalize()))` emits `i32 5`). Adds Eval/IR/
  integration tests. ADR 0058.
- `feat(str-rfind)`: **`.rfind(sub)`** returns the index of the last
  occurrence of `sub` (or -1 if absent) in **both** paths: the interpreter
  applies `strings.LastIndex`; the codegen constant-folds it to an `i32`
  literal (`print("abcabc".rfind("bc"))` emits `i32 4`). Adds
  Eval/IR/integration tests. ADR 0057.
- `feat(dict-get)`: **`.get(key[, default])`** returns the value for `key`,
  or `default` when absent, mirroring Python's `dict.get`. Dict keys now
  compare by string content (`dictKeyEq`), fixing dict indexing `d[key]`
  which previously failed on unboxed string keys. Adds an Eval unit test.
  ADR 0056.
- `feat(str-join)`: **`.join(list)`** joins a list of string elements with
  the receiver as separator in **both** paths: the interpreter evaluates the
  list arg and applies `strings.Join`; the codegen folds the receiver and a
  constant list of string elements (`print("-".join(["a", "b", "c"]))`
  emits the global "a-b-c"). Adds Eval/IR/integration tests. ADR 0055.
- `feat(str-lstrip-rstrip)`: **`.lstrip()`** / **`.rstrip()`** remove leading
  / trailing whitespace (any `unicode.IsSpace` rune) in **both** paths: the
  interpreter applies `TrimLeftFunc`/`TrimRightFunc`; the codegen
  constant-folds them to a trimmed string constant
  (`print(len("  hi  ".lstrip()))` emits `i32 4`). Adds Eval/IR/integration
  tests. ADR 0054.
- `feat(str-count)`: **`.count(sub)`** returns the number of non-overlapping
  occurrences of `sub` in **both** paths: the interpreter applies
  `strings.Count`; the codegen constant-folds it to an `i32` literal
  (`print("ababab".count("ab"))` emits `i32 3`). Adds Eval/IR/integration
  tests. ADR 0053.
- `feat(str-startswith-endswith)`: **`.startswith(sub)`** / **`.endswith(sub)`**
  return 1 or 0 in **both** paths: the interpreter applies
  `strings.HasPrefix`/`strings.HasSuffix`; the codegen constant-folds them to
  `i32 1`/`i32 0`. Adds Eval/IR/integration tests. ADR 0052.
- `feat(str-find)`: **`.find(sub)`** returns the index of the first
  occurrence of `sub` (or -1 if absent) in **both** paths: the interpreter
  applies `strings.Index`; the codegen constant-folds it to an `i32` literal
  (`print("abcabc".find("bc"))` emits `i32 1`). Adds Eval/IR/integration
  tests. ADR 0051.
- `feat(str-replace)`: **`.replace(old, new)`** replaces every occurrence in
  **both** paths: the interpreter evaluates both args as strings and applies
  `strings.ReplaceAll`; the codegen constant-folds the receiver and both
  constant string args (`print("aXbXc".replace("X", "-"))` emits the folded
  global "a-b-c"). Adds Eval/IR/integration tests. ADR 0050.
- `feat(str-eq)`: **string equality `==` / `!=`** folds in **both** paths. The
  interpreter compares string contents (`sval`) instead of handles; the
  codegen constant-folds via `stringVal` before the int comparison. Adds
  Eval/IR/integration tests. ADR 0049.
- `feat(str-split)`: **`.split()` folds to substring count** — `dictMethodElems`
  gains a `split` case (space-separated, matching the interpreter default), so
  `len("a b c".split())` -> 3 folds in the AOT codegen. Adds Eval/IR/
  integration tests. ADR 0048.
- `feat(str-index)`: **string indexing** `"abc"[1]` -> 98 ('b') in **both**
  paths. The interpreter's Index gains a `str` kind returning the char code;
  the codegen's Index constant-folds `*StrLit` via `stringVal`. Adds
  Eval/IR/integration tests. ADR 0047.
- `feat(dict-minmax)`: **`min`/`max` now fold dict-method Call args** — the
  min/max elems resolution wires `dictMethodElems`, so
  `max({1: 2, 3: 4}.keys())` -> 3 and `min({1: 2, 3: 4}.values())` -> 2 in
  the AOT codegen. Adds Eval/IR/integration tests. ADR 0046.
- `feat(dict-items)`: **`.items()` on constant dict literals** — the
  interpreter now builds a list of `[key, value]` pairs, and the AOT codegen
  folds `len({1: 2, 3: 4}.items())` to the pair count 2 (via `dictMethodElems`
  returning keys). Adds Eval/IR/integration tests. ADR 0045.
- `feat(list-append)`: **`.append()` on constant list literals** constant-folds
  in the AOT codegen: `[1, 2, 3].append(4)` lowers to `[1, 2, 3, 4]`, so
  `len`/`sum` work. Adds a list-method branch in the `Attr` callee path and
  extends `dictMethodElems` to fold append. Adds Eval/IR/integration tests.
  ADR 0044.
- `feat(dict-methods)`: **`.keys()` and `.values()`** on constant dict literals
  constant-fold in the AOT codegen: `{1: 2, 3: 4}.keys()` lowers to a list
  `[1, 3]` and `.values()` to `[2, 4]`, so `len(...)`/`sum(...)` fold. Adds a
  `dictMethodElems` helper used by `len` and `sum`, and a dict-method branch
  in the `Attr` callee path. Adds Eval/IR/integration tests. ADR 0043.
- `feat(print-string-methods)`: `print` now lowers any constant-foldable
  string arg via `g.stringVal(a)` (not just `*StrLit`), so
  `print("AbC".upper())` emits the folded string global. Resolves the print
  limitation in ADR 0042. Adds IR test `TestIRStrMethodPrintLowersString`
  and integration test `TestExecStrMethodPrint`.
- `feat(string-methods)`: **constant-fold string methods** in the AOT codegen:
  `.upper()`, `.lower()`, `.strip()` on constant string literals fold at
  codegen time (package-level `stringConst` Call-folding + `irGen` `stringVal`
  Call-folding), so `len("AbC".upper())` -> 3. Adds IR tests
  (`TestIRStrMethod*`), interpreter Eval tests (`TestGenStrMethod`), and an
  integration test (`TestExecStrMethod`). ADR 0042.
- `feat(lambda)`: **`lambda` anonymous functions** in **both** the interpreter
  and the AOT codegen. `(lambda x: int: x + 1)(5)` -> 6 and `f = lambda x: int:
  x * 2; f(3)` -> 6. A lambda is lowered to a closure exactly like `def`: the
  interpreter allocs a closure capturing the environment; the codegen emits an
  anonymous FuncDef (`lambda_N`) at module level (globals builder) and a call
  to it. Named lambdas resolve via a new `g.lambdas` map. Adds Eval tests
  (`TestGenLambdaInline`/`Named`), IR tests (`TestIRLambda*`), and integration
  tests (`TestExecLambda*`). ADR 0041.
- `feat(codegen)`: **Comprehension indexing** — `d[key]` on a lowered **dict
  comprehension** now folds to the mapped constant value (keys recorded via a
  new `compKeys` map), erroring `key not found` when absent; `s[key]` on a
  **set comprehension** is a membership test returning the element when
  present, erroring `not in set` otherwise (interpreter semantics). Previously
  the `Index` case treated every comprehension as a list-shaped struct and
  GEP'd positionally, which was wrong for sets/dicts. Adds IR tests
  (`TestIRDictComprehensionIndex*`, `TestIRSetComprehensionIndex*`) and
  interpreter Eval tests (`TestGenDictComprehensionIndex`,
  `TestGenSetComprehensionIndex`). ADR 0040.
- `feat(codegen)`: **`sum`/`min`/`max` over set and dict literals** now fold
  elements/keys in the AOT LLVM codegen (previously only list literals and
  comprehension results were supported). Set literals fold `Elems`, dict
  literals fold `Keys` (interpreter semantics). Adds IR tests
  (`TestIRSumSetLiteral*`, `TestIRMinDictLiteral*`, `TestIRMaxSetLiteral*`)
  and interpreter Eval tests (`TestGenMinSetLiteral`, `TestGenMaxSetLiteral`).
- `feat(codegen)`: **Set and dict comprehensions** now lower to dedicated
  `@.setN` / `@.dictN` globals in the AOT LLVM codegen (previously
  interpreter-only). Set comprehensions deduplicate folded elements; dict
  comprehensions fold key/value pairs into a `{i32, [n x i32], [n x i32]}`
  global. Both support `len(...)` via the `compLen` map. Adds IR codegen tests
  (`TestIRSetComprehension*`, `TestIRDictComprehension*`) and interpreter Eval
  tests (`TestGenSetComprehensionLen`, `TestGenDictComprehensionLen`).

- `feat(aot)`: **`len` and list-index over runtime-variable-element lists** —
  the AOT codegen's `emitList` required every list element to be an integer
  literal, so `len([a, b])` and `[a, b][0]` failed with `list literal elements
  must be integers` even though the interpreter accepts them. `len([a, b])`
  now returns the element count directly (no `emitList` global), and
  `[a, b][i]` evaluates the indexed element via `g.value` directly. Adds IR
  checks (`TestIRRuntimeLenIndex`) and exec tests (len + index runtime cases).
  ADR 0035.


- `feat(aot)`: **`min`/`max`/`sum` over lists with runtime-variable elements** —
  the interpreter folds `min`/`max`/`sum` over any list, but the AOT codegen
  required every element to be an integer literal — `min([a, b])` failed with
  `list literal elements must be integers`. The codegen now lowers each element
  directly via `g.value` (an `icmp`+`select` chain for `min`/`max`, an `add`
  chain for `sum`), so runtime-variable elements work exactly like literals.
  Adds IR checks (`TestIRRuntimeListAgg`) and exec tests
  (`TestExecSumMinMaxAbs` runtime-element cases). ADR 0034.


- `feat(aot)`: **zero-argument `print()` in the LLVM AOT codegen** —
  the interpreter's `print` with no arguments writes nothing (the arg loop is
  empty), but the AOT codegen rejected `print()` with `print needs an
  argument`. The codegen now accepts zero-argument `print()` and emits no
  `printf` — a no-op that mirrors the interpreter exactly. Adds IR checks
  (`TestIRZeroArgPrintCompilesWithLLC`), an interpreter stdout-capture unit
  test, and exec tests (`TestExecMultiArgPrint` zero-arg cases). ADR 0033.


- `feat(aot)`: **`case _:` wildcard in the LLVM AOT `match` statement** —
  the interpreter's `match` treats `case _:` as a wildcard that matches any
  subject, but the AOT codegen lowered `_` as a normal pattern — it compared
  the subject to an undefined `_` global (wrong: `case _:` only matched when
  the subject was 0). The codegen now detects a `*Name` pattern with value
  `"_"` and lowers `pat := sub`, making the `icmp eq` compare the subject to
  itself (always true) — an unconditional branch to the case body, exactly like
  the interpreter. Adds IR checks (`TestIRMatchWildcardCompilesWithLLC`) and
  exec tests (`TestExecMatchWildcard`). ADR 0032.


- `feat(aot)`: **string-literal `print` arguments in the LLVM AOT codegen** —
  the interpreter's `print` prints strings via `Repr` (e.g. `print("hi")`
  writes `hi`), but the AOT codegen emitted `printf("%d\n", <str-ptr>)` —
  passing a string pointer to a `%d` format (wrong output). The codegen now
  detects string-literal `print` arguments and emits `printf("%s\n", <str>)`
  for them, keeping `%d\n` for integer args, so `print("hi")` writes `hi\n`
  and `print(1, "hi", 2)` writes `1\nhi\n2\n` in the AOT path exactly like
  the interpreter. Adds IR checks (`TestIRMultiArgPrintCompilesWithLLC`),
  interpreter stdout-capture unit tests, and exec tests (`TestExecMultiArgPrint`).
  ADR 0031.


- `feat(aot)`: **multi-argument `print` in the LLVM AOT codegen** —
  the interpreter's `print` already wrote every argument to stdout (one per
  line), but the AOT codegen silently dropped all but the first argument — it
  emitted `printf` only for `Args[0]`. The codegen now emits one `printf("%d\n")`
  per argument, so `print(1, 2, 3)` writes `1\n2\n3\n` in the AOT path exactly
  like the interpreter. Adds IR checks (`TestIRMultiArgPrintCompilesWithLLC`),
  an interpreter stdout-capture unit test (`TestEvalMultiArgPrint`), and
  end-to-end exec tests (`TestExecMultiArgPrint`). ADR 0030.


- `feat(aot)`: **`%` modulo operator in the LLVM AOT codegen** —
  the interpreter already evaluated `%` (signed remainder), but the AOT
  runtime path only folded it on constant operands and rejected it on runtime
  operands (`unsupported operator "%"`). The codegen now lowers `%` to `srem`,
  closing the interpreter-vs-AOT gap: `17 % 5` folds to `2` at compile time and
  `x % 5` lowers to `srem i32` at runtime. Adds IR checks (`TestIRModuloCompilesWithLLC`),
  interpreter unit tests, and end-to-end exec tests (`TestExecModulo`). ADR 0029.


- `feat`: **multi-argument range iterables in comprehensions** —
  `range(start, stop)` and `range(start, stop, step)` as comprehension
  iterables in both the interpreter and the LLVM AOT codegen. The AOT path
  unrolls the comprehension body over the stepped range at compile time; the
  interpreter's `rangeBounds` and `range` builtin accept 1-3 args with a step.
  Adds IR checks (`TestIRMultiArgRangeComprehension`) and end-to-end exec tests
  (`TestExecMultiArgRangeComprehensionAOT`). ADR 0028.


- `feat(aot)`: **aggregate builtins over comprehensions** — `len`, `sum`,
  `min`, `max` now accept a lowered comprehension result in the LLVM AOT
  codegen. `len([...])` loads the stored count field; `sum`/`min`/`max` fold
  the folded comprehension elements (all constants) to a single constant at
  codegen time. Adds IR checks (`TestIRAggregateOverComprehension`,
  `TestIRSumFoldsComprehension`) and end-to-end exec tests
  (`TestExecAggregateOverComprehensionAOT`). ADR 0027.


- `feat`: **ternary conditional expressions** (`then if cond else otherwise`)
  in both the interpreter and the LLVM AOT codegen. The condition is an
  or-level expression; the `else` branch is a full expression, so nested
  ternaries bind right. In the AOT path a constant condition folds to the taken
  branch, and a runtime condition lowers to an LLVM `select i1 cond,
  i32 then, i32 else` — allocation-free and block-free. The comprehension
  iterable is parsed as an or-level expression so the comprehension's own `if`
  filter is not mistaken for a ternary. Adds interpreter tests
  (`TestEvalTernary`), IR checks (`TestIRTernary*`), and end-to-end exec tests
  (`TestExecTernaryAOT`). ADR 0026.


- `feat(aot)`: **list comprehensions in the LLVM AOT codegen** — list
  comprehensions over a constant iterable (inline list literal or `range(n)`)
  lower to a dedicated global struct, unrolled and constant-folded at compile
  time. A constant `if` condition filters elements at compile time, and the
  folded result can be indexed inline exactly like a list literal
  (`[x * 2 for x in [1, 2, 3]][1]` → `getelementptr` + `load` at the constant
  key). Like list/dict/set literals, comprehensions must be used inline (no
  assignment-to-variable indirection) in the codegen path; the interpreter
  evaluates comprehensions at runtime and is unchanged. Adds IR checks
  (`TestIRComprehension*`) and exec tests (`TestExecListComprehensionAOT`).
  ADR 0025.

- `feat(aot)`: **string-constant concatenation + `len` in the LLVM AOT
  codegen** — `+` on two string literals folds to a single concatenated
  string constant, and `len` of a string-constant expression (including a
  chain of `+`-concats) folds to its character count (`len("ab" + "cd")`
  → 4). The semantic analyzer types `str + str` as `str` (no arithmetic
  warning). Adds exec tests (`TestExecStringConstLen`), IR checks
  (`TestIRStringConstLenFolds`), and interpreter parity tests
  (`TestEvalStringConcatLen`). ADR 0024.

- `feat(aot)`: **inline dict/set literals with constant-key indexing + `len`
  in the LLVM AOT codegen** — `{1: 10, 2: 20}[1]`, `{1, 2, 3}[2]`, and
  `len({1: 10, 2: 20})` now lower to dedicated global structs (dicts:
  `{i32 count, [n x i32] keys, [n x i32] vals}`; sets: `{i32 count, [n x i32]
  elems}`). Constant-key lookup folds at compile time; literals must be used
  inline (no assignment-to-variable indirection), matching the list-literal
  limitation. Interpreter indexes dicts/sets at runtime and is unchanged.
  Adds exec tests (`TestExecDictSetLiterals`), IR checks
  (`TestIRDictSetGlobals`), and interpreter parity tests
  (`TestEvalDictSetIndexLen`). ADR 0023.

- `feat(aot)`: **boolean `and`/`or` operators + `//` floor division in the
  LLVM AOT codegen** — the parser/semantic/interpreter already supported
  `and`/`or` and `//`, but codegen rejected them as unsupported operators.
  `and`/`or` now lower to i1 logic zero-extended to `i32` (evaluate-both-then-
  combine, mirroring the interpreter) and `//` lowers to `sdiv`; literal
  operands are constant-folded. Adds exec tests (`TestExecAndOrBool`), IR
  checks (`TestIRAndOrCompilesWithLLC`), and interpreter parity tests
  (`TestEvalAndOrFloorDiv`). ADR 0022.

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
