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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	revtag "istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/istioctl/pkg/util"
	v1alpha12 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/cache"
	"istio.io/istio/operator/pkg/helmreconciler"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/operator/pkg/verifier"
	pkgversion "istio.io/istio/operator/pkg/version"
	operatorVer "istio.io/istio/operator/version"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube"
)

type InstallArgs struct {
	// InFilenames is an array of paths to the input IstioOperator CR files.
	InFilenames []string
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
	// ManifestsPath is a path to a ManifestsPath and profiles directory in the local filesystem with a release tgz.
	ManifestsPath string
	// Revision is the Istio control plane revision the command targets.
	Revision string
}

func (a *InstallArgs) String() string {
	var b strings.Builder
	b.WriteString("InFilenames:      " + fmt.Sprint(a.InFilenames) + "\n")
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
func InstallCmdWithArgs(ctx cli.Context, rootArgs *RootArgs, iArgs *InstallArgs) *cobra.Command {
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
			if !labels.IsDNS1123Label(iArgs.Revision) && cmd.PersistentFlags().Changed("revision") {
				return fmt.Errorf("invalid revision specified: %v", iArgs.Revision)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return err
			}
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			p := NewPrinterForWriter(cmd.OutOrStderr())
			return Install(kubeClient, rootArgs, iArgs, cmd.OutOrStdout(), l, p)
		},
	}

	addFlags(ic, rootArgs)
	addInstallFlags(ic, iArgs)
	return ic
}

// InstallCmd generates an Istio install manifest and applies it to a cluster
func InstallCmd(ctx cli.Context) *cobra.Command {
	return InstallCmdWithArgs(ctx, &RootArgs{}, &InstallArgs{})
}

func Install(kubeClient kube.CLIClient, rootArgs *RootArgs, iArgs *InstallArgs, stdOut io.Writer, l clog.Logger, p Printer,
) error {
	kubeClient, client, err := KubernetesClients(kubeClient, l)
	if err != nil {
		return err
	}

	tag, err := GetTagVersion(operatorVer.OperatorVersionString)
	if err != nil {
		return fmt.Errorf("fetch Istio version: %v", err)
	}

	// return warning if current date is near the EOL date
	if operatorVer.IsEOL() {
		warnMarker := color.New(color.FgYellow).Add(color.Italic).Sprint("WARNING:")
		fmt.Printf("%s Istio %v may be out of support (EOL) already: see https://istio.io/latest/docs/releases/supported-releases/ for supported releases\n",
			warnMarker, operatorVer.OperatorCodeBaseVersion)
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
	_ = detectIstioVersionDiff(p, tag, ns, kubeClient, iop)
	exists := revtag.PreviousInstallExists(context.Background(), kubeClient.Kube())
	err = detectDefaultWebhookChange(p, kubeClient, iop, exists)
	if err != nil {
		return fmt.Errorf("failed to detect the default webhook change: %v", err)
	}

	// Warn users if they use `istioctl install` without any config args.
	if !rootArgs.DryRun && !iArgs.SkipConfirmation {
		prompt := fmt.Sprintf("This will install the Istio %s %q profile (with components: %s) into the cluster. Proceed? (y/N)",
			tag, profile, humanReadableJoin(enabledComponents))
		if !Confirm(prompt, stdOut) {
			p.Println("Cancelled.")
			os.Exit(1)
		}
	}

	iop.Name = savedIOPName(iop)

	// Detect whether previous installation exists prior to performing the installation.
	if err := InstallManifests(iop, iArgs.Force, rootArgs.DryRun, kubeClient, client, iArgs.ReadinessTimeout, l); err != nil {
		return fmt.Errorf("failed to install manifests: %v", err)
	}
	opts := &helmreconciler.ProcessDefaultWebhookOptions{
		Namespace: ns,
		DryRun:    rootArgs.DryRun,
	}
	if processed, err := helmreconciler.ProcessDefaultWebhook(kubeClient, iop, exists, opts); err != nil {
		return fmt.Errorf("failed to process default webhook: %v", err)
	} else if processed {
		p.Println("Made this installation the default for injection and validation.")
	}

	if iArgs.Verify {
		if rootArgs.DryRun {
			l.LogAndPrint("Control plane health check is not applicable in dry-run mode")
			return nil
		}
		l.LogAndPrint("\n\nVerifying installation:")
		installationVerifier, err := verifier.NewStatusVerifier(kubeClient, client, iop.Namespace, iArgs.ManifestsPath,
			iArgs.InFilenames, clioptions.ControlPlaneOptions{Revision: iop.Spec.Revision},
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
//
//	force   validation warnings are written to logger but command is not aborted
//	DryRun  all operations are done but nothing is written
//
// Returns final IstioOperator after installation if successful.
func InstallManifests(iop *v1alpha12.IstioOperator, force bool, dryRun bool, kubeClient kube.Client, client client.Client,
	waitTimeout time.Duration, l clog.Logger,
) error {
	// Needed in case we are running a test through this path that doesn't start a new process.
	cache.FlushObjectCaches()
	opts := &helmreconciler.Options{
		DryRun: dryRun, Log: l, WaitTimeout: waitTimeout, ProgressLog: progress.NewLog(),
		Force: force,
	}
	reconciler, err := helmreconciler.NewHelmReconciler(client, kubeClient, iop, opts)
	if err != nil {
		return err
	}
	status, err := reconciler.Reconcile()
	if err != nil {
		return fmt.Errorf("errors occurred during operation: %v", err)
	}
	if status.Status != v1alpha1.InstallStatus_HEALTHY {
		return fmt.Errorf("errors occurred during operation")
	}

	// Previously we may install IOP file from the old version of istioctl. Now since we won't install IOP file
	// anymore, and it didn't provide much value, we can delete it if it exists.
	reconciler.DeleteIOPInClusterIfExists(iop)

	opts.ProgressLog.SetState(progress.StateComplete)

	return nil
}

func savedIOPName(iop *v1alpha12.IstioOperator) string {
	ret := "installed-state"
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
func detectIstioVersionDiff(p Printer, tag string, ns string, kubeClient kube.CLIClient, iop *v1alpha12.IstioOperator) error {
	warnMarker := color.New(color.FgYellow).Add(color.Italic).Sprint("WARNING:")
	revision := iop.Spec.Revision
	if revision == "" {
		revision = util.DefaultRevisionName
	}
	icps, err := kubeClient.GetIstioVersions(context.TODO(), ns)
	if err != nil {
		return err
	}
	if len(*icps) != 0 {
		var icpTags []string
		var icpTag string
		// create normalized tags for multiple control plane revisions
		for _, icp := range *icps {
			if icp.Revision != revision {
				continue
			}
			tagVer, err := GetTagVersion(icp.Info.GitTag)
			if err != nil {
				return err
			}
			icpTags = append(icpTags, tagVer)
		}
		// sort different versions of control plane revisions
		sort.Strings(icpTags)
		// capture latest revision installed for comparison
		for _, val := range icpTags {
			if val != "" {
				icpTag = val
			}
		}
		// when the revision is passed
		if icpTag != "" && tag != icpTag {
			check := "         Before upgrading, you may wish to use 'istioctl x precheck' to check for upgrade warnings.\n"
			revisionWarning := "         Running this command will overwrite it; use revisions to upgrade alongside the existing version.\n"
			if revision != util.DefaultRevisionName {
				revisionWarning = ""
			}
			if icpTag < tag {
				p.Printf("%s Istio is being upgraded from %s to %s.\n"+revisionWarning+check,
					warnMarker, icpTag, tag)
			} else {
				p.Printf("%s Istio is being downgraded from %s to %s.\n"+revisionWarning+check,
					warnMarker, icpTag, tag)
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
	return iop.Spec.Profile, constants.IstioSystemNamespace, enabledComponents, nil
}

func humanReadableJoin(ss []string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	case 2:
		return ss[0] + " and " + ss[1]
	default:
		return strings.Join(ss[:len(ss)-1], ", ") + ", and " + ss[len(ss)-1]
	}
}

func detectDefaultWebhookChange(p Printer, client kube.CLIClient, iop *v1alpha12.IstioOperator, exists bool) error {
	if !helmreconciler.DetectIfTagWebhookIsNeeded(iop, exists) {
		return nil
	}
	mwhs, err := client.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{
		LabelSelector: "app=sidecar-injector,istio.io/rev=default,istio.io/tag=default",
	})
	if err != nil {
		return err
	}
	// If there is no default webhook but a revisioned default webhook exists,
	// and we are installing a new IOP with default semantics, the default webhook shifts.
	if exists && len(mwhs.Items) == 0 && iop.Spec.GetRevision() == "" {
		p.Println("This installation will make default injection and validation pointing to the default revision, and " +
			"originally it was pointing to the revisioned one.")
	}
	return nil
}
