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

const getHooksHelp = `
This command downloads hooks for a given release.

Hooks are formatted in YAML and separated by the YAML '---\n' separator.
`

type getHooksCmd struct {
	release string
	out     io.Writer
	client  helm.Interface
	version int32
}

func newGetHooksCmd(client helm.Interface, out io.Writer) *cobra.Command {
	ghc := &getHooksCmd{
		out:    out,
		client: client,
	}
	cmd := &cobra.Command{
		Use:     "hooks [flags] RELEASE_NAME",
		Short:   "download all hooks for a named release",
		Long:    getHooksHelp,
		PreRunE: func(_ *cobra.Command, _ []string) error { return setupConnection() },
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errReleaseRequired
			}
			ghc.release = args[0]
			ghc.client = ensureHelmClient(ghc.client)
			return ghc.run()
		},
	}
	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.Int32Var(&ghc.version, "revision", 0, "get the named release with revision")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

func (g *getHooksCmd) run() error {
	res, err := g.client.ReleaseContent(g.release, helm.ContentReleaseVersion(g.version))
	if err != nil {
		fmt.Fprintln(g.out, g.release)
		return prettyError(err)
	}

	for _, hook := range res.Release.Hooks {
		fmt.Fprintf(g.out, "---\n# %s\n%s\n", hook.Name, hook.Manifest)
	}
	return nil
}
