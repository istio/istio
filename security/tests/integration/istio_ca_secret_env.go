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

package integration

import (
	"fmt"

	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/tests/integration/framework"
)

const (
	istioCaSelfSigned = "istio-ca-self-signed"
)

type (
	// SecretTestEnv is the test environment for istio.default secret test
	SecretTestEnv struct {
		framework.TestEnv
		name      string
		ClientSet *kubernetes.Clientset
		NameSpace string
		Hub       string
		Tag       string
	}
)

// NewSecretTestEnv creates the environment instance
func NewSecretTestEnv(name, kubeConfig, hub, tag string) *SecretTestEnv {
	clientset, err := CreateClientset(kubeConfig)
	if err != nil {
		glog.Errorf("failed to initialize K8s client: %v", err)
		return nil
	}

	namespace, err := createTestNamespace(clientset, testNamespacePrefix)
	if err != nil {
		glog.Errorf("failed to create test namespace: %v", err)
		return nil
	}

	return &SecretTestEnv{
		ClientSet: clientset,
		name:      name,
		NameSpace: namespace,
		Hub:       hub,
		Tag:       tag,
	}
}

// GetName return environment ID
func (env *SecretTestEnv) GetName() string {
	return env.name
}

// GetComponents is the key of a environment
// It defines what components a environment contains.
// Components will be stored in framework for start and stop
func (env *SecretTestEnv) GetComponents() []framework.Component {
	return []framework.Component{
		NewKubernetesPod(
			env.ClientSet,
			env.NameSpace,
			istioCaSelfSigned,
			fmt.Sprintf("%v/istio-ca:%v", env.Hub, env.Tag),
			[]string{
				"/usr/local/bin/istio_ca",
			},
			[]string{
				"--self-signed-ca",
			},
		),
	}
}

// Bringup doing general setup for environment level, not components.
// Bringup() is called by framework.SetUp()
func (env *SecretTestEnv) Bringup() error {
	return nil
}

// Cleanup clean everything created by this test environment, not component level
// Cleanup() is being called in framework.TearDown()
func (env *SecretTestEnv) Cleanup() error {
	glog.Infof("cleaning up environment...")
	err := deleteTestNamespace(env.ClientSet, env.NameSpace)
	if err != nil {
		retErr := fmt.Errorf("failed to delete namespace: %v error: %v", env.NameSpace, err)
		glog.Error(retErr)
		return retErr
	}
	return nil
}
