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
	"os"
	"strings"

	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/plugin"

	"github.com/spf13/cobra"
)

type pluginRemoveCmd struct {
	names []string
	home  helmpath.Home
	out   io.Writer
}

func newPluginRemoveCmd(out io.Writer) *cobra.Command {
	pcmd := &pluginRemoveCmd{out: out}
	cmd := &cobra.Command{
		Use:   "remove <plugin>...",
		Short: "remove one or more Helm plugins",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return pcmd.complete(args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return pcmd.run()
		},
	}
	return cmd
}

func (pcmd *pluginRemoveCmd) complete(args []string) error {
	if len(args) == 0 {
		return errors.New("please provide plugin name to remove")
	}
	pcmd.names = args
	pcmd.home = settings.Home
	return nil
}

func (pcmd *pluginRemoveCmd) run() error {
	debug("loading installed plugins from %s", settings.PluginDirs())
	plugins, err := findPlugins(settings.PluginDirs())
	if err != nil {
		return err
	}
	var errorPlugins []string
	for _, name := range pcmd.names {
		if found := findPlugin(plugins, name); found != nil {
			if err := removePlugin(found); err != nil {
				errorPlugins = append(errorPlugins, fmt.Sprintf("Failed to remove plugin %s, got error (%v)", name, err))
			} else {
				fmt.Fprintf(pcmd.out, "Removed plugin: %s\n", name)
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

func removePlugin(p *plugin.Plugin) error {
	if err := os.Remove(p.Dir); err != nil {
		return err
	}
	return runHook(p, plugin.Delete)
}

func findPlugin(plugins []*plugin.Plugin, name string) *plugin.Plugin {
	for _, p := range plugins {
		if p.Metadata.Name == name {
			return p
		}
	}
	return nil
}
