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

package v1alpha3

import (
	"testing"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config/labels"
)

func TestMatchTLS(t *testing.T) {
	type args struct {
		match       *v1alpha3.TLSMatchAttributes
		proxyLabels labels.Collection
		gateways    map[string]bool
		port        int
		namespace   string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"source namespace match",
			args{
				match: &v1alpha3.TLSMatchAttributes{
					SourceNamespace: "foo",
				},
				namespace: "foo",
			},
			true,
		},
		{
			"source namespace not match",
			args{
				match: &v1alpha3.TLSMatchAttributes{
					SourceNamespace: "foo",
				},
				namespace: "bar",
			},
			false,
		},
		{
			"source namespace not match when empty",
			args{
				match: &v1alpha3.TLSMatchAttributes{
					SourceNamespace: "foo",
				},
				namespace: "",
			},
			false,
		},
		{
			"source namespace any",
			args{
				match:     &v1alpha3.TLSMatchAttributes{},
				namespace: "bar",
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchTLS(tt.args.match, tt.args.proxyLabels, tt.args.gateways, tt.args.port, tt.args.namespace); got != tt.want {
				t.Errorf("matchTLS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchTCP(t *testing.T) {
	type args struct {
		match       *v1alpha3.L4MatchAttributes
		proxyLabels labels.Collection
		gateways    map[string]bool
		port        int
		namespace   string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"source namespace match",
			args{
				match: &v1alpha3.L4MatchAttributes{
					SourceNamespace: "foo",
				},
				namespace: "foo",
			},
			true,
		},
		{
			"source namespace not match",
			args{
				match: &v1alpha3.L4MatchAttributes{
					SourceNamespace: "foo",
				},
				namespace: "bar",
			},
			false,
		},
		{
			"source namespace not match when empty",
			args{
				match: &v1alpha3.L4MatchAttributes{
					SourceNamespace: "foo",
				},
				namespace: "",
			},
			false,
		},
		{
			"source namespace any",
			args{
				match:     &v1alpha3.L4MatchAttributes{},
				namespace: "bar",
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchTCP(tt.args.match, tt.args.proxyLabels, tt.args.gateways, tt.args.port, tt.args.namespace); got != tt.want {
				t.Errorf("matchTLS() = %v, want %v", got, tt.want)
			}
		})
	}
}
