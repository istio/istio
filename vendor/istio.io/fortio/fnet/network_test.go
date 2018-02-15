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

package fnet // import "istio.io/fortio/fnet"

import (
	"testing"
)

func TestNormalizePort(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		output string
	}{
		{
			"port number only",
			"8080",
			":8080",
		},
		{
			"IPv4 host:port",
			"10.10.10.1:8080",
			"10.10.10.1:8080",
		},
		{
			"IPv6 [host]:port",
			"[2001:db1::1]:8080",
			"[2001:db1::1]:8080",
		},
	}

	for _, tc := range tests {
		port := NormalizePort(tc.input)
		if port != tc.output {
			t.Errorf("Test case %s failed to normailze port %s\n\texpected: %s\n\t  actual: %s",
				tc.name,
				tc.input,
				tc.output,
				port,
			)
		}
	}
}
