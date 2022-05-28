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

package main

import (
	"log"
	"regexp"
	"testing"

	"istio.io/istio/pkg/test/util/assert"
)

func TestClusterNameRegex(t *testing.T) {
	regexList, err := ReadClusterNameRegex(metricsFileName)
	if err != nil {
		log.Fatal(err)
	}
	regexString := BuildClusterNameRegex(regexList)
	regex, _ := regexp.Compile(regexString)

	type fields struct {
		input string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name:   "kubernetes service match upstream_cx_total",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.clusrr.local.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_total",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.clusrr.local.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "external service match upstream_cx_total",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_total"},
			want:   ".upstream_cx_total",
		},
		{
			name:   "kubernetes service match upstream_cx_active",
			fields: fields{input: "cluster.outbound|80||foo.bar.svc.clusrr.local.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
		{
			name:   "kubernetes service with subset match upstream_cx_active",
			fields: fields{input: "cluster.outbound|80|stable|foo.bar.svc.clusrr.local.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
		{
			name:   "external service match upstream_cx_active",
			fields: fields{input: "cluster.outbound|443||istio.io.upstream_cx_active"},
			want:   ".upstream_cx_active",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, regex.FindString(test.fields.input))
		})
	}
}
