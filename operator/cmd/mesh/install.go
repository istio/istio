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
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/istioctl/pkg/clioptions"
	revtag "istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/istioctl/pkg/verifier"
	v1alpha12 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/controller/istiocontrolplane"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	pkgversion "istio.io/istio/operator/pkg/version"
	operatorVer "istio.io/istio/operator/version"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

type InstallArgs struct {
	// InFilenames is an array of paths to the input IstioOperator CR files.
	InFilenames []string
	// KubeConfigPath is the path to kube config file.
	KubeConfigPath string
	// Context is the cluster context in the kube config
	Context string
	// ReadinessTimeout is maximum time to wait for all Istio resources to be ready. wait must be true for this setting
	// to take effect.
	ReadinessTimeout time.Duration
	// SkipConfirmation determines whether the user is prompted for confirmation.
	// If set to true, the user is not prompted and a Yes response is assumed in all cases.
	SkipConfirmation bool
	// Force proceeds even if there are validation errors
	Force bool
	// Verify after installation
	Verify bool
	// Set is a string with element format "path=value" where path is an IstioOperator path and the value is a
	// value to set the node at that path to.
	Set []string
	// ManifestsPath is a path to a ManifestsPath and profiles directory in the local filesystem, or URL with a release tgz.
	ManifestsPath string
	// Revision is the Istio control plane revision the command targets.
	Revision string
}

func (a *InstallArgs) String() string {
	var b strings.Builder
	b.WriteString("InFilenames:      " + fmt.Sprint(a.InFilenames) + "\n")
	b.WriteString("KubeConfigPath:   " + a.KubeConfigPath + "\n")
	b.WriteString("Context:          " + a.Context + "\n")
	b.WriteString("ReadinessTimeout: " + fmt.Sprint(a.ReadinessTimeout) + "\n")
	b.WriteString("SkipConfirmation: " + fmt.Sprint(a.SkipConfirmation) + "\n")
	b.WriteString("Force:            " + fmt.Sprint(a.Force) + "\n")
	b.WriteString("Verify:           " + fmt.Sprint(a.Verify) + "\n")
	b.WriteString("Set:              " + fmt.Sprint(a.Set) + "\n")
	b.WriteString("ManifestsPath:    " + a.ManifestsPath + "\n")
	b.WriteString("Revision:         " + a.Revision + "\n")
	return b.String()
}

func addInstallFlags(cmd *cobra.Command, args *InstallArgs) {
	cmd.PersistentFlags().StringSliceVarP(&args.InFilenames, "filename", "f", nil, filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.KubeConfigPath, "kubeconfig", "c", "", KubeConfigFlagHelpStr)
	cmd.PersistentFlags().StringVar(&args.Context, "context", "", ContextFlagHelpStr)
	cmd.PersistentFlags().DurationVar(&args.ReadinessTimeout, "readiness-timeout", 300*time.Second,
		"Maximum time to wait for Istio resources in each component to be ready.")
	cmd.PersistentFlags().BoolVarP(&args.SkipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.Force, "force", false, ForceFlagHelpStr)
	cmd.PersistentFlags().BoolVar(&args.Verify, "verify", false, VerifyCRInstallHelpStr)
	cmd.PersistentFlags().StringArrayVarP(&args.Set, "set", "s", nil, setFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.ManifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.ManifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.Revision, "revision", "r", "", revisionFlagHelpStr)
}

// InstallCmdWithArgs generates an Istio install manifest and applies it to a cluster
func InstallCmdWithArgs(rootArgs *RootArgs, iArgs *InstallArgs, logOpts *log.Options) *cobra.Command {
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

  # For setting boolean-string option, it should be enclosed quotes and escaped with a backslash (\).
  istioctl install --set meshConfig.defaultConfig.proxyMetadata.PROXY_XDS_VIA_AGENT=\"false\"
`,
		Args: cobra.ExactArgs(0),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if !labels.IsDNS1123Label(iArgs.Revision) && cmd.PersistentFlags().Changed("revision") {
				return fmt.Errorf("invalid revision specified: %v", iArgs.Revision)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			p := NewPrinterForWriter(cmd.OutOrStderr())
			return Install(rootArgs, iArgs, logOpts, cmd.OutOrStdout(), l, p)
		},
	}

	addFlags(ic, rootArgs)
	addInstallFlags(ic, iArgs)
	return ic
}

// InstallCmd generates an Istio install manifest and applies it to a cluster
func InstallCmd(logOpts *log.Options) *cobra.Command {
	return InstallCmdWithArgs(&RootArgs{}, &InstallArgs{}, logOpts)
}

func Install(rootArgs *RootArgs, iArgs *InstallArgs, logOpts *log.Options, stdOut io.Writer, l clog.Logger, p Printer) error {
	kubeClient, client, err := KubernetesClients(iArgs.KubeConfigPath, iArgs.Context, l)
	if err != nil {
		return err
	}

	tag, err := GetTagVersion(operatorVer.OperatorVersionString)
	if err != nil {
		return fmt.Errorf("fetch Istio version: %v", err)
	}
	setFlags := applyFlagAliases(iArgs.Set, iArgs.ManifestsPath, iArgs.Revision)

	_, iop, err := manifest.GenerateConfig(iArgs.InFilenames, setFlags, iArgs.Force, kubeClient, l)
	if err != nil {
		return fmt.Errorf("generate config: %v", err)
	}

	profile, ns, enabledComponents, err := getProfileNSAndEnabledComponents(iop)
	if err != nil {
		return fmt.Errorf("failed to get profile, namespace or enabled components: %v", err)
	}

	// Ignore the err because we don't want to show
	// "no running Istio pods in istio-system" for the first time
	_ = detectIstioVersionDiff(p, tag, ns, kubeClient, setFlags)

	// Warn users if they use `istioctl install` without any config args.
	if !rootArgs.DryRun && !iArgs.SkipConfirmation {
		prompt := fmt.Sprintf("This will install the Istio %s %s profile with %q components into the cluster. Proceed? (y/N)", tag, profile, enabledComponents)
		if profile == "empty" {
			prompt = fmt.Sprintf("This will install the Istio %s %s profile into the cluster. Proceed? (y/N)", tag, profile)
		}
		if !confirm(prompt, stdOut) {
			p.Println("Cancelled.")
			os.Exit(1)
		}
	}
	if err := configLogs(logOpts); err != nil {
		return fmt.Errorf("could not configure logs: %s", err)
	}

	// Detect whether previous installation exists prior to performing the installation.
	exists := revtag.PreviousInstallExists(context.Background(), kubeClient.Kube())
	pilotEnabled := iop.Spec.Components.Pilot != nil && iop.Spec.Components.Pilot.Enabled.Value
	rev := iop.Spec.Revision
	if rev == "" && pilotEnabled {
		_ = revtag.DeleteTagWebhooks(context.Background(), kubeClient.Kube(), revtag.DefaultRevisionName)
	}
	iop, err = InstallManifests(iop, iArgs.Force, rootArgs.DryRun, kubeClient, client, iArgs.ReadinessTimeout, l)
	if err != nil {
		return fmt.Errorf("failed to install manifests: %v", err)
	}

	if !exists || rev == "" && pilotEnabled {
		p.Println("Making this installation the default for injection and validation.")
		if rev == "" {
			rev = revtag.DefaultRevisionName
		}
		autoInjectNamespaces := validateEnableNamespacesByDefault(iop)

		o := &revtag.GenerateOptions{
			Tag:                  revtag.DefaultRevisionName,
			Revision:             rev,
			Overwrite:            true,
			AutoInjectNamespaces: autoInjectNamespaces,
		}
		// If tag cannot be created could be remote cluster install, don't fail out.
		tagManifests, err := revtag.Generate(context.Background(), kubeClient, o, ns)
		if err == nil {
			err = revtag.Create(kubeClient, tagManifests)
			if err != nil {
				return err
			}
		}
	}

	if iArgs.Verify {
		if rootArgs.DryRun {
			l.LogAndPrint("Control plane health check is not applicable in dry-run mode")
			return nil
		}
		l.LogAndPrint("\n\nVerifying installation:")
		installationVerifier, err := verifier.NewStatusVerifier(iop.Namespace, iArgs.ManifestsPath, iArgs.KubeConfigPath,
			iArgs.Context, iArgs.InFilenames, clioptions.ControlPlaneOptions{Revision: iop.Spec.Revision},
			verifier.WithLogger(l),
			verifier.WithIOP(iop),
		)
		if err != nil {
			return fmt.Errorf("failed to setup verifier: %v", err)
		}
		if err := installationVerifier.Verify(); err != nil {
			return fmt.Errorf("verification failed with the following error: %v", err)
		}
	}

	return nil
}

// InstallManifests generates manifests from the given istiooperator instance and applies them to the
// cluster. See GenManifests for more description of the manifest generation process.
//  force   validation warnings are written to logger but command is not aborted
//  DryRun  all operations are done but nothing is written
// Returns final IstioOperator after installation if successful.
func InstallManifests(iop *v1alpha12.IstioOperator, force bool, dryRun bool, kubeClient kube.Client, client client.Client,
	waitTimeout time.Duration, l clog.Logger) (*v1alpha12.IstioOperator, error) {
	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	opts := &helmreconciler.Options{
		DryRun: dryRun, Log: l, WaitTimeout: waitTimeout, ProgressLog: progress.NewLog(),
		Force: force,
	}
	reconciler, err := helmreconciler.NewHelmReconciler(client, kubeClient, iop, opts)
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
	iopStr, err := yaml.Marshal(iop)
	if err != nil {
		return iop, err
	}

	return iop, saveIOPToCluster(reconciler, string(iopStr))
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

// detectIstioVersionDiff will show warning if istioctl version and control plane version are different
// nolint: interfacer
func detectIstioVersionDiff(p Printer, tag string, ns string, kubeClient kube.ExtendedClient, setFlags []string) error {
	warnMarker := color.New(color.FgYellow).Add(color.Italic).Sprint("WARNING:")
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
			p.Printf("%s Istio control planes installed: %s.\n"+
				"%s "+msg+" installed version of Istio has been detected. Running this command will overwrite it.\n", warnMarker, strings.Join(icpTags, ", "), warnMarker)
		}
		// when the revision is passed
		if icpTag != "" && tag != icpTag && revision != "" {
			if icpTag < tag {
				p.Printf("%s Istio is being upgraded from %s -> %s.\n"+
					"%s Before upgrading, you may wish to use 'istioctl analyze' to check for "+
					"IST0002 and IST0135 deprecation warnings.\n", warnMarker, icpTag, tag, warnMarker)
			} else {
				p.Printf("%s Istio is being downgraded from %s -> %s.\n", warnMarker, icpTag, tag)
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
			if c.Enabled.GetValue() {
				enabledComponents = append(enabledComponents, name.UserFacingComponentName(name.IngressComponentName))
				break
			}
		}
		for _, c := range iop.Spec.Components.EgressGateways {
			if c.Enabled.GetValue() {
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

// validateEnableNamespacesByDefault checks whether there is .Values.sidecarInjectorWebhook.enableNamespacesByDefault set in the Istio Operator.
// Should be used in installer when deciding whether to enable an automatic sidecar injection in all namespaces.
func validateEnableNamespacesByDefault(iop *v1alpha12.IstioOperator) bool {
	if iop == nil || iop.Spec == nil || iop.Spec.Values == nil {
		return false
	}
	sidecarValues := iop.Spec.Values.AsMap()["sidecarInjectorWebhook"]
	sidecarMap, ok := sidecarValues.(map[string]interface{})
	if !ok {
		return false
	}
	autoInjectNamespaces, ok := sidecarMap["enableNamespacesByDefault"].(bool)
	if !ok {
		return false
	}

	return autoInjectNamespaces
}
