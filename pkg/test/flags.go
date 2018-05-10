//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package test

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/impl/driver"
)

var arguments = driver.DefaultArgs()
var showHelp = false
var logOptions = log.DefaultOptions()

// The command we use to parse flags.
var cmdForLog = &cobra.Command{}

// init registers the command-line flags that we can exposed for "go test".
func init() {
	// First, attach the logOptions to the cobra command. This just establishes their relationship.
	logOptions.AttachCobraFlags(cmdForLog)

	// Attach our own flags to Cobra flags. This is how we will read them.
	attachFlags(cmdForLog.PersistentFlags().StringVar, cmdForLog.PersistentFlags().BoolVar)

	// Apply the flags captured in cobra to the regular Go flags, so that Go flag code won't barf when
	// it sees these flags.
	cmdForLog.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Value.Type() == "bool" {
			_ = flag.Bool(f.Name, false, f.Usage)
		} else {
			_ = flag.String(f.Name, f.Value.String(), f.Usage)
		}
	})
}

func processFlags() error {
	flag.Parse()
	if err := cmdForLog.PersistentFlags().Parse(os.Args[1:]); err != nil {
		if err == pflag.ErrHelp {
			showHelp = true
		} else {
			return err
		}
	}

	if err := log.Configure(logOptions); err != nil {
		return err
	}

	if showHelp {
		doShowHelp()
		os.Exit(0)
	}

	return nil
}

func attachFlags(stringVar func(*string, string, string, string), boolVar func(*bool, string, bool, string)) {
	stringVar(&arguments.Labels, "labels", arguments.Labels,
		"Only run tests with the given labels")

	stringVar(&arguments.Environment, "environment", arguments.Environment,
		fmt.Sprintf("Specify the environment to run the tests against. Allowed values are: [%s, %s]",
			driver.EnvLocal, driver.EnvKubernetes))

	stringVar(&arguments.KubeConfig, "config", arguments.KubeConfig,
		"The path to the kube config file for cluster environments")

	boolVar(&showHelp, "hh", showHelp,
		"Show the help page for the test framework")
}

func doShowHelp() {
	var lines []string
	cmdForLog.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		line := fmt.Sprintf("  -%s\t%s", f.Name, f.Usage)
		lines = append(lines, line)
	})

	fmt.Printf(
		`Command-line options for the test framework:
		
%s

`, strings.Join(lines, "\n"))
}
