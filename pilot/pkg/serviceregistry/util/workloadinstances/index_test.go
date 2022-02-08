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

	"github.com/google/go-cmp/cmp"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/spiffe"
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
		Ports: []*networking.Port{
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
	// Setup a couple of workload instances for test. These will be selected by the `selector` SE
	wi1 := &model.WorkloadInstance{
		Name:      selector.Name,
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	wi2 := &model.WorkloadInstance{
		Name:      "some-other-name",
		Namespace: selector.Namespace,
		Endpoint: &model.IstioEndpoint{
			Address:        "3.3.3.3",
			Labels:         map[string]string{"app": "wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI(selector.Name, "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	wi3 := &model.WorkloadInstance{
		Name:      "another-name",
		Namespace: "dns-selector",
		Endpoint: &model.IstioEndpoint{
			Address:        "2.2.2.2",
			Labels:         map[string]string{"app": "dns-wle"},
			ServiceAccount: spiffe.MustGenSpiffeURI("dns-selector", "default"),
			TLSMode:        model.IstioMutualTLSModeLabel,
		},
	}

	index := NewIndex()

	// test update
	index.Insert(wi1)
	index.Insert(wi2)
	index.Insert(wi3)

	workloadinstances := FindAllInIndex(index, ByServiceSelector(selector.Namespace, labels.Collection{{"app": "wle"}}))

	expected := map[string]*model.WorkloadInstance{
		wi1.Name: wi1,
		wi2.Name: wi2,
	}

	if diff := cmp.Diff(2, len(workloadinstances)); diff != "" {
		t.Errorf("FindAllInIndex() returned unexpected number of workload instances: %v", diff)
	}

	for _, wi := range workloadinstances {
		if diff := cmp.Diff(expected[wi.Name], wi); diff != "" {
			t.Errorf("FindAllInIndex() returned unexpected workload instance %q: %v", wi.Name, diff)
		}
	}

	deleted := index.Delete(wi1)
	if diff := cmp.Diff(wi1, deleted); diff != "" {
		t.Errorf("1st Delete() returned unexpected value: %v", diff)
	}

	deleted = index.Delete(wi1)
	if diff := cmp.Diff((*model.WorkloadInstance)(nil), deleted); diff != "" {
		t.Errorf("2nd Delete() returned unexpected value: %v", diff)
	}
}
