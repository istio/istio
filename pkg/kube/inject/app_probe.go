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
	"strings"

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
	updateNamedPort := func(p *Prober, portMap map[int32]bool, containerName string) *Prober {
		if p == nil {
			return nil
		}
		if p.GRPC != nil {
			grpcPort := p.GRPC.Port
			if !isPortIncluded(grpcPort, portMap) {
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
			// First, check if the string port refers to a named port in the specific container
			portValInt, err := getPortValAsInt(pod, probePort.StrVal, containerName)
			// could not determine the match.
			if err != nil {
				return nil
			}
			if !isPortIncluded(portValInt, portMap) {
				return nil
			}
			*probePort = intstr.FromInt32(portValInt)
		} else if probePort.Type == intstr.Int {
			if !isPortIncluded(probePort.IntVal, portMap) {
				return nil
			}
			*probePort = intstr.FromInt32(probePort.IntVal)
		} else if probePort.IntVal == targetPort {
			// Already is rewritten
			return nil
		}
		return p
	}
	portMap := getPortInclusionOrExclusionList(pod)
	for _, c := range allContainers(pod) {
		if c.Name == ProxyContainerName {
			continue
		}
		readyz, livez, startupz, prestopz, poststartz := status.FormatProberURL(c.Name)

		if h := updateNamedPort(kubeProbeToInternalProber(c.ReadinessProbe), portMap, c.Name); h != nil {
			out[readyz] = h
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.LivenessProbe), portMap, c.Name); h != nil {
			out[livez] = h
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.StartupProbe), portMap, c.Name); h != nil {
			out[startupz] = h
		}
		if c.Lifecycle != nil {
			if h := updateNamedPort(kubeLifecycleHandlerToInternalProber(c.Lifecycle.PreStop), portMap, c.Name); h != nil {
				out[prestopz] = h
			}
			if h := updateNamedPort(kubeLifecycleHandlerToInternalProber(c.Lifecycle.PostStart), portMap, c.Name); h != nil {
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

func getPortValAsInt(pod *corev1.Pod, strVal string, containerName string) (portValInt int32, err error) {
	containerPortNum := lookupNamedPort(pod, strVal, containerName)
	if containerPortNum > 0 {
		// Found a matching named port in container
		portValInt = containerPortNum
	} else {
		// No named port found, try to parse as integer
		var portValInt64 int64
		portValInt64, err = strconv.ParseInt(strVal, 10, 32)
		portValInt = int32(portValInt64)
	}
	return
}

// lookupNamedPort finds the port number for a named port in the pod containers
// If containerName is specified, it will first look for the port in that container before checking other containers
func lookupNamedPort(pod *corev1.Pod, name string, containerName string) int32 {
	// First look in the specified container if provided
	if containerName != "" {
		// Check regular containers
		for _, container := range pod.Spec.Containers {
			if container.Name == containerName {
				for _, port := range container.Ports {
					if port.Name == name {
						return port.ContainerPort
					}
				}
				break // Found the container but no matching port
			}
		}

		// Check init containers
		for _, container := range pod.Spec.InitContainers {
			if container.Name == containerName {
				for _, port := range container.Ports {
					if port.Name == name {
						return port.ContainerPort
					}
				}
				break // Found the container but no matching port
			}
		}
	}
	return 0
}

func getPortInclusionOrExclusionList(pod *corev1.Pod) map[int32]bool {
	annotations := pod.Annotations
	if annotations == nil {
		// No annotations means include all ports (return empty map)
		return make(map[int32]bool)
	}

	includePortsStr := annotations[annotation.SidecarTrafficIncludeInboundPorts.Name]
	excludePortsStr := annotations[annotation.SidecarTrafficExcludeInboundPorts.Name]

	// Include all ports by default
	includedPorts := make(map[int32]bool)

	if includePortsStr != "" {
		if includePortsStr == "*" {
			// Include all ports, then exclude any specified
			return applyPortExclusion(includedPorts, excludePortsStr)
		}
		return parsePortList(includePortsStr, true)
	}

	// If only exclusions are specified
	return applyPortExclusion(includedPorts, excludePortsStr)
}

func applyPortExclusion(base map[int32]bool, excludePortsStr string) map[int32]bool {
	if excludePortsStr == "" {
		return base
	}

	for _, portStr := range splitPorts(excludePortsStr) {
		if port := extractPortAsInt(portStr); port != 0 {
			base[int32(port)] = false
		} else {
			log.Errorf("Failed to parse port %v from excludeInboundPorts", portStr)
		}
	}
	return base
}

func parsePortList(portList string, include bool) map[int32]bool {
	ports := make(map[int32]bool)
	for _, portStr := range splitPorts(portList) {
		if port := extractPortAsInt(portStr); port != 0 {
			ports[int32(port)] = include
		} else {
			log.Errorf("Failed to parse port %v from includeInboundPorts", portStr)
		}
	}
	return ports
}

func allContainers(pod *corev1.Pod) []corev1.Container {
	return append(slices.Clone(pod.Spec.InitContainers), pod.Spec.Containers...)
}

// patchRewriteProbe generates the patch for webhook.
func patchRewriteProbe(annotations map[string]string, pod *corev1.Pod, defaultPort int32) {
	includedAndExcludedPorts := getPortInclusionOrExclusionList(pod)
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
		convertProbe(&c, statusPort, includedAndExcludedPorts)
		pod.Spec.Containers[i] = c
	}
	for i, c := range pod.Spec.InitContainers {
		// Skip sidecar container.
		if c.Name == ProxyContainerName {
			continue
		}
		convertProbe(&c, statusPort, includedAndExcludedPorts)
		pod.Spec.InitContainers[i] = c
	}
}

func convertProbe(c *corev1.Container, statusPort int, includedAndExcludedPorts map[int32]bool) {
	readyz, livez, startupz, prestopz, poststartz := status.FormatProberURL(c.Name)

	if shouldPatchProbe(c.ReadinessProbe, includedAndExcludedPorts) {
		if probePatch := convertAppProber(c.ReadinessProbe, readyz, statusPort); probePatch != nil {
			c.ReadinessProbe = probePatch
		}
	}

	if shouldPatchProbe(c.LivenessProbe, includedAndExcludedPorts) {
		if probePatch := convertAppProber(c.LivenessProbe, livez, statusPort); probePatch != nil {
			c.LivenessProbe = probePatch
		}
	}

	if shouldPatchProbe(c.StartupProbe, includedAndExcludedPorts) {
		if probePatch := convertAppProber(c.StartupProbe, startupz, statusPort); probePatch != nil {
			c.StartupProbe = probePatch
		}
	}

	if c.Lifecycle != nil {
		if shouldPatchLifecycleHandler(c.Lifecycle.PreStop, includedAndExcludedPorts) {
			if lifecycleHandlerPatch := convertAppLifecycleHandler(c.Lifecycle.PreStop, prestopz, statusPort); lifecycleHandlerPatch != nil {
				c.Lifecycle.PreStop = lifecycleHandlerPatch
			}
		}

		if shouldPatchLifecycleHandler(c.Lifecycle.PostStart, includedAndExcludedPorts) {
			if lifecycleHandlerPatch := convertAppLifecycleHandler(c.Lifecycle.PostStart, poststartz, statusPort); lifecycleHandlerPatch != nil {
				c.Lifecycle.PostStart = lifecycleHandlerPatch
			}
		}
	}
}

func shouldPatchProbe(probe *corev1.Probe, includedAndExcludedPorts map[int32]bool) bool {
	if probe == nil {
		return false
	}

	if len(includedAndExcludedPorts) == 0 {
		return true
	}

	var probePort int32
	if probe.HTTPGet != nil {
		probePort = probe.HTTPGet.Port.IntVal
	} else if probe.TCPSocket != nil {
		probePort = probe.TCPSocket.Port.IntVal
	} else if probe.GRPC != nil {
		probePort = probe.GRPC.Port
	}

	return isPortIncluded(probePort, includedAndExcludedPorts)
}

// if port is specifically included
func isPortIncluded(port int32, ports map[int32]bool) bool {
	included, exists := ports[port]
	if included && exists {
		return true
	} else if exists && !included {
		return false
	}

	if !exists {
		for _, val := range ports {
			if val {
				return false
			}
		}
	}

	return true
}

func shouldPatchLifecycleHandler(handler *corev1.LifecycleHandler, includedAndExcludedPorts map[int32]bool) bool {
	if handler == nil {
		return false
	}

	var handlerPort int32
	if handler.HTTPGet != nil {
		handlerPort = handler.HTTPGet.Port.IntVal
	} else if handler.TCPSocket != nil {
		handlerPort = handler.TCPSocket.Port.IntVal
	}
	return isPortIncluded(handlerPort, includedAndExcludedPorts)
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

// extractPortAsInt extracts a port number as integer from a string
// Returns 0 if the port string cannot be parsed as an integer
func extractPortAsInt(portStr string) int {
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil {
		return 0
	}
	return port
}
