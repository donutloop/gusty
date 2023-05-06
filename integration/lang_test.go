package integration

import (
	"bytes"
	"github.com/donutloop/gusty/pkg/lang"
	"log"
	"os"
	"testing"
)

func TestFunctionWithLetAndCaller(t *testing.T) {
	input := `function add(a, b) { let donut = 43 printf(donut) } add(1,2)`

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
