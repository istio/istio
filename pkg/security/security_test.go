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

package security

import (
	"testing"
)

func TestSdsCertificateConfigFromResourceName(t *testing.T) {
	cases := []struct {
		name             string
		resource         string
		root             bool
		key              bool
		output           SdsCertificateConfig
		rootResourceName string
		resourceName     string
	}{
		{
			"cert",
			"file-cert:cert~key",
			false,
			true,
			SdsCertificateConfig{"cert", "key", ""},
			"",
			"file-cert:cert~key",
		},
		{
			"root cert",
			"file-root:root",
			true,
			false,
			SdsCertificateConfig{"", "", "root"},
			"file-root:root",
			"",
		},
		{
			"invalid prefix",
			"file:root",
			false,
			false,
			SdsCertificateConfig{"", "", ""},
			"",
			"",
		},
		{
			"invalid contents",
			"file-root:root~extra",
			false,
			false,
			SdsCertificateConfig{"", "", ""},
			"",
			"",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := SdsCertificateConfigFromResourceName(tt.resource)
			if got != tt.output {
				t.Fatalf("got %v, expected %v", got, tt.output)
			}
			if root := got.IsRootCertificate(); root != tt.root {
				t.Fatalf("unexpected isRootCertificate got %v, expected %v", root, tt.root)
			}
			if key := got.IsKeyCertificate(); key != tt.key {
				t.Fatalf("unexpected IsKeyCertificate got %v, expected %v", key, tt.key)
			}
			if root := got.GetRootResourceName(); root != tt.rootResourceName {
				t.Fatalf("unexpected GetRootResourceName got %v, expected %v", root, tt.rootResourceName)
			}
			if cert := got.GetResourceName(); cert != tt.resourceName {
				t.Fatalf("unexpected GetRootResourceName got %v, expected %v", cert, tt.resourceName)
			}
		})
	}
}
