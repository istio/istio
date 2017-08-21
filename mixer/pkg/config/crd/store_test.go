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
	"context"
	"fmt"
	"os"
	"os/user"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	cfg "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/config/store"
	// import GKE cluster authentication plugin
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// import OIDC cluster authentication plugin, e.g. for Tectonic
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

func kubeconfig(t *testing.T) string {
	usr, err := user.Current()
	if err != nil {
		t.Fatal(err.Error())
	}

	configPath := path.Join(usr.HomeDir, ".kube", "config")
	// For Bazel sandbox we search a different location:
	if _, err = os.Stat(configPath); err != nil {
		configPath, _ = os.Getwd()
		configPath = configPath + "/../../../testdata/kubernetes/config"
	}
	return configPath
}

func createNamespace(cl kubernetes.Interface) (string, error) {
	ns, err := cl.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "istio-test-",
		},
	})
	if err != nil {
		return "", err
	}
	glog.Infof("Created namespace %s", ns.Name)
	return ns.Name, nil
}

func deleteNamespace(cl kubernetes.Interface, ns string) {
	if ns != "" && ns != "default" {
		if err := cl.CoreV1().Namespaces().Delete(ns, &metav1.DeleteOptions{}); err != nil {
			glog.Warningf("Error deleting namespace: %v", err)
		}
		glog.Infof("Deleted namespace %s", ns)
	}
}

func hasCRDs(client *dynamic.ResourceClient, kinds []string) ([]string, error) {
	list, err := client.List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	remainings := map[string]bool{}
	for _, k := range kinds {
		remainings[k] = true
	}
	for _, uns := range list.(*unstructured.UnstructuredList).Items {
		kind := uns.Object["spec"].(map[string]interface{})["names"].(map[string]interface{})["kind"].(string)
		if remainings[kind] {
			delete(remainings, kind)
		}
	}
	if len(remainings) == 0 {
		return nil, nil
	}
	result := make([]string, 0, len(remainings))
	for k := range remainings {
		result = append(result, k)
	}
	return result, nil
}

func ensureCRDs(cfg *rest.Config) error {
	testingKinds := []string{"Handler", "Action"}
	cfg.APIPath = "/apis"
	cfg.GroupVersion = &schema.GroupVersion{
		Group:   "apiextensions.k8s.io",
		Version: "v1beta1",
	}

	dynClient, err := dynamic.NewClient(cfg)
	if err != nil {
		return err
	}
	client := dynClient.Resource(&metav1.APIResource{
		Name:         "customresourcedefinitions",
		SingularName: "customresourcedefinition",
		Namespaced:   false,
		Kind:         "CustomResourceDefinition",
	}, "")
	testingKinds, err = hasCRDs(client, testingKinds)
	if err != nil {
		return err
	}
	for len(testingKinds) > 0 {
		for _, kind := range testingKinds {
			_, err = client.Create(&unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apiextensions.k8s.io/v1beta1",
					"kind":       "CustomResourceDefinition",
					"metadata": map[string]interface{}{
						"name": fmt.Sprintf("%ss.%s", strings.ToLower(kind), apiGroup),
					},
					"spec": map[string]interface{}{
						"group":   apiGroup,
						"version": apiVersion,
						"scope":   "Namespaced",
						"names": map[string]interface{}{
							"plural":   strings.ToLower(kind) + "s",
							"singular": strings.ToLower(kind),
							"kind":     kind,
						},
					},
				},
			})
			if err != nil {
				return err
			}
		}
		testingKinds, err = hasCRDs(client, testingKinds)
		if err != nil {
			return err
		}
	}
	return nil
}

func getTempClient(t *testing.T) (*Store, string, func()) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig(t))
	if err != nil {
		t.Fatal(err.Error())
	}
	if err = ensureCRDs(cfg); err != nil {
		t.Fatal(err.Error())
	}
	k8sclient := kubernetes.NewForConfigOrDie(cfg)
	ns, err := createNamespace(k8sclient)
	if err != nil {
		t.Fatal(err.Error())
	}
	store, err := NewStore(kubeconfig(t), []string{ns})
	if err != nil {
		deleteNamespace(k8sclient, ns)
		t.Fatal(err.Error())
	}
	return store, ns, func() { deleteNamespace(k8sclient, ns) }
}

func waitFor(wch <-chan store.Event, ct store.ChangeType, key store.Key) {
	expected := store.Event{Key: key, Type: ct}
	for ev := range wch {
		if ev == expected {
			return
		}
	}
}

func TestStore(t *testing.T) {
	s, ns, close := getTempClient(t)
	s.RegisterKind("Handler", &cfg.Handler{})
	s.RegisterKind("Action", &cfg.Action{})
	defer close()
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Init(ctx); err != nil {
		t.Fatal(err.Error())
	}
	defer cancel()

	wch, err := s.Watch(ctx, []string{"Handler"})
	if err != nil {
		t.Fatal(err.Error())
	}
	k := store.Key{Kind: "Handler", Namespace: ns, Name: "default"}
	h := &cfg.Handler{}
	if err := s.Get(k, h); err != store.ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
	h.Name = "default"
	h.Adapter = "noop"
	if err := s.Put(k, h); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	waitFor(wch, store.Update, k)
	h2 := &cfg.Handler{}
	if err := s.Get(k, h2); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	if !reflect.DeepEqual(h, h2) {
		t.Errorf("Got %+v, Want %+v", h2, h)
	}
	if err := s.Delete(k); err != nil {
		t.Errorf("Got %v, Want nil", err)
	}
	waitFor(wch, store.Delete, k)
	if err := s.Get(k, h2); err != store.ErrNotFound {
		t.Errorf("Got %v, Want ErrNotFound", err)
	}
}

func TestStoreWrongKind(t *testing.T) {
	s, ns, close := getTempClient(t)
	s.RegisterKind("Action", &cfg.Action{})
	defer close()
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Init(ctx); err != nil {
		t.Fatal(err.Error())
	}
	defer cancel()

	k := store.Key{Kind: "Handler", Namespace: ns, Name: "default"}
	h := &cfg.Handler{}
	h.Name = "default"
	h.Adapter = "noop"
	if err := s.Put(k, h); err == nil {
		t.Error("Got nil, Want error")
	}

	if err := s.Get(k, h); err == nil {
		t.Error("Got nil, Want error")
	}
	if err := s.Delete(k); err == nil {
		t.Error("Got nil, Want error")
	}
}
