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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"istio.io/istio/cni/pkg/install-cni/pkg/config"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/containernetworking/cni/libcni"
	"github.com/coreos/etcd/pkg/fileutil"
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

func createCNIConfigFile(cfg *config.Config, saToken string) error {
	cniConfig, err := readCNIConfigTemplate(getCNIConfigTemplate(cfg))
	if err != nil {
		return err
	}

	cniConfig, err = replaceCNIConfigVars(cniConfig, getCNIConfigVars(cfg), saToken)
	if err != nil {
		return err
	}

	return writeCNIConfig(cniConfig, getPluginConfig(cfg))
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

func replaceCNIConfigVars(cniConfig []byte, vars cniConfigVars, saToken string) ([]byte, error) {
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

	return []byte(cniConfigStr), nil
}

func writeCNIConfig(cniConfig []byte, cfg pluginConfig) error {
	cniConfigFilepath := getCNIConfigFilepath(cfg)

	if cfg.chainedCNIPlugin && fileutil.Exist(cniConfigFilepath) {
		// This section overwrites an existing plugins list entry for istio-cni
		existingCNIConfig, err := ioutil.ReadFile(cniConfigFilepath)
		if err != nil {
			return err
		}
		cniConfig, err = insertCNIConfig(cniConfig, existingCNIConfig)
		if err != nil {
			return err
		}

		if strings.HasSuffix(cniConfigFilepath, ".conf") {
			// If the old CNI config filename ends with .conf, rename it to .conflist, because it has changed to be a list
			// Also remove the old file
			err = os.Remove(cniConfigFilepath)
			if err != nil {
				return err
			}
			log.Infof("Renaming %s extension to .conflist", cniConfigFilepath)
			cniConfigFilepath += "list"
		}
	}

	tmpFile, err := ioutil.TempFile(filepath.Dir(cniConfigFilepath), filepath.Base(cniConfigFilepath)+".tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	err = os.Chmod(tmpFile.Name(), 0644)
	if err != nil {
		return err
	}

	_, err = tmpFile.Write(cniConfig)
	if err != nil {
		_ = tmpFile.Close()
		return err
	}

	if err = tmpFile.Close(); err != nil {
		return err
	}

	err = os.Rename(tmpFile.Name(), cniConfigFilepath)
	if err != nil {
		return err
	}

	log.Infof("Created CNI config %s", cniConfigFilepath)
	return nil
}

func getCNIConfigFilepath(cfg pluginConfig) string {
	filename := cfg.cniConfName
	if len(filename) == 0 {
		var err error
		filename, err = getDefaultCNINetwork(cfg.mountedCNINetDir)
		if err != nil {
			log.Infoa(err)
			filename = "YYY-istio-cni.conf"
		}
	}
	cniConfigFilepath := filepath.Join(cfg.mountedCNINetDir, filename)

	if !cfg.chainedCNIPlugin {
		return cniConfigFilepath
	}

	if !fileutil.Exist(cniConfigFilepath) &&
		strings.HasSuffix(cniConfigFilepath, ".conf") &&
		fileutil.Exist(cniConfigFilepath+"list") {
		log.Infof("%s doesn't exist, but %[1]slist does; Using it instead", cniConfigFilepath)
		cniConfigFilepath += "list"
	}

	return cniConfigFilepath
}

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
	var rawIstio interface{}
	err := json.Unmarshal(newCNIConfig, &rawIstio)
	if err != nil {
		return nil, fmt.Errorf("error loading Istio CNI config (JSON error): %v", err)
	}
	istioMap := rawIstio.(map[string]interface{})
	delete(istioMap, "cniVersion")

	var rawExisting interface{}
	err = json.Unmarshal(existingCNIConfig, &rawExisting)
	if err != nil {
		return nil, fmt.Errorf("error loading existing CNI config (JSON error): %v", err)
	}

	var newMap map[string]interface{}
	existingMap := rawExisting.(map[string]interface{})

	if _, ok := existingMap["type"]; ok {
		// Assume it is a regular network conf file
		newMap = map[string]interface{}{}
		newMap["name"] = "k8s-pod-network"
		newMap["cniVersion"] = "0.3.1"

		delete(existingMap, "cniVersion")

		plugins := make([]map[string]interface{}, 2)
		plugins[0] = existingMap
		plugins[1] = istioMap

		newMap["plugins"] = plugins
	} else {
		// Assume it is a network list file
		newMap = existingMap
		plugins := newMap["plugins"].([]interface{})
		newMap["plugins"] = append(plugins, istioMap)

	}

	return json.MarshalIndent(newMap, "", "  ")
}
