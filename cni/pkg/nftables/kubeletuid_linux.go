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

package nftables

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/procfs"
)

// getKubeletUIDFromPath finds the kubelet process UID by inspecting the proc filesystem path.
func getKubeletUIDFromPath(procPath string) (string, error) {
	fs, err := procfs.NewFS(procPath)
	if err != nil {
		return "", fmt.Errorf("failed to read procfs from %s: %v", procPath, err)
	}

	procs, err := fs.AllProcs()
	if err != nil {
		return "", fmt.Errorf("failed to read processes from %s: %v", procPath, err)
	}

	// Find kubelet process
	for _, proc := range procs {
		comm, err := proc.Comm()
		if err != nil {
			// Process might have exited, skip
			continue
		}

		if comm == "kubelet" {
			// Lets check the command line to ensure it's really kubelet
			cmdline, err := proc.CmdLine()
			if err != nil {
				continue
			}

			kubeletFound := false
			for _, arg := range cmdline {
				if strings.Contains(strings.ToLower(arg), "kubelet") {
					kubeletFound = true
					break
				}
			}
			if !kubeletFound {
				continue
			}

			// Get process status with UIDs
			status, err := proc.NewStatus()
			if err != nil {
				continue
			}

			realUID := status.UIDs[0]
			realUIDStr := strconv.FormatUint(realUID, 10)

			return realUIDStr, nil
		}
	}

	return "", fmt.Errorf("kubelet process not found in %s", procPath)
}
