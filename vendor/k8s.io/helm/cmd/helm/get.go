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
	"io"

	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/helm"
)

var getHelp = `
This command shows the details of a named release.

It can be used to get extended information about the release, including:

  - The values used to generate the release
  - The chart used to generate the release
  - The generated manifest file

By default, this prints a human readable collection of information about the
chart, the supplied values, and the generated manifest file.
`

var errReleaseRequired = errors.New("release name is required")

type getCmd struct {
	release string
	out     io.Writer
	client  helm.Interface
	version int32
}

func newGetCmd(client helm.Interface, out io.Writer) *cobra.Command {
	get := &getCmd{
		out:    out,
		client: client,
	}

	cmd := &cobra.Command{
		Use:     "get [flags] RELEASE_NAME",
		Short:   "download a named release",
		Long:    getHelp,
		PreRunE: func(_ *cobra.Command, _ []string) error { return setupConnection() },
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errReleaseRequired
			}
			get.release = args[0]
			if get.client == nil {
				get.client = newClient()
			}
			return get.run()
		},
	}

	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.Int32Var(&get.version, "revision", 0, "get the named release with revision")

	cmd.AddCommand(newGetValuesCmd(nil, out))
	cmd.AddCommand(newGetManifestCmd(nil, out))
	cmd.AddCommand(newGetHooksCmd(nil, out))
	cmd.AddCommand(newGetNotesCmd(nil, out))

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

// getCmd is the command that implements 'helm get'
func (g *getCmd) run() error {
	res, err := g.client.ReleaseContent(g.release, helm.ContentReleaseVersion(g.version))
	if err != nil {
		return prettyError(err)
	}
	return printRelease(g.out, res.Release)
}
