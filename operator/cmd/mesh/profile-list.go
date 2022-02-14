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
	"sort"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/helm"
)

type profileListArgs struct {
	// manifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
}

func addProfileListFlags(cmd *cobra.Command, args *profileListArgs) {
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
}

func profileListCmd(rootArgs *RootArgs, plArgs *profileListArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lists available Istio configuration profiles",
		Long:  "The list subcommand lists the available Istio configuration profiles.",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return profileList(cmd, rootArgs, plArgs)
		},
	}
}

// profileList list all the builtin profiles.
func profileList(cmd *cobra.Command, args *RootArgs, plArgs *profileListArgs) error {
	initLogsOrExit(args)
	profiles, err := helm.ListProfiles(plArgs.manifestsPath)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		cmd.Println("No profiles available.")
	} else {
		cmd.Println("Istio configuration profiles:")
		sort.Strings(profiles)
		for _, profile := range profiles {
			cmd.Printf("    %s\n", profile)
		}
	}

	return nil
}
