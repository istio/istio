// Copyright 2017 Istio Authors
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

package main

import (
	"errors"

	"github.com/spf13/cobra"
)

var (
	mixerFile string
	mixerCmd  = &cobra.Command{
		Use:   "mixer",
		Short: "Istio Mixer configuration (deprecated)",
		Long: `
This method of configuring Mixer is removed in favor of kubectl
`,
		SilenceUsage: true,
		Hidden:       true,
		RunE: func(c *cobra.Command, args []string) error {
			return errors.New("this method of configuring Mixer is removed in favor of kubectl")
		},
	}
)

func init() {
	// flag is maintained so that the user gets the correct error message.
	mixerCmd.PersistentFlags().StringVarP(&mixerFile, "file", "f", "",
		"Input file with contents of the Mixer rule")
	rootCmd.AddCommand(mixerCmd)
}
