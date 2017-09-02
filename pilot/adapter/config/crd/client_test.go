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

	"istio.io/pilot/model"
	"istio.io/pilot/platform/kube"
	"istio.io/pilot/test/mock"
	"istio.io/pilot/test/util"
)

func kubeconfig(t *testing.T) string {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err.Error())
	}

	kubeconfig := usr.HomeDir + "/.kube/config"

	// For Bazel sandbox we search a different location:
	if _, err = os.Stat(kubeconfig); err != nil {
		kubeconfig, _ = os.Getwd()
		kubeconfig = kubeconfig + "/../../../platform/kube/config"
	}

	return kubeconfig
}

func makeClient(t *testing.T) *Client {
	desc := append(model.IstioConfigTypes, mock.Types...)
	cl, err := NewClient(kubeconfig(t), desc, "")
	if err != nil {
		t.Fatal(err)
	}

	err = cl.RegisterResources()
	if err != nil {
		t.Fatal(err)
	}

	// TODO(kuat) initial watch always fails, takes time to register, keep
	// around as a work-around
	// kr.DeregisterResources()

	return cl
}

// makeTempClient allocates a namespace and cleans it up on test completion
func makeTempClient(t *testing.T) (*Client, string, func()) {
	_, client, err := kube.CreateInterface(kubeconfig(t))
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

func TestIstioConfig(t *testing.T) {
	client, ns, cleanup := makeTempClient(t)
	defer cleanup()
	mock.CheckIstioConfigTypes(client, ns, t)
}
