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
	"path/filepath"

	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/proto/hapi/chart"
)

const createDesc = `
This command creates a chart directory along with the common files and
directories used in a chart.

For example, 'helm create foo' will create a directory structure that looks
something like this:

	foo/
	  |
	  |- .helmignore        # Contains patterns to ignore when packaging Helm charts.
	  |
	  |- Chart.yaml         # Information about your chart
	  |
	  |- values.yaml        # The default values for your templates
	  |
	  |- charts/            # Charts that this chart depends on
	  |
	  |- templates/         # The template files
	  |
	  |- templates/tests/   # The test files

'helm create' takes a path for an argument. If directories in the given path
do not exist, Helm will attempt to create them as it goes. If the given
destination exists and there are files in that directory, conflicting files
will be overwritten, but other files will be left alone.
`

type createCmd struct {
	home    helmpath.Home
	name    string
	out     io.Writer
	starter string
}

func newCreateCmd(out io.Writer) *cobra.Command {
	cc := &createCmd{out: out}

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "create a new chart with the given name",
		Long:  createDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			cc.home = settings.Home
			if len(args) == 0 {
				return errors.New("the name of the new chart is required")
			}
			if len(args) > 1 {
				return errors.New("command 'create' doesn't support multiple arguments")
			}
			cc.name = args[0]
			return cc.run()
		},
	}

	cmd.Flags().StringVarP(&cc.starter, "starter", "p", "", "the named Helm starter scaffold")
	return cmd
}

func (c *createCmd) run() error {
	fmt.Fprintf(c.out, "Creating %s\n", c.name)
	chartname := filepath.Base(c.name)
	cfile := &chart.Metadata{
		Name:        chartname,
		Description: "A Helm chart for Kubernetes",
		Version:     "0.1.0",
		AppVersion:  "1.0",
		ApiVersion:  chartutil.ApiVersionV1,
	}

	if c.starter != "" {
		// Create from the starter
		lstarter := filepath.Join(c.home.Starters(), c.starter)
		return chartutil.CreateFrom(cfile, filepath.Dir(c.name), lstarter)
	}

	_, err := chartutil.Create(cfile, filepath.Dir(c.name))
	return err
}
