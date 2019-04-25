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

	"golang.org/x/crypto/ssh/terminal"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/repo"
	"syscall"
)

type repoAddCmd struct {
	name     string
	url      string
	username string
	password string
	home     helmpath.Home
	noupdate bool

	certFile string
	keyFile  string
	caFile   string

	out io.Writer
}

func newRepoAddCmd(out io.Writer) *cobra.Command {
	add := &repoAddCmd{out: out}

	cmd := &cobra.Command{
		Use:   "add [flags] [NAME] [URL]",
		Short: "add a chart repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkArgsLength(len(args), "name for the chart repository", "the url of the chart repository"); err != nil {
				return err
			}

			add.name = args[0]
			add.url = args[1]
			add.home = settings.Home

			return add.run()
		},
	}

	f := cmd.Flags()
	f.StringVar(&add.username, "username", "", "chart repository username")
	f.StringVar(&add.password, "password", "", "chart repository password")
	f.BoolVar(&add.noupdate, "no-update", false, "raise error if repo is already registered")
	f.StringVar(&add.certFile, "cert-file", "", "identify HTTPS client using this SSL certificate file")
	f.StringVar(&add.keyFile, "key-file", "", "identify HTTPS client using this SSL key file")
	f.StringVar(&add.caFile, "ca-file", "", "verify certificates of HTTPS-enabled servers using this CA bundle")

	return cmd
}

func (a *repoAddCmd) run() error {
	if a.username != "" && a.password == "" {
		fmt.Fprint(a.out, "Password:")
		password, err := readPassword()
		fmt.Fprintln(a.out)
		if err != nil {
			return err
		}
		a.password = password
	}

	if err := addRepository(a.name, a.url, a.username, a.password, a.home, a.certFile, a.keyFile, a.caFile, a.noupdate); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%q has been added to your repositories\n", a.name)
	return nil
}

func readPassword() (string, error) {
	password, err := terminal.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", err
	}
	return string(password), nil
}

func addRepository(name, url, username, password string, home helmpath.Home, certFile, keyFile, caFile string, noUpdate bool) error {
	f, err := repo.LoadRepositoriesFile(home.RepositoryFile())
	if err != nil {
		return err
	}

	if noUpdate && f.Has(name) {
		return fmt.Errorf("repository name (%s) already exists, please specify a different name", name)
	}

	cif := home.CacheIndex(name)
	c := repo.Entry{
		Name:     name,
		Cache:    cif,
		URL:      url,
		Username: username,
		Password: password,
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   caFile,
	}

	r, err := repo.NewChartRepository(&c, getter.All(settings))
	if err != nil {
		return err
	}

	if err := r.DownloadIndexFile(home.Cache()); err != nil {
		return fmt.Errorf("Looks like %q is not a valid chart repository or cannot be reached: %s", url, err.Error())
	}

	f.Update(&c)

	return f.WriteFile(home.RepositoryFile(), 0644)
}
