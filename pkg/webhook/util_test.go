// Copyright 2019 Istio Authors
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

package webhook

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/mcp/testing/testcerts"
)

func TestLoadCaCertPem(t *testing.T) {
	cases := []struct {
		name      string
		cert      []byte
		wantError bool
	}{
		{
			name:      "valid pem",
			cert:      testcerts.CACert,
			wantError: false,
		},
		{
			name:      "pem decode error",
			cert:      append([]byte("-----codec"), testcerts.CACert...),
			wantError: true,
		},
		{
			name:      "pem wrong type",
			wantError: true,
		},
		{
			name:      "invalid x509",
			cert:      testcerts.BadCert,
			wantError: true,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %s", i, c.name), func(tt *testing.T) {
			err := verifyCABundle(c.cert)
			if err != nil {
				if !c.wantError {
					tt.Fatalf("unexpected error: got error %q", err)
				}
			} else {
				if c.wantError {
					tt.Fatal("expected error")
				}
			}
		})
	}
}
