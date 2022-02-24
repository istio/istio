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
	"io"
	"os"

	"github.com/spf13/cobra"

	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/pkg/log"
)

type profileDiffArgs struct {
	// manifestsPath is a path to a charts and profiles directory in the local filesystem, or URL with a release tgz.
	manifestsPath string
}

func addProfileDiffFlags(cmd *cobra.Command, args *profileDiffArgs) {
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "charts", "", "", ChartsDeprecatedStr)
	cmd.PersistentFlags().StringVarP(&args.manifestsPath, "manifests", "d", "", ManifestsFlagHelpStr)
}

func profileDiffCmd(rootArgs *RootArgs, pfArgs *profileDiffArgs, logOpts *log.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <profile|file1.yaml> <profile|file2.yaml>",
		Short: "Diffs two Istio configuration profiles",
		Long:  "The diff subcommand displays the differences between two Istio configuration profiles.",
		Example: `  # Profile diff by providing yaml files
  istioctl profile diff manifests/profiles/default.yaml manifests/profiles/demo.yaml

  # Profile diff by providing a profile name
  istioctl profile diff default demo`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("diff requires two profiles")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			isdifferent, err := profileDiff(cmd, rootArgs, pfArgs, args, logOpts)
			if err != nil {
				return err
			}
			if isdifferent {
				os.Exit(1)
			}
			return nil
		},
	}
}

// profileDiff compare two profile files.
func profileDiff(cmd *cobra.Command, rootArgs *RootArgs, pfArgs *profileDiffArgs, args []string, logOpts *log.Options) (bool, error) {
	initLogsOrExit(rootArgs)

	l := clog.NewConsoleLogger(cmd.OutOrStdout(), cmd.OutOrStderr(), nil)
	setFlags := make([]string, 0)
	if pfArgs.manifestsPath != "" {
		setFlags = append(setFlags, fmt.Sprintf("installPackagePath=%s", pfArgs.manifestsPath))
	}
	if err := configLogs(logOpts); err != nil {
		return false, fmt.Errorf("could not configure logs: %s", err)
	}
	return profileDiffInternal(args[0], args[1], setFlags, cmd.OutOrStdout(), l)
}

func profileDiffInternal(profileA, profileB string, setFlags []string, writer io.Writer, l clog.Logger) (bool, error) {
	a, _, err := manifest.GenIOPFromProfile(profileA, "", setFlags, true, true, nil, l)
	if err != nil {
		return false, fmt.Errorf("could not read %q: %v", profileA, err)
	}

	b, _, err := manifest.GenIOPFromProfile(profileB, "", setFlags, true, true, nil, l)
	if err != nil {
		return false, fmt.Errorf("could not read %q: %v", profileB, err)
	}

	diff := util.YAMLDiff(a, b)
	if diff == "" {
		fmt.Fprintln(writer, "Profiles are identical")
	} else {
		fmt.Fprintf(writer, "The difference between profiles:\n%s", diff)
		return true, nil
	}

	return false, nil
}
