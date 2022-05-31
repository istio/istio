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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/translate"
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
	// force proceeds even if there are validation errors
	force bool
	// purge results in deletion of all Istio resources.
	purge bool
	// revision is the Istio control plane revision the command targets.
	revision string
	// istioNamespace is the target namespace of istio control plane.
	istioNamespace string
	// filename is the path of input IstioOperator CR.
	filename string
	// set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	set []string
	// manifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
	// verbose generates verbose output.
	verbose bool
}

const (
	AllResourcesRemovedWarning = "All Istio resources will be pruned from the cluster\n"
	NoResourcesRemovedWarning  = "No resources will be pruned from the cluster. Please double check the input configs\n"
	GatewaysRemovedWarning     = "You are about to remove the following gateways: %s." +
		" To avoid downtime, please quit this command and reinstall the gateway(s) with a revision that is not being removed from the cluster.\n"
	PurgeWithRevisionOrOperatorSpecifiedWarning = "Purge uninstall will remove all Istio resources, ignoring the specified revision or operator file"
)

func addUninstallFlags(cmd *cobra.Command, args *uninstallArgs) {
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", KubeConfigFlagHelpStr)
	cmd.PersistentFlags().StringVar(&args.context, "context", "", ContextFlagHelpStr)
	cmd.PersistentFlags().BoolVarP(&args.skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, ForceFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.purge, "purge", false, "Delete all Istio related sources for all versions")
	cmd.PersistentFlags().StringVarP(&args.revision, "revision", "r", "", revisionFlagHelpStr)
	cmd.PersistentFlags().StringVar(&args.istioNamespace, "istioNamespace", istioDefaultNamespace,
		"The namespace of Istio Control Plane.")
	cmd.PersistentFlags().StringVarP(&args.filename, "filename", "f", "",
		"The filename of the IstioOperator CR.")
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringArrayVarP(&args.set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().BoolVarP(&args.verbose, "verbose", "v", false, "Verbose output.")
}

// UninstallCmd command uninstalls Istio from a cluster
func UninstallCmd(logOpts *log.Options) *cobra.Command {
	rootArgs := &RootArgs{}
	uiArgs := &uninstallArgs{}
	uicmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Istio from a cluster",
		Long:  "The uninstall command uninstalls Istio from a cluster",
		Example: `  # Uninstall a single control plane by revision
  istioctl x uninstall --revision foo

  # Uninstall a single control plane by iop file
  istioctl x uninstall -f iop.yaml
  
  # Uninstall all control planes and shared resources
  istioctl x uninstall --purge`,
		Args: func(cmd *cobra.Command, args []string) error {
			if uiArgs.revision == "" && manifest.GetValueForSetFlag(uiArgs.set, "revision") == "" && uiArgs.filename == "" && !uiArgs.purge {
				return fmt.Errorf("at least one of the --revision (or --set revision=<revision>), --filename or --purge flags must be set")
			}
			if len(args) > 0 {
				return fmt.Errorf("istioctl uninstall does not take arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return uninstall(cmd, rootArgs, uiArgs, logOpts)
		},
	}
	addFlags(uicmd, rootArgs)
	addUninstallFlags(uicmd, uiArgs)
	return uicmd
}

// uninstall uninstalls control plane by either pruning by target revision or deleting specified manifests.
func uninstall(cmd *cobra.Command, rootArgs *RootArgs, uiArgs *uninstallArgs, logOpts *log.Options) error {
	l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
	if err := configLogs(logOpts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}
	kubeClient, client, err := KubernetesClients(uiArgs.kubeConfigPath, uiArgs.context, l)
	if err != nil {
		l.LogAndFatal(err)
	}
	cache.FlushObjectCaches()
	opts := &helmreconciler.Options{DryRun: rootArgs.DryRun, Log: l, ProgressLog: progress.NewLog()}
	var h *helmreconciler.HelmReconciler

	// If the user is performing a purge install but also specified a revision or filename, we should warn
	// that the purge will still remove all resources
	if uiArgs.purge && (uiArgs.revision != "" || uiArgs.filename != "") {
		l.LogAndPrint(PurgeWithRevisionOrOperatorSpecifiedWarning)
	}
	// If only revision flag is set, we would prune resources by the revision label.
	// Otherwise we would merge the revision flag and the filename flag and delete resources by generated manifests.
	if uiArgs.filename == "" {
		emptyiops := &v1alpha1.IstioOperatorSpec{Profile: "empty", Revision: uiArgs.revision}
		iop, err := translate.IOPStoIOP(emptyiops, "empty", iopv1alpha1.Namespace(emptyiops))
		if err != nil {
			return err
		}
		h, err := helmreconciler.NewHelmReconciler(client, kubeClient, iop, opts)
		if err != nil {
			return fmt.Errorf("failed to create reconciler: %v", err)
		}
		objectsList, err := h.GetPrunedResources(uiArgs.revision, uiArgs.purge, "")
		if err != nil {
			return err
		}
		preCheckWarnings(cmd, uiArgs, uiArgs.revision, objectsList, nil, l)

		if err := h.DeleteObjectsList(objectsList, ""); err != nil {
			return fmt.Errorf("failed to delete control plane resources by revision: %v", err)
		}
		opts.ProgressLog.SetState(progress.StateUninstallComplete)
		return nil
	}
	manifestMap, iop, err := manifest.GenManifests([]string{uiArgs.filename},
		applyFlagAliases(uiArgs.set, uiArgs.manifestsPath, uiArgs.revision), uiArgs.force, nil, kubeClient, l)
	if err != nil {
		return err
	}
	cpManifest := manifestMap[name.PilotComponentName]
	cpObjects, err := object.ParseK8sObjectsFromYAMLManifest(strings.Join(cpManifest, object.YAMLSeparator))
	if err != nil {
		return err
	}
	preCheckWarnings(cmd, uiArgs, iop.Spec.Revision, nil, cpObjects, l)
	h, err = helmreconciler.NewHelmReconciler(client, kubeClient, iop, opts)
	if err != nil {
		return fmt.Errorf("failed to create reconciler: %v", err)
	}
	if err := h.DeleteControlPlaneByManifests(manifestMap, iop.Spec.Revision, uiArgs.purge); err != nil {
		return fmt.Errorf("failed to delete control plane by manifests: %v", err)
	}
	opts.ProgressLog.SetState(progress.StateUninstallComplete)
	return nil
}

// preCheckWarnings checks possible breaking changes and issue warnings to users, it checks the following:
// 1. checks proxies still pointing to the target control plane revision.
// 2. lists to be pruned resources if user uninstall by --revision flag.
func preCheckWarnings(cmd *cobra.Command, uiArgs *uninstallArgs,
	rev string, resourcesList []*unstructured.UnstructuredList, objectsList object.K8sObjects, l *clog.ConsoleLogger,
) {
	pids, err := proxyinfo.GetIDsFromProxyInfo(uiArgs.kubeConfigPath, uiArgs.context, rev, uiArgs.istioNamespace)
	if err != nil {
		l.LogAndError(err.Error())
	}
	needConfirmation, message := false, ""
	if uiArgs.purge {
		needConfirmation = true
		message += AllResourcesRemovedWarning
	} else {
		rmListString, gwList := constructResourceListOutput(resourcesList, objectsList)
		if rmListString == "" {
			l.LogAndPrint(NoResourcesRemovedWarning)
			return
		}
		if uiArgs.verbose {
			message += fmt.Sprintf("The following resources will be pruned from the cluster: %s\n",
				rmListString)
		}

		if len(pids) != 0 && rev != "" {
			needConfirmation = true
			message += fmt.Sprintf("There are still %d proxies pointing to the control plane revision %s\n", len(pids), rev)
			// just print the count only if there is a large list of proxies
			if len(pids) <= 30 {
				message += fmt.Sprintf("%s\n", strings.Join(pids, "\n"))
			}
			message += "If you proceed with the uninstall, these proxies will become detached from any control plane" +
				" and will not function correctly.\n"
		}
		if gwList != "" {
			needConfirmation = true
			message += fmt.Sprintf(GatewaysRemovedWarning, gwList)
		}
	}
	if uiArgs.skipConfirmation {
		l.LogAndPrint(message)
		return
	}
	message += "Proceed? (y/N)"
	if needConfirmation && !confirm(message, cmd.OutOrStdout()) {
		cmd.Print("Cancelled.\n")
		os.Exit(1)
	}
}

// constructResourceListOutput is a helper function to construct the output of to be removed resources list
func constructResourceListOutput(resourcesList []*unstructured.UnstructuredList, objectsList object.K8sObjects) (string, string) {
	var items []unstructured.Unstructured
	if objectsList != nil {
		items = objectsList.UnstructuredItems()
	}
	for _, usList := range resourcesList {
		items = append(items, usList.Items...)
	}
	kindNameMap := make(map[string][]string)
	for _, o := range items {
		nameList := kindNameMap[o.GetKind()]
		if nameList == nil {
			kindNameMap[o.GetKind()] = []string{}
		}
		kindNameMap[o.GetKind()] = append(kindNameMap[o.GetKind()], o.GetName())
	}
	if len(kindNameMap) == 0 {
		return "", ""
	}
	output, gwlist := "", []string{}
	for kind, name := range kindNameMap {
		output += fmt.Sprintf("%s: %s. ", kind, strings.Join(name, ", "))
		if kind == "Deployment" {
			for _, n := range name {
				if strings.Contains(n, "gateway") {
					gwlist = append(gwlist, n)
				}
			}
		}
	}
	return output, strings.Join(gwlist, ", ")
}
