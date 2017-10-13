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
	"reflect"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/pilot/platform/kube"
	"istio.io/pilot/proxy"
	"istio.io/pilot/test/util"
)

func makeClient(t *testing.T) (*rest.Config, kubernetes.Interface) {
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

	config, cl, err := kube.CreateInterface(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}

	return config, cl
}

func TestInitializerRun(t *testing.T) {
	restConfig, cl := makeClient(t)
	t.Parallel()
	ns, err := util.CreateNamespace(cl)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(cl, ns)

	config := &Config{}
	i, err := NewInitializer(restConfig, config, cl)
	if err != nil {
		t.Fatal(err.Error())
	}

	stop := make(chan struct{})
	go i.Run(stop)

	time.Sleep(3 * time.Second)
	close(stop)
}

func TestInitialize(t *testing.T) {
	restConfig, cl := makeClient(t)
	t.Parallel()
	ns, err := util.CreateNamespace(cl)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(cl, ns)

	mesh := proxy.DefaultMeshConfig()

	cases := []struct {
		name                   string
		in                     string
		wantPatchBytesFilename string
		policy                 InjectionPolicy
		managedNamespace       string
		objNamespace           string
		wantPatched            bool
		wantDebug              bool
	}{
		{
			name:                   "initializer not configured",
			in:                     "testdata/hello.yaml",
			policy:                 InjectionPolicyEnabled,
			objNamespace:           v1.NamespaceDefault,
			managedNamespace:       v1.NamespaceAll,
			wantPatchBytesFilename: "testdata/hello.yaml.patch",
		},
		{
			name:                   "required with NamespaceAll",
			in:                     "testdata/required.yaml",
			policy:                 InjectionPolicyEnabled,
			objNamespace:           v1.NamespaceDefault,
			managedNamespace:       v1.NamespaceAll,
			wantPatchBytesFilename: "testdata/required.yaml.patch",
			wantPatched:            true,
		},
		{
			name:                   "required with default namespace",
			in:                     "testdata/required.yaml",
			policy:                 InjectionPolicyEnabled,
			objNamespace:           v1.NamespaceDefault,
			managedNamespace:       v1.NamespaceDefault,
			wantPatchBytesFilename: "testdata/required.yaml.patch",
			wantPatched:            true,
		},
		{
			name:                   "first initializer",
			in:                     "testdata/first-initializer.yaml",
			policy:                 InjectionPolicyEnabled,
			objNamespace:           v1.NamespaceDefault,
			managedNamespace:       v1.NamespaceDefault,
			wantPatchBytesFilename: "testdata/first-initializer.yaml.patch",
			wantPatched:            true,
		},
		{
			name:                   "second initializer",
			in:                     "testdata/second-initializer.yaml",
			policy:                 InjectionPolicyEnabled,
			objNamespace:           v1.NamespaceDefault,
			managedNamespace:       v1.NamespaceDefault,
			wantPatchBytesFilename: "testdata/second-initializer.yaml.patch",
			wantDebug:              true,
		},
	}

	for _, c := range cases {
		config := &Config{
			Policy:            c.policy,
			IncludeNamespaces: []string{c.managedNamespace},
			Params: Params{
				InitImage:       InitImageName(unitTestHub, unitTestTag, c.wantDebug),
				ProxyImage:      ProxyImageName(unitTestHub, unitTestTag, c.wantDebug),
				ImagePullPolicy: "IfNotPresent",
				Verbosity:       DefaultVerbosity,
				SidecarProxyUID: DefaultSidecarProxyUID,
				Version:         "12345678",
				Mesh:            &mesh,
			},
			InitializerName: DefaultInitializerName,
		}
		i, err := NewInitializer(restConfig, config, cl)
		if err != nil {
			t.Fatal(err.Error())
		}

		var (
			gotNamespace        string
			gotName             string
			gotPatchBytes       []byte
			gotGroupVersionKind schema.GroupVersionKind
			gotPatched          bool
		)
		mockPatch := func(namespace, name string, patchBytes []byte, obj runtime.Object) error {
			gotNamespace = namespace
			gotName = name
			gotPatchBytes = patchBytes
			gotPatched = true

			gvk, _, err := injectScheme.ObjectKind(obj) // nolint: vetshadow
			if err != nil {
				t.Fatalf("%v: failed to determine GroupVersionKind of obj: %v", c.name, err)
			}
			gotGroupVersionKind = gvk
			return nil
		}

		raw, err := ioutil.ReadFile(c.in)
		if err != nil {
			t.Fatalf("%v: ReadFile(%v) failed: %v", c.name, c.in, err)
		}
		var typeMeta metav1.TypeMeta
		if err = yaml.Unmarshal(raw, &typeMeta); err != nil {
			t.Fatalf("%v: Unmarshal(typeMeta) failed: %v", c.name, err)
		}

		wantGroupVersionKind := schema.FromAPIVersionAndKind(typeMeta.APIVersion, typeMeta.Kind)

		obj, err := injectScheme.New(wantGroupVersionKind)
		if err != nil {
			t.Fatalf("%v: failed to create obj from GroupVersionKind: %v", c.name, err)
		}
		if err = yaml.Unmarshal(raw, obj); err != nil {
			t.Fatalf("%v: Unmarshal(obj) failed: %v", c.name, err)
		}

		if err := i.initialize(obj, mockPatch); err != nil {
			t.Fatalf("%v: initialize() returned an error: %v", c.name, err)
		}

		if gotPatched != c.wantPatched {
			t.Fatalf("%v: incorrect patching of object: got patched=%v want patched=%v", c.name, gotPatched, c.wantPatched)
		}

		if gotPatched {
			m, err := meta.Accessor(obj)
			if err != nil {
				t.Fatalf("%v: failed to create accessor object: %v", c.name, err)
			}

			if gotNamespace != m.GetNamespace() {
				t.Errorf("%v: wrong namespace: got %q want %q", c.name, gotNamespace, m.GetNamespace())
			}
			if gotName != m.GetName() {
				t.Errorf("%v: wrong name: got %q want %q", c.name, gotName, m.GetName())
			}
			wantGroupVersionKind.Group = gotGroupVersionKind.Group
			if !reflect.DeepEqual(&gotGroupVersionKind, &wantGroupVersionKind) {
				t.Errorf("%v: wrong GroupVersionKind of runtime.Object: got %#v want %#v",
					c.name, gotGroupVersionKind, wantGroupVersionKind)
			}

			util.CompareContent(gotPatchBytes, c.wantPatchBytesFilename, t)
		}
	}
}
