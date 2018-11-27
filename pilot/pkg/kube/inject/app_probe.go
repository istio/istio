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
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pkg/log"
)

const (
	// StatusPortCmdFlagName is the name of the command line flag passed to pilot-agent for sidecar readiness probe.
	// We reuse it for taking over application's readiness probing as well.
	// TODO: replace the hardcoded statusPort elsewhere by this variable as much as possible.
	StatusPortCmdFlagName = "statusPort"

	// TODO: any constant refers to this container's name?
	istioProxyContainerName = "istio-proxy"
)

var (
	// regex pattern for to extract the pilot agent probing port.
	// Supported format, --statusPort, -statusPort, --statusPort=15020.
	statusPortPattern = regexp.MustCompile(fmt.Sprintf(`^-{1,2}%s(=(?P<port>\d+))?$`, StatusPortCmdFlagName))
)

func rewriteAppHTTPProbe(spec *SidecarInjectionSpec, podSpec *corev1.PodSpec) {
	statusPort := -1
	pi := -1
	if spec == nil || podSpec == nil {
		return
	}
	for _, c := range spec.Containers {
		if c.Name != istioProxyContainerName {
			continue
		}
		for i, arg := range c.Args {
			// Skip for unrelated args.
			match := statusPortPattern.FindAllStringSubmatch(strings.TrimSpace(arg), -1)
			if len(match) != 1 {
				continue
			}
			groups := statusPortPattern.SubexpNames()
			portStr := ""
			for ind, s := range match[0] {
				if groups[ind] == "port" {
					portStr = s
					break
				}
			}
			// Port not found from current arg, extract from next arg.
			if portStr == "" {
				// Matches the regex pattern, but without actual values provided.
				if len(c.Args) <= i+1 {
					log.Errorf("No statusPort value provided, skip app probe rewriting")
					return
				}
				portStr = c.Args[i+1]
			}
			p, err := strconv.Atoi(portStr)
			if err != nil {
				log.Errorf("Failed to convert statusPort to int %v, err %v", portStr, err)
				return
			}
			statusPort = p
			break
		}
	}
	// Pilot agent statusPort is not defined, skip changing application http probe.
	if statusPort == -1 {
		return
	}

	// Change the application containers' probe to point to sidecar's status port.
	rewriteProbe := func(probe *corev1.Probe, portMap map[string]int32) {
		if probe == nil || probe.HTTPGet == nil {
			return
		}
		httpGet := probe.HTTPGet
		header := corev1.HTTPHeader{
			Name:  status.IstioAppPortHeader,
			Value: httpGet.Port.String(),
		}
		// A named port, resolve by looking at port map.
		if httpGet.Port.Type == intstr.String {
			port, exists := portMap[httpGet.Port.StrVal]
			if !exists {
				log.Errorf("named port not found in the map skip rewriting probing %v", *probe)
				return
			}
			header.Value = strconv.Itoa(int(port))
		}
		httpGet.HTTPHeaders = append(httpGet.HTTPHeaders, header)
		httpGet.Port = intstr.FromInt(statusPort)
	}
	for _, c := range podSpec.Containers {
		// Skip sidecar container.
		if c.Name == istioProxyContainerName {
			continue
		}
		portMap := map[string]int32{}
		for _, p := range c.Ports {
			portMap[p.Name] = p.ContainerPort
		}
		rewriteProbe(c.ReadinessProbe, portMap)
		rewriteProbe(c.LivenessProbe, portMap)
	}
}
