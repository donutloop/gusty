# 0274. An int that meets `/=` becomes a float, and the change of state crosses as a pair

## Status

Accepted. Ships as one file (`pkg/lang/floatbind.go`, `bindFloatRebinding`) plus two emitting sites in
`pkg/lang/codegen.go`: the plain assignment whose value is a double, and the augmented assignment whose
operator is `/`. One `@rt_float_new` and one tagged store on the statement that changes the variable's
state; nothing anywhere else. No new runtime helper, no new instruction, no change to `ret`'s type, no new
representation.

Closes the `/=` half of roadmap **Gap P.1**: `x = 7` / `x /= 2` / `print(x)` was `3` on the compiled leg and
`3.5` on the interpreted one and in CPython, and `x = 1` / `x /= 3` printed `0` where the reference prints
`0.3333333333333333` — both at exit 0. Closes **Gap R.155**: `y = 12345` / `x = 8` / `x = 2.5` / `print(x, y)`
printed `1074003968` for `y`, which is a different number from the one the program wrote, at exit 0.

Files what the same sweep measured and this commit does not fix: **Gap R.156** (a float-state variable
returned from a function), **Gap R.157** (a variable whose state changes twice — the `t += i / 2`
accumulator), **Gap R.158** (a float-state variable handed to a function), **Gap R.159** (one as a container
element), **Gap R.160** (an ordering against a float literal). Gap P.1's *other* half — an untyped parameter
that receives a double keeps the int word — is untouched and still pinned as debt.

## Context

Two statements end with a double where the variable's own binding had chosen an `i32`:

```gusty
x = 7
x /= 2          # `/` is true division: the answer is a float whatever arrives
x = 2.5         # the rebinding the reference answers with a float too
```

CPython keeps a value and its kind together, so both leave `x` holding a float. This backend keeps a
variable's kind in the width of its stack slot, and nothing was said about the change of mind. The emitted
code was eight bytes into a four-byte allocation:

```llvm
  %_x = alloca i32          ; from `x = 7`
  store double %t1, double* %_x    ; from `x = 2.5`
```

LLVM's module verifier cannot see through an opaque pointer, so the module verified, `llc` accepted it, and
the variable's *neighbour* paid for the difference. The measured program was six tokens long:

| program | CPython | before · interpreter | before · compiled |
|---|---|---|---|
| `y = 12345` / `x = 8` / `x = 2.5` / `print(x, y)` | `2.5 12345` | ✅ | `2.5 1074003968` |
| `x = 7` / `x /= 2` / `print(x)` | `3.5` | ✅ | `3` |
| `x = 1` / `x /= 3` / `print(x)` | `0.3333333333333333` | ✅ | `0` |
| `x = 8` / `x /= 2` / `print(x)` | `4.0` | ✅ | `4` |
| `t = 0` / `t += 1.5` / `print(t)` | `1.5` | ✅ | exit 1, `writes a double into the i32 slot` |
| `f = 2.5` / `f /= 2` / `print(f)` | `1.25` | ✅ | ✅ (the float road already served a name born a float) |

The first attempt at the row fixed only the operator — `floatResult := n.Op == "/"` — and the measurement
that followed is the reason this ADR exists. `x = 8` / `x /= 2` printed `3`: the store had widened the
write, the integer road had truncated the answer, and neither the diagnostics nor the exit code objected.
A row about a wrong answer cannot be closed by a change that produces a different wrong answer.

## Decision

**The operator names the domain; the pair carries the state.**

1. `/=` chooses the double domain the way `/` already did (ADR 0253's rule), whatever the operands' kinds
   are: `floatResult` is true when `n.Op == "/"` and neither side is already a float. `//`, `%`, `*`, `+`
   and `-` keep the kinds they have always read, because those answer an `int` for two `int`s, and a target
   that was already a float keeps the road it had — the rows for `f = 2.5` / `f /= 2` and `t = 0.0` /
   `t += 1.5` are pinned unchanged so this row cannot take them over.
2. When the double that arrives belongs to a variable whose slot is an `i32`, `bindFloatRebinding` takes the
   statement: the double goes through `@rt_float_new`, the name is bound to the `(payload, tag)` pair with
   the float's tag through the `bindTaggedVar` ADR 0166 introduced, and the origin is recorded as
   `taggedOriginFloat` — the third origin in the family that has to say where its tag came from
   (ADR 0267's arithmetic origin, ADR 0273's call-crossing one).
3. `numericPairVar` vouches for a float-rebound name exactly as it does for an arithmetic one, so print,
   truthiness, equality, a number position, the chosen operand of `and`/`or`, negation and `abs` need no new
   case: they already ask a pair whose payload is a float box.
4. The rebinding **roots the variable's slot** (`rt_root_put`). The slot used to hold an immediate and now
   holds a heap handle; unrooted, the collector cannot see the box, recycles its handle, and the next
   `@rt_float_new` writes a different double over the value the variable still names. This was measured, not
   predicted: with the root missing, `h = 1` / `h /= 3` / `print(h + 1)` / `print(h * 2)` answered
   `1.3333333333333333` then `2.6666666666666665` — the second print reading the first one's answer through
   the recycled box — and the exit code was 0 (ADR 0181's rule that every slot holding a handle says so).
5. The gate is negative rather than positive. The rebinding is asked only of a name that is not already a
   float (`floatVars`), not already a pair, not a parameter (the signature allocated that slot and the
   `define` decides its shape), not a container, verdict, `None`, text or instance name, and whose slot was
   allocated before the statement (`hadSlot`) so the float that *opens* a variable may still choose a double
   slot. Anything the gate declines walks the road it walked before this commit, refusal included: the
   `writes a double into the i32 slot` message is still reachable, and Gap R.88 stays open.
6. A body that contains a `/=` is a body that returns a double. `scanRebinds` — which reads a function's
   return word off what its body does (ADR 0254) — now records a `/=` target's newest value as the quotient
   rather than the written `2`, because what the operator answers is a double whatever the operands were.
   That is the difference between `def f(x): x = x / 2` answering and `def f(x): x /= 2` refusing, and it is
   the operator stating the fact rather than the literal: `x /= 2` in a body returns `3.5` for `f(7)`.

## Agentic rationale

Nothing about the CLI moved: same flags, same exit classes, same JSON payload members. What an agent gets new
is honesty where it previously got arithmetic. Three refusals name the position that keeps one word rather
than answering a number the program never wrote (`integration/float_state_test.go` pins each with its exit
class, and forbids exit 2 on all of them), and the promoted
`programs/probe_int_state_becomes_float.gy` — twelve lines, three engines, the same bytes, recorded
`oracle: match` in `integration/conformance-matrix.json` — is the machine-readable statement of what the row
now means. The probe is the part an agent can re-run blind; the refusals are the part it must not mistake for
bugs, which is why each one's message names `floatbind.go`'s own reason rather than a generic AOT limit.

## Codegen / IR implications

```llvm
  %_x = alloca i32                          ; `x = 7` — the slot it always had
  store i32 7, i32* %_x
  %t1 = sitofp i32 %_x.ld to double         ; `x /= 2` — the double domain the operator chose
  %t2 = fdiv double %t1, %t2d
  %t3 = call i32 @rt_float_new(double %t2)  ; the box
  store i32 %t3, i32* %_x                   ; the payload …
  store i32 1, i32* %_x_tag                 ; … and the tag
  call void @rt_root_put(i32* %_x)          ; the slot holds a handle now
```

* `@rt_float_new` is ADR 0172's float box, `@rt_num_arith` ADR 0265's arithmetic helper, `bindTaggedVar`
  ADR 0166's binding: every piece already had a name.
* A variable that never changes state emits none of this — `fibonacci` and `function_calls` contain no
  `.anst`, no `%q0` and no `rt_float_new` before or after, which the benchmark rows keep honest.
* `bindTaggedVar` deletes the name's `doubleSlot` record when it takes a name over, and the two emitting
  sites note the allocation when they store a plain double: `doubleSlot` is the one fact that says whether a
  later `store double` fits, and `floatVars` (what the newest binding *is*) cannot say it once a rebinding
  has deleted the float status.
* The answer of the arithmetic is a pair whose kind only `@rt_num_arith` knows, so the tag stored beside the
  variable is the answer's tag, not a compile-time guess — the same discipline ADR 0273 states for the
  answer crossing a call.

## Alternatives rejected

* **Widen the variable's slot to a `double` in place.** The alloca is already emitted by the first binding,
  and the module would need the whole frame re-planned to know a variable changed kind mid-block; the
  corruption is exactly what happens when the width is decided before the change of mind is visible.
* **Emit `store double` into the `i32` slot and let the verifier catch it.** It does not. Opaque pointers
  make a type-mismatched store invisible at the module level, and the six-line program above is the proof:
  verified, linked, exit 0, wrong number in the neighbour.
* **Box at every read instead of at the binding.** That is the shape ADR 0166 refuses — it makes the pair
  depend on how often and in what order the variable is read, and the same variable answers a different way
  through `print` than through `+`. Binding the pair once is what makes the read roads uniform.
* **Refuse the rebinding, as the `+=` road already did.** The refusal was honest and it was also the bug:
  the reference has one answer here, and a program that states it gets it on the other engine. Refusing the
  *return*, the accumulator and the call argument (Gaps R.156–R.158) is defensible precisely because the
  statement that changes the state now answers correctly.
* **Let the float-rebound name keep `floatVars` set.** Measured: `t = 0.0` / `t += 1.5` then answered `3.0`,
  because the second binding boxed a value that was already a box handle. `floatVars` answers "what is the
  newest binding"; for a boxed name that question belongs to the tag.
