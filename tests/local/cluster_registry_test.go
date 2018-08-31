// Copyright 2018 Istio Authors
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
package local

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api/v1"
	k8s_cr "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
)

func getAPIsURLs() ([]string, error) {

	urlPath := os.Getenv("GOPATH")
	if len(urlPath) == 0 {
		return []string{}, fmt.Errorf("GOPATH is not set")
	}
	urlPath = path.Join(urlPath, "out/log")
	if _, err := os.Stat(urlPath); err != nil {
		return []string{}, fmt.Errorf("problem with location of URL files, error %v", err)
	}
	var urls []string
	_ = filepath.Walk(urlPath, func(path string, f os.FileInfo, _ error) error {
		if !f.IsDir() {
			if filepath.Ext(path) == ".url" && strings.HasPrefix(f.Name(), "apiserver") {
				b, err := ioutil.ReadFile(path)
				if err == nil {
					urls = append(urls, strings.TrimSuffix(string(b), "\n"))
				}
			}
		}
		return nil
	})

	return urls, nil
}

func createClusterConfig(urls []string) (*[]clientcmdapi.Config, error) {
	clusterName := "cluster-"

	clusterConfigs := []clientcmdapi.Config{}
	for i, url := range urls {
		cluster := clientcmdapi.Cluster{
			Server: url,
		}
		context := clientcmdapi.Context{
			Cluster: clusterName + strconv.Itoa(i+1),
		}
		clusterConfig := clientcmdapi.Config{
			Kind:           "Config",
			APIVersion:     "v1",
			CurrentContext: "local",
			Clusters: []clientcmdapi.NamedCluster{
				{
					Name:    clusterName + strconv.Itoa(i+1),
					Cluster: cluster,
				},
			},
			Contexts: []clientcmdapi.NamedContext{
				{
					Name:    "local",
					Context: context,
				},
			},
		}
		clusterConfigs = append(clusterConfigs, clusterConfig)
	}

	return &clusterConfigs, nil
}

func createSecretFromClusterConfig(apiServerURL string, namespace string, clusterConfigs *[]clientcmdapi.Config) error {

	client, err := buildLocalClient(apiServerURL)
	if err != nil {
		return err
	}

	for _, config := range *clusterConfigs {
		clusterName := config.Clusters[0].Name
		secret := v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				Labels: map[string]string{
					"config.istio.io/multiClusterConfig": "true",
				},
			},
			Data: map[string][]byte{},
		}
		b, _ := json.Marshal(config)
		secret.Data[clusterName] = b

		_, err := client.CoreV1().Secrets(namespace).Create(&secret)
		if err != nil {
			return err
		}
	}
	return nil
}

func createClusterConfigmap(apiServerURL string, namespace string, clusterConfigs *[]clientcmdapi.Config) error {

	client, err := buildLocalClient(apiServerURL)
	if err != nil {
		return err
	}
	var pilotCfgStore string
	clusters := map[string]k8s_cr.Cluster{}
	for i, config := range *clusterConfigs {
		clusterName := config.Clusters[0].Name
		pilotCfgStore = "false"
		if i == 0 {
			pilotCfgStore = "true"
		}
		annotations := map[string]string{
			"config.istio.io/pilotEndpoint":               "192.168." + strconv.Itoa(i) + ".1:9080",
			"config.istio.io/platform":                    "Kubernetes",
			"config.istio.io/pilotCfgStore":               pilotCfgStore,
			"config.istio.io/accessConfigSecret":          clusterName,
			"config.istio.io/accessConfigSecretNamespace": namespace,
		}
		cluster := k8s_cr.Cluster{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Cluster",
				APIVersion: "clusterregistry.k8s.io/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        clusterName,
				Namespace:   namespace,
				Annotations: annotations,
			},
			Spec: k8s_cr.ClusterSpec{
				KubernetesAPIEndpoints: k8s_cr.KubernetesAPIEndpoints{
					ServerEndpoints: []k8s_cr.ServerAddressByClientCIDR{
						{
							ClientCIDR:    "10.23." + strconv.Itoa(i) + ".0/28",
							ServerAddress: config.Clusters[0].Cluster.Server,
						},
					},
				},
			},
		}
		clusters[clusterName] = cluster
	}

	configmap := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterregistry",
			Namespace: namespace,
		},
		Data: map[string]string{},
	}
	for cn, cd := range clusters {

		dataBytes, err1 := json.Marshal(cd)
		if err1 != nil {
			return err1
		}
		configmap.Data[cn] = string(dataBytes)
	}
	_, err = client.CoreV1().ConfigMaps(namespace).Create(&configmap)

	return err
}

func initMulticlusterPilot(IstioSrc string) (*bootstrap.Server, error) {

	serverAgrs := bootstrap.PilotArgs{
		Namespace: "istio-system",
		DiscoveryOptions: envoy.DiscoveryServiceOptions{
			HTTPAddr:        ":18080", // An unused port will be chosen
			GrpcAddr:        ":0",
			EnableCaching:   true,
			EnableProfiling: true,
		},
		//TODO: start mixer first, get its address
		Mesh: bootstrap.MeshArgs{
			MixerAddress:    "istio-mixer.istio-system:9091",
			RdsRefreshDelay: types.DurationProto(10 * time.Millisecond),
		},
		Config: bootstrap.ConfigArgs{
			KubeConfig:                 IstioSrc + "/.circleci/mc-config",
			ClusterRegistriesConfigmap: "clusterregistry",
			ClusterRegistriesNamespace: "istio-testing",
		},
		Service: bootstrap.ServiceArgs{
			Registries: []string{
				string(serviceregistry.KubernetesRegistry)},
		},
	}
	// Create the server for the discovery service.
	discoveryServer, err := bootstrap.NewServer(serverAgrs)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery service: %v", err)
	}

	return discoveryServer, nil
}

func startMulticlusterPilot(s *bootstrap.Server, stop chan struct{}) {
	// Start the server
	s.Start(stop)
}

func prepareEnv(t *testing.T) error {
	var err error

	urls, err := getAPIsURLs()
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return fmt.Errorf("NOURL")
	}
	t.Logf("Retrieved URLs : %+v", urls)

	clusterConfigs, err := createClusterConfig(urls)
	if err != nil {
		return err
	}
	if err = checkLocalAPIServer(urls[0]); err != nil {
		return fmt.Errorf("error in checkLocalAPIServer: %v", err)
	}
	if err = ensureTestNamespace(urls[0], "istio-testing"); err != nil {
		return fmt.Errorf("error in checkLocalAPIServer: %v", err)
	}
	if err = createSecretFromClusterConfig(urls[0], "istio-testing", clusterConfigs); err != nil {
		return fmt.Errorf("error in createSecretFromClusterConfig: %v", err)
	}

	if err = createClusterConfigmap(urls[0], "istio-testing", clusterConfigs); err != nil {
		return fmt.Errorf("error in createCusterConfigmap: %v", err)
	}

	return err
}

func TestClusterRegistry(t *testing.T) {

	if err := prepareEnv(t); err != nil {
		if err.Error() == "NOURL" {
			t.Skip("No API servers' URLs were found, skipping...")
		}
		t.Fatalf("Failed with error: %v", err)
	}
	stop := make(chan struct{})
	defer func() {
		close(stop)
	}()

	istioPath := os.Getenv("GOPATH") + "/src/istio.io/istio"
	pilot, err := initMulticlusterPilot(istioPath)
	if err != nil {
		t.Fatalf("Failed to initialize pilot error: %v", err)
	}

	go func() {
		startMulticlusterPilot(pilot, stop)
	}()
}
