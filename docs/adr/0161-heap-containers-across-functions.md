# ADR 0161 — Heap containers across function boundaries (AOT), Gap I.1

## Status
Accepted

## Context
In the LLVM AOT backend a list, dict or set is not an aggregate value: it is an
`i32` **handle** into the runtime heap (`@heap`, `rt_alloc`, `rt_set_elem`,
`rt_dict_put`, `rt_set_add`, `rt_get_elem`, `rt_list_len`, …) — see ADR 0009.
The interpreter has always had Python's reference semantics there, so the two
backends had quietly diverged at the one place where a handle has to travel: a
function call.

Writing the L6.6 parity program made this visible (roadmap Gap I). Two distinct
bugs sat on either side of the call edge:

- **Call site.** A container *literal* was lowered to its compile-time form — a
  `private global {i32, [N x i32]}` — and passed as an `i32` argument:

  ```llvm
  %t4 = call i32 @total(i32 @.lst1)   ; llc-20: global variable reference must
                                     ;          have pointer type
  ```

  a hard verifier failure, so `total([1, 2, 3])` simply did not compile.

- **Callee.** Nothing recorded that the incoming `i32` was a handle. The body
  lowered `for x in xs` against the *value* of the parameter, producing a
  `0..handle` range loop — a silent miscompile, wrong answers with no error.

Both had to be fixed together: materialising a heap list at the call site
without teaching the callee to read it just moves the wrong answer.

The design question was *where the knowledge lives*. The AOT backend has no
type-checker pass of its own to ask, per-callsite specialization (à la the
Phase 8 monomorphization plan) is out of scope, and requiring users to annotate
every container parameter (`def total(xs: list[int])`) to get correct code from
an unannotated program is a bad deal: the interpreter accepts it, so the AOT
path would be needlessly capability-limited.

## Decision
1. **Handles are the only calling convention for containers.** A container
   argument is always an `i32` handle produced by the runtime heap: literals are
   materialised with `rt_alloc(kind)` + `rt_set_elem`/`rt_set_add`/`rt_dict_put`
   at the call site (kinds mirror the runtime: 1 list, 2 dict, 3 set). A
   variable already holds a handle and is passed unchanged. A materialised
   handle needs no extra rooting: no GC point exists between `rt_alloc` and the
   call, and the callee roots its own parameter slot.
2. **Parameter kinds are *inferred*, not requested.** `pkg/lang/heapargs.go`
   owns `heapArgKinds(prog)`, a pure whole-module AST analysis (it emits no IR,
   so it runs before codegen) that answers, per function parameter, "does a
   list/dict/set handle arrive here?". Witnesses, in priority order:
   1. the parameter's annotation — `list[T]`, `set[T]`, `dict[K, V]`,
      `Sequence[T]`, `Iterator[T]`;
   2. its default value (`def total(xs=[1, 2])`), which witnesses the kind even
      when every call omits the argument;
   3. any call site in the module — literal argument, a variable that holds a
      container, a comprehension, a generator call, or a keyword argument.
3. **Fixed point over the call graph.** Forwarding is the common case
   (`def doubled(xs): return total(xs)`), and a naive single pass would type
   `total`'s parameter as an integer because no call site hands it a literal.
   Parameter kinds therefore feed back into the variable-kind map and the call
   sites are re-classified until stable (bounded at 8 rounds; real chains are
   shallow). Propagation is guarded: a name that is ever assigned a plain
   non-container value is excluded, so an unrelated scalar `xs` cannot be
   dragged into container treatment by a same-named parameter.
4. **The callee reads the handle with the runtime helpers.** Registering an
   inferred parameter as a container makes `for x in xs`, `len(xs)`, `xs[i]`,
   `xs.append(v)` and `print(xs)` use `rt_list_len`/`rt_get_elem`/`rt_dict_get`
   exactly as they do for container *variables* — one representation, one set of
   accessors, no parameter-specific special case in the expression lowering.
   Rooting keys are per *(function, parameter)* (`gcRegKey`) rather than per
   name, so two functions with a parameter called `xs` each get a root.
5. **Capability gaps become messages, never bad IR.** The heap stores `i32`
   slots, so a string (`i8*` global) cannot be a container element yet. The call
   site now fails with
   `codegen: strings inside runtime containers are not supported by the AOT
   backend yet (the interpreter supports them)` instead of emitting
   `rt_set_elem(i32 %h, i32 1, i32 @.str1)` for `llc` to reject. Under
   `--json` the same failure is `{"ok": false, "phase": "compile", "error": …}`
   on stdout, so an agent can branch on it without reading stderr prose
   (ADR 0004, ADR 0006). Remaining work is tracked as Gap I.2.
6. **Parity is the acceptance test.** `integration/programs/heap_containers.gy`
   is in the conformance matrix (interpreter stdout must equal AOT stdout),
   `integration/heap_args_test.go` asserts exact outputs *and* that `llc`
   accepts each module, and `pkg/lang/heapargs_test.go` pins the inference table
   plus the two IR shapes that used to break (`@rt_alloc(i32 1)` at the call
   site, `@rt_list_len`/`@rt_get_elem` in the callee, and no `i32 @.lst` operand
   in a call).

## Alternatives rejected
- **Require container annotations for AOT.** Passes the tests but makes the AOT
  backend strictly less capable than the interpreter for code that is already
  legal, and violates the parity rule that both backends run the same program.
- **Pass containers by value (copy the static array).** Breaks aliasing
  semantics (`xs.append(v)` in the callee would not be visible to the caller),
  costs a copy per call, and still needs the runtime heap on the callee side.
- **Monomorphize per call-site container shape.** The right long-term answer for
  *types*, but a Phase 8 change; it does not address the unannotated case where
  the callee's own code decides how the handle is read.
- **Opaque `ptr`-typed container arguments.** This backend passes handles as
  `i32` indices into `@heap` everywhere (ADR 0009); special-casing the call
  edge would split the representation and the GC's view of roots.
- **Emit the string-container case and let `llc` fail.** Fails late, in a tool
  the user did not invoke, with a message that names neither the construct nor
  the workaround; exactly the "agent must hand-inspect IR" failure mode this
  project treats as a bug.
