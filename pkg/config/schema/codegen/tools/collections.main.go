// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"
	"strings"

	"istio.io/istio/pkg/config/schema/ast"
	"istio.io/istio/pkg/config/schema/codegen"
)

// Utility for generating collections.gen.go. Called from gen.go
func main() {
	if len(os.Args) != 4 && len(os.Args) != 7 {
		fmt.Printf("Invalid args: %v", os.Args)
		os.Exit(-1)
	}

	pkg := os.Args[1]
	input := os.Args[2]
	output := os.Args[3]
	var splitOn, splitOutput, splitPrefix string
	if len(os.Args) > 4 {
		splitOn = os.Args[4]
		splitOutput = os.Args[5]
		splitPrefix = os.Args[6]
	}

	// Read the input file
	b, err := os.ReadFile(input)
	if err != nil {
		fmt.Printf("unable to read input file: %v", err)
		os.Exit(-2)
	}

	// Parse the file.
	m, err := ast.Parse(string(b))
	if err != nil {
		fmt.Printf("failed parsing input file: %v", err)
		os.Exit(-3)
	}

	var contents string

	if pkg == "gvk" {
		contents, err = codegen.WriteGvk(pkg, m)
		if err != nil {
			fmt.Printf("Error applying static init template: %v", err)
			os.Exit(-3)
		}
		if err = os.WriteFile(output, []byte(contents), os.ModePerm); err != nil {
			fmt.Printf("Error writing output file: %v", err)
			os.Exit(-4)
		}
		return
	}
	if pkg == "kind" {
		contents, err = codegen.WriteKind(pkg, m)
		if err != nil {
			fmt.Printf("Error applying static init template: %v", err)
			os.Exit(-3)
		}
		if err = os.WriteFile(output, []byte(contents), os.ModePerm); err != nil {
			fmt.Printf("Error writing output file: %v", err)
			os.Exit(-4)
		}
		return
	}
	if splitOn == "" {
		contents, err = codegen.StaticCollections(pkg, m, func(name string) bool {
			return true
		}, "")
		if err != nil {
			fmt.Printf("Error applying static init template: %v", err)
			os.Exit(-3)
		}
		if err = os.WriteFile(output, []byte(contents), os.ModePerm); err != nil {
			fmt.Printf("Error writing output file: %v", err)
			os.Exit(-4)
		}
		return
	}
	fullPrefix := "// +build !" + splitPrefix
	fullContents, err := codegen.StaticCollections(pkg, m, func(name string) bool {
		return true
	}, fullPrefix)
	if err != nil {
		fmt.Printf("Error applying static init template: %v", err)
		os.Exit(-3)
	}
	if err = os.WriteFile(output, []byte(fullContents), os.ModePerm); err != nil {
		fmt.Printf("Error writing output file: %v", err)
		os.Exit(-4)
	}
	matchPrefix := "// +build " + splitPrefix
	matchContents, err := codegen.StaticCollections(pkg, m, func(name string) bool {
		return !strings.Contains(name, splitOn)
	}, matchPrefix)
	if err != nil {
		fmt.Printf("Error applying static init template: %v", err)
		os.Exit(-3)
	}
	if err = os.WriteFile(splitOutput, []byte(matchContents), os.ModePerm); err != nil {
		fmt.Printf("Error writing output file: %v", err)
		os.Exit(-4)
	}
	return
}
