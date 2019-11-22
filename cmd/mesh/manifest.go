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
	"github.com/spf13/cobra"
)

// ManifestCmd is a group of commands related to manifest generation, installation, diffing and migration.
func ManifestCmd() *cobra.Command {
	mc := &cobra.Command{
		Use:   "manifest",
		Short: "Commands related to Istio manifests",
		Long:  "The manifest subcommand generates, applies, diffs or migrates Istio manifests.",
	}

	mgcArgs := &manifestGenerateArgs{}
	mdcArgs := &manifestDiffArgs{}
	macArgs := &manifestApplyArgs{}
	mvArgs := &manifestVersionsArgs{}
	mmcArgs := &manifestMigrateArgs{}

	args := &rootArgs{}

	mgc := manifestGenerateCmd(args, mgcArgs)
	mdc := manifestDiffCmd(args, mdcArgs)
	mac := manifestApplyCmd(args, macArgs)
	mvc := manifestVersionsCmd(args, mvArgs)
	mmc := manifestMigrateCmd(args, mmcArgs)

	addFlags(mc, args)
	addFlags(mgc, args)
	addFlags(mdc, args)
	addFlags(mac, args)
	addFlags(mvc, args)
	addFlags(mmc, args)

	addManifestGenerateFlags(mgc, mgcArgs)
	addManifestDiffFlags(mdc, mdcArgs)
	addManifestApplyFlags(mac, macArgs)
	addManifestVersionsFlags(mvc, mvArgs)
	addManifestMigrateFlags(mmc, mmcArgs)

	mc.AddCommand(mgc)
	mc.AddCommand(mdc)
	mc.AddCommand(mac)
	mc.AddCommand(mmc)
	mc.AddCommand(mvc)

	return mc
}
