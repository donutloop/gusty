package integration

import (
	"bytes"
	"github.com/donutloop/gusty/pkg/lang"
	"log"
	"os"
	"testing"
)

func TestFunctionWithLetAndCaller(t *testing.T) {
	input := `function add(a i32, b i32) { let donut = 43 printf(donut) printf(a) } add(1,2)`

	tokens := lang.Tokenize(input)
	nodes, err := lang.Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}

	actualLvmIR, err := lang.GenerateLLVMIR(nodes)
	if err != nil {
		t.Fatal(err)
	}

	expectedLlvmIR, err := os.ReadFile("./expected/program_1.ll")
	if err != nil {
		log.Fatal(err)
	}

	v := bytes.Compare(expectedLlvmIR, []byte(actualLvmIR))
	if v != 0 {
		t.Error("program code is not bad")
	}

	t.Log("Actual LLVM IR: ")
	t.Log(actualLvmIR)

	t.Log("Expected LLVM IR: ")
	t.Log(string(expectedLlvmIR))
}

func TestLet(t *testing.T) {
	input := `let donutloop = 42`

	tokens := lang.Tokenize(input)
	nodes, err := lang.Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}

	actualLvmIR, err := lang.GenerateLLVMIR(nodes)
	if err != nil {
		t.Fatal(err)
	}

	expectedLlvmIR, err := os.ReadFile("./expected/let.ll")
	if err != nil {
		log.Fatal(err)
	}

	v := bytes.Compare(expectedLlvmIR, []byte(actualLvmIR))
	if v != 0 {
		t.Error("program code is not bad")
	}

	t.Log("Actual LLVM IR: ")
	t.Log(actualLvmIR)

	t.Log("Expected LLVM IR: ")
	t.Log(string(expectedLlvmIR))
}

func TestAddTwoConst(t *testing.T) {
	lang.GenerateRandomIdentifier = func() string {
		return "9b3c24fa-f1d5-4d41-9fd1-0637244ce4f3"
	}

	input := `printf(42 + 42)`

	tokens := lang.Tokenize(input)
	nodes, err := lang.Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}

	actualLvmIR, err := lang.GenerateLLVMIR(nodes)
	if err != nil {
		t.Fatal(err)
	}

	expectedLlvmIR, err := os.ReadFile("./expected/add.ll")
	if err != nil {
		log.Fatal(err)
	}

	v := bytes.Compare(expectedLlvmIR, []byte(actualLvmIR))
	if v != 0 {
		t.Error("program code is not bad")
	}

	t.Log("Actual LLVM IR: ")
	t.Log(actualLvmIR)

	t.Log("Expected LLVM IR: ")
	t.Log(string(expectedLlvmIR))
}
