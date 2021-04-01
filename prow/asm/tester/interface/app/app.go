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

package app

import (
	"fmt"
	"os"
	"path/filepath"

	"istio.io/istio/prow/asm/tester/interface/types"
	"sigs.k8s.io/kubetest2/pkg/metadata"
)

// Main implements the kubetest2 pipeline tester binary entrypoint
// Each pipeline tester binary should invoke this
func Main(testerName string, newPipelineTester types.NewPipelineTester) {
	// see cmd.go for the rest of the CLI boilerplate
	if err := Run(testerName, newPipelineTester); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// RealMain contains nearly all of the application logic / control flow
// beyond the command line boilerplate
func RealMain(opts types.Options, pt types.BasePipelineTester) (result error) {
	_, lifecycleEnvEnabled := pt.(types.LifecycleEnv)
	_, lifecycleTestsEnabled := pt.(types.LifecycleTests)

	// setup the metadata writer
	junitRunner, err := os.Create(
		filepath.Join(os.Getenv("ARTIFACTS"), "junit_tester_runner.xml"),
	)
	if err != nil {
		return fmt.Errorf("error creating the junit output file: %w", err)
	}
	writer := metadata.NewWriter("kubetest2-pipeline-tester", junitRunner)
	// defer writing out the metadata on exit
	// NOTE: defer is LIFO, so this should actually be the finish time
	defer func() {
		if err := writer.Finish(); err != nil && result == nil {
			result = err
		}
		if err := junitRunner.Sync(); err != nil && result == nil {
			result = err
		}
		if err := junitRunner.Close(); err != nil && result == nil {
			result = err
		}
	}()

	if lifecycleEnvEnabled {
		// setup env if specified
		if opts.SetupEnv {
			if err := writer.WrapStep("SetupEnv", pt.(types.LifecycleEnv).SetupEnv); err != nil {
				return err
			}
		}
		// teardown env at the end if specified
		defer func() {
			if opts.TeardownEnv {
				if err := writer.WrapStep("TeardownEnv", pt.(types.LifecycleEnv).TeardownEnv); err != nil {
					result = err
				}
			}
		}()
	}

	// setup system if specified
	if opts.SetupSystem {
		if err := writer.WrapStep("SetupSystem", pt.SetupSystem); err != nil {
			return err
		}
	}

	// teardown system at the end if specified
	defer func() {
		if opts.TeardownSystem {
			if err := writer.WrapStep("TeardownSystem", pt.TeardownSystem); err != nil {
				result = err
			}
		}
	}()

	if lifecycleTestsEnabled {
		// setup tests if specified
		if opts.SetupTests {
			if err := writer.WrapStep("SetupTests", pt.(types.LifecycleTests).SetupTests); err != nil {
				return err
			}
		}
		// teardown tests at the end if specified
		defer func() {
			if opts.TeardownTests {
				if err := writer.WrapStep("TeardownTests", pt.(types.LifecycleTests).TeardownTests); err != nil {
					result = err
				}
			}
		}()
	}

	// run the tests if specified.
	if opts.RunTests {
		if err := writer.WrapStep("RunTests", pt.RunTests); err != nil {
			return err
		}
	}

	return nil
}
