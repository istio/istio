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

	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/plugin"
	"k8s.io/helm/pkg/plugin/installer"

	"github.com/spf13/cobra"
)

type pluginInstallCmd struct {
	source  string
	version string
	home    helmpath.Home
	out     io.Writer
}

const pluginInstallDesc = `
This command allows you to install a plugin from a url to a VCS repo or a local path.

Example usage:
    $ helm plugin install https://github.com/technosophos/helm-template
`

func newPluginInstallCmd(out io.Writer) *cobra.Command {
	pcmd := &pluginInstallCmd{out: out}
	cmd := &cobra.Command{
		Use:   "install [options] <path|url>...",
		Short: "install one or more Helm plugins",
		Long:  pluginInstallDesc,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return pcmd.complete(args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return pcmd.run()
		},
	}
	cmd.Flags().StringVar(&pcmd.version, "version", "", "specify a version constraint. If this is not specified, the latest version is installed")
	return cmd
}

func (pcmd *pluginInstallCmd) complete(args []string) error {
	if err := checkArgsLength(len(args), "plugin"); err != nil {
		return err
	}
	pcmd.source = args[0]
	pcmd.home = settings.Home
	return nil
}

func (pcmd *pluginInstallCmd) run() error {
	installer.Debug = settings.Debug

	i, err := installer.NewForSource(pcmd.source, pcmd.version, pcmd.home)
	if err != nil {
		return err
	}
	if err := installer.Install(i); err != nil {
		return err
	}

	debug("loading plugin from %s", i.Path())
	p, err := plugin.LoadDir(i.Path())
	if err != nil {
		return err
	}

	if err := runHook(p, plugin.Install); err != nil {
		return err
	}

	fmt.Fprintf(pcmd.out, "Installed plugin: %s\n", p.Metadata.Name)
	return nil
}
