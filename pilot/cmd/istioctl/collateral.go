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
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var (
	outputDir      string
	prependContent string

	markdownCmd = &cobra.Command{
		Use:               "markdown",
		Short:             "Generate markdown documentation for Istioctl",
		DisableAutoGenTag: true,
		Hidden:            true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return doc.GenMarkdownTree(rootCmd, outputDir)
		},
	}
	completeCmd = &cobra.Command{
		Use:   "completion",
		Short: "Generate bash completion for Istioctl",
		Long: `
Output shell completion code for the bash shell. The shell output must
be evaluated to provide interactive completion of istioctl
commands.`,
		Example: `
# Add the following to .bash_profile.
source <(istioctl completion)

# Create a separate completion file and source that from .bash_profile
istioctl completion > ~/.istioctl-complete.bash
echo "source ~/.istioctl-complete.bash" >> ~/.bash_profile
`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return rootCmd.GenBashCompletion(os.Stdout)
		},
	}
)

func init() {
	markdownCmd.PersistentFlags().StringVar(&outputDir, "dir", ".",
		"Output directory for generated markdown files")
	markdownCmd.PersistentFlags().StringVar(&prependContent, "prepend", "",
		"Prepend content to the generated markdown files")
	rootCmd.AddCommand(markdownCmd)
	rootCmd.AddCommand(completeCmd)
}
