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
	"istio.io/istio/tests/integration/framework"
	"k8s.io/client-go/kubernetes"
)

type (
	// CertRotationTestEnv is the test environment for CA certificate rotation test
	CertRotationTestEnv struct {
		framework.TestEnv
		name      string
		ClientSet *kubernetes.Clientset
		NameSpace string
		Hub       string
		Tag       string
	}
)

const (
	istioCaSelfSignedShortTTL = "istio-ca-self-signed-short-ttl"
)

// NewCertRotationTestEnv creates the environment instance
func NewCertRotationTestEnv(name string, clientset *kubernetes.Clientset, hub string, tag string) *CertRotationTestEnv {
	namespace, err := createTestNamespace(clientset, testNamespacePrefix)
	if err != nil {
		return nil
	}

	return &CertRotationTestEnv{
		name:      name,
		ClientSet: clientset,
		NameSpace: namespace,
		Hub:       hub,
		Tag:       tag,
	}
}

// GetName return environment ID
func (env *CertRotationTestEnv) GetName() string {
	return env.name
}

// GetComponents is the key of a environment
// It defines what components a environment contains.
// Components will be stored in framework for start and stop
func (env *CertRotationTestEnv) GetComponents() []framework.Component {
	return []framework.Component{
		NewKubernetesPod(
			env.ClientSet,
			env.NameSpace,
			istioCaSelfSignedShortTTL,
			fmt.Sprintf("%v/istio-ca:%v", env.Hub, env.Tag),
			[]string{
				"/usr/local/bin/istio_ca",
			},
			[]string{
				"--self-signed-ca",
				"--cert-ttl", "60s",
			},
		),
	}
}

// Bringup doing general setup for environment level, not components.
// Bringup() is called by framework.SetUp()
func (env *CertRotationTestEnv) Bringup() error {
	return nil
}

// Cleanup clean everything created by this test environment, not component level
// Cleanup() is being called in framework.TearDown()
func (env *CertRotationTestEnv) Cleanup() error {
	glog.Infof("cleaning up environment...")
	err := deleteTestNamespace(env.ClientSet, env.NameSpace)
	if err != nil {
		glog.Errorf("failed to delete the namespace: %v error: %v", env.NameSpace, err)
	}
	return nil
}
