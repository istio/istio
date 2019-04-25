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
	"os"

	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/repo"
)

type repoRemoveCmd struct {
	out  io.Writer
	name string
	home helmpath.Home
}

func newRepoRemoveCmd(out io.Writer) *cobra.Command {
	remove := &repoRemoveCmd{out: out}

	cmd := &cobra.Command{
		Use:     "remove [flags] [NAME]",
		Aliases: []string{"rm"},
		Short:   "remove a chart repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("need at least one argument, name of chart repository")
			}

			remove.home = settings.Home
			for i := 0; i < len(args); i++ {
				remove.name = args[i]
				if err := remove.run(); err != nil {
					return err
				}
			}
			return nil
		},
	}

	return cmd
}

func (r *repoRemoveCmd) run() error {
	return removeRepoLine(r.out, r.name, r.home)
}

func removeRepoLine(out io.Writer, name string, home helmpath.Home) error {
	repoFile := home.RepositoryFile()
	r, err := repo.LoadRepositoriesFile(repoFile)
	if err != nil {
		return err
	}

	if !r.Remove(name) {
		return fmt.Errorf("no repo named %q found", name)
	}
	if err := r.WriteFile(repoFile, 0644); err != nil {
		return err
	}

	if err := removeRepoCache(name, home); err != nil {
		return err
	}

	fmt.Fprintf(out, "%q has been removed from your repositories\n", name)

	return nil
}

func removeRepoCache(name string, home helmpath.Home) error {
	if _, err := os.Stat(home.CacheIndex(name)); err == nil {
		err = os.Remove(home.CacheIndex(name))
		if err != nil {
			return err
		}
	}
	return nil
}
