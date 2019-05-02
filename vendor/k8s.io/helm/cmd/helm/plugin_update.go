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
	"strings"

	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/plugin"
	"k8s.io/helm/pkg/plugin/installer"

	"github.com/spf13/cobra"
)

type pluginUpdateCmd struct {
	names []string
	home  helmpath.Home
	out   io.Writer
}

func newPluginUpdateCmd(out io.Writer) *cobra.Command {
	pcmd := &pluginUpdateCmd{out: out}
	cmd := &cobra.Command{
		Use:   "update <plugin>...",
		Short: "update one or more Helm plugins",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return pcmd.complete(args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return pcmd.run()
		},
	}
	return cmd
}

func (pcmd *pluginUpdateCmd) complete(args []string) error {
	if len(args) == 0 {
		return errors.New("please provide plugin name to update")
	}
	pcmd.names = args
	pcmd.home = settings.Home
	return nil
}

func (pcmd *pluginUpdateCmd) run() error {
	installer.Debug = settings.Debug
	debug("loading installed plugins from %s", settings.PluginDirs())
	plugins, err := findPlugins(settings.PluginDirs())
	if err != nil {
		return err
	}
	var errorPlugins []string

	for _, name := range pcmd.names {
		if found := findPlugin(plugins, name); found != nil {
			if err := updatePlugin(found, pcmd.home); err != nil {
				errorPlugins = append(errorPlugins, fmt.Sprintf("Failed to update plugin %s, got error (%v)", name, err))
			} else {
				fmt.Fprintf(pcmd.out, "Updated plugin: %s\n", name)
			}
		} else {
			errorPlugins = append(errorPlugins, fmt.Sprintf("Plugin: %s not found", name))
		}
	}
	if len(errorPlugins) > 0 {
		return fmt.Errorf(strings.Join(errorPlugins, "\n"))
	}
	return nil
}

func updatePlugin(p *plugin.Plugin, home helmpath.Home) error {
	exactLocation, err := filepath.EvalSymlinks(p.Dir)
	if err != nil {
		return err
	}
	absExactLocation, err := filepath.Abs(exactLocation)
	if err != nil {
		return err
	}

	i, err := installer.FindSource(absExactLocation, home)
	if err != nil {
		return err
	}
	if err := installer.Update(i); err != nil {
		return err
	}

	debug("loading plugin from %s", i.Path())
	updatedPlugin, err := plugin.LoadDir(i.Path())
	if err != nil {
		return err
	}

	return runHook(updatedPlugin, plugin.Update)
}
