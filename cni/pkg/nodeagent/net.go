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
	"strings"

	corev1 "k8s.io/api/core/v1"

	"istio.io/api/annotation"
	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/util"
)

func getPodLevelTrafficOverrides(pod *corev1.Pod) config.PodLevelOverrides {
	// If true, the pod will run in 'ingress mode'. This is intended to be used for "ingress" type workloads which handle
	// non-mesh traffic on inbound, and send to the mesh on outbound.
	// Basically, this just disables inbound redirection.
	podCfg := config.PodLevelOverrides{IngressMode: false}

	if ingressMode, present := util.CheckBooleanAnnotation(pod, annotation.AmbientBypassInboundCapture.Name); present {
		podCfg.IngressMode = ingressMode
	}

	podCfg.DNSProxy = config.PodDNSUnset

	if dnsCapture, present := util.CheckBooleanAnnotation(pod, annotation.AmbientDnsCapture.Name); present {
		if dnsCapture {
			podCfg.DNSProxy = config.PodDNSEnabled
		} else {
			podCfg.DNSProxy = config.PodDNSDisabled
		}
	}

	if virt, hasVirt := pod.Annotations[annotation.IoIstioRerouteVirtualInterfaces.Name]; hasVirt {
		virtInterfaces := strings.Split(virt, ",")
		for _, splitVirt := range virtInterfaces {
			trim := strings.TrimSpace(splitVirt)
			if trim != "" {
				podCfg.VirtualInterfaces = append(podCfg.VirtualInterfaces, trim)
			}
		}
	}

	return podCfg
}
