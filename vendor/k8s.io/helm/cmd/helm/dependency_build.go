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

	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm/helmpath"
)

const dependencyBuildDesc = `
Build out the charts/ directory from the requirements.lock file.

Build is used to reconstruct a chart's dependencies to the state specified in
the lock file. This will not re-negotiate dependencies, as 'helm dependency update'
does.

If no lock file is found, 'helm dependency build' will mirror the behavior
of 'helm dependency update'.
`

type dependencyBuildCmd struct {
	out       io.Writer
	chartpath string
	verify    bool
	keyring   string
	helmhome  helmpath.Home
}

func newDependencyBuildCmd(out io.Writer) *cobra.Command {
	dbc := &dependencyBuildCmd{out: out}

	cmd := &cobra.Command{
		Use:   "build [flags] CHART",
		Short: "rebuild the charts/ directory based on the requirements.lock file",
		Long:  dependencyBuildDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbc.helmhome = settings.Home
			dbc.chartpath = "."

			if len(args) > 0 {
				dbc.chartpath = args[0]
			}
			return dbc.run()
		},
	}

	f := cmd.Flags()
	f.BoolVar(&dbc.verify, "verify", false, "verify the packages against signatures")
	f.StringVar(&dbc.keyring, "keyring", defaultKeyring(), "keyring containing public keys")

	return cmd
}

func (d *dependencyBuildCmd) run() error {
	man := &downloader.Manager{
		Out:       d.out,
		ChartPath: d.chartpath,
		HelmHome:  d.helmhome,
		Keyring:   d.keyring,
		Getters:   getter.All(settings),
	}
	if d.verify {
		man.Verify = downloader.VerifyIfPossible
	}

	return man.Build()
}
