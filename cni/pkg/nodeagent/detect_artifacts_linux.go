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

package nodeagent

import (
	"fmt"
	"strings"

	"github.com/vishvananda/netlink"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/ipset"
	"istio.io/istio/cni/pkg/util"
)

// detectIptablesArtifacts checks for the presence of Istio iptables artifacts (specifically IPsets)
// on the host network to determine if a previous iptables-based deployment exists.
// Returns:
//   - true if iptables artifacts (IPsets) are detected
//   - false if no artifacts are detected or if detection fails
//   - error if there was a failure during detection
func detectIptablesArtifacts(enableIPv6 bool) (bool, error) {
	var detected bool
	var detectionErr error

	// Run the detection in the host network namespace
	err := util.RunAsHost(func() error {
		// Check if the IPv4 IPset exists
		v4Name := fmt.Sprintf(ipset.V4Name, config.ProbeIPSet)
		v4Exists, err := ipsetExists(v4Name)
		if err != nil {
			log.Debugf("failed to check for v4 IPset %s: %v", v4Name, err)
			detectionErr = fmt.Errorf("v4 IPset detection failed: %w", err)
			// Lets continue checking for v6 (if enabled)
		}

		if v4Exists {
			log.Infof("detected iptables artifact: IPset %s exists", v4Name)
			detected = true
			// Found v4 artifact, no need to check for v6
			return nil
		}

		// Check if the IPv6 IPset exists
		if enableIPv6 {
			v6Name := fmt.Sprintf(ipset.V6Name, config.ProbeIPSet)
			v6Exists, err := ipsetExists(v6Name)
			if err != nil {
				log.Debugf("failed to check for v6 IPset %s: %v", v6Name, err)
				if detectionErr != nil {
					detectionErr = fmt.Errorf("%w; v6 IPset detection failed: %w", detectionErr, err)
				} else {
					detectionErr = fmt.Errorf("v6 IPset detection failed: %w", err)
				}
			}

			if v6Exists {
				log.Infof("detected iptables artifact: IPset %s exists", v6Name)
				detected = true
			}
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("failed to run detection in host namespace: %w", err)
	}

	return detected, detectionErr
}

// ipsetExists checks if an IPset with the given name exists on the host.
// Returns:
//   - true, nil if the IPset exists
//   - false, nil if the IPset does not exist (expected for clean/fresh setup)
//   - false, error for any errors
func ipsetExists(name string) (bool, error) {
	_, err := netlink.IpsetList(name)
	if err == nil {
		// IPset exists
		return true, nil
	}

	if strings.Contains(err.Error(), "no such file") {
		// IPSet does not exist
		return false, nil
	}

	return false, err
}
