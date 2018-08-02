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

package settings

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"istio.io/istio/pkg/log"
)

var settings = defaultSettings()
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
	// First, apply the environment variables.
	applyEnvironmentVariables(settings)

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

	// TODO: Instead of using hub/tag, we should be using the local registry to load images from.
	// See https://github.com/istio/istio/issues/6178 for details.

	// Capture environment variables
	hub := os.Getenv("HUB")
	tag := os.Getenv("TAG")
	settings.Hub = hub
	settings.Tag = tag

	return nil
}

func attachFlags(stringVar func(*string, string, string, string), boolVar func(*bool, string, bool, string)) {
	stringVar(&settings.WorkDir, "work_dir", os.TempDir(),
		"Local working directory for creating logs/temp files. If left empty, os.TempDir() is used.")

	stringVar((*string)(&settings.Environment), "environment", string(settings.Environment),
		fmt.Sprintf("Specify the environment to run the tests against. Allowed values are: [%s, %s]",
			Local, Kubernetes))

	stringVar(&settings.KubeConfig, "config", settings.KubeConfig,
		"The path to the kube config file for cluster environments")

	boolVar(&showHelp, "hh", showHelp,
		"Show the help page for the test framework")

	boolVar(&settings.NoCleanup, "no-cleanup", settings.NoCleanup, "Do not cleanup resources after test completion")
}

func doShowHelp() {
	var lines []string
	cmdForLog.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		line := fmt.Sprintf("  -%-24s %s", f.Name, f.Usage)
		lines = append(lines, line)
	})

	fmt.Printf(
		`Command-line options for the test framework:
		
%s

`, strings.Join(lines, "\n"))
}

func applyEnvironmentVariables(a *Settings) {
	if ISTIO_TEST_KUBE_CONFIG.Value() != "" {
		a.KubeConfig = ISTIO_TEST_KUBE_CONFIG.Value()
	}

	if ISTIO_TEST_ENVIRONMENT.Value() != "" {
		a.Environment = EnvironmentID(ISTIO_TEST_ENVIRONMENT.Value())
	}
}
