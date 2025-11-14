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

// getKubeletUIDFromPath finds the kubelet or kubelite process UID by inspecting the proc filesystem path.
// In standard Kubernetes distributions, it looks for the "kubelet" process.
// On some platforms like MicroK8s, where multiple k8s components are consolidated, it looks for the "kubelite" process.
func getKubeletUIDFromPath(procPath string) (string, error) {
	fs, err := procfs.NewFS(procPath)
	if err != nil {
		return "", fmt.Errorf("failed to read procfs from %s: %v", procPath, err)
	}

	procs, err := fs.AllProcs()
	if err != nil {
		return "", fmt.Errorf("failed to read processes from %s: %v", procPath, err)
	}

	// List of process names to search for, in order of preference
	processNames := []string{"kubelet", "kubelite"}

	for _, proc := range procs {
		comm, err := proc.Comm()
		if err != nil {
			// Process might have exited, skip
			continue
		}

		for _, targetName := range processNames {
			if comm == targetName {
				// Lets check the command line to ensure it's really the target process
				cmdline, err := proc.CmdLine()
				if err != nil {
					continue
				}

				// Verify that this process is actually related to kubelet by checking
				// if "kubelet" appears in any of the command line arguments.
				// This works for both:
				//   - Standard kubelet: /usr/bin/kubelet [args...]
				//   - MicroK8s kubelite: /snap/microk8s/.../kubelite --kubelet-args-file=...
				processFound := false
				for _, arg := range cmdline {
					if strings.Contains(strings.ToLower(arg), "kubelet") {
						processFound = true
						break
					}
				}
				if !processFound {
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
	}

	return "", fmt.Errorf("kubelet or kubelite process not found in %s", procPath)
}
