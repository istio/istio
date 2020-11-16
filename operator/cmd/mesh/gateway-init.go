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

	"github.com/spf13/cobra"
	"istio.io/istio/pkg/config/labels"
)

func operatorGatewayInstallCmd() *cobra.Command {
	rootArgs := &rootArgs{}
	iArgs := &installArgs{}

	ig := &cobra.Command{
		Use:     "install",
		Short:   "Applies an Istio manifest, installing or reconfiguring Istio on a cluster.",
		Long:    "The install command generates an Istio install manifest and applies it to a cluster.",
		Aliases: []string{"apply"},
		Example: `  # Apply a default Istio gateway installation
  istioctl gateway install --set revision=canary
`,
		Args: cobra.ExactArgs(0),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if !labels.IsDNS1123Label(iArgs.revision) && cmd.PersistentFlags().Changed("revision") {
				return fmt.Errorf("invalid revision specified: %v", iArgs.revision)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: implement the command to apply to install ingress gateway.
			fmt.Println("TODO: implement the command to apply to install ingress gateway.")
			return nil
		}}

	addFlags(ig, rootArgs)
	addInstallFlags(ig, iArgs)
	return ig
}
