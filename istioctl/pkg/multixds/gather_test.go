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

package multixds

import (
	"testing"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pkg/kube"
)

func TestMakeSan(t *testing.T) {
	cases := []struct {
		revision string
		want     string
	}{
		{"", "istiod.istio-system.svc"},
		{"default", "istiod.istio-system.svc"},
		{"canary", "istiod-canary.istio-system.svc"},
	}
	for _, c := range cases {
		if got := makeSan("istio-system", c.revision); got != c.want {
			t.Errorf("makeSan(%q) = %q, want %q", c.revision, got, c.want)
		}
	}
}

func TestDefaultSan(t *testing.T) {
	cases := []struct {
		name string
		opts clioptions.CentralControlPlaneOptions
		want string
	}{
		{"ip address", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116:15012"}, "istiod.istio-system.svc"},
		{"localhost", clioptions.CentralControlPlaneOptions{Xds: "localhost:15012"}, "istiod.istio-system.svc"},
		{"ip without port", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116"}, "istiod.istio-system.svc"},
		{"dns name kept", clioptions.CentralControlPlaneOptions{Xds: "istiod.example.com:15012"}, ""},
		{"explicit authority wins", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116:15012", XDSSAN: "custom.san"}, "custom.san"},
		{"insecure skips", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116:15012", InsecureSkipVerify: true}, ""},
		{"plaintext skips", clioptions.CentralControlPlaneOptions{Xds: "172.18.6.116:15012", Plaintext: true}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defaultSan(&c.opts, "istio-system", kube.NewFakeClient())
			if c.opts.XDSSAN != c.want {
				t.Errorf("XDSSAN = %q, want %q", c.opts.XDSSAN, c.want)
			}
		})
	}
}
