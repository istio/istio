// Copyright Istio Authors
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

package file

import (
	"fmt"
	"path/filepath"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
)

func TestController(t *testing.T) {
	stop := test.NewStop(t)
	root := t.TempDir()

	controller, err := NewController(root, "example.com", collections.Pilot, kubecontroller.Options{
		KrtDebugger: krt.GlobalDebugHandler,
	})
	assert.NoError(t, err)
	go controller.Run(stop)
	tt := assert.NewTracker[string](t)
	controller.RegisterEventHandler(gvk.Gateway, TrackerHandler(tt, kind.Gateway))
	controller.RegisterEventHandler(gvk.VirtualService, TrackerHandler(tt, kind.VirtualService))

	file.WriteOrFail(t, filepath.Join(root, "gw.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: foo
  namespace: default
spec:
  servers:
  - port:
      number: 80
      protocol: HTTP2
      name: http
    hosts:
    - "*.example.com"`))

	tt.WaitOrdered("add/Gateway/default/foo")
	out := controller.List(gvk.Gateway, "")
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %v", len(out))
	}
	if out[0].Name != "foo" {
		t.Fatalf("expected config name foo, got %v", out[0].Name)
	}
	if out[0].Spec.(*networking.Gateway).Servers[0].Port.Protocol != "HTTP2" {
		t.Fatalf("expected config protocol HTTP2, got %v", out[0].Spec.(*networking.Gateway).Servers[0].Port.Protocol)
	}

	file.WriteOrFail(t, filepath.Join(root, "gw.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: foo
  namespace: default
spec:
  servers:
  - port:
      number: 80
      protocol: HTTP
      name: http
    hosts:
    - "*.example.com"`))
	tt.WaitOrdered("update/Gateway/default/foo")

	out = controller.List(gvk.Gateway, "")
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %v", len(out))
	}
	if out[0].Name != "foo" {
		t.Fatalf("expected config name foo, got %v", out[0].Name)
	}
	if out[0].Spec.(*networking.Gateway).Servers[0].Port.Protocol != "HTTP" {
		t.Fatalf("expected config protocol HTTP, got %v", out[0].Spec.(*networking.Gateway).Servers[0].Port.Protocol)
	}

	file.WriteOrFail(t, filepath.Join(root, "gw2.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: bar
  namespace: default
spec:
  servers:
  - port:
      number: 80
      protocol: HTTP
      name: http
    hosts:
    - "*.example.com"`))
	tt.WaitOrdered("add/Gateway/default/bar")

	out = controller.List(gvk.Gateway, "")
	if len(out) != 2 {
		t.Fatalf("expected 2 config, got %v", len(out))
	}

	file.WriteOrFail(t, filepath.Join(root, "gw.yaml"), []byte(``))
	tt.WaitOrdered("delete/Gateway/default/foo")
	out = controller.List(gvk.Gateway, "")
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %v", len(out))
	}

	file.WriteOrFail(t, filepath.Join(root, "vs.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: foo
  namespace: default
spec:
  hosts:
    - "*.example.com"
  http:
  - route:
    - destination:
        host: foo`))
	tt.WaitOrdered("add/VirtualService/default/foo")

	out = controller.List(gvk.Gateway, "")
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %v", len(out))
	}

	out = controller.List(gvk.VirtualService, "")
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %v", len(out))
	}
	if out[0].Name != "foo" {
		t.Fatalf("expected config name foo, got %v", out[0].Name)
	}
}

func TestControllerGet(t *testing.T) {
	stop := test.NewStop(t)
	root := t.TempDir()

	controller, err := NewController(root, "example.com", collections.Pilot, kubecontroller.Options{
		KrtDebugger: krt.GlobalDebugHandler,
	})
	assert.NoError(t, err)
	go controller.Run(stop)
	tt := assert.NewTracker[string](t)
	controller.RegisterEventHandler(gvk.Gateway, TrackerHandler(tt, kind.Gateway))

	file.WriteOrFail(t, filepath.Join(root, "gw.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: foo
  namespace: default
spec:
  servers:
  - port:
      number: 80
      protocol: HTTP2
      name: http
    hosts:
    - "*.example.com"`))

	tt.WaitOrdered("add/Gateway/default/foo")
	out := controller.Get(gvk.Gateway, "foo", "default")
	if out.Name != "foo" {
		t.Fatalf("expected config name foo, got %v", out.Name)
	}
	out = controller.Get(gvk.Gateway, "bar", "default")
	if out != nil {
		t.Fatalf("expected config to be nil, got %v", out)
	}

	file.WriteOrFail(t, filepath.Join(root, "gw2.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: bar
  namespace: default
spec:
  servers:
  - port:
      number: 80
      protocol: HTTP
      name: http
    hosts:
    - "*.example.com"`))
	tt.WaitOrdered("add/Gateway/default/bar")

	out = controller.Get(gvk.Gateway, "foo", "default")
	if out.Name != "foo" {
		t.Fatalf("expected config name foo, got %v", out.Name)
	}
	out = controller.Get(gvk.Gateway, "bar", "default")
	if out.Name != "bar" {
		t.Fatalf("expected config name bar, got %v", out.Name)
	}
}

func TestControllerList(t *testing.T) {
	stop := test.NewStop(t)
	root := t.TempDir()

	controller, err := NewController(root, "example.com", collections.Pilot, kubecontroller.Options{
		KrtDebugger: krt.GlobalDebugHandler,
	})
	assert.NoError(t, err)
	go controller.Run(stop)
	tt := assert.NewTracker[string](t)
	controller.RegisterEventHandler(gvk.ServiceEntry, TrackerHandler(tt, kind.ServiceEntry))
	controller.RegisterEventHandler(gvk.WorkloadEntry, TrackerHandler(tt, kind.WorkloadEntry))

	file.WriteOrFail(t, filepath.Join(root, "se.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: foo
  namespace: default
spec:
  hosts:
  - "*.example.com"
  ports:
  - number: 80
    name: http
    protocol: HTTP`))
	tt.WaitOrdered("add/ServiceEntry/default/foo")

	file.WriteOrFail(t, filepath.Join(root, "se2.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: bar
  namespace: default2
spec:
  hosts:
  - "*.example.com"
  ports:
  - number: 80
    name: http
    protocol: HTTP`))
	tt.WaitOrdered("add/ServiceEntry/default2/bar")
	out := controller.List(gvk.ServiceEntry, "default")
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %v", len(out))
	}

	file.WriteOrFail(t, filepath.Join(root, "we.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: foo
  namespace: default
spec:
  address: 127.0.0.1`))
	tt.WaitOrdered("add/WorkloadEntry/default/foo")

	file.WriteOrFail(t, filepath.Join(root, "we2.yaml"), []byte(`
apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: bar
  namespace: default2
spec:
  address: 127.0.0.2`))

	tt.WaitOrdered("add/WorkloadEntry/default2/bar")
	out = controller.List(gvk.WorkloadEntry, "default")
	if len(out) != 1 {
		t.Fatalf("expected 1 config, got %v", len(out))
	}
}

func TrackerHandler(tracker *assert.Tracker[string], k kind.Kind) func(o config.Config, n config.Config, e model.Event) {
	return func(o config.Config, n config.Config, e model.Event) {
		tracker.Record(fmt.Sprintf("%v/%v/%v", e, k, krt.GetKey(n)))
	}
}
