//go:build agent

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

package collateral

import (
	"fmt"

	"github.com/spf13/cobra"
)

// cobraCommandWithFilter returns a Cobra command used to output a tool's collateral files (markdown docs, bash
// completion & man pages). It allows passing in a set of predicates to filter out and remove items selectively.
// The root argument must be the root command for the tool.
func CobraCommand(root *cobra.Command, meta Metadata) *cobra.Command {
	return &cobra.Command{
		Use:    "collateral",
		Short:  "Generate collateral support files for this program",
		Hidden: true,

		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("this build is not compiled with collateral support")
		},
	}
}
