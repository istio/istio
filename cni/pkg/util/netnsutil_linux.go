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

package util

import (
	netns "github.com/containernetworking/plugins/pkg/ns"

	pconstants "istio.io/istio/cni/pkg/constants"
)

// RunAsHost executes the given function `f` within the host network namespace
func RunAsHost(f func() error) error {
	if f == nil {
		return nil
	}

	// A network namespace switch is definitely not required in this case, which helps with testing
	if pconstants.HostNetNSPath == pconstants.SelfNetNSPath {
		return f()
	}
	return netns.WithNetNSPath(pconstants.HostNetNSPath, func(_ netns.NetNS) error {
		return f()
	})
}
