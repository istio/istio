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

// Package perf is a helper library for writing Mixer perf tests. It is designed to have a serializable, declarative
// configuration, that can be run in various execution modes transparently. The package is designed to work
// seamlessly with the Go perf testing infrastructure.
//
// The entry point to the tests is the "Run*" methods. The benchmarks are expected to call one of the methods, passing in
// testing.B, the test declaration (i.e. Setup struct), an environment variable that contains the ambient template,
// adapter info (i.e. Env struct), along with any other Run-method specific inputs.
//
// The top-level struct for test declaration is the Setup struct. This contains two major fields, namely Config and Load.
// The Config field contains the full configuration needed for a Mixer Server, including global and service config Yaml files,
// as well as the adapters and templates that should be incorporated from the ambient context. The Load section is a
// customizable declaration of the type of the load that should be applied to Mixer during test.  There is a standard
// set of configs/setups available in this package as well, to simplify test authoring.
//
// Currently, the tests can be run in two modes: inprocess, or coprocess. Inprocess creates a client within the same
// process as the test, whereas coprocess creates the client in a separate, external process. Creating the client in
// an external process isolates the server code as the only executing code within the benchmark, thus enables more
// accurate measurement of various perf outputs (i.e. memory usage, cpu usage etc.)
//
// Execution-wise, the test is orchestrated by the Controller struct, which internally uses testing.B. When the Run*
// entry method is called, it first creates a Controller, which establishes an Rpc server for registration of agents
// (i.e. clients). Then, the run method would either launch the external exe hosting a ClientServer, or it will
// simply start one locally. Once the ClientServer starts, it registers itself with the Controller. Once the registration
// is done, the controller initializes the client(s) by uploading Setup for the test and giving the address of the Mixer
// server. Then, controller commands the client(s) to execute the load, multiplied by an iteration factor which is obtained
// from testing.B. This can happen multiple times. Finally, the controller signals the clients to gracefully close
// and ends the benchmark.
package perf

import (
	"github.com/ghodss/yaml"
)

// Setup is the self-contained, top-level definition of a perf test.  This structure is meant to be
// fully-serializable to support  intra-process communication. As such, it should only have serializable members.
type Setup struct {
	// Config is the Mixer config to use for this test.
	Config Config `json:"config"`

	// Loads is an array of Load passed from different clients in parallel.
	Loads []Load `json:"loads"`
}

// marshalLoad converts a load object into YAML serialized form.
func marshalLoad(load *Load) ([]byte, error) {
	return yaml.Marshal(load)
}

// unmarshalLoad reads a Load object from a YAML serialized form.
func unmarshalLoad(bytes []byte, load *Load) error {
	return yaml.Unmarshal(bytes, load)
}
