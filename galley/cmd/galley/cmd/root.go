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

package cmd

import (
	"flag"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"istio.io/istio/galley/pkg/settings"

	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/galley/pkg/server"
	istiocmd "istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/version"
)

const defaultSettingsFile = "galley-settings.yaml"

var (
	settingsFile   = defaultSettingsFile
	loggingOptions = log.DefaultOptions()
)

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {

	var (
		livenessProbeController  probe.Controller
		readinessProbeController probe.Controller
	)

	rootCmd := &cobra.Command{
		Use:          "galley",
		Short:        "Galley provides configuration management services for Istio.",
		Long:         "Galley provides configuration management services for Istio.",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%q is an invalid argument", args[0])
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {

			s, err := settings.ParseFileOrDefault(settingsFile)
			if err != nil {
				log.Fatalf("Unable to load and parse settings file (%q): %v", settingsFile, err)
				return
			}

			livenessProbeOptions, err := settings.ToProbeOptions(&s.General.Liveness)
			if err != nil {
				log.Fatalf("Invalid liveness probe options: %v", err)
				return
			}
			if livenessProbeOptions.IsValid() {
				livenessProbeController = probe.NewFileController(livenessProbeOptions)
			}

			readinessProbeOptions, err := settings.ToProbeOptions(&s.General.Readiness)
			if err != nil {
				log.Fatalf("Invalid liveness probe options: %v", err)
				return
			}
			if readinessProbeOptions.IsValid() {
				readinessProbeController = probe.NewFileController(readinessProbeOptions)
			}

			serverArgs, err := settings.ToServerArgs(s)
			if err != nil {
				log.Fatalf("Invalid server args: %v", err)
			}

			validationArgs, err := settings.ToValidationArgs(s)
			if err != nil {
				log.Fatalf("Invalid validation args: %v", err)
			}

			if !serverArgs.EnableServer && !validationArgs.EnableValidation {
				log.Fatala("Galley must be running under at least one mode: server or validation")
			}

			if serverArgs.EnableServer {
				go server.RunServer(serverArgs, livenessProbeController, readinessProbeController)
			}
			if validationArgs.EnableValidation {
				go validation.RunValidation(validationArgs, s.General.KubeConfig, livenessProbeController, readinessProbeController)
			}
			galleyStop := make(chan struct{})
			go server.StartSelfMonitoring(galleyStop, uint(s.General.MonitoringPort))

			if s.General.EnableProfiling {
				go server.StartProfiling(galleyStop, uint(s.General.PprofPort))
			}

			go server.StartProbeCheck(livenessProbeController, readinessProbeController, galleyStop)
			istiocmd.WaitSignal(galleyStop)
		},
	}

	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	rootCmd.PersistentFlags().StringVar(&settingsFile, "settings", defaultSettingsFile,
		"Settings file to use")

	rootCmd.AddCommand(probeCmd())
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Galley Server",
		Section: "galley CLI",
		Manual:  "Istio Galley Server",
	}))

	loggingOptions.AttachCobraFlags(rootCmd)

	return rootCmd
}
