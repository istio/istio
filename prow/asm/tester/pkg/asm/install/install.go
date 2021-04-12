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
	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/kube"
	"istio.io/istio/prow/asm/tester/pkg/resource"
	"log"
	"os"
	"path/filepath"
)

// installationType enumerates the installation type.
type installationType string

const (
	basic      installationType = "basic"
	bareMetal                   = "bareMetal"
	multiCloud                  = "multiCloud"
	mcp                         = "mcp"
)

// basicInstallConfig contains all information needed to perform a basic installation.
// required because we have a bad architecture and need to overwrite settings from
// resource.Settings with the per-revision values.
type basicInstallConfig struct {
	pkg      string
	ca       string
	wip      string
	overlay  string
	flags    string
	revision string
	contexts string
}

func (c *installer) install(r *revision.RevisionConfig) error {
	installType := c.installationType()
	log.Printf("🏄 performing ASM installation type: %s\n", installType)
	switch installType {
	case basic:
		return c.installASM(c.generateBasicInstallConfig(r))
	case bareMetal:
		return c.installASMOnBareMetal()
	case multiCloud:
		return c.installASMOnMulticloud()
	case mcp:
		return c.installASMOnManagedControlPlane()
	default:
		return fmt.Errorf("unknown installation type %q passed to ASM installer", installType)
	}
}

// installationType uses existing settings to determine what type of installation
// we should be performing.
func (c *installer) installationType() installationType {
	if c.settings.ControlPlane == string(resource.Unmanaged) {
		switch c.settings.ClusterType {
		case string(resource.GKEOnGCP):
			return basic
		case string(resource.BareMetal):
			return bareMetal
		default:
			return multiCloud
		}
	} else {
		return mcp
	}
}

func (c *installer) installASM(config *basicInstallConfig) error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"install_asm",
		[]string{
			config.pkg,
			config.ca,
			config.wip,
			config.overlay,
			config.flags,
			config.revision,
			config.contexts,
		})
}

func (c *installer) generateBasicInstallConfig(r *revision.RevisionConfig) *basicInstallConfig {
	config := &basicInstallConfig{
		pkg:      filepath.Join(c.settings.RepoRootDir, resource.ConfigDirPath, "kpt-pkg"),
		ca:       c.settings.CA,
		wip:      c.settings.WIP,
		overlay:  "\"\"",
		revision: "\"\"",
		flags:    "\"\"",
		contexts: kube.ContextArr(c.settings.KubectlContexts),
	}
	if r != nil {
		if r.Name != "" {
			config.revision = r.Name
			config.flags = fmt.Sprintf("\" --revision_name %s\"", r.Name)
		}
		if r.CA != "" {
			config.ca = r.CA
		}
		if r.Overlay != "" {
			config.overlay = r.Overlay
		}
	}
	return config
}

func (c *installer) installASMOnBareMetal() error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"install_asm_on_proxied_clusters",
		nil,
		exec.WithAdditionalEnvs([]string{
			fmt.Sprintf("HTTP_PROXY=%s", os.Getenv("MC_HTTP_PROXY")),
			fmt.Sprintf("HTTPS_PROXY=%s", os.Getenv("MC_HTTP_PROXY")),
		}),
	)
}

func (c *installer) installASMOnMulticloud() error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"install_asm_on_multicloud",
		[]string{
			c.settings.CA,
			c.settings.WIP,
		},
		exec.WithAdditionalEnvs([]string{
			fmt.Sprintf("HTTP_PROXY=%s", os.Getenv("MC_HTTP_PROXY")),
			fmt.Sprintf("HTTPS_PROXY=%s", os.Getenv("MC_HTTP_PROXY")),
		}))
}

func (c *installer) installASMOnManagedControlPlane() error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"install_asm_managed_control_plane",
		[]string{
			kube.ContextArr(c.settings.KubectlContexts),
		},
	)
}

// processKubeconfigs should perform steps required after running ASM installation
func (c *installer) processKubeconfigs() error {
	return exec.Dispatch(
		c.settings.RepoRootDir,
		"process_kubeconfigs",
		nil)
}

// preInstall contains all steps required before performing the direct install
func (c *installer) preInstall() error {
	if c.settings.ControlPlane == string(resource.Unmanaged) {
		if err := exec.Dispatch(c.settings.RepoRootDir,
			"prepare_images", nil); err != nil {
			return err
		}
	} else {
		if err := exec.Dispatch(c.settings.RepoRootDir,
			"prepare_images_for_managed_control_plane", nil); err != nil {
			return err
		}
	}

	if err := exec.Dispatch(c.settings.RepoRootDir,
		"build_istioctl",
		nil); err != nil {
		return err
	}

	if c.settings.ControlPlane == string(resource.Unmanaged) {
		if c.settings.CA == string(resource.PrivateCA) {
			if err := exec.Dispatch(
				c.settings.RepoRootDir,
				"setup_private_ca",
				[]string{c.settings.KubectlContexts}); err != nil {
				return err
			}
		}
		if c.settings.WIP == string(resource.HUB) {
			if err := exec.Dispatch(
				c.settings.RepoRootDir,
				"register_clusters_in_hub",
				[]string{
					c.settings.GCRProject,
					kube.ContextArr(c.settings.KubectlContexts),
				}); err != nil {
				return err
			}
		}
		if c.settings.ClusterType != string(resource.GKEOnGCP) {
			if err := exec.Dispatch(
				c.settings.RepoRootDir,
				"create_asm_revision_label",
				nil); err != nil {
				return err
			}
		}
	}
	revLabel := revisionLabel()
	log.Printf("setting ASM_REVISION_LABEL=%s", revLabel)
	os.Setenv("ASM_REVISION_LABEL", revLabel)
	return nil
}

// postInstall contains all steps required after installing
func (c *installer) postInstall() error {
	if err := c.processKubeconfigs(); err != nil {
		return err
	}
	return nil
}
