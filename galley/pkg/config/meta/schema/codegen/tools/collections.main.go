// Copyright 2019 Istio Authors
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

// +build ignore

package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"istio.io/istio/galley/pkg/config/meta/schema"
	"istio.io/istio/galley/pkg/config/meta/schema/codegen"
)

// Utility for generating collections.gen.go. Called from gen.go
func main() {
	if len(os.Args) != 4 {
		fmt.Printf("Invalid args: %v", os.Args)
		os.Exit(-1)
	}

	pkg := os.Args[1]
	input := os.Args[2]
	output := os.Args[3]

	c, err := readMetadata(input)
	if err != nil {
		fmt.Printf("Error reading metadata: %v", err)
		os.Exit(-2)
	}

	var names []string
	for _, r := range c.AllCollections().All() {
		names = append(names, r.Name.String())
	}
	contents, err := codegen.StaticCollections(pkg, names)
	if err != nil {
		fmt.Printf("Error applying static init template: %v", err)
		os.Exit(-3)
	}

	if err = ioutil.WriteFile(output, []byte(contents), os.ModePerm); err != nil {
		fmt.Printf("Error writing output file: %v", err)
		os.Exit(-4)
	}
}

func readMetadata(path string) (*schema.Metadata, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read input file: %v", err)
	}

	return schema.ParseAndBuild(string(b))
}
