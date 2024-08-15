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

	binversion "istio.io/istio/operator/version"
	"istio.io/istio/pkg/url"
)

var (
	baseVersion    = binversion.OperatorVersionString
	setFlagHelpStr = `Override an IstioOperator value, e.g. to choose a profile
(--set profile=demo), enable or disable components (--set components.cni.enabled=true), or override Istio
settings (--set meshConfig.enableTracing=true). See documentation for more info:` + url.IstioOperatorSpec
	// ManifestsFlagHelpStr is the command line description for --manifests
	ManifestsFlagHelpStr = `Specify a path to a directory of charts and profiles
(e.g. ~/Downloads/istio-` + baseVersion + `/manifests).
`
)

const (
	ChartsDeprecatedStr         = "Deprecated, use --manifests instead."
	revisionFlagHelpStr         = `Target control plane revision for the command.`
	skipConfirmationFlagHelpStr = `The skipConfirmation determines whether the user is prompted for confirmation.
If set to true, the user is not prompted and a Yes response is assumed in all cases.`
	filenameFlagHelpStr = `Path to file containing IstioOperator custom resource
This flag can be specified multiple times to overlay multiple files. Multiple files are overlaid in left to right order.`
	ForceFlagHelpStr       = `Proceed even with validation errors.`
	VerifyCRInstallHelpStr = "Verify the Istio control plane after installation/in-place upgrade"
)

type RootArgs struct {
	// DryRun performs all steps except actually applying the manifests or creating output dirs/files.
	DryRun bool
}

func addFlags(cmd *cobra.Command, rootArgs *RootArgs) {
	cmd.PersistentFlags().BoolVarP(&rootArgs.DryRun, "dry-run", "",
		false, "Console/log output only, make no changes.")
}
