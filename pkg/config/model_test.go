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

package config

import (
	"fmt"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/test/config"
	"istio.io/istio/pkg/test/util/assert"
)

func TestDeepCopy(t *testing.T) {
	cfg := Config{
		Meta: Meta{
			Name:              "name1",
			Namespace:         "zzz",
			CreationTimestamp: time.Now(),
			Labels:            map[string]string{"app": "test-app"},
			Annotations:       map[string]string{"policy.istio.io/checkRetries": "3"},
		},
		Spec: &networking.Gateway{},
	}

	copied := cfg.DeepCopy()

	if diff := cmp.Diff(copied, cfg, protocmp.Transform()); diff != "" {
		t.Fatalf("cloned config is not identical: %v", diff)
	}

	copied.Labels["app"] = "cloned-app"
	copied.Annotations["policy.istio.io/checkRetries"] = "0"
	if cfg.Labels["app"] == copied.Labels["app"] ||
		cfg.Annotations["policy.istio.io/checkRetries"] == copied.Annotations["policy.istio.io/checkRetries"] {
		t.Fatal("Did not deep copy labels and annotations")
	}

	// change the copied gateway to see if the original config is not effected
	copiedGateway := copied.Spec.(*networking.Gateway)
	copiedGateway.Selector = map[string]string{"app": "test"}

	gateway := cfg.Spec.(*networking.Gateway)
	if gateway.Selector != nil {
		t.Errorf("Original gateway is mutated")
	}
}

type TestStruct struct {
	Name string `json:"name"`
}

func TestDeepCopyTypes(t *testing.T) {
	cases := []struct {
		input  Spec
		modify func(c Spec) Spec
		option cmp.Option
	}{
		// Istio type
		{
			&networking.VirtualService{Gateways: []string{"foo"}},
			func(c Spec) Spec {
				c.(*networking.VirtualService).Gateways = []string{"bar"}
				return c
			},
			protocmp.Transform(),
		},
		// Kubernetes type
		{
			&corev1.PodSpec{ServiceAccountName: "foobar"},
			func(c Spec) Spec {
				c.(*corev1.PodSpec).ServiceAccountName = "bar"
				return c
			},
			nil,
		},
		// gateway-api type
		{
			&k8s.GatewayClassSpec{ControllerName: "foo"},
			func(c Spec) Spec {
				c.(*k8s.GatewayClassSpec).ControllerName = "bar"
				return c
			},
			nil,
		},
		// mock type
		{
			&config.MockConfig{Key: "foobar"},
			func(c Spec) Spec {
				c.(*config.MockConfig).Key = "bar"
				return c
			},
			protocmp.Transform(),
		},
		// XDS type, to test golang/proto
		{
			&cluster.Cluster{Name: "foobar"},
			func(c Spec) Spec {
				c.(*cluster.Cluster).Name = "bar"
				return c
			},
			protocmp.Transform(),
		},
		// Random struct pointer
		{
			&TestStruct{Name: "foobar"},
			func(c Spec) Spec {
				c.(*TestStruct).Name = "bar"
				return c
			},
			nil,
		},
		// Random struct
		{
			TestStruct{Name: "foobar"},
			func(c Spec) Spec {
				x := c.(TestStruct)
				x.Name = "bar"
				return x
			},
			nil,
		},
		// Slice
		{
			[]string{"foo"},
			func(c Spec) Spec {
				x := c.([]string)
				x[0] = "a"
				return x
			},
			nil,
		},
		// Array
		{
			[1]string{"foo"},
			func(c Spec) Spec {
				x := c.([1]string)
				x[0] = "a"
				return x
			},
			nil,
		},
		// Map
		{
			map[string]string{"a": "b"},
			func(c Spec) Spec {
				x := c.(map[string]string)
				x["a"] = "x"
				return x
			},
			nil,
		},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%T", tt.input), func(t *testing.T) {
			cpy := DeepCopy(tt.input)
			if diff := cmp.Diff(tt.input, cpy, tt.option); diff != "" {
				t.Fatalf("Type was %T now is %T. Diff: %v", tt.input, cpy, diff)
			}
			changed := tt.modify(tt.input)
			if cmp.Equal(cpy, changed, tt.option) {
				t.Fatal("deep copy allowed modification")
			}
		})
	}
}

func TestApplyJSON(t *testing.T) {
	cases := []struct {
		input  Spec
		json   string
		output Spec
		option cmp.Option
	}{
		// Istio type
		{
			input:  &networking.VirtualService{},
			json:   `{"gateways":["foobar"],"fake-field":1}`,
			output: &networking.VirtualService{Gateways: []string{"foobar"}},
		},
		// Kubernetes type
		{
			input:  &corev1.PodSpec{},
			json:   `{"serviceAccountName":"foobar","fake-field":1}`,
			output: &corev1.PodSpec{ServiceAccountName: "foobar"},
		},
		// gateway-api type
		{
			input:  &k8s.GatewayClassSpec{},
			json:   `{"controllerName":"foobar","fake-field":1}`,
			output: &k8s.GatewayClassSpec{ControllerName: "foobar"},
		},
		// mock type
		{
			input:  &config.MockConfig{},
			json:   `{"key":"foobar","fake-field":1}`,
			output: &config.MockConfig{Key: "foobar"},
		},
		// XDS type, to test golang/proto
		{
			input:  &cluster.Cluster{},
			json:   `{"name":"foobar","fake-field":1}`,
			output: &cluster.Cluster{Name: "foobar"},
			option: protocmp.Transform(),
		},
		// Random struct
		{
			input:  &TestStruct{},
			json:   `{"name":"foobar","fake-field":1}`,
			output: &TestStruct{Name: "foobar"},
		},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%T", tt.input), func(t *testing.T) {
			if err := ApplyJSON(tt.input, tt.json); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.input, tt.output, protocmp.Transform()); diff != "" {
				t.Fatalf("Diff: %v", diff)
			}
			if err := ApplyJSONStrict(tt.input, tt.json); err == nil {
				t.Fatal("expected error from non existent field in strict mode")
			}
		})
	}
}

func TestToJSON(t *testing.T) {
	cases := []struct {
		input Spec
		json  string
	}{
		// Istio type
		{
			input: &networking.VirtualService{Gateways: []string{"foobar"}},
			json:  `{"gateways":["foobar"]}`,
		},
		// Kubernetes type
		{
			input: &corev1.PodSpec{ServiceAccountName: "foobar"},
			json:  `{"containers":null,"serviceAccountName":"foobar"}`,
		},
		// gateway-api type
		{
			input: &k8s.GatewayClassSpec{ControllerName: "foobar"},
			json:  `{"controllerName":"foobar"}`,
		},
		// mock type
		{
			input: &config.MockConfig{Key: "foobar"},
			json:  `{"key":"foobar"}`,
		},
		// XDS type, to test golang/proto
		{
			input: &cluster.Cluster{Name: "foobar"},
			json:  `{"name":"foobar"}`,
		},
		// Random struct
		{
			input: &TestStruct{Name: "foobar"},
			json:  `{"name":"foobar"}`,
		},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%T", tt.input), func(t *testing.T) {
			jb, err := ToJSON(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if string(jb) != tt.json {
				t.Fatalf("got %v want %v", string(jb), tt.json)
			}
		})
	}
}

func TestEqualsTypes(t *testing.T) {
	mk := func(a Spec) Config {
		return Config{
			Spec: a,
		}
	}
	cases := []struct {
		input  Config
		modify func(c Spec) Spec
		option cmp.Option
	}{
		// Istio type
		{
			mk(&networking.VirtualService{Gateways: []string{"foo"}}),
			func(c Spec) Spec {
				c.(*networking.VirtualService).Gateways = []string{"bar"}
				return c
			},
			protocmp.Transform(),
		},
		// Kubernetes type
		{
			mk(&corev1.PodSpec{ServiceAccountName: "foobar"}),
			func(c Spec) Spec {
				c.(*corev1.PodSpec).ServiceAccountName = "bar"
				return c
			},
			nil,
		},
		// gateway-api type
		{
			mk(&k8s.GatewayClassSpec{ControllerName: "foo"}),
			func(c Spec) Spec {
				c.(*k8s.GatewayClassSpec).ControllerName = "bar"
				return c
			},
			nil,
		},
		// mock type
		{
			mk(&config.MockConfig{Key: "foobar"}),
			func(c Spec) Spec {
				c.(*config.MockConfig).Key = "bar"
				return c
			},
			protocmp.Transform(),
		},
		// XDS type, to test golang/proto
		{
			mk(&cluster.Cluster{Name: "foobar"}),
			func(c Spec) Spec {
				c.(*cluster.Cluster).Name = "bar"
				return c
			},
			protocmp.Transform(),
		},
		// Random struct pointer
		{
			mk(&TestStruct{Name: "foobar"}),
			func(c Spec) Spec {
				c.(*TestStruct).Name = "bar"
				return c
			},
			nil,
		},
		// Random struct
		{
			mk(TestStruct{Name: "foobar"}),
			func(c Spec) Spec {
				x := c.(TestStruct)
				x.Name = "bar"
				return x
			},
			nil,
		},
		// Slice
		{
			mk([]string{"foo"}),
			func(c Spec) Spec {
				x := c.([]string)
				x[0] = "a"
				return x
			},
			nil,
		},
		// Array
		{
			mk([1]string{"foo"}),
			func(c Spec) Spec {
				x := c.([1]string)
				x[0] = "a"
				return x
			},
			nil,
		},
		// Map
		{
			mk(map[string]string{"a": "b"}),
			func(c Spec) Spec {
				x := c.(map[string]string)
				x["a"] = "x"
				return x
			},
			nil,
		},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%T", tt.input.Spec), func(t *testing.T) {
			cpy := tt.input.DeepCopy()
			assert.Equal(t, tt.input.Equals(&cpy), true)
			tt.input.Spec = tt.modify(tt.input.Spec)
			assert.Equal(t, tt.input.Equals(&cpy), false)
		})
	}
}
