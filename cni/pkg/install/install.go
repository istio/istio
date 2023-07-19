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
	"os"
	"path/filepath"
	"sync/atomic"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

var installLog = log.RegisterScope("install", "CNI install")

type Installer struct {
	cfg                *config.InstallConfig
	isReady            *atomic.Value
	kubeconfigFilepath string
	cniConfigFilepath  string
}

// NewInstaller returns an instance of Installer with the given config
func NewInstaller(cfg *config.InstallConfig, isReady *atomic.Value) *Installer {
	return &Installer{
		cfg:                cfg,
		kubeconfigFilepath: filepath.Join(cfg.MountedCNINetDir, cfg.KubeconfigFilename),
		isReady:            isReady,
	}
}

func (in *Installer) install(ctx context.Context) (sets.Set[string], error) {
	copiedFiles, err := copyBinaries(in.cfg.CNIBinSourceDir, in.cfg.CNIBinTargetDirs)
	if err != nil {
		cniInstalls.With(resultLabel.Value(resultCopyBinariesFailure)).Increment()
		return copiedFiles, fmt.Errorf("copy binaries: %v", err)
	}

	if err := writeKubeConfigFile(in.cfg); err != nil {
		cniInstalls.With(resultLabel.Value(resultCreateKubeConfigFailure)).Increment()
		return copiedFiles, fmt.Errorf("write kubeconfig: %v", err)
	}

	cfgPath, err := createCNIConfigFile(ctx, in.cfg)
	if err != nil {
		cniInstalls.With(resultLabel.Value(resultCreateCNIConfigFailure)).Increment()
		return copiedFiles, fmt.Errorf("create CNI config file: %v", err)
	}
	in.cniConfigFilepath = cfgPath

	return copiedFiles, nil
}

// Run starts the installation process, verifies the configuration, then sleeps.
// If an invalid configuration is detected, the installation process will restart to restore a valid state.
func (in *Installer) Run(ctx context.Context) error {
	installedBins, err := in.install(ctx)
	if err != nil {
		return err
	}
	installLog.Info("Installation succeed, start watching for re-installation.")

	for {
		if err := in.sleepCheckInstall(ctx, installedBins); err != nil {
			return err
		}

		installLog.Info("Detect changes to the CNI configuration and binaries, attempt reinstalling...")
		// We don't support (or want) to silently (re)deploy any binaries that were not in the initial "snapshot"
		// so we intentionally discard/do not update the list of installedBins on redeploys.
		if _, err := in.install(ctx); err != nil {
			return err
		}
		installLog.Info("CNI configuration and binaries reinstalled.")
	}
}

// Cleanup remove Istio CNI's config, kubeconfig file, and binaries.
func (in *Installer) Cleanup() error {
	installLog.Info("Cleaning up.")
	if len(in.cniConfigFilepath) > 0 && file.Exists(in.cniConfigFilepath) {
		if in.cfg.ChainedCNIPlugin {
			installLog.Infof("Removing Istio CNI config from CNI config file: %s", in.cniConfigFilepath)

			// Read JSON from CNI config file
			cniConfigMap, err := util.ReadCNIConfigMap(in.cniConfigFilepath)
			if err != nil {
				return err
			}
			// Find Istio CNI and remove from plugin list
			plugins, err := util.GetPlugins(cniConfigMap)
			if err != nil {
				return fmt.Errorf("%s: %w", in.cniConfigFilepath, err)
			}
			for i, rawPlugin := range plugins {
				plugin, err := util.GetPlugin(rawPlugin)
				if err != nil {
					return fmt.Errorf("%s: %w", in.cniConfigFilepath, err)
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
			if err = file.AtomicWrite(in.cniConfigFilepath, cniConfig, os.FileMode(0o644)); err != nil {
				return err
			}
		} else {
			installLog.Infof("Removing Istio CNI config file: %s", in.cniConfigFilepath)
			if err := os.Remove(in.cniConfigFilepath); err != nil {
				return err
			}
		}
	}

	if len(in.kubeconfigFilepath) > 0 && file.Exists(in.kubeconfigFilepath) {
		installLog.Infof("Removing Istio CNI kubeconfig file: %s", in.kubeconfigFilepath)
		if err := os.Remove(in.kubeconfigFilepath); err != nil {
			return err
		}
	}

	for _, targetDir := range in.cfg.CNIBinTargetDirs {
		if istioCNIBin := filepath.Join(targetDir, "istio-cni"); file.Exists(istioCNIBin) {
			installLog.Infof("Removing binary: %s", istioCNIBin)
			if err := os.Remove(istioCNIBin); err != nil {
				return err
			}
		}
	}
	return nil
}

// sleepCheckInstall verifies the configuration then blocks until an invalid configuration is detected, and return nil.
// If an error occurs or context is canceled, the function will return the error.
// Returning from this function will set the pod to "NotReady".
func (in *Installer) sleepCheckInstall(ctx context.Context, installedBinFiles sets.Set[string]) error {
	// Watch our specific binaries, in each configured binary dir.
	// We may or may not be the only CNI plugin in play, and if we are not
	// we shouldn't fire events for binaries that are not ours.
	var binPaths []string
	for _, bindir := range in.cfg.CNIBinTargetDirs {
		for _, binary := range installedBinFiles.UnsortedList() {
			binPaths = append(binPaths, filepath.Join(bindir, binary))
		}
	}
	targets := append(
		binPaths,
		in.cfg.MountedCNINetDir,
		constants.ServiceAccountPath,
	)
	// Create file watcher before checking for installation
	// so that no file modifications are missed while and after checking
	// note: we create a file watcher for each invocation, otherwise when we write to the directories
	// we would get infinite looping of events
	//
	// Additionally, fsnotify will lose existing watches on atomic copies (due to overwrite/rename),
	// so we have to re-watch after re-copy to make sure we always have fresh watches.
	watcher, err := util.CreateFileWatcher(targets...)
	if err != nil {
		return err
	}
	defer func() {
		SetNotReady(in.isReady)
		watcher.Close()
	}()

	for {
		if err := checkInstall(in.cfg, in.cniConfigFilepath); err != nil {
			// Pod set to "NotReady" due to invalid configuration
			installLog.Infof("Invalid configuration. %v", err)
			return nil
		}
		// Check if file has been modified or if an error has occurred during checkInstall before setting isReady to true
		select {
		case <-watcher.Events:
			return nil
		case err := <-watcher.Errors:
			return err
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Valid configuration; set isReady to true and wait for modifications before checking again
			SetReady(in.isReady)
			cniInstalls.With(resultLabel.Value(resultSuccess)).Increment()
			// Pod set to "NotReady" before termination
			return watcher.Wait(ctx)
		}
	}
}

// checkInstall returns an error if an invalid CNI configuration is detected
func checkInstall(cfg *config.InstallConfig, cniConfigFilepath string) error {
	defaultCNIConfigFilename, err := getDefaultCNINetwork(cfg.MountedCNINetDir)
	if err != nil {
		return err
	}
	defaultCNIConfigFilepath := filepath.Join(cfg.MountedCNINetDir, defaultCNIConfigFilename)
	if defaultCNIConfigFilepath != cniConfigFilepath {
		if len(cfg.CNIConfName) > 0 || !cfg.ChainedCNIPlugin {
			// Install was run with overridden CNI config file so don't error out on preempt check
			// Likely the only use for this is testing the script
			installLog.Warnf("CNI config file %s preempted by %s", cniConfigFilepath, defaultCNIConfigFilepath)
		} else {
			return fmt.Errorf("CNI config file %s preempted by %s", cniConfigFilepath, defaultCNIConfigFilepath)
		}
	}

	if !file.Exists(cniConfigFilepath) {
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
			return fmt.Errorf("%s: %w", cniConfigFilepath, err)
		}
		for _, rawPlugin := range plugins {
			plugin, err := util.GetPlugin(rawPlugin)
			if err != nil {
				return fmt.Errorf("%s: %w", cniConfigFilepath, err)
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
