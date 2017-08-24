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

// Package cmd provides a simple command to be used to output the auto-generated
// collateral files for the various broker CLI commands. More specifically, this
// outputs markdown files and man pages that describe the CLI commands, along
// with bash completion files.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	brks "istio.io/broker/cmd/brks/cmd"
	"istio.io/broker/cmd/shared"
)

// GetRootCmd returns the root of the cobra command-tree.
func GetRootCmd(args []string, printf, fatalf shared.FormatFn) *cobra.Command {
	outputDir := ""

	rootCmd := &cobra.Command{
		Use:   "brkcol",
		Short: "Generate collateral for Broker CLI commands",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("'%s' is an invalid argument", args[0])
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			work(printf, fatalf, outputDir)
		},
	}
	rootCmd.Flags().StringVarP(&outputDir, "outputDir", "o", ".", "Directory where to generate the CLI collateral files")

	return rootCmd
}

func work(printf, fatalf shared.FormatFn, outputDir string) {
	roots := []*cobra.Command{
		brks.GetRootCmd(nil),
	}

	printf("Outputting Broker CLI collateral files to %s", outputDir)
	for _, r := range roots {
		hdr := doc.GenManHeader{
			Title:   "Istio Broker",
			Section: "Broker CLI",
			Manual:  "Istio Broker",
		}

		if err := doc.GenManTree(r, &hdr, outputDir); err != nil {
			fatalf("Unable to output manpage tree: %v", err)
		}

		if err := doc.GenMarkdownTree(r, outputDir); err != nil {
			fatalf("Unable to output markdown tree: %v", err)
		}

		if err := doc.GenYamlTree(r, outputDir); err != nil {
			fatalf("Unable to output YAML tree: %v", err)
		}

		if err := r.GenBashCompletionFile(outputDir + "/" + r.Name() + ".bash"); err != nil {
			fatalf("Unable to output bash completion file: %v", err)
		}
	}
}
