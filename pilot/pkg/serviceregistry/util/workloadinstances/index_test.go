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

package workloadinstances

import (
	"testing"
	"time"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/util/assert"
)

var GlobalTime = time.Now()

// ServiceEntry with a selector
var selector = &config.Config{
	Meta: config.Meta{
		GroupVersionKind:  gvk.ServiceEntry,
		Name:              "selector",
		Namespace:         "selector",
		CreationTimestamp: GlobalTime,
	},
	Spec: &networking.ServiceEntry{
		Hosts: []string{"selector.com"},
		Ports: []*networking.ServicePort{
			{Number: 444, Name: "tcp-444", Protocol: "tcp"},
			{Number: 445, Name: "http-445", Protocol: "http"},
		},
		WorkloadSelector: &networking.WorkloadSelector{
			Labels: map[string]string{"app": "wle"},
		},
		Resolution: networking.ServiceEntry_STATIC,
	},
}

func TestIndex(t *testing.T) {
	// Setup a couple of workload instances for test
	wi1 := &model.WorkloadInstance{
		Name:      selector.Name,
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Addresses:      []string{"2.2.2.2"},
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURIForTrustDomain("cluster.local", selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	wi2 := &model.WorkloadInstance{
		Name:      "some-other-name",
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Addresses:      []string{"3.3.3.3"},
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURIForTrustDomain("cluster.local", selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	wi3 := &model.WorkloadInstance{
		Name:      "another-name",
		Namespace: "dns-selector",
		Endpoint: &model.IstioEndpoint{
			Addresses:      []string{"2.2.2.2"},
			Labels:         map[string]string{"app": "dns-wle"},
			ServiceAccount: spiffe.MustGenSpiffeURIForTrustDomain("cluster.local", "dns-selector", "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	index := NewIndex()

	// test update
	index.Insert(wi1)
	index.Insert(wi2)
	index.Insert(wi3)

	assert.Equal(t, wi1, index.Get(wi1))
	assert.Equal(t, wi2, index.Get(wi2))
	assert.Equal(t, wi3, index.Get(wi3))

	verifyGetByIP := func(ip string, expected []*model.WorkloadInstance) {
		assert.Equal(t, expected, index.GetByIP(ip))
	}

	// GetByIP should return 2 workload instances

	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi3, wi1})

	// Delete should return previously inserted value

	deleted := index.Delete(wi1)
	assert.Equal(t, wi1, deleted)

	// GetByIP should return 1 workload instance

	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi3})

	// Delete should return nil since there is no such element in the index

	deleted = index.Delete(wi1)
	assert.Equal(t, nil, deleted)

	// GetByIP should return nil

	verifyGetByIP("1.1.1.1", nil)
}

func TestIndex_FindAll(t *testing.T) {
	// Setup a couple of workload instances for test. These will be selected by the `selector` SE
	wi1 := &model.WorkloadInstance{
		Name:      selector.Name,
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Addresses: []string{"2.2.2.2"},
			Labels:    map[string]string{"app": "wle"}, // should match
		},
	}

	wi2 := &model.WorkloadInstance{
		Name:      "same-ip",
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Addresses: []string{"2.2.2.2"},
			Labels:    map[string]string{"app": "wle"}, // should match
		},
	}

	wi3 := &model.WorkloadInstance{
		Name:      "another-ip",
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Addresses: []string{"3.3.3.3"},
			Labels:    map[string]string{"app": "another-wle"}, // should not match because of another label
		},
	}

	wi4 := &model.WorkloadInstance{
		Name:      "another-name",
		Namespace: "another-namespace", // should not match because of another namespace
		Endpoint: &model.IstioEndpoint{
			Addresses: []string{"2.2.2.2"},
			Labels:    map[string]string{"app": "wle"},
		},
	}

	index := NewIndex()

	// test update
	index.Insert(wi1)
	index.Insert(wi2)
	index.Insert(wi3)
	index.Insert(wi4)

	// test search by service selector
	actual := FindAllInIndex(index, ByServiceSelector(selector.Namespace, labels.Instance{"app": "wle"}))
	want := []*model.WorkloadInstance{wi1, wi2}

	assert.Equal(t, len(want), len(actual))

	got := map[string]*model.WorkloadInstance{}
	for _, instance := range actual {
		got[instance.Name] = instance
	}

	for _, expected := range want {
		assert.Equal(t, expected, got[expected.Name])
	}
}
