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

package crd

import (
	"os"
	"os/user"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/broker/pkg/model/config"
	"istio.io/broker/pkg/testing/mock"
	"istio.io/broker/pkg/testing/util"
)

func kubeconfig(t *testing.T) string {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err.Error())
	}
	f := usr.HomeDir + "/.kube/config"

	// For Bazel sandbox, the config is mounted in a different location
	// as specified in prow/broker-presubmit.sh
	if _, err = os.Stat(f); err != nil {
		wd, _ := os.Getwd()
		f = wd + "/../config"
	}

	return f
}

func makeClient(t *testing.T) *Client {
	desc := append(config.BrokerConfigTypes, mock.FakeConfig)
	cl, err := NewClient(kubeconfig(t), desc)
	if err != nil {
		t.Fatal(err)
	}

	err = cl.RegisterResources()
	if err != nil {
		t.Fatal(err)
	}

	return cl
}

func createInterface(kubeconfig string) (*rest.Config, kubernetes.Interface, error) {
	kube, err := resolveConfig(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	config, err := clientcmd.BuildConfigFromFlags("", kube)
	if err != nil {
		return nil, nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	return config, client, err
}

// makeTempClient allocates a namespace and cleans it up on test completion
func makeTempClient(t *testing.T) (*Client, string, func()) {
	_, client, err := createInterface(kubeconfig(t))
	if err != nil {
		t.Fatal(err)
	}
	ns, err := util.CreateNamespace(client)
	if err != nil {
		t.Fatal(err.Error())
	}
	cl := makeClient(t)

	// the rest of the test can run in parallel
	t.Parallel()
	return cl, ns, func() { util.DeleteNamespace(client, ns) }
}

func TestStoreInvariant(t *testing.T) {
	client, ns, cleanup := makeTempClient(t)
	defer cleanup()
	mock.CheckMapInvariant(client, t, ns, 5)
}

func TestBrokerConfig(t *testing.T) {
	client, ns, cleanup := makeTempClient(t)
	defer cleanup()
	mock.CheckBrokerConfigTypes(client, ns, t)
}
