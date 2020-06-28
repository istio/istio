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
	"strings"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	proxyinfo "istio.io/istio/pkg/proxy"
	"istio.io/pkg/log"
)

type uninstallArgs struct {
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config.
	context string
	// skipConfirmation determines whether the user is prompted for confirmation.
	// If set to true, the user is not prompted and a Yes response is assumed in all cases.
	skipConfirmation bool
	// revision is the Istio control plane revision the command targets.
	revision string
	// istioNamespace is the target namespace of istio control plane.
	istioNamespace string
}

func addUninstallFlags(cmd *cobra.Command, args *uninstallArgs) {
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config.")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use.")
	cmd.PersistentFlags().BoolVarP(&args.skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.revision, "revision", "r", "", revisionFlagHelpStr)
	cmd.PersistentFlags().StringVar(&args.istioNamespace, "istioNamespace", istioDefaultNamespace,
		"The namespace of Istio Control Plane")
	cmd.MarkPersistentFlagRequired("revision")
}

func UninstallCmd(logOpts *log.Options) *cobra.Command {
	rootArgs := &rootArgs{}
	uiArgs := &uninstallArgs{}
	uicmd := &cobra.Command{
		Use:   "uninstall --revision foo",
		Short: "uninstall the control plane by revision",
		Long:  "The uninstall command uninstall the control plane by revision and" +
			" issue possible warning if there are proxies still pointing to it.",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstallRev(cmd, rootArgs, uiArgs, logOpts)
		}}
	addUninstallFlags(uicmd, uiArgs)
	return uicmd
}

func uninstallRev(cmd *cobra.Command, rootArgs *rootArgs, uiArgs *uninstallArgs, logOpts *log.Options) error {
	l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
	pids, err := getStaleProxies(uiArgs)
	if err != nil {
		return err
	}
	if len(pids) != 0 && !rootArgs.dryRun && !uiArgs.skipConfirmation {
		if !confirm(fmt.Sprintf("There are still proxies pointing to the control plane revision: %s:\n%s."+
			" You can choose to proceed or upgrade the data plane then retry. Proceed? (y/N)",
			uiArgs.revision, strings.Join(pids, " \n")), cmd.OutOrStdout()) {
			cmd.Print("Cancelled.\n")
			os.Exit(1)
		}
	}
	if err := configLogs(logOpts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}
	if err := pruneManifests(uiArgs.revision, rootArgs.dryRun,
		uiArgs.kubeConfigPath, uiArgs.context, l); err != nil {
		return fmt.Errorf("failed to apply manifests: %v", err)
	}

	return nil
}

// getStaleProxies get proxies still pointing to the target control plane revision.
func getStaleProxies(uiArgs *uninstallArgs) ([]string, error) {
	return proxyinfo.GetIDsFromProxyInfo(uiArgs.kubeConfigPath, uiArgs.context, uiArgs.revision, uiArgs.istioNamespace)
}

// pruneManifests prunes manifests in the cluster that belongs to the target control plane revision.
func pruneManifests(revision string, dryRun bool, kubeConfigPath string, context string, l clog.Logger) error {
	restConfig, _, client, err := K8sConfig(kubeConfigPath, context)
	if err != nil {
		return err
	}

	cache.FlushObjectCaches()
	opts := &helmreconciler.Options{DryRun: dryRun, Log: l, ProgressLog: progress.NewLog()}
	h, err := helmreconciler.NewHelmReconciler(client, restConfig, nil, opts)
	if err != nil {
		return fmt.Errorf("failed to create reconciler: %v", err)
	}
	if err := h.PruneControlPlaneByRevision(revision); err != nil {
		return fmt.Errorf("failed to prune control plane by revision: %v", err)
	}
	opts.ProgressLog.SetState(progress.StateComplete)

	// TODO(richardwxn): remove installed state CR
	return nil
}
