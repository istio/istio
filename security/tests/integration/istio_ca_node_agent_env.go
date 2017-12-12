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
	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/platform/kube"
	"istio.io/istio/tests/integration/framework"
)

type (
	NodeAgentTestEnv struct {
		framework.TestEnv
		name      string
		ClientSet *kubernetes.Clientset
		NameSpace string
		Hub       string
		Tag       string
	}
)

// NewSecretTestEnv creates the environment instance
func NewNodeAgentTestEnv(name string, clientset *kubernetes.Clientset, hub string, tag string) *NodeAgentTestEnv {
	namespace, err := createTestNamespace(clientset, testNamespacePrefix)
	if err != nil {
		return nil
	}

	return &NodeAgentTestEnv{
		ClientSet: clientset,
		name:      name,
		NameSpace: namespace,
		Hub:       hub,
		Tag:       tag,
	}
}

// GetName return environment ID
func (env *NodeAgentTestEnv) GetName() string {
	return env.name
}

// GetComponents is the key of a environment
// It defines what components a environment contains.
// Components will be stored in framework for start and stop
func (env *NodeAgentTestEnv) GetComponents() []framework.Component {
	return []framework.Component{
		NewKubernetesPod(
			env.ClientSet,
			env.NameSpace,
			"istio-ca-with-given-certificate",
			fmt.Sprintf("%v/istio-ca-test:%v", env.Hub, env.Tag),
			[]string{},
			[]string{},
		),
		NewKubernetesService(
			env.ClientSet,
			env.NameSpace,
			"istio-ca",
			v1.ServiceTypeClusterIP,
			8060,
			map[string]string{
				"pod-group": "istio-ca-with-given-certificate-pod-group",
			},
			map[string]string{
				kube.KubeServiceAccountsOnVMAnnotation: "nodeagent.google.com",
			},
		),
		NewKubernetesPod(
			env.ClientSet,
			env.NameSpace,
			"node-agent",
			fmt.Sprintf("%v/node-agent-test:%v", env.Hub, env.Tag),
			[]string{},
			[]string{},
		),
		NewKubernetesService(
			env.ClientSet,
			env.NameSpace,
			"node-agent-service",
			v1.ServiceTypeLoadBalancer,
			8080,
			map[string]string{
				"pod-group": "node-agent-pod-group",
			},
			map[string]string{},
		),
	}
}

// Bringup doing general setup for environment level, not components.
// Bringup() is called by framework.SetUp()
func (env *NodeAgentTestEnv) Bringup() error {
	return nil
}

// Cleanup clean everything created by this test environment, not component level
// Cleanup() is being called in framework.TearDown()
func (env *NodeAgentTestEnv) Cleanup() error {
	glog.Infof("cleaning up environment...")
	err := deleteTestNamespace(env.ClientSet, env.NameSpace)
	if err != nil {
		glog.Errorf("failed to delete namespace: %v error: %v", env.NameSpace, err)
	}
	return nil
}

// GetNodeAgentIPAddress returns the external LoadBalancer IP address of the service
func (env *NodeAgentTestEnv) GetNodeAgentIPAddress() (string, error) {
	return getServiceExternalIPAddress(env.ClientSet, env.NameSpace, "node-agent-service")
}
