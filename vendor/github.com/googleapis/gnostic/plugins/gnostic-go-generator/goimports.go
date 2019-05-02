// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
)

// Run goimports to format and update imports statements in generated code.
func goimports(filename string, inputBytes []byte) (outputBytes []byte, err error) {
	if false {
		return inputBytes, nil
	}
	cmd := exec.Command(os.Getenv("GOPATH") + "/bin/goimports")
	input, _ := cmd.StdinPipe()
	output, _ := cmd.StdoutPipe()
	cmderr, _ := cmd.StderrPipe()
	err = cmd.Start()
	if err != nil {
		return
	}
	input.Write(inputBytes)
	input.Close()

	outputBytes, _ = ioutil.ReadAll(output)
	errors, _ := ioutil.ReadAll(cmderr)
	if len(errors) > 0 {
		errors := strings.Replace(string(errors), "<standard input>", filename, -1)
		log.Printf("Syntax errors in generated code:\n%s", errors)
		return inputBytes, nil
	}

	return
}
