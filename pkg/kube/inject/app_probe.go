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

// Package inject implements kube-inject or webhoook autoinject feature to inject sidecar.
// This file is focused on rewriting Kubernetes app probers to support mutual TLS.
package inject

import (
	"encoding/json"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
)

// ShouldRewriteAppHTTPProbers returns if we should rewrite apps' probers config.
func ShouldRewriteAppHTTPProbers(annotations map[string]string, specSetting bool) bool {
	if annotations != nil {
		if value, ok := annotations[annotation.SidecarRewriteAppHTTPProbers.Name]; ok {
			if isSetInAnnotation, err := strconv.ParseBool(value); err == nil {
				return isSetInAnnotation
			}
		}
	}
	return specSetting
}

// FindSidecar returns the pointer to the first container whose name matches the "istio-proxy".
func FindSidecar(pod *corev1.Pod) *corev1.Container {
	return FindContainerFromPod(ProxyContainerName, pod)
}

// FindContainerFromPod returns the pointer to the first container whose name matches in init containers or regular containers
func FindContainerFromPod(name string, pod *corev1.Pod) *corev1.Container {
	if c := FindContainer(name, pod.Spec.Containers); c != nil {
		return c
	}
	return FindContainer(name, pod.Spec.InitContainers)
}

// FindContainer returns the pointer to the first container whose name matches.
func FindContainer(name string, containers []corev1.Container) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

// convertAppProber returns an overwritten `Probe` for pilot agent to take over.
func convertAppProber(probe *corev1.Probe, newURL string, statusPort int) *corev1.Probe {
	if probe == nil {
		return nil
	}
	if probe.HTTPGet != nil {
		return convertAppProberHTTPGet(probe, newURL, statusPort)
	} else if probe.TCPSocket != nil {
		return convertAppProberTCPSocket(probe, newURL, statusPort)
	} else if probe.GRPC != nil {
		return convertAppProberGRPC(probe, newURL, statusPort)
	}

	return nil
}

// rewriteHTTPGetAction rewrites a HTTPGet action with given URL and port.
// Also rewrites the scheme to HTTP if the scheme is HTTPS
// as pilot agent uses https to request application endpoint.
func rewriteHTTPGetAction(action *corev1.HTTPGetAction, url string, port int) {
	action.Port = intstr.FromInt32(int32(port))
	action.Path = url
	// Kubelet -> HTTP -> Pilot Agent -> HTTPS -> Application
	if action.Scheme == corev1.URISchemeHTTPS {
		action.Scheme = corev1.URISchemeHTTP
	}
}

// convertAppLifecycleHandler returns an overwritten `LifecycleHandler` for pilot agent to take over.
func convertAppLifecycleHandler(lifecycleHandler *corev1.LifecycleHandler, newURL string, statusPort int) *corev1.LifecycleHandler {
	if lifecycleHandler == nil {
		return nil
	}
	if lifecycleHandler.HTTPGet != nil {
		return convertAppLifecycleHandlerHTTPGet(lifecycleHandler, newURL, statusPort)
	}
	if lifecycleHandler.TCPSocket != nil {
		return convertAppLifecycleHandlerTCPSocket(lifecycleHandler, newURL, statusPort)
	}
	return nil
}

// convertAppLifecycleHandlerHTTPGet returns an overwritten `LifecycleHandler` with HTTPGet for pilot agent to take over.
func convertAppLifecycleHandlerHTTPGet(lifecycleHandler *corev1.LifecycleHandler, newURL string, statusPort int) *corev1.LifecycleHandler {
	lh := lifecycleHandler.DeepCopy()
	rewriteHTTPGetAction(lh.HTTPGet, newURL, statusPort)
	return lh
}

// convertAppLifecycleHandlerTCPSocket returns an overwritten `LifecycleHandler` with TCPSocket for pilot agent to take over.
func convertAppLifecycleHandlerTCPSocket(lifecycleHandler *corev1.LifecycleHandler, newURL string, statusPort int) *corev1.LifecycleHandler {
	lh := lifecycleHandler.DeepCopy()
	// the sidecar intercepts all tcp connections, so we change it to a HTTP probe and the sidecar will check tcp
	lh.HTTPGet = &corev1.HTTPGetAction{}
	rewriteHTTPGetAction(lh.HTTPGet, newURL, statusPort)
	lh.TCPSocket = nil
	return lh
}

// convertAppProberHTTPGet returns an overwritten `Probe` (HttpGet) for pilot agent to take over.
func convertAppProberHTTPGet(probe *corev1.Probe, newURL string, statusPort int) *corev1.Probe {
	p := probe.DeepCopy()
	rewriteHTTPGetAction(p.HTTPGet, newURL, statusPort)
	return p
}

// convertAppProberTCPSocket returns an overwritten `Probe` (TcpSocket) for pilot agent to take over.
func convertAppProberTCPSocket(probe *corev1.Probe, newURL string, statusPort int) *corev1.Probe {
	p := probe.DeepCopy()
	// the sidecar intercepts all tcp connections, so we change it to a HTTP probe and the sidecar will check tcp
	p.HTTPGet = &corev1.HTTPGetAction{}
	rewriteHTTPGetAction(p.HTTPGet, newURL, statusPort)
	p.TCPSocket = nil
	return p
}

// convertAppProberGRPC returns an overwritten `Probe` (gRPC) for pilot agent to take over.
func convertAppProberGRPC(probe *corev1.Probe, newURL string, statusPort int) *corev1.Probe {
	p := probe.DeepCopy()
	// the sidecar intercepts all gRPC connections, so we change it to a HTTP probe and the sidecar will check gRPC
	p.HTTPGet = &corev1.HTTPGetAction{}
	rewriteHTTPGetAction(p.HTTPGet, newURL, statusPort)
	// For gRPC prober, we change to HTTP,
	// and pilot agent uses gRPC to request application prober endpoint.
	// Kubelet -> HTTP -> Pilot Agent -> gRPC -> Application
	p.GRPC = nil
	return p
}

type KubeAppProbers map[string]*Prober

// Prober represents a single container prober
type Prober struct {
	HTTPGet        *corev1.HTTPGetAction   `json:"httpGet,omitempty"`
	TCPSocket      *corev1.TCPSocketAction `json:"tcpSocket,omitempty"`
	GRPC           *corev1.GRPCAction      `json:"grpc,omitempty"`
	TimeoutSeconds int32                   `json:"timeoutSeconds,omitempty"`
}

// DumpAppProbers returns a json encoded string as `status.KubeAppProbers`.
// Also update the probers so that all usages of named port will be resolved to integer.
func DumpAppProbers(pod *corev1.Pod, targetPort int32) string {
	out := KubeAppProbers{}
	updateNamedPort := func(p *Prober, portMap map[int32]bool) *Prober {
		if p == nil {
			return nil
		}
		if p.GRPC != nil {
			grpcPort := p.GRPC.Port
			if _, exists := portMap[grpcPort]; !exists && len(portMap) > 0 {
				return nil
			}
			// don't need to update for gRPC probe port as it only supports integer
			return p
		}
		if p.HTTPGet == nil && p.TCPSocket == nil {
			return nil
		}

		var probePort *intstr.IntOrString
		if p.HTTPGet != nil {
			probePort = &p.HTTPGet.Port
		} else {
			probePort = &p.TCPSocket.Port
		}

		if probePort.Type == intstr.String {
			portValInt64, _ := strconv.ParseInt(probePort.StrVal, 10, 32)
			portValInt := int32(portValInt64)
			if _, exists := portMap[portValInt]; !exists && len(portMap) > 0 {
				return nil
			}
			*probePort = intstr.FromInt32(portValInt)
		} else if probePort.Type == intstr.Int {
			if _, exists := portMap[probePort.IntVal]; !exists && len(portMap) > 0 {
				return nil
			}
			*probePort = intstr.FromInt32(probePort.IntVal)
		} else if probePort.IntVal == targetPort {
			// Already is rewritten
			return nil
		}
		return p
	}
	portMap := getIncludedPorts(pod)
	for _, c := range allContainers(pod) {
		if c.Name == ProxyContainerName {
			continue
		}
		readyz, livez, startupz, prestopz, poststartz := status.FormatProberURL(c.Name)

		if h := updateNamedPort(kubeProbeToInternalProber(c.ReadinessProbe), portMap); h != nil {
			out[readyz] = h
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.LivenessProbe), portMap); h != nil {
			out[livez] = h
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.StartupProbe), portMap); h != nil {
			out[startupz] = h
		}
		if c.Lifecycle != nil {
			if h := updateNamedPort(kubeLifecycleHandlerToInternalProber(c.Lifecycle.PreStop), portMap); h != nil {
				out[prestopz] = h
			}
			if h := updateNamedPort(kubeLifecycleHandlerToInternalProber(c.Lifecycle.PostStart), portMap); h != nil {
				out[poststartz] = h
			}
		}
	}
	// prevent generate '{}'
	if len(out) == 0 {
		return ""
	}
	b, err := json.Marshal(out)
	if err != nil {
		log.Errorf("failed to serialize the app prober config %v", err)
		return ""
	}
	return string(b)
}

func getIncludedPorts(pod *corev1.Pod) map[int32]bool {
	// Get pod annotations
	blankPorts := make(map[int32]bool)
	annotations := pod.Annotations
	if annotations == nil {
		// If no annotations, include all ports by default
		return blankPorts
	}

	includedPorts := make(map[int32]bool)

	includeInboundPortsKey := annotation.SidecarTrafficIncludeInboundPorts.Name
	// Check if includeInboundPorts annotation is present
	if includePortsStr, exists := annotations[includeInboundPortsKey]; exists && includePortsStr != "" {
		// If includeInboundPorts is specified, only include those ports
		if includePortsStr == "*" {
			// "*" means include all ports
			return blankPorts
		}

		// Parse comma-separated list of ports
		for _, portStr := range splitPorts(includePortsStr) {
			if port, err := strconv.Atoi(portStr); err == nil {
				includedPorts[int32(port)] = true
			} else {
				log.Errorf("Failed to parse port %v from includeInboundPorts: %v", portStr, err)
			}
		}
	} else {
		// If includeInboundPorts is not specified, include all ports by default
		includedPorts = blankPorts

		excludeInboundPortsKey := annotation.SidecarTrafficExcludeInboundPorts.Name
		// Then exclude ports specified in excludeInboundPorts
		if excludePortsStr, exists := annotations[excludeInboundPortsKey]; exists && excludePortsStr != "" {
			for _, portStr := range splitPorts(excludePortsStr) {
				if port, err := strconv.Atoi(portStr); err == nil {
					delete(includedPorts, int32(port))
				} else {
					log.Errorf("Failed to parse port %v from excludeInboundPorts: %v", portStr, err)
				}
			}
		}
	}
	return includedPorts
}

func allContainers(pod *corev1.Pod) []corev1.Container {
	return append(slices.Clone(pod.Spec.InitContainers), pod.Spec.Containers...)
}

// patchRewriteProbe generates the patch for webhook.
func patchRewriteProbe(annotations map[string]string, pod *corev1.Pod, defaultPort int32) {
	statusPort := int(defaultPort)
	if v, f := annotations[annotation.SidecarStatusPort.Name]; f {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Errorf("Invalid annotation %v=%v: %v", annotation.SidecarStatusPort.Name, v, err)
		}
		statusPort = p
	}
	for i, c := range pod.Spec.Containers {
		// Skip sidecar container.
		if c.Name == ProxyContainerName {
			continue
		}
		convertProbe(&c, statusPort)
		pod.Spec.Containers[i] = c
	}
	for i, c := range pod.Spec.InitContainers {
		// Skip sidecar container.
		if c.Name == ProxyContainerName {
			continue
		}
		convertProbe(&c, statusPort)
		pod.Spec.InitContainers[i] = c
	}
}

func convertProbe(c *corev1.Container, statusPort int) {
	readyz, livez, startupz, prestopz, poststartz := status.FormatProberURL(c.Name)
	if probePatch := convertAppProber(c.ReadinessProbe, readyz, statusPort); probePatch != nil {
		c.ReadinessProbe = probePatch
	}
	if probePatch := convertAppProber(c.LivenessProbe, livez, statusPort); probePatch != nil {
		c.LivenessProbe = probePatch
	}
	if probePatch := convertAppProber(c.StartupProbe, startupz, statusPort); probePatch != nil {
		c.StartupProbe = probePatch
	}
	if c.Lifecycle != nil {
		if lifecycleHandlerPatch := convertAppLifecycleHandler(c.Lifecycle.PreStop, prestopz, statusPort); lifecycleHandlerPatch != nil {
			c.Lifecycle.PreStop = lifecycleHandlerPatch
		}
		if lifecycleHandlerPatch := convertAppLifecycleHandler(c.Lifecycle.PostStart, poststartz, statusPort); lifecycleHandlerPatch != nil {
			c.Lifecycle.PostStart = lifecycleHandlerPatch
		}
	}
}

// kubeProbeToInternalProber converts a Kubernetes Probe to an Istio internal Prober
func kubeProbeToInternalProber(probe *corev1.Probe) *Prober {
	if probe == nil {
		return nil
	}

	if probe.HTTPGet != nil {
		return &Prober{
			HTTPGet:        probe.HTTPGet,
			TimeoutSeconds: probe.TimeoutSeconds,
		}
	}

	if probe.TCPSocket != nil {
		return &Prober{
			TCPSocket:      probe.TCPSocket,
			TimeoutSeconds: probe.TimeoutSeconds,
		}
	}

	if probe.GRPC != nil {
		return &Prober{
			GRPC:           probe.GRPC,
			TimeoutSeconds: probe.TimeoutSeconds,
		}
	}

	return nil
}

// kubeLifecycleHandlerToInternalProber converts a Kubernetes LifecycleHandler to an Istio internal Prober
func kubeLifecycleHandlerToInternalProber(lifecycelHandler *corev1.LifecycleHandler) *Prober {
	if lifecycelHandler == nil {
		return nil
	}
	if lifecycelHandler.HTTPGet != nil {
		return &Prober{
			HTTPGet: lifecycelHandler.HTTPGet,
		}
	}
	if lifecycelHandler.TCPSocket != nil {
		return &Prober{
			TCPSocket: lifecycelHandler.TCPSocket,
		}
	}

	return nil
}
