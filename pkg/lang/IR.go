package lang

import (
	"fmt"
	"github.com/google/uuid"
	"tinygo.org/x/go-llvm"
)

func init() {
	// Initialize LLVM
	// lvm.InitializeAllAsmPrinters() is a function that initializes all the available
	// assembly printers for various target architectures. Assembly printers are responsible for converting
	// LLVM's intermediate representation (IR) into a human-readable assembly language format specific to the
	// target architecture.
	//
	// When you use the LLVM library to compile or optimize your code, you typically need to initialize various
	// components of the LLVM library. The InitializeAllAsmPrinters() function is one of these components.
	// Other components include target information, target machine code, assembly parsers, and targets.
	llvm.InitializeAllTargetInfos()

	// llvm.InitializeAllTargets() is a function that initializes all the available
	// targets for various target architectures. Targets are responsible for generating the machine code,
	// assembly, and object files specific to a particular architecture or platform.
	//
	// When you use the LLVM library to compile or optimize your code, you typically need to initialize various
	// components of the LLVM library. The InitializeAllTargets() function is one of these components.
	// Other components include target information, target machine code, assembly printers, and assembly parsers.
	llvm.InitializeAllTargets()

	// llvm.InitializeAllTargetMCs() is a function that initializes all the available
	// target machine code (MC) components for various target architectures. The machine code components are
	// responsible for generating the actual machine code from the LLVM's intermediate representation (IR)
	// specific to the target architecture.
	//
	// When using the LLVM library to compile or optimize your code, you typically need to initialize various
	// components of the LLVM library. The InitializeAllTargetMCs() function is one of these components.
	// Other components include target information, assembly printers, assembly parsers, and targets.
	llvm.InitializeAllTargetMCs()

	// llvm.InitializeAllAsmParsers() is a function that initializes all the available
	// assembly parsers for various target architectures. Assembly parsers are responsible for parsing the
	// human-readable assembly language into LLVM's intermediate representation (IR) specific to the target
	// architecture.
	//
	// When you use the LLVM library to compile or optimize your code, you typically need to initialize various
	// components of the LLVM library. The InitializeAllAsmParsers() function is one of these components.
	// Other components include target information, target machine code, assembly printers, and targets.
	llvm.InitializeAllAsmParsers()

	// llvm.InitializeAllAsmPrinters() is a function that initializes all the available
	// assembly printers for various target architectures. Assembly printers are responsible for converting
	// LLVM's intermediate representation (IR) into a human-readable assembly language format specific to the
	// target architecture.
	//
	// When you use the LLVM library to compile or optimize your code, you typically need to initialize various
	// components of the LLVM library. The InitializeAllAsmPrinters() function is one of these components.
	// Other components include target information, target machine code, assembly parsers, and targets.
	llvm.InitializeAllAsmPrinters()
}

// Caller represents a function or method in the LLVM IR.
type Caller struct {
	Value *llvm.Value // The LLVM value representing the function or method.
	Type  *llvm.Type  // The LLVM type representing the function or method signature.
}

// Variable represents a local variable in the LLVM IR.
type Variable struct {
	Value *llvm.Value // The LLVM value representing the local variable.
}

// Argument represents a function or method argument in the LLVM IR.
type Argument struct {
	Value *llvm.Value // The LLVM value representing the function or method argument.
}

// Global represents a global variable in the LLVM IR.
type Global struct {
	Value *llvm.Value // The LLVM value representing the global variable.
}

// Scope represents the current scope for an LLVM function or method.
// It contains mappings of names to callers (functions or methods),
// local variables, and function or method arguments.
type Scope struct {
	Callers          map[string]Caller
	Variables        map[string]Variable
	Arguments        map[string]Argument
	PreviousVariable Variable
}

// GlobalScope represents the global scope for the LLVM module.
// It contains mappings of names to callers (functions or methods),
// global variables, and module-level globals.
type GlobalScope struct {
	Callers   map[string]Caller
	Variables map[string]Variable
	Globals   map[string]Global
}

// newScope creates a new empty scope.
func newScope() Scope {
	return Scope{
		Callers:   make(map[string]Caller),
		Variables: make(map[string]Variable),
		Arguments: make(map[string]Argument),
	}
}

// globalScope is a package-level variable holding the global scope for the LLVM module.
var globalScope GlobalScope

// init initializes the global scope.
func init() {
	globalScope = GlobalScope{
		Callers:   make(map[string]Caller),
		Variables: make(map[string]Variable),
		Globals:   make(map[string]Global),
	}
}

// printfIndentifier is a constant string representing the printf function identifier.
const (
	printfIndentifier = "printf"
)

func GenerateLLVMIR(nodes []Node) (string, error) {

	mainFunctionScope := newScope()

	module := llvm.NewModule("main")

	mainType := llvm.FunctionType(llvm.Int32Type(), []llvm.Type{}, false)
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

	for i := 0; i < len(nodes); i++ {
		node := nodes[i]
		switch n := node.(type) {
		case *AddOperationNode:
			err := generateAdd(&mainFunctionScope, mainBuilder, n)
			if err != nil {
				return "", err
			}
		case *CallerNode:
			err := generateCaller(&mainFunctionScope, mainBuilder, n)
			if err != nil {
				return "", err
			}
		case *LetNode:
			err := generateLet(&mainFunctionScope, mainBuilder, n)
			if err != nil {
				return "", err
			}
		case *WhileNode:
			// Skipping LLVM IR generation for while node for simplicity
			// todo to be implemented
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
					err := generateLet(&currentFunctionScope, currentFunctionBuilder, bodyNode)
					if err != nil {
						return "", err
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

	mainBuilder.CreateRet(llvm.ConstInt(llvm.Int32Type(), 0, false))

	// Verify the module
	if err := llvm.VerifyModule(module, llvm.ReturnStatusAction); err != nil {
		return "", err
	}

	return module.String(), nil
}

// generateCaller takes a scope, a functionBuilder builder, and a callerNode,
// and generates the LLVM IR for calling the function represented by the callerNode.
// It returns an error if any issues are encountered.
//
// scope:            A pointer to the current scope.
// functionBuilder:  The LLVM builder associated with the current function.
// callerNode:          The abstract syntax tree (AST) node representing the caller statement.
func generateCaller(scope *Scope, functionBuilder llvm.Builder, callerNode *CallerNode) error {
	if callerNode.isParameterOperation {
		err := generateAdd(scope, functionBuilder, callerNode.AddOperationNode)
		if err != nil {
			return err
		}
	}

	// Special case for handling printf calls
	if callerNode.FunctionName == printfIndentifier {
		// Load the value of the parameter and create a GEP for the format string
		format := functionBuilder.CreateInBoundsGEP(globalScope.Globals["format_string"].Value.Type(), *globalScope.Globals["format_string"].Value, []llvm.Value{llvm.ConstInt(llvm.Int32Type(), 0, false), llvm.ConstInt(llvm.Int32Type(), 0, false)}, "format")

		if callerNode.isParameterOperation {
			// Create the call instruction for printf with the format string and value as arguments
			value := functionBuilder.CreateLoad(scope.PreviousVariable.Value.Type(), *scope.PreviousVariable.Value, "")
			functionBuilder.CreateCall(*globalScope.Callers[printfIndentifier].Type, *globalScope.Callers[printfIndentifier].Value, []llvm.Value{format, value}, "")
			scope.PreviousVariable.Value = nil
		} else if variable, ok := scope.Variables[callerNode.Parameters[0].Identifier]; ok {
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

// generateLet is a function that generates LLVM IR code for a "let" statement.
// The let statement assigns a value to a new local variable in the current scope.
// This function handles the case where the value is an int32.
//
// scope:            A pointer to the current scope.
// functionBuilder:  The LLVM builder associated with the current function.
// letNode:          The abstract syntax tree (AST) node representing the let statement.
//
// Returns an error if the value type of the letNode is not supported.
func generateLet(scope *Scope, functionBuilder llvm.Builder, letNode *LetNode) error {
	// Check if the value is of type int32
	if intValue, ok := letNode.Value.(int32); ok {
		// Create an alloca instruction to allocate memory for the new local variable
		letNodeAlloca := functionBuilder.CreateAlloca(llvm.Int32Type(), letNode.Identifier)
		// Set the alignment of the allocated memory to 4 bytes
		letNodeAlloca.SetAlignment(4)
		// Create a constant int32 LLVM value from the intValue
		letNodeConstInt := llvm.ConstInt(llvm.Int32Type(), uint64(intValue), true)
		// Store the constant int32 value in the allocated memory
		functionBuilder.CreateStore(letNodeConstInt, letNodeAlloca)
		// Add the new local variable to the current scope
		scope.Variables[letNode.Identifier] = Variable{
			Value: &letNodeAlloca,
		}
	} else {
		// Return an error if the value type is not supported
		return fmt.Errorf("invalid value type for let node: %v", letNode)
	}
	return nil
}

// generateAdd is a function that generates LLVM IR code for an "add" statement.
// The add statement adds two number values in the current scope.
// This function handles the case where the value is an int32.
//
// scope:            A pointer to the current scope.
// functionBuilder:  The LLVM builder associated with the current function.
// AddOperationNode: The abstract syntax tree (AST) node representing the let statement.
//
// Returns an error if the value type of the AddOperationNode is not supported.
func generateAdd(scope *Scope, functionBuilder llvm.Builder, addOperationNode *AddOperationNode) error {
	// Check if the left value is of type int32
	intLeftValue, ok := addOperationNode.LeftValue.(int32)
	if !ok {
		// Return an error if the value type is not supported
		return fmt.Errorf("invalid value type for add operation node: %v", addOperationNode)
	}

	// Check if the right value is of type int32
	intRightValue, ok := addOperationNode.RightValue.(int32)
	if !ok {
		// Return an error if the value type is not supported
		return fmt.Errorf("invalid value type for operation node: %v", addOperationNode)
	}

	// Create a constant int32 LLVM value from the left int32 value
	leftValueConstInt := llvm.ConstInt(llvm.Int32Type(), uint64(intLeftValue), true)
	// Create a constant int32 LLVM value from the left int32 value
	rightValueConstInt := llvm.ConstInt(llvm.Int32Type(), uint64(intRightValue), true)
	// Create an add instruction to add left and right constant int32 values
	v := functionBuilder.CreateAdd(leftValueConstInt, rightValueConstInt, "")

	variableName := GenerateRandomIdentifier()
	resultAlloca := functionBuilder.CreateAlloca(llvm.Int32Type(), variableName)
	// Set the alignment of the allocated memory to 4 bytes
	resultAlloca.SetAlignment(4)
	// Store the constant int32 value in the allocated memory
	functionBuilder.CreateStore(v, resultAlloca)

	// Add the new local variable to the current scope
	scope.PreviousVariable = Variable{
		Value: &resultAlloca,
	}

	return nil
}

var GenerateRandomIdentifier = func() string {
	return uuid.New().String()
}
