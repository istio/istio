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
	"errors"
	"fmt"
	"io"

	"github.com/gosuri/uitable"
	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/repo"
)

type repoListCmd struct {
	out  io.Writer
	home helmpath.Home
}

func newRepoListCmd(out io.Writer) *cobra.Command {
	list := &repoListCmd{out: out}

	cmd := &cobra.Command{
		Use:   "list [flags]",
		Short: "list chart repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			list.home = settings.Home
			return list.run()
		},
	}

	return cmd
}

func (a *repoListCmd) run() error {
	f, err := repo.LoadRepositoriesFile(a.home.RepositoryFile())
	if err != nil {
		return err
	}
	if len(f.Repositories) == 0 {
		return errors.New("no repositories to show")
	}
	table := uitable.New()
	table.AddRow("NAME", "URL")
	for _, re := range f.Repositories {
		table.AddRow(re.Name, re.URL)
	}
	fmt.Fprintln(a.out, table)
	return nil
}
