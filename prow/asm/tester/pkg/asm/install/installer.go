// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package install

import (
	"fmt"
	"istio.io/istio/prow/asm/tester/pkg/asm/install/revision"
	"istio.io/istio/prow/asm/tester/pkg/resource"
	"log"
	"path/filepath"
)

// Installer is capable of performing a complete ASM installation.
type Installer interface {
	Install() error
}

var _ Installer = (*installer)(nil)

func Install(settings *resource.Settings) error {
	i := &installer{
		settings: settings,
	}
	return i.Install()
}

// Rather than using this single overloaded installer, we could use a factory method
// that returns an Installer given settings.
type installer struct {
	settings *resource.Settings
}

// Install performs an ASM installation. There one-off steps that have to occur
// both before and after the actual installation.
func (c *installer) Install() error {
	if c.settings.RevisionConfig == "" {
		return c.installInner(&revision.Config{})
	}

	revisionConfigPath := filepath.Join(c.settings.RepoRootDir, resource.ConfigDirPath, "revision-deployer", c.settings.RevisionConfig)
	revisionConfig, err := revision.ParseConfig(revisionConfigPath)
	if err != nil {
		return err
	}
	for _, config := range revisionConfig.Configs {
		log.Printf("🍳 installing ASM revision %q", config.Name)
		if err := c.installInner(&config); err != nil {
			return fmt.Errorf("failed installing revision %q: %w", config.Name, err)
		}
	}

	return nil
}

func (c *installer) installInner(r *revision.Config) error {
	if err := c.preInstall(); err != nil {
		return err
	}
	if err := c.install(r); err != nil {
		return err
	}
	if err := c.postInstall(r); err != nil {
		return err
	}
	return nil
}
