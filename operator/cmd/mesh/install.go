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
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/istioctl/pkg/verifier"
	v1alpha12 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/controller/istiocontrolplane"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	pkgversion "istio.io/istio/operator/pkg/version"
	operatorVer "istio.io/istio/operator/version"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
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
	// verify after installation
	verify bool
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
	cmd.PersistentFlags().StringVarP(&args.kubeConfigPath, "kubeconfig", "c", "", KubeConfigFlagHelpStr)
	cmd.PersistentFlags().StringVar(&args.context, "context", "", ContextFlagHelpStr)
	cmd.PersistentFlags().DurationVar(&args.readinessTimeout, "readiness-timeout", 300*time.Second,
		"Maximum time to wait for Istio resources in each component to be ready.")
	cmd.PersistentFlags().BoolVarP(&args.skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.force, "force", false, ForceFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.verify, "verify", false, VerifyCRInstallHelpStr)
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
		Use:     "install",
		Short:   "Applies an Istio manifest, installing or reconfiguring Istio on a cluster.",
		Long:    "The install command generates an Istio install manifest and applies it to a cluster.",
		Aliases: []string{"apply"},
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
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if !labels.IsDNS1123Label(iArgs.revision) && cmd.PersistentFlags().Changed("revision") {
				return fmt.Errorf("invalid revision specified: %v", iArgs.revision)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApplyCmd(cmd, rootArgs, iArgs, logOpts)
		},
	}

	addFlags(ic, rootArgs)
	addInstallFlags(ic, iArgs)
	return ic
}

func runApplyCmd(cmd *cobra.Command, rootArgs *rootArgs, iArgs *installArgs, logOpts *log.Options) error {
	l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
	var opts clioptions.ControlPlaneOptions
	kubeClient, err := kube.NewExtendedClient(kube.BuildClientCmd(iArgs.kubeConfigPath, iArgs.context), opts.Revision)
	if err != nil {
		return err
	}
	restConfig, clientset, client, err := K8sConfig(iArgs.kubeConfigPath, iArgs.context)
	if err != nil {
		return err
	}
	if err := k8sversion.IsK8VersionSupported(clientset, l); err != nil {
		return err
	}
	tag, err := GetTagVersion(operatorVer.OperatorVersionString)
	if err != nil {
		return err
	}
	setFlags := applyFlagAliases(iArgs.set, iArgs.manifestsPath, iArgs.revision)

	_, iop, err := manifest.GenerateConfig(iArgs.inFilenames, setFlags, iArgs.force, restConfig, l)
	if err != nil {
		return err
	}

	profile, ns, enabledComponents, err := getProfileNSAndEnabledComponents(iop)
	if err != nil {
		return fmt.Errorf("failed to get profile, namespace or enabled components: %v", err)
	}

	// Ignore the err because we don't want to show
	// "no running Istio pods in istio-system" for the first time
	_ = DetectIstioVersionDiff(cmd, tag, ns, kubeClient, setFlags)

	// Warn users if they use `istioctl install` without any config args.
	if !rootArgs.dryRun && !iArgs.skipConfirmation {
		prompt := fmt.Sprintf("This will install the Istio %s %s profile with %q components into the cluster. Proceed? (y/N)", tag, profile, enabledComponents)
		if profile == "empty" {
			prompt = fmt.Sprintf("This will install the Istio %s %s profile into the cluster. Proceed? (y/N)", tag, profile)
		}
		if !confirm(prompt, cmd.OutOrStdout()) {
			cmd.Print("Cancelled.\n")
			os.Exit(1)
		}
	}
	if err := configLogs(logOpts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}
	iop, err = InstallManifests(iop, iArgs.force, rootArgs.dryRun, restConfig, client, iArgs.readinessTimeout, l)
	if err != nil {
		return fmt.Errorf("failed to install manifests: %v", err)
	}

	if iArgs.verify {
		if rootArgs.dryRun {
			l.LogAndPrint("Control plane health check is not applicable in dry-run mode")
			return nil
		}
		l.LogAndPrint("\n\nVerifying installation:")
		installationVerifier := verifier.NewStatusVerifier(iop.Namespace, iArgs.manifestsPath, iArgs.kubeConfigPath,
			iArgs.context, iArgs.inFilenames, clioptions.ControlPlaneOptions{Revision: iop.Spec.Revision}, l, iop)
		if err := installationVerifier.Verify(); err != nil {
			return fmt.Errorf("verification failed with the following error: %v", err)
		}
	}

	return nil
}

// InstallManifests generates manifests from the given istiooperator instance and applies them to the
// cluster. See GenManifests for more description of the manifest generation process.
//  force   validation warnings are written to logger but command is not aborted
//  dryRun  all operations are done but nothing is written
// Returns final IstioOperator after installation if successful.
func InstallManifests(iop *v1alpha12.IstioOperator, force bool, dryRun bool, restConfig *rest.Config, client client.Client,
	waitTimeout time.Duration, l clog.Logger) (*v1alpha12.IstioOperator, error) {
	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	opts := &helmreconciler.Options{
		DryRun: dryRun, Log: l, WaitTimeout: waitTimeout, ProgressLog: progress.NewLog(),
		Force: force,
	}
	reconciler, err := helmreconciler.NewHelmReconciler(client, restConfig, iop, opts)
	if err != nil {
		return iop, err
	}
	status, err := reconciler.Reconcile()
	if err != nil {
		return iop, fmt.Errorf("errors occurred during operation: %v", err)
	}
	if status.Status != v1alpha1.InstallStatus_HEALTHY {
		return iop, fmt.Errorf("errors occurred during operation")
	}

	opts.ProgressLog.SetState(progress.StateComplete)

	// Save a copy of what was installed as a CR in the cluster under an internal name.
	iop.Name = savedIOPName(iop)
	if iop.Annotations == nil {
		iop.Annotations = make(map[string]string)
	}
	iop.Annotations[istiocontrolplane.IgnoreReconcileAnnotation] = "true"
	iopStr, err := util.MarshalWithJSONPB(iop)
	if err != nil {
		return iop, err
	}

	return iop, saveIOPToCluster(reconciler, iopStr)
}

func savedIOPName(iop *v1alpha12.IstioOperator) string {
	ret := name.InstalledSpecCRPrefix
	if iop.Name != "" {
		ret += "-" + iop.Name
	}
	if iop.Spec.Revision != "" {
		ret += "-" + iop.Spec.Revision
	}
	return ret
}

// DetectIstioVersionDiff will show warning if istioctl version and control plane version are different
// nolint: interfacer
func DetectIstioVersionDiff(cmd *cobra.Command, tag string, ns string, kubeClient kube.ExtendedClient, setFlags []string) error {
	icps, err := kubeClient.GetIstioVersions(context.TODO(), ns)
	if err != nil {
		return err
	}
	if len(*icps) != 0 {
		var icpTags []string
		var icpTag string
		// create normalized tags for multiple control plane revisions
		for _, icp := range *icps {
			tagVer, err := GetTagVersion(icp.Info.GitTag)
			if err != nil {
				return err
			}
			icpTags = append(icpTags, tagVer)
		}
		// sort different versions of control plane revsions
		sort.Strings(icpTags)
		// capture latest revision installed for comparison
		for _, val := range icpTags {
			if val != "" {
				icpTag = val
			}
		}
		revision := manifest.GetValueForSetFlag(setFlags, "revision")
		msg := ""
		// when the revision is not passed and if the ns has a prior istio
		if revision == "" && tag != icpTag {
			if icpTag < tag {
				msg = "A newer"
			} else {
				msg = "An older"
			}
			cmd.Printf("! Istio control planes installed: %s.\n"+
				"! "+msg+" installed version of Istio has been detected. Running this command will overwrite it.\n", strings.Join(icpTags, ", "))
		}
		// when the revision is passed
		if icpTag != "" && tag != icpTag && revision != "" {
			if icpTag > tag {
				cmd.Printf("! Istio is being upgraded from %s -> %s.\n"+
					"! Before upgrading, you may wish to use 'istioctl analyze' to check for IST0002 and IST0135 deprecation warnings.\n", icpTag, tag)
			} else {
				cmd.Printf("! Istio is being downgraded from %s -> %s.", icpTag, tag)
			}
		}
	}
	return nil
}

// GetTagVersion returns istio tag version
func GetTagVersion(tagInfo string) (string, error) {
	if pkgversion.IsVersionString(tagInfo) {
		tagInfo = pkgversion.TagToVersionStringGrace(tagInfo)
	}
	tag, err := pkgversion.NewVersionFromString(tagInfo)
	if err != nil {
		return "", err
	}
	return tag.String(), nil
}

// getProfileNSAndEnabledComponents get the profile and all the enabled components
// from the given input files and --set flag overlays.
func getProfileNSAndEnabledComponents(iop *v1alpha12.IstioOperator) (string, string, []string, error) {
	var enabledComponents []string
	if iop.Spec.Components != nil {
		for _, c := range name.AllCoreComponentNames {
			enabled, err := translate.IsComponentEnabledInSpec(c, iop.Spec)
			if err != nil {
				return "", "", nil, fmt.Errorf("failed to check if component: %s is enabled or not: %v", string(c), err)
			}
			if enabled {
				enabledComponents = append(enabledComponents, name.UserFacingComponentName(c))
			}
		}
		for _, c := range iop.Spec.Components.IngressGateways {
			if c.Enabled.Value {
				enabledComponents = append(enabledComponents, name.UserFacingComponentName(name.IngressComponentName))
				break
			}
		}
		for _, c := range iop.Spec.Components.EgressGateways {
			if c.Enabled.Value {
				enabledComponents = append(enabledComponents, name.UserFacingComponentName(name.EgressComponentName))
				break
			}
		}
	}

	if configuredNamespace := v1alpha12.Namespace(iop.Spec); configuredNamespace != "" {
		return iop.Spec.Profile, configuredNamespace, enabledComponents, nil
	}
	return iop.Spec.Profile, name.IstioDefaultNamespace, enabledComponents, nil
}
