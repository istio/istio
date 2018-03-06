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

package model_test

import (
	"reflect"
	"sort"
	"testing"

	routing "istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

func TestRejectConflictingEgressRules(t *testing.T) {
	cases := []struct {
		name  string
		in    map[string]*routing.EgressRule
		out   map[string]*routing.EgressRule
		valid bool
	}{
		{name: "no conflicts",
			in: map[string]*routing.EgressRule{"cnn": {
				Destination: &routing.IstioService{
					Service: "*cnn.com",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
			},
				"bbc": {
					Destination: &routing.IstioService{
						Service: "*bbc.com",
					},

					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http"},
						{Port: 443, Protocol: "https"},
					},
				},
			},
			out: map[string]*routing.EgressRule{"cnn": {
				Destination: &routing.IstioService{
					Service: "*cnn.com",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
			},
				"bbc": {
					Destination: &routing.IstioService{
						Service: "*bbc.com",
					},
					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http"},
						{Port: 443, Protocol: "https"},
					},
				},
			},
			valid: true},
		{name: "a conflict in a domain",
			in: map[string]*routing.EgressRule{"cnn2": {
				Destination: &routing.IstioService{
					Service: "*cnn.com",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
			},
				"cnn1": {
					Destination: &routing.IstioService{
						Service: "*cnn.com",
					},
					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http"},
						{Port: 443, Protocol: "https"},
					},
				},
			},
			out: map[string]*routing.EgressRule{
				"cnn1": {
					Destination: &routing.IstioService{
						Service: "*cnn.com",
					},
					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http"},
						{Port: 443, Protocol: "https"},
					},
				},
			},
			valid: false},
		{name: "a conflict in a domain, different ports",
			in: map[string]*routing.EgressRule{"cnn2": {
				Destination: &routing.IstioService{
					Service: "*cnn.com",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
			},
				"cnn1": {
					Destination: &routing.IstioService{
						Service: "*cnn.com",
					},
					Ports: []*routing.EgressRule_Port{
						{Port: 8080, Protocol: "http"},
						{Port: 8081, Protocol: "https"},
					},
				},
			},
			out: map[string]*routing.EgressRule{
				"cnn1": {
					Destination: &routing.IstioService{
						Service: "*cnn.com",
					},
					Ports: []*routing.EgressRule_Port{
						{Port: 8080, Protocol: "http"},
						{Port: 8081, Protocol: "https"},
					},
				},
			},
			valid: false},
		{name: "two conflicts, two rules rejected",
			in: map[string]*routing.EgressRule{"cnn2": {
				Destination: &routing.IstioService{
					Service: "*cnn.com",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
			},
				"cnn1": {
					Destination: &routing.IstioService{
						Service: "*cnn.com",
					},
					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http"},
						{Port: 443, Protocol: "https"},
					},
				},
				"cnn3": {
					Destination: &routing.IstioService{
						Service: "*cnn.com",
					},
					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http"},
						{Port: 443, Protocol: "https"},
					},
				},
			},
			out: map[string]*routing.EgressRule{
				"cnn1": {
					Destination: &routing.IstioService{
						Service: "*cnn.com",
					},
					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http"},
						{Port: 443, Protocol: "https"},
					},
				},
			},
			valid: false},
		{name: "no conflicts on port",
			in: map[string]*routing.EgressRule{"rule1": {
				Destination: &routing.IstioService{
					Service: "10.10.10.10",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 1000, Protocol: "tcp"},
				},
			},
				"rule2": {
					Destination: &routing.IstioService{
						Service: "10.10.10.11",
					},

					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http2"},
						{Port: 1000, Protocol: "tcp"},
					},
				},
			},
			out: map[string]*routing.EgressRule{"rule1": {
				Destination: &routing.IstioService{
					Service: "10.10.10.10",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 1000, Protocol: "tcp"},
				},
			},
				"rule2": {
					Destination: &routing.IstioService{
						Service: "10.10.10.11",
					},

					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http2"},
						{Port: 1000, Protocol: "tcp"},
					},
				},
			},
			valid: true},
		{name: "conflicts on port between tcp protocols",
			in: map[string]*routing.EgressRule{"rule1": {
				Destination: &routing.IstioService{
					Service: "10.10.10.10",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 1000, Protocol: "tcp"},
				},
			},
				"rule2": {
					Destination: &routing.IstioService{
						Service: "10.10.10.11",
					},

					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "http2"},
						{Port: 1000, Protocol: "mongo"},
					},
				},
			},
			out: map[string]*routing.EgressRule{"rule1": {
				Destination: &routing.IstioService{
					Service: "10.10.10.10",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 1000, Protocol: "tcp"},
				},
			},
			},
			valid: false},
		{name: "conflicts on port between http protocols",
			in: map[string]*routing.EgressRule{"rule1": {
				Destination: &routing.IstioService{
					Service: "10.10.10.10",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 1000, Protocol: "tcp"},
				},
			},
				"rule2": {
					Destination: &routing.IstioService{
						Service: "10.10.10.11",
					},

					Ports: []*routing.EgressRule_Port{
						{Port: 80, Protocol: "mongo"},
						{Port: 1000, Protocol: "tcp"},
					},
				},
			},
			out: map[string]*routing.EgressRule{"rule1": {
				Destination: &routing.IstioService{
					Service: "10.10.10.10",
				},
				Ports: []*routing.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 1000, Protocol: "tcp"},
				},
			},
			},
			valid: false},
	}

	for _, c := range cases {
		in := mapToArr(c.in)
		want := mapToArr(c.out)
		got, errs := model.RejectConflictingEgressRules(in)
		if (errs == nil) != c.valid {
			t.Errorf("RejectConflictingEgressRules failed on %s: got valid=%v but wanted valid=%v, errs=%v",
				c.name, errs == nil, c.valid, errs)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("RejectConflictingEgressRules failed on %s: got\n%v but wanted \n%v, errs= %v",
				c.name, got, want, errs)
		}
	}
}

func mapToArr(rmap map[string]*routing.EgressRule) []model.Config {
	m := make([]model.Config, 0, len(rmap))
	for k, r := range rmap {
		m = append(m, model.Config{
			ConfigMeta: model.ConfigMeta{
				Name: k,
			},
			Spec: r,
		})
	}

	sort.SliceStable(m, func(i, j int) bool {
		return m[i].Key() < m[j].Key()
	})

	return m
}
