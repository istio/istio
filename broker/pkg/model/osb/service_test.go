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

package osb

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"

	brokerconfig "istio.io/api/broker/v1/config"
)

func TestAddPlan(t *testing.T) {
	c := new(Service)
	s := &ServicePlan{
		Name: "test plan",
	}
	c.AddPlan(s)

	for _, got := range c.Plans {
		if !reflect.DeepEqual(got, *s) {
			t.Errorf("failed: \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(s))
		}
	}
}

func TestNewService(t *testing.T) {
	sp := &brokerconfig.ServiceClass{
		Deployment: &brokerconfig.Deployment{
			Instance: "productpage",
		},
		Entry: &brokerconfig.CatalogEntry{
			Name:        "istio-bookinfo-productpage",
			Id:          "4395a443-f49a-41b0-8d14-d17294cf612f",
			Description: "A book info service",
		},
	}
	got := NewService(sp)
	want := &Service{
		Name:        sp.GetEntry().GetName(),
		ID:          sp.GetEntry().GetId(),
		Description: sp.GetEntry().GetDescription(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("failed: \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}
}
