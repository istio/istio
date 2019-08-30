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

// ProfileCmd is a group of commands related to profile listing, dumping and diffing.
func ProfileCmd() *cobra.Command {
	pc := &cobra.Command{
		Use:   "profile",
		Short: "Commands related to Istio configuration profiles.",
		Long:  "The profile subcommand lists, dumps or diffs Istio configuration profiles.",
	}

	pdArgs := &profileDumpArgs{}
	args := &rootArgs{}

	plc := profileListCmd(args)
	pdc := profileDumpCmd(args, pdArgs)
	pdfc := profileDiffCmd(args)

	addFlags(pc, args)
	addFlags(plc, args)
	addFlags(pdc, args)
	addFlags(pdfc, args)

	addProfileDumpFlags(pdc, pdArgs)

	pc.AddCommand(plc)
	pc.AddCommand(pdc)
	pc.AddCommand(pdfc)

	return pc
}
