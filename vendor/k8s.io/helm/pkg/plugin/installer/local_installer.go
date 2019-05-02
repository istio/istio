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

package installer // import "k8s.io/helm/pkg/plugin/installer"

import (
	"fmt"
	"path/filepath"

	"k8s.io/helm/pkg/helm/helmpath"
)

// LocalInstaller installs plugins from the filesystem.
type LocalInstaller struct {
	base
}

// NewLocalInstaller creates a new LocalInstaller.
func NewLocalInstaller(source string, home helmpath.Home) (*LocalInstaller, error) {
	src, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("unable to get absolute path to plugin: %v", err)
	}
	i := &LocalInstaller{
		base: newBase(src, home),
	}
	return i, nil
}

// Install creates a symlink to the plugin directory in $HELM_HOME.
//
// Implements Installer.
func (i *LocalInstaller) Install() error {
	if !isPlugin(i.Source) {
		return ErrMissingMetadata
	}
	return i.link(i.Source)
}

// Update updates a local repository
func (i *LocalInstaller) Update() error {
	debug("local repository is auto-updated")
	return nil
}
