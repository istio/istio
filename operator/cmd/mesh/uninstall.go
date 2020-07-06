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
	"k8s.io/client-go/rest"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/name"
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
	// filename is the path of input IstioOperator CR.
	filename string
}

func addUninstallFlags(cmd *cobra.Command, args *uninstallArgs) {
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config.")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use.")
	cmd.PersistentFlags().BoolVarP(&args.skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.revision, "revision", "r", "", revisionFlagHelpStr)
	cmd.PersistentFlags().StringVar(&args.istioNamespace, "istioNamespace", istioDefaultNamespace,
		"The namespace of Istio Control Plane")
	cmd.PersistentFlags().StringVarP(&args.filename, "fileName", "f", "",
		"The filename of the IstioOperator CR")
}

func UninstallCmd(logOpts *log.Options) *cobra.Command {
	rootArgs := &rootArgs{}
	uiArgs := &uninstallArgs{}
	uicmd := &cobra.Command{
		Use:   "uninstall --revision foo",
		Short: "uninstall the control plane by revision",
		Long:  "The uninstall command uninstall the control plane by revision and",
		Args: func(cmd *cobra.Command, args []string) error {
			if (uiArgs.revision == "" && uiArgs.filename == "") || (uiArgs.revision != "" && uiArgs.filename != "") {
				return fmt.Errorf("either one of the --revision or --filename flags needed to be set")
			}
			if len(args) > 0 {
				return fmt.Errorf("too many positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstall(cmd, rootArgs, uiArgs, logOpts)
		}}
	addUninstallFlags(uicmd, uiArgs)
	return uicmd
}

func uninstall(cmd *cobra.Command, rootArgs *rootArgs, uiArgs *uninstallArgs, logOpts *log.Options) error {
	l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
	if err := configLogs(logOpts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}
	restConfig, _, clt, err := K8sConfig(uiArgs.kubeConfigPath, uiArgs.context)
	if err != nil {
		return err
	}
	cache.FlushObjectCaches()
	opts := &helmreconciler.Options{DryRun: rootArgs.dryRun, Log: l, ProgressLog: progress.NewLog()}
	h, err := helmreconciler.NewHelmReconciler(clt, restConfig, nil, opts)
	if err != nil {
		return fmt.Errorf("failed to create reconciler: %v", err)
	}
	if err := uninstallByManifest(cmd, rootArgs, uiArgs, restConfig, h, l); err != nil {
		return fmt.Errorf("failed to uninstall control plane: %v", err)
	}
	opts.ProgressLog.SetState(progress.StateComplete)
	return nil
}

// TODO: Do we still need this part? uninstallByRev uninstalls control plane belongs to the target revision.
//func uninstallByRev(cmd *cobra.Command, rootArgs *rootArgs, uiArgs *uninstallArgs, h *helmreconciler.HelmReconciler) error {
//	if err := checkStaleProxies(cmd, rootArgs, uiArgs, uiArgs.revision); err != nil {
//		return err
//	}
//
//	if err := h.PruneControlPlaneByRevision(uiArgs.revision); err != nil {
//		return fmt.Errorf("failed to prune control plane by revision: %v", err)
//	}
//	return nil
//}

// uninstallByManifest uninstalls control plane by target manifests and revision.
func uninstallByManifest(cmd *cobra.Command, rootArgs *rootArgs, uiArgs *uninstallArgs,
	restConfig *rest.Config, h *helmreconciler.HelmReconciler, l clog.Logger) error {
	var (
		manifestMap name.ManifestMap
		err         error
		iop         *v1alpha1.IstioOperatorSpec
	)
	if uiArgs.revision != "" {
		setFlag := fmt.Sprintf("revision=%s", uiArgs.revision)
		manifestMap, iop, err = GenManifests([]string{}, []string{setFlag}, true, restConfig, l)
	} else if uiArgs.filename != "" {
		manifestMap, iop, err = GenManifests([]string{uiArgs.filename}, []string{}, true, restConfig, l)
	}
	if err != nil {
		return err
	}
	if iop.Revision == "" {
		return fmt.Errorf("revision is not expecified")
	}
	if err := checkStaleProxies(cmd, rootArgs, uiArgs, iop.Revision); err != nil {
		return err
	}

	cpManifests := manifestMap[name.PilotComponentName]
	if err := h.PruneControlPlaneByManifests(strings.Join(cpManifests, "---"), iop.Revision); err != nil {
		return fmt.Errorf("failed to prune control plane by manifests: %v", err)
	}
	return nil
}

// checkStaleProxies checks proxies still pointing to the target control plane revision.
func checkStaleProxies(cmd *cobra.Command, rootArgs *rootArgs, uiArgs *uninstallArgs, rev string) error {
	pids, err := proxyinfo.GetIDsFromProxyInfo(uiArgs.kubeConfigPath, uiArgs.context, rev, uiArgs.istioNamespace)
	if err != nil {
		return err
	}
	if len(pids) != 0 && !rootArgs.dryRun && !uiArgs.skipConfirmation {
		if !confirm(fmt.Sprintf("There are still proxies pointing to the control plane revision %s:\n%s."+
			" If you proceed with the uninstall, these proxies will become detached from any control plane"+
			" and will not function correctly.. Proceed? (y/N)",
			uiArgs.revision, strings.Join(pids, " \n")), cmd.OutOrStdout()) {
			cmd.Print("Cancelled.\n")
			os.Exit(1)
		}
	}
	return nil
}
