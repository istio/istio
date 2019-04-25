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
	"path/filepath"

	"github.com/Masterminds/semver"
	"github.com/gosuri/uitable"
	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/chartutil"
)

const dependencyDesc = `
Manage the dependencies of a chart.

Helm charts store their dependencies in 'charts/'. For chart developers, it is
often easier to manage a single dependency file ('requirements.yaml')
which declares all dependencies.

The dependency commands operate on that file, making it easy to synchronize
between the desired dependencies and the actual dependencies stored in the
'charts/' directory.

A 'requirements.yaml' file is a YAML file in which developers can declare chart
dependencies, along with the location of the chart and the desired version.
For example, this requirements file declares two dependencies:

    # requirements.yaml
    dependencies:
    - name: nginx
      version: "1.2.3"
      repository: "https://example.com/charts"
    - name: memcached
      version: "3.2.1"
      repository: "https://another.example.com/charts"

The 'name' should be the name of a chart, where that name must match the name
in that chart's 'Chart.yaml' file.

The 'version' field should contain a semantic version or version range.

The 'repository' URL should point to a Chart Repository. Helm expects that by
appending '/index.yaml' to the URL, it should be able to retrieve the chart
repository's index. Note: 'repository' can be an alias. The alias must start
with 'alias:' or '@'.

Starting from 2.2.0, repository can be defined as the path to the directory of
the dependency charts stored locally. The path should start with a prefix of
"file://". For example,

    # requirements.yaml
    dependencies:
    - name: nginx
      version: "1.2.3"
      repository: "file://../dependency_chart/nginx"

If the dependency chart is retrieved locally, it is not required to have the
repository added to helm by "helm add repo". Version matching is also supported
for this case.
`

const dependencyListDesc = `
List all of the dependencies declared in a chart.

This can take chart archives and chart directories as input. It will not alter
the contents of a chart.

This will produce an error if the chart cannot be loaded. It will emit a warning
if it cannot find a requirements.yaml.
`

func newDependencyCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dependency update|build|list",
		Aliases: []string{"dep", "dependencies"},
		Short:   "manage a chart's dependencies",
		Long:    dependencyDesc,
	}

	cmd.AddCommand(newDependencyListCmd(out))
	cmd.AddCommand(newDependencyUpdateCmd(out))
	cmd.AddCommand(newDependencyBuildCmd(out))

	return cmd
}

type dependencyListCmd struct {
	out       io.Writer
	chartpath string
}

func newDependencyListCmd(out io.Writer) *cobra.Command {
	dlc := &dependencyListCmd{out: out}

	cmd := &cobra.Command{
		Use:     "list [flags] CHART",
		Aliases: []string{"ls"},
		Short:   "list the dependencies for the given chart",
		Long:    dependencyListDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			cp := "."
			if len(args) > 0 {
				cp = args[0]
			}

			var err error
			dlc.chartpath, err = filepath.Abs(cp)
			if err != nil {
				return err
			}
			return dlc.run()
		},
	}
	return cmd
}

func (l *dependencyListCmd) run() error {
	c, err := chartutil.Load(l.chartpath)
	if err != nil {
		return err
	}

	r, err := chartutil.LoadRequirements(c)
	if err != nil {
		if err == chartutil.ErrRequirementsNotFound {
			fmt.Fprintf(l.out, "WARNING: no requirements at %s\n", filepath.Join(l.chartpath, "charts"))
			return nil
		}
		return err
	}

	l.printRequirements(r, l.out)
	fmt.Fprintln(l.out)
	l.printMissing(r)
	return nil
}

func (l *dependencyListCmd) dependencyStatus(dep *chartutil.Dependency) string {
	filename := fmt.Sprintf("%s-%s.tgz", dep.Name, "*")
	archives, err := filepath.Glob(filepath.Join(l.chartpath, "charts", filename))
	if err != nil {
		return "bad pattern"
	} else if len(archives) > 1 {
		return "too many matches"
	} else if len(archives) == 1 {
		archive := archives[0]
		if _, err := os.Stat(archive); err == nil {
			c, err := chartutil.Load(archive)
			if err != nil {
				return "corrupt"
			}
			if c.Metadata.Name != dep.Name {
				return "misnamed"
			}

			if c.Metadata.Version != dep.Version {
				constraint, err := semver.NewConstraint(dep.Version)
				if err != nil {
					return "invalid version"
				}

				v, err := semver.NewVersion(c.Metadata.Version)
				if err != nil {
					return "invalid version"
				}

				if constraint.Check(v) {
					return "ok"
				}
				return "wrong version"
			}
			return "ok"
		}
	}

	folder := filepath.Join(l.chartpath, "charts", dep.Name)
	if fi, err := os.Stat(folder); err != nil {
		return "missing"
	} else if !fi.IsDir() {
		return "mispackaged"
	}

	c, err := chartutil.Load(folder)
	if err != nil {
		return "corrupt"
	}

	if c.Metadata.Name != dep.Name {
		return "misnamed"
	}

	if c.Metadata.Version != dep.Version {
		constraint, err := semver.NewConstraint(dep.Version)
		if err != nil {
			return "invalid version"
		}

		v, err := semver.NewVersion(c.Metadata.Version)
		if err != nil {
			return "invalid version"
		}

		if constraint.Check(v) {
			return "unpacked"
		}
		return "wrong version"
	}

	return "unpacked"
}

// printRequirements prints all of the requirements in the yaml file.
func (l *dependencyListCmd) printRequirements(reqs *chartutil.Requirements, out io.Writer) {
	table := uitable.New()
	table.MaxColWidth = 80
	table.AddRow("NAME", "VERSION", "REPOSITORY", "STATUS")
	for _, row := range reqs.Dependencies {
		table.AddRow(row.Name, row.Version, row.Repository, l.dependencyStatus(row))
	}
	fmt.Fprintln(out, table)
}

// printMissing prints warnings about charts that are present on disk, but are not in the requirements.
func (l *dependencyListCmd) printMissing(reqs *chartutil.Requirements) {
	folder := filepath.Join(l.chartpath, "charts/*")
	files, err := filepath.Glob(folder)
	if err != nil {
		fmt.Fprintln(l.out, err)
		return
	}

	for _, f := range files {
		fi, err := os.Stat(f)
		if err != nil {
			fmt.Fprintf(l.out, "Warning: %s\n", err)
		}
		// Skip anything that is not a directory and not a tgz file.
		if !fi.IsDir() && filepath.Ext(f) != ".tgz" {
			continue
		}
		c, err := chartutil.Load(f)
		if err != nil {
			fmt.Fprintf(l.out, "WARNING: %q is not a chart.\n", f)
			continue
		}
		found := false
		for _, d := range reqs.Dependencies {
			if d.Name == c.Metadata.Name {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(l.out, "WARNING: %q is not in requirements.yaml.\n", f)
		}
	}

}
