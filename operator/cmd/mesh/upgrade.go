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

package mesh // import "istio.io/istio/operator/cmd/mesh"

import (
	"time"

	"github.com/spf13/cobra"

	"istio.io/pkg/log"
)

type upgradeArgs struct {
	*installArgs
}

func addUpgradeFlags(cmd *cobra.Command, args *upgradeArgs) {
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
}

// UpgradeCmd upgrades Istio control plane in-place with eligibility checks.
func UpgradeCmd(logOpts *log.Options) *cobra.Command {
	rootArgs := &rootArgs{}
	upgradeArgs := &upgradeArgs{
		installArgs: &installArgs{},
	}
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Istio control plane in-place",
		Long: "The upgrade command is an alias for the install command" +
			" that performs additional upgrade-related checks.",
		RunE: func(cmd *cobra.Command, args []string) (e error) {
			return runApplyCmd(cmd, rootArgs, upgradeArgs.installArgs, logOpts)
		},
	}
	addFlags(cmd, rootArgs)
	addUpgradeFlags(cmd, upgradeArgs)
	return cmd
}
