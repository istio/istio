// Copyright 2016 Google Inc.
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

package env

import (
	"os/user"
	"testing"

	"k8s.io/client-go/1.5/kubernetes"
	"k8s.io/client-go/1.5/pkg/api"
	"k8s.io/client-go/1.5/pkg/api/v1"

	"istio.io/manager/model"
	"istio.io/manager/test"
)

func TestCamelKabob(t *testing.T) {
	if CamelCaseToKabobCase("ExampleNameX") != "example-name-x" {
		t.Fail()
	}
	if CamelCaseToKabobCase("example1") != "example1" {
		t.Fail()
	}
	if CamelCaseToKabobCase("exampleX") != "example-x" {
		t.Fail()
	}
}

func TestKeyEncoding(t *testing.T) {
	x := model.ConfigKey{Name: test.MockName}
	var n, v string
	n, v = decodeName(encodeName(x))
	if n != test.MockName {
		t.Errorf("Wanted %s, got %s", test.MockName, n)
	}
	if v != x.Version {
		t.Fail()
	}
	x.Version = "version"
	n, v = decodeName(encodeName(x))
	if n != test.MockName {
		t.Errorf("Wanted %s, got %s", test.MockName, n)
	}
	if v != x.Version {
		t.Fail()
	}
}

func TestThirdPartyResources(t *testing.T) {
	usr, err := user.Current()
	if err != nil {
		t.Fatalf(err.Error())
	}

	// TODO: this file needs to be explicitly added to the sandbox on Linux
	kubeconfig := usr.HomeDir + "/.kube/config"
	kr, err := NewKubernetesRegistry(kubeconfig, test.MockMapping)
	if err != nil {
		t.Error(err)
	}

	// registration should be idempotent
	if err = kr.RegisterResources(); err != nil {
		t.Error(err)
	}

	ns, err := makeNamespace(kr.client, t)
	defer deleteNamespace(kr.client, ns, t)

	kr.Namespace = ns
	test.CheckMapInvariant(kr, t)

	// TODO(kuat) initial watch always fails, takes time to register TPR, keep
	// around as a work-around
	//err = kr.DeregisterResources()
	if err != nil {
		t.Error(err)
	}
}

func makeNamespace(cl *kubernetes.Clientset, t *testing.T) (string, error) {
	ns, err := cl.Core().Namespaces().Create(&v1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: "istio-test-",
		},
	})
	if err != nil {
		return "", err
	}
	t.Logf("Created namespace %s", ns.Name)
	return ns.Name, nil
}

func deleteNamespace(cl *kubernetes.Clientset, ns string, t *testing.T) {
	t.Logf("Deleting namespace %s", ns)
	if ns != "" && ns != "default" {
		cl.Core().Namespaces().Delete(ns, &api.DeleteOptions{})
	}
}
