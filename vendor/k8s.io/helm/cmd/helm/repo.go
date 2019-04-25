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
	"io"

	"github.com/spf13/cobra"
)

var repoHelm = `
This command consists of multiple subcommands to interact with chart repositories.

It can be used to add, remove, list, and index chart repositories.
Example usage:
    $ helm repo add [NAME] [REPO_URL]
`

func newRepoCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo [FLAGS] add|remove|list|index|update [ARGS]",
		Short: "add, list, remove, update, and index chart repositories",
		Long:  repoHelm,
	}

	cmd.AddCommand(newRepoAddCmd(out))
	cmd.AddCommand(newRepoListCmd(out))
	cmd.AddCommand(newRepoRemoveCmd(out))
	cmd.AddCommand(newRepoIndexCmd(out))
	cmd.AddCommand(newRepoUpdateCmd(out))

	return cmd
}
