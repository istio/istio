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
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/pkg/log"
)

type upgradeArgs struct {
	*InstallArgs
}

func addUpgradeFlags(cmd *cobra.Command, args *upgradeArgs) {
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
}

// UpgradeCmd upgrades Istio control plane in-place with eligibility checks.
func UpgradeCmd(logOpts *log.Options) *cobra.Command {
	rootArgs := &RootArgs{}
	upgradeArgs := &upgradeArgs{
		InstallArgs: &InstallArgs{},
	}
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Istio control plane in-place",
		Long: "The upgrade command is an alias for the install command" +
			" that performs additional upgrade-related checks.",
		RunE: func(cmd *cobra.Command, args []string) (e error) {
			l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.ErrOrStderr(), installerScope)
			p := NewPrinterForWriter(cmd.OutOrStderr())
			return Install(rootArgs, upgradeArgs.InstallArgs, logOpts, cmd.OutOrStdout(), l, p)
		},
	}
	addFlags(cmd, rootArgs)
	addUpgradeFlags(cmd, upgradeArgs)
	return cmd
}
