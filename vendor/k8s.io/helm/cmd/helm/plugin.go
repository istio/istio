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
	"os/exec"

	"k8s.io/helm/pkg/plugin"

	"github.com/spf13/cobra"
)

const pluginHelp = `
Manage client-side Helm plugins.
`

func newPluginCmd(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "add, list, or remove Helm plugins",
		Long:  pluginHelp,
	}
	cmd.AddCommand(
		newPluginInstallCmd(out),
		newPluginListCmd(out),
		newPluginRemoveCmd(out),
		newPluginUpdateCmd(out),
	)
	return cmd
}

// runHook will execute a plugin hook.
func runHook(p *plugin.Plugin, event string) error {
	hook := p.Metadata.Hooks.Get(event)
	if hook == "" {
		return nil
	}

	prog := exec.Command("sh", "-c", hook)
	// TODO make this work on windows
	// I think its ... ¯\_(ツ)_/¯
	// prog := exec.Command("cmd", "/C", p.Metadata.Hooks.Install())

	debug("running %s hook: %s", event, prog)

	plugin.SetupPluginEnv(settings, p.Metadata.Name, p.Dir)
	prog.Stdout, prog.Stderr = os.Stdout, os.Stderr
	if err := prog.Run(); err != nil {
		if eerr, ok := err.(*exec.ExitError); ok {
			os.Stderr.Write(eerr.Stderr)
			return fmt.Errorf("plugin %s hook for %q exited with error", event, p.Metadata.Name)
		}
		return err
	}
	return nil
}
