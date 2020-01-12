// Copyright 2019 Istio Authors
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

	"github.com/spf13/cobra"
)

type profileDumpArgs struct {
	// inFilename is the path to the input IstioOperator CR.
	inFilename string
	// If set, display the translated Helm values rather than IstioOperatorSpec.
	helmValues bool
	// configPath sets the root node for the subtree to display the config for.
	configPath string
}

func addProfileDumpFlags(cmd *cobra.Command, args *profileDumpArgs) {
	cmd.PersistentFlags().StringVarP(&args.inFilename, "filename", "f", "", filenameFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&args.configPath, "config-path", "p", "",
		"The path the root of the configuration subtree to dump e.g. trafficManagement.components.pilot. By default, dump whole tree")
	cmd.PersistentFlags().BoolVarP(&args.helmValues, "helm-values", "", false,
		"If set, dumps the Helm values that IstioControlPlaceSpec is translated to before manifests are rendered")
}

func profileDumpCmd(rootArgs *rootArgs, pdArgs *profileDumpArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "dump [<profile>]",
		Short: "Dumps an Istio configuration profile",
		Long:  "The dump subcommand dumps the values in an Istio configuration profile.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("too many positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			l := NewLogger(rootArgs.logToStdErr, cmd.OutOrStdout(), cmd.ErrOrStderr())
			return profileDump(args, rootArgs, pdArgs, l)
		}}

}

func profileDump(args []string, rootArgs *rootArgs, pdArgs *profileDumpArgs, l *Logger) error {
	initLogsOrExit(rootArgs)

	if len(args) == 1 && pdArgs.inFilename != "" {
		return fmt.Errorf("cannot specify both profile name and filename flag")
	}

	profile := ""
	if len(args) == 1 {
		profile = args[0]
	}
	y, err := genProfile(pdArgs.helmValues, pdArgs.inFilename, profile, "", pdArgs.configPath, true, l)
	if err != nil {
		return err
	}

	l.print(y + "\n")

	return nil
}
