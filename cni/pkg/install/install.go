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
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/sleep"
	"istio.io/istio/pkg/util/sets"
)

var installLog = scopes.CNIAgent

type Installer struct {
	cfg                *config.InstallConfig
	isReady            *atomic.Value
	kubeconfigFilepath string
	// TODO(jaellio): Allow users to configure file path in installer and add file path validation
	// (valid priority)
	cniConfigFilepath string
}

// NewInstaller returns an instance of Installer with the given config
func NewInstaller(cfg *config.InstallConfig, isReady *atomic.Value) *Installer {
	return &Installer{
		cfg:                cfg,
		kubeconfigFilepath: filepath.Join(cfg.CNIAgentRunDir, constants.CNIPluginKubeconfName),
		isReady:            isReady,
	}
}

func (in *Installer) installAll(ctx context.Context) (sets.String, error) {
	// Install binaries
	// Currently we _always_ do this, since the binaries do not live in a shared location
	// and we harm no one by doing so.
	copiedFiles, err := copyBinaries(in.cfg.CNIBinSourceDir, in.cfg.CNIBinTargetDirs)
	if err != nil {
		if strings.Contains(err.Error(), "read-only file system") {
			log.Warnf("hint: some Kubernetes environments require customization of the CNI directory." +
				" Ensure you properly set global.platform=<name> during installation")
		}
		cniInstalls.With(resultLabel.Value(resultCopyBinariesFailure)).Increment()
		return copiedFiles, fmt.Errorf("copy binaries: %v", err)
	}

	// Write kubeconfig with our current service account token as the contents, to the Istio agent rundir.
	// We do not write this to the common/shared CNI config dir, because it's not CNI config, we do not
	// need to watch it, and writing non-shared stuff to that location creates churn for other node agents.
	// Only our plugin consumes this kubeconfig, and it resides in our owned rundir on the host node,
	// so we are good to simply write it out if our watched svcacct token changes.
	if err := writeKubeConfigFile(in.cfg); err != nil {
		cniInstalls.With(resultLabel.Value(resultCreateKubeConfigFailure)).Increment()
		return copiedFiles, fmt.Errorf("write kubeconfig: %v", err)
	}

	// Install CNI netdir config (if needed) - we write/update this in the shared node CNI netdir,
	// which may be watched by other CNIs, and so we don't want to trigger writes to this file
	// unless it's missing or the contents are not what we expect.
	// TODO(jaellio): Remove this log
	log.Infof("installAll cniConfigFilePath %v", in.cniConfigFilepath)
	if err := checkValidCNIConfig(ctx, in.cfg, in.cniConfigFilepath); err != nil {
		installLog.Infof("configuration requires updates, (re)writing CNI config file at %q: %v", in.cniConfigFilepath, err)
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
	installLog.Info("initial installation complete, start watching for re-installation")
	throttle := newInstallationThrottle(in)
	for {
		throttle.Throttle(ctx)
		// if sleepWatchInstall yields without error, that means the config might have been modified in some fashion.
		// so we rerun `install`, which will update the modified config if it has fallen out of sync with
		// our desired state
		err := in.sleepWatchInstall(ctx, installedBins)
		if err != nil {
			installLog.Errorf("error watching node CNI config: %v", err)
			return err
		}
		installLog.Info("detected changes to the node-level CNI setup, checking to see if configs or binaries need redeploying")
		// We don't support (or want) to silently (re)deploy any binaries that were not in the initial "snapshot"
		// so we intentionally discard/do not update the list of installedBins on redeploys.
		if _, err := in.installAll(ctx); err != nil {
			return err
		}
		installLog.Info("Istio CNI configuration and binaries validated/reinstalled")
	}
}

// Cleanup remove Istio CNI's config, kubeconfig file, and binaries.
func (in *Installer) Cleanup() error {
	installLog.Info("cleaning up CNI installation")
	if len(in.cniConfigFilepath) > 0 && file.Exists(in.cniConfigFilepath) {
		if in.cfg.ChainedCNIPlugin {
			installLog.Infof("removing Istio CNI config from CNI config file: %s", in.cniConfigFilepath)

			// Read JSON from CNI config file
			cniConfigMap, err := util.ReadCNIConfigMap(in.cniConfigFilepath)
			if err != nil {
				return fmt.Errorf("failed to read CNI config map from file %s: %w", in.cniConfigFilepath, err)
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
				return fmt.Errorf("failed to marshal CNI config map in file %s: %w", in.cniConfigFilepath, err)
			}
			if err = file.AtomicWrite(in.cniConfigFilepath, cniConfig, os.FileMode(0o644)); err != nil {
				return fmt.Errorf("failed to write updated CNI config to file %s: %w", in.cniConfigFilepath, err)
			}
		} else {
			installLog.Infof("removing Istio CNI config file: %s", in.cniConfigFilepath)
			if err := os.Remove(in.cniConfigFilepath); err != nil {
				return fmt.Errorf("failed to remove CNI config file %s: %w", in.cniConfigFilepath, err)
			}
		}
	}

	if len(in.kubeconfigFilepath) > 0 && file.Exists(in.kubeconfigFilepath) {
		installLog.Infof("removing Istio CNI kubeconfig file: %s", in.kubeconfigFilepath)
		if err := os.Remove(in.kubeconfigFilepath); err != nil {
			return fmt.Errorf("failed to remove kubeconfig file %s: %w", in.kubeconfigFilepath, err)
		}
	}

	for _, targetDir := range in.cfg.CNIBinTargetDirs {
		if istioCNIBin := filepath.Join(targetDir, "istio-cni"); file.Exists(istioCNIBin) {
			installLog.Infof("removing binary: %s", istioCNIBin)
			if err := os.Remove(istioCNIBin); err != nil {
				return fmt.Errorf("failed to remove binary %s: %w", istioCNIBin, err)
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
		in.cfg.K8sServiceAccountPath,
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
	if err := checkValidCNIConfig(ctx, in.cfg, in.cniConfigFilepath); err != nil {
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
func checkValidCNIConfig(ctx context.Context, cfg *config.InstallConfig, cniConfigFilepath string) error {
	// filename of the primary CNI config file which may contain the Istio CNI config
	// OR filename of the Istio owned config which may contain the primary CNI config
	// and/or the Istio CNI plugin
	// defaultCNIConfigFileName is the name of the highest priority, valid config
	cniConfigFilenames, err := getHighestPriorityConfigFilename(cfg.MountedCNINetDir)
	log.Infof("jaellio - defautlCNICOnfigFilename: %v", cniConfigFilenames)
	if err != nil || len(cniConfigFilenames) == 0 {
		return err
	}
	firstCNIConfigFilename := cniConfigFilenames[0]
	secondCNIConfigFilename := ""
	if len(cniConfigFilenames) >= 2 {
		secondCNIConfigFilename = cniConfigFilenames[1]
	}

	// TODO(jaellio): Check priority and remove hardcoded istio conf
	// TODO(jaellio): might be able to remote this if we provide a default value
	// to cniConfigFilePath and make it configurable
	if firstCNIConfigFilename != "02-istio-conf.conflist" && cfg.ChainedCNIPlugin {
		if len(cfg.CNIConfName) == 0 {
			cfg.CNIConfName = firstCNIConfigFilename
		}
		return fmt.Errorf("Istio owned CNI config does not exist or is not the highest priority. Got %s instead", firstCNIConfigFilename)
	}

	// filepath for the highest priority, valid config
	defaultCNIConfigFilepath := filepath.Join(cfg.MountedCNINetDir, firstCNIConfigFilename)
	log.Infof("jaellio - path defaultCNIConfigFilepath: %s", defaultCNIConfigFilepath)
	// TODO(jaellio): When is cniConfigFilepath set prior to the first call of checkValidCNIConfig?
	// check if the highest priority, valid config is the expected filepath
	log.Infof("jaellio - compare defaultCNIConfigFilepath %s and cniConfigFilePath %s", defaultCNIConfigFilepath, cniConfigFilepath)
	if defaultCNIConfigFilepath != cniConfigFilepath {
		// TODO(jaellio): what is the meaning of CNIConfName
		if len(cfg.CNIConfName) > 0 || !cfg.ChainedCNIPlugin {
			// Install was run with overridden CNI config file so don't error out on preempt check
			// Likely the only use for this is testing the script
			installLog.Warnf("CNI config file %q preempted by %q", cniConfigFilepath, defaultCNIConfigFilepath)
		} else {
			// If CNIConfName isn't set yet, set it to the default CNI config filename
			if len(cfg.CNIConfName) == 0 {
				log.Infof("jaellio - set CNIConfName to default CNI config file name")
				cfg.CNIConfName = firstCNIConfigFilename
			}
			log.Infof("jaellio - got error since defaultCNIConfigFilepath %s and cniConfigFilePath %s are not equal", defaultCNIConfigFilepath, cniConfigFilepath)
			return fmt.Errorf("CNI config file %q preempted by %q", cniConfigFilepath, defaultCNIConfigFilepath)
		}
	}

	if !file.Exists(cniConfigFilepath) {
		return fmt.Errorf("CNI config file removed: %s", cniConfigFilepath)
	}

	// TODO(jaellio): Check assumption that CNIConfName is the primary CNI config name
	if cfg.ChainedCNIPlugin {
		// TODO(jaellio): In the cleanest way possible, if the highest priority config is an istio owned config
		// make sure we can get the name of the primary cni config. This handles the case if the CNI daemonset
		// restarts
		if len(cfg.CNIConfName) == 0 {
			cfg.CNIConfName = secondCNIConfigFilename
		}

		// Get Istio owned CNI config plugings
		cniConfigMap, err := util.ReadCNIConfigMap(cniConfigFilepath)
		if err != nil {
			return err
		}
		plugins, err := util.GetPlugins(cniConfigMap)
		if err != nil {
			return fmt.Errorf("%s: %w", cniConfigFilepath, err)
		}

		// Get primary CNI config plugins to ensure Istio CNI config is up to date
		primaryCNIConfigFilepath, err := getCNIConfigFilepath(ctx, cfg.CNIConfName, cfg.MountedCNINetDir, cfg.ChainedCNIPlugin)
		if err != nil {
			return err
		}
		primaryCNIConfigmap, err := util.ReadCNIConfigMap(primaryCNIConfigFilepath)
		if err != nil {
			return err
		}
		primaryPlugins, err := util.GetPlugins(primaryCNIConfigmap)
		if err != nil {
			return fmt.Errorf("%s: %w", primaryCNIConfigFilepath, err)
		}

		// Create a map to index plugins by their "type" field
		pluginMap := make(map[string]map[string]any)
		for _, rawPlugin := range plugins {
			plugin, err := util.GetPlugin(rawPlugin)
			if err != nil {
				return fmt.Errorf("%s: %w", cniConfigFilepath, err)
			}
			if pluginType, ok := plugin["type"].(string); ok {
				pluginMap[pluginType] = plugin
			} else {
				return fmt.Errorf("plugin type %v not a string", plugin["type"])
			}
		}

		// Verify that the Istio CNI config exists in the CNI config plugin map
		if _, exists := pluginMap["istio-cni"]; !exists {
			return fmt.Errorf("istio-cni plugin not found in Istio CNI config file %s", firstCNIConfigFilename)
		}

		// Verifies the Istio CNI config contains all non istio-cni plugins from the primary CNI config
		// and checks that the plugins are equivalent
		for _, rawPrimaryPlugin := range primaryPlugins {
			primaryPlugin, err := util.GetPlugin(rawPrimaryPlugin)
			if err != nil {
				return fmt.Errorf("%s: %w", primaryCNIConfigFilepath, err)
			}
			primaryType, ok := primaryPlugin["type"].(string)
			if !ok {
				return fmt.Errorf("plugin type %v not a string", primaryPlugin["type"])
			}

			_, exists := pluginMap[primaryType]
			if !exists {
				return fmt.Errorf("plugin of type %s from primary CNI config is missing in Istio CNI config file", primaryType)
			}

			// TODO(jaellio): check plugin equality
			/*
				if !equalPlugins(plugin, primaryPlugin) {
					return fmt.Errorf("plugin of type %s does match between %s and %s", primaryType, cniConfigFilepath, primaryCNIConfigFilepath)
				}
			*/
		}

		return nil
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

// installationThrottle is a small wrapper around a rate limitter. It aims to avoid excessive writes to CNI configuration,
// and detect if there is a loop of requests, typically caused by another component constantly reverting our work.
// Where possible, the remediate steps are logged.
type installationThrottle struct {
	limiter *rate.Limiter
	hits    int
	in      *Installer
}

func newInstallationThrottle(in *Installer) *installationThrottle {
	return &installationThrottle{
		// Setup the limiter to once every 5s. We don't actually limit to only 1/5, this is just to use it to keep track
		// of whether we got a lot of requests
		limiter: rate.NewLimiter(rate.Limit(0.2), 1),
		hits:    0,
		in:      in,
	}
}

func (i *installationThrottle) Throttle(ctx context.Context) {
	res := i.limiter.Reserve()
	// Slightly weird usage of the limiter, as we are not strictly using it for limiting
	// First, we get a reservation. This will use up the limit for 5s
	if res.Delay() == 0 {
		// If its available, we haven't tried to install in over 5s, reset our hits and return
		i.hits = 0
		return
	}
	// Otherwise, wait. We only wait up to 1s.
	sleep.UntilContext(ctx, min(res.Delay(), time.Second))
	// Increment our hits. If we are spamming this loop, we will hit this many times as we continually are sending >1 RPS
	i.hits++
	// Log every 5 times to not spam too much (and not log on initial startup where some reconciling is expected
	if i.hits > 5 {
		detectedCNI := ""
		if strings.Contains(i.in.cniConfigFilepath, "cilium") {
			detectedCNI = "cilium"
		}
		hint := ""
		switch detectedCNI {
		case "cilium":
			hint = " Hint: Cilium CNI was detected; ensure 'cni.exclusive=false' in the Cilium configuration."
		}
		log.Warnf("Configuration has been reconciled multiple times in a short period of time. "+
			"This may be due to a conflicting component constantly reverting our work.%s", hint)
	}
}
