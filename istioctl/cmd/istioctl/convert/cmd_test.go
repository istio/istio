// Copyright 2017 Istio Authors.
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

package convert

import (
	"io"
	"os"
	"testing"

	"istio.io/istio/pilot/test/util"
)

func TestCommand(t *testing.T) {
	tt := []struct {
		in  []string
		out string
	}{
		{in: []string{"rule-default-route.yaml"},
			out: "rule-default-route.yaml"},

		{in: []string{"rule-weighted-route.yaml"},
			out: "rule-weighted-route.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-content-route.yaml"},
			out: "rule-content-route.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-regex-route.yaml"},
			out: "rule-regex-route.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-fault-injection.yaml"},
			out: "rule-fault-injection.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-redirect-injection.yaml"},
			out: "rule-redirect-injection.yaml"},

		{in: []string{"rule-default-route.yaml",
			"rule-websocket-route.yaml"},
			out: "rule-websocket-route.yaml"},

		{in: []string{"rule-default-route-append-headers.yaml"},
			out: "rule-default-route-append-headers.yaml"},

		{in: []string{"rule-default-route-cors-policy.yaml"},
			out: "rule-default-route-cors-policy.yaml"},

		{in: []string{"rule-default-route-mirrored.yaml"},
			out: "rule-default-route-mirrored.yaml"},

		{in: []string{"egress-rule-google.yaml"},
			out: "egress-rule-google.yaml"},

		{in: []string{"egress-rule-httpbin.yaml"},
			out: "egress-rule-httpbin.yaml"},

		{in: []string{"egress-rule-tcp-wikipedia-cidr.yaml"},
			out: "egress-rule-tcp-wikipedia-cidr.yaml"},

		{in: []string{"egress-rule-wildcard-httpbin.yaml"},
			out: "egress-rule-wildcard-httpbin.yaml"},
	}

	for _, tc := range tt {
		t.Run(tc.out, func(t *testing.T) {
			readers := make([]io.Reader, 0, len(tc.in))
			for _, filename := range tc.in {
				file, err := os.Open("testdata/v1alpha1/" + filename)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close() // nolint: errcheck
				readers = append(readers, file)
			}

			outFilename := "testdata/v1alpha3/" + tc.out
			out, err := os.Create(outFilename)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close() // nolint: errcheck

			if err := convertConfigs(readers, out); err != nil {
				t.Fatalf("Unexpected error converting configs: %v", err)
			}

			util.CompareYAML(outFilename, t)
		})
	}
}
