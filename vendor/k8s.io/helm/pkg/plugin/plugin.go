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

package plugin // import "k8s.io/helm/pkg/plugin"

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	helm_env "k8s.io/helm/pkg/helm/environment"

	"github.com/ghodss/yaml"
)

const pluginFileName = "plugin.yaml"

// Downloaders represents the plugins capability if it can retrieve
// charts from special sources
type Downloaders struct {
	// Protocols are the list of schemes from the charts URL.
	Protocols []string `json:"protocols"`
	// Command is the executable path with which the plugin performs
	// the actual download for the corresponding Protocols
	Command string `json:"command"`
}

// Metadata describes a plugin.
//
// This is the plugin equivalent of a chart.Metadata.
type Metadata struct {
	// Name is the name of the plugin
	Name string `json:"name"`

	// Version is a SemVer 2 version of the plugin.
	Version string `json:"version"`

	// Usage is the single-line usage text shown in help
	Usage string `json:"usage"`

	// Description is a long description shown in places like `helm help`
	Description string `json:"description"`

	// Command is the command, as a single string.
	//
	// The command will be passed through environment expansion, so env vars can
	// be present in this command. Unless IgnoreFlags is set, this will
	// also merge the flags passed from Helm.
	//
	// Note that command is not executed in a shell. To do so, we suggest
	// pointing the command to a shell script.
	Command string `json:"command"`

	// IgnoreFlags ignores any flags passed in from Helm
	//
	// For example, if the plugin is invoked as `helm --debug myplugin`, if this
	// is false, `--debug` will be appended to `--command`. If this is true,
	// the `--debug` flag will be discarded.
	IgnoreFlags bool `json:"ignoreFlags"`

	// UseTunnel indicates that this command needs a tunnel.
	// Setting this will cause a number of side effects, such as the
	// automatic setting of HELM_HOST.
	UseTunnel bool `json:"useTunnel"`

	// Hooks are commands that will run on events.
	Hooks Hooks

	// Downloaders field is used if the plugin supply downloader mechanism
	// for special protocols.
	Downloaders []Downloaders `json:"downloaders"`
}

// Plugin represents a plugin.
type Plugin struct {
	// Metadata is a parsed representation of a plugin.yaml
	Metadata *Metadata
	// Dir is the string path to the directory that holds the plugin.
	Dir string
}

// PrepareCommand takes a Plugin.Command and prepares it for execution.
//
// It merges extraArgs into any arguments supplied in the plugin. It
// returns the name of the command and an args array.
//
// The result is suitable to pass to exec.Command.
func (p *Plugin) PrepareCommand(extraArgs []string) (string, []string) {
	parts := strings.Split(os.ExpandEnv(p.Metadata.Command), " ")
	main := parts[0]
	baseArgs := []string{}
	if len(parts) > 1 {
		baseArgs = parts[1:]
	}
	if !p.Metadata.IgnoreFlags {
		baseArgs = append(baseArgs, extraArgs...)
	}
	return main, baseArgs
}

// LoadDir loads a plugin from the given directory.
func LoadDir(dirname string) (*Plugin, error) {
	data, err := ioutil.ReadFile(filepath.Join(dirname, pluginFileName))
	if err != nil {
		return nil, err
	}

	plug := &Plugin{Dir: dirname}
	if err := yaml.Unmarshal(data, &plug.Metadata); err != nil {
		return nil, err
	}
	return plug, nil
}

// LoadAll loads all plugins found beneath the base directory.
//
// This scans only one directory level.
func LoadAll(basedir string) ([]*Plugin, error) {
	plugins := []*Plugin{}
	// We want basedir/*/plugin.yaml
	scanpath := filepath.Join(basedir, "*", pluginFileName)
	matches, err := filepath.Glob(scanpath)
	if err != nil {
		return plugins, err
	}

	if matches == nil {
		return plugins, nil
	}

	for _, yaml := range matches {
		dir := filepath.Dir(yaml)
		p, err := LoadDir(dir)
		if err != nil {
			return plugins, err
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

// FindPlugins returns a list of YAML files that describe plugins.
func FindPlugins(plugdirs string) ([]*Plugin, error) {
	found := []*Plugin{}
	// Let's get all UNIXy and allow path separators
	for _, p := range filepath.SplitList(plugdirs) {
		matches, err := LoadAll(p)
		if err != nil {
			return matches, err
		}
		found = append(found, matches...)
	}
	return found, nil
}

// SetupPluginEnv prepares os.Env for plugins. It operates on os.Env because
// the plugin subsystem itself needs access to the environment variables
// created here.
func SetupPluginEnv(settings helm_env.EnvSettings,
	shortName, base string) {
	for key, val := range map[string]string{
		"HELM_PLUGIN_NAME": shortName,
		"HELM_PLUGIN_DIR":  base,
		"HELM_BIN":         os.Args[0],

		// Set vars that may not have been set, and save client the
		// trouble of re-parsing.
		"HELM_PLUGIN": settings.PluginDirs(),
		"HELM_HOME":   settings.Home.String(),

		// Set vars that convey common information.
		"HELM_PATH_REPOSITORY":       settings.Home.Repository(),
		"HELM_PATH_REPOSITORY_FILE":  settings.Home.RepositoryFile(),
		"HELM_PATH_CACHE":            settings.Home.Cache(),
		"HELM_PATH_LOCAL_REPOSITORY": settings.Home.LocalRepository(),
		"HELM_PATH_STARTER":          settings.Home.Starters(),

		"TILLER_HOST":      settings.TillerHost,
		"TILLER_NAMESPACE": settings.TillerNamespace,
	} {
		os.Setenv(key, val)
	}

	if settings.Debug {
		os.Setenv("HELM_DEBUG", "1")
	}
}
