package lang

import (
	"fmt"
	"tinygo.org/x/go-llvm"
)

type Caller struct {
	Value *llvm.Value
	Type  *llvm.Type
}

type Variable struct {
	Value *llvm.Value
}

type Argument struct {
	Value *llvm.Value
}

type Global struct {
	Value *llvm.Value
}

type Scope struct {
	Callers   map[string]Caller
	Variables map[string]Variable
	Arguments map[string]Argument
}

type GlobalScope struct {
	Callers   map[string]Caller
	Variables map[string]Variable
	Globals   map[string]Global
}

func newScope() Scope {
	return Scope{
		Callers:   make(map[string]Caller),
		Variables: make(map[string]Variable),
		Arguments: make(map[string]Argument),
	}
}

var globalScope GlobalScope

func init() {
	globalScope = GlobalScope{
		Callers:   make(map[string]Caller),
		Variables: make(map[string]Variable),
		Globals:   make(map[string]Global),
	}
}

const (
	printfIndentifier = "printf"
)

func GenerateLLVMIR(nodes []Node) (string, error) {

	mainFunctionScope := newScope()

	// Initialize LLVM
	llvm.InitializeNativeTarget()
	llvm.InitializeNativeAsmPrinter()

	module := llvm.NewModule("example")

	mainType := llvm.FunctionType(llvm.VoidType(), []llvm.Type{}, false)
	mainFunc := llvm.AddFunction(module, "main", mainType)

	printfType := llvm.FunctionType(llvm.Int32Type(), []llvm.Type{llvm.PointerType(llvm.Int32Type(), 0)}, true)
	printf := llvm.AddFunction(module, printfIndentifier, printfType)
	globalScope.Callers[printfIndentifier] = Caller{
		Value: &printf,
		Type:  &printfType,
	}

	// Create format string
	formatString := llvm.ConstString("%d\n", false)
	formatGlobal := llvm.AddGlobal(module, formatString.Type(), "format_string")
	formatGlobal.SetInitializer(formatString)
	formatGlobal.SetGlobalConstant(true)
	globalScope.Globals["format_string"] = Global{
		Value: &formatGlobal,
	}

	entry := llvm.AddBasicBlock(mainFunc, "entry")
	mainBuilder := llvm.NewBuilder()
	defer mainBuilder.Dispose()
	mainBuilder.SetInsertPointAtEnd(entry)

	for _, node := range nodes {
		switch n := node.(type) {
		case *CallerNode:
			err := generateCaller(&mainFunctionScope, mainBuilder, n)
			if err != nil {
				return "", err
			}
		case *LetNode:
			// Skipping LLVM IR generation for while node for simplicity
		case *WhileNode:
			// Skipping LLVM IR generation for while node for simplicity
		case *FunctionNode:
			// Create function prototype

			var llvmParameters []llvm.Type
			for _, parameter := range n.Parameters {
				if parameter.Type == Integer32Type {
					llvmParameters = append(llvmParameters, llvm.Int32Type())
				}
			}

			functionType := llvm.FunctionType(llvm.VoidType(), llvmParameters, false)
			function := llvm.AddFunction(module, n.Name, functionType)
			function.SetFunctionCallConv(llvm.CCallConv)

			currentFunctionScope := newScope()

			var i int
			for _, parameter := range n.Parameters {
				llvmParameter := function.Param(i)
				currentFunctionScope.Arguments[parameter.Identifier] = Argument{
					Value: &llvmParameter,
				}
				i++
			}

			currentFunctionBuilder := llvm.NewBuilder()
			defer currentFunctionBuilder.Dispose()

			// Create a new basic block and set the builder's insert point
			entry := llvm.AddBasicBlock(function, "entry")
			currentFunctionBuilder.SetInsertPointAtEnd(entry)

			// Generate LLVM IR for the function body
			for _, bodyNode := range n.Body {
				switch bodyNode := bodyNode.(type) {
				case *LetNode:
					letNodeValue := bodyNode.Value
					if intValue, ok := letNodeValue.(int32); ok {
						letNodeAlloca := currentFunctionBuilder.CreateAlloca(llvm.Int32Type(), bodyNode.Identifier)
						letNodeAlloca.SetAlignment(4)
						letNodeConstInt := llvm.ConstInt(llvm.Int32Type(), uint64(intValue), true)
						currentFunctionBuilder.CreateStore(letNodeConstInt, letNodeAlloca)
						currentFunctionScope.Variables[bodyNode.Identifier] = Variable{
							Value: &letNodeAlloca,
						}
					}
				case *CallerNode:
					err := generateCaller(&currentFunctionScope, currentFunctionBuilder, bodyNode)
					if err != nil {
						return "", err
					}
				}
			}

			mainFunctionScope.Callers[n.Name] = Caller{
				Value: &function,
				Type:  &functionType,
			}

			// Return void
			currentFunctionBuilder.CreateRetVoid()
		}
	}

	mainBuilder.CreateRetVoid()

	// Verify the module
	if err := llvm.VerifyModule(module, llvm.ReturnStatusAction); err != nil {
		return "", err
	}

	return module.String(), nil
}

// generateCaller takes a scope, a functionBuilder builder, and a callerNode,
// and generates the LLVM IR for calling the function represented by the callerNode.
// It returns an error if any issues are encountered.
func generateCaller(scope *Scope, functionBuilder llvm.Builder, callerNode *CallerNode) error {

	// Special case for handling printf calls
	if callerNode.FunctionName == printfIndentifier {
		// Load the value of the parameter and create a GEP for the format string
		format := functionBuilder.CreateInBoundsGEP(globalScope.Globals["format_string"].Value.Type(), *globalScope.Globals["format_string"].Value, []llvm.Value{llvm.ConstInt(llvm.Int32Type(), 0, false), llvm.ConstInt(llvm.Int32Type(), 0, false)}, "format")

		if variable, ok := scope.Variables[callerNode.Parameters[0].Identifier]; ok {
			value := functionBuilder.CreateLoad(variable.Value.Type(), *variable.Value, callerNode.Parameters[0].Identifier+"Value")
			// Create the call instruction for printf with the format string and value as arguments
			functionBuilder.CreateCall(*globalScope.Callers[printfIndentifier].Type, *globalScope.Callers[printfIndentifier].Value, []llvm.Value{format, value}, "")
		} else if argument, ok := scope.Arguments[callerNode.Parameters[0].Identifier]; ok {
			// Create the call instruction for printf with the format string and value as arguments
			functionBuilder.CreateCall(*globalScope.Callers[printfIndentifier].Type, *globalScope.Callers[printfIndentifier].Value, []llvm.Value{format, *argument.Value}, "")
		}

		return nil
	}

	// Retrieve the caller from the global scope using the function name
	caller, ok := scope.Callers[callerNode.FunctionName]
	// If the caller is not found, return an error
	if !ok {
		return fmt.Errorf("caller not found in scope: %s", callerNode.FunctionName)
	}

	// If the caller's Value is nil, return an error
	if caller.Value == nil {
		return fmt.Errorf("nil function value for caller: %s", callerNode.FunctionName)
	}

	// If the caller's Type is nil, return an error
	if caller.Type == nil {
		return fmt.Errorf("nil function type for caller: %s", callerNode.FunctionName)
	}

	// Dereference the caller's Type and Value pointers
	callerType := *caller.Type
	callerValue := *caller.Value

	var llvmParameterValues []llvm.Value
	for _, parameter := range callerNode.Parameters {
		if parameter.Type == Integer32Type {
			llvmParameterValues = append(llvmParameterValues, llvm.ConstInt(llvm.Int32Type(), uint64(parameter.Value.(int32)), true))
		}
	}

	// Create the LLVM IR call instruction with the function scope builder,
	// using the caller's Type, Value, and an empty slice of llvm.Value as arguments.
	functionBuilder.CreateCall(callerType, callerValue, llvmParameterValues, "")

	// If no issues were encountered, return nil
	return nil
}
