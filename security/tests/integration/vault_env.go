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

package integration

import (
	"fmt"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/integration/framework"
)

type (
	// VaultTestEnv is the test environment for Vault test
	VaultTestEnv struct {
		framework.TestEnv
		name      string
		comps     []framework.Component
		ClientSet *kubernetes.Clientset
		NameSpace string
		Hub       string
		Tag       string
	}
)

const (
	vaultName    = "vault"
	vaultService = "vault-service"
	vaultPort    = 8200
)

// NewVaultTestEnv creates the environment instance
func NewVaultTestEnv(name, kubeConfig, hub, tag string) *VaultTestEnv {
	clientset, err := CreateClientset(kubeConfig)
	if err != nil {
		log.Errorf("failed to initialize K8s client: %v", err)
		return nil
	}

	namespace, err := createTestNamespace(clientset, testNamespacePrefix)
	if err != nil {
		log.Errorf("failed to create test namespace: %v", err)
		return nil
	}

	return &VaultTestEnv{
		name:      name,
		ClientSet: clientset,
		NameSpace: namespace,
		Hub:       hub,
		Tag:       tag,
	}
}

// GetName return environment ID
func (env *VaultTestEnv) GetName() string {
	return env.name
}

// GetComponents is the key of a environment
// It defines what components a environment contains.
// Components will be stored in framework for start and stop
func (env *VaultTestEnv) GetComponents() []framework.Component {
	if env.comps == nil {
		env.comps = []framework.Component{
			NewKubernetesPod(
				env.ClientSet,
				env.NameSpace,
				vaultName,
				fmt.Sprintf("%s/vault-test:%s", env.Hub, env.Tag),
				[]string{},
				[]string{},
			),
			NewKubernetesService(
				env.ClientSet,
				env.NameSpace,
				vaultService,
				v1.ServiceTypeLoadBalancer,
				vaultPort,
				map[string]string{
					"pod-group": vaultName + podGroupPostfix,
				},
				map[string]string{},
			),
			//NewKubernetesService(
			//	env.ClientSet,
			//	env.NameSpace,
			//	vaultService,
			//	v1.ServiceTypeClusterIP,
			//	vaultPort,
			//	map[string]string{
			//		"pod-group": vaultName+ podGroupPostfix,
			//	},
			//	map[string]string{
			//		kube.KubeServiceAccountsOnVMAnnotation: "nodeagent.google.com",
			//	},
			//),
		}
	}
	return env.comps
}

// Bringup doing general setup for environment level, not components.
// Bringup() is called by framework.SetUp()
func (env *VaultTestEnv) Bringup() error {
	return nil
}

// Cleanup clean everything created by this test environment, not component level
// Cleanup() is being called in framework.TearDown()
func (env *VaultTestEnv) Cleanup() error {
	log.Infof("cleaning up environment...")
	err := deleteTestNamespace(env.ClientSet, env.NameSpace)
	if err != nil {
		retErr := fmt.Errorf("failed to delete the namespace: %v error: %v", env.NameSpace, err)
		log.Errorf("%v", retErr)
		return retErr
	}
	return nil
}

// GetVaultIPAddress returns the external LoadBalancer IP address of the Vault testing service
func (env *VaultTestEnv) GetVaultIPAddress() (string, error) {
	return getServiceExternalIPAddress(env.ClientSet, env.NameSpace, vaultService)
}

// GetVaultClusterIPAddress returns the clusterIP address of the Vault testing service
func (env *VaultTestEnv) GetVaultClusterIPAddress() (string, error) {
	return getServiceClusterIPAddress(env.ClientSet, env.NameSpace, vaultService)
}
