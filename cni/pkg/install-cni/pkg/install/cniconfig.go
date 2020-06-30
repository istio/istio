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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/containernetworking/cni/libcni"
	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/spf13/viper"

	"istio.io/istio/cni/pkg/install-cni/pkg/constants"
	"istio.io/pkg/log"
)

func createCNIConfigFile() error {
	contents, err := readCNITemplate()
	if err != nil {
		return err
	}

	contents, err = replaceVariables(contents)
	if err != nil {
		return err
	}

	return saveFile(contents)
}

func readCNITemplate() ([]byte, error) {
	if configFile := viper.GetString(constants.CNINetworkConfigFile); fileutil.Exist(configFile) {
		contents, err := ioutil.ReadFile(configFile)
		if err != nil {
			return nil, err
		}
		log.Infof("Using CNI config template from %s", configFile)
		return contents, nil
	}

	if config := viper.GetString(constants.CNINetworkConfig); len(config) > 0 {
		log.Infof("Using CNI config template from CNI_NETWORK_CONFIG environment variable.")
		return []byte(config), nil
	}

	return nil, errors.New("need CNI_NETWORK_CONFIG or CNI_NETWORK_CONFIG_FILE to be set")
}

func replaceVariables(input []byte) ([]byte, error) {
	var err error
	out := string(input)

	out = strings.ReplaceAll(out, "__KUBERNETES_SERVICE_HOST__", os.Getenv("KUBERNETES_SERVICE_HOST"))
	out = strings.ReplaceAll(out, "__KUBERNETES_SERVICE_PORT__", os.Getenv("KUBERNETES_SERVICE_PORT"))

	node := os.Getenv("KUBERNETES_NODE_NAME")
	if len(node) == 0 {
		node, err = os.Hostname()
		if err != nil {
			return nil, err
		}
	}
	out = strings.ReplaceAll(out, "__KUBERNETES_NODE_NAME__", node)

	out = strings.ReplaceAll(out, "__KUBECONFIG_FILENAME__", viper.GetString(constants.KubeCfgFilename))
	out = strings.ReplaceAll(out, "__KUBECONFIG_FILEPATH__", filepath.Join(viper.GetString(constants.CNINetDir), viper.GetString(constants.KubeCfgFilename)))
	out = strings.ReplaceAll(out, "__LOG_LEVEL__", viper.GetString(constants.LogLevel))

	// Log the config file before inserting service account token.
	// This way auth token is not visible in the logs.
	log.Infof("CNI config: %s", out)

	token, err := readServiceAccountToken()
	if err != nil {
		return nil, err
	}
	out = strings.ReplaceAll(out, "__SERVICEACCOUNT_TOKEN__", token)

	return []byte(out), nil
}

func saveFile(contents []byte) error {
	finalFilename := getCNIConfFilename()

	if viper.GetBool(constants.ChainedCNIPlugin) && fileutil.Exist(finalFilename) {
		// This section overwrites an existing plugins list entry to for istio-cni
		existingContent, err := ioutil.ReadFile(finalFilename)
		if err != nil {
			return err
		}
		contents, err = transformCNIPluginIntoList(contents, existingContent)
		if err != nil {
			return err
		}

		if strings.HasSuffix(finalFilename, ".conf") {
			// If the old config filename ends with .conf, rename it to .conflist, because it has changed to be a list
			// Also remove the old file
			err = os.Remove(finalFilename)
			if err != nil {
				return err
			}
			log.Infof("Renaming %s extension to .conflist", finalFilename)
			finalFilename += "list"
		}
	}

	tmpFilename := filepath.Join(viper.GetString(constants.MountedCNINetDir), "istio-cni.conf.tmp")
	err := ioutil.WriteFile(tmpFilename, contents, 0644)
	if err != nil {
		return err
	}

	err = os.Rename(tmpFilename, finalFilename)
	if err != nil {
		return err
	}

	log.Infof("Created CNI config %s", finalFilename)
	return nil
}

func getCNIConfFilename() string {
	mountedCNINetDir := viper.GetString(constants.MountedCNINetDir)

	filename := viper.GetString(constants.CNIConfName)
	if filename == "" {
		var err error
		filename, err = getDefaultCNINetwork(mountedCNINetDir)
		if err != nil {
			log.Infoa(err)
			filename = "YYY-istio-cni.conf"
		}
	}

	filename = filepath.Join(mountedCNINetDir, filename)

	if !viper.GetBool(constants.ChainedCNIPlugin) {
		return filename
	}

	if !fileutil.Exist(filename) &&
		strings.HasSuffix(filename, ".conf") &&
		fileutil.Exist(filename+"list") {
		log.Infof("%s doesn't exist, but %[1]slist does; Using it instead", filename)
		filename += "list"
	}

	return filename
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

// newContent = istio-cni content, that should be merged into existingContent
// existingContent = contents of the file that is already present
func transformCNIPluginIntoList(newContent, existingContent []byte) ([]byte, error) {
	var rawIstio interface{}
	err := json.Unmarshal(newContent, &rawIstio)
	if err != nil {
		return nil, fmt.Errorf("error loading Istio CNI config (JSON error): %v", err)
	}
	istioMap := rawIstio.(map[string]interface{})
	delete(istioMap, "cniVersion")

	var rawExisting interface{}
	err = json.Unmarshal(existingContent, &rawExisting)
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
