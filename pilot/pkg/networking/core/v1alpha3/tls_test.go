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

package v1alpha3

import (
	"testing"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func TestVirtualServiceForHost(t *testing.T) {
	wildcard := &v1alpha3.VirtualService{Hosts: []string{"*"}}
	cnn := &v1alpha3.VirtualService{Hosts: []string{"www.cnn.com", "*.cnn.com", "*.com"}}
	cnnEdition := &v1alpha3.VirtualService{Hosts: []string{"edition.cnn.com"}}
	uk := &v1alpha3.VirtualService{Hosts: []string{"*.co.uk"}}
	configs := []model.Config{{Spec: wildcard}, {Spec: cnn}, {Spec: cnnEdition}, {Spec: uk}}

	tests := []struct {
		in   string
		want *v1alpha3.VirtualService
	}{
		{in: "www.cnn.com", want: cnn},
		{in: "money.cnn.com", want: cnn},
		{in: "edition.cnn.com", want: cnnEdition},
		{in: "bbc.co.uk", want: uk},
		{in: "www.wikipedia.org", want: wildcard},
	}

	f := makeVirtualServiceForHost(configs)
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if vs := f(model.Hostname(tt.in)); vs != tt.want {
				t.Fatalf("incorrect virtual service match")
			}
		})
	}
}
