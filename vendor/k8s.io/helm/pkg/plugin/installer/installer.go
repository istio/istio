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

package installer

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"k8s.io/helm/pkg/helm/helmpath"
)

// ErrMissingMetadata indicates that plugin.yaml is missing.
var ErrMissingMetadata = errors.New("plugin metadata (plugin.yaml) missing")

// Debug enables verbose output.
var Debug bool

// Installer provides an interface for installing helm client plugins.
type Installer interface {
	// Install adds a plugin to $HELM_HOME.
	Install() error
	// Path is the directory of the installed plugin.
	Path() string
	// Update updates a plugin to $HELM_HOME.
	Update() error
}

// Install installs a plugin to $HELM_HOME.
func Install(i Installer) error {
	if _, pathErr := os.Stat(path.Dir(i.Path())); os.IsNotExist(pathErr) {
		return errors.New(`plugin home "$HELM_HOME/plugins" does not exist`)
	}

	if _, pathErr := os.Stat(i.Path()); !os.IsNotExist(pathErr) {
		return errors.New("plugin already exists")
	}

	return i.Install()
}

// Update updates a plugin in $HELM_HOME.
func Update(i Installer) error {
	if _, pathErr := os.Stat(i.Path()); os.IsNotExist(pathErr) {
		return errors.New("plugin does not exist")
	}

	return i.Update()
}

// NewForSource determines the correct Installer for the given source.
func NewForSource(source, version string, home helmpath.Home) (Installer, error) {
	// Check if source is a local directory
	if isLocalReference(source) {
		return NewLocalInstaller(source, home)
	} else if isRemoteHTTPArchive(source) {
		return NewHTTPInstaller(source, home)
	}
	return NewVCSInstaller(source, version, home)
}

// FindSource determines the correct Installer for the given source.
func FindSource(location string, home helmpath.Home) (Installer, error) {
	installer, err := existingVCSRepo(location, home)
	if err != nil && err.Error() == "Cannot detect VCS" {
		return installer, errors.New("cannot get information about plugin source")
	}
	return installer, err
}

// isLocalReference checks if the source exists on the filesystem.
func isLocalReference(source string) bool {
	_, err := os.Stat(source)
	return err == nil
}

// isRemoteHTTPArchive checks if the source is a http/https url and is an archive
func isRemoteHTTPArchive(source string) bool {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		for suffix := range Extractors {
			if strings.HasSuffix(source, suffix) {
				return true
			}
		}
	}
	return false
}

// isPlugin checks if the directory contains a plugin.yaml file.
func isPlugin(dirname string) bool {
	_, err := os.Stat(filepath.Join(dirname, "plugin.yaml"))
	return err == nil
}

func debug(format string, args ...interface{}) {
	if Debug {
		format = fmt.Sprintf("[debug] %s\n", format)
		fmt.Printf(format, args...)
	}
}
