/*
Copyright The Helm Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

const docsDesc = `
Generate documentation files for Helm.

This command can generate documentation for Helm in the following formats:

- Markdown
- Man pages

It can also generate bash autocompletions.

	$ helm docs markdown -dir mydocs/
`

type docsCmd struct {
	out           io.Writer
	dest          string
	docTypeString string
	topCmd        *cobra.Command
}

func newDocsCmd(out io.Writer) *cobra.Command {
	dc := &docsCmd{out: out}

	cmd := &cobra.Command{
		Use:    "docs",
		Short:  "Generate documentation as markdown or man pages",
		Long:   docsDesc,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dc.topCmd = cmd.Root()
			return dc.run()
		},
	}

	f := cmd.Flags()
	f.StringVar(&dc.dest, "dir", "./", "directory to which documentation is written")
	f.StringVar(&dc.docTypeString, "type", "markdown", "the type of documentation to generate (markdown, man, bash)")

	return cmd
}

func (d *docsCmd) run() error {
	switch d.docTypeString {
	case "markdown", "mdown", "md":
		return doc.GenMarkdownTree(d.topCmd, d.dest)
	case "man":
		manHdr := &doc.GenManHeader{Title: "HELM", Section: "1"}
		return doc.GenManTree(d.topCmd, manHdr, d.dest)
	case "bash":
		return d.topCmd.GenBashCompletionFile(filepath.Join(d.dest, "completions.bash"))
	default:
		return fmt.Errorf("unknown doc type %q. Try 'markdown' or 'man'", d.docTypeString)
	}
}
