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
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/helm"
)

var getValuesHelp = `
This command downloads a values file for a given release.
`

type getValuesCmd struct {
	release   string
	allValues bool
	out       io.Writer
	client    helm.Interface
	version   int32
	output    string
}

func newGetValuesCmd(client helm.Interface, out io.Writer) *cobra.Command {
	get := &getValuesCmd{
		out:    out,
		client: client,
	}
	cmd := &cobra.Command{
		Use:     "values [flags] RELEASE_NAME",
		Short:   "download the values file for a named release",
		Long:    getValuesHelp,
		PreRunE: func(_ *cobra.Command, _ []string) error { return setupConnection() },
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errReleaseRequired
			}
			get.release = args[0]
			get.client = ensureHelmClient(get.client)
			return get.run()
		},
	}

	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.Int32Var(&get.version, "revision", 0, "get the named release with revision")
	f.BoolVarP(&get.allValues, "all", "a", false, "dump all (computed) values")
	f.StringVar(&get.output, "output", "yaml", "output the specified format (json or yaml)")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

// getValues implements 'helm get values'
func (g *getValuesCmd) run() error {
	res, err := g.client.ReleaseContent(g.release, helm.ContentReleaseVersion(g.version))
	if err != nil {
		return prettyError(err)
	}

	values, err := chartutil.ReadValues([]byte(res.Release.Config.Raw))
	if err != nil {
		return err
	}

	// If the user wants all values, compute the values and return.
	if g.allValues {
		values, err = chartutil.CoalesceValues(res.Release.Chart, res.Release.Config)
		if err != nil {
			return err
		}
	}

	result, err := formatValues(g.output, values)
	if err != nil {
		return err
	}
	fmt.Fprintln(g.out, result)
	return nil
}

func formatValues(format string, values chartutil.Values) (string, error) {
	switch format {
	case "", "yaml":
		out, err := values.YAML()
		if err != nil {
			return "", err
		}
		return out, nil
	case "json":
		out, err := json.Marshal(values)
		if err != nil {
			return "", fmt.Errorf("Failed to Marshal JSON output: %s", err)
		}
		return string(out), nil
	default:
		return "", fmt.Errorf("Unknown output format %q", format)
	}
}
