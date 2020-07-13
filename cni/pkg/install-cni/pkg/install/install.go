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
	"encoding/json"
	"fmt"
	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
	"io/ioutil"
	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
	"istio.io/istio/cni/pkg/install-cni/pkg/util"
	"istio.io/pkg/log"
	"os"
	"path/filepath"
)

type configFiles struct {
	kubeconfigFilepath string
	cniConfigFilepath  string
}

// Run starts the installation process with given configuration
func Run(cfg *config.Config) (err error) {
	var files configFiles

	defer func() {
		if cleanErr := cleanup(cfg, files); cleanErr != nil {
			if err != nil {
				err = errors.Wrap(err, cleanErr.Error())
			} else {
				err = cleanErr
			}
		}
	}()

	for {
		if err = copyBinaries(cfg.UpdateCNIBinaries, cfg.SkipCNIBinaries); err != nil {
			return
		}

		var saToken string
		saToken, err = readServiceAccountToken()
		if err != nil {
			return
		}

		files.kubeconfigFilepath, err = createKubeconfigFile(cfg, saToken)
		if err != nil {
			return
		}

		files.cniConfigFilepath, err = createCNIConfigFile(cfg, saToken)
		if err != nil {
			return
		}

		if !cfg.Sleep {
			err = checkInstall(cfg, files.cniConfigFilepath)
			return
		}
		// Otherwise, keep container alive

		// Create file watcher before checking for installation
		ctx := context.Background()
		var watcher *fsnotify.Watcher
		var fileModified chan bool
		var errChan chan error
		watcher, fileModified, errChan, err = util.CreateFileWatcher(cfg.MountedCNINetDir)
		if err != nil {
			return
		}

		for {
			if err = checkInstall(cfg, files.cniConfigFilepath); err != nil {
				log.Infof("Invalid configuration. %v", err)
				break
			}
			// Valid configuration; Wait for modifications before checking again
			if err = util.WaitForFileMod(ctx, fileModified, errChan); err != nil {
				return
			}
		}

		if err = watcher.Close(); err != nil {
			return
		}
		log.Info("Restarting script...")
	}
}

func readServiceAccountToken() (string, error) {
	saToken := constants.ServiceAccountPath + "/token"
	if !fileutil.Exist(saToken) {
		return "", fmt.Errorf("SA Token file %s does not exist. Is this not running within a pod?", saToken)
	}

	token, err := ioutil.ReadFile(saToken)
	if err != nil {
		return "", err
	}

	return string(token), nil
}

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
		cniConfigMap, err := getCNIConfigMap(cniConfigFilepath)
		if err != nil {
			return err
		}

		plugins := cniConfigMap["plugins"].([]interface{})
		for _, plugin := range plugins {
			if plugin.(map[string]interface{})["type"] == "istio-cni" {
				return nil
			}
		}

		return fmt.Errorf("istio-cni CNI config removed from CNI config file: %s", cniConfigFilepath)
	} else {
		// Verify that Istio CNI config exists as a standalone plugin
		cniConfigMap, err := getCNIConfigMap(cniConfigFilepath)
		if err != nil {
			return err
		}

		if cniConfigMap["type"] != "istio-cni" {
			return fmt.Errorf("istio-cni CNI config file modified: %s", cniConfigFilepath)
		}
		return nil
	}
}

func cleanup(cfg *config.Config, files configFiles) error {
	log.Info("Cleaning up.")
	if len(files.cniConfigFilepath) > 0 && fileutil.Exist(files.cniConfigFilepath) {
		if cfg.ChainedCNIPlugin {
			log.Infof("Removing Istio CNI config from CNI config file: %s", files.cniConfigFilepath)
			// Read JSON from CNI config file
			cniConfigMap, err := getCNIConfigMap(files.cniConfigFilepath)
			if err != nil {
				return err
			}

			// Find Istio CNI and remove from plugin list
			plugins := cniConfigMap["plugins"].([]interface{})
			for i, plugin := range plugins {
				if plugin.(map[string]interface{})["type"] == "istio-cni" {
					cniConfigMap["plugins"] = append(plugins[:i], plugins[i+1:]...)
					break
				}
			}

			cniConfig, err := json.MarshalIndent(cniConfigMap, "", "  ")
			if err != nil {
				return err
			}

			// Write CNI config file atomically
			if err = util.WriteAtomically(files.cniConfigFilepath, cniConfig, 0644); err != nil {
				return err
			}
		} else {
			log.Infof("Removing Istio CNI config file: %s", files.cniConfigFilepath)
			if err := os.Remove(files.cniConfigFilepath); err != nil {
				return err
			}
		}
	}

	if len(files.kubeconfigFilepath) > 0 && fileutil.Exist(files.kubeconfigFilepath) {
		log.Infof("Removing Istio CNI kubeconfig file: %s", files.kubeconfigFilepath)
		if err := os.Remove(files.kubeconfigFilepath); err != nil {
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

func getCNIConfigMap(path string) (map[string]interface{}, error) {
	cniConfig, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cniConfigMap map[string]interface{}
	if err = json.Unmarshal(cniConfig, &cniConfigMap); err != nil {
		return nil, errors.Wrap(err, path)
	}

	return cniConfigMap, nil
}
