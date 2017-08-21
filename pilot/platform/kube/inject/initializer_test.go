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

package inject

import (
	"io/ioutil"
	"os"
	"os/user"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/pilot/platform/kube"
	"istio.io/pilot/proxy"
	"istio.io/pilot/test/util"
)

func makeClient(t *testing.T) kubernetes.Interface {
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

	cl, err := kube.CreateInterface(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}

	return cl
}

func TestInitializerRun(t *testing.T) {
	cl := makeClient(t)
	t.Parallel()
	ns, err := util.CreateNamespace(cl)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(cl, ns)
	mesh := proxy.DefaultMeshConfig()

	options := InitializerOptions{
		Hub:             unitTestHub,
		Tag:             unitTestTag,
		Namespace:       ns,
		InjectionPolicy: InjectionPolicyOptOut,
	}
	i := NewInitializer(cl, &mesh, options)

	stop := make(chan struct{})
	go i.Run(stop)
	time.Sleep(3 * time.Second)
	close(stop)
}

func TestHasIstioInitializerNext(t *testing.T) {
	cl := makeClient(t)
	t.Parallel()
	ns, err := util.CreateNamespace(cl)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(cl, ns)
	mesh := proxy.DefaultMeshConfig()

	options := InitializerOptions{
		Hub:             unitTestHub,
		Tag:             unitTestTag,
		Namespace:       ns,
		InjectionPolicy: InjectionPolicyOptOut,
	}
	i := NewInitializer(cl, &mesh, options)

	cases := []struct {
		meta *metav1.ObjectMeta
		want bool
	}{
		{
			meta: &metav1.ObjectMeta{
				Name:        "no-initializer",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			meta: &metav1.ObjectMeta{
				Name:         "empty-initializer",
				Namespace:    "test-namespace",
				Annotations:  map[string]string{},
				Initializers: &metav1.Initializers{},
			},
			want: false,
		},
		{
			meta: &metav1.ObjectMeta{
				Name:        "initializer-empty-pending",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
				Initializers: &metav1.Initializers{
					Pending: []metav1.Initializer{},
				},
			},
			want: false,
		},
		{
			meta: &metav1.ObjectMeta{
				Name:        "single-initializer",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
				Initializers: &metav1.Initializers{
					Pending: []metav1.Initializer{
						{Name: "unknown-initializer"},
					},
				},
			},
			want: false,
		},
		{
			meta: &metav1.ObjectMeta{
				Name:        "single-initializer-match",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
				Initializers: &metav1.Initializers{
					Pending: []metav1.Initializer{
						{Name: initializerName},
					},
				},
			},
			want: true,
		},
	}

	for _, c := range cases {
		if got := i.hasIstioInitializerNext(c.meta); got != c.want {
			t.Errorf("hasIstioInitializerNext(%v): got %v want %v", c.meta.Name, got, c.want)
		}
	}
}

func TestInitializerModifyResource(t *testing.T) {
	cl := makeClient(t)
	t.Parallel()
	mesh := proxy.DefaultMeshConfig()

	namespace := "test-namespace"

	cases := []struct {
		in               string
		want             string
		objNamespace     string
		managedNamespace string
	}{
		{
			in:               "testdata/kube-system.yaml",
			want:             "testdata/kube-system.yaml.injected",
			managedNamespace: v1.NamespaceAll,
			objNamespace:     "kube-system",
		},
		{
			in:               "testdata/non-managed-namespace.yaml",
			want:             "testdata/non-managed-namespace.yaml.injected",
			managedNamespace: namespace,
			objNamespace:     "non-matching",
		},
		{
			in:               "testdata/single-initializer.yaml",
			want:             "testdata/single-initializer.yaml.injected",
			managedNamespace: namespace,
			objNamespace:     namespace,
		},
		{
			in:               "testdata/multiple-initializer.yaml",
			want:             "testdata/multiple-initializer.yaml.injected",
			managedNamespace: namespace,
			objNamespace:     namespace,
		},
		{
			in:               "testdata/not-required.yaml",
			want:             "testdata/not-required.yaml.injected",
			managedNamespace: namespace,
			objNamespace:     namespace,
		},
		{
			in:               "testdata/required.yaml",
			want:             "testdata/required.yaml.injected",
			managedNamespace: namespace,
			objNamespace:     namespace,
		},
	}

	for _, c := range cases {
		options := InitializerOptions{
			Hub:             unitTestHub,
			Tag:             unitTestTag,
			Namespace:       c.managedNamespace,
			InjectionPolicy: InjectionPolicyOptOut,
		}
		i := NewInitializer(cl, &mesh, options)

		raw, err := ioutil.ReadFile(c.in)
		if err != nil {
			t.Fatalf("ReadFile(%v) failed: %v", c.in, err)
		}
		var deployment appsv1beta1.Deployment
		if err = yaml.Unmarshal(raw, &deployment); err != nil {
			t.Fatalf("Unmarshal(%v) failed: %v", c.in, err)
		}
		orig := deployment.ObjectMeta.Namespace
		deployment.ObjectMeta.Namespace = c.objNamespace
		err = i.modifyResource(&deployment.ObjectMeta,
			&deployment.Spec.Template.ObjectMeta, &deployment.Spec.Template.Spec)
		if err != nil {
			t.Fatalf("modifyResource(%v) failed: %v", c.in, err)
		}
		deployment.ObjectMeta.Namespace = orig
		got, err := yaml.Marshal(&deployment)
		if err != nil {
			t.Fatalf("Marshal(%v) failed: %v", c.in, err)
		}
		util.CompareContent(got, c.want, t)
	}
}
