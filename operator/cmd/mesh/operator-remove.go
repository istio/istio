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

package mesh

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/istioctl/pkg/cli"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
)

type operatorRemoveArgs struct {
	// skipConfirmation determines whether the user is prompted for confirmation.
	// If set to true, the user is not prompted and a Yes response is assumed in all cases.
	skipConfirmation bool
	// force proceeds even if there are validation errors
	force bool
	// operatorNamespace is the namespace the operator controller is installed into.
	operatorNamespace string
	// revision is the Istio control plane revision the command targets.
	revision string
	// purge if set to true, all revisions of Istio operator will be specified.
	purge bool
}

func addOperatorRemoveFlags(cmd *cobra.Command, oiArgs *operatorRemoveArgs) {
	cmd.PersistentFlags().BoolVarP(&oiArgs.skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&oiArgs.force, "force", false, ForceFlagHelpStr)
	cmd.PersistentFlags().StringVar(&oiArgs.operatorNamespace, "operatorNamespace", operatorDefaultNamespace, OperatorNamespaceHelpstr)
	cmd.PersistentFlags().StringVarP(&oiArgs.revision, "revision", "r", "", OperatorRevFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&oiArgs.purge, "purge", false, AllOperatorRevFlagHelpStr)
}

func operatorRemoveCmd(ctx cli.Context, rootArgs *RootArgs, orArgs *operatorRemoveArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Removes the Istio operator controller from the cluster.",
		Long:  "The remove subcommand removes the Istio operator controller from the cluster.",
		Args: func(cmd *cobra.Command, args []string) error {
			if orArgs.revision == "" && !orArgs.purge {
				return fmt.Errorf("at least one of the --revision or --purge flags must be set")
			}
			if len(args) > 0 {
				return fmt.Errorf("istioctl operator remove does not take arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.OutOrStderr(), installerScope)
			operatorRemove(cmd, client, rootArgs, orArgs, l)
			return nil
		},
	}
}

// operatorRemove removes the Istio operator controller from the cluster.
func operatorRemove(cmd *cobra.Command, cliClient kube.CLIClient, args *RootArgs, orArgs *operatorRemoveArgs, l clog.Logger) {
	kubeClient, client, err := KubernetesClients(cliClient, l)
	if err != nil {
		l.LogAndFatal(err)
	}

	// If the user is performing purge but also specified a revision, we should warn
	// that the purge will still remove all resources
	if orArgs.purge && orArgs.revision != "" {
		orArgs.revision = ""
		l.LogAndPrint("Purge remove will remove all Istio operator controller, ignoring the specified revision\n")
	}

	var installed bool
	if orArgs.revision == "default" {
		installed, err = isControllerInstalled(kubeClient.Kube(), orArgs.operatorNamespace, "")
	} else {
		installed, err = isControllerInstalled(kubeClient.Kube(), orArgs.operatorNamespace, orArgs.revision)
	}

	if installed && err != nil {
		l.LogAndFatal(err)
	}
	if !installed && !orArgs.purge {
		l.LogAndPrintf("Operator controller is not installed in %s namespace (no Deployment detected).", orArgs.operatorNamespace)
		if !orArgs.force {
			l.LogAndFatal("Aborting, use --force to override.")
		}
	}

	var message string
	message = "All revisions of Istio operator will be removed from cluster, Proceed? (y/N)"
	if orArgs.revision != "" {
		message = "Istio operator revision " + orArgs.revision + " will be removed from cluster, Proceed? (y/N)"
	}
	if !orArgs.skipConfirmation && !args.DryRun && !Confirm(message, cmd.OutOrStdout()) {
		cmd.Print("Cancelled.\n")
		os.Exit(1)
	}

	l.LogAndPrintf("Removing Istio operator...")
	// Create an empty IOP for the purpose of populating revision. Apply code requires a non-nil IOP.
	var iop *iopv1alpha1.IstioOperator
	if orArgs.revision != "" {
		emptyiops := &v1alpha1.IstioOperatorSpec{Profile: "empty", Revision: orArgs.revision}
		iop, err = translate.IOPStoIOP(emptyiops, "", "")
		if err != nil {
			l.LogAndFatal(err)
		}
	}
	reconciler, err := helmreconciler.NewHelmReconciler(client, kubeClient, iop, &helmreconciler.Options{DryRun: args.DryRun, Log: l})
	if err != nil {
		l.LogAndFatal(err)
	}
	rs, err := reconciler.GetPrunedResources(orArgs.revision, false, string(name.IstioOperatorComponentName))
	if err != nil {
		l.LogAndFatal(err)
	}
	if err := reconciler.DeleteObjectsList(rs, string(name.IstioOperatorComponentName)); err != nil {
		l.LogAndFatal(err)
	}

	l.LogAndPrint(color.New(color.FgGreen).Sprint("✔ ") + "Removal complete")
}
