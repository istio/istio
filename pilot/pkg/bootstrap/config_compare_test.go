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

package bootstrap

import (
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
)

func TestNeedsPush(t *testing.T) {
	cases := []struct {
		name     string
		prev     config.Config
		curr     config.Config
		expected bool
	}{
		{
			name: "different gvk",
			prev: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "acme2-v1",
					Namespace:        "not-default",
				},
				Spec: &networking.VirtualService{},
			},
			curr: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             "acme2-v1",
					Namespace:        "not-default",
				},
				Spec: &networking.VirtualService{},
			},
			expected: true,
		},
		{
			name: "same gvk label change",
			prev: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Labels:           map[string]string{"test": "test-v1"},
				},
				Spec: &networking.VirtualService{},
			},
			curr: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Labels:           map[string]string{"test": "test-v2"},
				},
				Spec: &networking.VirtualService{},
			},
			expected: false,
		},
		{
			name: "same gvk spec change",
			prev: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Labels:           map[string]string{"test": "test-v1"},
				},
				Spec: &networking.VirtualService{Hosts: []string{"test-host"}},
			},
			curr: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.VirtualService,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Labels:           map[string]string{"test": "test-v1"},
				},
				Spec: &networking.VirtualService{Hosts: []string{"test-host", "test-host2"}},
			},
			expected: true,
		},
		{
			name: "config with istio.io label",
			prev: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.Ingress,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Labels:           map[string]string{constants.AlwaysPushLabel: "true"},
				},
			},
			curr: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.Ingress,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Labels:           map[string]string{constants.AlwaysPushLabel: "true"},
				},
			},
			expected: true,
		},
		{
			name: "config with istio.io annotation",
			prev: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.Ingress,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Annotations:      map[string]string{constants.AlwaysPushLabel: "true"},
				},
			},
			curr: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.Ingress,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Annotations:      map[string]string{constants.AlwaysPushLabel: "true"},
				},
			},
			expected: true,
		},
		{
			name: "non istio resources",
			prev: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TCPRoute,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Labels:           map[string]string{"test": "test-v1"},
				},
				Spec: &networking.VirtualService{},
			},
			curr: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.TCPRoute,
					Name:             "acme2-v1",
					Namespace:        "not-default",
					Labels:           map[string]string{"test": "test-v2"},
				},
				Spec: &networking.VirtualService{},
			},
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := needsPush(c.prev, c.curr); got != c.expected {
				t.Errorf("unexpected needsPush result. expected: %v got: %v", c.expected, got)
			}
		})
	}
}
