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
	"istio.io/istio/pilot/pkg/features"
	"istio.io/pkg/log"
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
func FindSidecar(containers []corev1.Container) *corev1.Container {
	return FindContainer(ProxyContainerName, containers)
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
	} else if probe.TCPSocket != nil && features.RewriteTCPProbes {
		return convertAppProberTCPSocket(probe, newURL, statusPort)
	} else if probe.GRPC != nil {
		return convertAppProberGRPC(probe, newURL, statusPort)
	}

	return nil
}

// convertAppProberHTTPGet returns an overwritten `Probe` (HttpGet) for pilot agent to take over.
func convertAppProberHTTPGet(probe *corev1.Probe, newURL string, statusPort int) *corev1.Probe {
	p := probe.DeepCopy()
	// Change the application container prober config.
	p.HTTPGet.Port = intstr.FromInt(statusPort)
	p.HTTPGet.Path = newURL
	// For HTTPS prober, we change to HTTP,
	// and pilot agent uses https to request application prober endpoint.
	// Kubelet -> HTTP -> Pilot Agent -> HTTPS -> Application
	if p.HTTPGet.Scheme == corev1.URISchemeHTTPS {
		p.HTTPGet.Scheme = corev1.URISchemeHTTP
	}
	return p
}

// convertAppProberTCPSocket returns an overwritten `Probe` (TcpSocket) for pilot agent to take over.
func convertAppProberTCPSocket(probe *corev1.Probe, newURL string, statusPort int) *corev1.Probe {
	p := probe.DeepCopy()
	// the sidecar intercepts all tcp connections, so we change it to a HTTP probe and the sidecar will check tcp
	p.HTTPGet = &corev1.HTTPGetAction{}
	p.HTTPGet.Port = intstr.FromInt(statusPort)
	p.HTTPGet.Path = newURL

	p.TCPSocket = nil
	return p
}

// convertAppProberGRPC returns an overwritten `Probe` (gRPC) for pilot agent to take over.
func convertAppProberGRPC(probe *corev1.Probe, newURL string, statusPort int) *corev1.Probe {
	p := probe.DeepCopy()
	// the sidecar intercepts all gRPC connections, so we change it to a HTTP probe and the sidecar will check gRPC
	p.HTTPGet = &corev1.HTTPGetAction{}
	p.HTTPGet.Port = intstr.FromInt(statusPort)
	p.HTTPGet.Path = newURL
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
func DumpAppProbers(podSpec *corev1.PodSpec, targetPort int32) string {
	out := KubeAppProbers{}
	updateNamedPort := func(p *Prober, portMap map[string]int32) *Prober {
		if p == nil {
			return nil
		}
		if p.GRPC != nil {
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
			port, exists := portMap[probePort.StrVal]
			if !exists {
				return nil
			}
			*probePort = intstr.FromInt(int(port))
		} else if probePort.IntVal == targetPort {
			// Already is rewritten
			return nil
		}
		return p
	}
	for _, c := range podSpec.Containers {
		if c.Name == ProxyContainerName {
			continue
		}
		readyz, livez, startupz := status.FormatProberURL(c.Name)
		portMap := map[string]int32{}
		for _, p := range c.Ports {
			if p.Name != "" {
				portMap[p.Name] = p.ContainerPort
			}
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.ReadinessProbe), portMap); h != nil {
			out[readyz] = h
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.LivenessProbe), portMap); h != nil {
			out[livez] = h
		}
		if h := updateNamedPort(kubeProbeToInternalProber(c.StartupProbe), portMap); h != nil {
			out[startupz] = h
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
		readyz, livez, startupz := status.FormatProberURL(c.Name)
		if probePatch := convertAppProber(c.ReadinessProbe, readyz, statusPort); probePatch != nil {
			c.ReadinessProbe = probePatch
		}
		if probePatch := convertAppProber(c.LivenessProbe, livez, statusPort); probePatch != nil {
			c.LivenessProbe = probePatch
		}
		if probePatch := convertAppProber(c.StartupProbe, startupz, statusPort); probePatch != nil {
			c.StartupProbe = probePatch
		}
		pod.Spec.Containers[i] = c
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

	if probe.TCPSocket != nil && features.RewriteTCPProbes {
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
