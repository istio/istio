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
	"fmt"
	"os"
	"os/user"
	"testing"

	"github.com/golang/glog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"

	"istio.io/manager/model"
	"istio.io/manager/test/mock"
)

func TestThirdPartyResourcesClient(t *testing.T) {
	cl := makeClient(t)
	ns := makeNamespace(cl.client, t)
	defer deleteNamespace(cl.client, ns)

	mock.CheckMapInvariant(cl, t, ns, 5)

	// check secret
	secret := "istio-secret"
	_, err := cl.client.Core().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: secret},
		Data:       map[string][]byte{"value": []byte(secret)},
	})
	if err != nil {
		t.Error(err)
	}

	data, err := cl.GetSecret(fmt.Sprintf("%s.%s", secret, ns))
	if err != nil {
		t.Errorf("GetSecret => got %q", err)
	} else {
		value := string(data["value"])
		if value != secret || len(data) != 1 {
			t.Errorf("GetSecret => got %q, want %q", data, secret)
		}
	}

	_, err = cl.GetSecret(secret)
	if err == nil {
		t.Errorf("GetSecret(%q) => got no error", secret)
	}

	// TODO(kuat) initial watch always fails, takes time to register TPR, keep
	// around as a work-around
	// kr.DeregisterResources()
}

func makeClient(t *testing.T) *Client {
	usr, err := user.Current()
	if err != nil {
		t.Fatalf(err.Error())
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

func makeNamespace(cl *kubernetes.Clientset, t *testing.T) string {
	ns, err := cl.Core().Namespaces().Create(&v1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: "istio-test-",
		},
	})
	if err != nil {
		t.Fatalf(err.Error())
	}
	glog.Infof("Created namespace %s", ns.Name)
	return ns.Name
}

func deleteNamespace(cl *kubernetes.Clientset, ns string) {
	if ns != "" && ns != "default" {
		if err := cl.Core().Namespaces().Delete(ns, &v1.DeleteOptions{}); err != nil {
			glog.Warningf("Error deleting namespace: %v", err)
		}
		glog.Infof("Deleted namespace %s", ns)
	}
}
