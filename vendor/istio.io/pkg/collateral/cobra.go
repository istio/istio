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

package collateral

import (
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// CobraCommand returns a Cobra command used to output a tool's collateral files (markdown docs, bash completion & man pages)
// The root argument must be the root command for the tool.
func CobraCommand(root *cobra.Command, hdr *doc.GenManHeader) *cobra.Command {
	c := Control{
		OutputDir: ".",
	}
	var all bool

	cmd := &cobra.Command{
		Use:    "collateral",
		Short:  "Generate collateral support files for this program",
		Hidden: true,

		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				c.EmitYAML = true
				c.EmitBashCompletion = true
				c.EmitZshCompletion = true
				c.EmitManPages = true
				c.EmitMarkdown = true
				c.EmitHTMLFragmentWithFrontMatter = true
				c.ManPageInfo = *hdr
			}

			return EmitCollateral(root, &c)
		},
	}

	cmd.Flags().StringVarP(&c.OutputDir, "outputDir", "o", c.OutputDir, "Directory where to generate the collateral files")
	cmd.Flags().BoolVarP(&all, "all", "", all, "Produce all supported collateral files")
	cmd.Flags().BoolVarP(&c.EmitMarkdown, "markdown", "", c.EmitMarkdown, "Produce markdown documentation files")
	cmd.Flags().BoolVarP(&c.EmitManPages, "man", "", c.EmitManPages, "Produce man pages")
	cmd.Flags().BoolVarP(&c.EmitBashCompletion, "bash", "", c.EmitBashCompletion, "Produce bash completion files")
	cmd.Flags().BoolVarP(&c.EmitZshCompletion, "zsh", "", c.EmitZshCompletion, "Produce zsh completion files")
	cmd.Flags().BoolVarP(&c.EmitYAML, "yaml", "", c.EmitYAML, "Produce YAML documentation files")
	cmd.Flags().BoolVarP(&c.EmitHTMLFragmentWithFrontMatter, "html_fragment_with_front_matter",
		"", c.EmitHTMLFragmentWithFrontMatter, "Produce an HTML documentation file with Hugo/Jekyll-compatible front matter.")

	return cmd
}
