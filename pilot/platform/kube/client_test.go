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
	"os"
	"os/user"
	"testing"

	"istio.io/manager/model"
	"istio.io/manager/test/mock"
	"istio.io/manager/test/util"
)

func TestThirdPartyResourcesClient(t *testing.T) {
	cl := makeClient(t)
	ns, err := util.CreateNamespace(cl.client)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(cl.client, ns)

	mock.CheckMapInvariant(cl, t, ns, 5)

	// TODO(kuat) initial watch always fails, takes time to register TPR, keep
	// around as a work-around
	// kr.DeregisterResources()
}

func makeClient(t *testing.T) *Client {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err.Error())
	}

	kubeconfig := usr.HomeDir + "/.kube/config"

	// For Bazel sandbox we search a different location:
	if _, err = os.Stat(kubeconfig); err != nil {
		kubeconfig, _ = os.Getwd()
		kubeconfig = kubeconfig + "/config"
	}

	km := model.KindMap{}
	for k, v := range model.IstioConfig {
		km[k] = v
	}
	km[mock.Kind] = mock.Mapping[mock.Kind]

	cl, err := NewClient(kubeconfig, km)
	if err != nil {
		t.Fatalf(err.Error())
	}

	err = cl.RegisterResources()
	if err != nil {
		t.Fatalf(err.Error())
	}

	return cl
}
