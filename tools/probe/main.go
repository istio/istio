// Copyright 2017 Istio Authors
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

package main

import (
	"fmt"
	"os"
)

func main() {
	// This is a tool for k8s liveness/readiness probe; this commandline
	// tool check the existence of a file path and fails if the file does
	// not exist. See //pkg/probe for the part of creating such files.
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Target is unspecified.\n")
		os.Exit(2)
	}
	target := os.Args[1]
	if _, err := os.Stat(target); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find the path %s: %v\n", target, err)
		os.Exit(1)
	}
}
