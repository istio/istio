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

package tpr

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

func makeClient(namespace string, t *testing.T) *Client {
	desc := append(model.IstioConfigTypes, mock.Types...)
	cl, err := NewClient(kubeconfig(t), desc, namespace)
	if err != nil {
		t.Fatal(err)
	}

	err = cl.RegisterResources()
	if err != nil {
		t.Fatal(err)
	}

	return cl
}

func TestThirdPartyResourcesClient(t *testing.T) {
	client, err := kube.CreateInterface(kubeconfig(t))
	if err != nil {
		t.Fatal(err)
	}
	ns, err := util.CreateNamespace(client)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(client, ns)
	cl := makeClient(ns, t)
	t.Parallel()

	mock.CheckMapInvariant(cl, t, 5)

	// TODO(kuat) initial watch always fails, takes time to register TPR, keep
	// around as a work-around
	// kr.DeregisterResources()
}
