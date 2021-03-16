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

package types

import "github.com/spf13/pflag"

// NewPipelineTester should return a new instance of a PipelineTester along with a flagset
// bound to the tester with any additional Tester specific CLI flags
//
// opts will provide access to options defined by common flags
type NewPipelineTester func(opts Options) (pipelineTester BasePipelineTester, flags *pflag.FlagSet)

// Options holds values that control which phases to run for the pipeline.
type Options struct {
	Help           bool
	SetupEnv       bool
	SetupSystem    bool
	SetupTests     bool
	RunTests       bool
	TeardownTests  bool
	TeardownSystem bool
	TeardownEnv    bool
}

// BasePipelineTester defines the base interface for implementing a pipeline-like
// tester
type BasePipelineTester interface {
	// SetupSystem should set up the SUT - System Under Test, usually this
	// means installing system control plane on the Kubernetes cluster(s)
	SetupSystem() error
	// RunTests will run the test cases. For projects written in Go this will
	// be a simple wrapper of go test command
	RunTests() error
	// TeardownSystem will tear down system setups performed in SetupSystem
	TeardownSystem() error
}

type LifecycleEnv interface {
	// SetupEnv should perform initial setups for running the test flow, such
	// as performing prechecks, installing tools, setup env vars
	SetupEnv() error
	// TeardownEnv will tear down env setups performed in SetupEnv
	TeardownEnv() error
}

type LifecycleTests interface {
	// SetupTests should perform required setups before running the actual
	// tests, such as installing applications that are needed in the tests
	SetupTests() error
	// TeardownTests will tear down the setups performed in SetupTests
	TeardownTests() error
}
