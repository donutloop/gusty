package integration

import (
	"bytes"
	"fmt"
	"github.com/donutloop/gusty/pkg/lang"
	"log"
	"os"
	"testing"
)

func TestFunctionWithLetAndCaller(t *testing.T) {
	input := `function add(a, b) { let donut = 43 } add(1,2)`

	tokens := lang.Tokenize(input)
	nodes, err := lang.Parse(tokens)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	actualLvmIR, err := lang.GenerateLLVMIR(nodes)
	if err != nil {
		t.Fatal(err)

	}

	// Save the LLVM IR to a file
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
