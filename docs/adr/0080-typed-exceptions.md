# ADR 0080: Typed exceptions with messages (raise/except)

## Decision

Make `raise` and `except` Python-semantics: `raise ValueError("msg")` raises a
typed exception carrying a class name and an optional message; `except
ValueError:` catches exactly that raised class; `except Exception:` (or a bare
`except:`) catches any exception.

## Details

- **Exception constructors**: built-in exception classes (`Exception`,
  `ValueError`, `TypeError`, `KeyError`, `IndexError`, `RuntimeError`,
  `StopIteration`, `ZeroDivisionError`) are callable in the interpreter
  (`evalCall`), producing an exception object (`kind="exn"`, `class`=type name,
  `sval`=message from the first string argument).
- **`raise Expr`**: evaluates the expression. An exception object carries its
  class+message; a string becomes `Exception` with that message; a bare `raise`
  raises `Exception`. The result is an `*EvalError{ExnType, ExnMsg}`.
- **`except Name:`**: matches the raised `ExnType` exactly. `except Exception:`
  and bare `except:` match any. An unmatched exception propagates to the caller
  carrying type + message.
- Example: `try: raise ValueError("bad") except ValueError: ...` catches; an
  unmatched `except TypeError:` lets the `ValueError` propagate.

## Scope

- Interpreter only: the LLVM AOT codegen has no `try`/`except`/`raise`
  handling at all (documented limitation). A future ADR will cover codegen
  exception unwinding.

## Alternatives Rejected

- **Exception subtyping** (e.g. `except Exception` matching subclasses): rejected
  for simplicity — matching is exact by class name. `Exception` is treated as the
  catch-all.
