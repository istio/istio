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

	"istio.io/pkg/log"
)

// ProfileCmd is a group of commands related to profile listing, dumping and diffing.
func ProfileCmd(logOpts *log.Options) *cobra.Command {
	pc := &cobra.Command{
		Use:   "profile",
		Short: "Commands related to Istio configuration profiles",
		Long:  "The profile command lists, dumps or diffs Istio configuration profiles.",
		Example: "istioctl profile list\n" +
			"istioctl install --set profile=demo  # Use a profile from the list",
	}

	pdArgs := &profileDumpArgs{}
	plArgs := &profileListArgs{}
	pdfArgs := &profileDiffArgs{}
	args := &RootArgs{}

	plc := profileListCmd(args, plArgs)
	pdc := profileDumpCmd(args, pdArgs, logOpts)
	pdfc := profileDiffCmd(args, pdfArgs, logOpts)

	addFlags(pc, args)
	addFlags(plc, args)
	addFlags(pdc, args)
	addFlags(pdfc, args)

	addProfileDumpFlags(pdc, pdArgs)
	addProfileListFlags(plc, plArgs)
	addProfileDiffFlags(pdfc, pdfArgs)

	pc.AddCommand(plc)
	pc.AddCommand(pdc)
	pc.AddCommand(pdfc)

	return pc
}
