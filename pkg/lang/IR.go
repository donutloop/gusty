package lang

import (
	"fmt"
	"tinygo.org/x/go-llvm"
)

type Caller struct {
	Value *llvm.Value
	Type  *llvm.Type
}

type GlobalScope struct {
	Callers map[string]Caller
}

func GenerateLLVMIR(nodes []Node) (string, error) {

	globalScope := GlobalScope{
		Callers: make(map[string]Caller),
	}

	// Initialize LLVM
	llvm.InitializeNativeTarget()
	llvm.InitializeNativeAsmPrinter()

	builder := llvm.NewBuilder()
	defer builder.Dispose()
	module := llvm.NewModule("example")

	mainType := llvm.FunctionType(llvm.VoidType(), []llvm.Type{}, false)
	mainFunc := llvm.AddFunction(module, "main", mainType)

	printfType := llvm.FunctionType(llvm.Int32Type(), []llvm.Type{llvm.PointerType(llvm.Int32Type(), 0)}, true)
	printf := llvm.AddFunction(module, "printf", printfType)

	// Create format string
	formatString := llvm.ConstString("%d\n", false)
	formatGlobal := llvm.AddGlobal(module, formatString.Type(), "format_string")
	formatGlobal.SetInitializer(formatString)
	formatGlobal.SetGlobalConstant(true)

	entry := llvm.AddBasicBlock(mainFunc, "entry")
	mainBuilder := llvm.NewBuilder()
	defer mainBuilder.Dispose()
	mainBuilder.SetInsertPointAtEnd(entry)

	for _, node := range nodes {
		switch n := node.(type) {
		case *CallerNode:
			caller, ok := globalScope.Callers[n.FunctionName]
			if !ok {
				return "", fmt.Errorf("caller not found in scope: %s", n.FunctionName)
			}
			if caller.Value == nil {
				return "", fmt.Errorf("nil function value for caller: %s", n.FunctionName)
			}
			if caller.Type == nil {
				return "", fmt.Errorf("nil function type for caller: %s", n.FunctionName)
			}
			callerType := *caller.Type
			callerValue := *caller.Value

			mainBuilder.CreateCall(callerType, callerValue, []llvm.Value{}, "")
		case *LetNode:
			// Skipping LLVM IR generation for while node for simplicity
		case *WhileNode:
			// Skipping LLVM IR generation for while node for simplicity
		case *FunctionNode:
			// Create function prototype
			functionType := llvm.FunctionType(llvm.VoidType(), []llvm.Type{}, false)
			function := llvm.AddFunction(module, n.Name, functionType)
			function.SetFunctionCallConv(llvm.CCallConv)

			// Create a new basic block and set the builder's insert point
			entry := llvm.AddBasicBlock(function, "entry")
			builder.SetInsertPointAtEnd(entry)

			// Generate LLVM IR for the function body
			for _, bodyNode := range n.Body {
				switch bodyNode := bodyNode.(type) {
				case *LetNode:
					letNodeValue := bodyNode.Value
					if intValue, ok := letNodeValue.(int); ok {
						letNodeAlloca := builder.CreateAlloca(llvm.Int32Type(), bodyNode.Identifier)
						letNodeAlloca.SetAlignment(4)
						letNodeConstInt := llvm.ConstInt(llvm.Int32Type(), uint64(intValue), true)
						builder.CreateStore(letNodeConstInt, letNodeAlloca)
						value := builder.CreateLoad(letNodeAlloca.Type(), letNodeAlloca, "value")
						format := builder.CreateInBoundsGEP(formatGlobal.Type(), formatGlobal, []llvm.Value{llvm.ConstInt(llvm.Int32Type(), 0, false), llvm.ConstInt(llvm.Int32Type(), 0, false)}, "format")
						builder.CreateCall(printfType, printf, []llvm.Value{format, value}, "")
					}
				}
			}

			globalScope.Callers[n.Name] = Caller{
				Value: &function,
				Type:  &functionType,
			}

			// Return void
			builder.CreateRetVoid()

		}
	}

	mainBuilder.CreateRetVoid()

	// Verify the module
	if err := llvm.VerifyModule(module, llvm.ReturnStatusAction); err != nil {

		return "", err
	}

	return module.String(), nil
}
