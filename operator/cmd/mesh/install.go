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
	"time"

	"github.com/spf13/cobra"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/istioctl/pkg/install/k8sversion"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	// installedSpecCRPrefix is the prefix of any IstioOperator CR stored in the cluster that is a copy of the CR used
	// in the last install operation.
	installedSpecCRPrefix = "installed-state"
)

type installArgs struct {
	// inFilenames is an array of paths to the input IstioOperator CR files.
	inFilenames []string
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config
	context string
	// readinessTimeout is maximum time to wait for all Istio resources to be ready. wait must be true for this setting
	// to take effect.
	readinessTimeout time.Duration
	// skipConfirmation determines whether the user is prompted for confirmation.
	// If set to true, the user is not prompted and a Yes response is assumed in all cases.
	skipConfirmation bool
	// force proceeds even if there are validation errors
	force bool
	// set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	set []string
	// manifestsPath is a path to a manifestsPath and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
	// revision is the Istio control plane revision the command targets.
	revision string
}

func addInstallFlags(cmd *cobra.Command, args *installArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.inFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", "Path to kube config.")
	cmd.PersistentFlags().StringVar(&args.context, "context", "", "The name of the kubeconfig context to use.")
	cmd.PersistentFlags().DurationVar(&args.readinessTimeout, "readiness-timeout", 300*time.Second,
		"Maximum time to wait for Istio resources in each component to be ready.")
	cmd.PersistentFlags().BoolVarP(&args.skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, "Proceed even with validation errors.")
	cmd.PersistentFlags().StringArrayVarP(&args.set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.revision, "revision", "r", "", revisionFlagHelpStr)
}

// InstallCmd generates an Istio install manifest and applies it to a cluster
func InstallCmd(logOpts *log.Options) *cobra.Command {
	rootArgs := &rootArgs{}
	iArgs := &installArgs{}

	ic := &cobra.Command{
		Use:   "install",
		Short: "Applies an Istio manifest, installing or reconfiguring Istio on a cluster.",
		Long:  "The install generates an Istio install manifest and applies it to a cluster.",
		// nolint: lll
		Example: `  # Apply a default Istio installation
  istioctl install

  # Enable Tracing
  istioctl install --set meshConfig.enableTracing=true

  # Generate the demo profile and don't wait for confirmation
  istioctl install --set profile=demo --skip-confirmation

  # To override a setting that includes dots, escape them with a backslash (\).  Your shell may require enclosing quotes.
  istioctl install --set "values.sidecarInjectorWebhook.injectedAnnotations.container\.apparmor\.security\.beta\.kubernetes\.io/istio-proxy=runtime/default"
`,
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApplyCmd(cmd, rootArgs, iArgs, logOpts)
		}}

	addFlags(ic, rootArgs)
	addInstallFlags(ic, iArgs)
	return ic
}

func runApplyCmd(cmd *cobra.Command, rootArgs *rootArgs, iArgs *installArgs, logOpts *log.Options) error {
	l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
	// Warn users if they use `istioctl install` without any config args.
	if len(iArgs.inFilenames) == 0 && len(iArgs.set) == 0 && !rootArgs.dryRun && !iArgs.skipConfirmation {
		if !confirm("This will install the default Istio profile into the cluster. Proceed? (y/N)", cmd.OutOrStdout()) {
			cmd.Print("Cancelled.\n")
			os.Exit(1)
		}
	}
	if err := configLogs(logOpts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}
	if err := InstallManifests(applyFlagAliases(iArgs.set, iArgs.manifestsPath, iArgs.revision), iArgs.inFilenames, iArgs.force, rootArgs.dryRun,
		iArgs.kubeConfigPath, iArgs.context, iArgs.readinessTimeout, l); err != nil {
		return fmt.Errorf("failed to install manifests: %v", err)
	}

	return nil
}

// InstallManifests generates manifests from the given input files and --set flag overlays and applies them to the
// cluster. See GenManifests for more description of the manifest generation process.
//  force   validation warnings are written to logger but command is not aborted
//  dryRun  all operations are done but nothing is written
func InstallManifests(setOverlay []string, inFilenames []string, force bool, dryRun bool,
	kubeConfigPath string, context string, waitTimeout time.Duration, l clog.Logger) error {

	restConfig, clientset, client, err := K8sConfig(kubeConfigPath, context)
	if err != nil {
		return err
	}

	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("error getting Kubernetes version: %w", err)
	}
	ok, err := k8sversion.CheckKubernetesVersion(serverVersion)
	if err != nil {
		return fmt.Errorf("error checking if Kubernetes version is supported: %w", err)
	}
	if !ok {
		l.LogAndPrintf("\nThe Kubernetes version %s is not supported by Istio %s. The minimum supported Kubernetes version is %s.\n"+
			"Proceeding with the installation, but you might experience problems. "+
			"See https://istio.io/latest/docs/setup/platform-setup/ for a list of supported versions.\n",
			serverVersion.GitVersion, version.Info.Version, k8sversion.MinK8SVersion)
	}

	_, iops, err := manifest.GenerateConfig(inFilenames, setOverlay, force, restConfig, l)
	if err != nil {
		return err
	}

	crName := installedSpecCRPrefix
	if iops.Revision != "" {
		crName += "-" + iops.Revision
	}
	iop, err := translate.IOPStoIOP(iops, crName, iopv1alpha1.Namespace(iops))
	if err != nil {
		return err
	}

	if err := createNamespace(clientset, iop.Namespace); err != nil {
		return err
	}

	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	opts := &helmreconciler.Options{DryRun: dryRun, Log: l, WaitTimeout: waitTimeout, ProgressLog: progress.NewLog(),
		Force: force}
	reconciler, err := helmreconciler.NewHelmReconciler(client, restConfig, iop, opts)
	if err != nil {
		return err
	}
	status, err := reconciler.Reconcile()
	if err != nil {
		return fmt.Errorf("errors occurred during operation")
	}
	if status.Status != v1alpha1.InstallStatus_HEALTHY {
		return fmt.Errorf("errors occurred during operation")
	}

	opts.ProgressLog.SetState(progress.StateComplete)

	// Save state to cluster in IstioOperator CR.
	iopStr, err := translate.IOPStoIOPstr(iops, crName, iopv1alpha1.Namespace(iops))
	if err != nil {
		return err
	}

	return saveIOPToCluster(reconciler, iopStr)
}
