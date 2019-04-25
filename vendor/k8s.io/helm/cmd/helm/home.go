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

	"github.com/spf13/cobra"
)

var longHomeHelp = `
This command displays the location of HELM_HOME. This is where
any helm configuration files live.
`

func newHomeCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "home",
		Short: "displays the location of HELM_HOME",
		Long:  longHomeHelp,
		Run: func(cmd *cobra.Command, args []string) {
			h := settings.Home
			fmt.Fprintln(out, h)
			if settings.Debug {
				fmt.Fprintf(out, "Repository: %s\n", h.Repository())
				fmt.Fprintf(out, "RepositoryFile: %s\n", h.RepositoryFile())
				fmt.Fprintf(out, "Cache: %s\n", h.Cache())
				fmt.Fprintf(out, "Stable CacheIndex: %s\n", h.CacheIndex("stable"))
				fmt.Fprintf(out, "Starters: %s\n", h.Starters())
				fmt.Fprintf(out, "LocalRepository: %s\n", h.LocalRepository())
				fmt.Fprintf(out, "Plugins: %s\n", h.Plugins())
			}
		},
	}
	return cmd
}
