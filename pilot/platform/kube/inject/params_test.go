// Copyright 2018 Istio Authors
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

package inject

import "testing"

const (
	// This is the hub to expect in
	// platform/kube/inject/testdata/frontend.yaml.injected and the
	// other .injected "want" YAMLs
	unitTestHub = "docker.io/istio"

	// Tag name should be kept in sync with value in
	// platform/kube/inject/refresh.sh
	unitTestTag = "unittest"
)

func TestImageName(t *testing.T) {
	want := "docker.io/istio/proxy_init:latest"
	if got := InitImageName("docker.io/istio", "latest", true); got != want {
		t.Errorf("InitImageName() failed: got %q want %q", got, want)
	}
	want = "docker.io/istio/proxy_debug:latest"
	if got := ProxyImageName("docker.io/istio", "latest", true); got != want {
		t.Errorf("ProxyImageName() failed: got %q want %q", got, want)
	}
	want = "docker.io/istio/proxy:latest"
	if got := ProxyImageName("docker.io/istio", "latest", false); got != want {
		t.Errorf("ProxyImageName(debug:false) failed: got %q want %q", got, want)
	}
}
