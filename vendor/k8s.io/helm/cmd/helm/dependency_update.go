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
	"path/filepath"

	"github.com/spf13/cobra"
	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm/helmpath"
)

const dependencyUpDesc = `
Update the on-disk dependencies to mirror the requirements.yaml file.

This command verifies that the required charts, as expressed in 'requirements.yaml',
are present in 'charts/' and are at an acceptable version. It will pull down
the latest charts that satisfy the dependencies, and clean up old dependencies.

On successful update, this will generate a lock file that can be used to
rebuild the requirements to an exact version.

Dependencies are not required to be represented in 'requirements.yaml'. For that
reason, an update command will not remove charts unless they are (a) present
in the requirements.yaml file, but (b) at the wrong version.
`

// dependencyUpdateCmd describes a 'helm dependency update'
type dependencyUpdateCmd struct {
	out         io.Writer
	chartpath   string
	helmhome    helmpath.Home
	verify      bool
	keyring     string
	skipRefresh bool
}

// newDependencyUpdateCmd creates a new dependency update command.
func newDependencyUpdateCmd(out io.Writer) *cobra.Command {
	duc := &dependencyUpdateCmd{out: out}

	cmd := &cobra.Command{
		Use:     "update [flags] CHART",
		Aliases: []string{"up"},
		Short:   "update charts/ based on the contents of requirements.yaml",
		Long:    dependencyUpDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			cp := "."
			if len(args) > 0 {
				cp = args[0]
			}

			var err error
			duc.chartpath, err = filepath.Abs(cp)
			if err != nil {
				return err
			}

			duc.helmhome = settings.Home

			return duc.run()
		},
	}

	f := cmd.Flags()
	f.BoolVar(&duc.verify, "verify", false, "verify the packages against signatures")
	f.StringVar(&duc.keyring, "keyring", defaultKeyring(), "keyring containing public keys")
	f.BoolVar(&duc.skipRefresh, "skip-refresh", false, "do not refresh the local repository cache")

	return cmd
}

// run runs the full dependency update process.
func (d *dependencyUpdateCmd) run() error {
	man := &downloader.Manager{
		Out:        d.out,
		ChartPath:  d.chartpath,
		HelmHome:   d.helmhome,
		Keyring:    d.keyring,
		SkipUpdate: d.skipRefresh,
		Getters:    getter.All(settings),
	}
	if d.verify {
		man.Verify = downloader.VerifyAlways
	}
	if settings.Debug {
		man.Debug = true
	}
	return man.Update()
}
