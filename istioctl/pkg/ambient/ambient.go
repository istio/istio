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

package ambient

import (
	"github.com/spf13/cobra"
	"istio.io/istio/istioctl/pkg/cli"
)

func Cmd(ctx cli.Context) *cobra.Command {
	ambientCmd := &cobra.Command{
		Use:   "ambient",
		Short: "Ambient mesh management commands",
		Long: `Commands for managing Istio ambient mesh.

Ambient mesh is a new data plane mode for Istio that provides a simplified architecture
without requiring sidecar proxies for every workload. This command provides tools for
migrating from sidecar mesh to ambient mesh and managing ambient mesh resources.
`,
		Example: `  # Migrate from sidecar mesh to ambient mesh
  istioctl ambient migrate

  # Migrate specific namespace with dry run
  istioctl ambient migrate --namespace my-namespace --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}

	// Add migrate subcommand
	ambientCmd.AddCommand(MigrateCmd(ctx))

	return ambientCmd
}
