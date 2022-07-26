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
	"flag"

	"github.com/spf13/cobra"

	binversion "istio.io/istio/operator/version"
	"istio.io/istio/pkg/url"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

var (
	baseVersion    = binversion.OperatorVersionString
	setFlagHelpStr = `Override an IstioOperator value, e.g. to choose a profile
(--set profile=demo), enable or disable components (--set components.cni.enabled=true), or override Istio
settings (--set meshConfig.enableTracing=true). See documentation for more info:` + url.IstioOperatorSpec
	// ManifestsFlagHelpStr is the command line description for --manifests
	ManifestsFlagHelpStr = `Specify a path to a directory of charts and profiles
(e.g. ~/Downloads/istio-` + baseVersion + `/manifests)
or release tar URL (e.g. ` + url.ReleaseTar + `).
`
)

const (
	ChartsDeprecatedStr         = "Deprecated, use --manifests instead."
	ControlPlaneRevStr          = "Control plane revision"
	revisionFlagHelpStr         = `Target control plane revision for the command.`
	skipConfirmationFlagHelpStr = `The skipConfirmation determines whether the user is prompted for confirmation.
If set to true, the user is not prompted and a Yes response is assumed in all cases.`
	filenameFlagHelpStr = `Path to file containing IstioOperator custom resource
This flag can be specified multiple times to overlay multiple files. Multiple files are overlaid in left to right order.`
	installationCompleteStr = `Installation complete`
	ForceFlagHelpStr        = `Proceed even with validation errors.`
	KubeConfigFlagHelpStr   = `Path to kube config.`
	ContextFlagHelpStr      = `The name of the kubeconfig context to use.`
	HubFlagHelpStr          = `The hub for the operator controller image.`
	TagFlagHelpStr          = `The tag for the operator controller image.`
	ImagePullSecretsHelpStr = `The imagePullSecrets are used to pull the operator image from the private registry,
could be secret list separated by comma, eg. '--imagePullSecrets imagePullSecret1,imagePullSecret2'`
	OperatorNamespaceHelpstr = `The namespace the operator controller is installed into.`
	OperatorRevFlagHelpStr   = `Target revision for the operator.`
	ComponentFlagHelpStr     = "Specify which component to generate manifests for."
	VerifyCRInstallHelpStr   = "Verify the Istio control plane after installation/in-place upgrade"
)

type RootArgs struct {
	// DryRun performs all steps except actually applying the manifests or creating output dirs/files.
	DryRun bool
}

func addFlags(cmd *cobra.Command, rootArgs *RootArgs) {
	cmd.PersistentFlags().BoolVarP(&rootArgs.DryRun, "dry-run", "",
		false, "Console/log output only, make no changes.")
}

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:          "mesh",
		Short:        "Command line Istio install utility.",
		SilenceUsage: true,
		Long: "This command uses the Istio operator code to generate templates, query configurations and perform " +
			"utility operations.",
	}
	rootCmd.SetArgs(args)
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	rootCmd.AddCommand(ManifestCmd(log.DefaultOptions()))
	rootCmd.AddCommand(InstallCmd(log.DefaultOptions()))
	rootCmd.AddCommand(ProfileCmd(log.DefaultOptions()))
	rootCmd.AddCommand(OperatorCmd())
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(UpgradeCmd(log.DefaultOptions()))

	return rootCmd
}
