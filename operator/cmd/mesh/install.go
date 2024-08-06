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

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/operator/pkg/install"
	"istio.io/istio/operator/pkg/render"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/operator/pkg/util/progress"
	pkgversion "istio.io/istio/operator/pkg/version"
	operatorVer "istio.io/istio/operator/version"
	"istio.io/istio/pkg/art"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
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
			p.Printf("%v\n", art.IstioColoredArt())
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
	if err := k8sversion.IsK8VersionSupported(kubeClient, l); err != nil {
		return fmt.Errorf("check minimum supported Kubernetes version: %v", err)
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

	manifests, vals, err := render.GenerateManifest(iArgs.InFilenames, setFlags, iArgs.Force, kubeClient, l)
	if err != nil {
		return fmt.Errorf("generate config: %v", err)
	}

	namespace := vals.GetPathString("metadata.namespace")
	revision := vals.GetPathString("spec.values.revision")
	profile := ptr.NonEmptyOrDefault(vals.GetPathString("spec.profile"), "default")

	// Print information about version changing
	detectIstioVersionDiff(p, tag, namespace, kubeClient, revision)

	// Install is mutating state in the cluster; give users a confirmation to ensure they want this.
	if !rootArgs.DryRun && !iArgs.SkipConfirmation {
		prompt := fmt.Sprintf("This will install the Istio %s profile %q into the cluster. Proceed? (y/N)", tag, profile)
		if !Confirm(prompt, stdOut) {
			p.Println("Cancelled.")
			os.Exit(1)
		}
	}

	i := install.Installer{
		Force:          iArgs.Force,
		DryRun:         rootArgs.DryRun,
		SkipWait:       false,
		Kube:           kubeClient,
		WaitTimeout:    iArgs.ReadinessTimeout,
		Logger:         l,
		Values:         vals,
		ProgressLogger: progress.NewLog(),
	}
	if err := i.InstallManifests(manifests); err != nil {
		return fmt.Errorf("failed to install manifests: %v", err)
	}

	// Post-install message
	if profile == "ambient" {
		p.Println("The ambient profile has been installed successfully, enjoy Istio without sidecars!")
	}
	return nil
}

// detectIstioVersionDiff will show warning if istioctl version and control plane version are different
// nolint: interfacer
func detectIstioVersionDiff(p Printer, tag string, ns string, kubeClient kube.CLIClient, revision string) {
	warnMarker := color.New(color.FgYellow).Add(color.Italic).Sprint("WARNING:")
	if revision == "" {
		revision = util.DefaultRevisionName
	}
	icps, err := kubeClient.GetIstioVersions(context.TODO(), ns)
	if err != nil {
		// Istio may not be installed, no problem
		return
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
				return
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
