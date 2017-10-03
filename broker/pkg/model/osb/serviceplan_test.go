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

func TestNewServicePlan(t *testing.T) {
	sp := &brokerconfig.ServicePlan{
		Services: []string{
			"service-class/default/productpage-service-class",
		},
		Plan: &brokerconfig.CatalogPlan{
			Name:        "istio-yearly",
			Id:          "cdd76b03-a28b-4638-b4e2-19ee44b36db7",
			Description: "yearly subscription",
		},
	}
	got := NewServicePlan(sp)
	want := &ServicePlan{
		Name:        sp.GetPlan().GetName(),
		ID:          sp.GetPlan().GetId(),
		Description: sp.GetPlan().GetDescription(),
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("failed: \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}
}
