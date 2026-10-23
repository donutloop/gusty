// codegen_builtin_print.go is the printing road: what `print` and `printf` emit, one
// rendered part at a time, through the sink the module declares (roadmap L11.2, ADR 0258).
package lang

import (
	"fmt"
	"strings"
)

// builtinPrint is the road `print` and `printf` take: the parts a value renders to, asked
// of the print sink in codegen_ir_sink.go rather than of a guess made here
func (g *irGen) builtinPrint(b *strings.Builder, c *Call, fnName string) (string, error) {
	// print(*args, sep=" ", end="\n") — Python's separator/terminator
	// semantics, identical on both backends (ADR 0165). An argument is
	// written WITHOUT its own terminator; `sep` is emitted between arguments
	// and `end` after the last one. So print("a =", n) is one line, print()
	// is a blank line, and print(x, end="") keeps the line open.
	sep, end := " ", "\n"
	var last string
	// emitText writes a literal string (the separator / terminator). `%` is
	// doubled so printf treats it as text, not as a directive.
	emitText := func(text string) {
		name, size := g.fmtStr(strings.ReplaceAll(text, "%", "%%"))
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0))\n", t, size, size, name))
		last = t
	}
	args := make([]Expr, 0, len(c.Args))
	for _, a := range c.Args {
		kw, isKw := a.(*KeywordArg)
		if !isKw {
			args = append(args, a)
			continue
		}
		sl, isStr := kw.Value.(*StrLit)
		if !isStr {
			return "", fmt.Errorf("codegen: print's %s must be a compile-time string constant (CPython accepts any expression)", kw.Name)
		}
		switch kw.Name {
		case "sep":
			sep = sl.Value
		case "end":
			end = sl.Value
		default:
			return "", fmt.Errorf("codegen: print got an unexpected keyword argument %q", kw.Name)
		}
	}
	if len(args) < 1 {
		// print() writes just the terminator: a blank line, like Python.
		emitText(end)
		return last, nil
	}
	// Gap L.5: the line is written after the arguments, not during them. Each argument's
	// lowering is recorded so the statement's writes can be threaded past the whole argument
	// list — a call that prints is an *evaluation*, and the caller's own bytes must not be on
	// the stream before it runs (roadmap Gap L.5, ADR 0355; `pkg/lang/print_atomic.go`).
	argMarks := make([]int, 0, len(args)+1)
	for i, a := range args {
		// The separator is written *after* the argument it follows, so it belongs to that
		// argument's chunk and not to the one coming next: the mark is taken once the
		// separator is on the stream, which is what keeps a chunk's writes behind its own
		// argument's evaluation and the line's writes in order.
		if i > 0 {
			emitText(sep)
		}
		argMarks = append(argMarks, b.Len())
		// `print(a or b)` prints an **operand**, and the operand it prints is the one the test chooses —
		// a fact about a value, not about a verdict, so the printer is asked of the operand rather than
		// of the operator (ADR 0261's rule, roadmap Gap R.147, ADR 0269). Two roads: when the source
		// wrote the test, the chosen operand is the whole expression and every arm below gets to render
		// it as it would render that operand alone; otherwise the answer is one operand or the other and
		// only the run time knows which, so the pair is selected and given to the one printer in the
		// module that reads a tag. A shape neither road can state is refused, never answered with the 1
		// that used to come out here (ADR 0166).
		if ln, isLogic := a.(*BinOp); isLogic && (ln.Op == "and" || ln.Op == "or") {
			if chosen, decided := constantLogicArm(ln); decided {
				a = chosen
			} else if g.logicAnswerIsFloat(ln) {
				// Both operands answer with a double, so the answer does too, and the chain's float arm
				// below renders it from the double door (`select i1 …, double …`) exactly as it renders
				// any other float expression. No box, no tag, no second formatter.
			} else {
				payload, tag, handled, lerr := g.logicPrintPair(b, ln)
				if lerr != nil {
					return "", lerr
				}
				if !handled {
					return "", logicWordErr(ln)
				}
				fmt.Fprintf(b, "  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", payload, tag)
				continue
			}
		}
		if nm, ok := a.(*Name); ok && g.unionVars[nm.Value] {
			g.emitUnionPrint(b, nm.Value, "")
			continue
		}
		// A subscript whose base is a call that hands back a container is printed by the roads below,
		// which read a container *variable* — and the answer is one: the callee materialised the object at
		// its `ret` and the answer reaches here under a rooted name carrying the notebooks those roads
		// consult (the dict's key/value kind, the list's element kind, the set's member kind, the mixed
		// reads and the tagged read beside them). Rewriting the base to that name **before any of them is
		// asked** is what makes `print(build()[i])` and `ys = build(); print(ys[i])` one program rather than
		// an answer and a wrong number: the kind roads have to run first, because a dict built from a
		// literal carries no per-slot tags and the tagged read below would hand back its @str_tab index and
		// call it a number (roadmap Gap R.214, ADR 0334; the return half is ADR 0333).
		if redo, okAns := g.containerAnswerSubscript(b, a); okAns {
			a = redo
		}
		// print a constant string: literals and folded string-method results. This is a
		// context that wants the BYTES, so it takes the global pointer from strConst
		// directly -- value() now hands back an @str_tab index, which is the language's
		// string value and not something printf's %s may read (Gap R.42, ADR 0224).
		if txt, ok := g.stringVal(a); ok {
			fmtName, size := g.fmtStr("%s")
			v := g.strConst(txt)
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i8* %s)\n", t, size, size, fmtName, v))
			last = t
			continue
		}
		// an f-string: print each constant part as a string and each
		// interpolated expression as its value (no runtime string type).
		// All parts are combined into a single printf call so the output
		// matches the interpreter's one-string Repr.
		if fs, ok := a.(*FString); ok {
			// Two fields the one printf below cannot state: the void, which has no digits, and a
			// container, whose text is its slots rather than its handle. Both used to answer a number
			// (`f"v={None}"` wrote `v=0`, `f"{xs}"` wrote `0` for `[1, 2]`) because the format string's
			// only words are an i32 and an i8*. Such an interpolation is built by the VALUE road — the
			// same door a binding, a concatenation and `str()` use — and handed to the module's string
			// printer, so the print of a program and the same program with the text named are one answer
			// (roadmap L11.1, ADR 0359).
			if g.fstringNeedsRenderer(fs) {
				if err := g.emitFStringPrint(b, fs); err != nil {
					return "", err
				}
				continue
			}
			var fmtLit string
			var operands []string
			for _, part := range fs.Parts {
				if part.Lit != "" {
					// escape % as %% so printf shows a literal %
					fmtLit += strings.ReplaceAll(part.Lit, "%", "%%")
					continue
				}
				// A name the pair road bound — from a slot read, a loop variable, an arithmetic
				// answer or a call — contributes the digits the module's ONE tag-reading printer
				// writes for its (payload, tag) pair, which is the same helper `str()` calls: the
				// field, `str(n)` and `print(n)` cannot disagree about `7` versus `7.0`, and a text
				// slot contributes its text rather than its interned index. A field that asked for
				// `!r` asks the same door with the quote flag on, which is what `repr()` does — so
				// `f"{n!r}"` writes `'a'` where it used to write nothing at all (roadmap L11.1,
				// Gap R.146's rendering positions; ADR 0303's one door, ADR 0187's pair). A field
				// that asked for a FORMAT SPEC still goes to the spec engine below: this door renders
				// a value, it does not pad one.
				if part.Spec == "" {
					if fp, ft, isPair, perr := g.filedPairOf(b, part.Expr); perr != nil {
						return "", perr
					} else if isPair {
						quote := "0"
						if part.Conv == ConvRepr {
							quote = "1"
						}
						fmtLit += "%s"
						sv := g.newTmp()
						b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_of_value(i32 %s, i32 %s, i32 %s)\n", sv, fp, ft, quote))
						sp := g.newTmp()
						b.WriteString(fmt.Sprintf("  %s = call i8* @rt_str_ptr(i32 %s)\n", sp, sv))
						operands = append(operands, "i8* "+sp)
						continue
					}
				}
				// A field that ASKED for a format is answered by the shared spec engine, at
				// compile time where the value is a constant and by refusing where it is not.
				// Falling through to the plain %d/%s below is the defect this row exists to end:
				// `f"{3.5:.2f}"` printed `3.5` here and on the interpreter at exit 0, and the two
				// engines agreeing is exactly why parity never saw it (Gap R.186, ADR 0299).
				// A TEXT field asked for `!r` is not folded into the format string: quoting is the
				// printer's job, and the door below asks it with the quote flag on. Embedding the quoted
				// literal in `@.fmtN` is what made `print(f"{'a'!r}")` spend the contract's forbidden exit 2
				// — `@.fmt1` came out `[0 x i8]` against a `[4 x i8]` use, which `llc` rejects (roadmap
				// Gap R.192; ADR 0166's rule, ADR 0303's one door).
				printerQuotes := part.Spec == "" && part.Conv == ConvRepr && g.fieldWantsPrinterQuotes(part.Expr)
				if !printerQuotes && (part.Spec != "" || (part.Conv != ConvNone && fieldIsConstantLiteral(part.Expr))) {
					lit, ok := g.formatFieldConst(part)
					if !ok {
						return "", fmt.Errorf("codegen: the format spec %q needs a value this backend "+
							"can read at compile time (AOT backend)", part.Spec)
					}
					fmtLit += strings.ReplaceAll(lit, "%", "%%")
					continue
				}
				if part.Expr != nil {
					// A name the pair road bound — from a slot read, a loop variable, an arithmetic
					// answer or a call — contributes the digits the module's ONE tag-reading printer
					// writes for its (payload, tag) pair, which is the same helper `str()` calls: the
					// field, `str(n)` and `print(n)` cannot disagree about `14` versus `14.0`, and a
					// text slot contributes its text rather than its interned index (roadmap L11.1,
					// Gap R.146's rendering positions; ADR 0303's one door, ADR 0187's pair).
					if exprIsContainerShape(part.Expr) {
						// A container interpolated into an f-string is a heap object, and this road
						// builds ONE printf format string: the only words it can hand printf are an
						// i32 or an i8*, neither of which renders `[1, 2]`. Emitting the handle as
						// `%d` is what produced `printf(..., i32 @.lst1)` -- a global address where a
						// heap handle belongs, which llc rejects as an invalid module and ADR 0166
						// forbids outright. Rendering it needs rt_print_list_mixed's sink, which
						// cannot compose into a single printf; so the field is REFUSED here, and the
						// interpreter -- which has no such limit -- answers [1, 2] like the
						// reference (Gap R.186 / Gap R.60's remainder, ADR 0299, owed to L11.1).
						return "", fmt.Errorf("codegen: an f-string field that is a container has no word to " +
							"travel in beside the text around it (AOT backend)")
					} else if g.isFloat(part.Expr) {
						// %s of rt_fmt_double's text, not %.17g: Python's str(0.1) is
						// "0.1" and str(2.0) is "2.0".
						fmtLit += "%s"
						fv := g.floatValue(b, part.Expr)
						if fv == "" {
							return "", g.floatOperandRefusal(part.Expr)
						}
						g.floatFmtUsed = true
						fs := g.newTmp()
						b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
						operands = append(operands, "i8* "+fs)
					} else if _, knownStr := g.stringVal(part.Expr); knownStr || g.printsAsInternedStr(part.Expr) {
						// An interpolated string is an @str_tab index now, so printf must be
						// handed the bytes through rt_str_ptr -- `%d` printed the index and
						// `f"hi {n}"` came out as `hi 0` (Gap R.42, ADR 0224).
						fmtLit += "%s"
						vv, err := g.value(b, part.Expr)
						if err != nil {
							return "", err
						}
						// `!r` of a text asks the module's one printer with the quote flag on — the same
						// door `repr()` uses — rather than the raw bytes `!s` wants. The conversion used to
						// be answered with an empty string here, at exit 0, for any field that was not a
						// literal (roadmap Gap R.186, Gap R.192; ADR 0303's one door).
						if part.Conv == ConvRepr {
							g.heapUsed = true
							qv := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_of_value(i32 %s, i32 %d, i32 1)\n", qv, vv, int(TagStr)))
							vv = qv
						}
						sp := g.newTmp()
						b.WriteString(fmt.Sprintf("  %s = call i8* @rt_str_ptr(i32 %s)\n", sp, vv))
						operands = append(operands, "i8* "+sp)
					} else if g.printsAsBool(part.Expr) {
						g.heapUsed = true // rt_bool_text lives in the heap runtime module
						// An interpolated bool contributes its word: `f"{flag}"` is "True", the
						// text print writes and str() returns, because all three ask one predicate
						// (roadmap L11.1 step 2, ADR 0257).
						fmtLit += "%s"
						pv, perr := g.value(b, part.Expr)
						if perr != nil {
							return "", perr
						}
						bp := g.newTmp()
						b.WriteString(fmt.Sprintf("  %s = call i8* @rt_bool_text(i32 %s)\n", bp, pv))
						operands = append(operands, "i8* "+bp)
					} else {
						fmtLit += "%d"
						vv, err := g.value(b, part.Expr)
						if err != nil {
							return "", err
						}
						operands = append(operands, "i32 "+vv)
					}
				}
			}
			fmtName, size := g.fmtStr(fmtLit)
			t := g.newTmp()
			callArgs := fmt.Sprintf("i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0)", size, size, fmtName)
			if len(operands) > 0 {
				callArgs += ", " + strings.Join(operands, ", ")
			}
			b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(%s)\n", t, callArgs))
			last = t
			continue
		}
		if nm, ok := a.(*Name); ok {
			if g.runtimeDicts[nm.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
				if g.dictKeyStr[nm.Value] || g.dictValStr[nm.Value] {
					// keys and/or values are @str_tab indices: the flagged printer renders
					// {'a': 1} / {1: 'a'} exactly as the interpreter's Repr does (Gap I.2).
					ks, vs := "0", "0"
					if g.dictKeyStr[nm.Value] {
						ks = "1"
					}
					if g.dictValStr[nm.Value] {
						vs = "1"
					}
					b.WriteString(fmt.Sprintf("  call void @rt_dict_print_s(i32 %%h%d, i32 0, i32 %s, i32 %s)\n", hs, ks, vs))
				} else {
					b.WriteString(fmt.Sprintf("  call void @rt_dict_print(i32 %%h%d, i32 0)\n", hs))
				}
				continue
			}
			if g.runtimeSets[nm.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
				printer := "rt_set_print"
				if g.setElemStr[nm.Value] {
					printer = "rt_set_print_str"
				}
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 0)\n", printer, hs))
				continue
			}
		}
		// None prints as "None" in both backends. The decision is static, like every
		// other print format here: values are untagged i32s, so codegen — not the
		// runtime — knows which expressions are the None singleton (ADR 0172).
		if g.isNoneExpr(a) {
			g.heapUsed = true // the None printer lives in the heap runtime module
			// Evaluate the argument first: `print(emit())` must run emit() (and any
			// output it produces) before writing None, exactly as the interpreter
			// interleaves them. Skipping the call is the classic way to lose output.
			if _, isLit := a.(*NoneLit); !isLit {
				if _, err := g.value(b, a); err != nil {
					return "", err
				}
			}
			b.WriteString("  call void @rt_print_none(i32 0)\n")
			continue
		}
		if g.printsAsInternedStr(a) {
			g.heapUsed = true
			v, err := g.value(b, a)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(b, "  call void @rt_print_str(i32 %s, i32 0)\n", v)
			continue
		}
		if sl, ok := a.(*Slice); ok {
			if nm2, ok2 := sl.Obj.(*Name); ok2 {
				if _, isList := g.listVars[nm2.Value]; isList {
					h, err := g.value(b, sl)
					if err != nil {
						return "", err
					}
					// The sliced object is a copy of the base's slots WITH their tags (`rt_slice`
					// pairs them, ADR 0187's rule as ADR 0294 applied it), so the printer has to be the
					// one that reads a tag. Asking only the base's element notebook printed a text slot as
					// its @str_tab index and a None as its zero — `print(xs[0:2])` answered `[1, 0]` for
					// `[1, "a"]` while `print(xs)` printed `[1, 'a']`, because the name arm below asks
					// `mixedLists` first and this arm did not (roadmap Gap R.230, ADR 0335).
					printer := "rt_print_list"
					if g.mixedLists[nm2.Value] {
						printer = "rt_print_list_mixed"
					} else if g.listElemStr[nm2.Value] {
						printer = "rt_print_list_str"
					}
					if g.isTupleExpr(sl.Obj) {
						// The copy is a fresh object and it carries the attribute word of the object it
						// was cut from, so the parentheses come from the value and not from the road that
						// copied it: `t[0:1]` prints (1,) because the object says tuple (ADR 0361).
						fmt.Fprintf(b, "  call void @rt_print_list_or_tuple(i32 %s)\n", h)
						g.tupleUsed = true
						continue
					}
					fmt.Fprintf(b, "  call void @%s(i32 %s, i32 0)\n", printer, h)
					continue
				}
			}
		}
		if nm, ok := a.(*Name); ok && g.taggedVars[nm.Value] {
			// print() of a name whose pair the OBJECT decides reads both allocas — and both are
			// unread unless some path wrote them. The written-flag test sat only on the ordinary
			// slot load, so a capture no arm bound printed its zero instead of trapping (Gap R.68).
			g.checkBound(b, nm.Value, nm.Span())
			vt := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", vt, nm.Value))
			tg := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s_tag\n", tg, nm.Value))
			b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", vt, tg))
			continue
		}
		// print(xs[0][0] + 1), print(-xs[0][0]): arithmetic on a slot whose kind lives in the object
		// rather than in the spelling. The ordinary numeric road asks for one i32 with no tag attached
		// and refuses (correctly — a payload without its tag is a number wearing another object's
		// bits); this is the other answer to the same question, asked of the object and printed from
		// the (payload, tag) pair the answer arrives with (roadmap L11.1, ADR 0265). It is taken only
		// where the ordinary road would have refused, so no program that was answered before is now
		// routed through here.
		// print(twice(xs[0][0])) — the answer of a function whose body did the arithmetic over a slot the
		// literal never described. The callee's tag word says what the objects said, and the one printer
		// that takes a value *and* its kind renders it, which is the same door ADR 0265 opened for the
		// expression itself and ADR 0269 for an operand a test chose (roadmap L11.1, ADR 0273).
		if g.instanceExpressionClass(a) != "" {
			// `print(C())` and `print(c)` after `c = C()`: the value is a heap handle and always was,
			// but only the object's slot-0 class id says what to call it. Without this arm the print road
			// treats it as an int and prints the slot (roadmap Gap R.209).
			h, herr := g.value(b, a)
			if herr != nil {
				return "", herr
			}
			g.heapUsed = true
			b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %d, i32 0)\n", h, int(TagInstance)))
			continue
		}
		if printed, perr := g.pairCallPrint(b, a); perr != nil {
			return "", perr
		} else if printed {
			continue
		}
		// print(min(n, 3)), print(sum([n, 1])): the answer of a fold whose winner the run time chose.
		// The fold returns one of the values it was handed, so the answer's kind is the winner's own
		// kind and the ordinary road — which compares lifted doubles and returns one word — cannot
		// carry it (roadmap L11.1, Gap R.146; ADR 0316).
		if printed, perr := g.pairFoldPrint(b, a); perr != nil {
			return "", perr
		} else if printed {
			continue
		}
		if g.arithWouldRefuse(a) {
			if val, tg, okPair, perr := g.taggedArithPair(b, a); perr != nil {
				return "", perr
			} else if okPair {
				g.heapUsed = true
				g.floatFmtUsed = true
				b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", val, tg))
				continue
			}
		}
		if ix, ok := a.(*Index); ok {
			// The subscript's five pair arms — a dict literal, a list literal, an element of a mixed
			// list, an entry of a mixed dict, a slot of a container that is itself a read — are one
			// door, in one order, asked by this road and by the f-string field and by nothing else, so
			// `print(x)` and `f"{x}"` of one read cannot answer differently. The arms, their order and
			// the runtime flags they set live in `subscript_pair.go` (roadmap Gap R.283, where the
			// field asked NONE of them and printed a payload; ADR 0187's pair, ADR 0232's one question).
			if val, tag, okPair, perr := g.subscriptPairOf(b, ix); perr != nil {
				return "", perr
			} else if okPair {
				b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", val, tag))
				continue
			}
		}
		// print([f(x) for x in xs]) — a comprehension is a container, so the runtime
		// printer renders it. The constant path used to printf the folded global
		// (@.lst1), which llc rejects outright: "global variable reference must have
		// pointer type" (roadmap L11.7, ADR 0192 — ADR 0188's rule one construct later).
		// The printer is the one for the container the comprehension means: a set comp is
		// rendered `{1, 2}` and a dict comp `{1: 2}`, not `[1, 2]`, which is what made
		// `print({x for x in [3, 1, 2]})` a question about the comprehension's kind and not
		// only about its elements (roadmap Gap J.2, ADR 0234).
		// print(t) of the immutable container: the brackets belong to the object, so the print road
		// hands the handle to the container dispatcher and lets the object answer — the same
		// arrangement that made `print(xs)` of a built container honest (ADR 0251), asked of a
		// spelling the list roads were never written for (roadmap L11.3, ADR 0361).
		if g.isTupleExpr(a) && !g.printsAsInternedStr(a) {
			// containerOperand, not g.value: a tuple of constants folds to a compile-time global
			// (@.lstN), and handing the address of that global to the printer is the exit-2 shape ADR 0188
			// removed for lists. The operand has to be the heap object the materialiser built, because its
			// own bits are what answer the bracket question (roadmap L11.3, ADR 0361).
			h, herr := g.containerOperand(b, a)
			if herr != nil {
				return "", herr
			}
			b.WriteString(fmt.Sprintf("  call void @rt_print_container_value(i32 %s, i32 0)\n", h))
			continue
		}
		// print([1] + [2]), print([1] * 3), print((1, 2) * 2) — a sequence the door builds is a fresh heap
		// object, and only the object can say how to render it: its bit 8 sends the list printer to the
		// per-slot tags the copy wrote (ADR 0187) and its attribute bit says whether the brackets are
		// `[ ]` or `( )` (ADR 0361). The print road asks the dispatcher rather than choosing a printer from
		// the expression, which is what keeps `print(xs + ys)` and `print(ys)` after `ys = xs + ys` one
		// program written two ways (roadmap Gap R.33, ADR 0365).
		if _, applies := g.seqBuildsContainer(a); applies {
			h, herr := g.containerOperand(b, a)
			if herr != nil {
				return "", herr
			}
			g.heapUsed = true
			g.tupleUsed = true
			b.WriteString(fmt.Sprintf("  call void @rt_print_container_value(i32 %s, i32 0)\n", h))
			continue
		}
		// A slice of a container lowers to `rt_slice`, whose answer is a heap HANDLE; printf's %d
		// printed that handle as `1` where the reference prints `[2, 3]`. Ask before the numeric
		// road does, the same way the comprehension and sorted() arms beside this do (Gap R.179).
		if sl, ok := a.(*Slice); ok && g.isContainerExpr(sl.Obj) && !g.printsAsInternedStr(a) {
			// Lower the slice itself — `containerOperand` answers "not a container" for a *Slice,
			// and the answer here is the handle `rt_slice` returns.
			h, herr := g.value(b, a)
			if herr != nil {
				return "", herr
			}
			if g.isTupleExpr(sl.Obj) {
				b.WriteString(fmt.Sprintf("  call void @rt_print_container_value(i32 %s, i32 0)\n", h))
				g.tupleUsed = true
				continue
			}
			b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
			continue
		}
		// A DICT VIEW (`keys()`, `values()`) lowers through the call road to a heap LIST of the
		// dict's keys or values — and print never learned, so printf's %d printed the handle: `0`
		// where the reference prints `dict_keys(['a'])` (roadmap Gap R.182, ADR 0291's arrangement).
		// The reference's wrapper word is a separate row; the wrong number goes first.
		if cv, word, isView := g.dictViewElems(a); isView {
			// containerOperand, not g.value: a view over an INT-valued dict folds to a literal that
			// g.value renders as the ADDRESS of a compile-time global, and feeding that to
			// rt_print_list_mixed is the exit-2 shape ADR 0188 removed for literals.
			h, verr := g.containerOperand(b, &ListLit{Elems: cv})
			if verr != nil {
				return "", verr
			}
			// The reference names the WRAPPER in the rendering — `dict_keys(['a'])`, not `['a']` —
			// so the compiled leg prints the word, the list, and the closing paren (Gap R.182).
			b.WriteString(fmt.Sprintf("  call void @rt_out_txt(i8* getelementptr inbounds ([%d x i8], [%d x i8]* @.%s, i32 0, i32 0))\n", len(word)+1, len(word)+1, "fmtdict"+word))
			b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
			b.WriteString("  call void @rt_out_txt(i8* getelementptr inbounds ([2 x i8], [2 x i8]* @.fmtdictclose, i32 0, i32 0))\n")
			continue
		}
		// print(build()) — a call whose answer is a container. The answer is a heap handle and its kind
		// is in the scan's table, so the operand is lowered to the name that holds it and printed by the
		// printer that name would be printed by: `ys = build()` / `print(ys)` has printed `[1, 2]` all
		// along, and `print(build())` printing `0` was the same object asked a second question and given
		// the first answer it had (roadmap Gap R.67, ADR 0333).
		if ansName, ansKind, okAns := g.containerAnswerOperandRead(b, a, containerReadWhole); okAns {
			h, herr := g.value(b, ansName)
			if herr != nil {
				return "", herr
			}
			g.heapUsed = true
			// Which door prints the answer is asked of the answer, and the tuple is the answer that has to be
			// asked of the OBJECT: a callee that handed back a tuple wrote the parentheses and the caller never
			// saw them, so no road-level fact can name the brackets — the attribute word beside the slots can.
			// A described list, set or dict keeps the printer its kind names (roadmap Gap R.67's arrangement,
			// read again for the immutable container at L11.3, ADR 0361).
			ansNameStr := ""
			if nm, isName := ansName.(*Name); isName {
				ansNameStr = nm.Value
			}
			if ansKind == tupleAnswer || g.tupleVars[ansNameStr] {
				b.WriteString(fmt.Sprintf("  call void @rt_print_container_value(i32 %s, i32 0)\n", h))
				g.tupleUsed = true
			} else {
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %s, i32 0)\n", g.answerPrinter(ansNameStr, ansKind), h))
			}
			continue
		}
		if comp, ok := a.(*Comp); ok {
			h, cerr := g.containerOperand(b, a)
			if cerr != nil {
				return "", cerr
			}
			printer := "rt_print_list_mixed"
			switch comp.Kind {
			case CompSet:
				printer = "rt_set_print"
			case CompDict:
				printer = "rt_dict_print"
			}
			b.WriteString(fmt.Sprintf("  call void @%s(i32 %s, i32 0)\n", printer, h))
			continue
		}
		// print([1, 2] if c else [9]) — a container the TEST chooses. The numeric road lowers the arm through
		// `value`, which renders a container literal as the ADDRESS of its compile-time global and hands that
		// to printf as an `i32`: `global variable reference must have pointer type`, at the exit-2 class
		// ADR 0166 reserves for a compiler bug. The container roads are asked here, before the number road
		// gets a vote, and the arm's builder runs in the block the branch enters (roadmap Gap R.385; the
		// arm-does-not-run rule is Gap R.340 / ADR 0374, the global-in-a-word rule is ADR 0188 / ADR 0192).
		if ce, ok := a.(*CondExpr); ok {
			if kind, isCont := g.staticContainerKind(ce); isCont {
				h, cerr := g.containerOperand(b, ce)
				if cerr != nil {
					return "", cerr
				}
				g.heapUsed = true
				printer := "rt_print_list"
				switch kind {
				case "set":
					printer = "rt_set_print"
				case "dict":
					printer = "rt_dict_print"
				}
				// The immutable spelling's brackets belong to the object, not to the road that built it: a
				// tuple arm wrote the parentheses into the value's attribute word and the caller never saw them,
				// so the printer is the one that asks (ADR 0361's rule, read at the ternary by roadmap Gap R.385).
				if g.isTupleExpr(ce.If) && g.isTupleExpr(ce.Else) {
					printer = "rt_print_container_value"
					g.tupleUsed = true
				}
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %s, i32 0)\n", printer, h))
				continue
			}
			// One arm is a container and the pair has no kind to name: the number road below would lower that
			// arm to a handle and print it as a number, or hand the assembler a global. Refuse the program by
			// name instead (ADR 0166: a refusal at exit 1 over a wrong answer at exit 0 or a rejected module).
			if g.ternaryHasContainerArm(ce) {
				return "", ternaryContainerArmErr(ce)
			}
		}
		// print(sorted(xs)) — that lowering hands back a runtime list handle, and only
		// the runtime printer can render it: the static path would printf the handle,
		// which is the invalid-IR shape ADR 0188 removed for literals (ADR 0191).
		if call, ok := a.(*Call); ok {
			// reversed("abc") is a string reversal and takes its own path; only a
			// container argument means this lowering produced a heap handle.
			if cname, ok := call.Fn.(*Name); ok && (cname.Value == "sorted" || cname.Value == "reversed") && g.isContainerExpr(call.Args[0]) {
				h, cerr := g.sortedRuntime(b, call)
				if cerr != nil {
					return "", cerr
				}
				// The two orderings answer a LIST whatever their argument was — `sorted((3, 1))` is `[1, 3]`, with
				// brackets, in the reference — so the printer is the one the compiler knows, not the one the
				// object would be asked: the copy `sortedRuntime` builds carries no attribute word of its own
				// (roadmap Gap R.67's arrangement, and ADR 0361's rule that a road asks what it can see).
				b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
				continue
			}
		}
		if nm, ok := a.(*Name); ok && g.listVars[nm.Value] {
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
			printer := "rt_print_list"
			if g.listElemStr[nm.Value] {
				printer = "rt_print_list_str"
			}
			if g.mixedLists[nm.Value] {
				printer = "rt_print_list_mixed"
			}
			b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 0)\n", printer, hs))
			continue
		}
		// A loop variable unrolled from an inline list literal carries the tag its element was
		// built with. Printing the i32 alone showed a float box's handle and None's zero where
		// Python shows 1.5 and None; the tag travels with the element, and rt_print_mixed_value
		// is the one place that knows how to read it. Only the tags nothing else can render
		// take this branch — an integer and an interned string already have their answers from the
		// paths above, and routing them here too dragged the string runtime into a module for a
		// loop over plain numbers. A container element is a handle, and printing it without its
		// tag is how `for row in [[1, 2], [3, 4]]` printed 0 and 1 (roadmap L11.1, ADR 0233). A
		// bool joins them for the same reason a float does: the unrolled loop forgot the verdict
		// when it bound the name, so only the element's tag can still say True (Gap R.112).
		if nm, ok := a.(*Name); ok && g.loopElemTag != nil && !g.listVars[nm.Value] && !g.taggedVars[nm.Value] {
			if tg, has := g.loopElemTag[nm.Value]; has && (tg == int32(TagFloat) || tg == int32(TagNone) ||
				tg == int32(TagBool) || tg == int32(TagList) || tg == int32(TagDict) || tg == int32(TagSet)) {
				vv := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", vv, nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %d, i32 0)\n", vv, tg))
				if tg == int32(TagFloat) {
					g.floatFmtUsed = true
				}
				continue
			}
		}
		// A bool prints as True/False. The value is the 0/1 the comparison produced — an
		// untagged word, because giving bools their own tagged word is the L11.1 destination and
		// not this rung — so the question 「is this expression a bool?」 is answered from the AST,
		// where it was always answerable. It takes this branch only after every container, tag
		// and text arm above has declined, so what it intercepts is exactly what used to reach
		// printf("%d") and answer 1 (roadmap L11.1 step 2, ADR 0257).
		if g.printsAsBool(a) {
			// The bool printer lives in the heap runtime module, as the None printer does, and
			// that module drags in the root table it reads kinds from — so a program that prints
			// a verdict and allocates nothing else has to say the heap runtime is in use, or llc
			// reports `use of undefined value '@gc.kinds'` on a two-line program (ADR 0209's
			// rule about gates, roadmap L11.1 step 2, ADR 0257).
			g.heapUsed = true
			v, err := g.value(b, a)
			if err != nil {
				return "", err
			}
			b.WriteString(fmt.Sprintf("  call void @rt_print_bool(i32 %s, i32 0)\n", v))
			continue
		}
		var t string
		if g.isFloat(a) {
			// Python renders a float as its shortest round-tripping text with a
			// ".0" when integral (rt_fmt_double); printf's %.17g invented digits
			// and %g truncated them, and neither marked 2.0 as a float.
			fmtName, size := g.fmtStr("%s")
			fv := g.floatValue(b, a)
			if fv == "" {
				// An empty operand here would reach printf as `rt_fmt_double(double )` and
				// llc would reject the module: refuse it at the front end instead (ADR 0166).
				return "", g.floatOperandRefusal(a)
			}
			g.floatFmtUsed = true
			fs := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
			t = g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i8* %s)\n", t, size, size, fmtName, fs))
		} else {
			// A container literal in print position is a *rendering* question, not a storage
			// question: build the runtime object and let the runtime printer render it. The old
			// gate was literalNeedsHeap — "does it contain a string?" — so an all-int literal
			// fell through to the static path and printed the elements-array global as a number:
			// `print([1, 2])` emitted `printf("%d\n", i32 @.lst1)`, which llc rejects outright
			// ("global variable reference must have pointer type"), and `print(set())` answered
			// `0`, the handle (roadmap Gap J.6, item (3)).
			if lit, ok := emptyContainerLiteral(a); ok {
				// `set()` / `list()` / `dict()` are containers the same way a literal is, and the
				// empty set has no literal spelling at all — so the constructor has to reach the
				// printer too, not just the literal (Gap K.3).
				a = lit
			}
			// print(xs[i]) with both parts literal folds to the element itself, and this branch
			// has to see that element: a folded container element that reaches the number path
			// emits printf("%d", i32 @.lst1), which llc refuses outright — the compiler rejecting
			// its own module for an ordinary program (roadmap L11.1, ADR 0166).
			if ix, isIx := a.(*Index); isIx {
				if base, isLit := ix.Obj.(*ListLit); isLit {
					if k, isConst := g.foldConstInt(ix.Idx); isConst && k >= 0 && k < int64(len(base.Elems)) {
						a = base.Elems[k]
					}
				}
			}
			if isContainerLiteral(a) {
				if _, isLL := a.(*ListLit); isLL && (literalMixedKinds(a) || g.literalNeedsTags(a) || g.literalHasPairElement(a.(*ListLit))) && g.taggableMixedList(a.(*ListLit)) {
					// Heterogeneous list of taggable elements: build it with per-element
					// tags and print through the tag-aware printer, which is what the
					// interpreter's Repr does element by element (ADR 0184).
					g.heapUsed = true
					h, err := g.heapListFromTagged(b, a.(*ListLit))
					if err != nil {
						return "", err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
					continue
				}
				// A literal that mixes kinds is printed through the same dispatching
				// printers its variable-bound twin uses, whenever every slot can say what
				// it holds (ADR 0232); only a slot that cannot is the refusal.
				if (literalMixedKinds(a) || g.literalNeedsTags(a)) && !g.literalMixedIsTaggable(a) {
					noun := "list"
					switch a.(type) {
					case *SetLit:
						noun = "set"
					case *DictLit:
						noun = "dict"
					}
					return "", mixedKindErr(noun)
				}
				g.heapUsed = true
				switch lit := a.(type) {
				case *ListLit:
					h, err := g.heapListFrom(b, lit, "")
					if err != nil {
						return "", err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_print_list(i32 %s, i32 0)\n", h))
				case *SetLit:
					h, err := g.heapSetFrom(b, lit, "")
					if err != nil {
						return "", err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_print(i32 %s, i32 0)\n", h))
				case *DictLit:
					h, err := g.heapDictFrom(b, lit, "")
					if err != nil {
						return "", err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_dict_print(i32 %s, i32 0)\n", h))
				}
				continue
			}
			v, err := g.value(b, a)
			if err != nil {
				return "", err
			}
			// a generator result is a runtime heap list handle: print it
			// as a list rather than an int.
			if g.listOperands[v] {
				b.WriteString(fmt.Sprintf("  call void @rt_print_list(i32 %s, i32 0)\n", v))
				continue
			}
			fmtName, size := g.fmtStr("%d")
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i32 %s)\n", t, size, size, fmtName, v))
		}
		if i == len(args)-1 {
			last = t
		}
	}
	argMarks = append(argMarks, b.Len())
	if rewritten, moved, _ := deferPrintLineWrites(b.String(), argMarks); moved {
		b.Reset()
		b.WriteString(rewritten)
	}
	emitText(end)
	return last, nil
}
