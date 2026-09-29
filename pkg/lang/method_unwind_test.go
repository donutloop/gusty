package lang

import (
	"strings"
	"testing"
)

// Unit coverage for roadmap Gap R.41 (ADR 0223): a method is a call like any other. It needs its
// own unwind target, its caller has to look at the exception flag after calling it, and a refusal
// inside its body has to come out of the compiler instead of being dropped on the floor -- all
// three were missing, and each is asserted here on the emitted artifact rather than on prose.

const methodTrySrc = `class C:
    def m(self) -> int:
        try:
            return 3
        finally:
            print("m fin")

c = C()
print(c.m())
`

func TestMethodGetsItsOwnRaiseExit(t *testing.T) {
	res, err := Compile(methodTrySrc)
	if err != nil {
		t.Fatalf("a method containing a `try` must compile now (Gap R.41): %v", err)
	}
	if !strings.Contains(res.IR, "define i32 @gy_C_m(") {
		t.Fatalf("the method was not emitted as a function at all:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "gy_C_m.raiseexit:") {
		t.Fatalf("the method has no unwind target of its own; the old emission branched to an empty label:\n%s", res.IR)
	}
	// The bug in one line: an empty branch target, which llc rejects as "expected value token".
	if strings.Contains(res.IR, "br label %\n") {
		t.Fatalf("the module still contains a branch to an empty label:\n%s", res.IR)
	}
	if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the emitted module does not verify: %v", verr)
	}
}

func TestMethodRaiseDoesNotLandInMainsHandler(t *testing.T) {
	src := `class C:
    def m(self) -> int:
        raise ValueError("boom")

print(C().m())
`
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("a raise inside a method must compile: %v", err)
	}
	body := res.IR[strings.Index(res.IR, "define i32 @gy_C_m("):]
	if end := strings.Index(body[1:], "\n}"); end > 0 {
		body = body[:end+2]
	}
	if strings.Contains(body, "br label %main.raiseexit") {
		t.Fatalf("a raise in a method branched into main's raise-exit, unwinding a frame it does not own:\n%s", body)
	}
	if !strings.Contains(body, "br label %gy_C_m.raiseexit") {
		t.Fatalf("a raise in a method does not reach the method's own unwind target:\n%s", body)
	}
}

func TestMethodCallSiteChecksTheExceptionFlag(t *testing.T) {
	src := `class C:
    def bad(self) -> int:
        raise ValueError("boom")

    def m(self) -> int:
        return self.bad()

try:
    print(C().m())
except:
    print("caught")
`
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("method call with a raise must compile: %v", err)
	}
	if !strings.Contains(res.IR, "call i32 @gy_C_bad(") {
		t.Fatalf("the nested self-call was not emitted:\n%s", res.IR)
	}
	// Every call into program code is followed by a read of the flag; a method call was not,
	// so an exception raised by a method simply was not there when the caller looked.
	after := res.IR[strings.Index(res.IR, "call i32 @gy_C_bad("):]
	if !strings.Contains(after, "load i32, i32* @exn_flag") {
		t.Fatalf("no exception check follows a method call:\n%s", res.IR)
	}
	if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the emitted module does not verify: %v", verr)
	}
}

// TestRefusalInsideMethodBodyIsACompileError is the exit-code half: an unsupported construct in a
// method used to be discarded by the emitter, which then produced a half-written function and an
// `llc` rejection -- exit 2, "the compiler is broken", for what is a source the front end should
// name (ADR 0166's rule, reached through a method this time).
func TestRefusalInsideMethodBodyIsACompileError(t *testing.T) {
	src := `class C:
    def m(self) -> int:
        return [][0]

print(C().m())
`
	res, err := Compile(src)
	if err == nil {
		t.Fatalf("the compiler accepted a construct it cannot lower and emitted:\n%s", res.IR)
	}
	if !strings.Contains(err.Error(), "list index out of range") {
		t.Fatalf("refusal = %q, want the codegen refusal for the constant fold", err.Error())
	}
}

// TestMethodUnwindClosesItsFrame is the memory half of the same claim: the unwind path is a
// return like any other and has to pop the root frame the prologue pushed, or every raise out of
// a method leaks a frame (ADR 0181).
func TestMethodUnwindClosesItsFrame(t *testing.T) {
	res, err := Compile(methodTrySrc)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	body := res.IR[strings.Index(res.IR, "gy_C_m.raiseexit:"):]
	if end := strings.Index(body, "\n}"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "call void @rt_frame_close(") {
		t.Fatalf("the method's unwind path does not close its root frame:\n%s", body)
	}
}
