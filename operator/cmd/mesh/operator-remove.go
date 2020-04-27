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
	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/util/clog"
)

type operatorRemoveArgs struct {
	operatorInitArgs
	// force proceeds even if there are validation errors
	force bool
}

func addOperatorRemoveFlags(cmd *cobra.Command, oiArgs *operatorRemoveArgs) {
	addOperatorInitFlags(cmd, &oiArgs.operatorInitArgs)
	cmd.PersistentFlags().BoolVar(&oiArgs.force, "force", false, "Proceed even with errors")
}

func operatorRemoveCmd(rootArgs *rootArgs, orArgs *operatorRemoveArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Removes the Istio operator controller from the cluster.",
		Long:  "The remove subcommand removes the Istio operator controller from the cluster.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.OutOrStderr())
			operatorRemove(rootArgs, orArgs, l)
		}}
}

// operatorRemove removes the Istio operator controller from the cluster.
func operatorRemove(args *rootArgs, orArgs *operatorRemoveArgs, l clog.Logger) {
	initLogsOrExit(args)

	restConfig, clientset, client, err := K8sConfig(orArgs.kubeConfigPath, orArgs.context)
	if err != nil {
		l.LogAndFatal(err)
	}

	installed, err := isControllerInstalled(clientset, orArgs.common.operatorNamespace)
	if installed && err != nil {
		l.LogAndFatal(err)
	}
	if !installed {
		l.LogAndPrintf("Operator controller is not installed in %s namespace (no Deployment detected).", orArgs.common.operatorNamespace)
		if !orArgs.force {
			l.LogAndFatal("Aborting, use --force to override.")
		}
	}

	l.LogAndPrintf("Removing Istio operator...")
	reconciler, err := helmreconciler.NewHelmReconciler(client, restConfig, nil, &helmreconciler.Options{DryRun: args.dryRun, Log: l})
	if err != nil {
		l.LogAndFatal(err)
	}
	if err := reconciler.DeleteComponent(string(name.IstioOperatorComponentName)); err != nil {
		l.LogAndFatal(err)
	}

	l.LogAndPrint("✔ Removal complete")
}
