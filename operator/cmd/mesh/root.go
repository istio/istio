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
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	baseVersion    = binversion.OperatorCodeBaseVersion
	releaseURL     = `https://github.com/istio/istio/releases/download/` + baseVersion + `/istio-` + baseVersion + `-linux-amd64.tar.gz`
	setFlagHelpStr = `Override an IstioOperator value, e.g. to choose a profile
(--set profile=demo), enable or disable components (--set components.policy.enabled=true), or override Istio
settings (--set meshConfig.enableTracing=true). See documentation for more info:
https://istio.io/docs/reference/config/istio.operator.v1alpha1/#IstioOperatorSpec`
	// ManifestsFlagHelpStr is the command line description for --manifests
	ManifestsFlagHelpStr = `Specify a path to a directory of charts and profiles
(e.g. ~/Downloads/istio-` + baseVersion + `/manifests)
or release tar URL (e.g. ` + releaseURL + `).
`
	ChartsDeprecatedStr         = "Deprecated, use --manifests instead."
	revisionFlagHelpStr         = `Target control plane revision for the command.`
	skipConfirmationFlagHelpStr = `skipConfirmation determines whether the user is prompted for confirmation.
If set to true, the user is not prompted and a Yes response is assumed in all cases.`
	filenameFlagHelpStr = `Path to file containing IstioOperator custom resource
This flag can be specified multiple times to overlay multiple files. Multiple files are overlaid in left to right order.`
	installationCompleteStr = `Installation complete`
)

type rootArgs struct {
	// Dry run performs all steps except actually applying the manifests or creating output dirs/files.
	dryRun bool
}

func addFlags(cmd *cobra.Command, rootArgs *rootArgs) {
	cmd.PersistentFlags().BoolVarP(&rootArgs.dryRun, "dry-run", "",
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
	rootCmd.AddCommand(ProfileCmd())
	rootCmd.AddCommand(OperatorCmd())
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(UpgradeCmd())

	version.Info.Version = binversion.OperatorVersionString

	return rootCmd
}
