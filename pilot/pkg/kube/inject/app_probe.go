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

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"strconv"
)

const(
	// StatusPortCmdFlagName is the name of the command line flag passed to pilot-agent for sidecar readiness probe.
	// We reuse it for taking over application's readiness probing as well.
	// TODO: replace the hardcoded statusPort elsewhere by this variable as much as possible.
	StatusPortCmdFlagName = "statusPort"
)

// TODO(incfly): support more than one container probing.
func appProbePath(kind string, containers []corev1.Container) string {
	for _, c := range containers {
		probe := c.ReadinessProbe
		if kind == "live" {
			probe = c.LivenessProbe
		}
		if probe == nil || probe.Handler.HTTPGet == nil {
			continue
		}
		hp := probe.Handler.HTTPGet
		port := 0
		if hp.Port.Type == intstr.String {
			name := hp.Port.String()
			for _, cp := range c.Ports {
				if cp.Name == name {
					port = int(cp.ContainerPort)
					break
				}
			}
		} else {
			port = hp.Port.IntValue()
		}
		return fmt.Sprintf(":%v%v", port, hp.Path)
	}
	return ""
}

func rewriteAppHTTPProbe(spec *SidecarInjectionSpec, podSpec *corev1.PodSpec) {
	statusPort := -1
	pi := -1
	for _, c := range spec.Containers {
		// TODO: any constant refers to this container's name?
		if c.Name != "istio-proxy" {
			continue
		}
		for i, arg := range c.Args {
			if arg == StatusPortCmdFlagName {
				pi = i
				break
			}
		}
		if pi != -1 {
			statusPort, _ = strconv.Atoi(c.Args[pi+1])
		}
	}
	// pilot agent statusPort is not defined, skip changing application http probe.
	if statusPort == -1 {
		return
	}
	// Change the application containers' probe to point to sidecar's status port.
	rewriteProbe := func(probe *corev1.Probe, path string) {
		if probe == nil || probe.HTTPGet == nil {
			return
		}
		probe.HTTPGet.Path = path
		probe.HTTPGet.Port = intstr.FromInt(statusPort)
	}
	for _, c := range podSpec.Containers {
		rewriteProbe(c.ReadinessProbe, "/app/ready")
		rewriteProbe(c.LivenessProbe, "/app/live")
	}
}
