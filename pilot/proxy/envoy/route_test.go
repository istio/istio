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

package envoy

import (
	"strings"
	"testing"
)

var (
	testHosts = []struct {
		hosts    []string
		expected string
	}{
		{[]string{"service"}, "service"},
		{[]string{""}, ""},
		{[]string{"a", "b"}, ""},
		{[]string{"a", ""}, ""},
		{[]string{"service.example"}, "service.example"},
		{[]string{"serviceA.default.cluster", "serviceB.default.cluster"}, "default.cluster"},
		{[]string{"serviceA.a.cluster", "serviceB.b.cluster"}, "cluster"},
	}
)

func TestSharedHost(t *testing.T) {
	if out := sharedHost(); out != nil {
		t.Errorf("sharedHost() => Got %q, expected nil", out)
	}
	for _, test := range testHosts {
		shared := make([][]string, 0)
		for _, host := range test.hosts {
			shared = append(shared, strings.Split(host, "."))
		}
		out := sharedHost(shared...)
		if strings.Join(out, ".") != test.expected {
			t.Errorf("sharedHost(%v) => Got %q, expected %q", test.hosts, out, test.expected)
		}
	}
}

func TestBuildListenerSSLContext(t *testing.T) {
	const dir = "/some/testing/dir"
	context := buildListenerSSLContext(dir)
	if !context.RequireClientCertificate {
		t.Errorf("buildListenerSSLContext(%v) => Got RequireClientCertificate: %v, expected true.",
			dir, context.RequireClientCertificate)
	}
}
