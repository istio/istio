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
	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
)

// ManifestCmd is a group of commands related to manifest generation, installation, diffing and migration.
func ManifestCmd(ctx cli.Context) *cobra.Command {
	mc := &cobra.Command{
		Use:   "manifest",
		Short: "Commands related to Istio manifests",
		Long:  "The manifest command generates and diffs Istio manifests.",
	}

	mgcArgs := &ManifestGenerateArgs{}
	mdcArgs := &manifestDiffArgs{}

	args := &RootArgs{}

	mgc := ManifestGenerateCmd(ctx, args, mgcArgs)
	mdc := manifestDiffCmd(mdcArgs)
	ic := InstallCmd(ctx)

	addFlags(mc, args)
	addFlags(mgc, args)
	addFlags(mdc, args)

	addManifestGenerateFlags(mgc, mgcArgs)
	addManifestDiffFlags(mdc, mdcArgs)

	mc.AddCommand(mgc)
	mc.AddCommand(mdc)
	mc.AddCommand(ic)

	return mc
}
