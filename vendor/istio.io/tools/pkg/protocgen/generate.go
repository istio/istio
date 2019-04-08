// Copyright 2018 Istio Authors
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

package protocgen

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/golang/protobuf/proto"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

// GenerateFn is a function definition for encapsulating the ore logic of code generation.
type GenerateFn func(req plugin.CodeGeneratorRequest) (*plugin.CodeGeneratorResponse, error)

// Generate is a wrapper for a main function of a protoc generator plugin.
func Generate(fn GenerateFn) {
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		fatal("Unable to read input proto: %v\n", err)
	}

	var request plugin.CodeGeneratorRequest
	if err = proto.Unmarshal(data, &request); err != nil {
		fatal("Unable to parse input proto: %v\n", err)
	}

	response, err := fn(request)
	if err != nil {
		fatal("%v", err)
	}

	data, err = proto.Marshal(response)
	if err != nil {
		fatal("Unable to serialize output proto: %v\n", err)
	}

	_, err = os.Stdout.Write(data)
	if err != nil {
		fatal("Unable to write output proto: %v\n", err)
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format, args...)
	os.Exit(1)
}
