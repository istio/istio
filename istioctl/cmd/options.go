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

package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var helpFlags = map[string]bool{
	"context":              true,
	"istioNamespace":       true,
	"kubeconfig":           true,
	"log_as_json":          true,
	"log_stacktrace_level": true,
	"log_target":           true,
	"log_caller":           true,
	"log_output_level":     true,
	"namespace":            true,
	"vklog":                true,
	// istioctl also inherits support for log_rotate, log_rotate_max_age, log_rotate_max_backups,
	// log_rotate_max_size, but these are rarely appropriate for a user-facing CLI so we ignore them
}

func optionsCommand() *cobra.Command {
	retval := &cobra.Command{
		Use:   "options",
		Short: "Displays istioctl global options",
		Args:  cobra.ExactArgs(0),
	}

	retval.SetHelpFunc(func(c *cobra.Command, args []string) {
		c.Printf("The following options can be passed to any command:\n\n")
		// (Currently the only global options we show are help options)
		retval.Root().PersistentFlags().VisitAll(func(flag *pflag.Flag) {
			if _, ok := helpFlags[flag.Name]; ok {
				// Currently every flag.Shorthand is "", so there is no point in showing shorthands
				shorthand := "   "
				if flag.Shorthand != "" {
					shorthand = "-" + flag.Shorthand + ","
				}
				c.Printf("  %s --%s: %s\n", shorthand, flag.Name, flag.Usage)
			}
		})
	})

	return retval
}

// validateFlagIsSetManuallyOrNot can validate that a persistent flag is set manually or not by user for given command
func validateFlagIsSetManuallyOrNot(istioCmd *cobra.Command, flagName string) bool {
	if istioCmd != nil {
		allPersistentFlagSet := istioCmd.PersistentFlags()
		if flagName != "" {
			return allPersistentFlagSet.Changed(flagName)
		}
	}
	return false
}
