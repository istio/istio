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

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"istio.io/istio/prow/asm/tester/interface/types"
)

// Run instantiates and executes the pipeline tester cobra command, returning the result
func Run(testerName string, newPipelineTester types.NewPipelineTester) error {
	return NewCommand(testerName, newPipelineTester).Execute()
}

// NewCommand returns a new cobra.Command for pipeline tester
func NewCommand(testerName string, newPipelineTester types.NewPipelineTester) *cobra.Command {
	cmd := &cobra.Command{
		Use: testerName,
		// we defer showing usage, so that we can include deployer and test
		// specific usage in RealMain(...)
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runE(cmd, args, testerName, newPipelineTester)
		},
	}
	// we implement custom flag parsing below
	cmd.DisableFlagParsing = true
	return cmd
}

// runE implements the custom CLI logic
func runE(
	cmd *cobra.Command, args []string,
	testerName string, newPipelineTester types.NewPipelineTester,
) error {
	// setup the options struct & flags, etc.
	opts := types.Options{}
	testerCommonFlags := pflag.NewFlagSet(testerName, pflag.ContinueOnError)
	pipelineTester, testerCustomFlags := newPipelineTester(opts)
	bindFlags(&opts, pipelineTester, testerCommonFlags)

	// NOTE: unknown flags are forwarded to the real tester as arguments
	testerCommonFlags.ParseErrorsWhitelist.UnknownFlags = true

	// parse the tester common flags
	// NOTE: parseError should contain the first error from parsing.
	// We will later show this + usage if there is one
	parseError := testerCommonFlags.Parse(args)

	// check that the real tester did not register any identical flags
	testerCustomFlags.VisitAll(func(f *pflag.Flag) {
		if testerCommonFlags.Lookup(f.Name) != nil {
			panic(errors.Errorf("tester common flag %#v re-registered", f.Name))
		}
		if f.Shorthand != "" && testerCommonFlags.ShorthandLookup(f.Shorthand) != nil {
			panic(errors.Errorf("tester common shorthand flag %#v re-registered", f.Shorthand))
		}
	})

	// parse the combined common flags and custom flags
	allFlags := pflag.NewFlagSet(testerName, pflag.ContinueOnError)
	allFlags.AddFlagSet(testerCommonFlags)
	allFlags.AddFlagSet(testerCustomFlags)
	if err := allFlags.Parse(args); err != nil && parseError == nil {
		// NOTE: we only retain the first parse error currently, and handle below
		parseError = err
	}

	// print usage and return if no args are provided, or help is explicitly requested
	if len(args) == 0 || opts.Help {
		fmt.Printf("Usage for tester:\n")
		fmt.Println("  tester [Flags]")
		fmt.Println()
		fmt.Println("Common tester flags:")
		testerCommonFlags.PrintDefaults()
		fmt.Println()
		fmt.Println("Custom tester flags:")
		testerCustomFlags.PrintDefaults()
		return nil
	}

	// otherwise if we encountered any errors with the user input
	// show the error / help, usage and then return
	if parseError != nil {
		return parseError
	}

	// run RealMain, which contains all of the logic beyond the CLI boilerplate
	return RealMain(opts, pipelineTester)
}

// bindFlags registers all first class kubetest2 flags
func bindFlags(o *types.Options, bpt types.BasePipelineTester, flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Help, "help", "h", false, "display help")
	_, lifecycleEnvEnabled := bpt.(types.LifecycleEnv)
	_, lifecycleTestsEnabled := bpt.(types.LifecycleTests)

	if lifecycleEnvEnabled {
		flags.BoolVar(&o.SetupEnv, "setup-env", false, "setup the environment. This will be the first step of the pipeline, so it's recommended to install required tools and/or setup env vars, IAMs in this step")
		flags.BoolVar(&o.TeardownEnv, "teardown-env", false, "teardown the environment")
	}

	flags.BoolVar(&o.SetupSystem, "setup-system", false, "setup the system. For most of the Kubernetes-based projects, it's recommended to install the SUT on the clusters in this step")
	flags.BoolVar(&o.TeardownSystem, "teardown-system", false, "teardown the system")

	if lifecycleTestsEnabled {
		flags.BoolVar(&o.SetupTests, "setup-tests", false, "perform required setups before running the final tests")
		flags.BoolVar(&o.TeardownTests, "teardown-tests", false, "perform required teardowns after running the tests")
	}

	flags.BoolVar(&o.RunTests, "run-tests", false, "run the tests")
}
