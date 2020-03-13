// Copyright 2020 Istio Authors
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

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	helpFlags = map[string]bool{
		"log_as_json":          true,
		"log_stacktrace_level": true,
		"log_target":           true,
		"log_caller":           true,
		"log_output_level":     true,
		// istioctl also inherits support for log_rotate, log_rotate_max_age, log_rotate_max_backups,
		// log_rotate_max_size, but these are rarely appropriate for a user-facing CLI so we ignore them
	}
)

func logHelpCommand(rootCmd *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "log-help",
		Short: "Displays istioctl logging options",
		Args:  cobra.ExactArgs(0),
		Run: func(c *cobra.Command, args []string) {
			fmt.Printf("Help Flags:\n")
			rootCmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
				if _, ok := helpFlags[flag.Name]; ok {
					// "20" is the widthof the largest flag, "log_stacktrace_level"
					fmt.Fprintf(c.OutOrStdout(), "--%-20s %s\n", flag.Name, flag.Usage)
				}
			})
		},
	}
}
