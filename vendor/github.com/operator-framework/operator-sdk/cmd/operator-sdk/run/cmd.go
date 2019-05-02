// Copyright 2019 The Operator-SDK Authors
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

package run

import (
	"github.com/spf13/cobra"
)

// NewCmd returns a command that contains subcommands to run specific
// operator types.
func NewCmd() *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Runs a generic operator",
		Long: `Runs a generic operator. This is intended to be used when running
in a Pod inside a cluster. Developers wanting to run their operator locally
should use "up local" instead.`,
	}

	runCmd.AddCommand(newRunAnsibleCmd())
	runCmd.AddCommand(newRunHelmCmd())
	return runCmd
}
