# Common Expression Language - Go Toolchain

[![Build Status](https://travis-ci.org/google/cel-go.svg?branch=master)](https://travis-ci.org/google/cel-go) [![Go Report Card](https://goreportcard.com/badge/github.com/google/cel-go)](https://goreportcard.com/report/github.com/google/cel-go)

This repo contains the Go toolchain for the Common Expression Language (CEL).
CEL is a non-Turing complete language designed to be portable and fast. It is
best suited to  applications where sandboxing a full-fledged language like
JavaScript or Lua would be too resource intensive, but side-effect free dynamic
computations are strongly desired.

## Want to learn more?

* See the [CEL Spec][1] for more details about the language.
* Ask for support on the [CEL Go Discuss][2] Google group.

## Want to integrate CEL?

* See [GoDoc][6] to learn how to integrate CEL into services written in Go.
  [![GoDoc](https://godoc.org/github.com/google/cel-go?status.svg)][6]
* See the [CEL C++][3] toolchain (under development) for information about how
  to integrate CEL evaluation into other environments.

## Want to contribute?

* See [CONTRIBUTING.md](./CONTRIBUTING.md) to get started.
* Use [GitHub Issues][4] to request features or report bugs.

## How does CEL work?

Write an expression in the CEL syntax, then parse, check, and interpret.

| Step      | Description                                                    |
|-----------|----------------------------------------------------------------|
| Parse     | Parses a string expression to a protocol buffer representation.|
| Check     | Type-checks a parsed expression against a given environment.   |
| Interpret | Evaluates parsed expressions against a set of inputs.          |      |

Type-checking an expression in an optional, but strongly encouraged step that
can be used to reject some expressions as semantically invalid using static
analysis. Additionally, the type-check produces some additional metadata
related to function overload resolution and object field selection which may
be used by the interpret step to speed up execution.

### Example

The following example shows the parse, check, and intepretation of a simple
program. After the parse and type-check steps, the service checks whether there
are any errors that need to be reported.

```go
import(
    "fmt"

    "github.com/google/cel-go/checker"
    "github.com/google/cel-go/checker/decls"
    "github.com/google/cel-go/common/packages"
    "github.com/google/cel-go/common/types"
    "github.com/google/cel-go/interpreter"
    "github.com/google/cel-go/parser"
)

// Parse the expression and returns the accumulated errors.
src := common.NewTextSource("a || b && c.exists(x, x > 2)")
expr, errors := parser.Parse(src)
if len(errors.GetErrors()) != 0 {
    return nil, fmt.Error(errors.ToDisplayString())
}

// Check the expression matches expectations given the declarations for
// the identifiers a, b, c where the identifiers are scoped to the default
// package (empty string):
typeProvider := types.NewProvider()
env := checker.NewStandardEnv(packages.DefaultPackage, typeProvider)
env.Add(decls.NewIdent("a", decls.Bool, nil),
    decls.NewIdent("b", decls.Bool, nil),
    decls.NewIdent("c", decls.NewListType(decls.Int), nil))
c, errors := checker.Check(expr, src, env)
if len(errors.GetErrors()) != 0 {
    return nil, fmt.Error(errors.ToDisplayString())
}

// Interpret the checked expression using the standard overloads.
i := interpreter.NewStandardInterpreter(packages.DefaultPackage, typeProvider)
eval := i.NewInterpretable(interpreter.NewCheckedProgram(c))
result, state := eval.Eval(
    interpreter.NewActivation(
        map[string]interface{}{
            "a": false,
            "b": true,
            "c": []int{1, 2, 3, 4, 5}}))

fmt.Println(state)
fmt.Println(result)
```

More examples like these can be found within the unit tests which can be run
using [Bazel][5]:

```
bazel test ...
```

## License

Released under the [Apache License](LICENSE).

Disclaimer: This is not an official Google product.

[1]:  https://github.com/google/cel-spec
[2]:  https://groups.google.com/forum/#!forum/cel-go-discuss
[3]:  https://github.com/google/cel-cpp
[4]:  https://github.com/google/cel-go/issues
[5]:  https://bazel.build
[6]:  https://godoc.org/github.com/google/cel-go
