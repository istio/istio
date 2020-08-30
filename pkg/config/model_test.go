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
	"sigs.k8s.io/service-apis/apis/v1alpha1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/test/config"
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

	if diff := cmp.Diff(copied, cfg); diff != "" {
		t.Fatalf("cloned config is not identical: %v", diff)
	}

	copied.Labels["app"] = "cloned-app"
	copied.Annotations["policy.istio.io/checkRetries"] = "0"
	if cfg.Labels["app"] == copied.Labels["app"] ||
		cfg.Annotations["policy.istio.io/checkRetries"] == copied.Annotations["policy.istio.io/checkRetries"] {
		t.Fatalf("Did not deep copy labels and annotations")
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
	Name string
}

func TestDeepCopyTypes(t *testing.T) {
	cases := []struct {
		input  ConfigSpec
		modify func(c ConfigSpec) ConfigSpec
		option cmp.Option
	}{
		// Istio type
		{
			&networking.VirtualService{Gateways: []string{"foo"}},
			func(c ConfigSpec) ConfigSpec {
				c.(*networking.VirtualService).Gateways = []string{"bar"}
				return c
			},
			nil,
		},
		// Kubernetes type
		{
			&corev1.PodSpec{ServiceAccountName: "foobar"},
			func(c ConfigSpec) ConfigSpec {
				c.(*corev1.PodSpec).ServiceAccountName = "bar"
				return c
			},
			nil,
		},
		//service-apis type
		{
			&v1alpha1.GatewayClassSpec{Controller: "foo"},
			func(c ConfigSpec) ConfigSpec {
				c.(*v1alpha1.GatewayClassSpec).Controller = "bar"
				return c
			},
			nil,
		},
		// mock type
		{
			&config.MockConfig{Key: "foobar"},
			func(c ConfigSpec) ConfigSpec {
				c.(*config.MockConfig).Key = "bar"
				return c
			},
			nil,
		},
		// XDS type, to test golang/proto
		{
			&cluster.Cluster{Name: "foobar"},
			func(c ConfigSpec) ConfigSpec {
				c.(*cluster.Cluster).Name = "bar"
				return c
			},
			protocmp.Transform(),
		},
		// Random struct
		{
			&TestStruct{Name: "foobar"},
			func(c ConfigSpec) ConfigSpec {
				c.(*TestStruct).Name = "bar"
				return c
			},
			nil,
		},
	}
	for _, tt := range cases {
		t.Run(fmt.Sprintf("%T", tt.input), func(t *testing.T) {
			copy := DeepCopy(tt.input)
			if diff := cmp.Diff(tt.input, copy, tt.option); diff != "" {
				t.Fatalf("Type was %T now is %T. Diff: %v", tt.input, copy, diff)
			}
			changed := tt.modify(tt.input)
			if cmp.Equal(copy, changed, tt.option) {
				t.Fatalf("deep copy allowed modification")
			}
		})
	}
}
