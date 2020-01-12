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
	"os"

	"istio.io/operator/pkg/util"

	"github.com/spf13/cobra"

	"istio.io/operator/pkg/helm"
)

func profileDiffCmd(rootArgs *rootArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <file1.yaml> <file2.yaml>",
		Short: "Diffs two Istio configuration profiles",
		Long:  "The diff subcommand displays the differences between two Istio configuration profiles.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("diff requires two profiles")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return profileDiff(rootArgs, args)
		}}

}

// profileDiff compare two profile files.
func profileDiff(rootArgs *rootArgs, args []string) error {
	initLogsOrExit(rootArgs)

	a, err := helm.ReadProfileYAML(args[0])
	if err != nil {
		return fmt.Errorf("could not read %q: %v", args[0], err)
	}

	b, err := helm.ReadProfileYAML(args[1])
	if err != nil {
		return fmt.Errorf("could not read %q: %v", args[1], err)
	}

	diff := util.YAMLDiff(a, b)
	if diff == "" {
		fmt.Println("Profiles are identical")
	} else {
		fmt.Printf("Difference of profiles:\n%s", diff)
		os.Exit(1)
	}

	return nil
}
