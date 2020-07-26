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
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/pkg/errors"

	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
	"istio.io/istio/cni/pkg/install-cni/pkg/util"
	"istio.io/pkg/log"
)

type Installer struct {
	cfg                *config.Config
	saToken            string
	kubeconfigFilepath string
	cniConfigFilepath  string
}

// NewInstaller returns an instance of Installer with the given config
func NewInstaller(cfg *config.Config) *Installer {
	return &Installer{
		cfg: cfg,
	}
}

// Run starts the installation process, verifies the configuration, then sleeps.
// If an invalid configuration is detected, the installation process will restart to restore a valid state.
func (in *Installer) Run(ctx context.Context) (err error) {
	for {
		if err = copyBinaries(in.cfg.UpdateCNIBinaries, in.cfg.SkipCNIBinaries); err != nil {
			return
		}

		if in.saToken, err = readServiceAccountToken(); err != nil {
			return
		}

		if in.kubeconfigFilepath, err = createKubeconfigFile(in.cfg, in.saToken); err != nil {
			return
		}

		if in.cniConfigFilepath, err = createCNIConfigFile(ctx, in.cfg, in.saToken); err != nil {
			return
		}

		if err = sleepCheckInstall(ctx, in.cfg, in.cniConfigFilepath); err != nil {
			return
		}

		log.Info("Restarting...")
	}
}

// Cleanup remove Istio CNI's config, kubeconfig file, and binaries.
func (in *Installer) Cleanup() error {
	log.Info("Cleaning up.")
	if len(in.cniConfigFilepath) > 0 && fileutil.Exist(in.cniConfigFilepath) {
		if in.cfg.ChainedCNIPlugin {
			log.Infof("Removing Istio CNI config from CNI config file: %s", in.cniConfigFilepath)

			// Read JSON from CNI config file
			cniConfigMap, err := util.ReadCNIConfigMap(in.cniConfigFilepath)
			if err != nil {
				return err
			}
			// Find Istio CNI and remove from plugin list
			plugins, err := util.GetPlugins(cniConfigMap)
			if err != nil {
				return errors.Wrap(err, in.cniConfigFilepath)
			}
			for i, rawPlugin := range plugins {
				plugin, err := util.GetPlugin(rawPlugin)
				if err != nil {
					return errors.Wrap(err, in.cniConfigFilepath)
				}
				if plugin["type"] == "istio-cni" {
					cniConfigMap["plugins"] = append(plugins[:i], plugins[i+1:]...)
					break
				}
			}

			cniConfig, err := util.MarshalCNIConfig(cniConfigMap)
			if err != nil {
				return err
			}
			if err = util.AtomicWrite(in.cniConfigFilepath, cniConfig, os.FileMode(0644)); err != nil {
				return err
			}
		} else {
			log.Infof("Removing Istio CNI config file: %s", in.cniConfigFilepath)
			if err := os.Remove(in.cniConfigFilepath); err != nil {
				return err
			}
		}
	}

	if len(in.kubeconfigFilepath) > 0 && fileutil.Exist(in.kubeconfigFilepath) {
		log.Infof("Removing Istio CNI kubeconfig file: %s", in.kubeconfigFilepath)
		if err := os.Remove(in.kubeconfigFilepath); err != nil {
			return err
		}
	}

	log.Info("Removing existing binaries")
	if istioCNIBin := filepath.Join(constants.HostCNIBinDir, "istio-cni"); fileutil.Exist(istioCNIBin) {
		if err := os.Remove(istioCNIBin); err != nil {
			return err
		}
	}
	if istioIptablesBin := filepath.Join(constants.HostCNIBinDir, "istio-iptables"); fileutil.Exist(istioIptablesBin) {
		if err := os.Remove(istioIptablesBin); err != nil {
			return err
		}
	}
	return nil
}

func readServiceAccountToken() (string, error) {
	saToken := constants.ServiceAccountPath + "/token"
	if !fileutil.Exist(saToken) {
		return "", fmt.Errorf("service account token file %s does not exist. Is this not running within a pod?", saToken)
	}

	token, err := ioutil.ReadFile(saToken)
	if err != nil {
		return "", err
	}

	return string(token), nil
}

// sleepCheckInstall blocks until an invalid configuration is detected and returns nil
// or returns an error if an error occurs or context is canceled.
func sleepCheckInstall(ctx context.Context, cfg *config.Config, cniConfigFilepath string) error {
	// Create file watcher before checking for installation
	// so that no file modifications are missed while and after checking
	watcher, fileModified, errChan, err := util.CreateFileWatcher(cfg.MountedCNINetDir)
	if err != nil {
		return err
	}
	defer func() {
		_ = watcher.Close()
	}()

	for {
		if checkErr := checkInstall(cfg, cniConfigFilepath); checkErr != nil {
			log.Infof("Invalid configuration. %v", checkErr)
			return nil
		}
		// Valid configuration; Wait for modifications before checking again
		if err = util.WaitForFileMod(ctx, fileModified, errChan); err != nil {
			return err
		}
	}
}

// checkInstall returns an error if an invalid CNI configuration is detected
func checkInstall(cfg *config.Config, cniConfigFilepath string) error {
	defaultCNIConfigFilename, err := getDefaultCNINetwork(cfg.MountedCNINetDir)
	if err != nil {
		return err
	}
	defaultCNIConfigFilepath := filepath.Join(cfg.MountedCNINetDir, defaultCNIConfigFilename)
	if defaultCNIConfigFilepath != cniConfigFilepath {
		if len(cfg.CNIConfName) > 0 {
			// Install was run with overridden CNI config file so don't error out on preempt check
			// Likely the only use for this is testing the script
			log.Warnf("CNI config file %s preempted by %s", cniConfigFilepath, defaultCNIConfigFilepath)
		} else {
			return fmt.Errorf("CNI config file %s preempted by %s", cniConfigFilepath, defaultCNIConfigFilepath)
		}
	}

	if !fileutil.Exist(cniConfigFilepath) {
		return fmt.Errorf("CNI config file removed: %s", cniConfigFilepath)
	}

	if cfg.ChainedCNIPlugin {
		// Verify that Istio CNI config exists in the CNI config plugin list
		cniConfigMap, err := util.ReadCNIConfigMap(cniConfigFilepath)
		if err != nil {
			return err
		}
		plugins, err := util.GetPlugins(cniConfigMap)
		if err != nil {
			return errors.Wrap(err, cniConfigFilepath)
		}
		for _, rawPlugin := range plugins {
			plugin, err := util.GetPlugin(rawPlugin)
			if err != nil {
				return errors.Wrap(err, cniConfigFilepath)
			}
			if plugin["type"] == "istio-cni" {
				return nil
			}
		}

		return fmt.Errorf("istio-cni CNI config removed from CNI config file: %s", cniConfigFilepath)
	}
	// Verify that Istio CNI config exists as a standalone plugin
	cniConfigMap, err := util.ReadCNIConfigMap(cniConfigFilepath)
	if err != nil {
		return err
	}

	if cniConfigMap["type"] != "istio-cni" {
		return fmt.Errorf("istio-cni CNI config file modified: %s", cniConfigFilepath)
	}
	return nil
}
