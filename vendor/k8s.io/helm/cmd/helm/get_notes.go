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

	"k8s.io/helm/pkg/helm"
)

var getNotesHelp = `
This command shows notes provided by the chart of a named release.
`

type getNotesCmd struct {
	release string
	out     io.Writer
	client  helm.Interface
	version int32
}

func newGetNotesCmd(client helm.Interface, out io.Writer) *cobra.Command {
	get := &getNotesCmd{
		out:    out,
		client: client,
	}

	cmd := &cobra.Command{
		Use:     "notes [flags] RELEASE_NAME",
		Short:   "displays the notes of the named release",
		Long:    getNotesHelp,
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
	f.Int32Var(&get.version, "revision", 0, "get the notes of the named release with revision")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

func (n *getNotesCmd) run() error {
	res, err := n.client.ReleaseStatus(n.release, helm.StatusReleaseVersion(n.version))
	if err != nil {
		return prettyError(err)
	}

	if len(res.Info.Status.Notes) > 0 {
		fmt.Fprintf(n.out, "NOTES:\n%s\n", res.Info.Status.Notes)
	}
	return nil
}
