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

package kube

import (
	"log"
	"os"
	"os/user"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"

	"istio.io/manager/model"
	"istio.io/manager/test"
)

var (
	camelKabobs = []struct{ in, out string }{
		{"ExampleNameX", "example-name-x"},
		{"example1", "example1"},
		{"exampleXY", "example-x-y"},
	}
)

func TestCamelKabob(t *testing.T) {
	for _, tt := range camelKabobs {
		s := camelCaseToKabobCase(tt.in)
		if s != tt.out {
			t.Errorf("camelCaseToKabobCase(%q) => %q, want %q", tt.in, s, tt.out)
		}
	}
}

func TestThirdPartyResourcesClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	cl := makeClient(t)
	ns := makeNamespace(cl.client, t)
	defer deleteNamespace(cl.client, ns)

	test.CheckMapInvariant(cl, t, ns, 10)

	// TODO(kuat) initial watch always fails, takes time to register TPR, keep
	// around as a work-around
	// kr.DeregisterResources()
}

func TestController(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	cl := makeClient(t)
	ns := makeNamespace(cl.client, t)
	defer deleteNamespace(cl.client, ns)

	stop := make(chan struct{})
	defer close(stop)

	ctl := NewController(cl, ns, 1*time.Second)
	added, deleted := 0, 0
	n := 5
	ctl.AppendHandler(test.MockKind, func(c *model.Config, ev int) error {
		log.Printf("Handler %s: %v", eventString(ev), c)
		switch ev {
		case evAdd:
			if deleted != 0 {
				t.Errorf("Events are not serialized (add)")
			}
			added++
		case evDelete:
			if added != n {
				t.Errorf("Events are not serialized (delete)")
			}
			deleted++
		}
		log.Printf("Added %d, deleted %d", added, deleted)
		return nil
	})
	go ctl.Run(stop)

	test.CheckMapInvariant(cl, t, ns, n)
	eventually(func() bool { return added == n && deleted == n }, t)
}

func TestControllerRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	cl := makeClient(t)
	ns := makeNamespace(cl.client, t)
	defer deleteNamespace(cl.client, ns)
	stop := make(chan struct{})
	defer close(stop)
	ctl := NewController(cl, ns, 1*time.Second)
	test.CheckMapInvariant(ctl, t, ns, 3)
}

func eventually(f func() bool, t *testing.T) {
	interval := 64 * time.Millisecond
	for i := 0; i < 10; i++ {
		if f() {
			return
		}
		t.Logf("Sleeping %v", interval)
		time.Sleep(interval)
		interval = 2 * interval
	}
	t.Fatal("Failed to satisfy function")
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
		kubeconfig = kubeconfig + "/platform/kube/config"
		if _, err = os.Stat(kubeconfig); err != nil {
			t.Fatalf("Cannot find .kube/config file")
		}
	}

	cl, err := NewClient(kubeconfig, test.MockMapping)
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
	log.Printf("Created namespace %s", ns.Name)
	return ns.Name
}

func deleteNamespace(cl *kubernetes.Clientset, ns string) {
	if ns != "" && ns != "default" {
		cl.Core().Namespaces().Delete(ns, &v1.DeleteOptions{})
	}
}
