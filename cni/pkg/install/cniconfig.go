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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/containernetworking/cni/libcni"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/plugin"
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
)

func createCNIConfigFile(ctx context.Context, cfg *config.InstallConfig) (string, error) {
	pluginConfig := plugin.Config{
		PluginLogLevel:     cfg.PluginLogLevel,
		CNIAgentRunDir:     cfg.CNIAgentRunDir,
		AmbientEnabled:     cfg.AmbientEnabled,
		EnablementSelector: cfg.AmbientEnablementSelector,
		ExcludeNamespaces:  strings.Split(cfg.ExcludeNamespaces, ","),
		PodNamespace:       cfg.PodNamespace,
	}

	pluginConfig.Name = "istio-cni"
	pluginConfig.Type = "istio-cni"
	pluginConfig.CNIVersion = "0.3.1"

	marshalledJSON, err := json.MarshalIndent(pluginConfig, "", "  ")
	if err != nil {
		return "", err
	}
	marshalledJSON = append(marshalledJSON, "\n"...)

	return writeCNIConfig(ctx, marshalledJSON, cfg)
}

// writeCNIConfig will
// 1. read in the existing CNI config file
// 2. append the `istio`-specific entry
// 3. write the combined result back out to the same path, overwriting the original.
func writeCNIConfig(ctx context.Context, pluginConfig []byte, cfg *config.InstallConfig) (string, error) {
	cniConfigFilepath, err := getCNIConfigFilepath(ctx, cfg.CNIConfName, cfg.MountedCNINetDir, cfg.ChainedCNIPlugin)
	if err != nil {
		return "", err
	}

	if cfg.ChainedCNIPlugin {
		if !file.Exists(cniConfigFilepath) {
			return "", fmt.Errorf("CNI config file %s removed during configuration", cniConfigFilepath)
		}
		// This section overwrites an existing plugins list entry for istio-cni
		existingCNIConfig, err := os.ReadFile(cniConfigFilepath)
		if err != nil {
			return "", err
		}
		pluginConfig, err = insertCNIConfig(pluginConfig, existingCNIConfig)
		if err != nil {
			return "", err
		}
	}

	if err = file.AtomicWrite(cniConfigFilepath, pluginConfig, os.FileMode(0o644)); err != nil {
		installLog.Errorf("Failed to write CNI config file %v: %v", cniConfigFilepath, err)
		return cniConfigFilepath, err
	}

	if cfg.ChainedCNIPlugin && strings.HasSuffix(cniConfigFilepath, ".conf") {
		// If the old CNI config filename ends with .conf, rename it to .conflist, because it has to be changed to a list
		installLog.Infof("Renaming %s extension to .conflist", cniConfigFilepath)
		err = os.Rename(cniConfigFilepath, cniConfigFilepath+"list")
		if err != nil {
			installLog.Errorf("Failed to rename CNI config file %v: %v", cniConfigFilepath, err)
			return cniConfigFilepath, err
		}
		cniConfigFilepath += "list"
	}

	installLog.Infof("created CNI config %s", cniConfigFilepath)
	installLog.Debugf("CNI config: %s", pluginConfig)
	return cniConfigFilepath, nil
}

// If configured as chained CNI plugin, waits indefinitely for a main CNI config file to exist before returning
// Or until cancelled by parent context
func getCNIConfigFilepath(ctx context.Context, cniConfName, mountedCNINetDir string, chained bool) (string, error) {
	if !chained {
		if len(cniConfName) == 0 {
			cniConfName = "YYY-istio-cni.conf"
		}
		return filepath.Join(mountedCNINetDir, cniConfName), nil
	}

	watcher, err := util.CreateFileWatcher(mountedCNINetDir)
	if err != nil {
		return "", err
	}
	defer watcher.Close()

	for len(cniConfName) == 0 {
		cniConfName, err = getDefaultCNINetwork(mountedCNINetDir)
		if err == nil {
			break
		}
		installLog.Warnf("Istio CNI is configured as chained plugin, but cannot find existing CNI network config: %v", err)
		installLog.Infof("Waiting for CNI network config file to be written in %v...", mountedCNINetDir)
		if err := watcher.Wait(ctx); err != nil {
			return "", err
		}
	}

	cniConfigFilepath := filepath.Join(mountedCNINetDir, cniConfName)

	for !file.Exists(cniConfigFilepath) {
		if strings.HasSuffix(cniConfigFilepath, ".conf") && file.Exists(cniConfigFilepath+"list") {
			installLog.Infof("%s doesn't exist, but %[1]slist does; Using it as the CNI config file instead.", cniConfigFilepath)
			cniConfigFilepath += "list"
		} else if strings.HasSuffix(cniConfigFilepath, ".conflist") && file.Exists(cniConfigFilepath[:len(cniConfigFilepath)-4]) {
			installLog.Infof("%s doesn't exist, but %s does; Using it as the CNI config file instead.", cniConfigFilepath, cniConfigFilepath[:len(cniConfigFilepath)-4])
			cniConfigFilepath = cniConfigFilepath[:len(cniConfigFilepath)-4]
		} else {
			installLog.Infof("CNI config file %s does not exist. Waiting for file to be written...", cniConfigFilepath)
			if err := watcher.Wait(ctx); err != nil {
				return "", err
			}
		}
	}

	installLog.Debugf("CNI config file %s exists, proceeding", cniConfigFilepath)

	return cniConfigFilepath, err
}

// Follows the same semantics as kubelet
// https://github.com/kubernetes/kubernetes/blob/954996e231074dc7429f7be1256a579bedd8344c/pkg/kubelet/dockershim/network/cni/cni.go#L144-L184
func getDefaultCNINetwork(confDir string) (string, error) {
	files, err := libcni.ConfFiles(confDir, []string{".conf", ".conflist"})
	switch {
	case err != nil:
		return "", err
	case len(files) == 0:
		return "", fmt.Errorf("no networks found in %s", confDir)
	}

	sort.Strings(files)
	for _, confFile := range files {
		var confList *libcni.NetworkConfigList
		if strings.HasSuffix(confFile, ".conflist") {
			confList, err = libcni.ConfListFromFile(confFile)
			if err != nil {
				installLog.Warnf("Error loading CNI config list file %s: %v", confFile, err)
				continue
			}
		} else {
			conf, err := libcni.ConfFromFile(confFile)
			if err != nil {
				installLog.Warnf("Error loading CNI config file %s: %v", confFile, err)
				continue
			}
			// Ensure the config has a "type" so we know what plugin to run.
			// Also catches the case where somebody put a conflist into a conf file.
			if conf.Network.Type == "" {
				installLog.Warnf("Error loading CNI config file %s: no 'type'; perhaps this is a .conflist?", confFile)
				continue
			}

			confList, err = libcni.ConfListFromConf(conf)
			if err != nil {
				installLog.Warnf("Error converting CNI config file %s to list: %v", confFile, err)
				continue
			}
		}
		if len(confList.Plugins) == 0 {
			installLog.Warnf("CNI config list %s has no networks, skipping", confList.Name)
			continue
		}

		return filepath.Base(confFile), nil
	}

	return "", fmt.Errorf("no valid networks found in %s", confDir)
}

// insertCNIConfig will append newCNIConfig to existingCNIConfig
func insertCNIConfig(newCNIConfig, existingCNIConfig []byte) ([]byte, error) {
	var istioMap map[string]any
	err := json.Unmarshal(newCNIConfig, &istioMap)
	if err != nil {
		return nil, fmt.Errorf("error loading Istio CNI config (JSON error): %v", err)
	}

	var existingMap map[string]any
	err = json.Unmarshal(existingCNIConfig, &existingMap)
	if err != nil {
		return nil, fmt.Errorf("error loading existing CNI config (JSON error): %v", err)
	}

	delete(istioMap, "cniVersion")

	var newMap map[string]any

	if _, ok := existingMap["type"]; ok {
		// Assume it is a regular network conf file
		delete(existingMap, "cniVersion")

		plugins := make([]map[string]any, 2)
		plugins[0] = existingMap
		plugins[1] = istioMap

		newMap = map[string]any{
			"name":       "k8s-pod-network",
			"cniVersion": "0.3.1",
			"plugins":    plugins,
		}
	} else {
		// Assume it is a network list file
		newMap = existingMap
		plugins, err := util.GetPlugins(newMap)
		if err != nil {
			return nil, fmt.Errorf("existing CNI config: %v", err)
		}

		for i, rawPlugin := range plugins {
			plugin, err := util.GetPlugin(rawPlugin)
			if err != nil {
				return nil, fmt.Errorf("existing CNI plugin: %v", err)
			}
			if plugin["type"] == "istio-cni" {
				plugins = append(plugins[:i], plugins[i+1:]...)
				break
			}
		}

		newMap["plugins"] = append(plugins, istioMap)
	}

	return util.MarshalCNIConfig(newMap)
}
