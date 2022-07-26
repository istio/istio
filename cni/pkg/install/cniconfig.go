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
	"istio.io/istio/cni/pkg/util"
	"istio.io/istio/pkg/file"
)

type pluginConfig struct {
	mountedCNINetDir string
	cniConfName      string
	chainedCNIPlugin bool
}

type cniConfigTemplate struct {
	cniNetworkConfigFile string
	cniNetworkConfig     string
}

type cniConfigVars struct {
	cniNetDir          string
	kubeconfigFilename string
	logLevel           string
	k8sServiceHost     string
	k8sServicePort     string
	k8sNodeName        string
	logUDSAddress      string
}

func getPluginConfig(cfg *config.InstallConfig) pluginConfig {
	return pluginConfig{
		mountedCNINetDir: cfg.MountedCNINetDir,
		cniConfName:      cfg.CNIConfName,
		chainedCNIPlugin: cfg.ChainedCNIPlugin,
	}
}

func getCNIConfigTemplate(cfg *config.InstallConfig) cniConfigTemplate {
	return cniConfigTemplate{
		cniNetworkConfigFile: cfg.CNINetworkConfigFile,
		cniNetworkConfig:     cfg.CNINetworkConfig,
	}
}

func getCNIConfigVars(cfg *config.InstallConfig) cniConfigVars {
	return cniConfigVars{
		cniNetDir:          cfg.CNINetDir,
		kubeconfigFilename: cfg.KubeconfigFilename,
		logLevel:           cfg.LogLevel,
		k8sServiceHost:     cfg.K8sServiceHost,
		k8sServicePort:     cfg.K8sServicePort,
		k8sNodeName:        cfg.K8sNodeName,
		logUDSAddress:      cfg.LogUDSAddress,
	}
}

func createCNIConfigFile(ctx context.Context, cfg *config.InstallConfig, saToken string) (string, error) {
	cniConfig, err := readCNIConfigTemplate(getCNIConfigTemplate(cfg))
	if err != nil {
		return "", err
	}

	cniConfig = replaceCNIConfigVars(cniConfig, getCNIConfigVars(cfg), saToken)

	return writeCNIConfig(ctx, cniConfig, getPluginConfig(cfg))
}

func readCNIConfigTemplate(template cniConfigTemplate) ([]byte, error) {
	if file.Exists(template.cniNetworkConfigFile) {
		cniConfig, err := os.ReadFile(template.cniNetworkConfigFile)
		if err != nil {
			return nil, err
		}
		installLog.Infof("Using CNI config template from %s", template.cniNetworkConfigFile)
		return cniConfig, nil
	}

	if len(template.cniNetworkConfig) > 0 {
		installLog.Infof("Using CNI config template from CNI_NETWORK_CONFIG environment variable.")
		return []byte(template.cniNetworkConfig), nil
	}

	return nil, fmt.Errorf("need CNI_NETWORK_CONFIG or CNI_NETWORK_CONFIG_FILE to be set")
}

func replaceCNIConfigVars(cniConfig []byte, vars cniConfigVars, saToken string) []byte {
	cniConfigStr := string(cniConfig)

	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__LOG_LEVEL__", vars.logLevel)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__LOG_UDS_ADDRESS__", vars.logUDSAddress)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBECONFIG_FILENAME__", vars.kubeconfigFilename)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBECONFIG_FILEPATH__", filepath.Join(vars.cniNetDir, vars.kubeconfigFilename))
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBERNETES_SERVICE_HOST__", vars.k8sServiceHost)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBERNETES_SERVICE_PORT__", vars.k8sServicePort)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBERNETES_NODE_NAME__", vars.k8sNodeName)

	// Log the config file before inserting service account token.
	// This way auth token is not visible in the logs.
	installLog.Infof("CNI config: %s", cniConfigStr)

	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__SERVICEACCOUNT_TOKEN__", saToken)

	return []byte(cniConfigStr)
}

func writeCNIConfig(ctx context.Context, cniConfig []byte, cfg pluginConfig) (string, error) {
	cniConfigFilepath, err := getCNIConfigFilepath(ctx, cfg)
	if err != nil {
		return "", err
	}

	if cfg.chainedCNIPlugin {
		if !file.Exists(cniConfigFilepath) {
			return "", fmt.Errorf("CNI config file %s removed during configuration", cniConfigFilepath)
		}
		// This section overwrites an existing plugins list entry for istio-cni
		existingCNIConfig, err := os.ReadFile(cniConfigFilepath)
		if err != nil {
			return "", err
		}
		cniConfig, err = insertCNIConfig(cniConfig, existingCNIConfig)
		if err != nil {
			return "", err
		}
	}

	if err = file.AtomicWrite(cniConfigFilepath, cniConfig, os.FileMode(0o644)); err != nil {
		installLog.Errorf("Failed to write CNI config file %v: %v", cniConfigFilepath, err)
		return cniConfigFilepath, err
	}

	if cfg.chainedCNIPlugin && strings.HasSuffix(cniConfigFilepath, ".conf") {
		// If the old CNI config filename ends with .conf, rename it to .conflist, because it has to be changed to a list
		installLog.Infof("Renaming %s extension to .conflist", cniConfigFilepath)
		err = os.Rename(cniConfigFilepath, cniConfigFilepath+"list")
		if err != nil {
			installLog.Errorf("Failed to rename CNI config file %v: %v", cniConfigFilepath, err)
			return cniConfigFilepath, err
		}
		cniConfigFilepath += "list"
	}

	installLog.Infof("Created CNI config %s", cniConfigFilepath)
	return cniConfigFilepath, nil
}

// If configured as chained CNI plugin, waits indefinitely for a main CNI config file to exist before returning
// Or until cancelled by parent context
func getCNIConfigFilepath(ctx context.Context, cfg pluginConfig) (string, error) {
	filename := cfg.cniConfName

	if !cfg.chainedCNIPlugin {
		if len(filename) == 0 {
			filename = "YYY-istio-cni.conf"
		}
		return filepath.Join(cfg.mountedCNINetDir, filename), nil
	}

	watcher, fileModified, errChan, err := util.CreateFileWatcher(cfg.mountedCNINetDir)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = watcher.Close()
	}()

	for len(filename) == 0 {
		filename, err = getDefaultCNINetwork(cfg.mountedCNINetDir)
		if err == nil {
			break
		}
		installLog.Warnf("Istio CNI is configured as chained plugin, but cannot find existing CNI network config: %v", err)
		installLog.Infof("Waiting for CNI network config file to be written in %v...", cfg.mountedCNINetDir)
		if err = util.WaitForFileMod(ctx, fileModified, errChan); err != nil {
			return "", err
		}
	}

	cniConfigFilepath := filepath.Join(cfg.mountedCNINetDir, filename)

	for !file.Exists(cniConfigFilepath) {
		if strings.HasSuffix(cniConfigFilepath, ".conf") && file.Exists(cniConfigFilepath+"list") {
			installLog.Infof("%s doesn't exist, but %[1]slist does; Using it as the CNI config file instead.", cniConfigFilepath)
			cniConfigFilepath += "list"
		} else if strings.HasSuffix(cniConfigFilepath, ".conflist") && file.Exists(cniConfigFilepath[:len(cniConfigFilepath)-4]) {
			installLog.Infof("%s doesn't exist, but %s does; Using it as the CNI config file instead.", cniConfigFilepath, cniConfigFilepath[:len(cniConfigFilepath)-4])
			cniConfigFilepath = cniConfigFilepath[:len(cniConfigFilepath)-4]
		} else {
			installLog.Infof("CNI config file %s does not exist. Waiting for file to be written...", cniConfigFilepath)
			if err = util.WaitForFileMod(ctx, fileModified, errChan); err != nil {
				return "", err
			}
		}
	}

	installLog.Infof("CNI config file %s exists. Proceeding.", cniConfigFilepath)

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

// newCNIConfig = istio-cni config, that should be inserted into existingCNIConfig
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
