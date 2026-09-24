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

// kubeletProcess describes how to identify a kubelet-hosting process by name.
type kubeletProcess struct {
	// name is the value expected in /proc/<pid>/comm.
	name string

	// requireKubeletInCmdline, when true, requires that at least one cmdline argument
	// contains the substring "kubelet" before the process is accepted. Set to false for
	// platforms (e.g. k3s) where kubelet is embedded directly in the binary and argv do
	// not include any kubelet name.
	requireKubeletInCmdline bool
}

// kubeletProcesses lists platforms whose kubelet-hosting process we can identify,
// in order of preference.
var kubeletProcesses = []kubeletProcess{
	// Standard Kubernetes: /usr/bin/kubelet [args...]
	{"kubelet", true},
	// MicroK8s consolidates k8s components; kubelite path contains "kubelet".
	{"kubelite", true},
	// k3s embeds kubelet directly in the k3s binary (k3s server / k3s agent).
	// No kubelet substring appears in argv under a default install.
	{"k3s", false},
}

// getKubeletUIDFromPath finds the UID of the kubelet-hosting process by inspecting procPath.
func getKubeletUIDFromPath(procPath string) (string, error) {
	fs, err := procfs.NewFS(procPath)
	if err != nil {
		return "", fmt.Errorf("failed to read procfs from %s: %v", procPath, err)
	}

	procs, err := fs.AllProcs()
	if err != nil {
		return "", fmt.Errorf("failed to read processes from %s: %v", procPath, err)
	}

	for _, proc := range procs {
		comm, err := proc.Comm()
		if err != nil {
			// Process might have exited, skip
			continue
		}

		for _, kp := range kubeletProcesses {
			if comm != kp.name {
				continue
			}

			if kp.requireKubeletInCmdline {
				cmdline, err := proc.CmdLine()
				if err != nil {
					continue
				}
				found := false
				for _, arg := range cmdline {
					if strings.Contains(strings.ToLower(arg), "kubelet") {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			status, err := proc.NewStatus()
			if err != nil {
				continue
			}

			return strconv.FormatUint(status.UIDs[0], 10), nil
		}
	}

	return "", fmt.Errorf("no kubelet process found in %s", procPath)
}
