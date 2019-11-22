// Copyright 2019 Istio Authors
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

package mesh

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type manifestApplyArgs struct {
	// inFilename is the path to the input IstioControlPlane CR.
	inFilename string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config
	context string
	// readinessTimeout is maximum time to wait for all Istio resources to be ready.
	readinessTimeout time.Duration
	// wait is flag that indicates whether to wait resources ready before exiting.
	wait bool
	// skipConfirmation determines whether the user is prompted for confirmation.
	// If set to true, the user is not prompted and a Yes response is assumed in all cases.
	skipConfirmation bool
	// force proceeds even if there are validation errors
	force bool
	// set is a string with element format "path=value" where path is an IstioControlPlane path and the value is a
	// value to set the node at that path to.
	set []string
}

func addManifestApplyFlags(cmd *cobra.Command, args *manifestApplyArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use")
	cmd.PersistentFlags().BoolVar(&args.skipConfirmation, "skip-confirmation", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors")
	cmd.PersistentFlags().DurationVar(&args.readinessTimeout, "readiness-timeout", 300*time.Second, "Maximum seconds to wait for all Istio resources to be ready."+
		" The --wait flag must be set for this flag to apply")
	cmd.PersistentFlags().BoolVarP(&args.wait, "wait", "w", false, "Wait, if set will wait until all Pods, Services, and minimum number of Pods "+
		"of a Deployment are in a ready state before the command exits. It will wait for a maximum duration of --readiness-timeout seconds")
	cmd.PersistentFlags().StringSliceVarP(&args.set, "set", "s", nil, setFlagHelpStr)
}

func manifestApplyCmd(rootArgs *rootArgs, maArgs *manifestApplyArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Generates and applies an Istio install manifest.",
		Long:  "The apply subcommand generates an Istio install manifest and applies it to a cluster.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			// Passing cmd.OutOrStdXXX() allows capturing command output for e2e tests.
			l := newLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.OutOrStderr())
			// Warn users if they use `manifest apply` without any config args.
			if maArgs.inFilename == "" && len(maArgs.set) == 0 && !maArgs.skipConfirmation {
				if !confirm("This will install the default Istio profile into the cluster. Proceed? (y/N)", cmd.OutOrStdout()) {
					cmd.Print("Cancelled.\n")
					os.Exit(1)
				}
			}
			manifestApply(rootArgs, maArgs, l)
		}}
}

func manifestApply(args *rootArgs, maArgs *manifestApplyArgs, l *logger) {
	if err := configLogs(args.logToStdErr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}
	if err := genApplyManifests(maArgs.set, maArgs.inFilename, maArgs.force, args.dryRun, args.verbose,
		maArgs.kubeConfigPath, maArgs.context, maArgs.readinessTimeout, l); err != nil {
		l.logAndFatalf("Failed to generate and apply manifests, error: %v", err)
	}
}

func confirm(msg string, writer io.Writer) bool {
	fmt.Fprintf(writer, "%s ", msg)

	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		return false
	}
	response = strings.ToUpper(response)
	if response == "Y" || response == "YES" {
		return true
	}

	return false
}
