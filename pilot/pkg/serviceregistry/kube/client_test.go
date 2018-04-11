// Copyright 2017 Istio Authors
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

package kube

import (
	"io/ioutil"
	"os"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var (
	testConfig = clientcmdapi.Config{
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"red-user": {Token: "red-token"}},
		Clusters: map[string]*clientcmdapi.Cluster{
			"cow-cluster": {Server: "http://cow.org:8080"}},
		Contexts: map[string]*clientcmdapi.Context{
			"federal-context": {AuthInfo: "red-user", Cluster: "cow-cluster", Namespace: "hammer-ns"}},
		CurrentContext: "federal-context",
	}
)

func TestLoadConfigFromMultiPath(t *testing.T) {
	configFile, _ := ioutil.TempFile("", "")
	defer os.Remove(configFile.Name())

	if err := clientcmd.WriteToFile(testConfig, configFile.Name()); err != nil {
		t.Fatalf("Failed to write config file to %s", configFile.Name())
	}

	config, _ := LoadConfigFromMultiPath(configFile.Name())
	if config == nil {
		t.Errorf("Could not load kubernetes configuration from file")
	}

	// load config from environment variable
	config, _ = LoadConfigFromMultiPath("")
	if config == nil {
		t.Errorf("Could not load kubernetes configuration from environment variable")
	}
}
