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

func (in *Installer) installAll(ctx context.Context) (sets.String, error) {
	// Install binaries
	// Currently we _always_ do this, since the binaries do not live in a shared location
	// and we harm no one by doing so.
	copiedFiles, err := copyBinaries(in.cfg.CNIBinSourceDir, in.cfg.CNIBinTargetDirs)
	if err != nil {
		cniInstalls.With(resultLabel.Value(resultCopyBinariesFailure)).Increment()
		return copiedFiles, fmt.Errorf("copy binaries: %v", err)
	}

	// Install kubeconfig (if needed) - we write/update this in the shared node CNI netdir,
	// which may be watched by other CNIs, and so we don't want to trigger writes to this file
	// unless it's missing or the contents are not what we expect.
	if err := maybeWriteKubeConfigFile(in.cfg); err != nil {
		cniInstalls.With(resultLabel.Value(resultCreateKubeConfigFailure)).Increment()
		return copiedFiles, fmt.Errorf("write kubeconfig: %v", err)
	}

	// Install CNI netdir config (if needed) - we write/update this in the shared node CNI netdir,
	// which may be watched by other CNIs, and so we don't want to trigger writes to this file
	// unless it's missing or the contents are not what we expect.
	if err := checkValidCNIConfig(in.cfg, in.cniConfigFilepath); err != nil {
		installLog.Infof("missing (or invalid) configuration detected, (re)writing CNI config file at %s", in.cniConfigFilepath)
		cfgPath, err := createCNIConfigFile(ctx, in.cfg)
		if err != nil {
			cniInstalls.With(resultLabel.Value(resultCreateCNIConfigFailure)).Increment()
			return copiedFiles, fmt.Errorf("create CNI config file: %v", err)
		}
		in.cniConfigFilepath = cfgPath
	} else {
		installLog.Infof("valid Istio config present in node-level CNI file %s, not modifying", in.cniConfigFilepath)
	}

	return copiedFiles, nil
}

// Run starts the installation process, verifies the configuration, then sleeps.
// If the configuration is invalid, a full redeployal of config, binaries, and svcAcct credentials to the
// shared node CNI dir will be attempted.
//
// If changes occurred but the config is still valid, only the binaries and (optionally) svcAcct credentials
// will be redeployed.
func (in *Installer) Run(ctx context.Context) error {
	installedBins, err := in.installAll(ctx)
	if err != nil {
		return err
	}
	installLog.Info("Installation succeed, start watching for re-installation.")

	for {
		// if sleepWatchInstall yields without error, that means the config might have been modified in some fashion.
		// so we rerun `install`, which will update the modified config if it has fallen out of sync with
		// our desired state
		err := in.sleepWatchInstall(ctx, installedBins)
		if err != nil {
			installLog.Error("error watching node CNI config")
			return err
		}
		installLog.Info("Detected changes to the node-level CNI setup, checking to see if configs or binaries need redeploying")
		// We don't support (or want) to silently (re)deploy any binaries that were not in the initial "snapshot"
		// so we intentionally discard/do not update the list of installedBins on redeploys.
		if _, err := in.installAll(ctx); err != nil {
			return err
		}
		installLog.Info("Istio CNI configuration and binaries validated/reinstalled.")
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

// sleepWatchInstall blocks until any file change for the binaries or config are detected.
// At that point, the func yields so the caller can recheck the validity of the install.
// If an error occurs or context is canceled, the function will return an error.
func (in *Installer) sleepWatchInstall(ctx context.Context, installedBinFiles sets.String) error {
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
		setNotReady(in.isReady)
		watcher.Close()
	}()

	// Before we process whether any file events have been triggered, we must check that the file is correct
	// at this moment, and if not, yield. This is to catch other CNIs which might have mutated the file between
	// the (theoretical) window after we initially install/write, but before we actually start the filewatch.
	if err := checkValidCNIConfig(in.cfg, in.cniConfigFilepath); err != nil {
		return nil
	}

	// If a file we are watching has a change event, yield and let caller check validity
	select {
	case <-watcher.Events:
		// Something changed, and we must yield
		return nil
	case err := <-watcher.Errors:
		// We had a watch error - that's no good
		return err
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Valid configuration; set isReady to true and wait for modifications before checking again
		setReady(in.isReady)
		cniInstalls.With(resultLabel.Value(resultSuccess)).Increment()
		// Pod set to "NotReady" before termination
		return watcher.Wait(ctx)
	}
}

// checkValidCNIConfig returns an error if an invalid CNI configuration is detected
func checkValidCNIConfig(cfg *config.InstallConfig, cniConfigFilepath string) error {
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

// Sets isReady to true.
func setReady(isReady *atomic.Value) {
	installReady.Record(1)
	isReady.Store(true)
}

// Sets isReady to false.
func setNotReady(isReady *atomic.Value) {
	installReady.Record(0)
	isReady.Store(false)
}
