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
	"os"

	"github.com/spf13/cobra"
)

type manifestUninstallArgs struct {
	// inFilename is an array of paths to the input IstioOperator CR files.
	inFilename []string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config
	context string
	// skipConfirmation determines whether the user is prompted for confirmation.
	// If set to true, the user is not prompted and a Yes response is assumed in all cases.
	skipConfirmation bool
	// force proceeds even if there are validation errors
	force bool
	// set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	set []string
}

func addManifestUninstallFlags(cmd *cobra.Command, args *manifestUninstallArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilename, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use")
	cmd.PersistentFlags().BoolVarP(&args.skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors")
	cmd.PersistentFlags().StringArrayVarP(&args.set, "set", "s", nil, SetFlagHelpStr)
}

func manifestUninstallCmd(rootArgs *rootArgs, muArgs *manifestUninstallArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "(experimental) Removes Istio from a cluster.",
		Long: `(experimental) The uninstall subcommand generates an Istio manifest and removes manifest items from a cluster.
THIS COMMAND IS STILL UNDER ACTIVE DEVELOPMENT AND NOT READY FOR PRODUCTION USE.`,
		Example: "istioctl manifest uninstall  # deletes the default profile on the current Kubernetes cluster context\n" +
			"istioctl manifest uninstall --set profile=demo",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.ErrOrStderr())
			// Warn users if they use `manifest uninstall` without any config args.
			if len(muArgs.inFilename) == 0 && len(muArgs.set) == 0 && !rootArgs.dryRun && !muArgs.skipConfirmation {
				if !confirm("This will remove the default Istio profile from the cluster. Proceed? (y/N)", cmd.OutOrStdout()) {
					cmd.Print("Cancelled.\n")
					os.Exit(1)
				}
			}
			return manifestUninstall(rootArgs, muArgs, l)
		}}
}

func manifestUninstall(args *rootArgs, maArgs *manifestUninstallArgs, l *Logger) error {
	if err := configLogs(args.logToStdErr); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}
	if err := genUninstallManifests(maArgs.set, maArgs.inFilename, maArgs.force, args.dryRun, args.verbose,
		maArgs.kubeConfigPath, maArgs.context, l); err != nil {
		return fmt.Errorf("failed to generate and uninstall manifests, error: %v", err)
	}

	return nil
}
