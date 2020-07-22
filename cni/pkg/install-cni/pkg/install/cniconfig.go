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
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/containernetworking/cni/libcni"
	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/pkg/errors"

	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"istio.io/istio/cni/pkg/install-cni/pkg/util"
	"istio.io/pkg/log"
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
}

func getPluginConfig(cfg *config.Config) pluginConfig {
	return pluginConfig{
		mountedCNINetDir: cfg.MountedCNINetDir,
		cniConfName:      cfg.CNIConfName,
		chainedCNIPlugin: cfg.ChainedCNIPlugin,
	}
}

func getCNIConfigTemplate(cfg *config.Config) cniConfigTemplate {
	return cniConfigTemplate{
		cniNetworkConfigFile: cfg.CNINetworkConfigFile,
		cniNetworkConfig:     cfg.CNINetworkConfig,
	}
}

func getCNIConfigVars(cfg *config.Config) cniConfigVars {
	return cniConfigVars{
		cniNetDir:          cfg.CNINetDir,
		kubeconfigFilename: cfg.KubeconfigFilename,
		logLevel:           cfg.LogLevel,
		k8sServiceHost:     cfg.K8sServiceHost,
		k8sServicePort:     cfg.K8sServicePort,
		k8sNodeName:        cfg.K8sNodeName,
	}
}

func createCNIConfigFile(ctx context.Context, cfg *config.Config, saToken string) (string, error) {
	cniConfig, err := readCNIConfigTemplate(getCNIConfigTemplate(cfg))
	if err != nil {
		return "", err
	}

	cniConfig = replaceCNIConfigVars(cniConfig, getCNIConfigVars(cfg), saToken)

	return writeCNIConfig(ctx, cniConfig, getPluginConfig(cfg))
}

func readCNIConfigTemplate(template cniConfigTemplate) ([]byte, error) {
	if fileutil.Exist(template.cniNetworkConfigFile) {
		cniConfig, err := ioutil.ReadFile(template.cniNetworkConfigFile)
		if err != nil {
			return nil, err
		}
		log.Infof("Using CNI config template from %s", template.cniNetworkConfigFile)
		return cniConfig, nil
	}

	if len(template.cniNetworkConfig) > 0 {
		log.Infof("Using CNI config template from CNI_NETWORK_CONFIG environment variable.")
		return []byte(template.cniNetworkConfig), nil
	}

	return nil, errors.New("need CNI_NETWORK_CONFIG or CNI_NETWORK_CONFIG_FILE to be set")
}

func replaceCNIConfigVars(cniConfig []byte, vars cniConfigVars, saToken string) []byte {
	cniConfigStr := string(cniConfig)

	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__LOG_LEVEL__", vars.logLevel)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBECONFIG_FILENAME__", vars.kubeconfigFilename)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBECONFIG_FILEPATH__", filepath.Join(vars.cniNetDir, vars.kubeconfigFilename))
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBERNETES_SERVICE_HOST__", vars.k8sServiceHost)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBERNETES_SERVICE_PORT__", vars.k8sServicePort)
	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__KUBERNETES_NODE_NAME__", vars.k8sNodeName)

	// Log the config file before inserting service account token.
	// This way auth token is not visible in the logs.
	log.Infof("CNI config: %s", cniConfigStr)

	cniConfigStr = strings.ReplaceAll(cniConfigStr, "__SERVICEACCOUNT_TOKEN__", saToken)

	return []byte(cniConfigStr)
}

func writeCNIConfig(ctx context.Context, cniConfig []byte, cfg pluginConfig) (string, error) {
	cniConfigFilepath, err := getCNIConfigFilepath(ctx, cfg)
	if err != nil {
		return "", err
	}

	if cfg.chainedCNIPlugin {
		if !fileutil.Exist(cniConfigFilepath) {
			return "", fmt.Errorf("CNI config file %s removed during configuration", cniConfigFilepath)
		}
		// This section overwrites an existing plugins list entry for istio-cni
		existingCNIConfig, err := ioutil.ReadFile(cniConfigFilepath)
		if err != nil {
			return "", err
		}
		cniConfig, err = insertCNIConfig(cniConfig, existingCNIConfig)
		if err != nil {
			return "", err
		}
	}

	if err = util.AtomicWrite(cniConfigFilepath, cniConfig, os.FileMode(0644)); err != nil {
		return "", err
	}

	if cfg.chainedCNIPlugin && strings.HasSuffix(cniConfigFilepath, ".conf") {
		// If the old CNI config filename ends with .conf, rename it to .conflist, because it has to be changed to a list
		log.Infof("Renaming %s extension to .conflist", cniConfigFilepath)
		err = os.Rename(cniConfigFilepath, cniConfigFilepath+"list")
		if err != nil {
			return "", err
		}
		cniConfigFilepath += "list"
	}

	log.Infof("Created CNI config %s", cniConfigFilepath)
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
		if err = util.WaitForFileMod(ctx, fileModified, errChan); err != nil {
			return "", err
		}
	}

	cniConfigFilepath := filepath.Join(cfg.mountedCNINetDir, filename)

	for !fileutil.Exist(cniConfigFilepath) {
		if strings.HasSuffix(cniConfigFilepath, ".conf") && fileutil.Exist(cniConfigFilepath+"list") {
			log.Infof("%s doesn't exist, but %[1]slist does; Using it as the CNI config file instead.", cniConfigFilepath)
			cniConfigFilepath += "list"
		} else if strings.HasSuffix(cniConfigFilepath, ".conflist") && fileutil.Exist(cniConfigFilepath[:len(cniConfigFilepath)-4]) {
			log.Infof("%s doesn't exist, but %s does; Using it as the CNI config file instead.", cniConfigFilepath, cniConfigFilepath[:len(cniConfigFilepath)-4])
			cniConfigFilepath = cniConfigFilepath[:len(cniConfigFilepath)-4]
		} else {
			log.Infof("CNI config file %s does not exist. Waiting for file to be written...", cniConfigFilepath)
			if err = util.WaitForFileMod(ctx, fileModified, errChan); err != nil {
				return "", err
			}
		}
	}

	log.Infof("CNI config file %s exists. Proceeding.", cniConfigFilepath)

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
				log.Warnf("Error loading CNI config list file %s: %v", confFile, err)
				continue
			}
		} else {
			conf, err := libcni.ConfFromFile(confFile)
			if err != nil {
				log.Warnf("Error loading CNI config file %s: %v", confFile, err)
				continue
			}
			// Ensure the config has a "type" so we know what plugin to run.
			// Also catches the case where somebody put a conflist into a conf file.
			if conf.Network.Type == "" {
				log.Warnf("Error loading CNI config file %s: no 'type'; perhaps this is a .conflist?", confFile)
				continue
			}

			confList, err = libcni.ConfListFromConf(conf)
			if err != nil {
				log.Warnf("Error converting CNI config file %s to list: %v", confFile, err)
				continue
			}
		}
		if len(confList.Plugins) == 0 {
			log.Warnf("CNI config list %s has no networks, skipping", confList.Name)
			continue
		}

		return filepath.Base(confFile), nil
	}

	return "", fmt.Errorf("no valid networks found in %s", confDir)
}

// newCNIConfig = istio-cni config, that should be inserted into existingCNIConfig
func insertCNIConfig(newCNIConfig, existingCNIConfig []byte) ([]byte, error) {
	var istioMap map[string]interface{}
	err := json.Unmarshal(newCNIConfig, &istioMap)
	if err != nil {
		return nil, fmt.Errorf("error loading Istio CNI config (JSON error): %v", err)
	}

	var existingMap map[string]interface{}
	err = json.Unmarshal(existingCNIConfig, &existingMap)
	if err != nil {
		return nil, fmt.Errorf("error loading existing CNI config (JSON error): %v", err)
	}

	delete(istioMap, "cniVersion")

	var newMap map[string]interface{}

	if _, ok := existingMap["type"]; ok {
		// Assume it is a regular network conf file
		delete(existingMap, "cniVersion")

		plugins := make([]map[string]interface{}, 2)
		plugins[0] = existingMap
		plugins[1] = istioMap

		newMap = map[string]interface{}{
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
		newMap["plugins"] = append(plugins, istioMap)
	}

	return util.MarshalCNIConfig(newMap)
}
