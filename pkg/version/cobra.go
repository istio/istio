// Copyright 2018 Istio Authors
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

package version

import (
	"fmt"

	"github.com/spf13/cobra"
)

// CobraCommand returns a command used to print version information.
func CobraCommand() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Prints out build version information",
		Run: func(cmd *cobra.Command, args []string) {
			if short {
				fmt.Fprintln(cmd.OutOrStdout(), Info)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), Info.LongForm())
			}
		},
	}

	cmd.PersistentFlags().BoolVarP(&short, "short", "s", short, "Displays a short form of the version information")

	return cmd
}
