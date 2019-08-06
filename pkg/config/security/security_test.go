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

// This file describes the abstract model of services (and their instances) as
// represented in Istio. This model is independent of the underlying platform
// (Kubernetes, Mesos, etc.). Platform specific adapters found populate the
// model object with various fields, from the metadata found in the platform.
// The platform independent proxy code uses the representation in the model to
// generate the configuration files for the Layer 7 proxy sidecar. The proxy
// code is specific to individual proxy implementations

package security_test

import (
	"reflect"
	"testing"

	"istio.io/istio/pkg/config/security"
)

func TestParseJwksURI(t *testing.T) {
	cases := []struct {
		in                   string
		expected             security.JwksInfo
		expectedErrorMessage string
	}{
		{
			in:                   "foo.bar.com",
			expectedErrorMessage: `URI scheme "" is not supported`,
		},
		{
			in:                   "tcp://foo.bar.com:abc",
			expectedErrorMessage: `URI scheme "tcp" is not supported`,
		},
		{
			in:                   "http://foo.bar.com:abc",
			expectedErrorMessage: `strconv.Atoi: parsing "abc": invalid syntax`,
		},
		{
			in: "http://foo.bar.com",
			expected: security.JwksInfo{
				Hostname: "foo.bar.com",
				Scheme:   "http",
				Port:     80,
				UseSSL:   false,
			},
		},
		{
			in: "https://foo.bar.com",
			expected: security.JwksInfo{
				Hostname: "foo.bar.com",
				Scheme:   "https",
				Port:     443,
				UseSSL:   true,
			},
		},
		{
			in: "http://foo.bar.com:1234",
			expected: security.JwksInfo{
				Hostname: "foo.bar.com",
				Scheme:   "http",
				Port:     1234,
				UseSSL:   false,
			},
		},
		{
			in: "https://foo.bar.com:1234/secure/key",
			expected: security.JwksInfo{
				Hostname: "foo.bar.com",
				Scheme:   "https",
				Port:     1234,
				UseSSL:   true,
			},
		},
	}
	for _, c := range cases {
		actual, err := security.ParseJwksURI(c.in)
		if err != nil {
			if c.expectedErrorMessage != err.Error() {
				t.Errorf("ParseJwksURI(%s): expected error (%s), got (%v)", c.in, c.expectedErrorMessage, err)
			}
		} else {
			if c.expectedErrorMessage != "" {
				t.Errorf("ParseJwksURI(%s): expected error (%s), got no error", c.in, c.expectedErrorMessage)
			}
			if !reflect.DeepEqual(c.expected, actual) {
				t.Errorf("expected %+v, got %+v", c.expected, actual)
			}
		}
	}
}
