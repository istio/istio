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
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"

	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
	"istio.io/istio/cni/pkg/install-cni/pkg/util"
	"istio.io/pkg/log"
)

type configFiles struct {
	kubeconfigFilepath string
	cniConfigFilepath  string
}

// Run starts the installation process with given configuration
func Run(cfg *config.Config) (err error) {
	var files configFiles

	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func(sigChan chan os.Signal, cancel context.CancelFunc) {
		sig := <-sigChan
		log.Infof("Exit signal received: %s", sig)
		cancel()
	}(sigChan, cancel)

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

		files.cniConfigFilepath, err = createCNIConfigFile(ctx, cfg, saToken)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// Error was caused by interrupt/termination signal
				err = nil
			}
			return
		}

		if cfg.ExecuteOnce {
			err = checkInstall(cfg, files.cniConfigFilepath)
			return
		}
		// Otherwise, keep container alive

		// Create file watcher before checking for installation
		// so that no file modifications are missed while and after checking
		var watcher *fsnotify.Watcher
		var fileModified chan bool
		var errChan chan error
		watcher, fileModified, errChan, err = util.CreateFileWatcher(cfg.MountedCNINetDir)
		if err != nil {
			return
		}

		for {
			if checkErr := checkInstall(cfg, files.cniConfigFilepath); checkErr != nil {
				log.Infof("Invalid configuration. %v", checkErr)
				break
			}
			// Valid configuration; Wait for modifications before checking again
			if waitErr := util.WaitForFileMod(ctx, fileModified, errChan); waitErr != nil {
				if !errors.Is(waitErr, context.Canceled) {
					// Error was not caused by interrupt/termination signal
					err = waitErr
				}
				if closeErr := watcher.Close(); closeErr != nil {
					if err != nil {
						err = errors.Wrap(err, closeErr.Error())
					} else {
						err = closeErr
					}
				}
				return
			}
		}

		if err = watcher.Close(); err != nil {
			log.Warnf("error closing file watcher: %v", err)
		}
		log.Info("Restarting...")
	}
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

func cleanup(cfg *config.Config, files configFiles) error {
	log.Info("Cleaning up.")
	if len(files.cniConfigFilepath) > 0 && fileutil.Exist(files.cniConfigFilepath) {
		if cfg.ChainedCNIPlugin {
			log.Infof("Removing Istio CNI config from CNI config file: %s", files.cniConfigFilepath)

			// Read JSON from CNI config file
			cniConfigMap, err := util.ReadCNIConfigMap(files.cniConfigFilepath)
			if err != nil {
				return err
			}
			// Find Istio CNI and remove from plugin list
			plugins, err := util.GetPlugins(cniConfigMap)
			if err != nil {
				return errors.Wrap(err, files.cniConfigFilepath)
			}
			for i, rawPlugin := range plugins {
				plugin, err := util.GetPlugin(rawPlugin)
				if err != nil {
					return errors.Wrap(err, files.cniConfigFilepath)
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
			if err = util.WriteAtomically(files.cniConfigFilepath, cniConfig, os.FileMode(0644)); err != nil {
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
