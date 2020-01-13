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

// OperatorCmd is a group of commands related to installation and management of the operator controller.
func OperatorCmd() *cobra.Command {
	oc := &cobra.Command{
		Use:   "operator",
		Short: "Commands related to Istio operator controller.",
		Long:  "The operator subcommand installs, removes and shows the status of the operator controller.",
	}

	oiArgs := &operatorInitArgs{}
	orArgs := &operatorRemoveArgs{}
	args := &rootArgs{}

	oic := operatorInitCmd(args, oiArgs)
	orc := operatorRemoveCmd(args, orArgs)

	addFlags(oic, args)
	addFlags(orc, args)

	addOperatorInitFlags(oic, oiArgs)
	addOperatorRemoveFlags(orc, orArgs)

	oc.AddCommand(oic)
	oc.AddCommand(orc)

	return oc
}
