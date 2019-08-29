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

	"istio.io/operator/pkg/helm"
)

func profileListCmd(rootArgs *rootArgs) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lists available Istio configuration profiles.",
		Long:  "The list subcommand is used to list available Istio configuration profiles.",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			profileList(rootArgs)
		}}

}

// profileList list all the builtin profiles.
func profileList(args *rootArgs) {
	initLogsOrExit(args)
	profiles := helm.ListBuiltinProfiles()
	if len(profiles) == 0 {
		fmt.Println("No profiles available.")
	} else {
		fmt.Println("Istio configuration profiles:")
		for _, profile := range profiles {
			fmt.Printf("    %s\n", profile)
		}
	}
}
