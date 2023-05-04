package main

import (
	"fmt"
	"github.com/donutloop/gusty/pkg/lang"
	"io/ioutil"
	"log"
	"testing"
)

func TestLang(t *testing.T) {
	input := `function add(a, b) { let donut = 0 }`

	tokens := lang.Tokenize(input)
	nodes, err := lang.Parse(tokens)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	llvmIR, err := lang.GenerateLLVMIR(nodes)
	if err != nil {
		t.Fatal(err)
	}

	// Save the LLVM IR to a file
	err = ioutil.WriteFile("output.ll", []byte(llvmIR), 0644)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(llvmIR)
}
